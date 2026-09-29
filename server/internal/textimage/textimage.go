// Package textimage 定义只靠文字描述开始做产品图的判定。
// 本包不调用模型、不写资金账本、不生成图片字节。
// 没有真实供应商时结果是失败或待确认，填上的输出编号不是核销凭证。
package textimage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	BillingPendingLabel = "计费待确认"
	HonestyNotice       = "没有真实文生图结果，不能当作已经完成的产品图"
	ReadinessInternal   = "internal_test"

	QuoteUnconfirmed = "unconfirmed"
	QuoteConfirmed   = "confirmed"

	StatusQuoted     = "quoted"
	StatusSubmitting = "submitting"
	StatusQueued     = "queued"
	StatusUnknown    = "unknown"
	StatusFailed     = "failed"
	StatusCompleted  = "completed"

	QualityUnknown = "unknown"
	QualityFail    = "fail"

	OriginFixture            = "fixture"
	OriginServer             = "server"
	RealGenerationIncomplete = "真实出图未完成"
	ConceptualNotice         = "文字描述是概念输出，没有实物参考时不宣称主体保真"
	FixtureNotice            = "这是服务端登记的夹具图，不是模型出图"
	ProductionNotAuthorized  = "NOT_AUTHORIZED"
)

var (
	ErrPromptRequired        = errors.New("文字描述不能为空")
	ErrQuoteUnconfirmed      = errors.New("报价未确认")
	ErrSubjectClaimForbidden = errors.New("客户端不能声称主体已保护")
	ErrRelabelForbidden      = errors.New("失败记录不能改成成功")
)

// OpenInput 是一次文字描述请求。照片可空。金额不从客户端进入。
type OpenInput struct {
	TenantID         string
	ProjectID        string
	Prompt           string
	PhotoAssetID     string
	BillingConnected bool
	QuotedAmount     string
}

// Quote 是尚未执行的文字请求。计费与生产授权默认未通过。
type Quote struct {
	Prompt               string
	PhotoRequired        bool
	PhotoAssetID         string
	Fingerprint          string
	BillingLabel         string
	QuoteStatus          string
	Charged              bool
	BillingPassed        bool
	ProductionAuthorized bool
	SubjectProtected     bool
}

// SubmitInput 决定是新的失败/待确认，还是回到同一条记录。
type SubmitInput struct {
	Quote            Quote
	GenerationUsable bool
	SameFingerprint  bool
	ExistingID       string
	ExistingStatus   string
}

// Record 是一次文字生成的诚实结果。ShowImage 为真才允许界面出图。
type Record struct {
	ID                   string
	Status               string
	Quality              string
	Idempotent           bool
	Regenerate           bool
	CallSupplier         bool
	ShowImage            bool
	OutputAssetID        string
	OutputIsReceipt      bool
	Charged              bool
	BillingPassed        bool
	ProductionAuthorized bool
	SubjectProtected     bool
	BillingLabel         string
	DeliveryReadiness    string
	Pending              []string
}

// Open 接受纯文字描述。未接通计费时文案固定为「计费待确认」，不扣费。
func Open(in OpenInput) (Quote, error) {
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return Quote{}, ErrPromptRequired
	}
	return Quote{
		Prompt:        prompt,
		PhotoRequired: false,
		PhotoAssetID:  strings.TrimSpace(in.PhotoAssetID),
		Fingerprint:   Fingerprint(in.TenantID, in.ProjectID, prompt),
		BillingLabel:  billingLabel(in.BillingConnected, in.QuotedAmount),
		QuoteStatus:   QuoteUnconfirmed,
	}, nil
}

func billingLabel(connected bool, amount string) string {
	amount = strings.TrimSpace(amount)
	if !connected || amount == "" {
		return BillingPendingLabel
	}
	return amount
}

