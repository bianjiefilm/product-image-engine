package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
	"github.com/bianjiefilm/product-image-engine/server/internal/lightscene"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func (s *Server) handleLightCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                      true,
		"default_mode":                 string(lightscene.ModeFidelity),
		"creative_available":           s.Cfg.LightCreativePathAvailable(),
		"billing_label":                lightscene.BillingPendingLabel,
		"production_generation_passed": false,
		"billing_passed":               false,
		"verified_product":             false,
		"delivery_readiness":           lightscene.ReadinessInternal,
		"honesty":                      lightscene.HonestyNotice,
		"subject_gate":                 "required",
	})
}

func (s *Server) handleLightAcceptance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"samples":                      lightscene.AcceptanceSamples(),
		"production_generation_passed": false,
		"billing_passed":               false,
		"verified_product":             false,
	})
}

func (s *Server) handleCreateLightScene(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		InputID                     string `json:"input_id"`
		Mode                        string `json:"mode"`
		ProtectedRegion             string `json:"protected_region"`
		LightingIntent              string `json:"lighting_intent"`
		ConfirmCreativeAfterFailure bool   `json:"confirm_creative_after_failure"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	projectID := r.PathValue("id")
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), projectID); err != nil {
		writeStoreErr(w, err)
		return
	}
	in, err := s.St.GetInput(r.Context(), p.Tenant(), projectID, strings.TrimSpace(req.InputID))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	version := in.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	if version == "" {
		version = in.ID
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == string(lightscene.ModeCreative) {
		latest, lerr := s.St.LatestLightJobForInput(r.Context(), p.Tenant(), projectID, in.ID)
		reports := s.lightReportsForInput(r.Context(), p.Tenant(), projectID, in.ID, in.PlatformAssetID)
		fidelityFailed := false
		for _, report := range reports {
			if report.Verdict == fidelity.VerdictFail {
				fidelityFailed = true
				break
			}
		}
		lightFailed := lerr == nil && latest.Mode == string(lightscene.ModeFidelity) &&
			(latest.JobStatus == lightscene.StatusFailed || latest.Quality == lightscene.QualityFail)
		if (fidelityFailed || lightFailed) && !req.ConfirmCreativeAfterFailure {
			writeErr(w, http.StatusConflict, "silent_creative_forbidden", "保真失败后不能静默改成整图生成")
			return
		}
	}
	q, err := lightscene.OpenQuote(lightscene.QuoteInput{
		TenantID: p.Tenant(), ProjectID: projectID, InputID: in.ID, InputVersion: version,
		Mode: mode, ProtectedRegion: req.ProtectedRegion, LightingIntent: req.LightingIntent,
		CreativePath: s.Cfg.LightCreativePathAvailable(), BillingConnected: false,
	})
	if errors.Is(err, lightscene.ErrCreativeUnavailable) {
		writeErr(w, http.StatusUnprocessableEntity, "creative_unavailable", err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if existing, ferr := s.St.FindLightJobByFingerprint(r.Context(), p.Tenant(), q.Fingerprint); ferr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(existing, true)})
		return
	} else if !errors.Is(ferr, store.ErrNotFound) {
		writeStoreErr(w, ferr)
		return
	}
	if err := s.St.InvalidateOpenLightQuotes(r.Context(), p.Tenant(), projectID, in.ID, q.Fingerprint); err != nil {
		writeStoreErr(w, err)
		return
	}
	job, err := s.St.InsertLightJob(r.Context(), store.LightJob{
		TenantID: p.Tenant(), ProjectID: projectID, InputID: in.ID, InputVersion: version,
		Mode: string(q.Mode), ProtectedRegion: q.ProtectedRegion, LightingIntent: q.LightingIntent,
		Fingerprint: q.Fingerprint, QuoteStatus: q.QuoteStatus, BillingLabel: q.BillingLabel,
		JobStatus: lightscene.StatusQuoted, Quality: lightscene.QualityUnknown, Evidence: lightscene.EvidenceNone,
		Pending: []string{"主体保护待确认"}, AllowedUses: []string{"预览"},
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": s.lightView(job, false)})
}

func (s *Server) handleListLightScene(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), r.PathValue("id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	jobs, err := s.St.ListLightJobs(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取光影场景失败")
		return
	}
	views := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, s.lightView(job, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views, "billing_label": lightscene.BillingPendingLabel})
}

func (s *Server) handleConfirmLightScene(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadLightJob(w, r)
	if !ok {
		return
	}
	var req lightForm
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if lightFormMismatch(job, req) {
		writeErr(w, http.StatusConflict, "quote_stale", "报价与当前模式、输入或光影意图不一致,请重新生成报价")
		return
	}
	if job.QuoteStatus == lightscene.QuoteInvalid {
		writeErr(w, http.StatusConflict, "quote_invalid", lightscene.ErrQuoteInvalid.Error())
		return
	}
	if job.JobStatus != lightscene.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, true)})
		return
	}
	job.QuoteStatus = lightscene.QuoteConfirmed
	if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
}

func (s *Server) handleSubmitLightScene(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	job, ok := s.loadLightJob(w, r)
	if !ok {
		return
	}
	var req lightForm
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if lightFormMismatch(job, req) {
		writeErr(w, http.StatusConflict, "quote_stale", "报价与当前模式、输入或光影意图不一致,请重新生成报价")
		return
	}
	if job.JobStatus != lightscene.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, true)})
		return
	}
	if job.QuoteStatus == lightscene.QuoteInvalid {
		writeErr(w, http.StatusConflict, "quote_invalid", lightscene.ErrQuoteInvalid.Error())
		return
	}
	if job.QuoteStatus != lightscene.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", lightscene.ErrQuoteUnconfirmed.Error())
		return
	}
	claimed, err := s.St.ClaimLightSubmit(r.Context(), p.Tenant(), job.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !claimed {
		latest, lerr := s.St.GetLightJob(r.Context(), p.Tenant(), job.ProjectID, job.ID)
		if lerr != nil {
			writeStoreErr(w, lerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(latest, true)})
		return
	}
	genSt, _ := s.Cfg.GenerationUsable()
	if genSt != 0 {
		job.JobStatus = lightscene.StatusFailed
		job.Evidence = lightscene.EvidenceNone
		job.Quality = lightscene.QualityUnknown
		job.Deliverable = false
		job.VerifiedProduct = false
		job.Pending = append(job.Pending, "没有真实供应商，未执行生成")
		if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
		return
	}
	res, err := s.Tasks.Submit(r.Context(), platform.SubmitRequest{
		IdempotencyKey: job.Fingerprint,
		Kind:           "product-image-light-scene",
		Payload: map[string]any{
			"project_id": job.ProjectID, "input_id": job.InputID, "input_version": job.InputVersion,
			"mode": job.Mode, "protected_region": job.ProtectedRegion, "lighting_intent": job.LightingIntent,
			"tenant_id": p.Tenant(),
		},
	})
	if err != nil || strings.TrimSpace(res.TaskID) == "" {
		job.JobStatus = lightscene.StatusUnknown
		job.Evidence = lightscene.EvidenceNone
		job.Quality = keepFail(job.Quality)
		job.Deliverable = false
		job.VerifiedProduct = false
		job.Pending = append(job.Pending, "供应商结果未知，先核对")
		if uerr := s.St.UpdateLightJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
		return
	}
	job.JobStatus = lightscene.StatusQueued
	job.PlatformTaskID = res.TaskID
	job.Evidence = lightscene.EvidencePlatformTask
	job.Quality = keepFail(job.Quality)
	job.Deliverable = false
	job.VerifiedProduct = false
	if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
}

func (s *Server) handleRefreshLightScene(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadLightJob(w, r)
	if !ok {
		return
	}
	if job.JobStatus == lightscene.StatusFailed || job.Quality == lightscene.QualityFail {
		changed := job.JobStatus != lightscene.StatusFailed || job.VerifiedProduct || job.Deliverable
		job.JobStatus = lightscene.StatusFailed
		job.VerifiedProduct = false
		job.Deliverable = false
		if changed {
			if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
				writeStoreErr(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, true)})
		return
	}
	if job.PlatformTaskID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, true)})
		return
	}
	st, err := s.Tasks.Status(r.Context(), job.PlatformTaskID)
	if err != nil {
		job.JobStatus = lightscene.StatusUnknown
		job.VerifiedProduct = false
		job.Deliverable = false
		job.Pending = lightscene.AppendPending(job.Pending, "供应商结果未知，先核对")
		if uerr := s.St.UpdateLightJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, true)})
		return
	}
	switch st.Status {
	case "failed":
		job.JobStatus = lightscene.StatusFailed
	default:
		// 平台成功或仍在跑都先待核对。输出编号不是供应商回执。
		job.JobStatus = lightscene.StatusUnknown
		if receipt, _ := st.Result["vendor_receipt_id"].(string); strings.TrimSpace(receipt) != "" {
			job.VendorReceiptID = strings.TrimSpace(receipt)
		}
		job.Pending = lightscene.AppendPending(job.Pending, "供应商结果待核对")
	}
	job.VerifiedProduct = false
	job.Deliverable = false
	job = s.persistLightGate(r, job)
	if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
}

func (s *Server) handleLightQuality(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadLightJob(w, r)
	if !ok {
		return
	}
	var req struct {
		Verdict string `json:"verdict"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	next, err := lightscene.ApplyClientVerdict(executionFromLight(job), req.Verdict)
	if err != nil {
		code := "quality_pass_unauthorized"
		if errors.Is(err, lightscene.ErrRelabelForbidden) {
			code = "quality_relabel_forbidden"
		}
		writeErr(w, http.StatusConflict, code, err.Error())
		return
	}
	if job.Quality == lightscene.QualityFail && next.Quality == lightscene.QualityFail {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, true)})
		return
	}
	job.Quality = next.Quality
	job.Deliverable = false
	job.VerifiedProduct = false
	if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
}

