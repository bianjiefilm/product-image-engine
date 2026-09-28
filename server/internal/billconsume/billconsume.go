// Package billconsume 是产品图的统一计费消费端。
// 它只组装配额事实、付款主体和报价确认，不建钱包、不复制余额、不扣款。
// 没有真实平台扣费时，Billing 与 Production Authorized 保持未通过。
package billconsume

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const (
	KindPersonal     = "personal"
	KindOrganization = "organization"

	DisplayPersonal = "个人付款"
)

var (
	ErrPayerNotAuthorized = errors.New("billconsume: 付款主体未授权")
	ErrPhoneNotAccount    = errors.New("billconsume: 手机号不是平台账号")
	ErrUsageInvalid       = errors.New("billconsume: 用量事实不完整")
	ErrPrivatePoints      = errors.New("billconsume: 不使用图像点数")
	ErrEntitlementMissing = errors.New("billconsume: 能力未开通")
	ErrReturnTarget       = errors.New("billconsume: 返回位置不在本应用登记表")
	ErrLookupRequired     = errors.New("billconsume: 先查原任务和原扣费")
	ErrIdempotencyChanged = errors.New("billconsume: 不能更换幂等键")
	ErrOrderFunds         = errors.New("billconsume: 不能用订单服务款扣图像费")
)

// Context 是服务端已经验证过的工作上下文。客户端自报的组织不会出现在这里。
type Context struct {
	SourceType         string
	SourceRef          string
	PersonalAccountRef string
	Phone              string
	VerifiedOrg        *OrgPayer
	AuthorizedOrgs     []OrgPayer
}

// OrgPayer 是已验证的组织付款主体，不含余额。
type OrgPayer struct {
	TenantID   string
	AccountRef string
	Display    string
}

// Payer 是本次报价使用的付款主体。
type Payer struct {
	Kind       string
	AccountRef string
	Display    string
	TenantID   string
}

// Book 是某付款主体的可见标签。本包不保存金额。
type Book struct {
	AccountRef string
	Label      string
}

// UsageFact 是本产品上报的用量事实。
type UsageFact struct {
	ImageCount     int
	Resolution     string
	Capability     string
	PricingVersion string
}

// QuoteView 是统一 Billing 给出的报价展示，不是本地账本。
type QuoteView struct {
	AmountMinor       *int64
	Currency          string
	IncludedAllowance bool
	PrivatePoints     string
}

// Confirmation 表示当前参数是否仍与已确认报价一致。
type Confirmation struct {
	GenerateAllowed bool
	MustRequote     bool
}

// TopupInput 描述余额不足时的统一充值入口。
type TopupInput struct {
	QuoteRef     string
	ReturnTarget string
	Allowed      []string
	FundsShort   bool
}

// Handoff 只带回来源位置和报价引用，不允许据此生成。
type Handoff struct {
	Entry           string `json:"entry"`
	ReturnTarget    string `json:"return_target"`
	QuoteRef        string `json:"quote_ref"`
	GenerateAllowed bool   `json:"generate_allowed"`
}

// PageDecision 是从充值页返回后的判定。
type PageDecision struct {
	GenerateAllowed bool
	Reason          string
}

// Attempt 是一次可能超时的生成与扣费尝试。
type Attempt struct {
	Status         string
	TaskID         string
	ChargeRef      string
	IdempotencyKey string
	ReplacementKey string
	Resubmit       bool
	Recharge       bool
}

// LevelsReport 是 HUI-2085 五级完成口径。前一级不推出后一级。
type LevelsReport struct {
	ImplementationReady  string
	Integration          string
	Billing              string
	ChannelBilling       string
	ProductionAuthorized string
}

func ResolvePayer(ctx Context) (Payer, error) {
	sourced := ctx.SourceType == "order" || ctx.SourceType == "campaign"
	if sourced {
		if strings.TrimSpace(ctx.SourceRef) == "" || ctx.VerifiedOrg == nil {
			return Payer{}, ErrPayerNotAuthorized
		}
		org := *ctx.VerifiedOrg
		if !orgAuthorized(org, ctx.AuthorizedOrgs) {
			return Payer{}, ErrPayerNotAuthorized
		}
		if looksLikePhone(org.AccountRef) {
			return Payer{}, ErrPhoneNotAccount
		}
		display := strings.TrimSpace(org.Display)
		if display == "" {
			display = "组织付款"
		}
		return Payer{
			Kind: KindOrganization, AccountRef: org.AccountRef, Display: display, TenantID: org.TenantID,
		}, nil
	}
	ref := strings.TrimSpace(ctx.PersonalAccountRef)
	if ref == "" || looksLikePhone(ref) {
		return Payer{}, ErrPhoneNotAccount
	}
	return Payer{Kind: KindPersonal, AccountRef: ref, Display: DisplayPersonal}, nil
}

func orgAuthorized(org OrgPayer, allowed []OrgPayer) bool {
	if strings.TrimSpace(org.TenantID) == "" || strings.TrimSpace(org.AccountRef) == "" {
		return false
	}
	for _, item := range allowed {
		if item.TenantID == org.TenantID && item.AccountRef == org.AccountRef {
			return true
		}
	}
	return false
}

func VisibleBooks(current Payer, books []Book) []Book {
	out := make([]Book, 0, 1)
	for _, book := range books {
		if book.AccountRef == current.AccountRef {
			out = append(out, book)
		}
	}
	return out
}

