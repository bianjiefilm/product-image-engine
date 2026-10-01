package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sourceWireRun() si.Run {
	r := si.Run{ID: "run-a", Owner: si.Owner, UsageKey: "usage-key", TaskKey: "task-key", BusinessRef: "product-image:run:run-a", Intent: si.Intent{Scope: si.Scope{AppID: "product-image", TenantID: "acct-a", ProjectID: "project-a", UserID: "usr-a", PrincipalAccountID: "acct-a", PayerAccountID: "acct-a"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝色纸盒", PricingVersion: "price-a", Quantity: 1}}
	now := time.Now().Unix()
	r.Quote = &si.Quote{UsageID: "usage-a", UsageKey: r.UsageKey, QuoteID: "quote-a", PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: now, ExpiresAtUnix: now + 300}
	return r
}
func sourceWireConfig(base string) config.Config {
	return config.Config{AppID: "product-image", IdentityAppID: "product-image", IdentityBaseURL: base, IdentityToken: "identity-test", BillingBaseURL: base, BillingToken: "bill-test", TaskBaseURL: base, TaskToken: "task-test", UploadBaseURL: base, UploadToken: "upload-test"}
}
func TestSourceWireScopeAndOneAttempt(t *testing.T) {
	r := sourceWireRun()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls++
		if q.Header.Get("X-App-ID") != "product-image" || q.Header.Get("X-PilotSeaView-Internal-Token") != "task-test" || q.Header.Get("X-Principal-ID") != "" || q.Header.Get("X-Service-ID") != "" {
			t.Error("wrong token family")
		}
		b, _ := io.ReadAll(q.Body)
		var body map[string]json.RawMessage
		if json.Unmarshal(b, &body) != nil || len(body) != 9 {
			t.Error("source submit body")
		}
		if q.URL.Path != "/internal/v1/tasks" || q.Method != "POST" || len(q.URL.Query()) != 0 {
			t.Error("wrong route")
		}
		w.WriteHeader(503)
		io.WriteString(w, `{"code":"task_source_unavailable"}`)
	}))
	defer srv.Close()
	c, e := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Tasks.Submit(t.Context(), r); !errors.Is(e, si.ErrUnavailable) || calls != 1 {
		t.Fatal(e, calls)
	}
}
func TestSourceContextReadHasNoSelector(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		if len(m) != 2 || m["app_id"] != "product-image" || m["principal_id"] != "usr-a" || r.Header.Get("X-PilotSeaView-Internal-Token") != "identity-test" {
			t.Error("read changes selection or credential")
		}
		io.WriteString(w, `{"ok":true,"resolved":false,"reason":"selection_required","context":{"app_id":"product-image","principal_id":"usr-a","switchability":{"allowed":true,"options":["org-a","org-b"],"restored":false}}}`)
	}))
	defer srv.Close()
	c, e := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Context.Resolve(context.Background(), si.ContextQuery{AppID: "product-image", UserID: "usr-a", TenantID: "org-a", ReadOnly: true})
	if e != nil || got.Resolved || got.Reason != "selection_required" || calls != 1 {
		t.Fatal(got, e, calls)
	}
}
func TestSourceWireTaskReadFullScope(t *testing.T) {
	r := sourceWireRun()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls++
		v := q.URL.Query()
		if q.Method != "GET" || len(v) != 5 || v.Get("payer_user_id") != "usr-a" || v.Get("payer_account_id") != "acct-a" || v.Get("project_id") != "project-a" || v.Get("idempotency_key") != "task-key" {
			t.Error("scope missing")
		}
		w.WriteHeader(404)
		io.WriteString(w, `{"code":"task_not_found"}`)
	}))
	defer srv.Close()
	c, e := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	_, found, e := c.Tasks.Lookup(t.Context(), r)
	if e != nil || found || calls != 1 {
		t.Fatal(found, e, calls)
	}
}
func TestSourceWireUnconfigured(t *testing.T) {
	c, e := NewSourceClients(config.Config{}, nil)
	if c != nil || !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(c, e)
	}
	cfg := sourceWireConfig("http://127.0.0.1:18101")
	cfg.BillingToken = cfg.TaskToken
	if _, e = NewSourceClients(cfg, nil); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(e)
	}
}
func sourceTaskJSON(r si.Run) []byte {
	q := r.Quote
	s := r.Intent.Scope
	raw, _ := json.Marshal(map[string]any{"task_id": "task-a", "funding_version": "source_reservation_v1", "app_id": s.AppID, "payer_account_id": s.PayerAccountID, "payer_user_id": s.UserID, "project_id": s.ProjectID, "idempotency_key": r.TaskKey, "capability": r.Intent.Capability, "provider": r.Intent.Provider, "status": "queued", "phase": "hold_pending", "usage_id": q.UsageID, "quote_id": q.QuoteID, "quantity": q.Quantity, "unit_price_minor": q.UnitPriceMinor, "amount_minor": q.AmountMinor, "pricing_version": q.PricingVersion, "business_ref": q.BusinessRef, "hold_id": "", "charge_id": "", "release_id": "", "provider_result_received": false, "output_ready": false, "result_json": "", "error_code": "", "next_retry_at": "", "created_at": "2026-10-01T00:00:00Z"})
	return raw
}
func TestSourceDecodeTaskOriginalTuple(t *testing.T) {
	r := sourceWireRun()
	got, e := decodeSourceTask(sourceTaskJSON(r), r)
	if e != nil || got.ID != "task-a" || got.Scope != r.Intent.Scope {
		t.Fatal(got, e)
	}
	bad := strings.Replace(string(sourceTaskJSON(r)), `"payer_user_id":"usr-a"`, `"payer_user_id":"usr-b"`, 1)
	if _, e = decodeSourceTask([]byte(bad), r); !errors.Is(e, si.ErrInvariant) {
		t.Fatal(e)
	}
}
func sourceBillJSON(r si.Run) []byte {
	q := r.Quote
	s := r.Intent.Scope
	usage := map[string]any{"funding_version": "source_reservation_v1", "payer_user_id": s.UserID, "idempotency_key": r.UsageKey, "quote_id": q.QuoteID, "quoted_unit_price_minor": q.UnitPriceMinor, "quoted_at_unix": q.QuotedAtUnix, "quote_expires_at_unix": q.ExpiresAtUnix, "usage_id": q.UsageID, "app_id": s.AppID, "capability": r.Intent.Capability, "quantity": q.Quantity, "pricing_version": q.PricingVersion, "business_ref": r.BusinessRef, "payer_account_id": s.PayerAccountID, "quoted_amount_minor": q.AmountMinor, "hold_id": "", "status": "quoted", "created_at_unix": q.QuotedAtUnix, "updated_at_unix": q.QuotedAtUnix}
	quote := map[string]any{"quote_id": q.QuoteID, "quoted_at_unix": q.QuotedAtUnix, "expires_at_unix": q.ExpiresAtUnix, "usage_id": q.UsageID, "amount_minor": q.AmountMinor, "unit_price_minor": q.UnitPriceMinor, "quantity": q.Quantity, "pricing_version": q.PricingVersion}
	raw, _ := json.Marshal(map[string]any{"ok": true, "facts": map[string]any{"usage": usage, "quote": quote, "hold": nil, "charge": nil, "release": nil, "refund": nil}})
	return raw
}
func TestSourceDecodeBillExactIntegerAndMismatch(t *testing.T) {
	r := sourceWireRun()
	r.Quote.UnitPriceMinor = 9007199254740993
	r.Quote.AmountMinor = r.Quote.UnitPriceMinor
	got, e := decodeSourceBill(sourceBillJSON(r), r)
	if e != nil || got.Quote == nil || got.Quote.AmountMinor != 9007199254740993 {
		t.Fatal(got, e)
	}
	for _, pair := range [][2]string{{`"payer_account_id":"acct-a"`, `"payer_account_id":"acct-b"`}, {`"funding_version":"source_reservation_v1"`, `"funding_version":"legacy"`}, {`"quantity":1`, `"quantity":1.0`}, {`"amount_minor":9007199254740993`, `"amount_minor":null`}, {`"quote_id":"quote-a"`, `"quote_id":"quote-b"`}} {
		bad := strings.Replace(string(sourceBillJSON(r)), pair[0], pair[1], 1)
		if _, e := decodeSourceBill([]byte(bad), r); !errors.Is(e, si.ErrInvariant) {
			t.Errorf("accepted %s %v", pair[1], e)
		}
	}
}
func TestSourceWireBillingOperations(t *testing.T) {
	r := sourceWireRun()
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls[q.Method+q.URL.Path]++
		if q.Header.Get("X-PilotSeaView-Internal-Token") != "bill-test" || q.Header.Get("X-App-ID") != "product-image" || q.Header.Get("X-Principal-ID") != "" {
			t.Error("billing credential family")
		}
		if q.Method == "GET" {
			v := q.URL.Query()
			if v.Get("app_id") != "product-image" || v.Get("payer_account_id") != "acct-a" || v.Get("payer_user_id") != "usr-a" {
				t.Error("billing scope")
			}
			want := 3
			if strings.HasSuffix(q.URL.Path, "by-idempotency") {
				want = 4
				if v.Get("idempotency_key") != r.UsageKey {
					t.Error("original key")
				}
			}
			if len(v) != want {
				t.Error("query alias")
			}
			w.Write(sourceBillJSON(r))
			return
		}
		var body map[string]json.RawMessage
		raw, _ := io.ReadAll(q.Body)
		json.Unmarshal(raw, &body)
		for k, v := range map[string]string{"app_id": "product-image", "payer_account_id": "acct-a", "payer_user_id": "usr-a", "funding_version": "source_reservation_v1"} {
			if sourceString(body, k) != v {
				t.Error("body scope", k)
			}
		}
		var env map[string]json.RawMessage
		json.Unmarshal(sourceBillJSON(r), &env)
		var facts map[string]json.RawMessage
		json.Unmarshal(env["facts"], &facts)
		if strings.HasSuffix(q.URL.Path, "/usage") {
			if len(body) != 9 || sourceString(body, "idempotency_key") != r.UsageKey {
				t.Error("usage key")
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "fact": facts["usage"], "replayed": false})
		} else {
			if len(body) != 5 || sourceString(body, "usage_id") != r.Quote.UsageID {
				t.Error("quote scope")
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "quote": facts["quote"]})
		}
	}))
	defer srv.Close()
	c, e := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	f, found, e := c.Bill.Lookup(t.Context(), r)
	if e != nil || !found {
		t.Fatal(f, found, e)
	}
	r.Bill = &f
	if _, found, e = c.Bill.Get(t.Context(), r); e != nil || !found {
		t.Fatal(found, e)
	}
	if e = c.Bill.Usage(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	if e = c.Bill.Quote(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 4 {
		t.Fatal(calls)
	}
	for _, n := range calls {
		if n != 1 {
			t.Fatal(calls)
		}
	}
}
func TestSourceWireNotFoundIsNotAny404(t *testing.T) {
	r := sourceWireRun()
	for _, body := range []string{`{"code":"billing_legacy_bridge_missing"}`, `{}`, `bad`, `{"code":"billing_unified_not_found","Code":"billing_unified_not_found"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { w.WriteHeader(404); io.WriteString(w, body) }))
		c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
		_, found, e := c.Bill.Lookup(t.Context(), r)
		srv.Close()
		if e == nil || found {
			t.Fatal("404 forged absence", body, found, e)
		}
	}
}
func TestSourceWireTaskGetCancelReconcile(t *testing.T) {
	r := sourceWireRun()
	r.TaskID = "task-a"
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls[q.Method+q.URL.Path]++
		if len(q.URL.Query()) != 4 || q.URL.Query().Get("project_id") != "project-a" || q.URL.Query().Get("payer_user_id") != "usr-a" || q.Header.Get("X-PilotSeaView-Internal-Token") != "task-test" {
			t.Error("task original scope")
		}
		b, _ := io.ReadAll(q.Body)
		if len(b) != 0 {
			t.Error("task read body")
		}
		w.Write(sourceTaskJSON(r))
	}))
	defer srv.Close()
	c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if _, found, e := c.Tasks.Get(t.Context(), r); e != nil || !found {
		t.Fatal(found, e)
	}
	if e := c.Tasks.Cancel(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	if e := c.Tasks.Reconcile(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 3 {
		t.Fatal(calls)
	}
}
func TestSourceDecodeOutputOriginalReference(t *testing.T) {
	r := sourceWireRun()
	var m map[string]any
	json.Unmarshal(sourceTaskJSON(r), &m)
	out := map[string]any{"asset_id": "asset-a", "reference_id": "ref-a", "project_id": "project-a", "content_type": "image/png", "sha256": strings.Repeat("a", 64), "size_bytes": 1024}
	b, _ := json.Marshal(out)
	m["result_json"] = string(b)
	m["output_ready"] = true
	m["status"] = "succeeded"
	m["phase"] = "succeeded"
	m["hold_id"] = "hold-a"
	m["charge_id"] = "charge-a"
	raw, _ := json.Marshal(m)
	f, e := decodeSourceTask(raw, r)
	if e != nil || f.Output == nil || f.Output.ReferenceID != "ref-a" {
		t.Fatal(f, e)
	}
	delete(out, "reference_id")
	b, _ = json.Marshal(out)
	m["result_json"] = string(b)
	raw, _ = json.Marshal(m)
	if _, e := decodeSourceTask(raw, r); !errors.Is(e, si.ErrInvariant) {
		t.Fatal(e)
	}
}
func TestSourceWireAssetOwnerAndActiveReference(t *testing.T) {
	r := sourceWireRun()
	o := si.Output{AssetID: "asset-a", ReferenceID: "ref-a", ProjectID: "project-a", AppID: "product-image", PrincipalType: "user", PrincipalID: "usr-a", SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 1024}
	r.Task = &si.TaskFact{Output: &o}
	r.Output = &o
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls++
		if q.Header.Get("X-Principal-ID") != "usr-a" || q.Header.Get("X-Principal-Type") != "user" || q.Header.Get("X-PilotSeaView-Internal-Token") != "upload-test" {
			t.Error("upload original owner")
		}
		if strings.Contains(q.URL.Path, "/references/") {
			ref := map[string]any{"asset_version": "durable_asset_v1", "app_id": "product-image", "principal_type": "user", "principal_id": "usr-a", "asset_id": "asset-a", "project_id": "project-a", "reference_id": "ref-a", "state": "active", "created_at": "2026-10-01T00:00:00Z"}
			if q.Method == "POST" {
				ref["state"] = "released"
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "reference": ref})
			return
		}
		if len(q.URL.Query()) != 4 {
			t.Error("upload scope")
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": map[string]any{"asset_version": "durable_asset_v1", "app_id": "product-image", "principal_type": "user", "principal_id": "usr-a", "asset_id": "asset-a", "filename": "output.png", "content_type": "image/png", "sha256": strings.Repeat("a", 64), "size_bytes": 1024, "status": "ready", "active_reference_count": 1}})
	}))
	defer srv.Close()
	c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	got, e := c.Assets.Verify(t.Context(), r)
	if e != nil || got.ReferenceState != "active" {
		t.Fatal(got, e)
	}
	ob := si.Outbox{Scope: r.Intent.Scope, Output: o}
	if state, e := c.Assets.Reference(t.Context(), ob); e != nil || state != "active" {
		t.Fatal(state, e)
	}
	if e := c.Assets.Release(t.Context(), ob); e != nil {
		t.Fatal(e)
	}
	if calls != 4 {
		t.Fatal(calls)
	}
}
func TestSourceDecodeOutputPendingWithoutPublicResult(t *testing.T) {
	r := sourceWireRun()
	var m map[string]any
	json.Unmarshal(sourceTaskJSON(r), &m)
	m["output_ready"] = true
	m["provider_result_received"] = true
	m["phase"] = "capture_unknown"
	raw, _ := json.Marshal(m)
	f, e := decodeSourceTask(raw, r)
	if e != nil || f.Output != nil || f.Phase != "capture_unknown" {
		t.Fatal(f, e)
	}
}
func TestSourceContextDeepLinkDeniedBeforeWire(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if _, e := c.Context.Resolve(t.Context(), si.ContextQuery{AppID: "product-image", UserID: "usr-a", SourceRef: "source-a", ReadOnly: true}); !errors.Is(e, si.ErrInvalid) || calls != 0 {
		t.Fatal(e, calls)
	}
}

type sourceLostResponse struct {
	next  http.RoundTripper
	calls int
}

func (t *sourceLostResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	res, e := t.next.RoundTrip(r)
	if e != nil {
		return res, e
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return nil, io.ErrUnexpectedEOF
}
func TestSourceWireLostResponseNeverRetries(t *testing.T) {
	r := sourceWireRun()
	serverCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { serverCalls++; w.Write(sourceTaskJSON(r)) }))
	defer srv.Close()
	rt := &sourceLostResponse{next: srv.Client().Transport}
	h := &http.Client{Transport: rt}
	c, e := NewSourceClients(sourceWireConfig(srv.URL), h)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Tasks.Submit(t.Context(), r); !errors.Is(e, si.ErrUnavailable) || serverCalls != 1 || rt.calls != 1 {
		t.Fatal(e, serverCalls, rt.calls)
	}
}
func TestSourceWireTaskSubmitSuccessOriginalBilling(t *testing.T) {
	r := sourceWireRun()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls++
		raw, _ := io.ReadAll(q.Body)
		var m map[string]json.RawMessage
		json.Unmarshal(raw, &m)
		var bill map[string]json.RawMessage
		json.Unmarshal(m["billing"], &bill)
		if len(bill) != 11 || sourceString(bill, "funding_version") != "source_reservation_v1" || sourceString(bill, "usage_idempotency_key") != r.UsageKey || sourceString(bill, "quote_id") != r.Quote.QuoteID || string(bill["amount_minor"]) != "25" {
			t.Error("original billing proof")
		}
		var p map[string]json.RawMessage
		json.Unmarshal(m["params"], &p)
		if len(p) != 4 || sourceString(p, "model") != "qwen-image-2.0" || string(p["n"]) != "1" {
			t.Error("provider params")
		}
		w.Write(sourceTaskJSON(r))
	}))
	defer srv.Close()
	c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if e := c.Tasks.Submit(t.Context(), r); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
}
func TestSourceWireUploadWrongReferenceDenied(t *testing.T) {
	r := sourceWireRun()
	o := si.Output{AssetID: "asset-a", ReferenceID: "ref-a", ProjectID: "project-a", AppID: "product-image", PrincipalType: "user", PrincipalID: "usr-a", SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 1024}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		io.WriteString(w, `{"ok":true,"reference":{"asset_version":"durable_asset_v1","app_id":"product-image","principal_type":"user","principal_id":"usr-other","asset_id":"asset-a","project_id":"project-a","reference_id":"ref-a","state":"active","created_at":"2026-10-01T00:00:00Z"}}`)
	}))
	defer srv.Close()
	c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	if _, e := c.Assets.Reference(t.Context(), si.Outbox{Scope: r.Intent.Scope, Output: o}); !errors.Is(e, si.ErrInvariant) {
		t.Fatal(e)
	}
}
func TestSourceWireDownloadGrantScopedEcho(t *testing.T) {
	r := sourceWireRun()
	o := si.Output{AssetID: "asset-a", ReferenceID: "ref-a", ProjectID: "project-a", AppID: "product-image", PrincipalType: "user", PrincipalID: "usr-a", SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 1024}
	r.Task = &si.TaskFact{Output: &o}
	r.Output = &o
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls++
		if q.Method != "POST" || q.URL.Path != "/internal/v1/upload/durable/assets/asset-a/download-url" || len(q.URL.Query()) != 0 || q.Header.Get("X-Principal-ID") != "usr-a" {
			t.Error("download scope")
		}
		b, _ := io.ReadAll(q.Body)
		var m map[string]json.RawMessage
		json.Unmarshal(b, &m)
		if len(m) != 4 || sourceString(m, "principal_id") != "usr-a" {
			t.Error("download owner")
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "download_url": "https://sample.oss-cn-hangzhou.aliyuncs.com/output.png?temporary=fixture", "expires_at": "2026-10-01T00:01:00Z", "asset": map[string]any{"asset_version": "durable_asset_v1", "app_id": "product-image", "principal_type": "user", "principal_id": "usr-a", "asset_id": "asset-a", "filename": "output.png", "content_type": "image/png", "sha256": strings.Repeat("a", 64), "size_bytes": 1024, "status": "ready", "active_reference_count": 1}})
	}))
	defer srv.Close()
	c, _ := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
	url, e := c.Assets.downloadGrant(t.Context(), r)
	if e != nil || url == "" || calls != 1 {
		t.Fatal("grant failed", e, calls)
	}
}
func sourceChargedBillJSON(r si.Run) []byte {
	var env map[string]any
	json.Unmarshal(sourceBillJSON(r), &env)
	facts := env["facts"].(map[string]any)
	usage := facts["usage"].(map[string]any)
	usage["status"] = "charged"
	usage["hold_id"] = "hold-a"
	facts["hold"] = map[string]any{"hold_id": "hold-a", "usage_id": "usage-a", "funding_version": "source_reservation_v1", "payer_user_id": "usr-a", "quote_id": "quote-a", "status": "spent", "idempotency_key": "hold-key", "amount_minor": 25, "spent_minor": 25, "created_at_unix": 1, "closed_at_unix": 2, "allocations": []any{}}
	facts["charge"] = map[string]any{"charge_id": "charge-a", "usage_id": "usage-a", "payer_account_id": "acct-a", "app_id": "product-image", "capability": "image.generate", "quantity": 1, "pricing_version": "price-a", "amount_minor": 25, "promo_minor": 0, "allowance_minor": 25, "cash_minor": 0, "deduction_version": "promo-allowance-cash/v1", "status": "charged", "reason": "source image", "created_at_unix": 2, "idempotency_key": "charge-key", "allocations": []any{}}
	raw, _ := json.Marshal(env)
	return raw
}
func TestSourceDecodeChargeAmountAndStatusBound(t *testing.T) {
	r := sourceWireRun()
	got, e := decodeSourceBill(sourceChargedBillJSON(r), r)
	if e != nil || got.ChargedMinor == nil || *got.ChargedMinor != 25 || got.ChargeID != "charge-a" {
		t.Fatal(got, e)
	}
	for _, pair := range [][2]string{{`"allowance_minor":25`, `"allowance_minor":24`}, {`"status":"charged"`, `"status":"bogus"`}, {`"spent_minor":25`, `"spent_minor":0`}} {
		bad := strings.Replace(string(sourceChargedBillJSON(r)), pair[0], pair[1], 1)
		if _, e := decodeSourceBill([]byte(bad), r); !errors.Is(e, si.ErrInvariant) {
			t.Fatal("accepted charge inconsistency", pair[1], e)
		}
	}
}