func (s *Server) handleSelectLightScene(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadLightJob(w, r)
	if !ok {
		return
	}
	priorVerified := job.VerifiedProduct
	job = s.persistLightGate(r, job)
	next, err := lightscene.Select(executionFromLight(job))
	if err != nil {
		if job.VerifiedProduct && !priorVerified {
			job.VerifiedProduct = false
			job.Deliverable = false
			if job.Quality == lightscene.QualityPass {
				job.Quality = lightscene.QualityUnknown
			}
		}
		_ = s.St.UpdateLightJob(r.Context(), job)
		writeErr(w, http.StatusConflict, "not_deliverable", err.Error())
		return
	}
	job.Selection = next.Selection
	job.Deliverable = next.Deliverable
	job.VerifiedProduct = next.VerifiedProduct
	job.Pending = next.Pending
	job.AllowedUses = next.AllowedUses
	if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.lightView(job, false)})
}

func (s *Server) handleExportLightScene(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadLightJob(w, r)
	if !ok {
		return
	}
	job = s.persistLightGate(r, job)
	rec, err := lightscene.Export(executionFromLight(job))
	if err != nil {
		_ = s.St.UpdateLightJob(r.Context(), job)
		writeErr(w, http.StatusConflict, "export_blocked", err.Error())
		return
	}
	job.ExportCount++
	if err := s.St.UpdateLightJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	view := s.lightView(job, false)
	view["output_asset_id"] = rec.OutputAssetID
	view["output_version"] = rec.OutputVersion
	view["starts_generation"] = rec.StartsGeneration
	writeJSON(w, http.StatusOK, map[string]any{"job": view})
}

