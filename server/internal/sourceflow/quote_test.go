package sourceflow

import (
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"testing"
	"time"
)

func TestSourceUsageLostResponseKeepsOriginalRun(t *testing.T) {
	f := flowFixture(t)
	f.bill.AfterCommit = "usage"
	a, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e == nil || a.ID == "" {
		t.Fatal(a, e)
	}
	f.reopen(t)
	b, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e != nil || a.ID != b.ID || a.UsageKey != b.UsageKey || b.Quote == nil || f.bill.UsageCommits != 1 || f.bill.UsageAttempts != 1 || f.task.Submits != 0 {
		t.Fatal(b, e, f.bill)
	}
}
func TestSourceQuoteLostResponseRecoversFrozenExpiry(t *testing.T) {
	f := flowFixture(t)
	f.bill.AfterCommit = "quote"
	a, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e == nil || a.ID == "" {
		t.Fatal(a, e)
	}
	original := f.bill.facts[billKey(a)].Quote
	f.reopen(t)
	b, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e != nil || b.ID != a.ID || b.Quote == nil || b.RetryAt != 0 || b.LastError != "" || *b.Quote != *original || f.bill.QuoteAttempts != 1 || f.bill.QuoteCommits != 1 || f.task.Submits != 0 {
		t.Fatal(b, e)
	}
}
func TestSourceConfirmExplicitAndIdempotent(t *testing.T) {
	f := flowFixture(t)
	r := f.quoted(t)
	if r.ConfirmationHash != "" || f.task.Submits != 0 {
		t.Fatal(r)
	}
	a, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || a.ConfirmationHash == "" || a.Phase != "confirmed" {
		t.Fatal(a, e)
	}
	before := f.bill.QuoteAttempts
	b, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || b.ConfirmationHash != a.ConfirmationHash || b.ConfirmedAt != a.ConfirmedAt || before != f.bill.QuoteAttempts || f.task.Submits != 0 {
		t.Fatal(b, e)
	}
	if _, e = f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, "wrong-hash"); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
}
func TestSourceQuoteExpiredCannotRepriceSameRun(t *testing.T) {
	f := flowFixture(t)
	r := f.quoted(t)
	f.now = f.now.Add(300 * time.Second)
	if _, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	b, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e != nil || b.Quote == nil || *b.Quote != *r.Quote || f.bill.QuoteAttempts != 1 || f.task.Submits != 0 {
		t.Fatal(b, e)
	}
	req := request()
	req.RequestKey = "request-explicit-new"
	c, e := f.svc.Create(t.Context(), f.actor, f.project.ID, req)
	if e != nil || c.ID == r.ID || c.Quote == nil || c.Quote.QuoteID == r.Quote.QuoteID {
		t.Fatal(c, e)
	}
}
func TestSourceUsageReplayChangedPromptOrPricingConflicts(t *testing.T) {
	f := flowFixture(t)
	f.quoted(t)
	req := request()
	req.Prompt = "different"
	if _, e := f.svc.Create(t.Context(), f.actor, f.project.ID, req); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	f.svc.Profile.PricingVersion = "new-version"
	if _, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request()); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	if f.bill.UsageCommits != 1 || f.task.Submits != 0 {
		t.Fatal(f.bill)
	}
}
func TestSourceQuoteMalformedFactCannotConfirm(t *testing.T) {
	edits := []func(*si.BillFact){func(b *si.BillFact) { b.Scope.UserID = "other" }, func(b *si.BillFact) { b.UsageKey = "other" }, func(b *si.BillFact) { b.BusinessRef = "other" }, func(b *si.BillFact) { b.Quantity = 2 }, func(b *si.BillFact) { b.Quote.AmountMinor = -1 }, func(b *si.BillFact) { b.Quote.ExpiresAtUnix++ }, func(b *si.BillFact) { b.Quote.UsageID = "other" }}
	for i, edit := range edits {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			f := flowFixture(t)
			f.bill.AfterCommit = "quote"
			r, _ := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
			f.now = f.now.Add(31 * time.Second)
			f.bill.Mutate = edit
			got, e := f.svc.RecoverQuote(t.Context(), r)
			if e == nil || got.Quote != nil || f.task.Submits != 0 {
				t.Fatal(got, e)
			}
		})
	}
}
func TestSourceConfirmFreshBillUnknownOrHoldDenies(t *testing.T) {
	f := flowFixture(t)
	r := f.quoted(t)
	f.bill.Mutate = func(b *si.BillFact) { b.HoldID = "unexpected-hold"; b.Status = "held" }
	if _, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e == nil {
		t.Fatal("confirmed after hold")
	}
	if saved, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID); e != nil || saved.ConfirmationHash != "" || f.task.Submits != 0 {
		t.Fatal(saved, e)
	}
}
func TestSourceConfirmArchiveRaceCloses(t *testing.T) {
	f := flowFixture(t)
	r := f.quoted(t)
	f.bill.BeforeGet = func() {
		status := "archived"
		if _, e := f.store.UpdateProject(t.Context(), f.project.TenantID, f.project.ID, store.ProjectUpdate{Status: &status}); e != nil {
			t.Fatal(e)
		}
	}
	if out, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e == nil || out.ID != r.ID {
		t.Fatal("confirmed concurrently archived project or lost durable run", out, e)
	}
	if saved, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID); e != nil || saved.ConfirmationHash != "" {
		t.Fatal(saved, e)
	}
}
func TestSourceQuoteProfileClosedMakesNoUsage(t *testing.T) {
	f := flowFixture(t)
	f.svc.Profile.Enabled = false
	if _, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request()); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(e)
	}
	if f.bill.UsageAttempts != 0 || f.task.Submits != 0 {
		t.Fatal(f.bill)
	}
}

