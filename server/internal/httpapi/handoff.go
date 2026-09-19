package httpapi

// 跨应用续接与成果回流(HUI-1745 I1)。
//
// 红线(票面):
//   - 接受交接只创建/恢复工程,绝不自动调用收费模型(生成开关独立);
//   - 绑定唯一键 = tenant_scope+source_app+source_ref+target_app+purpose(逐字落实);
//   - 同 handoff 并发/重复只绑定一次(handoff_id+内容指纹去重);
//   - 新版需求 → 不可变新快照,用户确认差异后采用,不覆盖人工编辑与已选输出;
//   - 回执只重传既有成果;回执失败不追加生成任务或扣费;
//   - 外部需求文本只是数据,绝不拼接进任何执行路径。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/handoff"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/receiptdoc"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// Registry 目标地址簿(app-registry/v1);main(或测试)装配。
// nil 时回执/返回来源 fail-closed。
type Registry = appregistry.Manifest

// errBody 结构化业务错误(端点间传递)。
type httpErr struct {
	status int
	code   string
	msg    string
}

func (e *httpErr) Error() string { return e.msg }

func herr(status int, code, msg string) *httpErr {
	return &httpErr{status: status, code: code, msg: msg}
}

// ---- POST /api/v1/handoffs/accept ----------------------------------------

type acceptRequest struct {
	Handoff json.RawMessage `json:"handoff"`
	Purpose string          `json:"purpose"`
}

