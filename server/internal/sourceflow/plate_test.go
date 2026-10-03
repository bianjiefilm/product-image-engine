package sourceflow

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// plateAssets is the Upload authority for the paid plate. It serves exactly
// the bytes the test hands it; Verify reports the matching ready asset.
type plateAssets struct {
	mu        sync.Mutex
	output    si.Output
	body      []byte
	tamper    func([]byte) []byte
	failNext  int             // the next N downloads fail as if the connection broke
	failRuns  map[string]bool // runs whose download never succeeds (persistent outage or tamper)
	downloads int
	verifies  int
}

func (a *plateAssets) Verify(context.Context, si.Run) (si.Output, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.verifies++
	return a.output, nil
}
func (a *plateAssets) Reference(context.Context, si.Outbox) (string, error) { return "active", nil }
func (a *plateAssets) Release(context.Context, si.Outbox) error             { return nil }
func (a *plateAssets) Download(_ context.Context, r si.Run) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.downloads++
	if a.failRuns[r.ID] {
		return nil, si.ErrUnavailable
	}
	if a.failNext > 0 {
		a.failNext--
		return nil, si.ErrUnavailable
	}
	body := append([]byte(nil), a.body...)
	if a.tamper != nil {
		body = a.tamper(body)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func gradientPlate(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*60/w), uint8(50 + y*90/h), uint8(110 + (x+y)*40/(w+h)), 255})
		}
	}
	return pngBytes(img)
}

type plateFixture struct {
	*fixture
	set    *fs.Set
	c      *fs.LoadedCase
	assets *plateAssets
}

func newPlateFixture(t *testing.T, caseID string) *plateFixture {
	t.Helper()
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	var c *fs.LoadedCase
	for _, x := range set.Cases() {
		if x.CaseID == caseID {
			c = x
		}
	}
	if c == nil {
		t.Fatalf("case %s", caseID)
	}
	f := &plateFixture{fixture: flowFixture(t), set: set, c: c, assets: &plateAssets{}}
	f.enable()
	return f
}

// enable (re)installs plate wiring; open() builds a fresh Service on reopen.
func (f *plateFixture) enable() {
	f.svc.PlateEnabled = true
	f.svc.Samples = f.set
	f.svc.Assets = f.assets
	f.svc.Build = platelock.Build{VCSRevision: "test-rev"}
}

func (f *plateFixture) request(key string) PlateRequest {
	return PlateRequest{RequestKey: key, InputID: "pin_test", BackgroundIntent: f.c.Intents[0], OriginalSHA256: f.c.Original.SHA256}
}

// succeeded drives one plate run through quote, confirm, submit and a
// successful, charged Task whose output is exactly `plate`.
func (f *plateFixture) succeeded(t *testing.T, key string, plate []byte) si.Run {
	t.Helper()
	ctx := t.Context()
	r, e := f.svc.CreatePlate(ctx, f.actor, f.project.ID, f.request(key))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	r, e = f.svc.Confirm(ctx, f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil {
		t.Fatal(e)
	}
	if r, e = f.svc.Advance(ctx, r.ID); e != nil {
		t.Fatal(r, e)
	}
	task := f.task.facts[taskKey(r)]
	o := si.Output{AssetID: "asset-plate", ReferenceID: "ref-plate", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: si.SHA256Hex(plate), ContentType: "image/png", SizeBytes: int64(len(plate))}
	task.Status, task.Phase, task.HoldID, task.ChargeID, task.Output = "succeeded", "succeeded", "hold-plate", "charge-plate", &o
	f.task.facts[taskKey(r)] = task
	b := f.bill.facts[billKey(r)]
	n := int64(25)
	b.Status, b.HoldID, b.ChargeID, b.ChargedMinor = "charged", "hold-plate", "charge-plate", &n
	f.bill.facts[billKey(r)] = b
	ready := o
	ready.Status, ready.ReferenceState = "ready", "active"
	f.assets.output, f.assets.body = ready, plate
	// The coordinator observes the finished Task, then the output attaches.
	r, e = f.svc.Advance(ctx, r.ID)
	if e != nil {
		t.Fatal(r, e)
	}
	r, e = f.svc.ObserveOutput(ctx, r)
	if e != nil || r.Output == nil || r.Phase != "succeeded" || r.ChargedBill == nil {
		t.Fatal(r, e)
	}
	return r
}

func TestPlateReadyReasonCodes(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	if ok, why := f.svc.PlateReady("personal"); !ok || why != "" {
		t.Fatal(ok, why)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Service)
		source string
		want   string
	}{
		{"flag off", func(s *Service) { s.PlateEnabled = false }, "personal", "plate_lock_disabled"},
		{"no sample dir", func(s *Service) { s.Samples = nil }, "personal", "sample_set_unconfigured"},
		{"invalid sample set", func(s *Service) { s.Samples, s.SamplesReason = nil, "sample_set_invalid" }, "personal", "sample_set_invalid"},
		{"organization payer", func(*Service) {}, "delegation", "plate_org_not_supported"},
		{"source runtime not ready", func(s *Service) { s.OutputRecoveryReady = false }, "personal", "source_unconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newPlateFixture(t, "carton-1024x1024")
			g.svc.OutputRecoveryReady = true
			tc.mutate(g.svc)
			if ok, why := g.svc.PlateReady(tc.source); ok || why != tc.want {
				t.Fatalf("PlateReady = %v %q, want %q", ok, why, tc.want)
			}
		})
	}
}

