package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/sourceflow"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

// Test ports represent external service facts; Router, JWT and SQLite are real.
type sourceHTTPPorts struct {
	mu                                                                      sync.Mutex
	bill                                                                    si.BillFact
	task                                                                    si.TaskFact
	usage, quotes, submits, cancels, reconciles, releases, downloads, reads int
	failUsage, failGet                                                      bool
	payload                                                                 string
}

func (p *sourceHTTPPorts) Lookup(ctx context.Context, r si.Run) (si.BillFact, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	return p.bill, p.bill.UsageID != "", nil
}
func (p *sourceHTTPPorts) Get(ctx context.Context, r si.Run) (si.BillFact, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.failGet {
		return si.BillFact{}, false, si.ErrUnavailable
	}
	return p.bill, p.bill.UsageID != "", nil
}
func (p *sourceHTTPPorts) Usage(ctx context.Context, r si.Run) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usage++
	p.bill = si.BillFact{Scope: r.Intent.Scope, UsageID: "usage_" + r.ID, UsageKey: r.UsageKey, Capability: r.Intent.Capability, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Status: "ingested", Quantity: 1}
	if p.failUsage {
		return errors.New("https://private.invalid?token=secret")
	}
	return nil
}
func (p *sourceHTTPPorts) Quote(ctx context.Context, r si.Run) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.quotes++
	q := si.Quote{UsageID: p.bill.UsageID, UsageKey: r.UsageKey, QuoteID: "quote_" + r.ID, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: time.Now().Unix() - 1, ExpiresAtUnix: time.Now().Unix() + 299}
	p.bill.Quote = &q
	p.bill.Status = "quoted"
	return nil
}

type sourceHTTPTasks struct{ p *sourceHTTPPorts }

func (t sourceHTTPTasks) Lookup(ctx context.Context, r si.Run) (si.TaskFact, bool, error) {
	t.p.mu.Lock()
	defer t.p.mu.Unlock()
	t.p.reads++
	return t.p.task, t.p.task.ID != "", nil
}
func (t sourceHTTPTasks) Get(ctx context.Context, r si.Run) (si.TaskFact, bool, error) {
	return t.Lookup(ctx, r)
}
func (t sourceHTTPTasks) Submit(ctx context.Context, r si.Run) error {
	t.p.mu.Lock()
	defer t.p.mu.Unlock()
	t.p.submits++
	t.p.task = si.TaskFact{ID: "task_http", IdempotencyKey: r.TaskKey, Phase: "queued", Status: "queued", Provider: r.Intent.Provider, Capability: r.Intent.Capability, Scope: r.Intent.Scope, Quote: *r.Quote}
	return nil
}
func (t sourceHTTPTasks) Cancel(ctx context.Context, r si.Run) error {
	t.p.mu.Lock()
	defer t.p.mu.Unlock()
	t.p.cancels++
	return nil
}
func (t sourceHTTPTasks) Reconcile(ctx context.Context, r si.Run) error {
	t.p.mu.Lock()
	defer t.p.mu.Unlock()
	t.p.reconciles++
	return nil
}

type sourceHTTPAssets struct{ p *sourceHTTPPorts }

func (a sourceHTTPAssets) Verify(ctx context.Context, r si.Run) (si.Output, error) {
	if r.Task == nil || r.Task.Output == nil {
		return si.Output{}, si.ErrUnavailable
	}
	return *r.Task.Output, nil
}
func (a sourceHTTPAssets) Reference(ctx context.Context, o si.Outbox) (string, error) {
	return "active", nil
}
func (a sourceHTTPAssets) Release(ctx context.Context, o si.Outbox) error {
	a.p.mu.Lock()
	defer a.p.mu.Unlock()
	a.p.releases++
	return nil
}
func (a sourceHTTPAssets) Download(ctx context.Context, r si.Run) (io.ReadCloser, error) {
	a.p.mu.Lock()
	defer a.p.mu.Unlock()
	a.p.downloads++
	return io.NopCloser(strings.NewReader(a.p.payload)), nil
}

type sourceHTTPFixture struct {
	*fixture
	ports   *sourceHTTPPorts
	actor   sourceflow.Actor
	project store.Project
	token   string
}

func newSourceHTTPFixture(t *testing.T) *sourceHTTPFixture { return newSourceHTTPFixtureWith(t, nil) }

