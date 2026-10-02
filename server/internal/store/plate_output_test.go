package store

import (
	"strings"
	"testing"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

func TestOutputQualityBindsOnlyAMatchingLimitedDerivation(t *testing.T) {
	s, _, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	run, _, _ := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	composite := []byte("\x89PNG-composite")
	report := `{"report_version":"product-source-quality/v2"}`
	if _, e := s.SavePlateDerivation(t.Context(), si.PlateDerivation{RunID: run.ID, State: si.DerivationDerived, PNG: composite, SHA256: si.SHA256Hex(composite), SizeBytes: int64(len(composite)), Width: 1024, Height: 1024, ReportJSON: report, ReportSHA256: si.SHA256Hex([]byte(report)), CandidateState: si.CandidateLimited}); e != nil {
		t.Fatal(e)
	}
	owner, hit, e := s.PlateDerivationOwnsSHA(t.Context(), "acct_u", proj.ID, si.SHA256Hex(composite))
	if e != nil || !hit || owner != run.ID {
		t.Fatalf("composite digest must resolve to its run: %q %v %v", owner, hit, e)
	}
	if _, hit, _ := s.PlateDerivationOwnsSHA(t.Context(), "acct_other", proj.ID, si.SHA256Hex(composite)); hit {
		t.Fatal("another tenant must not resolve it")
	}
	out, e := s.CreateOutput(t.Context(), ProjectOutput{TenantScope: "acct_u", ProjectID: proj.ID, PlatformAssetID: "asset_out", ResultSHA256: si.SHA256Hex(composite), ResultSize: int64(len(composite)), MediaType: "image/png", FileName: "x.png"})
	if e != nil {
		t.Fatal(e)
	}
	q := OutputQuality{OutputID: out.ID, TenantScope: "acct_u", ProjectID: proj.ID, RunID: run.ID, ResultSHA256: out.ResultSHA256, CandidateState: si.CandidateLimited, OriginalChargeID: "charge_a", ScopeJSON: `{"claim":"frozen_synthetic_sample_only"}`, LimitsJSON: `["limit"]`, ReportSHA256: si.SHA256Hex([]byte(report))}
	saved, e := s.SaveOutputQuality(t.Context(), q)
	if e != nil || saved.OriginalChargeID != "charge_a" {
		t.Fatal(saved, e)
	}
	if again, e := s.SaveOutputQuality(t.Context(), q); e != nil || again != saved {
		t.Fatalf("saving twice returns the stored binding: %v", e)
	}
	for name, mutate := range map[string]func(*OutputQuality){
		"another output digest": func(x *OutputQuality) { x.ResultSHA256 = strings.Repeat("9", 64) },
		"a failing candidate":   func(x *OutputQuality) { x.CandidateState = si.CandidateNotUsable },
		"another report":        func(x *OutputQuality) { x.ReportSHA256 = strings.Repeat("8", 64) },
		"no original charge":    func(x *OutputQuality) { x.OriginalChargeID = "" },
	} {
		bad := q
		bad.OutputID = "out_missing"
		mutate(&bad)
		if _, e := s.SaveOutputQuality(t.Context(), bad); e == nil {
			t.Fatalf("%s was bound", name)
		}
	}
	if _, e := s.db.Exec(`UPDATE project_output_quality SET candidate_state='limited_candidate'`); e == nil {
		t.Fatal("a quality binding is immutable")
	}
	list, e := s.ListOutputQualities(t.Context(), "acct_u", proj.ID)
	if e != nil || len(list) != 1 || list[0].OutputID != out.ID {
		t.Fatal(list, e)
	}
}
