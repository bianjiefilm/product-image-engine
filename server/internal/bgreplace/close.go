package bgreplace

import (
	"encoding/json"
	"strings"
)

const (
	// ProtectionScope 是背景替换必须保住的主体范围,进入任务指纹的 protected_region。
	ProtectionScope = "logo,packaging_text,spec,structure"

	// RealGenerationIncomplete 没有真实模型凭证或完整证据时的固定说明。
	RealGenerationIncomplete = "真实出图未完成"

	StatusQuotaInsufficient = "quota_insufficient"

	AxisLogo          = "logo"
	AxisPackagingText = "packaging_text"
	AxisSpec          = "spec"
	AxisStructure     = "structure"

	similarityMaskNotice = "单一相似度不能掩盖 Logo、包装文字、规格或结构错误"
)

// ProductChecks 是商品本身的分项检查。相似度不能单独充当结论。
type ProductChecks struct {
	Logo           string   `json:"logo"`
	PackagingText  string   `json:"packaging_text"`
	Spec           string   `json:"spec"`
	Structure      string   `json:"structure"`
	SimilarityOnly bool     `json:"similarity_only"`
	Similarity     *float64 `json:"similarity,omitempty"`
}

// RealGenerationInput 判断能不能声称真实出图已经完成。凭证只作为是否配置的事实,不包含密钥。
type RealGenerationInput struct {
	Credential bool
	ModelRef   string
	TaskID     string
	AssetID    string
	FeeRef     string
	Quality    string
	Checks     ProductChecks
}

// ParseChecks 解析已保存的检查。空文档视为全部未知。
func ParseChecks(raw string) ProductChecks {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	var checks ProductChecks
	if err := json.Unmarshal([]byte(raw), &checks); err != nil {
		return ProductChecks{}
	}
	checks, _ = normalizeChecks(checks)
	return checks
}

// ClassifySubmitFailure 把平台提交错误分成额度不足、模型失败或未知。
// 普通 4xx 仍是未知,与已合入的拒绝语义一致;只有明确的额度或模型失败才分开。
func ClassifySubmitFailure(err error) string {
	if err == nil {
		return StatusUnknown
	}
	msg := err.Error()
	if quotaMessage(msg) {
		return StatusQuotaInsufficient
	}
	if modelFailureMessage(msg) {
		return StatusFailed
	}
	return StatusUnknown
}

// Restore 把未知、失败、额度不足或刷新留在原任务上。不重生成,不换模式,不标成可交付。
func Restore(ex Execution, kind string) Execution {
	ex.Regenerate = false
	ex.CallPlatformTask = false
	ex.Idempotent = true
	ex.CreativeFallback = false
	ex.Deliverable = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	switch kind {
	case StatusQuotaInsufficient:
		ex.Status = StatusQuotaInsufficient
		ex.Pending = appendUnique(ex.Pending, "额度不足，已恢复原任务，未重新生成，未换模式")
	case StatusFailed:
		ex.Status = StatusFailed
		ex.Pending = appendUnique(ex.Pending, "模型失败，已恢复原任务，未重新生成")
	case StatusUnknown:
		ex.Status = StatusUnknown
		ex.Pending = appendUnique(ex.Pending, "供应商结果未知，先核对")
	}
	if ex.Quality == QualityFail {
		ex.Quality = QualityFail
		ex.Deliverable = false
		ex.AllowedUses = []string{"不可作为可交付版本"}
	}
	return ex
}

// InspectProduct 记录 Logo、包装文字、规格和结构。任何一项失败都盖过相似度。
// 检查全部通过也不写成可交付:没有真实出图证据时质量保持未知。
func InspectProduct(ex Execution, checks ProductChecks) (Execution, ProductChecks, error) {
	norm, err := normalizeChecks(checks)
	if err != nil {
		return ex, ProductChecks{}, err
	}
	ex.Deliverable = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	ex.CreativeFallback = false
	if ex.Quality == QualityFail || norm.HasFail() {
		ex.Quality = QualityFail
		ex.AllowedUses = []string{"不可作为可交付版本"}
		for _, reason := range norm.FailReasons() {
			ex.Pending = appendUnique(ex.Pending, reason)
		}
		if norm.SimilarityOnly || norm.Similarity != nil {
			ex.Pending = appendUnique(ex.Pending, similarityMaskNotice)
		}
		return ex, norm, nil
	}
	ex.Quality = QualityUnknown
	if norm.SimilarityOnly || norm.Similarity != nil {
		ex.Pending = appendUnique(ex.Pending, similarityMaskNotice)
	}
	if !norm.AllPass() {
		ex.Pending = appendUnique(ex.Pending, "Logo、包装文字、规格或结构尚未全部确认")
	} else {
		ex.Pending = appendUnique(ex.Pending, RealGenerationIncomplete)
	}
	return ex, norm, nil
}

