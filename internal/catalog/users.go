package catalog

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrEmailTaken     = errors.New("email already registered")
	ErrUserNotFound   = errors.New("user not found")
	ErrBadProfile     = errors.New("invalid profile")
	ErrNotApproved    = errors.New("account waiting for approval")
	ErrNotAdmin       = errors.New("admin only")
	ErrCannotDenySelf = errors.New("cannot deny the admin account")
)

var accentHex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

const (
	DefaultAccent   = "#5b9dff"
	DefaultBackdrop = "aurora"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Theme        string
	Accent       string
	Backdrop     string
	VaultSalt    string
	VaultWrap    string
	Approved     bool
	IsAdmin      bool
}

func (db *DB) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	admins, err := db.AdminCount(ctx)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:           newID(),
		Email:        email,
		PasswordHash: passwordHash,
		Theme:        "dark",
		Accent:       DefaultAccent,
		Backdrop:     DefaultBackdrop,
		Approved:     admins == 0,
		IsAdmin:      admins == 0,
	}
	approved, admin := 0, 0
	if u.Approved {
		approved = 1
	}
	if u.IsAdmin {
		admin = 1
	}
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO users (id, email, password_hash, created_at, display_name, theme, accent, backdrop, approved, is_admin)
VALUES (?, ?, ?, ?, '', 'dark', ?, ?, ?, ?)
`, u.ID, u.Email, u.PasswordHash, time.Now().UTC().Format(time.RFC3339), DefaultAccent, DefaultBackdrop, approved, admin)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return u, nil
}

const userSelect = `id, email, password_hash, display_name, theme, accent, backdrop, vault_salt, vault_wrap, approved, is_admin`

func scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	u := &User{}
	var approved, admin int
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Theme, &u.Accent, &u.Backdrop, &u.VaultSalt, &u.VaultWrap, &approved, &admin)
	u.Approved = approved == 1
	u.IsAdmin = admin == 1
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.Theme == "" {
		u.Theme = "dark"
	}
	if u.Accent == "" {
		u.Accent = DefaultAccent
	}
	if u.Backdrop == "" {
		u.Backdrop = DefaultBackdrop
	}
	return u, nil
}

func (db *DB) UserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	return scanUser(db.SQL.QueryRowContext(ctx, `
SELECT `+userSelect+` FROM users WHERE email = ?
`, email))
}

func (db *DB) UpdatePassword(ctx context.Context, email, passwordHash string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	res, err := db.SQL.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE email = ?`, passwordHash, email)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (db *DB) DeleteUser(ctx context.Context, userID string) error {
	user, err := db.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	folders, err := db.ListCollabFolders(ctx, userID)
	if err != nil {
		return err
	}
	for _, f := range folders {
		if f.OwnerID == userID {
			if err := db.DeleteCollabFolder(ctx, userID, f.ID); err != nil {
				return err
			}
			continue
		}
		_ = db.LeaveCollab(ctx, userID, f.ID)
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM shares WHERE owner_id = ?`, userID); err != nil {
		return err
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM objects WHERE owner_id = ?`, userID); err != nil {
		return err
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM collab_invites WHERE invited_by = ? OR email = ?`, userID, user.Email); err != nil {
		return err
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM notifications WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM email_codes WHERE email = ?`, user.Email); err != nil {
		return err
	}
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (db *DB) UserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(db.SQL.QueryRowContext(ctx, `
SELECT `+userSelect+` FROM users WHERE id = ?
`, id))
}

func (db *DB) SetVaultIfEmpty(ctx context.Context, userID, salt, wrap string) (*User, error) {
	salt = strings.TrimSpace(salt)
	wrap = strings.TrimSpace(wrap)
	if salt == "" || wrap == "" || len(salt) > 128 || len(wrap) > 2048 {
		return nil, ErrBadProfile
	}
	if _, err := db.UserByID(ctx, userID); err != nil {
		return nil, err
	}
	_, err := db.SQL.ExecContext(ctx, `
UPDATE users SET vault_salt = ?, vault_wrap = ?
WHERE id = ? AND vault_wrap = ''
`, salt, wrap, userID)
	if err != nil {
		return nil, err
	}
	return db.UserByID(ctx, userID)
}

