package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrUploadNotFound = errors.New("upload session not found")
	ErrUploadDenied   = errors.New("not your upload")
	ErrUploadState    = errors.New("upload is not in the right state")
)

type UploadSession struct {
	ID        string
	OwnerID   string
	FolderID  string
	Key       string
	Size      int64
	Received  int64
	Parts     int
	EncVer    int
	EncWrap   string
	CreatedAt time.Time
}

func (db *DB) CreateUploadSession(ctx context.Context, ownerID, folderID, key string, size int64, encVer int, encWrap string) (*UploadSession, error) {
	key = strings.TrimSpace(strings.TrimPrefix(key, "/"))
	if key == "" || len(key) > 512 || strings.Contains(key, "..") {
		return nil, ErrObjectNotFound
	}
	if size < 1 {
		return nil, ErrUploadState
	}
	if encVer != 0 && encVer != 1 && encVer != 2 {
		encVer = 0
		encWrap = ""
	}
	now := time.Now().UTC()
	s := &UploadSession{
		ID:        newID(),
		OwnerID:   ownerID,
		FolderID:  strings.TrimSpace(folderID),
		Key:       key,
		Size:      size,
		EncVer:    encVer,
		EncWrap:   strings.TrimSpace(encWrap),
		CreatedAt: now,
	}
	_, err := db.SQL.ExecContext(ctx, `
INSERT INTO upload_sessions (id, owner_id, folder_id, key, size, received, parts, enc_ver, enc_wrap, created_at)
VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?, ?)
`, s.ID, s.OwnerID, s.FolderID, s.Key, s.Size, s.EncVer, s.EncWrap, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) UploadSession(ctx context.Context, id, ownerID string) (*UploadSession, error) {
	s := &UploadSession{}
	var created string
	err := db.SQL.QueryRowContext(ctx, `
SELECT id, owner_id, folder_id, key, size, received, parts, enc_ver, enc_wrap, created_at
FROM upload_sessions WHERE id = ?
`, id).Scan(&s.ID, &s.OwnerID, &s.FolderID, &s.Key, &s.Size, &s.Received, &s.Parts, &s.EncVer, &s.EncWrap, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, err
	}
	if s.OwnerID != ownerID {
		return nil, ErrUploadDenied
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return s, nil
}

func (db *DB) AddUploadBytes(ctx context.Context, id, ownerID string, n int64) (*UploadSession, error) {
	s, err := db.UploadSession(ctx, id, ownerID)
	if err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, ErrUploadState
	}
	if s.Received+n > s.Size {
		return nil, ErrUploadState
	}
	_, err = db.SQL.ExecContext(ctx, `
UPDATE upload_sessions SET received = received + ?, parts = parts + 1 WHERE id = ?
`, n, id)
	if err != nil {
		return nil, err
	}
	s.Received += n
	s.Parts++
	return s, nil
}

func (db *DB) DeleteUploadSession(ctx context.Context, id, ownerID string) error {
	s, err := db.UploadSession(ctx, id, ownerID)
	if err != nil {
		return err
	}
	_, err = db.SQL.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id = ?`, s.ID)
	return err
}
