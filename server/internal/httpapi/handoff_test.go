package httpapi

// 跨应用续接与成果回流端点测试(HUI-1745 I1)。
// 覆盖:order/campaign/standalone 三来源接受、降级明确拒绝、负例矩阵
// (错租户/过期/同 handoff 异内容/异 tuple 兑换/旧需求版本回执/重复提交)、
// 接受零生成调用、回执投递(§8 帧验签+幂等+失败重传不追加任务)。

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/receiptdoc"
)

const testSecret = "test-receipt-secret"

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// pngBytes 生成确定性的合法 PNG(可被 png.Decode 打开,仅验证集成用)。
func pngBytes(t *testing.T, w, h int, seed byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: seed, G: seed + 1, B: seed + 2, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("生成的 PNG 必须真实可打开: %v", err)
	}
	return buf.Bytes()
}

// deriveTenant 与身份桩同派生公式:tenant = acct_<sha(account:email)[:16]>。
func deriveTenant(email string) string {
	sum := sha256.Sum256([]byte("account:" + strings.ToLower(email)))
	return "acct_" + hex.EncodeToString(sum[:])[:16]
}

// handoffDoc 构造交接文档(target_app=本应用默认 ID product-image-engine)。
// sourceRef 是来源工程定位:同一来源工程的多次交接(新版需求)必须相同,
// 五元组(tenant+source_app+source_ref+target_app+purpose)才指向同一绑定。
func handoffDoc(id, sourceApp, kind, tenant, sourceRef, bindingRef, rev, brief string) map[string]any {
	d := map[string]any{
		"schema_version":     "order-handoff/v1",
		"handoff_id":         id,
		"source_app":         sourceApp,
		"target_app":         "product-image-engine",
		"principal_id":       "principal-pay-1",
		"brief_version":      brief,
		"source_project_ref": sourceRef,
		"source_revision":    rev,
		"actor": map[string]any{
			"issuer": "https://identity.example.invalid", "app_id": sourceApp, "subject": "subject-1",
		},
		"binding": map[string]any{"binding_ref": bindingRef, "proof_digest": strings.Repeat("b", 64)},
		"gating":  map[string]any{"policy_version": "order-gating/v1", "open_gates": []any{}},
		"assets": []any{map[string]any{
			"asset_ref": "asset_from_" + sourceApp, "sha256": strings.Repeat("c", 64),
			"size_bytes": 2048, "media_type": "image/png",
		}},
		"delivery_spec": map[string]any{"media_type": "image/png", "description": "产品主图需求 " + brief},
		"scopes":        []any{"project.resume", "asset.import", "receipt.write"},
		"issued_at":     time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05Z"),
		"expires_at":    time.Now().UTC().Add(5 * time.Minute).Format("2006-01-02T15:04:05Z"),
	}
	switch kind {
	case "order":
		d["order_ref"] = "100234"
		d["stage_ref"] = "30012"
	case "campaign":
		delete(d, "order_ref")
		delete(d, "stage_ref")
		d["source_profile"] = map[string]any{
			"profile_version": "source-profile/v1", "source_kind": "campaign",
			"tenant_scope": tenant, "capabilities": []any{"image.generate"},
			"constraints": []any{"max_assets:8"}, "return_target_id": "ti-campaign-web",
			"campaign_ref": "camp-2026-091",
		}
	case "standalone":
		delete(d, "order_ref")
		delete(d, "stage_ref")
		d["source_profile"] = map[string]any{
			"profile_version": "source-profile/v1", "source_kind": "standalone",
			"tenant_scope": tenant, "capabilities": []any{"image.generate"},
			"constraints": []any{}, "return_target_id": "ti-campaign-web",
		}
	}
	return d
}

func acceptBody(doc map[string]any, purpose string) map[string]any {
	return map[string]any{"handoff": doc, "purpose": purpose}
}

