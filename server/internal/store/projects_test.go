package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开 store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProjectCRUDAndDefaults(t *testing.T) {
	s := openTest(t)
	p, err := s.CreateProject(context.Background(), Project{
		TenantID: "acct_a", Name: "春季主图", CreatedBy: "usr_a1",
		UsageKind: "主图", WidthPx: 800, HeightPx: 800,
	})
	if err != nil {
		t.Fatalf("创建: %v", err)
	}
	if p.Status != "draft" || p.ID == "" {
		t.Fatalf("默认状态/ID 异常: %+v", p)
	}
	if p.SourceType != "" {
		t.Fatalf("无订单工程 source_type 应可为空, got %q", p.SourceType)
	}
	got, err := s.GetProject(context.Background(), "acct_a", p.ID)
	if err != nil || got.Name != "春季主图" {
		t.Fatalf("取回: %v %+v", err, got)
	}

	newName := "春季主图-改"
	active := "active"
	up, err := s.UpdateProject(context.Background(), "acct_a", p.ID, ProjectUpdate{Name: &newName, Status: &active})
	if err != nil || up.Name != newName || up.Status != active {
		t.Fatalf("更新: %v %+v", err, up)
	}
	list, err := s.ListProjects(context.Background(), "acct_a")
	if err != nil || len(list) != 1 {
		t.Fatalf("列表: %v %d", err, len(list))
	}
}

func TestTenantIsolation(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	pa, err := s.CreateProject(ctx, Project{TenantID: "acct_a", Name: "甲的工程", CreatedBy: "usr_a"})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := s.CreateProject(ctx, Project{TenantID: "acct_b", Name: "乙的工程", CreatedBy: "usr_b"})
	if err != nil {
		t.Fatal(err)
	}
	// 跨租户读取必须不可见(与不存在同型,防租户探测)
	if _, err := s.GetProject(ctx, "acct_b", pa.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户 Get 应 ErrNotFound, got %v", err)
	}
	if la, _ := s.ListProjects(ctx, "acct_b"); len(la) != 1 || la[0].ID != pb.ID {
		t.Fatalf("乙只能看到自己的工程: %+v", la)
	}
	// 跨租户更新不可行
	newName := "劫持"
	if _, err := s.UpdateProject(ctx, "acct_b", pa.ID, ProjectUpdate{Name: &newName}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户更新应 ErrNotFound, got %v", err)
	}
	// 跨租户挂输入/版本不可行
	if _, err := s.AddInput(ctx, ProjectInput{ProjectID: pa.ID, TenantID: "acct_b", PlatformAssetID: "asset_x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户挂输入应 ErrNotFound, got %v", err)
	}
	if _, err := s.AddVersion(ctx, ProjectVersion{ProjectID: pa.ID, TenantID: "acct_b"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户写版本应 ErrNotFound, got %v", err)
	}
}

func TestInputsLifecycle(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p, err := s.CreateProject(ctx, Project{TenantID: "acct_a", Name: "P", CreatedBy: "usr_a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddInput(ctx, ProjectInput{ProjectID: p.ID, TenantID: "acct_a"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("缺 asset_id 应校验失败, got %v", err)
	}
	in, err := s.AddInput(ctx, ProjectInput{ProjectID: p.ID, TenantID: "acct_a",
		PlatformAssetID: "asset_123", SnapshotName: "src.png", SnapshotSize: 1024, SnapshotContentType: "image/png"})
	if err != nil || in.ID == "" {
		t.Fatalf("添加输入: %v", err)
	}
	inputs, err := s.ListInputs(ctx, "acct_a", p.ID)
	if err != nil || len(inputs) != 1 || inputs[0].PlatformAssetID != "asset_123" {
		t.Fatalf("列输入: %v %+v", err, inputs)
	}
	if err := s.DeleteInput(ctx, "acct_a", p.ID, in.ID); err != nil {
		t.Fatalf("删除输入: %v", err)
	}
	if err := s.DeleteInput(ctx, "acct_a", p.ID, in.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应 ErrNotFound, got %v", err)
	}
}

func TestVersionsAutoIncrement(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p, err := s.CreateProject(ctx, Project{TenantID: "acct_a", Name: "P", CreatedBy: "usr_a"})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.AddVersion(ctx, ProjectVersion{ProjectID: p.ID, TenantID: "acct_a", PlatformTaskID: "task_1"})
	if err != nil || v1.VersionNo != 1 {
		t.Fatalf("v1: %v %+v", err, v1)
	}
	v2, err := s.AddVersion(ctx, ProjectVersion{ProjectID: p.ID, TenantID: "acct_a", PlatformTaskID: "task_2", Status: "pending"})
	if err != nil || v2.VersionNo != 2 {
		t.Fatalf("v2: %v %+v", err, v2)
	}
	vs, err := s.ListVersions(ctx, "acct_a", p.ID)
	if err != nil || len(vs) != 2 || vs[0].VersionNo != 2 {
		t.Fatalf("列版本(倒序): %v %+v", err, vs)
	}
}

func TestValidationAndMigrationsIdempotent(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.CreateProject(ctx, Project{TenantID: "t", Name: "  ", CreatedBy: "u"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("空名应校验失败, got %v", err)
	}
	if _, err := s.CreateProject(ctx, Project{TenantID: "t", Name: "x", SourceType: "hacker"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法 source_type 应校验失败, got %v", err)
	}
	// 重复 Open 同一路径:迁移幂等
	dir := t.TempDir()
	path := filepath.Join(dir, "again.db")
	for i := 0; i < 2; i++ {
		s2, err := Open(path)
		if err != nil {
			t.Fatalf("第 %d 次打开失败: %v", i+1, err)
		}
		s2.Close()
	}
}
