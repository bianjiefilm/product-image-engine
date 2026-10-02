package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sourceflow"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// Stable HTTP reason codes for the limited-fidelity mode. They are API.
const plateScope = fidelitysamples.Scope

// frozenCaseFor resolves a project input to a frozen sample from LOCAL facts
// only (the product's own photo registry). It never calls a remote service,
// so capabilities stay a pure read and a miss costs nothing.
func (s *Server) frozenCaseFor(ctx context.Context, in store.ProjectInput, principalTenant string) (*fidelitysamples.LoadedCase, bool) {
	if s.Source == nil || s.Source.Samples == nil || in.PlatformAssetID == "" {
		return nil, false
	}
	seen := map[string]bool{}
	for _, tenant := range []string{in.TenantID, principalTenant} {
		if tenant == "" || seen[tenant] {
			continue
		}
		seen[tenant] = true
		if pa, err := s.St.GetPhotoAssetByAssetID(ctx, tenant, in.PlatformAssetID); err == nil {
			if c, ok := s.Source.Samples.BySHA(pa.SHA256); ok {
				return c, true
			}
		}
	}
	return nil, false
}

func canvasView(c fidelitysamples.Canvas) map[string]any {
	return map[string]any{"width_px": c.W, "height_px": c.H}
}

// plateCapability is the capabilities entry for background_plate_lock. It
// reports readiness and which of THIS project's inputs are frozen samples.
func (s *Server) plateCapability(ctx context.Context, auth sourceflow.Authorization, project store.Project) map[string]any {
	entry := map[string]any{"mode": si.ModePlateLock, "size": "1024*1024", "model": "qwen-image-2.0", "claim": plateScope, "fidelity": "frozen_sample_only", "visual_quality": "unknown", "supported_input_ids": []string{}, "supported_inputs": []any{}}
	ready, reason := s.Source.PlateReady(auth.PayerSource)
	switch {
	case project.Status == "archived":
		ready, reason = false, "project_archived"
	case !auth.CanWrite:
		ready, reason = false, "project_write_forbidden"
	case !ready && reason == "source_unconfigured" && !s.Source.OutputRecoveryReady:
		reason = s.sourceRuntimeReason()
	}
	entry["ready"], entry["disabled"], entry["reason"] = ready, !ready, reason
	entry["can_quote"], entry["can_confirm"] = ready, ready
	if ready {
		ids, views := []string{}, []any{}
		inputs, err := s.St.ListInputs(ctx, auth.Scope.TenantID, auth.Scope.ProjectID)
		if err == nil {
			p, _ := principalFrom(ctx)
			for _, in := range inputs {
				if c, ok := s.frozenCaseFor(ctx, in, p.Tenant()); ok {
					ids = append(ids, in.ID)
					views = append(views, map[string]any{"input_id": in.ID, "kind": c.Kind, "canvas": canvasView(c.Canvas)})
				}
			}
		}
		entry["supported_input_ids"], entry["supported_inputs"] = ids, views
	}
	return entry
}

// readOriginal reads the bytes behind a project input from Upload. Tests
// replace the seam; production uses the same verified read as background jobs.
func (s *Server) readOriginal(ctx context.Context, p platform.Principal, tenant, project, input string) ([]byte, error) {
	if s.ReadOriginal != nil {
		return s.ReadOriginal(ctx, p, tenant, project, input)
	}
	return s.readInputOriginal(ctx, p, tenant, project, input)
}

func (s *Server) handleSourcePlateCreate(w http.ResponseWriter, r *http.Request, body map[string]string) {
	actor, _ := sourceActor(r)
	p, _ := principalFrom(r.Context())
	project := r.PathValue("id")
	auth, e := s.Source.Auth.ForProject(r.Context(), actor, project, true)
	if e != nil {
		if errors.Is(e, si.ErrNotFound) {
			if current, re := s.Source.Auth.ForProject(r.Context(), actor, project, false); re == nil && !current.CanWrite {
				e = si.ErrForbidden
			}
		}
		s.sourceResult(w, r, si.Run{}, e)
		return
	}
	if ready, reason := s.Source.PlateReady(auth.PayerSource); !ready {
		writeErr(w, 503, reason, "限定保真换背景当前不可用")
		return
	}
	inputID := body["input_id"]
	if !sourceSegment(inputID) || !si.ValidIntentText(body["background_intent"]) {
		writeErr(w, 400, "invalid_request", "素材编号或背景描述不合法")
		return
	}
	in, err := s.St.GetInput(r.Context(), auth.Scope.TenantID, project, inputID)
	if err != nil {
		s.sourceResult(w, r, si.Run{}, si.ErrNotFound)
		return
	}
	c, ok := s.frozenCaseFor(r.Context(), in, p.Tenant())
	if !ok {
		s.sourceResult(w, r, si.Run{}, si.ErrNotFrozen)
		return
	}
	// The registry says it is a frozen sample; prove it against the real bytes
	// before anything is priced.
	original, err := s.readOriginal(r.Context(), p, auth.Scope.TenantID, project, inputID)
	if err != nil {
		writeErr(w, 409, "original_missing", "原图缺失，无法核对")
		return
	}
	if si.SHA256Hex(original) != c.Original.SHA256 {
		s.sourceResult(w, r, si.Run{}, si.ErrNotFrozen)
		return
	}
	run, e := s.Source.CreatePlate(r.Context(), actor, project, sourceflow.PlateRequest{
		RequestKey: body["request_key"], InputID: inputID, BackgroundIntent: body["background_intent"], OriginalSHA256: c.Original.SHA256,
	})
	s.sourceResult(w, r, run, e)
}

