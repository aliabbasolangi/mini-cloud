package avatar

import (
	"errors"
	"image"
	"image/jpeg"
	_ "image/gif"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp"

	"minicloud/internal/thumb"
)

const maxEdge = 256

var ErrNotImage = errors.New("not an image")

type Store struct {
	dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Path(userID string) string {
	return filepath.Join(s.dir, userID+".jpg")
}

func (s *Store) Exists(userID string) bool {
	_, err := os.Stat(s.Path(userID))
	return err == nil
}

func (s *Store) Save(userID string, r io.Reader) error {
	img, _, err := image.Decode(r)
	if err != nil {
		return ErrNotImage
	}
	fitted := thumb.Fit(img, maxEdge)
	tmp, err := os.CreateTemp(s.dir, "avatar-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	err = jpeg.Encode(tmp, fitted, &jpeg.Options{Quality: 86})
	closeErr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	return os.Rename(tmpName, s.Path(userID))
}

func (s *Store) Open(userID string) (io.ReadCloser, error) {
	f, err := os.Open(s.Path(userID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	return f, err
}

func (s *Store) Remove(userID string) error {
	err := os.Remove(s.Path(userID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
