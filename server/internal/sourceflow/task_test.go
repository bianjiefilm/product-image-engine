package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"sync"
	"testing"
	"time"
)

func TestSourceTaskLostResponseNeverCreatesSecondTask(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.AfterCommit = "submit_unobservable"
	if out, _ := f.svc.Advance(t.Context(), r.ID); out.ID != r.ID {
		t.Fatal(out)
	}
	f.reopen(t)
	f.task.ReadError = nil
	for i := 0; i < 3; i++ {
		_, _ = f.svc.Advance(t.Context(), r.ID)
		f.now = f.now.Add(31 * time.Second)
	}
	got, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || got.TaskID == "" || f.task.Submits != 1 || f.task.Commits != 1 || f.task.Dispatches != 1 {
		t.Fatal(got, e, f.task)
	}
}
func TestSourceTaskUnknownLookupMakesNoSubmit(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.ReadError = si.ErrUnavailable
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e == nil || out.ID != r.ID || f.task.Submits != 0 || f.task.Commits != 0 {
		t.Fatal(out, e)
	}
}
func TestSourceTaskUnconfirmedNeverSubmits(t *testing.T) {
	f := flowFixture(t)
	r := f.quoted(t)
	_, _ = f.svc.Advance(t.Context(), r.ID)
	if f.task.Submits != 0 || f.task.Commits != 0 {
		t.Fatal("unconfirmed dispatched")
	}
}
func TestSourceTaskTwoStoresHaveOneCommit(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	other, e := store.Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	second := &Service{Store: other, Auth: &Authorizer{Store: other, Context: f.svc.Auth.Context, AppID: f.svc.Auth.AppID}, Bill: f.svc.Bill, Tasks: f.svc.Tasks, Assets: f.svc.Assets, Profile: f.svc.Profile, Now: f.svc.Now}
	var wg sync.WaitGroup
	for _, svc := range []*Service{f.svc, second} {
		wg.Add(1)
		go func(s *Service) { defer wg.Done(); _, _ = s.Advance(t.Context(), r.ID) }(svc)
	}
	wg.Wait()
	if f.task.Commits != 1 || f.task.Dispatches != 1 {
		t.Fatal(f.task)
	}
}
func TestSourceTaskExpiryStopsOnlyUnattempted(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.now = f.now.Add(300 * time.Second)
	if _, e := f.svc.Advance(t.Context(), r.ID); e == nil {
		t.Fatal("expired unattempted")
	}
	if f.task.Submits != 0 {
		t.Fatal(f.task)
	}
	g := flowFixture(t)
	r = g.confirmed(t)
	g.task.AfterCommit = "submit"
	_, _ = g.svc.Advance(t.Context(), r.ID)
	g.now = g.now.Add(300 * time.Second)
	out, e := g.svc.Advance(t.Context(), r.ID)
	if e != nil || out.TaskID == "" || g.task.Submits != 1 || g.task.Commits != 1 {
		t.Fatal(out, e)
	}
}
func TestSourceTaskWrongReceiptCannotBind(t *testing.T) {
	edits := []func(*si.TaskFact){func(f *si.TaskFact) { f.Scope.UserID = "other" }, func(f *si.TaskFact) { f.ID = "" }, func(f *si.TaskFact) { f.IdempotencyKey = "other" }, func(f *si.TaskFact) { f.Quote.AmountMinor++ }, func(f *si.TaskFact) { f.Provider = "other" }}
	for i, edit := range edits {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			f := flowFixture(t)
			r := f.confirmed(t)
			f.task.AfterCommit = "submit_unobservable"
			_, _ = f.svc.Advance(t.Context(), r.ID)
			f.now = f.now.Add(31 * time.Second)
			f.task.ReadError = nil
			f.task.Mutate = edit
			out, e := f.svc.Advance(t.Context(), r.ID)
			if e == nil || out.TaskID != "" || f.task.Commits != 1 {
				t.Fatal(out, e)
			}
		})
	}
}
func TestSourceCancelBeforeSubmitPersistsNoDispatch(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	out, e := f.svc.Cancel(t.Context(), f.actor, f.project.ID, r.ID)
	if e != nil || !out.CancelRequested || out.SubmitAttempted || out.Phase != "not_dispatched" {
		t.Fatal(out, e)
	}
	_, _ = f.svc.Advance(t.Context(), r.ID)
	if f.task.Submits != 0 || f.task.Commits != 0 {
		t.Fatal(f.task)
	}
}
func TestSourceCancelWinsBeforeSubmitMarker(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.BeforeLookup = func() {
		f.task.BeforeLookup = nil
		if _, e := f.store.CancelSourceRun(t.Context(), r.Intent.Scope, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	_, _ = f.svc.Advance(t.Context(), r.ID)
	if f.task.Submits != 0 {
		t.Fatal("dispatched after cancellation CAS")
	}
	saved, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || !saved.CancelRequested {
		t.Fatal(saved, e)
	}
}
func TestSourceCancelAfterUnknownFindsOriginalTask(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.AfterCommit = "submit_unobservable"
	_, _ = f.svc.Advance(t.Context(), r.ID)
	_, _ = f.svc.Cancel(t.Context(), f.actor, f.project.ID, r.ID)
	f.reopen(t)
	f.task.ReadError = nil
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e != nil || !out.CancelRequested || out.TaskID == "" || f.task.Cancels != 1 || f.task.Submits != 1 || f.task.Commits != 1 {
		t.Fatal(out, e, f.task)
	}
}
func TestSourceCancelUnknownAbsenceNeverClaimsReleased(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.AfterCommit = "submit_unobservable"
	_, _ = f.svc.Advance(t.Context(), r.ID)
	f.task.facts = map[string]si.TaskFact{}
	_, _ = f.svc.Cancel(t.Context(), f.actor, f.project.ID, r.ID)
	f.task.ReadError = nil
	f.now = f.now.Add(31 * time.Second)
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e == nil || out.Phase != "cancel_pending" || !out.CancelRequested || f.task.Submits != 1 {
		t.Fatal(out, e)
	}
}
func TestSourceTaskArchiveKeepsOriginalLinkedTaskRecovery(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e != nil || out.TaskID == "" {
		t.Fatal(out, e)
	}
	status := "archived"
	if _, e = f.store.UpdateProject(t.Context(), f.project.TenantID, f.project.ID, store.ProjectUpdate{Status: &status}); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(31 * time.Second)
	again, e := f.svc.Advance(t.Context(), r.ID)
	if e != nil || again.TaskID != out.TaskID || f.task.Submits != 1 {
		t.Fatal(again, e)
	}
}
func TestSourceTaskClosedProfileStopsNewSubmitKeepsLinkedRead(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.svc.Profile.Enabled = false
	if _, e := f.svc.Advance(t.Context(), r.ID); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(e)
	}
	if f.task.Submits != 0 {
		t.Fatal(f.task)
	}
	g := flowFixture(t)
	r = g.confirmed(t)
	out, e := g.svc.Advance(t.Context(), r.ID)
	if e != nil {
		t.Fatal(out, e)
	}
	g.svc.Profile.Enabled = false
	g.now = g.now.Add(31 * time.Second)
	again, e := g.svc.Advance(t.Context(), r.ID)
	if e != nil || again.TaskID != out.TaskID || g.task.Submits != 1 {
		t.Fatal(again, e)
	}
}

func TestSourceTaskClosedProfileNeverResubmitsAttemptedAbsent(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.AfterCommit = "submit_unobservable"
	_, _ = f.svc.Advance(t.Context(), r.ID)
	f.task.facts = map[string]si.TaskFact{}
	f.task.ReadError = nil
	f.svc.Profile.Enabled = false
	f.now = f.now.Add(31 * time.Second)
	_, _ = f.svc.Advance(t.Context(), r.ID)
	if f.task.Submits != 1 || f.task.Commits != 1 {
		t.Fatal("closed source profile resubmitted absent attempted task", f.task)
	}
}

func TestSourceTaskRemoteCancellationIsObservedWithoutLocalCancel(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.Mutate = func(f *si.TaskFact) { f.Status = "canceled"; f.Phase = "canceled" }
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e != nil || out.Task == nil || out.Task.Status != "canceled" || out.CancelRequested || out.Phase != "review_required" || out.Output != nil {
		t.Fatal(out, e)
	}
}
func TestSourceTaskOneReadHasOneBackoffObservation(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	before := out.Attempts
	f.now = f.now.Add(31 * time.Second)
	out, e = f.svc.Advance(t.Context(), r.ID)
	if e != nil || out.Attempts != before+1 {
		t.Fatal("one poll must not double advance backoff", out.Attempts, before, e)
	}
}

func TestSourceTaskRepeatConfirmKeepsHeldChargedFacts(t *testing.T) {
	for _, status := range []string{"held", "charged"} {
		t.Run(status, func(t *testing.T) {
			f := flowFixture(t)
			r := f.confirmed(t)
			r, e := f.svc.Advance(t.Context(), r.ID)
			if e != nil {
				t.Fatal(e)
			}
			key := billKey(r)
			bill := f.bill.facts[key]
			bill.Status = status
			bill.HoldID = "hold-original"
			if status == "charged" {
				bill.ChargeID = "charge-original"
				amount := int64(25)
				bill.ChargedMinor = &amount
			}
			f.bill.facts[key] = bill
			f.now = f.now.Add(31 * time.Second)
			gets := f.bill.Gets
			writes := f.bill.UsageAttempts + f.bill.QuoteAttempts
			submits := f.task.Submits
			out, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, r.ConfirmationHash)
			if e != nil || out.Phase != r.Phase || out.TaskID != r.TaskID || out.EventsJSON != r.EventsJSON || out.ConfirmedAt != r.ConfirmedAt || f.bill.Gets != gets || f.bill.UsageAttempts+f.bill.QuoteAttempts != writes || f.task.Submits != submits || f.bill.facts[key].Status != status {
				t.Fatal("repeat confirm changed Task/Bill authority", out, e)
			}
		})
	}
}

