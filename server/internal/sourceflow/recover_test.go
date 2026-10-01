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