// mustRegistry 构造测试登记表:orders 的 receipt target 指向 stub(loopback http 白名单)。
func mustRegistry(t *testing.T, ordersReceiptURL string) *appregistry.Manifest {
	t.Helper()
	manifest := fmt.Sprintf(`{
		"manifest_version": "app-registry/v1",
		"apps": [
			{"app_id": "orders", "display_name": "订单", "enabled": true,
			 "supported_source_kinds": ["order"],
			 "capabilities": [{"name": "order.handoff", "menu_visible": true, "requires_billing": false}],
			 "launch_targets": [{"target_id": "ti-orders-web", "kind": "launch", "url": "https://guanlan-order.example.invalid/launch"}],
			 "receipt_targets": [{"target_id": "rc-orders-main", "kind": "receipt", "url": %q}]},
			{"app_id": "campaign-tool", "display_name": "活动", "enabled": false,
			 "supported_source_kinds": ["campaign", "standalone"],
			 "capabilities": [{"name": "image.generate", "menu_visible": false, "requires_billing": true}],
			 "launch_targets": [{"target_id": "ti-campaign-web", "kind": "launch", "url": "https://campaign-tool.example.invalid/launch"}],
			 "receipt_targets": []}
		]
	}`, ordersReceiptURL)
	m, err := appregistry.LoadManifest([]byte(manifest))
	if err != nil {
		t.Fatalf("测试登记表加载失败: %v", err)
	}
	return m
}

// ---- 回执/上传双桩(等价于 E2E 的 eco 桩;仅存在于测试进程) --------------------

type ecoStub struct {
	srv        *httptest.Server
	mu         sync.Mutex
	events     []map[string]any
	seenEvents map[string]string
	uploads    atomic.Int64
	failNext   atomic.Int64
	revoked    map[string]bool
}

