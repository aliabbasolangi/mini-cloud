package thumb

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestFitShrinks(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 80, B: 40, A: 255})
		}
	}
	got := Fit(src, 160)
	if got.Bounds().Dx() != 160 || got.Bounds().Dy() != 80 {
		t.Fatalf("got %dx%d want 160x80", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestDecodePNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 8 {
		t.Fatalf("width %d", img.Bounds().Dx())
	}
}

func TestLooksLikeImage(t *testing.T) {
	if !LooksLikeImage("holiday.JPG") {
		t.Fatal("jpg")
	}
	if LooksLikeImage("notes.txt") {
		t.Fatal("txt should not look like an image")
	}
}
