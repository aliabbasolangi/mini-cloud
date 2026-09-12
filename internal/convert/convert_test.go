package convert

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageToJPEGAndPDF(t *testing.T) {
	src := tinyPNG(t)
	jpg, err := Convert(src, "logo.png", "jpg")
	if err != nil || !strings.HasSuffix(jpg.Name, ".jpg") || len(jpg.Bytes) < 20 {
		t.Fatalf("jpg: %v %+v", err, jpg)
	}
	pdf, err := Convert(src, "logo.png", "pdf")
	if err != nil || !bytes.Contains(pdf.Bytes, []byte("%PDF")) {
		t.Fatalf("pdf: %v", err)
	}
}

func TestTextToDocxAndBack(t *testing.T) {
	doc, err := Convert([]byte("hello Ali\nsecond line"), "notes.txt", "docx")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doc.Bytes, []byte("PK")) {
		t.Fatal("docx should be a zip")
	}
	txt, err := Convert(doc.Bytes, "notes.docx", "txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(txt.Bytes), "hello Ali") {
		t.Fatalf("round trip got %q", txt.Bytes)
	}
}

func TestTextToPDF(t *testing.T) {
	pdf, err := Convert([]byte("Mini Cloud"), "hello.txt", "pdf")
	if err != nil || !bytes.Contains(pdf.Bytes, []byte("%PDF")) {
		t.Fatalf("pdf: %v", err)
	}
}

func TestUnsupported(t *testing.T) {
	_, err := Convert([]byte("x"), "file.xyz", "mp4")
	if err != ErrUnsupported {
		t.Fatalf("got %v", err)
	}
}
