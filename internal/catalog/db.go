package catalog

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB is the card catalog: who you are, and what you named each file.
// The actual file bytes still live on the shelf (the blob store).
type DB struct {
	SQL *sql.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{SQL: sqlDB}, nil
}

func (db *DB) Close() error {
	return db.SQL.Close()
}

func migrate(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS objects (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL REFERENCES users(id),
	key TEXT NOT NULL,
	blob_sha TEXT NOT NULL,
	size INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE (owner_id, key)
);

CREATE TABLE IF NOT EXISTS shares (
	token TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL REFERENCES users(id),
	object_key TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	revoked INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
`)
	return err
}
