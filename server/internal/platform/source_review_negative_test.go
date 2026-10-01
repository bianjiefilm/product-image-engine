package platform

import (
	"encoding/json"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This is the public a2 settlement envelope, including the hold that remains
// present after ReleaseUsage updates its authoritative status to released.
func independentReleasedWire(r si.Run) []byte {
	var env map[string]any
	json.Unmarshal(sourceChargedBillJSON(r), &env)
	facts := env["facts"].(map[string]any)
	facts["usage"].(map[string]any)["status"] = "released"
	hold := facts["hold"].(map[string]any)
	hold["status"] = "released"
	hold["spent_minor"] = 0
	facts["charge"] = nil
	facts["release"] = map[string]any{"release_id": "release-a", "usage_id": "usage-a", "hold_id": "hold-a", "status": "released", "idempotency_key": "release-key", "reason": "canceled", "resolution": "not_dispatched", "evidence_ref": "task-a", "amount_minor": 25, "released_at_unix": 2}
	raw, _ := json.Marshal(env)
	return raw
}
func TestHUI2232IndependentAcceptActualReleasedSettlement(t *testing.T) {
	r := sourceWireRun()
	f, e := decodeSourceBill(independentReleasedWire(r), r)
	if e != nil || f.Status != "released" || f.ReleaseID != "release-a" {
		t.Fatalf("actual published released settlement rejected: fact=%+v err=%v", f, e)
	}
}

func TestHUI2232IndependentRedirectCannotResendCredentialedSubmit(t *testing.T) {
	r := sourceWireRun()
	redirected := 0
	leaked := false
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		redirected++
		leaked = q.Method == "POST" && q.Header.Get("X-PilotSeaView-Internal-Token") == "task-test"
		w.Write(sourceTaskJSON(r))
	}))
	defer dst.Close()
	initial := 0
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		initial++
		http.Redirect(w, q, dst.URL+"/different-task-service", http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	c, e := NewSourceClients(sourceWireConfig(src.URL), src.Client())
	if e != nil {
		t.Fatal(e)
	}
	e = c.Tasks.Submit(t.Context(), r)
	if !errors.Is(e, si.ErrUnavailable) || initial != 1 || redirected != 0 || leaked {
		t.Fatalf("redirected original credentialed POST: initial=%d destination=%d credentialLeaked=%v err=%v", initial, redirected, leaked, e)
	}
}
func TestHUI2232IndependentRejectNonterminalReleaseFact(t *testing.T) {
	r := sourceWireRun()
	var env map[string]any
	json.Unmarshal(independentReleasedWire(r), &env)
	facts := env["facts"].(map[string]any)
	facts["usage"].(map[string]any)["status"] = "held"
	facts["hold"].(map[string]any)["status"] = "open"
	facts["release"].(map[string]any)["status"] = "open"
	raw, _ := json.Marshal(env)
	f, e := decodeSourceBill(raw, r)
	if !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("nonterminal release accepted: fact=%+v err=%v", f, e)
	}
}
func TestHUI2232IndependentRejectInconsistentRefundAmount(t *testing.T) {
	r := sourceWireRun()
	var env map[string]any
	json.Unmarshal(sourceChargedBillJSON(r), &env)
	facts := env["facts"].(map[string]any)
	facts["usage"].(map[string]any)["status"] = "refunded"
	facts["hold"].(map[string]any)["status"] = "refunded"
	facts["charge"].(map[string]any)["status"] = "refunded"
	facts["refund"] = map[string]any{"refund_id": "refund-a", "charge_id": "charge-a", "idempotency_key": "refund-key", "reason": "fixture", "promo_minor": 0, "allowance_minor": 0, "cash_minor": 0, "created_at_unix": 3}
	raw, _ := json.Marshal(env)
	f, e := decodeSourceBill(raw, r)
	if !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("zero-source refund of allowance25 accepted: fact=%+v err=%v", f, e)
	}
}
func TestSourceWireRedirectGuardAllClientsPreservesCaller(t *testing.T) {
	r := sourceWireRun()
	destinationCalls := 0
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { destinationCalls++; w.WriteHeader(200) }))
	defer dst.Close()
	original := 0
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		original++
		http.Redirect(w, q, dst.URL, http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	caller := src.Client()
	caller.Timeout = 17 * time.Second
	callerRedirects := 0
	caller.CheckRedirect = func(q *http.Request, via []*http.Request) error { callerRedirects++; return nil }
	transport := caller.Transport
	c, e := NewSourceClients(sourceWireConfig(src.URL), caller)
	if e != nil {
		t.Fatal(e)
	}
	c.Bill.Lookup(t.Context(), r)
	c.Tasks.Submit(t.Context(), r)
	c.Context.Resolve(t.Context(), si.ContextQuery{AppID: "product-image", UserID: "usr-a", ReadOnly: true})
	out := si.Output{AssetID: "asset-a", ReferenceID: "ref-a", ProjectID: "project-a", AppID: "product-image", PrincipalType: "user", PrincipalID: "usr-a", SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 1024}
	c.Assets.Reference(t.Context(), si.Outbox{Scope: r.Intent.Scope, Output: out})
	if original != 4 || destinationCalls != 0 || callerRedirects != 0 || caller.Timeout != 17*time.Second || caller.Transport != transport || c.Tasks.c.HTTP == caller || c.Tasks.c.HTTP.Transport != transport || c.Tasks.c.HTTP.Timeout != caller.Timeout {
		t.Fatal("redirect/caller mutation", original, destinationCalls, callerRedirects)
	}
	resp, e := caller.Get(src.URL)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if callerRedirects != 1 || destinationCalls != 1 {
		t.Fatal("caller was changed")
	}
}
func TestSourceDecodeSuccessCarriesOriginalFinancialIDs(t *testing.T) {
	r := sourceWireRun()
	var m map[string]any
	json.Unmarshal(sourceTaskJSON(r), &m)
	result, _ := json.Marshal(map[string]any{"asset_id": "asset-a", "reference_id": "ref-a", "project_id": "project-a", "content_type": "image/png", "sha256": strings.Repeat("a", 64), "size_bytes": 1024})
	m["result_json"] = string(result)
	m["output_ready"] = true
	m["status"] = "succeeded"
	m["phase"] = "succeeded"
	raw, _ := json.Marshal(m)
	if _, e := decodeSourceTask(raw, r); !errors.Is(e, si.ErrInvariant) {
		t.Fatal("success without original charge accepted", e)
	}
	m["hold_id"] = "hold-a"
	m["charge_id"] = "charge-a"
	raw, _ = json.Marshal(m)
	got, e := decodeSourceTask(raw, r)
	if e != nil || got.HoldID != "hold-a" || got.ChargeID != "charge-a" || got.ReleaseID != "" {
		t.Fatal(got, e)
	}
}
func TestSourceDecodeReleasedAndRefundedConservation(t *testing.T) {
	r := sourceWireRun()
	for _, resolution := range []string{"not_dispatched", "provider_failed", "provider_canceled"} {
		var env map[string]any
		json.Unmarshal(independentReleasedWire(r), &env)
		facts := env["facts"].(map[string]any)
		facts["release"].(map[string]any)["resolution"] = resolution
		raw, _ := json.Marshal(env)
		f, e := decodeSourceBill(raw, r)
		if e != nil || f.ReleaseID != "release-a" {
			t.Fatal(resolution, f, e)
		}
	}
	var env map[string]any
	json.Unmarshal(sourceChargedBillJSON(r), &env)
	facts := env["facts"].(map[string]any)
	facts["usage"].(map[string]any)["status"] = "refunded"
	facts["hold"].(map[string]any)["status"] = "refunded"
	facts["charge"].(map[string]any)["status"] = "refunded"
	refund := map[string]any{"refund_id": "refund-a", "charge_id": "charge-a", "idempotency_key": "refund-key", "reason": "fixture", "promo_minor": 0, "allowance_minor": 25, "cash_minor": 0, "created_at_unix": 3}
	facts["refund"] = refund
	raw, _ := json.Marshal(env)
	f, e := decodeSourceBill(raw, r)
	if e != nil || f.RefundID != "refund-a" || f.ChargedMinor == nil || *f.ChargedMinor != 25 {
		t.Fatal(f, e)
	}
	refund["allowance_minor"] = 0
	refund["cash_minor"] = 25
	raw, _ = json.Marshal(env)
	if _, e := decodeSourceBill(raw, r); !errors.Is(e, si.ErrInvariant) {
		t.Fatal("refund changed source despite conserved total", e)
	}
}
