package billassemble

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveSpeaksToLoopbackFixture 用空闲环回端口证明适配器能报价和读余额。
// 夹具报价的 origin 是 fixture。预占、结算、释放不发送。这不是 Billing PASS。
func TestLiveSpeaksToLoopbackFixture(t *testing.T) {
	if base, token := os.Getenv("PRODUCT_BILLING_BASE_URL"), firstToken("PRODUCT_BILLING_TOKEN", "PLATFORM_BILLING_TOKEN"); base != "" && token != "" {
		t.Logf("现场计费环境变量已设置，本测试仍只用自建夹具，不向该地址扣款: %s", base)
	}
	fx := startTenantFixture(t)
	defer fx.close()
	if fx.port == 18101 || forbiddenListenPort(fx.port) {
		t.Fatalf("夹具占用了禁止端口 %d", fx.port)
	}

	live := &Live{
		AppID: "product-image", BillingToken: fx.token, BillingBase: fx.url, Enabled: true,
	}
	ctx := context.Background()
	quoted, err := live.Quote(ctx, QuoteCall{
		TenantID: "tenant_a", PayerAccountID: "acct_a", Capability: "image.generate.standard",
		Model: "standard", Size: "800x800", Quantity: 1, PricingVersion: PricingVersion,
		BusinessRef: "proj_a", IdempotencyKey: "fp-a",
	})
	if err != nil {
		t.Fatalf("报价失败: %v", err)
	}
	if quoted.Origin != QuoteOriginFixture || quoted.FundsState != FundsNotSent || !quoted.Sent || quoted.AmountMinor != 150 || quoted.BalanceMinor != 5000 || quoted.FundsShort {
		t.Fatalf("夹具报价不是 Billing PASS，且只能看见 A 的余额: %+v", quoted)
	}
	if !hasEntitlement(quoted.Entitlements, "image.generate.standard") || hasEntitlement(quoted.Entitlements, "image.generate.premium") {
		t.Fatalf("A 看见了不属于自己的能力: %+v", quoted.Entitlements)
	}
	estimate := fmt.Sprintf("本次预计 ¥%d.%02d", quoted.AmountMinor/100, quoted.AmountMinor%100)
	if estimate != "本次预计 ¥1.50" {
		t.Fatalf("有报价对象才显示估价，得到 %s", estimate)
	}
	again, err := live.Quote(ctx, QuoteCall{
		TenantID: "tenant_a", PayerAccountID: "acct_a", Capability: "image.generate.standard",
		Model: "standard", Size: "800x800", Quantity: 1, PricingVersion: PricingVersion,
		BusinessRef: "proj_a", IdempotencyKey: "fp-a",
	})
	if err != nil || again.UsageID != quoted.UsageID {
		t.Fatalf("同一幂等键应回到原报价: %+v %v", again, err)
	}

	other, err := live.Quote(ctx, QuoteCall{
		TenantID: "tenant_b", PayerAccountID: "acct_b", Capability: "image.generate.premium",
		Model: "premium", Size: "1024x1024", Quantity: 1, PricingVersion: PricingVersion,
		BusinessRef: "proj_b", IdempotencyKey: "fp-b",
	})
	if err != nil || other.BalanceMinor != 9000 || other.BalanceMinor == quoted.BalanceMinor || other.Origin != QuoteOriginFixture {
		t.Fatalf("B 应只看见自己的余额: %+v %v", other, err)
	}
	if _, err := live.Quote(ctx, QuoteCall{
		TenantID: "tenant_a", PayerAccountID: "acct_b", Capability: "image.generate.standard",
		Quantity: 1, PricingVersion: PricingVersion, BusinessRef: "proj_a", IdempotencyKey: "fp-cross",
	}); err == nil {
		t.Fatal("A 的租户不能用 B 的账号报价")
	}
	hidden, err := live.ReadBalanceFor(ctx, "tenant_a", "acct_b")
	if err != nil || hidden.Known || hidden.Minor == 9000 || hidden.Minor == 5000 {
		t.Fatalf("A 不能读到 B 的余额: %+v %v", hidden, err)
	}
	own, err := live.ReadBalanceFor(ctx, "tenant_b", "acct_b")
	if err != nil || !own.Known || own.Minor != 9000 {
		t.Fatalf("B 应读到自己的余额: %+v %v", own, err)
	}

	held, err := live.Hold(ctx, HoldCall{UsageID: quoted.UsageID, PayerAccountID: "acct_a", AmountMinor: quoted.AmountMinor, IdempotencyKey: "hold-a"})
	if err != ErrFundsNotExecuted || held.FundsAction != FundsNotSent || held.Sent {
		t.Fatalf("预占不应发送: %+v %v", held, err)
	}
	settled, err := live.Settle(ctx, SettleCall{UsageID: quoted.UsageID, IdempotencyKey: "chg-a"})
	if err != ErrFundsNotExecuted || settled.FundsAction != FundsNotSent || settled.Sent {
		t.Fatalf("结算不应发送: %+v %v", settled, err)
	}
	released, err := live.Release(ctx, ReleaseCall{HoldID: "uhold_a", IdempotencyKey: "rel-a"})
	if err != ErrFundsNotExecuted || released.FundsAction != FundsNotSent || released.Sent {
		t.Fatalf("释放不应发送: %+v %v", released, err)
	}
	if fx.fundHits() != 0 {
		t.Fatalf("夹具收到了资金请求: %d", fx.fundHits())
	}
	if fx.quoteHits() < 2 || fx.usageHits() < 2 || fx.balanceHits() < 2 {
		t.Fatalf("适配器没有对上夹具: quote=%d usage=%d balance=%d", fx.quoteHits(), fx.usageHits(), fx.balanceHits())
	}
	for _, body := range fx.deniedBodies() {
		if strings.Contains(body, "9000") || strings.Contains(body, "5000") {
			t.Fatalf("拒绝跨租户时泄漏了余额: %s", body)
		}
	}
}

