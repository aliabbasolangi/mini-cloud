package blob

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFSIngestRenamesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFS(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "part.bin")
	if err := os.WriteFile(src, []byte("chunked-hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Ingest(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 13 {
		t.Fatalf("size %d", got.Size)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should be gone after ingest")
	}
	r, err := store.Get(context.Background(), got.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()

	src2 := filepath.Join(dir, "again.bin")
	if err := os.WriteFile(src2, []byte("chunked-hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := store.Ingest(context.Background(), src2)
	if err != nil || again.SHA256 != got.SHA256 {
		t.Fatalf("dedupe %v %+v", err, again)
	}
}
