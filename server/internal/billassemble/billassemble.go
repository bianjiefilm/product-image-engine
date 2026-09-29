// Package billassemble 把产品图用量接到已有平台报价和查回。
// 真实端口可以报价、只读余额、查询任务。本轮不发送预占、结算或释放。
package billassemble

import (
	"context"
	"errors"
	"strings"
)

const (
	PricingVersion = "pricing-2026-09"

	ConnMissing       = "配置缺失"
	ConnUnimplemented = "未实现"
	ConnReady         = "已配置"

	SettlementUnknown = "unknown"
	PendingCheck      = "作品完成待核对"
	FundsNotExecuted  = "未执行"
	// FundsNotSent 表示本轮没有发送预占、结算或释放。它不是“已核对未扣”。
	FundsNotSent = "not_sent"
	// FundsUnconfigured 表示身份或计费配置缺失，调用没有发出。
	FundsUnconfigured = "unconfigured"
	// QuoteOriginFixture 标记夹具或测试双报价。它不是 Billing PASS。
	QuoteOriginFixture = "fixture"
)

var (
	ErrConfigMissing    = errors.New(ConnMissing)
	ErrUnimplemented    = errors.New(ConnUnimplemented)
	ErrFundsNotExecuted = errors.New(FundsNotExecuted)
	ErrUnavailable      = errors.New("计费服务不可达")
	ErrUsageInvalid     = errors.New("用量事实不完整")
)

// LinkConfig 是计费连接。缺任一项都是配置缺失，而不是「未扣费」。
type LinkConfig struct {
	Enabled bool
	BaseURL string
	Token   string
	AppID   string
}

func Connection(cfg LinkConfig) string {
	if !cfg.Enabled || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Token) == "" || strings.TrimSpace(cfg.AppID) == "" {
		return ConnMissing
	}
	return ConnReady
}

// QuoteCall 是服务端已经决定的用量。付款人不能从浏览器正文抄进来。
type QuoteCall struct {
	TenantID       string
	PayerAccountID string
	Capability     string
	Model          string
	Size           string
	Quantity       int64
	PricingVersion string
	BusinessRef    string
	IdempotencyKey string
}

// QuoteFact 是一次报价读到的事实。余额未知时不得当成充足。
type QuoteFact struct {
	UsageID           string
	AmountMinor       int64
	Currency          string
	IncludedAllowance bool
	BalanceKnown      bool
	BalanceMinor      int64
	FundsShort        bool
	Sent              bool
	Connection        string
	Origin            string
	FundsState        string
	EntitlementsKnown bool
	Entitlements      []string
}

type HoldCall struct {
	UsageID        string
	PayerAccountID string
	IdempotencyKey string
	AmountMinor    int64
}

type HoldFact struct {
	HoldID      string
	AmountMinor int64
	Sent        bool
	FundsAction string
	Connection  string
}

type ReleaseCall struct {
	HoldID         string
	IdempotencyKey string
}

type ReleaseFact struct {
	ReleasedMinor int64
	Sent          bool
	FundsAction   string
}

type SettleCall struct {
	UsageID        string
	IdempotencyKey string
}

type SettleFact struct {
	ChargeID    string
	Sent        bool
	FundsAction string
}

type LookupCall struct {
	TaskID  string
	UsageID string
}

// LookupFact 记下任务和费用引用。引用只是任务单上的字符串，Settlement 在没有账本核对时保持 unknown。
type LookupFact struct {
	TaskID      string
	TaskStatus  string
	ChargeRef   string
	FoundTask   bool
	FoundCharge bool
	Sent        bool
	Settlement  string
	Connection  string
}

type Balance struct {
	Known      bool
	Minor      int64
	Connection string
}

// Port 是报价装配。测试双和真实端口都实现它。
type Port interface {
	Quote(ctx context.Context, in QuoteCall) (QuoteFact, error)
	Hold(ctx context.Context, in HoldCall) (HoldFact, error)
	Release(ctx context.Context, in ReleaseCall) (ReleaseFact, error)
	Settle(ctx context.Context, in SettleCall) (SettleFact, error)
	Lookup(ctx context.Context, in LookupCall) (LookupFact, error)
	ReadBalance(ctx context.Context, accountID string) (Balance, error)
}

// ReconcileResult 是生成是否完成。费用引用不能当成计费通过。
type ReconcileResult struct {
	Settlement    string
	BillingPassed bool
	Regenerate    bool
}

// Reconcile 在任何路径都不把计费写成通过，也不把费用引用写成已结算。
// 作品完成时待核对，不重新生成。没有任务时保持 unknown。
func Reconcile(workComplete bool, _ LookupFact) ReconcileResult {
	if workComplete {
		return ReconcileResult{Settlement: PendingCheck, BillingPassed: false, Regenerate: false}
	}
	return ReconcileResult{Settlement: SettlementUnknown, BillingPassed: false, Regenerate: false}
}
