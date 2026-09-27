package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func (s *Server) handleBgCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                      true,
		"default_mode":                 string(bgreplace.ModeFidelity),
		"creative_available":           s.Cfg.CreativePathAvailable(),
		"billing_label":                bgreplace.BillingPendingLabel,
		"production_generation_passed": false,
		"billing_passed":               false,
		"delivery_readiness":           bgreplace.ReadinessInternal,
		"honesty":                      bgreplace.HonestyNotice,
	})
}

func (s *Server) handleBgAcceptance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"samples":                      bgreplace.AcceptanceSamples(),
		"production_generation_passed": false,
		"billing_passed":               false,
	})
}

func (s *Server) handleCreateBgReplace(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		InputID          string `json:"input_id"`
		Mode             string `json:"mode"`
		ProtectedRegion  string `json:"protected_region"`
		BackgroundIntent string `json:"background_intent"`
		ExplicitCreative bool   `json:"explicit_creative"`
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
	if mode == string(bgreplace.ModeCreative) && !req.ExplicitCreative {
		latest, lerr := s.St.LatestBgJobForInput(r.Context(), p.Tenant(), projectID, in.ID)
		if lerr == nil && latest.Mode == string(bgreplace.ModeFidelity) &&
			(latest.JobStatus == bgreplace.StatusFailed || latest.Quality == bgreplace.QualityFail) {
			writeErr(w, http.StatusConflict, "silent_creative_forbidden", "保真失败后不能静默改成整图生成")
			return
		}
	}
	q, err := bgreplace.OpenQuote(bgreplace.QuoteInput{
		TenantID: p.Tenant(), ProjectID: projectID, InputID: in.ID, InputVersion: version,
		Mode: mode, ProtectedRegion: req.ProtectedRegion, BackgroundIntent: req.BackgroundIntent,
		CreativePath: s.Cfg.CreativePathAvailable(), BillingConnected: false,
	})
	if errors.Is(err, bgreplace.ErrCreativeUnavailable) {
		writeErr(w, http.StatusUnprocessableEntity, "creative_unavailable", err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if existing, ferr := s.St.FindBgJobByFingerprint(r.Context(), p.Tenant(), q.Fingerprint); ferr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(existing, true)})
		return
	} else if !errors.Is(ferr, store.ErrNotFound) {
		writeStoreErr(w, ferr)
		return
	}
	if err := s.St.InvalidateOpenBgQuotes(r.Context(), p.Tenant(), projectID, in.ID, q.Fingerprint); err != nil {
		writeStoreErr(w, err)
		return
	}
	job, err := s.St.InsertBgJob(r.Context(), store.BgJob{
		TenantID: p.Tenant(), ProjectID: projectID, InputID: in.ID, InputVersion: version,
		Mode: string(q.Mode), ProtectedRegion: q.ProtectedRegion, BackgroundIntent: q.BackgroundIntent,
		Fingerprint: q.Fingerprint, QuoteStatus: q.QuoteStatus, BillingLabel: q.BillingLabel,
		JobStatus: bgreplace.StatusQuoted, Quality: bgreplace.QualityUnknown, Evidence: bgreplace.EvidenceNone,
		Pending: []string{"主体质量待确认"}, AllowedUses: []string{"预览"},
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleListBgReplace(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), r.PathValue("id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	jobs, err := s.St.ListBgJobs(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取背景替换失败")
		return
	}
	views := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, s.bgView(job, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views, "billing_label": bgreplace.BillingPendingLabel})
}

