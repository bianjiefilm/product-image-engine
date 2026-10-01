package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"io"
	"strings"
	"testing"
	"time"
)

type outputAuthority struct {
	output                                    si.Output
	verifyError, referenceError               error
	state                                     string
	lostRelease                               bool
	releaseNoChange                           bool
	verifies, downloads, releases, references int
	items                                     []si.Outbox
}

func (a *outputAuthority) Verify(context.Context, si.Run) (si.Output, error) {
	a.verifies++
	return a.output, a.verifyError
}
func (a *outputAuthority) Reference(_ context.Context, o si.Outbox) (string, error) {
	a.references++
	a.items = append(a.items, o)
	return a.state, a.referenceError
}
func (a *outputAuthority) Release(_ context.Context, o si.Outbox) error {
	a.releases++
	a.items = append(a.items, o)
	if !a.releaseNoChange {
		a.state = "released"
	}
	if a.lostRelease {
		a.lostRelease = false
		return si.ErrUnavailable
	}
	return nil
}
func (a *outputAuthority) Download(context.Context, si.Run) (io.ReadCloser, error) {
	a.downloads++
	return io.NopCloser(strings.NewReader("verified-bytes")), nil
}

func sourceReadyForOutput(t *testing.T, f *fixture) (si.Run, *outputAuthority) {
	t.Helper()
	r := sourceSucceededForOutput(t, f)
	b := f.bill.facts[billKey(r)]
	b.Status = "charged"
	b.HoldID = r.Task.HoldID
	b.ChargeID = r.Task.ChargeID
	n := int64(25)
	b.ChargedMinor = &n
	f.bill.facts[billKey(r)] = b
	o := *r.Task.Output
	o.Status = "ready"
	o.ReferenceState = "active"
	a := &outputAuthority{output: o, state: "active"}
	f.svc.Assets = a
	return r, a
}

func TestSourceOutputOriginalAssociationSelectAndCurrentRefund(t *testing.T) {
	f := flowFixture(t)
	r, a := sourceReadyForOutput(t, f)
	r, e := f.svc.ObserveOutput(t.Context(), r)
	if e != nil || r.Phase != "succeeded" || r.Output == nil || r.ChargedBill == nil {
		t.Fatal(r, e)
	}
	old, e := f.store.ListOutputs(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID)
	if e != nil || len(old) != 0 {
		t.Fatal("legacy rows created", old, e)
	}
	r, e = f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID)
	if e != nil || !r.Selected {
		t.Fatal(r, e)
	}
	body, e := f.svc.OpenOutput(t.Context(), f.actor, f.project.ID, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	body.Close()
	first := *r.ChargedBill
	b := f.bill.facts[billKey(r)]
	b.Status = "refunded"
	b.RefundID = "refund-source"
	f.bill.facts[billKey(r)] = b
	if body, e = f.svc.OpenOutput(t.Context(), f.actor, f.project.ID, r.ID); e == nil || body != nil || a.downloads != 1 {
		t.Fatal("refund reused old charge", e, a.downloads)
	}
	r, e = f.svc.ObserveOutput(t.Context(), r)
	if e == nil || r.Bill.Status != "refunded" || r.Selected || r.Phase == "succeeded" || r.ChargedBill.ChargeID != first.ChargeID || *r.ChargedBill.ChargedMinor != 25 || *r.Bill.ChargedMinor != 25 {
		t.Fatal(r, e)
	}
	if f.task.Submits != 0 || f.bill.UsageAttempts != 1 || f.bill.QuoteAttempts != 1 || a.releases != 0 {
		t.Fatal("output caused dispatch/funds/ref release")
	}
}

func TestSourceOutputWrongOriginalProofAndFreshTimeoutPreserveReceipt(t *testing.T) {
	f := flowFixture(t)
	r, a := sourceReadyForOutput(t, f)
	r, e := f.svc.ObserveOutput(t.Context(), r)
	if e != nil {
		t.Fatal(e)
	}
	a.output.ReferenceID = "foreign"
	if _, e = f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); e == nil {
		t.Fatal("wrong reference selected")
	}
	r, e = f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(31 * time.Second)
	f.bill.ReadError = si.ErrUnavailable
	out, e := f.svc.ObserveOutput(t.Context(), r)
	if e == nil || out.Output == nil || out.ChargedBill == nil || *out.ChargedBill.ChargedMinor != 25 || out.LastError == "" {
		t.Fatal("receipt lost on refresh timeout", out, e)
	}
}

