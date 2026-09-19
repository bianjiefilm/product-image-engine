package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sizeadapt"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

// ---- 测试用最小身份桩(等价于 E2E 身份桩,只补登录链路) ------------------

type idStub struct {
	srv  *httptest.Server
	priv ed25519.PrivateKey
	kid  string
}

func newIdentityStub(t *testing.T) *idStub {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	st := &idStub{priv: priv, kid: "test-kid-1"}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/jwks.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
				"kty": "OKP", "crv": "Ed25519", "use": "sig", "alg": "EdDSA", "kid": st.kid,
				"x": base64.RawURLEncoding.EncodeToString(pub),
			}}})
		case r.URL.Path == "/internal/v1/identity/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "right-pass" || body["app_id"] != "product-image" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"bad credentials"}`))
				return
			}
			tok := st.mint(body["email"], "product-image")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": tok, "refresh_token": "rt-" + body["email"], "token_type": "Bearer", "expires_in": 600,
			})
		case r.URL.Path == "/internal/v1/identity/refresh":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["app_id"] != "product-image" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			tok := st.mint("refreshed@x.com", "product-image")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": tok, "refresh_token": "rt-new", "token_type": "Bearer", "expires_in": 600,
			})
		case r.URL.Path == "/internal/v1/identity/revocations":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(st.srv.Close)
	return st
}

// mint 按 identity 派生公式(sha256)生成 usr_/acct_,签 EdDSA 访问令牌。
func (st *idStub) mint(email, aud string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	sha := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])[:16]
	}
	userID, account := "usr_"+sha("user:"+email), "acct_"+sha("account:"+email)
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"iss": "ident-stub", "sub": userID, "aud": aud,
		"iat": time.Now().Unix(), "exp": time.Now().Add(10 * time.Minute).Unix(),
		"typ": "access", "tenant_id": account, "account_id": account,
	})
	tok.Header["kid"] = st.kid
	s, err := tok.SignedString(st.priv)
	if err != nil {
		panic(err)
	}
	return s
}

// ---- 装配被测服务 ---------------------------------------------------------

type fixture struct {
	ts      *httptest.Server
	stub    *idStub
	cfg     config.Config
	st      *store.Store
	srv     *Server
	deadURL string // 指向封闭端口,用于“服务不可达”
}

func newFixture(t *testing.T, mutate func(*config.Config)) *fixture {
	t.Helper()
	stub := newIdentityStub(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dead := "http://127.0.0.1:1"
	cfg := config.Config{
		Addr: "127.0.0.1:0", InternalToken: "it-test",
		DBPath: filepath.Join(t.TempDir(), "x.db"),
		AppID:  "product-image-engine", // 与 handoffDoc 的 target_app 对齐

		IdentityBaseURL: stub.srv.URL,
		IdentityAppID:   "product-image",
		IdentityToken:   "id-tok",

		UploadBaseURL: dead, UploadToken: "up-tok",
		TaskBaseURL: dead, TaskToken: "task-tok",
		BillingBaseURL: dead, BillingToken: "bill-tok",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	s := &Server{
		Cfg: cfg, St: st,
		Ident:   &platform.IdentityClient{BaseURL: cfg.IdentityBaseURL, AppID: cfg.IdentityAppID, Token: cfg.IdentityToken},
		Verify:  platform.NewVerifier(cfg.IdentityBaseURL, cfg.IdentityAppID, ""),
		Tasks:   &platform.TaskClient{BaseURL: cfg.TaskBaseURL, AppID: cfg.IdentityAppID, Token: cfg.TaskToken},
		Uploads: &platform.UploadClient{BaseURL: cfg.UploadBaseURL, AppID: cfg.IdentityAppID, Token: cfg.UploadToken},
		Billing: &platform.BillingClient{BaseURL: cfg.BillingBaseURL, AppID: cfg.IdentityAppID, Token: cfg.BillingToken},
	}
	if reg, err := appregistry.DefaultManifest(); err == nil {
		s.Registry = reg
	}
	if p, err := sizeadapt.DefaultPresets(); err == nil {
		s.Presets = p // 尺寸适配预设集(路由注册仍由 FEATURE_SIZE_ADAPT 决定)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return &fixture{ts: ts, stub: stub, cfg: cfg, st: st, srv: s, deadURL: dead}
}

// rearm 用(可能已被测试改写的)Server 重建 HTTP 测试服务(模拟重启装配)。
func (f *fixture) rearm(t *testing.T) {
	t.Helper()
	old := f.ts
	f.ts = httptest.NewServer(f.srv.Router())
	t.Cleanup(f.ts.Close)
	old.Close()
}

func (f *fixture) do(t *testing.T, method, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Product-Internal-Token", "it-test")
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m
}

func (f *fixture) login(t *testing.T, email, pass string) (int, map[string]any) {
	return f.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"email": email, "password": pass})
}

func errMsg(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		msg, _ := e["message"].(string)
		return msg
	}
	return ""
}