func TestCreatePlateFreezesInputAndQuotesWithoutATask(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-a"))
	if e != nil || r.Quote == nil || r.Intent.Mode != si.ModePlateLock {
		t.Fatal(r, e)
	}
	if r.Intent.Prompt != bgreplace.ModelPlatePrompt(f.c.Intents[0]) || r.Intent.Size != "1024*1024" || r.Intent.Quantity != 1 || r.Intent.Model != "qwen-image-2.0" {
		t.Fatalf("the price profile and prompt are server frozen: %+v", r.Intent)
	}
	frozen, e := f.store.GetPlateInput(t.Context(), r)
	if e != nil || frozen.OriginalSHA256 != f.c.Original.SHA256 || frozen.MaskSHA256 != f.c.Mask.SHA256 || frozen.CaseID != f.c.CaseID || frozen.BackgroundIntent != f.c.Intents[0] || si.PlateDigest(frozen) != r.Intent.PlateDigest {
		t.Fatal(frozen, e)
	}
	if f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 || f.task.Submits != 0 {
		t.Fatalf("a quote must not dispatch: usage=%d quote=%d submits=%d", f.bill.UsageCommits, f.bill.QuoteCommits, f.task.Submits)
	}
	again, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-a"))
	if e != nil || again.ID != r.ID || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatal("same key must replay the same run", again.ID, r.ID, e)
	}
	other := f.request("plate-a")
	other.BackgroundIntent = f.c.Intents[1]
	if _, e = f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, other); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("same key, different background: %v", e)
	}
}

func TestCreatePlateRefusesBeforeAnyRemoteCall(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	notFrozen := f.request("plate-b")
	notFrozen.OriginalSHA256 = si.SHA256Hex([]byte("a customer photo"))
	badIntent := f.request("plate-c")
	badIntent.BackgroundIntent = "line\nbreak"
	empty := f.request("plate-d")
	empty.BackgroundIntent = ""
	for name, tc := range map[string]struct {
		req  PlateRequest
		want error
	}{"F04 photo is not a frozen sample": {notFrozen, si.ErrNotFrozen}, "control character": {badIntent, si.ErrInvalid}, "empty background": {empty, si.ErrInvalid}} {
		if _, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, tc.req); !errors.Is(e, tc.want) {
			t.Fatalf("%s: %v want %v", name, e, tc.want)
		}
	}
	f.svc.PlateEnabled = false
	if _, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-e")); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(e)
	}
	if f.bill.UsageAttempts != 0 || f.bill.QuoteAttempts != 0 || f.task.Submits != 0 {
		t.Fatalf("refusals must not touch Billing or Task: %+v", f.bill)
	}
	runs, _ := f.store.ListSourceRuns(t.Context(), mustScope(t, f))
	if len(runs) != 0 {
		t.Fatalf("a refused request left %d runs", len(runs))
	}
}

