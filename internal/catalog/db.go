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
	if err := migrateApproval(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate approval: %w", err)
	}
	if err := migrateCollabRoles(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate collab roles: %w", err)
	}
	if err := migrateUploads(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate uploads: %w", err)
	}
	if err := migrateShareLinks(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate share links: %w", err)
	}
	if err := migrateLook(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate look: %w", err)
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
		"accent TEXT NOT NULL DEFAULT '#5b9dff'",
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

func migrateApproval(sqlDB *sql.DB) error {
	for _, col := range []string{
		"approved INTEGER NOT NULL DEFAULT 0",
		"is_admin INTEGER NOT NULL DEFAULT 0",
	} {
		if _, err := sqlDB.Exec("ALTER TABLE users ADD COLUMN " + col); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	var admins int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&admins); err != nil {
		return err
	}
	if admins > 0 {
		return nil
	}
	if _, err := sqlDB.Exec(`UPDATE users SET approved = 1`); err != nil {
		return err
	}
	_, err := sqlDB.Exec(`
UPDATE users SET is_admin = 1
WHERE id = (SELECT id FROM users ORDER BY created_at ASC, id ASC LIMIT 1)
`)
	return err
}

func migrateUploads(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS upload_sessions (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	folder_id TEXT NOT NULL DEFAULT '',
	key TEXT NOT NULL,
	size INTEGER NOT NULL,
	received INTEGER NOT NULL DEFAULT 0,
	parts INTEGER NOT NULL DEFAULT 0,
	enc_ver INTEGER NOT NULL DEFAULT 0,
	enc_wrap TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
`)
	return err
}

func migrateCollabRoles(sqlDB *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE collab_folders ADD COLUMN default_role TEXT NOT NULL DEFAULT 'editor'`,
		`ALTER TABLE collab_members ADD COLUMN role TEXT NOT NULL DEFAULT 'editor'`,
		`ALTER TABLE collab_invites ADD COLUMN role TEXT NOT NULL DEFAULT 'editor'`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return nil
}

func migrateShareLinks(sqlDB *sql.DB) error {
	for _, col := range []string{
		"kind TEXT NOT NULL DEFAULT 'file'",
		"folder_id TEXT NOT NULL DEFAULT ''",
		"role TEXT NOT NULL DEFAULT 'viewer'",
	} {
		if _, err := sqlDB.Exec("ALTER TABLE shares ADD COLUMN " + col); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return nil
}

func migrateLook(sqlDB *sql.DB) error {
	if _, err := sqlDB.Exec(`ALTER TABLE users ADD COLUMN backdrop TEXT NOT NULL DEFAULT 'aurora'`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	_, err := sqlDB.Exec(`UPDATE users SET accent = '#5b9dff' WHERE lower(accent) IN ('#d4a574', '#e3c27a')`)
	return err
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
