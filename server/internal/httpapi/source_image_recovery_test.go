package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// This fixture is a scripted external wire boundary, not the implementation of
// Billing/Task or a provider. Product Router/JWT/file SQLite/published SDK are real.
type sourceRecoveryWire struct {
	mu                     sync.Mutex
	f                      *sourceHTTPFixture
	now                    int64
	fail, calls, committed map[string]int
	usage, quote, task     map[string]any
}

func (p *sourceRecoveryWire) FailAfterCommit(operation string, count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail[operation] = count
}
func (p *sourceRecoveryWire) Calls(operation string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[operation]
}
func (p *sourceRecoveryWire) Committed(operation string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.committed[operation]
}
func (p *sourceRecoveryWire) mutations() [6]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return [6]int{p.calls["billing.usage"], p.calls["billing.source_quote"], p.calls["task.source_submit"], p.calls["task.source_reconcile"], p.calls["task.source_cancel"], p.calls["upload.durable_release"]}
}
func (p *sourceRecoveryWire) lose(w http.ResponseWriter, operation string) bool {
	if p.fail[operation] == 0 {
		return false
	}
	p.fail[operation]--
	// Close only after committing the fixture fact: an actual lost HTTP response.
	c, _, e := w.(http.Hijacker).Hijack()
	if e != nil {
		return false
	}
	c.Close()
	return true
}
func (p *sourceRecoveryWire) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	service, op := "", ""
	switch {
	case strings.HasPrefix(r.URL.Path, "/internal/v1/billing/unified/"):
		service = "bill"
		if r.Method == "GET" {
			op = "billing.read"
		} else if strings.HasSuffix(r.URL.Path, "/usage") {
			op = "billing.usage"
		} else if strings.HasSuffix(r.URL.Path, "/quote") {
			op = "billing.source_quote"
		}
	case strings.HasPrefix(r.URL.Path, "/internal/v1/tasks"):
		service = "task"
		if r.Method == "GET" {
			op = "task.read"
		} else if r.URL.Path == "/internal/v1/tasks" {
			op = "task.source_submit"
		} else if strings.HasSuffix(r.URL.Path, "/reconcile") {
			op = "task.source_reconcile"
		}
	}
	if op == "" {
		t.Errorf("unexpected wire %s %s", r.Method, r.URL.Path)
		w.WriteHeader(400)
		return
	}
	p.calls[op]++
	if r.Header.Get("X-App-ID") != "product-image" || r.Header.Get("X-PilotSeaview-Internal-Token") != service+"-wire" || r.Header.Get("X-Service-ID") != "" || r.Header.Get("X-Principal-ID") != "" {
		t.Errorf("incorrect credential family for %s", op)
		w.WriteHeader(403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" || op == "task.source_reconcile" {
		q := r.URL.Query()
		if q.Get("app_id") != "product-image" || q.Get("payer_user_id") != p.f.actor.UserID || q.Get("payer_account_id") != p.f.actor.AccountID {
			t.Error("changed payer query")
			w.WriteHeader(403)
			return
		}
		if service == "task" && q.Get("project_id") != p.f.project.ID {
			t.Error("changed project query")
			w.WriteHeader(403)
			return
		}
		wantQuery := 3
		if service == "task" {
			wantQuery++
		}
		byKey := strings.HasSuffix(r.URL.Path, "/by-idempotency")
		if byKey {
			wantQuery++
		}
		if len(q) != wantQuery {
			t.Error("unknown query selector")
			w.WriteHeader(400)
			return
		}
		for _, values := range q {
			if len(values) != 1 {
				t.Error("duplicate query selector")
				w.WriteHeader(400)
				return
			}
		}
		if service == "bill" && r.URL.Path != "/internal/v1/billing/unified/usage/by-idempotency" && r.URL.Path != "/internal/v1/billing/unified/usage/usage-wire" || service == "task" && r.URL.Path != "/internal/v1/tasks/by-idempotency" && r.URL.Path != "/internal/v1/tasks/task-wire" && r.URL.Path != "/internal/v1/tasks/task-wire/reconcile" {
			t.Error("changed original upstream path")
			w.WriteHeader(400)
			return
		}
		fact := p.usage
		if service == "task" {
			fact = p.task
		}
		if fact == nil {
			w.WriteHeader(404)
			code := "billing_unified_not_found"
			if service == "task" {
				code = "task_not_found"
			}
			json.NewEncoder(w).Encode(map[string]any{"code": code})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/by-idempotency") && q.Get("idempotency_key") != fact["idempotency_key"] {
			t.Error("changed original key")
			w.WriteHeader(403)
			return
		}
		if service == "task" {
			if op == "task.source_reconcile" {
				p.committed[op]++
			}
			json.NewEncoder(w).Encode(fact)
		} else {
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "facts": map[string]any{"usage": p.usage, "quote": p.quote, "hold": nil, "charge": nil, "release": nil, "refund": nil}})
		}
		return
	}
	var b map[string]any
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	if d.Decode(&b) != nil || b["app_id"] != "product-image" || b["payer_user_id"] != p.f.actor.UserID || b["payer_account_id"] != p.f.actor.AccountID {
		t.Error("changed body scope")
		w.WriteHeader(400)
		return
	}
	switch op {
	case "billing.usage":
		if b["funding_version"] != "source_reservation_v1" || b["quantity"] != json.Number("1") || b["capability"] != "image.generate" || b["pricing_version"] != "http-test-v1" || b["idempotency_key"] == "" {
			t.Error("changed usage intent")
			w.WriteHeader(400)
			return
		}
		if p.usage == nil {
			p.committed[op]++
			p.usage = map[string]any{"funding_version": "source_reservation_v1", "payer_user_id": b["payer_user_id"], "idempotency_key": b["idempotency_key"], "quote_id": "", "quoted_unit_price_minor": 0, "quoted_at_unix": 0, "quote_expires_at_unix": 0, "usage_id": "usage-wire", "app_id": b["app_id"], "capability": b["capability"], "quantity": 1, "pricing_version": b["pricing_version"], "business_ref": b["business_ref"], "payer_account_id": b["payer_account_id"], "quoted_amount_minor": 0, "hold_id": "", "status": "ingested", "created_at_unix": p.now, "updated_at_unix": p.now}
		}
		if p.lose(w, op) {
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "fact": p.usage, "replayed": false})
	case "billing.source_quote":
		if p.usage == nil || b["usage_id"] != "usage-wire" || b["funding_version"] != "source_reservation_v1" {
			t.Error("quote changed original usage")
			w.WriteHeader(400)
			return
		}
		if p.quote == nil {
			p.committed[op]++
			p.quote = map[string]any{"quote_id": "quote-wire", "quoted_at_unix": p.now, "expires_at_unix": p.now + 300, "usage_id": "usage-wire", "amount_minor": 25, "unit_price_minor": 25, "quantity": 1, "pricing_version": "http-test-v1"}
			for k, v := range map[string]any{"quote_id": "quote-wire", "quoted_unit_price_minor": 25, "quoted_at_unix": p.now, "quote_expires_at_unix": p.now + 300, "quoted_amount_minor": 25, "status": "quoted"} {
				p.usage[k] = v
			}
		}
		if p.lose(w, op) {
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "quote": p.quote})
	case "task.source_submit":
		q, ok := b["billing"].(map[string]any)
		if !ok || p.quote == nil || b["project_id"] != p.f.project.ID || q["usage_id"] != "usage-wire" || q["quote_id"] != "quote-wire" || q["amount_minor"] != json.Number("25") || q["expires_at_unix"] != json.Number(jsonInt(p.now+300)) {
			t.Error("changed task billing tuple")
			w.WriteHeader(400)
			return
		}
		params, ok := b["params"].(map[string]any)
		if !ok || b["provider"] != "modelxing-qwen-image-2.0-v1" || b["capability"] != "image.generate" || params["model"] != "qwen-image-2.0" || params["size"] != "1024*1024" || params["n"] != json.Number("1") || params["prompt"] != "蓝色纸盒" || q["business_ref"] != p.usage["business_ref"] || q["pricing_version"] != "http-test-v1" || q["quantity"] != json.Number("1") || q["unit_price_minor"] != json.Number("25") || q["quoted_at_unix"] != json.Number(jsonInt(p.now)) {
			t.Error("changed original generation intent")
			w.WriteHeader(400)
			return
		}
		if p.task == nil {
			p.committed[op]++
			p.task = map[string]any{"task_id": "task-wire", "funding_version": "source_reservation_v1", "app_id": b["app_id"], "payer_account_id": b["payer_account_id"], "payer_user_id": b["payer_user_id"], "project_id": b["project_id"], "idempotency_key": b["idempotency_key"], "capability": b["capability"], "provider": b["provider"], "status": "queued", "phase": "hold_pending", "usage_id": q["usage_id"], "quote_id": q["quote_id"], "quantity": q["quantity"], "unit_price_minor": q["unit_price_minor"], "amount_minor": q["amount_minor"], "pricing_version": q["pricing_version"], "business_ref": q["business_ref"], "hold_id": "", "charge_id": "", "release_id": "", "provider_result_received": false, "output_ready": false, "result_json": "", "error_code": "", "next_retry_at": "", "created_at": "2026-10-01T00:00:00Z"}
		}
		if p.lose(w, op) {
			return
		}
		json.NewEncoder(w).Encode(p.task)
	}
}
func jsonInt(v int64) string { b, _ := json.Marshal(v); return string(b) }