func NewUsageFact(count int, resolution, capability, version string) (UsageFact, error) {
	fact := UsageFact{
		ImageCount:     count,
		Resolution:     strings.TrimSpace(resolution),
		Capability:     strings.TrimSpace(capability),
		PricingVersion: strings.TrimSpace(version),
	}
	if fact.ImageCount <= 0 || fact.Resolution == "" || fact.Capability == "" || fact.PricingVersion == "" {
		return UsageFact{}, ErrUsageInvalid
	}
	if privatePoints(fact.Capability) || privatePoints(fact.Resolution) {
		return UsageFact{}, ErrPrivatePoints
	}
	return fact, nil
}

func (f UsageFact) Report(payerAccountRef, idempotencyKey string) map[string]any {
	return map[string]any{
		"image_count":      f.ImageCount,
		"quantity":         int64(f.ImageCount),
		"resolution":       f.Resolution,
		"capability":       f.Capability,
		"pricing_version":  f.PricingVersion,
		"payer_account_id": payerAccountRef,
		"idempotency_key":  idempotencyKey,
		"currency":         "CNY",
	}
}

func Present(view QuoteView) (string, error) {
	if privatePoints(view.PrivatePoints) || privatePoints(view.Currency) {
		return "", ErrPrivatePoints
	}
	if view.IncludedAllowance {
		return "已包含额度", nil
	}
	if view.AmountMinor != nil {
		if view.Currency != "" && view.Currency != "CNY" {
			return "", ErrUsageInvalid
		}
		return formatCNY(*view.AmountMinor), nil
	}
	return "待确认", nil
}

func formatCNY(minor int64) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s本次预计 ¥%d.%02d", sign, minor/100, minor%100)
}

func Allow(capability string, entitled bool, _ int64) error {
	if strings.TrimSpace(capability) == "" || !entitled {
		return ErrEntitlementMissing
	}
	return nil
}

func Fingerprint(payer Payer, fact UsageFact) string {
	raw := strings.Join([]string{
		payer.Kind, payer.AccountRef, payer.TenantID,
		fmt.Sprintf("%d", fact.ImageCount), fact.Resolution, fact.Capability, fact.PricingVersion,
	}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func Confirm(confirmedFingerprint, currentFingerprint string) Confirmation {
	if confirmedFingerprint != "" && confirmedFingerprint == currentFingerprint {
		return Confirmation{GenerateAllowed: true}
	}
	return Confirmation{GenerateAllowed: false, MustRequote: true}
}

func TopupHandoff(in TopupInput) (Handoff, error) {
	target := strings.TrimSpace(in.ReturnTarget)
	if strings.Contains(target, "://") || !listed(target, in.Allowed) {
		return Handoff{}, ErrReturnTarget
	}
	if strings.TrimSpace(in.QuoteRef) == "" {
		return Handoff{}, ErrUsageInvalid
	}
	return Handoff{
		Entry:           "billing_center",
		ReturnTarget:    target,
		QuoteRef:        in.QuoteRef,
		GenerateAllowed: false,
	}, nil
}

func AfterTopupPage(page string, quoteFresh bool) PageDecision {
	if page == "success" && !quoteFresh {
		return PageDecision{GenerateAllowed: false, Reason: "requote_required"}
	}
	if !quoteFresh {
		return PageDecision{GenerateAllowed: false, Reason: "requote_required"}
	}
	return PageDecision{GenerateAllowed: true}
}

func Recover(a Attempt) (Attempt, error) {
	status := strings.ToLower(strings.TrimSpace(a.Status))
	if status != "timeout" && status != "unknown" {
		a.Resubmit = false
		a.Recharge = false
		return a, nil
	}
	if strings.TrimSpace(a.TaskID) == "" || strings.TrimSpace(a.ChargeRef) == "" {
		return Attempt{}, ErrLookupRequired
	}
	if strings.TrimSpace(a.ReplacementKey) != "" && a.ReplacementKey != a.IdempotencyKey {
		return Attempt{}, ErrIdempotencyChanged
	}
	a.Resubmit = false
	a.Recharge = false
	return a, nil
}

func RejectPaymentSource(source string) error {
	switch strings.TrimSpace(source) {
	case "order_service", "order_payment":
		return ErrOrderFunds
	default:
		return nil
	}
}

// Levels 固定完成本地实现等级。假报价不会把后四级改成通过。
func Levels() LevelsReport {
	return LevelsReport{
		ImplementationReady:  "PASS",
		Integration:          "UNKNOWN",
		Billing:              "UNKNOWN",
		ChannelBilling:       "NOT_APPLICABLE",
		ProductionAuthorized: AuthorizeProduction(false, ""),
	}
}

// AuthorizeProduction 在本票恒为 NOT_AUTHORIZED。
// 本地假证据、空证据和未显式授权都不能放行真实扣款。
func AuthorizeProduction(explicit bool, evidence string) string {
	if !explicit || strings.TrimSpace(evidence) == "" || evidence == "local-fake" {
		return "NOT_AUTHORIZED"
	}
	return "NOT_AUTHORIZED"
}

func listed(target string, allowed []string) bool {
	if target == "" {
		return false
	}
	for _, item := range allowed {
		if item == target {
			return true
		}
	}
	return false
}

func looksLikePhone(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	}
	if len(raw) < 11 || len(raw) > 15 {
		return false
	}
	for _, r := range raw {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func privatePoints(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(raw, "点数") || strings.Contains(raw, "修点") || strings.Contains(lower, "image_coin") || strings.Contains(lower, "image coin")
}
