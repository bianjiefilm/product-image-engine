package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bianjiefilm/product-image-engine/server/internal/handoff"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// campaignCurrent 是活动工程当前已采用快照上的来源事实。读取不写选定版本。
type campaignCurrent struct {
	binding store.SourceBinding
	snap    store.HandoffSnapshot
	profile handoff.SourceContext
}

func (s *Server) loadCampaignCurrent(ctx context.Context, tenant, projectID string) (campaignCurrent, *apiErr) {
	if _, err := s.St.GetProject(ctx, tenant, projectID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return campaignCurrent{}, &apiErr{http.StatusNotFound, "not_found", "资源不存在(或不属于当前账号)"}
		}
		return campaignCurrent{}, &apiErr{http.StatusInternalServerError, "internal", "服务内部错误"}
	}
	binding, err := s.St.GetBindingByProject(ctx, tenant, projectID)
	if errors.Is(err, store.ErrNotFound) {
		return campaignCurrent{}, &apiErr{http.StatusNotFound, "not_campaign", "该工程不是活动来源"}
	}
	if err != nil {
		return campaignCurrent{}, &apiErr{http.StatusInternalServerError, "internal", "服务内部错误"}
	}
	if binding.CurrentSnapshotID == "" {
		return campaignCurrent{}, &apiErr{http.StatusConflict, "campaign_ref_missing", "活动来源快照缺失"}
	}
	snap, err := s.St.GetSnapshot(ctx, tenant, binding.CurrentSnapshotID)
	if err != nil {
		return campaignCurrent{}, &apiErr{http.StatusConflict, "campaign_ref_missing", "活动来源快照缺失"}
	}
	if snap.SourceKind != "campaign" {
		return campaignCurrent{}, &apiErr{http.StatusNotFound, "not_campaign", "该工程不是活动来源"}
	}
	var profile handoff.SourceContext
	if err := json.Unmarshal([]byte(snap.ProfileJSON), &profile); err != nil || profile.CampaignRef == "" {
		return campaignCurrent{}, &apiErr{http.StatusConflict, "campaign_ref_missing", "活动引用缺失,拒绝恢复或选定"}
	}
	return campaignCurrent{binding: binding, snap: snap, profile: profile}, nil
}

func stringList(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// handleGetCampaignRestore 重登后读回活动来源、明确选定的版本和该版本的回执。
// 本接口不写选定版本。打开页面或下载文件也不算选定。
func (s *Server) handleGetCampaignRestore(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	cur, aerr := s.loadCampaignCurrent(r.Context(), p.Tenant(), r.PathValue("id"))
	if aerr != nil {
		aerr.write(w)
		return
	}
	resp := map[string]any{
		"source_kind":     "campaign",
		"campaign_ref":    cur.profile.CampaignRef,
		"source_app":      cur.snap.SourceApp,
		"source_ref":      cur.binding.SourceRef,
		"purpose":         cur.binding.Purpose,
		"handoff_id":      cur.snap.HandoffID,
		"brief_version":   cur.snap.BriefVersion,
		"source_revision": cur.snap.SourceRevision,
		"authorization": map[string]any{
			"scopes":       stringList(cur.profile.Scopes),
			"capabilities": stringList(cur.profile.Capabilities),
			"constraints":  stringList(cur.profile.Constraints),
		},
		"selected_version": nil,
		"receipt":          nil,
	}
	sel, err := s.St.GetCampaignSelection(r.Context(), p.Tenant(), cur.binding.ProjectID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	version := map[string]any{
		"output_id":       sel.OutputID,
		"snapshot_id":     sel.SnapshotID,
		"result_sha256":   "",
		"brief_version":   sel.BriefVersion,
		"source_revision": sel.SourceRevision,
		"file_name":       "",
		"stale":           sel.SnapshotID != cur.snap.ID,
	}
	if out, oerr := s.St.GetOutput(r.Context(), p.Tenant(), sel.OutputID); oerr == nil {
		version["result_sha256"] = out.ResultSHA256
		version["file_name"] = out.FileName
	}
	resp["selected_version"] = version
	if rec, rerr := s.St.FindReceiptByOutput(r.Context(), p.Tenant(), sel.OutputID); rerr == nil {
		resp["receipt"] = map[string]any{
			"id":         rec.ID,
			"output_id":  rec.OutputID,
			"event_id":   rec.EventID,
			"status":     rec.Status,
			"target_app": rec.TargetApp,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type campaignSelectRequest struct {
	OutputID string `json:"output_id"`
}

// handleSelectCampaignVersion 只接受对当前活动快照成果的明确选定。
func (s *Server) handleSelectCampaignVersion(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req campaignSelectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.OutputID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON 且含 output_id")
		return
	}
	ctx := r.Context()
	projID := r.PathValue("id")
	cur, aerr := s.loadCampaignCurrent(ctx, p.Tenant(), projID)
	if aerr != nil {
		aerr.write(w)
		return
	}
	out, err := s.St.GetOutput(ctx, p.Tenant(), req.OutputID)
	if err != nil || out.ProjectID != projID {
		writeErr(w, http.StatusNotFound, "not_found", "输出不存在(或不属于当前账号)")
		return
	}
	if out.Kind != "result" || out.SnapshotID != cur.snap.ID {
		writeErr(w, http.StatusConflict, "output_not_current", "只能选定当前活动需求版本下的成果")
		return
	}
	_, perr := s.St.GetCampaignSelection(ctx, p.Tenant(), projID)
	existed := perr == nil
	if perr != nil && !errors.Is(perr, store.ErrNotFound) {
		writeStoreErr(w, perr)
		return
	}
	sel, same, err := s.St.PutCampaignSelection(ctx, store.CampaignSelection{
		TenantScope: p.Tenant(), ProjectID: projID, OutputID: out.ID, SnapshotID: out.SnapshotID,
		CampaignRef: cur.profile.CampaignRef, BriefVersion: out.BriefVersion, SourceRevision: out.SourceRevision,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	body := map[string]any{"selection": sel}
	if same {
		body["duplicate"] = true
		writeJSON(w, http.StatusOK, body)
		return
	}
	if !existed {
		writeJSON(w, http.StatusCreated, body)
		return
	}
	body["replaced"] = true
	writeJSON(w, http.StatusOK, body)
}

// rejectUnselectedCampaignReceipt 活动回执必须对上明确选定的输出版本。
// 订单来源不走这里。已有回执的重复提交仍由调用方先返回原回执。
func (s *Server) rejectUnselectedCampaignReceipt(ctx context.Context, tenant, projectID string, out store.ProjectOutput) *apiErr {
	sel, err := s.St.GetCampaignSelection(ctx, tenant, projectID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && (sel.OutputID != out.ID || sel.SnapshotID != out.SnapshotID)) {
		return &apiErr{http.StatusConflict, "version_not_selected", "活动成果须先明确选定这一版再回传"}
	}
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "服务内部错误"}
	}
	return nil
}
