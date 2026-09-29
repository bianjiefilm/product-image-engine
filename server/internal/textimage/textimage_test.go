package textimage

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestOpenAllowsTextWithoutPhotoAndKeepsBillingPending(t *testing.T) {
	q, err := Open(OpenInput{
		TenantID: "ten", ProjectID: "prj", Prompt: "白色陶瓷杯，浅灰棚拍",
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.Prompt == "" || q.PhotoRequired || q.PhotoAssetID != "" {
		t.Fatalf("文字描述不应强制实物照片: %+v", q)
	}
	if q.BillingLabel != BillingPendingLabel || q.Charged || q.BillingPassed || q.ProductionAuthorized {
		t.Fatalf("计费未接通不能扣费或标通过: %+v", q)
	}
	if q.SubjectProtected || q.QuoteStatus != QuoteUnconfirmed {
		t.Fatalf("开单不能声称主体已保护: %+v", q)
	}
}

func TestEmptyPromptRejected(t *testing.T) {
	if _, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", Prompt: "  "}); err != ErrPromptRequired {
		t.Fatalf("空描述应拒绝, got %v", err)
	}
}

func TestClientAmountDoesNotBecomeACharge(t *testing.T) {
	q, err := Open(OpenInput{
		TenantID: "ten", ProjectID: "prj", Prompt: "玻璃杯",
		BillingConnected: false, QuotedAmount: "¥9.90",
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.BillingLabel != BillingPendingLabel || q.Charged || q.BillingPassed || q.ProductionAuthorized {
		t.Fatalf("客户端金额不能当成已扣费: %+v", q)
	}
}

func TestSubmitWithoutSupplierFailsWithoutImage(t *testing.T) {
	q, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", Prompt: "红色水壶"})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	rec, err := DecideSubmit(SubmitInput{Quote: q, GenerationUsable: false})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusFailed || rec.ShowImage || rec.CallSupplier || rec.SubjectProtected {
		t.Fatalf("没有供应商时应失败且不出现图: %+v", rec)
	}
	if rec.BillingPassed || rec.ProductionAuthorized || rec.Charged || rec.BillingLabel != BillingPendingLabel {
		t.Fatalf("失败不能写成计费或生产已通过: %+v", rec)
	}
}

func TestSameRequestStaysTheSameFailure(t *testing.T) {
	q, _ := Open(OpenInput{TenantID: "ten", ProjectID: "prj", Prompt: "红色水壶"})
	q.QuoteStatus = QuoteConfirmed
	rec, err := DecideSubmit(SubmitInput{
		Quote: q, GenerationUsable: true, SameFingerprint: true,
		ExistingID: "txt_1", ExistingStatus: StatusFailed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusFailed || !rec.Idempotent || rec.Regenerate || rec.CallSupplier || rec.ID != "txt_1" || rec.ShowImage {
		t.Fatalf("同一请求刷新后仍应是原失败记录: %+v", rec)
	}
	pending, err := DecideSubmit(SubmitInput{
		Quote: q, GenerationUsable: true, SameFingerprint: true,
		ExistingID: "txt_2", ExistingStatus: StatusUnknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != StatusUnknown || pending.ID != "txt_2" || pending.Regenerate || pending.ShowImage || pending.SubjectProtected {
		t.Fatalf("待确认记录刷新后不能改成成功或新任务: %+v", pending)
	}
}

func TestClientCannotClaimSubjectProtectedOrSuccess(t *testing.T) {
	rec := Record{ID: "txt_1", Status: StatusFailed, Quality: QualityUnknown, BillingLabel: BillingPendingLabel}
	if _, err := ApplyClientClaim(rec, "subject_protected"); err != ErrSubjectClaimForbidden {
		t.Fatalf("客户端不能声称主体已保护, got %v", err)
	}
	if _, err := ApplyClientClaim(rec, "pass"); err != ErrRelabelForbidden {
		t.Fatalf("客户端不能把失败改成通过, got %v", err)
	}
	if _, err := ApplyClientClaim(rec, "success"); err != ErrRelabelForbidden {
		t.Fatalf("客户端不能把失败改成成功, got %v", err)
	}
}

func TestFilledOutputIDIsNotAReceipt(t *testing.T) {
	rec := Record{ID: "txt_1", Status: StatusFailed, Quality: QualityUnknown, BillingLabel: BillingPendingLabel}
	next := NoteOutputID(rec, "filled-output")
	if next.Status != StatusFailed || next.ShowImage || next.SubjectProtected || next.OutputIsReceipt || next.BillingPassed || next.ProductionAuthorized {
		t.Fatalf("填上的输出编号不能核销失败记录: %+v", next)
	}
	unknown := Record{ID: "txt_2", Status: StatusUnknown, Quality: QualityUnknown, BillingLabel: BillingPendingLabel}
	noted := NoteOutputID(unknown, "filled-output")
	if noted.Status != StatusUnknown || noted.ShowImage || noted.OutputIsReceipt || noted.SubjectProtected {
		t.Fatalf("待确认记录不能靠输出编号变成成片: %+v", noted)
	}
}

func TestFingerprintChangesWithPrompt(t *testing.T) {
	a := Fingerprint("ten", "prj", "红色水壶")
	b := Fingerprint("ten", "prj", "蓝色水壶")
	if a == "" || a == b {
		t.Fatal("不同文字描述必须是不同请求")
	}
}

func trustedFixtureAdmit() AdmitInput {
	return AdmitInput{
		Status: StatusCompleted, JobTenant: "ten", JobProject: "prj",
		ActorTenant: "ten", ActorProject: "prj",
		InputVersion: "ver-1", AssetInputVersion: "ver-1",
		AssetID: "txfix_1", Registered: true,
		RegisteredSHA: strings.Repeat("ab", 32), FileSHA: strings.Repeat("ab", 32),
		Origin: OriginFixture, ResultVersion: "txfix_1",
	}
}

func TestAdmitFixtureWhenEveryCheckHolds(t *testing.T) {
	got := Admit(trustedFixtureAdmit())
	if !got.Show || got.Reason != "" {
		t.Fatalf("五项齐全的夹具应可展示: %+v", got)
	}
}

func TestAdmitRejectsUntrustedResults(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AdmitInput)
		reason string
	}{
		{"客户端自报", func(in *AdmitInput) { in.ClientSupplied = true }, "client_claim"},
		{"别的租户", func(in *AdmitInput) { in.ActorTenant = "other" }, "tenant_or_project"},
		{"别的工程", func(in *AdmitInput) { in.ActorProject = "other" }, "tenant_or_project"},
		{"尚未完成", func(in *AdmitInput) { in.Status = StatusFailed }, "not_completed"},
		{"输入版本不一致", func(in *AdmitInput) { in.AssetInputVersion = "ver-old" }, "input_version"},
		{"没有登记", func(in *AdmitInput) { in.Registered = false }, "asset_hash"},
		{"哈希不符", func(in *AdmitInput) { in.FileSHA = strings.Repeat("cd", 32) }, "asset_hash"},
		{"空资产", func(in *AdmitInput) { in.AssetID = "" }, "asset_hash"},
		{"不是夹具", func(in *AdmitInput) { in.Origin = "supplier" }, "not_fixture"},
		{"晚于选定版本", func(in *AdmitInput) {
			in.SelectedVersion = "txfix_kept"
			in.ResultVersion = "txfix_late"
		}, "selected_version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := trustedFixtureAdmit()
			tc.mutate(&in)
			got := Admit(in)
			if got.Show || got.Reason != tc.reason {
				t.Fatalf("应拒绝 %s: %+v", tc.reason, got)
			}
		})
	}
}

func TestAcceptServerTaskNeedsServerAssetAndBytes(t *testing.T) {
	trusted := AcceptServerTask(ServerTaskInput{
		Status: "succeeded", AssetID: "asset_real", ServerBytes: true,
	})
	if !trusted.Accept || trusted.Status != StatusCompleted || trusted.AssetID != "asset_real" || trusted.Reason != "" {
		t.Fatalf("服务端任务带真实资产和字节时应完成: %+v", trusted)
	}
	cases := []struct {
		name   string
		in     ServerTaskInput
		reason string
	}{
		{"没有资产", ServerTaskInput{Status: "succeeded"}, "no_server_asset"},
		{"只有凭证", ServerTaskInput{Status: "succeeded", CredentialConfigured: true}, "no_server_asset"},
		{"有资产没有字节", ServerTaskInput{Status: "completed", AssetID: "asset_real", CredentialConfigured: true}, "no_server_bytes"},
		{"缺凭证有字节仍可接受的反例不能靠客户端", ServerTaskInput{Status: "succeeded", AssetID: "filled-output", ServerBytes: true, ClientSupplied: true}, "client_claim"},
		{"客户端成功词", ServerTaskInput{Status: "success", AssetID: "asset_real", ServerBytes: true}, "pending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AcceptServerTask(tc.in)
			if got.Accept || got.Status == StatusCompleted || got.AssetID != "" || got.Reason != tc.reason {
				t.Fatalf("不应接受: %+v", got)
			}
		})
	}
	failed := AcceptServerTask(ServerTaskInput{Status: "failed", AssetID: "asset_real", ServerBytes: true})
	if failed.Accept || failed.Status != StatusFailed || failed.AssetID != "" {
		t.Fatalf("失败任务不能带着资产完成: %+v", failed)
	}
	bare := AcceptServerTask(ServerTaskInput{Status: "succeeded", CredentialConfigured: false})
	if bare.Accept || bare.Status != StatusUnknown {
		t.Fatalf("缺凭证不能本身变成成功: %+v", bare)
	}
}

func TestAdmitServerOriginWhenBytesMatch(t *testing.T) {
	in := trustedFixtureAdmit()
	in.Origin = OriginServer
	in.AssetID = "asset_real"
	in.ResultVersion = "asset_real"
	got := Admit(in)
	if !got.Show || got.Reason != "" {
		t.Fatalf("服务端登记且哈希一致应可展示: %+v", got)
	}
	in.SelectedVersion = "asset_kept"
	in.ResultVersion = "asset_late"
	late := Admit(in)
	if late.Show || late.Reason != "selected_version" {
		t.Fatalf("晚于选定版本的服务端结果不能展示: %+v", late)
	}
}

func TestLocalTestPaintIsImageNotAPlatformStatus(t *testing.T) {
	a, err := PaintLocalTest("红色水壶")
	if err != nil || len(a) < 8 {
		t.Fatal(err)
	}
	b, err := PaintLocalTest("蓝色水壶")
	if err != nil || bytes.Equal(a, b) {
		t.Fatalf("不同描述应得到不同的本地测试图: %v n=%d", err, len(b))
	}
	if _, err := png.Decode(bytes.NewReader(a)); err != nil {
		t.Fatalf("本地测试图必须是可解码 PNG: %v", err)
	}
	if ModelCallUsable("") || ModelCallUsable("secret-model-key") {
		t.Fatal("凭证字符串本身不是可调用的图像模型")
	}
}

func TestRealGenerationStaysIncomplete(t *testing.T) {
	if RealGenerationCompleted(true, OriginFixture) || RealGenerationCompleted(true, "supplier") || RealGenerationCompleted(false, "") {
		t.Fatal("本轮不能把任何图写成真实出图完成")
	}
	if RealGenerationIncomplete != "真实出图未完成" || !strings.Contains(ConceptualNotice, "不宣称主体保真") || !strings.Contains(FixtureNotice, "不是模型出图") {
		t.Fatal("文案必须标明夹具、概念输出和真实出图未完成")
	}
	if ProductionNotAuthorized != "NOT_AUTHORIZED" {
		t.Fatalf("生产授权应为 NOT_AUTHORIZED, got %s", ProductionNotAuthorized)
	}
}
