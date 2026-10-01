package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// This exercises only the existing generic registration helper, not new routes.
func sourceGenericGuardRun(t *testing.T, st *store.Store, data []byte) si.Run {
	t.Helper()
	ctx := t.Context()
	scope := si.Scope{AppID: "product-image", UserID: "usr-guard", PrincipalAccountID: "acct-guard", PayerAccountID: "acct-guard", TenantID: "acct-guard"}
	p, e := st.CreateProject(ctx, store.Project{TenantID: scope.TenantID, CreatedBy: scope.UserID, Name: "source guard", SourceType: "standalone", WidthPx: 1024, HeightPx: 1024})
	if e != nil {
		t.Fatal(e)
	}
	scope.ProjectID = p.ID
	r, _, e := st.CreateSourceRun(ctx, si.Intent{Scope: scope, RequestKey: "guard-request", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "box", PricingVersion: "guard-price", Quantity: 1})
	if e != nil {
		t.Fatal(e)
	}
	claim := func() {
		t.Helper()
		var ok bool
		r, ok, e = st.ClaimSourceRun(ctx, scope, r.ID, time.Now().Unix(), 30)
		if e != nil || !ok {
			t.Fatal(e, ok)
		}
	}
	claim()
	b := si.BillFact{Scope: scope, UsageID: "usage-guard", UsageKey: r.UsageKey, Capability: r.Intent.Capability, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Status: "ingested", Quantity: 1}
	r, e = st.SaveSourceBillObservation(ctx, r, b, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	q := si.Quote{UsageID: b.UsageID, UsageKey: r.UsageKey, QuoteID: "quote-guard", PricingVersion: b.PricingVersion, BusinessRef: b.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: time.Now().Unix(), ExpiresAtUnix: time.Now().Unix() + 300}
	r, e = st.SaveSourceQuoteUnderLease(ctx, r, q, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	claim()
	r, e = st.ConfirmSourceRunForProject(ctx, r, si.QuoteHash(q), time.Now().Unix(), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	claim()
	r, e = st.MarkSourceSubmitAttempt(ctx, r, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	o := si.Output{AssetID: "asset-source-guard", ReferenceID: "ref-source-guard", ProjectID: scope.ProjectID, AppID: scope.AppID, PrincipalType: "user", PrincipalID: scope.UserID, SHA256: hex.EncodeToString(sum[:]), ContentType: "image/png", SizeBytes: int64(len(data))}
	fact := si.TaskFact{ID: "task-guard", IdempotencyKey: r.TaskKey, Scope: scope, Quote: q, Provider: r.Intent.Provider, Capability: r.Intent.Capability, HoldID: "hold-guard", ChargeID: "charge-guard", Status: "succeeded", Phase: "succeeded", Output: &o}
	next := r
	next.Task = &fact
	next.TaskID = fact.ID
	next.Phase = "asset_pending"
	r, e = st.SaveSourceObservation(ctx, next, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestSourceOutputGenericRegistrationCannotPromoteVerifiedSource(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset_id": "legacy-asset"})
	}))
	defer up.Close()
	f := newFixture(t, nil)
	f.srv.Cfg.UploadBaseURL = up.URL
	f.srv.Uploads.BaseURL = up.URL
	data := pngBytes(t, 1024, 1024, 7)
	r := sourceGenericGuardRun(t, f.st, data)
	_, _, e := f.srv.registerOutputBytes(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, "image.png", "image/png", data, nil)
	if e != nil || calls != 1 {
		t.Fatal("unverified metadata blocked old registration", e, calls)
	}
	b := *r.Bill
	b.Quote = r.Quote
	b.Status = "charged"
	b.HoldID = r.Task.HoldID
	b.ChargeID = r.Task.ChargeID
	n := int64(25)
	b.ChargedMinor = &n
	r, err := f.st.SaveSourceSettlementObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch)
	if err != nil {
		t.Fatal(err)
	}
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	r, err = f.st.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if err != nil {
		t.Fatal(err)
	}
	_, _, e = f.srv.registerOutputBytes(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, "image.png", "image/png", data, nil)
	if e == nil || e.status != 409 || calls != 1 {
		t.Fatal("source bypassed through old duplicate/registration", e, calls)
	}
	r, err = f.st.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, e = f.srv.registerOutputBytes(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, "image.png", "image/png", data, nil)
	if e == nil || e.status != 409 || calls != 1 {
		t.Fatal("tombstone bypass", e, calls)
	}
	p, err := f.st.CreateProject(t.Context(), store.Project{TenantID: r.Intent.Scope.TenantID, CreatedBy: r.Intent.Scope.UserID, Name: "independent", SourceType: "standalone", WidthPx: 1024, HeightPx: 1024})
	if err != nil {
		t.Fatal(err)
	}
	_, _, e = f.srv.registerOutputBytes(t.Context(), r.Intent.Scope.TenantID, p.ID, "image.png", "image/png", data, nil)
	if e != nil || calls != 2 {
		t.Fatal("cross-project SHA ban", e, calls)
	}
}
