package bgreplace

import (
	"image/color"
	"testing"
)

func TestCoverageRequiresFrozenAuthorizationAndEveryAxis(t *testing.T) {
	original := solidPNG(t, 4, 4, color.NRGBA{R: 80, A: 255})
	mask := maskPNG(t, [][]bool{{true, true, false, false}, {true, true, false, false}, {true, true, false, false}, {true, true, false, false}})
	good := CoverageSample{ID: "synthetic-fixture", Source: "test generated pixels", License: "test-only", ApprovalRef: "contract-test", Scope: "synthetic test only", OriginalSHA256: ImageSHA(original), MaskSHA256: ImageSHA(mask), Width: 4, Height: 4, Regions: map[string][]CoverageRegion{
		AxisLogo: {{0, 0, 2, 1}}, AxisPackagingText: {{0, 1, 2, 2}}, AxisSpec: {{0, 2, 2, 3}}, AxisStructure: {{0, 3, 2, 4}},
	}}
	got, err := VerifyCoverage(original, mask, good)
	if err != nil || got.SampleID != good.ID || got.CoverageSHA256 == "" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, which := range []string{"license", "original", "mask", "axis", "uncovered", "bounds", "empty"} {
		t.Run(which, func(t *testing.T) {
			bad := good
			bad.Regions = map[string][]CoverageRegion{}
			for k, v := range good.Regions {
				bad.Regions[k] = append([]CoverageRegion(nil), v...)
			}
			switch which {
			case "license":
				bad.License = ""
			case "original":
				bad.OriginalSHA256 = "wrong"
			case "mask":
				bad.MaskSHA256 = "wrong"
			case "axis":
				delete(bad.Regions, AxisSpec)
			case "uncovered":
				bad.Regions[AxisPackagingText] = []CoverageRegion{{1, 1, 3, 2}}
			case "bounds":
				bad.Regions[AxisLogo] = []CoverageRegion{{-1, 0, 1, 1}}
			case "empty":
				bad.Regions[AxisLogo] = []CoverageRegion{{0, 0, 0, 1}}
			}
			if _, err := VerifyCoverage(original, mask, bad); err == nil {
				t.Fatal("invalid coverage accepted")
			}
		})
	}
}

func TestCoverageCannotUsePixelEqualityToExcuseMissingText(t *testing.T) {
	original := solidPNG(t, 2, 1, color.NRGBA{R: 40, A: 255})
	mask := maskPNG(t, [][]bool{{true, false}})
	sample := CoverageSample{ID: "fixture", Source: "test", License: "test", ApprovalRef: "test", Scope: "test", OriginalSHA256: ImageSHA(original), MaskSHA256: ImageSHA(mask), Width: 2, Height: 1, Regions: map[string][]CoverageRegion{AxisLogo: {{0, 0, 1, 1}}, AxisPackagingText: {{1, 0, 2, 1}}, AxisSpec: {{0, 0, 1, 1}}, AxisStructure: {{0, 0, 1, 1}}}}
	if _, err := SubjectPixelChecks(original, original, mask); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCoverage(original, mask, sample); err == nil {
		t.Fatal("equal protected pixels cannot prove unprotected text")
	}
}
