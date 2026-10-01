package sourceflow

import (
	"context"
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
		_, _ = s.Advance(ctx, r.ID)
	}
	return ctx.Err()
}

// RunRecovery owns one joined coordinator loop. It does not spawn detached work.
func (s *Service) RunRecovery(ctx context.Context) error {
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
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.recoveryPass(ctx)
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
