package showcasevideo

import "testing"

func TestOpenRequiresProductImageAndKnownMove(t *testing.T) {
	q, err := Open(OpenInput{
		TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: Move360,
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.ImageID != "in_cup" || q.CameraMove != Move360 || q.Fingerprint == "" {
		t.Fatalf("已有产品图应能开 360 度展示视频: %+v", q)
	}
	if q.ShowVideo || q.PlayStill || q.VideoURL != "" {
		t.Fatalf("开单不能播放视频或静图: %+v", q)
	}
	if q.BillingLabel != BillingPendingLabel || q.Charged || q.BillingPassed || q.ProductionAuthorized {
		t.Fatalf("计费未接通不能扣费或标通过: %+v", q)
	}
	scene, err := Open(OpenInput{
		TenantID: "ten", ProjectID: "prj", ImageID: "out_pack", CameraMove: MoveScene,
	})
	if err != nil || scene.CameraMove != MoveScene {
		t.Fatalf("场景运镜应被接受: %v %+v", err, scene)
	}
}

func TestMissingImageOrUnknownMoveRejected(t *testing.T) {
	if _, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", CameraMove: Move360}); err != ErrImageRequired {
		t.Fatalf("没有产品图应拒绝, got %v", err)
	}
	if _, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "  ", CameraMove: MoveScene}); err != ErrImageRequired {
		t.Fatalf("空白产品图应拒绝, got %v", err)
	}
	for _, move := range []string{"", "zoom", "orbit", "360度", "创意运镜", "文生视频", "t2v", "text-to-video"} {
		if _, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: move}); err != ErrMoveNotAllowed {
			t.Fatalf("运镜 %q 应拒绝, got %v", move, err)
		}
	}
}

func TestClientAmountDoesNotBecomeACharge(t *testing.T) {
	q, err := Open(OpenInput{
		TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: Move360,
		BillingConnected: false, QuotedAmount: "¥9.90",
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.BillingLabel != BillingPendingLabel || q.Charged || q.BillingPassed || q.ProductionAuthorized || q.ShowVideo {
		t.Fatalf("客户端金额不能当成已扣费或已出视频: %+v", q)
	}
}

func TestSubmitWithoutSupplierFailsWithoutVideo(t *testing.T) {
	q, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: Move360})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	rec, err := DecideSubmit(SubmitInput{Quote: q, VideoProviderUsable: false})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusFailed || rec.ShowVideo || rec.PlayStill || rec.VideoURL != "" || rec.CallSupplier {
		t.Fatalf("没有视频供应商时应失败且不出现视频: %+v", rec)
	}
	if rec.BillingPassed || rec.ProductionAuthorized || rec.Charged || rec.BillingLabel != BillingPendingLabel {
		t.Fatalf("失败不能写成计费或生产已通过: %+v", rec)
	}
}

