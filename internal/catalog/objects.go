package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrObjectNotFound = errors.New("object not found")
	ErrKeyTaken       = errors.New("a file with that name already exists")
	ErrQuotaExceeded  = errors.New("storage quota exceeded")
)

type Object struct {
	ID        string
	OwnerID   string
	Key       string
	BlobSHA   string
	Size      int64
	CreatedAt time.Time
	EncVer    int
	EncWrap   string
}

const objectSelect = `id, owner_id, key, blob_sha, size, created_at, enc_ver, enc_wrap`

func scanObject(scan func(dest ...any) error) (*Object, error) {
	var created string
	o := &Object{}
	err := scan(&o.ID, &o.OwnerID, &o.Key, &o.BlobSHA, &o.Size, &created, &o.EncVer, &o.EncWrap)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, err
	}
	o.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return o, nil
}

func (db *DB) UpsertObject(ctx context.Context, ownerID, key, blobSHA string, size int64) (*Object, error) {
	return db.UpsertObjectEnc(ctx, ownerID, key, blobSHA, size, 0, "")
}

func (db *DB) UpsertObjectEnc(ctx context.Context, ownerID, key, blobSHA string, size int64, encVer int, encWrap string) (*Object, error) {
	if encVer != 0 && encVer != 1 && encVer != 2 {
		encVer = 0
		encWrap = ""
	}
	if encVer == 1 || encVer == 2 {
		encWrap = strings.TrimSpace(encWrap)
		if encWrap == "" || len(encWrap) > 2048 {
			encVer = 0
			encWrap = ""
		}
	} else {
		encWrap = ""
	}
	now := time.Now().UTC()
	id := newID()
	_, err := db.SQL.ExecContext(ctx, `
INSERT INTO objects (id, owner_id, key, blob_sha, size, created_at, enc_ver, enc_wrap)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(owner_id, key) DO UPDATE SET
	blob_sha = excluded.blob_sha,
	size = excluded.size,
	enc_ver = excluded.enc_ver,
	enc_wrap = excluded.enc_wrap
`, id, ownerID, key, blobSHA, size, now.Format(time.RFC3339), encVer, encWrap)
	if err != nil {
		return nil, err
	}

	obj, err := db.ObjectByKey(ctx, ownerID, key)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (db *DB) ObjectByKey(ctx context.Context, ownerID, key string) (*Object, error) {
	return scanObject(db.SQL.QueryRowContext(ctx, `
SELECT `+objectSelect+`
FROM objects WHERE owner_id = ? AND key = ?
`, ownerID, key).Scan)
}

func (db *DB) ListPersonalObjects(ctx context.Context, ownerID string) ([]Object, error) {
	return db.listObjects(ctx, `
SELECT `+objectSelect+`
FROM objects WHERE owner_id = ? AND key NOT LIKE '~share/%' ORDER BY key
`, ownerID)
}

func (db *DB) ListObjectsByPrefix(ctx context.Context, ownerID, prefix string) ([]Object, error) {
	return db.listObjects(ctx, `
SELECT `+objectSelect+`
FROM objects WHERE owner_id = ? AND key LIKE ? ORDER BY key
`, ownerID, prefix+"%")
}

func (db *DB) ListObjects(ctx context.Context, ownerID string) ([]Object, error) {
	return db.listObjects(ctx, `
SELECT `+objectSelect+`
FROM objects WHERE owner_id = ? ORDER BY key
`, ownerID)
}

func (db *DB) listObjects(ctx context.Context, query string, args ...any) ([]Object, error) {
	rows, err := db.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Object
	for rows.Next() {
		o, err := scanObject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	if out == nil {
		out = []Object{}
	}
	return out, rows.Err()
}

func (db *DB) RenameObject(ctx context.Context, ownerID, from, to string) error {
	if from == to {
		return nil
	}
	if _, err := db.ObjectByKey(ctx, ownerID, from); err != nil {
		return err
	}
	if _, err := db.ObjectByKey(ctx, ownerID, to); err == nil {
		return ErrKeyTaken
	} else if !errors.Is(err, ErrObjectNotFound) {
		return err
	}

	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE objects SET key = ? WHERE owner_id = ? AND key = ?`, to, ownerID, from)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrObjectNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE shares SET object_key = ? WHERE owner_id = ? AND object_key = ?`,
		to, ownerID, from); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) UsageBytes(ctx context.Context, ownerID string) (int64, error) {
	var n sql.NullInt64
	err := db.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size), 0) FROM objects WHERE owner_id = ?`, ownerID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n.Int64, nil
}

func (db *DB) DeletePrefix(ctx context.Context, ownerID, prefix string) (int64, error) {
	if prefix == "" || !strings.HasSuffix(prefix, "/") || strings.Contains(prefix, "..") {
		return 0, ErrObjectNotFound
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	like := prefix + "%"
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM shares WHERE owner_id = ? AND object_key LIKE ?`, ownerID, like); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM objects WHERE owner_id = ? AND key LIKE ?`, ownerID, like)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func (db *DB) DeleteObject(ctx context.Context, ownerID, key string) error {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM objects WHERE owner_id = ? AND key = ?`, ownerID, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrObjectNotFound
	}
	return nil
}
