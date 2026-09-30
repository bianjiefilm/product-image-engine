package bgreplace

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
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

func TestFitPlateSamplesNearestNeighbor(t *testing.T) {
	wide := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	wide.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 0, B: 0, A: 255})
	wide.SetNRGBA(1, 0, color.NRGBA{R: 1, G: 1, B: 1, A: 255})
	wide.SetNRGBA(2, 0, color.NRGBA{R: 0, G: 180, B: 0, A: 255})
	wide.SetNRGBA(3, 0, color.NRGBA{R: 2, G: 2, B: 2, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, wide); err != nil {
		t.Fatal(err)
	}
	fitted, err := FitPlate(buf.Bytes(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	img := mustPNG(t, fitted)
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Fatalf("缩放后尺寸不对: %+v", img.Bounds())
	}
	if px(img, 0, 0) != (color.NRGBA{R: 200, G: 0, B: 0, A: 255}) || px(img, 1, 0) != (color.NRGBA{R: 0, G: 180, B: 0, A: 255}) {
		t.Fatalf("最近邻取样错了: %+v %+v", px(img, 0, 0), px(img, 1, 0))
	}
	same := solidPNG(t, 2, 1, color.NRGBA{R: 4, G: 5, B: 6, A: 255})
	kept, err := FitPlate(same, 2, 1)
	if err != nil || !bytes.Equal(kept, same) {
		t.Fatal("同尺寸底板应原样返回")
	}
}

func TestFitPlateTurnsJPEGIntoPNG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 0, B: 0, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 180, B: 0, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	fitted, err := FitPlate(buf.Bytes(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	img := mustPNG(t, fitted)
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Fatalf("JPEG 底板应变成同尺寸 PNG: %+v", img.Bounds())
	}
	if _, err := png.Decode(bytes.NewReader(fitted)); err != nil {
		t.Fatal(err)
	}
}

func TestSubjectPixelChecksPassOnlyWhenMaskMatchesOriginal(t *testing.T) {
	original := solidPNG(t, 2, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	plate := solidPNG(t, 2, 1, color.NRGBA{R: 200, G: 10, B: 10, A: 255})
	mask := maskPNG(t, [][]bool{{true, false}})
	locked, err := LockSubject(original, plate, mask)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := SubjectPixelChecks(original, locked, mask)
	if err != nil {
		t.Fatal(err)
	}
	if checks.Logo != QualityPass || checks.PackagingText != QualityPass || checks.Spec != QualityPass || checks.Structure != QualityPass || checks.SimilarityOnly || checks.Similarity != nil {
		t.Fatalf("蒙版内像素一致时应分项通过，且不能靠相似度: %+v", checks)
	}
	src := mustPNG(t, locked)
	tampered := image.NewNRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			tampered.SetNRGBA(x, y, px(src, x, y))
		}
	}
	tampered.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 1, B: 1, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := SubjectPixelChecks(original, buf.Bytes(), mask); err == nil || err.Error() != "主体像素与原图不一致" {
		t.Fatalf("改写主体必须拒绝: %v", err)
	}
}

func TestMaskReadyRejectsEmptyFullAndMismatched(t *testing.T) {
	original := solidPNG(t, 2, 1, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	if err := MaskReady(original, maskPNG(t, [][]bool{{false, false}})); err == nil || err.Error() != "主体蒙版没有保护任何像素" {
		t.Fatalf("空蒙版: %v", err)
	}
	if err := MaskReady(original, maskPNG(t, [][]bool{{true, true}})); err == nil || err.Error() != "蒙版盖住了整张图" {
		t.Fatalf("整张蒙版: %v", err)
	}
	if err := MaskReady(original, maskPNG(t, [][]bool{{true}})); err == nil || err.Error() != "尺寸不一致" {
		t.Fatalf("尺寸: %v", err)
	}
	if err := MaskReady(original, maskPNG(t, [][]bool{{true, false}})); err != nil {
		t.Fatal(err)
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