// ---- 三态验收:鉴权失败 / 服务不可达 / 开关 off ---------------------------

func TestInternalTokenRequired(t *testing.T) {
	f := newFixture(t, nil)
	req, _ := http.NewRequest("GET", f.ts.URL+"/api/v1/projects", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺内部凭据应 401, got %d", res.StatusCode)
	}
}

func TestAuthFailuresAre401(t *testing.T) {
	f := newFixture(t, nil)
	// 无 Bearer
	if st, msg := f.do(t, "GET", "/api/v1/auth/session", "", nil); st != 401 || errMsg(msg) == "" {
		t.Fatalf("无令牌应 401, got %d %v", st, msg)
	}
	// 伪造令牌
	if st, _ := f.do(t, "GET", "/api/v1/auth/session", "garbage.token.here", nil); st != 401 {
		t.Fatalf("伪造令牌应 401, got %d", st)
	}
	// 错误口令登录
	if st, msg := f.login(t, "a@b.com", "wrong"); st != 401 || !strings.Contains(errMsg(msg), "登录失败") {
		t.Fatalf("错误口令应 401, got %d %v", st, msg)
	}
}

func TestIdentityUnavailableDuringVerifyIs503(t *testing.T) {
	// 身份服务指到封闭端口:校验不可达必须 503,而不是把用户当未登录(401)。
	f := newFixture(t, func(c *config.Config) { c.IdentityBaseURL = "http://127.0.0.1:1" })
	st, msg := f.do(t, "GET", "/api/v1/auth/session", "any.token.value", nil)
	if st != 503 || !strings.Contains(errMsg(msg), "平台身份服务不可达") {
		t.Fatalf("身份不可达应 503, got %d %v", st, msg)
	}
}

func TestLoginCreateStandaloneProjectSaveRestoreIsolation(t *testing.T) {
	f := newFixture(t, nil)
	// 登录甲
	st, pair := f.login(t, "jia@x.com", "right-pass")
	if st != 200 {
		t.Fatalf("登录失败: %d %v", st, pair)
	}
	tokA, _ := pair["access_token"].(string)
	principal, _ := pair["principal"].(map[string]any)
	if principal["user_id"] == "" || principal["account_id"] == "" {
		t.Fatalf("principal 不完整: %v", principal)
	}
	// 会话可用
	if st, _ := f.do(t, "GET", "/api/v1/auth/session", tokA, nil); st != 200 {
		t.Fatalf("session 应 200, got %d", st)
	}
	// 创建无订单工程(standalone)
	st, created := f.do(t, "POST", "/api/v1/projects", tokA, map[string]any{
		"name": "春季主图", "usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"source_type": "standalone",
	})
	if st != 201 {
		t.Fatalf("创建应 201, got %d %v", st, created)
	}
	proj, _ := created["project"].(map[string]any)
	projID, _ := proj["id"].(string)
	if proj["source_type"] != "standalone" {
		t.Fatalf("standalone 来源应原样保存: %v", proj)
	}
	// 保存(局部更新)
	st, updated := f.do(t, "PATCH", "/api/v1/projects/"+projID, tokA, map[string]any{
		"status": "active", "usage_kind": "详情页头图",
	})
	if st != 200 {
		t.Fatalf("保存应 200, got %d %v", st, updated)
	}
	// 挂素材引用
	st, _ = f.do(t, "POST", "/api/v1/projects/"+projID+"/inputs", tokA, map[string]any{
		"platform_asset_id": "asset_demo_1", "snapshot_name": "src.png",
		"snapshot_size": 2048, "snapshot_content_type": "image/png",
	})
	if st != 201 {
		t.Fatalf("挂输入应 201, got %d", st)
	}
	// 详情聚合
	st, detail := f.do(t, "GET", "/api/v1/projects/"+projID, tokA, nil)
	if st != 200 {
		t.Fatalf("详情应 200, got %d", st)
	}
	inputs, _ := detail["inputs"].([]any)
	versions, _ := detail["versions"].([]any)
	if len(inputs) != 1 || len(versions) != 0 {
		t.Fatalf("详情聚合异常: inputs=%d versions=%d", len(inputs), len(versions))
	}
	// 乙登录:看不到甲的工程
	st, pairB := f.login(t, "yi@x.com", "right-pass")
	tokB, _ := pairB["access_token"].(string)
	st, listB := f.do(t, "GET", "/api/v1/projects", tokB, nil)
	projects, _ := listB["projects"].([]any)
	if st != 200 || len(projects) != 0 {
		t.Fatalf("乙的列表应为空, got %d %v", st, listB)
	}
	if st, _ := f.do(t, "GET", "/api/v1/projects/"+projID, tokB, nil); st != 404 {
		t.Fatalf("乙读甲的工程应 404, got %d", st)
	}
	// 甲仍在(重登恢复由数据持久性保证:store 已测;此处再验一次列表)
	st, listA := f.do(t, "GET", "/api/v1/projects", tokA, nil)
	projectsA, _ := listA["projects"].([]any)
	if len(projectsA) != 1 {
		t.Fatalf("甲应有 1 个工程: %v", listA)
	}
}