func (s *Server) handleConfirmBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	var req bgForm
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if bgFormMismatch(job, req) {
		writeErr(w, http.StatusConflict, "quote_stale", "报价与当前模式或输入不一致,请重新生成报价")
		return
	}
	if job.QuoteStatus == bgreplace.QuoteInvalid {
		writeErr(w, http.StatusConflict, "quote_invalid", bgreplace.ErrQuoteInvalid.Error())
		return
	}
	if job.JobStatus != bgreplace.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	job.QuoteStatus = bgreplace.QuoteConfirmed
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleSubmitBgReplace(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	var req bgForm
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if bgFormMismatch(job, req) {
		writeErr(w, http.StatusConflict, "quote_stale", "报价与当前模式或输入不一致,请重新生成报价")
		return
	}
	if job.JobStatus != bgreplace.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	if job.QuoteStatus != bgreplace.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", bgreplace.ErrQuoteUnconfirmed.Error())
		return
	}
	claimed, err := s.St.ClaimBgSubmit(r.Context(), p.Tenant(), job.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !claimed {
		latest, lerr := s.St.GetBgJob(r.Context(), p.Tenant(), job.ProjectID, job.ID)
		if lerr != nil {
			writeStoreErr(w, lerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(latest, true)})
		return
	}
	genSt, _ := s.Cfg.GenerationUsable()
	if genSt != 0 {
		job.JobStatus = bgreplace.StatusGenerationUnavailable
		job.Evidence = bgreplace.EvidenceNone
		if job.Quality == "" {
			job.Quality = bgreplace.QualityUnknown
		}
		job.Deliverable = false
		job.Pending = append(job.Pending, "生成质量未验证")
		if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
		return
	}
	res, err := s.Tasks.Submit(r.Context(), platform.SubmitRequest{
		IdempotencyKey: job.Fingerprint,
		Kind:           "product-image-background-replace",
		Payload: map[string]any{
			"project_id": job.ProjectID, "input_id": job.InputID, "input_version": job.InputVersion,
			"mode": job.Mode, "protected_region": job.ProtectedRegion, "background_intent": job.BackgroundIntent,
			"tenant_id": p.Tenant(),
		},
	})
	if err != nil || strings.TrimSpace(res.TaskID) == "" {
		job.JobStatus = bgreplace.StatusUnknown
		job.Evidence = bgreplace.EvidenceNone
		if job.Quality == "" {
			job.Quality = bgreplace.QualityUnknown
		}
		job.Deliverable = false
		job.Pending = append(job.Pending, "供应商结果未知，先核对")
		if uerr := s.St.UpdateBgJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
		return
	}
	job.JobStatus = bgreplace.StatusQueued
	job.PlatformTaskID = res.TaskID
	job.Evidence = bgreplace.EvidencePlatformTask
	if job.Quality == "" {
		job.Quality = bgreplace.QualityUnknown
	}
	job.Deliverable = false
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleRefreshBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	if job.PlatformTaskID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	st, err := s.Tasks.Status(r.Context(), job.PlatformTaskID)
	if err != nil {
		if errors.Is(err, platform.ErrUnavailable) {
			job.JobStatus = bgreplace.StatusUnknown
			if job.Quality == "" {
				job.Quality = bgreplace.QualityUnknown
			}
			if uerr := s.St.UpdateBgJob(r.Context(), job); uerr != nil {
				writeStoreErr(w, uerr)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
			return
		}
		s.mapPlatformErr(w, "生成任务", err)
		return
	}
	switch st.Status {
	case "succeeded":
		job.JobStatus = bgreplace.StatusCompleted
		if id, _ := st.Result["asset_id"].(string); id != "" && job.OutputAssetID == "" {
			job.OutputAssetID = id
			job.OutputVersion = job.ID
		}
	case "failed":
		job.JobStatus = bgreplace.StatusFailed
	default:
		if job.JobStatus != bgreplace.StatusUnknown {
			job.JobStatus = bgreplace.StatusRunning
		}
	}
	if job.Quality == "" {
		job.Quality = bgreplace.QualityUnknown
	}
	job.Deliverable = false
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleBgQuality(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
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
	if req.Verdict != bgreplace.QualityFail {
		writeErr(w, http.StatusConflict, "quality_pass_unauthorized", "没有保真证据,只能记录失败,不能标为通过或未知")
		return
	}
	if job.Quality == bgreplace.QualityFail {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	job.Quality = bgreplace.QualityFail
	job.Deliverable = false
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleSelectBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	next, err := bgreplace.Select(executionFromJob(job))
	if err != nil {
		writeErr(w, http.StatusConflict, "not_deliverable", err.Error())
		return
	}
	job.Selection = next.Selection
	job.Deliverable = next.Deliverable
	job.Pending = next.Pending
	job.AllowedUses = next.AllowedUses
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleExportBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	rec, err := bgreplace.Export(executionFromJob(job))
	if err != nil {
		writeErr(w, http.StatusConflict, "export_blocked", err.Error())
		return
	}
	job.ExportCount++
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	view := s.bgView(job, false)
	view["output_asset_id"] = rec.OutputAssetID
	view["output_version"] = rec.OutputVersion
	view["starts_generation"] = rec.StartsGeneration
	writeJSON(w, http.StatusOK, map[string]any{"job": view})
}

func (s *Server) loadBgJob(w http.ResponseWriter, r *http.Request) (store.BgJob, bool) {
	p, _ := principalFrom(r.Context())
	job, err := s.St.GetBgJob(r.Context(), p.Tenant(), r.PathValue("id"), r.PathValue("jobId"))
	if err != nil {
		writeStoreErr(w, err)
		return store.BgJob{}, false
	}
	return job, true
}

func (s *Server) bgView(job store.BgJob, idempotent bool) map[string]any {
	if job.BillingLabel == "" {
		job.BillingLabel = bgreplace.BillingPendingLabel
	}
	return map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "input_id": job.InputID,
		"input_version": job.InputVersion, "mode": job.Mode, "protected_region": job.ProtectedRegion,
		"background_intent": job.BackgroundIntent, "fingerprint": job.Fingerprint,
		"quote_status": job.QuoteStatus, "billing_label": job.BillingLabel,
		"job_status": job.JobStatus, "quality": job.Quality, "evidence": job.Evidence,
		"platform_task_id": job.PlatformTaskID, "output_asset_id": job.OutputAssetID,
		"output_version": job.OutputVersion, "selection": job.Selection, "export_count": job.ExportCount,
		"deliverable": job.Deliverable, "pending": job.Pending, "allowed_uses": job.AllowedUses,
		"production_generation_passed": false, "billing_passed": false,
		"delivery_readiness": bgreplace.ReadinessInternal, "honesty": bgreplace.HonestyNotice,
		"idempotent": idempotent,
	}
}

type bgForm struct {
	Mode             string `json:"mode"`
	InputID          string `json:"input_id"`
	BackgroundIntent string `json:"background_intent"`
}

func bgFormMismatch(job store.BgJob, req bgForm) bool {
	if req.Mode != "" && req.Mode != job.Mode {
		return true
	}
	if req.InputID != "" && req.InputID != job.InputID {
		return true
	}
	if req.BackgroundIntent != "" && req.BackgroundIntent != job.BackgroundIntent {
		return true
	}
	return false
}

func quoteFromJob(job store.BgJob) bgreplace.Quote {
	return bgreplace.Quote{
		Mode: bgreplace.Mode(job.Mode), ProtectedRegion: job.ProtectedRegion,
		InputID: job.InputID, InputVersion: job.InputVersion, BackgroundIntent: job.BackgroundIntent,
		Fingerprint: job.Fingerprint, BillingLabel: job.BillingLabel, QuoteStatus: job.QuoteStatus,
	}
}

func executionFromJob(job store.BgJob) bgreplace.Execution {
	return bgreplace.Execution{
		Status: job.JobStatus, Mode: bgreplace.Mode(job.Mode), Quality: job.Quality,
		Evidence: job.Evidence, Deliverable: job.Deliverable, OutputAssetID: job.OutputAssetID,
		OutputVersion: job.OutputVersion, Selection: job.Selection, Pending: job.Pending,
		AllowedUses: job.AllowedUses, DeliveryReadiness: bgreplace.ReadinessInternal,
	}
}
