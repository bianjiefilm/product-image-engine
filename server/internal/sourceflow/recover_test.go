package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"testing"
	"time"
)

func TestSourceRecoveryCancelledStopsAndDoesNotDispatch(t *testing.T) {
	f := flowFixture(t)
	f.confirmed(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if e := f.svc.RunRecovery(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if f.task.Submits != 0 {
		t.Fatal(f.task)
	}
}
func TestSourceRecoveryBackoffBounds(t *testing.T) {
	for i, base := range []int64{1, 2, 4, 8, 16, 32, 60, 60, 60} {
		for _, id := range []string{"run-a", "run-b", "run-c"} {
			d := sourceDelay(id, int64(i))
			if d < base || d > base+base/10 {
				t.Fatal(i, d)
			}
		}
	}
}

type blockingTaskPort struct {
	TaskPort
	entered chan time.Duration
	stop    chan struct{}
}

func (b *blockingTaskPort) Lookup(ctx context.Context, r si.Run) (si.TaskFact, bool, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return si.TaskFact{}, false, si.ErrInvariant
	}
	select {
	case b.entered <- time.Until(deadline):
	default:
	}
	select {
	case <-ctx.Done():
		return si.TaskFact{}, false, ctx.Err()
	case <-b.stop:
		return si.TaskFact{}, false, si.ErrUnavailable
	}
}
func TestSourceRecoveryNoOverlapAndJoinsCancellation(t *testing.T) {
	f := flowFixture(t)
	f.confirmed(t)
	b := &blockingTaskPort{TaskPort: f.task, entered: make(chan time.Duration, 1), stop: make(chan struct{})}
	f.svc.Tasks = b
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.svc.RunRecovery(ctx) }()
	select {
	case bound := <-b.entered:
		if bound <= 0 || bound > 20*time.Second {
			t.Fatal(bound)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker never entered")
	}
	if e := f.svc.RunRecovery(t.Context()); !errors.Is(e, si.ErrConflict) {
		t.Fatal("overlapping worker", e)
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker failed to join")
	}
}
func TestSourceRecoverySelectsAtMostTenDueRows(t *testing.T) {
	f := flowFixture(t)
	for i := 0; i < 12; i++ {
		req := request()
		req.RequestKey = string(rune('a' + i))
		r, e := f.svc.Create(t.Context(), f.actor, f.project.ID, req)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e != nil {
			t.Fatal(e)
		}
	}
	if e := f.svc.recoveryPass(t.Context()); e != nil {
		t.Fatal(e)
	}
	if f.task.Commits != 10 {
		t.Fatal("unbounded pass", f.task.Commits)
	}
}

func TestSourceRecoveryObservesOriginalOutputWithCreationDisabled(t *testing.T) {
	f := flowFixture(t)
	r, a := sourceReadyForOutput(t, f)
	f.svc.Profile.Enabled = false
	if e := f.svc.recoveryPass(t.Context()); e != nil {
		t.Fatal(e)
	}
	got, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || got.Phase != "succeeded" || got.Output == nil || got.ChargedBill == nil || got.TaskID != r.TaskID || got.TaskKey != r.TaskKey {
		t.Fatal("original output not recovered", got, e)
	}
	if a.verifies != 1 || f.task.Submits != 0 || f.bill.UsageAttempts != 1 || f.bill.QuoteAttempts != 1 {
		t.Fatal("recovery dispatched or changed funds", a, f.task, f.bill)
	}
}

func TestSourceRecoveryDrainsOriginalOutboxWithoutDueRuns(t *testing.T) {
	f := flowFixture(t)
	r, a := sourceReadyForOutput(t, f)
	r, e := f.svc.ObserveOutput(t.Context(), r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.svc.DeleteOutput(t.Context(), f.actor, f.project.ID, r.ID); e != nil {
		t.Fatal(e)
	}
	f.svc.Profile.Enabled = false
	if e = f.svc.recoveryPass(t.Context()); e != nil {
		t.Fatal(e)
	}
	items, e := f.store.ListDueSourceOutbox(t.Context(), f.now.Unix(), 10)
	if e != nil || len(items) != 0 || a.releases != 1 || a.state != "released" {
		t.Fatal("original outbox not drained", items, a, e)
	}
	for _, item := range a.items {
		if item.Scope != r.Intent.Scope || item.Output.AssetID != r.Output.AssetID || item.Output.ReferenceID != r.Output.ReferenceID {
			t.Fatal("foreign release", item)
		}
	}
	if f.task.Submits != 0 || f.bill.UsageAttempts != 1 || f.bill.QuoteAttempts != 1 {
		t.Fatal("outbox changed funds or dispatch")
	}
}

func TestSourceRecoveryObservesAndDrainsDeletedLateOutputInOnePass(t *testing.T) {
	f := flowFixture(t)
	r, a := sourceReadyForOutput(t, f)
	if _, e := f.svc.DeleteOutput(t.Context(), f.actor, f.project.ID, r.ID); e != nil {
		t.Fatal(e)
	}
	f.svc.Profile.Enabled = false
	if e := f.svc.recoveryPass(t.Context()); e != nil {
		t.Fatal(e)
	}
	got, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || !got.Deleted || got.Output != nil || got.Selected || a.releases != 1 || a.state != "released" {
		t.Fatal("late deleted output not reconciled", got, a, e)
	}
	if f.task.Submits != 0 || f.bill.UsageAttempts != 1 || f.bill.QuoteAttempts != 1 {
		t.Fatal("late output dispatched or changed funds")
	}
}

func TestSourceRecoveryDatabaseFailureExitsCoordinator(t *testing.T) {
	f := flowFixture(t)
	if e := f.store.Close(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	e := f.svc.RunRecovery(ctx)
	if e == nil || errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
		t.Fatal("unexpected DB failure was hidden by live worker", e)
	}
}

func TestSourceRecoveryTerminalSettlementDoesNotInventOutput(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			f := flowFixture(t)
			r := f.confirmed(t)
			fact := si.TaskFact{ID: "task-terminal", IdempotencyKey: r.TaskKey, Scope: r.Intent.Scope, Quote: *r.Quote, Provider: r.Intent.Provider, Capability: r.Intent.Capability, Status: status, Phase: status, HoldID: "hold-terminal", ReleaseID: "release-terminal"}
			f.task.facts = map[string]si.TaskFact{taskKey(r): fact}
			b := f.bill.facts[billKey(r)]
			b.Status = "released"
			b.HoldID = fact.HoldID
			b.ReleaseID = fact.ReleaseID
			f.bill.facts[billKey(r)] = b
			a := &outputAuthority{}
			f.svc.Assets = a
			f.svc.Profile.Enabled = false
			if e := f.svc.recoveryPass(t.Context()); e != nil {
				t.Fatal(e)
			}
			got, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if e != nil || got.Phase != "review_required" || got.Task == nil || got.Task.Status != status || got.Bill.Status != "released" || got.Bill.ReleaseID != fact.ReleaseID || got.Output != nil || got.ChargedBill != nil || a.verifies != 0 || f.task.Submits != 0 {
				t.Fatal("terminal settlement guessed output", got, a, e)
			}
		})
	}
}
func TestSourceRecoveryUnknownTaskCannotObserveOutputOrDispatch(t *testing.T) {
	f := flowFixture(t)
	r := f.confirmed(t)
	f.task.ReadError = si.ErrUnavailable
	f.svc.Profile.Enabled = false
	a := &outputAuthority{}
	f.svc.Assets = a
	if e := f.svc.recoveryPass(t.Context()); e != nil {
		t.Fatal(e)
	}
	got, e := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || got.Output != nil || got.TaskID != "" || a.verifies != 0 || f.task.Submits != 0 || f.bill.UsageAttempts != 1 || f.bill.QuoteAttempts != 1 {
		t.Fatal("unknown provider implied success or new dispatch", got, a, e)
	}
}
