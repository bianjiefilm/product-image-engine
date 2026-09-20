package httpapi

// 产品图模板库端点(HUI-1705 / FEAT-0206)。
//
// 红线(拍板):
//   - 模板=确定性预设+参数引用集:schema 禁止任何生成参数字段(prompt/model/…),
//     请求体先过 imagetmpl.ScanForbiddenKeys,命中即 422 forbidden_field;
//   - 一键套用=登记制:只把参数引用落到工程配置写点 + 写留痕,零触发生成、
//     零扣费、不改工程既有产物;同 (工程,模板,版本) 重复套用幂等不重复写;
//   - 双层库:内置模板(TplBuiltins,只读带版本;改/删 → 403 builtin_readonly)
//     + 租户自定义(CRUD,可从内置派生);跨租户 404/0 行;
//   - FEATURE_TEMPLATES off → 路由不注册(server.go)= 404 不可见(fail-closed)。

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/imagetmpl"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

const tplBodyLimit = 1 << 20 // 模板/套用请求体上限,与工程域同口径

// tplPresetExists 参数引用存在性判定(装配层持 PresetSet;nil 视为未装配 fail-closed)。
func (s *Server) tplPresetExists() (func(string) bool, *apiErr) {
	if s.Presets == nil {
		return nil, &apiErr{http.StatusServiceUnavailable, "presets_unavailable",
			"尺寸适配预设集未装配,无法校验模板尺寸预设引用(fail-closed)"}
	}
	return func(name string) bool { _, ok := s.Presets.Get(name); return ok }, nil
}

// readTplBody 读体 + 红线扫描 + 严格解码(未知字段拒绝)。
// 顺序:坏 JSON/未知字段 → 400;含生成参数字段 → 422。
func readTplBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, tplBodyLimit))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON(≤1MiB)")
		return false
	}
	if err := imagetmpl.ScanForbiddenKeys(raw); err != nil {
		if errors.Is(err, imagetmpl.ErrForbiddenField) {
			writeErr(w, http.StatusUnprocessableEntity, "forbidden_field",
				"模板含生成参数字段,模板库只收确定性预设参数(尺寸预设引用+纯数值)")
			return false
		}
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是合法 JSON")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体字段不符合模板 schema:"+err.Error())
		return false
	}
	return true
}

// tplResolve 解析模板:内置(TplBuiltins)优先,其余按 (租户,id) 查自定义。
func (s *Server) tplResolve(w http.ResponseWriter, r *http.Request, tenant, id string) (imagetmpl.Template, bool) {
	if imagetmpl.IsBuiltinID(id) {
		if s.TplBuiltins == nil {
			writeErr(w, http.StatusServiceUnavailable, "templates_unavailable", "内置模板集未装配,拒绝服务(fail-closed)")
			return imagetmpl.Template{}, false
		}
		tp, ok := s.TplBuiltins.Get(id)
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", "模板不存在")
			return imagetmpl.Template{}, false
		}
		return tp, true
	}
	tp, err := s.St.GetImageTemplate(r.Context(), tenant, id)
	if err != nil {
		writeStoreErr(w, err)
		return imagetmpl.Template{}, false
	}
	return tp, true
}

// ---- GET /api/v1/image-templates --------------------------------------------

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	out := []imagetmpl.Template{}
	if s.TplBuiltins != nil {
		out = append(out, s.TplBuiltins.List()...)
	}
	customs, err := s.St.ListImageTemplates(r.Context(), p.Tenant())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取自定义模板失败")
		return
	}
	out = append(out, customs...)
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

// ---- GET /api/v1/image-templates/{id} ---------------------------------------

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tp, ok := s.tplResolve(w, r, p.Tenant(), r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, tp)
}

// ---- POST /api/v1/image-templates -------------------------------------------

