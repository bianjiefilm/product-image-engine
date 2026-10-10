package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCampaignSelectionIsExplicitAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "campaign.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	p, err := s.CreateProject(ctx, Project{
		TenantID: "tenant-77", Name: "活动工程", CreatedBy: "u1",
		SourceType: "campaign", SourceRef: "src-camp",
	})
	if err != nil {
		t.Fatalf("工程: %v", err)
	}
	b := mkBinding(t, s, p.ID, "主图")
	snap, err := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-camp", ContentFP: "fp-camp",
		SourceKind: "campaign", SourceApp: "touch-engine", PrincipalID: "principal-pay-1",
		SourceRevision: "rev-1", BriefVersion: "brief-1",
		ProfileJSON: `{"source_kind":"campaign","campaign_ref":"camp-2026-091"}`,
	})
	if err != nil {
		t.Fatalf("快照: %v", err)
	}
	out, err := s.CreateOutput(ctx, ProjectOutput{
		TenantScope: "tenant-77", ProjectID: p.ID, PlatformAssetID: "asset_camp",
		ResultSHA256: strings.Repeat("a", 64), ResultSize: 100, MediaType: "image/png",
		FileName: "chosen.png", SnapshotID: snap.ID, BriefVersion: "brief-1", SourceRevision: "rev-1",
	})
	if err != nil {
		t.Fatalf("输出: %v", err)
	}
	if _, err := s.GetCampaignSelection(ctx, "tenant-77", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("登记成果本身不是选定: %v", err)
	}
	sel, same, err := s.PutCampaignSelection(ctx, CampaignSelection{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: out.ID, SnapshotID: snap.ID,
		CampaignRef: "camp-2026-091", BriefVersion: "brief-1", SourceRevision: "rev-1",
	})
	if err != nil || same || sel.OutputID != out.ID {
		t.Fatalf("首次选定: same=%v err=%v sel=%+v", same, err, sel)
	}
	again, same, err := s.PutCampaignSelection(ctx, CampaignSelection{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: out.ID, SnapshotID: snap.ID,
		CampaignRef: "camp-2026-091", BriefVersion: "brief-1", SourceRevision: "rev-1",
	})
	if err != nil || !same || again.ID != sel.ID || again.UpdatedAt != sel.UpdatedAt {
		t.Fatalf("同版重选应幂等: same=%v err=%v %+v", same, err, again)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	got, err := reopened.GetCampaignSelection(ctx, "tenant-77", p.ID)
	if err != nil || got.OutputID != out.ID || got.CampaignRef != "camp-2026-091" || got.SourceRevision != "rev-1" {
		t.Fatalf("重开后选定版本丢失: %v %+v", err, got)
	}
	if _, err := reopened.GetCampaignSelection(ctx, "other", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户必须不可见: %v", err)
	}
}

func TestReselectMovesPointerWithoutRewritingReceipt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p, err := s.CreateProject(ctx, Project{
		TenantID: "tenant-77", Name: "活动工程", CreatedBy: "u1",
		SourceType: "campaign", SourceRef: "src-camp",
	})
	if err != nil {
		t.Fatalf("工程: %v", err)
	}
	b := mkBinding(t, s, p.ID, "主图")
	snap, err := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-camp-move", ContentFP: "fp-camp-move",
		SourceKind: "campaign", SourceApp: "touch-engine", PrincipalID: "principal-pay-1",
		SourceRevision: "rev-1", BriefVersion: "brief-1",
		ProfileJSON: `{"source_kind":"campaign","campaign_ref":"camp-2026-091"}`,
	})
	if err != nil {
		t.Fatalf("快照: %v", err)
	}
	first, err := s.CreateOutput(ctx, ProjectOutput{
		TenantScope: "tenant-77", ProjectID: p.ID, PlatformAssetID: "asset_camp_1",
		ResultSHA256: strings.Repeat("a", 64), ResultSize: 100, MediaType: "image/png",
		FileName: "first.png", SnapshotID: snap.ID, BriefVersion: "brief-1", SourceRevision: "rev-1",
	})
	if err != nil {
		t.Fatalf("第一版: %v", err)
	}
	second, err := s.CreateOutput(ctx, ProjectOutput{
		TenantScope: "tenant-77", ProjectID: p.ID, PlatformAssetID: "asset_camp_2",
		ResultSHA256: strings.Repeat("b", 64), ResultSize: 120, MediaType: "image/png",
		FileName: "second.png", SnapshotID: snap.ID, BriefVersion: "brief-1", SourceRevision: "rev-1",
	})
	if err != nil {
		t.Fatalf("第二版: %v", err)
	}
	if _, _, err := s.PutCampaignSelection(ctx, CampaignSelection{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: first.ID, SnapshotID: snap.ID,
		CampaignRef: "camp-2026-091", BriefVersion: "brief-1", SourceRevision: "rev-1",
	}); err != nil {
		t.Fatalf("选定第一版: %v", err)
	}
	receipt, err := s.CreateReceipt(ctx, SourceReceipt{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: first.ID,
		EventID: "wr-frozen-camp", HandoffID: "h-camp-move", TargetApp: "touch-engine",
		RunID: "manual-" + first.ID, Sequence: 1,
		Payload:   `{"schema_version":"order-receipt/v1","event_id":"wr-frozen-camp"}`,
		PayloadFP: strings.Repeat("c", 64), Status: "delivered",
	})
	if err != nil {
		t.Fatalf("回执: %v", err)
	}
	moved, same, err := s.PutCampaignSelection(ctx, CampaignSelection{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: second.ID, SnapshotID: snap.ID,
		CampaignRef: "camp-2026-091", BriefVersion: "brief-1", SourceRevision: "rev-1",
	})
	if err != nil || same || moved.OutputID != second.ID {
		t.Fatalf("改选应移动指针: same=%v err=%v %+v", same, err, moved)
	}
	kept, err := s.GetReceipt(ctx, "tenant-77", receipt.ID)
	if err != nil || kept.OutputID != first.ID || kept.Status != "delivered" || kept.EventID != "wr-frozen-camp" || kept.Payload != receipt.Payload {
		t.Fatalf("改选不得改写已有回执: %v %+v", err, kept)
	}
	if _, err := s.FindReceiptByOutput(ctx, "tenant-77", second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("新选定版本不得继承旧回执: %v", err)
	}
}
