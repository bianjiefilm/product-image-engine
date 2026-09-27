// Package fidelity 计算主体保真结论。
// 本包不调用生成模型、不写库、不扣费。客户端不能自报通过：
// 结论只由 Evaluate 根据保护范围、检查项和授权事实算出。
// 未获真实供应商授权时，真实生成保真保持 unknown，不得写成已通过。
package fidelity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaVersion   = "subject-fidelity/v1"
	MethodVersionV1 = "subject-protect/v1"

	ModeFidelity = "fidelity"
	ModeCreative = "creative"

	VerdictPass    = "pass"
	VerdictFail    = "fail"
	VerdictUnknown = "unknown"

	ResultPass    = "pass"
	ResultFail    = "fail"
	ResultUnknown = "unknown"

	RealGenerationUnknown    = "unknown"
	RealGenerationAuthorized = "authorized"

	SampleLogoText              = "logo_text"
	SampleTransparentReflective = "transparent_reflective"
	SampleFineEdge              = "fine_edge"
	SampleOcclusion             = "occlusion"
	SampleSmallProduct          = "small_product"
	SampleSimilarSKU            = "similar_sku"

	CheckProductCount  = "product_count"
	CheckLogoText      = "logo_text"
	CheckShape         = "shape"
	CheckKeyTexture    = "key_texture"
	CheckDeclaredColor = "declared_color"
)

// RequiredProtected 保真模式必须声明的保护对象。
var RequiredProtected = []string{
	CheckProductCount,
	CheckLogoText,
	CheckShape,
	CheckKeyTexture,
	CheckDeclaredColor,
}

// RequiredChecks 与保护对象同名：缺一项或结果不是明确通过，就不能判保真通过。
var RequiredChecks = RequiredProtected

var sampleClasses = map[string]bool{
	SampleLogoText:              true,
	SampleTransparentReflective: true,
	SampleFineEdge:              true,
	SampleOcclusion:             true,
	SampleSmallProduct:          true,
	SampleSimilarSKU:            true,
}

var checkNames = map[string]bool{
	CheckProductCount:  true,
	CheckLogoText:      true,
	CheckShape:         true,
	CheckKeyTexture:    true,
	CheckDeclaredColor: true,
}

var failReasons = map[string]string{
	CheckProductCount:  "商品数量不一致",
	CheckLogoText:      "包装文字或 Logo 漂移",
	CheckShape:         "形状或结构变化",
	CheckKeyTexture:    "关键纹理变化",
	CheckDeclaredColor: "声明颜色变化",
}

// Check 一项机器或人工可复核的检查。结果只有 pass/fail/unknown。
type Check struct {
	Name   string `json:"name"`
	Result string `json:"result"`
}

// Input 一次主体保护评估的输入。SupplierAuthorized 只由服务端注入。
type Input struct {
	Mode                 string   `json:"mode"`
	MethodVersion        string   `json:"method_version"`
	InputRef             string   `json:"input_ref"`
	InputVersion         string   `json:"input_version"`
	OutputRef            string   `json:"output_ref"`
	MaskRef              string   `json:"mask_ref"`
	AllowedRegions       []string `json:"allowed_regions"`
	Protected            []string `json:"protected"`
	SubjectEdit          bool     `json:"subject_edit"`
	SubjectEditConfirmed bool     `json:"subject_edit_confirmed"`
	ClaimsExactProduct   bool     `json:"claims_exact_product"`
	SimilarityOnly       bool     `json:"similarity_only"`
	SupplierAuthorized   bool     `json:"supplier_authorized"`
	MachineScope         string   `json:"machine_scope"`
	HumanScope           string   `json:"human_scope"`
	Checks               []Check  `json:"checks"`
	SampleClass          string   `json:"sample_class"`
}

