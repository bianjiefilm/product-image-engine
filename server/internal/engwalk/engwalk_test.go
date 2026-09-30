package engwalk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
)

func TestContractStoresServerPNG(t *testing.T) {
	quietLiveEnv(t)
	origin := encodeContractPNG(t, true)
	srv, calls := pngServer(t, http.StatusOK, origin)
	otherCalls := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherCalls++
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	t.Cleanup(other.Close)

	gate := NewGate()
	res, err := gate.Submit(context.Background(), Submit{
		Fingerprint: "fp-intact",
		Mode:        "text_image",
		Endpoint:    srv.URL,
		Similarity:  0.99,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("provider calls = %d", *calls)
	}
	sum := sha256.Sum256(origin)
	want := hex.EncodeToString(sum[:])
	if res.SHA256 != want || res.ByteLen != len(origin) || !bytes.Equal(res.Asset, origin) {
		t.Fatalf("stored bytes are not the server bytes sha=%s len=%d", res.SHA256, res.ByteLen)
	}
	storedSum := sha256.Sum256(res.Asset)
	if hex.EncodeToString(storedSum[:]) != want {
		t.Fatal("hash of stored bytes differs from the server")
	}
	if _, err := png.Decode(bytes.NewReader(res.Asset)); err != nil {
		t.Fatalf("stored bytes do not decode: %v", err)
	}
	if res.Export["sha256"] != want || res.Export["byte_len"] != len(origin) || res.Export["openable"] != true {
		t.Fatalf("export = %#v", res.Export)
	}
	if res.ProviderCalls != 1 || !res.Generation || res.Outcome != OutcomeStored {
		t.Fatalf("generation outcome = %+v calls=%d gen=%v", res.Outcome, res.ProviderCalls, res.Generation)
	}
	if !res.ContractPassed {
		t.Fatal("contract should pass when the png is stored and the broken logo is refused")
	}
	if res.Status != "真实出图未完成" || res.LivePassed || res.ProductionGenerationPassed || res.BillingPassed {
		t.Fatalf("commercial flags status=%s live=%v prod=%v bill=%v", res.Status, res.LivePassed, res.ProductionGenerationPassed, res.BillingPassed)
	}
	if res.LivePassed {
		t.Fatal("LivePassed is true for a 127.0.0.1 host")
	}
	if res.Live.ErrorClass != "配置缺失" || res.Live.Passed {
		t.Fatalf("live = %+v", res.Live)
	}
	assertAxesMatchPixels(t, res.Asset, res.IntactAxes)
	assertChecksFollowAxes(t, res.IntactAxes, res.Intact.Checks)
	if res.Intact.ExactProduct || res.Intact.Deliverable {
		t.Fatal("contract image became a verified receipt")
	}
	if res.BrokenAxes.Logo != fidelity.ResultFail ||
		res.BrokenAxes.PackagingText != res.IntactAxes.PackagingText ||
		res.BrokenAxes.Spec != res.IntactAxes.Spec ||
		res.BrokenAxes.Structure != res.IntactAxes.Structure {
		t.Fatalf("broken axes = %+v intact = %+v", res.BrokenAxes, res.IntactAxes)
	}
	if pixelAt(t, res.Asset, 0, 0) != (color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}) {
		t.Fatal("derived broken case rewrote the stored marker")
	}
	if res.Similarity != 0.99 {
		t.Fatalf("similarity = %v", res.Similarity)
	}
	if checkResult(res.Broken.Checks, fidelity.CheckLogoText) != fidelity.ResultFail ||
		res.Broken.Verdict == fidelity.VerdictPass || res.Broken.ExactProduct || res.Broken.Deliverable {
		t.Fatal("similarity flipped a logo failure into a pass")
	}
	blocked, code, _ := fidelity.BlocksVerifiedReceipt([]fidelity.Report{res.Broken})
	if !blocked || code != "fidelity_failed" {
		t.Fatalf("broken logo receipt blocked=%v code=%s", blocked, code)
	}
	assertFixtureCharge(t, res, "fp-intact")
	assertLayerNames(t, res.Layers)
	for _, layer := range res.Layers {
		if !layer.Passed {
			t.Fatalf("layer %s did not pass", layer.Name)
		}
	}

	res.Asset[0] ^= 0xff
	again, err := gate.Submit(context.Background(), Submit{
		Fingerprint: "fp-intact",
		Mode:        "text_image",
		Endpoint:    other.URL,
		Similarity:  0.1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if otherCalls != 0 || *calls != 1 {
		t.Fatalf("repeat generated again first=%d other=%d", *calls, otherCalls)
	}
	if again.ChargeCount != 1 || len(again.Charges) != 1 || again.SHA256 != want || !bytes.Equal(again.Asset, origin) {
		t.Fatalf("replay changed the stored asset or charge count=%d", again.ChargeCount)
	}
	if again.ProviderCalls != 1 {
		t.Fatalf("replay provider calls = %d", again.ProviderCalls)
	}
}

func TestMissingMarkerIsLogoFailure(t *testing.T) {
	quietLiveEnv(t)
	origin := encodeContractPNG(t, false)
	srv, calls := pngServer(t, http.StatusOK, origin)
	res, err := NewGate().Submit(context.Background(), Submit{
		Fingerprint: "fp-broken",
		Mode:        "text_image",
		Endpoint:    srv.URL,
		Similarity:  0.99,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || !bytes.Equal(res.Asset, origin) {
		t.Fatalf("calls=%d stored=%d", *calls, len(res.Asset))
	}
	if pixelAt(t, res.Asset, 0, 0) == (color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}) {
		t.Fatal("fixture unexpectedly still has the marker")
	}
	if res.IntactAxes.Logo != fidelity.ResultFail || checkResult(res.Intact.Checks, fidelity.CheckLogoText) != fidelity.ResultFail {
		t.Fatal("similarity flipped a logo failure into a pass")
	}
	if res.Intact.Verdict == fidelity.VerdictPass || res.Intact.ExactProduct || res.Broken.Verdict == fidelity.VerdictPass || res.Broken.ExactProduct {
		t.Fatal("similarity flipped a logo failure into a pass")
	}
	blocked, _, _ := fidelity.BlocksVerifiedReceipt([]fidelity.Report{res.Broken})
	if !blocked || !res.ContractPassed || len(res.Asset) == 0 {
		t.Fatalf("stored broken image blocked=%v contract=%v", blocked, res.ContractPassed)
	}
}

func TestFailedOrUnknownDoesNotStoreOrCharge(t *testing.T) {
	quietLiveEnv(t)
	cases := []struct {
		name    string
		status  int
		body    []byte
		outcome string
		class   string
	}{
		{name: "503", status: http.StatusServiceUnavailable, body: []byte("down"), outcome: OutcomeFailed, class: "http_503"},
		{name: "empty", status: http.StatusOK, body: nil, outcome: OutcomeFailed, class: "empty_body"},
		{name: "unknown", status: http.StatusOK, body: []byte("not-a-png"), outcome: OutcomeUnknown, class: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := pngServer(t, tc.status, tc.body)
			gate := NewGate()
			first, err := gate.Submit(context.Background(), Submit{
				Fingerprint: "fp-" + tc.name,
				Mode:        "text_image",
				Endpoint:    srv.URL,
				Similarity:  0.99,
			})
			if err != nil {
				t.Fatal(err)
			}
			second, err := gate.Submit(context.Background(), Submit{
				Fingerprint: "fp-" + tc.name,
				Mode:        "text_image",
				Endpoint:    srv.URL,
			})
			if err != nil {
				t.Fatal(err)
			}
			if *calls != 1 {
				t.Fatalf("failed submit generated twice: %d", *calls)
			}
			for _, res := range []Result{first, second} {
				if len(res.Asset) == 0 && res.ContractPassed {
					t.Fatal("ContractPassed is true when the stored bytes are empty")
				}
				if len(res.Asset) != 0 || res.Generation || res.ContractPassed || res.ChargeCount != 0 || len(res.Charges) != 0 {
					t.Fatalf("stored a failure: asset=%d gen=%v contract=%v charges=%d", len(res.Asset), res.Generation, res.ContractPassed, res.ChargeCount)
				}
				if res.Outcome != tc.outcome || res.FailureClass != tc.class || res.Outcome == OutcomeStored {
					t.Fatalf("outcome=%s class=%s", res.Outcome, res.FailureClass)
				}
				if res.Status != "真实出图未完成" || res.LivePassed || res.ProductionGenerationPassed || res.BillingPassed {
					t.Fatalf("status=%s live=%v", res.Status, res.LivePassed)
				}
				if tc.outcome == OutcomeUnknown && (res.Generation || res.ContractPassed || res.Outcome == OutcomeStored) {
					t.Fatal("unknown became success")
				}
				assertLayerNames(t, res.Layers)
				for _, layer := range res.Layers {
					want := layer.Name == "charge"
					if layer.Passed != want {
						t.Fatalf("layer %s passed=%v", layer.Name, layer.Passed)
					}
				}
			}
		})
	}
}

func TestUnsupportedModeDoesNotCall(t *testing.T) {
	quietLiveEnv(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	res, err := NewGate().Submit(context.Background(), Submit{
		Fingerprint: "fp-mode",
		Mode:        "nope",
		Endpoint:    srv.URL,
		Similarity:  0.99,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || res.Generation || res.ContractPassed || res.Outcome != OutcomeUnknown || res.ChargeCount != 0 || len(res.Asset) != 0 {
		t.Fatalf("unsupported mode still ran: calls=%d %+v", calls, res.Outcome)
	}
}

func TestIncompleteSubmitDoesNotStore(t *testing.T) {
	_, err := NewGate().Submit(context.Background(), Submit{})
	if err == nil || err.Error() != "提交不完整" || !errors.Is(err, ErrSubmitIncomplete) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoopbackResponseIsNotLivePass(t *testing.T) {
	origin := encodeContractPNG(t, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write(origin)
	}))
	t.Cleanup(srv.Close)
	fact := performLive(context.Background(), srv.URL)
	sum := sha256.Sum256(origin)
	want := hex.EncodeToString(sum[:])
	if fact.Passed {
		t.Fatal("LivePassed is true for a 127.0.0.1 host")
	}
	if fact.StatusCode != http.StatusOK || fact.ByteLen != len(origin) || fact.SHA256 != want {
		t.Fatalf("live response was not recorded: %+v", fact)
	}
	for _, host := range []string{"127.0.0.1", "127.0.0.1:9"} {
		judged := judgeLive(host, http.StatusOK, origin)
		if judged.Passed {
			t.Fatal("LivePassed is true for a 127.0.0.1 host")
		}
	}
}

func TestCredentialWithoutEndpointDoesNotDial(t *testing.T) {
	const secret = "cred-do-not-print"
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL", secret)
	t.Setenv("PRODUCT_BG_MODEL_CREDENTIAL", "")
	t.Setenv("PRODUCT_LIGHT_MODEL_CREDENTIAL", "")
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_URL", "")
	t.Setenv("PRODUCT_BG_MODEL_URL", "")
	t.Setenv("PRODUCT_LIGHT_MODEL_URL", "")
	tripped := false
	prev := liveClient.Transport
	liveClient.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		tripped = true
		return nil, errors.New("should not dial")
	})
	t.Cleanup(func() { liveClient.Transport = prev })
	fact := liveAttempt(config.Load())
	if tripped {
		t.Fatal("dialed without a model URL")
	}
	if fact.Passed || fact.StatusCode != 0 || fact.ByteLen != 0 || fact.SHA256 != "" || fact.ErrorClass != "端点缺失" {
		t.Fatalf("live fact status=%d len=%d class=%s passed=%v", fact.StatusCode, fact.ByteLen, fact.ErrorClass, fact.Passed)
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || strings.Contains(fact.ErrorClass, secret) {
		t.Fatal("secret leaked")
	}
}

func quietLiveEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL", "")
	t.Setenv("PRODUCT_BG_MODEL_CREDENTIAL", "")
	t.Setenv("PRODUCT_LIGHT_MODEL_CREDENTIAL", "")
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_URL", "")
	t.Setenv("PRODUCT_BG_MODEL_URL", "")
	t.Setenv("PRODUCT_LIGHT_MODEL_URL", "")
}

func TestURLWithoutCredentialDoesNotDial(t *testing.T) {
	origin := encodeContractPNG(t, true)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write(origin)
	}))
	t.Cleanup(srv.Close)
	quietLiveEnv(t)
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_URL", srv.URL)
	fact := liveAttempt(config.Load())
	if calls != 0 {
		t.Fatalf("dialed with a URL and no credential: %d", calls)
	}
	if fact.Passed || fact.ErrorClass != "配置缺失" || fact.SHA256 != "" {
		t.Fatalf("live = %+v", fact)
	}
}

