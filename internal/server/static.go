package server

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"nyx-drop/internal/slug"
	"nyx-drop/internal/store"
)

func (s *siteRoutes) serve(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Revalidate cached assets so they cannot outlive the site's TTL.
	w.Header().Set("Cache-Control", "no-cache")
	if !slug.Valid(id) {
		s.serveNotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row, err := s.deps.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.serveNotFound(w, r)
		} else {
			s.deps.Logger.Error("lookup site failed", "id", id, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if !row.Permanent && !s.deps.Clock.Now().Before(row.ExpiresAt) {
		s.serveNotFound(w, r)
		return
	}

	dataRoot, err := os.OpenRoot(s.deps.Config.DataDir)
	if err != nil {
		s.serveNotFound(w, r)
		return
	}
	defer dataRoot.Close()
	parent, err := openSiteDir(dataRoot, "sites")
	if err != nil {
		s.serveNotFound(w, r)
		return
	}
	defer parent.Close()
	root, err := openSiteDir(parent, id)
	if err != nil { // e.g. reserved in SQLite but not yet renamed into view
		s.serveNotFound(w, r)
		return
	}
	defer root.Close()

	name, trailingSlash, ok := requestPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	file, servedName, err := openSiteFile(root, name, trailingSlash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	// Sites are immutable in this slice. The publication identity and final
	// path identify the representation without re-reading large files on
	// HEAD/Range. PUT must bind this validator to its selected generation.
	key := id + "\x00" + row.CreatedAt.Format(time.RFC3339Nano) + "\x00" + servedName
	w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(key))))
	http.ServeContent(w, r, path.Base(servedName), row.CreatedAt, file)
}

func requestPath(urlPath string) (name string, trailingSlash, ok bool) {
	if !strings.HasPrefix(urlPath, "/") {
		return "", false, false
	}
	if urlPath == "/" {
		return ".", true, true
	}
	name = strings.TrimSuffix(urlPath[1:], "/")
	if !fs.ValidPath(name) || strings.HasPrefix(name, ".") || strings.Contains(name, "/.") ||
		strings.ContainsRune(name, '\\') || strings.ContainsFunc(name, unicode.IsControl) {
		return "", false, false
	}
	return name, strings.HasSuffix(urlPath, "/"), true
}

// Resolve under a pinned parent and confirm the opened directory is the
// same non-symlink entry we inspected. The volume is app-owned; uploads
// cannot install symlinks, and os.Root also prevents escapes during opens.
func openSiteDir(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if err != nil || !info.IsDir() {
		return nil, fs.ErrNotExist
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		root.Close()
		return nil, fs.ErrNotExist
	}
	return root, nil
}

// openSiteFile walks through directory handles, never re-resolving the
// live site path. It never serves a directory listing or a special file.
func openSiteFile(root *os.Root, name string, trailingSlash bool) (*os.File, string, error) {
	dir := root
	defer func() {
		if dir != root {
			dir.Close()
		}
	}()
	remaining := name
	for {
		component, rest, nested := strings.Cut(remaining, "/")
		local, err := filepath.Localize(component)
		if err != nil {
			return nil, "", fs.ErrNotExist
		}
		if nested {
			next, err := openSiteDir(dir, local)
			if err != nil {
				return nil, "", err
			}
			if dir != root {
				dir.Close()
			}
			dir, remaining = next, rest
			continue
		}
		info, err := dir.Lstat(local)
		if err != nil {
			return nil, "", err
		}
		if info.IsDir() {
			next, err := openSiteDir(dir, local)
			if err != nil {
				return nil, "", err
			}
			if dir != root {
				dir.Close()
			}
			dir, local = next, "index.html"
			name = path.Join(name, local)
			info, err = dir.Lstat(local)
			if err != nil {
				return nil, "", err
			}
		} else if trailingSlash {
			return nil, "", fs.ErrNotExist
		}
		if !info.Mode().IsRegular() {
			return nil, "", fs.ErrNotExist
		}
		file, err := dir.Open(local)
		if err != nil {
			return nil, "", err
		}
		actual, err := file.Stat()
		if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
			file.Close()
			return nil, "", fs.ErrNotExist
		}
		return file, name, nil
	}
}
