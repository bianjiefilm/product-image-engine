package platelock_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	pl "github.com/bianjiefilm/product-image-engine/server/internal/platelock"
)

func loadSet(t *testing.T) *fs.Set {
	t.Helper()
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func caseByID(t *testing.T, set *fs.Set, id string) *fs.LoadedCase {
	t.Helper()
	for _, c := range set.Cases() {
		if c.CaseID == id {
			return c
		}
	}
	t.Fatalf("case %s missing", id)
	return nil
}

func solid(w, h int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return enc(img)
}

func enc(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// gradient is a deterministic, textured, clearly different plate.
func gradient(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*60/w), uint8(50 + y*90/h), uint8(110 + (x+y)*40/(w+h)), 255})
		}
	}
	return enc(img)
}

func inputFor(c *fs.LoadedCase, plate []byte) pl.Input {
	return pl.Input{
		Case: c.Case, Original: c.OriginalBytes, Mask: c.MaskBytes, CoverageSHA256: c.Evidence.CoverageSHA256, Plate: plate,
		Facts: pl.Facts{Mode: "background_plate_lock", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", PricingVersion: "test-v1",
			UsageID: "usage-1", QuoteID: "quote-1", TaskID: "task-1", OriginalChargeID: "charge-1",
			PlateAssetID: "asset-1", PlateReferenceID: "ref-1", PlateSHA256: "aa", TaskSucceeded: true, PlateHashVerified: true, ChargeSeen: true},
		Build: pl.Build{VCSRevision: "deadbeef", VCSModified: false},
	}
}

func axisState(t *testing.T, r pl.Report, name string) string {
	t.Helper()
	for _, a := range r.Axes {
		if a.Name == name {
			return a.State
		}
	}
	t.Fatalf("axis %s missing", name)
	return ""
}

// Calibration of bg-lock-thresholds/v1 against the three required references
// plus boundary plates. These numbers may only ever be tightened.
func TestThresholdCalibration(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	bg := color.NRGBA{226, 222, 214, 255} // the frozen carton background
	noisy := func() []byte {
		img, _ := png.Decode(bytes.NewReader(c.OriginalBytes))
		out := image.NewNRGBA(img.Bounds())
		for y := 0; y < 1024; y++ {
			for x := 0; x < 1024; x++ {
				p := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				d := uint8((x*7 + y*13) % 7) // 0..6, i.e. +-3 around 3
				p.R, p.G, p.B = clamp(int(p.R)+int(d)-3), clamp(int(p.G)+int(d)-3), clamp(int(p.B)+int(d)-3)
				out.SetNRGBA(x, y, p)
			}
		}
		return enc(out)
	}()
	half := func() []byte { // left half equals the original background, right half is far away
		img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		for y := 0; y < 1024; y++ {
			for x := 0; x < 1024; x++ {
				if x < 512 {
					img.SetNRGBA(x, y, bg)
				} else {
					img.SetNRGBA(x, y, color.NRGBA{20, 60, 120, 255})
				}
			}
		}
		return enc(img)
	}()
	cases := []struct {
		name           string
		plate          []byte
		newPlate, back string
		state          string
	}{
		{"F07 plate equals the original", c.OriginalBytes, pl.Fail, pl.Fail, pl.CandidateNotUsable},
		{"F07 plate is the original with +-3 noise", noisy, pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"background colour only 6 away", solid(1024, 1024, color.NRGBA{232, 228, 220, 255}), pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"light grey that is 12 away on one channel", solid(1024, 1024, color.NRGBA{214, 214, 214, 255}), pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"only half the background changed", half, pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"white studio background", solid(1024, 1024, color.NRGBA{250, 250, 250, 255}), pl.Pass, pl.Pass, pl.CandidateLimited},
		{"deep blue solid", solid(1024, 1024, color.NRGBA{30, 60, 110, 255}), pl.Pass, pl.Pass, pl.CandidateLimited},
		{"textured gradient", gradient(1024, 1024), pl.Pass, pl.Pass, pl.CandidateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := pl.Derive(inputFor(c, tc.plate))
			if got := axisState(t, out.Report, pl.AxisNewPlate); got != tc.newPlate {
				t.Errorf("plate_is_new_background = %s, want %s", got, tc.newPlate)
			}
			if got := axisState(t, out.Report, pl.AxisBackground); got != tc.back {
				t.Errorf("background_changed = %s, want %s", got, tc.back)
			}
			if out.Report.CandidateState != tc.state {
				t.Errorf("candidate_state = %s, want %s", out.Report.CandidateState, tc.state)
			}
			if out.Composite == nil {
				t.Fatal("a failed background must still produce a record with its composite")
			}
			// Subject pixels are never what fails in these cases.
			for _, n := range []string{pl.AxisMaskPixels, pl.AxisLogo, pl.AxisText, pl.AxisSpec, pl.AxisStructure, pl.AxisProductCount, pl.AxisOutputSize, pl.AxisPlateSize} {
				if got := axisState(t, out.Report, n); got != pl.Pass {
					t.Errorf("%s = %s, want PASS", n, got)
				}
			}
		})
	}
}

