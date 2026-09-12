package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FS stores blobs as files named by their fingerprint.
// Example: data/blobs/9f86d08... (the hash of the file contents)
type FS struct {
	dir string
}

func NewFS(dir string) (*FS, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &FS{dir: dir}, nil
}

func (s *FS) Put(ctx context.Context, r io.Reader) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	tmp, err := os.CreateTemp(s.dir, "upload-*.tmp")
	if err != nil {
		return Result{}, err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op if we already renamed it into place
	}()

	// Write the file and fingerprint it in one pass — we never load the
	// whole file into memory. A 2 GB upload uses a small buffer, not 2 GB of RAM.
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return Result{}, err
	}
	if err := tmp.Close(); err != nil {
		return Result{}, err
	}

	sum := hex.EncodeToString(h.Sum(nil))
	dest := filepath.Join(s.dir, sum)

	if err := os.Rename(tmpName, dest); err != nil {
		if !errors.Is(err, os.ErrExist) && !fileExists(dest) {
			return Result{}, err
		}
		// Same fingerprint already on the shelf — that is a duplicate upload.
		_ = os.Remove(tmpName)
	}

	return Result{SHA256: sum, Size: size}, nil
}

func (s *FS) Get(ctx context.Context, sum string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validSHA256(sum) {
		return nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.dir, sum))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func validSHA256(sum string) bool {
	if len(sum) != 64 {
		return false
	}
	for _, c := range strings.ToLower(sum) {
		if c < '0' || (c > '9' && c < 'a') || c > 'f' {
			return false
		}
	}
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
