package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

func plateFrozen(intent string) si.PlateInput {
	h := strings.Repeat("a", 64)
	return si.PlateInput{CaseID: "carton-1024x1024", SetID: "frozen-sample-set", SetSHA256: h, InputID: "pin_a", OriginalSHA256: strings.Repeat("b", 64), MaskSHA256: strings.Repeat("c", 64), CoverageSHA256: strings.Repeat("d", 64), CanvasW: 1024, CanvasH: 1024, BackgroundIntent: intent, FitID: "fit-cover-center-catmullrom/v1", ComposeID: "compose-mask-lock/v1", ThresholdsVersion: "bg-lock-thresholds/v1"}
}

func plateIntent(project string, p si.PlateInput) si.Intent {
	in := sourceIntent()
	in.Scope.ProjectID = project
	in.Mode, in.Prompt, in.PlateDigest = si.ModePlateLock, "只生成背景："+p.BackgroundIntent, si.PlateDigest(p)
	return in
}

func openPlateStore(t *testing.T) (*Store, string, Project) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plate.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	proj, e := s.CreateProject(t.Context(), Project{TenantID: "acct_u", CreatedBy: "usr_u", Name: "plate", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	return s, path, proj
}

func TestPlateRunInputIsFrozenImmutableAndSurvivesReopen(t *testing.T) {
	s, path, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	run, dup, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	if e != nil || dup || run.Intent.PlateDigest != si.PlateDigest(frozen) {
		t.Fatal(run, dup, e)
	}
	again, dup, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	if e != nil || !dup || again.ID != run.ID {
		t.Fatal("same key and same frozen input must replay the run", again.ID, run.ID, dup, e)
	}
	var n int
	if e := s.db.QueryRow(`SELECT count(*) FROM product_plate_inputs`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("a replay must not add a second frozen input: %d %v", n, e)
	}
	other := plateFrozen("暖色原木桌面")
	if _, _, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, other), other); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("same key, different background: %v", e)
	}
	bad := plateIntent(proj.ID, frozen)
	bad.PlateDigest = strings.Repeat("0", 64)
	if _, _, e := s.CreateSourcePlateRunForProject(t.Context(), bad, frozen); !errors.Is(e, si.ErrInvalid) {
		t.Fatalf("intent digest must match the frozen input: %v", e)
	}
	if _, e := s.db.Exec(`UPDATE product_plate_inputs SET digest=? WHERE run_id=?`, strings.Repeat("e", 64), run.ID); e == nil {
		t.Fatal("a frozen input must be immutable")
	}
	if _, e := s.db.Exec(`DELETE FROM product_plate_inputs WHERE run_id=?`, run.ID); e == nil {
		t.Fatal("a frozen input must not be deletable")
	}
	s.Close()
	s2, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	reread, e := s2.GetSourceRunForRecovery(t.Context(), run.ID)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s2.GetPlateInput(t.Context(), reread)
	if e != nil || got != frozen {
		t.Fatalf("frozen input after reopen: %+v %v", got, e)
	}
	// Old rows stay byte stable: a text intent never gains a PlateDigest key.
	raw, _ := json.Marshal(sourceIntent())
	if strings.Contains(string(raw), "PlateDigest") {
		t.Fatalf("legacy intent JSON changed: %s", raw)
	}
	// A frozen input read against the wrong run fails closed.
	tampered := reread
	tampered.Intent.PlateDigest = strings.Repeat("f", 64)
	if _, e := s2.GetPlateInput(t.Context(), tampered); !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("digest mismatch must be an invariant failure: %v", e)
	}
}

func TestPlateDerivationIsWriteOnceAndVerifiedOnRead(t *testing.T) {
	s, _, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	run, _, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	if e != nil {
		t.Fatal(e)
	}
	png1 := []byte("\x89PNG-first")
	report := `{"report_version":"product-source-quality/v2"}`
	d := si.PlateDerivation{RunID: run.ID, State: si.DerivationDerived, PNG: png1, SHA256: si.SHA256Hex(png1), SizeBytes: int64(len(png1)), Width: 1024, Height: 1024, ReportJSON: report, ReportSHA256: si.SHA256Hex([]byte(report)), CandidateState: si.CandidateLimited}
	saved, e := s.SavePlateDerivation(t.Context(), d)
	if e != nil || saved.SHA256 != d.SHA256 {
		t.Fatal(saved, e)
	}
	png2 := []byte("\x89PNG-second")
	d2 := d
	d2.PNG, d2.SHA256, d2.SizeBytes = png2, si.SHA256Hex(png2), int64(len(png2))
	kept, e := s.SavePlateDerivation(t.Context(), d2)
	if e != nil || kept.SHA256 != d.SHA256 {
		t.Fatalf("the first derivation wins: %+v %v", kept, e)
	}
	if _, e := s.db.Exec(`UPDATE product_plate_derivations SET candidate_state='limited_candidate' WHERE run_id=?`, run.ID); e == nil {
		t.Fatal("a derivation is write-once")
	}
	for name, mutate := range map[string]func(*si.PlateDerivation){
		"digest not matching the bytes": func(x *si.PlateDerivation) { x.SHA256 = strings.Repeat("0", 64) },
		"report digest wrong":           func(x *si.PlateDerivation) { x.ReportSHA256 = strings.Repeat("0", 64) },
		"unknown candidate state":       func(x *si.PlateDerivation) { x.CandidateState = "great" },
		"limited without a composite":   func(x *si.PlateDerivation) { x.PNG, x.SHA256, x.SizeBytes = nil, "", 0 },
	} {
		bad := d
		bad.RunID = "sir_other"
		mutate(&bad)
		if _, e := s.SavePlateDerivation(t.Context(), bad); !errors.Is(e, si.ErrInvariant) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
	// Corruption on disk is caught when read, never served.
	if _, e := s.db.Exec(`DROP TRIGGER product_plate_derivations_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`UPDATE product_plate_derivations SET derived_png=? WHERE run_id=?`, []byte("tampered"), run.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.GetPlateDerivation(t.Context(), run.ID); !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("tampered bytes must fail verification on read: %v", e)
	}
}

func TestF09SixteenConcurrentPlateCreatesYieldOneRunAndOneFrozenInput(t *testing.T) {
	s, _, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	ids := make([]string, 16)
	errs := make([]error, 16)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
			ids[i], errs[i] = r.ID, e
		}()
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("create %d: %q %v", i, ids[i], errs[i])
		}
	}
	var runs, inputs int
	if e := s.db.QueryRow(`SELECT count(*) FROM product_source_runs`).Scan(&runs); e != nil || runs != 1 {
		t.Fatal(runs, e)
	}
	if e := s.db.QueryRow(`SELECT count(*) FROM product_plate_inputs`).Scan(&inputs); e != nil || inputs != 1 {
		t.Fatal(inputs, e)
	}
}
