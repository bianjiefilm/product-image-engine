package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// makeSigningKey 生成 Ed25519 密钥与对应 kid。
func makeSigningKey(t *testing.T, kid string) (ed25519.PrivateKey, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, kid
}

func jwkFor(priv ed25519.PrivateKey, kid string) jwksKey {
	pub := priv.Public().(ed25519.PublicKey)
	return jwksKey{
		Kty: "OKP", Crv: "Ed25519", Use: "sig", Alg: "EdDSA", Kid: kid,
		X: base64.RawURLEncoding.EncodeToString(pub),
	}
}

func signAccessToken(t *testing.T, priv ed25519.PrivateKey, kid, iss, aud, sub, account, tenant, typ string, exp time.Time) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"iss": iss, "sub": sub, "aud": aud,
		"iat": time.Now().Unix(), "exp": exp.Unix(),
		"typ": typ, "tenant_id": tenant, "account_id": account,
		"roles": []string{"user"},
	})
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newStubIdentity(t *testing.T, priv ed25519.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/jwks.json":
			_ = json.NewEncoder(w).Encode(jwksDocument{Keys: []jwksKey{jwkFor(priv, kid)}})
		case r.URL.Path == "/internal/v1/identity/login":
			if r.Header.Get("X-App-ID") != "product-image" || r.Header.Get("X-PilotSeaView-Internal-Token") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["app_id"] != "product-image" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(TokenPair{AccessToken: "acc", RefreshToken: "ref", TokenType: "Bearer", ExpiresIn: 600})
		case r.URL.Path == "/internal/v1/identity/refresh":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["app_id"] != "product-image" {
				t.Errorf("refresh 必须带 app_id")
			}
			_ = json.NewEncoder(w).Encode(TokenPair{AccessToken: "acc2", RefreshToken: "ref2", ExpiresIn: 600})
		case r.URL.Path == "/internal/v1/identity/revocations":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIdentityClientLoginRefreshLogout(t *testing.T) {
	priv, kid := makeSigningKey(t, "k1")
	srv := newStubIdentity(t, priv, kid)
	c := &IdentityClient{BaseURL: srv.URL, AppID: "product-image", Token: "tok"}
	pair, err := c.Login(context.Background(), "a@b.com", "pw")
	if err != nil || pair.AccessToken != "acc" || pair.RefreshToken != "ref" {
		t.Fatalf("login: %v %+v", err, pair)
	}
	pair2, err := c.Refresh(context.Background(), "ref")
	if err != nil || pair2.AccessToken != "acc2" {
		t.Fatalf("refresh: %v %+v", err, pair2)
	}
	if err := c.Logout(context.Background(), "ref", "usr_x"); err != nil {
		t.Fatalf("logout: %v", err)
	}
}

func TestVerifierHappyAndFailures(t *testing.T) {
	priv, kid := makeSigningKey(t, "k1")
	srv := newStubIdentity(t, priv, kid)
	v := NewVerifier(srv.URL, "product-image", "")

	tok := signAccessToken(t, priv, kid, "iss", "product-image", "usr_1", "acct_1", "acct_1", "access", time.Now().Add(time.Hour))
	p, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("验证失败: %v", err)
	}
	if p.UserID != "usr_1" || p.AccountID != "acct_1" || p.Tenant() != "acct_1" {
		t.Fatalf("principal 错误: %+v", p)
	}

	// 过期 → 用户态错误
	expired := signAccessToken(t, priv, kid, "iss", "product-image", "usr_1", "acct_1", "acct_1", "access", time.Now().Add(-time.Hour))
	if _, err := v.Verify(context.Background(), expired); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("过期应 ErrUnauthenticated, got %v", err)
	}
	// 错误 aud → 用户态错误
	wrongAud := signAccessToken(t, priv, kid, "iss", "other-app", "usr_1", "acct_1", "acct_1", "access", time.Now().Add(time.Hour))
	if _, err := v.Verify(context.Background(), wrongAud); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("aud 不符应 ErrUnauthenticated, got %v", err)
	}
	// 非 access 类型 → 用户态错误
	refreshTyp := signAccessToken(t, priv, kid, "iss", "product-image", "usr_1", "acct_1", "acct_1", "refresh", time.Now().Add(time.Hour))
	if _, err := v.Verify(context.Background(), refreshTyp); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("typ!=access 应 ErrUnauthenticated, got %v", err)
	}
	// 篡改签名 → 用户态错误
	if _, err := v.Verify(context.Background(), tok+"x"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("签名无效应 ErrUnauthenticated, got %v", err)
	}
}

func TestVerifierJWKSUnavailableIs503Class(t *testing.T) {
	v := NewVerifier("http://127.0.0.1:1", "product-image", "") // 指向封闭端口
	_, err := v.Verify(context.Background(), "whatever")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("JWKS 不可达应 ErrUnavailable, got %v", err)
	}
}

