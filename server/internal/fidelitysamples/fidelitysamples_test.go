package fidelitysamples_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
)

func TestCommittedSetLoadsWithNineVerifiedCases(t *testing.T) {
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases()) != 9 {
		t.Fatalf("cases = %d, want 9", len(set.Cases()))
	}
	kinds := map[string]int{}
	canvases := map[fs.Canvas]int{}
	for _, c := range set.Cases() {
		kinds[c.Kind]++
		canvases[c.Canvas]++
		if len(c.Intents) < 2 || c.ProductCount != 1 || c.Evidence.CoverageSHA256 == "" || c.Evidence.Scope != fs.Scope {
			t.Fatalf("case %s incomplete: %+v", c.CaseID, c.Evidence)
		}
		got, ok := set.BySHA(c.Original.SHA256)
		if !ok || got.CaseID != c.CaseID {
			t.Fatalf("BySHA(%s) lost %s", c.Original.SHA256, c.CaseID)
		}
		if _, ok := set.BySHA(c.Mask.SHA256); ok {
			t.Fatalf("a mask digest must never resolve as an original")
		}
		// The CoverageSample reuse is real: an independent VerifyCoverage agrees.
		if _, err := bgreplace.VerifyCoverage(c.OriginalBytes, c.MaskBytes, c.Case.CoverageSample()); err != nil {
			t.Fatalf("%s: %v", c.CaseID, err)
		}
	}
	for _, k := range []string{fs.KindCarton, fs.KindHandledMetal, fs.KindGlassBottle} {
		if kinds[k] != 3 {
			t.Fatalf("kind %s has %d cases", k, kinds[k])
		}
	}
	for _, cv := range []fs.Canvas{{W: 1024, H: 1024}, {W: 1024, H: 1280}, {W: 768, H: 1024}} {
		if canvases[cv] != 3 {
			t.Fatalf("canvas %+v has %d cases", cv, canvases[cv])
		}
	}
}

func TestLoadRejectsAnyBrokenSetAsAUnit(t *testing.T) {
	setPixel := func(t *testing.T, path string, a func(*image.NRGBA)) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		n := image.NewNRGBA(img.Bounds())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		a(n)
		var buf bytes.Buffer
		if err := png.Encode(&buf, n); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cartonMask := "masks/carton-1024x1024.png"
	cases := []struct {
		name, want string
		mutate     func(t *testing.T, dir string)
	}{
		{"F01 text region cut out of the mask", "packaging_text", func(t *testing.T, dir string) {
			set, _ := fs.Load(sampletest.Dir(t))
			r := set.Cases()[0].Coverage.PackagingText[0]
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) { m.SetNRGBA(r[0]+3, r[1]+3, color.NRGBA{}) })
			sampletest.Resign(t, dir, nil)
		}},
		{"F02 fully transparent mask", "没有保护任何像素", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) { m.Pix = make([]uint8, len(m.Pix)) })
			sampletest.Resign(t, dir, nil)
		}},
		{"F03 fully opaque mask", "蒙版盖住了整张图", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) {
				for i := range m.Pix {
					m.Pix[i] = 255
				}
			})
			sampletest.Resign(t, dir, nil)
		}},
		{"F05 one original pixel changed, manifest not re-signed", "sha256 differs", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, "originals/carton-1024x1024.png"), func(m *image.NRGBA) { m.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 255}) })
		}},
		{"two product components but manifest says one", "product components", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) {
				for y := 10; y < 30; y++ {
					for x := 10; x < 30; x++ {
						m.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
					}
				}
			})
			sampletest.Resign(t, dir, nil)
		}},
		{"unknown manifest field", "unknown field", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, bytes.Replace(raw, []byte(`"set_id"`), []byte(`"passed": true, "set_id"`), 1), 0o644)
		}},
		{"duplicate manifest key", "duplicate key", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, bytes.Replace(raw, []byte(`"version": "v1",`), []byte(`"version": "v1", "version": "v1",`), 1), 0o644)
		}},
		{"trailing data", "trailing data", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, append(raw, []byte(`{}`)...), 0o644)
		}},
		{"path escapes the directory", "escapes", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.Cases[0].Original.Path = "../manifest.json" })
		}},
		{"symlinked image file", "regular file", func(t *testing.T, dir string) {
			outside := filepath.Join(t.TempDir(), "x.png")
			raw, _ := os.ReadFile(filepath.Join(dir, "originals/carton-1024x1024.png"))
			os.WriteFile(outside, raw, 0o644)
			p := filepath.Join(dir, "originals/carton-1024x1024.png")
			os.Remove(p)
			if err := os.Symlink(outside, p); err != nil {
				t.Skip("symlinks unavailable")
			}
		}},
		{"symlinked directory leaves the sample directory", "outside", func(t *testing.T, dir string) {
			outside := filepath.Join(t.TempDir(), "originals")
			if err := os.Rename(filepath.Join(dir, "originals"), outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "originals")); err != nil {
				t.Skip("symlinks unavailable")
			}
		}},
		{"canvas does not match the PNG", "canvas", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.Cases[0].Canvas = fs.Canvas{W: 1024, H: 1000} })
		}},
		{"only one background intent", "intents", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.Cases[0].Intents = m.Cases[0].Intents[:1] })
		}},
		{"unapproved thresholds version", "header", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.ThresholdsVersion = "bg-lock-thresholds/v0" })
		}},
		{"set digest tampered", "set_sha256", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			set, _ := fs.Load(sampletest.Dir(t))
			os.WriteFile(p, bytes.Replace(raw, []byte(set.SHA256()), []byte(strings.Repeat("0", 64)), 1), 0o644)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := sampletest.Copy(t)
			c.mutate(t, dir)
			_, err := fs.Load(dir)
			if !errors.Is(err, fs.ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load error = %v, want ErrInvalid containing %q", err, c.want)
			}
		})
	}
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if _, err := fs.Load(dir); !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("Load(%q) = %v", dir, err)
		}
	}
}

func TestValidIntent(t *testing.T) {
	for in, want := range map[string]bool{"深蓝色背景": true, "": false, " x": false, "x ": false, "a\nb": false, strings.Repeat("界", 200): true, strings.Repeat("界", 201): false} {
		if fs.ValidIntent(in) != want {
			t.Fatalf("ValidIntent(%q) = %v, want %v", in, !want, want)
		}
	}
}
