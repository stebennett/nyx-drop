package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func testZIP(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "fixture-content"); err != nil {
		t.Fatal(err)
	}
	empty, err := zw.Create("empty/")
	if err != nil || empty == nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestZIPPromotesOnceAndValidatesChecksumAtLimit(t *testing.T) {
	data := testZIP(t, "wrap/inner/index.html")
	root := t.TempDir()
	limits := Limits{SizeBytes: int64(len("fixture-content")), FileCount: 1}
	result, err := ZIP(context.Background(), bytes.NewReader(data), int64(len(data)), root, limits)
	if err != nil || result.Dir != filepath.Join(root, "wrap") || result.FileCount != 1 || result.SizeBytes != limits.SizeBytes {
		t.Fatalf("result = %+v %v", result, err)
	}
	if data, err := os.ReadFile(filepath.Join(result.Dir, "inner", "index.html")); err != nil || string(data) != "fixture-content" {
		t.Fatalf("extracted bytes = %q %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "wrap" {
		t.Fatalf("expected only the nonempty directory: %v %v", entries, err)
	}
	at := bytes.Index(data, []byte("fixture-content"))
	if at < 0 {
		t.Fatal("missing stored data in fixture")
	}
	data[at] ^= 1
	if _, err := ZIP(context.Background(), bytes.NewReader(data), int64(len(data)), t.TempDir(), limits); !errors.Is(err, ErrArchive) {
		t.Fatalf("expected checksum validation failure at the exact byte limit, got %v", err)
	}
}

func TestZIPPreservesStorageError(t *testing.T) {
	data := testZIP(t, "site/index.html")
	dir := t.TempDir()
	var w diskFullWriter
	_, err := extractZIP(context.Background(), bytes.NewReader(data), int64(len(data)), dir, Limits{SizeBytes: 100, FileCount: 1},
		func(_ *os.Root, _ string) (io.WriteCloser, error) { return &w, nil })
	if !errors.Is(err, syscall.ENOSPC) || !w.wrote || !w.closed {
		t.Fatalf("ENOSPC was not propagated from a real write attempt: %v %+v", err, w)
	}
}

func TestZIPRejectsUnusableFilesystemComponent(t *testing.T) {
	data := testZIP(t, strings.Repeat("a", 1024)+"/index.html")
	if _, err := ZIP(context.Background(), bytes.NewReader(data), int64(len(data)), t.TempDir(), Limits{100, 1}); !errors.Is(err, ErrPath) {
		t.Fatalf("expected invalid path for a directory name the filesystem cannot create, got %v", err)
	}
}

func TestZIPStopsOnCancellationDuringExtraction(t *testing.T) {
	data := testZIP(t, "index.html")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var w diskFullWriter
	_, err := extractZIP(ctx, bytes.NewReader(data), int64(len(data)), t.TempDir(), Limits{100, 1},
		func(_ *os.Root, _ string) (io.WriteCloser, error) {
			cancel()
			return &w, nil
		})
	if !errors.Is(err, context.Canceled) || w.wrote || !w.closed {
		t.Fatalf("cancelled extraction wrote to storage or failed to close: %v %+v", err, w)
	}
}

type diskFullWriter struct{ wrote, closed bool }

func (w *diskFullWriter) Write(p []byte) (int, error) {
	w.wrote = len(p) > 0
	return 1, &os.PathError{Op: "write", Path: "index.html", Err: syscall.ENOSPC}
}

func (w *diskFullWriter) Close() error {
	w.closed = true
	return nil
}
