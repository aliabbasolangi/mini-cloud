package catalog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrObjectNotFound = errors.New("object not found")
	ErrKeyTaken       = errors.New("a file with that name already exists")
)

type Object struct {
	ID        string
	OwnerID   string
	Key       string
	BlobSHA   string
	Size      int64
	CreatedAt time.Time
}

func (db *DB) UpsertObject(ctx context.Context, ownerID, key, blobSHA string, size int64) (*Object, error) {
	now := time.Now().UTC()
	id := newID()
	_, err := db.SQL.ExecContext(ctx, `
INSERT INTO objects (id, owner_id, key, blob_sha, size, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(owner_id, key) DO UPDATE SET
	blob_sha = excluded.blob_sha,
	size = excluded.size
`, id, ownerID, key, blobSHA, size, now.Format(time.RFC3339))
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
	var created string
	o := &Object{}
	err := db.SQL.QueryRowContext(ctx, `
SELECT id, owner_id, key, blob_sha, size, created_at
FROM objects WHERE owner_id = ? AND key = ?
`, ownerID, key).Scan(&o.ID, &o.OwnerID, &o.Key, &o.BlobSHA, &o.Size, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, err
	}
	o.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return o, nil
}

func (db *DB) ListObjects(ctx context.Context, ownerID string) ([]Object, error) {
	rows, err := db.SQL.QueryContext(ctx, `
SELECT id, owner_id, key, blob_sha, size, created_at
FROM objects WHERE owner_id = ? ORDER BY key
`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Object
	for rows.Next() {
		var created string
		var o Object
		if err := rows.Scan(&o.ID, &o.OwnerID, &o.Key, &o.BlobSHA, &o.Size, &created); err != nil {
			return nil, err
		}
		o.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, o)
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
