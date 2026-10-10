package lightscene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
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
	unauthorized := fidelity.Report{Verdict: fidelity.VerdictPass, ExactProduct: true, Deliverable: true}
	gated := SubjectGate(ex, []fidelity.Report{unauthorized})
	if gated.VerifiedProduct || gated.Deliverable || gated.Quality == QualityPass {
		t.Fatalf("未授权的通过结论不能核实成片: %+v", gated)
	}
	withReceipt := ex
	withReceipt.VendorReceiptID = "vr_1"
	stillFilled := SubjectGate(withReceipt, []fidelity.Report{unauthorized})
	if stillFilled.VerifiedProduct || stillFilled.Quality == QualityPass {
		t.Fatalf("填好的输出编号加上回执,没有真实授权仍不能核实: %+v", stillFilled)
	}
	authorized := unauthorized
	authorized.RealGeneration = fidelity.RealGenerationAuthorized
	ok := SubjectGate(withReceipt, []fidelity.Report{authorized})
	if !ok.VerifiedProduct || ok.ProductionGenerationPassed || ok.BillingPassed {
		t.Fatalf("有回执且真实授权通过时仍不能宣称生产出图或计费已通过: %+v", ok)
	}
	failed := withReceipt
	failed.Status = StatusFailed
	failed.Quality = QualityPass
	relabel := SubjectGate(failed, []fidelity.Report{authorized})
	if relabel.VerifiedProduct || relabel.Quality == QualityPass || relabel.Status != StatusFailed {
		t.Fatalf("失败记录不能改成已验证产品图: %+v", relabel)
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
	}, []fidelity.Report{{
		Verdict: fidelity.VerdictPass, ExactProduct: true, Deliverable: true,
		RealGeneration: fidelity.RealGenerationAuthorized,
	}})
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

func TestNoCredentialDoesNotCallChargeForgeOrOwnOtherModes(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "ten", ProjectID: "prj", InputID: "in", InputVersion: "v1",
		LightingIntent: "好看纯色背景占位夹具",
	})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	ex, err := DecideSubmit(SubmitInput{
		Quote: q, GenerationUsable: true, CredentialConfigured: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ex.CallPlatformTask || ex.Status != StatusFailed || ex.Evidence == EvidencePlatformTask {
		t.Fatalf("没有光影凭证不得提交平台任务: %+v", ex)
	}
	if ex.BillingPassed || ex.ProductionGenerationPassed || ex.VerifiedProduct || ex.Deliverable {
		t.Fatalf("没有凭证不得扣费或写成通过: %+v", ex)
	}
	if ex.OutputAssetID != "" || ex.OutputVersion != "" || ex.VendorReceiptID != "" || ex.Selection == "selected" {
		t.Fatalf("夹具、纯色、占位或好看背景不得写成模型结果: %+v", ex)
	}
	if q.BillingLabel != BillingPendingLabel {
		t.Fatalf("没有账单金额不得写成已扣费: %q", q.BillingLabel)
	}
	for _, status := range []string{StatusFailed, StatusUnknown} {
		kept := KeepSelectedVersion(Execution{
			Selection: "selected", OutputVersion: "ver_user", OutputAssetID: "asset_user",
			Status: StatusQueued, Mode: ModeFidelity,
		}, status)
		if kept.OutputVersion != "ver_user" || kept.OutputAssetID != "asset_user" || kept.Selection != "selected" {
			t.Fatalf("%s 覆盖了已选定版本: %+v", status, kept)
		}
		if kept.VerifiedProduct || kept.Deliverable || kept.BillingPassed || kept.ProductionGenerationPassed {
			t.Fatalf("%s 把选定版本写成已通过: %+v", status, kept)
		}
	}
	masked := SubjectGate(Execution{
		Status: StatusUnknown, Mode: ModeFidelity, VendorReceiptID: "vr_aesthetic",
		OutputAssetID: "pretty-bg", OutputVersion: "v-pretty",
	}, []fidelity.Report{{
		Verdict: fidelity.VerdictPass, ExactProduct: true, Deliverable: true,
		RealGeneration: fidelity.RealGenerationAuthorized,
		Checks: []fidelity.Check{
			{Name: "aesthetic", Result: "0.99"},
			{Name: "logo", Result: fidelity.ResultFail},
			{Name: "packaging_text", Result: fidelity.ResultFail},
			{Name: "spec", Result: fidelity.ResultFail},
			{Name: "structure", Result: fidelity.ResultFail},
		},
	}})
	if masked.VerifiedProduct || masked.Deliverable || masked.Quality == QualityPass || masked.BillingPassed {
		t.Fatalf("美观分不能掩盖 Logo、包装、规格或结构: %+v", masked)
	}
	for _, sample := range AcceptanceSamples() {
		if sample.Quality == QualityPass || sample.ModelUsage == "模型" || sample.CostLabel != BillingPendingLabel {
			t.Fatalf("结构样本被写成模型结果: %+v", sample)
		}
	}
	assertLightPackageDoesNotOwnOtherModes(t)
}

func assertLightPackageDoesNotOwnOtherModes(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			switch path {
			case "net/http", "image", "image/png", "image/jpeg", "image/gif":
				t.Fatalf("光影包不应通过 %s 画图或注册路由", path)
			}
			if strings.Contains(path, "bgreplace") || strings.Contains(path, "sourceimage") || strings.Contains(path, "campaign") {
				t.Fatalf("光影包不应挡住其他模式: import %s", path)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.TypeSpec:
				switch n.Name.Name {
				case "Upload", "PhotoUpload", "CompareRoute", "BackgroundReplace", "BgReplace", "SourceImage":
					t.Fatalf("光影包不应声明挡住其他模式的类型 %s", n.Name.Name)
				}
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(n.Value)
				if err != nil {
					return true
				}
				for _, route := range []string{
					"/api/v1/photos",
					"/api/v1/uploads",
					"/api/v1/background-replacements",
					"/api/v1/compare",
					"/compare",
				} {
					if strings.Contains(text, route) {
						t.Fatalf("光影包不应注册其他模式路由 %s", text)
					}
				}
			}
			return true
		})
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
