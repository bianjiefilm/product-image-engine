package billconsume

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandaloneDefaultsToPersonalPayer(t *testing.T) {
	payer, err := ResolvePayer(Context{
		PersonalAccountRef: "acct_person",
		Phone:              "13800138000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payer.Kind != KindPersonal || payer.AccountRef != "acct_person" || payer.Display != "个人付款" {
		t.Fatalf("payer = %+v", payer)
	}
	if strings.Contains(payer.AccountRef, "13800138000") {
		t.Fatal("手机号不能当平台账号")
	}
}

func TestOrderEntryShowsVerifiedOrgAndHidesOtherMerchant(t *testing.T) {
	orgA := OrgPayer{TenantID: "org_a", AccountRef: "acct_org_a", Display: "A商家付款"}
	orgB := OrgPayer{TenantID: "org_b", AccountRef: "acct_org_b", Display: "B商家付款"}
	payer, err := ResolvePayer(Context{
		SourceType:         "campaign",
		SourceRef:          "camp_1",
		PersonalAccountRef: "acct_person",
		VerifiedOrg:        &orgA,
		AuthorizedOrgs:     []OrgPayer{orgA},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payer.Kind != KindOrganization || payer.Display != "A商家付款" || payer.AccountRef != "acct_org_a" {
		t.Fatalf("payer = %+v", payer)
	}
	visible := VisibleBooks(payer, []Book{
		{AccountRef: "acct_org_a", Label: "A商家付款"},
		{AccountRef: "acct_org_b", Label: "B商家付款"},
	})
	if len(visible) != 1 || visible[0].AccountRef != "acct_org_a" {
		t.Fatalf("visible = %+v", visible)
	}
	if _, err := ResolvePayer(Context{
		SourceType:     "order",
		SourceRef:      "ord_1",
		VerifiedOrg:    &orgB,
		AuthorizedOrgs: []OrgPayer{orgA},
	}); err != ErrPayerNotAuthorized {
		t.Fatalf("未授权组织应拒绝, got %v", err)
	}
}

func TestUsageFactReportsOnlyProductFacts(t *testing.T) {
	fact, err := NewUsageFact(2, "1024x1024", "image.generate.standard", "pricing-2026-09")
	if err != nil {
		t.Fatal(err)
	}
	body := fact.Report("acct_person", "idem-1")
	for _, key := range []string{"image_count", "resolution", "capability", "pricing_version"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("缺少 %s: %+v", key, body)
		}
	}
	if body["image_count"] != 2 || body["quantity"] != int64(2) {
		t.Fatalf("张数未上报: %+v", body)
	}
	raw := strings.ToLower(mustJSON(body))
	for _, banned := range []string{"image_coin", "修点", "点数", "wallet", "balance_cny", "13800138000"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("用量事实含禁止字段 %s: %s", banned, raw)
		}
	}
	if _, err := NewUsageFact(0, "1024x1024", "image.generate.standard", "pricing-2026-09"); err == nil {
		t.Fatal("张数必须大于 0")
	}
}

func TestPresentationUsesCNYOrAllowance(t *testing.T) {
	amount := int64(150)
	label, err := Present(QuoteView{AmountMinor: &amount, Currency: "CNY"})
	if err != nil || label != "本次预计 ¥1.50" {
		t.Fatalf("label=%q err=%v", label, err)
	}
	included, err := Present(QuoteView{IncludedAllowance: true})
	if err != nil || included != "已包含额度" {
		t.Fatalf("included=%q err=%v", included, err)
	}
	if _, err := Present(QuoteView{PrivatePoints: "120点数"}); err != ErrPrivatePoints {
		t.Fatalf("私有点数应拒绝, got %v", err)
	}
	pending, err := Present(QuoteView{})
	if err != nil || pending != "待确认" {
		t.Fatalf("未报价应待确认, got %q %v", pending, err)
	}
}

func TestCashDoesNotGrantMissingCapability(t *testing.T) {
	err := Allow("image.generate.premium", false, 9_999_00)
	if err != ErrEntitlementMissing {
		t.Fatalf("got %v", err)
	}
	if err := Allow("image.generate.standard", true, 0); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteMustBeReconfirmedAfterChange(t *testing.T) {
	payer := Payer{Kind: KindPersonal, AccountRef: "acct_person", Display: "个人付款"}
	fact, err := NewUsageFact(1, "800x800", "image.generate.standard", "pricing-2026-09")
	if err != nil {
		t.Fatal(err)
	}
	fp := Fingerprint(payer, fact)
	confirmed := Confirm(fp, fp)
	if !confirmed.GenerateAllowed || confirmed.MustRequote {
		t.Fatalf("同指纹应可确认: %+v", confirmed)
	}
	changed, err := NewUsageFact(2, "800x800", "image.generate.standard", "pricing-2026-09")
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []UsageFact{
		changed,
		mustFact(t, 1, "1024x1024", "image.generate.standard", "pricing-2026-09"),
		mustFact(t, 1, "800x800", "image.generate.premium", "pricing-2026-09"),
	} {
		got := Confirm(fp, Fingerprint(payer, next))
		if got.GenerateAllowed || !got.MustRequote {
			t.Fatalf("参数变化后仍可生成: %+v", got)
		}
	}
	org := Payer{Kind: KindOrganization, AccountRef: "acct_org_a", Display: "A商家付款"}
	switched := Confirm(fp, Fingerprint(org, fact))
	if switched.GenerateAllowed || !switched.MustRequote {
		t.Fatalf("切换付款主体后仍可生成: %+v", switched)
	}
	sameModel := fact
	sameModel.Model = fact.Capability
	if Fingerprint(payer, fact) != Fingerprint(payer, sameModel) {
		t.Fatal("缺省模型应等于能力，不能无故使报价失效")
	}
	otherModel := fact
	otherModel.Model = "painter.premium"
	if got := Confirm(fp, Fingerprint(payer, otherModel)); got.GenerateAllowed || !got.MustRequote {
		t.Fatalf("换模型后仍可生成: %+v", got)
	}
	otherSize := fact
	otherSize.Size = "640x640"
	if got := Confirm(fp, Fingerprint(payer, otherSize)); got.GenerateAllowed || !got.MustRequote {
		t.Fatalf("换尺寸后仍可生成: %+v", got)
	}
}

func TestInsufficientBalanceOnlyHandsOffTopup(t *testing.T) {
	hand, err := TopupHandoff(TopupInput{
		QuoteRef:     "qte_1",
		ReturnTarget: "ti-product-image-engine-web",
		Allowed:      []string{"ti-product-image-engine-web"},
		FundsShort:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hand.Entry != "billing_center" || hand.ReturnTarget != "ti-product-image-engine-web" || hand.QuoteRef != "qte_1" || hand.GenerateAllowed {
		t.Fatalf("handoff = %+v", hand)
	}
	if _, err := TopupHandoff(TopupInput{
		QuoteRef: "qte_1", ReturnTarget: "https://evil.example/back", Allowed: []string{"ti-product-image-engine-web"}, FundsShort: true,
	}); err != ErrReturnTarget {
		t.Fatalf("自由 URL 应拒绝, got %v", err)
	}
	after := AfterTopupPage("success", false)
	if after.GenerateAllowed || after.Reason != "requote_required" {
		t.Fatalf("充值成功页不能直接生成: %+v", after)
	}
}

func TestUnknownLooksUpOriginalTaskAndCharge(t *testing.T) {
	got, err := Recover(Attempt{
		Status: "timeout", TaskID: "task_1", ChargeRef: "chg_1", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Resubmit || got.Recharge || got.TaskID != "task_1" || got.ChargeRef != "chg_1" || got.IdempotencyKey != "idem-1" {
		t.Fatalf("recover = %+v", got)
	}
	if _, err := Recover(Attempt{Status: "unknown", IdempotencyKey: "idem-new"}); err != ErrLookupRequired {
		t.Fatalf("未知且无原任务应先查, got %v", err)
	}
	if _, err := Recover(Attempt{
		Status: "unknown", TaskID: "task_1", ChargeRef: "chg_1", IdempotencyKey: "idem-1", ReplacementKey: "idem-2",
	}); err != ErrIdempotencyChanged {
		t.Fatalf("换键应拒绝, got %v", err)
	}
	for _, status := range []string{"lost", "lost_response", "retry"} {
		got, err := Recover(Attempt{Status: status, TaskID: "task_1", IdempotencyKey: "idem-1"})
		if err != nil || got.Resubmit || got.Recharge || got.TaskID != "task_1" || got.ChargeRef != "" {
			t.Fatalf("%s 应挂回原任务且不新扣费: %+v %v", status, got, err)
		}
	}
}

func TestOrderServiceFundsAreNotImagePayer(t *testing.T) {
	if err := RejectPaymentSource("order_service"); err != ErrOrderFunds {
		t.Fatalf("got %v", err)
	}
	if err := RejectPaymentSource("billing_account"); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionLevelsStayUnproven(t *testing.T) {
	levels := Levels()
	if levels.ImplementationReady != "PASS" || levels.Integration != "UNKNOWN" || levels.Billing != "UNKNOWN" {
		t.Fatalf("levels = %+v", levels)
	}
	if levels.ChannelBilling != "NOT_APPLICABLE" || levels.ProductionAuthorized != "NOT_AUTHORIZED" {
		t.Fatalf("levels = %+v", levels)
	}
	if AuthorizeProduction(false, "") != "NOT_AUTHORIZED" || AuthorizeProduction(true, "") != "NOT_AUTHORIZED" {
		t.Fatal("没有证据不能授权生产")
	}
	if AuthorizeProduction(true, "local-fake") != "NOT_AUTHORIZED" {
		t.Fatal("本票不接受本地假证据")
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func mustFact(t *testing.T, count int, resolution, capability, version string) UsageFact {
	t.Helper()
	fact, err := NewUsageFact(count, resolution, capability, version)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}