// plateView is the public projection of a plate-lock run's derived result.
// Nothing here is a score; states are the four-valued axes of the frozen report.
func (s *Server) plateView(ctx context.Context, run si.Run) map[string]any {
	out := map[string]any{"result_kind": "derived_composite", "claim": plateScope, "human_usefulness": "NOT_RUN", "derivation_state": "pending", "candidate_state": si.CandidatePending}
	if s.Source == nil || s.Source.Store == nil {
		return out
	}
	frozen, err := s.Source.Store.GetPlateInput(ctx, run)
	if err != nil {
		out["derivation_state"], out["candidate_state"] = "invalid", si.CandidateBlocked
		return out
	}
	out["sample_set_id"], out["case_id"], out["background_intent"] = frozen.SetID, frozen.CaseID, frozen.BackgroundIntent
	out["canvas"] = map[string]any{"width_px": frozen.CanvasW, "height_px": frozen.CanvasH}
	if run.Output != nil && !run.Deleted {
		out["plate_task"] = map[string]any{"task_id": run.TaskID, "asset_id": run.Output.AssetID, "reference_id": run.Output.ReferenceID, "sha256": run.Output.SHA256}
	}
	d, found, err := s.Source.Store.GetPlateDerivation(ctx, run.ID)
	if err != nil {
		out["derivation_state"], out["candidate_state"] = "invalid", si.CandidateBlocked
		return out
	}
	if !found {
		return out
	}
	out["derivation_state"], out["candidate_state"] = d.State, d.CandidateState
	if d.State == si.DerivationBlocked {
		out["blocked_reason"] = d.BlockedReason
		return out
	}
	rep, err := platelock.DecodeReport([]byte(d.ReportJSON))
	if err != nil {
		out["derivation_state"], out["candidate_state"] = "invalid", si.CandidateBlocked
		return out
	}
	axes := []any{}
	for _, a := range rep.Axes {
		axes = append(axes, map[string]any{"name": a.Name, "state": a.State, "machine_checked": a.MachineChecked, "evidence": a.Evidence})
	}
	out["verdicts"] = map[string]any{"fidelity": rep.Verdicts.Fidelity, "generation": rep.Verdicts.Generation, "billing": rep.Verdicts.Billing, "human_usefulness": rep.Verdicts.HumanUsefulness}
	out["axes"], out["limits"] = axes, rep.Limits
	out["report_sha256"], out["composite_sha256"] = d.ReportSHA256, d.SHA256
	if len(d.PNG) > 0 && !run.Deleted {
		out["result"] = map[string]any{"content_type": "image/png", "size_bytes": strconv.FormatInt(d.SizeBytes, 10), "width_px": d.Width, "height_px": d.Height, "sha256": d.SHA256}
	}
	return out
}

func qualityHeader(candidate string) string {
	switch candidate {
	case si.CandidateLimited:
		return "limited"
	case si.CandidateNotUsable:
		return "fail"
	}
	return "pending"
}

// handlePlateContent streams the derived composite from the product's own
// store (digest re-checked on read). A failed candidate may be looked at, with
// a verdict header, so the user can see why it is unusable; it is never
// selectable or exportable.
func (s *Server) handlePlateContent(w http.ResponseWriter, r *http.Request) {
	actor, _ := sourceActor(r)
	pc, e := s.Source.LoadPlate(r.Context(), actor, r.PathValue("id"), r.PathValue("runId"))
	if e != nil {
		s.sourceResult(w, r, pc.Run, e)
		return
	}
	if pc.Derivation.State != si.DerivationDerived || len(pc.Derivation.PNG) == 0 {
		writeErr(w, 409, "no_composite", "没有可显示的合成结果")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(pc.Derivation.PNG)))
	w.Header().Set("Content-Disposition", `inline; filename="background-replaced.png"`)
	w.Header().Set("X-Quality-Verdict", qualityHeader(pc.Derivation.CandidateState))
	_, _ = w.Write(pc.Derivation.PNG)
}

