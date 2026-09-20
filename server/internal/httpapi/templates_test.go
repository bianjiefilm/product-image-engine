package httpapi

// 模板库 HTTP 验收(HUI-1705 / FEAT-0206):
//   off → 全部路由 404 不可见;on → 内置披露(只读+版本)、自定义 CRUD 校验矩阵
//   (含生成参数字段即 422)、套用=登记制落点+留痕+幂等、跨租户 404、零触发生成。

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/imagetmpl"
)

// newTplFixture 开关 on + 装配内置模板集(off 基线测试用裸 fixture)。
func newTplFixture(t *testing.T, extra func(*config.Config)) *fixture {
	t.Helper()
	f := newFixture(t, func(c *config.Config) {
		c.TemplatesEnabled = true
		if extra != nil {
			extra(c)
		}
	})
	bs, err := imagetmpl.DefaultBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	f.srv.TplBuiltins = bs
	return f
}

func tplLogin(t *testing.T, f *fixture, email string) string {
	t.Helper()
	_, pair := f.login(t, email, "right-pass")
	tok, _ := pair["access_token"].(string)
	return tok
}

func validCustomBody() map[string]any {
	return map[string]any{
		"name": "我的美妆模板", "industry": "beauty", "label": "测试模板",
		"params": map[string]any{
			"size_preset": "taobao_main", "subject_ratio_pct": 70, "margin_pct": 10, "bg_luma": 255,
		},
	}
}

// ---- 开关 off:路由不注册 = 404 不可见 ----------------------------------------

func TestTemplatesDisabledRoutesInvisible404(t *testing.T) {
	f := newFixture(t, nil) // FEATURE_TEMPLATES 默认 off
	tok := tplLogin(t, f, "a@x.com")

	if st, _ := f.do(t, "GET", "/api/v1/image-templates", tok, nil); st != 404 {
		t.Fatalf("off 时列表应 404: %d", st)
	}
	if st, _ := f.do(t, "POST", "/api/v1/image-templates", tok, validCustomBody()); st != 404 {
		t.Fatalf("off 时创建应 404: %d", st)
	}
	if st, _ := f.do(t, "GET", "/api/v1/image-templates/tplbi_beauty_main_white", tok, nil); st != 404 {
		t.Fatalf("off 时详情应 404: %d", st)
	}
	if st, _ := f.do(t, "PATCH", "/api/v1/image-templates/tpl_x", tok, validCustomBody()); st != 404 {
		t.Fatalf("off 时更新应 404: %d", st)
	}
	if st, _, _ := f.doRaw(t, "DELETE", "/api/v1/image-templates/tpl_x", tok, nil); st != 404 {
		t.Fatalf("off 时删除应 404: %d", st)
	}
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "P", "source_type": "standalone"})
	proj, _ := created["project"].(map[string]any)
	pid, _ := proj["id"].(string)
	if st, _ := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": "tplbi_beauty_main_white"}); st != 404 {
		t.Fatalf("off 时套用应 404: %d", st)
	}
	if st, _ := f.do(t, "GET", "/api/v1/projects/"+pid+"/image-template-applies", tok, nil); st != 404 {
		t.Fatalf("off 时留痕列表应 404: %d", st)
	}
	// off 不影响 readyz(ok 仍 true),gates 里可见 templates.enabled=false。
	st, ready := f.do(t, "GET", "/readyz", "", nil)
	gates, _ := ready["gates"].(map[string]any)
	tg, _ := gates["templates"].(map[string]any)
	if st != 200 || ready["ok"] != true || tg["enabled"] != false {
		t.Fatalf("readyz 应报告 templates off 且服务仍 ok: %d %v", st, ready)
	}
}

// ---- 内置模板披露:只读层 + 版本 + 行业覆盖 ----------------------------------

