package store

import (
	"encoding/json"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"path/filepath"
	"testing"
)

func billForRun(r si.Run) si.BillFact {
	return si.BillFact{Scope: r.Intent.Scope, UsageID: "usage-original", UsageKey: r.UsageKey, Capability: r.Intent.Capability, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Status: "ingested", Quantity: 1}
}
func leaseForQuote(t *testing.T, s *Store, r si.Run) si.Run {
	t.Helper()
	out, ok, e := s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix(), 30)
	if e != nil || !ok {
		t.Fatal(out, ok, e)
	}
	return out
}
func TestSourceUsageDurableBeforeQuoteAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.Close() }()
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	b := billForRun(r)
	saved, e := s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch)
	if e != nil || saved.Bill == nil || saved.Quote != nil || saved.Bill.UsageID != b.UsageID {
		t.Fatal(saved, e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || got.Bill == nil || got.Bill.UsageID != b.UsageID || got.Quote != nil {
		t.Fatal(got, e)
	}
	q := sourceQuote(got)
	q.UsageID = "another"
	if _, e = s.SaveSourceQuoteUnderLease(t.Context(), got, q, got.Revision, got.LeaseEpoch); e == nil {
		t.Fatal("retargeted durable usage")
	}
}
func TestSourceUsageObservationRejectsForeignTupleAndStaleLease(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	edits := []func(*si.BillFact){func(b *si.BillFact) { b.Scope.UserID = "other" }, func(b *si.BillFact) { b.UsageKey = "other" }, func(b *si.BillFact) { b.BusinessRef = "other" }, func(b *si.BillFact) { b.Quantity = 2 }, func(b *si.BillFact) { b.PricingVersion = "other" }, func(b *si.BillFact) { b.Capability = "other" }, func(b *si.BillFact) { b.HoldID = "fabricated-hold" }, func(b *si.BillFact) { b.UsageID = "" }}
	for i, edit := range edits {
		b := billForRun(r)
		edit(&b)
		if _, e = s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch); e == nil {
			t.Fatal(i, "bad fact accepted")
		}
	}
	newer, ok, e := s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix()+31, 30)
	if e != nil || !ok {
		t.Fatal(e)
	}
	if _, e = s.SaveSourceBillObservation(t.Context(), r, billForRun(r), r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("stale writer", e)
	}
	saved, e := s.SaveSourceBillObservation(t.Context(), newer, billForRun(newer), newer.Revision, newer.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveSourceBillObservation(t.Context(), newer, billForRun(newer), newer.Revision, newer.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("stale revision", saved, e)
	}
}
func TestSourceQuoteUnderLeaseFencesFrozenQuote(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	b := billForRun(r)
	r, e = s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	q := sourceQuote(r)
	q.UsageID = b.UsageID
	r, e = s.SaveSourceQuoteUnderLease(t.Context(), r, q, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	newer, ok, e := s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix()+31, 30)
	if e != nil || !ok {
		t.Fatal(e)
	}
	changed := q
	changed.QuoteID = "changed"
	if _, e = s.SaveSourceQuoteUnderLease(t.Context(), r, changed, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.SaveSourceQuoteUnderLease(t.Context(), newer, changed, newer.Revision, newer.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
}
func TestSourceConfirmTransactionRejectsArchivedOrWrongOwner(t *testing.T) {
	for _, kind := range []string{"archived", "wrong-owner", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			s := openTest(t)
			in := sourceIntent()
			p, e := s.CreateProject(t.Context(), Project{TenantID: in.Scope.TenantID, CreatedBy: in.Scope.UserID, Name: "source", WidthPx: 800, HeightPx: 800, SourceType: "standalone"})
			if e != nil {
				t.Fatal(e)
			}
			in.Scope.ProjectID = p.ID
			r, _, e := s.CreateSourceRun(t.Context(), in)
			if e != nil {
				t.Fatal(e)
			}
			q := sourceQuote(r)
			r, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
			if e != nil {
				t.Fatal(e)
			}
			r = leaseForQuote(t, s, r)
			if kind == "wrong-owner" {
				_, e = s.db.Exec(`UPDATE projects SET created_by='other' WHERE id=?`, p.ID)
			} else {
				_, e = s.UpdateProject(t.Context(), p.TenantID, p.ID, ProjectUpdate{Status: &kind})
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.ConfirmSourceRunForProject(t.Context(), r, si.QuoteHash(q), 1100, r.Revision, r.LeaseEpoch); e == nil {
				t.Fatal("confirmed inaccessible project")
			}
			got, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if e != nil || got.ConfirmationHash != "" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestSourceUsageBeforeQuoteUniqueBinding(t *testing.T) {
	s := openTest(t)
	a, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	a = leaseForQuote(t, s, a)
	a, e = s.SaveSourceBillObservation(t.Context(), a, billForRun(a), a.Revision, a.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	in := sourceIntent()
	in.RequestKey = "another"
	b, _, e := s.CreateSourceRun(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	b = leaseForQuote(t, s, b)
	if _, e = s.SaveSourceBillObservation(t.Context(), b, billForRun(b), b.Revision, b.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("second run bound same upstream usage", e)
	}
}

func TestSourceConfirmTransactionRejectsTombstoneAndStaleLease(t *testing.T) {
	for _, kind := range []string{"deleted", "canceled", "stale", "mutatedscope"} {
		t.Run(kind, func(t *testing.T) {
			s := openTest(t)
			in := sourceIntent()
			p, e := s.CreateProject(t.Context(), Project{TenantID: in.Scope.TenantID, CreatedBy: in.Scope.UserID, Name: "source", WidthPx: 800, HeightPx: 800, SourceType: "standalone"})
			if e != nil {
				t.Fatal(e)
			}
			in.Scope.ProjectID = p.ID
			r, _, e := s.CreateSourceRun(t.Context(), in)
			if e != nil {
				t.Fatal(e)
			}
			q := sourceQuote(r)
			r, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
			if e != nil {
				t.Fatal(e)
			}
			r = leaseForQuote(t, s, r)
			switch kind {
			case "deleted":
				_, e = s.db.Exec(`UPDATE product_source_runs SET deleted=1, revision=revision+1 WHERE id=?`, r.ID)
			case "canceled":
				_, e = s.db.Exec(`UPDATE product_source_runs SET cancel_requested=1, revision=revision+1 WHERE id=?`, r.ID)
			case "stale":
				_, _, e = s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix()+31, 30)
			case "mutatedscope":
				r.Intent.Scope.PayerAccountID = "other"
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.ConfirmSourceRunForProject(t.Context(), r, si.QuoteHash(q), 1100, r.Revision, r.LeaseEpoch); e == nil {
				t.Fatal("confirmed inaccessible/stale request", kind)
			}
		})
	}
}

func TestSourceUsageObservationCannotEraseObservedQuote(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	b := billForRun(r)
	q := sourceQuote(r)
	q.UsageID = b.UsageID
	b.Status = "quoted"
	b.Quote = &q
	r, e = s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	downgraded := b
	downgraded.Status = "ingested"
	downgraded.Quote = nil
	if _, e = s.SaveSourceBillObservation(t.Context(), r, downgraded, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("observed original quote erased")
	}
	changed := q
	changed.UnitPriceMinor = 26
	changed.AmountMinor = 26
	b.Quote = &changed
	if _, e = s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("observed original quote repriced")
	}
}

func TestSourceQuoteScanRejectsChangedObservedBillTuple(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	b := billForRun(r)
	q := sourceQuote(r)
	q.UsageID = b.UsageID
	b.Status = "quoted"
	b.Quote = &q
	r, e = s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.SaveSourceQuoteUnderLease(t.Context(), r, q, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	b.Scope.AppID = "other-app"
	raw, e := json.Marshal(sourceObservation{Bill: &b})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE product_source_runs SET observation_json=? WHERE id=?`, string(raw), r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID); !errors.Is(e, si.ErrInvariant) {
		t.Fatal("read wrong persisted Bill tuple", e)
	}
}
