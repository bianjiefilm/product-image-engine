package sourceflow

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// PlateRequest is everything a browser may influence: which registered input,
// which words describe the background. OriginalSHA256 is the digest of that
// input's bytes as already proven against Upload by the HTTP layer; the service
// still only accepts a digest that names a frozen sample.
type PlateRequest struct{ RequestKey, InputID, BackgroundIntent, OriginalSHA256 string }

// PlateReady reports whether new plate-lock runs may be created and why not.
// Reason codes are stable API: plate_lock_disabled, sample_set_unconfigured,
// sample_set_invalid, plate_org_not_supported, source_unconfigured.
func (s *Service) PlateReady(payerSource string) (bool, string) {
	switch {
	case s == nil || !s.PlateEnabled:
		return false, "plate_lock_disabled"
	case s.Samples == nil:
		if s.SamplesReason == "sample_set_invalid" {
			return false, "sample_set_invalid"
		}
		return false, "sample_set_unconfigured"
	case payerSource != "personal":
		return false, "plate_org_not_supported"
	}
	if _, _, ready := s.CreationCapabilities(payerSource); !ready {
		return false, "source_unconfigured"
	}
	return true, ""
}

// CreatePlate freezes the sample binding, prompt and price profile, then runs
// the normal usage and quote flow. It returns a quote and never a Task.
func (s *Service) CreatePlate(ctx context.Context, actor Actor, project string, in PlateRequest) (si.Run, error) {
	if s == nil || s.Store == nil || s.Auth == nil || nilPort(s.Bill) {
		return si.Run{}, si.ErrUnconfigured
	}
	auth, e := s.Auth.ForProject(ctx, actor, project, true)
	if e != nil {
		return si.Run{}, e
	}
	if ready, _ := s.PlateReady(auth.PayerSource); !ready || !s.Profile.CanCreate(auth.PayerSource) {
		return si.Run{}, si.ErrUnconfigured
	}
	if !si.ValidIntentText(in.BackgroundIntent) || !fs.ValidIntent(in.BackgroundIntent) {
		return si.Run{}, si.ErrInvalid
	}
	c, ok := s.Samples.BySHA(in.OriginalSHA256)
	if !ok {
		return si.Run{}, si.ErrNotFrozen
	}
	frozen := si.PlateInput{
		CaseID: c.CaseID, SetID: s.Samples.ID(), SetSHA256: s.Samples.SHA256(), InputID: in.InputID,
		OriginalSHA256: c.Original.SHA256, MaskSHA256: c.Mask.SHA256, CoverageSHA256: c.Evidence.CoverageSHA256,
		CanvasW: c.Canvas.W, CanvasH: c.Canvas.H, BackgroundIntent: in.BackgroundIntent,
		FitID: platelock.FitID, ComposeID: platelock.ComposeID, ThresholdsVersion: fs.ThresholdsVersion,
	}
	if !frozen.Valid() {
		return si.Run{}, si.ErrInvalid
	}
	intent := si.Intent{
		Scope: auth.Scope, RequestKey: in.RequestKey, Mode: si.ModePlateLock, Prompt: bgreplace.ModelPlatePrompt(in.BackgroundIntent),
		Size: s.Profile.Size, Provider: s.Profile.Provider, Model: s.Profile.Model, Capability: s.Profile.Capability,
		PricingVersion: s.Profile.PricingVersion, Quantity: s.Profile.Quantity, PlateDigest: si.PlateDigest(frozen),
	}
	if kept, ok, e := s.reuseGeneration(ctx, intent); e != nil || ok {
		return kept, e
	}
	r, _, e := s.Store.CreateSourcePlateRunForProject(ctx, intent, frozen)
	if e != nil {
		return r, e
	}
	if r.Quote != nil {
		return r, nil
	}
	return s.RecoverQuote(ctx, r)
}

const (
	maxPlateExcluded = 500
	plateQuietAfter  = 8
	plateQuietDelay  = 15 * time.Minute
)

type plateBackoff struct {
	until time.Time
	tries int
}