// newSourceHTTPFixtureWith lets a test add config (for example an Upload stub)
// on top of the shared source-image wiring.
func newSourceHTTPFixtureWith(t *testing.T, mutate func(*config.Config)) *sourceHTTPFixture {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		if mutate != nil {
			mutate(c)
		}
	})
	sourceDB := filepath.Join(t.TempDir(), "source-http.db")
	if e := f.st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(sourceDB)
	if e != nil {
		t.Fatal(e)
	}
	f.st = st
	f.srv.St = st
	f.cfg.DBPath = sourceDB
	f.srv.Cfg.DBPath = sourceDB
	t.Cleanup(func() { f.st.Close() })
	token := f.stub.mint("source@example.com", "product-image")
	p, e := f.srv.Verify.Verify(t.Context(), token)
	if e != nil {
		t.Fatal(e)
	}
	actor := sourceflow.Actor{UserID: p.UserID, AccountID: p.AccountID}
	project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: actor.AccountID, CreatedBy: actor.UserID, Name: "Source", SourceType: "standalone", WidthPx: 1024, HeightPx: 1024})
	if e != nil {
		t.Fatal(e)
	}
	ports := &sourceHTTPPorts{payload: "verified-local-png"}
	auth := &sourceflow.Authorizer{Store: f.st, AppID: f.cfg.AppID}
	f.srv.Source = &sourceflow.Service{Store: f.st, Auth: auth, Bill: ports, Tasks: sourceHTTPTasks{ports}, Assets: sourceHTTPAssets{ports}, Profile: sourceflow.Profile{Enabled: true, Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", PricingVersion: "http-test-v1", Quantity: 1}}
	f.srv.TextPaint = sourcePanicPainter{}
	f.srv.ImageHTTP = &http.Client{Transport: sourcePanicTransport{}}
	f.rearm(t)
	return &sourceHTTPFixture{fixture: f, ports: ports, actor: actor, project: project, token: token}
}

type sourcePanicPainter struct{}

func (sourcePanicPainter) Paint(context.Context, string) ([]byte, error) {
	panic("source reached TextPaint")
}

type sourcePanicTransport struct{}

func (sourcePanicTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("source reached direct provider")
}
func (f *sourceHTTPFixture) path(s string) string {
	return "/api/v1/projects/" + f.project.ID + "/source-image-runs" + s
}
func (f *sourceHTTPFixture) raw(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Product-Internal-Token", "it-test")
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.srv.Router().ServeHTTP(w, r)
	return w
}
func (f *sourceHTTPFixture) create(t *testing.T) si.Run {
	t.Helper()
	w := f.raw(t, "POST", f.path(""), f.token, `{"request_key":"request-http","mode":"text_generate","prompt":"蓝色纸盒","size":"1024*1024"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	auth, e := f.srv.Source.Auth.ForProject(t.Context(), f.actor, f.project.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	runs, e := f.st.ListSourceRuns(t.Context(), auth.Scope)
	if e != nil || len(runs) != 1 {
		t.Fatal(runs, e)
	}
	return runs[0]
}
func (f *sourceHTTPFixture) ready(t *testing.T) si.Run {
	t.Helper()
	r := f.create(t)
	r, e := f.srv.Source.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.srv.Source.Advance(t.Context(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	n := int64(25)
	o := si.Output{AssetID: "asset_http", ReferenceID: "reference_http", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: strings.Repeat("a", 64), ContentType: "image/png", Status: "ready", ReferenceState: "active", SizeBytes: int64(len(f.ports.payload))}
	f.ports.task.Status = "succeeded"
	f.ports.task.Phase = "succeeded"
	f.ports.task.Output = &o
	f.ports.task.HoldID = "hold_http"
	f.ports.task.ChargeID = "charge_http"
	f.ports.bill.Status = "charged"
	f.ports.bill.HoldID = "hold_http"
	f.ports.bill.ChargeID = "charge_http"
	f.ports.bill.ChargedMinor = &n
	r, e = f.srv.Source.ObserveOutput(t.Context(), r)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (p *sourceHTTPPorts) mutations() [6]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return [6]int{p.usage, p.quotes, p.submits, p.cancels, p.reconciles, p.releases}
}

func TestSourceUnconfiguredRoutesRemain(t *testing.T) {
	f := newFixture(t, nil)
	tok := f.stub.mint("source@example.com", "product-image")
	for _, path := range []string{"/api/v1/projects/prj_x/source-image-capabilities", "/api/v1/projects/prj_x/source-image-runs", "/api/v1/projects/prj_x/source-image-runs/run_x/content"} {
		code, _ := f.do(t, "GET", path, tok, nil)
		if code != 503 {
			t.Fatalf("source route %s: got %d want dedicated 503", path, code)
		}
	}
}
func TestSourceStrictRequestsRejectBeforeMutation(t *testing.T) {
	f := newSourceHTTPFixture(t)
	cases := []struct {
		method, suffix, body string
		want                 int
	}{
		{"GET", "", `{}`, 400}, {"HEAD", "", "", 405}, {"PUT", "", `{}`, 405},
		{"POST", "", `null`, 400}, {"POST", "", `[]`, 400}, {"POST", "", `{} {}`, 400},
		{"POST", "", `{"request_key":"a","request_key":"b"}`, 400},
		{"POST", "", `{"request_key":"a","Request_Key":"b"}`, 400},
		{"POST", "", `{"payer_account_id":"foreign"}`, 400},
		{"POST", "", `{"params":{"n":1,"n":2}}`, 400},
		{"POST", "", `{"request_key":null,"mode":"text_generate","prompt":"x","size":"1024*1024"}`, 400},
		{"POST", "", `{"request_key":9223372036854775808}`, 400},
		{"POST", "", `{"request_key":"x","mode":"text_generate","prompt":"` + strings.Repeat("x", 32769) + `","size":"1024*1024"}`, 400},
		{"GET", "?limit=1&limit=2", "", 400}, {"GET", "?Limit=1", "", 400}, {"GET", "?limit=1e2", "", 400}, {"GET", "?limit=9223372036854775808", "", 400},
		{"GET", "/by-request?request_key=a&request_key=b", "", 400}, {"GET", "/by-request?request_key=null", "", 404},
		{"POST", "/unknown/confirm?app_id=evil", `{}`, 400}, {"POST", "/unknown/confirm", `{"quote_id":"x","quote_fingerprint":null}`, 400},
		{"POST", "/unknown/reconcile", `{"unknown":true}`, 400}, {"DELETE", "/unknown/output", `null`, 400},
		{"GET", "//unknown", "", 400}, {"GET", "/unknown%2Fforeign", "", 400},
	}
	for _, c := range cases {
		t.Run(c.method+c.suffix+c.body[:min(len(c.body), 30)], func(t *testing.T) {
			w := f.raw(t, c.method, f.path(c.suffix), f.token, c.body)
			if w.Code != c.want {
				t.Fatalf("got %d want %d: %s", w.Code, c.want, w.Body.String())
			}
		})
	}
	if got := f.ports.mutations(); got != [6]int{} {
		t.Fatal("rejected requests mutated", got)
	}
}
func TestSourceAuthenticatedQuoteConfirmAndHistory(t *testing.T) {
	f := newSourceHTTPFixture(t)
	w := f.raw(t, "GET", f.path(""), "", "")
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = f.raw(t, "GET", f.path(""), f.stub.mint("source@example.com", "foreign-app"), "")
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r := f.create(t)
	if f.ports.submits != 0 {
		t.Fatal("quote submitted")
	}
	w = f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, `{"quote_id":"quote_http","quote_fingerprint":"tampered"}`)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"quote_id": r.Quote.QuoteID, "quote_fingerprint": si.QuoteHash(*r.Quote)})
	w = f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, string(body))
	if w.Code != 200 || f.ports.submits != 1 {
		t.Fatal(w.Code, w.Body.String(), f.ports.submits)
	}
	w = f.raw(t, "POST", f.path("/"+r.ID+"/confirm"), f.token, string(body))
	if w.Code != 200 || f.ports.submits != 1 {
		t.Fatal("double confirm", w.Code, w.Body.String(), f.ports.submits)
	}
	before := f.ports.mutations()
	stored, _ := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	for _, suffix := range []string{"", "/" + r.ID, "/by-request?request_key=request-http"} {
		w = f.raw(t, "GET", f.path(suffix), f.token, "")
		if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal(w.Code, w.Body.String(), w.Header())
		}
		if !strings.Contains(w.Body.String(), `"amount_minor":"25"`) {
			t.Fatal(w.Body.String())
		}
	}
	now, _ := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if before != f.ports.mutations() || !reflect.DeepEqual(stored, now) {
		t.Fatal("GET advanced/wrote")
	}
	foreign := f.stub.mint("foreign@example.com", "product-image")
	w = f.raw(t, "GET", f.path("/"+r.ID), foreign, "")
	if w.Code != 404 || strings.Contains(w.Body.String(), r.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	f.srv.Source.Profile.Enabled = false
	if w = f.raw(t, "GET", f.path("/"+r.ID), f.token, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = f.raw(t, "POST", f.path(""), f.token, `{"request_key":"new","mode":"text_generate","prompt":"x","size":"1024*1024"}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
	status := "archived"
	_, e := f.st.UpdateProject(t.Context(), f.project.TenantID, f.project.ID, store.ProjectUpdate{Status: &status})
	if e != nil {
		t.Fatal(e)
	}
	if w = f.raw(t, "GET", f.path("/"+r.ID), f.token, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = f.raw(t, "POST", f.path("/"+r.ID+"/reconcile"), f.token, `{}`); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSourceUpstreamUnknownKeepsAuthorizedOriginalID(t *testing.T) {
	f := newSourceHTTPFixture(t)
	f.ports.failUsage = true
	w := f.raw(t, "POST", f.path(""), f.token, `{"request_key":"lost","mode":"text_generate","prompt":"x","size":"1024*1024"}`)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"run_id":"sir_`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("leaked upstream", w.Body.String())
	}
	w = f.raw(t, "GET", f.path("/by-request?request_key=lost"), f.token, "")
	if w.Code != 200 || f.ports.usage != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSourceContentExportReadOnlyFreshBillAndDelete(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.ready(t)
	before := f.ports.mutations()
	snapshot, _ := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	w := f.raw(t, "GET", f.path("/"+r.ID+"/content"), f.token, "")
	if w.Code != 200 || w.Body.String() != f.ports.payload || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "GET", f.path("/"+r.ID+"/export"), f.token, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	z, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	if len(z.File) != 3 {
		t.Fatal(z.File)
	}
	for _, entry := range z.File {
		b, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(b)
		b.Close()
		if entry.Name == "image.png" && (entry.Method != zip.Store || string(raw) != f.ports.payload) {
			t.Fatal(entry.Name)
		}
		if entry.Name == "quality.json" && (!strings.Contains(string(raw), `"human_adoption":"NOT_RUN"`) || strings.Contains(string(raw), "PASS")) {
			t.Fatal(string(raw))
		}
	}
	now, _ := f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if before != f.ports.mutations() || !reflect.DeepEqual(snapshot, now) {
		t.Fatal("download mutated run")
	}
	f.ports.failGet = true
	w = f.raw(t, "GET", f.path("/"+r.ID+"/content"), f.token, "")
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	f.ports.failGet = false
	f.ports.bill.Status = "refunded"
	f.ports.bill.RefundID = "refund_http"
	w = f.raw(t, "GET", f.path("/"+r.ID+"/export"), f.token, "")
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	now, _ = f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if !reflect.DeepEqual(snapshot, now) {
		t.Fatal("refund GET mutated")
	}
	f.ports.bill.Status = "charged"
	f.ports.bill.RefundID = ""
	w = f.raw(t, "DELETE", f.path("/"+r.ID+"/output"), f.token, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	now, _ = f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if !now.Deleted || now.Selected || !reflect.DeepEqual(now.Output, snapshot.Output) || f.ports.releases != 0 {
		t.Fatal(now, f.ports.releases)
	}
	w = f.raw(t, "GET", f.path("/"+r.ID+"/content"), f.token, "")
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestSourceIDCannotReachLegacyOwner(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.create(t)
	for _, suffix := range []string{"/submit", "/refresh", "/confirm", "/select"} {
		code, _ := f.do(t, "POST", "/api/v1/projects/"+f.project.ID+"/text-images/"+r.ID+suffix, f.token, map[string]any{})
		if code != 404 {
			t.Fatal(suffix, code)
		}
	}
	if f.ports.submits != 0 {
		t.Fatal(f.ports.submits)
	}
}

type sourceHTTPContext func(context.Context, si.ContextQuery) (si.ContextFact, error)

func (c sourceHTTPContext) Resolve(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
	return c(ctx, q)
}
func TestSourceCreateRechecksArchiveAfterAuthorization(t *testing.T) {
	f := newSourceHTTPFixture(t)
	project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: "org_http", CreatedBy: f.actor.UserID, Name: "Organization", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	f.project = project
	f.srv.Source.Profile.OrgEnabled = true
	f.srv.Source.Auth.Context = sourceHTTPContext(func(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
		status := "archived"
		_, e := f.st.UpdateProject(ctx, project.TenantID, project.ID, store.ProjectUpdate{Status: &status})
		if e != nil {
			return si.ContextFact{}, e
		}
		return si.ContextFact{Resolved: true, PayerKnown: true, AppID: q.AppID, UserID: q.UserID, TenantID: project.TenantID, PayerAccountID: "payer_http", PayerSource: "delegation", Role: "editor", MemberRole: "payer"}, nil
	})
	_, e = f.srv.Source.Create(t.Context(), f.actor, project.ID, sourceflow.NewRunRequest{RequestKey: "archived-race", Mode: "text_generate", Prompt: "x", Size: "1024*1024"})
	if !errors.Is(e, si.ErrForbidden) || f.ports.usage != 0 {
		t.Fatal("Create bypassed archive after authorize", e, f.ports.usage)
	}
}

func TestSourcePaginationScopeAndStrictCursor(t *testing.T) {
	f := newSourceHTTPFixture(t)
	first := f.create(t)
	scope := first.Intent.Scope
	for i := 0; i < 4; i++ {
		in := first.Intent
		in.RequestKey = "page-" + string(rune('a'+i))
		if _, _, e := f.st.CreateSourceRunForProject(t.Context(), in); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 4; page++ {
		path := f.path("?limit=2")
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := f.raw(t, "GET", path, f.token, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var body struct {
			Runs []struct {
				ID string `json:"run_id"`
			} `json:"runs"`
			Next string `json:"next_cursor"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		for _, r := range body.Runs {
			if seen[r.ID] {
				t.Fatal("repeated page", r.ID)
			}
			seen[r.ID] = true
		}
		cursor = body.Next
		if cursor == "" {
			break
		}
	}
	if len(seen) != 5 {
		t.Fatal("lost page", seen)
	}
	other := f.stub.mint("other@example.com", "product-image")
	w := f.raw(t, "GET", f.path("?limit=2"), other, "")
	if w.Code != 404 || strings.Contains(w.Body.String(), first.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, raw := range []string{`{"v":null,"created_at":"1","id":"sir_a"}`, `{"v":1,"created_at":"1e2","id":"sir_a"}`, `{"v":1,"created_at":"9223372036854775808","id":"sir_a"}`, `{"v":2,"created_at":"1","id":"sir_a"}`, `{"v":1,"created_at":"1","id":"sir_a","other":1}`, `{"v":1,"created_at":"1","id":"sir_a","id":"sir_b"}`} {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
		w = f.raw(t, "GET", f.path("?cursor="+encoded), f.token, "")
		if w.Code != 400 {
			t.Fatal(raw, w.Code, w.Body.String())
		}
	}
	// A foreign transport cursor only changes the position, never the SQL scope.
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"created_at":"9223372036854775807","id":"sir_foreign"}`))
	w = f.raw(t, "GET", f.path("?limit=1&cursor="+raw), f.token, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	rows, e := f.st.ListSourceRuns(t.Context(), scope)
	if e != nil || len(rows) != 5 {
		t.Fatal(rows, e)
	}
}
func TestSourceStrictInputInvalidUTF8AndControlQuery(t *testing.T) {
	f := newSourceHTTPFixture(t)
	for _, c := range []struct{ method, suffix, body string }{
		{"POST", "", `{"request_key":"bad-utf8","mode":"text_generate","prompt":"` + string([]byte{0xff}) + `","size":"1024*1024"}`},
		{"GET", "/by-request?request_key=bad%00key", ""},
	} {
		w := f.raw(t, c.method, f.path(c.suffix), f.token, c.body)
		if w.Code != 400 {
			t.Fatal("invalid text accepted", w.Code, w.Body.String())
		}
	}
	if f.ports.mutations() != [6]int{} {
		t.Fatal("invalid text caused paid work")
	}
}
func TestSourceMethodGuardsAllResources(t *testing.T) {
	f := newSourceHTTPFixture(t)
	for _, route := range []struct{ suffix, method string }{{"", "GET"}, {"/by-request?request_key=x", "GET"}, {"/unknown", "GET"}, {"/unknown/confirm", "POST"}, {"/unknown/reconcile", "POST"}, {"/unknown/cancel", "POST"}, {"/unknown/select", "POST"}, {"/unknown/output", "DELETE"}, {"/unknown/content", "GET"}, {"/unknown/export", "GET"}} {
		wrong := "PATCH"
		w := f.raw(t, wrong, f.path(route.suffix), f.token, `{}`)
		if w.Code != 405 {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
	if f.ports.mutations() != [6]int{} {
		t.Fatal("wrong method mutated")
	}
}
func TestSourceCrossUserOrganizationReadAndWriteDenied(t *testing.T) {
	f := newSourceHTTPFixture(t)
	project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: "org_shared", CreatedBy: f.actor.UserID, Name: "Shared", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	f.project = project
	f.srv.Source.Profile.OrgEnabled = true
	var queries []si.ContextQuery
	f.srv.Source.Auth.Context = sourceHTTPContext(func(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
		queries = append(queries, q)
		return si.ContextFact{Resolved: true, PayerKnown: true, AppID: q.AppID, UserID: q.UserID, TenantID: project.TenantID, PayerAccountID: "payer_shared", PayerSource: "delegation", Role: "editor", MemberRole: "payer"}, nil
	})
	r := f.create(t)
	before := f.ports.mutations()
	queries = nil
	foreign := f.stub.mintExtra("other@example.com", "product-image", jwt.MapClaims{"org_memberships": map[string]any{"org_shared": "owner"}, "org_payers": map[string]any{"org_shared": "payer_shared"}})
	for _, route := range []struct{ method, suffix, body string }{{"GET", "/" + r.ID, ""}, {"POST", "/" + r.ID + "/reconcile", `{}`}, {"POST", "/" + r.ID + "/cancel", `{}`}, {"DELETE", "/" + r.ID + "/output", `{}`}} {
		w := f.raw(t, route.method, f.path(route.suffix), foreign, route.body)
		if w.Code != 404 || strings.Contains(w.Body.String(), r.ID) {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
	for _, q := range queries {
		if q.ReadOnly && (q.TenantID != "" || q.SourceRef != "") {
			t.Fatal("GET sent selection", q)
		}
	}
	if before != f.ports.mutations() {
		t.Fatal("foreign write mutated")
	}
	w := f.raw(t, "GET", f.path(""), foreign, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), r.ID) {
		t.Fatal("list exposed other user", w.Code, w.Body.String())
	}
}
func TestSourceLegacyIDNotSourceAndArchivedUnknownNotDisclosed(t *testing.T) {
	f := newSourceHTTPFixture(t)
	for _, id := range []string{"legacy_id", "sir_unknown"} {
		w := f.raw(t, "GET", f.path("/"+id), f.token, "")
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	status := "archived"
	f.st.UpdateProject(t.Context(), f.project.TenantID, f.project.ID, store.ProjectUpdate{Status: &status})
	w := f.raw(t, "POST", f.path("/sir_unknown/cancel"), f.token, `{}`)
	if w.Code != 404 {
		t.Fatal("archived unknown disclosed", w.Code, w.Body.String())
	}
}

func TestSourceFrozenReportCannotLeakDuplicateOrPrivateFields(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.ready(t)
	original := r.QualityJSON
	for _, mutated := range []string{`{"run_id":"https://private.invalid?token=secret",` + original[1:], strings.Replace(original, `"ordinary conceptual image; product preservation is not verified"`, `"https://private.invalid?token=secret"`, 1)} {
		r.QualityJSON = mutated
		if _, e := sourceFrozenQuality(r); !errors.Is(e, si.ErrInvariant) {
			t.Fatal("untrusted frozen report was exported", e)
		}
	}
}

func TestSourceCapabilitiesDoNotClaimUnwiredProductReady(t *testing.T) {
	f := newSourceHTTPFixture(t)
	w := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
	var body struct {
		Modes []struct {
			Mode       string `json:"mode"`
			Ready      bool   `json:"ready"`
			Reason     string `json:"reason"`
			CanQuote   bool   `json:"can_quote"`
			CanConfirm bool   `json:"can_confirm"`
		} `json:"modes"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if w.Code != 200 || len(body.Modes) != 3 || body.Modes[0].Ready || body.Modes[0].Reason != "runtime_output_recovery_unconfigured" || !body.Modes[0].CanQuote || !body.Modes[0].CanConfirm {
		t.Fatal("partial runtime claimed complete readiness", w.Code, w.Body.String())
	}
	f.srv.Source.Bill = nil
	w = f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if body.Modes[0].CanQuote || body.Modes[0].CanConfirm || body.Modes[0].Ready {
		t.Fatal("nil bill port claimed capability", w.Body.String())
	}
}

func TestSourceKnownReadonlyOrganizationRoleGetsForbiddenWrite(t *testing.T) {
	f := newSourceHTTPFixture(t)
	project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: "org_role", CreatedBy: f.actor.UserID, Name: "Role", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	f.project = project
	f.srv.Source.Profile.OrgEnabled = true
	role := "editor"
	member := "payer"
	f.srv.Source.Auth.Context = sourceHTTPContext(func(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
		return si.ContextFact{Resolved: true, PayerKnown: true, AppID: q.AppID, UserID: q.UserID, TenantID: project.TenantID, PayerAccountID: "payer_role", PayerSource: "delegation", Role: role, MemberRole: member}, nil
	})
	r := f.create(t)
	role, member = "viewer", "viewer"
	before := f.ports.mutations()
	w := f.raw(t, "GET", f.path("/"+r.ID), f.token, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = f.raw(t, "POST", f.path("/"+r.ID+"/cancel"), f.token, `{}`)
	if w.Code != 403 {
		t.Fatal("known readable role received wrong status", w.Code, w.Body.String())
	}
	if before != f.ports.mutations() {
		t.Fatal("readonly member mutated")
	}
}

func TestSourceHTTPBoundsAuthorizationBeforePlatformCalls(t *testing.T) {
	f := newSourceHTTPFixture(t)
	project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: "org_budget", CreatedBy: f.actor.UserID, Name: "Bounded", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	f.project = project
	f.srv.Source.Profile.OrgEnabled = true
	bounded := true
	f.srv.Source.Auth.Context = sourceHTTPContext(func(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			bounded = false
		}
		return si.ContextFact{Resolved: true, PayerKnown: true, AppID: q.AppID, UserID: q.UserID, TenantID: project.TenantID, PayerAccountID: "payer_budget", PayerSource: "delegation", Role: "editor", MemberRole: "payer"}, nil
	})
	w := f.raw(t, "POST", f.path(""), f.token, `{"request_key":"bounded","mode":"text_generate","prompt":"x","size":"1024*1024"}`)
	if w.Code != 200 || !bounded {
		t.Fatal("authorization had no bounded request context", w.Code, bounded)
	}
}

func TestSourceActionsRemainOriginalAndNewFlagsDoNotBlockRecovery(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.create(t)
	w := f.raw(t, "POST", f.path("/"+r.ID+"/cancel"), f.token, `{}`)
	if w.Code != 200 || f.ports.submits != 0 || f.ports.releases != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	view := w.Body.String()
	if !strings.Contains(view, `"phase":"not_dispatched"`) {
		t.Fatal(view)
	}
	// A separate explicit request is needed after cancellation.
	in := r.Intent
	in.RequestKey = "recover-existing"
	run, _, e := f.st.CreateSourceRunForProject(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	// Test ports are scoped to the current original run; no remote authority is reused.
	f.ports.bill = si.BillFact{}
	run, e = f.srv.Source.RecoverQuote(t.Context(), run)
	if e != nil {
		t.Fatal(e)
	}
	run, e = f.srv.Source.Confirm(t.Context(), f.actor, f.project.ID, run.ID, run.Quote.QuoteID, si.QuoteHash(*run.Quote))
	if e != nil {
		t.Fatal(e)
	}
	run, e = f.srv.Source.Advance(t.Context(), run.ID)
	if e != nil {
		t.Fatal(e)
	}
	key, id := run.TaskKey, run.TaskID
	before := f.ports.submits
	f.srv.Source.Profile.Enabled = false
	w = f.raw(t, "POST", f.path("/"+run.ID+"/reconcile"), f.token, `{}`)
	if w.Code != 200 || f.ports.reconciles != 1 || f.ports.submits != before {
		t.Fatal(w.Code, w.Body.String())
	}
	current, e := f.st.GetSourceRunForRecovery(t.Context(), run.ID)
	if e != nil || current.TaskID != id || current.TaskKey != key || current.Output != nil {
		t.Fatal("reconcile changed original or invented output", current, e)
	}
}
func TestSourceFreshSelectRefundAndHistoricalAmounts(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.ready(t)
	w := f.raw(t, "POST", f.path("/"+r.ID+"/select"), f.token, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.ports.bill.Status = "refunded"
	f.ports.bill.RefundID = "refund_http"
	w = f.raw(t, "POST", f.path("/"+r.ID+"/select"), f.token, `{}`)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := f.ports.mutations()
	w = f.raw(t, "GET", f.path("/"+r.ID), f.token, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, part := range []string{`"status":"refunded"`, `"original_charged_minor":"25"`, `"charged_minor":"25"`, `"refund_id":"refund_http"`, `"refund_amount_minor":null`, `"selected":false`, `"refresh_status":"unknown"`} {
		if !strings.Contains(w.Body.String(), part) {
			t.Fatal("missing real original fact", part, w.Body.String())
		}
	}
	if before != f.ports.mutations() {
		t.Fatal("refund read mutated")
	}
}
func TestSourceMoneyProjectionPreservesCanonicalInt64(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.create(t)
	r.Quote.AmountMinor = 9223372036854775807
	r.Quote.UnitPriceMinor = 9223372036854775807
	raw, e := json.Marshal(sourceView(r))
	if e != nil || !strings.Contains(string(raw), `"amount_minor":"9223372036854775807"`) {
		t.Fatal(string(raw), e)
	}
}
func TestSourceExplicitDeleteBeforeLateOutputNeverRevivesHTTP(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.create(t)
	r, e := f.srv.Source.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.srv.Source.Advance(t.Context(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	w := f.raw(t, "DELETE", f.path("/"+r.ID+"/output"), f.token, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	n := int64(25)
	o := si.Output{AssetID: "asset_http", ReferenceID: "reference_http", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: strings.Repeat("a", 64), ContentType: "image/png", Status: "ready", ReferenceState: "active", SizeBytes: 18}
	f.ports.task.Status = "succeeded"
	f.ports.task.Phase = "succeeded"
	f.ports.task.Output = &o
	f.ports.task.HoldID = "hold_http"
	f.ports.task.ChargeID = "charge_http"
	f.ports.bill.Status = "charged"
	f.ports.bill.HoldID = "hold_http"
	f.ports.bill.ChargeID = "charge_http"
	f.ports.bill.ChargedMinor = &n
	r, e = f.st.GetSourceRunForRecovery(t.Context(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.srv.Source.ObserveOutput(t.Context(), r)
	if e != nil || !r.Deleted || r.Output != nil || r.Selected {
		t.Fatal("late output resurrected", r, e)
	}
	before := f.ports.mutations()
	w = f.raw(t, "GET", f.path("/"+r.ID), f.token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"output":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "GET", f.path("/"+r.ID+"/export"), f.token, "")
	if w.Code != 409 || before != f.ports.mutations() {
		t.Fatal(w.Code, w.Body.String())
	}
}

// A real HTTP server must bound a stalled request body as well as platform work.
// ResponseController exposes deadlines through an ordinary ResponseWriter.
type sourceDeadlineRecorder struct {
	*httptest.ResponseRecorder
	read, write time.Time
}

func (w *sourceDeadlineRecorder) SetReadDeadline(v time.Time) error {
	if !v.IsZero() {
		w.read = v
	}
	return nil
}
func (w *sourceDeadlineRecorder) SetWriteDeadline(v time.Time) error {
	if !v.IsZero() {
		w.write = v
	}
	return nil
}
func TestSourceHTTPBoundsBodyAndResponseIO(t *testing.T) {
	f := newSourceHTTPFixture(t)
	req := httptest.NewRequest("GET", f.path(""), nil)
	req.Header.Set("X-Product-Internal-Token", "it-test")
	req.Header.Set("Authorization", "Bearer "+f.token)
	w := &sourceDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	f.srv.Router().ServeHTTP(w, req)
	if w.Code != 200 || w.read.IsZero() || w.write.IsZero() || time.Until(w.read) > 20*time.Second || time.Until(w.write) > 20*time.Second {
		t.Fatal("body/response IO remained unbounded", w.Code, w.read, w.write)
	}
}

func TestSourceFileDBReopenJWTSessionKeepsOriginalFacts(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.ready(t)
	before := f.ports.mutations()
	w := f.raw(t, "GET", f.path("/"+r.ID), f.token, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	first := w.Body.String()
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
	freshLogin := f.stub.mint("source@example.com", "product-image")
	w = f.raw(t, "GET", f.path("/"+r.ID), freshLogin, "")
	if w.Code != 200 || w.Body.String() != first || before != f.ports.mutations() {
		t.Fatal("relogin/reopen changed original facts", w.Code, w.Body.String())
	}
	w = f.raw(t, "GET", f.path("/by-request?request_key=request-http"), freshLogin, "")
	if w.Code != 200 || w.Body.String() != first {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSourceCookieOnlyDoesNotGrantGoAuth(t *testing.T) {
	f := newSourceHTTPFixture(t)
	req := httptest.NewRequest("POST", f.path(""), strings.NewReader(`{"request_key":"cookie","mode":"text_generate","prompt":"x","size":"1024*1024"}`))
	req.Header.Set("X-Product-Internal-Token", "it-test")
	req.Header.Set("Cookie", "access_token="+f.token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.srv.Router().ServeHTTP(w, req)
	if w.Code != 401 || f.ports.mutations() != [6]int{} {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSourceViewIncludesPayerLabelWithoutGuessingAmounts(t *testing.T) {
	f := newSourceHTTPFixture(t)
	r := f.create(t)
	p := sourceView(r)["payment"].(map[string]any)
	if p["payer_label"] != "个人账户" || p["payer_account_id"] != f.actor.AccountID || p["charged_minor"] != nil {
		t.Fatal("payer label/unknown charge contract missing", p)
	}
	r.Intent.Scope.TenantID = "org_display"
	r.Intent.Scope.PayerAccountID = "payer_display"
	p = sourceView(r)["payment"].(map[string]any)
	if p["payer_label"] != "组织委托账户" || p["payer_account_id"] != "payer_display" {
		t.Fatal(p)
	}
}

func TestSourceReadCapabilitiesCannotGrantViewerWritePermission(t *testing.T) {
	for _, roles := range []struct {
		role, member string
		canWrite     bool
		status       int
	}{{"viewer", "payer", false, 200}, {"editor", "viewer", false, 200}, {"unknown", "payer", false, 404}, {"editor", "unknown", false, 404}, {"member", "owner", false, 200}, {"owner", "owner", true, 200}, {"admin", "payer", true, 200}} {
		t.Run(roles.role+roles.member, func(t *testing.T) {
			f := newSourceHTTPFixture(t)
			project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: "org_caps_role", CreatedBy: f.actor.UserID, Name: "Viewer capability", SourceType: "standalone"})
			if e != nil {
				t.Fatal(e)
			}
			f.project = project
			f.srv.Source.Profile.OrgEnabled = true
			f.srv.Source.Auth.Context = sourceHTTPContext(func(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
				if !q.ReadOnly || q.TenantID != "" || q.SourceRef != "" {
					t.Fatal("GET performed selection", q)
				}
				return si.ContextFact{Resolved: true, PayerKnown: true, AppID: q.AppID, UserID: q.UserID, TenantID: project.TenantID, PayerAccountID: "payer_caps_role", PayerSource: "delegation", Role: roles.role, MemberRole: roles.member}, nil
			})
			w := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
			if w.Code != roles.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if roles.status == 404 {
				if f.ports.mutations() != [6]int{} {
					t.Fatal("unknown role mutated")
				}
				return
			}
			var b struct {
				Modes []struct {
					CanQuote            bool   `json:"can_quote"`
					CanConfirm          bool   `json:"can_confirm"`
					Reason              string `json:"reason"`
					AuthorizationReason string `json:"authorization_reason"`
					RuntimeReason       string `json:"runtime_reason"`
				} `json:"modes"`
			}
			json.Unmarshal(w.Body.Bytes(), &b)
			wantReason := "project_write_forbidden"
			if roles.canWrite {
				wantReason = "runtime_output_recovery_unconfigured"
			}
			if len(b.Modes) == 0 || b.Modes[0].CanQuote != roles.canWrite || b.Modes[0].CanConfirm != roles.canWrite || b.Modes[0].Reason != wantReason {
				t.Fatal("GET capability granted viewer write", w.Body.String())
			}
			wantAuthorization := "project_write_forbidden"
			if roles.canWrite {
				wantAuthorization = ""
			}
			if b.Modes[0].AuthorizationReason != wantAuthorization || b.Modes[0].RuntimeReason != "runtime_output_recovery_unconfigured" {
				t.Fatal("authorization/runtime reasons conflated", w.Body.String())
			}
			if f.ports.mutations() != [6]int{} {
				t.Fatal("read capability mutated")
			}
		})
	}
}

func TestSourceCreateKnownReadonlyProjectReturnsForbidden(t *testing.T) {
	f := newSourceHTTPFixture(t)
	project, e := f.st.CreateProject(t.Context(), store.Project{TenantID: "org_readonly_create", CreatedBy: f.actor.UserID, Name: "Known read project", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	f.project = project
	f.srv.Source.Profile.OrgEnabled = true
	f.srv.Source.Auth.Context = sourceHTTPContext(func(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
		return si.ContextFact{Resolved: true, PayerKnown: true, AppID: q.AppID, UserID: q.UserID, TenantID: project.TenantID, PayerAccountID: "payer_readonly", PayerSource: "delegation", Role: "viewer", MemberRole: "viewer"}, nil
	})
	w := f.raw(t, "POST", f.path(""), f.token, `{"request_key":"readonly-create","mode":"text_generate","prompt":"x","size":"1024*1024"}`)
	if w.Code != 403 || f.ports.mutations() != [6]int{} {
		t.Fatal("known read project create permission/status", w.Code, w.Body.String())
	}
}

func TestSourceIntegerSecondPaginationNanosecondTieIsStable(t *testing.T) {
	f := newSourceHTTPFixture(t)
	scope := si.Scope{AppID: "product-image", TenantID: f.actor.AccountID, ProjectID: f.project.ID, UserID: f.actor.UserID, PrincipalAccountID: f.actor.AccountID, PayerAccountID: f.actor.AccountID}
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	instants := []time.Time{base.Add(-time.Second), base.Add(100 * time.Millisecond), base.Add(100*time.Millisecond + time.Nanosecond), base.Add(100 * time.Millisecond), base.Add(time.Second)}
	savedNow := store.Now
	defer func() { store.Now = savedNow }()
	ids := []string{}
	for i, instant := range instants {
		store.Now = func() time.Time { return instant }
		in := si.Intent{Scope: scope, RequestKey: "nano-prefix-" + strconv.Itoa(i), Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "Time fixture", PricingVersion: "http-test-v1", Quantity: 1}
		r, dup, e := f.st.CreateSourceRunForProject(t.Context(), in)
		if e != nil || dup {
			t.Fatal(r, dup, e)
		}
		ids = append(ids, r.ID)
	}
	store.Now = savedNow
	// Source created_at is an INTEGER Unix second, not RFC3339Nano text. The three
	// .1/.100000001/same-instant inputs intentionally tie; ID defines their order.
	ties := append([]string(nil), ids[1:4]...)
	sort.Sort(sort.Reverse(sort.StringSlice(ties)))
	want := append([]string{ids[4]}, ties...)
	want = append(want, ids[0])
	actual := []string{}
	cursor := ""
	for i := 0; i < 4; i++ {
		path := f.path("?limit=2")
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		w := f.raw(t, "GET", path, f.token, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var page struct {
			Runs []struct {
				ID string `json:"run_id"`
			} `json:"runs"`
			Next string `json:"next_cursor"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil {
			t.Fatal(e)
		}
		for _, r := range page.Runs {
			actual = append(actual, r.ID)
		}
		cursor = page.Next
		if cursor == "" {
			break
		}
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			t.Fatal(e)
		}
		var transport struct {
			At string `json:"created_at"`
			ID string `json:"id"`
		}
		json.Unmarshal(raw, &transport)
		if transport.At != strconv.FormatInt(base.Unix(), 10) || transport.ID != page.Runs[len(page.Runs)-1].ID {
			t.Fatal("cursor lost integer-second/id tie", string(raw))
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("cross-page duplicate/omission or unstable tie", actual, want)
	}
	if f.ports.mutations() != [6]int{} {
		t.Fatal("time pagination wrote platform state")
	}
}

func TestSourceStartupReadinessProjectionIsReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name, reason   string
		ready, enabled bool
	}{{"joined", "", true, true}, {"creation_off", "", true, false}, {"host_missing", "source_download_hosts_unconfigured", false, true}, {"not_started", "", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSourceHTTPFixture(t)
			run := f.create(t)
			svc := f.srv.Source
			svc.OutputRecoveryReady = tc.ready
			svc.OutputRecoveryReason = tc.reason
			svc.Profile.Enabled = tc.enabled
			before := f.ports.mutations()
			old, e := f.st.GetSourceRun(t.Context(), run.Intent.Scope, run.ID)
			if e != nil {
				t.Fatal(e)
			}
			w := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
			var caps struct {
				Ready bool `json:"automatic_output_recovery_ready"`
				Modes []struct {
					Ready         bool   `json:"ready"`
					RuntimeReason string `json:"runtime_reason"`
				} `json:"modes"`
			}
			if e = json.Unmarshal(w.Body.Bytes(), &caps); e != nil {
				t.Fatal(e)
			}
			wantReason := tc.reason
			if !tc.ready && wantReason == "" {
				wantReason = "runtime_output_recovery_unconfigured"
			}
			if w.Code != 200 || caps.Ready != tc.ready || caps.Modes[0].Ready != (tc.ready && tc.enabled) || caps.Modes[0].RuntimeReason != wantReason {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, suffix := range []string{"/" + run.ID, "", "/by-request?request_key=" + run.Intent.RequestKey} {
				w = f.raw(t, "GET", f.path(suffix), f.token, "")
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), `"automatic_output_recovery_ready":`+strconv.FormatBool(tc.ready)) || !strings.Contains(w.Body.String(), `"automatic_output_recovery_reason":`+strconv.Quote(wantReason)) {
					t.Fatal("stale HTTP readiness projection", w.Body.String())
				}
			}
			got, e := f.st.GetSourceRun(t.Context(), run.Intent.Scope, run.ID)
			if e != nil || !reflect.DeepEqual(old, got) || before != f.ports.mutations() {
				t.Fatal("GET readiness changed original facts", e)
			}
		})
	}
}
