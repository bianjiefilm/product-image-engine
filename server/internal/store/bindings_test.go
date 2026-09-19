package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkBinding(t *testing.T, s *Store, projectID, purpose string) SourceBinding {
	t.Helper()
	b, err := s.CreateBinding(context.Background(), SourceBinding{
		TenantScope: "tenant-77", SourceApp: "orders", SourceRef: "order-project-a",
		TargetApp: "product-image", Purpose: purpose, ProjectID: projectID,
		CurrentSnapshotID: "snap_x",
	})
	if err != nil {
		t.Fatalf("create binding: %v", err)
	}
	return b
}

// TestBindingUniqueKeyIsFiveTuple 票面唯一键逐字落实:
// tenant_scope+source_app+source_ref+target_app+purpose 同键只绑定一次;
// 任一分量不同都是不同绑定(不得仅凭 source_ref 判重)。
func TestBindingUniqueKeyIsFiveTuple(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p1, err := s.CreateProject(ctx, Project{TenantID: "tenant-77", Name: "A", CreatedBy: "u1"})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	p2, err := s.CreateProject(ctx, Project{TenantID: "tenant-77", Name: "B", CreatedBy: "u1"})
	if err != nil {
		t.Fatalf("project: %v", err)
	}

	b1 := mkBinding(t, s, p1.ID, "主图")
	if b1.ID == "" || b1.CurrentSnapshotID != "snap_x" {
		t.Fatalf("绑定创建异常: %+v", b1)
	}
	// 同五元组重复绑定 → 冲突(同 handoff 并发/重复只绑定一次)。
	_, err = s.CreateBinding(ctx, SourceBinding{
		TenantScope: "tenant-77", SourceApp: "orders", SourceRef: "order-project-a",
		TargetApp: "product-image", Purpose: "主图", ProjectID: p2.ID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("同五元组应冲突: %v", err)
	}
	// source_ref 相同但 purpose 不同 → 不同绑定(不得仅凭 source_ref 判重)。
	b2 := mkBinding(t, s, p2.ID, "详情页")
	if b2.ID == b1.ID {
		t.Fatalf("不同 purpose 应是新绑定")
	}
	// source_ref 相同但 source_app 不同 → 不同绑定。
	b3, err := s.CreateBinding(ctx, SourceBinding{
		TenantScope: "tenant-77", SourceApp: "campaign-tool", SourceRef: "order-project-a",
		TargetApp: "product-image", Purpose: "主图", ProjectID: p2.ID,
	})
	if err != nil {
		t.Fatalf("不同 source_app 应允许: %v", err)
	}
	if b3.ID == b1.ID {
		t.Fatalf("不同 source_app 应是新绑定")
	}
	// source_ref 相同但 tenant 不同 → 不同绑定(跨租户互不可见)。
	b4, err := s.CreateBinding(ctx, SourceBinding{
		TenantScope: "tenant-88", SourceApp: "orders", SourceRef: "order-project-a",
		TargetApp: "product-image", Purpose: "主图", ProjectID: p2.ID,
	})
	if err != nil {
		t.Fatalf("不同租户应允许: %v", err)
	}
	if b4.ID == b1.ID {
		t.Fatalf("不同租户应是新绑定")
	}
	// 五元组查询命中。
	got, err := s.FindBindingByKey(ctx, BindingKey{
		TenantScope: "tenant-77", SourceApp: "orders", SourceRef: "order-project-a",
		TargetApp: "product-image", Purpose: "主图",
	})
	if err != nil || got.ID != b1.ID {
		t.Fatalf("五元组查询应命中 b1: %v %+v", err, got)
	}
	// purpose 为空拒绝。
	if _, err := s.CreateBinding(ctx, SourceBinding{TenantScope: "t", SourceApp: "a", SourceRef: "r", TargetApp: "x", Purpose: " ", ProjectID: p1.ID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("空 purpose 应拒绝: %v", err)
	}
	// 跨租户按 id 不可见。
	if _, err := s.GetBinding(ctx, "tenant-88", b1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户读绑定应同型不存在: %v", err)
	}
}

// TestSnapshotSemantics 快照语义:同 handoff_id 同内容 SAME;异内容 CONFLICT 保留原快照;
// 新 handoff_id 新快照;内容列不可变。
func TestSnapshotSemantics(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p, _ := s.CreateProject(ctx, Project{TenantID: "tenant-77", Name: "A", CreatedBy: "u1"})
	b := mkBinding(t, s, p.ID, "主图")

	v1, err := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-1", ContentFP: "fp-1",
		SourceKind: "order", SourceApp: "orders", PrincipalID: "principal-a",
		SourceRevision: "rev-1", BriefVersion: "12", ProfileJSON: `{"v":1}`,
	})
	if err != nil {
		t.Fatalf("创建快照: %v", err)
	}
	// 同 handoff_id 再写 → 冲突(调用方先 Find 决定 SAME/CONFLICT)。
	if _, err := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-1", ContentFP: "fp-1",
		SourceKind: "order", SourceApp: "orders", PrincipalID: "p", SourceRevision: "rev-1",
		BriefVersion: "12", ProfileJSON: `{"v":1}`,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("同 handoff_id 重复写应冲突: %v", err)
	}
	// 原快照保留且内容不变(CONFLICT 不覆盖)。
	got, err := s.FindSnapshotByHandoff(ctx, "tenant-77", "h-1")
	if err != nil || got.ID != v1.ID || got.ContentFP != "fp-1" || got.ProfileJSON != `{"v":1}` {
		t.Fatalf("原快照必须保留不覆盖: %v %+v", err, got)
	}
	// 新 handoff_id → 新快照(新需求版本)。
	v2, err := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-2", ContentFP: "fp-2",
		SourceKind: "order", SourceApp: "orders", PrincipalID: "principal-a",
		SourceRevision: "rev-2", BriefVersion: "13", ProfileJSON: `{"v":2}`,
	})
	if err != nil || v2.ID == v1.ID {
		t.Fatalf("新 handoff_id 应新快照: %v", err)
	}
	// 同租户同 handoff_id,异租户互不影响。
	if _, err := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-88", BindingID: b.ID, HandoffID: "h-1", ContentFP: "fp-x",
		SourceKind: "order", SourceApp: "orders", PrincipalID: "p", SourceRevision: "rev-1",
		BriefVersion: "12", ProfileJSON: `{}`,
	}); err != nil {
		t.Fatalf("异租户同 handoff_id 应允许: %v", err)
	}
	// 采用:绑定指针移动;旧快照行原样保留。
	ab, err := s.AdoptBindingSnapshot(ctx, "tenant-77", b.ID, v2.ID)
	if err != nil || ab.CurrentSnapshotID != v2.ID {
		t.Fatalf("采用失败: %v %+v", err, ab)
	}
	old1, _ := s.GetSnapshot(ctx, "tenant-77", v1.ID)
	if old1.BriefVersion != "12" || old1.SourceRevision != "rev-1" {
		t.Fatalf("旧快照必须不可变: %+v", old1)
	}
	list, err := s.ListSnapshotsForBinding(ctx, "tenant-77", b.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("绑定快照列表应 2 条: %v %d", err, len(list))
	}
}

