package convert

import (
	"bytes"
	"compress/zlib"
	"fmt"
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

func TestPDFToDocxKeepsReadableText(t *testing.T) {
	pdf, err := TextToPDF("Ali Abbas\nSoftware engineer")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Convert(pdf, "cv.pdf", "docx")
	if err != nil {
		t.Fatal(err)
	}
	txt, err := Convert(doc.Bytes, "cv.docx", "txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(txt.Bytes), "Ali") {
		t.Fatalf("got %q", txt.Bytes)
	}
}

func TestCompressedPDFToText(t *testing.T) {
	content := "BT /F1 11 Tf (SafeKeeping vault) Tj ET\n"
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var pdf bytes.Buffer
	fmt.Fprintf(&pdf, "%%PDF-1.4\n1 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", zbuf.Len())
	pdf.Write(zbuf.Bytes())
	pdf.WriteString("\nendstream\nendobj\n")
	got := PDFToText(pdf.Bytes())
	if !strings.Contains(got, "SafeKeeping") {
		t.Fatalf("got %q", got)
	}
}

func TestBinaryPDFDoesNotBecomeGarbageDocx(t *testing.T) {
	src := append([]byte("%PDF-1.4\n<< /Length 40 >>\nstream\n"), bytes.Repeat([]byte{0x80, 0xff, 0x00}, 40)...)
	src = append(src, []byte("\nendstream\n")...)
	_, err := Convert(src, "x.pdf", "docx")
	if err != ErrNoText {
		t.Fatalf("got %v", err)
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