// ApplyProductGuard 在选定或导出前挡住失败检查和主体保真失败。
// 不把未知抬成通过,也不因相似度改写商品错误。
func ApplyProductGuard(ex Execution, checks ProductChecks, fidelityFailed bool) Execution {
	checks, _ = normalizeChecks(checks)
	ex.Deliverable = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	ex.CreativeFallback = false
	if ex.Quality == QualityFail || checks.HasFail() || fidelityFailed {
		ex.Quality = QualityFail
		ex.Deliverable = false
		ex.AllowedUses = []string{"不可作为可交付版本"}
		for _, reason := range checks.FailReasons() {
			ex.Pending = appendUnique(ex.Pending, reason)
		}
		if fidelityFailed {
			ex.Pending = appendUnique(ex.Pending, "主体保真未通过，不能作为可交付版本")
		}
		if checks.SimilarityOnly || checks.Similarity != nil {
			ex.Pending = appendUnique(ex.Pending, similarityMaskNotice)
		}
	}
	return ex
}

// RealGenerationState 只有凭证、模型、任务、资产和全部分项通过同时成立才算真实出图完成。
// 调用方仍不得把 production_generation_passed 写成 true。
func RealGenerationState(in RealGenerationInput) (bool, string) {
	checks, _ := normalizeChecks(in.Checks)
	if !in.Credential || strings.TrimSpace(in.ModelRef) == "" || strings.TrimSpace(in.TaskID) == "" || strings.TrimSpace(in.AssetID) == "" {
		return false, RealGenerationIncomplete
	}
	if in.Quality != QualityPass || !checks.AllPass() || checks.SimilarityOnly || checks.HasFail() {
		return false, RealGenerationIncomplete
	}
	return true, ""
}

func (c ProductChecks) HasFail() bool {
	for _, axis := range []string{c.Logo, c.PackagingText, c.Spec, c.Structure} {
		if axis == QualityFail {
			return true
		}
	}
	return false
}

func (c ProductChecks) AllPass() bool {
	for _, axis := range []string{c.Logo, c.PackagingText, c.Spec, c.Structure} {
		if axis != QualityPass {
			return false
		}
	}
	return true
}

func (c ProductChecks) FailReasons() []string {
	pairs := []struct {
		axis, reason string
	}{
		{c.Logo, "Logo 被改写"},
		{c.PackagingText, "包装文字被改写"},
		{c.Spec, "规格被改写"},
		{c.Structure, "结构被改写"},
	}
	out := make([]string, 0)
	for _, pair := range pairs {
		if pair.axis == QualityFail {
			out = append(out, pair.reason)
		}
	}
	return out
}

func normalizeChecks(checks ProductChecks) (ProductChecks, error) {
	var err error
	if checks.Logo, err = normAxis("Logo", checks.Logo); err != nil {
		return ProductChecks{}, err
	}
	if checks.PackagingText, err = normAxis("包装文字", checks.PackagingText); err != nil {
		return ProductChecks{}, err
	}
	if checks.Spec, err = normAxis("规格", checks.Spec); err != nil {
		return ProductChecks{}, err
	}
	if checks.Structure, err = normAxis("结构", checks.Structure); err != nil {
		return ProductChecks{}, err
	}
	return checks, nil
}

func normAxis(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return QualityUnknown, nil
	}
	switch value {
	case QualityPass, QualityFail, QualityUnknown:
		return value, nil
	default:
		return "", errorsNew(name + " 只能是 pass、fail 或 unknown")
	}
}

func errorsNew(msg string) error {
	return &axisError{msg: msg}
}

type axisError struct{ msg string }

func (e *axisError) Error() string { return e.msg }

func quotaMessage(msg string) bool {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "status 402") || strings.Contains(lower, "status 429") {
		return true
	}
	if strings.Contains(msg, "额度不足") || strings.Contains(msg, "余额不足") {
		return true
	}
	for _, key := range []string{"insufficient_quota", "quota_exceeded", "quota_insufficient", "payment_required", "insufficient_balance"} {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

func modelFailureMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(msg, "模型失败") || strings.Contains(lower, "model_failed") || strings.Contains(lower, "model_error")
}

// AppendPending 追加一条待确认说明。同一句已存在时保持原样。
func AppendPending(items []string, item string) []string {
	return appendUnique(items, item)
}

func appendUnique(items []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return items
	}
	for _, have := range items {
		if have == item {
			return items
		}
	}
	return append(items, item)
}
