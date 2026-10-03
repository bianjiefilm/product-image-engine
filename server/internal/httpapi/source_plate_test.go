package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

type plateHTTPFixture struct {
	*sourceHTTPFixture
	eco       *ecoStub
	set       *fs.Set
	c         *fs.LoadedCase
	inputID   string
	reads     atomic.Int64 // calls to the Upload original-read seam
	servedPNG []byte       // what the seam returns; defaults to the frozen original
}

func gradientPNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*60/w), uint8(50 + y*90/h), uint8(110 + (x+y)*40/(w+h)), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func newPlateHTTPFixture(t *testing.T, caseID string) *plateHTTPFixture {
	t.Helper()
	eco := newEcoStub(t)
	sf := newSourceHTTPFixtureWith(t, func(c *config.Config) {
		c.UploadBaseURL, c.UploadToken = eco.srv.URL, "up-tok"
		c.ReceiptKeys = "orders:" + testSecret
		c.BgPlateLockEnabled = true
	})
	attachRegistry(t, sf.fixture, mustRegistry(t, eco.srv.URL+"/internal/v1/handoff/receipts"))
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	var c *fs.LoadedCase
	for _, x := range set.Cases() {
		if x.CaseID == caseID {
			c = x
		}
	}
	f := &plateHTTPFixture{sourceHTTPFixture: sf, eco: eco, set: set, c: c, servedPNG: c.OriginalBytes}
	svc := f.srv.Source
	svc.PlateEnabled, svc.Samples, svc.OutputRecoveryReady = true, set, true
	f.srv.ReadOriginal = func(context.Context, platform.Principal, string, string, string) ([]byte, error) {
		f.reads.Add(1)
		return f.servedPNG, nil
	}
	f.inputID = f.addInput(t, "asset_frozen", c.Original.SHA256)
	return f
}

