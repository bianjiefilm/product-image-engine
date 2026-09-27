// Package bgreplace 定义 AI 背景替换的报价、任务与结果语义。
// 本包不调用模型、不写资金账本。没有真实报价金额时费用文案固定为「计费待确认」。
// 生产出图与计费是否通过由调用方显式证据决定；本包的判定结果恒为未通过。
package bgreplace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	BillingPendingLabel = "计费待确认"
	HonestyNotice       = "生成质量未验证，不代表生产出图已通过"
	ReadinessInternal   = "internal_test"

	ModeFidelity Mode = "fidelity"
	ModeCreative Mode = "creative"

	QuoteUnconfirmed = "unconfirmed"
	QuoteConfirmed   = "confirmed"
	QuoteInvalid     = "invalid"

	StatusQuoted                = "quoted"
	StatusQueued                = "queued"
	StatusRunning               = "running"
	StatusUnknown               = "unknown"
	StatusFailed                = "failed"
	StatusCompleted             = "completed"
	StatusGenerationUnavailable = "generation_unavailable"

	QualityUnknown = "unknown"
	QualityPass    = "pass"
	QualityFail    = "fail"

	EvidenceNone         = "none"
	EvidencePlatformTask = "platform_task"
)

// Mode 是背景替换模式。保真为默认；创意必须对应真实生成路径。
type Mode string

var (
	ErrCreativeUnavailable = errors.New("创意模式尚未接通真实生成路径")
	ErrQuoteUnconfirmed    = errors.New("报价未确认")
	ErrQuoteInvalid        = errors.New("报价已失效，请重新确认")
	ErrNotDeliverable      = errors.New("主体质量未通过，不能作为可交付版本")
	ErrNoOutput            = errors.New("尚未产生可打开的输出")
)

// QuoteInput 是开报价所需的已确认输入。金额不从客户端进入。
type QuoteInput struct {
	TenantID         string
	ProjectID        string
	InputID          string
	InputVersion     string
	Mode             string
	ProtectedRegion  string
	BackgroundIntent string
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
	BackgroundIntent           string
	Fingerprint                string
	BillingLabel               string
	QuoteStatus                string
	MustReconfirm              bool
	ProductionGenerationPassed bool
	BillingPassed              bool
}

// Execution 是一次逻辑任务的判定，不代表已经调用平台。
type Execution struct {
	Status                     string
	Mode                       Mode
	Quality                    string
	Evidence                   string
	Idempotent                 bool
	Regenerate                 bool
	CallPlatformTask           bool
	Deliverable                bool
	CreativeFallback           bool
	ProductionGenerationPassed bool
	BillingPassed              bool
	DeliveryReadiness          string
	OutputAssetID              string
	OutputVersion              string
	Selection                  string
	Pending                    []string
	AllowedUses                []string
}

// SubmitInput 决定提交是新任务、原任务返回，还是诚实不可用。
type SubmitInput struct {
	Quote            Quote
	GenerationUsable bool
	ExistingStatus   string
	SameFingerprint  bool
}

// ExportRecord 只引用已有输出版本。
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
	RepairMin  int    `json:"repair_min"`
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
	q := Quote{
		Mode: mode, ProtectedRegion: region,
		InputID: in.InputID, InputVersion: in.InputVersion,
		BackgroundIntent: strings.TrimSpace(in.BackgroundIntent),
		QuoteStatus:      QuoteUnconfirmed,
		BillingLabel:     billingLabel(in.BillingConnected, in.QuotedAmount),
		Fingerprint: Fingerprint(in.TenantID, in.ProjectID, in.InputID, in.InputVersion,
			string(mode), region, in.BackgroundIntent),
	}
	return q, nil
}

func billingLabel(connected bool, amount string) string {
	amount = strings.TrimSpace(amount)
	if !connected || amount == "" {
		return BillingPendingLabel
	}
	return amount
}

// Fingerprint 绑定租户、工程、输入版本、模式与保护区域。
func Fingerprint(tenant, project, inputID, inputVersion, mode, region, intent string) string {
	raw := strings.Join([]string{tenant, project, inputID, inputVersion, mode, region, strings.TrimSpace(intent)}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// InvalidateIfChanged 在模式或输入版本变化时使未确认报价失效。
func InvalidateIfChanged(q Quote, nextMode Mode, nextInputVersion string) Quote {
	if q.Mode == nextMode && q.InputVersion == nextInputVersion {
		return q
	}
	q.QuoteStatus = QuoteInvalid
	q.MustReconfirm = true
	return q
}

// DecideSubmit 给出提交判定。同指纹的未知/已有任务不再生成。
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
		Pending:           []string{"主体质量待确认"},
		AllowedUses:       []string{"预览"},
	}
	if in.SameFingerprint && in.ExistingStatus != "" {
		base.Status = in.ExistingStatus
		base.Idempotent = true
		base.Regenerate = false
		base.CallPlatformTask = false
		if in.ExistingStatus == StatusUnknown {
			base.Evidence = EvidenceNone
			base.Pending = append(base.Pending, "供应商结果未知，先核对")
		}
		return base, nil
	}
	if !in.GenerationUsable {
		base.Status = StatusGenerationUnavailable
		base.Evidence = EvidenceNone
		base.Pending = append(base.Pending, "生成质量未验证")
		return base, nil
	}
	base.Status = StatusQueued
	base.Evidence = EvidencePlatformTask
	base.CallPlatformTask = true
	return base, nil
}

// ApplyQuality 记录主体质量。失败不改变模式，也不自动可交付。
func ApplyQuality(ex Execution, verdict string) Execution {
	switch verdict {
	case QualityFail:
		ex.Quality = QualityFail
	case QualityPass:
		ex.Quality = QualityPass
	default:
		ex.Quality = QualityUnknown
	}
	ex.CreativeFallback = false
	ex.Deliverable = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	return ex
}

// Select 把完成的候选标成用户选定。失败与未完成不能当可交付版本。
func Select(ex Execution) (Execution, error) {
	if ex.Quality == QualityFail {
		return ex, ErrNotDeliverable
	}
	if ex.Status != StatusCompleted {
		return ex, errors.New("任务尚未完成，不能选定")
	}
	ex.Selection = "selected"
	ex.CreativeFallback = false
	ex.ProductionGenerationPassed = false
	ex.BillingPassed = false
	ex.Deliverable = ex.Quality == QualityPass
	if ex.Quality != QualityPass {
		ex.Pending = append(ex.Pending, "主体质量待确认")
		ex.AllowedUses = []string{"受限预览"}
	} else {
		ex.AllowedUses = []string{"受限导出"}
	}
	return ex, nil
}

// Export 复用选定结果的资产版本，不启动新的背景生成。
func Export(ex Execution) (ExportRecord, error) {
	if ex.Selection != "selected" {
		return ExportRecord{}, errors.New("尚未选定结果")
	}
	if ex.Quality == QualityFail {
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
