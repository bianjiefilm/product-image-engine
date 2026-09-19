package receiptdoc

// order-receipt/v1 校验与 §8 帧签名测试。

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func validReceipt() Receipt {
	return Receipt{
		SchemaVersion: SchemaVersionV1, EventID: "wr-1", RunID: "run-1", HandoffID: "h-1",
		SourceApp: "product-image-engine", TargetApp: "orders", PrincipalID: "principal-1",
		ProjectRef: "src-1", ProjectRevision: "rev-1", BriefVersion: "brief-1",
		Sequence: 1, Status: "succeeded",
		Assets:          []Asset{{AssetRef: "asset_1", SHA256: strings.Repeat("a", 64), SizeBytes: 10, MediaType: "image/png"}},
		BillingFactRefs: []string{"fact_1"},
		OccurredAt:      "2026-09-19T10:00:00Z",
	}
}

func TestReceiptValidate(t *testing.T) {
	r := validReceipt()
	if err := r.Validate(); err != nil {
		t.Fatalf("合法回执应通过: %v", err)
	}
	mut := func(f func(*Receipt)) string {
		x := validReceipt()
		f(&x)
		if err := x.Validate(); err == nil {
			return ""
		}
		return "rejected"
	}
	checks := map[string]func(*Receipt){
		"schema v2":     func(x *Receipt) { x.SchemaVersion = "order-receipt/v2" },
		"空 event":       func(x *Receipt) { x.EventID = "" },
		"空白 event":      func(x *Receipt) { x.EventID = "　" }, // U+3000
		"状态枚举":          func(x *Receipt) { x.Status = "ok" },
		"sequence 0":    func(x *Receipt) { x.Sequence = 0 },
		"sequence -1":   func(x *Receipt) { x.Sequence = -1 },
		"sha 大写":        func(x *Receipt) { x.Assets[0].SHA256 = strings.ToUpper(strings.Repeat("a", 64)) },
		"sha 短":         func(x *Receipt) { x.Assets[0].SHA256 = "abc" },
		"坏时间":           func(x *Receipt) { x.OccurredAt = "2026-09-19 10:00:00" },
		"billing 空 ref": func(x *Receipt) { x.BillingFactRefs = []string{""} },
		"project_ref空":  func(x *Receipt) { x.ProjectRef = "" },
	}
	for name, f := range checks {
		if mut(f) != "rejected" {
			t.Fatalf("%s 应拒绝", name)
		}
	}
}

func TestFrameSignVerify(t *testing.T) {
	secret := []byte("s3cret")
	ts := time.Now().Unix()
	raw := []byte(`{"k":"v"}`)
	sig := FrameSign(secret, ts, "product-image-engine", "orders", "wr-9", raw)
	if !strings.HasPrefix(sig, SignaturePrefix) {
		t.Fatalf("签名前缀: %s", sig)
	}
	// 帧字段(source/target/event)必须参与签名:不匹配即失败。
	if err := FrameVerify(secret, strconv.FormatInt(ts, 10), sig, "other", "orders", "wr-9", raw, time.Unix(ts, 0)); err == nil {
		t.Fatal("source 不匹配应拒绝")
	}
	// 正确通过路径
	if err := FrameVerify(secret, strconv.FormatInt(ts, 10), sig, "product-image-engine", "orders", "wr-9", raw, time.Unix(ts, 0)); err != nil {
		t.Fatalf("自验签应通过: %v", err)
	}
	// 篡改 body
	if err := FrameVerify(secret, strconv.FormatInt(ts, 10), sig, "product-image-engine", "orders", "wr-9", []byte(`{"k":"V"}`), time.Unix(ts, 0)); err == nil {
		t.Fatal("篡改 body 应拒绝")
	}
	// 重放(超 300s 偏差)
	if err := FrameVerify(secret, strconv.FormatInt(ts, 10), sig, "product-image-engine", "orders", "wr-9", raw, time.Unix(ts+301, 0)); err == nil {
		t.Fatal("超时偏差应拒绝")
	}
	// 垃圾时间戳
	if err := FrameVerify(secret, "12x0", sig, "product-image-engine", "orders", "wr-9", raw, time.Unix(ts, 0)); err == nil {
		t.Fatal("垃圾时间戳应拒绝")
	}
	// 错误签名格式
	if err := FrameVerify(secret, strconv.FormatInt(ts, 10), "sha256=zzzz", "product-image-engine", "orders", "wr-9", raw, time.Unix(ts, 0)); err == nil {
		t.Fatal("非 hex 签名应拒绝")
	}
	// 重发:时间戳刷新,raw_body 不变 → 新签名有效(回执重传语义)
	sig2 := FrameSign(secret, ts+10, "product-image-engine", "orders", "wr-9", raw)
	if err := FrameVerify(secret, strconv.FormatInt(ts+10, 10), sig2, "product-image-engine", "orders", "wr-9", raw, time.Unix(ts+10, 0)); err != nil {
		t.Fatalf("重发刷新签名应通过: %v", err)
	}
}
