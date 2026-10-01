package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"math"
	"time"
)

type NewRunRequest struct{ RequestKey, Mode, Prompt, Size string }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
func (s *Service) Create(ctx context.Context, actor Actor, project string, in NewRunRequest) (si.Run, error) {
	if s == nil || s.Store == nil || s.Auth == nil || nilPort(s.Bill) {
		return si.Run{}, si.ErrUnconfigured
	}
	auth, e := s.Auth.ForProject(ctx, actor, project, true)
	if e != nil {
		return si.Run{}, e
	}
	if !s.Profile.CanCreate(auth.PayerSource) {
		return si.Run{}, si.ErrUnconfigured
	}
	intent := si.Intent{Scope: auth.Scope, RequestKey: in.RequestKey, Mode: in.Mode, Prompt: in.Prompt, Size: in.Size, Provider: s.Profile.Provider, Model: s.Profile.Model, Capability: s.Profile.Capability, PricingVersion: s.Profile.PricingVersion, Quantity: s.Profile.Quantity}
	r, _, e := s.Store.CreateSourceRun(ctx, intent)
	if e != nil {
		return r, e
	}
	if r.Quote != nil {
		return r, nil
	}
	return s.RecoverQuote(ctx, r)
}
func (s *Service) quoteError(ctx context.Context, r si.Run, cause error) (si.Run, error) {
	r.LastError = "platform_unknown"
	if errors.Is(cause, si.ErrInvariant) || errors.Is(cause, si.ErrInvalid) {
		r.LastError = "invariant"
	}
	if r.Attempts == math.MaxInt64 {
		return r, si.ErrInvariant
	}
	r.Attempts++
	now := s.now().Unix()
	if now > math.MaxInt64-31 {
		return r, si.ErrInvariant
	}
	r.RetryAt = now + 31
	// Recording an unknown does not release the lease or alter any remote key/fact.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	saved, e := s.Store.SaveSourceObservation(cleanup, r, r.Revision, r.LeaseEpoch)
	if e == nil {
		return saved, cause
	}
	if current, readErr := s.Store.GetSourceRun(cleanup, r.Intent.Scope, r.ID); readErr == nil {
		return current, cause
	}
	return r, cause
}
func (s *Service) RecoverQuote(ctx context.Context, in si.Run) (si.Run, error) {
	if s == nil || s.Store == nil || nilPort(s.Bill) {
		return si.Run{}, si.ErrUnconfigured
	}
	r, e := s.Store.GetSourceRunForRecovery(ctx, in.ID)
	if e != nil {
		return r, e
	}
	if in.Owner != r.Owner || in.Fingerprint != r.Fingerprint || in.Intent != r.Intent || in.UsageKey != r.UsageKey || in.TaskKey != r.TaskKey || in.BusinessRef != r.BusinessRef {
		return r, si.ErrConflict
	}
	if r.Deleted || r.CancelRequested || r.SubmitAttempted {
		return r, si.ErrConflict
	}
	if r.Quote != nil {
		return r, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, claimed, e := s.Store.ClaimSourceRun(ctx, r.Intent.Scope, r.ID, s.now().Unix(), 30)
	if e != nil {
		return r, e
	}
	if !claimed {
		return r, si.ErrConflict
	}
	var fact si.BillFact
	var found bool
	if r.Bill == nil {
		fact, found, e = s.Bill.Lookup(ctx, r)
	} else {
		fact, found, e = s.Bill.Get(ctx, r)
	}
	if e != nil {
		return s.quoteError(ctx, r, e)
	}
	if !found {
		if r.Bill != nil {
			return s.quoteError(ctx, r, si.ErrInvariant)
		}
		if e = s.Bill.Usage(ctx, r); e != nil {
			return s.quoteError(ctx, r, e)
		}
		fact, found, e = s.Bill.Lookup(ctx, r)
		if e != nil {
			return s.quoteError(ctx, r, e)
		}
		if !found {
			return s.quoteError(ctx, r, si.ErrUnavailable)
		}
	}
	if e = si.ValidateQuoteBill(r, fact); e != nil {
		return s.quoteError(ctx, r, e)
	}
	next, e := s.Store.SaveSourceBillObservation(ctx, r, fact, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.quoteError(ctx, r, e)
	}
	r = next
	if fact.Quote == nil {
		if e = s.Bill.Quote(ctx, r); e != nil {
			return s.quoteError(ctx, r, e)
		}
		fact, found, e = s.Bill.Get(ctx, r)
		if e != nil {
			return s.quoteError(ctx, r, e)
		}
		if !found || fact.Quote == nil {
			return s.quoteError(ctx, r, si.ErrUnavailable)
		}
		if e = si.ValidateQuoteBill(r, fact); e != nil {
			return s.quoteError(ctx, r, e)
		}
		next, e = s.Store.SaveSourceBillObservation(ctx, r, fact, r.Revision, r.LeaseEpoch)
		if e != nil {
			return s.quoteError(ctx, r, e)
		}
		r = next
	}
	next, e = s.Store.SaveSourceQuoteUnderLease(ctx, r, *fact.Quote, r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.quoteError(ctx, r, e)
	}
	return next, nil
}
func (s *Service) Confirm(ctx context.Context, actor Actor, project, id, quoteID, hash string) (si.Run, error) {
	if s == nil || s.Store == nil || s.Auth == nil || nilPort(s.Bill) {
		return si.Run{}, si.ErrUnconfigured
	}
	r, e := s.LoadRun(ctx, actor, project, id, true)
	if e != nil {
		return r, e
	}
	auth, e := s.Auth.ForProject(ctx, actor, project, true)
	if e != nil {
		return r, e
	}
	if auth.Scope != r.Intent.Scope {
		return r, si.ErrConflict
	}
	if !s.Profile.CanCreate(auth.PayerSource) {
		return r, si.ErrUnconfigured
	}
	if r.Quote == nil || quoteID != r.Quote.QuoteID || hash != si.QuoteHash(*r.Quote) || r.Deleted || r.CancelRequested {
		return r, si.ErrConflict
	}
	if r.ConfirmationHash == "" && (s.now().Unix() < r.Quote.QuotedAtUnix || s.now().Unix() >= r.Quote.ExpiresAtUnix) {
		return r, si.ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, claimed, e := s.Store.ClaimSourceRun(ctx, r.Intent.Scope, r.ID, s.now().Unix(), 30)
	if e != nil {
		return r, e
	}
	if !claimed {
		return r, si.ErrConflict
	}
	fact, found, e := s.Bill.Get(ctx, r)
	if e != nil {
		return s.quoteError(ctx, r, e)
	}
	if !found {
		return s.quoteError(ctx, r, si.ErrUnavailable)
	}
	if e = si.ValidateQuoteBill(r, fact); e != nil {
		return s.quoteError(ctx, r, e)
	}
	out, e := s.Store.ConfirmSourceRunForProject(ctx, r, hash, s.now().Unix(), r.Revision, r.LeaseEpoch)
	if e != nil {
		return s.quoteError(ctx, r, e)
	}
	return out, nil
}
