package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"io"
	"math"
	"reflect"
	"time"
)

func (s *Service) claimOutput(ctx context.Context, in si.Run) (si.Run, error) {
	if s == nil || s.Store == nil {
		return si.Run{}, si.ErrUnconfigured
	}
	r, e := s.Store.GetSourceRunForRecovery(ctx, in.ID)
	if e != nil {
		return r, e
	}
	if in.Owner != r.Owner || in.Intent != r.Intent || in.TaskID != r.TaskID || in.TaskKey != r.TaskKey || in.UsageKey != r.UsageKey || in.Fingerprint != r.Fingerprint || !reflect.DeepEqual(in.Quote, r.Quote) {
		return r, si.ErrConflict
	}
	if r.Task == nil || r.ConfirmationHash == "" {
		return r, si.ErrConflict
	}
	r, ok, e := s.Store.ClaimSourceRun(ctx, r.Intent.Scope, r.ID, s.now().Unix(), 30)
	if e != nil {
		return r, e
	}
	if !ok {
		return r, si.ErrConflict
	}
	return r, nil
}
func (s *Service) ObserveOutput(ctx context.Context, in si.Run) (si.Run, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, e := s.claimOutput(ctx, in)
	if e != nil {
		return r, e
	}
	if nilPort(s.Tasks) || nilPort(s.Bill) {
		return s.taskError(ctx, r, si.ErrUnconfigured)
	}
	fact, found, e := s.Tasks.Get(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if !found {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	// Keep the fenced lease until all original authority reads finish.
	r, e = s.observeTask(ctx, r, fact, false)
	if e != nil {
		return r, e
	}
	bill, found, e := s.Bill.Get(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if !found {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	r, e = s.Store.SaveSourceSettlementObservation(ctx, r, bill, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if r.Task.Status != "succeeded" {
		return s.observeTask(ctx, r, *r.Task, true)
	}
	if bill.Status != "charged" && !(r.Deleted && bill.Status == "refunded") {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	if nilPort(s.Assets) {
		return s.taskError(ctx, r, si.ErrUnconfigured)
	}
	output, e := s.Assets.Verify(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if r.Deleted && r.Output == nil {
		out, e := s.Store.QueueDeletedSourceReference(ctx, r, output, r.Revision, r.LeaseEpoch)
		if e != nil {
			return s.taskError(ctx, r, e)
		}
		return out, nil
	}
	out, e := s.Store.AttachSourceOutput(ctx, r, output, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	return out, nil
}
func (s *Service) DeleteOutput(ctx context.Context, actor Actor, project, id string) (si.Run, error) {
	r, e := s.LoadRun(ctx, actor, project, id, true)
	if e != nil {
		return r, e
	}
	return s.Store.DeleteSourceOutput(ctx, r.Intent.Scope, r.ID)
}
func (s *Service) Select(ctx context.Context, actor Actor, project, id string) (si.Run, error) {
	r, e := s.LoadRun(ctx, actor, project, id, true)
	if e != nil {
		return r, e
	}
	if r.Deleted || r.Output == nil {
		return r, si.ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, e = s.claimOutput(ctx, r)
	if e != nil {
		return r, e
	}
	if nilPort(s.Bill) || nilPort(s.Assets) {
		return s.taskError(ctx, r, si.ErrUnconfigured)
	}
	bill, found, e := s.Bill.Get(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if !found {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	r, e = s.Store.SaveSourceSettlementObservation(ctx, r, bill, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if bill.Status != "charged" {
		return s.taskError(ctx, r, si.ErrConflict)
	}
	actual, e := s.Assets.Verify(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if *r.Output != actual {
		return s.taskError(ctx, r, si.ErrInvariant)
	}
	out, e := s.Store.SelectSourceOutput(ctx, r, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	return out, nil
}

// OpenOutput performs current read-only availability checks. It does not update
// snapshots or expose a supplier URL; a later refund cannot reuse old charge proof.
func (s *Service) OpenOutput(ctx context.Context, actor Actor, project, id string) (io.ReadCloser, error) {
	r, e := s.LoadRun(ctx, actor, project, id, false)
	if e != nil {
		return nil, e
	}
	if r.Deleted || r.Output == nil || si.ValidateReadyOutput(r, *r.Output) != nil {
		return nil, si.ErrConflict
	}
	if nilPort(s.Bill) || nilPort(s.Assets) {
		return nil, si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	bill, found, e := s.Bill.Get(ctx, r)
	if e != nil {
		cancel()
		return nil, e
	}
	if !found {
		cancel()
		return nil, si.ErrUnavailable
	}
	if si.ValidateSettlement(r, bill) != nil || bill.Status != "charged" {
		cancel()
		return nil, si.ErrConflict
	}
	body, e := s.Assets.Download(ctx, r)
	if e != nil {
		cancel()
		return nil, e
	}
	return &sourceOutputStream{ReadCloser: body, cancel: cancel}, nil
}

type sourceOutputStream struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (s *sourceOutputStream) Close() error { defer s.cancel(); return s.ReadCloser.Close() }

func (s *Service) outboxError(ctx context.Context, item si.Outbox, cause error) error {
	code := "reference_unknown"
	if errors.Is(cause, si.ErrInvariant) {
		code = "reference_invariant"
	}
	delay := sourceDelay(item.ID, item.Attempts)
	now := s.now().Unix()
	if now > math.MaxInt64-delay {
		return si.ErrInvariant
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = s.Store.SaveSourceOutboxError(cleanup, item, item.LeaseEpoch, code, now+delay)
	return cause
}
func (s *Service) DrainReferenceOutbox(ctx context.Context) error {
	if s == nil || s.Store == nil || nilPort(s.Assets) {
		return si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, e := s.Store.ListDueSourceOutbox(ctx, s.now().Unix(), 10)
	if e != nil {
		return e
	}
	var first error
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		item, claimed, e := s.Store.ClaimSourceOutbox(ctx, item.ID, s.now().Unix(), 30)
		if e != nil {
			if first == nil {
				first = e
			}
			continue
		}
		if !claimed {
			continue
		}
		state, e := s.Assets.Reference(ctx, item)
		if e == nil && state == "active" {
			e = s.Assets.Release(ctx, item)
			if e == nil {
				state, e = s.Assets.Reference(ctx, item)
			}
		}
		if e != nil || state != "released" {
			if e == nil {
				e = si.ErrUnavailable
			}
			_ = s.outboxError(ctx, item, e)
			if first == nil {
				first = e
			}
			continue
		}
		item.Output.ReferenceState = "released"
		if e = s.Store.FinishSourceRelease(ctx, item, item.LeaseEpoch); e != nil && first == nil {
			first = e
		}
	}
	return first
}
