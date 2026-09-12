package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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

	if _, err := db.UpsertObject(ctx, user.ID, "photos/extra.png", "def", 20); err != nil {
		t.Fatal(err)
	}
	n, err := db.DeletePrefix(ctx, user.ID, "photos/")
	if err != nil || n != 2 {
		t.Fatalf("delete folder: %v n=%d", err, n)
	}

	if _, err := db.UpsertObject(ctx, user.ID, "keep.txt", "zzz", 3); err != nil {
		t.Fatal(err)
	}
	used, err := db.UsageBytes(ctx, user.ID)
	if err != nil || used != 3 {
		t.Fatalf("usage %v %d", err, used)
	}

	if err := db.DeleteObject(ctx, user.ID, "keep.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ObjectByKey(ctx, user.ID, "photos/holiday.jpg"); err != ErrObjectNotFound {
		t.Fatalf("got %v want not found", err)
	}
}

func TestUpdateProfile(t *testing.T) {
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
	got, err := db.UpdateProfile(ctx, user.ID, "Ali", "light", "#4a90d9")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Ali" || got.Theme != "light" || got.Accent != "#4a90d9" {
		t.Fatalf("profile %+v", got)
	}
	if _, err := db.UpdateProfile(ctx, user.ID, "Ali", "neon", "#fff"); err != ErrBadProfile {
		t.Fatalf("bad profile: %v", err)
	}
}

func TestEmailCodeConsumeAndCooldown(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	if err := db.PutEmailCode(ctx, "Ali@example.com", PurposeRegister, "hash-1", "pw", now); err != nil {
		t.Fatal(err)
	}
	if err := db.PutEmailCode(ctx, "ali@example.com", PurposeRegister, "hash-2", "pw", now.Add(10*time.Second)); err != ErrCodeCooldown {
		t.Fatalf("cooldown: %v", err)
	}

	if _, err := db.ConsumeEmailCode(ctx, "ali@example.com", PurposeRegister, "nope", now.Add(time.Minute)); err != ErrCodeWrong {
		t.Fatalf("wrong: %v", err)
	}
	extra, err := db.ConsumeEmailCode(ctx, "ali@example.com", PurposeRegister, "hash-1", now.Add(time.Minute))
	if err != nil || extra != "pw" {
		t.Fatalf("consume %q %v", extra, err)
	}
	if _, err := db.ConsumeEmailCode(ctx, "ali@example.com", PurposeRegister, "hash-1", now.Add(2*time.Minute)); err != ErrCodeNotFound {
		t.Fatalf("second consume: %v", err)
	}
}

func TestVaultAndEncryptedObject(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	user, err := db.CreateUser(ctx, "vault@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := db.SetVaultIfEmpty(ctx, user.ID, "c2FsdA", "d3JhcA")
	if err != nil || saved.VaultSalt != "c2FsdA" || saved.VaultWrap != "d3JhcA" {
		t.Fatalf("set vault: %+v %v", saved, err)
	}
	again, err := db.SetVaultIfEmpty(ctx, user.ID, "other", "nope")
	if err != nil || again.VaultWrap != "d3JhcA" {
		t.Fatalf("vault should stay write-once: %+v %v", again, err)
	}
	if err := db.ClearVault(ctx, user.Email); err != nil {
		t.Fatal(err)
	}
	cleared, err := db.UserByID(ctx, user.ID)
	if err != nil || cleared.VaultWrap != "" {
		t.Fatalf("clear vault: %+v %v", cleared, err)
	}

	obj, err := db.UpsertObjectEnc(ctx, user.ID, "secret.txt", "sha", 12, 1, "wrap-bytes")
	if err != nil || obj.EncVer != 1 || obj.EncWrap != "wrap-bytes" {
		t.Fatalf("enc object: %+v %v", obj, err)
	}
	got, err := db.ObjectByKey(ctx, user.ID, "secret.txt")
	if err != nil || got.EncVer != 1 {
		t.Fatalf("reload enc: %+v %v", got, err)
	}
}

func TestFirstUserIsAdminLaterUsersWait(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	owner, err := db.CreateUser(ctx, "ali@example.com", "hash")
	if err != nil || !owner.IsAdmin || !owner.Approved {
		t.Fatalf("first user should be admin: %+v %v", owner, err)
	}
	guest, err := db.CreateUser(ctx, "sam@example.com", "hash")
	if err != nil || guest.IsAdmin || guest.Approved {
		t.Fatalf("later user should wait: %+v %v", guest, err)
	}
	ok, err := db.SetApproved(ctx, guest.ID, true)
	if err != nil || !ok.Approved {
		t.Fatalf("approve: %+v %v", ok, err)
	}
}

func TestEnsureAdminPromotesExistingUser(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	first, err := db.CreateUser(ctx, "first@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.CreateUser(ctx, "owner@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin(ctx, "owner@example.com", ""); err != nil {
		t.Fatal(err)
	}
	got, err := db.UserByEmail(ctx, "owner@example.com")
	if err != nil || !got.IsAdmin || !got.Approved {
		t.Fatalf("owner %+v %v", got, err)
	}
	old, err := db.UserByID(ctx, first.ID)
	if err != nil || old.IsAdmin {
		t.Fatalf("old admin should be demoted: %+v %v", old, err)
	}
	if owner.ID == "" {
		t.Fatal("missing owner")
	}
}

func TestEnsureAdminCreatesMissingOwner(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	if err := db.EnsureAdmin(ctx, "owner@example.com", "hash"); err != nil {
		t.Fatal(err)
	}
	got, err := db.UserByEmail(ctx, "owner@example.com")
	if err != nil || !got.IsAdmin || !got.Approved || got.PasswordHash != "hash" {
		t.Fatalf("created owner %+v %v", got, err)
	}
	if err := db.EnsureAdmin(ctx, "owner@example.com", "other-hash"); err != nil {
		t.Fatal(err)
	}
	again, err := db.UserByEmail(ctx, "owner@example.com")
	if err != nil || again.PasswordHash != "hash" {
		t.Fatalf("existing owner password must stay: %+v %v", again, err)
	}
}
