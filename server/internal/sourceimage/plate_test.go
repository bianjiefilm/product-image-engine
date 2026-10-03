package sourceimage

import (
	"strings"
	"testing"
)

func legacyIntent() Intent {
	return Intent{Scope: Scope{AppID: "product-image", TenantID: "acct_u", ProjectID: "prj_a", UserID: "usr_u", PrincipalAccountID: "acct_u", PayerAccountID: "acct_u"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝纸盒", PricingVersion: "test-price-v1", Quantity: 1}
}

func frozenPlate() PlateInput {
	h := strings.Repeat("a", 64)
	return PlateInput{CaseID: "carton-1024x1024", SetID: "frozen-sample-set", SetSHA256: h, InputID: "pin_a", OriginalSHA256: strings.Repeat("b", 64), MaskSHA256: strings.Repeat("c", 64), CoverageSHA256: strings.Repeat("d", 64), CanvasW: 1024, CanvasH: 1024, BackgroundIntent: "深蓝色背景", FitID: "fit-cover-center-catmullrom/v1", ComposeID: "compose-mask-lock/v1", ThresholdsVersion: "bg-lock-thresholds/v1"}
}

// This digest was produced by the code BEFORE the plate-lock mode existed. If it
// changes, every persisted text_generate run would fail its fingerprint check.
const legacyFingerprint = "a4233dcbcddd62323656cb34489b0b04e398f111191997f29eecc74073534130"

func TestLegacyTextIntentFingerprintIsByteStable(t *testing.T) {
	fp, err := Fingerprint(legacyIntent())
	if err != nil || fp != legacyFingerprint {
		t.Fatalf("legacy fingerprint changed: %s %v", fp, err)
	}
}

func TestFingerprintModeRules(t *testing.T) {
	plate := legacyIntent()
	plate.Mode, plate.Prompt, plate.PlateDigest = ModePlateLock, "只生成背景", PlateDigest(frozenPlate())
	if fp, err := Fingerprint(plate); err != nil || fp == legacyFingerprint {
		t.Fatalf("a plate run has its own fingerprint: %s %v", fp, err)
	}
	for name, mutate := range map[string]func(*Intent){
		"text mode carrying a plate digest":    func(i *Intent) { i.Mode, i.PlateDigest = ModeTextGenerate, PlateDigest(frozenPlate()) },
		"plate mode without a digest":          func(i *Intent) { i.PlateDigest = "" },
		"plate mode with a short digest":       func(i *Intent) { i.PlateDigest = "abc" },
		"plate mode with an upper-case digest": func(i *Intent) { i.PlateDigest = strings.ToUpper(i.PlateDigest) },
		"unknown mode":                         func(i *Intent) { i.Mode = "reference_edit" },
		"another model":                        func(i *Intent) { i.Model = "other" },
		"another size":                         func(i *Intent) { i.Size = "512*512" },
		"two images":                           func(i *Intent) { i.Quantity = 2 },
	} {
		in := plate
		mutate(&in)
		if _, err := Fingerprint(in); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestPlateInputValidityAndDigestBinding(t *testing.T) {
	p := frozenPlate()
	if !p.Valid() {
		t.Fatal("a complete frozen input must be valid")
	}
	if PlateDigest(p) != PlateDigest(frozenPlate()) {
		t.Fatal("digest must be deterministic")
	}
	other := p
	other.BackgroundIntent = "暖色原木桌面"
	if PlateDigest(other) == PlateDigest(p) {
		t.Fatal("the background words are part of the digest")
	}
	for name, mutate := range map[string]func(*PlateInput){
		"short set digest":      func(x *PlateInput) { x.SetSHA256 = "x" },
		"empty case":            func(x *PlateInput) { x.CaseID = "" },
		"zero canvas":           func(x *PlateInput) { x.CanvasW = 0 },
		"huge canvas":           func(x *PlateInput) { x.CanvasH = 5000 },
		"control character":     func(x *PlateInput) { x.BackgroundIntent = "a\nb" },
		"intent over 200 runes": func(x *PlateInput) { x.BackgroundIntent = strings.Repeat("界", 201) },
	} {
		x := p
		mutate(&x)
		if x.Valid() {
			t.Errorf("%s accepted", name)
		}
	}
	if !ValidIntentText(strings.Repeat("界", 200)) || ValidIntentText("") {
		t.Fatal("ValidIntentText bounds")
	}
}

func TestPlateQualityReportIsAPendingMarkerNotAVerdict(t *testing.T) {
	r := Run{Owner: Owner, Intent: legacyIntent()}
	r.Intent.Mode, r.Intent.Prompt, r.Intent.PlateDigest = ModePlateLock, "只生成背景", PlateDigest(frozenPlate())
	raw, err := QualityReport(r)
	if err != nil || string(raw) != `{"report_version":"product-source-quality/v2","state":"derivation_pending"}` {
		t.Fatalf("%s %v", raw, err)
	}
	for _, banned := range []string{"pass", "limited_candidate", "fidelity"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Fatalf("the run snapshot must not decide quality: %s", raw)
		}
	}
}

func TestValidatePlateDerivation(t *testing.T) {
	png := []byte("\x89PNG-x")
	report := `{"report_version":"product-source-quality/v2"}`
	good := PlateDerivation{RunID: "sir_a", State: DerivationDerived, PNG: png, SHA256: SHA256Hex(png), SizeBytes: int64(len(png)), Width: 10, Height: 10, ReportJSON: report, ReportSHA256: SHA256Hex([]byte(report)), CandidateState: CandidateLimited}
	if err := ValidatePlateDerivation(good); err != nil {
		t.Fatal(err)
	}
	noComposite := good
	noComposite.PNG, noComposite.SHA256, noComposite.SizeBytes, noComposite.CandidateState = nil, "", 0, CandidateNotUsable
	if err := ValidatePlateDerivation(noComposite); err != nil {
		t.Fatalf("an uncomposable plate is a legitimate failing record: %v", err)
	}
	blocked := PlateDerivation{RunID: "sir_a", State: DerivationBlocked, BlockedReason: BlockedSampleSetChanged, CandidateState: CandidateBlocked}
	if err := ValidatePlateDerivation(blocked); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*PlateDerivation){
		"digest mismatch":           func(d *PlateDerivation) { d.SHA256 = strings.Repeat("0", 64) },
		"size mismatch":             func(d *PlateDerivation) { d.SizeBytes++ },
		"report digest mismatch":    func(d *PlateDerivation) { d.ReportSHA256 = strings.Repeat("0", 64) },
		"unknown candidate":         func(d *PlateDerivation) { d.CandidateState = "great" },
		"blocked reason on derived": func(d *PlateDerivation) { d.BlockedReason = "x" },
		"limited without bytes":     func(d *PlateDerivation) { d.PNG, d.SHA256, d.SizeBytes = nil, "", 0 },
		"unknown state":             func(d *PlateDerivation) { d.State = "done" },
	} {
		d := good
		mutate(&d)
		if ValidatePlateDerivation(d) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	bad := blocked
	bad.PNG = png
	if ValidatePlateDerivation(bad) == nil {
		t.Error("a blocked record must carry no bytes")
	}
}
