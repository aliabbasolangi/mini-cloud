package convert

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

func convertImage(src []byte, srcName, to string) (*Result, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, ErrUnsupported
	}
	name := Stem(srcName) + "." + to
	switch to {
	case "png":
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		return &Result{Bytes: buf.Bytes(), Name: name, ContentType: "image/png"}, nil
	case "jpg":
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
		return &Result{Bytes: buf.Bytes(), Name: name, ContentType: "image/jpeg"}, nil
	case "gif":
		var buf bytes.Buffer
		if err := gif.Encode(&buf, img, nil); err != nil {
			return nil, err
		}
		return &Result{Bytes: buf.Bytes(), Name: name, ContentType: "image/gif"}, nil
	case "pdf":
		var jpg bytes.Buffer
		if err := jpeg.Encode(&jpg, img, &jpeg.Options{Quality: 88}); err != nil {
			return nil, err
		}
		out, err := JPEGToPDF(jpg.Bytes(), img.Bounds().Dx(), img.Bounds().Dy())
		if err != nil {
			return nil, err
		}
		return &Result{Bytes: out, Name: Stem(srcName) + ".pdf", ContentType: "application/pdf"}, nil
	default:
		return nil, ErrUnsupported
	}
}
