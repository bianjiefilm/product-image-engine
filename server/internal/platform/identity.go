// Package platform 封装对 public-ai 平台服务的调用。
// 约定(见 public-ai docs/integration-guide.md + examples/bff-identity-billing):
//   - 每个请求带 X-App-ID + X-PilotSeaView-Internal-Token(本 app 在该服务的专用 token);
//   - 禁止 legacy 共享 PLATFORM_INTERNAL_TOKEN 路径;
//   - identity JWT 为 EdDSA(Ed25519),claims 含 sub/aud/tenant_id/account_id/typ,
//     JWKS 为 OKP(Ed25519),位于 /.well-known/jwks.json。
package platform

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const internalTokenHeader = "X-PilotSeaView-Internal-Token"

// 错误分类:调用方据此映射 401(身份无效)或 503(平台不可用/未配置)。
var (
	// ErrUnauthenticated 令牌缺失、无效或已过期——属于用户态问题。
	ErrUnauthenticated = errors.New("platform: 登录状态无效或已过期")
	// ErrUnavailable 平台服务不可达或返回 5xx——属于平台侧问题,绝不伪造成功。
	ErrUnavailable = errors.New("platform: 平台服务不可达")
)

// TokenPair 与 platform-identity 应答一致。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// Principal 由已验证 JWT 提取的平台身份事实。
type Principal struct {
	UserID    string   `json:"user_id"`    // sub(usr_…)
	AccountID string   `json:"account_id"` // acct_…
	TenantID  string   `json:"tenant_id"`  // 可空
	Roles     []string `json:"roles,omitempty"`
}

// Tenant 返回本产品的租户作用域:tenant_id 优先,空则回退 account_id。
func (p Principal) Tenant() string {
	if p.TenantID != "" {
		return p.TenantID
	}
	return p.AccountID
}

// IdentityClient 调用 platform-identity 的内部端点。
type IdentityClient struct {
	BaseURL string // 例:http://127.0.0.1:18101
	AppID   string // aud
	Token   string // IDENTITY_APP_TOKENS 中本 app 专用 token
	HTTP    *http.Client
}

func (c *IdentityClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *IdentityClient) postJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-App-ID", c.AppID)
	req.Header.Set(internalTokenHeader, c.Token)
	res, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: identity %s: %v", ErrUnavailable, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return fmt.Errorf("%w: identity %s: status %d", ErrUnavailable, path, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("identity %s: status %d: %s", path, res.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// Login 用邮箱+口令换 TokenPair(app_id 由 client 注入,即 JWT aud)。
func (c *IdentityClient) Login(ctx context.Context, email, password string) (TokenPair, error) {
	var out TokenPair
	err := c.postJSON(ctx, "/internal/v1/identity/login", map[string]string{
		"email": email, "password": password, "app_id": c.AppID,
	}, &out)
	return out, err
}

// Refresh 刷新令牌;refresh 必须带 app_id(平台红线)。
func (c *IdentityClient) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	var out TokenPair
	err := c.postJSON(ctx, "/internal/v1/identity/refresh", map[string]string{
		"refresh_token": refreshToken, "app_id": c.AppID,
	}, &out)
	return out, err
}

// Logout 吊销刷新令牌。
func (c *IdentityClient) Logout(ctx context.Context, refreshToken, userID string) error {
	return c.postJSON(ctx, "/internal/v1/identity/revocations", map[string]string{
		"refresh_token": refreshToken, "user_id": userID,
	}, nil)
}

// ---- JWT / JWKS(EdDSA / OKP Ed25519) ---------------------------------

type jwksDocument struct {
	Keys []jwksKey `json:"keys"`
}

type jwksKey struct {
	Kty string `json:"kty"` // OKP
	Crv string `json:"crv"` // Ed25519
	Use string `json:"use"`
	Alg string `json:"alg"` // EdDSA
	Kid string `json:"kid"`
	X   string `json:"x"` // base64url 公钥
}

// Verifier 本地校验 identity 签发的访问令牌(JWKS 缓存 + 未知 kid 重取一次)。
type Verifier struct {
	BaseURL string // JWKS 所在 identity 基址
	AppID   string // 期望 aud
	Issuer  string // 可选:非空才校验 iss
	HTTP    *http.Client

	mu       sync.Mutex
	keys     map[string][]byte // kid -> ed25519.PublicKey
	fetched  time.Time
	fetchTTL time.Duration
}