func newEcoStub(t *testing.T) *ecoStub {
	s := &ecoStub{seenEvents: map[string]string{}, revoked: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/upload/assets", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SHA256 string `json:"sha256"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.uploads.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"asset_id": "asset_stub_" + body.SHA256[:12]})
	})
	mux.HandleFunc("POST /internal/v1/handoff/receipts", func(w http.ResponseWriter, r *http.Request) {
		if s.failNext.Load() > 0 {
			s.failNext.Add(-1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"stub down"}`))
			return
		}
		raw := readAll(r)
		var doc struct {
			EventID   string `json:"event_id"`
			SourceApp string `json:"source_app"`
			TargetApp string `json:"target_app"`
			Assets    []struct {
				AssetRef string `json:"asset_ref"`
			} `json:"assets"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil || doc.EventID == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// §8 帧验签(与 guanlan-order handoffv1.FrameVerify 同规则)。
		if err := receiptdoc.FrameVerify([]byte(testSecret), r.Header.Get(receiptdoc.HeaderTimestamp),
			r.Header.Get(receiptdoc.HeaderSignature), doc.SourceApp, doc.TargetApp, doc.EventID, raw, time.Now()); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}
		for _, a := range doc.Assets {
			s.mu.Lock()
			rev := s.revoked[a.AssetRef]
			s.mu.Unlock()
			if rev {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"asset_revoked"}`))
				return
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if prev, ok := s.seenEvents[doc.EventID]; ok {
			if prev != string(raw) {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusOK) // 幂等重投:200,不产生第二条
			return
		}
		s.seenEvents[doc.EventID] = string(raw)
		var full map[string]any
		_ = json.Unmarshal(raw, &full)
		s.events = append(s.events, full)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /__control/revoke", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AssetRef string `json:"asset_ref"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.revoked[body.AssetRef] = true
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func readAll(r *http.Request) []byte {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r.Body)
	return buf.Bytes()
}

func (s *ecoStub) eventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// attachRegistry 给 fixture 换登记表并重启 HTTP 服务。
func attachRegistry(t *testing.T, f *fixture, m *appregistry.Manifest) {
	t.Helper()
	f.srv.Registry = m
	f.rearm(t)
}

func errCodeOf(m map[string]any) (string, bool) {
	e, ok := m["error"].(map[string]any)
	if !ok {
		return "", false
	}
	c, _ := e["code"].(string)
	return c, true
}

// ---- 三来源接受 ---------------------------------------------------------------

func TestAcceptHandoffThreeSources(t *testing.T) {
	f := newFixture(t, nil)
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")

	cases := []struct{ kind, sourceApp string }{
		{"order", "orders"}, {"campaign", "campaign-tool"}, {"standalone", "campaign-tool"},
	}
	for i, c := range cases {
		doc := handoffDoc(fmt.Sprintf("h-3src-%d", i), c.sourceApp, c.kind, tenant,
			"src-3src-"+c.kind, "bind-3src-"+c.kind, "rev-1", "brief-1")
		st, resp := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
		if st != 200 {
			t.Fatalf("%s 接受应 200: %d %v", c.kind, st, resp)
		}
		if resp["resolution"] != "created" {
			t.Fatalf("%s 首次接受应 created: %v", c.kind, resp["resolution"])
		}
		proj, _ := resp["project"].(map[string]any)
		if proj["source_type"] != c.kind {
			t.Fatalf("%s 工程 source_type 应为 %s: %v", c.kind, c.kind, proj["source_type"])
		}
		if proj["usage_kind"] != "主图" {
			t.Fatalf("用途应写入工程: %v", proj["usage_kind"])
		}
		// 来源上下文可见:来源/素材/需求版本/付款主体/授权范围。
		ctxm, _ := resp["source_context"].(map[string]any)
		if ctxm["source_app"] != c.sourceApp || ctxm["principal_id"] != "principal-pay-1" {
			t.Fatalf("来源上下文不完整: %v", ctxm)
		}
		scopes, _ := ctxm["scopes"].([]any)
		if len(scopes) != 3 {
			t.Fatalf("授权范围(scopes)应展示: %v", scopes)
		}
		if _, ok := ctxm["assets"].([]any); !ok {
			t.Fatalf("素材应展示: %v", ctxm["assets"])
		}
		snap, _ := resp["snapshot"].(map[string]any)
		if snap["brief_version"] != "brief-1" {
			t.Fatalf("需求版本应展示: %v", snap)
		}
		projID, _ := proj["id"].(string)
		_, detail := f.do(t, "GET", "/api/v1/projects/"+projID, tok, nil)
		inputs, _ := detail["inputs"].([]any)
		if len(inputs) != 1 {
			t.Fatalf("%s 交接素材应挂为输入: %d", c.kind, len(inputs))
		}
	}
	_, list := f.do(t, "GET", "/api/v1/bindings", tok, nil)
	bindings, _ := list["bindings"].([]any)
	if len(bindings) != 3 {
		t.Fatalf("应有 3 条绑定: %v", list)
	}
}

// ---- 负例矩阵 ----------------------------------------------------------------

func TestAcceptHandoffNegatives(t *testing.T) {
	f := newFixture(t, nil)
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")

	t.Run("campaign 错租户 403", func(t *testing.T) {
		doc := handoffDoc("h-neg-tenant", "campaign-tool", "campaign", "tenant-other-99", "src-neg", "bind-neg-1", "rev-1", "brief-1")
		st, resp := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
		if st != 403 {
			t.Fatalf("错租户应 403: %d %v", st, resp)
		}
	})
	t.Run("过期交接 410(垃圾时间 fail-closed)", func(t *testing.T) {
		doc := handoffDoc("h-neg-exp", "orders", "order", tenant, "src-neg", "bind-neg-2", "rev-1", "brief-1")
		doc["expires_at"] = time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05Z")
		st, resp := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
		if st != 410 {
			t.Fatalf("过期应 410: %d %v", st, resp)
		}
		doc2 := handoffDoc("h-neg-garbage", "orders", "order", tenant, "src-neg", "bind-neg-2b", "rev-1", "brief-1")
		doc2["expires_at"] = "not-a-timestamp"
		st, resp = f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc2, "主图"))
		if st != 410 {
			t.Fatalf("垃圾 expires_at 应 fail-closed 410: %d %v", st, resp)
		}
	})
	t.Run("不支持 profile 明确拒绝", func(t *testing.T) {
		doc := handoffDoc("h-neg-v2", "campaign-tool", "standalone", tenant, "src-neg", "bind-neg-3", "rev-1", "brief-1")
		prof := doc["source_profile"].(map[string]any)
		prof["profile_version"] = "source-profile/v2"
		st, resp := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
		if st != 400 {
			t.Fatalf("profile v2 应 400: %d %v", st, resp)
		}
		if code, _ := errCodeOf(resp); code != "unsupported_profile" {
			t.Fatalf("错误码应为 unsupported_profile: %v", resp)
		}
	})
	t.Run("standalone 伪造订单(含 order_ref)拒绝", func(t *testing.T) {
		doc := handoffDoc("h-neg-fake", "campaign-tool", "standalone", tenant, "src-neg", "bind-neg-4", "rev-1", "brief-1")
		doc["order_ref"] = "100234"
		st, _ := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
		if st != 400 {
			t.Fatalf("standalone 含 order_ref 应 400: %d", st)
		}
	})
	t.Run("错目标应用拒绝", func(t *testing.T) {
		doc := handoffDoc("h-neg-target", "orders", "order", tenant, "src-neg", "bind-neg-5", "rev-1", "brief-1")
		doc["target_app"] = "video-tool"
		st, _ := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
		if st != 400 {
			t.Fatalf("错 target_app 应 400: %d", st)
		}
	})
	t.Run("未认证拒绝", func(t *testing.T) {
		doc := handoffDoc("h-neg-auth", "orders", "order", tenant, "src-neg", "bind-neg-6", "rev-1", "brief-1")
		st, _ := f.do(t, "POST", "/api/v1/handoffs/accept", "garbage", acceptBody(doc, "主图"))
		if st != 401 {
			t.Fatalf("无效身份应 401: %d", st)
		}
	})
}

