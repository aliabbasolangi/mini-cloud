package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrShareNotFound  = errors.New("share not found")
	ErrShareExpired   = errors.New("share expired")
	ErrShareNotFolder = errors.New("this link is for a file")
	ErrBadShareTTL    = errors.New("pick how long the link should last")
	ErrBadShare       = errors.New("choose a file or a folder")
)

const (
	ShareKindFile   = "file"
	ShareKindFolder = "folder"
	shareTTL        = 24 * time.Hour
)

var ShareNeverTime = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

type Share struct {
	Token     string
	OwnerID   string
	ObjectKey string
	FolderID  string
	Kind      string
	Role      string
	ExpiresAt time.Time
}

func (s *Share) NeverExpires() bool {
	return s != nil && !s.ExpiresAt.IsZero() && s.ExpiresAt.Year() >= 9000
}

type ShareInput struct {
	Key      string
	FolderID string
	Role     string
	TTLHours int
	Never    bool
}

func ShareExpiry(ttlHours int, never bool) (time.Time, error) {
	if never {
		return ShareNeverTime, nil
	}
	if ttlHours == 0 {
		ttlHours = int(shareTTL / time.Hour)
	}
	switch ttlHours {
	case 1, 24, 168, 720:
		return time.Now().UTC().Add(time.Duration(ttlHours) * time.Hour), nil
	default:
		return time.Time{}, ErrBadShareTTL
	}
}

func NormalizeShareRole(role string) (string, error) {
	if strings.TrimSpace(role) == "" {
		return CollabRoleViewer, nil
	}
	return NormalizeMemberRole(role)
}

func newShareToken() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (db *DB) CreateShare(ctx context.Context, ownerID string, in ShareInput) (*Share, error) {
	role, err := NormalizeShareRole(in.Role)
	if err != nil {
		return nil, err
	}
	expires, err := ShareExpiry(in.TTLHours, in.Never)
	if err != nil {
		return nil, err
	}

	s := &Share{
		Token:     newShareToken(),
		OwnerID:   ownerID,
		Role:      role,
		ExpiresAt: expires,
	}

	switch {
	case strings.TrimSpace(in.FolderID) != "" && strings.TrimSpace(in.Key) == "":
		folder, err := db.collabByID(ctx, strings.TrimSpace(in.FolderID))
		if err != nil {
			return nil, err
		}
		if folder.OwnerID != ownerID {
			return nil, ErrCollabDenied
		}
		s.Kind = ShareKindFolder
		s.FolderID = folder.ID
	case strings.TrimSpace(in.Key) != "":
		if _, err := db.ObjectByKey(ctx, ownerID, in.Key); err != nil {
			return nil, err
		}
		s.Kind = ShareKindFile
		s.ObjectKey = in.Key
		s.Role = CollabRoleViewer
	default:
		return nil, ErrBadShare
	}

	now := time.Now().UTC()
	_, err = db.SQL.ExecContext(ctx, `
INSERT INTO shares (token, owner_id, object_key, expires_at, revoked, created_at, kind, folder_id, role)
VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)
`, s.Token, s.OwnerID, s.ObjectKey, s.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339), s.Kind, s.FolderID, s.Role)
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
SELECT owner_id, object_key, expires_at, revoked, kind, folder_id, role
FROM shares WHERE token = ?
`, token).Scan(&s.OwnerID, &s.ObjectKey, &expires, &revoked, &s.Kind, &s.FolderID, &s.Role)
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
	if s.Kind == "" {
		if s.FolderID != "" {
			s.Kind = ShareKindFolder
		} else {
			s.Kind = ShareKindFile
		}
	}
	if s.Role == "" {
		s.Role = CollabRoleViewer
	}
	if !s.NeverExpires() && time.Now().UTC().After(s.ExpiresAt) {
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

func (db *DB) JoinViaShare(ctx context.Context, userID, token string) (*CollabFolder, error) {
	share, err := db.ValidShare(ctx, token)
	if err != nil {
		return nil, err
	}
	if share.Kind != ShareKindFolder || share.FolderID == "" {
		return nil, ErrShareNotFolder
	}
	f, err := db.collabByID(ctx, share.FolderID)
	if err != nil {
		return nil, err
	}
	if f.OwnerID == userID {
		f.Role = CollabRoleOwner
		return f, nil
	}
	if existing, err := db.CollabAccess(ctx, userID, share.FolderID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrCollabDenied) {
		return nil, err
	}
	role, err := NormalizeMemberRole(share.Role)
	if err != nil {
		role = CollabRoleViewer
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.SQL.ExecContext(ctx, `
INSERT OR IGNORE INTO collab_members (folder_id, user_id, created_at, role)
VALUES (?, ?, ?, ?)
`, share.FolderID, userID, now, role); err != nil {
		return nil, err
	}
	return db.CollabAccess(ctx, userID, share.FolderID)
}