func (s *Service) plateLock(id string) *sync.Mutex {
	m, _ := s.plateLocks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// DerivePlate produces (or returns) the run's single derivation record. It
// makes no Task, Usage, Quote or charge call: the only remote action is the
// verified download of the already paid plate. Repeating it, concurrently or
// after a restart, returns the same stored record.
func (s *Service) DerivePlate(ctx context.Context, id string) (si.PlateDerivation, error) {
	if s == nil || s.Store == nil {
		return si.PlateDerivation{}, si.ErrUnconfigured
	}
	mu := s.plateLock(id)
	mu.Lock()
	defer mu.Unlock()
	if d, ok, e := s.Store.GetPlateDerivation(ctx, id); e != nil || ok {
		return d, e
	}
	r, e := s.Store.GetSourceRunForRecovery(ctx, id)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	if r.Intent.Mode != si.ModePlateLock || r.Deleted || r.Output == nil || r.Task == nil || r.ChargedBill == nil || r.Quote == nil || si.ValidateReadyOutput(r, *r.Output) != nil {
		return si.PlateDerivation{}, si.ErrConflict
	}
	frozen, e := s.Store.GetPlateInput(ctx, r)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	if bgreplace.ModelPlatePrompt(frozen.BackgroundIntent) != r.Intent.Prompt {
		return si.PlateDerivation{}, si.ErrInvariant
	}
	if s.Samples == nil {
		return si.PlateDerivation{}, si.ErrUnconfigured
	}
	c, ok := s.Samples.BySHA(frozen.OriginalSHA256)
	if !ok || c.CaseID != frozen.CaseID || c.Mask.SHA256 != frozen.MaskSHA256 || c.Evidence.CoverageSHA256 != frozen.CoverageSHA256 || c.Canvas.W != frozen.CanvasW || c.Canvas.H != frozen.CanvasH {
		// The approved set changed after this run was priced. Nothing may be
		// regenerated or charged again; the paid plate reference stays.
		return s.Store.SavePlateDerivation(ctx, si.PlateDerivation{RunID: id, State: si.DerivationBlocked, BlockedReason: si.BlockedSampleSetChanged, CandidateState: si.CandidateBlocked})
	}
	plate, e := s.readPlate(ctx, r)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	out := platelock.Derive(platelock.Input{
		Case: c.Case, Original: c.OriginalBytes, Mask: c.MaskBytes, CoverageSHA256: c.Evidence.CoverageSHA256, Plate: plate,
		Facts: platelock.Facts{
			Mode: r.Intent.Mode, Provider: r.Intent.Provider, Model: r.Intent.Model, PricingVersion: r.Intent.PricingVersion,
			UsageID: r.Quote.UsageID, QuoteID: r.Quote.QuoteID, TaskID: r.TaskID, OriginalChargeID: r.ChargedBill.ChargeID,
			PlateAssetID: r.Output.AssetID, PlateReferenceID: r.Output.ReferenceID, PlateSHA256: r.Output.SHA256,
			TaskSucceeded: r.Task.Status == "succeeded", PlateHashVerified: true, ChargeSeen: r.ChargedBill.Status == "charged",
		},
		Build: s.Build,
	})
	report, e := platelock.ReportJSON(out.Report)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	d := si.PlateDerivation{RunID: id, State: si.DerivationDerived, PNG: out.Composite, ReportJSON: string(report), ReportSHA256: si.SHA256Hex(report), CandidateState: out.Report.CandidateState}
	if len(out.Composite) > 0 {
		d.SHA256, d.SizeBytes, d.Width, d.Height = si.SHA256Hex(out.Composite), int64(len(out.Composite)), c.Canvas.W, c.Canvas.H
	}
	return s.Store.SavePlateDerivation(ctx, d)
}

// readPlate downloads the paid plate and proves it is the verified Upload
// asset: exact size and SHA-256. Anything else is an invariant failure that
// stores nothing, so a swapped byte stream can never become a candidate.
func (s *Service) readPlate(ctx context.Context, r si.Run) ([]byte, error) {
	if nilPort(s.Assets) {
		return nil, si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, e := s.Assets.Download(ctx, r)
	if e != nil {
		return nil, e
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, 16<<20+1))
	if e != nil {
		return nil, si.ErrUnavailable
	}
	if int64(len(raw)) != r.Output.SizeBytes || si.SHA256Hex(raw) != r.Output.SHA256 {
		return nil, si.ErrInvariant
	}
	return raw, nil
}

// PlateContent is a derived composite read back for display or export.
type PlateContent struct {
	Run        si.Run
	Input      si.PlateInput
	Derivation si.PlateDerivation
}

// LoadPlate returns the run with its frozen input and derivation, proving the
// run still has a charged original bill (a later refund hides the result).
func (s *Service) LoadPlate(ctx context.Context, actor Actor, project, id string) (PlateContent, error) {
	r, e := s.LoadRun(ctx, actor, project, id, false)
	if e != nil {
		return PlateContent{}, e
	}
	if r.Intent.Mode != si.ModePlateLock || r.Deleted {
		return PlateContent{Run: r}, si.ErrConflict
	}
	frozen, e := s.Store.GetPlateInput(ctx, r)
	if e != nil {
		return PlateContent{Run: r}, e
	}
	d, ok, e := s.Store.GetPlateDerivation(ctx, r.ID)
	if e != nil || !ok {
		if e == nil {
			e = si.ErrConflict // not derived yet
		}
		return PlateContent{Run: r, Input: frozen}, e
	}
	if nilPort(s.Bill) {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bill, found, e := s.Bill.Get(ctx, r)
	if e != nil {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, e
	}
	if !found {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, si.ErrUnavailable
	}
	if si.ValidateSettlement(r, bill) != nil || bill.Status != "charged" {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, si.ErrConflict
	}
	return PlateContent{Run: r, Input: frozen, Derivation: d}, nil
}

// requireLimitedCandidate gates select, export and return.
func (s *Service) requireLimitedCandidate(ctx context.Context, r si.Run) error {
	d, ok, e := s.Store.GetPlateDerivation(ctx, r.ID)
	if e != nil {
		return e
	}
	if !ok {
		return si.ErrConflict
	}
	if d.CandidateState != si.CandidateLimited {
		return si.ErrNotUsable
	}
	return nil
}

// settlePlate lets an explicit reconcile (and the recovery loop) push a plate
// run from "Task finished" to "derived" without any new remote mutation.
func (s *Service) settlePlate(ctx context.Context, observed si.Run) {
	if observed.Intent.Mode != si.ModePlateLock || observed.ConfirmationHash == "" || observed.Task == nil || observed.Deleted {
		return
	}
	if observed.Output == nil && (observed.Phase == "asset_pending" || observed.Phase == "review_required") && (observed.Task.Status == "succeeded" || observed.Task.Status == "failed" || observed.Task.Status == "canceled") {
		next, _ := s.ObserveOutput(ctx, observed)
		observed = next
	}
	if observed.Output != nil && observed.Phase == "succeeded" {
		if _, e := s.DerivePlate(ctx, observed.ID); e != nil {
			// A derivation failure on a charged run must never be invisible: the
			// loop retries with backoff, and without this line nothing anywhere
			// would say why a paid run never becomes a candidate.
			log.Printf("sourceflow: plate derive %s: %v", observed.ID, e)
		}
	}
}

// derivePending retries derivations the loop owes, with per-run backoff so a
// persistently failing download is not hammered once a second.
func (s *Service) derivePending(ctx context.Context) {
	// Runs still backing off are excluded in the query itself, so a few runs that
	// keep failing can never occupy the whole window and starve newer ones.
	var waiting []string
	now := s.now()
	s.plateRetry.Range(func(k, v any) bool {
		if now.Before(v.(plateBackoff).until) && len(waiting) < maxPlateExcluded {
			waiting = append(waiting, k.(string))
		}
		return true
	})
	runs, e := s.Store.ListPlateRunsAwaitingDerivation(ctx, 3, waiting)
	if e != nil {
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil {
			return
		}
		if v, ok := s.plateRetry.Load(r.ID); ok && s.now().Before(v.(plateBackoff).until) {
			continue
		}
		if _, e := s.DerivePlate(ctx, r.ID); e != nil {
			log.Printf("sourceflow: plate derive retry %s: %v", r.ID, e)
			b, _ := s.plateRetry.Load(r.ID)
			tries := 1
			if prev, ok := b.(plateBackoff); ok {
				tries = prev.tries + 1
			}
			delay := time.Duration(min(1<<uint(min(tries, 6)), 60)) * time.Second
			if tries >= plateQuietAfter {
				// Persistently failing (hash mismatch, dead host): stop re-downloading up to
				// 16 MiB every minute. An explicit Reconcile still derives it at any time.
				delay = plateQuietDelay
			}
			s.plateRetry.Store(r.ID, plateBackoff{until: s.now().Add(delay), tries: tries})
			if errors.Is(e, context.Canceled) {
				return
			}
			continue
		}
		s.plateRetry.Delete(r.ID)
	}
}