func TestTemplatesBuiltinsDisclosed(t *testing.T) {
	f := newTplFixture(t, nil)
	tok := tplLogin(t, f, "a@x.com")

	st, resp := f.do(t, "GET", "/api/v1/image-templates", tok, nil)
	if st != 200 {
		t.Fatalf("列表应 200: %d %v", st, resp)
	}
	list, _ := resp["templates"].([]any)
	if len(list) < 8 {
		t.Fatalf("内置至少 8 个(四行业各≥2): %d", len(list))
	}
	industries := map[string]bool{}
	for _, it := range list {
		tp, _ := it.(map[string]any)
		if tp["source"] != "builtin" {
			t.Fatalf("内置 source 应为 builtin: %v", tp)
		}
		if v, _ := tp["version"].(float64); v < 1 {
			t.Fatalf("版本应披露(≥1): %v", tp)
		}
		if id, _ := tp["id"].(string); !strings.HasPrefix(id, "tplbi_") {
			t.Fatalf("内置 id 前缀不符: %q", id)
		}
		ind, _ := tp["industry"].(string)
		industries[ind] = true
	}
	for _, ind := range []string{"beauty", "electronics_3c", "apparel", "food"} {
		if !industries[ind] {
			t.Fatalf("行业 %s 未覆盖: %v", ind, industries)
		}
	}

	// 单查内置:参数引用披露;未知识别 404。
	st, one := f.do(t, "GET", "/api/v1/image-templates/tplbi_beauty_main_white", tok, nil)
	if st != 200 {
		t.Fatalf("内置单查应 200: %d %v", st, one)
	}
	params, _ := one["params"].(map[string]any)
	if params["size_preset"] != "taobao_main" {
		t.Fatalf("内置参数引用应披露: %v", one)
	}
	if st, _ := f.do(t, "GET", "/api/v1/image-templates/tplbi_nope", tok, nil); st != 404 {
		t.Fatalf("未知模板应 404: %d", st)
	}
}

// ---- 自定义模板 CRUD + 校验矩阵 ---------------------------------------------

