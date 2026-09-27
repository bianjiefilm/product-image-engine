package lightscene

import (
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
)

func TestOpenQuoteDefaultsFidelityAndBillingPending(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "ten", ProjectID: "prj", InputID: "in", InputVersion: "v1",
		LightingIntent: "侧光棚拍",
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.Mode != ModeFidelity || q.ProtectedRegion != "subject" || q.BillingLabel != BillingPendingLabel {
		t.Fatalf("默认报价不诚实: %+v", q)
	}
	if q.QuoteStatus != QuoteUnconfirmed || q.ProductionGenerationPassed || q.BillingPassed {
		t.Fatalf("未确认报价不能宣称已通过: %+v", q)
	}
}

func TestCreativeWithoutPathDoesNotFallback(t *testing.T) {
	_, err := OpenQuote(QuoteInput{
		TenantID: "ten", ProjectID: "prj", InputID: "in", InputVersion: "v1",
		Mode: string(ModeCreative), LightingIntent: "戏剧光",
	})
	if err != ErrCreativeUnavailable {
		t.Fatalf("未接通创意路径应拒绝, got %v", err)
	}
}

func TestFingerprintChangesWithModeInputAndIntent(t *testing.T) {
	base := Fingerprint("ten", "prj", "in", "v1", string(ModeFidelity), "subject", "侧光")
	if base == Fingerprint("ten", "prj", "in", "v2", string(ModeFidelity), "subject", "侧光") {
		t.Fatal("改输入版本必须改变指纹")
	}
	if base == Fingerprint("ten", "prj", "in", "v1", string(ModeCreative), "subject", "侧光") {
		t.Fatal("改模式必须改变指纹")
	}
	if base == Fingerprint("ten", "prj", "in", "v1", string(ModeFidelity), "subject", "逆光") {
		t.Fatal("改光影意图必须改变指纹")
	}
}

func TestInvalidateUnconfirmedQuoteWhenInputChanges(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "ten", ProjectID: "prj", InputID: "in", InputVersion: "v1",
		LightingIntent: "侧光",
	})
	if err != nil {
		t.Fatal(err)
	}
	next := InvalidateIfChanged(q, ModeFidelity, "v2", "侧光")
	if next.QuoteStatus != QuoteInvalid || !next.MustReconfirm {
		t.Fatalf("改输入应使报价失效: %+v", next)
	}
}

func TestSubmitWithoutVendorIsFailedNotVerified(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "ten", ProjectID: "prj", InputID: "in", InputVersion: "v1",
		LightingIntent: "侧光",
	})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	ex, err := DecideSubmit(SubmitInput{Quote: q, GenerationUsable: false})
	if err != nil {
		t.Fatal(err)
	}
	if ex.Status != StatusFailed || ex.CallPlatformTask || ex.VerifiedProduct || ex.Deliverable {
		t.Fatalf("没有供应商时应失败且不可验证: %+v", ex)
	}
	if ex.Quality != QualityUnknown || ex.ProductionGenerationPassed || ex.BillingPassed {
		t.Fatalf("失败不能写成已验证产品图: %+v", ex)
	}
}

