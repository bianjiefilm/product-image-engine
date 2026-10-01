package sourceflow

import (
	"encoding/json"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourceQuoteFormalSDKUnquotedUsageRecovery(t *testing.T) {
	f := flowFixture(t)
	usageCalls, quoteCalls, getCalls := 0, 0, 0
	var usage map[string]any
	var quote map[string]any
	loseQuote := true
	foreign := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-App-ID") != "product-image" || r.Header.Get("X-PilotSeaView-Internal-Token") != "bill-test" || r.Header.Get("X-Principal-ID") != "" {
			t.Error("wrong Billing credentials")
		}
		if r.Method == "GET" {
			v := r.URL.Query()
			if v.Get("app_id") != "product-image" || v.Get("payer_user_id") != f.actor.UserID || v.Get("payer_account_id") != f.actor.AccountID {
				t.Error("wrong original payer scope")
			}
			if usage == nil {
				w.WriteHeader(404)
				json.NewEncoder(w).Encode(map[string]any{"code": "billing_unified_not_found"})
				return
			}
			if !strings.HasSuffix(r.URL.Path, "by-idempotency") {
				getCalls++
				if !strings.HasSuffix(r.URL.Path, "/usage-original") {
					t.Error("retargeted usage ID", r.URL)
				}
			}
			output := map[string]any{}
			for k, v := range usage {
				output[k] = v
			}
			qcopy := quote
			if foreign {
				output["usage_id"] = "usage-foreign"
				if quote != nil {
					qcopy = map[string]any{}
					for k, v := range quote {
						qcopy[k] = v
					}
					qcopy["usage_id"] = "usage-foreign"
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "facts": map[string]any{"usage": output, "quote": qcopy, "hold": nil, "charge": nil, "release": nil, "refund": nil}})
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("request JSON")
		}
		if strings.HasSuffix(r.URL.Path, "/usage") {
			usageCalls++
			usage = map[string]any{"funding_version": "source_reservation_v1", "payer_user_id": body["payer_user_id"], "idempotency_key": body["idempotency_key"], "quote_id": "", "quoted_unit_price_minor": 0, "quoted_at_unix": 0, "quote_expires_at_unix": 0, "usage_id": "usage-original", "app_id": body["app_id"], "capability": body["capability"], "quantity": 1, "pricing_version": body["pricing_version"], "business_ref": body["business_ref"], "payer_account_id": body["payer_account_id"], "quoted_amount_minor": 0, "hold_id": "", "status": "ingested", "created_at_unix": f.now.Unix(), "updated_at_unix": f.now.Unix()}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "fact": usage, "replayed": false})
			return
		}
		quoteCalls++
		if body["usage_id"] != "usage-original" {
			t.Error("quote wrong usage")
		}
		quote = map[string]any{"quote_id": "quote-original", "quoted_at_unix": f.now.Unix(), "expires_at_unix": f.now.Unix() + 300, "usage_id": "usage-original", "amount_minor": 25, "unit_price_minor": 25, "quantity": 1, "pricing_version": "test-price-v1"}
		usage["quote_id"] = "quote-original"
		usage["quoted_unit_price_minor"] = 25
		usage["quoted_at_unix"] = f.now.Unix()
		usage["quote_expires_at_unix"] = f.now.Unix() + 300
		usage["quoted_amount_minor"] = 25
		usage["status"] = "quoted"
		if loseQuote {
			loseQuote = false
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{"error": "test response lost"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "quote": quote})
	}))
	defer srv.Close()
	clients, e := platform.NewSourceClients(config.Config{AppID: "product-image", IdentityAppID: "product-image", IdentityBaseURL: srv.URL, BillingBaseURL: srv.URL, TaskBaseURL: srv.URL, UploadBaseURL: srv.URL, IdentityToken: "identity-test", BillingToken: "bill-test", TaskToken: "task-test", UploadToken: "upload-test"}, srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	f.svc.Bill = clients.Bill
	r, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e == nil || r.ID == "" || r.Bill == nil || r.Bill.UsageID != "usage-original" || r.Quote != nil {
		t.Fatal(r, e)
	}
	f.reopen(t)
	f.svc.Bill = clients.Bill
	foreign = true
	failed, e := f.svc.RecoverQuote(t.Context(), r)
	if e == nil || failed.Quote != nil {
		t.Fatal("server usage change accepted", failed, e)
	}
	stored, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || stored.Bill == nil || stored.Bill.UsageID != "usage-original" || stored.Quote != nil {
		t.Fatal("durable usage overwritten", stored, e)
	}
	foreign = false
	f.now = f.now.Add(31 * time.Second)
	out, e := f.svc.RecoverQuote(t.Context(), stored)
	if e != nil || out.Quote == nil || out.Quote.UsageID != "usage-original" || usageCalls != 1 || quoteCalls != 1 || getCalls != 2 || f.task.Submits != 0 {
		t.Fatal(out, e, usageCalls, quoteCalls, getCalls)
	}
	// The original pre-loss time is immutable even after two restart/retry intervals.
	if out.Quote.ExpiresAtUnix != quote["expires_at_unix"] {
		t.Fatal("quote expiry renewed")
	}
}
