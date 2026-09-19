package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// 工程域端点:全部按已验证 principal 的租户作用域读写。
// standalone(无订单语境)是一等来源——创建工程从不要求订单字段。

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	list, err := s.St.ListProjects(r.Context(), p.Tenant())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取工程列表失败")
		return
	}
	if list == nil {
		list = []store.Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": list})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		Name       string `json:"name"`
		UsageKind  string `json:"usage_kind"`
		WidthPx    *int   `json:"width_px"`
		HeightPx   *int   `json:"height_px"`
		SourceType string `json:"source_type"`
		SourceRef  string `json:"source_ref"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	if (req.SourceType == "order" || req.SourceType == "campaign") && strings.TrimSpace(req.SourceRef) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "挂接 order/campaign 来源时必须提供 source_ref")
		return
	}
	proj, err := s.St.CreateProject(r.Context(), store.Project{
		TenantID: p.Tenant(), Name: req.Name, UsageKind: req.UsageKind,
		WidthPx: derefInt(req.WidthPx), HeightPx: derefInt(req.HeightPx),
		SourceType: req.SourceType, SourceRef: req.SourceRef, CreatedBy: p.UserID,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": proj})
}

// handleGetProject 返回工程详情聚合:基本信息 + 已选输入 + 版本(任务与费用事实)。
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	id := r.PathValue("id")
	proj, err := s.St.GetProject(r.Context(), p.Tenant(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	inputs, err := s.St.ListInputs(r.Context(), p.Tenant(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取输入失败")
		return
	}
	if inputs == nil {
		inputs = []store.ProjectInput{}
	}
	versions, err := s.St.ListVersions(r.Context(), p.Tenant(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取版本失败")
		return
	}
	if versions == nil {
		versions = []store.ProjectVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project": proj, "inputs": inputs, "versions": versions,
	})
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		Name       *string `json:"name"`
		Status     *string `json:"status"`
		UsageKind  *string `json:"usage_kind"`
		WidthPx    *int    `json:"width_px"`
		HeightPx   *int    `json:"height_px"`
		SourceType *string `json:"source_type"`
		SourceRef  *string `json:"source_ref"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	proj, err := s.St.UpdateProject(r.Context(), p.Tenant(), r.PathValue("id"), store.ProjectUpdate{
		Name: req.Name, Status: req.Status, UsageKind: req.UsageKind,
		WidthPx: req.WidthPx, HeightPx: req.HeightPx,
		SourceType: req.SourceType, SourceRef: req.SourceRef,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": proj})
}

func (s *Server) handleAddInput(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		PlatformAssetID     string `json:"platform_asset_id"`
		SnapshotName        string `json:"snapshot_name"`
		SnapshotSize        int64  `json:"snapshot_size"`
		SnapshotContentType string `json:"snapshot_content_type"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	in, err := s.St.AddInput(r.Context(), store.ProjectInput{
		ProjectID: r.PathValue("id"), TenantID: p.Tenant(),
		PlatformAssetID: req.PlatformAssetID, SnapshotName: req.SnapshotName,
		SnapshotSize: req.SnapshotSize, SnapshotContentType: req.SnapshotContentType,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"input": in})
}

func (s *Server) handleDeleteInput(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	if err := s.St.DeleteInput(r.Context(), p.Tenant(), r.PathValue("id"), r.PathValue("inputId")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSubmitVersion 提交生成(占位链路):开关/配置门 → platform-task → 落版本行。
// I0 不实现真实生成引擎;本端点 fail-closed,绝不伪造任务成功。
func (s *Server) handleSubmitVersion(w http.ResponseWriter, r *http.Request) {
	if st, msg := s.Cfg.GenerationUsable(); st != 0 {
		writeErr(w, st, "generation_disabled", msg)
		return
	}
	p, _ := principalFrom(r.Context())
	var req struct {
		PlatformAssetID string `json:"platform_asset_id"`
		UsageNote       string `json:"usage_note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	// 工程必须在租户内存在
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), r.PathValue("id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	res, err := s.Tasks.Submit(r.Context(), platform.SubmitRequest{
		IdempotencyKey: "ver-" + p.Tenant() + "-" + r.PathValue("id") + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Kind:           "product-image-generate",
		Payload: map[string]any{
			"project_id":        r.PathValue("id"),
			"platform_asset_id": req.PlatformAssetID,
			"usage_note":        req.UsageNote,
		},
	})
	if err != nil {
		s.mapPlatformErr(w, "生成任务", err)
		return
	}
	ver, err := s.St.AddVersion(r.Context(), store.ProjectVersion{
		ProjectID: r.PathValue("id"), TenantID: p.Tenant(),
		PlatformTaskID: res.TaskID, PlatformAssetID: req.PlatformAssetID, Status: "pending",
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"version": ver})
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	versions, err := s.St.ListVersions(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取版本失败")
		return
	}
	if versions == nil {
		versions = []store.ProjectVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// handleBillingBalance 只读账本展示(钱包主账在 billing.db,本产品零资金账本)。
func (s *Server) handleBillingBalance(w http.ResponseWriter, r *http.Request) {
	if st, msg := s.Cfg.BillingUsable(); st != 0 {
		writeErr(w, st, "billing_disabled", msg)
		return
	}
	p, _ := principalFrom(r.Context())
	led, err := s.Billing.Ledger(r.Context(), p.AccountID)
	if err != nil {
		s.mapPlatformErr(w, "计费", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance_cny": led.BalanceCNY, "items": led.Items,
		"account_id": p.AccountID,
	})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "资源不存在(或不属于当前账号)")
	case errors.Is(err, store.ErrValidation):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "internal", "服务内部错误")
	}
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
