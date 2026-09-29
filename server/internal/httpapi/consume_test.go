package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/billassemble"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

func TestConsumeQuoteUsesServerPayerAndPort(t *testing.T) {
	f := newFixture(t, nil)
	rec := &billassemble.Record{
		UnitMinor: 150, BalanceKnown: true, BalanceMinor: 10_000,
		Entitled: []string{"image.generate.standard"},
	}
	f.srv.Bills = rec
	f.rearm(t)
	tok := f.stub.mintExtra("jia@x.com", "product-image", jwt.MapClaims{
		"org_memberships": []any{"org_a"},
		"org_payers": []any{
			map[string]any{"tenant_id": "org_a", "account_ref": "acct_org_a", "display": "A商家付款"},
			map[string]any{"tenant_id": "org_b", "account_ref": "acct_org_b", "display": "B商家付款"},
		},
	})
	st, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("session: %d %v", st, session)
	}
	principal, _ := session["principal"].(map[string]any)
	account, _ := principal["account_id"].(string)
	project, err := f.st.CreateProject(t.Context(), projectInput(account))
	if err != nil {
		t.Fatal(err)
	}
	quoteBody := map[string]any{
		"project_id": project.ID, "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard", "pricing_version": "pricing-cheap",
		"source_type": "campaign", "source_ref": "camp_body", "source_tenant_id": "org_a",
		"phone": "13800138000", "payment_source": "billing_account",
		"books": []any{
			map[string]any{"account_ref": "acct_org_a", "label": "A商家付款"},
			map[string]any{"account_ref": "acct_org_b", "label": "B商家付款"},
		},
	}
	st, body := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, quoteBody)
	if st != http.StatusOK {
		t.Fatalf("quote: %d %v", st, body)
	}
	if body["payer_display"] != "个人付款" || body["payer_account_ref"] != account {
		t.Fatalf("独立工程必须个人付款，请求体里的活动和组织不能改 payer: %v", body)
	}
	if body["presentation"] != "本次预计 ¥1.50" || body["quoted"] != true || body["settlement"] != "unknown" || body["connection"] != "已配置" {
		t.Fatalf("报价应来自端口且尚未结算: %v", body)
	}
	if body["charged"] != nil || body["generate_allowed"] != false {
		t.Fatalf("charged 必须是 JSON null，报价本身不允许生成: %v", body)
	}
	if body["production_authorized"] != "NOT_AUTHORIZED" || body["billing_pass"] != "UNKNOWN" {
		t.Fatalf("不能把计费标成生产通过: %v", body)
	}
	usage, _ := body["usage_fact"].(map[string]any)
	if usage["pricing_version"] != billassemble.PricingVersion || usage["payer_account_id"] != account {
		t.Fatalf("价格版本和付款人必须来自服务端: %v", usage)
	}
	ref, _ := body["quote_ref"].(string)
	if !strings.HasPrefix(ref, "usg_") {
		t.Fatalf("quote_ref 应使用平台用量号: %q", ref)
	}
	raw := mapString(body)
	if containsAny(raw, "B商家付款", "13800138000", "点数", "修点", "pricing-cheap") {
		t.Fatalf("应答泄漏了不该出现的内容: %s", raw)
	}
	if st, denied := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, withPayer(quoteBody, "acct_attacker")); st != http.StatusForbidden {
		t.Fatalf("请求体里的其他付款人应拒绝: %d %v", st, denied)
	}
	if st, phone := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, withPayer(quoteBody, "13800138000")); st != http.StatusBadRequest || errCode(phone) != "phone_not_account" {
		t.Fatalf("手机号不能当付款账号: %d %v", st, phone)
	}

	st, confirmed := f.do(t, "POST", "/api/v1/billing/consume/confirm", tok, quoteBody)
	if st != http.StatusOK || confirmed["generate_allowed"] != true || confirmed["charged"] != nil {
		t.Fatalf("确认报价后可以生成，但不结算: %d %v", st, confirmed)
	}
	if rec.NetReserved() != 0 || rec.Settles != 0 {
		t.Fatalf("确认不能预占或结算，净额必须为零: reserved=%d settles=%d", rec.NetReserved(), rec.Settles)
	}
	quoteBody["image_count"] = 2
	st, changed := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, quoteBody)
	if st != http.StatusOK || changed["must_requote"] != true || changed["generate_allowed"] != false || changed["charged"] != nil {
		t.Fatalf("改数量后必须重新确认: %d %v", st, changed)
	}
	if changed["presentation"] != "本次预计 ¥3.00" {
		t.Fatalf("数量变化后金额要重算: %v", changed["presentation"])
	}

	orgProject, err := f.st.CreateProject(t.Context(), store.Project{
		TenantID: "org_a", Name: "活动工程", CreatedBy: "usr", SourceType: "campaign", SourceRef: "camp_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	orgBody := map[string]any{
		"project_id": orgProject.ID, "source_type": "campaign", "source_ref": "camp_body",
		"source_tenant_id": "org_b", "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard", "pricing_version": "pricing-cheap",
		"books": quoteBody["books"],
	}
	st, org := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, orgBody)
	if st != http.StatusOK || org["payer_display"] != "A商家付款" || org["payer_account_ref"] != "acct_org_a" {
		t.Fatalf("活动工程只用已验证的本组织付款，忽略请求体租户: %d %v", st, org)
	}
	if containsAny(mapString(org), "B商家付款") {
		t.Fatalf("不能看到别的商家: %v", org)
	}
	if st, denied := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, withPayer(orgBody, "acct_attacker")); st != http.StatusForbidden {
		t.Fatalf("活动工程也不能改用请求体付款人: %d %v", st, denied)
	}
	foreign, err := f.st.CreateProject(t.Context(), store.Project{
		TenantID: "org_b", Name: "别人的工程", CreatedBy: "usr", SourceType: "campaign", SourceRef: "camp_b",
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignBody := map[string]any{
		"project_id": foreign.ID, "source_type": "campaign", "source_tenant_id": "org_b",
		"image_count": 1, "resolution": "800x800", "capability": "image.generate.standard",
	}
	if st, denied := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, foreignBody); st != http.StatusNotFound {
		t.Fatalf("没有成员资格不能读另一租户的工程: %d %v", st, denied)
	}
	if rec.NetReserved() != 0 || rec.Settles != 0 {
		t.Fatalf("整段报价不能留下预占: reserved=%d settles=%d", rec.NetReserved(), rec.Settles)
	}
}