func mustScope(t *testing.T, f *plateFixture) si.Scope {
	t.Helper()
	auth, e := f.svc.Auth.ForProject(t.Context(), f.actor, f.project.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	return auth.Scope
}

func TestDerivePlateOncePaidLockedAndIdempotent(t *testing.T) {
	f := newPlateFixture(t, "handled_metal-1024x1280")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-ok", gradientPlate(1024, 1024))
	d, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || d.State != si.DerivationDerived || d.CandidateState != si.CandidateLimited || d.Width != 1024 || d.Height != 1280 {
		t.Fatal(d.State, d.CandidateState, e)
	}
	rep, e := platelock.DecodeReport([]byte(d.ReportJSON))
	if e != nil || rep.Facts.TaskID != r.TaskID || rep.Facts.UsageID != r.Quote.UsageID || rep.Facts.QuoteID != r.Quote.QuoteID || rep.Facts.OriginalChargeID != "charge-plate" || rep.Facts.Plate.SHA256 != r.Output.SHA256 || rep.Facts.CompositeSHA256 != d.SHA256 || rep.Build.VCSRevision != "test-rev" {
		t.Fatalf("%+v %v", rep.Facts, e)
	}
	if _, e := bgreplace.SubjectPixelChecks(f.c.OriginalBytes, d.PNG, f.c.MaskBytes); e != nil {
		t.Fatalf("the stored composite must keep every protected pixel: %v", e)
	}
	again, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || again.SHA256 != d.SHA256 || again.ReportSHA256 != d.ReportSHA256 || f.assets.downloads != 1 {
		t.Fatalf("a repeat must return the stored record without another download (downloads=%d): %v", f.assets.downloads, e)
	}
	if f.task.Submits != 1 || f.task.Commits != 1 || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatalf("derivation made a remote mutation: submits=%d usage=%d quote=%d", f.task.Submits, f.bill.UsageCommits, f.bill.QuoteCommits)
	}
}

func TestF09SixteenConcurrentDerivationsStoreOneRecord(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-race", gradientPlate(1024, 1024))
	var wg sync.WaitGroup
	shas := make([]string, 16)
	errs := make([]error, 16)
	for i := range shas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, e := f.svc.DerivePlate(context.Background(), r.ID)
			shas[i], errs[i] = d.SHA256, e
		}()
	}
	wg.Wait()
	for i := range shas {
		if errs[i] != nil || shas[i] == "" || shas[i] != shas[0] {
			t.Fatalf("derivation %d: %q %v", i, shas[i], errs[i])
		}
	}
	if f.assets.downloads != 1 {
		t.Fatalf("16 callers downloaded the plate %d times", f.assets.downloads)
	}
}

func TestF08TamperedDownloadStoresNothingAndKeepsRetrying(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-tamper", gradientPlate(1024, 1024))
	f.assets.tamper = func(b []byte) []byte { b[len(b)/2] ^= 0xFF; return b }
	if _, e := f.svc.DerivePlate(t.Context(), r.ID); !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("a download that does not match the verified asset: %v", e)
	}
	if _, ok, _ := f.store.GetPlateDerivation(t.Context(), r.ID); ok {
		t.Fatal("a derivation was stored from bytes that failed verification")
	}
	waiting, e := f.store.ListPlateRunsAwaitingDerivation(t.Context(), 5, nil)
	if e != nil || len(waiting) != 1 || waiting[0].ID != r.ID {
		t.Fatal("the run must still be owed a derivation", waiting, e)
	}
	f.assets.tamper = nil
	if d, e := f.svc.DerivePlate(t.Context(), r.ID); e != nil || d.CandidateState != si.CandidateLimited {
		t.Fatal(d.CandidateState, e)
	}
	if f.task.Submits != 1 || f.bill.UsageCommits != 1 {
		t.Fatal("retrying a derivation must not spend again")
	}
}

func TestDerivePendingIsNotStarvedByPermanentlyFailingRuns(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	plate := gradientPlate(1024, 1024)
	f.assets.failRuns = map[string]bool{}
	var all []si.Run
	for _, k := range []string{"run-a", "run-b", "run-c", "run-d"} {
		all = append(all, f.succeeded(t, k, plate))
	}
	// The loop lists oldest first (updated_at, id): the three smallest ids are the
	// stuck ones and the largest is the healthy run queued behind them.
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	bad, healthy := all[:3], all[3]
	for _, r := range bad {
		f.assets.failRuns[r.ID] = true
	}
	ctx := t.Context()
	f.svc.derivePending(ctx) // the three oldest fail and enter backoff
	f.svc.derivePending(ctx) // still backing off: the newer healthy run must be reached now
	if d, ok, e := f.store.GetPlateDerivation(ctx, healthy.ID); e != nil || !ok || d.CandidateState != si.CandidateLimited {
		t.Fatal("a healthy run behind three failing ones was never derived by the loop", ok, d.CandidateState, e)
	}
	for _, r := range bad {
		if _, ok, _ := f.store.GetPlateDerivation(ctx, r.ID); ok {
			t.Fatal("a failing run must stay owed, not be derived")
		}
	}
	if f.task.Submits != 4 || f.bill.UsageCommits != 4 {
		t.Fatal("retrying derivations must not spend again", f.task.Submits, f.bill.UsageCommits)
	}
}

