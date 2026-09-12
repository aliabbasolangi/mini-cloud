package thumb

import (
	"context"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"minicloud/internal/blob"
)

const maxEdge = 160

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

func LooksLikeImage(key string) bool {
	switch strings.ToLower(filepath.Ext(key)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func (s *Store) Path(sha256 string) string {
	return filepath.Join(s.dir, sha256+".jpg")
}

func (s *Store) Exists(sha256 string) bool {
	_, err := os.Stat(s.Path(sha256))
	return err == nil
}

// Ensure writes a small JPEG preview for this blob if it is an image.
// Safe to call twice — the second time is a no-op.
func (s *Store) Ensure(ctx context.Context, blobs blob.Store, sha256 string) error {
	if s.Exists(sha256) {
		return nil
	}
	src, err := blobs.Get(ctx, sha256)
	if err != nil {
		return err
	}
	defer src.Close()

	img, err := Decode(src)
	if err != nil {
		return err
	}
	return s.write(sha256, Fit(img, maxEdge))
}

func (s *Store) Open(sha256 string) (io.ReadCloser, error) {
	f, err := os.Open(s.Path(sha256))
	if errors.Is(err, os.ErrNotExist) {
		return nil, blob.ErrNotFound
	}
	return f, err
}

func Decode(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, ErrNotImage
	}
	return img, nil
}

func Fit(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	if w <= max && h <= max {
		return src
	}
	nw, nh := w, h
	if w >= h {
		nw = max
		nh = h * max / w
	} else {
		nh = max
		nw = w * max / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func (s *Store) write(sha256 string, img image.Image) error {
	tmp, err := os.CreateTemp(s.dir, "thumb-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	err = jpeg.Encode(tmp, img, &jpeg.Options{Quality: 82})
	closeErr := tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	return os.Rename(tmpName, s.Path(sha256))
}