func TestSameRequestStaysTheSameFailureOrPending(t *testing.T) {
	q, _ := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: MoveScene})
	q.QuoteStatus = QuoteConfirmed
	rec, err := DecideSubmit(SubmitInput{
		Quote: q, VideoProviderUsable: true, SameFingerprint: true,
		ExistingID: "vid_1", ExistingStatus: StatusFailed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusFailed || !rec.Idempotent || rec.Regenerate || rec.CallSupplier || rec.ID != "vid_1" || rec.ShowVideo || rec.PlayStill {
		t.Fatalf("同一请求刷新后仍应是原失败记录: %+v", rec)
	}
	pending, err := DecideSubmit(SubmitInput{
		Quote: q, VideoProviderUsable: true, SameFingerprint: true,
		ExistingID: "vid_2", ExistingStatus: StatusUnknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != StatusUnknown || pending.ID != "vid_2" || pending.Regenerate || pending.ShowVideo || pending.PlayStill || pending.VideoURL != "" {
		t.Fatalf("待确认记录刷新后不能改成成功或新视频: %+v", pending)
	}
}

func TestFailureCannotBeRelabeledSuccess(t *testing.T) {
	rec := Record{ID: "vid_1", Status: StatusFailed, BillingLabel: BillingPendingLabel}
	if _, err := ApplyClientClaim(rec, "success"); err != ErrRelabelForbidden {
		t.Fatalf("失败不能改成成功, got %v", err)
	}
	if _, err := ApplyClientClaim(rec, "pass"); err != ErrRelabelForbidden {
		t.Fatalf("失败不能改成通过, got %v", err)
	}
	next := NoteOutputID(rec, "filled-video")
	if next.Status != StatusFailed || next.ShowVideo || next.PlayStill || next.OutputIsReceipt || next.BillingPassed || next.ProductionAuthorized || next.VideoURL != "" {
		t.Fatalf("填上的输出编号不能把失败核销成视频: %+v", next)
	}
}

func TestFingerprintChangesWithImageOrMove(t *testing.T) {
	a := Fingerprint("ten", "prj", "in_cup", Move360)
	b := Fingerprint("ten", "prj", "in_cup", MoveScene)
	c := Fingerprint("ten", "prj", "out_pack", Move360)
	if a == "" || a == b || a == c {
		t.Fatal("不同产品图或运镜必须是不同请求")
	}
}

func TestUnconfirmedQuoteDoesNotSubmitOrCharge(t *testing.T) {
	q, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: Move360})
	if err != nil {
		t.Fatal(err)
	}
	if q.QuoteStatus != QuoteUnconfirmed {
		t.Fatalf("开单后报价应仍未确认: %+v", q)
	}
	rec, err := DecideSubmit(SubmitInput{Quote: q, VideoProviderUsable: true})
	if err != ErrQuoteUnconfirmed {
		t.Fatalf("未确认报价不得提交, got %v", err)
	}
	if rec.CallSupplier || rec.Charged || rec.ShowVideo || rec.PlayStill || rec.VideoURL != "" || rec.Status == StatusFailed {
		t.Fatalf("拒绝提交时不能调用、扣费或留下视频结果: %+v", rec)
	}
}

func TestCallerSupplierFlagDoesNotAuthorizeCall(t *testing.T) {
	q, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: MoveScene})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	rec, err := DecideSubmit(SubmitInput{Quote: q, VideoProviderUsable: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusFailed || rec.CallSupplier || rec.Charged || rec.BillingPassed || rec.ProductionAuthorized {
		t.Fatalf("没有已接入的图生视频时不得调用或扣费: %+v", rec)
	}
	if rec.ShowVideo || rec.PlayStill || rec.VideoURL != "" || rec.BillingLabel != BillingPendingLabel {
		t.Fatalf("调用标记不能变成可播放视频: %+v", rec)
	}
	again, err := DecideSubmit(SubmitInput{
		Quote: q, VideoProviderUsable: true, SameFingerprint: true,
		ExistingID: "vid_same", ExistingStatus: StatusFailed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != "vid_same" || again.Status != StatusFailed || !again.Idempotent || again.CallSupplier || again.Charged || again.Regenerate {
		t.Fatalf("同一输入再次提交不得重新调用或扣费: %+v", again)
	}
}

func TestStillPlaceholderAndEmptyOutputAreNotVideo(t *testing.T) {
	q, err := Open(OpenInput{TenantID: "ten", ProjectID: "prj", ImageID: "in_cup", CameraMove: Move360})
	if err != nil {
		t.Fatal(err)
	}
	q.QuoteStatus = QuoteConfirmed
	rec, err := DecideSubmit(SubmitInput{Quote: q, VideoProviderUsable: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, outputID := range []string{
		"https://example.invalid/placeholder.mp4",
		"image/jpeg",
		"",
		"   ",
	} {
		next := NoteOutputID(rec, outputID)
		if next.Status != StatusFailed || next.ShowVideo || next.PlayStill || next.VideoURL != "" || next.Charged || next.OutputIsReceipt || next.BillingPassed || next.ProductionAuthorized {
			t.Fatalf("输出 %q 不能当成视频成功: %+v", outputID, next)
		}
		if outputID == "   " && next.OutputAssetID != "" {
			t.Fatalf("空白输出编号应被丢掉: %+v", next)
		}
	}
	if _, err := ApplyClientClaim(rec, "可播放"); err != ErrRelabelForbidden {
		t.Fatalf("静图或占位口径不能改成可播放, got %v", err)
	}
}
