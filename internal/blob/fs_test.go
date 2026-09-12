package blob

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestFSPutAndGet(t *testing.T) {
	store, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Put(context.Background(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 5 {
		t.Fatalf("size: got %d want 5", got.Size)
	}
	if len(got.SHA256) != 64 {
		t.Fatalf("sha256 should be 64 hex chars, got %q", got.SHA256)
	}

	r, err := store.Get(context.Background(), got.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("body: got %q want hello", body)
	}
}

func TestFSGetMissing(t *testing.T) {
	store, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != ErrNotFound {
		t.Fatalf("got %v want ErrNotFound", err)
	}
}
