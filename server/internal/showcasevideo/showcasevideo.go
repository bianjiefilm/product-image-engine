// Package showcasevideo 定义从已有产品图发起展示视频的判定。
// 本包不调用模型、不写资金账本、不生成视频字节。
// 没有真实视频供应商时结果是失败或待确认。静图和填上的输出编号都不是视频。
package showcasevideo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	Move360   = "360"
	MoveScene = "scene"

	BillingPendingLabel = "计费待确认"
	HonestyNotice       = "没有真实展示视频，不能播放静图或假视频"
	ReadinessInternal   = "internal_test"

	QuoteUnconfirmed = "unconfirmed"
	QuoteConfirmed   = "confirmed"

	StatusQuoted     = "quoted"
	StatusSubmitting = "submitting"
	StatusUnknown    = "unknown"
	StatusFailed     = "failed"

	QualityUnknown = "unknown"
)

var (
	ErrImageRequired    = errors.New("没有产品图就不能发起展示视频")
	ErrMoveNotAllowed   = errors.New("运镜只允许 360 度或场景运镜")
	ErrQuoteUnconfirmed = errors.New("报价未确认")
	ErrRelabelForbidden = errors.New("失败记录不能改成成功")
)

// OpenInput 是一次展示视频请求。产品图必须已存在，运镜只能是明确选项。金额不从客户端进入。
type OpenInput struct {
	TenantID         string
	ProjectID        string
	ImageID          string
	CameraMove       string
	BillingConnected bool
	QuotedAmount     string
}

// Quote 是尚未执行的展示视频请求。计费与生产授权默认未通过，也不提供可播放地址。
type Quote struct {
	ImageID              string
	CameraMove           string
	Fingerprint          string
	BillingLabel         string
	QuoteStatus          string
	Charged              bool
	BillingPassed        bool
	ProductionAuthorized bool
	ShowVideo            bool
	PlayStill            bool
	VideoURL             string
}

// SubmitInput 决定是新的失败/待确认，还是回到同一条记录。
type SubmitInput struct {
	Quote               Quote
	VideoProviderUsable bool
	SameFingerprint     bool
	ExistingID          string
	ExistingStatus      string
}

// Record 是一次展示视频的诚实结果。ShowVideo 与 PlayStill 为假时界面不能出画面。
type Record struct {
	ID                   string
	Status               string
	Quality              string
	Idempotent           bool
	Regenerate           bool
	CallSupplier         bool
	ShowVideo            bool
	PlayStill            bool
	VideoURL             string
	OutputAssetID        string
	OutputIsReceipt      bool
	Charged              bool
	BillingPassed        bool
	ProductionAuthorized bool
	BillingLabel         string
	DeliveryReadiness    string
	Pending              []string
}

// Open 只接受已点名的产品图，以及 360 度或场景运镜。未接通计费时文案固定为「计费待确认」。
func Open(in OpenInput) (Quote, error) {
	imageID := strings.TrimSpace(in.ImageID)
	if imageID == "" {
		return Quote{}, ErrImageRequired
	}
	move := strings.TrimSpace(in.CameraMove)
	if move != Move360 && move != MoveScene {
		return Quote{}, ErrMoveNotAllowed
	}
	return Quote{
		ImageID:      imageID,
		CameraMove:   move,
		Fingerprint:  Fingerprint(in.TenantID, in.ProjectID, imageID, move),
		BillingLabel: billingLabel(in.BillingConnected, in.QuotedAmount),
		QuoteStatus:  QuoteUnconfirmed,
	}, nil
}

func billingLabel(connected bool, amount string) string {
	amount = strings.TrimSpace(amount)
	if !connected || amount == "" {
		return BillingPendingLabel
	}
	return amount
}

// Fingerprint 绑定租户、工程、产品图和运镜。同一组合刷新仍是同一请求。
func Fingerprint(tenant, project, imageID, move string) string {
	raw := strings.Join([]string{tenant, project, strings.TrimSpace(imageID), strings.TrimSpace(move)}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// DecideSubmit 给出提交判定。没有视频供应商时失败；已有失败或待确认则原样返回。
func DecideSubmit(in SubmitInput) (Record, error) {
	if in.Quote.QuoteStatus != QuoteConfirmed {
		return Record{}, ErrQuoteUnconfirmed
	}
	base := Record{
		BillingLabel: in.Quote.BillingLabel, Quality: QualityUnknown,
		DeliveryReadiness: ReadinessInternal,
		Pending:           []string{"没有真实展示视频"},
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
	if !in.VideoProviderUsable {
		base.Status = StatusFailed
		base.Pending = append(base.Pending, "没有真实视频供应商，未执行生成")
		return base, nil
	}
	base.Status = StatusUnknown
	base.CallSupplier = true
	base.Pending = append(base.Pending, "供应商结果待确认")
	return base, nil
}

// ApplyClientClaim 拒绝把失败或待确认改成成功。
func ApplyClientClaim(rec Record, claim string) (Record, error) {
	switch strings.TrimSpace(claim) {
	case "pass", "success", "completed":
		return rec, ErrRelabelForbidden
	default:
		return rec, ErrRelabelForbidden
	}
}

// NoteOutputID 记下别人填上的输出编号，但不把它当成可播放视频或核销凭证。
func NoteOutputID(rec Record, outputID string) Record {
	rec.OutputAssetID = strings.TrimSpace(outputID)
	rec.OutputIsReceipt = false
	rec.ShowVideo = false
	rec.PlayStill = false
	rec.VideoURL = ""
	rec.BillingPassed = false
	rec.ProductionAuthorized = false
	rec.Charged = false
	if rec.Status == StatusFailed || rec.Status == StatusUnknown || rec.Status == "" {
		return rec
	}
	rec.Status = StatusUnknown
	return rec
}
