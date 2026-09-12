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
	ErrEmailTaken    = errors.New("email already registered")
	ErrUserNotFound  = errors.New("user not found")
	ErrBadProfile    = errors.New("invalid profile")
)

var accentHex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Theme        string
	Accent       string
	VaultSalt    string
	VaultWrap    string
}

func (db *DB) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u := &User{
		ID:           newID(),
		Email:        email,
		PasswordHash: passwordHash,
		Theme:        "dark",
		Accent:       "#d4a574",
	}
	_, err := db.SQL.ExecContext(ctx, `
INSERT INTO users (id, email, password_hash, created_at, display_name, theme, accent)
VALUES (?, ?, ?, ?, '', 'dark', '#d4a574')
`, u.ID, u.Email, u.PasswordHash, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return u, nil
}

const userSelect = `id, email, password_hash, display_name, theme, accent, vault_salt, vault_wrap`

func scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Theme, &u.Accent, &u.VaultSalt, &u.VaultWrap)
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
		u.Accent = "#d4a574"
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

func NormalizeProfile(displayName, theme, accent string) (string, string, string, error) {
	displayName = strings.TrimSpace(displayName)
	if len(displayName) > 80 {
		return "", "", "", ErrBadProfile
	}
	theme = strings.ToLower(strings.TrimSpace(theme))
	if theme == "" {
		theme = "dark"
	}
	if theme != "dark" && theme != "light" {
		return "", "", "", ErrBadProfile
	}
	accent = strings.TrimSpace(accent)
	if accent == "" {
		accent = "#d4a574"
	}
	if !accentHex.MatchString(accent) {
		return "", "", "", ErrBadProfile
	}
	return displayName, theme, strings.ToLower(accent), nil
}

func (db *DB) UpdateProfile(ctx context.Context, id, displayName, theme, accent string) (*User, error) {
	displayName, theme, accent, err := NormalizeProfile(displayName, theme, accent)
	if err != nil {
		return nil, err
	}
	res, err := db.SQL.ExecContext(ctx, `
UPDATE users SET display_name = ?, theme = ?, accent = ? WHERE id = ?
`, displayName, theme, accent, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrUserNotFound
	}
	return db.UserByID(ctx, id)
}
