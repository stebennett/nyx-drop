package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"nyx-drop/internal/clock"
	"nyx-drop/internal/config"
	"nyx-drop/internal/extract"
	"nyx-drop/internal/metrics"
	"nyx-drop/internal/store"
)

const testUploadToken = "unit-test-credential"

func siteConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		BaseDomain: testBaseDomain, Scheme: "https", Port: 8080, PublicPort: 9443,
		DataDir: t.TempDir(), TTL: time.Hour, UploadToken: testUploadToken,
		MaxUploadSize: 64 << 10, MaxSiteSize: 64, MaxFileCount: 3,
	}
}

func siteDeps(cfg *config.Config, clk clock.Clock, db *store.DB) Deps {
	reg := prometheus.NewRegistry()
	return Deps{Config: cfg, Clock: clk, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics: metrics.New(reg), Registry: reg, Health: store.Readiness(db, cfg.DataDir), Store: db}
}

func siteServer(t *testing.T, cfg *config.Config, clk clock.Clock) (http.Handler, *store.DB) {
	t.Helper()
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h, err := New(siteDeps(cfg, clk, db))
	if err != nil {
		t.Fatal(err)
	}
	return h, db
}

type zipEntry struct {
	name, body string
	mode       os.FileMode
}

func archive(t *testing.T, files ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		h := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		h.SetMode(file.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartBody(t *testing.T, data []byte, fields ...string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, field := range fields {
		part, err := w.CreateFormFile(field, "site.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func uploadRequest(body io.Reader, contentType, credential string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://"+testBaseDomain+"/api/sites", body)
	r.Header.Set("Content-Type", contentType)
	if credential != "" {
		r.Header.Set("Authorization", credential)
	}
	return r
}

func record(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPublishZIPAndServeAcrossRestart(t *testing.T) {
	cfg := siteConfig(t)
	start := time.Date(2026, 11, 27, 5, 30, 0, 900, time.FixedZone("test", -7*3600))
	clk := clock.NewFake(start)
	h, db := siteServer(t, cfg, clk)
	body, ct := multipartBody(t, archive(t,
		zipEntry{"bundle/index.html", "<h1>hello</h1>", 0},
		zipEntry{"bundle/docs/index.html", "docs", 0},
		zipEntry{"bundle/assets/site.css", "a{}", 0},
		zipEntry{"bundle/.private/ignored", strings.Repeat("x", 100), 0},
		zipEntry{"bundle/link", "index.html", os.ModeSymlink},
	), "file")
	upload := uploadRequest(body, ct, "Bearer "+testUploadToken)
	upload.Header.Set("X-Forwarded-Host", "untrusted.example")
	upload.Header.Set("X-Forwarded-Proto", "http")
	res := record(h, upload)
	if res.Code != http.StatusCreated || res.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("upload = %d %s", res.Code, res.Body)
	}
	var created struct {
		ID        string `json:"id"`
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z0-9]{6}$`).MatchString(created.ID) || len(created.ID) > 63 ||
		created.URL != "https://"+created.ID+"."+testBaseDomain+":9443" ||
		created.ExpiresAt != start.Add(cfg.TTL).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("invalid creation response %+v", created)
	}
	row, err := db.Get(context.Background(), created.ID)
	if err != nil || row.Source != "api" || row.Permanent || row.FileCount != 3 || row.SizeBytes != 21 ||
		!row.CreatedAt.Equal(start) || !row.UpdatedAt.Equal(start) || !row.ExpiresAt.Equal(start.Add(cfg.TTL)) {
		t.Fatalf("invalid row: %+v, %v", row, err)
	}
	get := func(method, suffix, key, value string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, created.URL+suffix, nil)
		r.Host = strings.ToUpper(r.Host)
		if key != "" {
			r.Header.Set(key, value)
		}
		w := record(h, r)
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s %s lacks nosniff", method, suffix)
		}
		return w
	}
	first := get("GET", "/", "", "")
	etag := first.Header().Get("ETag")
	if first.Code != 200 || first.Body.String() != "<h1>hello</h1>" || etag == "" || !strings.HasPrefix(first.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("initial GET: %d %v %s", first.Code, first.Header(), first.Body)
	}
	t.Logf("upload=201 id=%s url=%s expires_at=%s initial GET=200", created.ID, created.URL, created.ExpiresAt)
	for _, tc := range []struct {
		method, path, header, value string
		status                      int
		body                        string
	}{
		{"GET", "/docs", "", "", 200, "docs"},
		{"GET", "/docs/", "", "", 200, "docs"},
		{"GET", "/assets/site.css", "", "", 200, "a{}"},
		{"GET", "/assets/", "", "", 404, "404 page not found\n"},
		{"GET", "/.private/ignored", "", "", 404, "404 page not found\n"},
		{"GET", "/link", "", "", 404, "404 page not found\n"},
		{"HEAD", "/", "", "", 200, ""},
		{"GET", "/index.html", "If-None-Match", etag, 304, ""},
		{"GET", "/", "If-Modified-Since", first.Header().Get("Last-Modified"), 304, ""},
		{"GET", "/", "Range", "bytes=4-8", 206, "hello"},
		{"GET", "/", "If-Match", `"different"`, 412, ""},
	} {
		w := get(tc.method, tc.path, tc.header, tc.value)
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Errorf("%s %s (%s): got %d %q, want %d %q", tc.method, tc.path, tc.header, w.Code, w.Body, tc.status, tc.body)
		}
	}
	if w := get("POST", "/index.html", "", ""); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("unexpected site method response: %d %v", w.Code, w.Header())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	h, db = siteServer(t, cfg, clk)
	if w := get("GET", "/", "", ""); w.Code != 200 || w.Header().Get("ETag") != etag || w.Body.String() != first.Body.String() {
		t.Fatalf("GET after restart: %d %v %s", w.Code, w.Header(), w.Body)
	}
	if first.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("site bytes must be revalidated before reuse")
	}
	t.Log("reopened the same data directory: GET=200 with identical bytes and ETag")
	clk.Advance(cfg.TTL - time.Nanosecond)
	if w := get("GET", "/", "", ""); w.Code != 200 {
		t.Fatalf("before expiry: %d", w.Code)
	}
	clk.Advance(time.Nanosecond)
	assertBranded404(t, get("GET", "/", "If-None-Match", etag))
	if _, err := db.Get(context.Background(), created.ID); err != nil {
		t.Fatalf("expiry test must keep the row intact: %v", err)
	}
	if err := db.SetPermanent(context.Background(), created.ID, true, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if w := get("GET", "/", "", ""); w.Code != 200 {
		t.Fatalf("permanent site should ignore expiry: %d", w.Code)
	}
	assertBranded404(t, record(h, httptest.NewRequest("GET", "http://unknown."+testBaseDomain+"/", nil)))
	assertBranded404(t, record(h, httptest.NewRequest("GET", "http://two.labels."+testBaseDomain+"/", nil)))
	if _, err := url.ParseRequestURI(created.URL); err != nil {
		t.Fatal(err)
	}
}

func TestZIPCreateErrors(t *testing.T) {
	cfg := siteConfig(t)
	h, _ := siteServer(t, cfg, clock.NewFake(time.Time{}))
	method := record(h, httptest.NewRequest("GET", "http://"+testBaseDomain+"/api/sites", nil))
	assertJSONError(t, method, 405)
	if method.Header().Get("Allow") != "POST" {
		t.Fatalf("Allow = %q", method.Header().Get("Allow"))
	}
	good := archive(t, zipEntry{"index.html", "ok", 0})
	badArchive := func(names ...string) []byte {
		t.Helper()
		var es []zipEntry
		for _, name := range names {
			es = append(es, zipEntry{name, "", 0})
		}
		return archive(t, es...)
	}
	for _, tc := range []struct {
		name, auth string
		files      []byte
		fields     []string
		status     int
	}{
		{"missing token", "", good, []string{"file"}, 401},
		{"bad token", "Bearer wrong", good, []string{"file"}, 401},
		{"wrong scheme", "Basic " + testUploadToken, good, []string{"file"}, 401},
		{"bad content type", "authorized", nil, nil, 400},
		{"invalid zip", "authorized", []byte("not a zip"), []string{"file"}, 400},
		{"no file", "authorized", good, []string{}, 400},
		{"unknown field", "authorized", good, []string{"other"}, 400},
		{"two files", "authorized", good, []string{"file", "file"}, 400},
		{"mixed", "authorized", good, []string{"file", "files"}, 400},
		{"traversal", "authorized", badArchive("safe/../outside"), []string{"file"}, 400},
		{"absolute", "authorized", badArchive("/outside"), []string{"file"}, 400},
		{"backslash", "authorized", badArchive(`dir\file`), []string{"file"}, 400},
		{"control", "authorized", badArchive("a\x7fb"), []string{"file"}, 400},
		{"non-UTF8", "authorized", badArchive("a\xffb"), []string{"file"}, 400},
		{"dot component", "authorized", badArchive("a/./b"), []string{"file"}, 400},
		{"duplicate", "authorized", badArchive("same", "same"), []string{"file"}, 400},
		{"ancestor conflict", "authorized", badArchive("same/child", "same-elsewhere", "same"), []string{"file"}, 400},
		{"drive path", "authorized", badArchive("C:/absolute"), []string{"file"}, 400},
		{"overlong path", "authorized", badArchive(strings.Repeat("a/", 2048) + "file"), []string{"file"}, 400},
		{"empty archive", "authorized", badArchive(), []string{"file"}, 400},
		{"dotfiles only", "authorized", badArchive(".hidden", "dir/.git/content"), []string{"file"}, 400},
		{"size limit", "authorized", archive(t, zipEntry{"large", strings.Repeat("a", 65), 0}), []string{"file"}, 413},
		{"count limit", "authorized", badArchive("a", "b", "c", "d"), []string{"file"}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r *http.Request
			credential := tc.auth
			if credential == "authorized" {
				credential = "Bearer " + testUploadToken
			}
			if tc.fields == nil {
				r = uploadRequest(strings.NewReader("body"), "text/plain", credential)
			} else {
				body, ct := multipartBody(t, tc.files, tc.fields...)
				r = uploadRequest(body, ct, credential)
			}
			assertJSONError(t, record(h, r), tc.status)
		})
	}
	for _, chunked := range []bool{false, true} {
		body, ct := multipartBody(t, good, "file")
		body.Write(bytes.Repeat([]byte("e"), int(cfg.MaxUploadSize)))
		r := uploadRequest(body, ct, "Bearer "+testUploadToken)
		if chunked {
			r.ContentLength = -1 // includes bytes after the final multipart boundary
		}
		assertJSONError(t, record(h, r), 413)
	}
	assertEmptyDirs(t, cfg.DataDir, "tmp", "sites")
}

func TestPublishFailureAndCollision(t *testing.T) {
	for _, scenario := range []string{"retry", "exhaust", "orphan", "rename", "no space", "store closed"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := siteConfig(t)
			clk := clock.NewFake(time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC))
			h, db := siteServer(t, cfg, clk)
			s := h.(*srv).sites
			const old, fresh = "amber-otter-abc123", "amber-otter-xyz456"
			var calls, expectedRows int
			status, expectedCalls := 500, 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.newSlug = func() string { calls++; return old }
			switch scenario {
			case "retry", "exhaust":
				now := clk.Now()
				if err := db.Create(ctx, store.Site{ID: old, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), Source: "api"}); err != nil {
					t.Fatal(err)
				}
				expectedRows = 1
				expectedCalls = 5
				if scenario == "retry" {
					status, expectedCalls, expectedRows = 201, 2, 2
					s.newSlug = func() string {
						calls++
						if calls == 1 {
							return old
						}
						return fresh
					}
				}
			case "orphan":
				if err := os.Mkdir(filepath.Join(s.sitesDir, old), 0700); err != nil {
					t.Fatal(err)
				}
				expectedCalls = 5
			case "rename":
				s.rename = func(src, dst string) error {
					if _, err := db.Get(ctx, old); err != nil {
						t.Fatalf("slug must be reserved before rename: %v", err)
					}
					if data, err := os.ReadFile(filepath.Join(src, "index.html")); err != nil || string(data) != "complete" {
						t.Fatalf("expected complete staged tree: %q %v", data, err)
					}
					assertBranded404(t, record(h, httptest.NewRequest("GET", "http://"+old+"."+testBaseDomain+"/", nil)))
					cancel()
					return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.ENOSPC}
				}
			case "no space":
				expectedCalls = 0
				s.extractZIP = func(_ context.Context, _ io.ReaderAt, _ int64, dir string, _ extract.Limits) (extract.Result, error) {
					p := filepath.Join(dir, "partial.html")
					if err := os.WriteFile(p, []byte("partial"), 0600); err != nil {
						t.Fatal(err)
					}
					return extract.Result{}, &os.PathError{Op: "write", Path: p, Err: syscall.ENOSPC}
				}
			case "store closed":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			body, ct := multipartBody(t, archive(t, zipEntry{"index.html", "complete", 0}), "file")
			w := record(h, uploadRequest(body, ct, "bearer "+testUploadToken).WithContext(ctx))
			if status == 201 {
				if w.Code != 201 || !strings.Contains(w.Body.String(), fresh) {
					t.Fatalf("retry = %d %s", w.Code, w.Body)
				}
			} else {
				assertJSONError(t, w, status)
				if w.Body.String() != "{\"error\":\"internal error\"}\n" {
					t.Fatalf("internal error response exposed details: %s", w.Body)
				}
			}
			if calls != expectedCalls {
				t.Fatalf("slug attempts = %d, want %d", calls, expectedCalls)
			}
			assertEmptyDirs(t, cfg.DataDir, "tmp")
			if scenario != "retry" && scenario != "orphan" {
				assertEmptyDirs(t, cfg.DataDir, "sites")
			}
			if scenario == "orphan" {
				if _, err := os.Stat(filepath.Join(s.sitesDir, old)); err != nil {
					t.Fatalf("preexisting orphan removed: %v", err)
				}
				assertBranded404(t, record(h, httptest.NewRequest("GET", "http://"+old+"."+testBaseDomain+"/", nil)))
			}
			if scenario != "store closed" {
				if n, _, err := db.Counts(context.Background()); err != nil || n != expectedRows {
					t.Fatalf("stored rows = %d, want %d (err=%v)", n, expectedRows, err)
				}
			}
			if scenario == "rename" {
				if _, err := db.Get(context.Background(), old); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("reservation not rolled back: %v", err)
				}
			}
		})
	}
}

func assertJSONError(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); w.Code != status || err != nil || len(payload) != 1 || payload["error"] == "" || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected JSON error %d, got %d %v %s", status, w.Code, w.Header(), w.Body)
	}
}

func assertEmptyDirs(t *testing.T, data string, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(data, dir))
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s not empty: %v %v", dir, entries, err)
		}
	}
}
