package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sourceflow"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

const sourceBodyLimit = 32 << 10

func sourceSegment(v string) bool {
	if len(v) == 0 || len(v) > 256 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// This sits outside ServeMux because its canonical redirects must not turn an
// ambiguous source request into an authorized request with a different path.
func sourcePathGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/source-image-") {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
			invalid := r.URL.RawPath != "" || len(parts) < 5
			for _, p := range parts {
				if !sourceSegment(p) {
					invalid = true
				}
			}
			if invalid {
				writeErr(w, 400, "invalid_request", "资源路径不合法")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func sourceActor(r *http.Request) (sourceflow.Actor, bool) {
	p, ok := principalFrom(r.Context())
	return sourceflow.Actor{UserID: p.UserID, AccountID: p.AccountID}, ok && p.UserID != "" && p.AccountID != ""
}
func (s *Server) sourceService(w http.ResponseWriter, r *http.Request) bool {
	if s.Source == nil || s.Source.Store == nil || s.Source.Auth == nil || s.Source.Auth.AppID != s.Cfg.AppID || s.Cfg.AppID != s.Cfg.IdentityAppID {
		writeErr(w, 503, "source_unconfigured", "正式生成尚未配置")
		return false
	}
	if _, ok := sourceActor(r); !ok {
		writeErr(w, 401, "unauthenticated", "登录状态无效")
		return false
	}
	return true
}
func sourceMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeErr(w, 405, "method_not_allowed", "请求方法不支持")
		return false
	}
	return true
}
func sourceQuery(r *http.Request, allowed ...string) (url.Values, error) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return nil, si.ErrInvalid
	}
	a := map[string]bool{}
	for _, v := range allowed {
		a[v] = true
	}
	for k, v := range q {
		if !a[k] || len(v) != 1 || v[0] == "" || !utf8.ValidString(v[0]) {
			return nil, si.ErrInvalid
		}
	}
	return q, nil
}
func sourceReadRequest(w http.ResponseWriter, r *http.Request, allowed ...string) (url.Values, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !sourceMethod(w, r, http.MethodGet) {
		return nil, false
	}
	q, e := sourceQuery(r, allowed...)
	if e == nil && r.Body != nil {
		var b [1]byte
		n, err := r.Body.Read(b[:])
		if n != 0 || err != io.EOF {
			e = si.ErrInvalid
		}
	}
	if e != nil {
		writeErr(w, 400, "invalid_request", "查询或请求体不合法")
		return nil, false
	}
	return q, true
}