func TestCustomTemplateCRUDValidationMatrix(t *testing.T) {
	f := newTplFixture(t, nil)
	tok := tplLogin(t, f, "jia@x.com")

	var customID string
	t.Run("空白新建 201 version=1", func(t *testing.T) {
		st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, validCustomBody())
		if st != 201 {
			t.Fatalf("创建应 201: %d %v", st, resp)
		}
		if resp["source"] != "custom" || resp["version"] != float64(1) {
			t.Fatalf("新模板应 custom/v1: %v", resp)
		}
		id, _ := resp["id"].(string)
		if !bytes.HasPrefix([]byte(id), []byte("tpl_")) || bytes.HasPrefix([]byte(id), []byte("tplbi_")) {
			t.Fatalf("自定义 id 应 tpl_ 前缀: %q", id)
		}
		customID = id
	})
	t.Run("同租户重名 409", func(t *testing.T) {
		st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, validCustomBody())
		if code, _ := errCodeOf(resp); st != 409 || code != "name_conflict" {
			t.Fatalf("重名应 409: %d %v", st, resp)
		}
	})
	t.Run("空名 400", func(t *testing.T) {
		body := validCustomBody()
		body["name"] = "  "
		if st, _ := f.do(t, "POST", "/api/v1/image-templates", tok, body); st != 400 {
			t.Fatalf("空名应 400: %d", st)
		}
	})
	t.Run("超长名 400", func(t *testing.T) {
		body := validCustomBody()
		body["name"] = strings.Repeat("美", 65)
		if st, _ := f.do(t, "POST", "/api/v1/image-templates", tok, body); st != 400 {
			t.Fatalf("超长名应 400: %d", st)
		}
	})
	t.Run("非法行业 400", func(t *testing.T) {
		body := validCustomBody()
		body["industry"] = "crypto"
		if st, _ := f.do(t, "POST", "/api/v1/image-templates", tok, body); st != 400 {
			t.Fatalf("非法行业应 400: %d", st)
		}
	})
	t.Run("未知尺寸预设引用 400", func(t *testing.T) {
		body := validCustomBody()
		body["params"] = map[string]any{"size_preset": "no_such_preset", "subject_ratio_pct": 70, "margin_pct": 10, "bg_luma": 255}
		st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, body)
		if code, _ := errCodeOf(resp); st != 400 || code != "unknown_size_preset" {
			t.Fatalf("未知预设引用应 400: %d %v", st, resp)
		}
	})
	t.Run("数值边界越界 400", func(t *testing.T) {
		for _, c := range []struct {
			key string
			val int
		}{
			{"margin_pct", 41}, {"margin_pct", -1},
			{"subject_ratio_pct", 9}, {"subject_ratio_pct", 101},
			{"bg_luma", 256}, {"bg_luma", -1},
		} {
			params := map[string]any{"size_preset": "taobao_main", "subject_ratio_pct": 70, "margin_pct": 10, "bg_luma": 255}
			params[c.key] = c.val
			body := validCustomBody()
			body["name"] = "边界" + c.key
			body["params"] = params
			if st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, body); st != 400 {
				t.Fatalf("%s=%d 应 400: %d %v", c.key, c.val, st, resp)
			}
		}
	})
	t.Run("含生成参数字段 422", func(t *testing.T) {
		for _, key := range []string{"prompt", "negative_prompt", "model", "steps", "seed", "guidance_scale"} {
			body := validCustomBody()
			body["name"] = "红线" + key
			body["params"] = map[string]any{
				"size_preset": "taobao_main", "subject_ratio_pct": 70, "margin_pct": 10, "bg_luma": 255,
				key: "anything",
			}
			st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, body)
			if code, _ := errCodeOf(resp); st != 422 || code != "forbidden_field" {
				t.Fatalf("含 %s 应 422 forbidden_field: %d %v", key, st, resp)
			}
		}
	})
	t.Run("未知字段 400", func(t *testing.T) {
		body := validCustomBody()
		body["whatever"] = 1
		if st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, body); st != 400 {
			t.Fatalf("未知字段应 400: %d %v", st, resp)
		}
	})
	t.Run("从内置派生 201", func(t *testing.T) {
		body := validCustomBody()
		body["name"] = "派生美妆"
		body["derive_from"] = "tplbi_beauty_main_white"
		st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, body)
		if st != 201 {
			t.Fatalf("派生应 201: %d %v", st, resp)
		}
		params, _ := resp["params"].(map[string]any)
		if params["size_preset"] != "taobao_main" || params["margin_pct"] == float64(0) {
			t.Fatalf("派生应继承内置参数: %v", resp)
		}
	})
	t.Run("未知派生源 400", func(t *testing.T) {
		body := validCustomBody()
		body["derive_from"] = "tplbi_nope"
		if st, resp := f.do(t, "POST", "/api/v1/image-templates", tok, body); st != 400 {
			t.Fatalf("未知派生源应 400: %d %v", st, resp)
		}
	})
	t.Run("PATCH 自定义 version+1", func(t *testing.T) {
		body := map[string]any{
			"name": "我的美妆模板改",
			"params": map[string]any{
				"size_preset": "jd_main", "subject_ratio_pct": 60, "margin_pct": 18, "bg_luma": 250,
			},
		}
		st, resp := f.do(t, "PATCH", "/api/v1/image-templates/"+customID, tok, body)
		if st != 200 || resp["version"] != float64(2) || resp["name"] != "我的美妆模板改" {
			t.Fatalf("更新应 200 且 version+1: %d %v", st, resp)
		}
	})
	t.Run("PATCH 含生成参数字段 422", func(t *testing.T) {
		body := map[string]any{"params": map[string]any{
			"size_preset": "jd_main", "subject_ratio_pct": 60, "margin_pct": 18, "bg_luma": 250, "prompt": "x",
		}}
		st, resp := f.do(t, "PATCH", "/api/v1/image-templates/"+customID, tok, body)
		if code, _ := errCodeOf(resp); st != 422 || code != "forbidden_field" {
			t.Fatalf("PATCH 含 prompt 应 422: %d %v", st, resp)
		}
	})
	t.Run("DELETE 自定义 204 后 404", func(t *testing.T) {
		st, _, raw := f.doRaw(t, "DELETE", "/api/v1/image-templates/"+customID, tok, nil)
		if st != 204 {
			t.Fatalf("删除应 204: %d %s", st, raw)
		}
		if st, _ := f.do(t, "GET", "/api/v1/image-templates/"+customID, tok, nil); st != 404 {
			t.Fatalf("删除后取回应 404: %d", st)
		}
	})
}

// ---- 内置只读:改/删一律拒绝 -------------------------------------------------

