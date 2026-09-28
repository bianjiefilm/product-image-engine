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
	for _, move := range []string{"", "zoom", "orbit", "360度", "创意运镜"} {
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