// The HTTP contract is a recursively closed object: this segment accepts only
// exact scalar strings (or an empty action object), never arbitrary params.
func sourceBody(w http.ResponseWriter, r *http.Request, keys ...string) (map[string]string, bool) {
	if _, e := sourceQuery(r); e != nil {
		writeErr(w, 400, "invalid_request", "查询参数不支持")
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, sourceBodyLimit))
	if e != nil || !utf8.Valid(raw) {
		writeErr(w, 400, "invalid_request", "请求体超过限制或读取失败")
		return nil, false
	}
	if len(raw) == 0 && len(keys) == 0 {
		return map[string]string{}, true
	}
	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typ != "application/json" {
		writeErr(w, 400, "invalid_request", "请求体必须是 JSON")
		return nil, false
	}
	var obj map[string]json.RawMessage
	if platform.DecodeSourceJSON(raw, &obj) != nil || obj == nil || len(obj) != len(keys) {
		writeErr(w, 400, "invalid_request", "请求体字段不合法")
		return nil, false
	}
	out := map[string]string{}
	for _, key := range keys {
		v, exists := obj[key]
		var value string
		if !exists || len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &value) != nil || !utf8.ValidString(value) {
			writeErr(w, 400, "invalid_request", "请求体字段不合法")
			return nil, false
		}
		out[key] = value
	}
	return out, true
}
func (s *Server) handleSourceResource(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(si.TaskRequestBudgetSeconds)*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
	defer func() { _ = controller.SetReadDeadline(time.Time{}); _ = controller.SetWriteDeadline(time.Time{}) }()
	p := strings.Split(r.URL.Path, "/")
	last := p[len(p)-1]
	switch last {
	case "source-image-capabilities":
		s.handleSourceCapabilities(w, r)
	case "source-image-runs":
		if r.Method == http.MethodPost {
			s.handleSourceCreate(w, r)
		} else {
			s.handleSourceList(w, r)
		}
	case "by-request":
		s.handleSourceFind(w, r)
	case "confirm":
		s.handleSourceConfirm(w, r)
	case "reconcile":
		s.handleSourceReconcile(w, r)
	case "cancel":
		s.handleSourceCancel(w, r)
	case "select":
		s.handleSourceSelect(w, r)
	case "output":
		s.handleSourceDelete(w, r)
	case "content":
		s.handleSourceContent(w, r)
	case "export":
		s.handleSourceExport(w, r)
	default:
		s.handleSourceGet(w, r)
	}
}
func (s *Server) sourceResult(w http.ResponseWriter, r *http.Request, run si.Run, e error) {
	if e == nil {
		writeJSON(w, 200, sourceView(run))
		return
	}
	status, code, msg := 503, "source_unavailable", "正式服务暂时不可用，原请求状态待核实"
	switch {
	case errors.Is(e, si.ErrNotFound):
		status, code, msg = 404, "not_found", "资源不存在"
	case errors.Is(e, si.ErrForbidden):
		status, code, msg = 403, "forbidden", "工程当前不允许此操作"
	case errors.Is(e, si.ErrInvalid):
		status, code, msg = 400, "invalid_request", "请求不合法"
	case errors.Is(e, si.ErrConflict):
		status, code, msg = 409, "conflict", "原请求事实与本次操作冲突"
	case errors.Is(e, si.ErrUnconfigured):
		code, msg = "source_unconfigured", "正式生成尚未配置"
	}
	body := map[string]any{"error": map[string]string{"code": code, "message": msg}}
	// Even coordinator failures may carry an internal Run. Only a fresh authorized
	// read allows its ID into the public error response.
	actor, ok := sourceActor(r)
	if run.ID != "" && ok && status != 404 {
		if authorized, err := s.Source.LoadRun(r.Context(), actor, r.PathValue("id"), run.ID, false); err == nil {
			body["run_id"] = authorized.ID
		}
	}
	writeJSON(w, status, body)
}
func (s *Server) sourceLoad(w http.ResponseWriter, r *http.Request, write bool) (si.Run, bool) {
	actor, _ := sourceActor(r)
	run, e := s.Source.LoadRun(r.Context(), actor, r.PathValue("id"), r.PathValue("runId"), write)
	if e != nil {
		if write && (errors.Is(e, si.ErrForbidden) || errors.Is(e, si.ErrNotFound)) {
			if _, re := s.Source.LoadRun(r.Context(), actor, r.PathValue("id"), r.PathValue("runId"), false); re == nil {
				e = si.ErrForbidden
			} else {
				e = si.ErrNotFound
			}
		}
		s.sourceResult(w, r, si.Run{}, e)
		return si.Run{}, false
	}
	return run, true
}
func (s *Server) handleSourceCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := sourceReadRequest(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	actor, _ := sourceActor(r)
	auth, e := s.Source.Auth.ForProject(r.Context(), actor, r.PathValue("id"), false)
	if e != nil {
		s.sourceResult(w, r, si.Run{}, e)
		return
	}
	canQuote, canConfirm, ready := s.Source.CreationCapabilities(auth.PayerSource)
	reason := ""
	project, e := s.Source.Store.GetProject(r.Context(), auth.Scope.TenantID, auth.Scope.ProjectID)
	if e != nil {
		s.sourceResult(w, r, si.Run{}, si.ErrUnavailable)
		return
	}
	if project.Status == "archived" {
		ready = false
		canQuote, canConfirm = false, false
		reason = "project_archived"
	} else if !canQuote || !canConfirm {
		reason = "source_unconfigured"
	} else if !ready {
		reason = "runtime_output_recovery_unconfigured"
	}
	writeJSON(w, 200, map[string]any{"modes": []any{map[string]any{"mode": "text_generate", "can_quote": canQuote, "can_confirm": canConfirm, "ready": ready, "disabled": !ready, "reason": reason, "size": "1024*1024", "model": "qwen-image-2.0", "fidelity": "not_applicable", "visual_quality": "unknown"}, map[string]any{"mode": "background_plate_lock", "ready": false, "disabled": true, "reason": "formal_contract_unconfigured"}, map[string]any{"mode": "reference_edit", "ready": false, "disabled": true, "reason": "formal_contract_unconfigured"}}, "historical_read": true, "automatic_output_recovery_ready": s.Source.OutputRecoveryReady})
}
func (s *Server) handleSourceCreate(w http.ResponseWriter, r *http.Request) {
	if !sourceMethod(w, r, http.MethodPost) {
		return
	}
	body, ok := sourceBody(w, r, "request_key", "mode", "prompt", "size")
	if !ok || !s.sourceService(w, r) {
		return
	}
	actor, _ := sourceActor(r)
	run, e := s.Source.Create(r.Context(), actor, r.PathValue("id"), sourceflow.NewRunRequest{RequestKey: body["request_key"], Mode: body["mode"], Prompt: body["prompt"], Size: body["size"]})
	s.sourceResult(w, r, run, e)
}

