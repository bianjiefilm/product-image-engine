package imagetmpl

// 模板库领域验收(HUI-1705 / FEAT-0206):
//   - 内置种子:四行业至少各 1、只读常量、版本披露、尺寸预设引用全部命中仓内
//     缺省预设集(sizeadapt);
//   - 参数校验矩阵:纯数值边界 + 引用存在性;
//   - 红线:模板含任何生成参数字段(prompt/model/…)即拒绝(fail-closed)。

import (
	"errors"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/sizeadapt"
)

func validParams() Params {
	return Params{SizePreset: "taobao_main", SubjectRatioPct: 70, MarginPct: 10, BGLuma: 255}
}

// ---- 内置种子:加载 / 行业覆盖 / 版本披露 / 引用合法 -------------------------

func TestDefaultBuiltins(t *testing.T) {
	bs, err := DefaultBuiltins()
	if err != nil {
		t.Fatalf("内置模板应可加载: %v", err)
	}
	list := bs.List()
	if len(list) == 0 {
		t.Fatal("内置模板不应为空")
	}
	industries := map[string]int{}
	ids := map[string]bool{}
	for _, tp := range list {
		if tp.Source != SourceBuiltin {
			t.Fatalf("内置模板 source 应为 builtin: %+v", tp)
		}
		if !strings.HasPrefix(tp.ID, BuiltinIDPrefix) {
			t.Fatalf("内置模板 id 应带 %s 前缀: %q", BuiltinIDPrefix, tp.ID)
		}
		if tp.Version < 1 {
			t.Fatalf("内置模板版本未披露(应 ≥1): %+v", tp)
		}
		if strings.TrimSpace(tp.Name) == "" {
			t.Fatalf("内置模板应有具名: %+v", tp)
		}
		if err := ValidateParams(tp.Params, nil); err != nil {
			t.Fatalf("内置模板 %s 参数应合法: %v", tp.ID, err)
		}
		if ids[tp.ID] {
			t.Fatalf("内置模板 id 重复: %q", tp.ID)
		}
		ids[tp.ID] = true
		industries[tp.Industry]++
	}
	for _, ind := range []string{IndustryBeauty, IndustryElectronics, IndustryApparel, IndustryFood} {
		if industries[ind] < 1 {
			t.Fatalf("行业 %s 至少 1 个内置模板, 实际分布: %v", ind, industries)
		}
	}
	// 引用合法性(拍板 1):内置尺寸预设引用必须命中仓内缺省预设集。
	presets, err := sizeadapt.DefaultPresets()
	if err != nil {
		t.Fatalf("缺省预设集应可加载: %v", err)
	}
	for _, tp := range list {
		if _, ok := presets.Get(tp.Params.SizePreset); !ok {
			t.Fatalf("内置模板 %s 引用了不存在的尺寸预设 %q", tp.ID, tp.Params.SizePreset)
		}
	}
	// Get 按 id 命中;未知识别。
	if got, ok := bs.Get(list[0].ID); !ok || got.ID != list[0].ID {
		t.Fatalf("Get 应按 id 命中: %v %v", got, ok)
	}
	if _, ok := bs.Get("tplbi_nope"); ok {
		t.Fatal("未知内置模板不应命中")
	}
}

func TestLoadBuiltinsFailClosed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"id 缺前缀", `{"templates":[{"id":"bad_x","industry":"beauty","name":"n","version":1,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}}]}`},
		{"id 重复", `{"templates":[
			{"id":"tplbi_a","industry":"beauty","name":"n1","version":1,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}},
			{"id":"tplbi_a","industry":"food","name":"n2","version":1,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}}]}`},
		{"版本未披露", `{"templates":[{"id":"tplbi_a","industry":"beauty","name":"n","version":0,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}}]}`},
		{"参数越界", `{"templates":[{"id":"tplbi_a","industry":"beauty","name":"n","version":1,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":99,"bg_luma":255}}]}`},
		{"含生成参数字段", `{"templates":[{"id":"tplbi_a","industry":"beauty","name":"n","version":1,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255,"prompt":"x"}}]}`},
		{"未知字段", `{"templates":[{"id":"tplbi_a","industry":"beauty","name":"n","version":1,"params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255},"model":"gpt"}]}`},
		{"空集", `{"templates":[]}`},
		{"坏 JSON", `{`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := LoadBuiltins([]byte(c.raw)); err == nil {
				t.Fatalf("非法内置种子应整体拒绝: %s", c.raw)
			}
		})
	}
}

// ---- 参数校验矩阵:纯数值边界 + 引用存在性 ----------------------------------