func TestDerivePendingStopsHammeringAfterRepeatedFailures(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	f.assets.failRuns = map[string]bool{}
	r := f.succeeded(t, "stuck-only", gradientPlate(1024, 1024))
	f.assets.failRuns[r.ID] = true
	ctx := t.Context()
	for i := 0; i < 12; i++ {
		f.svc.derivePending(ctx)
		f.now = f.now.Add(61 * time.Second)
	}
	before := f.assets.downloads
	f.svc.derivePending(ctx)
	f.now = f.now.Add(61 * time.Second)
	f.svc.derivePending(ctx)
	if f.assets.downloads != before {
		t.Fatalf("a run that kept failing is still retried every minute (%d -> %d downloads)", before, f.assets.downloads)
	}
}

func TestF07F06UnusablePlatesAreRecordedNotRegenerated(t *testing.T) {
	for name, tc := range map[string]struct {
		caseID string
		plate  func(f *plateFixture) []byte
		axis   string
	}{
		"background unchanged (plate is the original)": {"carton-1024x1024", func(f *plateFixture) []byte { return f.c.OriginalBytes }, platelock.AxisBackground},
		"wrong plate size": {"carton-1024x1024", func(*plateFixture) []byte { return gradientPlate(512, 512) }, platelock.AxisPlateSize},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPlateFixture(t, tc.caseID)
			f.svc.OutputRecoveryReady = true
			r := f.succeeded(t, "plate-bad", tc.plate(f))
			d, e := f.svc.DerivePlate(t.Context(), r.ID)
			if e != nil || d.CandidateState != si.CandidateNotUsable {
				t.Fatal(d.CandidateState, e)
			}
			rep, e := platelock.DecodeReport([]byte(d.ReportJSON))
			if e != nil || rep.Facts.OriginalChargeID != "charge-plate" {
				t.Fatalf("the paid charge fact must stay on the record: %+v %v", rep.Facts, e)
			}
			failed := false
			for _, a := range rep.Axes {
				if a.Name == tc.axis && a.State == platelock.Fail {
					failed = true
				}
			}
			if !failed {
				t.Fatalf("%s should FAIL: %+v", tc.axis, rep.Axes)
			}
			if _, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrNotUsable) {
				t.Fatalf("select of an unusable candidate: %v", e)
			}
			if f.task.Submits != 1 || f.task.Commits != 1 || f.bill.UsageCommits != 1 {
				t.Fatal("an unusable result must not trigger a second generation")
			}
		})
	}
}

func TestSelectIsGatedByTheDerivedCandidate(t *testing.T) {
	f := newPlateFixture(t, "glass_bottle-768x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-select", gradientPlate(1024, 1024))
	if _, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("before derivation: %v", e)
	}
	if _, e := f.svc.DerivePlate(t.Context(), r.ID); e != nil {
		t.Fatal(e)
	}
	got, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID)
	if e != nil || !got.Selected {
		t.Fatal(got.Selected, e)
	}
}

func TestPlateRunsNeverExposeTheRawPlate(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-raw", gradientPlate(1024, 1024))
	if _, e := f.svc.OpenOutput(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("OpenOutput on a plate run must refuse: %v", e)
	}
}

