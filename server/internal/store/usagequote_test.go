package store

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestUsageQuoteIsNotAWallet(t *testing.T) {
	raw, err := os.ReadFile("migrations/0010_usage_quote.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.ToLower(string(raw))
	for _, banned := range []string{"wallet", "balance_cny", "topup", "recharge", "image_coin", "点数", "修点"} {
		if strings.Contains(sqlText, banned) {
			t.Fatalf("用量报价表不能保存 %s", banned)
		}
	}
	s := openTest(t)
	ctx := context.Background()
	saved, err := s.SaveUsageQuote(ctx, UsageQuote{
		StorageTenant: "personal/default:acct_1", ProjectID: "prj_1",
		PayerKind: "personal", PayerAccountRef: "acct_1", PayerDisplay: "个人付款",
		ImageCount: 1, Resolution: "800x800", Capability: "image.generate.standard",
		PricingVersion: "pricing-2026-09", Fingerprint: "fp-1", QuoteRef: "qte_1",
		Presentation: "待确认", IdempotencyKey: "idem-1", Status: "draft",
	})
	if err != nil || saved.ID == "" {
		t.Fatalf("保存报价: %v %+v", err, saved)
	}
	cols, err := s.db.Query(`PRAGMA table_info(image_usage_quotes)`)
	if err != nil {
		t.Fatal(err)
	}
	defer cols.Close()
	for cols.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := cols.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(name, "balance") || strings.Contains(name, "wallet") {
			t.Fatalf("列 %s 像余额副本", name)
		}
	}
	if _, err := s.GetUsageQuote(ctx, "org_other", saved.ID); err != ErrNotFound {
		t.Fatalf("其他商家不能读到报价: %v", err)
	}
	changed, err := s.SaveUsageQuote(ctx, UsageQuote{
		StorageTenant: "personal/default:acct_1", ProjectID: "prj_1",
		PayerKind: "personal", PayerAccountRef: "acct_1", PayerDisplay: "个人付款",
		ImageCount: 2, Resolution: "800x800", Capability: "image.generate.standard",
		PricingVersion: "pricing-2026-09", Fingerprint: "fp-2", QuoteRef: "qte_2",
		Presentation: "本次预计 ¥3.00", IdempotencyKey: "idem-1", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID != saved.ID || changed.Confirmed || changed.Fingerprint != "fp-2" {
		t.Fatalf("同一工程应覆盖并取消确认: %+v", changed)
	}
}
