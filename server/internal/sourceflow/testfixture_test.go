package sourceflow

import (
	"context"
	"fmt"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"io"
	"path/filepath"
	"testing"
	"time"
)

// These ports model a remote durable authority; reopening product SQLite retains
// remote facts. They are test-only, never a runtime fallback or identity bridge.
type billFixture struct {
	ReadError                                                error
	facts                                                    map[string]si.BillFact
	AfterCommit                                              string
	UsageAttempts, UsageCommits, QuoteAttempts, QuoteCommits int
	Lookups, Gets                                            int
	now                                                      func() time.Time
	BeforeGet                                                func()
	Mutate                                                   func(*si.BillFact)
	store                                                    *store.Store
}

func billKey(r si.Run) string { return fmt.Sprintf("%q/%q", r.Intent.Scope, r.UsageKey) }
func (b *billFixture) Lookup(ctx context.Context, r si.Run) (si.BillFact, bool, error) {
	b.Lookups++
	return b.read(r)
}
func (b *billFixture) Get(ctx context.Context, r si.Run) (si.BillFact, bool, error) {
	b.Gets++
	if b.BeforeGet != nil {
		b.BeforeGet()
	}
	return b.read(r)
}
func (b *billFixture) read(r si.Run) (si.BillFact, bool, error) {
	if b.ReadError != nil {
		return si.BillFact{}, false, b.ReadError
	}
	f, ok := b.facts[billKey(r)]
	if f.Quote != nil {
		q := *f.Quote
		f.Quote = &q
	}
	if b.Mutate != nil {
		b.Mutate(&f)
	}
	return f, ok, nil
}
func (b *billFixture) Usage(ctx context.Context, r si.Run) error {
	b.UsageAttempts++
	// Product intent must already be durable before even the first remote write.
	if saved, e := b.store.GetSourceRun(ctx, r.Intent.Scope, r.ID); e != nil || saved.UsageKey != r.UsageKey {
		return si.ErrInvariant
	}
	k := billKey(r)
	if _, ok := b.facts[k]; !ok {
		b.UsageCommits++
		b.facts[k] = si.BillFact{Scope: r.Intent.Scope, UsageID: fmt.Sprintf("usage-%d", b.UsageCommits), UsageKey: r.UsageKey, Capability: r.Intent.Capability, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Status: "ingested", Quantity: 1}
	}
	if b.AfterCommit == "usage" {
		b.AfterCommit = ""
		return si.ErrUnavailable
	}
	return nil
}
func (b *billFixture) Quote(ctx context.Context, r si.Run) error {
	b.QuoteAttempts++
	k := billKey(r)
	f, ok := b.facts[k]
	if !ok || r.Bill == nil || r.Bill.UsageID != f.UsageID {
		return si.ErrInvariant
	}
	if f.Quote == nil {
		b.QuoteCommits++
		q := si.Quote{UsageID: f.UsageID, UsageKey: r.UsageKey, QuoteID: fmt.Sprintf("quote-%d", b.QuoteCommits), PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: b.now().Unix(), ExpiresAtUnix: b.now().Unix() + 300}
		f.Quote = &q
		f.Status = "quoted"
		b.facts[k] = f
	}
	if b.AfterCommit == "quote" {
		b.AfterCommit = ""
		return si.ErrUnavailable
	}
	return nil
}

type taskFixture struct{ Submits, Commits, Dispatches int }

func (f *taskFixture) Lookup(context.Context, si.Run) (si.TaskFact, bool, error) {
	return si.TaskFact{}, false, nil
}
func (f *taskFixture) Get(context.Context, si.Run) (si.TaskFact, bool, error) {
	return si.TaskFact{}, false, nil
}
func (f *taskFixture) Submit(context.Context, si.Run) error    { f.Submits++; return si.ErrUnavailable }
func (f *taskFixture) Cancel(context.Context, si.Run) error    { return si.ErrUnavailable }
func (f *taskFixture) Reconcile(context.Context, si.Run) error { return si.ErrUnavailable }

type assetFixture struct{}

func (*assetFixture) Verify(context.Context, si.Run) (si.Output, error) {
	return si.Output{}, si.ErrUnavailable
}
func (*assetFixture) Reference(context.Context, si.Outbox) (string, error) {
	return "", si.ErrUnavailable
}
func (*assetFixture) Release(context.Context, si.Outbox) error { return si.ErrUnavailable }
func (*assetFixture) Download(context.Context, si.Run) (io.ReadCloser, error) {
	return nil, si.ErrUnavailable
}

type fixture struct {
	store   *store.Store
	svc     *Service
	actor   Actor
	project store.Project
	bill    *billFixture
	task    *taskFixture
	assets  *assetFixture
	context *authContext
	path    string
	now     time.Time
}

func flowFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{actor: Actor{UserID: "usr-a", AccountID: "acct-a"}, path: filepath.Join(t.TempDir(), "source-flow.db"), now: time.Now().UTC()}
	f.bill = &billFixture{facts: map[string]si.BillFact{}, now: func() time.Time { return f.now }}
	f.task = &taskFixture{}
	f.assets = &assetFixture{}
	f.context = &authContext{err: si.ErrForbidden}
	f.open(t)
	f.project = authProject(t, f.store, f.actor.AccountID, f.actor.UserID)
	t.Cleanup(func() { f.store.Close() })
	return f
}
func (f *fixture) open(t *testing.T) {
	t.Helper()
	s, e := store.Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	f.store = s
	f.bill.store = s
	f.svc = &Service{Store: s, Auth: &Authorizer{Store: s, Context: f.context, AppID: "product-image"}, Bill: f.bill, Tasks: f.task, Assets: f.assets, Profile: Profile{Enabled: true, Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Quantity: 1, PricingVersion: "test-price-v1"}, Now: func() time.Time { return f.now }}
}
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	if e := f.store.Close(); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(31 * time.Second)
	f.open(t)
}
func request() NewRunRequest {
	return NewRunRequest{RequestKey: "request-a", Mode: "text_generate", Prompt: "蓝纸盒", Size: "1024*1024"}
}
func (f *fixture) quoted(t *testing.T) si.Run {
	t.Helper()
	r, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	return r
}
func (f *fixture) confirmed(t *testing.T) si.Run {
	t.Helper()
	r := f.quoted(t)
	out, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil {
		t.Fatal(out, e)
	}
	return out
}