func TestConsumeRefusesOrderFundsAndRereadsBalanceAfterTopup(t *testing.T) {
	f := newFixture(t, nil)
	rec := &billassemble.Record{
		UnitMinor: 150, BalanceKnown: true, BalanceMinor: 0,
		Entitled: []string{"image.generate.standard"},
	}
	f.srv.Bills = rec
	f.rearm(t)
	tok := f.stub.mintExtra("jia@x.com", "product-image", nil)
	_, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	principal, _ := session["principal"].(map[string]any)
	account, _ := principal["account_id"].(string)
	project, err := f.st.CreateProject(t.Context(), projectInput(account))
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"project_id": project.ID, "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.premium", "pricing_version": "pricing-2026-09",
		"payment_source": "order_service",
	}
	if st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body); st != http.StatusConflict || errCode(got) != "order_funds" {
		t.Fatalf("订单服务款不能扣图像费: %d %v", st, got)
	}
	body["payment_source"] = "billing_account"
	if st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body); st != http.StatusConflict || errCode(got) != "entitlement_missing" {
		t.Fatalf("有现金也不能开通未授权能力: %d %v", st, got)
	}
	body["capability"] = "image.generate.standard"
	st, short := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body)
	if st != http.StatusPaymentRequired || short["generate_allowed"] != false || short["charged"] != nil {
		t.Fatalf("余额不足应只给充值入口: %d %v", st, short)
	}
	if short["presentation"] != "本次预计 ¥1.50" {
		t.Fatalf("余额不足仍要展示报价，不能编造成已扣: %v", short["presentation"])
	}
	handoff, _ := short["topup"].(map[string]any)
	if handoff["entry"] != "billing_center" || handoff["return_target"] != "ti-product-image-engine-web" || handoff["quote_ref"] == "" || handoff["generate_allowed"] != false {
		t.Fatalf("充值入口: %v", handoff)
	}
	before := rec.BalanceReads
	st, back := f.do(t, "POST", "/api/v1/billing/consume/return", tok, map[string]any{
		"project_id": project.ID, "page": "success",
	})
	if st != http.StatusOK || back["generate_allowed"] != false || back["reason"] != "requote_required" || back["charged"] != nil {
		t.Fatalf("充值成功页不能直接生成: %d %v", st, back)
	}
	if rec.BalanceReads <= before || back["balance_known"] != true {
		t.Fatalf("返回后只重读余额: reads %d→%d body=%v", before, rec.BalanceReads, back)
	}
	if rec.NetReserved() != 0 || rec.Settles != 0 {
		t.Fatalf("余额不足不能预占: reserved=%d settles=%d", rec.NetReserved(), rec.Settles)
	}
}

