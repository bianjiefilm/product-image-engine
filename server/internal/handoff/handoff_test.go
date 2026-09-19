package handoff

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSharedVectors 消费 HUI-1724 共享向量(testdata/source-profile-vectors.json,
// 从 public-ai docs/contracts/order-handoff/v1/ 同源复制钉版)。
// 本应用是 profile 感知消费者:断言 profiled_accepted 列;
// old_v1_accepted 列属于旧解析器(public-ai 侧职责),此处不断言。
func TestSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/source-profile-vectors.json")
	if err != nil {
		t.Fatalf("读取向量失败: %v", err)
	}
	var vecs struct {
		Cases []struct {
			Name             string `json:"name"`
			VectorKind       string `json:"vector_kind"`
			Document         string `json:"document"`
			ProfiledAccepted bool   `json:"profiled_accepted"`
			ExpectError      string `json:"expect_error"`
			Now              string `json:"now"`
			ExpiresAt        string `json:"expires_at"`
			ExpectExpired    bool   `json:"expect_expired"`
			Declared         string `json:"declared"`
			Expected         string `json:"expected"`
			ExpectMatch      bool   `json:"expect_match"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vecs); err != nil {
		t.Fatalf("向量解析失败: %v", err)
	}
	if len(vecs.Cases) < 10 {
		t.Fatalf("向量数量异常: %d", len(vecs.Cases))
	}
	for _, c := range vecs.Cases {
		t.Run(c.Name, func(t *testing.T) {
			switch c.VectorKind {
			case "handoff":
				h, err := Parse([]byte(c.Document))
				if c.ProfiledAccepted {
					if err != nil {
						t.Fatalf("应接受却拒绝: %v", err)
					}
					if c.ExpectError != "" && ErrCode(err) != c.ExpectError {
						t.Fatalf("错误码不符: %s", ErrCode(err))
					}
					// 三来源 kind 断言(从向量名/文档推导)
					if strings.Contains(c.Name, "campaign") && h.Kind() != KindCampaign {
						t.Fatalf("kind 应为 campaign,实为 %s", h.Kind())
					}
					if strings.Contains(c.Name, "standalone") && h.Kind() != KindStandalone {
						t.Fatalf("kind 应为 standalone,实为 %s", h.Kind())
					}
					if strings.Contains(c.Name, "order-plain") && h.Kind() != KindOrder {
						t.Fatalf("kind 应为 order,实为 %s", h.Kind())
					}
					// 指纹稳定性:同文档两次解析指纹一致
					fp1, err := h.ContentFingerprint()
					if err != nil {
						t.Fatalf("指纹失败: %v", err)
					}
					h2, err := Parse([]byte(c.Document))
					if err != nil {
						t.Fatalf("二次解析失败: %v", err)
					}
					fp2, _ := h2.ContentFingerprint()
					if fp1 != fp2 {
						t.Fatalf("同文档指纹不稳定")
					}
				} else {
					if err == nil {
						t.Fatalf("应拒绝却接受")
					}
					if c.ExpectError != "" && ErrCode(err) != c.ExpectError {
						t.Fatalf("错误码不符: 期望 %s 实际 %s(%v)", c.ExpectError, ErrCode(err), err)
					}
				}
			case "clock":
				now, err := time.Parse(time.RFC3339, c.Now)
				if err != nil {
					t.Fatalf("向量 now 非法: %v", err)
				}
				if got := IsExpired(now, c.ExpiresAt); got != c.ExpectExpired {
					t.Fatalf("过期判定不符: 期望 %v 实际 %v", c.ExpectExpired, got)
				}
			case "tenant":
				if got := TenantScopeMatches(c.Declared, c.Expected); got != c.ExpectMatch {
					t.Fatalf("租户比对不符: 期望 %v 实际 %v", c.ExpectMatch, got)
				}
			default:
				t.Fatalf("未知向量类型 %q", c.VectorKind)
			}
		})
	}
}

// orderDoc 生成一份合法纯 v1(order)文档。
func orderDoc() map[string]any {
	return map[string]any{
		"schema_version":     "order-handoff/v1",
		"handoff_id":         "handoff-t-1",
		"source_app":         "orders",
		"target_app":         "product-image",
		"principal_id":       "principal-a",
		"order_ref":          "100234",
		"stage_ref":          "30012",
		"brief_version":      "12",
		"source_project_ref": "order-project-a",
		"source_revision":    "rev-1",
		"actor": map[string]any{
			"issuer": "https://identity.example.invalid", "app_id": "orders", "subject": "subject-a",
		},
		"binding":       map[string]any{"binding_ref": "binding-t-1", "proof_digest": strings.Repeat("a", 64)},
		"gating":        map[string]any{"policy_version": "order-gating/v1", "open_gates": []any{}},
		"assets":        []any{map[string]any{"asset_ref": "asset-a", "sha256": strings.Repeat("c", 64), "size_bytes": 10, "media_type": "image/png"}},
		"delivery_spec": map[string]any{"media_type": "image/png", "description": "主图制作"},
		"scopes":        []any{"project.resume", "asset.import"},
		"issued_at":     "2026-09-12T10:00:00Z", "expires_at": "2026-09-12T10:15:00Z",
	}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func wantReject(t *testing.T, raw []byte, code string) {
	t.Helper()
	h, err := Parse(raw)
	if err == nil {
		t.Fatalf("应拒绝却接受: %+v", h)
	}
	if code != "" && ErrCode(err) != code {
		t.Fatalf("错误码不符: 期望 %s 实际 %s(%v)", code, ErrCode(err), err)
	}
}

// TestNegativeCases 负例矩阵(结构/语义/来源纪律)。
func TestNegativeCases(t *testing.T) {
	t.Run("未知字段拒绝", func(t *testing.T) {
		d := orderDoc()
		d["rogue_field"] = "x"
		wantReject(t, marshal(t, d), CodeInvalidDocument)
	})
	t.Run("重复键拒绝", func(t *testing.T) {
		raw := `{"schema_version":"order-handoff/v1","schema_version":"order-handoff/v1"}`
		wantReject(t, []byte(raw), CodeInvalidDocument)
	})
	t.Run("尾随内容拒绝", func(t *testing.T) {
		raw := append(marshal(t, orderDoc()), []byte(" {}")...)
		wantReject(t, raw, CodeInvalidDocument)
	})
	t.Run("null 拒绝", func(t *testing.T) {
		raw := []byte(`{"schema_version":"order-handoff/v1","handoff_id":null}`)
		wantReject(t, raw, CodeInvalidDocument)
	})
	t.Run("未知 schema_version 显式拒绝", func(t *testing.T) {
		d := orderDoc()
		d["schema_version"] = "order-handoff/v0-draft"
		wantReject(t, marshal(t, d), CodeUnsupportedSchemaVersion)
		d2 := orderDoc()
		d2["schema_version"] = "order-handoff/v1.1"
		wantReject(t, marshal(t, d2), CodeUnsupportedSchemaVersion)
	})
	t.Run("扩展文档声明 order 被拒", func(t *testing.T) {
		d := orderDoc()
		delete(d, "order_ref")
		delete(d, "stage_ref")
		d["source_profile"] = map[string]any{
			"profile_version": "source-profile/v1", "source_kind": "order",
			"tenant_scope": "tenant-77", "capabilities": []any{"image.generate"},
			"constraints": []any{}, "return_target_id": "ti-orders-web",
		}
		wantReject(t, marshal(t, d), CodeSourceKindMismatch)
	})
	t.Run("standalone 不得伪造订单(order_ref/stage_ref 出现即拒)", func(t *testing.T) {
		d := orderDoc()
		d["source_profile"] = map[string]any{
			"profile_version": "source-profile/v1", "source_kind": "standalone",
			"tenant_scope": "tenant-77", "capabilities": []any{"image.generate"},
			"constraints": []any{}, "return_target_id": "ti-video-web",
		}
		wantReject(t, marshal(t, d), CodeInvalidDocument)
	})
	t.Run("campaign 缺 campaign_ref 拒绝;standalone 带 campaign_ref 拒绝", func(t *testing.T) {
		d := orderDoc()
		delete(d, "order_ref")
		delete(d, "stage_ref")
		p := map[string]any{
			"profile_version": "source-profile/v1", "source_kind": "campaign",
			"tenant_scope": "tenant-77", "capabilities": []any{"image.generate"},
			"constraints": []any{}, "return_target_id": "ti-campaign-web",
		}
		d["source_profile"] = p
		wantReject(t, marshal(t, d), CodeInvalidDocument)
		p["source_kind"] = "standalone"
		p["campaign_ref"] = "camp-1"
		wantReject(t, marshal(t, d), CodeInvalidDocument)
	})
	t.Run("不支持 profile 版本显式拒绝", func(t *testing.T) {
		d := orderDoc()
		delete(d, "order_ref")
		delete(d, "stage_ref")
		d["source_profile"] = map[string]any{
			"profile_version": "source-profile/v2", "source_kind": "standalone",
			"tenant_scope": "tenant-77", "capabilities": []any{"image.generate"},
			"constraints": []any{}, "return_target_id": "ti-video-web",
		}
		wantReject(t, marshal(t, d), CodeUnsupportedProfile)
	})
	t.Run("actor.app_id 必须等于 source_app", func(t *testing.T) {
		d := orderDoc()
		a := d["actor"].(map[string]any)
		a["app_id"] = "someone-else"
		wantReject(t, marshal(t, d), CodeInvalidDocument)
	})
	t.Run("scopes 封闭枚举与去重", func(t *testing.T) {
		d := orderDoc()
		d["scopes"] = []any{"generation.start"}
		wantReject(t, marshal(t, d), CodeInvalidDocument)
		d2 := orderDoc()
		d2["scopes"] = []any{"project.resume", "project.resume"}
		wantReject(t, marshal(t, d2), CodeInvalidDocument)
	})
	t.Run("时间窗纪律", func(t *testing.T) {
		d := orderDoc()
		d["expires_at"] = "2026-09-12T10:00:00Z"
		wantReject(t, marshal(t, d), CodeInvalidDocument) // issued == expires
		d2 := orderDoc()
		d2["expires_at"] = "2026-09-12T10:15:01Z"
		wantReject(t, marshal(t, d2), CodeInvalidDocument) // >900s
		d3 := orderDoc()
		d3["issued_at"] = "2026-09-12 10:00:00"
		wantReject(t, marshal(t, d3), CodeInvalidDocument)
		d4 := orderDoc()
		d4["expires_at"] = "2026-02-30T10:15:00Z"
		wantReject(t, marshal(t, d4), CodeInvalidDocument) // 假日期
	})
	t.Run("sha256/proof_digest 纪律", func(t *testing.T) {
		d := orderDoc()
		d["binding"] = map[string]any{"binding_ref": "b", "proof_digest": strings.ToUpper(strings.Repeat("a", 64))}
		wantReject(t, marshal(t, d), CodeInvalidDocument)
		d2 := orderDoc()
		assets := d2["assets"].([]any)
		assets[0].(map[string]any)["sha256"] = "zz"
		wantReject(t, marshal(t, d2), CodeInvalidDocument)
	})
	t.Run("全空白字符串拒绝", func(t *testing.T) {
		d := orderDoc()
		d["brief_version"] = "\u00A0\u3000" // 非常规空白(White_Space 集合内)
		wantReject(t, marshal(t, d), CodeInvalidDocument)
	})
	t.Run("嵌套超限拒绝", func(t *testing.T) {
		deep := orderDoc()
		cur := map[string]any{"v": deep}
		for i := 0; i < 20; i++ {
			cur = map[string]any{"v": cur}
		}
		wantReject(t, marshal(t, cur), CodeInvalidDocument)
	})
	t.Run("同 handoff_id 异内容指纹不同", func(t *testing.T) {
		h1, err := Parse(marshal(t, orderDoc()))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		d := orderDoc()
		d["source_revision"] = "rev-2"
		h2, err := Parse(marshal(t, d))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		f1, _ := h1.ContentFingerprint()
		f2, _ := h2.ContentFingerprint()
		if f1 == f2 {
			t.Fatalf("异内容指纹不应相同")
		}
	})
}