// ---- 幂等绑定 / 快照冲突 / 兑换一次性 ----------------------------------------

func TestAcceptIdempotentBindingSnapshotConflictRedemptionOnce(t *testing.T) {
	f := newFixture(t, nil)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")

	doc := handoffDoc("h-idem-1", "orders", "order", tenant, "src-idem", "bind-idem-1", "rev-1", "brief-1")
	st, r1 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
	if st != 200 || r1["resolution"] != "created" {
		t.Fatalf("首次接受: %d %v", st, r1)
	}
	proj1, _ := r1["project"].(map[string]any)
	st, r2 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
	if st != 200 || r2["resolution"] != "restored" || r2["snapshot_resolution"] != "SNAPSHOT_SAME" {
		t.Fatalf("重放应 restored+SAME: %d %v", st, r2)
	}
	proj2, _ := r2["project"].(map[string]any)
	if proj1["id"] != proj2["id"] {
		t.Fatalf("同 handoff 应恢复同工程: %v vs %v", proj1["id"], proj2["id"])
	}
	// 同 handoff_id 异内容 → 409,保留原快照。
	docC := handoffDoc("h-idem-1", "orders", "order", tenant, "src-idem", "bind-idem-1", "rev-1", "brief-1")
	docC["delivery_spec"].(map[string]any)["description"] = "被篡改的需求"
	st, r3 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(docC, "主图"))
	if st != 409 {
		t.Fatalf("同 handoff 异内容应 409: %d %v", st, r3)
	}
	_, r4 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
	proj4, _ := r4["project"].(map[string]any)
	if proj4["id"] != proj1["id"] {
		t.Fatalf("冲突后原快照/绑定必须保留: %v", r4)
	}
	// 同 binding_ref 异 handoff(异内容)→ 兑换一次性拒绝。
	docD := handoffDoc("h-idem-2", "orders", "order", tenant, "src-idem", "bind-idem-1", "rev-2", "brief-2")
	st, r5 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(docD, "主图"))
	if st != 409 {
		t.Fatalf("同凭据异 tuple 应 409: %d %v", st, r5)
	}
	// 错主体:另一账号盗用同 handoff → 409,无串数据。
	st, pairB := f.login(t, "yi@x.com", "right-pass")
	tokB, _ := pairB["access_token"].(string)
	st, r6 := f.do(t, "POST", "/api/v1/handoffs/accept", tokB, acceptBody(doc, "主图"))
	if st != 409 {
		t.Fatalf("异主体重放同 handoff 应 409: %d %v", st, r6)
	}
	_, listB := f.do(t, "GET", "/api/v1/bindings", tokB, nil)
	bs, _ := listB["bindings"].([]any)
	if len(bs) != 0 {
		t.Fatalf("乙不应看到甲的绑定: %v", listB)
	}
	if st, _ := f.do(t, "GET", "/api/v1/projects/"+proj1["id"].(string), tokB, nil); st != 404 {
		t.Fatalf("乙读甲工程应 404: %d", st)
	}
}

// ---- 新版需求:不可变新快照 + 用户确认采用 + 旧版本回执拒绝 ---------------------

