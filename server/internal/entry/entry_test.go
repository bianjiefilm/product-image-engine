package entry

import (
	"errors"
	"testing"
)

func TestIndependentEntryUsesPersonalDefaultWithoutOrgOrOrder(t *testing.T) {
	ws, err := ResolveWorkspace(ResolveInput{
		AccountID:   "acct_1",
		Memberships: []string{"org_a", "org_b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != ScopePersonal || ws.Scope != PersonalDefault || ws.ForcedOrgSelection {
		t.Fatalf("独立入口应进 personal/default 且不强制选组织: %+v", ws)
	}
	if ws.StorageTenant != "personal/default:acct_1" {
		t.Fatalf("个人空间应按账号隔离: %s", ws.StorageTenant)
	}
	if ws.RequiresOrder {
		t.Fatal("个人使用不应要求订单")
	}
}

func TestSourceEntryStaysOnVerifiedOrg(t *testing.T) {
	ws, err := ResolveWorkspace(ResolveInput{
		AccountID:       "acct_1",
		SessionTenantID: "acct_1",
		Memberships:     []string{"org_customer", "org_other"},
		SourceType:      "order",
		SourceRef:       "ord_9",
		SourceTenantID:  "org_customer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != ScopeOrganization || ws.Scope != "org_customer" || ws.StorageTenant != "org_customer" {
		t.Fatalf("订单来源应留在已授权组织: %+v", ws)
	}
	if err := PlaceAssets(ws, []AuthorizedAsset{{Ref: "asset_9", OwnerTenant: "org_customer"}}); err != nil {
		t.Fatal(err)
	}
}

func TestCustomerAssetCannotLandInPersonalSpace(t *testing.T) {
	personal, err := ResolveWorkspace(ResolveInput{
		AccountID:   "acct_1",
		Memberships: []string{"org_customer", "org_other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = PlaceAssets(personal, []AuthorizedAsset{{Ref: "asset_9", OwnerTenant: "org_customer"}})
	if !errors.Is(err, ErrCustomerAssetInPersonal) {
		t.Fatalf("客户资产落到个人空间应拒绝, got %v", err)
	}
}

func TestSourceWithoutVerifiedTenantDoesNotFallBackToPersonal(t *testing.T) {
	_, err := ResolveWorkspace(ResolveInput{
		AccountID:   "acct_1",
		Memberships: []string{"org_customer"},
		SourceType:  "campaign",
		SourceRef:   "cmp_1",
	})
	if !errors.Is(err, ErrSourceTenantRequired) {
		t.Fatalf("缺少已验证来源租户时不能改落到个人空间, got %v", err)
	}
}

func TestActiveEnterpriseChoiceUsesChosenOrg(t *testing.T) {
	ws, err := ResolveWorkspace(ResolveInput{
		AccountID:        "acct_1",
		Memberships:      []string{"org_a", "org_b"},
		EnterpriseChosen: true,
		ChosenTenantID:   "org_b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != ScopeOrganization || ws.Scope != "org_b" || ws.ForcedOrgSelection {
		t.Fatalf("主动选择企业额度才进入该组织: %+v", ws)
	}
}

func TestUnrelatedTenantCannotReceiveSourceAssets(t *testing.T) {
	_, err := ResolveWorkspace(ResolveInput{
		AccountID:      "acct_1",
		Memberships:    []string{"org_other"},
		SourceType:     "order",
		SourceRef:      "ord_9",
		SourceTenantID: "org_customer",
	})
	if !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("未授权组织应拒绝, got %v", err)
	}
}

func TestGenerationDisabledDoesNotRegisterFakeImage(t *testing.T) {
	d := Decide(DecideInput{
		GenerationEnabled: false,
		BillingEnabled:    false,
		Fingerprint:       "fp-1",
	})
	if d.Status != StatusFailed || d.DegradedReason != "generation_unavailable" {
		t.Fatalf("未开生成应失败降级: %+v", d)
	}
	if d.RegisterOutput || d.OutputAssetID != "" || d.CallPlatform || d.Resubmit || d.Charged {
		t.Fatalf("不能登记假图或扣费: %+v", d)
	}
	if d.QuoteLabel != QuotePending || d.UsefulProven || d.SLAPromised {
		t.Fatalf("计费未接通应待确认，且不能宣称可用成片: %+v", d)
	}
}

func TestRefreshKeepsTheSameRunningTask(t *testing.T) {
	d := Decide(DecideInput{
		GenerationEnabled:   true,
		BillingEnabled:      false,
		Fingerprint:         "fp-1",
		ExistingTaskID:      "run_1",
		ExistingFingerprint: "fp-1",
		ExistingStatus:      StatusRunning,
	})
	if !d.SameTask || d.Resubmit || d.TaskID != "run_1" || d.Status != StatusRunning {
		t.Fatalf("刷新应恢复同一任务: %+v", d)
	}
	if d.RegisterOutput || d.Charged || d.QuoteLabel != QuotePending {
		t.Fatalf("未完成任务不能有成片或扣费: %+v", d)
	}
}

func TestUnreachableSecondLookDoesNotSubmitAgain(t *testing.T) {
	reachable := false
	first := Decide(DecideInput{
		GenerationEnabled: true,
		Fingerprint:       "fp-1",
		UpstreamReachable: &reachable,
	})
	if first.Status != StatusUnknown || first.RegisterOutput || first.Resubmit {
		t.Fatalf("任务服务不可达应为 unknown 且不造图: %+v", first)
	}
	again := Decide(DecideInput{
		GenerationEnabled:   true,
		Fingerprint:         "fp-1",
		ExistingTaskID:      "run_1",
		ExistingFingerprint: "fp-1",
		ExistingStatus:      StatusUnknown,
		UpstreamReachable:   &reachable,
	})
	if !again.SameTask || again.Status != StatusUnknown || again.Resubmit || again.CallPlatform {
		t.Fatalf("再次进入不能重复提交: %+v", again)
	}
}

func TestCompletedWithoutAssetIsNotAFinishedImage(t *testing.T) {
	reachable := true
	d := Decide(DecideInput{
		GenerationEnabled: true,
		BillingEnabled:    false,
		Fingerprint:       "fp-1",
		UpstreamReachable: &reachable,
		UpstreamStatus:    "succeeded",
	})
	if d.Status != StatusUnknown || d.RegisterOutput || d.OutputAssetID != "" || d.UsefulProven {
		t.Fatalf("没有真实资产不能完成: %+v", d)
	}
}

func TestUpstreamStatusesAndRealAssetStayUnproven(t *testing.T) {
	reachable := true
	cases := []struct {
		upstream string
		asset    string
		want     string
		register bool
	}{
		{"queued", "", StatusQueued, false},
		{"pending", "", StatusQueued, false},
		{"running", "", StatusRunning, false},
		{"failed", "", StatusFailed, false},
		{"completed", "asset_real", StatusCompleted, true},
		{"succeeded", "asset_real", StatusCompleted, true},
		{"mystery", "", StatusUnknown, false},
	}
	for _, tc := range cases {
		d := Decide(DecideInput{
			GenerationEnabled: true,
			BillingEnabled:    false,
			Fingerprint:       "fp-" + tc.upstream,
			UpstreamReachable: &reachable,
			UpstreamStatus:    tc.upstream,
			UpstreamAssetID:   tc.asset,
		})
		if d.Status != tc.want || d.RegisterOutput != tc.register || d.OutputAssetID != tc.asset && tc.register {
			t.Fatalf("%s → %+v", tc.upstream, d)
		}
		if d.UsefulProven || d.SLAPromised || d.Charged || d.QuoteLabel != QuotePending {
			t.Fatalf("真实资产也不能当成可售证明: %+v", d)
		}
	}
}

func TestSourcePathRepeatsFewerInputs(t *testing.T) {
	solo := Measure(PathFacts{
		UploadedNew: true, FilledScene: true, FilledSize: true,
		FilledBackground: true, ConfirmedQuote: true, Submitted: true,
	})
	sourced := Measure(PathFacts{
		HasAuthorizedAssets: true, FilledScene: true, FilledSize: true,
		FilledBackground: true, ConfirmedQuote: true, Submitted: true,
	})
	if solo.InputCount != 1 || sourced.InputCount != 0 || sourced.CrossProductReuploads != 0 {
		t.Fatalf("来源路径不应重复上传: solo=%+v sourced=%+v", solo, sourced)
	}
	if sourced.Steps >= solo.Steps || solo.SLAPromised || sourced.SLAPromised {
		t.Fatalf("来源步骤应更少且不承诺 SLA: solo=%+v sourced=%+v", solo, sourced)
	}
	reupload := Measure(PathFacts{HasAuthorizedAssets: true, UploadedNew: true, Submitted: true})
	if reupload.CrossProductReuploads != 1 {
		t.Fatalf("已授权仍重新上传应记一次: %+v", reupload)
	}
}

func TestRecoveryIncrementsWithoutNewSLA(t *testing.T) {
	j := Measure(PathFacts{HasAuthorizedAssets: true, Submitted: true, Recovered: true})
	if j.Recoveries != 1 || j.SLAPromised {
		t.Fatalf("中断恢复应计数且不承诺 SLA: %+v", j)
	}
}

func TestFirstScreenIsStartNotCatalog(t *testing.T) {
	screen := FirstScreen()
	if screen.Title != "开始做产品图" || screen.ShowsCatalog {
		t.Fatalf("首屏应是开始做产品图: %+v", screen)
	}
	if len(screen.Steps) < 4 {
		t.Fatalf("首屏应带出制作步骤: %+v", screen)
	}
}

func TestReturnHrefStaysExact(t *testing.T) {
	href, err := ExactReturn("order", "https://order.example/orders/ord_9")
	if err != nil || href != "https://order.example/orders/ord_9" {
		t.Fatalf("返回地址应原样保留: %q %v", href, err)
	}
	if _, err := ExactReturn("campaign", "https://campaign.example/c/cmp_1?stage=design"); err != nil {
		t.Fatal(err)
	}
	if _, err := ExactReturn("standalone", "https://order.example/orders/ord_9"); err == nil {
		t.Fatal("独立工程没有来源返回")
	}
	if _, err := ExactReturn("order", "javascript:alert(1)"); err == nil {
		t.Fatal("非法返回地址应拒绝")
	}
}