func firstToken(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func forbiddenListenPort(port int) bool {
	switch port {
	case 18084, 18120, 8080, 8081, 5173, 18101:
		return true
	default:
		return false
	}
}

func hasEntitlement(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

type tenantBox struct {
	tenant     string
	cash       int64
	capability string
}

type usageRow struct {
	id       string
	tenant   string
	payer    string
	quantity int64
}

type tenantFixture struct {
	url   string
	token string
	port  int
	srv   *http.Server

	mu      sync.Mutex
	funds   int
	quotes  int
	usages  int
	balance int
	denied  []string
	byKey   map[string]usageRow
}

func (f *tenantFixture) fundHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.funds
}

func (f *tenantFixture) quoteHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quotes
}

func (f *tenantFixture) usageHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usages
}

func (f *tenantFixture) balanceHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.balance
}

func (f *tenantFixture) deniedBodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.denied...)
}

func (f *tenantFixture) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = f.srv.Shutdown(ctx)
}

func startTenantFixture(t *testing.T) *tenantFixture {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if forbiddenListenPort(port) {
		_ = ln.Close()
		t.Fatalf("空闲端口落在禁止端口 %d", port)
	}
	fx := &tenantFixture{
		token: "fixture-tok",
		port:  port,
		url:   "http://" + ln.Addr().String(),
		byKey: map[string]usageRow{},
	}
	accounts := map[string]tenantBox{
		"acct_a": {tenant: "tenant_a", cash: 5000, capability: "image.generate.standard"},
		"acct_b": {tenant: "tenant_b", cash: 9000, capability: "image.generate.premium"},
	}
	fx.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-App-ID") != "product-image" || r.Header.Get("X-PilotSeaview-Internal-Token") != fx.token {
			http.Error(w, `{"ok":false,"error":{"code":"unauthorized"}}`, http.StatusForbidden)
			return
		}
		path := r.URL.Path
		if strings.Contains(path, "hold") || strings.Contains(path, "charge") || strings.Contains(path, "refund") || strings.Contains(path, "release") || strings.Contains(path, "settle") {
			fx.mu.Lock()
			fx.funds++
			fx.mu.Unlock()
			writeFixture(w, http.StatusForbidden, map[string]any{"ok": false, "error": map[string]string{"code": "funds_not_accepted"}})
			return
		}
		switch path {
		case "/internal/v1/billing/unified/usage":
			fx.mu.Lock()
			fx.usages++
			fx.mu.Unlock()
			fx.serveUsage(w, r, accounts)
		case "/internal/v1/billing/unified/quote":
			fx.mu.Lock()
			fx.quotes++
			fx.mu.Unlock()
			fx.serveQuote(w, r)
		case "/internal/v1/billing/unified/accounts":
			fx.mu.Lock()
			fx.balance++
			fx.mu.Unlock()
			fx.serveBalance(w, r, accounts)
		default:
			writeFixture(w, http.StatusNotFound, map[string]any{"ok": false, "error": map[string]string{"code": "billing_route_not_found"}})
		}
	})}
	go func() { _ = fx.srv.Serve(ln) }()
	t.Cleanup(fx.close)
	return fx
}

