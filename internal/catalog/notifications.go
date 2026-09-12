package catalog

import (
	"context"
	"strings"
	"time"
)

const (
	NotifInvite         = "invite"
	NotifInviteAccepted = "invite_accepted"
	NotifInviteDeclined = "invite_declined"
)

type Notification struct {
	ID        string
	UserID    string
	Kind      string
	Title     string
	Body      string
	InviteID  string
	FolderID  string
	Unread    bool
	CreatedAt time.Time
}

func displayWho(u *User) string {
	if u == nil {
		return "Someone"
	}
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Email
}

func (db *DB) AddNotification(ctx context.Context, n Notification) error {
	if n.ID == "" {
		n.ID = newID()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	unread := 1
	if !n.Unread {
		unread = 0
	}
	_, err := db.SQL.ExecContext(ctx, `
INSERT INTO notifications (id, user_id, kind, title, body, invite_id, folder_id, unread, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, n.ID, n.UserID, n.Kind, n.Title, n.Body, n.InviteID, n.FolderID, unread, n.CreatedAt.Format(time.RFC3339))
	return err
}

func (db *DB) upsertInviteNotification(ctx context.Context, userID, inviteID, folderID, title, body string) error {
	var id string
	err := db.SQL.QueryRowContext(ctx, `
SELECT id FROM notifications WHERE user_id = ? AND invite_id = ? AND kind = ?
`, userID, inviteID, NotifInvite).Scan(&id)
	now := time.Now().UTC().Format(time.RFC3339)
	if err == nil && id != "" {
		_, err = db.SQL.ExecContext(ctx, `
UPDATE notifications SET title = ?, body = ?, folder_id = ?, unread = 1, created_at = ? WHERE id = ?
`, title, body, folderID, now, id)
		return err
	}
	return db.AddNotification(ctx, Notification{
		UserID:   userID,
		Kind:     NotifInvite,
		Title:    title,
		Body:     body,
		InviteID: inviteID,
		FolderID: folderID,
		Unread:   true,
	})
}

func (db *DB) ListNotifications(ctx context.Context, userID string) ([]Notification, error) {
	rows, err := db.SQL.QueryContext(ctx, `
SELECT id, user_id, kind, title, body, invite_id, folder_id, unread, created_at
FROM notifications WHERE user_id = ? ORDER BY created_at DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var created string
		var unread int
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.InviteID, &n.FolderID, &unread, &created); err != nil {
			return nil, err
		}
		n.Unread = unread != 0
		n.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (db *DB) UnreadNotificationCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `
SELECT COUNT(*) FROM notifications WHERE user_id = ? AND unread = 1
`, userID).Scan(&n)
	return n, err
}

func (db *DB) MarkNotificationRead(ctx context.Context, userID, id string) error {
	_, err := db.SQL.ExecContext(ctx, `
UPDATE notifications SET unread = 0 WHERE id = ? AND user_id = ?
`, id, userID)
	return err
}

func (db *DB) MarkAllNotificationsRead(ctx context.Context, userID string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE notifications SET unread = 0 WHERE user_id = ?`, userID)
	return err
}

func (db *DB) markInviteNotificationsRead(ctx context.Context, userID, inviteID string) error {
	_, err := db.SQL.ExecContext(ctx, `
UPDATE notifications SET unread = 0 WHERE user_id = ? AND invite_id = ? AND kind = ?
`, userID, inviteID, NotifInvite)
	return err
}

func (db *DB) EnsureInviteNotifications(ctx context.Context, user *User) error {
	if user == nil {
		return nil
	}
	pending, err := db.PendingInvites(ctx, user.Email)
	if err != nil {
		return err
	}
	for _, inv := range pending {
		body := inv.InviterName + " invited you to collaborate on “" + inv.FolderName + "”."
		if err := db.upsertInviteNotification(ctx, user.ID, inv.ID, inv.FolderID, inv.FolderName, body); err != nil {
			return err
		}
	}
	return nil
}
