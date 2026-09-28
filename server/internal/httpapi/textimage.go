package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"github.com/bianjiefilm/product-image-engine/server/internal/textimage"
)

func (s *Server) handleTextImageCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":               true,
		"photo_required":        false,
		"billing_label":         textimage.BillingPendingLabel,
		"charged":               false,
		"billing_passed":        false,
		"production_authorized": false,
		"subject_protected":     false,
		"show_image":            false,
		"delivery_readiness":    textimage.ReadinessInternal,
		"honesty":               textimage.HonestyNotice,
	})
}

func (s *Server) handleCreateTextImage(w http.ResponseWriter, r *http.Request) {
	proj, ok := s.textProject(w, r)
	if !ok {
		return
	}
	var req struct {
		Prompt           string `json:"prompt"`
		PhotoAssetID     string `json:"photo_asset_id"`
		QuotedAmount     string `json:"quoted_amount"`
		SubjectProtected bool   `json:"subject_protected"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	q, err := textimage.Open(textimage.OpenInput{
		TenantID: proj.TenantID, ProjectID: proj.ID, Prompt: req.Prompt,
		PhotoAssetID: req.PhotoAssetID, BillingConnected: false, QuotedAmount: "",
	})
	if errors.Is(err, textimage.ErrPromptRequired) {
		writeErr(w, http.StatusBadRequest, "prompt_required", err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if existing, ferr := s.St.FindTextImageByFingerprint(r.Context(), proj.TenantID, q.Fingerprint); ferr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(existing, true)})
		return
	} else if !errors.Is(ferr, store.ErrNotFound) {
		writeStoreErr(w, ferr)
		return
	}
	job, err := s.St.InsertTextImageJob(r.Context(), store.TextImageJob{
		TenantID: proj.TenantID, ProjectID: proj.ID, Prompt: q.Prompt, PhotoAssetID: q.PhotoAssetID,
		Fingerprint: q.Fingerprint, QuoteStatus: q.QuoteStatus, BillingLabel: q.BillingLabel,
		JobStatus: textimage.StatusQuoted, Quality: textimage.QualityUnknown,
		Pending: []string{"没有真实生成图"},
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": s.textImageView(job, false)})
}

func (s *Server) handleListTextImage(w http.ResponseWriter, r *http.Request) {
	proj, ok := s.textProject(w, r)
	if !ok {
		return
	}
	jobs, err := s.St.ListTextImageJobs(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取文字请求失败")
		return
	}
	views := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, s.textImageView(job, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views, "billing_label": textimage.BillingPendingLabel, "show_image": false})
}

func (s *Server) handleConfirmTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	if job.JobStatus != textimage.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
		return
	}
	job.QuoteStatus = textimage.QuoteConfirmed
	if err := s.St.UpdateTextImageJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, false)})
}

func (s *Server) handleSubmitTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	if job.JobStatus == textimage.StatusFailed || job.JobStatus == textimage.StatusUnknown {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
		return
	}
	if job.QuoteStatus != textimage.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", textimage.ErrQuoteUnconfirmed.Error())
		return
	}
	if job.JobStatus != textimage.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
		return
	}
	claimed, err := s.St.ClaimTextImageSubmit(r.Context(), job.TenantID, job.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !claimed {
		latest, lerr := s.St.GetTextImageJob(r.Context(), job.TenantID, job.ProjectID, job.ID)
		if lerr != nil {
			writeStoreErr(w, lerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(latest, true)})
		return
	}
	job.JobStatus = textimage.StatusSubmitting
	genSt, _ := s.Cfg.GenerationUsable()
	if genSt != 0 {
		job.JobStatus = textimage.StatusFailed
		job.Quality = textimage.QualityUnknown
		job.OutputAssetID = ""
		job.Pending = append(job.Pending, "没有真实供应商，未执行生成")
		if err := s.St.UpdateTextImageJob(r.Context(), job); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, false)})
		return
	}
	res, err := s.Tasks.Submit(r.Context(), platform.SubmitRequest{
		IdempotencyKey: job.Fingerprint,
		Kind:           "product-image-text",
		Payload: map[string]any{
			"project_id": job.ProjectID, "prompt": job.Prompt, "tenant_id": job.TenantID,
		},
	})
	if err != nil || strings.TrimSpace(res.TaskID) == "" {
		job.JobStatus = textimage.StatusUnknown
		job.OutputAssetID = ""
		job.Pending = append(job.Pending, "供应商结果待确认")
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, false)})
		return
	}
	job.JobStatus = textimage.StatusUnknown
	job.PlatformTaskID = res.TaskID
	job.OutputAssetID = ""
	job.Pending = append(job.Pending, "供应商已受理，成片仍待确认")
	if err := s.St.UpdateTextImageJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, false)})
}

func (s *Server) handleRefreshTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	if job.JobStatus == textimage.StatusFailed {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
		return
	}
	if job.PlatformTaskID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
		return
	}
	st, err := s.Tasks.Status(r.Context(), job.PlatformTaskID)
	if err != nil {
		job.JobStatus = textimage.StatusUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingText(job.Pending, "供应商结果待确认")
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
		return
	}
	if st.Status == "failed" {
		job.JobStatus = textimage.StatusFailed
	} else {
		job.JobStatus = textimage.StatusUnknown
	}
	job.OutputAssetID = ""
	job.Pending = appendPendingText(job.Pending, "填上的输出编号不算核销凭证")
	if err := s.St.UpdateTextImageJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(job, true)})
}

func (s *Server) handleClaimTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	var req struct {
		Claim         string `json:"claim"`
		OutputAssetID string `json:"output_asset_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	rec := textimage.Record{
		ID: job.ID, Status: job.JobStatus, Quality: job.Quality, BillingLabel: job.BillingLabel,
	}
	_, err := textimage.ApplyClientClaim(rec, req.Claim)
	if errors.Is(err, textimage.ErrSubjectClaimForbidden) {
		writeErr(w, http.StatusConflict, "subject_claim_forbidden", err.Error())
		return
	}
	writeErr(w, http.StatusConflict, "relabel_forbidden", textimage.ErrRelabelForbidden.Error())
}

