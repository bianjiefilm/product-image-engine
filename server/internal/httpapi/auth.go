package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
)

// 认证端点:登录/刷新/登出/会话。全部走 platform-identity 标准内部端点;
// 本产品不保存口令、不派生身份,只持有换来的令牌并本地校验。

type authPairResponse struct {
	AccessToken  string             `json:"access_token"`
	RefreshToken string             `json:"refresh_token"`
	TokenType    string             `json:"token_type"`
	ExpiresIn    int64              `json:"expires_in"`
	Principal    platform.Principal `json:"principal"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "邮箱与口令不能为空")
		return
	}
	pair, err := s.Ident.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, platform.ErrUnavailable) {
			s.mapPlatformErr(w, "平台身份", err)
			return
		}
		writeErr(w, http.StatusUnauthorized, "unauthenticated", "登录失败:邮箱或口令不正确,或账号不存在")
		return
	}
	p, verr := s.Verify.Verify(r.Context(), pair.AccessToken)
	if verr != nil {
		s.writeAuthError(w, verr)
		return
	}
	writeJSON(w, http.StatusOK, authPairResponse{
		AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken,
		TokenType: pair.TokenType, ExpiresIn: pair.ExpiresIn, Principal: p,
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "缺少 refresh_token")
		return
	}
	pair, err := s.Ident.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if isUnavailable(err) {
			s.mapPlatformErr(w, "平台身份", err)
			return
		}
		writeErr(w, http.StatusUnauthorized, "unauthenticated", "刷新失败:会话可能已过期,请重新登录")
		return
	}
	p, verr := s.Verify.Verify(r.Context(), pair.AccessToken)
	if verr != nil {
		s.writeAuthError(w, verr)
		return
	}
	writeJSON(w, http.StatusOK, authPairResponse{
		AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken,
		TokenType: pair.TokenType, ExpiresIn: pair.ExpiresIn, Principal: p,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if err := s.Ident.Logout(r.Context(), req.RefreshToken, p.UserID); err != nil {
		if isUnavailable(err) {
			s.mapPlatformErr(w, "平台身份", err)
			return
		}
		writeErr(w, http.StatusBadGateway, "upstream_rejected", "平台身份服务拒绝了登出请求")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"principal": p})
}

func isUnavailable(err error) bool { return errors.Is(err, platform.ErrUnavailable) }
