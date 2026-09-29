package billassemble

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLiveQuoteDoesNotHoldOrCharge(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var payer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		switch r.URL.Path {
		case "/internal/v1/billing/unified/usage":
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			payer, _ = req["payer_account_id"].(string)
			if req["pricing_version"] != PricingVersion {
				t.Errorf("价格版本 %v", req["pricing_version"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "fact": map[string]any{"usage_id": "usg_live"}})
		case "/internal/v1/billing/unified/quote":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "quote": map[string]any{
				"usage_id": "usg_live", "amount_minor": 150, "quantity": 1, "pricing_version": PricingVersion,
			}})
		case "/internal/v1/billing/unified/accounts":
			if r.URL.Query().Get("account_id") != "acct_person" {
				t.Errorf("余额账号 %s", r.URL.Query().Get("account_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "snapshot": map[string]any{
				"account":      map[string]any{"cash_balance_minor": 1000},
				"entitlements": []any{map[string]any{"capability": "image.generate.standard", "app_id": "product-image", "status": "active"}},
			}})
		default:
			t.Errorf("不该发送 %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	live := &Live{AppID: "product-image", BillingToken: "bill-tok", BillingBase: srv.URL, Enabled: true}
	quoted, err := live.Quote(context.Background(), QuoteCall{
		PayerAccountID: "acct_person", Capability: "image.generate.standard",
		Quantity: 1, PricingVersion: PricingVersion, BusinessRef: "proj_1", IdempotencyKey: "fp-1",
	})
	if err != nil || quoted.UsageID != "usg_live" || quoted.AmountMinor != 150 || !quoted.BalanceKnown || quoted.FundsShort {
		t.Fatalf("报价: %+v %v", quoted, err)
	}
	if payer != "acct_person" {
		t.Fatalf("付款人应是服务端账号, got %q", payer)
	}
	if !quoted.EntitlementsKnown || len(quoted.Entitlements) != 1 {
		t.Fatalf("权益: %+v", quoted.Entitlements)
	}
	if _, err := live.Hold(context.Background(), HoldCall{UsageID: "usg_live", AmountMinor: 150, IdempotencyKey: "hold-1"}); err != ErrFundsNotExecuted {
		t.Fatalf("真实预占应拒绝发送, %v", err)
	}
	if _, err := live.Settle(context.Background(), SettleCall{UsageID: "usg_live", IdempotencyKey: "chg-1"}); err != ErrFundsNotExecuted {
		t.Fatalf("真实结算应拒绝发送, %v", err)
	}
	if _, err := live.Release(context.Background(), ReleaseCall{HoldID: "uhold_1", IdempotencyKey: "rel-1"}); err != ErrFundsNotExecuted {
		t.Fatalf("真实释放应拒绝发送, %v", err)
	}
	for _, path := range paths {
		if strings.Contains(path, "hold") || strings.Contains(path, "charge") || strings.Contains(path, "refund") {
			t.Fatalf("资金路径被发送: %v", paths)
		}
	}
}

func TestLiveMissingConfigDoesNotDial(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	live := &Live{AppID: "product-image", BillingBase: srv.URL}
	_, err := live.Quote(context.Background(), QuoteCall{PayerAccountID: "acct_person", Quantity: 1, Capability: "image.generate.standard", PricingVersion: PricingVersion, IdempotencyKey: "fp"})
	if err != ErrConfigMissing || called {
		t.Fatalf("缺配置不应发请求: err=%v called=%v", err, called)
	}
}

func TestLiveRouteMissingIsUnimplemented(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"billing_route_not_found"}}`))
	}))
	t.Cleanup(srv.Close)
	live := &Live{AppID: "product-image", BillingToken: "tok", BillingBase: srv.URL, Enabled: true}
	_, err := live.Quote(context.Background(), QuoteCall{
		PayerAccountID: "acct_person", Quantity: 1, Capability: "image.generate.standard",
		PricingVersion: PricingVersion, IdempotencyKey: "fp", BusinessRef: "proj",
	})
	if err != ErrUnimplemented {
		t.Fatalf("路由不存在应是未实现, got %v", err)
	}
}

func TestLiveLookupDoesNotInventACharge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/tasks/task_1" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-App-ID") != "product-image" {
			t.Errorf("app %s", r.Header.Get("X-App-ID"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "task_1", "status": "succeeded"})
	}))
	t.Cleanup(srv.Close)
	live := &Live{AppID: "product-image", TaskToken: "task-tok", TaskBase: srv.URL, Enabled: true}
	got, err := live.Lookup(context.Background(), LookupCall{TaskID: "task_1"})
	if err != nil || !got.FoundTask || got.FoundCharge || got.Settlement != SettlementUnknown {
		t.Fatalf("查回应保持未知: %+v %v", got, err)
	}
	reconciled := Reconcile(true, got)
	if reconciled.Settlement != PendingCheck || reconciled.Regenerate || reconciled.BillingPassed {
		t.Fatalf("查回后仍待核对: %+v", reconciled)
	}
}

func TestLiveLookupFeeRefIsNotASettlement(t *testing.T) {
	for _, key := range []string{"charge_id", "charge_ref", "fee_ref"} {
		t.Run(key, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path != "/internal/v1/tasks/task_fee" {
					t.Errorf("不该请求 %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "task_fee", "status": "succeeded", key: "chg_on_ticket"})
			}))
			t.Cleanup(srv.Close)
			live := &Live{AppID: "product-image", TaskToken: "task-tok", TaskBase: srv.URL, Enabled: true}
			got, err := live.Lookup(context.Background(), LookupCall{TaskID: "task_fee"})
			if err != nil || !got.FoundTask || !got.FoundCharge || got.ChargeRef != "chg_on_ticket" || got.Settlement != SettlementUnknown {
				t.Fatalf("费用引用可以记下，但不是已结算: %+v %v", got, err)
			}
			done := Reconcile(true, got)
			if done.BillingPassed || done.Settlement != PendingCheck || done.Regenerate {
				t.Fatalf("作品完成仍待核对: %+v", done)
			}
			open := Reconcile(false, LookupFact{})
			if open.Settlement != SettlementUnknown || open.BillingPassed || open.Regenerate {
				t.Fatalf("没有任务仍是未知: %+v", open)
			}
			for _, path := range paths {
				if strings.Contains(path, "hold") || strings.Contains(path, "charge") || strings.Contains(path, "refund") || strings.Contains(path, "billing") {
					t.Fatalf("查回不能发资金请求: %v", paths)
				}
			}
		})
	}
}
