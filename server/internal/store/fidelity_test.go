package store

import (
	"context"
	"errors"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
)

func TestFidelityReportImmutableAndTenantScoped(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p, err := s.CreateProject(ctx, Project{TenantID: "acct_a", Name: "保真", CreatedBy: "usr_a"})
	if err != nil {
		t.Fatal(err)
	}
	in := fidelity.Input{
		Mode: fidelity.ModeFidelity, MethodVersion: fidelity.MethodVersionV1,
		InputRef: "asset-in", InputVersion: "v1", OutputRef: "out-1", MaskRef: "mask-1",
		Protected:   append([]string(nil), fidelity.RequiredProtected...),
		SampleClass: fidelity.SampleLogoText, MachineScope: "像素", HumanScope: "目视",
		Checks: []fidelity.Check{{Name: fidelity.CheckLogoText, Result: fidelity.ResultFail}},
	}
	rep, err := fidelity.Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	saved, dup, err := s.SaveFidelityReport(ctx, "acct_a", p.ID, rep)
	if err != nil || dup || saved.ID == "" || saved.Verdict != fidelity.VerdictFail {
		t.Fatalf("首次保存: dup=%v err=%v %+v", dup, err, saved)
	}
	again, dup, err := s.SaveFidelityReport(ctx, "acct_a", p.ID, rep)
	if err != nil || !dup || again.ID != saved.ID {
		t.Fatalf("同指纹应幂等: dup=%v err=%v %+v", dup, err, again)
	}
	mut := rep
	mut.Verdict = fidelity.VerdictPass
	mut.ExactProduct = true
	mut.Deliverable = true
	if _, _, err := s.SaveFidelityReport(ctx, "acct_a", p.ID, mut); !errors.Is(err, ErrConflict) {
		t.Fatalf("改结论应冲突: %v", err)
	}
	listed, err := s.ListFidelityReports(ctx, "acct_a", p.ID)
	if err != nil || len(listed) != 1 || listed[0].Verdict != fidelity.VerdictFail {
		t.Fatalf("列表应仍是失败候选: %v %+v", err, listed)
	}
	if _, err := s.ListFidelityReports(ctx, "acct_b", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户列表应不可见: %v", err)
	}
	if _, err := s.GetFidelityReport(ctx, "acct_b", saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户应不可见: %v", err)
	}
}