func TestNewRequirementPendingAdoptStaleReceipt(t *testing.T) {
	eco := newEcoStub(t)
	f := newFixture(t, func(c *config.Config) {
		c.UploadBaseURL = eco.srv.URL
		c.UploadToken = "up-tok"
		c.ReceiptKeys = "orders:" + testSecret
	})
	attachRegistry(t, f, mustRegistry(t, eco.srv.URL+"/internal/v1/handoff/receipts"))

	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")

	doc1 := handoffDoc("h-ver-1", "orders", "order", tenant, "src-ver", "bind-ver-1", "rev-1", "brief-1")
	st, r1 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc1, "主图"))
	if st != 200 {
		t.Fatalf("v1 接受失败: %d %v", st, r1)
	}
	proj, _ := r1["project"].(map[string]any)
	projID, _ := proj["id"].(string)
	// 人工编辑(采用新版不得覆盖)。
	if st, _ := f.do(t, "PATCH", "/api/v1/projects/"+projID, tok, map[string]any{"usage_kind": "夏季主图(人工改)"}); st != 200 {
		t.Fatalf("人工编辑失败: %d", st)
	}
	// 登记输出 v1。
	st, o1 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "v1.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 1, 1, 10)),
	})
	if st != 201 {
		t.Fatalf("登记输出应 201: %d %v", st, o1)
	}
	out1, _ := o1["output"].(map[string]any)
	if out1["brief_version"] != "brief-1" {
		t.Fatalf("输出应记录需求版本上下文: %v", out1)
	}
	// v2 需求到达(新 handoff_id + 新凭据:一次性兑换按 binding_ref 计)
	// → pending_snapshot + diff;不自动采用。
	doc2 := handoffDoc("h-ver-2", "orders", "order", tenant, "src-ver", "bind-ver-2", "rev-2", "brief-2")
	st, r2 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc2, "主图"))
	if st != 200 || r2["resolution"] != "restored" {
		t.Fatalf("v2 接受应 restored: %d %v", st, r2)
	}
	if r2["snapshot_resolution"] != "SNAPSHOT_NEW" {
		t.Fatalf("新 handoff_id 应新快照: %v", r2["snapshot_resolution"])
	}
	if r2["pending_snapshot"] == nil || r2["diff"] == nil {
		t.Fatalf("应返回待采用快照与差异: %v", r2)
	}
	// 旧版本输出发回执 → 明确拒绝,无串数据,零投递。
	out1ID, _ := out1["id"].(string)
	st, rr := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out1ID+"/receipt", tok, nil)
	if st != 409 {
		t.Fatalf("旧需求版本回执应 409: %d %v", st, rr)
	}
	if code, _ := errCodeOf(rr); code != "stale_requirement_version" {
		t.Fatalf("错误码应为 stale_requirement_version: %v", rr)
	}
	if eco.eventCount() != 0 {
		t.Fatalf("不应有任何回执投出: %d", eco.eventCount())
	}
	// 用户确认差异后采用:绑定指针移动;人工编辑保留;已选输出不动。
	binding, _ := r2["binding"].(map[string]any)
	bindID, _ := binding["id"].(string)
	pending, _ := r2["pending_snapshot"].(map[string]any)
	snapID, _ := pending["id"].(string)
	st, ad := f.do(t, "POST", "/api/v1/bindings/"+bindID+"/adopt", tok, map[string]any{"snapshot_id": snapID})
	if st != 200 || ad["adopted"] != true {
		t.Fatalf("采用失败: %d %v", st, ad)
	}
	_, detail := f.do(t, "GET", "/api/v1/projects/"+projID, tok, nil)
	projAfter, _ := detail["project"].(map[string]any)
	if projAfter["usage_kind"] != "夏季主图(人工改)" {
		t.Fatalf("采用不得覆盖人工编辑: %v", projAfter["usage_kind"])
	}
	// v2 下重新登记输出 → 回执成功。
	st, o2 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "v2.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 2, 1, 20)),
	})
	if st != 201 {
		t.Fatalf("v2 登记输出应 201: %d %v", st, o2)
	}
	out2, _ := o2["output"].(map[string]any)
	out2ID, _ := out2["id"].(string)
	st, rr2 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out2ID+"/receipt", tok, nil)
	if st != 201 {
		t.Fatalf("v2 回执应 201: %d %v", st, rr2)
	}
	rec, _ := rr2["receipt"].(map[string]any)
	if rec["status"] != "delivered" {
		t.Fatalf("回执应 delivered: %v", rec)
	}
}

// ---- 回执投递:字段/验签/幂等/失败重传/撤销资产/零生成 -------------------------

