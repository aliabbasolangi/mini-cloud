package blob

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound means that fingerprint is not on the shelf.
var ErrNotFound = errors.New("blob not found")

// Result is what you get after putting a file on the shelf.
type Result struct {
	SHA256 string // fingerprint of the bytes
	Size   int64  // how many bytes we stored
}

// Store is the shelf: put raw bytes in, get them back by fingerprint.
// Later we can swap the disk implementation for R2/S3 without changing the API.
type Store interface {
	Put(ctx context.Context, r io.Reader) (Result, error)
	Get(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
