package catalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUserAndObjectRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	user, err := db.CreateUser(ctx, "Ali@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "ali@example.com" {
		t.Fatalf("email should be lowercased, got %q", user.Email)
	}

	_, err = db.CreateUser(ctx, "ali@example.com", "hash")
	if err != ErrEmailTaken {
		t.Fatalf("got %v want ErrEmailTaken", err)
	}

	obj, err := db.UpsertObject(ctx, user.ID, "holiday.jpg", "abc", 10)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Key != "holiday.jpg" {
		t.Fatalf("key %q", obj.Key)
	}

	got, err := db.ObjectByKey(ctx, user.ID, "holiday.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if got.BlobSHA != "abc" {
		t.Fatalf("sha %q", got.BlobSHA)
	}

	list, err := db.ListObjects(ctx, user.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %d", err, len(list))
	}

	share, err := db.CreateShare(ctx, user.ID, "holiday.jpg")
	if err != nil {
		t.Fatal(err)
	}
	gotShare, err := db.ValidShare(ctx, share.Token)
	if err != nil || gotShare.ObjectKey != "holiday.jpg" {
		t.Fatalf("share: %v %+v", err, gotShare)
	}
	if err := db.RenameObject(ctx, user.ID, "holiday.jpg", "photos/holiday.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ObjectByKey(ctx, user.ID, "holiday.jpg"); err != ErrObjectNotFound {
		t.Fatal("old name should be gone after a move")
	}
	moved, err := db.ObjectByKey(ctx, user.ID, "photos/holiday.jpg")
	if err != nil || moved.BlobSHA != "abc" {
		t.Fatalf("moved: %v %+v", err, moved)
	}

	if err := db.RevokeShare(ctx, user.ID, share.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ValidShare(ctx, share.Token); err != ErrShareNotFound {
		t.Fatalf("revoked share: %v", err)
	}

	if err := db.DeleteObject(ctx, user.ID, "photos/holiday.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ObjectByKey(ctx, user.ID, "photos/holiday.jpg"); err != ErrObjectNotFound {
		t.Fatalf("got %v want not found", err)
	}
}
