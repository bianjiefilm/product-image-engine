// Package lightscene 定义光影场景的报价、任务与结果语义。
// 本包不调用模型、不写资金账本。没有真实报价金额时费用文案固定为「计费待确认」。
// 光影结果不能绕过主体保护：没有保真通过且没有供应商回执时，不能写成已验证产品图。
package lightscene

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
)

const (
	BillingPendingLabel      = "计费待确认"
	HonestyNotice            = "生成质量未验证，不代表生产出图已通过"
	RealGenerationIncomplete = "真实出图未完成"
	ProductionNotAuthorized  = "NOT_AUTHORIZED"
	ReadinessInternal        = "internal_test"

	ModeFidelity Mode = "fidelity"
	ModeCreative Mode = "creative"

	QuoteUnconfirmed = "unconfirmed"
	QuoteConfirmed   = "confirmed"
	QuoteInvalid     = "invalid"

	StatusQuoted     = "quoted"
	StatusSubmitting = "submitting"
	StatusQueued     = "queued"
	StatusRunning    = "running"
	StatusUnknown    = "unknown"
	StatusFailed     = "failed"

	QualityUnknown = "unknown"
	QualityPass    = "pass"
	QualityFail    = "fail"

	EvidenceNone         = "none"
	EvidencePlatformTask = "platform_task"
)

// Mode 是光影场景模式。保真为默认，只改光影不改主体。
type Mode string

var (
	ErrCreativeUnavailable     = errors.New("创意模式尚未接通真实生成路径")
	ErrQuoteUnconfirmed        = errors.New("报价未确认")
	ErrQuoteInvalid            = errors.New("报价已失效，请重新确认")
	ErrNotDeliverable          = errors.New("主体保护未通过，不能作为已验证产品图")
	ErrSubjectGate             = errors.New("主体保护待确认，不能绕过主体保护")
	ErrNoOutput                = errors.New("尚未产生可打开的输出")
	ErrQualityPassUnauthorized = errors.New("没有保真证据,只能记录失败,不能标为通过")
	ErrRelabelForbidden        = errors.New("失败记录不能改标成已验证产品图")
)

// QuoteInput 是开报价所需的已确认输入。金额不从客户端进入。
type QuoteInput struct {
	TenantID         string
	ProjectID        string
	InputID          string
	InputVersion     string
	Mode             string
	ProtectedRegion  string
	LightingIntent   string
	CreativePath     bool
	BillingConnected bool
	QuotedAmount     string
}

// Quote 是尚未执行的费用与参数版本。
type Quote struct {
	Mode                       Mode
	ProtectedRegion            string
	InputID                    string
	InputVersion               string
	LightingIntent             string
	Fingerprint                string
	BillingLabel               string
	QuoteStatus                string
	MustReconfirm              bool
	ProductionGenerationPassed bool
	BillingPassed              bool
}

// Execution 是一次逻辑任务的判定，不代表已经调用供应商。
type Execution struct {
	Status                     string
	Mode                       Mode
	Quality                    string
	Evidence                   string
	Idempotent                 bool
	Regenerate                 bool
	CallPlatformTask           bool
	Deliverable                bool
	VerifiedProduct            bool
	CreativeFallback           bool
	ProductionGenerationPassed bool
	BillingPassed              bool
	DeliveryReadiness          string
	OutputAssetID              string
	OutputVersion              string
	VendorReceiptID            string
	Selection                  string
	Pending                    []string
	AllowedUses                []string
}

// SubmitInput 决定提交是新任务、原任务返回，还是诚实失败。
// CredentialConfigured 只表示产品光影模型凭证是否存在，不携带密钥。
type SubmitInput struct {
	Quote                Quote
	GenerationUsable     bool
	CredentialConfigured bool
	ExistingStatus       string
	SameFingerprint      bool
}

// ExportRecord 只引用已有输出版本，不启动新的光影生成。
type ExportRecord struct {
	OutputAssetID    string
	OutputVersion    string
	StartsGeneration bool
	Deliverable      bool
}

// Sample 是没有真实模型调用时的结构验收记录。
type Sample struct {
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Quality    string `json:"quality"`
	ModelUsage string `json:"model_usage"`
	CostLabel  string `json:"cost_label"`
}

