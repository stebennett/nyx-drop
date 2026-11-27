// Package extract validates and stages untrusted site archives.
package extract

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

var (
	ErrArchive = errors.New("invalid ZIP archive")
	ErrPath    = errors.New("invalid or duplicate ZIP path")
	ErrEmpty   = errors.New("upload contains no files")
	ErrSize    = errors.New("site exceeds maximum uncompressed size")
	ErrCount   = errors.New("site exceeds maximum file count")
)

type Limits struct {
	SizeBytes int64
	FileCount int
}

type Result struct {
	Dir       string // descendant of the supplied staging directory
	SizeBytes int64
	FileCount int
}

// Limit the work of walking and creating parents to filesystem-sized paths.
const maxPathBytes = 4095

// ZIP extracts into a fresh private directory owned by the caller. The caller
// removes that directory on both success (after publication) and failure.
// No archive permissions, timestamps, links or special files are installed.
func ZIP(ctx context.Context, r io.ReaderAt, size int64, dir string, limits Limits) (Result, error) {
	return extractZIP(ctx, r, size, dir, limits, func(root *os.Root, name string) (io.WriteCloser, error) {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	})
}

func extractZIP(ctx context.Context, r io.ReaderAt, size int64, dir string, limits Limits, create func(*os.Root, string) (io.WriteCloser, error)) (Result, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		if errors.Is(err, zip.ErrInsecurePath) {
			return Result{}, ErrPath
		}
		return Result{}, readError(err)
	}

	// Validate names before filtering files. A skipped entry cannot hide a
	// malformed name, duplicate, or file/directory conflict in the upload.
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		name := strings.TrimSuffix(f.Name, "/")
		if !validName(name) {
			return Result{}, ErrPath
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			return Result{}, ErrPath
		}
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return Result{}, err
	}
	defer root.Close()
	parents := map[string]bool{".": true}
	var result Result
	var first, multiple bool
	var candidate string
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		name := strings.TrimSuffix(f.Name, "/")
		if !f.Mode().IsDir() {
			prefix := name + "/"
			i := sort.SearchStrings(names, prefix)
			if i < len(names) && strings.HasPrefix(names[i], prefix) {
				return Result{}, ErrPath
			}
		}
		if !f.Mode().IsRegular() || strings.HasPrefix(name, ".") || strings.Contains(name, "/.") {
			continue
		}
		if result.FileCount >= limits.FileCount {
			return Result{}, ErrCount
		}
		remaining := limits.SizeBytes - result.SizeBytes
		if f.UncompressedSize64 > uint64(remaining) {
			return Result{}, ErrSize
		}
		local, err := filepath.Localize(name)
		if err != nil {
			return Result{}, ErrPath
		}
		if err := makeParentDirs(root, filepath.Dir(local), parents); err != nil {
			return Result{}, err
		}
		n, err := copyFile(ctx, f, remaining, func() (io.WriteCloser, error) { return create(root, local) })
		if err != nil {
			return Result{}, err
		}
		result.FileCount++
		result.SizeBytes += n
		top, _, nested := strings.Cut(name, "/")
		if !first {
			first = true
			if nested {
				candidate = top
			}
		} else if top != candidate {
			multiple = true
		}
	}
	if !first {
		return Result{}, ErrEmpty
	}
	result.Dir = dir
	if candidate != "" && !multiple {
		result.Dir = filepath.Join(dir, candidate)
	}
	return result, nil
}

func validName(name string) bool {
	if len(name) > maxPathBytes || name == "." || !fs.ValidPath(name) || strings.ContainsRune(name, '\\') || strings.ContainsFunc(name, unicode.IsControl) {
		return false
	}
	// A drive-qualified name is not a relative archive path, even on Unix.
	return !(len(name) >= 2 && name[1] == ':' && (name[0] >= 'a' && name[0] <= 'z' || name[0] >= 'A' && name[0] <= 'Z'))
}

// Create only the parents of files being kept, never explicit empty dirs.
// Cache created parents and build each branch from the top down.
func makeParentDirs(root *os.Root, dir string, parents map[string]bool) error {
	var missing []string
	for !parents[dir] {
		missing = append(missing, dir)
		dir = filepath.Dir(dir)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := root.Mkdir(missing[i], 0700); err != nil {
			return fileError(err)
		}
		parents[missing[i]] = true
	}
	return nil
}

func copyFile(ctx context.Context, entry *zip.File, remaining int64, create func() (io.WriteCloser, error)) (int64, error) {
	r, err := entry.Open()
	if err != nil {
		return 0, readError(err)
	}
	defer r.Close()
	w, err := create()
	if err != nil {
		return 0, fileError(err)
	}

	// Limit actual inflated bytes, not just the central directory claims.
	// Probe for EOF without writing an extra byte, which also forces CRC
	// validation at the exact budget. No limit+1 arithmetic can overflow.
	src := checkedReader{ctx, r}
	n, copyErr := io.Copy(w, io.LimitReader(src, remaining))
	if copyErr == nil && n == remaining {
		var extra [1]byte
		if _, err := io.ReadFull(src, extra[:]); err == nil {
			copyErr = ErrSize
		} else if err != io.EOF {
			copyErr = err
		}
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	if errors.Is(copyErr, ErrSize) {
		return 0, copyErr
	}
	if copyErr != nil {
		return 0, readError(copyErr)
	}
	return n, nil
}

// OS I/O errors (including failed writes/ENOSPC) and cancellation are not
// invalid archives. zip/flate errors, checksum mismatches and truncated
// input are validation errors. The only production writer is an os.File.
func readError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrShortWrite) {
		return err
	}
	return ErrArchive
}

func fileError(err error) error {
	if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENAMETOOLONG) || errors.Is(err, syscall.ENOTDIR) {
		return ErrPath
	}
	return err
}

type checkedReader struct {
	ctx context.Context
	r   io.Reader
}

func (r checkedReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
