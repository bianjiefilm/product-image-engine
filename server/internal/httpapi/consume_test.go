package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

func TestConsumeQuoteConfirmsCNYAndHidesOtherMerchants(t *testing.T) {
	f := newFixture(t, nil)
	amount := int64(150)
	f.srv.Consume = &ConsumeAdvisor{
		Granted:     []string{"image.generate.standard"},
		AmountMinor: &amount,
	}
	f.rearm(t)
	tok := f.stub.mintExtra("jia@x.com", "product-image", jwt.MapClaims{
		"org_payers": []any{map[string]any{
			"tenant_id": "org_a", "account_ref": "acct_org_a", "display": "A商家付款",
		}},
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
		"capability": "image.generate.standard", "pricing_version": "pricing-2026-09",
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
	if body["payer_display"] != "个人付款" || body["presentation"] != "本次预计 ¥1.50" || body["charged"] != false || body["generate_allowed"] != false {
		t.Fatalf("个人报价: %v", body)
	}
	if body["production_authorized"] != "NOT_AUTHORIZED" || body["billing_pass"] != "UNKNOWN" {
		t.Fatalf("不能把计费标成生产通过: %v", body)
	}
	raw := mapString(body)
	if containsAny(raw, "B商家付款", "13800138000", "点数", "修点") {
		t.Fatalf("应答泄漏了不该出现的内容: %s", raw)
	}
	st, confirmed := f.do(t, "POST", "/api/v1/billing/consume/confirm", tok, quoteBody)
	if st != http.StatusOK || confirmed["generate_allowed"] != true || confirmed["charged"] != false {
		t.Fatalf("确认报价后仍不扣款: %d %v", st, confirmed)
	}
	quoteBody["image_count"] = 2
	st, changed := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, quoteBody)
	if st != http.StatusOK || changed["must_requote"] != true || changed["generate_allowed"] != false {
		t.Fatalf("改数量后必须重新确认: %d %v", st, changed)
	}

	orgProject, err := f.st.CreateProject(t.Context(), projectInput("org_a"))
	if err != nil {
		t.Fatal(err)
	}
	orgBody := map[string]any{
		"project_id": orgProject.ID, "source_type": "campaign", "source_ref": "camp_1",
		"source_tenant_id": "org_a", "image_count": 1, "resolution": "800x800",
		"capability": "image.generate.standard", "pricing_version": "pricing-2026-09",
		"books": quoteBody["books"],
	}
	st, org := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, orgBody)
	if st != http.StatusOK || org["payer_display"] != "A商家付款" {
		t.Fatalf("活动入口应显示已验证组织付款: %d %v", st, org)
	}
	if containsAny(mapString(org), "B商家付款") {
		t.Fatalf("不能看到别的商家: %v", org)
	}
	orgBody["source_tenant_id"] = "org_b"
	if st, denied := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, orgBody); st != http.StatusForbidden {
		t.Fatalf("未授权商家应拒绝: %d %v", st, denied)
	}
}

func TestConsumeRefusesCashWithoutEntitlementAndTopupShortcut(t *testing.T) {
	f := newFixture(t, nil)
	amount := int64(999900)
	f.srv.Consume = &ConsumeAdvisor{AmountMinor: &amount, FundsShort: true}
	f.rearm(t)
	tok := f.stub.mintExtra("jia@x.com", "product-image", nil)
	st, session := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
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
	if st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body); st != http.StatusConflict {
		t.Fatalf("订单服务款不能扣图像费: %d %v", st, got)
	}
	body["payment_source"] = "billing_account"
	st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body)
	if st != http.StatusConflict || got["code"] == "order_funds" {
		t.Fatalf("有现金也不能开通未授权能力: %d %v", st, got)
	}
	body["capability"] = "image.generate.standard"
	f.srv.Consume.Granted = []string{"image.generate.standard"}
	st, short := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body)
	if st != http.StatusPaymentRequired || short["generate_allowed"] != false {
		t.Fatalf("余额不足应只给充值入口: %d %v", st, short)
	}
	handoff, _ := short["topup"].(map[string]any)
	if handoff["entry"] != "billing_center" || handoff["return_target"] != "ti-product-image-engine-web" || handoff["quote_ref"] == "" {
		t.Fatalf("充值入口: %v", handoff)
	}
	st, back := f.do(t, "POST", "/api/v1/billing/consume/return", tok, map[string]any{
		"project_id": project.ID, "page": "success",
	})
	if st != http.StatusOK || back["generate_allowed"] != false || back["reason"] != "requote_required" {
		t.Fatalf("充值成功页不能直接生成: %d %v", st, back)
	}
}

func TestConsumeUnknownReusesOriginalTask(t *testing.T) {
	f := newFixture(t, nil)
	f.srv.Consume = &ConsumeAdvisor{Granted: []string{"image.generate.standard"}, IncludedAllowance: true}
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
		"capability": "image.generate.standard", "pricing_version": "pricing-2026-09",
	}
	if st, got := f.do(t, "POST", "/api/v1/billing/consume/quote", tok, body); st != http.StatusOK || got["presentation"] != "已包含额度" {
		t.Fatalf("套餐额度: %d %v", st, got)
	}
	st, got := f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "timeout", "replacement_key": "idem-new",
	})
	if st != http.StatusConflict {
		t.Fatalf("超时且没有原任务应先查: %d %v", st, got)
	}
	st, got = f.do(t, "POST", "/api/v1/billing/consume/recover", tok, map[string]any{
		"project_id": project.ID, "status": "unknown", "task_id": "task_1", "charge_ref": "chg_1",
	})
	if st != http.StatusOK || got["resubmit"] != false || got["recharge"] != false || got["task_id"] != "task_1" || got["charge_ref"] != "chg_1" {
		t.Fatalf("应沿用原任务和原扣费: %d %v", st, got)
	}
}

func projectInput(tenant string) store.Project {
	return store.Project{TenantID: tenant, Name: "报价工程", CreatedBy: "usr", SourceType: "standalone"}
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
