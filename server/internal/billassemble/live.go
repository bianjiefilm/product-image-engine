package billassemble

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

// Live 调用已经存在的平台接口。预占、结算和释放在本轮不发送。
type Live struct {
	AppID        string
	BillingToken string
	TaskToken    string
	BillingBase  string
	TaskBase     string
	Enabled      bool
	HTTP         *http.Client
}

func (p *Live) Quote(ctx context.Context, in QuoteCall) (QuoteFact, error) {
	if err := p.billingReady(); err != nil {
		return QuoteFact{Connection: ConnMissing, FundsState: FundsUnconfigured}, err
	}
	if strings.TrimSpace(in.PayerAccountID) == "" || in.Quantity <= 0 || strings.TrimSpace(in.Capability) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.PricingVersion) == "" {
		return QuoteFact{Connection: ConnReady, FundsState: FundsNotSent}, ErrUsageInvalid
	}
	usageID, err := p.ingest(ctx, in)
	if err != nil {
		state := FundsNotSent
		conn := ConnReady
		if errors.Is(err, ErrConfigMissing) {
			state = FundsUnconfigured
			conn = ConnMissing
		}
		return QuoteFact{Connection: conn, FundsState: state}, err
	}
	fact, err := p.quoteUsage(ctx, usageID)
	if err != nil {
		return QuoteFact{Connection: ConnReady, UsageID: usageID, FundsState: FundsNotSent}, err
	}
	fact.UsageID = usageID
	fact.Connection = ConnReady
	fact.Sent = true
	fact.Currency = "CNY"
	fact.FundsState = FundsNotSent
	p.attachBalance(ctx, in.TenantID, in.PayerAccountID, &fact)
	return fact, nil
}

func (p *Live) Hold(context.Context, HoldCall) (HoldFact, error) {
	if err := p.billingReady(); err != nil {
		return HoldFact{Connection: ConnMissing, FundsAction: FundsUnconfigured, Sent: false}, err
	}
	return HoldFact{Connection: ConnReady, FundsAction: FundsNotSent, Sent: false}, ErrFundsNotExecuted
}

func (p *Live) Release(context.Context, ReleaseCall) (ReleaseFact, error) {
	if err := p.billingReady(); err != nil {
		return ReleaseFact{FundsAction: FundsUnconfigured, Sent: false}, err
	}
	return ReleaseFact{FundsAction: FundsNotSent, Sent: false}, ErrFundsNotExecuted
}

func (p *Live) Settle(context.Context, SettleCall) (SettleFact, error) {
	if err := p.billingReady(); err != nil {
		return SettleFact{FundsAction: FundsUnconfigured, Sent: false}, err
	}
	return SettleFact{FundsAction: FundsNotSent, Sent: false}, ErrFundsNotExecuted
}

func (p *Live) Lookup(ctx context.Context, in LookupCall) (LookupFact, error) {
	out := LookupFact{Settlement: SettlementUnknown, TaskID: strings.TrimSpace(in.TaskID)}
	if out.TaskID == "" {
		return out, nil
	}
	if strings.TrimSpace(p.TaskBase) == "" || strings.TrimSpace(p.TaskToken) == "" || strings.TrimSpace(p.AppID) == "" || loopbackBase(p.TaskBase) != nil {
		out.Connection = ConnMissing
		return out, ErrConfigMissing
	}
	client := p.consumer(p.TaskToken)
	client.Bases.Task = strings.TrimRight(p.TaskBase, "/")
	res, err := client.Call(ctx, platformconsumer.Call{
		Operation:  "task.get",
		PathParams: map[string]string{"id": out.TaskID},
		Query:      map[string]string{"app_id": p.AppID},
	})
	if err != nil || !res.Sent || res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, ErrUnavailable
	}
	var doc map[string]any
	if json.Unmarshal(res.Body, &doc) != nil {
		return out, ErrUnavailable
	}
	out.Sent = true
	out.FoundTask = true
	if status, _ := doc["status"].(string); status != "" {
		out.TaskStatus = status
	}
	for _, key := range []string{"charge_id", "charge_ref", "fee_ref"} {
		if ref, _ := doc[key].(string); strings.TrimSpace(ref) != "" {
			out.FoundCharge = true
			out.ChargeRef = strings.TrimSpace(ref)
			break
		}
	}
	return out, nil
}

func (p *Live) ReadBalance(ctx context.Context, accountID string) (Balance, error) {
	return p.ReadBalanceFor(ctx, "", accountID)
}

// ReadBalanceFor 只读指定租户名下的账号。租户不匹配时余额未知，不返回别的租户的数字。
func (p *Live) ReadBalanceFor(ctx context.Context, tenantID, accountID string) (Balance, error) {
	if err := p.billingReady(); err != nil {
		return Balance{Connection: ConnMissing}, err
	}
	var fact QuoteFact
	p.attachBalance(ctx, tenantID, accountID, &fact)
	if !fact.BalanceKnown {
		return Balance{Known: false, Connection: ConnReady}, nil
	}
	return Balance{Known: true, Minor: fact.BalanceMinor, Connection: ConnReady}, nil
}

func (p *Live) billingReady() error {
	if p == nil || !p.Enabled || strings.TrimSpace(p.BillingBase) == "" || strings.TrimSpace(p.BillingToken) == "" || strings.TrimSpace(p.AppID) == "" {
		return ErrConfigMissing
	}
	if err := loopbackBase(p.BillingBase); err != nil {
		return ErrConfigMissing
	}
	return nil
}