func TestReceiptDeliveryIdempotentResendRevokedNoGeneration(t *testing.T) {
	var taskCalls atomic.Int64
	taskStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		taskCalls.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"tk_1","status":"queued"}`))
	}))
	defer taskStub.Close()

	eco := newEcoStub(t)
	f := newFixture(t, func(c *config.Config) {
		c.UploadBaseURL = eco.srv.URL
		c.UploadToken = "up-tok"
		c.ReceiptKeys = "orders:" + testSecret
		c.TaskBaseURL = taskStub.URL // 可达:若任何路径误触发生成,计数立刻暴露
		c.TaskToken = "task-tok"
	})
	attachRegistry(t, f, mustRegistry(t, eco.srv.URL+"/internal/v1/handoff/receipts"))

	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")

	doc := handoffDoc("h-rcpt-1", "orders", "order", tenant, "src-rcpt", "bind-rcpt-1", "rev-1", "brief-1")
	st, r1 := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
	proj, _ := r1["project"].(map[string]any)
	projID, _ := proj["id"].(string)

	register := func(name string, seed byte) map[string]any {
		t.Helper()
		st, o := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
			"file_name": name, "content_type": "image/png", "data_b64": b64(pngBytes(t, 3, 2, seed)),
		})
		if st != 201 {
			t.Fatalf("登记输出 %s 应 201: %d %v", name, st, o)
		}
		out, _ := o["output"].(map[string]any)
		return out
	}
	out := register("r1.png", 30)
	outID, _ := out["id"].(string)
	assetRef, _ := out["platform_asset_id"].(string)

	// 同内容重复提交 → 幂等(同输出,不二次登记)。
	st, o1b := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "r1.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 3, 2, 30)),
	})
	if st != 200 || o1b["duplicate"] != true {
		t.Fatalf("同内容重复登记应幂等: %d %v", st, o1b)
	}
	if eco.uploads.Load() != 1 {
		t.Fatalf("同内容只应登记一次: %d", eco.uploads.Load())
	}
	// 回执投递成功。
	st, rc := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+outID+"/receipt", tok, nil)
	if st != 201 {
		t.Fatalf("回执应 201: %d %v", st, rc)
	}
	rec, _ := rc["receipt"].(map[string]any)
	if rec["status"] != "delivered" {
		t.Fatalf("应 delivered: %v", rec)
	}
	if eco.eventCount() != 1 {
		t.Fatalf("stub 应恰好记录一次: %d", eco.eventCount())
	}
	// 字段对照(对端 guanlan-order tool_receipt_events 期望)。
	eco.mu.Lock()
	ev := eco.events[0]
	eco.mu.Unlock()
	if ev["source_app"] != "product-image-engine" || ev["target_app"] != "orders" {
		t.Fatalf("回执方向错误: %v", ev)
	}
	if ev["handoff_id"] != "h-rcpt-1" || ev["principal_id"] != "principal-pay-1" {
		t.Fatalf("回执字段不符: %v", ev)
	}
	if ev["project_ref"] != "src-rcpt" || ev["project_revision"] != "rev-1" || ev["brief_version"] != "brief-1" {
		t.Fatalf("来源版本事实不符: %v", ev)
	}
	if ev["status"] != "succeeded" {
		t.Fatalf("回执状态应 succeeded: %v", ev["status"])
	}
	assets, _ := ev["assets"].([]any)
	a0, _ := assets[0].(map[string]any)
	if a0["asset_ref"] != assetRef {
		t.Fatalf("结果引用应为登记 asset_id: %v vs %v", a0["asset_ref"], assetRef)
	}
	// 红线:payload 不带内部路径/长期下载密钥。
	rawJSON, _ := json.Marshal(ev)
	rawStr := strings.ToLower(string(rawJSON))
	for _, banned := range []string{"/var/lib", "secret", "password", "signature=", "file://", " presigned"} {
		if strings.Contains(rawStr, banned) {
			t.Fatalf("回执不得携带 %q: %s", banned, rawStr)
		}
	}
	// 重复提交回执 → 原回执返回,不产生第二事件。
	st, rc2 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+outID+"/receipt", tok, nil)
	if st != 200 || rc2["duplicate"] != true {
		t.Fatalf("重复回执应幂等返回: %d %v", st, rc2)
	}
	if eco.eventCount() != 1 {
		t.Fatalf("重复提交不应产生第二事件: %d", eco.eventCount())
	}
	// resend(已投递)→ 不重发。
	recID, _ := rec["id"].(string)
	st, rs := f.do(t, "POST", "/api/v1/receipts/"+recID+"/resend", tok, nil)
	if st != 200 || rs["resent"] != false {
		t.Fatalf("已投递无需重发: %d %v", st, rs)
	}
	// 失败场景:stub 置障 → 新输出回执 failed;恢复后 resend 只重传既有成果。
	eco.failNext.Store(1)
	out2 := register("r2.png", 40)
	out2ID, _ := out2["id"].(string)
	st, rc3 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out2ID+"/receipt", tok, nil)
	rec3, _ := rc3["receipt"].(map[string]any)
	if st != 201 || rec3["status"] != "failed" {
		t.Fatalf("置障回执应 failed: %d %v", st, rc3)
	}
	if eco.eventCount() != 1 {
		t.Fatalf("失败投递不应被 stub 记录为事件: %d", eco.eventCount())
	}
	st, rs2 := f.do(t, "POST", "/api/v1/receipts/"+rec3["id"].(string)+"/resend", tok, nil)
	rsRec, _ := rs2["receipt"].(map[string]any)
	if st != 200 || rs2["resent"] != true || rsRec["status"] != "delivered" {
		t.Fatalf("恢复后重传应 delivered: %d %v", st, rs2)
	}
	if eco.eventCount() != 2 {
		t.Fatalf("重传后 stub 应有 2 条: %d", eco.eventCount())
	}
	// 撤销资产:登记→撤销→回执 403 → failed,明确结果无串数据。
	out3 := register("r3.png", 50)
	out3ID, _ := out3["id"].(string)
	asset3, _ := out3["platform_asset_id"].(string)
	revBody, _ := json.Marshal(map[string]string{"asset_ref": asset3})
	revReq, _ := http.NewRequest("POST", eco.srv.URL+"/__control/revoke", bytes.NewReader(revBody))
	revReq.Header.Set("Content-Type", "application/json")
	_, _ = http.DefaultClient.Do(revReq)
	st, rc4 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out3ID+"/receipt", tok, nil)
	rec4, _ := rc4["receipt"].(map[string]any)
	if st != 201 || rec4["status"] != "failed" {
		t.Fatalf("被撤销资产回执应 failed: %d %v", st, rc4)
	}
	if le, _ := rec4["last_error"].(string); !strings.Contains(le, "403") {
		t.Fatalf("失败原因应记录 403: %v", rec4)
	}
	if taskCalls.Load() != 0 {
		t.Fatalf("全程不得有任何生成任务调用: %d", taskCalls.Load())
	}
}

// ---- 接受交接绝不调用收费模型 -------------------------------------------------

func TestAcceptNeverCallsGeneration(t *testing.T) {
	var taskCalls atomic.Int64
	taskStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		taskCalls.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"tk_1","status":"queued"}`))
	}))
	defer taskStub.Close()

	f := newFixture(t, func(c *config.Config) {
		c.GenerationEnabled = true // 开关全开:接受交接也不得触发
		c.TaskBaseURL = taskStub.URL
		c.TaskToken = "task-tok"
	})
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")
	doc := handoffDoc("h-nogen-1", "orders", "order", tenant, "src-nogen", "bind-nogen-1", "rev-1", "brief-1")
	if st, resp := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图")); st != 200 {
		t.Fatalf("接受应 200: %d %v", st, resp)
	}
	if taskCalls.Load() != 0 {
		t.Fatalf("接受交接绝不能调用生成任务: %d 次", taskCalls.Load())
	}
	// 开关 off 时提交生成 → 503(I0 语义保持:生成开关独立)。
	f2 := newFixture(t, nil)
	st, pair2 := f2.login(t, "jia@x.com", "right-pass")
	tok2, _ := pair2["access_token"].(string)
	created := createProject(t, f2, tok2)
	st, resp := f2.do(t, "POST", "/api/v1/projects/"+created+"/versions", tok2, map[string]any{
		"platform_asset_id": "asset_x", "usage_note": "n",
	})
	if st != 503 {
		t.Fatalf("开关 off 提交生成应 503: %d %v", st, resp)
	}
}

