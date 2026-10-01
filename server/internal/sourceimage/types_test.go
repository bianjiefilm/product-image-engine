package sourceimage

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func validIntent() Intent {
	return Intent{Scope: Scope{AppID: "product-image", TenantID: "acct_a", ProjectID: "prj_a", UserID: "usr_a", PrincipalAccountID: "acct_a", PayerAccountID: "acct_a"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝纸盒", PricingVersion: "test-price-v1", Quantity: 1}
}
func TestSourceFingerprintFreezesCompleteIntent(t *testing.T) {
	in := validIntent()
	first, e := Fingerprint(in)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Intent){func(i *Intent) { i.Scope.PayerAccountID = "org_b" }, func(i *Intent) { i.Scope.UserID = "usr_b" }, func(i *Intent) { i.Scope.ProjectID = "prj_b" }, func(i *Intent) { i.Prompt = "红纸盒" }, func(i *Intent) { i.PricingVersion = "price-v2" }, func(i *Intent) { i.RequestKey = "request-b" }} {
		changed := in
		change(&changed)
		other, e := Fingerprint(changed)
		if e != nil || other == first {
			t.Fatal(other, e)
		}
	}
	for _, change := range []func(*Intent){func(i *Intent) { i.Quantity = 0 }, func(i *Intent) { i.Scope.AppID = "" }, func(i *Intent) { i.Prompt = "  " }, func(i *Intent) { i.Size = "800x800" }, func(i *Intent) { i.Mode = "reference_edit" }, func(i *Intent) { i.Model = "arbitrary" }} {
		changed := in
		change(&changed)
		if _, e := Fingerprint(changed); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
}
func TestSourceQuoteCanonicalAndFrozen(t *testing.T) {
	run := Run{Intent: validIntent(), UsageKey: "usage-a", BusinessRef: "product-image:run:a"}
	q := Quote{UsageID: "usage_a", UsageKey: run.UsageKey, QuoteID: "q_a", PricingVersion: run.Intent.PricingVersion, BusinessRef: run.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: 1000, ExpiresAtUnix: 1300}
	if e := ValidateQuote(run, q); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Quote){func(q *Quote) { q.AmountMinor = 0 }, func(q *Quote) { q.UnitPriceMinor = math.MaxInt64; q.Quantity = 2 }, func(q *Quote) { q.ExpiresAtUnix++ }, func(q *Quote) { q.UsageKey = "different" }, func(q *Quote) { q.PricingVersion = "different" }} {
		bad := q
		change(&bad)
		if e := ValidateQuote(run, bad); !errors.Is(e, ErrInvalid) {
			t.Fatal(bad, e)
		}
	}
	for _, raw := range []string{"null", "1.0", "1e2", "-1", "9223372036854775808", "\"25\"", "01", "-0"} {
		if _, e := ParseMinor(json.RawMessage(raw)); !errors.Is(e, ErrInvalid) {
			t.Fatal(raw, e)
		}
	}
	for _, raw := range []string{"0", "25", "9007199254740993", "9223372036854775807"} {
		if _, e := ParseMinor(json.RawMessage(raw)); e != nil {
			t.Fatal(raw, e)
		}
	}
}