func TestF17ChangedSampleSetBlocksWithoutSpendingOrDownloading(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-set", gradientPlate(1024, 1024))
	// A different, individually valid set: same original, a different mask.
	dir := sampletest.Copy(t)
	setPixel := func(path string) {
		raw := mustRead(t, path)
		img, _ := png.Decode(bytes.NewReader(raw))
		n := image.NewNRGBA(img.Bounds())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		n.SetNRGBA(5, 5, color.NRGBA{255, 255, 255, 255}) // a stray protected pixel: valid, but a different mask
		mustWrite(t, path, pngBytes(n))
	}
	setPixel(dir + "/masks/carton-1024x1024.png")
	sampletest.Resign(t, dir, nil)
	changed, e := fs.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	f.svc.Samples = changed
	d, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || d.State != si.DerivationBlocked || d.BlockedReason != si.BlockedSampleSetChanged || d.CandidateState != si.CandidateBlocked {
		t.Fatal(d, e)
	}
	if f.assets.downloads != 0 || f.task.Submits != 1 || f.bill.UsageCommits != 1 {
		t.Fatalf("a blocked derivation must not download or spend: downloads=%d", f.assets.downloads)
	}
	if _, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrNotUsable) {
		t.Fatal(e)
	}
}

func TestF18ReopenKeepsRecordAndTwoRunsDeriveEqualBytes(t *testing.T) {
	a := newPlateFixture(t, "handled_metal-768x1024")
	a.svc.OutputRecoveryReady = true
	ra := a.succeeded(t, "plate-det", gradientPlate(1024, 1024))
	da, e := a.svc.DerivePlate(t.Context(), ra.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.reopen(t)
	a.enable()
	stored, ok, e := a.store.GetPlateDerivation(t.Context(), ra.ID)
	if e != nil || !ok || stored.SHA256 != da.SHA256 || stored.ReportSHA256 != da.ReportSHA256 {
		t.Fatal("record lost across reopen", e)
	}
	b := newPlateFixture(t, "handled_metal-768x1024")
	b.svc.OutputRecoveryReady = true
	rb := b.succeeded(t, "plate-det", gradientPlate(1024, 1024))
	db, e := b.svc.DerivePlate(t.Context(), rb.ID)
	if e != nil || db.SHA256 != da.SHA256 {
		t.Fatalf("the same inputs must derive the same composite: %s vs %s (%v)", db.SHA256, da.SHA256, e)
	}
}

func TestReconcileAndRecoveryLoopDriveDerivation(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1280")
	f.svc.OutputRecoveryReady = true
	ctx := t.Context()
	r, e := f.svc.CreatePlate(ctx, f.actor, f.project.ID, f.request("plate-rec"))
	if e != nil {
		t.Fatal(e)
	}
	if r, e = f.svc.Confirm(ctx, f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e != nil {
		t.Fatal(e)
	}
	if r, e = f.svc.Advance(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	plate := gradientPlate(1024, 1024)
	task := f.task.facts[taskKey(r)]
	o := si.Output{AssetID: "asset-rec", ReferenceID: "ref-rec", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: si.SHA256Hex(plate), ContentType: "image/png", SizeBytes: int64(len(plate))}
	task.Status, task.Phase, task.HoldID, task.ChargeID, task.Output = "succeeded", "succeeded", "hold-rec", "charge-rec", &o
	f.task.facts[taskKey(r)] = task
	b := f.bill.facts[billKey(r)]
	n := int64(25)
	b.Status, b.HoldID, b.ChargeID, b.ChargedMinor = "charged", "hold-rec", "charge-rec", &n
	f.bill.facts[billKey(r)] = b
	ready := o
	ready.Status, ready.ReferenceState = "ready", "active"
	f.assets.output, f.assets.body = ready, plate
	f.now = f.now.Add(61 * 1e9) // past retry/lease windows

	// The coordinator alone carries the run from a finished Task to a derivation.
	if e = f.svc.recoveryPass(ctx); e != nil {
		t.Fatal(e)
	}
	d, ok, e := f.store.GetPlateDerivation(ctx, r.ID)
	if e != nil || !ok || d.CandidateState != si.CandidateLimited {
		t.Fatal("recovery loop did not derive", ok, d.CandidateState, e)
	}
	if f.task.Submits != 1 || f.bill.UsageCommits != 1 {
		t.Fatal("recovery spent again")
	}
	// An explicit reconcile of a finished run is idempotent and returns the same record.
	got, e := f.svc.Reconcile(ctx, f.actor, f.project.ID, r.ID)
	if e != nil || got.Phase != "succeeded" || f.assets.downloads != 1 {
		t.Fatal(got.Phase, f.assets.downloads, e)
	}
}

func TestIntentRuleAgreesBetweenSampleSetAndSourceImage(t *testing.T) {
	for _, s := range []string{"深蓝色背景", "", " x", "x ", "a\nb", "界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界", "ok\t"} {
		if fs.ValidIntent(s) != si.ValidIntentText(s) {
			t.Fatalf("rules disagree on %q", s)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := readFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := writeFile(p, b); e != nil {
		t.Fatal(e)
	}
}

func TestFlagOffRefusesAnUnconfirmedQuoteButReplaysAConfirmedOne(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-flag"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	f.svc.PlateEnabled = false
	if _, e = f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatalf("an unconfirmed plate quote must not start while the flag is off: %v", e)
	}
	f.svc.PlateEnabled = true
	confirmed, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || confirmed.ConfirmationHash == "" {
		t.Fatal(confirmed, e)
	}
	f.svc.PlateEnabled = false
	again, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || again.ConfirmationHash != confirmed.ConfirmationHash {
		t.Fatalf("a replay of an existing confirmation must keep working: %v", e)
	}
}

func TestF09SixteenConcurrentConfirmsAndAdvancesSubmitExactlyOnce(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-confirm-race"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	hash := si.QuoteHash(*r.Quote)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := f.svc.Confirm(context.Background(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, hash); e == nil {
				_, _ = f.svc.Advance(context.Background(), r.ID)
			}
		}()
	}
	wg.Wait()
	if f.task.Submits != 1 || f.task.Commits != 1 || f.task.Dispatches != 1 {
		t.Fatalf("16 racing confirms must dispatch once: submits=%d commits=%d", f.task.Submits, f.task.Commits)
	}
	runs, _ := f.store.ListSourceRuns(t.Context(), mustScope(t, f))
	if len(runs) != 1 || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatalf("runs=%d usage=%d quote=%d", len(runs), f.bill.UsageCommits, f.bill.QuoteCommits)
	}
}