// plateReport returns the frozen report only if it is canonical and bound to
// the stored composite (the "recompute and compare" gate).
func plateReport(d si.PlateDerivation) (platelock.Report, error) {
	rep, err := platelock.DecodeReport([]byte(d.ReportJSON))
	if err != nil || rep.Facts.CompositeSHA256 != d.SHA256 || rep.CandidateState != d.CandidateState || si.SHA256Hex([]byte(d.ReportJSON)) != d.ReportSHA256 {
		return platelock.Report{}, si.ErrInvariant
	}
	return rep, nil
}

func (s *Server) handlePlateExport(w http.ResponseWriter, r *http.Request) {
	actor, _ := sourceActor(r)
	pc, e := s.Source.LoadPlate(r.Context(), actor, r.PathValue("id"), r.PathValue("runId"))
	if e != nil {
		s.sourceResult(w, r, pc.Run, e)
		return
	}
	if pc.Derivation.CandidateState != si.CandidateLimited {
		s.sourceResult(w, r, pc.Run, si.ErrNotUsable)
		return
	}
	if _, e = plateReport(pc.Derivation); e != nil {
		s.sourceResult(w, r, pc.Run, e)
		return
	}
	snapshot, _ := json.Marshal(map[string]any{"report_version": "product-source-export/v2", "run": s.sourceRuntimeView(r.Context(), pc.Run), "this_download_billing_check": "charged", "result_sha256": pc.Derivation.SHA256, "human_usefulness": "NOT_RUN"})
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, entry := range []struct {
		name string
		raw  []byte
	}{{"image.png", pc.Derivation.PNG}, {"quality.json", []byte(pc.Derivation.ReportJSON)}, {"snapshot.json", snapshot}} {
		f, err := archive.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			writeErr(w, 500, "internal", "导出失败")
			return
		}
		if _, err = f.Write(entry.raw); err != nil {
			writeErr(w, 500, "internal", "导出失败")
			return
		}
	}
	if archive.Close() != nil {
		writeErr(w, 500, "internal", "导出失败")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="background-replaced.zip"`)
	_, _ = w.Write(buf.Bytes())
}

// handleSourceReturn registers the selected, limited-candidate composite as a
// project output so it can be returned to an order or campaign. It is the only
// door for derived bytes into project outputs; it never generates or charges.
func (s *Server) handleSourceReturn(w http.ResponseWriter, r *http.Request) {
	if !sourceMethod(w, r, http.MethodPost) {
		return
	}
	if _, ok := sourceBody(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	run, ok := s.sourceLoad(w, r, true)
	if !ok {
		return
	}
	actor, _ := sourceActor(r)
	p, _ := principalFrom(r.Context())
	pc, e := s.Source.LoadPlate(r.Context(), actor, r.PathValue("id"), run.ID)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	if pc.Derivation.CandidateState != si.CandidateLimited {
		s.sourceResult(w, r, run, si.ErrNotUsable)
		return
	}
	if !pc.Run.Selected {
		writeErr(w, 409, "not_selected", "请先选定这张结果")
		return
	}
	rep, e := plateReport(pc.Derivation)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	projID := r.PathValue("id")
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), projID); err != nil {
		writeStoreErr(w, err)
		return
	}
	if _, err := s.St.GetBindingByProject(r.Context(), p.Tenant(), projID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusConflict, "no_source_binding", "该工程无来源绑定(standalone 完整保留在本应用,不回传)")
		return
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}
	out, dup, aerr := s.registerOutputBytesFor(r.Context(), p.Tenant(), projID, "background-replaced.png", "image/png", pc.Derivation.PNG, s.bindingOutputContext, run.ID)
	if aerr != nil {
		aerr.write(w)
		return
	}
	scope, _ := json.Marshal(rep.Scope)
	limits, _ := json.Marshal(rep.Limits)
	q, err := s.St.SaveOutputQuality(r.Context(), store.OutputQuality{
		OutputID: out.ID, TenantScope: p.Tenant(), ProjectID: projID, RunID: run.ID, ResultSHA256: out.ResultSHA256,
		CandidateState: pc.Derivation.CandidateState, OriginalChargeID: rep.Facts.OriginalChargeID,
		ScopeJSON: string(scope), LimitsJSON: string(limits), ReportSHA256: pc.Derivation.ReportSHA256,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"output": out, "quality": qualityBindingView(q), "duplicate": dup})
}

func qualityBindingView(q store.OutputQuality) map[string]any {
	var scope map[string]any
	var limits []string
	_ = json.Unmarshal([]byte(q.ScopeJSON), &scope)
	_ = json.Unmarshal([]byte(q.LimitsJSON), &limits)
	return map[string]any{"output_id": q.OutputID, "run_id": q.RunID, "candidate_state": q.CandidateState, "scope": scope, "limits": limits, "report_sha256": q.ReportSHA256, "result_sha256": q.ResultSHA256, "receipt_includes_limits": false}
}