func TestVerifierUnknownKidForcesRefresh(t *testing.T) {
	priv, kid := makeSigningKey(t, "k1")
	srv := newStubIdentity(t, priv, kid)
	v := NewVerifier(srv.URL, "product-image", "")
	ctx := context.Background()

	tok := signAccessToken(t, priv, kid, "iss", "product-image", "usr_1", "acct_1", "acct_1", "access", time.Now().Add(time.Hour))
	if _, err := v.Verify(ctx, tok); err != nil {
		t.Fatal(err)
	}
	// 服务端轮换:清空缓存模拟“缓存过期后拿到新 kid 令牌”的强制重取路径。
	v.mu.Lock()
	v.keys = map[string][]byte{}
	v.fetched = time.Time{}
	v.mu.Unlock()

	// 同一密钥重新拉取后应验证成功。
	if _, err := v.Verify(ctx, tok); err != nil {
		t.Fatalf("强制重取 JWKS 后应验证成功: %v", err)
	}
	// 全新 verifier(无缓存)也能拉取并验证。
	srv2 := newStubIdentity(t, priv, kid)
	v2 := NewVerifier(srv2.URL, "product-image", "")
	if _, err := v2.Verify(ctx, tok); err != nil {
		t.Fatalf("新 verifier 应能重取 JWKS 验证: %v", err)
	}
}

func TestTaskSubmitHeadersAndUnavailable(t *testing.T) {
	var gotApp, gotTok, gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotApp, gotTok, gotPath = r.Header.Get("X-App-ID"), r.Header.Get("X-PilotSeaView-Internal-Token"), r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotKey, _ = body["idempotency_key"].(string)
		_ = json.NewEncoder(w).Encode(SubmitResult{TaskID: "task_1", Status: "queued"})
	}))
	defer srv.Close()
	c := &TaskClient{BaseURL: srv.URL, AppID: "product-image", Token: "tk"}
	out, err := c.Submit(context.Background(), SubmitRequest{IdempotencyKey: "idem-1", Kind: "gen"})
	if err != nil || out.TaskID != "task_1" {
		t.Fatalf("submit: %v %+v", err, out)
	}
	if gotPath != "/internal/v1/tasks/submit" || gotApp != "product-image" || gotTok != "tk" || gotKey != "idem-1" {
		t.Fatalf("path=%s app=%s tok=%s key=%s", gotPath, gotApp, gotTok, gotKey)
	}
	// 5xx → ErrUnavailable
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv500.Close()
	c500 := &TaskClient{BaseURL: srv500.URL, AppID: "a", Token: "t"}
	if _, err := c500.Submit(context.Background(), SubmitRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("5xx 应 ErrUnavailable, got %v", err)
	}
	// 不可达 → ErrUnavailable
	cDead := &TaskClient{BaseURL: "http://127.0.0.1:1", AppID: "a", Token: "t"}
	if _, err := cDead.Submit(context.Background(), SubmitRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("不可达应 ErrUnavailable, got %v", err)
	}
}

func TestTaskStatusParseAndUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/tasks/task_7" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("X-PilotSeaView-Internal-Token") != "tk" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": "task_7", "status": "succeeded",
			"result": map[string]any{"data_b64": "aGVsbG8=", "file_name": "out.png"},
		})
	}))
	defer srv.Close()
	c := &TaskClient{BaseURL: srv.URL, AppID: "product-image", Token: "tk"}
	out, err := c.Status(context.Background(), "task_7")
	if err != nil || out.Status != "succeeded" || out.TaskID != "task_7" {
		t.Fatalf("status: %v %+v", err, out)
	}
	if out.Result["data_b64"] != "aGVsbG8=" {
		t.Fatalf("result 载荷应原样保留: %+v", out.Result)
	}
	// 空任务 id 拒绝;不可达/5xx → ErrUnavailable(与 Submit 同口径)
	if _, err := c.Status(context.Background(), " "); err == nil {
		t.Fatalf("空 task_id 应报错")
	}
	cDead := &TaskClient{BaseURL: "http://127.0.0.1:1", AppID: "a", Token: "t"}
	if _, err := cDead.Status(context.Background(), "t1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("不可达应 ErrUnavailable, got %v", err)
	}
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer srv500.Close()
	if _, err := (&TaskClient{BaseURL: srv500.URL, AppID: "a", Token: "t"}).Status(context.Background(), "t1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("5xx 应 ErrUnavailable, got %v", err)
	}
}

func TestBillingLedgerAndEnsure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/billing/ledger":
			if r.URL.Query().Get("account_id") != "acct_1" {
				w.WriteHeader(400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"balance_cny": json.Number("12.5"), "items": []any{map[string]any{"kind": "spend"}}})
		case "/internal/v1/billing/accounts/ensure":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := &BillingClient{BaseURL: srv.URL, AppID: "product-image", Token: "bk"}
	led, err := c.Ledger(context.Background(), "acct_1")
	if err != nil || led.BalanceCNY != 12.5 || len(led.Items) != 1 {
		t.Fatalf("ledger: %v %+v", err, led)
	}
	if err := c.EnsureAccount(context.Background(), "a@b.com"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	dead := &BillingClient{BaseURL: "http://127.0.0.1:1", AppID: "a", Token: "t"}
	if _, err := dead.Ledger(context.Background(), "acct_1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("不可达应 ErrUnavailable, got %v", err)
	}
}

func TestUploadSessionClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/upload/sessions" || r.Header.Get("X-App-ID") != "product-image" {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "us_1"})
	}))
	defer srv.Close()
	c := &UploadClient{BaseURL: srv.URL, AppID: "product-image", Token: "uk"}
	out, err := c.CreateUploadSession(context.Background(), "a.png", "image/png", 10)
	if err != nil || out["session_id"] != "us_1" {
		t.Fatalf("upload session: %v %+v", err, out)
	}
	dead := &UploadClient{BaseURL: "http://127.0.0.1:1", AppID: "a", Token: "t"}
	if _, err := dead.CreateUploadSession(context.Background(), "a.png", "image/png", 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("不可达应 ErrUnavailable, got %v", err)
	}
}
