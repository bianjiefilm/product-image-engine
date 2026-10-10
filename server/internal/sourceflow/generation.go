package sourceflow

import (
	"context"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// generationEqual ignores the client request key. The same picture request must
// keep the original task key and usage key.
func generationEqual(a, b si.Intent) bool {
	return a.Scope == b.Scope && a.Mode == b.Mode && a.Prompt == b.Prompt && a.Size == b.Size &&
		a.PlateDigest == b.PlateDigest && a.Provider == b.Provider && a.Model == b.Model &&
		a.Capability == b.Capability && a.PricingVersion == b.PricingVersion && a.Quantity == b.Quantity
}

// findGeneration reads existing runs first. A missing browser hint must not
// mint another paid task for the same input. Deleted or cancelled runs are
// not reused.
func (s *Service) findGeneration(ctx context.Context, want si.Intent) (si.Run, bool, error) {
	var beforeAt int64
	var beforeID string
	var fallback si.Run
	var have bool
	for pages := 0; pages < 50; pages++ {
		runs, nextAt, nextID, err := s.Store.ListSourceRunsPage(ctx, want.Scope, 100, beforeAt, beforeID)
		if err != nil {
			return si.Run{}, false, err
		}
		for _, r := range runs {
			if r.Deleted || r.CancelRequested || !generationEqual(r.Intent, want) {
				continue
			}
			if r.ConfirmationHash != "" {
				return r, true, nil
			}
			if !have {
				fallback, have = r, true
			}
		}
		if nextID == "" {
			break
		}
		beforeAt, beforeID = nextAt, nextID
	}
	return fallback, have, nil
}

// reuseGeneration returns the existing run for this input, recovering its quote
// on the original keys. It never inserts a row.
func (s *Service) reuseGeneration(ctx context.Context, want si.Intent) (si.Run, bool, error) {
	if s == nil || s.Store == nil {
		return si.Run{}, false, si.ErrUnconfigured
	}
	found, ok, err := s.findGeneration(ctx, want)
	if err != nil || !ok {
		return si.Run{}, false, err
	}
	if found.Quote != nil {
		return found, true, nil
	}
	recovered, err := s.RecoverQuote(ctx, found)
	if err != nil {
		return found, true, err
	}
	return recovered, true, nil
}
