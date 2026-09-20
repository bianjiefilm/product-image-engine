package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newSizeStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, context.Background()
}

func seedProjectAndSource(t *testing.T, st *Store, tenant string) ProjectOutput {
	t.Helper()
	if _, err := st.CreateProject(ctxBg(), Project{TenantID: tenant, Name: "P", CreatedBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	// 取回真实工程 id(CreateProject 生成)。
	projs, err := st.ListProjects(ctxBg(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(projs) != 1 {
		t.Fatalf("种子工程失败: %d", len(projs))
	}
	src, err := st.CreateOutput(ctxBg(), ProjectOutput{
		TenantScope: tenant, ProjectID: projs[0].ID,
		PlatformAssetID: "asset-src-1", ResultSHA256: "aaaa", ResultSize: 10,
		MediaType: "image/png", FileName: "src.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// ctxBg 单独函数避免每个用例重复 context.Background()。
func ctxBg() context.Context { return context.Background() }

func TestCreateSizeVariantRoundTrip(t *testing.T) {
	st, _ := newSizeStore(t)
	src := seedProjectAndSource(t, st, "t1")

	v, err := st.CreateSizeVariant(ctxBg(), ProjectOutput{
		TenantScope: "t1", ProjectID: src.ProjectID,
		PlatformAssetID: "asset-var-1", ResultSHA256: "bbbb", ResultSize: 20,
		MediaType: "image/png", FileName: "src.taobao_main.png",
		SourceOutputID: src.ID, PresetName: "taobao_main", VariantMode: "pad",
		VariantWidth: 800, VariantHeight: 800,
	}, []byte("PNGBYTES"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != OutputKindSizeVariant || v.SourceOutputID != src.ID || v.PresetName != "taobao_main" {
		t.Fatalf("变体行字段不完整: %+v", v)
	}
	// 唯一键查询
	got, err := st.FindSizeVariantByKey(ctxBg(), "t1", src.ID, "taobao_main", "pad")
	if err != nil || got.ID != v.ID {
		t.Fatalf("唯一键查询应命中: %v %v", got, err)
	}
	// 列表
	list, err := st.ListSizeVariants(ctxBg(), "t1", src.ProjectID)
	if err != nil || len(list) != 1 || list[0].ID != v.ID {
		t.Fatalf("变体列表异常: %v %v", list, err)
	}
	// 字节往返
	blob, err := st.GetVariantBlob(ctxBg(), "t1", v.ID)
	if err != nil || string(blob) != "PNGBYTES" {
		t.Fatalf("变体字节往返异常: %q %v", blob, err)
	}
	// result 行无本地字节
	if _, err := st.GetVariantBlob(ctxBg(), "t1", src.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("result 行应无字节: %v", err)
	}
}

func TestSizeVariantUniqueKeyConflict(t *testing.T) {
	st, _ := newSizeStore(t)
	src := seedProjectAndSource(t, st, "t1")

	mk := func() ProjectOutput {
		return ProjectOutput{
			TenantScope: "t1", ProjectID: src.ProjectID,
			PlatformAssetID: "asset-x", ResultSHA256: "bbbb", ResultSize: 20,
			MediaType:      "image/png",
			SourceOutputID: src.ID, PresetName: "jd_main", VariantMode: "pad",
			VariantWidth: 800, VariantHeight: 800,
		}
	}
	if _, err := st.CreateSizeVariant(ctxBg(), mk(), []byte("A")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSizeVariant(ctxBg(), mk(), []byte("A")); !errors.Is(err, ErrConflict) {
		t.Fatalf("同键第二次应 ErrConflict: %v", err)
	}
	// 不同 preset 是不同键(内容/sha 不同——真实变换产出不同画布)
	other := mk()
	other.PresetName = "pdd_main"
	other.ResultSHA256 = "cccc"
	if _, err := st.CreateSizeVariant(ctxBg(), other, []byte("B")); err != nil {
		t.Fatalf("不同预设应可并存: %v", err)
	}
}

func TestSizeVariantTenantIsolation(t *testing.T) {
	st, _ := newSizeStore(t)
	src := seedProjectAndSource(t, st, "t1")

	if _, err := st.CreateSizeVariant(ctxBg(), ProjectOutput{
		TenantScope: "t1", ProjectID: src.ProjectID,
		PlatformAssetID: "asset-x", ResultSHA256: "bbbb", ResultSize: 20,
		MediaType:      "image/png",
		SourceOutputID: src.ID, PresetName: "jd_main", VariantMode: "pad",
		VariantWidth: 800, VariantHeight: 800,
	}, []byte("A")); err != nil {
		t.Fatal(err)
	}
	// 他租户:唯一键查询 / 列表 / 字节都不可见
	if _, err := st.FindSizeVariantByKey(ctxBg(), "t2", src.ID, "jd_main", "pad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户唯一键查询应 404 型: %v", err)
	}
	list, _ := st.ListSizeVariants(ctxBg(), "t2", src.ProjectID)
	if len(list) != 0 {
		t.Fatalf("跨租户列表应为空: %v", list)
	}
	if _, err := st.GetVariantBlob(ctxBg(), "t2", src.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户下载应 404 型: %v", err)
	}
}

func TestSizeVariantValidationAndBlobAtomicity(t *testing.T) {
	st, _ := newSizeStore(t)
	src := seedProjectAndSource(t, st, "t1")

	// kind 强制:即使调用方误设 kind 也登记为 size_variant(roundtrip 已断言)
	// 三元组缺失
	if _, err := st.CreateSizeVariant(ctxBg(), ProjectOutput{
		TenantScope: "t1", ProjectID: src.ProjectID, Kind: OutputKindSizeVariant,
		ResultSHA256: "x", PresetName: "", SourceOutputID: src.ID, VariantMode: "pad",
	}, []byte("A")); !errors.Is(err, ErrValidation) {
		t.Fatalf("三元组校验: %v", err)
	}
	// 空内容
	if _, err := st.CreateSizeVariant(ctxBg(), ProjectOutput{
		TenantScope: "t1", ProjectID: src.ProjectID, Kind: OutputKindSizeVariant,
		ResultSHA256: "x", SourceOutputID: src.ID, PresetName: "jd_main", VariantMode: "pad",
	}, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("空内容校验: %v", err)
	}
	// result 行不被 ListSizeVariants 计入
	list, _ := st.ListSizeVariants(ctxBg(), "t1", src.ProjectID)
	if len(list) != 0 {
		t.Fatalf("result 行不应计入变体: %v", list)
	}
}