func (p *sourceRecoveryWire) ReopenProduct(t *testing.T) {
	t.Helper()
	f := p.f
	if e := f.st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(f.cfg.DBPath)
	if e != nil {
		t.Fatal(e)
	}
	f.st = st
	f.srv.St = st
	f.srv.Source.Store = st
	f.srv.Source.Auth.Store = st
}
func newSourceRecoveryWire(t *testing.T) (*sourceHTTPFixture, *sourceRecoveryWire, *time.Time) {
	t.Helper()
	f := newSourceHTTPFixture(t)
	now := time.Now().UTC()
	p := &sourceRecoveryWire{f: f, now: now.Unix(), fail: map[string]int{}, calls: map[string]int{}, committed: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serve(t, w, r) }))
	t.Cleanup(srv.Close)
	clients, e := platform.NewSourceClients(config.Config{AppID: "product-image", IdentityAppID: "product-image", IdentityBaseURL: srv.URL, BillingBaseURL: srv.URL, TaskBaseURL: srv.URL, UploadBaseURL: srv.URL, IdentityToken: "identity-wire", BillingToken: "bill-wire", TaskToken: "task-wire", UploadToken: "upload-wire"}, srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	f.srv.Source.Bill = clients.Bill
	f.srv.Source.Tasks = clients.Tasks
	f.srv.Source.Assets = clients.Assets
	f.srv.Source.Now = func() time.Time { return now }
	return f, p, &now
}