// OpenQuote 按默认保真模式开报价。创意路径不存在时拒绝，不降级成整图生成。
func OpenQuote(in QuoteInput) (Quote, error) {
	mode := Mode(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = ModeFidelity
	}
	if mode != ModeFidelity && mode != ModeCreative {
		return Quote{}, errors.New("模式只能是 fidelity 或 creative")
	}
	if mode == ModeCreative && !in.CreativePath {
		return Quote{}, ErrCreativeUnavailable
	}
	region := strings.TrimSpace(in.ProtectedRegion)
	if region == "" {
		region = "subject"
	}
	intent := strings.TrimSpace(in.LightingIntent)
	return Quote{
		Mode: mode, ProtectedRegion: region,
		InputID: in.InputID, InputVersion: in.InputVersion, LightingIntent: intent,
		QuoteStatus:  QuoteUnconfirmed,
		BillingLabel: billingLabel(in.BillingConnected, in.QuotedAmount),
		Fingerprint: Fingerprint(in.TenantID, in.ProjectID, in.InputID, in.InputVersion,
			string(mode), region, intent),
	}, nil
}

func billingLabel(connected bool, amount string) string {
	amount = strings.TrimSpace(amount)
	if !connected || amount == "" {
		return BillingPendingLabel
	}
	return amount
}

// ModelFingerprint 是模型引用的指纹。没有模型调用时为空，不编造。
func ModelFingerprint(modelRef string) string {
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(modelRef))
	return hex.EncodeToString(sum[:])
}

// KeepSelectedVersion 让失败或未知留在原任务上，不替换用户已经选定的版本。
func KeepSelectedVersion(ex Execution, status string) Execution {
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	ex.CreativeFallback = false
	if ex.Selection != "selected" || strings.TrimSpace(ex.OutputVersion) == "" {
		return ex
	}
	switch status {
	case StatusFailed, StatusUnknown:
		ex.Status = status
		ex.Deliverable = false
		ex.VerifiedProduct = false
	}
	return ex
}

