package fidelity

import "testing"

func baseInput() Input {
	return Input{
		Mode:           ModeFidelity,
		MethodVersion:  MethodVersionV1,
		InputRef:       "asset-in",
		InputVersion:   "in-v1",
		OutputRef:      "out-1",
		MaskRef:        "mask-1",
		Protected:      append([]string(nil), RequiredProtected...),
		SampleClass:    SampleLogoText,
		MachineScope:   "边缘与文字像素差",
		HumanScope:     "包装文字与 Logo 目视",
		AllowedRegions: []string{"background"},
		Checks:         allPassChecks(),
	}
}

func allPassChecks() []Check {
	out := make([]Check, len(RequiredChecks))
	for i, name := range RequiredChecks {
		out[i] = Check{Name: name, Result: ResultPass}
	}
	return out
}

func TestUnauthorizedGenerationStaysUnknown(t *testing.T) {
	in := baseInput()
	in.SupplierAuthorized = false
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictUnknown || rep.ExactProduct || rep.Deliverable {
		t.Fatalf("未授权不得判通过: %+v", rep)
	}
	if rep.RealGeneration != RealGenerationUnknown {
		t.Fatalf("真实生成应为 unknown, got %q", rep.RealGeneration)
	}
	if !hasLimit(rep, "真实生成保真未授权，结论为待确认") {
		t.Fatalf("缺少待确认限制: %+v", rep.Limits)
	}
	if rep.HoldingWearVerified {
		t.Fatal("本票不得宣称持物/穿戴已验证")
	}
}

func TestClientCannotHideTextFailureBehindSimilarity(t *testing.T) {
	in := baseInput()
	in.SupplierAuthorized = true
	in.SimilarityOnly = true
	in.Checks = allPassChecks()
	setCheck(&in, CheckLogoText, ResultFail)
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictFail || rep.ExactProduct {
		t.Fatalf("文字失败不能被相似度盖过: %+v", rep)
	}
	if !hasLimit(rep, "相似度不能单独证明文字或结构保真") {
		t.Fatalf("缺少相似度限制: %+v", rep.Limits)
	}
}

func TestSimilarityAloneCannotPass(t *testing.T) {
	in := baseInput()
	in.SupplierAuthorized = true
	in.SimilarityOnly = true
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictUnknown || rep.ExactProduct {
		t.Fatalf("仅有相似度不得通过: %+v", rep)
	}
}

func TestCreativeModeIsNotFidelityPass(t *testing.T) {
	in := baseInput()
	in.Mode = ModeCreative
	in.SupplierAuthorized = true
	in.ClaimsExactProduct = true
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict == VerdictPass || rep.ExactProduct || rep.Deliverable {
		t.Fatalf("创意模式不得升级为保真通过: %+v", rep)
	}
	if rep.Verdict != VerdictFail {
		t.Fatalf("宣称精确商品图的创意结果应为未通过: %+v", rep)
	}
}

func TestCreativeWithoutExactClaimStaysUnconfirmed(t *testing.T) {
	in := baseInput()
	in.Mode = ModeCreative
	in.SupplierAuthorized = true
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictUnknown || rep.ExactProduct {
		t.Fatalf("创意结果默认不是精确商品图: %+v", rep)
	}
}

func TestUnconfirmedSubjectEditFails(t *testing.T) {
	in := baseInput()
	in.SupplierAuthorized = true
	in.SubjectEdit = true
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictFail || rep.Deliverable {
		t.Fatalf("未经确认的主体修改应失败且不可交付: %+v", rep)
	}
}

func TestConfirmedSubjectEditCanPassOnlyWhenAuthorized(t *testing.T) {
	in := baseInput()
	in.SubjectEdit = true
	in.SubjectEditConfirmed = true
	in.SupplierAuthorized = true
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictPass || !rep.ExactProduct || !rep.Deliverable {
		t.Fatalf("授权且检查齐全、修改已确认才可保真通过: %+v", rep)
	}
	if rep.RealGeneration != RealGenerationAuthorized {
		t.Fatalf("授权路径应标记 authorized, got %q", rep.RealGeneration)
	}
}

func TestMissingHumanScopeStaysUnknown(t *testing.T) {
	in := baseInput()
	in.SupplierAuthorized = true
	in.HumanScope = ""
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictUnknown || rep.ExactProduct {
		t.Fatalf("无人工核验范围不得通过: %+v", rep)
	}
}

func TestNoOutputCannotPass(t *testing.T) {
	in := baseInput()
	in.SupplierAuthorized = true
	in.OutputRef = ""
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictUnknown || rep.ExactProduct {
		t.Fatalf("没有输出版本不得判通过: %+v", rep)
	}
}

func TestMissingMaskRejected(t *testing.T) {
	in := baseInput()
	in.MaskRef = ""
	if _, err := Evaluate(in); err == nil {
		t.Fatal("缺少 mask 应拒绝")
	}
}

func TestRelabelDoesNotRewriteBody(t *testing.T) {
	failIn := baseInput()
	setCheck(&failIn, CheckShape, ResultFail)
	passIn := baseInput()
	if BodySHA256(failIn) == BodySHA256(passIn) {
		t.Fatal("失败与通过的输入指纹必须不同，避免原地改标签")
	}
	failRep, err := Evaluate(failIn)
	if err != nil {
		t.Fatal(err)
	}
	if failRep.Verdict != VerdictFail {
		t.Fatalf("形状失败应未通过: %+v", failRep)
	}
}

func TestExportDocumentKeepsLimits(t *testing.T) {
	in := baseInput()
	rep, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	doc := rep.ExportDocument()
	if doc["schema"] != SchemaVersion {
		t.Fatalf("schema: %v", doc["schema"])
	}
	if doc["exact_product"] != false || doc["holding_wear_verified"] != false {
		t.Fatalf("导出不得把未授权结果写成精确商品图: %+v", doc)
	}
	if doc["mode"] != ModeFidelity || doc["method_version"] != MethodVersionV1 {
		t.Fatalf("导出缺模式或方法版本: %+v", doc)
	}
}

func TestBlocksVerifiedReceipt(t *testing.T) {
	if block, _, _ := BlocksVerifiedReceipt(nil); block {
		t.Fatal("没有报告时不应拦截既有回执")
	}
	unknown := Report{Verdict: VerdictUnknown, ExactProduct: false}
	if block, code, _ := BlocksVerifiedReceipt([]Report{unknown}); !block || code != "fidelity_unconfirmed" {
		t.Fatalf("待确认不得回执为已验证: %v %s", block, code)
	}
	failed := Report{Verdict: VerdictFail}
	if block, code, _ := BlocksVerifiedReceipt([]Report{failed}); !block || code != "fidelity_failed" {
		t.Fatalf("未通过应拦住回执: %v %s", block, code)
	}
	passed := Report{Verdict: VerdictPass, ExactProduct: true}
	if block, _, _ := BlocksVerifiedReceipt([]Report{passed, failed}); block {
		t.Fatal("已确认的保真通过不应被后来的失败候选撤销")
	}
}

func setCheck(in *Input, name, result string) {
	for i := range in.Checks {
		if in.Checks[i].Name == name {
			in.Checks[i].Result = result
			return
		}
	}
	in.Checks = append(in.Checks, Check{Name: name, Result: result})
}

func hasLimit(rep Report, want string) bool {
	for _, s := range rep.Limits {
		if s == want {
			return true
		}
	}
	for _, s := range rep.Reasons {
		if s == want {
			return true
		}
	}
	return false
}