func TestSourceDeleteLateRefundedOutputReleaseLostReplyReopen(t *testing.T) {
	f := flowFixture(t)
	r, a := sourceReadyForOutput(t, f)
	b := f.bill.facts[billKey(r)]
	b.Status = "refunded"
	b.RefundID = "refund-late"
	f.bill.facts[billKey(r)] = b
	r, e := f.svc.DeleteOutput(t.Context(), f.actor, f.project.ID, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.svc.ObserveOutput(t.Context(), r)
	if e != nil || r.Output != nil || r.Selected || !r.Deleted || r.Phase == "succeeded" || r.ChargedBill != nil || r.Bill.RefundID != "refund-late" || *r.Bill.ChargedMinor != 25 {
		t.Fatal(r, e)
	}
	items, e := f.store.ListDueSourceOutbox(t.Context(), f.now.Unix(), 10)
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	id := items[0].ID
	a.lostRelease = true
	if e = f.svc.DrainReferenceOutbox(t.Context()); !errors.Is(e, si.ErrUnavailable) {
		t.Fatal(e)
	}
	f.reopen(t)
	f.svc.Assets = a
	items, e = f.store.ListDueSourceOutbox(t.Context(), f.now.Unix(), 10)
	if e != nil || len(items) != 1 || items[0].ID != id || items[0].Attempts != 1 {
		t.Fatal(items, e)
	}
	if e = f.svc.DrainReferenceOutbox(t.Context()); e != nil {
		t.Fatal(e)
	}
	if a.releases != 1 || a.references != 2 {
		t.Fatal("lost reply resubmitted release", a.releases, a.references)
	}
	for _, item := range a.items {
		if item.ID != id || item.Scope != r.Intent.Scope || item.Output.AssetID != r.Task.Output.AssetID || item.Output.ReferenceID != r.Task.Output.ReferenceID {
			t.Fatal("original identity changed", item)
		}
	}
	r, e = f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || r.Output != nil || !r.Deleted || r.Selected || r.Phase == "succeeded" {
		t.Fatal(r, e)
	}
	if _, e = f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); e == nil {
		t.Fatal("deleted run selected")
	}
}

func TestSourceReleaseOnlyFinishesActualReleasedGET(t *testing.T) {
	for _, state := range []string{"active", "active_after_release", "unknown", ""} {
		name := state
		if name == "" {
			name = "empty_state"
		}
		t.Run(name, func(t *testing.T) {
			f := flowFixture(t)
			r, a := sourceReadyForOutput(t, f)
			r, e := f.svc.ObserveOutput(t.Context(), r)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.svc.DeleteOutput(t.Context(), f.actor, f.project.ID, r.ID); e != nil {
				t.Fatal(e)
			}
			a.state = state
			if state == "active_after_release" {
				a.state = "active"
				a.releaseNoChange = true
			}
			if state == "active" {
				a.referenceError = si.ErrUnavailable
			}
			if e = f.svc.DrainReferenceOutbox(t.Context()); e == nil {
				t.Fatal("unknown reference completed")
			}
			if a.releases != 0 && state != "active_after_release" {
				t.Fatal("unknown GET released reference")
			}
			if state == "active_after_release" && (a.releases != 1 || a.references != 2) {
				t.Fatal("release result replaced actual GET", a.releases, a.references)
			}
			f.now = f.now.Add(31 * time.Second)
			items, e := f.store.ListDueSourceOutbox(t.Context(), f.now.Unix(), 10)
			if e != nil || len(items) != 1 || items[0].Attempts != 1 {
				t.Fatal(items, e)
			}
		})
	}
}

func TestSourceOutputUnknownBillNeverBecomesSuccess(t *testing.T) {
	f := flowFixture(t)
	r := sourceSucceededForOutput(t, f)
	f.bill.ReadError = si.ErrUnavailable
	out, e := f.svc.ObserveOutput(t.Context(), r)
	if !errors.Is(e, si.ErrUnavailable) || out.Phase == "succeeded" || out.Output != nil || out.ChargedBill != nil {
		t.Fatal(out, e)
	}
}
func TestSourceDeleteOutputUsesFullOriginalAuthorization(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	if _, e := f.svc.DeleteOutput(t.Context(), Actor{UserID: "other", AccountID: "other"}, f.project.ID, r.ID); !errors.Is(e, si.ErrNotFound) {
		t.Fatal(e)
	}
}

func sourceSucceededForOutput(t *testing.T, f *fixture) si.Run {
	t.Helper()
	r := f.confirmed(t)
	r, ok, e := f.store.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, f.now.Unix(), 30)
	if e != nil || !ok {
		t.Fatal(r, e)
	}
	r, e = f.store.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	o := si.Output{AssetID: "asset-source", ReferenceID: "ref-source", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 123}
	fact := si.TaskFact{ID: "task-source", Scope: r.Intent.Scope, IdempotencyKey: r.TaskKey, Quote: *r.Quote, Provider: r.Intent.Provider, Capability: r.Intent.Capability, Status: "succeeded", Phase: "succeeded", HoldID: "hold-source", ChargeID: "charge-source", Output: &o}
	in := r
	in.Task = &fact
	in.TaskID = fact.ID
	in.Phase = "asset_pending"
	in.LeaseUntil = 0
	r, e = f.store.SaveSourceObservation(t.Context(), in, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	f.task.facts = map[string]si.TaskFact{taskKey(r): fact}
	return r
}