func TestPairedURLDialsLoopbackPNG(t *testing.T) {
	const secret = "cred-do-not-print"
	origin := encodeContractPNG(t, true)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		calls++
		_, _ = w.Write(origin)
	}))
	t.Cleanup(srv.Close)
	quietLiveEnv(t)
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL", secret)
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_URL", srv.URL)
	fact := liveAttempt(config.Load())
	sum := sha256.Sum256(origin)
	want := hex.EncodeToString(sum[:])
	if calls != 1 {
		t.Fatalf("provider calls = %d", calls)
	}
	if fact.Passed || fact.ErrorClass != "loopback" || fact.StatusCode != http.StatusOK || fact.ByteLen != len(origin) || fact.SHA256 != want {
		t.Fatalf("live = %+v", fact)
	}
	if commercialStatus(fact.Passed) != "真实出图未完成" {
		t.Fatal("loopback became a live image")
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("secret leaked")
	}
}

func TestUnpairedURLIsNotDialed(t *testing.T) {
	const secret = "cred-do-not-print"
	origin := encodeContractPNG(t, true)
	textCalls := 0
	textSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		textCalls++
		_, _ = w.Write(origin)
	}))
	t.Cleanup(textSrv.Close)
	bgCalls := 0
	bgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bgCalls++
		_, _ = w.Write(origin)
	}))
	t.Cleanup(bgSrv.Close)
	quietLiveEnv(t)
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL", secret)
	t.Setenv("PRODUCT_BG_MODEL_CREDENTIAL", secret)
	t.Setenv("PRODUCT_BG_MODEL_URL", bgSrv.URL)
	t.Setenv("PRODUCT_TEXT_IMAGE_MODEL_URL", "")
	fact := liveAttempt(config.Load())
	if textCalls != 0 || bgCalls != 1 {
		t.Fatalf("text calls=%d bg calls=%d", textCalls, bgCalls)
	}
	if fact.Passed || fact.ErrorClass != "loopback" || fact.SHA256 == "" {
		t.Fatalf("live = %+v", fact)
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("secret leaked")
	}
}