func (db *DB) ClearVault(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := db.SQL.ExecContext(ctx, `
UPDATE users SET vault_salt = '', vault_wrap = '' WHERE email = ?
`, email)
	return err
}

func NormalizeBackdrop(backdrop string) (string, error) {
	backdrop = strings.ToLower(strings.TrimSpace(backdrop))
	if backdrop == "" {
		return DefaultBackdrop, nil
	}
	switch backdrop {
	case "aurora", "orbs", "mesh", "stars", "quiet":
		return backdrop, nil
	default:
		return "", ErrBadProfile
	}
}

func NormalizeProfile(displayName, theme, accent, backdrop string) (string, string, string, string, error) {
	displayName = strings.TrimSpace(displayName)
	if len(displayName) > 80 {
		return "", "", "", "", ErrBadProfile
	}
	theme = strings.ToLower(strings.TrimSpace(theme))
	if theme == "" {
		theme = "dark"
	}
	if theme != "dark" && theme != "light" {
		return "", "", "", "", ErrBadProfile
	}
	accent = strings.TrimSpace(accent)
	if accent == "" {
		accent = DefaultAccent
	}
	if !accentHex.MatchString(accent) {
		return "", "", "", "", ErrBadProfile
	}
	backdrop, err := NormalizeBackdrop(backdrop)
	if err != nil {
		return "", "", "", "", err
	}
	return displayName, theme, strings.ToLower(accent), backdrop, nil
}

func (db *DB) UpdateProfile(ctx context.Context, id, displayName, theme, accent, backdrop string) (*User, error) {
	displayName, theme, accent, backdrop, err := NormalizeProfile(displayName, theme, accent, backdrop)
	if err != nil {
		return nil, err
	}
	res, err := db.SQL.ExecContext(ctx, `
UPDATE users SET display_name = ?, theme = ?, accent = ?, backdrop = ? WHERE id = ?
`, displayName, theme, accent, backdrop, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrUserNotFound
	}
	return db.UserByID(ctx, id)
}

func (db *DB) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&n)
	return n, err
}

func (db *DB) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE approved = 0`).Scan(&n)
	return n, err
}

func (db *DB) ListAdmins(ctx context.Context) ([]*User, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT `+userSelect+` FROM users WHERE is_admin = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := db.SQL.QueryContext(ctx, `
SELECT `+userSelect+` FROM users ORDER BY approved ASC, created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) EnsureAdmin(ctx context.Context, email, passwordHash string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE users SET is_admin = 0`); err != nil {
		return err
	}
	user, err := db.UserByEmail(ctx, email)
	if err == nil {
		_, err = db.SQL.ExecContext(ctx, `UPDATE users SET is_admin = 1, approved = 1 WHERE id = ?`, user.ID)
		return err
	}
	if !errors.Is(err, ErrUserNotFound) {
		return err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil
	}
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO users (id, email, password_hash, created_at, display_name, theme, accent, backdrop, approved, is_admin)
VALUES (?, ?, ?, ?, '', 'dark', ?, ?, 1, 1)
`, newID(), email, passwordHash, time.Now().UTC().Format(time.RFC3339), DefaultAccent, DefaultBackdrop)
	return err
}

func (db *DB) SetApproved(ctx context.Context, id string, approved bool) (*User, error) {
	user, err := db.UserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user.IsAdmin {
		return user, nil
	}
	flag := 0
	if approved {
		flag = 1
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE users SET approved = ? WHERE id = ?`, flag, id); err != nil {
		return nil, err
	}
	return db.UserByID(ctx, id)
}
