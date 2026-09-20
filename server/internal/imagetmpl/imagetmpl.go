// Package imagetmpl 实现产品图模板库(HUI-1705 / FEAT-0206)。
//
// 模板 = 具名行业预设 + 参数引用集(拍板口径):
//   - 尺寸面:引用仓内既有 sizeadapt 预设集的 preset name(只存引用,不复制数值);
//   - 其余为纯数值参数(构图主体占比 / 留白 / 背景灰阶基调);
//   - 红线:模板 schema 禁止出现任何触发 AI 生成的语义字段(无 prompt/无模型
//     调用参数);ScanForbiddenKeys 对任意层级的 JSON 键做黑名单扫描,fail-closed;
//   - 内置模板是只读带版本的代码种子(builtins.json 内嵌);自定义模板归租户。
package imagetmpl

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// 模板来源。
const (
	SourceBuiltin = "builtin" // 内置行业预设:只读、带版本
	SourceCustom  = "custom"  // 租户自定义:CRUD,可从内置派生
)

// 行业枚举(内置四行业至少各 1;空串=未分类,允许用于自定义)。
const (
	IndustryBeauty      = "beauty"         // 美妆
	IndustryElectronics = "electronics_3c" // 3C 数码
	IndustryApparel     = "apparel"        // 服装
	IndustryFood        = "food"           // 食品
)

// BuiltinIDPrefix 内置模板 id 前缀(路由层据此拒绝改/删)。
const BuiltinIDPrefix = "tplbi_"

// 参数边界(拍板:纯数值,边界即契约)。
const (
	MaxNameRunes  = 64  // 模板名上限(字符)
	MaxLabelRunes = 128 // 展示标签上限
	MaxMarginPct  = 40  // 留白百分比上限
	MinRatioPct   = 10  // 主体占比下限
	MaxRatioPct   = 100 // 主体占比上限
)

// ErrForbiddenField 模板携带生成参数字段(红线拒绝,HTTP 层映射 422)。
var ErrForbiddenField = errors.New("imagetmpl: 模板含生成参数字段,模板库只收确定性预设参数")

// Params 参数引用集:尺寸预设引用 + 纯数值参数。
// 禁止在此结构外扩字段:严格解码 + 生成参数黑名单双层把关。
type Params struct {
	// SizePreset 引用 sizeadapt 预设集的 preset name(如 taobao_main);
	// 存在性由装配层(httpapi 持 PresetSet)校验,本包只校验格式。
	SizePreset string `json:"size_preset"`
	// SubjectRatioPct 构图主体占比(%)。
	SubjectRatioPct int `json:"subject_ratio_pct"`
	// MarginPct 留白百分比(%)。
	MarginPct int `json:"margin_pct"`
	// BGLuma 背景灰阶基调(0=黑..255=白;255 即常规白底)。
	BGLuma int `json:"bg_luma"`
}

// Template 模板(builtin 只读常量 / custom 租户行)。
type Template struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"` // builtin|custom
	TenantScope string    `json:"tenant_scope,omitempty"`
	Industry    string    `json:"industry"` // beauty|electronics_3c|apparel|food|""
	Name        string    `json:"name"`
	Label       string    `json:"label,omitempty"`
	Version     int       `json:"version"`
	Params      Params    `json:"params"`
	CreatedBy   string    `json:"created_by,omitempty"` // 平台 principal(custom 行审计)
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

var (
	presetNameRE    = regexp.MustCompile(`^[a-z0-9_]{2,64}$`) // 与 sizeadapt 预设名同构
	validIndustries = map[string]bool{
		"": true, IndustryBeauty: true, IndustryElectronics: true,
		IndustryApparel: true, IndustryFood: true,
	}
)

// ValidateParams 校验参数引用集。presetExists 非 nil 时额外校验引用存在性
// (装配层传 s.Presets.Get 包一层;内置种子加载期传 nil,引用命中由测试钉死)。
func ValidateParams(p Params, presetExists func(string) bool) error {
	if !presetNameRE.MatchString(p.SizePreset) {
		return fmt.Errorf("size_preset 须为 [a-z0-9_]{2,64} 的预设名引用, got %q", p.SizePreset)
	}
	if presetExists != nil && !presetExists(p.SizePreset) {
		return fmt.Errorf("size_preset 引用不存在: %q", p.SizePreset)
	}
	if p.MarginPct < 0 || p.MarginPct > MaxMarginPct {
		return fmt.Errorf("margin_pct 须 0..%d, got %d", MaxMarginPct, p.MarginPct)
	}
	if p.SubjectRatioPct < MinRatioPct || p.SubjectRatioPct > MaxRatioPct {
		return fmt.Errorf("subject_ratio_pct 须 %d..%d, got %d", MinRatioPct, MaxRatioPct, p.SubjectRatioPct)
	}
	if p.BGLuma < 0 || p.BGLuma > 255 {
		return fmt.Errorf("bg_luma 须 0..255, got %d", p.BGLuma)
	}
	return nil
}

