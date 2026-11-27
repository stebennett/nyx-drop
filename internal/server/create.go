package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nyx-drop/internal/extract"
	"nyx-drop/internal/lock"
	"nyx-drop/internal/slug"
	"nyx-drop/internal/store"
)

const createAttempts = 5

var (
	errMultipart = errors.New("expected multipart/form-data with exactly one ZIP file field named file")
	errCollision = errors.New("site ID reservation attempts exhausted")
)

// siteRoutes owns the site storage paths and the shared per-site mutation
// locks. Apex administration routes are registered separately.
type siteRoutes struct {
	*srv
	tmpDir, sitesDir string
	locks            lock.Sites
	newSlug          func() string
	extractZIP       func(context.Context, io.ReaderAt, int64, string, extract.Limits) (extract.Result, error)
	rename           func(string, string) error
}

func (s *srv) registerSiteRoutes() error {
	routes := &siteRoutes{
		srv: s, tmpDir: filepath.Join(s.deps.Config.DataDir, "tmp"), sitesDir: filepath.Join(s.deps.Config.DataDir, "sites"),
		newSlug: slug.New, extractZIP: extract.ZIP, rename: os.Rename,
	}
	for _, dir := range []string{routes.tmpDir, routes.sitesDir} {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("initialize site storage: %w", err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("check site storage: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("site storage is not a real directory: %s", dir)
		}
	}
	s.sites = routes
	s.apexMux.Handle("POST /api/sites", requireUploadToken(s.deps.Config.UploadToken, http.HandlerFunc(routes.create)))
	s.apexMux.HandleFunc("/api/sites", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	return nil
}

func requireUploadToken(token string, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) == 1 && token != "" {
			scheme, value, found := strings.Cut(values[0], " ")
			if found && strings.EqualFold(scheme, "Bearer") {
				got := sha256.Sum256([]byte(strings.TrimLeft(value, " ")))
				if subtle.ConstantTimeCompare(got[:], want[:]) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeAPIError(w, http.StatusUnauthorized, "invalid or expired upload credential")
	})
}

type createdSite struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *siteRoutes) create(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config
	if r.ContentLength > cfg.MaxUploadSize {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "upload exceeds maximum request size")
		return
	}

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		writeAPIError(w, http.StatusBadRequest, errMultipart.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxUploadSize)
	defer r.Body.Close()

	work, err := os.MkdirTemp(s.tmpDir, "upload-")
	if err != nil {
		s.createError(w, err)
		return
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil {
			s.deps.Logger.Error("clean upload staging", "error", err)
		}
	}()

	archive, err := os.CreateTemp(work, "archive-")
	if err != nil {
		s.createError(w, err)
		return
	}
	defer archive.Close()
	compressedBytes, err := readZIPPart(r.Body, params["boundary"], archive)
	if err != nil {
		s.createError(w, err)
		return
	}
	tree := filepath.Join(work, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		s.createError(w, err)
		return
	}
	result, err := s.extractZIP(r.Context(), archive, compressedBytes, tree, extract.Limits{
		SizeBytes: cfg.MaxSiteSize, FileCount: cfg.MaxFileCount,
	})
	if err != nil {
		s.createError(w, err)
		return
	}

	row, err := s.publish(r.Context(), result)
	if err != nil {
		s.createError(w, err)
		return
	}
	s.deps.Metrics.SitesCreated.Inc()
	s.deps.Metrics.UploadBytes.Add(float64(compressedBytes))
	s.deps.Logger.Info("site created", "id", row.ID, "expires_at", row.ExpiresAt, "size_bytes", row.SizeBytes, "file_count", row.FileCount)
	writeJSON(w, http.StatusCreated, createdSite{row.ID, cfg.SiteURL(row.ID), row.ExpiresAt})
}

func readZIPPart(body io.Reader, boundary string, dst *os.File) (int64, error) {
	mr := multipart.NewReader(body, boundary)
	part, err := mr.NextRawPart()
	if err != nil {
		return 0, multipartError(err)
	}
	if part.FormName() != "file" || part.FileName() == "" {
		return 0, errMultipart
	}
	n, err := io.Copy(dst, part)
	if err != nil {
		return 0, multipartError(err)
	}
	_, err = mr.NextRawPart()
	// NextRawPart wraps some truncated-body EOFs, distinguish those from
	// the exact EOF returned after a complete closing boundary.
	if err == nil {
		return 0, errMultipart
	}
	if err != io.EOF {
		return 0, multipartError(err)
	}
	// The multipart reader may stop at the closing boundary. Its buffered
	// read-ahead was counted already; drain the unread epilogue through the
	// same MaxBytesReader before admitting the request.
	if _, err := io.Copy(io.Discard, body); err != nil {
		return 0, multipartError(err)
	}
	return n, nil
}

func multipartError(err error) error {
	var max *http.MaxBytesError
	var storage *fs.PathError
	if errors.As(err, &max) || errors.As(err, &storage) || errors.Is(err, io.ErrShortWrite) {
		return err
	}
	return errMultipart
}

func (s *siteRoutes) publish(ctx context.Context, result extract.Result) (store.Site, error) {
	now := s.deps.Clock.Now().UTC()
	row := store.Site{
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(s.deps.Config.TTL),
		SizeBytes: result.SizeBytes, FileCount: result.FileCount, Source: "api",
	}
	for i := 0; i < createAttempts; i++ {
		if err := ctx.Err(); err != nil {
			return store.Site{}, err
		}
		row.ID = s.newSlug()
		if !slug.Valid(row.ID) {
			return store.Site{}, errors.New("generator returned an invalid site ID")
		}
		if err := s.reserveAndMove(ctx, row, result.Dir); errors.Is(err, store.ErrDuplicate) {
			continue
		} else if err != nil {
			return store.Site{}, err
		}
		return row, nil
	}
	return store.Site{}, errCollision
}

func (s *siteRoutes) reserveAndMove(ctx context.Context, row store.Site, tree string) error {
	unlock := s.locks.Lock(row.ID)
	defer unlock()
	dest := filepath.Join(s.sitesDir, row.ID)
	// Even a DB-less tree may remain after a crash. Never reserve its slug
	// (which could expose the orphan) or replace it. Storage is app-owned,
	// and all mutations in this process take the same per-site lock.
	if _, err := os.Lstat(dest); err == nil {
		return store.ErrDuplicate
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := s.deps.Store.Create(ctx, row); err != nil {
		return err
	}
	if err := s.rename(tree, dest); err != nil {
		// A disconnected upload client must not cancel compensation.
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if deleteErr := s.deps.Store.Delete(rollback, row.ID); deleteErr != nil {
			return errors.Join(err, fmt.Errorf("roll back site reservation %s: %w", row.ID, deleteErr))
		}
		return err
	}
	return nil
}

func (s *siteRoutes) createError(w http.ResponseWriter, err error) {
	var max *http.MaxBytesError
	switch {
	case errors.As(err, &max):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "upload exceeds maximum request size")
	case errors.Is(err, extract.ErrCount), errors.Is(err, extract.ErrSize):
		writeAPIError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, errMultipart), errors.Is(err, extract.ErrArchive), errors.Is(err, extract.ErrPath), errors.Is(err, extract.ErrEmpty):
		writeAPIError(w, http.StatusBadRequest, err.Error())
	default:
		// Filesystem errors can contain untrusted archive entry names.
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = fmt.Errorf("%s: %w", pe.Op, pe.Err)
		}
		s.deps.Logger.Error("create site failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
