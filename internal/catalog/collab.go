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
	CollabRoleViewer = "viewer"
)

var (
	ErrCollabNotFound   = errors.New("shared folder not found")
	ErrCollabDenied     = errors.New("not a member of this shared folder")
	ErrCollabName       = errors.New("bad shared folder name")
	ErrAlreadyMember    = errors.New("that person is already in the folder")
	ErrInviteNotFound   = errors.New("invite not found")
	ErrInviteWrongEmail = errors.New("this invite is for a different email")
	ErrCannotInviteSelf = errors.New("you are already in this folder")
	ErrBadCollabRole    = errors.New("role must be editor or viewer")
)

type CollabFolder struct {
	ID          string
	OwnerID     string
	Name        string
	Role        string
	DefaultRole string
	OwnerEmail  string
	CreatedAt   time.Time
}

type CollabInvite struct {
	ID          string
	FolderID    string
	FolderName  string
	Email       string
	InvitedBy   string
	InviterName string
	Role        string
	Status      string
	CreatedAt   time.Time
}

type CollabMember struct {
	UserID      string
	Email       string
	DisplayName string
	Role        string
	IsOwner     bool
}

func NormalizeMemberRole(role string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", CollabRoleEditor:
		return CollabRoleEditor, nil
	case CollabRoleViewer:
		return CollabRoleViewer, nil
	default:
		return "", ErrBadCollabRole
	}
}

func CanCollabWrite(role string) bool {
	return role == CollabRoleOwner || role == CollabRoleEditor
}