func (s *Server) handleAcceptHandoff(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req acceptRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, handoff.MaxDocumentBytes+(1<<20))).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON(handoff 文档 + purpose)")
		return
	}
	if len(req.Handoff) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "缺少 handoff 文档")
		return
	}
	purpose := strings.TrimSpace(req.Purpose)
	if purpose == "" || len([]rune(purpose)) > 256 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "purpose(制作用途)必填且 ≤256 字符")
		return
	}
	// 过期优先裁决(fail-closed):严格解析前先对 expires_at 做轻量预扫,
	// 已过期或垃圾时间一律 410——“已死凭据”不因结构性校验顺序报成格式错误。
	var pre struct {
		ExpiresAt string `json:"expires_at"`
	}
	_ = json.Unmarshal(req.Handoff, &pre) // 容错预扫;严格校验交给 Parse
	if handoff.IsExpired(time.Now().UTC(), pre.ExpiresAt) {
		writeErr(w, http.StatusGone, "handoff_expired", "交接已过期,须重新发起授权(短期凭据失效≠长任务失败;续接须重新授权)")
		return
	}
	h, err := handoff.Parse(req.Handoff)
	if err != nil {
		code := handoff.ErrCode(err)
		if code == "" {
			code = "invalid_document"
		}
		writeErr(w, http.StatusBadRequest, code, "交接文档被拒绝:"+err.Error())
		return
	}
	// target_app 必须是本应用(错目标拒绝)。
	if h.TargetApp != s.Cfg.AppID {
		writeErr(w, http.StatusBadRequest, "invalid_document",
			fmt.Sprintf("交接 target_app %q 非本应用 %q", h.TargetApp, s.Cfg.AppID))
		return
	}
	// 双保险:解析后的结构化过期复核(issued/expires 窗口已过同样 410)。
	if handoff.IsExpired(time.Now().UTC(), h.ExpiresAt) {
		writeErr(w, http.StatusGone, "handoff_expired", "交接已过期,须重新发起授权(短期凭据失效≠长任务失败;续接须重新授权)")
		return
	}
	tenant := p.Tenant()
	// 租户精确比对:profiled 来源必须声明 tenant_scope 且精确匹配;纯 v1 订单来源
	// 的租户事实经平台一次性兑换(HUI-656 域)绑定到兑换 principal。
	if h.SourceProfile != nil && !handoff.TenantScopeMatches(h.SourceProfile.TenantScope, tenant) {
		writeErr(w, http.StatusForbidden, "tenant_mismatch", "交接租户作用域与当前账号不匹配(拒绝,无串数据)")
		return
	}
	ctx := r.Context()
	fp, err := h.ContentFingerprint()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "指纹计算失败")
		return
	}

	// 1) 快照幂等裁决:同 handoff_id 同内容 SAME / 异内容 CONFLICT(保留原快照)/ 新 → NEW。
	var snap store.HandoffSnapshot
	snapResolution := "SNAPSHOT_NEW"
	existing, err := s.St.FindSnapshotByHandoff(ctx, tenant, h.HandoffID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		snap, err = s.St.CreateSnapshot(ctx, store.HandoffSnapshot{
			TenantScope: tenant, BindingID: "", HandoffID: h.HandoffID, ContentFP: fp,
			SourceKind: string(h.Kind()), SourceApp: h.SourceApp, PrincipalID: h.PrincipalID,
			SourceRevision: h.SourceRevision, BriefVersion: h.BriefVersion,
			ProfileJSON: marshalSourceContext(h),
		})
		if errors.Is(err, store.ErrConflict) {
			// 并发同 handoff:重读裁决。
			existing, err2 := s.St.FindSnapshotByHandoff(ctx, tenant, h.HandoffID)
			if err2 != nil {
				writeErr(w, http.StatusConflict, "snapshot_conflict", "同 handoff 并发写入冲突,请重试")
				return
			}
			snap = existing
		} else if err != nil {
			writeStoreErr(w, err)
			return
		}
	case err != nil:
		writeStoreErr(w, err)
		return
	default:
		if existing.ContentFP != fp {
			writeErr(w, http.StatusConflict, "snapshot_conflict",
				"同 handoff_id 但内容不同(409):保留原快照,绝不覆盖;新内容须以新 handoff_id 发起")
			return
		}
		snap = existing
		snapResolution = "SNAPSHOT_SAME"
	}

	// 2) 一次性兑换语义(binding_ref):重放同 tuple → SAME_RESULT;异 tuple → 拒绝。
	red, err := s.St.FindRedemption(ctx, h.Binding.BindingRef)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if _, err := s.St.CreateRedemption(ctx, store.Redemption{
			BindingRef: h.Binding.BindingRef, TenantScope: tenant, ContentFP: fp, SnapshotID: snap.ID,
		}); errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "redemption_conflict",
				"绑定凭据已被其他上下文消费(一次性兑换,拒绝异 tuple 重放)")
			return
		} else if err != nil {
			writeStoreErr(w, err)
			return
		}
	case err != nil:
		writeStoreErr(w, err)
		return
	default:
		if red.TenantScope != tenant || red.ContentFP != fp {
			writeErr(w, http.StatusConflict, "redemption_conflict",
				"绑定凭据已按不同上下文消费(一次性兑换:同 tuple 幂等,异 tuple 拒绝)")
			return
		}
	}

	// 3) 五元组绑定 find-or-create + 工程创建/恢复。
	key := store.BindingKey{TenantScope: tenant, SourceApp: h.SourceApp, SourceRef: h.SourceProjectRef,
		TargetApp: s.Cfg.AppID, Purpose: purpose}
	binding, err := s.St.FindBindingByKey(ctx, key)
	resolution := "restored"
	if errors.Is(err, store.ErrNotFound) {
		proj, err := s.createProjectForHandoff(ctx, tenant, p.UserID, h, purpose)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		binding, err = s.St.CreateBinding(ctx, store.SourceBinding{
			TenantScope: tenant, SourceApp: h.SourceApp, SourceRef: h.SourceProjectRef,
			TargetApp: s.Cfg.AppID, Purpose: purpose, ProjectID: proj.ID,
			CurrentSnapshotID: snap.ID,
		})
		if errors.Is(err, store.ErrConflict) {
			// 并发同键:恢复已有绑定(只绑定一次)。
			binding, err = s.St.FindBindingByKey(ctx, key)
		}
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		resolution = "created"
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}

	// 快照归属补链(创建时 BindingID 未知):把快照挂到绑定(仅首次)。
	if snap.BindingID == "" {
		if _, err := s.St.UpdateSnapshotBinding(ctx, tenant, snap.ID, binding.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
	}

	// 4) 新版需求(绑定当前快照 ≠ 本次快照)→ 返回待采用差异;绝不自动采用、
	//    不覆盖人工编辑与已选输出。
	proj, err := s.St.GetProject(ctx, tenant, binding.ProjectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	resp := map[string]any{
		"binding": binding, "snapshot": snap, "project": proj, "resolution": resolution,
		"snapshot_resolution": snapResolution,
		"source_context":      json.RawMessage(snap.ProfileJSON),
	}
	if binding.CurrentSnapshotID != snap.ID {
		cur, err := s.St.GetSnapshot(ctx, tenant, binding.CurrentSnapshotID)
		if err == nil {
			resp["pending_snapshot"] = snap
			resp["diff"] = diffSnapshots(cur, snap)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// createProjectForHandoff 创建工程并挂交接素材(仅首次创建;恢复不动已有输入)。
// 素材仅作为"数据引用"挂接——来源文本不进入任何执行路径。
func (s *Server) createProjectForHandoff(ctx context.Context, tenant, userID string, h *handoff.Handoff, purpose string) (store.Project, error) {
	name := h.DeliverySpec.Description
	if strings.TrimSpace(name) == "" {
		name = "来源工程-" + h.SourceProjectRef
	}
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	proj, err := s.St.CreateProject(ctx, store.Project{
		TenantID: tenant, Name: name, UsageKind: purpose,
		SourceType: string(h.Kind()), SourceRef: h.SourceProjectRef, CreatedBy: userID,
	})
	if err != nil {
		return store.Project{}, err
	}
	for _, a := range h.Assets {
		if _, err := s.St.AddInput(ctx, store.ProjectInput{
			ProjectID: proj.ID, TenantID: tenant,
			PlatformAssetID: a.AssetRef, SnapshotName: "handoff:" + a.AssetRef,
			SnapshotSize: a.SizeBytes, SnapshotContentType: a.MediaType,
		}); err != nil {
			return store.Project{}, err
		}
	}
	return proj, nil
}

func marshalSourceContext(h *handoff.Handoff) string {
	b, _ := json.Marshal(h.SourceContext())
	return string(b)
}

// diffSnapshots 新旧快照差异(数据对照,不产生任何指令)。
func diffSnapshots(oldS, newS store.HandoffSnapshot) map[string]any {
	return map[string]any{
		"old": map[string]string{
			"handoff_id": oldS.HandoffID, "source_revision": oldS.SourceRevision,
			"brief_version": oldS.BriefVersion,
		},
		"new": map[string]string{
			"handoff_id": newS.HandoffID, "source_revision": newS.SourceRevision,
			"brief_version": newS.BriefVersion,
		},
	}
}

// ---- GET /api/v1/bindings + GET /api/v1/bindings/{id} ---------------------

func (s *Server) handleListBindings(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	list, err := s.St.ListBindings(r.Context(), p.Tenant())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取绑定列表失败")
		return
	}
	// 可选过滤:某工程的来源绑定(工程详情页来源面板)。
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		filtered := make([]store.SourceBinding, 0, len(list))
		for _, b := range list {
			if b.ProjectID == pid {
				filtered = append(filtered, b)
			}
		}
		list = filtered
	}
	if list == nil {
		list = []store.SourceBinding{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": list})
}

// handleListOutputs 工程下用户明确选择的成果(登记即展示,不触发任何生成)。
func (s *Server) handleListOutputs(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	projID := r.PathValue("id")
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), projID); err != nil {
		writeStoreErr(w, err)
		return
	}
	list, err := s.St.ListOutputs(r.Context(), p.Tenant(), projID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取成果列表失败")
		return
	}
	if list == nil {
		list = []store.ProjectOutput{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"outputs": list})
}

// handleBindingReturnTarget 解析"返回来源"跳转地址(登记表白名单内的 launch target)。
// 来源未登记跳转目标 → 409 明确拒绝(来源不可用不阻塞工程保存/续接)。
func (s *Server) handleBindingReturnTarget(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	binding, err := s.St.GetBinding(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if s.Registry == nil {
		writeErr(w, http.StatusServiceUnavailable, "registry_unavailable", "应用登记表未装配,无法解析返回来源地址")
		return
	}
	var targetID string
	if snap, err := s.St.GetSnapshot(r.Context(), p.Tenant(), binding.CurrentSnapshotID); err == nil {
		var sc struct {
			ReturnTargetID string `json:"return_target_id"`
		}
		if json.Unmarshal([]byte(snap.ProfileJSON), &sc) == nil {
			targetID = sc.ReturnTargetID
		}
	}
	url, err := s.Registry.ResolveTarget(binding.SourceApp, targetID, appregistry.KindLaunch)
	if err != nil {
		writeErr(w, http.StatusConflict, "return_target_unregistered",
			"来源应用未登记可返回的跳转目标("+err.Error()+");来源不可用不影响本工程继续编辑")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

func (s *Server) handleGetBinding(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	binding, err := s.St.GetBinding(ctx, p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	proj, err := s.St.GetProject(ctx, p.Tenant(), binding.ProjectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	snaps, err := s.St.ListSnapshotsForBinding(ctx, p.Tenant(), binding.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取快照失败")
		return
	}
	if snaps == nil {
		snaps = []store.HandoffSnapshot{}
	}
	resp := map[string]any{"binding": binding, "project": proj, "snapshots": snaps}
	if binding.CurrentSnapshotID != "" {
		if cur, err := s.St.GetSnapshot(ctx, p.Tenant(), binding.CurrentSnapshotID); err == nil {
			resp["current_snapshot"] = cur
			resp["source_context"] = json.RawMessage(cur.ProfileJSON)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- POST /api/v1/bindings/{id}/adopt --------------------------------------

type adoptRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

func (s *Server) handleAdoptSnapshot(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req adoptRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.SnapshotID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON 且含 snapshot_id")
		return
	}
	ctx := r.Context()
	binding, err := s.St.GetBinding(ctx, p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	snap, err := s.St.GetSnapshot(ctx, p.Tenant(), req.SnapshotID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if snap.BindingID != binding.ID {
		writeErr(w, http.StatusBadRequest, "invalid_request", "快照不属于该绑定")
		return
	}
	if snap.ID == binding.CurrentSnapshotID {
		writeJSON(w, http.StatusOK, map[string]any{"binding": binding, "adopted": false, "note": "已是当前快照"})
		return
	}
	// 采用 = 只移动绑定指针 + 追加缺失素材引用;不修改工程人工编辑字段,
	// 不触碰任何已选输出与版本。
	updated, err := s.St.AdoptBindingSnapshot(ctx, p.Tenant(), binding.ID, snap.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	applied := 0
	var sc handoff.SourceContext
	if err := json.Unmarshal([]byte(snap.ProfileJSON), &sc); err == nil {
		for _, a := range sc.Assets {
			ins, err := s.St.ListInputs(ctx, p.Tenant(), binding.ProjectID)
			if err != nil {
				break
			}
			found := false
			for _, in := range ins {
				if in.PlatformAssetID == a.AssetRef {
					found = true
					break
				}
			}
			if !found {
				if _, err := s.St.AddInput(ctx, store.ProjectInput{
					ProjectID: binding.ProjectID, TenantID: p.Tenant(),
					PlatformAssetID: a.AssetRef, SnapshotName: "handoff:" + a.AssetRef,
					SnapshotSize: a.SizeBytes, SnapshotContentType: a.MediaType,
				}); err == nil {
					applied++
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"binding": updated, "adopted": true, "inputs_added": applied})
}

// ---- POST /api/v1/projects/{id}/outputs ------------------------------------

type outputRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	DataB64     string `json:"data_b64"`
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// handleRegisterOutput 用户明确选择输出版本:接收合法 PNG → sha256 →
// 经平台 upload 设施登记 → 落输出行。不触发生成、不扣费。
func (s *Server) handleRegisterOutput(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req outputRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON(file_name/content_type/data_b64)")
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.DataB64)
	if err != nil || len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "data_b64 必须是非空 base64")
		return
	}
	if len(data) > 8<<20 {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "测试用合法图片上限 8 MiB")
		return
	}
	// "真实可打开":只收 PNG 签名(测试用合法 PNG,标注仅验证集成)。
	if !bytesHasPrefix(data, pngMagic) {
		writeErr(w, http.StatusBadRequest, "invalid_request", "仅接受合法 PNG(魔数校验失败)")
		return
	}
	mediaType := req.ContentType
	if mediaType == "" {
		mediaType = "image/png"
	}
	if mediaType != "image/png" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "仅接受 image/png(测试合法图片)")
		return
	}
	// upload 客户端可用性 fail-closed(与生成/计费开关无关:登记产出≠生成)。
	if s.Uploads == nil || s.Cfg.UploadBaseURL == "" || s.Cfg.UploadToken == "" {
		writeErr(w, http.StatusServiceUnavailable, "upload_not_configured",
			"素材登记服务未配置(缺少 PLATFORM_UPLOAD_BASE_URL 或 PLATFORM_UPLOAD_TOKEN),不伪造登记成功")
		return
	}
	ctx := r.Context()
	projID := r.PathValue("id")
	if _, err := s.St.GetProject(ctx, p.Tenant(), projID); err != nil {
		writeStoreErr(w, err)
		return
	}
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])
	// 重复提交幂等:同工程同内容已登记 → 返回既有输出(不二次登记)。
	if existing, err := s.St.FindOutputByContent(ctx, p.Tenant(), projID, shaHex); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"output": existing, "duplicate": true})
		return
	}
	reg, err := s.Uploads.RegisterAsset(ctx, platform.RegisterAssetRequest{
		FileName: req.FileName, ContentType: mediaType, SizeBytes: int64(len(data)),
		SHA256: shaHex, ContentB64: base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		s.mapPlatformErr(w, "素材登记", err)
		return
	}
	// 记录选择时的需求版本上下文(回执据此判定"旧需求版本回执"负例)。
	out := store.ProjectOutput{
		TenantScope: p.Tenant(), ProjectID: projID, PlatformAssetID: reg.AssetID,
		ResultSHA256: shaHex, ResultSize: int64(len(data)), MediaType: mediaType,
		FileName: req.FileName, PlatformTaskID: "",
	}
	if binding, err := s.St.GetBindingByProject(ctx, p.Tenant(), projID); err == nil && binding.CurrentSnapshotID != "" {
		if snap, err := s.St.GetSnapshot(ctx, p.Tenant(), binding.CurrentSnapshotID); err == nil {
			out.SnapshotID, out.BriefVersion, out.SourceRevision = snap.ID, snap.BriefVersion, snap.SourceRevision
		}
	}
	created, err := s.St.CreateOutput(ctx, out)
	if errors.Is(err, store.ErrConflict) {
		existing, gerr := s.St.FindOutputByContent(ctx, p.Tenant(), projID, shaHex)
		if gerr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"output": existing, "duplicate": true})
			return
		}
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"output": created})
}

