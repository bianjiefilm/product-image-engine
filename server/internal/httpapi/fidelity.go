package httpapi

// HUI-1698 FEAT-0199:主体保护记录。
// 本端点只保存保护范围和检查结论，不调用生成模型，不扣费。
// SupplierAuthorized 固定为 false：没有真实供应商授权时，
// 结论只能是未通过或待确认，不能把生产出图写成已经通过。

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

type fidelityRequest struct {
	Mode                 string           `json:"mode"`
	MethodVersion        string           `json:"method_version"`
	InputRef             string           `json:"input_ref"`
	InputVersion         string           `json:"input_version"`
	OutputRef            string           `json:"output_ref"`
	MaskRef              string           `json:"mask_ref"`
	AllowedRegions       []string         `json:"allowed_regions"`
	Protected            []string         `json:"protected"`
	SubjectEdit          bool             `json:"subject_edit"`
	SubjectEditConfirmed bool             `json:"subject_edit_confirmed"`
	ClaimsExactProduct   bool             `json:"claims_exact_product"`
	SimilarityOnly       bool             `json:"similarity_only"`
	MachineScope         string           `json:"machine_scope"`
	HumanScope           string           `json:"human_scope"`
	Checks               []fidelity.Check `json:"checks"`
	SampleClass          string           `json:"sample_class"`
}

func (s *Server) handleCreateFidelityReport(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req fidelityRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是主体保护字段，不能自报结论或授权")
		return
	}
	if req.MethodVersion == "" {
		req.MethodVersion = fidelity.MethodVersionV1
	}
	rep, err := fidelity.Evaluate(fidelity.Input{
		Mode: req.Mode, MethodVersion: req.MethodVersion,
		InputRef: req.InputRef, InputVersion: req.InputVersion, OutputRef: req.OutputRef,
		MaskRef: req.MaskRef, AllowedRegions: req.AllowedRegions, Protected: req.Protected,
		SubjectEdit: req.SubjectEdit, SubjectEditConfirmed: req.SubjectEditConfirmed,
		ClaimsExactProduct: req.ClaimsExactProduct, SimilarityOnly: req.SimilarityOnly,
		SupplierAuthorized: false,
		MachineScope:       req.MachineScope, HumanScope: req.HumanScope,
		Checks: req.Checks, SampleClass: req.SampleClass,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if rep.Verdict == fidelity.VerdictPass || rep.ExactProduct || rep.Deliverable {
		writeErr(w, http.StatusConflict, "fidelity_not_authorized",
			"真实生成保真未授权，不能写成已经通过")
		return
	}
	projID := r.PathValue("id")
	if rep.OutputRef != "" {
		out, oerr := s.St.GetOutput(r.Context(), p.Tenant(), rep.OutputRef)
		if oerr != nil || out.ProjectID != projID {
			writeErr(w, http.StatusNotFound, "not_found", "输出版本不存在(或不属于当前工程)")
			return
		}
	}
	saved, dup, err := s.St.SaveFidelityReport(r.Context(), p.Tenant(), projID, rep)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "assessment_conflict", "同一评估不能改写结论")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"report": saved, "duplicate": dup})
}

func (s *Server) handleListFidelityReports(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	list, err := s.St.ListFidelityReports(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if list == nil {
		list = []store.SavedFidelityReport{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": list})
}

func (s *Server) handleGetFidelityReport(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	got, err := s.St.GetFidelityReport(r.Context(), p.Tenant(), r.PathValue("reportId"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if got.ProjectID != r.PathValue("id") {
		writeErr(w, http.StatusNotFound, "not_found", "资源不存在(或不属于当前账号)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"report": got, "export": got.ExportDocument(),
	})
}

func fidelityReports(rows []store.SavedFidelityReport) []fidelity.Report {
	out := make([]fidelity.Report, len(rows))
	for i := range rows {
		out[i] = rows[i].Report
	}
	return out
}