// Fingerprint 绑定租户、工程、输入版本、模式、保护区域与光影意图。
func Fingerprint(tenant, project, inputID, inputVersion, mode, region, intent string) string {
	raw := strings.Join([]string{
		tenant, project, inputID, inputVersion, mode, region, strings.TrimSpace(intent),
	}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// InvalidateIfChanged 在模式、输入版本或光影意图变化时使未确认报价失效。
func InvalidateIfChanged(q Quote, nextMode Mode, nextInputVersion, nextIntent string) Quote {
	if q.Mode == nextMode && q.InputVersion == nextInputVersion && q.LightingIntent == strings.TrimSpace(nextIntent) {
		return q
	}
	q.QuoteStatus = QuoteInvalid
	q.MustReconfirm = true
	return q
}

// DecideSubmit 给出提交判定。没有真实供应商时停在失败；未知任务不再生成。
func DecideSubmit(in SubmitInput) (Execution, error) {
	if in.Quote.QuoteStatus == QuoteInvalid {
		return Execution{}, ErrQuoteInvalid
	}
	if in.Quote.QuoteStatus != QuoteConfirmed {
		return Execution{}, ErrQuoteUnconfirmed
	}
	base := Execution{
		Mode: in.Quote.Mode, Quality: QualityUnknown,
		DeliveryReadiness: ReadinessInternal,
		Pending:           []string{"主体保护待确认"},
		AllowedUses:       []string{"预览"},
	}
	if in.SameFingerprint && in.ExistingStatus != "" {
		base.Status = in.ExistingStatus
		base.Idempotent = true
		base.Regenerate = false
		base.CallPlatformTask = false
		if in.ExistingStatus == StatusUnknown {
			base.Pending = append(base.Pending, "供应商结果未知，先核对")
		}
		return base, nil
	}
	if !in.GenerationUsable || !in.CredentialConfigured {
		base.Status = StatusFailed
		base.Evidence = EvidenceNone
		base.CallPlatformTask = false
		base.OutputAssetID = ""
		base.OutputVersion = ""
		base.VendorReceiptID = ""
		base.Selection = ""
		base.VerifiedProduct = false
		base.Deliverable = false
		base.BillingPassed = false
		base.ProductionGenerationPassed = false
		base.Pending = append(base.Pending, "没有真实供应商，未执行生成")
		base.Pending = append(base.Pending, RealGenerationIncomplete)
		return base, nil
	}
	base.Status = StatusQueued
	base.Evidence = EvidencePlatformTask
	base.CallPlatformTask = true
	return base, nil
}

// ApplyClientVerdict 只接受失败。客户端不能自报通过，也不能把失败改回未知。
func ApplyClientVerdict(ex Execution, verdict string) (Execution, error) {
	if ex.Quality == QualityFail && verdict != QualityFail {
		return ex, ErrRelabelForbidden
	}
	if verdict != QualityFail {
		return ex, ErrQualityPassUnauthorized
	}
	ex.Quality = QualityFail
	ex.VerifiedProduct = false
	ex.Deliverable = false
	ex.CreativeFallback = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	return ex, nil
}

// SubjectGate 用主体保护报告决定能否写成已验证产品图。
// 填好的输出编号不是供应商回执。失败结论不会被改成通过。
func SubjectGate(ex Execution, reports []fidelity.Report) Execution {
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	ex.VerifiedProduct = false
	ex.Deliverable = false
	ex.CreativeFallback = false
	// 失败记录保持失败。不能借主体保护报告或回执改成已验证产品图。
	if ex.Quality == QualityFail || ex.Status == StatusFailed {
		if ex.Status == StatusFailed && ex.Quality != QualityFail {
			ex.Quality = QualityUnknown
		}
		if ex.Quality == QualityFail {
			ex.Pending = appendPending(ex.Pending, "主体保真未通过")
		}
		return ex
	}
	for _, report := range reports {
		if report.Verdict == fidelity.VerdictFail {
			ex.Quality = QualityFail
			ex.Pending = appendPending(ex.Pending, "主体保真未通过")
			return ex
		}
	}
	if ProductAxesFailed(reports) {
		ex.Quality = QualityFail
		ex.VerifiedProduct = false
		ex.Deliverable = false
		ex.Pending = appendPending(ex.Pending, "美观分不能掩盖 Logo、包装、规格或结构")
		return ex
	}
	exact := false
	for _, report := range reports {
		if report.ExactProduct && report.Verdict == fidelity.VerdictPass &&
			report.RealGeneration == fidelity.RealGenerationAuthorized {
			exact = true
			break
		}
	}
	if !exact {
		ex.Quality = QualityUnknown
		ex.Pending = appendPending(ex.Pending, "主体保护待确认")
		return ex
	}
	if strings.TrimSpace(ex.VendorReceiptID) == "" {
		ex.Quality = QualityUnknown
		ex.Pending = appendPending(ex.Pending, "没有真实供应商回执")
		return ex
	}
	ex.Quality = QualityPass
	ex.VerifiedProduct = true
	ex.Deliverable = true
	ex.AllowedUses = []string{"受限导出"}
	return ex
}

// Select 只选定已过主体保护且有供应商回执的结果。失败不能当可交付版本。
func Select(ex Execution) (Execution, error) {
	if ex.Quality == QualityFail || ex.Status == StatusFailed {
		return ex, ErrNotDeliverable
	}
	if !ex.VerifiedProduct {
		return ex, ErrSubjectGate
	}
	ex.Selection = "selected"
	ex.CreativeFallback = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	return ex, nil
}

// Export 复用选定结果的资产版本，不启动新的光影生成。
func Export(ex Execution) (ExportRecord, error) {
	if ex.Selection != "selected" {
		return ExportRecord{}, errors.New("尚未选定结果")
	}
	if ex.Quality == QualityFail || !ex.VerifiedProduct {
		return ExportRecord{}, ErrNotDeliverable
	}
	if ex.OutputAssetID == "" || ex.OutputVersion == "" {
		return ExportRecord{}, ErrNoOutput
	}
	return ExportRecord{
		OutputAssetID: ex.OutputAssetID, OutputVersion: ex.OutputVersion,
		StartsGeneration: false, Deliverable: ex.Deliverable,
	}, nil
}

// AcceptanceSamples 返回两条结构验收样本。没有真实调用时质量保持未知。
func AcceptanceSamples() []Sample {
	return []Sample{
		{Kind: "logo_text", Label: "文字/Logo 商品", Quality: QualityUnknown, ModelUsage: "未调用", CostLabel: BillingPendingLabel},
		{Kind: "complex_material", Label: "复杂材质商品", Quality: QualityUnknown, ModelUsage: "未调用", CostLabel: BillingPendingLabel},
	}
}

// AppendPending 追加一条待确认说明。同一句已存在时保持原样。
func AppendPending(items []string, item string) []string {
	return appendPending(items, item)
}

// ProductAxesFailed 判断 Logo、包装文字、规格或结构是否失败。
// 美观分或其他通过结论不能单独推翻这些轴。SubjectGate 与创意静默入口共用此判断。
func ProductAxesFailed(reports []fidelity.Report) bool {
	for _, report := range reports {
		for _, check := range report.Checks {
			if !isProductAxis(check.Name) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(check.Result), fidelity.ResultFail) {
				return true
			}
		}
	}
	return false
}

func isProductAxis(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "logo", "logo_text", "packaging", "packaging_text", "spec", "structure", "shape":
		return true
	}
	switch strings.TrimSpace(name) {
	case "包装", "包装文字", "规格", "结构":
		return true
	}
	return false
}

func appendPending(items []string, item string) []string {
	for _, have := range items {
		if have == item {
			return items
		}
	}
	return append(items, item)
}