// Report 不可变结论。Deliverable/ExactProduct 仅在保真通过时为真。
type Report struct {
	Schema               string   `json:"schema"`
	Mode                 string   `json:"mode"`
	MethodVersion        string   `json:"method_version"`
	InputRef             string   `json:"input_ref"`
	InputVersion         string   `json:"input_version"`
	OutputRef            string   `json:"output_ref"`
	MaskRef              string   `json:"mask_ref"`
	AllowedRegions       []string `json:"allowed_regions"`
	Protected            []string `json:"protected"`
	SampleClass          string   `json:"sample_class"`
	Checks               []Check  `json:"checks"`
	MachineScope         string   `json:"machine_scope"`
	HumanScope           string   `json:"human_scope"`
	SubjectEdit          bool     `json:"subject_edit"`
	SubjectEditConfirmed bool     `json:"subject_edit_confirmed"`
	Verdict              string   `json:"verdict"`
	ExactProduct         bool     `json:"exact_product"`
	Deliverable          bool     `json:"deliverable"`
	RealGeneration       string   `json:"real_generation"`
	Reasons              []string `json:"reasons"`
	Limits               []string `json:"limits"`
	HoldingWearVerified  bool     `json:"holding_wear_verified"`
	BodySHA256           string   `json:"body_sha256"`
}

// Evaluate 算出结论。校验失败返回 error，不产生可交付报告。
func Evaluate(in Input) (Report, error) {
	in = normalize(in)
	if err := validate(in); err != nil {
		return Report{}, err
	}
	reasons := make([]string, 0)
	limits := make([]string, 0)
	if in.Mode == ModeCreative && in.ClaimsExactProduct {
		reasons = append(reasons, "创意结果不能当作精确商品图")
	}
	if in.SubjectEdit && !in.SubjectEditConfirmed {
		reasons = append(reasons, "主体修改未经单独确认")
	}
	byName := map[string]string{}
	for _, c := range in.Checks {
		byName[c.Name] = c.Result
	}
	anyUnknown := false
	for _, name := range RequiredChecks {
		res, ok := byName[name]
		if !ok || res == ResultUnknown {
			anyUnknown = true
			continue
		}
		if res == ResultFail {
			reasons = append(reasons, failReasons[name])
		}
	}
	if in.SimilarityOnly {
		limits = append(limits, "相似度不能单独证明文字或结构保真")
	}
	verdict := VerdictUnknown
	switch {
	case len(reasons) > 0:
		verdict = VerdictFail
	case !in.SupplierAuthorized ||
		strings.TrimSpace(in.MachineScope) == "" ||
		strings.TrimSpace(in.HumanScope) == "" ||
		anyUnknown ||
		in.SimilarityOnly ||
		in.Mode == ModeCreative ||
		in.OutputRef == "":
		verdict = VerdictUnknown
	default:
		verdict = VerdictPass
	}
	if !in.SupplierAuthorized {
		limits = append(limits, "真实生成保真未授权，结论为待确认")
	}
	if strings.TrimSpace(in.MachineScope) == "" || strings.TrimSpace(in.HumanScope) == "" {
		limits = append(limits, "机器检测或人工核验范围缺失，结论为待确认")
	}
	if in.Mode == ModeCreative && verdict != VerdictFail {
		limits = append(limits, "创意模式不是保真通过")
	}
	if in.OutputRef == "" && verdict != VerdictFail {
		limits = append(limits, "尚无输出版本，不能判保真通过")
	}
	real := RealGenerationUnknown
	if in.SupplierAuthorized {
		real = RealGenerationAuthorized
	}
	exact := verdict == VerdictPass && in.Mode == ModeFidelity
	rep := Report{
		Schema:               SchemaVersion,
		Mode:                 in.Mode,
		MethodVersion:        in.MethodVersion,
		InputRef:             in.InputRef,
		InputVersion:         in.InputVersion,
		OutputRef:            in.OutputRef,
		MaskRef:              in.MaskRef,
		AllowedRegions:       append([]string(nil), in.AllowedRegions...),
		Protected:            append([]string(nil), in.Protected...),
		SampleClass:          in.SampleClass,
		Checks:               append([]Check(nil), in.Checks...),
		MachineScope:         in.MachineScope,
		HumanScope:           in.HumanScope,
		SubjectEdit:          in.SubjectEdit,
		SubjectEditConfirmed: in.SubjectEditConfirmed,
		Verdict:              verdict,
		ExactProduct:         exact,
		Deliverable:          exact,
		RealGeneration:       real,
		Reasons:              reasons,
		Limits:               limits,
		HoldingWearVerified:  false,
		BodySHA256:           BodySHA256(in),
	}
	return rep, nil
}