func (f *tenantFixture) serveUsage(w http.ResponseWriter, r *http.Request, accounts map[string]tenantBox) {
	var req struct {
		PayerAccountID string `json:"payer_account_id"`
		TenantID       string `json:"tenant_id"`
		Quantity       int64  `json:"quantity"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if json.Unmarshal(body, &req) != nil || req.Quantity <= 0 || req.IdempotencyKey == "" {
		writeFixture(w, http.StatusBadRequest, map[string]any{"ok": false, "error": map[string]string{"code": "invalid_usage"}})
		return
	}
	box, ok := accounts[req.PayerAccountID]
	if !ok || box.tenant != req.TenantID {
		f.deny(w)
		return
	}
	f.mu.Lock()
	row, seen := f.byKey[req.TenantID+"\n"+req.IdempotencyKey]
	if !seen {
		row = usageRow{id: "usg_" + req.IdempotencyKey, tenant: req.TenantID, payer: req.PayerAccountID, quantity: req.Quantity}
		f.byKey[req.TenantID+"\n"+req.IdempotencyKey] = row
	}
	f.mu.Unlock()
	writeFixture(w, http.StatusOK, map[string]any{"ok": true, "fact": map[string]any{"usage_id": row.id}})
}

func (f *tenantFixture) serveQuote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UsageID string `json:"usage_id"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if json.Unmarshal(body, &req) != nil || req.UsageID == "" {
		writeFixture(w, http.StatusBadRequest, map[string]any{"ok": false, "error": map[string]string{"code": "invalid_quote"}})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var row usageRow
	found := false
	for _, item := range f.byKey {
		if item.id == req.UsageID {
			row = item
			found = true
			break
		}
	}
	if !found {
		writeFixture(w, http.StatusNotFound, map[string]any{"ok": false, "error": map[string]string{"code": "usage_not_found"}})
		return
	}
	writeFixture(w, http.StatusOK, map[string]any{"ok": true, "quote": map[string]any{
		"usage_id": row.id, "amount_minor": 150 * row.quantity, "origin": QuoteOriginFixture,
	}})
}

func (f *tenantFixture) serveBalance(w http.ResponseWriter, r *http.Request, accounts map[string]tenantBox) {
	accountID := r.URL.Query().Get("account_id")
	tenantID := r.URL.Query().Get("tenant_id")
	box, ok := accounts[accountID]
	if !ok || tenantID == "" || box.tenant != tenantID {
		f.deny(w)
		return
	}
	writeFixture(w, http.StatusOK, map[string]any{"ok": true, "snapshot": map[string]any{
		"account":      map[string]any{"cash_balance_minor": box.cash},
		"entitlements": []any{map[string]any{"capability": box.capability, "status": "active"}},
	}})
}

func (f *tenantFixture) deny(w http.ResponseWriter) {
	raw := `{"ok":false,"error":{"code":"billing_account_not_visible"}}`
	f.mu.Lock()
	f.denied = append(f.denied, raw)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, raw)
}

func writeFixture(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}
