package appregistry

// app-registry/v1 严格加载与解析测试:加载即校验、URL 白名单、精确解析。

import (
	"strings"
	"testing"
)

const goodManifest = `{
	"manifest_version": "app-registry/v1",
	"apps": [
		{"app_id": "orders", "display_name": "订单", "enabled": true,
		 "supported_source_kinds": ["order"],
		 "capabilities": [{"name": "order.handoff", "menu_visible": true, "requires_billing": false}],
		 "launch_targets": [{"target_id": "ti-orders-web", "kind": "launch", "url": "https://orders.example.invalid/launch"}],
		 "receipt_targets": [{"target_id": "rc-orders", "kind": "receipt", "url": "https://orders.example.invalid/receipt"}]},
		{"app_id": "campaign-tool", "display_name": "活动", "enabled": false,
		 "supported_source_kinds": ["campaign", "standalone"],
		 "capabilities": [{"name": "image.generate", "menu_visible": false, "requires_billing": true}],
		 "launch_targets": [{"target_id": "ti-camp", "kind": "launch", "url": "http://127.0.0.1:19999/launch"}],
		 "receipt_targets": []}
	]
}`

func TestLoadManifestAndResolve(t *testing.T) {
	m, err := LoadManifest([]byte(goodManifest))
	if err != nil {
		t.Fatalf("合法清单应加载成功: %v", err)
	}
	url, err := m.ResolveTarget("orders", "ti-orders-web", KindLaunch)
	if err != nil || url != "https://orders.example.invalid/launch" {
		t.Fatalf("精确解析失败: %q %v", url, err)
	}
	if _, err := m.ResolveTarget("orders", "ti-orders-web", KindReceipt); err == nil {
		t.Fatal("kind 不匹配应拒绝")
	}
	if _, err := m.ResolveTarget("orders", "nope", KindLaunch); err == nil {
		t.Fatal("未知 target 应拒绝")
	}
	if _, err := m.ResolveTarget("unknown-app", "ti-orders-web", KindLaunch); err == nil {
		t.Fatal("未知 app 应拒绝")
	}
	if rc, err := m.FirstReceiptTarget("orders"); err != nil || rc.TargetID != "rc-orders" {
		t.Fatalf("FirstReceiptTarget: %v %v", rc, err)
	}
	if _, err := m.FirstReceiptTarget("campaign-tool"); err == nil {
		t.Fatal("无 receipt target 应报错(fail-closed)")
	}
}

func TestLoadManifestRejects(t *testing.T) {
	cases := map[string]string{
		"未知版本":       strings.Replace(goodManifest, "app-registry/v1", "app-registry/v2", 1),
		"未知字段":       strings.Replace(goodManifest, `"apps": [`, `"extra": 1, "apps": [`, 1),
		"重复键":        strings.Replace(goodManifest, `"enabled": true,`, `"enabled": true, "enabled": true,`, 1),
		"坏 slug":     strings.Replace(goodManifest, `"app_id": "orders"`, `"app_id": "Orders!"`, 1),
		"外网 http":    strings.Replace(goodManifest, "https://orders.example.invalid/launch", "http://orders.example.invalid/launch", 1),
		"带 query":    strings.Replace(goodManifest, "/launch", "/launch?next=x", 1),
		"带 fragment": strings.Replace(goodManifest, "/launch", "/launch#frag", 1),
		"无 path":     strings.Replace(goodManifest, "https://orders.example.invalid/launch", "https://orders.example.invalid", 1),
		"userinfo":   strings.Replace(goodManifest, "https://orders.example.invalid/launch", "https://u@orders.example.invalid/launch", 1),
	}
	for name, raw := range cases {
		if _, err := LoadManifest([]byte(raw)); err == nil {
			t.Fatalf("%s 应拒绝加载", name)
		}
	}
	// loopback http 允许(测试桩需要)。
	if _, err := LoadManifest([]byte(goodManifest)); err != nil {
		t.Fatalf("loopback http 应允许: %v", err)
	}
}
