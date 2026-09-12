package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	PurposeRegister = "register"
	PurposeReset    = "reset"

	CodeTTL      = 10 * time.Minute
	CodeCooldown = 45 * time.Second
	MaxAttempts  = 5
)

var (
	ErrCodeNotFound     = errors.New("no code pending")
	ErrCodeExpired      = errors.New("code expired")
	ErrCodeWrong        = errors.New("wrong code")
	ErrTooManyAttempts  = errors.New("too many attempts")
	ErrCodeCooldown     = errors.New("wait before requesting another code")
	ErrUnknownPurpose   = errors.New("unknown code purpose")
)

type EmailCode struct {
	Email     string
	Purpose   string
	CodeHash  string
	Extra     string
	ExpiresAt time.Time
	Attempts  int
	CreatedAt time.Time
}

func NormalizePurpose(purpose string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case PurposeRegister, PurposeReset:
		return strings.ToLower(strings.TrimSpace(purpose)), nil
	default:
		return "", ErrUnknownPurpose
	}
}

func (db *DB) PutEmailCode(ctx context.Context, email, purpose, codeHash, extra string, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	purpose, err := NormalizePurpose(purpose)
	if err != nil {
		return err
	}

	var createdAt string
	err = db.SQL.QueryRowContext(ctx, `
SELECT created_at FROM email_codes WHERE email = ? AND purpose = ?
`, email, purpose).Scan(&createdAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if t, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil && now.Sub(t) < CodeCooldown {
			return ErrCodeCooldown
		}
	}

	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO email_codes (email, purpose, code_hash, extra, expires_at, attempts, created_at)
VALUES (?, ?, ?, ?, ?, 0, ?)
ON CONFLICT(email, purpose) DO UPDATE SET
	code_hash = excluded.code_hash,
	extra = excluded.extra,
	expires_at = excluded.expires_at,
	attempts = 0,
	created_at = excluded.created_at
`, email, purpose, codeHash, extra, now.Add(CodeTTL).UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	return err
}

func (db *DB) ConsumeEmailCode(ctx context.Context, email, purpose, codeHash string, now time.Time) (extra string, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	purpose, err = NormalizePurpose(purpose)
	if err != nil {
		return "", err
	}

	row := db.SQL.QueryRowContext(ctx, `
SELECT code_hash, extra, expires_at, attempts FROM email_codes WHERE email = ? AND purpose = ?
`, email, purpose)

	var storedHash, expiresAt string
	var attempts int
	if scanErr := row.Scan(&storedHash, &extra, &expiresAt, &attempts); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return "", ErrCodeNotFound
		}
		return "", scanErr
	}

	exp, parseErr := time.Parse(time.RFC3339, expiresAt)
	if parseErr != nil || !now.Before(exp) {
		_ = db.DeleteEmailCode(ctx, email, purpose)
		return "", ErrCodeExpired
	}
	if attempts >= MaxAttempts {
		_ = db.DeleteEmailCode(ctx, email, purpose)
		return "", ErrTooManyAttempts
	}
	if storedHash != codeHash {
		attempts++
		if attempts >= MaxAttempts {
			_ = db.DeleteEmailCode(ctx, email, purpose)
			return "", ErrTooManyAttempts
		}
		_, _ = db.SQL.ExecContext(ctx, `
UPDATE email_codes SET attempts = ? WHERE email = ? AND purpose = ?
`, attempts, email, purpose)
		return "", ErrCodeWrong
	}

	if err := db.DeleteEmailCode(ctx, email, purpose); err != nil {
		return "", err
	}
	return extra, nil
}

func (db *DB) EmailCodeExtra(ctx context.Context, email, purpose string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	purpose, err := NormalizePurpose(purpose)
	if err != nil {
		return "", err
	}
	var extra string
	err = db.SQL.QueryRowContext(ctx, `
SELECT extra FROM email_codes WHERE email = ? AND purpose = ?
`, email, purpose).Scan(&extra)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCodeNotFound
	}
	return extra, err
}

func (db *DB) DeleteEmailCode(ctx context.Context, email, purpose string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM email_codes WHERE email = ? AND purpose = ?`, email, purpose)
	return err
}
