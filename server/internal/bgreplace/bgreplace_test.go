package bgreplace

import "testing"

func TestOpenQuoteDefaultsAndBillingPending(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "acct_a", ProjectID: "proj_1", InputID: "pin_1",
		InputVersion: "v1", BackgroundIntent: "室内棚拍",
		CreativePath: false, BillingConnected: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.Mode != ModeFidelity || q.ProtectedRegion != "subject" {
		t.Fatalf("默认应为保真+主体保护, got %#v", q)
	}
	if q.BillingLabel != BillingPendingLabel {
		t.Fatalf("计费未连通应显示 %q, got %q", BillingPendingLabel, q.BillingLabel)
	}
	if q.QuoteStatus != QuoteUnconfirmed || q.ProductionGenerationPassed || q.BillingPassed {
		t.Fatalf("未确认报价不得宣称生产出图或计费已通过: %#v", q)
	}
	if q.Fingerprint == "" {
		t.Fatal("指纹不能为空")
	}
}

func TestCreativeRejectedWhenPathMissing(t *testing.T) {
	_, err := OpenQuote(QuoteInput{
		TenantID: "acct_a", ProjectID: "proj_1", InputID: "pin_1",
		InputVersion: "v1", Mode: string(ModeCreative), CreativePath: false,
	})
	if err != ErrCreativeUnavailable {
		t.Fatalf("创意路径不存在应拒绝, got %v", err)
	}
}

func TestModeChangeInvalidatesUnconfirmedQuote(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "acct_a", ProjectID: "proj_1", InputID: "pin_1",
		InputVersion: "v1", Mode: string(ModeFidelity), CreativePath: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	next := InvalidateIfChanged(q, ModeCreative, "v1")
	if next.QuoteStatus != QuoteInvalid || !next.MustReconfirm {
		t.Fatalf("改模式应使报价失效: %#v", next)
	}
	same := InvalidateIfChanged(q, ModeFidelity, "v1")
	if same.QuoteStatus != QuoteUnconfirmed {
		t.Fatalf("相同模式与输入版本不应失效: %#v", same)
	}
}

func TestSubmitIsIdempotentAndDoesNotRegenerateUnknown(t *testing.T) {
	q, err := OpenQuote(QuoteInput{
		TenantID: "acct_a", ProjectID: "proj_1", InputID: "pin_1", InputVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	first, err := DecideSubmit(SubmitInput{Quote: q, GenerationUsable: true})
	if err != nil || first.Status != StatusQueued || first.Idempotent || first.Quality != QualityUnknown {
		t.Fatalf("首次提交应排队且质量未知: %#v %v", first, err)
	}
	if first.ProductionGenerationPassed || first.Deliverable {
		t.Fatal("排队结果不能当成可交付或生产已通过")
	}
	again, err := DecideSubmit(SubmitInput{
		Quote: q, GenerationUsable: true, ExistingStatus: StatusUnknown, SameFingerprint: true,
	})
	if err != nil || !again.Idempotent || again.Status != StatusUnknown || again.Regenerate {
		t.Fatalf("未知结果必须原样返回且不重生成: %#v %v", again, err)
	}
}

func TestGenerationUnavailableStaysHonest(t *testing.T) {
	q, _ := OpenQuote(QuoteInput{
		TenantID: "acct_a", ProjectID: "proj_1", InputID: "pin_1", InputVersion: "v1",
	})
	q.QuoteStatus = QuoteConfirmed
	got, err := DecideSubmit(SubmitInput{Quote: q, GenerationUsable: false})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusGenerationUnavailable || got.Evidence != EvidenceNone || got.Quality != QualityUnknown {
		t.Fatalf("生成不可用应只留结构证据: %#v", got)
	}
	if got.CallPlatformTask {
		t.Fatal("生成不可用不得调用平台任务")
	}
}

func TestFidelityFailureDoesNotSwitchMode(t *testing.T) {
	ex := Execution{Status: StatusCompleted, Mode: ModeFidelity, Quality: QualityUnknown}
	got := ApplyQuality(ex, QualityFail)
	if got.Mode != ModeFidelity || got.Quality != QualityFail || got.CreativeFallback || got.Deliverable {
		t.Fatalf("失败不得切创意或变成可交付: %#v", got)
	}
	_, err := Select(got)
	if err != ErrNotDeliverable {
		t.Fatalf("失败候选不能选定为可交付, got %v", err)
	}
}

func TestExportReusesAssetVersion(t *testing.T) {
	selected, err := Select(Execution{
		Status: StatusCompleted, Mode: ModeFidelity, Quality: QualityUnknown,
		OutputAssetID: "asset_1", OutputVersion: "out_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Deliverable || selected.ProductionGenerationPassed {
		t.Fatal("质量未知的选定结果仍不可当作保真交付")
	}
	exp, err := Export(selected)
	if err != nil {
		t.Fatal(err)
	}
	if exp.OutputAssetID != "asset_1" || exp.OutputVersion != "out_1" || exp.StartsGeneration {
		t.Fatalf("导出必须复用同一版本且不重新生成: %#v", exp)
	}
}

func TestAcceptanceSamplesStayUnknown(t *testing.T) {
	samples := AcceptanceSamples()
	if len(samples) != 2 {
		t.Fatalf("需要文字/Logo 与复杂材质两条样本, got %d", len(samples))
	}
	seen := map[string]bool{}
	for _, s := range samples {
		seen[s.Kind] = true
		if s.Quality != QualityUnknown || s.ModelUsage != "未调用" || s.CostLabel != BillingPendingLabel {
			t.Fatalf("无真实调用时样本不得写成通过: %#v", s)
		}
	}
	if !seen["logo_text"] || !seen["complex_material"] {
		t.Fatalf("样本种类不全: %#v", seen)
	}
}