func clamp(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func TestEveryCaseLimitedCandidateWithRealisticPlate(t *testing.T) {
	set := loadSet(t)
	for _, c := range set.Cases() {
		out := pl.Derive(inputFor(c, gradient(1024, 1024)))
		if out.Report.CandidateState != pl.CandidateLimited || len(out.Report.Limits) < 4 {
			t.Fatalf("%s: %s limits=%d axes=%+v", c.CaseID, out.Report.CandidateState, len(out.Report.Limits), out.Report.Axes)
		}
		if out.Report.Verdicts.Fidelity != pl.Pass || out.Report.Verdicts.Generation != pl.Pass || out.Report.Verdicts.Billing != pl.Pass || out.Report.Verdicts.HumanUsefulness != "NOT_RUN" {
			t.Fatalf("%s verdicts %+v", c.CaseID, out.Report.Verdicts)
		}
		wantTrans := pl.NA
		if c.MaterialClass == fs.MaterialTransparent {
			wantTrans = pl.Unknown
			if len(out.Report.Limits) != 5 {
				t.Fatalf("glass must carry the transmission limit: %v", out.Report.Limits)
			}
		}
		if got := axisState(t, out.Report, pl.AxisTransmission); got != wantTrans {
			t.Fatalf("%s transmission = %s", c.CaseID, got)
		}
		for _, n := range []string{pl.AxisExtraObjects, pl.AxisIntent, pl.AxisVisual} {
			if axisState(t, out.Report, n) != pl.Unknown {
				t.Fatalf("%s: %s must stay UNKNOWN", c.CaseID, n)
			}
		}
		// Independent proof that the composite really locks the subject.
		if _, err := bgreplace.SubjectPixelChecks(c.OriginalBytes, out.Composite, c.MaskBytes); err != nil {
			t.Fatalf("%s: %v", c.CaseID, err)
		}
		img, _ := png.Decode(bytes.NewReader(out.Composite))
		if img.Bounds().Dx() != c.Canvas.W || img.Bounds().Dy() != c.Canvas.H {
			t.Fatalf("%s composite %v", c.CaseID, img.Bounds())
		}
	}
}

func TestF06WrongPlateSizeIsNotUsableButStillRecorded(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	out := pl.Derive(inputFor(c, gradient(512, 512)))
	if axisState(t, out.Report, pl.AxisPlateSize) != pl.Fail || out.Report.CandidateState != pl.CandidateNotUsable {
		t.Fatalf("%+v", out.Report)
	}
	if out.Composite == nil || out.Report.Facts.CompositeSHA256 == "" || out.Report.Facts.OriginalChargeID != "charge-1" {
		t.Fatal("the record must keep the charge fact and the composite")
	}
}

func TestF08UndecodablePlateBytesAreFailedAxesNotErrors(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	for name, plate := range map[string][]byte{"not a PNG": []byte("GIF89a not a png"), "empty": nil, "truncated PNG": gradient(1024, 1024)[:200]} {
		out := pl.Derive(inputFor(c, plate))
		if out.Composite != nil || out.Report.CandidateState != pl.CandidateNotUsable {
			t.Fatalf("%s: composite=%v state=%s", name, out.Composite != nil, out.Report.CandidateState)
		}
		for _, n := range []string{pl.AxisMaskPixels, pl.AxisPlateSize, pl.AxisNewPlate, pl.AxisBackground} {
			if axisState(t, out.Report, n) != pl.Fail {
				t.Fatalf("%s: %s must FAIL", name, n)
			}
		}
		raw, err := pl.ReportJSON(out.Report)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pl.DecodeReport(raw); err != nil {
			t.Fatalf("%s: a failed report must still be a valid frozen report: %v", name, err)
		}
	}
}

func TestFitPlateCoverNeverStretches(t *testing.T) {
	plate := gradient(1024, 1024)
	src, _ := png.Decode(bytes.NewReader(plate))
	for _, tc := range []struct{ w, h int }{{1024, 1024}, {1024, 1280}, {768, 1024}} {
		raw, err := pl.FitPlateCover(plate, tc.w, tc.h)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil || img.Bounds().Dx() != tc.w || img.Bounds().Dy() != tc.h {
			t.Fatalf("%v: %v %v", tc, img.Bounds(), err)
		}
		if tc.w != 1024 || tc.h != 1024 {
			// 768x1024 is a pure centre crop: column 0 of the output is column 128 of the plate.
			if tc.w == 768 {
				for _, y := range []int{0, 300, 1023} {
					if color.NRGBAModel.Convert(img.At(0, y)) != color.NRGBAModel.Convert(src.At(128, y)) {
						t.Fatalf("768x1024 is not a centre crop at y=%d", y)
					}
				}
			}
		}
	}
	if _, err := pl.FitPlateCover([]byte("x"), 10, 10); err == nil {
		t.Fatal("garbage plate accepted")
	}
	if _, err := pl.FitPlateCover(plate, 0, 10); err == nil {
		t.Fatal("zero canvas accepted")
	}
}

func TestComposeMatchesLockSubjectWhenLockSubjectSucceeds(t *testing.T) {
	set := loadSet(t)
	for _, c := range set.Cases() {
		fitted, err := pl.FitPlateCover(gradient(1024, 1024), c.Canvas.W, c.Canvas.H)
		if err != nil {
			t.Fatal(err)
		}
		want, err := bgreplace.LockSubject(c.OriginalBytes, fitted, c.MaskBytes)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pl.Compose(c.OriginalBytes, fitted, c.MaskBytes)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: Compose differs from LockSubject (%v)", c.CaseID, err)
		}
	}
	// ...and where LockSubject refuses (plate == original) Compose still answers.
	c := set.Cases()[0]
	if _, err := bgreplace.LockSubject(c.OriginalBytes, c.OriginalBytes, c.MaskBytes); err == nil {
		t.Fatal("LockSubject should refuse an unchanged background")
	}
	if _, err := pl.Compose(c.OriginalBytes, c.OriginalBytes, c.MaskBytes); err != nil {
		t.Fatal(err)
	}
}