func TestSourceUsageUnknownLookupDoesNotWrite(t *testing.T) {
	f := flowFixture(t)
	f.bill.ReadError = si.ErrUnavailable
	r, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e == nil || r.ID == "" || f.bill.UsageAttempts != 0 || f.bill.QuoteAttempts != 0 || f.task.Submits != 0 {
		t.Fatal(r, e)
	}
	if saved, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID); e != nil || saved.LastError == "" {
		t.Fatal(saved, e)
	}
}
func TestSourceConfirmBillUnavailableKeepsFrozenQuote(t *testing.T) {
	f := flowFixture(t)
	r := f.quoted(t)
	f.bill.ReadError = si.ErrUnavailable
	if _, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e == nil {
		t.Fatal("confirmed unavailable Bill")
	}
	saved, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || saved.Quote == nil || *saved.Quote != *r.Quote || saved.ConfirmationHash != "" || f.task.Submits != 0 {
		t.Fatal(saved, e)
	}
}
func TestSourceConfirmRevokedOrganizationMakesNoBillWrite(t *testing.T) {
	f := flowFixture(t)
	f.project = authProject(t, f.store, "org-a", "usr-project-owner")
	f.svc.Profile.OrgEnabled = true
	f.context.err = nil
	f.context.fact = si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: f.actor.UserID, TenantID: "org-a", Role: "editor", PayerSource: "delegation", PayerAccountID: "org-payer", MemberRole: "payer"}
	r := f.quoted(t)
	before := f.bill.Gets
	f.context.fact.Resolved = false
	if _, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); !errors.Is(e, si.ErrNotFound) {
		t.Fatal(e)
	}
	if f.bill.Gets != before || f.task.Submits != 0 {
		t.Fatal("revocation used cached authorization")
	}
}

func TestSourceQuoteRecoveryRejectsRetargetedCaller(t *testing.T) {
	f := flowFixture(t)
	f.bill.AfterCommit = "usage"
	r, e := f.svc.Create(t.Context(), f.actor, f.project.ID, request())
	if e == nil {
		t.Fatal("expected response loss")
	}
	r.Intent.Scope.PayerAccountID = "other"
	f.now = f.now.Add(31 * time.Second)
	if _, e = f.svc.RecoverQuote(t.Context(), r); e == nil {
		t.Fatal("accepted mutated original run")
	}
	if f.bill.UsageAttempts != 1 || f.bill.QuoteAttempts != 0 || f.task.Submits != 0 {
		t.Fatal("mutated owner caused external call")
	}
}

func TestSourceConfirmFreshOrganizationCollaborator(t *testing.T) {
	f := flowFixture(t)
	f.project = authProject(t, f.store, "org-a", "usr-project-owner")
	f.svc.Profile.OrgEnabled = true
	f.context.err = nil
	f.context.fact = si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: f.actor.UserID, TenantID: "org-a", Role: "editor", PayerSource: "delegation", PayerAccountID: "org-payer", MemberRole: "payer"}
	r := f.quoted(t)
	before := len(f.context.queries)
	out, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || out.ConfirmationHash == "" || out.Intent.Scope.PayerAccountID != "org-payer" || len(f.context.queries) <= before || f.task.Submits != 0 {
		t.Fatal(out, e)
	}
	for _, q := range f.context.queries[before:] {
		if q.ReadOnly || q.TenantID != "org-a" {
			t.Fatal("confirm lacked explicit fresh context", q)
		}
	}
}