func TestConsumeUnknownLooksUpStoredTask(t *testing.T) {
	f := newFixture(t, nil)
	rec := &billassemble.Record{
		Included: true, Entitled: []string{"image.generate.standard"},
	}
	f.srv.Bills = rec
	f.rearm(t)
	tok := f.stub.mintExtra("jia@x.com", "product-image", nil)
	_, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	principal, _ := session["principal"].(map[string]any)
	account, _ := principal["account_id"].(string)
	project, err := f.st.CreateProject(t.Context(), projectInput(account))
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"project_id": project.ID, "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard", "pricing_version": "pricing-cheap",
	}
	if st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body); st != http.StatusOK || got["presentation"] != "已包含额度" || got["charged"] != nil {
		t.Fatalf("套餐额度: %d %v", st, got)
	}
	st, got := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "timeout", "replacement_key": "idem-new",
	})
	if st != http.StatusConflict || errCode(got) != "lookup_required" {
		t.Fatalf("超时且没有原任务应先查: %d %v", st, got)
	}
	st, got = f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown", "task_id": "task_client", "charge_ref": "chg_client",
	})
	if st != http.StatusConflict || got["task_id"] == "task_client" || got["charge_ref"] == "chg_client" {
		t.Fatalf("不能采用客户端带来的任务号或扣费号: %d %v", st, got)
	}
	saved, err := f.st.GetUsageQuoteByProject(t.Context(), account, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.TaskID = "task_1"
	if _, err := f.st.SaveUsageQuote(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	rec.TaskStatus = map[string]string{"task_1": "unknown"}
	st, got = f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown",
	})
	if st != http.StatusOK || got["task_id"] != "task_1" || got["resubmit"] != false || got["recharge"] != false || got["charged"] != nil || got["settlement"] != "unknown" {
		t.Fatalf("应查已保存的任务，且没有账单就是未知: %d %v", st, got)
	}
	if st, mismatch := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown", "task_id": "task_other",
	}); st != http.StatusConflict {
		t.Fatalf("不同的任务号不能另开一条: %d %v", st, mismatch)
	}
	if st, mismatch := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown", "replacement_key": "idem-new",
	}); st != http.StatusConflict {
		t.Fatalf("不能更换幂等键: %d %v", st, mismatch)
	}
	st, done := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown", "work_complete": true,
	})
	if st != http.StatusOK || done["settlement"] != "作品完成待核对" || done["billing_passed"] != false || done["regenerate"] != false || done["charged"] != nil || done["resubmit"] != false {
		t.Fatalf("作品完成但没有账单应待核对，不能重新生成: %d %v", st, done)
	}
	if done["billing_pass"] != "UNKNOWN" || done["production_authorized"] != "NOT_AUTHORIZED" {
		t.Fatalf("待核对不是计费通过: %v", done)
	}
	rec.ChargeByTask = map[string]string{"task_1": "chg_on_ticket"}
	st, referenced := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown", "work_complete": true,
	})
	if st != http.StatusOK || referenced["charge_ref"] != "chg_on_ticket" || referenced["settlement"] != "作品完成待核对" || referenced["billing_passed"] != false || referenced["regenerate"] != false || referenced["charged"] != nil {
		t.Fatalf("任务单费用引用可以返回，但不能写成已结算: %d %v", st, referenced)
	}
	if referenced["billing_pass"] != "UNKNOWN" || referenced["production_authorized"] != "NOT_AUTHORIZED" {
		t.Fatalf("费用引用不是计费通过: %v", referenced)
	}
	st, open := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown",
	})
	if st != http.StatusOK || open["charge_ref"] != "chg_on_ticket" || open["settlement"] != "unknown" || open["billing_passed"] != false || open["charged"] != nil || open["billing_pass"] != "UNKNOWN" || open["production_authorized"] != "NOT_AUTHORIZED" {
		t.Fatalf("未完成时费用引用仍不是结算: %d %v", st, open)
	}
	if rec.NetReserved() != 0 || rec.Settles != 0 {
		t.Fatalf("查回不能扣费: reserved=%d settles=%d", rec.NetReserved(), rec.Settles)
	}
}

