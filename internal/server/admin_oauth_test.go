package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"nyx-drop/internal/auth"
	"nyx-drop/internal/clock"
	"nyx-drop/internal/config"
	"nyx-drop/internal/metrics"
)

const (
	testOAuthClient = "stub-client-id"
	testOAuthSecret = "stub-client-secret"
	testOAuthCode   = "stub-authorization-code"
	testOAuthToken  = "stub-access-token"
	testSessionKey  = "stub-cookie-signing-key"
)

func authFixture(t *testing.T, scheme string, log io.Writer, provider http.HandlerFunc) (http.Handler, *clock.Fake, string) {
	t.Helper()
	stub := httptest.NewServer(provider)
	t.Cleanup(stub.Close)
	if log == nil {
		log = io.Discard
	}
	cfg := &config.Config{
		BaseDomain: testBaseDomain, Scheme: scheme, Port: 8080, PublicPort: 9443,
		GitHubClientID: testOAuthClient, GitHubClientSecret: testOAuthSecret,
		AdminGitHubUser: "octocat", SessionSecret: testSessionKey,
	}
	fc := clock.NewFake(time.Unix(1783684800, 0))
	reg := prometheus.NewRegistry()
	h, err := New(Deps{
		Config: cfg, Clock: fc, Logger: slog.New(slog.NewJSONHandler(log, nil)),
		Metrics: metrics.New(reg), Registry: reg, Health: func(context.Context) error { return nil },
		GitHubEndpoints: auth.GitHubEndpoints{
			AuthorizeURL: stub.URL + "/login/oauth/authorize",
			TokenURL:     stub.URL + "/login/oauth/access_token",
			UserURL:      stub.URL + "/user",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, fc, scheme + "://" + testBaseDomain + ":9443"
}

func serveAuth(h http.Handler, r *http.Request, status int, t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != status {
		t.Fatalf("%s %s returned %d, want %d", r.Method, r.URL.Path, rec.Code, status)
	}
	return rec
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func assertAuthCookie(t *testing.T, rec *httptest.ResponseRecorder, name string, secure bool, age int) *http.Cookie {
	t.Helper()
	c := findCookie(rec, name)
	if c == nil {
		t.Fatalf("missing cookie %s", name)
	}
	if c.Path != "/" || c.Domain != "" || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != age {
		t.Fatalf("%s cookie does not have the expected scope, security attributes, and age", name)
	}
	if age < 0 && (c.Value != "" || c.Expires.IsZero()) {
		t.Fatalf("%s deletion must clear value and give a past expiry", name)
	}
	return c
}

func TestAdminOAuthRoundTrip(t *testing.T) {
	for _, scheme := range []string{"https", "http"} {
		t.Run(scheme, func(t *testing.T) {
			var log strings.Builder
			var exchanges, users atomic.Int32
			challenge := make(chan string, 1)
			wantCallback := scheme + "://" + testBaseDomain + ":9443/auth/callback"
			h, _, origin := authFixture(t, scheme, &log, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/login/oauth/authorize":
					q := r.URL.Query()
					state := q.Get("state")
					if r.Method != http.MethodGet || q.Get("client_id") != testOAuthClient || q.Get("redirect_uri") != wantCallback || len(state) != 43 || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("scope") != "" || q.Get("prompt") != "select_account" {
						t.Error("wrong authorize request")
					}
					challenge <- q.Get("code_challenge")
					http.Redirect(w, r, wantCallback+"?"+url.Values{"code": {testOAuthCode}, "state": {state}}.Encode(), http.StatusFound)
				case "/login/oauth/access_token":
					if exchanges.Add(1) != 1 {
						http.Error(w, "code already used", http.StatusBadRequest)
						return
					}
					v := r.PostFormValue("code_verifier")
					sum := sha256.Sum256([]byte(v))
					if r.Method != http.MethodPost || r.PostFormValue("grant_type") != "authorization_code" ||
						r.PostFormValue("client_id") != testOAuthClient || r.PostFormValue("client_secret") != testOAuthSecret ||
						r.PostFormValue("code") != testOAuthCode || r.PostFormValue("redirect_uri") != wantCallback || v == "" {
						t.Error("wrong token exchange")
					}
					select {
					case c := <-challenge:
						if base64.RawURLEncoding.EncodeToString(sum[:]) != c {
							t.Error("verifier does not match challenge")
						}
					default:
						t.Error("token endpoint reached before authorize")
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]string{"access_token": testOAuthToken, "token_type": "bearer"})
				case "/user":
					users.Add(1)
					if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+testOAuthToken {
						t.Error("wrong user request")
					}
					io.WriteString(w, `{"login":"OcToCaT"}`)
				default:
					http.NotFound(w, r)
				}
			})
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			visit := func(method, target string, status int) *httptest.ResponseRecorder {
				t.Helper()
				if strings.HasPrefix(target, "/") {
					target = origin + target
				}
				r := httptest.NewRequest(method, target, nil)
				r.Host = testBaseDomain + ":8080" // backend Host port not the public port
				r.Header.Set("X-Forwarded-Host", "not-the-public-host.example:7777")
				r.Header.Set("X-Forwarded-Proto", "bogus")
				if method == http.MethodPost {
					r.Header.Set("Origin", origin)
				}
				for _, c := range jar.Cookies(r.URL) {
					r.AddCookie(c)
				}
				rec := serveAuth(h, r, status, t)
				jar.SetCookies(r.URL, rec.Result().Cookies())
				if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("missing auth cache/referrer headers")
				}
				return rec
			}
			if visit(http.MethodGet, "/admin", 302).Header().Get("Location") != "/auth/login" {
				t.Fatal("wrong sign-in location")
			}
			login := visit(http.MethodGet, "/auth/login", 302)
			secure := scheme == "https"
			prefix := ""
			if secure {
				prefix = "__Host-"
			}
			state := assertAuthCookie(t, login, prefix+"oauth-state", secure, 600)
			client := &http.Client{
				Timeout:       5 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
			provider, err := client.Get(login.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			provider.Body.Close()
			location := provider.Header.Get("Location")
			if provider.StatusCode != 302 || !strings.HasPrefix(location, wantCallback+"?") {
				t.Fatal("stub did not redirect to configured callback")
			}
			cb := visit(http.MethodGet, location, 302)
			if cb.Header().Get("Location") != "/admin" {
				t.Fatal("wrong callback redirect")
			}
			assertAuthCookie(t, cb, prefix+"oauth-state", secure, -1)
			session := assertAuthCookie(t, cb, prefix+"session", secure, 604800)
			if body := visit(http.MethodGet, "/admin", 200).Body.String(); !strings.Contains(body, `action="/auth/logout"`) {
				t.Fatal("admin page missing sign-out form")
			}
			visit(http.MethodGet, "/api/admin/sites", 404) // only a guarded placeholder
			visit(http.MethodGet, location, 403)           // browser no longer sends state
			if exchanges.Load() != 1 || users.Load() != 1 {
				t.Fatal("unexpected provider request counts")
			}
			logout := visit(http.MethodPost, "/auth/logout", 302)
			assertAuthCookie(t, logout, prefix+"session", secure, -1)
			if logout.Header().Get("Location") != "/" {
				t.Fatal("wrong logout redirect")
			}
			visit(http.MethodGet, "/admin", 302)
			visit(http.MethodGet, "/api/admin/sites", 401)

			logs := log.String()
			if strings.Count(logs, `"msg":"admin login","user":"octocat"`) != 1 || !strings.Contains(logs, `"path":"/auth/callback"`) {
				t.Fatal("missing structured login or request event")
			}
			for _, protected := range []string{testOAuthSecret, testOAuthCode, testOAuthToken, testSessionKey, state.Value, session.Value, "code_verifier", "?code="} {
				if strings.Contains(logs, protected) {
					t.Fatal("credentials or callback query appeared in logs")
				}
			}
		})
	}
}
