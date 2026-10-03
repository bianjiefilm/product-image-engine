package samplegen_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	"github.com/bianjiefilm/product-image-engine/server/internal/samplegen"
)

func TestCommittedSetStillMatchesTheGenerator(t *testing.T) {
	if err := samplegen.Check(sampletest.Dir(t)); err != nil {
		t.Fatal(err)
	}
}

func TestBuildIsDeterministicAndCoversNineCases(t *testing.T) {
	a, b := samplegen.Build(), samplegen.Build()
	if len(a) != 9 || len(b) != 9 {
		t.Fatalf("cases = %d/%d", len(a), len(b))
	}
	for i := range a {
		if a[i].Meta.Original.PixelSHA256 != b[i].Meta.Original.PixelSHA256 || a[i].Meta.Mask.PixelSHA256 != b[i].Meta.Mask.PixelSHA256 {
			t.Fatalf("case %s differs between two builds", a[i].Meta.CaseID)
		}
		if a[i].Meta.Original.PixelSHA256 == a[i].Meta.Mask.PixelSHA256 {
			t.Fatalf("case %s original and mask are identical", a[i].Meta.CaseID)
		}
	}
}

func TestWriteThenCheckRoundTripAndTamperDetection(t *testing.T) {
	dir := t.TempDir()
	if err := samplegen.Write(dir); err != nil {
		t.Fatal(err)
	}
	if err := samplegen.Check(dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "masks", "carton-1024x1024.png")
	raw, _ := os.ReadFile(target)
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := samplegen.Check(dir); err == nil || !strings.Contains(err.Error(), "differ") {
		t.Fatalf("tampered mask not detected: %v", err)
	}
}

// R30: a frozen background direction must contrast clearly with the sample's own background,
// otherwise a perfectly good model plate would be (conservatively) judged background_changed=FAIL.
func TestEveryFrozenDirectionContrastsWithItsSampleBackground(t *testing.T) {
	for _, c := range samplegen.Build() {
		bg := c.Original.NRGBAAt(0, 0) // the sample's own background colour (the corner is never subject)
		for _, in := range c.Meta.Intents {
			ref, ok := samplegen.DirectionReference[in]
			if !ok {
				t.Fatalf("%s: direction %q has no reference colour; add it to samplegen.DirectionReference", c.Meta.CaseID, in)
			}
			d := math.Sqrt(math.Pow(float64(ref.R)-float64(bg.R), 2) + math.Pow(float64(ref.G)-float64(bg.G), 2) + math.Pow(float64(ref.B)-float64(bg.B), 2))
			if d < samplegen.MinDirectionContrast {
				t.Fatalf("%s: direction %q is only %.0f from the sample background (need >= %.0f)", c.Meta.CaseID, in, d, samplegen.MinDirectionContrast)
			}
		}
	}
}

func TestEveryDirectionReferenceBelongsToAFrozenDirection(t *testing.T) {
	used := map[string]bool{}
	for _, c := range samplegen.Build() {
		for _, in := range c.Meta.Intents {
			used[in] = true
		}
	}
	for in := range samplegen.DirectionReference {
		if !used[in] {
			t.Fatalf("reference colour for %q but no case freezes that direction", in)
		}
	}
}