func encodeContractPNG(t *testing.T, marker bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	if marker {
		img.SetNRGBA(0, 0, color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF})
	}
	img.SetNRGBA(1, 0, color.NRGBA{R: 0x44, G: 0x55, B: 0x66, A: 0xFF})
	img.SetNRGBA(2, 0, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xFF})
	img.SetNRGBA(3, 0, color.NRGBA{R: 0x01, G: 0x02, B: 0x03, A: 0xFF})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngServer(t *testing.T, status int, body []byte) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var got struct {
			Mode        string `json:"mode"`
			Fingerprint string `json:"fingerprint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		if got.Mode != "text_image" || got.Fingerprint == "" {
			http.Error(w, "task", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(status)
		if len(body) > 0 {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func assertAxesMatchPixels(t *testing.T, raw []byte, axes Axes) {
	t.Helper()
	want := Axes{
		Logo:          axisFromPixel(pixelAt(t, raw, 0, 0), color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}),
		PackagingText: axisFromPixel(pixelAt(t, raw, 1, 0), color.NRGBA{R: 0x44, G: 0x55, B: 0x66, A: 0xFF}),
		Spec:          axisFromPixel(pixelAt(t, raw, 2, 0), color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xFF}),
		Structure:     axisFromPixel(pixelAt(t, raw, 3, 0), color.NRGBA{R: 0x01, G: 0x02, B: 0x03, A: 0xFF}),
	}
	if axes != want {
		t.Fatalf("axes %+v, pixels say %+v", axes, want)
	}
}

func axisFromPixel(got, want color.NRGBA) string {
	if got == want {
		return fidelity.ResultPass
	}
	return fidelity.ResultFail
}

func assertChecksFollowAxes(t *testing.T, axes Axes, checks []fidelity.Check) {
	t.Helper()
	logo := fidelity.ResultFail
	if axes.Logo == fidelity.ResultPass && axes.PackagingText == fidelity.ResultPass {
		logo = fidelity.ResultPass
	}
	if checkResult(checks, fidelity.CheckLogoText) != logo {
		t.Fatalf("logo_text = %s", checkResult(checks, fidelity.CheckLogoText))
	}
	if checkResult(checks, fidelity.CheckShape) != axes.Structure {
		t.Fatalf("shape = %s", checkResult(checks, fidelity.CheckShape))
	}
	if checkResult(checks, fidelity.CheckDeclaredColor) != axes.Spec {
		t.Fatalf("declared_color = %s", checkResult(checks, fidelity.CheckDeclaredColor))
	}
}

func assertFixtureCharge(t *testing.T, res Result, fingerprint string) {
	t.Helper()
	if res.ChargeCount != 1 || len(res.Charges) != 1 {
		t.Fatalf("charges = %d lines=%d", res.ChargeCount, len(res.Charges))
	}
	line := res.Charges[0]
	if line.Fingerprint != fingerprint || line.SupplierCost.Label != "fixture" || line.SupplierCost.Amount != "1.00" {
		t.Fatalf("supplier cost = %+v", line.SupplierCost)
	}
	if line.UserPrice.Label != "user_price" || line.UserPrice.Amount == line.SupplierCost.Amount || line.UserPrice.Label == "fixture" {
		t.Fatalf("user price collided with fixture: %+v", line.UserPrice)
	}
	if line.Subsidy.Label != "subsidy" || line.Subsidy.Amount == line.SupplierCost.Amount || line.Subsidy.Label == "fixture" {
		t.Fatalf("subsidy collided with fixture: %+v", line.Subsidy)
	}
}

func assertLayerNames(t *testing.T, layers []Layer) {
	t.Helper()
	want := []string{"task", "asset", "hash", "export", "fidelity", "charge"}
	if len(layers) != len(want) {
		t.Fatalf("layers = %#v", layers)
	}
	for i := range want {
		if layers[i].Name != want[i] {
			t.Fatalf("layer %d = %s", i, layers[i].Name)
		}
	}
}

func pixelAt(t *testing.T, raw []byte, x, y int) color.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	if !ok {
		t.Fatal("color convert")
	}
	return c
}

func checkResult(checks []fidelity.Check, name string) string {
	for _, c := range checks {
		if c.Name == name {
			return c.Result
		}
	}
	return ""
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
