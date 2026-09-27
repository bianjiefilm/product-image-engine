package store

import (
	"context"
	"errors"
	"testing"
)

func TestEntrySessionIsolatesPersonalFromCustomerSource(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	personalProject, err := s.CreateProject(ctx, Project{
		TenantID: "personal/default:acct_1", Name: "个人主图", CreatedBy: "usr_1",
		UsageKind: "电商主图", WidthPx: 800, HeightPx: 800, SourceType: "standalone",
	})
	if err != nil {
		t.Fatal(err)
	}
	orgProject, err := s.CreateProject(ctx, Project{
		TenantID: "org_customer", Name: "订单主图", CreatedBy: "usr_1",
		UsageKind: "电商主图", WidthPx: 800, HeightPx: 800,
		SourceType: "order", SourceRef: "ord_9",
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := s.InsertEntrySession(ctx, EntrySession{
		AccountID: "acct_1", WorkspaceKind: "personal", WorkspaceScope: "personal/default",
		StorageTenant: "personal/default:acct_1", ProjectID: personalProject.ID,
		SourceType: "standalone", Fingerprint: "fp-personal", Status: "failed",
		QuoteLabel: "待确认", DegradedReason: "generation_unavailable",
		StepCount: 5, InputCount: 1,
	})
	if err != nil || personal.ID == "" {
		t.Fatalf("个人会话: %v %+v", err, personal)
	}
	org, err := s.InsertEntrySession(ctx, EntrySession{
		AccountID: "acct_1", WorkspaceKind: "organization", WorkspaceScope: "org_customer",
		StorageTenant: "org_customer", ProjectID: orgProject.ID,
		SourceType: "order", SourceRef: "ord_9",
		ReturnHref: "https://order.example/orders/ord_9",
		Fingerprint: "fp-order", Status: "queued", QuoteLabel: "待确认",
		AuthorizedAssets: []string{"asset_9"},
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListEntrySessions(ctx, "acct_1", "personal/default")
	if err != nil || len(listed) != 1 || listed[0].ProjectID != personalProject.ID {
		t.Fatalf("个人列表不能混入客户工程: %v %+v", err, listed)
	}
	if _, err := s.GetEntryByFingerprint(ctx, "personal/default:acct_1", "fp-order"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("个人空间不能按客户指纹取回: %v", err)
	}
	again, err := s.GetEntryByFingerprint(ctx, "org_customer", "fp-order")
	if err != nil || again.ID != org.ID || again.ReturnHref != "https://order.example/orders/ord_9" {
		t.Fatalf("来源会话应原样取回: %v %+v", err, again)
	}
	if len(again.AuthorizedAssets) != 1 || again.AuthorizedAssets[0] != "asset_9" {
		t.Fatalf("已授权素材应保留: %+v", again.AuthorizedAssets)
	}
	found, err := s.FindProjectBySource(ctx, "org_customer", "order", "ord_9")
	if err != nil || found.ID != orgProject.ID {
		t.Fatalf("组织来源工程: %v %+v", err, found)
	}
	if _, err := s.FindProjectBySource(ctx, "personal/default:acct_1", "order", "ord_9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("个人空间不应看见客户来源工程: %v", err)
	}
	if _, err := s.InsertEntrySession(ctx, org); !errors.Is(err, ErrConflict) {
		t.Fatalf("同指纹重复插入应冲突: %v", err)
	}
	running := "running"
	recovery := 1
	updated, err := s.UpdateEntrySession(ctx, org.StorageTenant, org.ID, EntryPatch{
		Status: &running, RecoveryCount: &recovery,
	})
	if err != nil || updated.ID != org.ID || updated.Status != "running" || updated.RecoveryCount != 1 {
		t.Fatalf("恢复应写回同一会话: %v %+v", err, updated)
	}
}
