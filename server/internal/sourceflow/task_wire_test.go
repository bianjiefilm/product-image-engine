package sourceflow

import (
	"encoding/json"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Published v0.1.1 wire fixture: no in-process TaskPort stand-in, no real provider.
func TestSourceTaskFormalSDKLostHTTPResponseReopensOriginalTask(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	submits, commits := 0, 0
	var mu sync.Mutex
	counts := func() (int, int) { mu.Lock(); defer mu.Unlock(); return submits, commits }
	unknown := false
	foreign := false
	var original map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.Header.Get("X-App-ID") != r.Intent.Scope.AppID || req.Header.Get("X-PilotSeaView-Internal-Token") != "task-test" || req.Header.Get("X-Principal-ID") != "" {
			t.Error("wrong Task credential")
		}
		if req.Method == "POST" {
			submits++
			var body map[string]any
			if e := json.NewDecoder(req.Body).Decode(&body); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			billing, _ := body["billing"].(map[string]any)
			if body["idempotency_key"] != r.TaskKey || body["project_id"] != r.Intent.Scope.ProjectID || billing["usage_id"] != r.Quote.UsageID || billing["quote_id"] != r.Quote.QuoteID || billing["amount_minor"] != float64(25) {
				t.Error("changed original wire tuple", body)
			}
			if original == nil {
				commits++
				original = map[string]any{"task_id": "task-original", "funding_version": "source_reservation_v1", "app_id": r.Intent.Scope.AppID, "payer_account_id": r.Intent.Scope.PayerAccountID, "payer_user_id": r.Intent.Scope.UserID, "project_id": r.Intent.Scope.ProjectID, "idempotency_key": r.TaskKey, "capability": r.Intent.Capability, "provider": r.Intent.Provider, "status": "queued", "phase": "hold_pending", "usage_id": r.Quote.UsageID, "quote_id": r.Quote.QuoteID, "quantity": r.Quote.Quantity, "unit_price_minor": r.Quote.UnitPriceMinor, "amount_minor": r.Quote.AmountMinor, "pricing_version": r.Quote.PricingVersion, "business_ref": r.BusinessRef, "hold_id": "", "charge_id": "", "release_id": "", "provider_result_received": false, "output_ready": false, "result_json": "", "error_code": "", "next_retry_at": "", "created_at": "2026-10-01T00:00:00Z"}
			}
			unknown = true
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			conn.Close()
			return
		}
		v := req.URL.Query()
		if v.Get("app_id") != r.Intent.Scope.AppID || v.Get("payer_account_id") != r.Intent.Scope.PayerAccountID || v.Get("payer_user_id") != r.Intent.Scope.UserID || v.Get("project_id") != r.Intent.Scope.ProjectID {
			t.Error("changed Task GET scope", req.URL)
		}
		if strings.HasSuffix(req.URL.Path, "by-idempotency") && v.Get("idempotency_key") != r.TaskKey {
			t.Error("changed Task key")
		}
		if original == nil {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]any{"code": "task_not_found"})
			return
		}
		if unknown {
			w.WriteHeader(503)
			return
		}
		out := map[string]any{}
		for k, v := range original {
			out[k] = v
		}
		if foreign {
			out["usage_id"] = "foreign-usage"
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	clients, e := platform.NewSourceClients(config.Config{AppID: r.Intent.Scope.AppID, IdentityAppID: r.Intent.Scope.AppID, IdentityBaseURL: srv.URL, BillingBaseURL: srv.URL, TaskBaseURL: srv.URL, UploadBaseURL: srv.URL, IdentityToken: "identity-test", BillingToken: "bill-test", TaskToken: "task-test", UploadToken: "upload-test"}, srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	f.svc.Tasks = clients.Tasks
	lost, e := f.svc.Advance(t.Context(), r.ID)
	sent, created := counts()
	if e == nil || lost.ID != r.ID || lost.TaskID != "" || !lost.SubmitAttempted || lost.SubmitStartedAt <= 0 || lost.SubmitBudgetSeconds != 20 || sent != 1 || created != 1 {
		t.Fatal(lost, e, sent, created)
	}
	f.reopen(t)
	f.svc.Tasks = clients.Tasks
	f.svc.Profile.Enabled = false
	mu.Lock()
	unknown = false
	mu.Unlock()
	got, e := f.svc.Advance(t.Context(), r.ID)
	sent, created = counts()
	if e != nil || got.TaskID != "task-original" || got.Task == nil || got.Output != nil || got.SubmitStartedAt != lost.SubmitStartedAt || sent != 1 || created != 1 {
		t.Fatal(got, e, sent, created)
	}
	mu.Lock()
	foreign = true
	mu.Unlock()
	f.reopen(t)
	f.svc.Tasks = clients.Tasks
	f.svc.Profile.Enabled = false
	bad, e := f.svc.Advance(t.Context(), r.ID)
	sent, created = counts()
	if e == nil || bad.TaskID != got.TaskID || bad.Task.Quote.UsageID != r.Quote.UsageID || bad.Output != nil || sent != 1 {
		t.Fatal("foreign observation altered binding", bad, e)
	}
}