func bytesHasPrefix(b, prefix []byte) bool {
	return len(b) >= len(prefix) && string(b[:len(prefix)]) == string(prefix)
}

// ---- POST /api/v1/projects/{id}/outputs/{outputId}/receipt ------------------

func (s *Server) handleSendReceipt(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	projID, outputID := r.PathValue("id"), r.PathValue("outputId")
	if _, err := s.St.GetProject(ctx, p.Tenant(), projID); err != nil {
		writeStoreErr(w, err)
		return
	}
	out, err := s.St.GetOutput(ctx, p.Tenant(), outputID)
	if err != nil || out.ProjectID != projID {
		writeErr(w, http.StatusNotFound, "not_found", "输出不存在(或不属于当前账号)")
		return
	}
	// 重复提交:同输出已有回执 → 原样返回(不产生新事件)。
	if existing, err := s.St.FindReceiptByOutput(ctx, p.Tenant(), outputID); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"receipt": existing, "duplicate": true})
		return
	}
	binding, err := s.St.GetBindingByProject(ctx, p.Tenant(), projID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusConflict, "no_source_binding",
			"该工程无来源绑定(standalone 完整保留在本应用,不回执)")
		return
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}
	snap, err := s.St.GetSnapshot(ctx, p.Tenant(), out.SnapshotID)
	if err != nil {
		writeErr(w, http.StatusConflict, "stale_requirement_version",
			"输出来源的需求版本上下文缺失,拒绝回执(无串数据)")
		return
	}
	// 旧需求版本回执:绑定下存在更新的需求版本(已采用或待采用)时,
	// 旧版本下选择的输出拒绝回执(明确结果,不混数据)。
	if latest, err := s.St.ListSnapshotsForBinding(ctx, p.Tenant(), binding.ID); err == nil && len(latest) > 0 {
		if latest[0].ID != out.SnapshotID {
			writeErr(w, http.StatusConflict, "stale_requirement_version",
				"来源需求已有新版本("+latest[0].BriefVersion+"),所选输出来自旧需求版本("+out.BriefVersion+
					");请在新版本下(确认采用后)重新选择输出再回执")
			return
		}
	}
	if binding.CurrentSnapshotID != out.SnapshotID {
		writeErr(w, http.StatusConflict, "stale_requirement_version",
			"所选输出来自旧需求版本("+out.BriefVersion+"),已采用新版本;请在新版本下重新选择输出后回执")
		return
	}
	// 投递配置 fail-closed:目标未登记 receipt target / 未配置专用密钥 → 拒绝。
	if s.Registry == nil {
		writeErr(w, http.StatusServiceUnavailable, "registry_unavailable", "应用登记表未装配,无法解析回执目标")
		return
	}
	target, err := s.Registry.FirstReceiptTarget(binding.SourceApp)
	if err != nil {
		writeErr(w, http.StatusConflict, "receipt_target_unregistered",
			"来源应用未登记 receipt target,拒绝回执(fail-closed):"+err.Error())
		return
	}
	secret := s.Cfg.ReceiptKeyFor(binding.SourceApp)
	if secret == "" {
		writeErr(w, http.StatusServiceUnavailable, "receipt_key_missing",
			"未配置来源应用回执专用密钥(PRODUCT_RECEIPT_KEYS),拒绝回执(不伪造成功)")
		return
	}
	// 构造 order-receipt/v1(费用事实只读既有账本,绝不触发扣费)。
	eventID := "wr-" + hexSha(tenantEventSeed(p.Tenant(), projID, outputID, out.ResultSHA256))
	runID := out.PlatformTaskID
	if runID == "" {
		runID = "manual-" + out.ID
	}
	receipt := receiptdoc.Receipt{
		SchemaVersion: receiptdoc.SchemaVersionV1, EventID: eventID, RunID: runID,
		HandoffID: snap.HandoffID, SourceApp: s.Cfg.AppID, TargetApp: binding.SourceApp,
		PrincipalID: snap.PrincipalID, ProjectRef: binding.SourceRef,
		ProjectRevision: out.SourceRevision, BriefVersion: out.BriefVersion,
		Sequence: 1, Status: "succeeded",
		Assets: []receiptdoc.Asset{{
			AssetRef: out.PlatformAssetID, SHA256: out.ResultSHA256,
			SizeBytes: out.ResultSize, MediaType: out.MediaType,
		}},
		BillingFactRefs: s.billingFactRefs(ctx, out),
		OccurredAt:      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	if err := receipt.Validate(); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "回执构造失败:"+err.Error())
		return
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "回执序列化失败")
		return
	}
	plFP := sha256.Sum256(payload)
	rec, err := s.St.CreateReceipt(ctx, store.SourceReceipt{
		TenantScope: p.Tenant(), ProjectID: projID, OutputID: outputID,
		EventID: eventID, HandoffID: snap.HandoffID, TargetApp: binding.SourceApp,
		RunID: runID, Sequence: 1, Payload: string(payload), PayloadFP: hex.EncodeToString(plFP[:]),
		Status: "pending",
	})
	if errors.Is(err, store.ErrConflict) {
		if existing, gerr := s.St.FindReceiptByOutput(ctx, p.Tenant(), outputID); gerr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"receipt": existing, "duplicate": true})
			return
		}
		writeErr(w, http.StatusConflict, "receipt_conflict", "同事件回执已存在")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// 先持久后发送;失败只记状态,不追加任务、不扣费。
	s.deliverReceipt(ctx, rec, target.URL, []byte(secret))
	final, err := s.St.GetReceipt(ctx, p.Tenant(), rec.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"receipt": final})
}