// Fingerprint 绑定租户、工程和文字描述。同一描述刷新仍是同一请求。
func Fingerprint(tenant, project, prompt string) string {
	raw := strings.Join([]string{tenant, project, strings.TrimSpace(prompt)}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// DecideSubmit 给出提交判定。没有供应商时失败；已有失败或待确认则原样返回。
func DecideSubmit(in SubmitInput) (Record, error) {
	if in.Quote.QuoteStatus != QuoteConfirmed {
		return Record{}, ErrQuoteUnconfirmed
	}
	base := Record{
		BillingLabel: in.Quote.BillingLabel, Quality: QualityUnknown,
		DeliveryReadiness: ReadinessInternal,
		Pending:           []string{"没有真实生成图"},
	}
	if base.BillingLabel == "" {
		base.BillingLabel = BillingPendingLabel
	}
	if in.SameFingerprint && in.ExistingID != "" && (in.ExistingStatus == StatusFailed || in.ExistingStatus == StatusUnknown) {
		base.ID = in.ExistingID
		base.Status = in.ExistingStatus
		base.Idempotent = true
		base.Pending = append(base.Pending, "同一请求仍是原记录")
		return base, nil
	}
	if !in.GenerationUsable {
		base.Status = StatusFailed
		base.Pending = append(base.Pending, "没有真实供应商，未执行生成")
		return base, nil
	}
	base.Status = StatusUnknown
	base.CallSupplier = true
	base.Pending = append(base.Pending, "供应商结果待确认")
	return base, nil
}

// ApplyClientClaim 拒绝客户端自报主体保护、通过或成功。
func ApplyClientClaim(rec Record, claim string) (Record, error) {
	switch strings.TrimSpace(claim) {
	case "subject_protected", "protected":
		return rec, ErrSubjectClaimForbidden
	case "pass", "success", "completed":
		return rec, ErrRelabelForbidden
	default:
		return rec, ErrRelabelForbidden
	}
}

// AdmitInput 是展示前的服务端事实。客户端声明不在这里面。
type AdmitInput struct {
	Status            string
	JobTenant         string
	JobProject        string
	ActorTenant       string
	ActorProject      string
	InputVersion      string
	AssetInputVersion string
	AssetID           string
	Registered        bool
	RegisteredSHA     string
	FileSHA           string
	Origin            string
	ClientSupplied    bool
	SelectedVersion   string
	ResultVersion     string
}

// AdmitDecision 决定能不能打开这张图。Reason 为空才表示通过。
type AdmitDecision struct {
	Show   bool
	Reason string
}

// Admit 只在完成、租户工程、输入版本和登记哈希同时成立，且来源是夹具或服务端任务时展示。
// 选定版本已存在时，晚到的另一版本不能顶上。
func Admit(in AdmitInput) AdmitDecision {
	if in.ClientSupplied {
		return AdmitDecision{Reason: "client_claim"}
	}
	if in.ActorTenant == "" || in.ActorProject == "" || in.ActorTenant != in.JobTenant || in.ActorProject != in.JobProject {
		return AdmitDecision{Reason: "tenant_or_project"}
	}
	if in.Status != StatusCompleted {
		return AdmitDecision{Reason: "not_completed"}
	}
	if strings.TrimSpace(in.InputVersion) == "" || in.InputVersion != in.AssetInputVersion {
		return AdmitDecision{Reason: "input_version"}
	}
	registeredSHA := strings.ToLower(strings.TrimSpace(in.RegisteredSHA))
	fileSHA := strings.ToLower(strings.TrimSpace(in.FileSHA))
	if !in.Registered || strings.TrimSpace(in.AssetID) == "" || len(registeredSHA) != 64 || registeredSHA != fileSHA {
		return AdmitDecision{Reason: "asset_hash"}
	}
	if in.Origin != OriginFixture && in.Origin != OriginServer {
		return AdmitDecision{Reason: "not_fixture"}
	}
	if in.SelectedVersion != "" && in.SelectedVersion != in.ResultVersion {
		return AdmitDecision{Reason: "selected_version"}
	}
	return AdmitDecision{Show: true}
}

// RealGenerationCompleted 恒为假。夹具和本地测试图都不是模型出图。
// 凭证是否存在不改变这个结论。
func RealGenerationCompleted(bool, string) bool { return false }

// ServerTaskInput 是服务端轮询到的任务事实。客户端请求体不在这里面。
// CredentialConfigured 只说明凭证字符串是否存在，不能单独变成成功。
type ServerTaskInput struct {
	Status               string
	AssetID              string
	ClientSupplied       bool
	CredentialConfigured bool
	ServerBytes          bool
}

// ServerTaskDecision 决定这条服务端任务能不能写成完成。
type ServerTaskDecision struct {
	Accept  bool
	Status  string
	AssetID string
	Reason  string
}

// AcceptServerTask 只接受服务端轮询到的完成态，并且资产编号和字节都在服务端。
// 缺凭证、只有凭证、或请求体里的编号，都不是成功。
func AcceptServerTask(in ServerTaskInput) ServerTaskDecision {
	if in.ClientSupplied {
		return ServerTaskDecision{Status: StatusUnknown, Reason: "client_claim"}
	}
	switch strings.ToLower(strings.TrimSpace(in.Status)) {
	case "failed":
		return ServerTaskDecision{Status: StatusFailed, Reason: "failed"}
	case "succeeded", "completed":
		asset := strings.TrimSpace(in.AssetID)
		if asset == "" {
			return ServerTaskDecision{Status: StatusUnknown, Reason: "no_server_asset"}
		}
		if !in.ServerBytes {
			// 凭证不能代替字节。CredentialConfigured 为真也同样拒绝。
			return ServerTaskDecision{Status: StatusUnknown, Reason: "no_server_bytes"}
		}
		return ServerTaskDecision{Accept: true, Status: StatusCompleted, AssetID: asset}
	default:
		return ServerTaskDecision{Status: StatusUnknown, Reason: "pending"}
	}
}

// ModelCallUsable 回答现在能不能调用图像模型。非空凭证不是端点。
// 本服务没有图像模型 URL，所以不能把凭证字符串当成一次出图。
func ModelCallUsable(credential string) bool {
	if strings.TrimSpace(credential) == "" {
		return false
	}
	return false
}

// NoteOutputID 记下别人填上的输出编号，但不把它当成核销凭证或成片。
func NoteOutputID(rec Record, outputID string) Record {
	rec.OutputAssetID = strings.TrimSpace(outputID)
	rec.OutputIsReceipt = false
	rec.ShowImage = false
	rec.SubjectProtected = false
	rec.BillingPassed = false
	rec.ProductionAuthorized = false
	rec.Charged = false
	if rec.Status == StatusFailed || rec.Status == StatusUnknown || rec.Status == "" {
		return rec
	}
	rec.Status = StatusUnknown
	return rec
}
