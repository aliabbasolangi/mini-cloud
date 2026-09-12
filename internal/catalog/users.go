package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrEmailTaken   = errors.New("email already registered")
	ErrUserNotFound = errors.New("user not found")
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
}

func (db *DB) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u := &User{ID: newID(), Email: email, PasswordHash: passwordHash}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return u, nil
}

func (db *DB) UserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u := &User{}
	err := db.SQL.QueryRowContext(ctx,
		`SELECT id, email, password_hash FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}
