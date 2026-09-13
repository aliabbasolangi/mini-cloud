package catalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCollabInviteAndAccess(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	owner, err := db.CreateUser(ctx, "ali@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := db.CreateUser(ctx, "sam@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	folder, err := db.CreateCollabFolder(ctx, owner.ID, "Thesis", CollabRoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if folder.DefaultRole != CollabRoleViewer {
		t.Fatalf("default role %q", folder.DefaultRole)
	}
	if _, err := db.CollabAccess(ctx, guest.ID, folder.ID); err != ErrCollabDenied {
		t.Fatalf("guest should not see it yet: %v", err)
	}

	inv, err := db.InviteToFolder(ctx, owner.ID, folder.ID, "sam@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingInvites(ctx, "sam@example.com")
	if err != nil || len(pending) != 1 || pending[0].FolderName != "Thesis" {
		t.Fatalf("pending %+v %v", pending, err)
	}

	guestNotes, err := db.ListNotifications(ctx, guest.ID)
	if err != nil || len(guestNotes) != 1 || guestNotes[0].Kind != NotifInvite {
		t.Fatalf("guest invite notice %+v %v", guestNotes, err)
	}

	opened, err := db.AcceptInvite(ctx, inv.ID, guest.ID, guest.Email)
	if err != nil || opened.Role != CollabRoleViewer {
		t.Fatalf("accept %v %+v", err, opened)
	}
	if err := db.SetMemberRole(ctx, owner.ID, folder.ID, guest.ID, CollabRoleEditor); err != nil {
		t.Fatal(err)
	}
	if got, err := db.CollabAccess(ctx, guest.ID, folder.ID); err != nil || got.Role != CollabRoleEditor {
		t.Fatalf("promoted %v %+v", err, got)
	}
	ownerNotes, err := db.ListNotifications(ctx, owner.ID)
	if err != nil || len(ownerNotes) != 1 || ownerNotes[0].Kind != NotifInviteAccepted {
		t.Fatalf("owner accept notice %+v %v", ownerNotes, err)
	}
	if _, err := db.CollabAccess(ctx, guest.ID, folder.ID); err != nil {
		t.Fatal(err)
	}

	key := CollabObjectKey(folder.ID, "notes.txt")
	if _, err := db.UpsertObject(ctx, owner.ID, key, "abc", 4); err != nil {
		t.Fatal(err)
	}
	personal, err := db.ListPersonalObjects(ctx, owner.ID)
	if err != nil || len(personal) != 0 {
		t.Fatalf("shared files must not appear in the personal list: %d %v", len(personal), err)
	}
	shared, err := db.ListObjectsByPrefix(ctx, owner.ID, CollabKeyPrefix+folder.ID+"/")
	if err != nil || len(shared) != 1 {
		t.Fatalf("shared list %d %v", len(shared), err)
	}
}