func (s *Server) loadLightJob(w http.ResponseWriter, r *http.Request) (store.LightJob, bool) {
	p, _ := principalFrom(r.Context())
	job, err := s.St.GetLightJob(r.Context(), p.Tenant(), r.PathValue("id"), r.PathValue("jobId"))
	if err != nil {
		writeStoreErr(w, err)
		return store.LightJob{}, false
	}
	return job, true
}

func (s *Server) persistLightGate(r *http.Request, job store.LightJob) store.LightJob {
	p, _ := principalFrom(r.Context())
	assetID := ""
	if in, err := s.St.GetInput(r.Context(), p.Tenant(), job.ProjectID, job.InputID); err == nil {
		assetID = in.PlatformAssetID
	}
	reports := s.lightReportsForInput(r.Context(), p.Tenant(), job.ProjectID, job.InputID, assetID)
	gated := lightscene.SubjectGate(executionFromLight(job), reports)
	job.Quality = gated.Quality
	job.Pending = gated.Pending
	job.AllowedUses = gated.AllowedUses
	job.Deliverable = gated.Deliverable
	job.VerifiedProduct = gated.VerifiedProduct
	return job
}

func (s *Server) lightReportsForInput(ctx context.Context, tenantID, projectID, inputID, assetID string) []fidelity.Report {
	list, err := s.St.ListFidelityReports(ctx, tenantID, projectID)
	if err != nil {
		return nil
	}
	out := make([]fidelity.Report, 0)
	for _, row := range list {
		if row.InputRef == inputID || (assetID != "" && row.InputRef == assetID) {
			out = append(out, row.Report)
		}
	}
	return out
}