// TestRedemptionOnce 一次性兑换:同 tuple 重放 SAME_RESULT;异 tuple 冲突。
func TestRedemptionOnce(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p, _ := s.CreateProject(ctx, Project{TenantID: "tenant-77", Name: "A", CreatedBy: "u1"})
	b := mkBinding(t, s, p.ID, "主图")
	v, _ := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-1", ContentFP: "fp-1",
		SourceKind: "order", SourceApp: "orders", PrincipalID: "p", SourceRevision: "rev-1",
		BriefVersion: "12", ProfileJSON: `{}`,
	})

	r1, err := s.CreateRedemption(ctx, Redemption{
		BindingRef: "binding-t-1", TenantScope: "tenant-77", ContentFP: "fp-1", SnapshotID: v.ID,
	})
	if err != nil {
		t.Fatalf("首次兑换: %v", err)
	}
	// 同 binding_ref 重放:调用方先 Find,核对 tuple 一致 → SAME_RESULT(不重复消费)。
	got, err := s.FindRedemption(ctx, "binding-t-1")
	if err != nil || got.SnapshotID != r1.SnapshotID || got.TenantScope != "tenant-77" || got.ContentFP != "fp-1" {
		t.Fatalf("兑换重放应命中原记录: %v %+v", err, got)
	}
	// 异 tuple(异租户)盗用同 binding_ref → 冲突。
	if _, err := s.CreateRedemption(ctx, Redemption{
		BindingRef: "binding-t-1", TenantScope: "tenant-88", ContentFP: "fp-1", SnapshotID: v.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("异 tuple 兑换应冲突: %v", err)
	}
	// 异内容同 ref → 冲突。
	if _, err := s.CreateRedemption(ctx, Redemption{
		BindingRef: "binding-t-1", TenantScope: "tenant-77", ContentFP: "fp-9", SnapshotID: v.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("异内容兑换应冲突: %v", err)
	}
}

// TestOutputAndReceiptLifecycle 输出登记幂等 + 回执 payload 不可变 + 状态机。
func TestOutputAndReceiptLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p, _ := s.CreateProject(ctx, Project{TenantID: "tenant-77", Name: "A", CreatedBy: "u1"})
	b := mkBinding(t, s, p.ID, "主图")
	v, _ := s.CreateSnapshot(ctx, HandoffSnapshot{
		TenantScope: "tenant-77", BindingID: b.ID, HandoffID: "h-1", ContentFP: "fp-1",
		SourceKind: "order", SourceApp: "orders", PrincipalID: "p", SourceRevision: "rev-1",
		BriefVersion: "12", ProfileJSON: `{}`,
	})

	o1, err := s.CreateOutput(ctx, ProjectOutput{
		TenantScope: "tenant-77", ProjectID: p.ID, PlatformAssetID: "asset_1",
		ResultSHA256: strings.Repeat("a", 64), ResultSize: 100, MediaType: "image/png",
		FileName: "result.png", SnapshotID: v.ID, BriefVersion: "12", SourceRevision: "rev-1",
	})
	if err != nil {
		t.Fatalf("登记输出: %v", err)
	}
	// 同内容重复提交 → 冲突;调用方按内容查询命中原输出(幂等)。
	if _, err := s.CreateOutput(ctx, ProjectOutput{
		TenantScope: "tenant-77", ProjectID: p.ID, PlatformAssetID: "asset_1",
		ResultSHA256: strings.Repeat("a", 64), ResultSize: 100, MediaType: "image/png",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("同内容重复登记应冲突: %v", err)
	}
	got, err := s.FindOutputByContent(ctx, "tenant-77", p.ID, strings.Repeat("a", 64))
	if err != nil || got.ID != o1.ID {
		t.Fatalf("内容幂等查询应命中: %v", err)
	}
	// 回执:先持久;payload 不可变;状态推进不改 payload。
	rc1, err := s.CreateReceipt(ctx, SourceReceipt{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: o1.ID, EventID: "evt-1",
		HandoffID: "h-1", TargetApp: "orders", RunID: "task-1", Sequence: 1,
		Payload: `{"immutable":true}`, PayloadFP: "pfp-1", Status: "pending",
	})
	if err != nil {
		t.Fatalf("写回执: %v", err)
	}
	if _, err := s.CreateReceipt(ctx, SourceReceipt{
		TenantScope: "tenant-77", ProjectID: p.ID, OutputID: o1.ID, EventID: "evt-1",
		HandoffID: "h-1", TargetApp: "orders", Sequence: 1, Payload: `x`, PayloadFP: "y",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("同事件回执重复写应冲突: %v", err)
	}
	rc2, err := s.FinishReceipt(ctx, "tenant-77", rc1.ID, "failed", "connection refused")
	if err != nil || rc2.Status != "failed" || rc2.Attempts != 1 || rc2.Payload != `{"immutable":true}` {
		t.Fatalf("失败推进异常: %v %+v", err, rc2)
	}
	rc3, err := s.FinishReceipt(ctx, "tenant-77", rc1.ID, "delivered", "")
	if err != nil || rc3.Status != "delivered" || rc3.Attempts != 2 || rc3.Payload != `{"immutable":true}` {
		t.Fatalf("重传成功异常: %v %+v", err, rc3)
	}
	// 查询恢复:列表可见,含失败与成功事实。
	list, err := s.ListReceipts(ctx, "tenant-77", p.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("回执列表异常: %v %d", err, len(list))
	}
	// 同输出重复提交回执请求 → FindReceiptByOutput 命中原回执(不产生新事件)。
	again, err := s.FindReceiptByOutput(ctx, "tenant-77", o1.ID)
	if err != nil || again.ID != rc1.ID {
		t.Fatalf("同输出回执查询应命中: %v", err)
	}
}
