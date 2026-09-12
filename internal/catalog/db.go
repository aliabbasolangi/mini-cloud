package catalog

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if err := migrateProfile(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate profile: %w", err)
	}
	if err := migrateEmailCodes(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate email codes: %w", err)
	}
	if err := migrateCollab(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate collab: %w", err)
	}
	if err := migrateNotifications(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate notifications: %w", err)
	}
	if err := migrateVault(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate vault: %w", err)
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

func migrateProfile(sqlDB *sql.DB) error {
	for _, col := range []string{
		"display_name TEXT NOT NULL DEFAULT ''",
		"theme TEXT NOT NULL DEFAULT 'dark'",
		"accent TEXT NOT NULL DEFAULT '#d4a574'",
	} {
		if _, err := sqlDB.Exec("ALTER TABLE users ADD COLUMN " + col); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return nil
}

func migrateCollab(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS collab_folders (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS collab_members (
	folder_id TEXT NOT NULL REFERENCES collab_folders(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL,
	PRIMARY KEY (folder_id, user_id)
);

CREATE TABLE IF NOT EXISTS collab_invites (
	id TEXT PRIMARY KEY,
	folder_id TEXT NOT NULL REFERENCES collab_folders(id) ON DELETE CASCADE,
	email TEXT NOT NULL,
	invited_by TEXT NOT NULL REFERENCES users(id),
	status TEXT NOT NULL DEFAULT 'pending',
	created_at TEXT NOT NULL,
	UNIQUE (folder_id, email)
);

CREATE TABLE IF NOT EXISTS collab_markers (
	folder_id TEXT NOT NULL REFERENCES collab_folders(id) ON DELETE CASCADE,
	prefix TEXT NOT NULL,
	PRIMARY KEY (folder_id, prefix)
);
`)
	return err
}

func migrateNotifications(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS notifications (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL,
	invite_id TEXT NOT NULL DEFAULT '',
	folder_id TEXT NOT NULL DEFAULT '',
	unread INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL
);
`)
	return err
}

func migrateVault(sqlDB *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN vault_salt TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN vault_wrap TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN enc_ver INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE objects ADD COLUMN enc_wrap TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return nil
}

func migrateEmailCodes(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS email_codes (
	email TEXT NOT NULL,
	purpose TEXT NOT NULL,
	code_hash TEXT NOT NULL,
	extra TEXT NOT NULL DEFAULT '',
	expires_at TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	PRIMARY KEY (email, purpose)
);
`)
	return err
}
