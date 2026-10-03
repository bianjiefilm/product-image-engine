package sourceflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"math"
	"time"
)

func sourceDelay(id string, attempts int64) int64 {
	d := int64(60)
	if attempts < 6 {
		d = 1 << uint(attempts)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, attempts)))
	return d + int64(sum[0])%(d/10+1)
}

func sourceLocalTerminal(phase string) bool {
	return phase == "succeeded" || phase == "failed" || phase == "canceled" || phase == "not_dispatched" || phase == "output_deleted"
}
func sourceTaskTerminalPhase(status string) string {
	switch status {
	case "succeeded":
		return "asset_pending"
	case "failed", "canceled":
		return "review_required"
	}
	return ""
}
func (s *Service) taskError(ctx context.Context, r si.Run, cause error) (si.Run, error) {
	in := r
	in.LastError = "platform_unknown"
	switch {
	case errors.Is(cause, si.ErrInvariant):
		in.LastError = "invariant"
	case errors.Is(cause, si.ErrUnconfigured):
		in.LastError = "unconfigured"
	case errors.Is(cause, si.ErrConflict):
		in.LastError = "conflict"
	case errors.Is(cause, si.ErrNotFound) || errors.Is(cause, si.ErrForbidden):
		in.LastError = "authorization_changed"
	}
	if !sourceLocalTerminal(r.Phase) {
		terminal := ""
		if r.Task != nil {
			terminal = sourceTaskTerminalPhase(r.Task.Status)
		}
		if r.Bill != nil && r.Bill.Status == "refunded" {
			in.Phase = "review_required"
		} else if terminal != "" {
			in.Phase = terminal
		} else if r.CancelRequested && r.SubmitAttempted {
			in.Phase = "cancel_pending"
		} else if r.Task == nil && r.SubmitAttempted && (errors.Is(cause, si.ErrUnavailable) || errors.Is(cause, si.ErrInvariant)) {
			in.Phase = "provider_unknown"
		}
	}
	delay := sourceDelay(r.ID, r.Attempts)
	now := s.now().Unix()
	if in.Attempts == math.MaxInt64 || now > math.MaxInt64-delay {
		return r, si.ErrInvariant
	}
	in.Attempts++
	in.RetryAt = now + delay
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	out, e := s.Store.SaveSourceObservation(cleanup, in, r.Revision, r.LeaseEpoch)
	if e == nil {
		return out, cause
	}
	if current, e := s.Store.GetSourceRun(cleanup, r.Intent.Scope, r.ID); e == nil {
		return current, cause
	}
	return r, cause
}
func (s *Service) observeTask(ctx context.Context, r si.Run, f si.TaskFact, finish bool) (si.Run, error) {
	if e := si.ValidateTaskFact(r, f); e != nil {
		return s.taskError(ctx, r, e)
	}
	in := r
	in.Task = &f
	in.TaskID = f.ID
	in.SubmitAttempted = true
	in.LastError = ""
	in.Phase = "task_linked"
	switch {
	case sourceTaskTerminalPhase(f.Status) != "":
		in.Phase = sourceTaskTerminalPhase(f.Status)
	case r.CancelRequested:
		in.Phase = "cancel_pending"
	case f.Phase == "provider_unknown":
		in.Phase = "provider_unknown"
		in.LastError = "provider_unknown"
	}
	if r.Bill != nil && r.Bill.Status == "refunded" {
		in.Phase = "review_required"
	}
	if sourceLocalTerminal(r.Phase) {
		in.Phase = r.Phase
	}
	delay := sourceDelay(r.ID, r.Attempts)
	now := s.now().Unix()
	if r.Attempts == math.MaxInt64 || now > math.MaxInt64-delay {
		return r, si.ErrInvariant
	}
	in.Attempts++
	in.RetryAt = now + delay
	if finish {
		in.LeaseUntil = 0
	}
	out, e := s.Store.SaveSourceObservation(ctx, in, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	return out, nil
}
func (s *Service) Advance(ctx context.Context, id string) (si.Run, error) {
	if s == nil || s.Store == nil {
		return si.Run{}, si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(si.TaskRequestBudgetSeconds)*time.Second)
	defer cancel()
	r, e := s.Store.GetSourceRunForRecovery(ctx, id)
	if e != nil {
		return r, e
	}
	if sourceLocalTerminal(r.Phase) {
		return r, nil
	}
	if r.Quote == nil {
		return s.RecoverQuote(ctx, r)
	}
	if r.ConfirmationHash == "" {
		return r, si.ErrConflict
	}
	if nilPort(s.Tasks) {
		return r, si.ErrUnconfigured
	}
	r, claimed, e := s.Store.ClaimSourceRun(ctx, r.Intent.Scope, r.ID, s.now().Unix(), 30)
	if e != nil {
		return r, e
	}
	if !claimed {
		return r, si.ErrConflict
	}
	var f si.TaskFact
	var found bool
	if r.TaskID != "" {
		f, found, e = s.Tasks.Get(ctx, r)
	} else {
		f, found, e = s.Tasks.Lookup(ctx, r)
	}
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if found {
		if !r.CancelRequested || (f.Status != "queued" && f.Status != "running") {
			return s.observeTask(ctx, r, f, true)
		}
		r, e = s.observeTask(ctx, r, f, false)
		if e != nil {
			return r, e
		}
		if r.CancelRequested && (f.Status == "queued" || f.Status == "running") {
			if e = s.Tasks.Cancel(ctx, r); e != nil {
				return s.taskError(ctx, r, e)
			}
			f, found, e = s.Tasks.Get(ctx, r)
			if e != nil {
				return s.taskError(ctx, r, e)
			}
			if !found {
				return s.taskError(ctx, r, si.ErrUnavailable)
			}
		}
		return s.observeTask(ctx, r, f, true)
	}
	if r.TaskID != "" || r.CancelRequested || r.Deleted {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	// No linked authority means no new Task POST while new creation is closed.
	if !s.Profile.Enabled || s.Auth == nil || nilPort(s.Bill) {
		return s.taskError(ctx, r, si.ErrUnconfigured)
	}
	auth, e := s.Auth.ForProject(ctx, Actor{UserID: r.Intent.Scope.UserID, AccountID: r.Intent.Scope.PrincipalAccountID}, r.Intent.Scope.ProjectID, true)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if auth.Scope != r.Intent.Scope {
		return s.taskError(ctx, r, si.ErrConflict)
	}
	if !s.Profile.CanCreate(auth.PayerSource) {
		return s.taskError(ctx, r, si.ErrUnconfigured)
	}
	if s.now().Unix() < r.Quote.QuotedAtUnix || s.now().Unix() >= r.Quote.ExpiresAtUnix {
		return s.taskError(ctx, r, si.ErrConflict)
	}
	bill, exists, e := s.Bill.Get(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if !exists {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	if e = si.ValidateQuoteBill(r, bill); e != nil {
		return s.taskError(ctx, r, e)
	}
	marked, e := s.Store.MarkSourceSubmitAttempt(ctx, r, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	r = marked
	if ctx.Err() != nil {
		return s.taskError(ctx, r, ctx.Err())
	}
	if e = s.Tasks.Submit(ctx, r); e != nil {
		return s.taskError(ctx, r, e)
	}
	f, found, e = s.Tasks.Lookup(ctx, r)
	if e != nil {
		return s.taskError(ctx, r, e)
	}
	if !found {
		return s.taskError(ctx, r, si.ErrUnavailable)
	}
	return s.observeTask(ctx, r, f, true)
}
func (s *Service) Cancel(ctx context.Context, actor Actor, project, id string) (si.Run, error) {
	r, e := s.LoadRun(ctx, actor, project, id, true)
	if e != nil {
		return r, e
	}
	r, e = s.Store.CancelSourceRun(ctx, r.Intent.Scope, r.ID)
	if e != nil {
		return r, e
	}
	if r.Phase == "not_dispatched" {
		return r, nil
	}
	return s.Advance(ctx, r.ID)
}
func (s *Service) Reconcile(ctx context.Context, actor Actor, project, id string) (si.Run, error) {
	r, e := s.LoadRun(ctx, actor, project, id, true)
	if e != nil {
		return r, e
	}
	if r.TaskID != "" && !nilPort(s.Tasks) {
		ctx, cancel := context.WithTimeout(ctx, time.Duration(si.TaskRequestBudgetSeconds)*time.Second)
		defer cancel()
		if e = s.Tasks.Reconcile(ctx, r); e != nil {
			return r, e
		}
	}
	out, e := s.Advance(ctx, r.ID)
	if e == nil {
		s.settlePlate(ctx, out)
		if again, readErr := s.Store.GetSourceRun(ctx, out.Intent.Scope, out.ID); readErr == nil {
			out = again
		}
	}
	return out, e
}