func TestConsumeMissingConfigDoesNotInventPrice(t *testing.T) {
	f := newFixture(t, nil)
	tok := f.stub.mintExtra("jia@x.com", "product-image", nil)
	_, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	principal, _ := session["principal"].(map[string]any)
	account, _ := principal["account_id"].(string)
	project, err := f.st.CreateProject(t.Context(), projectInput(account))
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"project_id": project.ID, "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard",
	}
	st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body)
	if st != http.StatusOK || got["connection"] != "配置缺失" || got["presentation"] != "配置缺失" || got["quoted"] != false || got["charged"] != nil || got["generate_allowed"] != false || got["settlement"] != "unknown" {
		t.Fatalf("没配计费要说明配置缺失，不能编报价: %d %v", st, got)
	}
	if containsAny(mapString(got), "bill-tok", "127.0.0.1") {
		t.Fatalf("应答不能带出令牌或地址: %v", got)
	}
}

func TestConsumeConfiguredWithoutPortIsUnimplemented(t *testing.T) {
	f := newFixture(t, func(cfg *config.Config) { cfg.BillingEnabled = true })
	tok := f.stub.mintExtra("jia@x.com", "product-image", nil)
	_, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	principal, _ := session["principal"].(map[string]any)
	account, _ := principal["account_id"].(string)
	project, err := f.st.CreateProject(t.Context(), projectInput(account))
	if err != nil {
		t.Fatal(err)
	}
	st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, map[string]any{
		"project_id": project.ID, "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard",
	})
	if st != http.StatusOK || got["connection"] != "未实现" || got["presentation"] != "未实现" || got["quoted"] != false || got["charged"] != nil || got["generate_allowed"] != false {
		t.Fatalf("配了但没装端口应报未实现: %d %v", st, got)
	}
}

func TestConsumeUnavailableDoesNotInventPrice(t *testing.T) {
	f := newFixture(t, func(cfg *config.Config) { cfg.BillingEnabled = true })
	f.srv.Bills = unavailableBills{}
	f.rearm(t)
	tok := f.stub.mintExtra("jia@x.com", "product-image", nil)
	_, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	principal, _ := session["principal"].(map[string]any)
	account, _ := principal["account_id"].(string)
	project, err := f.st.CreateProject(t.Context(), projectInput(account))
	if err != nil {
		t.Fatal(err)
	}
	st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, map[string]any{
		"project_id": project.ID, "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard",
	})
	if st != http.StatusServiceUnavailable || errCode(got) != "billing_unavailable" || !strings.Contains(errMsg(got), "计费服务不可达") {
		t.Fatalf("平台不可达不能编报价: %d %v", st, got)
	}
	if containsAny(mapString(got), "本次预计", "bill-tok") {
		t.Fatalf("不可达应答不能带价或令牌: %v", got)
	}
}

type unavailableBills struct{}

func (unavailableBills) Quote(context.Context, billassemble.QuoteCall) (billassemble.QuoteFact, error) {
	return billassemble.QuoteFact{}, billassemble.ErrUnavailable
}
func (unavailableBills) Hold(context.Context, billassemble.HoldCall) (billassemble.HoldFact, error) {
	return billassemble.HoldFact{}, billassemble.ErrFundsNotExecuted
}
func (unavailableBills) Release(context.Context, billassemble.ReleaseCall) (billassemble.ReleaseFact, error) {
	return billassemble.ReleaseFact{}, billassemble.ErrFundsNotExecuted
}
func (unavailableBills) Settle(context.Context, billassemble.SettleCall) (billassemble.SettleFact, error) {
	return billassemble.SettleFact{}, billassemble.ErrFundsNotExecuted
}
func (unavailableBills) Lookup(context.Context, billassemble.LookupCall) (billassemble.LookupFact, error) {
	return billassemble.LookupFact{Settlement: billassemble.SettlementUnknown}, nil
}
func (unavailableBills) ReadBalance(context.Context, string) (billassemble.Balance, error) {
	return billassemble.Balance{}, billassemble.ErrUnavailable
}

func projectInput(tenant string) store.Project {
	return store.Project{TenantID: tenant, Name: "报价工程", CreatedBy: "usr", SourceType: "standalone"}
}

func withPayer(body map[string]any, payer string) map[string]any {
	next := map[string]any{}
	for k, v := range body {
		next[k] = v
	}
	next["payer_account_ref"] = payer
	return next
}

func mapString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func containsAny(raw string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(raw, part) {
			return true
		}
	}
	return false
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}
