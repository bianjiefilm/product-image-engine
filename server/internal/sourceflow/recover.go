package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"time"
)

// recoveryPass limits the entire batch, including all original-key reads, to a
// single budget. Advance persists safe retry facts on transient failures.
func (s *Service) recoveryPass(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows, e := s.Store.ListDueSourceRuns(ctx, s.now().Unix(), 10)
	if e != nil {
		return e
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		observed, _ := s.Advance(ctx, r.ID)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Only an original confirmed Task snapshot can lead to output/settlement
		// observation. Unknown or still-running Tasks never imply success.
		if observed.ConfirmationHash != "" && observed.Task != nil && (observed.Phase == "asset_pending" || observed.Phase == "review_required") && (observed.Task.Status == "succeeded" || observed.Task.Status == "failed" || observed.Task.Status == "canceled") {
			_, _ = s.ObserveOutput(ctx, observed)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Outbox work remains recoverable even when no Run is due or creation is off.
	_ = s.DrainReferenceOutbox(ctx)
	return ctx.Err()
}

// RunRecovery owns one joined coordinator loop. It does not spawn detached work.
func (s *Service) RunRecovery(ctx context.Context) error {
	return s.RunRecoveryStarted(ctx, nil)
}

// RunRecoveryStarted signals registration only after the single-loop CAS and
// cancellation guard. Runtime readiness is frozen before exposing its Router.
func (s *Service) RunRecoveryStarted(ctx context.Context, started chan<- struct{}) error {
	if s == nil || s.Store == nil {
		return si.ErrUnconfigured
	}
	if !s.recoveryRunning.CompareAndSwap(false, true) {
		return si.ErrConflict
	}
	defer s.recoveryRunning.Store(false)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if started != nil {
		close(started)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		e := s.recoveryPass(ctx)
		if e != nil && ctx.Err() == nil && !errors.Is(e, context.DeadlineExceeded) && !errors.Is(e, context.Canceled) {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