func (p *Live) ingest(ctx context.Context, in QuoteCall) (string, error) {
	payload := map[string]any{
		"app_id": p.AppID, "capability": in.Capability, "quantity": in.Quantity,
		"pricing_version": in.PricingVersion, "business_ref": in.BusinessRef,
		"payer_account_id": in.PayerAccountID, "idempotency_key": in.IdempotencyKey,
	}
	if strings.TrimSpace(in.TenantID) != "" {
		payload["tenant_id"] = strings.TrimSpace(in.TenantID)
	}
	if strings.TrimSpace(in.Model) != "" {
		payload["model"] = strings.TrimSpace(in.Model)
	}
	if strings.TrimSpace(in.Size) != "" {
		payload["size"] = strings.TrimSpace(in.Size)
	}
	status, body, err := p.postBilling(ctx, "/internal/v1/billing/unified/usage", payload)
	if err != nil {
		return "", ErrUnavailable
	}
	if billingCode(body) == "billing_route_not_found" {
		return "", ErrUnimplemented
	}
	if status < 200 || status >= 300 {
		return "", ErrUnavailable
	}
	var doc struct {
		Fact struct {
			UsageID string `json:"usage_id"`
		} `json:"fact"`
	}
	if json.Unmarshal(body, &doc) != nil || strings.TrimSpace(doc.Fact.UsageID) == "" {
		return "", ErrUnavailable
	}
	return doc.Fact.UsageID, nil
}

func (p *Live) quoteUsage(ctx context.Context, usageID string) (QuoteFact, error) {
	raw, err := json.Marshal(map[string]string{"usage_id": usageID})
	if err != nil {
		return QuoteFact{}, err
	}
	client := p.consumer(p.BillingToken)
	client.Bases.Billing = strings.TrimRight(p.BillingBase, "/")
	res, err := client.Call(ctx, platformconsumer.Call{Operation: "billing.quote", Body: raw})
	if billingCode(res.Body) == "billing_route_not_found" {
		return QuoteFact{}, ErrUnimplemented
	}
	if err != nil || !res.Sent || res.StatusCode < 200 || res.StatusCode >= 300 {
		return QuoteFact{}, ErrUnavailable
	}
	var doc struct {
		Quote struct {
			AmountMinor int64  `json:"amount_minor"`
			Origin      string `json:"origin"`
		} `json:"quote"`
	}
	if json.Unmarshal(res.Body, &doc) != nil {
		return QuoteFact{}, ErrUnavailable
	}
	return QuoteFact{AmountMinor: doc.Quote.AmountMinor, Currency: "CNY", Origin: strings.TrimSpace(doc.Quote.Origin)}, nil
}

func (p *Live) attachBalance(ctx context.Context, tenantID, accountID string, fact *QuoteFact) {
	query := url.Values{}
	query.Set("account_id", accountID)
	if strings.TrimSpace(tenantID) != "" {
		query.Set("tenant_id", strings.TrimSpace(tenantID))
	}
	status, body, err := p.getBilling(ctx, "/internal/v1/billing/unified/accounts?"+query.Encode())
	if err != nil || status != http.StatusOK {
		return
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return
	}
	var snap map[string]json.RawMessage
	if json.Unmarshal(root["snapshot"], &snap) != nil {
		return
	}
	var account struct {
		Cash int64 `json:"cash_balance_minor"`
	}
	if json.Unmarshal(snap["account"], &account) != nil {
		return
	}
	fact.BalanceKnown = true
	fact.BalanceMinor = account.Cash
	if !fact.IncludedAllowance && fact.BalanceMinor < fact.AmountMinor {
		fact.FundsShort = true
	}
	raw, ok := snap["entitlements"]
	if !ok {
		return
	}
	var rows []struct {
		Capability string `json:"capability"`
		Status     string `json:"status"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	fact.EntitlementsKnown = true
	for _, row := range rows {
		if row.Capability == "" {
			continue
		}
		if row.Status != "" && row.Status != "active" {
			continue
		}
		fact.Entitlements = append(fact.Entitlements, row.Capability)
	}
}

func (p *Live) postBilling(ctx context.Context, path string, payload any) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BillingBase, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	p.auth(req)
	return p.do(req)
}

func (p *Live) getBilling(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.BillingBase, "/")+path, nil)
	if err != nil {
		return 0, nil, err
	}
	p.auth(req)
	return p.do(req)
}

func (p *Live) auth(req *http.Request) {
	req.Header.Set(platformconsumer.HeaderApp, p.AppID)
	req.Header.Set(platformconsumer.HeaderToken, p.BillingToken)
}

func (p *Live) do(req *http.Request) (int, []byte, error) {
	res, err := p.httpClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, body, nil
}

func (p *Live) httpClient() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func (p *Live) consumer(token string) *platformconsumer.Client {
	return &platformconsumer.Client{
		Caller: platformconsumer.Caller{Mode: "app", AppID: p.AppID, Token: token},
		HTTP:   p.httpClient(),
	}
}

func billingCode(body []byte) string {
	var doc struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &doc)
	return doc.Error.Code
}

func loopbackBase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return ErrConfigMissing
}