func TestBuiltinTemplateReadOnly(t *testing.T) {
	f := newTplFixture(t, nil)
	tok := tplLogin(t, f, "a@x.com")
	const bid = "tplbi_beauty_main_white"

	body := map[string]any{"name": "改内置"}
	st, resp := f.do(t, "PATCH", "/api/v1/image-templates/"+bid, tok, body)
	if code, _ := errCodeOf(resp); st != 403 || code != "builtin_readonly" {
		t.Fatalf("改内置应 403 builtin_readonly: %d %v", st, resp)
	}
	if st, resp := f.do(t, "DELETE", "/api/v1/image-templates/"+bid, tok, nil); st != 403 {
		if code, _ := errCodeOf(resp); st != 403 || code != "builtin_readonly" {
			t.Fatalf("删内置应 403 builtin_readonly: %d %v", st, resp)
		}
	}
	// 只读红线:内置原样仍在,版本未动。
	st, one := f.do(t, "GET", "/api/v1/image-templates/"+bid, tok, nil)
	if st != 200 || one["version"] != float64(1) {
		t.Fatalf("内置应原样保留 v1: %d %v", st, one)
	}
}

// ---- 跨租户:自定义模板 0 行/404 ---------------------------------------------

func TestTemplateCrossTenant404(t *testing.T) {
	f := newTplFixture(t, nil)
	tokA := tplLogin(t, f, "jia@x.com")
	st, resp := f.do(t, "POST", "/api/v1/image-templates", tokA, validCustomBody())
	if st != 201 {
		t.Fatalf("前置创建应 201: %d %v", st, resp)
	}
	customID, _ := resp["id"].(string)

	tokB := tplLogin(t, f, "yi@x.com")
	if st, _ := f.do(t, "GET", "/api/v1/image-templates/"+customID, tokB, nil); st != 404 {
		t.Fatalf("跨租户取回应 404: %d", st)
	}
	if st, _ := f.do(t, "PATCH", "/api/v1/image-templates/"+customID, tokB, map[string]any{"name": "偷改"}); st != 404 {
		t.Fatalf("跨租户更新应 404: %d", st)
	}
	if st, _, _ := f.doRaw(t, "DELETE", "/api/v1/image-templates/"+customID, tokB, nil); st != 404 {
		t.Fatalf("跨租户删除应 404: %d", st)
	}
	// 列表 0 串行:B 的列表只见内置,不见 A 的自定义。
	st, list := f.do(t, "GET", "/api/v1/image-templates", tokB, nil)
	if st != 200 {
		t.Fatalf("B 列表应 200: %d", st)
	}
	for _, it := range list["templates"].([]any) {
		tp, _ := it.(map[string]any)
		if id, _ := tp["id"].(string); id == customID {
			t.Fatalf("B 列表不得串出 A 的自定义模板: %v", tp)
		}
	}
	// A 原行未被动过。
	if st, one := f.do(t, "GET", "/api/v1/image-templates/"+customID, tokA, nil); st != 200 || one["name"] != "我的美妆模板" {
		t.Fatalf("A 原行应完好: %d %v", st, one)
	}
}

// ---- 套用 = 登记制落点:留痕 + 幂等 + 零触发生成 ------------------------------

