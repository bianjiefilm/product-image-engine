package textimage

import "testing"

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