func CanCollabManage(role string) bool {
	return role == CollabRoleOwner
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

func (db *DB) CreateCollabFolder(ctx context.Context, ownerID, name, defaultRole string) (*CollabFolder, error) {
	name, err := NormalizeCollabName(name)
	if err != nil {
		return nil, err
	}
	defaultRole, err = NormalizeMemberRole(defaultRole)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	f := &CollabFolder{
		ID:          newID(),
		OwnerID:     ownerID,
		Name:        name,
		Role:        CollabRoleOwner,
		DefaultRole: defaultRole,
		CreatedAt:   now,
	}
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO collab_folders (id, owner_id, name, created_at, default_role)
VALUES (?, ?, ?, ?, ?)
`, f.ID, f.OwnerID, f.Name, now.Format(time.RFC3339), f.DefaultRole)
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
	var role string
	err = db.SQL.QueryRowContext(ctx, `
SELECT role FROM collab_members WHERE folder_id = ? AND user_id = ?
`, folderID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCollabDenied
	}
	if err != nil {
		return nil, err
	}
	if role == "" {
		role = CollabRoleEditor
	}
	f.Role = role
	return f, nil
}

func (db *DB) collabByID(ctx context.Context, folderID string) (*CollabFolder, error) {
	f := &CollabFolder{ID: folderID}
	var created string
	err := db.SQL.QueryRowContext(ctx, `
SELECT f.owner_id, f.name, f.created_at, f.default_role, u.email
FROM collab_folders f
JOIN users u ON u.id = f.owner_id
WHERE f.id = ?
`, folderID).Scan(&f.OwnerID, &f.Name, &created, &f.DefaultRole, &f.OwnerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCollabNotFound
	}
	if err != nil {
		return nil, err
	}
	if f.DefaultRole == "" {
		f.DefaultRole = CollabRoleEditor
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return f, nil
}

func (db *DB) ListCollabFolders(ctx context.Context, userID string) ([]CollabFolder, error) {
	rows, err := db.SQL.QueryContext(ctx, `
SELECT f.id, f.owner_id, f.name, f.created_at, f.default_role, u.email,
	CASE WHEN f.owner_id = ? THEN 'owner' ELSE COALESCE(NULLIF(m.role, ''), 'editor') END
FROM collab_folders f
JOIN users u ON u.id = f.owner_id
LEFT JOIN collab_members m ON m.folder_id = f.id AND m.user_id = ?
WHERE f.owner_id = ?
   OR f.id IN (SELECT folder_id FROM collab_members WHERE user_id = ?)
ORDER BY f.name
`, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CollabFolder{}
	for rows.Next() {
		var created string
		var f CollabFolder
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &created, &f.DefaultRole, &f.OwnerEmail, &f.Role); err != nil {
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

func (db *DB) InviteToFolder(ctx context.Context, userID, folderID, email, role string) (*CollabInvite, error) {
	f, err := db.CollabAccess(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	if !CanCollabWrite(f.Role) {
		return nil, ErrCollabDenied
	}
	if strings.TrimSpace(role) == "" {
		role = f.DefaultRole
	}
	role, err = NormalizeMemberRole(role)
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
		Role:       role,
		Status:     InvitePending,
	}
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO collab_invites (id, folder_id, email, invited_by, status, created_at, role)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(folder_id, email) DO UPDATE SET
	invited_by = excluded.invited_by,
	status = excluded.status,
	created_at = excluded.created_at,
	role = excluded.role
`, inv.ID, folderID, email, userID, InvitePending, now, role)
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
SELECT i.id, i.folder_id, f.name, i.email, i.invited_by, i.status, i.created_at, i.role,
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
		if err := rows.Scan(&inv.ID, &inv.FolderID, &inv.FolderName, &inv.Email, &inv.InvitedBy, &inv.Status, &created, &inv.Role, &inv.InviterName); err != nil {
			return nil, err
		}
		if inv.Role == "" {
			inv.Role = CollabRoleEditor
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
	role, err := NormalizeMemberRole(inv.Role)
	if err != nil {
		role = CollabRoleEditor
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL.ExecContext(ctx, `
INSERT OR IGNORE INTO collab_members (folder_id, user_id, created_at, role)
VALUES (?, ?, ?, ?)
`, inv.FolderID, userID, now, role); err != nil {
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
SELECT folder_id, email, invited_by, status, created_at, role FROM collab_invites WHERE id = ?
`, id).Scan(&inv.FolderID, &inv.Email, &inv.InvitedBy, &inv.Status, &created, &inv.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	if inv.Role == "" {
		inv.Role = CollabRoleEditor
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
		Role:        CollabRoleOwner,
		IsOwner:     true,
	}}

	rows, err := db.SQL.QueryContext(ctx, `
SELECT u.id, u.email, u.display_name, m.role
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
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role); err != nil {
			return nil, err
		}
		if m.UserID == owner.ID {
			continue
		}
		if m.Role == "" {
			m.Role = CollabRoleEditor
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

func (db *DB) SetFolderDefaultRole(ctx context.Context, actorID, folderID, role string) (*CollabFolder, error) {
	f, err := db.CollabAccess(ctx, actorID, folderID)
	if err != nil {
		return nil, err
	}
	if !CanCollabManage(f.Role) {
		return nil, ErrCollabDenied
	}
	role, err = NormalizeMemberRole(role)
	if err != nil {
		return nil, err
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE collab_folders SET default_role = ? WHERE id = ?`, role, folderID); err != nil {
		return nil, err
	}
	f.DefaultRole = role
	return f, nil
}

func (db *DB) SetMemberRole(ctx context.Context, actorID, folderID, memberID, role string) error {
	f, err := db.CollabAccess(ctx, actorID, folderID)
	if err != nil {
		return err
	}
	if !CanCollabManage(f.Role) || memberID == f.OwnerID {
		return ErrCollabDenied
	}
	role, err = NormalizeMemberRole(role)
	if err != nil {
		return err
	}
	res, err := db.SQL.ExecContext(ctx, `
UPDATE collab_members SET role = ? WHERE folder_id = ? AND user_id = ?
`, role, folderID, memberID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCollabDenied
	}
	return nil
}

func (db *DB) ListFolderInvites(ctx context.Context, folderID string) ([]CollabInvite, error) {
	rows, err := db.SQL.QueryContext(ctx, `
SELECT i.id, i.folder_id, f.name, i.email, i.invited_by, i.status, i.created_at, i.role,
	COALESCE(NULLIF(u.display_name, ''), u.email)
FROM collab_invites i
JOIN collab_folders f ON f.id = i.folder_id
JOIN users u ON u.id = i.invited_by
WHERE i.folder_id = ? AND i.status = ?
ORDER BY i.created_at
`, folderID, InvitePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CollabInvite{}
	for rows.Next() {
		var created string
		var inv CollabInvite
		if err := rows.Scan(&inv.ID, &inv.FolderID, &inv.FolderName, &inv.Email, &inv.InvitedBy, &inv.Status, &created, &inv.Role, &inv.InviterName); err != nil {
			return nil, err
		}
		if inv.Role == "" {
			inv.Role = CollabRoleEditor
		}
		inv.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (db *DB) CancelInvite(ctx context.Context, actorID, folderID, inviteID string) error {
	f, err := db.CollabAccess(ctx, actorID, folderID)
	if err != nil {
		return err
	}
	if !CanCollabWrite(f.Role) {
		return ErrCollabDenied
	}
	res, err := db.SQL.ExecContext(ctx, `
DELETE FROM collab_invites WHERE id = ? AND folder_id = ? AND status = ?
`, inviteID, folderID, InvitePending)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInviteNotFound
	}
	return nil
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