func createProject(t *testing.T, f *fixture, tok string) string {
	t.Helper()
	st, resp := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{
		"name": "独立工程", "usage_kind": "主图", "source_type": "standalone",
	})
	if st != 201 {
		t.Fatalf("创建工程: %d %v", st, resp)
	}
	proj, _ := resp["project"].(map[string]any)
	id, _ := proj["id"].(string)
	return id
}

// ---- standalone 无绑定回执拒绝 + upload 未配置/非 PNG ---------------------------

func TestStandaloneReceiptRejectedAndUploadGate(t *testing.T) {
	f := newFixture(t, nil) // upload 未配置(指向 dead 地址)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	projID := createProject(t, f, tok)
	st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "x.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 1, 1, 1)),
	})
	if st != 503 {
		t.Fatalf("upload 未配置应 503: %d %v", st, resp)
	}
	if st, _ := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "x.bin", "content_type": "application/octet-stream", "data_b64": b64([]byte("not a png")),
	}); st != 400 {
		t.Fatalf("非 PNG 应 400: %d", st)
	}
	// 装配 upload 桩后登记输出;standalone 无绑定 → 回执 409。
	eco := newEcoStub(t)
	f.srv.Uploads = &platform.UploadClient{BaseURL: eco.srv.URL, AppID: "product-image", Token: "up-tok"}
	f.rearm(t)
	st, o := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "x.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 1, 1, 2)),
	})
	if st != 201 {
		t.Fatalf("登记输出应 201: %d %v", st, o)
	}
	out, _ := o["output"].(map[string]any)
	outID, _ := out["id"].(string)
	st, rr := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+outID+"/receipt", tok, nil)
	if st != 409 {
		t.Fatalf("standalone 回执应 409: %d %v", st, rr)
	}
	if code, _ := errCodeOf(rr); code != "no_source_binding" {
		t.Fatalf("错误码应为 no_source_binding: %v", rr)
	}
}