// billingFactRefs 只读账本抽取既有费用事实引用(ECO_BILLING_ENABLED 关闭/服务
// 不可用 → 空数组;绝不写账、绝不 hold/spend/refund)。
func (s *Server) billingFactRefs(ctx context.Context, out store.ProjectOutput) []string {
	refs := []string{}
	if s.Billing == nil || !s.Cfg.BillingEnabled {
		return refs
	}
	p, ok := principalFrom(ctx)
	if !ok {
		return refs
	}
	led, err := s.Billing.Ledger(ctx, p.AccountID)
	if err != nil {
		log.Printf("receipt: 账本只读失败(以空费用事实回执): %v", err)
		return refs
	}
	for _, it := range led.Items {
		if out.PlatformTaskID != "" && strings.Contains(fmt.Sprint(it["task_id"]), out.PlatformTaskID) {
			if id, ok := it["id"].(string); ok && id != "" {
				refs = append(refs, id)
			}
		}
	}
	return refs
}

func tenantEventSeed(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(h[:])[:32]
}

func hexSha(seed string) string {
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:])[:24]
}

// deliverReceipt 投递回执(§8 HMAC 帧;重投刷新时间戳与签名,payload 不变)。
// source_app=本应用,target_app=来源应用(方向与 handoff 相反)。
func (s *Server) deliverReceipt(ctx context.Context, rec store.SourceReceipt, url string, secret []byte) {
	now := time.Now().UTC()
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := receiptdoc.FrameSign(secret, now.Unix(), s.Cfg.AppID, rec.TargetApp, rec.EventID, []byte(rec.Payload))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(rec.Payload))
	if err != nil {
		_, _ = s.St.FinishReceipt(ctx, rec.TenantScope, rec.ID, "failed", "回执请求构造失败")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(receiptdoc.HeaderKeyID, "product-image-receipt-key-1")
	httpReq.Header.Set(receiptdoc.HeaderTimestamp, ts)
	httpReq.Header.Set(receiptdoc.HeaderSignature, sig)
	httpReq.Header.Set(receiptdoc.HeaderAttempt, "1")
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq)
	if err != nil {
		_, _ = s.St.FinishReceipt(ctx, rec.TenantScope, rec.ID, "failed", "回执投递失败:"+err.Error())
		return
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		_, _ = s.St.FinishReceipt(ctx, rec.TenantScope, rec.ID, "delivered", "")
		return
	}
	_, _ = s.St.FinishReceipt(ctx, rec.TenantScope, rec.ID, "failed",
		fmt.Sprintf("回执被接收端拒绝: status %d", res.StatusCode))
}