func TestF18DeriveIsDeterministic(t *testing.T) {
	set := loadSet(t)
	for _, id := range []string{"carton-1024x1024", "handled_metal-1024x1280", "glass_bottle-768x1024"} {
		c := caseByID(t, set, id)
		a, b := pl.Derive(inputFor(c, gradient(1024, 1024))), pl.Derive(inputFor(c, gradient(1024, 1024)))
		ra, _ := pl.ReportJSON(a.Report)
		rb, _ := pl.ReportJSON(b.Report)
		if !bytes.Equal(a.Composite, b.Composite) || !bytes.Equal(ra, rb) {
			t.Fatalf("%s: two derivations differ", id)
		}
	}
}

func TestSubjectTamperIsCaughtOnTheComposite(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	in := inputFor(c, gradient(1024, 1024))
	fitted, _ := pl.FitPlateCover(in.Plate, 1024, 1024)
	composite, _ := pl.Compose(in.Original, fitted, in.Mask)
	img, _ := png.Decode(bytes.NewReader(composite))
	bad := image.NewNRGBA(img.Bounds())
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			bad.Set(x, y, img.At(x, y))
		}
	}
	r := c.Coverage.PackagingText[0]
	bad.SetNRGBA(r[0]+5, r[1]+5, color.NRGBA{1, 2, 3, 255}) // one pixel of the brand text altered
	rep := pl.Evaluate(in, fitted, enc(bad))
	if axisState(t, rep, pl.AxisMaskPixels) != pl.Fail || axisState(t, rep, pl.AxisText) != pl.Fail || rep.CandidateState != pl.CandidateNotUsable || rep.Verdicts.Fidelity != pl.Fail {
		t.Fatalf("a single altered text pixel must fail fidelity: %+v", rep.Axes)
	}
	if axisState(t, rep, pl.AxisLogo) != pl.Pass {
		t.Fatal("untouched logo axis must stay PASS (axes do not collapse)")
	}
}

