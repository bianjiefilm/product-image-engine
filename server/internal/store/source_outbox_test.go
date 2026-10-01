package store

import (
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sourceOutputRun(t *testing.T, s *Store, key string) si.Run {
	t.Helper()
	in := sourceIntent()
	in.RequestKey = key
	p, e := s.CreateProject(t.Context(), Project{TenantID: in.Scope.TenantID, CreatedBy: in.Scope.UserID, Name: "source", WidthPx: 1024, HeightPx: 1024, SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	in.Scope.ProjectID = p.ID
	r, _, e := s.CreateSourceRun(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	b := billForRun(r)
	b.UsageID = "usage-" + key
	r, e = s.SaveSourceBillObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	q := sourceQuote(r)
	q.UsageID = b.UsageID
	q.QuoteID = "quote-" + key
	q.QuotedAtUnix = Now().Unix()
	q.ExpiresAtUnix = q.QuotedAtUnix + 300
	r, e = s.SaveSourceQuoteUnderLease(t.Context(), r, q, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	r, e = s.ConfirmSourceRunForProject(t.Context(), r, si.QuoteHash(q), Now().Unix(), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	r, e = s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	o := si.Output{AssetID: "asset-" + key, ReferenceID: "ref-" + key, ProjectID: in.Scope.ProjectID, AppID: in.Scope.AppID, PrincipalType: "user", PrincipalID: in.Scope.UserID, SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 123}
	fact := si.TaskFact{ID: "task-" + key, Scope: in.Scope, IdempotencyKey: r.TaskKey, Quote: q, Provider: in.Provider, Capability: in.Capability, Status: "succeeded", Phase: "succeeded", HoldID: "hold-" + key, ChargeID: "charge-" + key, Output: &o}
	next := r
	next.Task = &fact
	next.TaskID = fact.ID
	next.Phase = "asset_pending"
	r, e = s.SaveSourceObservation(t.Context(), next, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func sourceChargedFact(r si.Run) si.BillFact {
	b := *r.Bill
	b.Quote = r.Quote
	b.HoldID = r.Task.HoldID
	b.ChargeID = r.Task.ChargeID
	b.Status = "charged"
	amount := r.Quote.AmountMinor
	b.ChargedMinor = &amount
	return b
}
func TestSourceOutputRequiresPersistedChargeAndFencedOriginalAsset(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "output")
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	if _, e := s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("unverified charge produced usable output")
	}
	r, e := s.SaveSourceSettlementObservation(t.Context(), r, sourceChargedFact(r), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	bad := o
	bad.ReferenceID = "other"
	if _, e = s.AttachSourceOutput(t.Context(), r, bad, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("wrong original reference accepted")
	}
	out, e := s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if e != nil || out.Output == nil || out.Output.AssetID != o.AssetID || out.Phase != "succeeded" || out.ChargedBill == nil {
		t.Fatal(out, e)
	}
	if _, e = s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("stale output lease", e)
	}
}
func TestSourceDeleteBeforeLateOutputQueuesOriginalReferenceWithoutResurrection(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "late")
	r, e := s.SaveSourceSettlementObservation(t.Context(), r, sourceChargedFact(r), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || !r.Deleted {
		t.Fatal(r, e)
	}
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	if _, e = s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("normal attach accepted deleted run", e)
	}
	for _, edit := range []func(*si.Run){func(r *si.Run) { r.Owner = "legacy" }, func(r *si.Run) { r.Intent.Scope.UserID = "other" }, func(r *si.Run) { r.Intent.Scope.TenantID = "other" }, func(r *si.Run) { r.Intent.Scope.ProjectID = "other" }, func(r *si.Run) { r.TaskID = "other" }, func(r *si.Run) { r.Revision++ }, func(r *si.Run) { r.LeaseEpoch++ }} {
		bad := r
		edit(&bad)
		if _, e = s.QueueDeletedSourceReference(t.Context(), bad, o, bad.Revision, bad.LeaseEpoch); e == nil {
			t.Fatal("foreign/stale tombstone accepted", bad)
		}
	}
	badOutput := o
	badOutput.ReferenceID = "foreign-reference"
	if _, e = s.QueueDeletedSourceReference(t.Context(), r, badOutput, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("foreign Upload reference queued")
	}
	out, e := s.QueueDeletedSourceReference(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if e != nil || !out.Deleted || out.Output != nil || out.Selected || out.Phase == "succeeded" {
		t.Fatal(out, e)
	}
	items, e := s.ListDueSourceOutbox(t.Context(), Now().Unix(), 10)
	if e != nil || len(items) != 1 || items[0].Output.ReferenceID != o.ReferenceID || items[0].Scope != r.Intent.Scope {
		t.Fatal(items, e)
	}
}

func TestSourceDeleteUnknownSettlementCannotQueueOrPoisonGenericSHA(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "unknown-late")
	r, e := s.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	if _, e = s.QueueDeletedSourceReference(t.Context(), r, o, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("unknown financial state released reference")
	}
	items, e := s.ListDueSourceOutbox(t.Context(), Now().Unix(), 10)
	if e != nil || len(items) != 0 {
		t.Fatal(items, e)
	}
	hit, e := s.HasVerifiedSourceOutput(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, o.SHA256, o.AssetID)
	if e != nil || hit {
		t.Fatal("unverified Task metadata blocked legacy path", hit, e)
	}
	r, e = s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || !r.Deleted || r.Output != nil || r.Selected || r.Phase == "succeeded" || r.ChargedBill != nil {
		t.Fatal(r, e)
	}
}

func TestSourceDeleteRefundedLateReferenceCannotBecomeUsable(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "refunded-late")
	charged := sourceChargedFact(r)
	r, e := s.SaveSourceSettlementObservation(t.Context(), r, charged, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	refund := charged
	refund.Status = "refunded"
	refund.RefundID = "refund-original"
	r, e = s.SaveSourceSettlementObservation(t.Context(), r, refund, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	if _, e = s.QueueDeletedSourceReference(t.Context(), r, o, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("nondeleted reference queued")
	}
	r, e = s.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("refund promoted usable output")
	}
	before := r
	out, e := s.QueueDeletedSourceReference(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if e != nil || out.Output != nil || out.Selected || !out.Deleted || out.Phase == "succeeded" || !reflect.DeepEqual(out.Bill, before.Bill) || !reflect.DeepEqual(out.ChargedBill, before.ChargedBill) {
		t.Fatal(out, e)
	}
	items, e := s.ListDueSourceOutbox(t.Context(), Now().Unix(), 10)
	if e != nil || len(items) != 1 || items[0].Output.ReferenceID != o.ReferenceID {
		t.Fatal(items, e)
	}
	if hit, e := s.HasVerifiedSourceOutput(t.Context(), out.Intent.Scope.TenantID, out.Intent.Scope.ProjectID, o.SHA256, o.AssetID); e != nil || !hit {
		t.Fatal("late verified tombstone lost source ownership", hit, e)
	}
}
func TestSourceOutputChargedHistorySurvivesRefundReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.Close() }()
	r := sourceOutputRun(t, s, "history")
	charged := sourceChargedFact(r)
	r, e = s.SaveSourceSettlementObservation(t.Context(), r, charged, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	first := r.ChargedBill
	refund := charged
	refund.Status = "refunded"
	refund.RefundID = "refund-history"
	r, e = s.SaveSourceSettlementObservation(t.Context(), r, refund, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	if r.Bill.Status != "refunded" || *r.Bill.ChargedMinor != r.Quote.AmountMinor || !reflect.DeepEqual(first, r.ChargedBill) {
		t.Fatal("original charge erased", r)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || !reflect.DeepEqual(first, r.ChargedBill) {
		t.Fatal(r, e)
	}
	if _, e = s.SaveSourceSettlementObservation(t.Context(), r, charged, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("refund recharged by observation")
	}
	wrong := refund
	wrong.ChargeID = "other"
	if _, e = s.SaveSourceSettlementObservation(t.Context(), r, wrong, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("charge retargeted")
	}
	again, e := s.SaveSourceSettlementObservation(t.Context(), r, refund, r.Revision, r.LeaseEpoch)
	if e != nil || !reflect.DeepEqual(first, again.ChargedBill) || again.Bill.RefundID != refund.RefundID {
		t.Fatal(again, e)
	}
}

func TestSourceOutputGenericGuardOnlyVerifiedOriginalProject(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "guard")
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	assertHit := func(tenant, project, sha, asset string, want bool) {
		t.Helper()
		hit, e := s.HasVerifiedSourceOutput(t.Context(), tenant, project, sha, asset)
		if e != nil || hit != want {
			t.Fatal(hit, e, tenant, project)
		}
	}
	assertHit(r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, o.SHA256, "", false)
	r, e := s.SaveSourceSettlementObservation(t.Context(), r, sourceChargedFact(r), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	// A Bill and a client/Task SHA do not prove Upload verification.
	assertHit(r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, o.SHA256, "", false)
	r, e = s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	assertHit(r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, o.SHA256, "", true)
	assertHit(r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, "", o.AssetID, true)
	assertHit("other-tenant", r.Intent.Scope.ProjectID, o.SHA256, o.AssetID, false)
	assertHit(r.Intent.Scope.TenantID, "other-project", o.SHA256, o.AssetID, false)
	r, e = s.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	assertHit(r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, o.SHA256, "", true)
}

func TestSourceReleaseFullIdentityAndEpochFences(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "release-fence")
	r, e := s.SaveSourceSettlementObservation(t.Context(), r, sourceChargedFact(r), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	r, e = s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	items, e := s.ListDueSourceOutbox(t.Context(), Now().Unix(), 10)
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	item, ok, e := s.ClaimSourceOutbox(t.Context(), items[0].ID, Now().Unix(), 30)
	if e != nil || !ok {
		t.Fatal(item, e)
	}
	if _, ok, e := s.ClaimSourceOutbox(t.Context(), item.ID, Now().Unix(), 30); e != nil || ok {
		t.Fatal("duplicate claim", ok, e)
	}
	for _, edit := range []func(*si.Outbox){func(o *si.Outbox) { o.Scope.UserID = "other" }, func(o *si.Outbox) { o.Scope.TenantID = "other" }, func(o *si.Outbox) { o.Scope.ProjectID = "other" }, func(o *si.Outbox) { o.Output.AssetID = "other" }, func(o *si.Outbox) { o.Output.SHA256 = strings.Repeat("b", 64) }, func(o *si.Outbox) { o.State = "done" }, func(o *si.Outbox) { o.Reason = "other" }} {
		bad := item
		edit(&bad)
		bad.Output.ReferenceState = "released"
		if e = s.FinishSourceRelease(t.Context(), bad, item.LeaseEpoch); e == nil {
			t.Fatal("mutated identity/state accepted", bad)
		}
	}
	if e = s.FinishSourceRelease(t.Context(), item, item.LeaseEpoch); e == nil {
		t.Fatal("active reference completed")
	}
	if e = s.SaveSourceOutboxError(t.Context(), item, item.LeaseEpoch+1, "reference_unknown", Now().Unix()+5); e == nil {
		t.Fatal("stale error epoch accepted")
	}
	item.Output.ReferenceState = "released"
	if e = s.FinishSourceRelease(t.Context(), item, item.LeaseEpoch+1); e == nil {
		t.Fatal("wrong epoch completed")
	}
	if e = s.FinishSourceRelease(t.Context(), item, item.LeaseEpoch); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishSourceRelease(t.Context(), item, item.LeaseEpoch); e == nil {
		t.Fatal("recompleted fenced item")
	}
	if _, ok, e = s.ClaimSourceOutbox(t.Context(), item.ID, Now().Unix()+31, 30); e != nil || ok {
		t.Fatal("done claimed", ok, e)
	}
}

func TestSourceOutputSettlementRejectsForeignStaleAndAmounts(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "settlement-fences")
	b := sourceChargedFact(r)
	for _, edit := range []func(*si.BillFact){func(b *si.BillFact) { b.Scope.UserID = "other" }, func(b *si.BillFact) { b.Scope.TenantID = "other" }, func(b *si.BillFact) { b.Scope.ProjectID = "other" }, func(b *si.BillFact) { b.UsageID = "other" }, func(b *si.BillFact) { b.ChargeID = "other" }, func(b *si.BillFact) { n := int64(0); b.ChargedMinor = &n }, func(b *si.BillFact) { b.RefundID = "other" }, func(b *si.BillFact) { b.ReleaseID = "other" }} {
		bad := b
		edit(&bad)
		if _, e := s.SaveSourceSettlementObservation(t.Context(), r, bad, r.Revision, r.LeaseEpoch); e == nil {
			t.Fatal("foreign settlement accepted", bad)
		}
	}
	if _, e := s.SaveSourceSettlementObservation(t.Context(), r, b, r.Revision+1, r.LeaseEpoch); e == nil {
		t.Fatal("stale revision accepted")
	}
	if _, e := s.SaveSourceSettlementObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch+1); e == nil {
		t.Fatal("stale epoch accepted")
	}
	if _, e := s.SaveSourceSettlementObservation(t.Context(), r, b, r.Revision, r.LeaseEpoch); e != nil {
		t.Fatal(e)
	}
}

func TestSourceOutputArchiveRetainsOriginalReference(t *testing.T) {
	s := openTest(t)
	r := sourceOutputRun(t, s, "archive")
	r, e := s.SaveSourceSettlementObservation(t.Context(), r, sourceChargedFact(r), r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	r, e = s.AttachSourceOutput(t.Context(), r, o, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	status := "archived"
	if _, e = s.UpdateProject(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, ProjectUpdate{Status: &status}); e != nil {
		t.Fatal(e)
	}
	r, e = s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || r.Deleted || r.Output == nil || *r.Output != o {
		t.Fatal(r, e)
	}
	items, e := s.ListDueSourceOutbox(t.Context(), Now().Unix(), 10)
	if e != nil || len(items) != 0 {
		t.Fatal("archive released reference", items, e)
	}
	if _, e = s.DeleteSourceOutput(t.Context(), r.Intent.Scope, r.ID); !errors.Is(e, si.ErrForbidden) {
		t.Fatal("archive write allowed", e)
	}
}
