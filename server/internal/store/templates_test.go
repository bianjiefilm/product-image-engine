package store

// 模板库仓储验收(HUI-1705 / FEAT-0206):
//   - 自定义模板 CRUD:版本自增、租户作用域硬隔离(跨租户不可见);
//   - 套用=登记制落点:留痕行 + 工程参数引用写点,同 (工程,模板,版本) 幂等不重复写。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/imagetmpl"
)

func customTpl(name string) imagetmpl.Template {
	return imagetmpl.Template{
		TenantScope: "t1", Industry: imagetmpl.IndustryBeauty, Name: name, Label: "测试模板",
		Params: imagetmpl.Params{SizePreset: "taobao_main", SubjectRatioPct: 70, MarginPct: 10, BGLuma: 255},
	}
}

func seedPlainProject(t *testing.T, st *Store, tenant string) string {
	t.Helper()
	if _, err := st.CreateProject(ctxBg(), Project{TenantID: tenant, Name: "P", CreatedBy: "usr_a"}); err != nil {
		t.Fatal(err)
	}
	projs, err := st.ListProjects(ctxBg(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(projs) != 1 {
		t.Fatalf("种子工程失败: %d", len(projs))
	}
	return projs[0].ID
}

// ---- 自定义模板 CRUD --------------------------------------------------------

func TestImageTemplateCustomCRUD(t *testing.T) {
	st, _ := newSizeStore(t)
	tp, err := st.CreateImageTemplate(ctxBg(), customTpl("美妆主图A"))
	if err != nil {
		t.Fatalf("创建自定义模板应成功: %v", err)
	}
	if !strings.HasPrefix(tp.ID, "tpl_") || tp.Version != 1 || tp.Source != imagetmpl.SourceCustom {
		t.Fatalf("新模板应 tpl_ 前缀/version=1/source=custom: %+v", tp)
	}
	if tp.CreatedAt.IsZero() || tp.UpdatedAt.IsZero() {
		t.Fatalf("时间戳应落值: %+v", tp)
	}

	got, err := st.GetImageTemplate(ctxBg(), "t1", tp.ID)
	if err != nil || got.ID != tp.ID || got.Name != "美妆主图A" {
		t.Fatalf("按 (租户,id) 取回应命中: %v %v", got, err)
	}

	newName := "美妆主图A改"
	newParams := imagetmpl.Params{SizePreset: "jd_main", SubjectRatioPct: 60, MarginPct: 18, BGLuma: 250}
	up, err := st.UpdateImageTemplate(ctxBg(), "t1", tp.ID, ImageTemplateUpdate{Name: &newName, Params: &newParams})
	if err != nil {
		t.Fatalf("更新应成功: %v", err)
	}
	if up.Version != 2 || up.Name != newName || up.Params != newParams {
		t.Fatalf("更新应 version+1 且字段生效: %+v", up)
	}

	list, err := st.ListImageTemplates(ctxBg(), "t1")
	if err != nil || len(list) != 1 || list[0].Version != 2 {
		t.Fatalf("列表应 1 条且为 v2: %v %v", list, err)
	}

	if err := st.DeleteImageTemplate(ctxBg(), "t1", tp.ID); err != nil {
		t.Fatalf("删除应成功: %v", err)
	}
	if _, err := st.GetImageTemplate(ctxBg(), "t1", tp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后取回应 ErrNotFound: %v", err)
	}
	if list, _ := st.ListImageTemplates(ctxBg(), "t1"); len(list) != 0 {
		t.Fatalf("删除后列表应为空: %v", list)
	}
	if err := st.DeleteImageTemplate(ctxBg(), "t1", tp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应 ErrNotFound: %v", err)
	}
}

func TestImageTemplateCrossTenantInvisible(t *testing.T) {
	st, _ := newSizeStore(t)
	tp, err := st.CreateImageTemplate(ctxBg(), customTpl("甲的模板"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetImageTemplate(ctxBg(), "t2", tp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户取回应 ErrNotFound: %v", err)
	}
	if list, _ := st.ListImageTemplates(ctxBg(), "t2"); len(list) != 0 {
		t.Fatalf("跨租户列表应为 0 行: %v", list)
	}
	newName := "偷改"
	if _, err := st.UpdateImageTemplate(ctxBg(), "t2", tp.ID, ImageTemplateUpdate{Name: &newName}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户更新应 ErrNotFound: %v", err)
	}
	if err := st.DeleteImageTemplate(ctxBg(), "t2", tp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户删除应 ErrNotFound: %v", err)
	}
	if got, _ := st.GetImageTemplate(ctxBg(), "t1", tp.ID); got.Name != "甲的模板" {
		t.Fatalf("跨租户写不得串改原行: %+v", got)
	}
}

func TestImageTemplateStoreValidation(t *testing.T) {
	st, _ := newSizeStore(t)
	if _, err := st.CreateImageTemplate(ctxBg(), customTpl("  ")); !errors.Is(err, ErrValidation) {
		t.Fatalf("空名应 ErrValidation: %v", err)
	}
	bad := customTpl("越界")
	bad.Params.MarginPct = 41
	if _, err := st.CreateImageTemplate(ctxBg(), bad); !errors.Is(err, ErrValidation) {
		t.Fatalf("参数越界应 ErrValidation: %v", err)
	}
	badInd := customTpl("坏行业")
	badInd.Industry = "crypto"
	if _, err := st.CreateImageTemplate(ctxBg(), badInd); !errors.Is(err, ErrValidation) {
		t.Fatalf("非法行业应 ErrValidation: %v", err)
	}
	tp, _ := st.CreateImageTemplate(ctxBg(), customTpl("重名"))
	if _, err := st.CreateImageTemplate(ctxBg(), customTpl("重名")); !errors.Is(err, ErrConflict) {
		t.Fatalf("同租户重名应 ErrConflict: %v", err)
	}
	newParams := imagetmpl.Params{SizePreset: "jd_main", SubjectRatioPct: 60, MarginPct: 41, BGLuma: 250}
	if _, err := st.UpdateImageTemplate(ctxBg(), "t1", tp.ID, ImageTemplateUpdate{Params: &newParams}); !errors.Is(err, ErrValidation) {
		t.Fatalf("更新越界参数应 ErrValidation: %v", err)
	}
}

// ---- 套用 = 登记制落点(留痕 + 工程参数引用写点)+ 幂等 ----------------------

func TestApplyImageTemplateIdempotent(t *testing.T) {
	st, _ := newSizeStore(t)
	pid := seedPlainProject(t, st, "t1")
	paramsJSON := `{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}`

	a1, dup, err := st.ApplyImageTemplate(ctxBg(), ImageTemplateApply{
		TenantScope: "t1", ProjectID: pid, TemplateID: "tplbi_beauty_main_white",
		TemplateVersion: 1, Params: json.RawMessage(paramsJSON), AppliedBy: "usr_a",
	})
	if err != nil || dup {
		t.Fatalf("首次套用应 201 语义(dup=false): %v %v", dup, err)
	}
	if !strings.HasPrefix(a1.ID, "apply_") || a1.TemplateID != "tplbi_beauty_main_white" ||
		a1.TemplateVersion != 1 || a1.AppliedBy != "usr_a" || a1.CreatedAt.IsZero() {
		t.Fatalf("留痕行字段不完整: %+v", a1)
	}
	// 工程配置写点:参数引用落到工程行。
	proj, err := st.GetProject(ctxBg(), "t1", pid)
	if err != nil || proj.AppliedTplID != "tplbi_beauty_main_white" || proj.AppliedTplVersion != 1 {
		t.Fatalf("工程应携带模板参数引用: %+v %v", proj, err)
	}
	var pp imagetmpl.Params
	if err := json.Unmarshal(proj.AppliedTplParams, &pp); err != nil || pp.MarginPct != 10 {
		t.Fatalf("工程引用参数应可解析且保真: %s %v", string(proj.AppliedTplParams), err)
	}

	// 幂等:同 (工程,模板,版本) 重复套用 → 不重复写。
	a2, dup, err := st.ApplyImageTemplate(ctxBg(), ImageTemplateApply{
		TenantScope: "t1", ProjectID: pid, TemplateID: "tplbi_beauty_main_white",
		TemplateVersion: 1, Params: json.RawMessage(paramsJSON), AppliedBy: "usr_b",
	})
	if err != nil || !dup || a2.ID != a1.ID {
		t.Fatalf("重复套用应幂等返回既有留痕: %v %v %v", dup, a2, err)
	}
	if list, _ := st.ListTemplateApplies(ctxBg(), "t1", pid); len(list) != 1 {
		t.Fatalf("幂等命中不得新增留痕行: %d", len(list))
	}

	// 新版本 → 新留痕行,工程写点随之更新。
	v2JSON := `{"size_preset":"taobao_main","subject_ratio_pct":80,"margin_pct":12,"bg_luma":255}`
	if _, dup, err := st.ApplyImageTemplate(ctxBg(), ImageTemplateApply{
		TenantScope: "t1", ProjectID: pid, TemplateID: "tplbi_beauty_main_white",
		TemplateVersion: 2, Params: json.RawMessage(v2JSON), AppliedBy: "usr_a",
	}); err != nil || dup {
		t.Fatalf("新版本套用应新增: %v %v", dup, err)
	}
	if list, _ := st.ListTemplateApplies(ctxBg(), "t1", pid); len(list) != 2 {
		t.Fatalf("应 2 条留痕: %d", len(list))
	}
	proj, _ = st.GetProject(ctxBg(), "t1", pid)
	if proj.AppliedTplVersion != 2 {
		t.Fatalf("工程写点应更新到 v2: %+v", proj)
	}

	// 回头重放 v1:幂等命中,不回写工程(不重复写)。
	if _, dup, err := st.ApplyImageTemplate(ctxBg(), ImageTemplateApply{
		TenantScope: "t1", ProjectID: pid, TemplateID: "tplbi_beauty_main_white",
		TemplateVersion: 1, Params: json.RawMessage(paramsJSON), AppliedBy: "usr_a",
	}); err != nil || !dup {
		t.Fatalf("重放 v1 应幂等命中: %v %v", dup, err)
	}
	if proj, _ = st.GetProject(ctxBg(), "t1", pid); proj.AppliedTplVersion != 2 {
		t.Fatalf("幂等命中不得改写工程写点: %+v", proj)
	}
}

func TestApplyImageTemplateGates(t *testing.T) {
	st, _ := newSizeStore(t)
	pid := seedPlainProject(t, st, "t1")
	okJSON := json.RawMessage(`{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}`)
	mk := func(tenant, pid string) ImageTemplateApply {
		return ImageTemplateApply{TenantScope: tenant, ProjectID: pid,
			TemplateID: "tplbi_x", TemplateVersion: 1, Params: okJSON, AppliedBy: "usr_a"}
	}
	if _, _, err := st.ApplyImageTemplate(ctxBg(), mk("t1", "proj_nope")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在工程应 ErrNotFound: %v", err)
	}
	if _, _, err := st.ApplyImageTemplate(ctxBg(), mk("t2", pid)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户工程应 ErrNotFound(不可见): %v", err)
	}
	bad := mk("t1", pid)
	bad.Params = json.RawMessage(`not-json`)
	if _, _, err := st.ApplyImageTemplate(ctxBg(), bad); !errors.Is(err, ErrValidation) {
		t.Fatalf("参数非 JSON 应 ErrValidation: %v", err)
	}
	badV := mk("t1", pid)
	badV.TemplateVersion = 0
	if _, _, err := st.ApplyImageTemplate(ctxBg(), badV); !errors.Is(err, ErrValidation) {
		t.Fatalf("版本 0 应 ErrValidation: %v", err)
	}
	if list, _ := st.ListTemplateApplies(ctxBg(), "t2", pid); len(list) != 0 {
		t.Fatalf("跨租户留痕列表应 0 行: %v", list)
	}
}