func TestSourceRecoveryPublishedWireLostCommitReopen(t *testing.T) {
	for _, op := range []string{"billing.usage", "billing.source_quote", "task.source_submit"} {
		t.Run(op, func(t *testing.T) {
			f, p, now := newSourceRecoveryWire(t)
			p.FailAfterCommit(op, 1)
			body := `{"request_key":"wire-reopen","mode":"text_generate","prompt":"蓝色纸盒","size":"1024*1024"}`
			w := f.raw(t, "POST", f.path(""), f.token, body)
			want := 503
			if op == "task.source_submit" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("create=%d want=%d %s", w.Code, want, w.Body.String())
			}
			scope := si.Scope{AppID: "product-image", TenantID: f.actor.AccountID, ProjectID: f.project.ID, UserID: f.actor.UserID, PrincipalAccountID: f.actor.AccountID, PayerAccountID: f.actor.AccountID}
			r, e := f.st.FindSourceRun(t.Context(), scope, "wire-reopen")
			if e != nil {
				t.Fatal(e)
			}
			originalID, usageKey, taskKey, businessRef := r.ID, r.UsageKey, r.TaskKey, r.BusinessRef
			confirmBody := ""
			confirmationHash := ""
			if op == "task.source_submit" {
				b, _ := json.Marshal(map[string]string{"quote_id": r.Quote.QuoteID, "quote_fingerprint": si.QuoteHash(*r.Quote)})
				confirmBody = string(b)
				w = f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, confirmBody)
				if w.Code != 503 || p.Calls(op) != 1 || p.Committed(op) != 1 {
					t.Fatal("lost submit", w.Code, w.Body.String(), p.Calls(op), p.Committed(op))
				}
				confirmed, err := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
				if err != nil || confirmed.ConfirmationHash == "" {
					t.Fatal("lost submit did not retain explicit confirmation", err)
				}
				confirmationHash = confirmed.ConfirmationHash
			} else if p.Committed(op) != 1 || p.Calls("task.source_submit") != 0 {
				t.Fatal("unconfirmed quote dispatched")
			}
			p.ReopenProduct(t)
			*now = now.Add(31 * time.Second)
			if op == "task.source_submit" {
				w = f.raw(t, "POST", f.path("/"+r.ID+"/reconcile"), f.token, `{}`)
			} else {
				w = f.raw(t, "POST", f.path(""), f.token, body)
			}
			if w.Code != 200 {
				t.Fatal("reopen recovery", w.Code, w.Body.String())
			}
			r, e = f.st.FindSourceRun(t.Context(), scope, "wire-reopen")
			if e != nil || r.ID != originalID || r.UsageKey != usageKey || r.TaskKey != taskKey || r.BusinessRef != businessRef || r.Quote == nil || r.Quote.QuoteID != "quote-wire" || r.Quote.AmountMinor != 25 || r.Quote.ExpiresAtUnix != p.now+300 || p.Calls("billing.usage") != 1 || p.Committed("billing.usage") != 1 || p.Calls("billing.source_quote") != 1 || p.Committed("billing.source_quote") != 1 {
				t.Fatal("original identity/expiry changed", r, e)
			}
			p.mu.Lock()
			wireKeysMatch := p.usage["idempotency_key"] == usageKey && p.usage["business_ref"] == businessRef && (p.task == nil || p.task["idempotency_key"] == taskKey)
			p.mu.Unlock()
			if !wireKeysMatch {
				t.Fatal("published wire changed original keys")
			}
			if op == "task.source_submit" {
				if r.TaskID != "task-wire" || r.ConfirmationHash != confirmationHash || p.Calls(op) != 1 || p.Committed(op) != 1 {
					t.Fatal("duplicate dispatch", r)
				}
				w = f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, confirmBody)
				if w.Code != 200 || p.Calls(op) != 1 {
					t.Fatal("double confirmation dispatched", w.Code, w.Body.String())
				}
				p.mu.Lock()
				p.task["phase"] = "provider_unknown"
				p.task["status"] = "running"
				p.mu.Unlock()
				w = f.raw(t, "POST", f.path("/"+r.ID+"/reconcile"), f.token, `{}`)
				r, e = f.st.GetSourceRunForRecovery(t.Context(), r.ID)
				if w.Code != 200 || e != nil || r.Phase != "provider_unknown" || r.Output != nil || p.Calls(op) != 1 || p.Calls("billing.source_quote") != 1 {
					t.Fatal("unknown provider invented new work/output", w.Code, r, e)
				}
			} else if p.Calls("task.source_submit") != 0 {
				t.Fatal("quote recovery dispatched")
			}
			before, e := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
			if e != nil {
				t.Fatal(e)
			}
			f.srv.Source.Profile.Enabled = false
			mutationsBefore := p.mutations()
			fresh := f.stub.mint("source@example.com", "product-image")
			for _, suffix := range []string{"", "/" + r.ID, "/by-request?request_key=wire-reopen"} {
				w = f.raw(t, "GET", f.path(suffix), fresh, "")
				if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || !strings.Contains(w.Body.String(), `"amount_minor":"25"`) {
					t.Fatal("fresh login history", w.Code, w.Body.String())
				}
			}
			after, e := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
			if e != nil || !reflect.DeepEqual(before, after) || mutationsBefore != p.mutations() {
				t.Fatal("history advanced/wrote", e)
			}
		})
	}
}