// ---- 重启恢复(独立入口回归) ---------------------------------------------------

func TestBindingsSurviveRestart(t *testing.T) {
	f := newFixture(t, nil)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")
	doc := handoffDoc("h-restart-1", "orders", "order", tenant, "src-restart", "bind-restart-1", "rev-1", "brief-1")
	if st, resp := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图")); st != 200 {
		t.Fatalf("接受失败: %d %v", st, resp)
	}
	// 模拟重启:同 store 重开(不重开 SQLite,等价验证数据层持久)后重建服务。
	f.rearm(t)
	st, pair2 := f.login(t, "jia@x.com", "right-pass")
	tok2, _ := pair2["access_token"].(string)
	_, list := f.do(t, "GET", "/api/v1/bindings", tok2, nil)
	bs, _ := list["bindings"].([]any)
	if len(bs) != 1 {
		t.Fatalf("重启+重登后绑定必须仍在: %v", list)
	}
	b, _ := bs[0].(map[string]any)
	bindID, _ := b["id"].(string)
	st, agg := f.do(t, "GET", "/api/v1/bindings/"+bindID, tok2, nil)
	if st != 200 {
		t.Fatalf("绑定聚合: %d %v", st, agg)
	}
	ctxm, _ := agg["source_context"].(map[string]any)
	if ctxm == nil || ctxm["principal_id"] != "principal-pay-1" {
		t.Fatalf("来源上下文必须恢复: %v", agg)
	}
}

// hmacSelfCheck §8 帧签名串构造与对端实现一致性自检。
func TestHMACFrameMatchesCounterpart(t *testing.T) {
	secret := []byte("k")
	ts := time.Unix(1700000000, 0).Unix()
	raw := []byte(`{"a":1}`)
	sig := receiptdoc.FrameSign(secret, ts, "product-image-engine", "orders", "evt-1", raw)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(fmt.Sprintf("%d\n%s\n%s\n%s\n", ts, "product-image-engine", "orders", "evt-1")))
	_, _ = mac.Write(raw)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Fatalf("§8 帧签名与对端语义不一致: %s vs %s", sig, want)
	}
	if err := receiptdoc.FrameVerify(secret, fmt.Sprint(ts), sig, "product-image-engine", "orders", "evt-1", raw, time.Unix(ts, 0).Add(100*time.Second)); err != nil {
		t.Fatal("自验签应通过: " + err.Error())
	}
	if err := receiptdoc.FrameVerify(secret, fmt.Sprint(ts-301), sig, "product-image-engine", "orders", "evt-1", raw, time.Unix(ts, 0)); err == nil {
		t.Fatal("超 300s 偏差应拒绝")
	}
}