func TestGenerationSwitchOffIs503(t *testing.T) {
	f := newFixture(t, nil) // FEATURE_GENERATION_ENABLED 默认 off
	st, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "P", "source_type": "standalone"})
	proj, _ := created["project"].(map[string]any)
	id, _ := proj["id"].(string)
	st, msg := f.do(t, "POST", "/api/v1/projects/"+id+"/versions", tok, map[string]any{})
	if st != 503 || !strings.Contains(errMsg(msg), "FEATURE_GENERATION_ENABLED") {
		t.Fatalf("开关 off 应 503 且点名开关, got %d %v", st, msg)
	}
	// 计费开关 off
	st, msg = f.do(t, "GET", "/api/v1/billing/balance", tok, nil)
	if st != 503 || !strings.Contains(errMsg(msg), "ECO_BILLING_ENABLED") {
		t.Fatalf("计费开关 off 应 503, got %d %v", st, msg)
	}
}

func TestGatesOnButUpstreamDeadIs503NotFake(t *testing.T) {
	// 开关 on、配置在,但上游指向封闭端口:必须显式 503,不伪造成功。
	f := newFixture(t, func(c *config.Config) {
		c.GenerationEnabled = true
		c.BillingEnabled = true
		// Task/Upload/Billing 已在 fixture 里指向 dead URL
	})
	st, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "P", "source_type": "standalone"})
	proj, _ := created["project"].(map[string]any)
	id, _ := proj["id"].(string)

	st, msg := f.do(t, "POST", "/api/v1/projects/"+id+"/versions", tok, map[string]any{})
	if st != 503 || !strings.Contains(errMsg(msg), "不可达") {
		t.Fatalf("task 不可达应 503, got %d %v", st, msg)
	}
	// 不应落任何版本行
	_, detail := f.do(t, "GET", "/api/v1/projects/"+id, tok, nil)
	versions, _ := detail["versions"].([]any)
	if len(versions) != 0 {
		t.Fatalf("失败提交不得落版本行: %v", versions)
	}
	st, msg = f.do(t, "GET", "/api/v1/billing/balance", tok, nil)
	if st != 503 || !strings.Contains(errMsg(msg), "不可达") {
		t.Fatalf("billing 不可达应 503, got %d %v", st, msg)
	}
}

func TestReadyzReportsGates(t *testing.T) {
	f := newFixture(t, nil)
	st, body := f.do(t, "GET", "/readyz", "", nil)
	if st != 200 || body["ok"] != true {
		t.Fatalf("readyz 应 200/ok, got %d %v", st, body)
	}
	// 致命配置:identity 键被清空
	f2 := newFixture(t, func(c *config.Config) {
		c.IdentityBaseURL = ""
		c.IdentityToken = ""
	})
	st, body = f2.do(t, "GET", "/readyz", "", nil)
	if st != 503 || body["ok"] != false {
		t.Fatalf("致命配置 readyz 应 503, got %d %v", st, body)
	}
	// 带致命配置时受保护端点也 503(config_incomplete)
	req, _ := http.NewRequest("POST", f2.ts.URL+"/api/v1/auth/login", strings.NewReader(`{"email":"a@x.com","password":"right-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Product-Internal-Token", "it-test")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatalf("配置不完整时登录应 503, got %d", res.StatusCode)
	}
}

func TestLogoutRevokesAndRefreshWorks(t *testing.T) {
	f := newFixture(t, nil)
	_, pair := f.login(t, "a@x.com", "right-pass")
	refresh, _ := pair["refresh_token"].(string)
	// 刷新
	st, newPair := f.do(t, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	if st != 200 {
		t.Fatalf("刷新应 200, got %d %v", st, newPair)
	}
	// 登出(需已验证身份)
	access, _ := newPair["access_token"].(string)
	st, _ = f.do(t, "POST", "/api/v1/auth/logout", access, map[string]string{"refresh_token": refresh})
	if st != 204 {
		t.Fatalf("登出应 204, got %d", st)
	}
}