func (s *Server) lightView(job store.LightJob, idempotent bool) map[string]any {
	if job.BillingLabel == "" {
		job.BillingLabel = lightscene.BillingPendingLabel
	}
	return map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "input_id": job.InputID,
		"input_version": job.InputVersion, "mode": job.Mode, "protected_region": job.ProtectedRegion,
		"lighting_intent": job.LightingIntent, "fingerprint": job.Fingerprint,
		"quote_status": job.QuoteStatus, "billing_label": job.BillingLabel,
		"job_status": job.JobStatus, "quality": job.Quality, "evidence": job.Evidence,
		"platform_task_id": job.PlatformTaskID, "output_asset_id": job.OutputAssetID,
		"output_version": job.OutputVersion, "vendor_receipt_id": job.VendorReceiptID,
		"selection": job.Selection, "export_count": job.ExportCount,
		"deliverable": job.Deliverable, "verified_product": job.VerifiedProduct,
		"pending": job.Pending, "allowed_uses": job.AllowedUses,
		"production_generation_passed": false, "billing_passed": false,
		"delivery_readiness": lightscene.ReadinessInternal, "honesty": lightscene.HonestyNotice,
		"idempotent": idempotent,
	}
}

type lightForm struct {
	Mode           string `json:"mode"`
	InputID        string `json:"input_id"`
	LightingIntent string `json:"lighting_intent"`
}

func lightFormMismatch(job store.LightJob, req lightForm) bool {
	if req.Mode != "" && req.Mode != job.Mode {
		return true
	}
	if req.InputID != "" && req.InputID != job.InputID {
		return true
	}
	if req.LightingIntent != "" && req.LightingIntent != job.LightingIntent {
		return true
	}
	return false
}

func executionFromLight(job store.LightJob) lightscene.Execution {
	return lightscene.Execution{
		Status: job.JobStatus, Mode: lightscene.Mode(job.Mode), Quality: job.Quality,
		Evidence: job.Evidence, Deliverable: job.Deliverable, VerifiedProduct: job.VerifiedProduct,
		OutputAssetID: job.OutputAssetID, OutputVersion: job.OutputVersion,
		VendorReceiptID: job.VendorReceiptID, Selection: job.Selection, Pending: job.Pending,
		AllowedUses: job.AllowedUses, DeliveryReadiness: lightscene.ReadinessInternal,
	}
}

func keepFail(quality string) string {
	if quality == lightscene.QualityFail {
		return lightscene.QualityFail
	}
	if quality == "" {
		return lightscene.QualityUnknown
	}
	return quality
}