// ValidateTemplate 校验模板元数据 + 参数(存在性交由装配层)。
func ValidateTemplate(t Template) error {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return errors.New("模板名不能为空")
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return fmt.Errorf("模板名最长 %d 字符", MaxNameRunes)
	}
	if !validIndustries[t.Industry] {
		return fmt.Errorf("industry 非法: %q(允许 beauty/electronics_3c/apparel/food/空)", t.Industry)
	}
	if utf8.RuneCountInString(t.Label) > MaxLabelRunes {
		return fmt.Errorf("label 最长 %d 字符", MaxLabelRunes)
	}
	if t.Version < 1 {
		return errors.New("version 须 ≥1")
	}
	return ValidateParams(t.Params, nil)
}

// forbiddenParamKeys 生成参数黑名单(小写;红线:模板绝不携带生成语义)。
var forbiddenParamKeys = map[string]bool{
	"prompt": true, "positive_prompt": true, "negative_prompt": true, "system_prompt": true,
	"style_prompt": true, "master_prompt": true,
	"model": true, "model_id": true, "model_name": true, "ai_model": true, "checkpoint": true,
	"sampler": true, "steps": true, "num_inference_steps": true, "inference_steps": true,
	"guidance": true, "guidance_scale": true, "cfg": true, "cfg_scale": true,
	"seed": true, "strength": true, "denoise": true, "denoising_strength": true,
	"lora": true, "loras": true, "embedding": true,
	"generate": true, "generation": true, "ai_generate": true, "task_kind": true,
}

// ScanForbiddenKeys 递归扫描 JSON 全部键(不区分层级),命中黑名单 →
// ErrForbiddenField。请求体/内置种子入库前一律过此闸。
func ScanForbiddenKeys(raw []byte) error {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("imagetmpl: 请求体必须是合法 JSON: %w", err)
	}
	var found []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, sub := range x {
				if forbiddenParamKeys[strings.ToLower(k)] {
					found = append(found, path+k)
					continue
				}
				walk(k+".", sub)
			}
		case []any:
			for _, sub := range x {
				walk(path, sub)
			}
		}
	}
	walk("", doc)
	if len(found) > 0 {
		return fmt.Errorf("%w: 非法字段 %q", ErrForbiddenField, strings.Join(found, ", "))
	}
	return nil
}

// IsBuiltinID 判断 id 是否内置模板标识。
func IsBuiltinID(id string) bool { return strings.HasPrefix(id, BuiltinIDPrefix) }

// ---- 内置种子(只读、带版本;结构对齐 sizeadapt 的内嵌预设机制) ------------

type builtinFile struct {
	Comment   string     `json:"_comment"` // 种子说明(与 sizeadapt/presets.json 同构)
	Templates []Template `json:"templates"`
}

//go:embed builtins.json
var embeddedBuiltins []byte

// BuiltinSet 已校验内置模板集(不可变;Get/List 只读)。
type BuiltinSet struct {
	byID map[string]Template
	list []Template
}

// DefaultBuiltins 内嵌四行业内置种子(加载即校验,非法即错:fail-closed)。
func DefaultBuiltins() (*BuiltinSet, error) { return LoadBuiltins(embeddedBuiltins) }

// LoadBuiltins 加载并整体校验内置种子;任何非法项整体拒绝。
func LoadBuiltins(raw []byte) (*BuiltinSet, error) {
	if err := ScanForbiddenKeys(raw); err != nil {
		return nil, err
	}
	var f builtinFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("imagetmpl: 内置种子解析失败: %w", err)
	}
	if len(f.Templates) == 0 {
		return nil, errors.New("imagetmpl: 内置种子为空(四行业至少各 1)")
	}
	s := &BuiltinSet{byID: make(map[string]Template, len(f.Templates))}
	for i, tp := range f.Templates {
		tp.Source = SourceBuiltin
		if !IsBuiltinID(tp.ID) {
			return nil, fmt.Errorf("imagetmpl: 内置模板 #%d id 须带 %s 前缀, got %q", i, BuiltinIDPrefix, tp.ID)
		}
		if err := ValidateTemplate(tp); err != nil {
			return nil, fmt.Errorf("imagetmpl: 内置模板 #%d(%q)非法: %w", i, tp.ID, err)
		}
		if _, dup := s.byID[tp.ID]; dup {
			return nil, fmt.Errorf("imagetmpl: 内置模板 id 重复: %q", tp.ID)
		}
		s.byID[tp.ID] = tp
		s.list = append(s.list, tp)
	}
	return s, nil
}

// Get 按 id 取内置模板。
func (s *BuiltinSet) Get(id string) (Template, bool) {
	tp, ok := s.byID[id]
	return tp, ok
}

// List 全部内置模板(内嵌顺序)。
func (s *BuiltinSet) List() []Template { return append([]Template(nil), s.list...) }