func TestF22StateOfProperty(t *testing.T) {
	names := []string{pl.AxisMaskPixels, pl.AxisLogo, pl.AxisText, pl.AxisSpec, pl.AxisStructure, pl.AxisProductCount, pl.AxisOutputSize, pl.AxisPlateSize, pl.AxisNewPlate, pl.AxisBackground, pl.AxisTask, pl.AxisAssetHash, pl.AxisCharged}
	states := []string{pl.Pass, pl.Fail, pl.Unknown}
	// All 3^13 combinations would be 1.6M; sweep every single-axis deviation from
	// all-PASS plus every pair, which covers each axis in each position.
	build := func(over map[int]string) []pl.Axis {
		axes := make([]pl.Axis, 0, len(names)+4)
		for i, n := range names {
			st := pl.Pass
			if s, ok := over[i]; ok {
				st = s
			}
			axes = append(axes, pl.Axis{Name: n, State: st, MachineChecked: true})
		}
		for _, n := range []string{pl.AxisExtraObjects, pl.AxisIntent, pl.AxisVisual, pl.AxisTransmission} {
			axes = append(axes, pl.Axis{Name: n, State: pl.Unknown})
		}
		return axes
	}
	want := func(over map[int]string) string {
		anyFail, anyOther := false, false
		for _, s := range over {
			if s == pl.Fail {
				anyFail = true
			} else if s != pl.Pass {
				anyOther = true
			}
		}
		switch {
		case anyFail:
			return pl.CandidateNotUsable
		case anyOther:
			return pl.CandidatePending
		}
		return pl.CandidateLimited
	}
	if pl.StateOf(build(nil)) != pl.CandidateLimited {
		t.Fatal("all PASS must be a limited candidate")
	}
	for i := range names {
		for _, a := range states {
			over := map[int]string{i: a}
			if got := pl.StateOf(build(over)); got != want(over) {
				t.Fatalf("single %s=%s: got %s want %s", names[i], a, got, want(over))
			}
			for j := i + 1; j < len(names); j++ {
				for _, b := range states {
					over := map[int]string{i: a, j: b}
					if got := pl.StateOf(build(over)); got != want(over) {
						t.Fatalf("%s=%s %s=%s: got %s want %s", names[i], a, names[j], b, got, want(over))
					}
				}
			}
		}
	}
	// A missing required axis can never be a candidate.
	if pl.StateOf(build(nil)[1:]) != pl.CandidatePending {
		t.Fatal("a report without a required axis must be pending")
	}
}

func TestF22DecodeReportRejectsScoresAndInconsistentDocuments(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	good := pl.Derive(inputFor(c, gradient(1024, 1024))).Report
	raw, err := pl.ReportJSON(good)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := pl.DecodeReport(raw); err != nil || got.CandidateState != pl.CandidateLimited {
		t.Fatal(got.CandidateState, err)
	}
	mutations := map[string]func(string) string{
		"a similarity score field": func(s string) string {
			return strings.Replace(s, `"candidate_state"`, `"similarity_score":0.99,"candidate_state"`, 1)
		},
		"a quality score field":           func(s string) string { return strings.Replace(s, `"build"`, `"score":98,"build"`, 1) },
		"a browser supplied passed claim": func(s string) string { return strings.Replace(s, `"build"`, `"passed":true,"build"`, 1) },
		"state flipped to limited by hand": func(s string) string {
			return strings.Replace(s, `"candidate_state":"limited_candidate"`, `"candidate_state":"pending"`, 1)
		},
		"a limit axis forced to PASS": func(s string) string {
			return strings.Replace(s, `"name":"visual_composite_quality","state":"UNKNOWN"`, `"name":"visual_composite_quality","state":"PASS"`, 1)
		},
		"human usefulness claimed": func(s string) string {
			return strings.Replace(s, `"human_usefulness":"NOT_RUN"`, `"human_usefulness":"PASS"`, 1)
		},
		"trailing data":            func(s string) string { return s + "{}" },
		"non canonical whitespace": func(s string) string { return strings.Replace(s, `{"report_version"`, "{ \"report_version\"", 1) },
		"a FAIL axis hidden as PASS": func(s string) string {
			return strings.Replace(s, `"state":"PASS","machine_checked":true,"evidence":"protected=`, `"state":"FAIL","machine_checked":true,"evidence":"protected=`, 1)
		},
	}
	for name, mut := range mutations {
		if _, err := pl.DecodeReport([]byte(mut(string(raw)))); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A failing report cannot be laundered by flipping its FAIL axis to PASS.
	bad := pl.Derive(inputFor(c, gradient(512, 512))).Report
	badRaw, _ := pl.ReportJSON(bad)
	if _, err := pl.DecodeReport(badRaw); err != nil {
		t.Fatal(err)
	}
	laundered := strings.Replace(string(badRaw), `"name":"plate_size","state":"FAIL"`, `"name":"plate_size","state":"PASS"`, 1)
	if laundered == string(badRaw) {
		t.Fatal("test setup: plate_size FAIL not found")
	}
	if _, err := pl.DecodeReport([]byte(laundered)); err == nil {
		t.Error("a FAIL axis rewritten as PASS was accepted")
	}
}