func TestSourceRecoveryPublishedWirePendingOutputDoesNotUpgrade(t *testing.T) {
	for _, phase := range []string{"asset_pending", "capture_unknown"} {
		t.Run(phase, func(t *testing.T) {
			f, p, _ := newSourceRecoveryWire(t)
			r := f.create(t)
			body, _ := json.Marshal(map[string]string{"quote_id": r.Quote.QuoteID, "quote_fingerprint": si.QuoteHash(*r.Quote)})
			w := f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, string(body))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			result, _ := json.Marshal(map[string]any{"asset_id": "asset-wire", "reference_id": "reference-wire", "project_id": f.project.ID, "content_type": "image/png", "sha256": strings.Repeat("a", 64), "size_bytes": 123})
			p.mu.Lock()
			p.task["status"] = "running"
			p.task["phase"] = phase
			p.task["provider_result_received"] = true
			p.task["output_ready"] = true
			p.task["result_json"] = string(result)
			p.mu.Unlock()
			p.ReopenProduct(t)
			w = f.raw(t, "POST", f.path("/"+r.ID+"/reconcile"), f.token, `{}`)
			current, e := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
			if w.Code != 200 || e != nil || current.Task == nil || current.Task.Phase != phase || current.Task.Output == nil || current.Task.Output.AssetID != "asset-wire" || current.Output != nil || current.ChargedBill != nil || current.Phase == "succeeded" || p.Calls("task.source_submit") != 1 || p.Calls("billing.source_quote") != 1 {
				t.Fatal("pending authority upgraded or receipt lost", w.Code, current, e)
			}
			before := p.mutations()
			for _, action := range []struct{ method, suffix, body string }{{"POST", "/select", `{}`}, {"GET", "/export", ""}} {
				w = f.raw(t, action.method, f.path("/"+r.ID+action.suffix), f.token, action.body)
				if w.Code != 409 {
					t.Fatal("pending result became usable", w.Code, w.Body.String())
				}
			}
			if before != p.mutations() {
				t.Fatal("pending select/export mutated authority")
			}
		})
	}
}

