package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrShareNotFound = errors.New("share not found")
	ErrShareExpired  = errors.New("share expired")
)

const shareTTL = 24 * time.Hour

type Share struct {
	Token     string
	OwnerID   string
	ObjectKey string
	ExpiresAt time.Time
}

func newShareToken() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (db *DB) CreateShare(ctx context.Context, ownerID, key string) (*Share, error) {
	if _, err := db.ObjectByKey(ctx, ownerID, key); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s := &Share{
		Token:     newShareToken(),
		OwnerID:   ownerID,
		ObjectKey: key,
		ExpiresAt: now.Add(shareTTL),
	}
	_, err := db.SQL.ExecContext(ctx, `
INSERT INTO shares (token, owner_id, object_key, expires_at, revoked, created_at)
VALUES (?, ?, ?, ?, 0, ?)
`, s.Token, s.OwnerID, s.ObjectKey, s.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) ValidShare(ctx context.Context, token string) (*Share, error) {
	var expires string
	var revoked int
	s := &Share{Token: token}
	err := db.SQL.QueryRowContext(ctx, `
SELECT owner_id, object_key, expires_at, revoked FROM shares WHERE token = ?
`, token).Scan(&s.OwnerID, &s.ObjectKey, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	if err != nil {
		return nil, err
	}
	if revoked != 0 {
		return nil, ErrShareNotFound
	}
	s.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if time.Now().UTC().After(s.ExpiresAt) {
		return nil, ErrShareExpired
	}
	return s, nil
}

func (db *DB) RevokeShare(ctx context.Context, ownerID, token string) error {
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE shares SET revoked = 1 WHERE token = ? AND owner_id = ? AND revoked = 0`,
		token, ownerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrShareNotFound
	}
	return nil
}