func TestApplyTemplateFlowAndIdempotent(t *testing.T) {
	f := newTplFixture(t, nil)
	tok := tplLogin(t, f, "jia@x.com")
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "模板工程", "source_type": "standalone"})
	proj, _ := created["project"].(map[string]any)
	pid, _ := proj["id"].(string)
	const bid = "tplbi_beauty_main_white"

	// 首次套用内置:201 + 留痕 + 工程写点。
	st, r1 := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": bid})
	if st != 201 || r1["duplicate"] != false {
		t.Fatalf("首次套用应 201: %d %v", st, r1)
	}
	ap1, _ := r1["apply"].(map[string]any)
	if ap1["template_id"] != bid || ap1["template_version"] != float64(1) {
		t.Fatalf("留痕应披露模板与版本: %v", ap1)
	}
	if by, _ := ap1["applied_by"].(string); !bytes.HasPrefix([]byte(by), []byte("usr_")) {
		t.Fatalf("留痕应记录操作者: %v", ap1)
	}
	if _, ok := ap1["params"].(map[string]any); !ok {
		t.Fatalf("留痕应携带参数引用快照: %v", ap1)
	}
	applyID, _ := ap1["id"].(string)

	// 工程详情披露参数引用(登记制:只写引用)。
	st, detail := f.do(t, "GET", "/api/v1/projects/"+pid, tok, nil)
	projDetail, _ := detail["project"].(map[string]any)
	if projDetail["applied_tpl_id"] != bid || projDetail["applied_tpl_version"] != float64(1) {
		t.Fatalf("工程应携带模板引用: %v", projDetail)
	}

	// 留痕列表 1 条。
	st, li := f.do(t, "GET", "/api/v1/projects/"+pid+"/image-template-applies", tok, nil)
	applies, _ := li["applies"].([]any)
	if st != 200 || len(applies) != 1 {
		t.Fatalf("留痕应 1 条: %d %v", st, li)
	}

	// 幂等:同模板同版本重复套用 → 200 duplicate,不重复写。
	st, r2 := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": bid})
	if st != 200 || r2["duplicate"] != true {
		t.Fatalf("重复套用应 200 duplicate:true: %d %v", st, r2)
	}
	ap2, _ := r2["apply"].(map[string]any)
	if ap2["id"] != applyID {
		t.Fatalf("幂等应返回同一留痕: %v vs %v", ap1["id"], ap2["id"])
	}
	st, li = f.do(t, "GET", "/api/v1/projects/"+pid+"/image-template-applies", tok, nil)
	if applies, _ = li["applies"].([]any); len(applies) != 1 {
		t.Fatalf("幂等命中不得新增留痕: %d", len(applies))
	}

	// 自定义 v2:新版本 → 新留痕,工程写点更新。
	st, cu := f.do(t, "POST", "/api/v1/image-templates", tok, validCustomBody())
	cid, _ := cu["id"].(string)
	patch := map[string]any{"params": map[string]any{
		"size_preset": "jd_main", "subject_ratio_pct": 60, "margin_pct": 18, "bg_luma": 250,
	}}
	if st, up := f.do(t, "PATCH", "/api/v1/image-templates/"+cid, tok, patch); st != 200 || up["version"] != float64(2) {
		t.Fatalf("前置 PATCH 应 v2: %d %v", st, up)
	}
	st, r3 := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": cid})
	if st != 201 || r3["duplicate"] != false {
		t.Fatalf("套用自定义 v2 应 201: %d %v", st, r3)
	}
	st, r4 := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": cid})
	if st != 200 || r4["duplicate"] != true {
		t.Fatalf("重复套用 v2 应 200: %d %v", st, r4)
	}
	st, detail = f.do(t, "GET", "/api/v1/projects/"+pid, tok, nil)
	projDetail, _ = detail["project"].(map[string]any)
	if projDetail["applied_tpl_id"] != cid || projDetail["applied_tpl_version"] != float64(2) {
		t.Fatalf("工程写点应更新为自定义 v2: %v", projDetail)
	}
	// 回放内置 v1:幂等命中,不得改写工程写点。
	if st, r5 := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": bid}); st != 200 || r5["duplicate"] != true {
		t.Fatalf("回放内置 v1 应幂等命中: %d %v", st, r5)
	}
	st, detail = f.do(t, "GET", "/api/v1/projects/"+pid, tok, nil)
	projDetail, _ = detail["project"].(map[string]any)
	if projDetail["applied_tpl_id"] != cid {
		t.Fatalf("幂等命中不得改写工程写点: %v", projDetail)
	}
	st, li = f.do(t, "GET", "/api/v1/projects/"+pid+"/image-template-applies", tok, nil)
	if applies, _ = li["applies"].([]any); len(applies) != 2 {
		t.Fatalf("留痕应稳定在 2 条: %d", len(applies))
	}

	// 门控负例。
	if st, resp := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tok, map[string]any{"template_id": "tpl_nope"}); st != 404 {
		t.Fatalf("未知模板应 404: %d %v", st, resp)
	}
	if st, _ := f.do(t, "POST", "/api/v1/projects/proj_nope/image-template/apply", tok, map[string]any{"template_id": bid}); st != 404 {
		t.Fatalf("不存在工程应 404: %d", st)
	}
	tokB := tplLogin(t, f, "yi@x.com")
	st, resp := f.do(t, "POST", "/api/v1/projects/"+pid+"/image-template/apply", tokB, map[string]any{"template_id": bid})
	if code, _ := errCodeOf(resp); st != 403 || code != "tenant_mismatch" {
		t.Fatalf("跨租户套用应 403: %d %v", st, resp)
	}

	// 零耦合:登记制套用零触发生成——生成开关仍 off,提交照旧 503。
	st, msg := f.do(t, "POST", "/api/v1/projects/"+pid+"/versions", tok, map[string]any{})
	if st != 503 || !bytes.Contains([]byte(errMsg(msg)), []byte("FEATURE_GENERATION_ENABLED")) {
		t.Fatalf("套用不得影响生成门: %d %v", st, msg)
	}
}