type tplCreateRequest struct {
	Name       string            `json:"name"`
	Industry   string            `json:"industry"`
	Label      string            `json:"label"`
	Params     *imagetmpl.Params `json:"params"`
	DeriveFrom string            `json:"derive_from"` // 可选:从内置派生(复制参数作起点)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req tplCreateRequest
	if !readTplBody(w, r, &req) {
		return
	}
	t := imagetmpl.Template{
		TenantScope: p.Tenant(), Industry: req.Industry,
		Name: req.Name, Label: req.Label, Version: 1, CreatedBy: p.UserID,
	}
	// 派生:仅允许内置作派生源(拍板:从内置派生或空白新建)。
	if strings.TrimSpace(req.DeriveFrom) != "" {
		srcID := strings.TrimSpace(req.DeriveFrom)
		if !imagetmpl.IsBuiltinID(srcID) {
			writeErr(w, http.StatusBadRequest, "invalid_request", "derive_from 仅支持内置模板 id(tplbi_ 前缀)")
			return
		}
		if s.TplBuiltins == nil {
			writeErr(w, http.StatusServiceUnavailable, "templates_unavailable", "内置模板集未装配,拒绝服务(fail-closed)")
			return
		}
		src, ok := s.TplBuiltins.Get(srcID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_request", "derive_from 指向的内置模板不存在")
			return
		}
		t.Industry, t.Label, t.Params = src.Industry, src.Label, src.Params
	}
	if req.Params != nil {
		t.Params = *req.Params
	}
	exists, aerr := s.tplPresetExists()
	if aerr != nil {
		aerr.write(w)
		return
	}
	if err := imagetmpl.ValidateParams(t.Params, exists); err != nil {
		if strings.Contains(err.Error(), "引用不存在") {
			writeErr(w, http.StatusBadRequest, "unknown_size_preset",
				"size_preset 引用不存在:"+err.Error()+"(GET /api/v1/size-adapt/presets 查看可用列表)")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	created, err := s.St.CreateImageTemplate(r.Context(), t)
	if err != nil {
		writeTplStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// ---- PATCH /api/v1/image-templates/{id} -------------------------------------

type tplUpdateRequest struct {
	Name   *string           `json:"name"`
	Label  *string           `json:"label"`
	Params *imagetmpl.Params `json:"params"` // 全量替换(params 是定长结构,无局部合并)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	id := r.PathValue("id")
	if imagetmpl.IsBuiltinID(id) {
		writeErr(w, http.StatusForbidden, "builtin_readonly", "内置模板只读,不可修改;可从内置派生出自定义模板")
		return
	}
	var req tplUpdateRequest
	if !readTplBody(w, r, &req) {
		return
	}
	if req.Params != nil {
		exists, aerr := s.tplPresetExists()
		if aerr != nil {
			aerr.write(w)
			return
		}
		if err := imagetmpl.ValidateParams(*req.Params, exists); err != nil {
			if strings.Contains(err.Error(), "引用不存在") {
				writeErr(w, http.StatusBadRequest, "unknown_size_preset",
					"size_preset 引用不存在:"+err.Error()+"(GET /api/v1/size-adapt/presets 查看可用列表)")
				return
			}
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	updated, err := s.St.UpdateImageTemplate(r.Context(), p.Tenant(), id, store.ImageTemplateUpdate{
		Name: req.Name, Label: req.Label, Params: req.Params,
	})
	if err != nil {
		writeTplStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ---- DELETE /api/v1/image-templates/{id} ------------------------------------

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	id := r.PathValue("id")
	if imagetmpl.IsBuiltinID(id) {
		writeErr(w, http.StatusForbidden, "builtin_readonly", "内置模板只读,不可删除")
		return
	}
	if err := s.St.DeleteImageTemplate(r.Context(), p.Tenant(), id); err != nil {
		writeTplStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- POST /api/v1/projects/{id}/image-template/apply ------------------------

func (s *Server) handleApplyTemplate(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant, projID := p.Tenant(), r.PathValue("id")
	var req struct {
		TemplateID string `json:"template_id"`
	}
	if !readTplBody(w, r, &req) {
		return
	}
	tplID := strings.TrimSpace(req.TemplateID)
	if tplID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "template_id 必填")
		return
	}
	// 1) 工程租户门(403/404 分类,沿用工程域口径)。
	if done := s.writeProjectAccessErr(w, r.Context(), tenant, projID); done {
		return
	}
	// 2) 模板解析(内置或本租户自定义;跨租户/不存在 404)。
	tp, ok := s.tplResolve(w, r, tenant, tplID)
	if !ok {
		return
	}
	// 3) 登记制落点:留痕 + 工程参数引用写点(单事务;同键幂等)。
	paramsJSON, err := json.Marshal(tp.Params)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "服务内部错误")
		return
	}
	apply, dup, err := s.St.ApplyImageTemplate(r.Context(), store.ImageTemplateApply{
		TenantScope: tenant, ProjectID: projID,
		TemplateID: tp.ID, TemplateVersion: tp.Version,
		Params: paramsJSON, AppliedBy: p.UserID,
	})
	if err != nil {
		writeTplStoreErr(w, err)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"apply": apply, "duplicate": dup})
}

// ---- GET /api/v1/projects/{id}/image-template-applies -----------------------

func (s *Server) handleListTemplateApplies(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant, projID := p.Tenant(), r.PathValue("id")
	if done := s.writeProjectAccessErr(w, r.Context(), tenant, projID); done {
		return
	}
	list, err := s.St.ListTemplateApplies(r.Context(), tenant, projID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取套用留痕失败")
		return
	}
	if list == nil {
		list = []store.ImageTemplateApply{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"applies": list})
}

// ---- 错误映射 ----------------------------------------------------------------

// writeTplStoreErr 模板域 store 错误 → HTTP(冲突 409;其余沿用工程域口径)。
func writeTplStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "name_conflict", "同租户已有同名模板,请换一个名称")
	default:
		writeStoreErr(w, err)
	}
}