// Recovery after a product mapping commit but before its response is returned
// must reuse the same durable association. Upstream ports here are scripted
// facts from the existing HTTP fixture; this is not a published-wire claim.
func TestSourceRecoveryMappingReopenArchiveRoundTrip(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.ready(t)
	before := f.ports.mutations()
	p := &sourceRecoveryWire{f: f}
	p.ReopenProduct(t)
	if _, e := f.srv.Source.ObserveOutput(t.Context(), r); e != nil {
		t.Fatal(e)
	}
	current, e := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if e != nil || current.Output == nil || *current.Output != *r.Output || current.TaskID != r.TaskID || current.Quote == nil || *current.Quote != *r.Quote || f.ports.mutations() != before {
		t.Fatal("mapping recovery duplicated upstream work", current, e)
	}
	for _, status := range []string{"archived", "active"} {
		if _, e = f.st.UpdateProject(t.Context(), f.project.TenantID, f.project.ID, store.ProjectUpdate{Status: &status}); e != nil {
			t.Fatal(e)
		}
		w := f.raw(t, "GET", f.path("/"+r.ID), f.token, "")
		if w.Code != 200 {
			t.Fatal(status, w.Code)
		}
		if status == "archived" {
			w = f.raw(t, "POST", f.path("/"+r.ID+"/select"), f.token, `{}`)
			if w.Code != 403 {
				t.Fatal("archived project write", w.Code)
			}
		}
	}
	current, e = f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if e != nil || current.Deleted || current.Output == nil || *current.Output != *r.Output || f.ports.mutations() != before {
		t.Fatal("archive/unarchive lost reference or mutated Task", current, e)
	}
}

func TestSourceRecoveryWrongPublishedUploadFactsNoSelection(t *testing.T) {
	for _, fault := range []string{"app", "user", "project", "hash"} {
		t.Run(fault, func(t *testing.T) {
			f := newSourceHTTPFixture(t)
			r := f.ready(t)
			before := f.ports.mutations()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				if q.Method != "GET" || q.Header.Get("X-App-ID") != "product-image" || q.Header.Get("X-Principal-Type") != "user" || q.Header.Get("X-Principal-ID") != f.actor.UserID || q.Header.Get("X-PilotSeaview-Internal-Token") != "upload-proof-wire" {
					t.Error("wrong original Upload authority or mutation")
					w.WriteHeader(403)
					return
				}
				a := map[string]any{"asset_version": "durable_asset_v1", "app_id": "product-image", "principal_type": "user", "principal_id": f.actor.UserID, "asset_id": r.Output.AssetID, "filename": "output.png", "content_type": "image/png", "sha256": r.Output.SHA256, "size_bytes": r.Output.SizeBytes, "status": "ready", "active_reference_count": 1}
				ref := map[string]any{"asset_version": "durable_asset_v1", "app_id": "product-image", "principal_type": "user", "principal_id": f.actor.UserID, "asset_id": r.Output.AssetID, "project_id": f.project.ID, "reference_id": r.Output.ReferenceID, "state": "active", "created_at": "2026-10-01T00:00:00Z"}
				switch fault {
				case "app":
					a["app_id"] = "foreign"
				case "user":
					a["principal_id"] = "foreign"
				case "project":
					ref["project_id"] = "foreign"
				case "hash":
					a["sha256"] = strings.Repeat("b", 64)
				}
				if q.URL.Path == "/internal/v1/upload/durable/assets/"+r.Output.AssetID {
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": a})
				} else if q.URL.Path == "/internal/v1/upload/durable/assets/"+r.Output.AssetID+"/references/"+r.Output.ReferenceID {
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "reference": ref})
				} else {
					t.Error("unexpected Upload path", q.URL.Path)
					w.WriteHeader(400)
				}
			}))
			t.Cleanup(upstream.Close)
			clients, e := platform.NewSourceClients(config.Config{AppID: "product-image", IdentityAppID: "product-image", IdentityBaseURL: upstream.URL, BillingBaseURL: upstream.URL, TaskBaseURL: upstream.URL, UploadBaseURL: upstream.URL, IdentityToken: "identity-proof-wire", BillingToken: "bill-proof-wire", TaskToken: "task-proof-wire", UploadToken: "upload-proof-wire", SourceDownloadHosts: "fixture.oss-cn-hangzhou.aliyuncs.com"}, upstream.Client())
			if e != nil {
				t.Fatal(e)
			}
			f.srv.Source.Assets = clients.Assets
			var beforeExport si.Run
			for _, action := range []struct{ method, suffix, body string }{{"POST", "/select", `{}`}, {"GET", "/export", ""}} {
				w := f.raw(t, action.method, f.path("/"+r.ID+action.suffix), f.token, action.body)
				if w.Code != 503 {
					t.Fatal("foreign Upload fact became usable", fault, w.Code, w.Body.String())
				}
				if action.method == "POST" {
					beforeExport, e = f.st.GetSourceRunForRecovery(t.Context(), r.ID)
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			current, e := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
			if e != nil || !reflect.DeepEqual(current, beforeExport) || f.ports.mutations() != before {
				t.Fatal("GET export changed product facts or authority", e)
			}
			// SELECT is an explicit write: it may record its failed fresh proof and
			// lease/retry bookkeeping. Original receipts and identity must survive.
			if current.ID != r.ID || current.Owner != r.Owner || current.Fingerprint != r.Fingerprint ||
				current.UsageKey != r.UsageKey || current.TaskKey != r.TaskKey || current.BusinessRef != r.BusinessRef ||
				current.TaskID != r.TaskID || current.ConfirmationHash != r.ConfirmationHash || current.ConfirmedAt != r.ConfirmedAt ||
				current.Selected || current.Deleted || current.Phase != r.Phase ||
				!reflect.DeepEqual(current.Intent, r.Intent) || !reflect.DeepEqual(current.Quote, r.Quote) ||
				!reflect.DeepEqual(current.Output, r.Output) || !reflect.DeepEqual(current.ChargedBill, r.ChargedBill) {
				t.Fatal("bad proof replaced original receipt or became selected")
			}
		})
	}
}