// addInput attaches a project input whose local photo registry row carries sha.
func (f *plateHTTPFixture) addInput(t *testing.T, asset, sha string) string {
	t.Helper()
	in, err := f.st.AddInput(t.Context(), store.ProjectInput{ProjectID: f.project.ID, TenantID: f.actor.AccountID, PlatformAssetID: asset, SnapshotName: asset, SnapshotSize: 100, SnapshotContentType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.UpsertPhotoAsset(t.Context(), store.PhotoAsset{TenantID: f.actor.AccountID, SHA256: sha, SizeBytes: 100, MediaType: "image/png", WidthPx: 100, HeightPx: 100, Purpose: "product_photo", PlatformAssetID: asset, OriginalName: asset + ".png", CreatedBy: f.actor.UserID}); err != nil {
		t.Fatal(err)
	}
	return in.ID
}

func (f *plateHTTPFixture) createBody(key, input, intent string) string {
	b, _ := json.Marshal(map[string]string{"request_key": key, "mode": si.ModePlateLock, "input_id": input, "background_intent": intent})
	return string(b)
}

func (f *plateHTTPFixture) create(t *testing.T, key string) (int, map[string]any) {
	t.Helper()
	w := f.raw(t, "POST", f.path(""), f.token, f.createBody(key, f.inputID, f.c.Intents[0]))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (f *plateHTTPFixture) plateOf(t *testing.T, view map[string]any) map[string]any {
	t.Helper()
	plate, ok := view["plate"].(map[string]any)
	if !ok {
		t.Fatalf("a plate-lock run view must carry plate: %v", view)
	}
	return plate
}

// finish confirms the quote, lets the fake platform finish and charge the Task
// with `plate` as its output, and drives the run to a derivation through the
// ordinary reconcile route.
func (f *plateHTTPFixture) finish(t *testing.T, key string, plate []byte) (string, map[string]any) {
	t.Helper()
	code, view := f.create(t, key)
	if code != 200 {
		t.Fatal(code, view)
	}
	runID, _ := view["run_id"].(string)
	quote, _ := view["quote"].(map[string]any)
	body, _ := json.Marshal(map[string]string{"quote_id": quote["quote_id"].(string), "quote_fingerprint": quote["quote_fingerprint"].(string)})
	if w := f.raw(t, "POST", f.path("/"+runID+"/confirm"), f.token, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	n := int64(25)
	o := si.Output{AssetID: "asset_plate", ReferenceID: "reference_plate", ProjectID: f.project.ID, AppID: "product-image", PrincipalType: "user", PrincipalID: f.actor.UserID, SHA256: si.SHA256Hex(plate), ContentType: "image/png", Status: "ready", ReferenceState: "active", SizeBytes: int64(len(plate))}
	f.ports.mu.Lock()
	f.ports.payload = string(plate)
	f.ports.task.Status, f.ports.task.Phase, f.ports.task.Output, f.ports.task.HoldID, f.ports.task.ChargeID = "succeeded", "succeeded", &o, "hold_plate", "charge_plate"
	f.ports.bill.Status, f.ports.bill.HoldID, f.ports.bill.ChargeID, f.ports.bill.ChargedMinor = "charged", "hold_plate", "charge_plate", &n
	f.ports.mu.Unlock()
	w := f.raw(t, "POST", f.path("/"+runID+"/reconcile"), f.token, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return runID, out
}

func TestPlateCapabilitiesAreTruthfulAndReadOnly(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	other := f.addInput(t, "asset_customer", si.SHA256Hex([]byte("a customer photo")))
	before := f.ports.mutations()
	read := func() map[string]any {
		w := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var body struct {
			Modes []map[string]any `json:"modes"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		for _, m := range body.Modes {
			if m["mode"] == si.ModePlateLock {
				return m
			}
		}
		t.Fatal("no background_plate_lock entry")
		return nil
	}
	m := read()
	ids, _ := m["supported_input_ids"].([]any)
	if m["ready"] != true || m["reason"] != "" || len(ids) != 1 || ids[0] != f.inputID || m["claim"] != "frozen_synthetic_sample_only" {
		t.Fatalf("a frozen input is supported and an ordinary photo is not: %v (other=%s)", m, other)
	}
	for _, tc := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{"flag off", func() { f.srv.Source.PlateEnabled = false }, "plate_lock_disabled"},
		{"no sample directory", func() { f.srv.Source.PlateEnabled, f.srv.Source.Samples = true, nil }, "sample_set_unconfigured"},
		{"rejected sample set", func() { f.srv.Source.SamplesReason = "sample_set_invalid" }, "sample_set_invalid"},
	} {
		tc.mutate()
		if got := read(); got["ready"] != false || got["disabled"] != true || got["reason"] != tc.want {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
	f.srv.Source.PlateEnabled, f.srv.Source.Samples, f.srv.Source.SamplesReason = true, f.set, ""
	if f.reads.Load() != 0 || f.ports.mutations() != before {
		t.Fatalf("capabilities is a pure local read: reads=%d mutations=%v", f.reads.Load(), f.ports.mutations())
	}
}

func TestPlateCreateRefusesBeforeAnyPaidCall(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	customer := f.addInput(t, "asset_customer", si.SHA256Hex([]byte("a customer photo")))
	good := f.createBody("k1", f.inputID, f.c.Intents[0])
	cases := []struct {
		name, body string
		want       int
		code       string
		reads      int64
	}{
		{"F14 browser sends a mask", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","mask_png_base64":"AAAA"}`, 400, "invalid_request", 0},
		{"F14 browser sends sample_id", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","sample_id":"carton-1024x1024"}`, 400, "invalid_request", 0},
		{"F14 browser claims passed", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","passed":"true"}`, 400, "invalid_request", 0},
		{"F14 browser sends a prompt", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","prompt":"draw"}`, 400, "invalid_request", 0},
		{"F14 browser sends an amount", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","amount_minor":"1"}`, 400, "invalid_request", 0},
		{"missing input_id", `{"request_key":"k","mode":"background_plate_lock","background_intent":"x"}`, 400, "invalid_request", 0},
		{"duplicate key", `{"request_key":"k","request_key":"j","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x"}`, 400, "invalid_request", 0},
		{"control character in the background", f.createBody("k", f.inputID, "a\nb"), 400, "invalid_request", 0},
		{"background over 200 characters", f.createBody("k", f.inputID, strings.Repeat("界", 201)), 400, "invalid_request", 0},
		{"background with surrounding spaces", f.createBody("k", f.inputID, " 深蓝 "), 400, "invalid_request", 0},
		{"F04 an ordinary photo", f.createBody("k", customer, "深蓝背景"), 422, "sample_not_frozen", 0},
		{"another project's input", f.createBody("k", "pin_does_not_exist", "深蓝背景"), 404, "not_found", 0},
		{"F05 registry says frozen but the bytes differ by one pixel", f.createBody("k", f.inputID, "深蓝背景"), 422, "sample_not_frozen", 1},
	}
	f.servedPNG = func() []byte {
		img, _ := png.Decode(bytes.NewReader(f.c.OriginalBytes))
		n := image.NewNRGBA(img.Bounds())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		n.SetNRGBA(0, 0, color.NRGBA{9, 9, 9, 255})
		var b bytes.Buffer
		_ = png.Encode(&b, n)
		return b.Bytes()
	}()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := f.reads.Load()
			w := f.raw(t, "POST", f.path(""), f.token, tc.body)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			if got := f.reads.Load() - before; got != tc.reads {
				t.Fatalf("Upload reads = %d, want %d", got, tc.reads)
			}
			if f.ports.mutations() != [6]int{} {
				t.Fatalf("a refused request reached Billing/Task: %v", f.ports.mutations())
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{"flag off", func() { f.srv.Source.PlateEnabled = false }, "plate_lock_disabled"},
		{"F16 rejected sample set", func() {
			f.srv.Source.PlateEnabled, f.srv.Source.Samples, f.srv.Source.SamplesReason = true, nil, "sample_set_invalid"
		}, "sample_set_invalid"},
	} {
		tc.mutate()
		w := f.raw(t, "POST", f.path(""), f.token, good)
		if w.Code != 503 || !strings.Contains(w.Body.String(), tc.want) || f.ports.mutations() != [6]int{} {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestPlateHappyPathEndToEndOverHTTP(t *testing.T) {
	f := newPlateHTTPFixture(t, "handled_metal-1024x1280")
	plate := gradientPNG(1024, 1024)
	runID, view := f.finish(t, "plate-http", plate)
	if view["mode"] != si.ModePlateLock || view["output"] != nil || view["fidelity"] != "frozen_sample_only" {
		t.Fatalf("the raw plate must never be the advertised output: %v", view)
	}
	pl := f.plateOf(t, view)
	if pl["candidate_state"] != "limited_candidate" || pl["derivation_state"] != "derived" || pl["result_kind"] != "derived_composite" || pl["human_usefulness"] != "NOT_RUN" {
		t.Fatalf("%v", pl)
	}
	if verdicts, _ := pl["verdicts"].(map[string]any); verdicts["fidelity"] != "PASS" || verdicts["generation"] != "PASS" || verdicts["billing"] != "PASS" {
		t.Fatalf("%v", pl["verdicts"])
	}
	if limits, _ := pl["limits"].([]any); len(limits) < 4 {
		t.Fatalf("a limited candidate must state its limits: %v", pl["limits"])
	}
	if raw, _ := json.Marshal(view); strings.Contains(string(raw), "score") || strings.Contains(string(raw), "similarity") {
		t.Fatalf("no score may appear in a plate view: %s", raw)
	}
	// content: the derived composite, not the paid plate.
	w := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Quality-Verdict") != "limited" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(w.Code, w.Header())
	}
	composite := w.Body.Bytes()
	if si.SHA256Hex(composite) != pl["composite_sha256"] || bytes.Equal(composite, plate) {
		t.Fatal("content must be the stored derived composite")
	}
	img, err := png.Decode(bytes.NewReader(composite))
	if err != nil || img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 1280 {
		t.Fatalf("composite must be the case canvas: %v %v", img.Bounds(), err)
	}
	if _, err := bgreplace.SubjectPixelChecks(f.c.OriginalBytes, composite, f.c.MaskBytes); err != nil {
		t.Fatalf("every protected pixel must equal the original: %v", err)
	}
	// select, then export.
	if w := f.raw(t, "POST", f.path("/"+runID+"/select"), f.token, `{}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"selected":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "GET", f.path("/"+runID+"/export"), f.token, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(w.Code, w.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, zf := range zr.File {
		rc, _ := zf.Open()
		files[zf.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	if len(files) != 3 || !bytes.Equal(files["image.png"], composite) {
		t.Fatalf("export is image.png + quality.json + snapshot.json of the same composite: %v", len(files))
	}
	if rep, err := platelock.DecodeReport(files["quality.json"]); err != nil || rep.Facts.CompositeSHA256 != si.SHA256Hex(composite) || rep.Facts.OriginalChargeID != "charge_plate" {
		t.Fatalf("quality.json must be the frozen canonical report bound to this composite: %v", err)
	}
	// One run, one Task submit, one plate download; nothing was charged twice.
	if got := f.ports.mutations(); got[0] != 1 || got[1] != 1 || got[2] != 1 {
		t.Fatalf("usage/quote/submit must each be exactly one: %v", got)
	}
	if f.ports.downloads != 1 {
		t.Fatalf("the paid plate is downloaded once for derivation, never for display: %d", f.ports.downloads)
	}
}

func TestPlateUnusableCandidateIsVisibleButNeverSelectableExportableOrReturnable(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	// The "model" returned the original image as the background: F07.
	runID, view := f.finish(t, "plate-bad", f.c.OriginalBytes)
	pl := f.plateOf(t, view)
	if pl["candidate_state"] != "not_usable_candidate" {
		t.Fatalf("%v", pl)
	}
	failed := false
	for _, a := range pl["axes"].([]any) {
		if am := a.(map[string]any); am["name"] == platelock.AxisBackground && am["state"] == "FAIL" {
			failed = true
		}
	}
	if !failed {
		t.Fatal("background_changed must be FAIL and visible")
	}
	w := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, "")
	if w.Code != 200 || w.Header().Get("X-Quality-Verdict") != "fail" {
		t.Fatal(w.Code, w.Header())
	}
	composite := w.Body.Bytes()
	for _, tc := range []struct{ method, suffix, body string }{{"POST", "/select", `{}`}, {"GET", "/export", ""}, {"POST", "/return", `{}`}} {
		w := f.raw(t, tc.method, f.path("/"+runID+tc.suffix), f.token, tc.body)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"not_usable_candidate"`) {
			t.Fatalf("F13 %s: %d %s", tc.suffix, w.Code, w.Body.String())
		}
	}
	// F19: the failing composite's bytes cannot be pushed in through generic outputs.
	body, _ := json.Marshal(map[string]any{"file_name": "x.png", "content_type": "image/png", "data_b64": b64(composite)})
	w = f.raw(t, "POST", "/api/v1/projects/"+f.project.ID+"/outputs", f.token, string(body))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "source_output_owned") {
		t.Fatalf("F19: %d %s", w.Code, w.Body.String())
	}
	if f.eco.uploads.Load() != 0 {
		t.Fatalf("nothing may be registered upstream: %d", f.eco.uploads.Load())
	}
	if got := f.ports.mutations(); got[2] != 1 {
		t.Fatalf("an unusable result must not trigger regeneration: %v", got)
	}
}

func TestPlateStaysInsideItsOwnAccount(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	runID, _ := f.finish(t, "plate-own", gradientPNG(1024, 1024))
	stranger := f.stub.mint("stranger@example.com", "product-image")
	for _, tc := range []struct{ method, suffix, body string }{
		{"GET", "", ""}, {"GET", "/content", ""}, {"GET", "/export", ""}, {"POST", "/select", `{}`}, {"POST", "/return", `{}`}, {"DELETE", "/output", `{}`},
	} {
		w := f.raw(t, tc.method, f.path("/"+runID+tc.suffix), stranger, tc.body)
		if w.Code != 404 {
			t.Fatalf("F11 %s %s: %d %s", tc.method, tc.suffix, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "carton") || strings.Contains(w.Body.String(), f.c.Original.SHA256) {
			t.Fatalf("a stranger learned something: %s", w.Body.String())
		}
	}
}

func TestF21RetiredModelPlateRouteNeverCallsTheModel(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, func(c *config.Config) {
		c.BgReplaceEnabled = true
		c.BgModelCredential, c.BgModelURL, c.BgModelName = "cred", "https://model.example.invalid/v1", "m"
	})
	f.srv.ImageHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		panic("the retired route reached an image model")
	})}
	f.rearm(t)
	tok := f.stub.mint("source@example.com", "product-image")
	for _, body := range []string{"", `{"mask_png_base64":"AAAA"}`, `not json`} {
		st, m := f.do(t, "POST", "/api/v1/projects/prj_x/background-replacements/job_x/model-plate", tok, json.RawMessage(nil))
		_ = body
		if st != http.StatusGone {
			t.Fatalf("model-plate must be 410, got %d %v", st, m)
		}
		if code, _ := errCodeOf(m); code != "direct_supplier_route_retired" {
			t.Fatalf("%v", m)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("image model calls = %d", calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPlateReturnBindsQualityGatesReceiptAndKeepsVersions(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	tenant := f.actor.AccountID
	doc := handoffDoc("h-plate-1", "orders", "order", tenant, "src-plate", "bind-plate-1", "rev-1", "brief-1")
	w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(doc, "主图")))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	projID := accepted["project"].(map[string]any)["id"].(string)
	// Run the whole plate flow inside the order-bound project.
	f.project.ID = projID
	f.inputID = f.addInput(t, "asset_frozen", f.c.Original.SHA256) // same registered photo, this project
	runID, _ := f.finish(t, "plate-return", gradientPNG(1024, 1024))
	// Not selected yet: refuse.
	if w := f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`); w.Code != 409 || !strings.Contains(w.Body.String(), "not_selected") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.raw(t, "POST", f.path("/"+runID+"/select"), f.token, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var ret struct {
		Output  store.ProjectOutput `json:"output"`
		Quality map[string]any      `json:"quality"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ret)
	if ret.Quality["candidate_state"] != "limited_candidate" || ret.Quality["receipt_includes_limits"] != false || ret.Quality["run_id"] != runID {
		t.Fatalf("%v", ret.Quality)
	}
	content := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, "").Body.Bytes()
	if ret.Output.ResultSHA256 != si.SHA256Hex(content) {
		t.Fatal("the returned output must be exactly the displayed composite")
	}
	// Idempotent: a second return registers nothing new.
	again := f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`)
	if again.Code != 200 || f.eco.uploads.Load() != 1 {
		t.Fatalf("second return: %d uploads=%d", again.Code, f.eco.uploads.Load())
	}
	// The listing carries the quality binding (state, scope, limits, report digest).
	listed := f.raw(t, "GET", "/api/v1/projects/"+projID+"/outputs", f.token, "")
	if !strings.Contains(listed.Body.String(), `"quality_bindings"`) || !strings.Contains(listed.Body.String(), "frozen_synthetic_sample_only") {
		t.Fatalf("%s", listed.Body.String())
	}
	// Receipt: cites the original charge and the source run; delivered once.
	r := f.raw(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+ret.Output.ID+"/receipt", f.token, `{}`)
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	f.eco.mu.Lock()
	ev := f.eco.events[0]
	f.eco.mu.Unlock()
	refs, _ := ev["billing_fact_refs"].([]any)
	if ev["run_id"] != runID || len(refs) != 1 || refs[0] != "charge_plate" {
		t.Fatalf("receipt must cite the source run and the original charge: %v", ev)
	}
	assets, _ := ev["assets"].([]any)
	if a0, _ := assets[0].(map[string]any); a0["sha256"] != ret.Output.ResultSHA256 {
		t.Fatalf("receipt asset must be the returned composite: %v", a0)
	}
}

func TestPlateReturnOfAnOldRequirementVersionIsRefused(t *testing.T) {
	f := newPlateHTTPFixture(t, "glass_bottle-1024x1024")
	tenant := f.actor.AccountID
	w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(handoffDoc("h-plate-v1", "orders", "order", tenant, "src-plate-v", "bind-plate-v1", "rev-1", "brief-1"), "主图")))
	var accepted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	projID := accepted["project"].(map[string]any)["id"].(string)
	f.project.ID = projID
	f.inputID = f.addInput(t, "asset_frozen", f.c.Original.SHA256)
	runID, _ := f.finish(t, "plate-stale", gradientPNG(1024, 1024))
	if w := f.raw(t, "POST", f.path("/"+runID+"/select"), f.token, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`)
	var ret struct {
		Output store.ProjectOutput `json:"output"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ret)
	if w.Code != 201 || ret.Output.ID == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	// A newer requirement arrives before the receipt is sent.
	if w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(handoffDoc("h-plate-v2", "orders", "order", tenant, "src-plate-v", "bind-plate-v2", "rev-2", "brief-2"), "主图"))); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before, hadBefore, _ := f.st.GetOutputQuality(t.Context(), tenant, ret.Output.ID)
	r := f.raw(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+ret.Output.ID+"/receipt", f.token, `{}`)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "stale_requirement_version") || f.eco.eventCount() != 0 {
		t.Fatalf("F12: %d %s events=%d", r.Code, r.Body.String(), f.eco.eventCount())
	}
	after, hasAfter, _ := f.st.GetOutputQuality(t.Context(), tenant, ret.Output.ID)
	if !hadBefore || !hasAfter || before != after {
		t.Fatal("the quality binding must survive unchanged")
	}
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestPlateReceiptRefusesAnUnboundDerivedComposite(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	tenant := f.actor.AccountID
	doc := handoffDoc("h-plate-u", "orders", "order", tenant, "src-plate-u", "bind-plate-u", "rev-1", "brief-1")
	w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(doc, "主图")))
	var accepted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	projID := accepted["project"].(map[string]any)["id"].(string)
	f.project.ID = projID
	f.inputID = f.addInput(t, "asset_frozen", f.c.Original.SHA256)
	runID, _ := f.finish(t, "plate-unbound", gradientPNG(1024, 1024))
	d, ok, err := f.st.GetPlateDerivation(t.Context(), runID)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	// An interrupted return left an output row without its quality binding.
	out, err := f.st.CreateOutput(t.Context(), store.ProjectOutput{TenantScope: tenant, ProjectID: projID, PlatformAssetID: "asset_x", ResultSHA256: d.SHA256, ResultSize: d.SizeBytes, MediaType: "image/png", FileName: "x.png"})
	if err != nil {
		t.Fatal(err)
	}
	r := f.raw(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out.ID+"/receipt", f.token, `{}`)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "not_usable_candidate") || f.eco.eventCount() != 0 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
}

// F20 and the mode closure: an ordinary text run never carries a fidelity claim,
// and modes without real success evidence stay closed whatever else is enabled.
func TestOrdinaryTextRunNeverCarriesAFidelityClaimAndClosedModesStayClosed(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	// Text run in the same fixture, through the ordinary text request body.
	w := f.raw(t, "POST", f.path(""), f.token, `{"request_key":"request-text","mode":"text_generate","prompt":"蓝色纸盒","size":"1024*1024"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var view map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	if _, has := view["plate"]; has || view["fidelity"] != "not_applicable" || view["mode"] != "text_generate" {
		t.Fatalf("a text run must stay a text run: %v", view)
	}
	var runID, _ = view["run_id"].(string)
	got := f.raw(t, "GET", f.path("/"+runID), f.token, "")
	if strings.Contains(got.Body.String(), `"plate"`) || strings.Contains(got.Body.String(), "frozen_sample_only") || strings.Contains(got.Body.String(), "axes") {
		t.Fatalf("text view must not mention plate quality: %s", got.Body.String())
	}
	caps := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
	var body struct {
		Modes []map[string]any `json:"modes"`
	}
	_ = json.Unmarshal(caps.Body.Bytes(), &body)
	closed := 0
	for _, m := range body.Modes {
		if m["mode"] == "reference_edit" {
			closed++
			if m["ready"] != false || m["disabled"] != true || m["reason"] != "formal_contract_unconfigured" {
				t.Fatalf("reference_edit must stay closed: %v", m)
			}
		}
		if m["mode"] == "text_generate" && m["fidelity"] != "not_applicable" {
			t.Fatalf("text_generate must never claim fidelity: %v", m)
		}
	}
	if closed != 1 {
		t.Fatalf("modes = %v", body.Modes)
	}
}

func TestReadyzReportsBuildIdentityAndThePlateGate(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	read := func(t *testing.T, srv *plateHTTPFixture) map[string]any {
		t.Helper()
		w := srv.raw(t, "GET", "/readyz", "", "")
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body
	}
	body := read(t, f)
	build, _ := body["build"].(map[string]any)
	if _, ok := build["vcs_revision"]; !ok {
		t.Fatalf("readyz must carry build identity: %v", body["build"])
	}
	if _, ok := build["vcs_modified"].(bool); !ok {
		t.Fatalf("vcs_modified must be a boolean: %v", build)
	}
	gate, _ := body["gates"].(map[string]any)["plate_lock"].(map[string]any)
	if gate["enabled"] != true || gate["usable"] != true || gate["samples_loaded"] != true || gate["reason"] != "" {
		t.Fatalf("%v", gate)
	}
	f.srv.Source.SamplesReason, f.srv.Source.Samples = "sample_set_invalid", nil
	gate, _ = read(t, f)["gates"].(map[string]any)["plate_lock"].(map[string]any)
	if gate["usable"] != false || gate["reason"] != "sample_set_invalid" || gate["samples_loaded"] != false {
		t.Fatalf("a rejected sample set must show up in readyz without failing it: %v", gate)
	}
	plain := newFixture(t, nil)
	_, ready := plain.do(t, "GET", "/readyz", "", nil)
	pg, _ := ready["gates"].(map[string]any)["plate_lock"].(map[string]any)
	if pg["reason"] != "plate_lock_disabled" || pg["usable"] != false {
		t.Fatalf("without a source runtime the gate reads %v", pg)
	}
}

// Rollback is "turn the flag off", never "downgrade the binary". Off closes new
// work only; history, display, export, selection and deletion keep working.
func TestPlateFlagOffClosesNewWorkButKeepsHistoryAndRecovery(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	code, view := f.create(t, "plate-flag")
	if code != 200 {
		t.Fatal(code, view)
	}
	runID, _ := view["run_id"].(string)
	quote, _ := view["quote"].(map[string]any)
	confirm, _ := json.Marshal(map[string]string{"quote_id": quote["quote_id"].(string), "quote_fingerprint": quote["quote_fingerprint"].(string)})
	f.srv.Source.PlateEnabled = false
	if w := f.raw(t, "POST", f.path("/"+runID+"/confirm"), f.token, string(confirm)); w.Code != 503 {
		t.Fatalf("an unconfirmed quote must not start while the flag is off: %d %s", w.Code, w.Body.String())
	}
	if f.ports.mutations()[2] != 0 {
		t.Fatal("nothing may be submitted")
	}
	f.srv.Source.PlateEnabled = true
	runID, _ = f.finish(t, "plate-flag", gradientPNG(1024, 1024))
	f.srv.Source.PlateEnabled = false
	for _, tc := range []struct{ method, suffix, body string }{{"GET", "", ""}, {"GET", "/content", ""}, {"GET", "/export", ""}, {"POST", "/select", `{}`}, {"POST", "/reconcile", `{}`}} {
		if w := f.raw(t, tc.method, f.path("/"+runID+tc.suffix), f.token, tc.body); w.Code != 200 {
			t.Fatalf("%s %s must keep working with the flag off: %d %s", tc.method, tc.suffix, w.Code, w.Body.String())
		}
	}
	if w := f.raw(t, "POST", f.path(""), f.token, f.createBody("plate-new", f.inputID, f.c.Intents[1])); w.Code != 503 || !strings.Contains(w.Body.String(), "plate_lock_disabled") {
		t.Fatalf("a new request must be closed: %d %s", w.Code, w.Body.String())
	}
	if w := f.raw(t, "DELETE", f.path("/"+runID+"/output"), f.token, `{}`); w.Code != 200 {
		t.Fatalf("deleting an existing result must keep working: %d %s", w.Code, w.Body.String())
	}
	if w := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, ""); w.Code == 200 {
		t.Fatal("a deleted result must no longer be displayed")
	}
}