// ---- POST /api/v1/receipts/{id}/resend + GET 回执查询 -----------------------

func (s *Server) handleListReceipts(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	list, err := s.St.ListReceipts(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取回执失败")
		return
	}
	if list == nil {
		list = []store.SourceReceipt{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"receipts": list})
}

func (s *Server) handleResendReceipt(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	rec, err := s.St.GetReceipt(ctx, p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if rec.Status == "delivered" {
		writeJSON(w, http.StatusOK, map[string]any{"receipt": rec, "resent": false,
			"note": "已投递;同 event_id 重投会被对端幂等去重,无需重发"})
		return
	}
	// 查询恢复:只重传既有 payload(同 event_id、内容不变);绝不重新生成、不扣费。
	if s.Registry == nil {
		writeErr(w, http.StatusServiceUnavailable, "registry_unavailable", "应用登记表未装配,无法解析回执目标")
		return
	}
	target, err := s.Registry.ResolveTarget(rec.TargetApp, s.receiptTargetIDFor(rec.TargetApp), appregistry.KindReceipt)
	if err != nil {
		writeErr(w, http.StatusConflict, "receipt_target_unregistered", "来源应用未登记 receipt target:"+err.Error())
		return
	}
	secret := s.Cfg.ReceiptKeyFor(rec.TargetApp)
	if secret == "" {
		writeErr(w, http.StatusServiceUnavailable, "receipt_key_missing", "未配置来源应用回执专用密钥(PRODUCT_RECEIPT_KEYS)")
		return
	}
	s.deliverReceipt(ctx, rec, target, []byte(secret))
	final, gerr := s.St.GetReceipt(ctx, p.Tenant(), rec.ID)
	if gerr != nil {
		writeStoreErr(w, gerr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"receipt": final, "resent": true})
}

// receiptTargetIDFor 登记表里目标应用的 receipt target id(FirstReceiptTarget 同源)。
func (s *Server) receiptTargetIDFor(appID string) string {
	if s.Registry == nil {
		return ""
	}
	if t, err := s.Registry.FirstReceiptTarget(appID); err == nil {
		return t.TargetID
	}
	return ""
}
