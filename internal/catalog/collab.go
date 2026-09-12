package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	CollabKeyPrefix  = "~share/"
	InvitePending    = "pending"
	InviteAccepted   = "accepted"
	InviteDeclined   = "declined"
	CollabRoleOwner  = "owner"
	CollabRoleEditor = "editor"
)

var (
	ErrCollabNotFound   = errors.New("shared folder not found")
	ErrCollabDenied     = errors.New("not a member of this shared folder")
	ErrCollabName       = errors.New("bad shared folder name")
	ErrAlreadyMember    = errors.New("that person is already in the folder")
	ErrInviteNotFound   = errors.New("invite not found")
	ErrInviteWrongEmail = errors.New("this invite is for a different email")
	ErrCannotInviteSelf = errors.New("you are already in this folder")
)

type CollabFolder struct {
	ID         string
	OwnerID    string
	Name       string
	Role       string
	OwnerEmail string
	CreatedAt  time.Time
}

type CollabInvite struct {
	ID          string
	FolderID    string
	FolderName  string
	Email       string
	InvitedBy   string
	InviterName string
	Status      string
	CreatedAt   time.Time
}

type CollabMember struct {
	UserID      string
	Email       string
	DisplayName string
	IsOwner     bool
}

func CollabObjectKey(folderID, rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	return CollabKeyPrefix + folderID + "/" + rel
}

func IsCollabKey(key string) bool {
	return strings.HasPrefix(key, CollabKeyPrefix)
}

func NormalizeCollabName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return "", ErrCollabName
	}
	if strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") {
		return "", ErrCollabName
	}
	return name, nil
}

func (db *DB) CreateCollabFolder(ctx context.Context, ownerID, name string) (*CollabFolder, error) {
	name, err := NormalizeCollabName(name)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	f := &CollabFolder{
		ID:        newID(),
		OwnerID:   ownerID,
		Name:      name,
		Role:      CollabRoleOwner,
		CreatedAt: now,
	}
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO collab_folders (id, owner_id, name, created_at)
VALUES (?, ?, ?, ?)
`, f.ID, f.OwnerID, f.Name, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (db *DB) CollabAccess(ctx context.Context, userID, folderID string) (*CollabFolder, error) {
	f, err := db.collabByID(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if f.OwnerID == userID {
		f.Role = CollabRoleOwner
		return f, nil
	}
	var n int
	err = db.SQL.QueryRowContext(ctx, `
SELECT COUNT(*) FROM collab_members WHERE folder_id = ? AND user_id = ?
`, folderID, userID).Scan(&n)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrCollabDenied
	}
	f.Role = CollabRoleEditor
	return f, nil
}

func (db *DB) collabByID(ctx context.Context, folderID string) (*CollabFolder, error) {
	f := &CollabFolder{ID: folderID}
	var created string
	err := db.SQL.QueryRowContext(ctx, `
SELECT f.owner_id, f.name, f.created_at, u.email
FROM collab_folders f
JOIN users u ON u.id = f.owner_id
WHERE f.id = ?
`, folderID).Scan(&f.OwnerID, &f.Name, &created, &f.OwnerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCollabNotFound
	}
	if err != nil {
		return nil, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return f, nil
}

func (db *DB) ListCollabFolders(ctx context.Context, userID string) ([]CollabFolder, error) {
	rows, err := db.SQL.QueryContext(ctx, `
SELECT f.id, f.owner_id, f.name, f.created_at, u.email,
	CASE WHEN f.owner_id = ? THEN 'owner' ELSE 'editor' END
FROM collab_folders f
JOIN users u ON u.id = f.owner_id
WHERE f.owner_id = ?
   OR f.id IN (SELECT folder_id FROM collab_members WHERE user_id = ?)
ORDER BY f.name
`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CollabFolder{}
	for rows.Next() {
		var created string
		var f CollabFolder
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &created, &f.OwnerEmail, &f.Role); err != nil {
			return nil, err
		}
		f.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (db *DB) DeleteCollabFolder(ctx context.Context, userID, folderID string) error {
	f, err := db.CollabAccess(ctx, userID, folderID)
	if err != nil {
		return err
	}
	if f.Role != CollabRoleOwner {
		return ErrCollabDenied
	}
	if _, err := db.DeletePrefix(ctx, f.OwnerID, CollabKeyPrefix+folderID+"/"); err != nil && !errors.Is(err, ErrObjectNotFound) {
		return err
	}
	_, err = db.SQL.ExecContext(ctx, `DELETE FROM collab_folders WHERE id = ?`, folderID)
	return err
}

func (db *DB) InviteToFolder(ctx context.Context, userID, folderID, email string) (*CollabInvite, error) {
	f, err := db.CollabAccess(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, ErrUserNotFound
	}

	me, err := db.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(me.Email, email) || strings.EqualFold(f.OwnerEmail, email) {
		return nil, ErrCannotInviteSelf
	}

	if target, err := db.UserByEmail(ctx, email); err == nil {
		var n int
		_ = db.SQL.QueryRowContext(ctx, `
SELECT COUNT(*) FROM collab_members WHERE folder_id = ? AND user_id = ?
`, folderID, target.ID).Scan(&n)
		if n > 0 {
			return nil, ErrAlreadyMember
		}
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	inv := &CollabInvite{
		ID:         newID(),
		FolderID:   folderID,
		FolderName: f.Name,
		Email:      email,
		InvitedBy:  userID,
		Status:     InvitePending,
	}
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO collab_invites (id, folder_id, email, invited_by, status, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(folder_id, email) DO UPDATE SET
	invited_by = excluded.invited_by,
	status = excluded.status,
	created_at = excluded.created_at
`, inv.ID, folderID, email, userID, InvitePending, now)
	if err != nil {
		return nil, err
	}
	_ = db.SQL.QueryRowContext(ctx, `
SELECT id FROM collab_invites WHERE folder_id = ? AND email = ?
`, folderID, email).Scan(&inv.ID)

	if target, lookErr := db.UserByEmail(ctx, email); lookErr == nil {
		body := displayWho(me) + " invited you to collaborate on “" + f.Name + "”."
		_ = db.upsertInviteNotification(ctx, target.ID, inv.ID, folderID, f.Name, body)
	}
	return inv, nil
}