func NewVerifier(baseURL, appID, issuer string) *Verifier {
	return &Verifier{BaseURL: baseURL, AppID: appID, Issuer: issuer, fetchTTL: 5 * time.Minute}
}

func (v *Verifier) httpClient() *http.Client {
	if v.HTTP != nil {
		return v.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// fetchJWKS 拉取并解析 OKP Ed25519 JWKS。
func (v *Verifier) fetchJWKS(ctx context.Context) (map[string][]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(v.BaseURL, "/")+"/.well-known/jwks.json", nil)
	if err != nil {
		return nil, err
	}
	res, err := v.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: 拉取 JWKS 失败: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: JWKS 状态 %d", ErrUnavailable, res.StatusCode)
	}
	var doc jwksDocument
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: JWKS 解析失败: %v", ErrUnavailable, err)
	}
	keys := make(map[string][]byte, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" || k.Kid == "" || k.X == "" {
			continue // 只收 Ed25519
		}
		raw, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(raw) != 32 {
			continue
		}
		keys[k.Kid] = raw
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: JWKS 无可用 Ed25519 密钥", ErrUnavailable)
	}
	return keys, nil
}

func (v *Verifier) cachedKeys(ctx context.Context, force bool) (map[string][]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !force && v.keys != nil && time.Since(v.fetched) < v.fetchTTL {
		return v.keys, nil
	}
	keys, err := v.fetchJWKS(ctx)
	if err != nil {
		if !force && v.keys != nil {
			// 旧缓存过期但拉取失败:沿用旧缓存(err 交给未知 kid 流程再 force 一次)。
			return v.keys, nil
		}
		return nil, err
	}
	v.keys, v.fetched = keys, time.Now()
	return keys, nil
}

// Verify 校验访问令牌并返回 Principal。
// 映射:签名/aud/typ/exp 问题 → ErrUnauthenticated;JWKS 不可达 → ErrUnavailable。
func (v *Verifier) Verify(ctx context.Context, token string) (Principal, error) {
	keys, err := v.cachedKeys(ctx, false)
	if err != nil {
		return Principal{}, err
	}
	principal, verifyErr := v.verifyWithKeys(token, keys)
	if verifyErr == nil {
		return principal, nil
	}
	// 未知 kid 可能是密钥轮换:强制重取一次 JWKS 再验;仍失败则按原错误分类。
	if errors.Is(verifyErr, errUnknownKid) {
		keys, err := v.cachedKeys(ctx, true)
		if err != nil {
			return Principal{}, err
		}
		return v.verifyWithKeys(token, keys)
	}
	return Principal{}, verifyErr
}

// errUnknownKid 标记“缓存里没有该 kid”,用于触发一次 JWKS 强制重取。
var errUnknownKid = errors.New("platform: 未知签名密钥(kid)")

func (v *Verifier) verifyWithKeys(token string, keys map[string][]byte) (Principal, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithAudience(v.AppID),
		jwt.WithExpirationRequired(),
	)
	if v.Issuer != "" {
		parser = jwt.NewParser(
			jwt.WithValidMethods([]string{"EdDSA"}),
			jwt.WithAudience(v.AppID),
			jwt.WithIssuer(v.Issuer),
			jwt.WithExpirationRequired(),
		)
	}
	parsed, err := parser.Parse(token, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		pub, ok := keys[kid]
		if !ok {
			return nil, errUnknownKid
		}
		return ed25519.PublicKey(pub), nil
	})
	if err != nil {
		if errors.Is(err, errUnknownKid) {
			return Principal{}, err // 交给上层强制重取
		}
		return Principal{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Principal{}, fmt.Errorf("%w: claims 形状异常", ErrUnauthenticated)
	}
	if typ, _ := mc["typ"].(string); typ != "access" {
		return Principal{}, fmt.Errorf("%w: 令牌类型异常", ErrUnauthenticated)
	}
	str := func(k string) string { s, _ := mc[k].(string); return s }
	rolesRaw, _ := mc["roles"].([]any)
	roles := make([]string, 0, len(rolesRaw))
	for _, r := range rolesRaw {
		if s, ok := r.(string); ok {
			roles = append(roles, s)
		}
	}
	p := Principal{UserID: str("sub"), AccountID: str("account_id"), TenantID: str("tenant_id"), Roles: roles}
	if p.UserID == "" || p.AccountID == "" {
		return Principal{}, fmt.Errorf("%w: 令牌缺少 sub/account_id", ErrUnauthenticated)
	}
	return p, nil
}