func TestUnknownExistingJobDoesNotRegenerate(t *testing.T) {
	q, _ := OpenQuote(QuoteInput{
		TenantID: "ten", ProjectID: "prj", InputID: "in", InputVersion: "v1",
		LightingIntent: "侧光",
	})
	q.QuoteStatus = QuoteConfirmed
	ex, err := DecideSubmit(SubmitInput{
		Quote: q, GenerationUsable: true, SameFingerprint: true, ExistingStatus: StatusUnknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ex.Status != StatusUnknown || ex.CallPlatformTask || ex.Regenerate || !ex.Idempotent {
		t.Fatalf("待核对任务不得重生成: %+v", ex)
	}
}

func TestClientCannotMarkPassOrRelabelFailure(t *testing.T) {
	ex := Execution{Status: StatusFailed, Quality: QualityUnknown, Mode: ModeFidelity}
	if _, err := ApplyClientVerdict(ex, QualityPass); err == nil {
		t.Fatal("客户端不能自报通过")
	}
	failed, err := ApplyClientVerdict(ex, QualityFail)
	if err != nil || failed.Quality != QualityFail || failed.VerifiedProduct || failed.Deliverable {
		t.Fatalf("只能记下失败: %+v %v", failed, err)
	}
	if _, err := ApplyClientVerdict(failed, QualityUnknown); err == nil {
		t.Fatal("失败记录不能改回未知")
	}
	if _, err := ApplyClientVerdict(failed, QualityPass); err == nil {
		t.Fatal("失败记录不能改成已验证")
	}
}

func TestSubjectGateBlocksVerifiedProductWithoutFidelity(t *testing.T) {
	ex := Execution{Status: StatusUnknown, Quality: QualityUnknown, Mode: ModeFidelity, OutputAssetID: "filled-output"}
	gated := SubjectGate(ex, nil)
	if gated.VerifiedProduct || gated.Deliverable || gated.ProductionGenerationPassed {
		t.Fatalf("没有主体保护报告不能绕过: %+v", gated)
	}
	if !contains(gated.Pending, "主体保护待确认") {
		t.Fatalf("应提示主体保护待确认: %+v", gated.Pending)
	}

	failReport := fidelity.Report{Verdict: fidelity.VerdictFail, InputRef: "in"}
	failed := SubjectGate(ex, []fidelity.Report{failReport})
	if failed.Quality != QualityFail || failed.VerifiedProduct {
		t.Fatalf("主体保护失败必须挡住已验证产品图: %+v", failed)
	}

	unknownReport := fidelity.Report{Verdict: fidelity.VerdictUnknown, ExactProduct: false}
	pending := SubjectGate(ex, []fidelity.Report{unknownReport})
	if pending.VerifiedProduct || pending.Quality == QualityPass {
		t.Fatalf("待确认的主体保护不能当成通过: %+v", pending)
	}
}

func TestVendorReceiptDoesNotOverrideFidelityOrOutputID(t *testing.T) {
	ex := Execution{
		Status: StatusUnknown, Quality: QualityUnknown, Mode: ModeFidelity,
		OutputAssetID: "filled-output", VendorReceiptID: "",
	}
	exact := fidelity.Report{Verdict: fidelity.VerdictPass, ExactProduct: true, Deliverable: true}
	gated := SubjectGate(ex, []fidelity.Report{exact})
	if gated.VerifiedProduct || gated.Deliverable {
		t.Fatalf("填好的输出编号不能当供应商回执: %+v", gated)
	}
	withReceipt := ex
	withReceipt.VendorReceiptID = "vr_1"
	ok := SubjectGate(withReceipt, []fidelity.Report{exact})
	if !ok.VerifiedProduct || ok.ProductionGenerationPassed || ok.BillingPassed {
		t.Fatalf("有回执且主体通过时仍不能宣称生产出图或计费已通过: %+v", ok)
	}
}

func TestSelectAndExportDoNotStartGenerationOrVerifyFailure(t *testing.T) {
	failed := Execution{Status: StatusFailed, Quality: QualityFail, Mode: ModeFidelity}
	if _, err := Select(failed); err == nil {
		t.Fatal("失败不能选定为可交付版本")
	}
	unknown := Execution{Status: StatusUnknown, Quality: QualityUnknown, Mode: ModeFidelity, OutputAssetID: "asset"}
	selected, err := Select(SubjectGate(unknown, nil))
	if err == nil {
		t.Fatalf("没有主体保护不能选定: %+v", selected)
	}
	ready := SubjectGate(Execution{
		Status: StatusUnknown, Quality: QualityUnknown, Mode: ModeFidelity,
		VendorReceiptID: "vr_1", OutputAssetID: "asset", OutputVersion: "v-out",
	}, []fidelity.Report{{Verdict: fidelity.VerdictPass, ExactProduct: true, Deliverable: true}})
	picked, err := Select(ready)
	if err != nil || picked.Selection != "selected" || picked.ProductionGenerationPassed {
		t.Fatalf("选定不应宣称生产出图已通过: %+v %v", picked, err)
	}
	rec, err := Export(picked)
	if err != nil || rec.StartsGeneration || rec.OutputAssetID != "asset" || rec.OutputVersion != "v-out" {
		t.Fatalf("导出应复用同一版本且不重新生成: %+v %v", rec, err)
	}
}

func TestAcceptanceSamplesStayUnverified(t *testing.T) {
	samples := AcceptanceSamples()
	if len(samples) != 2 {
		t.Fatalf("应有两条结构样本, got %d", len(samples))
	}
	for _, s := range samples {
		if s.Quality != QualityUnknown || s.ModelUsage != "未调用" || s.CostLabel != BillingPendingLabel {
			t.Fatalf("样本不能冒充真实生成: %+v", s)
		}
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
