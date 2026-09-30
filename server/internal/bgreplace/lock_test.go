package bgreplace

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestLockSubjectKeepsProtectedPixelsAndReplacesTheRest(t *testing.T) {
	original := solidPNG(t, 2, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	plate := solidPNG(t, 2, 2, color.NRGBA{R: 200, G: 10, B: 10, A: 255})
	mask := maskPNG(t, [][]bool{
		{true, false},
		{true, false},
	})
	got, err := LockSubject(original, plate, mask)
	if err != nil {
		t.Fatal(err)
	}
	img := mustPNG(t, got)
	if px(img, 0, 0) != (color.NRGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("左上主体像素被改写: %+v", px(img, 0, 0))
	}
	if px(img, 0, 1) != (color.NRGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("左下主体像素被改写: %+v", px(img, 0, 1))
	}
	if px(img, 1, 0) != (color.NRGBA{R: 200, G: 10, B: 10, A: 255}) {
		t.Fatalf("背景像素没有换成底板: %+v", px(img, 1, 0))
	}
	if px(img, 1, 1) != (color.NRGBA{R: 200, G: 10, B: 10, A: 255}) {
		t.Fatalf("右下背景像素没有换成底板: %+v", px(img, 1, 1))
	}
}

func TestLockSubjectRejectsEmptyMaskAndSizeMismatch(t *testing.T) {
	original := solidPNG(t, 2, 1, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	plate := solidPNG(t, 2, 1, color.NRGBA{R: 9, G: 9, B: 9, A: 255})
	open := maskPNG(t, [][]bool{{false, false}})
	if _, err := LockSubject(original, plate, open); err == nil || err.Error() != "主体蒙版没有保护任何像素" {
		t.Fatalf("空蒙版应拒绝, got %v", err)
	}
	wide := solidPNG(t, 3, 1, color.NRGBA{A: 255})
	mask := maskPNG(t, [][]bool{{true, false}})
	if _, err := LockSubject(original, wide, mask); err == nil || err.Error() != "尺寸不一致" {
		t.Fatalf("尺寸不一致应拒绝, got %v", err)
	}
}

func TestLockSubjectRejectsUnchangedBackground(t *testing.T) {
	original := solidPNG(t, 2, 1, color.NRGBA{R: 4, G: 5, B: 6, A: 255})
	mask := maskPNG(t, [][]bool{{true, true}})
	if _, err := LockSubject(original, original, mask); err == nil || err.Error() != "蒙版盖住了整张图，背景没有换" {
		t.Fatalf("整张蒙版应拒绝, got %v", err)
	}
}

func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func maskPNG(t *testing.T, protect [][]bool) []byte {
	t.Helper()
	h := len(protect)
	w := len(protect[0])
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(0)
			if protect[y][x] {
				a = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustPNG(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func px(img image.Image, x, y int) color.NRGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
