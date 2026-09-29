package billassemble

import (
	"context"
	"testing"
)

func TestConnectionReportsMissingConfig(t *testing.T) {
	if got := Connection(LinkConfig{}); got != ConnMissing {
		t.Fatalf("空配置应是配置缺失, got %q", got)
	}
	if got := Connection(LinkConfig{Enabled: true, BaseURL: "http://127.0.0.1:18102", AppID: "product-image"}); got != ConnMissing {
		t.Fatalf("缺令牌应是配置缺失, got %q", got)
	}
	if got := Connection(LinkConfig{
		Enabled: true, BaseURL: "http://127.0.0.1:18102", Token: "tok", AppID: "product-image",
	}); got != ConnReady {
		t.Fatalf("齐备应为已配置, got %q", got)
	}
}

func TestRecordingHoldReleasesToZero(t *testing.T) {
	rec := &Record{UnitMinor: 150, BalanceMinor: 1000, BalanceKnown: true, Entitled: []string{"image.generate.standard"}}
	ctx := context.Background()
	quoted, err := rec.Quote(ctx, QuoteCall{
		PayerAccountID: "acct_person", Capability: "image.generate.standard",
		Quantity: 2, PricingVersion: PricingVersion, BusinessRef: "proj_1", IdempotencyKey: "fp-1",
	})
	if err != nil || quoted.AmountMinor != 300 || quoted.UsageID == "" || quoted.FundsShort {
		t.Fatalf("报价: %+v %v", quoted, err)
	}
	again, err := rec.Quote(ctx, QuoteCall{
		PayerAccountID: "acct_person", Capability: "image.generate.standard",
		Quantity: 2, PricingVersion: PricingVersion, BusinessRef: "proj_1", IdempotencyKey: "fp-1",
	})
	if err != nil || again.UsageID != quoted.UsageID {
		t.Fatalf("同一幂等键应回到原用量: %+v %v", again, err)
	}
	held, err := rec.Hold(ctx, HoldCall{UsageID: quoted.UsageID, PayerAccountID: "acct_person", IdempotencyKey: "hold-1", AmountMinor: quoted.AmountMinor})
	if err != nil || held.Sent || held.HoldID == "" || rec.NetReserved() != 300 {
		t.Fatalf("预占应记在测试双里: %+v reserved=%d err=%v", held, rec.NetReserved(), err)
	}
	if _, err := rec.Settle(ctx, SettleCall{UsageID: quoted.UsageID, IdempotencyKey: "chg-1"}); err != ErrFundsNotExecuted || rec.Settles != 1 || rec.NetReserved() != 300 {
		t.Fatalf("结算不应动用预占, reserved=%d settles=%d err=%v", rec.NetReserved(), rec.Settles, err)
	}
	released, err := rec.Release(ctx, ReleaseCall{HoldID: held.HoldID, IdempotencyKey: "rel-1"})
	if err != nil || released.ReleasedMinor != 300 || rec.NetReserved() != 0 {
		t.Fatalf("释放后净额应为 0: %+v reserved=%d err=%v", released, rec.NetReserved(), err)
	}
}

func TestReconcileKeepsUnknownWithoutABill(t *testing.T) {
	look := LookupFact{FoundTask: true, TaskID: "task_1", TaskStatus: "succeeded", Settlement: SettlementUnknown}
	got := Reconcile(true, look)
	if got.Settlement != PendingCheck || got.Regenerate || got.BillingPassed {
		t.Fatalf("完成但没有账单应待核对且不重做: %+v", got)
	}
	unknown := Reconcile(false, LookupFact{})
	if unknown.Settlement != SettlementUnknown || unknown.BillingPassed || unknown.Regenerate {
		t.Fatalf("没有账单不能写成未扣费证明: %+v", unknown)
	}
	billed := Reconcile(true, LookupFact{FoundCharge: true, ChargeRef: "chg_1"})
	if !billed.BillingPassed || billed.Settlement != "charged" || billed.Regenerate {
		t.Fatalf("有账单才可以通过: %+v", billed)
	}
}