// BodySHA256 输入指纹。改检查或改模式会得到新指纹，不能原地改结论。
func BodySHA256(in Input) string {
	in = normalize(in)
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ExportDocument 给下游引用的质量结论。持物/穿戴恒为 false。
func (r Report) ExportDocument() map[string]any {
	return map[string]any{
		"schema":                r.Schema,
		"mode":                  r.Mode,
		"method_version":        r.MethodVersion,
		"input_ref":             r.InputRef,
		"input_version":         r.InputVersion,
		"asset_ref":             r.OutputRef,
		"mask_ref":              r.MaskRef,
		"protected":             r.Protected,
		"allowed_regions":       r.AllowedRegions,
		"sample_class":          r.SampleClass,
		"checks":                r.Checks,
		"machine_scope":         r.MachineScope,
		"human_scope":           r.HumanScope,
		"verdict":               r.Verdict,
		"exact_product":         r.ExactProduct,
		"deliverable":           r.Deliverable,
		"real_generation":       r.RealGeneration,
		"reasons":               r.Reasons,
		"limits":                r.Limits,
		"holding_wear_verified": false,
	}
}

// BlocksVerifiedReceipt 决定这份输出能否当作已验证商品图回执。
// 没有报告时不拦截既有回执；已有保真通过则失败候选不能撤销它；
// 只有失败或待确认时拒绝把结果写成已验证。
func BlocksVerifiedReceipt(reports []Report) (bool, string, string) {
	if len(reports) == 0 {
		return false, "", ""
	}
	for _, r := range reports {
		if r.ExactProduct {
			return false, "", ""
		}
	}
	for _, r := range reports {
		if r.Verdict == VerdictFail {
			return true, "fidelity_failed", "主体保真未通过，结果保持候选，不能作为已验证商品图回执"
		}
	}
	return true, "fidelity_unconfirmed", "真实生成保真待确认，不能作为已验证商品图回执"
}

func normalize(in Input) Input {
	in.Mode = strings.TrimSpace(in.Mode)
	in.MethodVersion = strings.TrimSpace(in.MethodVersion)
	in.InputRef = strings.TrimSpace(in.InputRef)
	in.InputVersion = strings.TrimSpace(in.InputVersion)
	in.OutputRef = strings.TrimSpace(in.OutputRef)
	in.MaskRef = strings.TrimSpace(in.MaskRef)
	in.SampleClass = strings.TrimSpace(in.SampleClass)
	in.MachineScope = strings.TrimSpace(in.MachineScope)
	in.HumanScope = strings.TrimSpace(in.HumanScope)
	in.AllowedRegions = compactStrings(in.AllowedRegions)
	in.Protected = compactStrings(in.Protected)
	sort.Strings(in.AllowedRegions)
	sort.Strings(in.Protected)
	checks := append([]Check(nil), in.Checks...)
	sort.Slice(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	in.Checks = checks
	return in
}

func compactStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func validate(in Input) error {
	if in.Mode != ModeFidelity && in.Mode != ModeCreative {
		return fmt.Errorf("mode 必须是 fidelity 或 creative")
	}
	if in.MethodVersion != MethodVersionV1 {
		return fmt.Errorf("method_version 必须是 %s", MethodVersionV1)
	}
	if in.InputRef == "" || in.InputVersion == "" {
		return fmt.Errorf("input_ref 与 input_version 不能为空")
	}
	if in.MaskRef == "" {
		return fmt.Errorf("mask_ref 不能为空")
	}
	if !sampleClasses[in.SampleClass] {
		return fmt.Errorf("sample_class 不在受支持样本类")
	}
	have := map[string]bool{}
	for _, p := range in.Protected {
		have[p] = true
	}
	for _, need := range RequiredProtected {
		if !have[need] {
			return fmt.Errorf("protected 缺少 %s", need)
		}
	}
	seen := map[string]bool{}
	for _, c := range in.Checks {
		if !checkNames[c.Name] {
			return fmt.Errorf("未知检查项 %s", c.Name)
		}
		if c.Result != ResultPass && c.Result != ResultFail && c.Result != ResultUnknown {
			return fmt.Errorf("检查项 %s 的结果非法", c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("检查项 %s 重复", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}