type deadlineTaskPort struct {
	TaskPort
	seen  bool
	store *store.Store
	t     *testing.T
}

func (d *deadlineTaskPort) Submit(ctx context.Context, r si.Run) error {
	d.seen = true
	bound, ok := ctx.Deadline()
	if !ok || time.Until(bound) <= 0 || time.Until(bound) > 20*time.Second {
		d.t.Fatal("unbounded submit")
	}
	saved, e := d.store.GetSourceRun(ctx, r.Intent.Scope, r.ID)
	if e != nil || !saved.SubmitAttempted || saved.SubmitStartedAt <= 0 || saved.SubmitBudgetSeconds != 20 || saved.TaskKey != r.TaskKey {
		d.t.Fatal("submit before durable original budget", saved, e)
	}
	return d.TaskPort.Submit(ctx, r)
}
func TestSourceTaskSubmitHasActualTwentySecondDeadline(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	port := &deadlineTaskPort{TaskPort: f.task, store: f.store, t: t}
	f.svc.Tasks = port
	if _, e := f.svc.Advance(t.Context(), r.ID); e != nil || !port.seen {
		t.Fatal(e)
	}
}
func TestSourceTaskRetryAfterOriginalBudgetUsesSameIntent(t *testing.T) {
	f := flowFixture(t)
	clock := store.Now
	store.Now = func() time.Time { return f.now }
	t.Cleanup(func() { store.Now = clock })
	r := f.confirmed(t)
	f.task.AfterCommit = "submit_no_commit"
	first, e := f.svc.Advance(t.Context(), r.ID)
	if e == nil || first.SubmitStartedAt <= 0 || f.task.Submits != 1 || f.task.Commits != 0 {
		t.Fatal(first, e)
	}
	f.now = f.now.Add(19 * time.Second)
	_, _ = f.svc.Advance(t.Context(), r.ID)
	if f.task.Submits != 1 {
		t.Fatal("retried before request budget")
	}
	f.now = f.now.Add(12 * time.Second)
	out, e := f.svc.Advance(t.Context(), r.ID)
	if e != nil || out.TaskID == "" || out.SubmitStartedAt != first.SubmitStartedAt || out.SubmitBudgetSeconds != first.SubmitBudgetSeconds || out.TaskKey != first.TaskKey || out.UsageKey != first.UsageKey || out.BusinessRef != first.BusinessRef || f.task.Submits != 2 || f.task.Commits != 1 || f.task.Dispatches != 1 {
		t.Fatal(out, e, f.task)
	}
}