func (s *Server) loadTextImage(w http.ResponseWriter, r *http.Request) (store.TextImageJob, bool) {
	proj, ok := s.textProject(w, r)
	if !ok {
		return store.TextImageJob{}, false
	}
	job, err := s.St.GetTextImageJob(r.Context(), proj.TenantID, proj.ID, r.PathValue("jobId"))
	if err != nil {
		writeStoreErr(w, err)
		return store.TextImageJob{}, false
	}
	return job, true
}

func (s *Server) textProject(w http.ResponseWriter, r *http.Request) (store.Project, bool) {
	p, _ := principalFrom(r.Context())
	proj, err := s.projectFor(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return store.Project{}, false
	}
	return proj, true
}

func (s *Server) textImageView(job store.TextImageJob, idempotent bool) map[string]any {
	if job.BillingLabel == "" {
		job.BillingLabel = textimage.BillingPendingLabel
	}
	statusLabel := "待确认"
	if job.JobStatus == textimage.StatusFailed {
		statusLabel = "失败"
	} else if job.JobStatus == textimage.StatusQuoted {
		statusLabel = "待确认报价"
	}
	return map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "prompt": job.Prompt,
		"photo_asset_id": job.PhotoAssetID, "photo_required": false,
		"fingerprint": job.Fingerprint, "quote_status": job.QuoteStatus,
		"billing_label": job.BillingLabel, "job_status": job.JobStatus,
		"status_label": statusLabel, "quality": job.Quality,
		"platform_task_id": job.PlatformTaskID, "output_asset_id": "",
		"output_is_receipt": false, "show_image": false,
		"charged": false, "billing_passed": false, "production_authorized": false,
		"subject_protected": false, "pending": job.Pending,
		"delivery_readiness": textimage.ReadinessInternal, "honesty": textimage.HonestyNotice,
		"idempotent": idempotent,
	}
}

func appendPendingText(items []string, item string) []string {
	for _, have := range items {
		if have == item {
			return items
		}
	}
	return append(items, item)
}