func TestF10InterruptedDownloadIsRetriedWithoutSpending(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-dl", gradientPlate(1024, 1024))
	f.assets.failNext = 1
	if _, e := f.svc.DerivePlate(t.Context(), r.ID); !errors.Is(e, si.ErrUnavailable) {
		t.Fatalf("an interrupted download: %v", e)
	}
	if _, ok, _ := f.store.GetPlateDerivation(t.Context(), r.ID); ok {
		t.Fatal("nothing may be stored from an interrupted download")
	}
	d, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || d.CandidateState != si.CandidateLimited || f.assets.downloads != 2 {
		t.Fatal(d.CandidateState, f.assets.downloads, e)
	}
	if f.task.Submits != 1 || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatal("retrying a download must not spend")
	}
}

func TestF10LostSubmitResponseRecoversThePlateRunWithoutASecondTask(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	ctx := t.Context()
	r, e := f.svc.CreatePlate(ctx, f.actor, f.project.ID, f.request("plate-lost"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	if r, e = f.svc.Confirm(ctx, f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e != nil {
		t.Fatal(e)
	}
	f.task.AfterCommit = "submit" // the platform accepted the Task but the reply was lost
	if _, e = f.svc.Advance(ctx, r.ID); e == nil {
		t.Fatal("the lost reply must surface as an error")
	}
	f.reopen(t)
	f.enable()
	f.svc.OutputRecoveryReady = true
	again, e := f.svc.Advance(ctx, r.ID)
	if e != nil || again.TaskID == "" || f.task.Submits != 1 || f.task.Commits != 1 {
		t.Fatalf("recovery must find the original Task by its key: task=%q submits=%d commits=%d err=%v", again.TaskID, f.task.Submits, f.task.Commits, e)
	}
	if f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatal("recovery must not create a second usage or quote")
	}
}

func TestF15ChangedPriceProfileCannotReuseAnOldPlateRun(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-price"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	if _, e = f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, "not-the-quoted-hash"); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("a confirmation that does not carry the quoted fingerprint: %v", e)
	}
	f.svc.Profile.PricingVersion = "test-price-v2"
	if _, e = f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-price")); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("the same key under another price version is a different request: %v", e)
	}
	f.svc.Profile.PricingVersion = "test-price-v1"
	f.svc.Profile.Model = "another-model"
	if _, e = f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-price-2")); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatalf("a model outside the priced SKU must not create runs: %v", e)
	}
	if f.task.Submits != 0 {
		t.Fatal("nothing may be dispatched")
	}
}