// The product request completes and persists, but its HTTP response never reaches
// the client. This is real product HTTP/JWT/SQLite plus the published SDK wire;
// upstream services remain scripted fixtures, not production service acceptance.
func TestSourceRecoveryConfirmResponseLostReopenOriginalReceipt(t *testing.T) {
	f, p, now := newSourceRecoveryWire(t)
	r := f.create(t)
	body, _ := json.Marshal(map[string]string{"quote_id": r.Quote.QuoteID, "quote_fingerprint": si.QuoteHash(*r.Quote)})
	router := f.srv.Router()
	product := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		recorded := httptest.NewRecorder()
		router.ServeHTTP(recorded, q)
		if recorded.Code != 200 {
			t.Errorf("product confirm did not complete: %d %s", recorded.Code, recorded.Body.String())
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	t.Cleanup(product.Close)
	request, err := http.NewRequestWithContext(t.Context(), "POST", product.URL+f.path("/"+r.ID+"/confirm"), strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Product-Internal-Token", "it-test")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+f.token)
	request.Close = true
	client := product.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("product response was not lost")
	}
	original, err := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if err != nil || original.ConfirmationHash == "" || original.TaskID != "task-wire" || p.Calls("task.source_submit") != 1 || p.Committed("task.source_submit") != 1 {
		t.Fatal("lost product reply did not retain original receipt", original, err)
	}
	p.ReopenProduct(t)
	*now = now.Add(31 * time.Second)
	for range 2 {
		w := f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, string(body))
		if w.Code != 200 {
			t.Fatal("same explicit confirmation not recoverable", w.Code, w.Body.String())
		}
	}
	current, err := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if err != nil || current.ID != original.ID || current.UsageKey != original.UsageKey || current.TaskKey != original.TaskKey || current.BusinessRef != original.BusinessRef || current.TaskID != original.TaskID || current.ConfirmationHash != original.ConfirmationHash || !reflect.DeepEqual(current.Quote, original.Quote) || p.Calls("task.source_submit") != 1 || p.Committed("task.source_submit") != 1 || p.Calls("billing.usage") != 1 || p.Calls("billing.source_quote") != 1 {
		t.Fatal("repeated confirmation changed identity or dispatched twice", current, err)
	}
}