func TestValidateParamsMatrix(t *testing.T) {
	always := func(string) bool { return true }
	never := func(string) bool { return false }
	cases := []struct {
		name   string
		p      Params
		exists func(string) bool
		ok     bool
	}{
		{"全合法", validParams(), always, true},
		{"exists 为 nil 跳过存在性(装配层把关)", validParams(), nil, true},
		{"引用不存在", validParams(), never, false},
		{"引用为空", Params{SizePreset: "", SubjectRatioPct: 70, MarginPct: 10, BGLuma: 255}, always, false},
		{"引用含大写", Params{SizePreset: "TaoBao_Main", SubjectRatioPct: 70, MarginPct: 10, BGLuma: 255}, always, false},
		{"引用过短", Params{SizePreset: "a", SubjectRatioPct: 70, MarginPct: 10, BGLuma: 255}, always, false},
		{"留白下界 0", pWithMargin(0), always, true},
		{"留白上界 40", pWithMargin(40), always, true},
		{"留白 -1", pWithMargin(-1), always, false},
		{"留白 41", pWithMargin(41), always, false},
		{"占比下界 10", pWithRatio(10), always, true},
		{"占比上界 100", pWithRatio(100), always, true},
		{"占比 9", pWithRatio(9), always, false},
		{"占比 101", pWithRatio(101), always, false},
		{"灰阶下界 0", pWithLuma(0), always, true},
		{"灰阶上界 255", pWithLuma(255), always, true},
		{"灰阶 -1", pWithLuma(-1), always, false},
		{"灰阶 256", pWithLuma(256), always, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateParams(c.p, c.exists)
			if c.ok && err != nil {
				t.Fatalf("应合法: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("应拒绝: %+v", c.p)
			}
		})
	}
}

func pWithMargin(m int) Params { p := validParams(); p.MarginPct = m; return p }
func pWithRatio(r int) Params  { p := validParams(); p.SubjectRatioPct = r; return p }
func pWithLuma(l int) Params   { p := validParams(); p.BGLuma = l; return p }

// ---- 红线:含生成参数字段即拒绝 ---------------------------------------------

func TestScanForbiddenKeys(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		bad  bool
	}{
		{"顶层 prompt", `{"name":"x","prompt":"生成一只猫"}`, true},
		{"params 内嵌 prompt", `{"name":"x","params":{"prompt":"…"}}`, true},
		{"大小写不敏感 steps", `{"params":{"Steps":4}}`, true},
		{"数组内 seed", `{"a":[{"seed":1}]}`, true},
		{"model 字段", `{"model":"gpt","name":"x"}`, true},
		{"negative_prompt", `{"negative_prompt":"模糊"}`, true},
		{"干净文档", `{"name":"x","industry":"beauty","params":{"size_preset":"taobao_main","subject_ratio_pct":70,"margin_pct":10,"bg_luma":255}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ScanForbiddenKeys([]byte(c.raw))
			if c.bad {
				if err == nil {
					t.Fatalf("应拒绝: %s", c.raw)
				}
				if !errors.Is(err, ErrForbiddenField) {
					t.Fatalf("应归 ErrForbiddenField: %v", err)
				}
			} else if err != nil {
				t.Fatalf("干净文档应通过: %v", err)
			}
		})
	}
	if err := ScanForbiddenKeys([]byte(`{bad`)); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}

// ---- 模板级校验:名称 / 行业 / 版本 / 标签 ----------------------------------

func TestValidateTemplate(t *testing.T) {
	base := Template{ID: "tpl_x", Source: SourceCustom, Industry: IndustryBeauty, Name: "模板", Version: 1, Params: validParams()}
	if err := ValidateTemplate(base); err != nil {
		t.Fatalf("合法模板应通过: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Template)
	}{
		{"空名称", func(t *Template) { t.Name = "  " }},
		{"超长名称", func(t *Template) { t.Name = strings.Repeat("美", 65) }},
		{"非法行业", func(t *Template) { t.Industry = "crypto" }},
		{"版本 0", func(t *Template) { t.Version = 0 }},
		{"超长标签", func(t *Template) { t.Label = strings.Repeat("标", 129) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tp := base
			c.mut(&tp)
			if err := ValidateTemplate(tp); err == nil {
				t.Fatalf("应拒绝: %+v", tp)
			}
		})
	}
	if err := ValidateTemplate(func() Template { t := base; t.Industry = ""; return t }()); err != nil {
		t.Fatalf("空行业(未分类)应允许: %v", err)
	}
}
