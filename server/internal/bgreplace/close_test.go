package bgreplace

import (
	"errors"
	"testing"
)

func TestFingerprintBindsModeScopeAndInputVersion(t *testing.T) {
	base := Fingerprint("acct_a", "proj_1", "pin_1", "v1", string(ModeFidelity), ProtectionScope, "室内")
	if base == "" {
		t.Fatal("指纹不能为空")
	}
	if base == Fingerprint("acct_a", "proj_1", "pin_1", "v1", string(ModeCreative), ProtectionScope, "室内") {
		t.Fatal("模式必须进入指纹")
	}
	if base == Fingerprint("acct_a", "proj_1", "pin_1", "v2", string(ModeFidelity), ProtectionScope, "室内") {
		t.Fatal("输入版本必须进入指纹")
	}
	if base == Fingerprint("acct_a", "proj_1", "pin_1", "v1", string(ModeFidelity), "subject", "室内") {
		t.Fatal("主体保护范围必须进入指纹")
	}
}

func TestSimilarityCannotMaskProductErrors(t *testing.T) {
	score := 0.99
	ex := Execution{Status: StatusCompleted, Mode: ModeFidelity, Quality: QualityUnknown}
	got, checks, err := InspectProduct(ex, ProductChecks{
		Logo: QualityPass, PackagingText: QualityPass, Spec: QualityPass, Structure: QualityFail,
		Similarity: &score,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeFidelity || got.Quality != QualityFail || got.Deliverable || got.CreativeFallback {
		t.Fatalf("结构失败不得被相似度写成可交付: %#v", got)
	}
	if got.ProductionGenerationPassed || got.BillingPassed {
		t.Fatal("检查不得宣称生产出图或计费已通过")
	}
	if !checks.HasFail() || !containsPending(got.Pending, "结构被改写") || !containsPending(got.Pending, similarityMaskNotice) {
		t.Fatalf("应保留结构错误且点明相似度不能掩盖: %#v", got.Pending)
	}
	if _, err := Select(got); err != ErrNotDeliverable {
		t.Fatalf("失败候选不能选定, got %v", err)
	}
}

func TestSimilarityOnlyStaysUnknown(t *testing.T) {
	score := 0.99
	got, _, err := InspectProduct(Execution{Mode: ModeFidelity, Quality: QualityUnknown}, ProductChecks{SimilarityOnly: true, Similarity: &score})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quality != QualityUnknown || got.Deliverable || got.ProductionGenerationPassed {
		t.Fatalf("只有相似度不得通过: %#v", got)
	}
	if !containsPending(got.Pending, similarityMaskNotice) {
		t.Fatalf("应说明相似度不能掩盖商品错误: %#v", got.Pending)
	}
}

func TestInspectCannotClearFailureOrFakeCompletion(t *testing.T) {
	got, _, err := InspectProduct(Execution{Mode: ModeFidelity, Quality: QualityFail}, ProductChecks{
		Logo: QualityPass, PackagingText: QualityPass, Spec: QualityPass, Structure: QualityPass,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quality != QualityFail || got.Deliverable || got.Mode != ModeFidelity {
		t.Fatalf("已失败的检查不能改成通过: %#v", got)
	}
	done, notice := RealGenerationState(RealGenerationInput{
		Credential: false, ModelRef: "model_x", TaskID: "task_x", AssetID: "asset_x",
		Quality: QualityPass, Checks: ProductChecks{Logo: QualityPass, PackagingText: QualityPass, Spec: QualityPass, Structure: QualityPass},
	})
	if done || notice != RealGenerationIncomplete {
		t.Fatalf("没有真实模型凭证不得完成出图: %v %q", done, notice)
	}
}

func TestRestoreKeepsOriginalTask(t *testing.T) {
	base := Execution{Status: StatusQueued, Mode: ModeFidelity, Quality: QualityUnknown}
	for _, kind := range []string{StatusUnknown, StatusFailed, StatusQuotaInsufficient} {
		got := Restore(base, kind)
		if got.Mode != ModeFidelity || got.Status != kind || got.Regenerate || got.CallPlatformTask || got.Deliverable || got.CreativeFallback {
			t.Fatalf("%s 应恢复原任务且不换模式: %#v", kind, got)
		}
	}
	failed := Restore(Execution{Status: StatusFailed, Mode: ModeFidelity, Quality: QualityFail}, StatusUnknown)
	if failed.Quality != QualityFail || failed.Mode != ModeFidelity || failed.Deliverable {
		t.Fatalf("失败结论不能在恢复时被清掉: %#v", failed)
	}
}

func TestClassifyQuotaAndModelFailure(t *testing.T) {
	if got := ClassifySubmitFailure(errors.New("task submit: status 402: {\"error\":\"insufficient_quota\"}")); got != StatusQuotaInsufficient {
		t.Fatalf("402 应为额度不足, got %s", got)
	}
	if got := ClassifySubmitFailure(errors.New("task submit: status 400: 额度不足")); got != StatusQuotaInsufficient {
		t.Fatalf("额度不足文案应识别, got %s", got)
	}
	if got := ClassifySubmitFailure(errors.New("task submit: status 400: {\"error\":\"model_failed\"}")); got != StatusFailed {
		t.Fatalf("模型失败应分开, got %s", got)
	}
	if got := ClassifySubmitFailure(errors.New("task submit: status 400: bad")); got != StatusUnknown {
		t.Fatalf("普通拒绝仍应未知, got %s", got)
	}
	if got := ClassifySubmitFailure(errors.New("platform: 平台服务不可达: task submit: status 502")); got != StatusUnknown {
		t.Fatalf("不可达应为未知, got %s", got)
	}
}

func TestProductGuardBlocksFidelityFailure(t *testing.T) {
	got := ApplyProductGuard(Execution{Status: StatusCompleted, Mode: ModeFidelity, Quality: QualityUnknown, Deliverable: true}, ProductChecks{}, true)
	if got.Quality != QualityFail || got.Deliverable || got.Mode != ModeFidelity {
		t.Fatalf("主体保真失败不能变成可交付: %#v", got)
	}
}

func containsPending(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