type sourceCursor struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

func sourceDecodeCursor(value string) (int64, string, error) {
	if value == "" {
		return 0, "", nil
	}
	if len(value) > 1024 {
		return 0, "", si.ErrInvalid
	}
	raw, e := base64.RawURLEncoding.Strict().DecodeString(value)
	if e != nil || base64.RawURLEncoding.EncodeToString(raw) != value {
		return 0, "", si.ErrInvalid
	}
	var obj map[string]json.RawMessage
	if platform.DecodeSourceJSON(raw, &obj) != nil || len(obj) != 3 {
		return 0, "", si.ErrInvalid
	}
	for _, key := range []string{"v", "created_at", "id"} {
		if _, ok := obj[key]; !ok {
			return 0, "", si.ErrInvalid
		}
	}
	var c sourceCursor
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || !sourceSegment(c.ID) || !strings.HasPrefix(c.ID, "sir_") {
		return 0, "", si.ErrInvalid
	}
	at, e := si.ParseMinor(json.RawMessage(c.CreatedAt))
	if e != nil || at <= 0 {
		return 0, "", si.ErrInvalid
	}
	return at, c.ID, nil
}
func (s *Server) handleSourceList(w http.ResponseWriter, r *http.Request) {
	q, ok := sourceReadRequest(w, r, "limit", "cursor")
	if !ok {
		return
	}
	limit := 100
	if value := q.Get("limit"); value != "" {
		n, e := si.ParseMinor(json.RawMessage(value))
		if e != nil || n < 1 || n > 100 {
			writeErr(w, 400, "invalid_request", "分页限制不合法")
			return
		}
		limit = int(n)
	}
	at, id, e := sourceDecodeCursor(q.Get("cursor"))
	if e != nil {
		writeErr(w, 400, "invalid_request", "分页游标不合法")
		return
	}
	if !s.sourceService(w, r) {
		return
	}
	actor, _ := sourceActor(r)
	auth, e := s.Source.Auth.ForProject(r.Context(), actor, r.PathValue("id"), false)
	if e != nil {
		s.sourceResult(w, r, si.Run{}, e)
		return
	}
	runs, nextAt, nextID, e := s.Source.Store.ListSourceRunsPage(r.Context(), auth.Scope, limit, at, id)
	if e != nil {
		s.sourceResult(w, r, si.Run{}, e)
		return
	}
	views := []any{}
	for _, run := range runs {
		views = append(views, sourceView(run))
	}
	cursor := ""
	if nextID != "" {
		raw, _ := json.Marshal(sourceCursor{Version: 1, CreatedAt: strconv.FormatInt(nextAt, 10), ID: nextID})
		cursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	writeJSON(w, 200, map[string]any{"runs": views, "next_cursor": cursor})
}
func (s *Server) handleSourceFind(w http.ResponseWriter, r *http.Request) {
	q, ok := sourceReadRequest(w, r, "request_key")
	if !ok {
		return
	}
	key := q.Get("request_key")
	invalid := key == "" || len(key) > 256 || strings.TrimSpace(key) != key
	for _, c := range key {
		if unicode.IsControl(c) {
			invalid = true
		}
	}
	if invalid {
		writeErr(w, 400, "invalid_request", "缺少合法 request_key")
		return
	}
	if !s.sourceService(w, r) {
		return
	}
	actor, _ := sourceActor(r)
	auth, e := s.Source.Auth.ForProject(r.Context(), actor, r.PathValue("id"), false)
	if e != nil {
		s.sourceResult(w, r, si.Run{}, e)
		return
	}
	run, e := s.Source.Store.FindSourceRun(r.Context(), auth.Scope, key)
	s.sourceResult(w, r, run, e)
}
func (s *Server) handleSourceGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := sourceReadRequest(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	run, ok := s.sourceLoad(w, r, false)
	if ok {
		s.sourceResult(w, r, run, nil)
	}
}
func (s *Server) handleSourceConfirm(w http.ResponseWriter, r *http.Request) {
	if !sourceMethod(w, r, http.MethodPost) {
		return
	}
	body, ok := sourceBody(w, r, "quote_id", "quote_fingerprint")
	if !ok || !s.sourceService(w, r) {
		return
	}
	if _, ok := s.sourceLoad(w, r, true); !ok {
		return
	}
	actor, _ := sourceActor(r)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(si.TaskRequestBudgetSeconds)*time.Second)
	defer cancel()
	run, e := s.Source.Confirm(ctx, actor, r.PathValue("id"), r.PathValue("runId"), body["quote_id"], body["quote_fingerprint"])
	if e == nil {
		run, e = s.Source.Advance(ctx, run.ID)
	}
	s.sourceResult(w, r, run, e)
}
func (s *Server) sourceAction(w http.ResponseWriter, r *http.Request, method string, action func(context.Context, sourceflow.Actor, string, string) (si.Run, error)) {
	if !sourceMethod(w, r, method) {
		return
	}
	if _, ok := sourceBody(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	if _, ok := s.sourceLoad(w, r, true); !ok {
		return
	}
	actor, _ := sourceActor(r)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(si.TaskRequestBudgetSeconds)*time.Second)
	defer cancel()
	run, e := action(ctx, actor, r.PathValue("id"), r.PathValue("runId"))
	s.sourceResult(w, r, run, e)
}
func (s *Server) handleSourceReconcile(w http.ResponseWriter, r *http.Request) {
	s.sourceAction(w, r, http.MethodPost, func(ctx context.Context, a sourceflow.Actor, p, id string) (si.Run, error) {
		return s.Source.Reconcile(ctx, a, p, id)
	})
}
func (s *Server) handleSourceCancel(w http.ResponseWriter, r *http.Request) {
	s.sourceAction(w, r, http.MethodPost, func(ctx context.Context, a sourceflow.Actor, p, id string) (si.Run, error) {
		return s.Source.Cancel(ctx, a, p, id)
	})
}
func (s *Server) handleSourceSelect(w http.ResponseWriter, r *http.Request) {
	s.sourceAction(w, r, http.MethodPost, func(ctx context.Context, a sourceflow.Actor, p, id string) (si.Run, error) {
		return s.Source.Select(ctx, a, p, id)
	})
}
func (s *Server) handleSourceDelete(w http.ResponseWriter, r *http.Request) {
	s.sourceAction(w, r, http.MethodDelete, func(ctx context.Context, a sourceflow.Actor, p, id string) (si.Run, error) {
		return s.Source.DeleteOutput(ctx, a, p, id)
	})
}
