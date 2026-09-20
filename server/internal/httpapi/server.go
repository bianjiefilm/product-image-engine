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
	"sync"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sizeadapt"
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
	// Registry 应用登记表(HUI-1745 I1;nil 时回执/返回来源 fail-closed)。
	Registry *appregistry.Manifest
	// Presets 尺寸适配预设集(HUI-1703 FEAT-0204;main 装配,内嵌默认或
	// PRODUCT_SIZE_PRESETS 覆盖)。nil 时尺寸适配端点 fail-closed 503。
	Presets *sizeadapt.PresetSet

	// batchSubmitSem 批量提交进程内信号量(HUI-1704 拍板:并发上限 4,超出排队)。
	// 惰性初始化;semMu 仅保护初始化。
	semMu          sync.Mutex
	batchSubmitSem chan struct{}
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

	// 跨应用续接与成果回流(HUI-1745 I1)。
	mux.Handle("POST /api/v1/handoffs/accept", s.guard(true, s.handleAcceptHandoff))
	mux.Handle("GET /api/v1/bindings", s.guard(true, s.handleListBindings))
	mux.Handle("GET /api/v1/bindings/{id}", s.guard(true, s.handleGetBinding))
	mux.Handle("GET /api/v1/bindings/{id}/return-target", s.guard(true, s.handleBindingReturnTarget))
	mux.Handle("POST /api/v1/bindings/{id}/adopt", s.guard(true, s.handleAdoptSnapshot))
	mux.Handle("POST /api/v1/projects/{id}/outputs", s.guard(true, s.handleRegisterOutput))
	mux.Handle("GET /api/v1/projects/{id}/outputs", s.guard(true, s.handleListOutputs))
	mux.Handle("POST /api/v1/projects/{id}/outputs/{outputId}/receipt", s.guard(true, s.handleSendReceipt))
	mux.Handle("GET /api/v1/projects/{id}/receipts", s.guard(true, s.handleListReceipts))
	mux.Handle("POST /api/v1/receipts/{id}/resend", s.guard(true, s.handleResendReceipt))

	mux.Handle("GET /api/v1/billing/balance", s.guard(true, s.handleBillingBalance))

	// 批量生成批次编排(HUI-1704 / FEAT-0205):创建/提交/收集/重试/取消。
	// 生成门在提交步把关(FEATURE_GENERATION_ENABLED off → 创建成功、提交全 blocked)。
	mux.Handle("POST /api/v1/batches", s.guard(true, s.handleCreateBatch))
	mux.Handle("GET /api/v1/batches", s.guard(true, s.handleListBatches))
	mux.Handle("GET /api/v1/batches/{id}", s.guard(true, s.handleGetBatch))
	mux.Handle("POST /api/v1/batches/{id}/submit", s.guard(true, s.handleSubmitBatch))
	mux.Handle("POST /api/v1/batches/{id}/collect", s.guard(true, s.handleCollectBatch))
	mux.Handle("POST /api/v1/batches/{id}/retry-failed", s.guard(true, s.handleRetryFailedBatch))
	mux.Handle("POST /api/v1/batches/{id}/cancel", s.guard(true, s.handleCancelBatch))

	// 电商尺寸适配(HUI-1703 / FEAT-0204):纯确定性变换,与生成/计费零耦合。
	// 开关 FEATURE_SIZE_ADAPT off → 路由不注册 = 404 不可见(fail-closed)。
	if s.Cfg.SizeAdaptEnabled {
		mux.Handle("GET /api/v1/size-adapt/presets", s.guard(true, s.handleListSizePresets))
		mux.Handle("POST /api/v1/projects/{id}/size-adapt", s.guard(true, s.handleCreateSizeVariant))
		mux.Handle("GET /api/v1/projects/{id}/size-adapt", s.guard(true, s.handleListSizeVariants))
		mux.Handle("GET /api/v1/projects/{id}/size-adapt/{variantId}/download", s.guard(true, s.handleDownloadSizeVariant))
	}

	// 产品照片上传(HUI-1697 / FEAT-0198):登记制开关沿 size_adapt 语义,
	// FEATURE_PHOTO_UPLOAD off → 路由不注册 = 404 不可见(fail-closed)。
	if s.Cfg.PhotoUploadEnabled {
		mux.Handle("POST /api/v1/photos", s.guard(true, s.handlePhotoUpload))
		mux.Handle("GET /api/v1/photos/{id}/content", s.guard(true, s.handlePhotoContent))
		mux.Handle("GET /api/v1/projects/{id}/inputs/{inputId}/content", s.guard(true, s.handleInputContent))
	}

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
	photoSt, photoMsg := s.Cfg.PhotoUploadUsable()
	sizePresets := 0
	if s.Presets != nil {
		sizePresets = len(s.Presets.List())
	}
	body := map[string]any{
		"ok":    len(fatal) == 0,
		"fatal": fatal,
		"gates": map[string]any{
			"identity":   map[string]any{"configured": true, "base_url_configured": s.Cfg.IdentityBaseURL != ""},
			"generation": map[string]any{"enabled": s.Cfg.GenerationEnabled, "usable": genSt == 0, "reason": genMsg},
			"billing":    map[string]any{"enabled": s.Cfg.BillingEnabled, "usable": billSt == 0, "reason": billMsg},
			"size_adapt": map[string]any{"enabled": s.Cfg.SizeAdaptEnabled, "presets_loaded": sizePresets},
			"photo_upload": map[string]any{
				"enabled": s.Cfg.PhotoUploadEnabled, "usable": photoSt == 0, "reason": photoMsg,
				"max_bytes": s.Cfg.PhotoMaxBytes,
			},
		},
	}
	status := http.StatusOK
	if len(fatal) > 0 {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, body)
}