func (db *DB) PendingInvites(ctx context.Context, email string) ([]CollabInvite, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	rows, err := db.SQL.QueryContext(ctx, `
SELECT i.id, i.folder_id, f.name, i.email, i.invited_by, i.status, i.created_at,
	COALESCE(NULLIF(u.display_name, ''), u.email)
FROM collab_invites i
JOIN collab_folders f ON f.id = i.folder_id
JOIN users u ON u.id = i.invited_by
WHERE i.email = ? AND i.status = ?
ORDER BY i.created_at
`, email, InvitePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CollabInvite{}
	for rows.Next() {
		var created string
		var inv CollabInvite
		if err := rows.Scan(&inv.ID, &inv.FolderID, &inv.FolderName, &inv.Email, &inv.InvitedBy, &inv.Status, &created, &inv.InviterName); err != nil {
			return nil, err
		}
		inv.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (db *DB) AcceptInvite(ctx context.Context, inviteID, userID, email string) (*CollabFolder, error) {
	inv, err := db.inviteByID(ctx, inviteID)
	if err != nil {
		return nil, err
	}
	if inv.Status != InvitePending {
		return nil, ErrInviteNotFound
	}
	if !strings.EqualFold(inv.Email, email) {
		return nil, ErrInviteWrongEmail
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL.ExecContext(ctx, `
INSERT OR IGNORE INTO collab_members (folder_id, user_id, created_at)
VALUES (?, ?, ?)
`, inv.FolderID, userID, now); err != nil {
		return nil, err
	}
	if _, err := db.SQL.ExecContext(ctx, `
UPDATE collab_invites SET status = ? WHERE id = ?
`, InviteAccepted, inviteID); err != nil {
		return nil, err
	}
	_ = db.markInviteNotificationsRead(ctx, userID, inviteID)
	if actor, whoErr := db.UserByID(ctx, userID); whoErr == nil {
		if folder, folderErr := db.collabByID(ctx, inv.FolderID); folderErr == nil {
			_ = db.AddNotification(ctx, Notification{
				UserID:   inv.InvitedBy,
				Kind:     NotifInviteAccepted,
				Title:    folder.Name,
				Body:     displayWho(actor) + " accepted your invite to collaborate.",
				FolderID: inv.FolderID,
				Unread:   true,
			})
		}
	}
	return db.CollabAccess(ctx, userID, inv.FolderID)
}

func (db *DB) DeclineInvite(ctx context.Context, inviteID, email string) error {
	inv, err := db.inviteByID(ctx, inviteID)
	if err != nil {
		return err
	}
	if inv.Status != InvitePending || !strings.EqualFold(inv.Email, email) {
		return ErrInviteNotFound
	}
	_, err = db.SQL.ExecContext(ctx, `UPDATE collab_invites SET status = ? WHERE id = ?`, InviteDeclined, inviteID)
	if err != nil {
		return err
	}
	if guest, lookErr := db.UserByEmail(ctx, email); lookErr == nil {
		_ = db.markInviteNotificationsRead(ctx, guest.ID, inviteID)
		if folder, folderErr := db.collabByID(ctx, inv.FolderID); folderErr == nil {
			_ = db.AddNotification(ctx, Notification{
				UserID:   inv.InvitedBy,
				Kind:     NotifInviteDeclined,
				Title:    folder.Name,
				Body:     displayWho(guest) + " declined your invite to collaborate.",
				FolderID: inv.FolderID,
				Unread:   true,
			})
		}
	}
	return nil
}

func (db *DB) inviteByID(ctx context.Context, id string) (*CollabInvite, error) {
	inv := &CollabInvite{ID: id}
	var created string
	err := db.SQL.QueryRowContext(ctx, `
SELECT folder_id, email, invited_by, status, created_at FROM collab_invites WHERE id = ?
`, id).Scan(&inv.FolderID, &inv.Email, &inv.InvitedBy, &inv.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	inv.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return inv, nil
}

func (db *DB) ListCollabMembers(ctx context.Context, folderID string) ([]CollabMember, error) {
	f, err := db.collabByID(ctx, folderID)
	if err != nil {
		return nil, err
	}
	owner, err := db.UserByID(ctx, f.OwnerID)
	if err != nil {
		return nil, err
	}
	out := []CollabMember{{
		UserID:      owner.ID,
		Email:       owner.Email,
		DisplayName: owner.DisplayName,
		IsOwner:     true,
	}}

	rows, err := db.SQL.QueryContext(ctx, `
SELECT u.id, u.email, u.display_name
FROM collab_members m
JOIN users u ON u.id = m.user_id
WHERE m.folder_id = ?
ORDER BY u.email
`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m CollabMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName); err != nil {
			return nil, err
		}
		if m.UserID == owner.ID {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) LeaveCollab(ctx context.Context, userID, folderID string) error {
	f, err := db.CollabAccess(ctx, userID, folderID)
	if err != nil {
		return err
	}
	if f.Role == CollabRoleOwner {
		return ErrCollabDenied
	}
	_, err = db.SQL.ExecContext(ctx, `DELETE FROM collab_members WHERE folder_id = ? AND user_id = ?`, folderID, userID)
	return err
}

func (db *DB) RemoveCollabMember(ctx context.Context, actorID, folderID, memberID string) error {
	f, err := db.CollabAccess(ctx, actorID, folderID)
	if err != nil {
		return err
	}
	if f.Role != CollabRoleOwner || memberID == f.OwnerID {
		return ErrCollabDenied
	}
	_, err = db.SQL.ExecContext(ctx, `DELETE FROM collab_members WHERE folder_id = ? AND user_id = ?`, folderID, memberID)
	return err
}

func (db *DB) AddCollabMarker(ctx context.Context, folderID, prefix string) error {
	if prefix == "" || !strings.HasSuffix(prefix, "/") || strings.Contains(prefix, "..") {
		return ErrCollabName
	}
	_, err := db.SQL.ExecContext(ctx, `
INSERT OR IGNORE INTO collab_markers (folder_id, prefix) VALUES (?, ?)
`, folderID, prefix)
	return err
}

func (db *DB) ListCollabMarkers(ctx context.Context, folderID string) ([]string, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT prefix FROM collab_markers WHERE folder_id = ?`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) DeleteCollabMarkers(ctx context.Context, folderID, prefix string) error {
	_, err := db.SQL.ExecContext(ctx, `
DELETE FROM collab_markers WHERE folder_id = ? AND (prefix = ? OR prefix LIKE ?)
`, folderID, prefix, prefix+"%")
	return err
}
