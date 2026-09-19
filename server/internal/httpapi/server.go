// Package httpapi 提供产品图 Go server 的 HTTP API。
// 鉴权两层:服务间凭据(X-Product-Internal-Token,web BFF 专用)+
// 平台身份(JWT/EdDSA 经 JWKS 校验);租户作用域取自已验证 principal。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// Server 汇聚依赖;字段全部由 main(或测试)装配。
type Server struct {
	Cfg     config.Config
	St      *store.Store
	Ident   *platform.IdentityClient
	Verify  *platform.Verifier
	Tasks   *platform.TaskClient
	Uploads *platform.UploadClient
	Billing *platform.BillingClient
}

type ctxKey int

const principalKey ctxKey = 1

func principalFrom(ctx context.Context) (platform.Principal, bool) {
	p, ok := ctx.Value(principalKey).(platform.Principal)
	return p, ok
}

// Router 装配全部路由。
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	// 认证入口:只需服务间凭据(用户尚未登录)。
	mux.Handle("POST /api/v1/auth/login", s.guard(false, s.handleLogin))
	mux.Handle("POST /api/v1/auth/refresh", s.guard(false, s.handleRefresh))
	// 其余一律需要已验证平台身份。
	mux.Handle("POST /api/v1/auth/logout", s.guard(true, s.handleLogout))
	mux.Handle("GET /api/v1/auth/session", s.guard(true, s.handleSession))

	mux.Handle("GET /api/v1/projects", s.guard(true, s.handleListProjects))
	mux.Handle("POST /api/v1/projects", s.guard(true, s.handleCreateProject))
	mux.Handle("GET /api/v1/projects/{id}", s.guard(true, s.handleGetProject))
	mux.Handle("PATCH /api/v1/projects/{id}", s.guard(true, s.handleUpdateProject))
	mux.Handle("POST /api/v1/projects/{id}/inputs", s.guard(true, s.handleAddInput))
	mux.Handle("DELETE /api/v1/projects/{id}/inputs/{inputId}", s.guard(true, s.handleDeleteInput))
	mux.Handle("GET /api/v1/projects/{id}/versions", s.guard(true, s.handleListVersions))
	mux.Handle("POST /api/v1/projects/{id}/versions", s.guard(true, s.handleSubmitVersion))

	mux.Handle("GET /api/v1/billing/balance", s.guard(true, s.handleBillingBalance))

	return logRequests(mux)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r) // 不打请求体,不打令牌
		log.Printf("%s %s -> %s", r.Method, r.URL.Path, "done")
	})
}

// writeJSON 统一 JSON 应答。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr 统一错误应答:{"error":{"code","message"}}。
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// guard 服务间凭据校验(必须),平台身份校验(可选)。
func (s *Server) guard(needsAuth bool, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Cfg.InternalToken == "" || r.Header.Get("X-Product-Internal-Token") != s.Cfg.InternalToken {
			writeErr(w, http.StatusUnauthorized, "internal_unauthorized", "产品内部凭据缺失或不正确")
			return
		}
		// 致命配置问题:显式 503,绝不带病服务。
		if fatal := s.Cfg.FatalProblems(); len(fatal) > 0 {
			writeErr(w, http.StatusServiceUnavailable, "config_incomplete", "服务配置不完整:"+strings.Join(fatal, ";"))
			return
		}
		ctx := r.Context()
		if needsAuth {
			p, err := s.authenticate(ctx, r)
			if err != nil {
				s.writeAuthError(w, err)
				return
			}
			ctx = context.WithValue(ctx, principalKey, p)
		}
		next(w, r.WithContext(ctx))
	})
}

func (s *Server) authenticate(ctx context.Context, r *http.Request) (platform.Principal, error) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) || strings.TrimSpace(h[len(prefix):]) == "" {
		return platform.Principal{}, platform.ErrUnauthenticated
	}
	return s.Verify.Verify(ctx, strings.TrimSpace(h[len(prefix):]))
}

// writeAuthError 把身份错误映射为 401/503,中文原因区分用户态与平台态。
func (s *Server) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platform.ErrUnavailable):
		writeErr(w, http.StatusServiceUnavailable, "identity_unavailable",
			"平台身份服务不可达,暂时无法校验登录状态,请稍后重试")
	case errors.Is(err, platform.ErrUnauthenticated):
		writeErr(w, http.StatusUnauthorized, "unauthenticated", "登录状态无效或已过期,请重新登录")
	default:
		writeErr(w, http.StatusUnauthorized, "unauthenticated", "登录状态无效或已过期,请重新登录")
	}
}

// mapPlatformErr 平台调用错误 → 上游状态码+中文消息。
func (s *Server) mapPlatformErr(w http.ResponseWriter, svc string, err error) {
	if errors.Is(err, platform.ErrUnavailable) {
		writeErr(w, http.StatusServiceUnavailable, "upstream_unavailable",
			svc+"服务不可达或暂时故障,已拒绝本次请求(不伪造成功)")
		return
	}
	writeErr(w, http.StatusBadGateway, "upstream_rejected", svc+"服务拒绝了本次请求:"+err.Error())
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "product-image-server"})
}

// handleReadyz 配置门报告:致命问题 → 503;开关/可选服务状态一目了然。
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	fatal := s.Cfg.FatalProblems()
	genSt, genMsg := s.Cfg.GenerationUsable()
	billSt, billMsg := s.Cfg.BillingUsable()
	body := map[string]any{
		"ok":    len(fatal) == 0,
		"fatal": fatal,
		"gates": map[string]any{
			"identity":   map[string]any{"configured": true, "base_url_configured": s.Cfg.IdentityBaseURL != ""},
			"generation": map[string]any{"enabled": s.Cfg.GenerationEnabled, "usable": genSt == 0, "reason": genMsg},
			"billing":    map[string]any{"enabled": s.Cfg.BillingEnabled, "usable": billSt == 0, "reason": billMsg},
		},
	}
	status := http.StatusOK
	if len(fatal) > 0 {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, body)
}
