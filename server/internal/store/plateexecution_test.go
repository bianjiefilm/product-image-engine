package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPlateExecutionClaimsOnceAndFreezesQuote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	job := insertSubmittingBg(t, s)
	in := PlateExecution{TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID, Fingerprint: "immutable", Request: PlateRequest{PayerID: "payer", QuoteID: "quote", AmountMinor: 125, OriginalHash: "original", MaskHash: "mask", CoverageHash: "coverage", Model: "model", Provider: "xingmo", Size: "1024x1024", ParamsJSON: `{"prompt":"studio"}`, Mask: []byte("mask")}}
	in.Fingerprint = PlateFingerprint(in)
	got, err := s.PreparePlateExecution(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "quoted" {
		t.Fatal(got.State)
	}
	if won, err := s.ClaimPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID); err != nil || won {
		t.Fatal("unconfirmed execution claimed")
	}
	if err = s.ConfirmPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID, in.Fingerprint); err != nil {
		t.Fatal(err)
	}
	changed := in
	changed.Fingerprint = "different"
	changed.Request.PayerID = "other"
	changed.Fingerprint = PlateFingerprint(changed)
	if _, err = s.PreparePlateExecution(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed quote: %v", err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := s.ClaimPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID)
			if err != nil {
				t.Error(err)
			}
			if won {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("winners %d", winners.Load())
	}
	if err = s.UpdatePlateTask(ctx, in.TenantID, in.ProjectID, in.JobID, "unknown", "", ""); err != nil {
		t.Fatal(err)
	}
	if won, err := s.ClaimPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID); err != nil || won {
		t.Fatal("unknown execution regenerated")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := s.GetPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err != nil || reopened.Request.QuoteID != "quote" || reopened.Fingerprint != in.Fingerprint {
		t.Fatalf("lost snapshot %+v %v", reopened, err)
	}
	if _, err = s.GetPlateExecution(ctx, "other", in.ProjectID, in.JobID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross tenant visible")
	}
}

func TestPlateFingerprintBindsAuthorizationAndInputTuple(t *testing.T) {
	base := PlateExecution{TenantID: "tenant", ProjectID: "project", JobID: "job", Request: PlateRequest{PayerID: "payer", QuoteID: "quote", MaskHash: "mask", OriginalHash: "original", AmountMinor: 123}}
	want := PlateFingerprint(base)
	for _, field := range []string{"tenant", "project", "payer", "quote", "mask", "original", "amount"} {
		changed := base
		switch field {
		case "tenant":
			changed.TenantID = "other"
		case "project":
			changed.ProjectID = "other"
		case "payer":
			changed.Request.PayerID = "other"
		case "quote":
			changed.Request.QuoteID = "other"
		case "mask":
			changed.Request.MaskHash = "other"
		case "original":
			changed.Request.OriginalHash = "other"
		case "amount":
			changed.Request.AmountMinor++
		}
		if PlateFingerprint(changed) == want {
			t.Fatalf("fingerprint ignores %s", field)
		}
	}
}

func TestPlateExecutionCannotSwapTaskOnRecovery(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	job := insertSubmittingBg(t, s)
	in := PlateExecution{TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID, Fingerprint: "immutable", Request: PlateRequest{PayerID: "payer", QuoteID: "quote", AmountMinor: 1, OriginalHash: "a", MaskHash: "m", CoverageHash: "c", Model: "m", Provider: "p", Size: "s", ParamsJSON: `{}`, Mask: []byte("m")}}
	in.Fingerprint = PlateFingerprint(in)
	if _, err := s.PreparePlateExecution(ctx, in); err != nil {
		t.Fatal(err)
	}
	s.ConfirmPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID, in.Fingerprint)
	s.ClaimPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err := s.UpdatePlateTask(ctx, in.TenantID, in.ProjectID, in.JobID, "running", "task-one", "hold-one"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePlateTask(ctx, in.TenantID, in.ProjectID, in.JobID, "completed", "task-other", "hold-other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("task substitution %v", err)
	}
}
