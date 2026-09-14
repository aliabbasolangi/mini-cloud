package catalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUploadSessionAppendsInOrder(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	user, err := db.CreateUser(ctx, "ali@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := db.CreateUploadSession(ctx, user.ID, "", "movie.mp4", 100, 2, "wrap")
	if err != nil || sess.EncVer != 2 {
		t.Fatalf("create %v %+v", err, sess)
	}
	if _, err := db.UploadSession(ctx, sess.ID, "nope"); err != ErrUploadDenied {
		t.Fatalf("denied %v", err)
	}
	one, err := db.AddUploadBytes(ctx, sess.ID, user.ID, 40)
	if err != nil || one.Received != 40 || one.Parts != 1 {
		t.Fatalf("part1 %v %+v", err, one)
	}
	if _, err := db.AddUploadBytes(ctx, sess.ID, user.ID, 80); err != ErrUploadState {
		t.Fatalf("overflow %v", err)
	}
	two, err := db.AddUploadBytes(ctx, sess.ID, user.ID, 60)
	if err != nil || two.Received != 100 {
		t.Fatalf("part2 %v %+v", err, two)
	}
	obj, err := db.UpsertObjectEnc(ctx, user.ID, "movie.mp4", "abc", 100, 2, "wrap")
	if err != nil || obj.EncVer != 2 {
		t.Fatalf("enc2 %v %+v", err, obj)
	}
}
