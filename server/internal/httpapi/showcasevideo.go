package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/showcasevideo"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func (s *Server) handleShowcaseVideoCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":               true,
		"image_required":        true,
		"camera_moves":          []string{showcasevideo.Move360, showcasevideo.MoveScene},
		"billing_label":         showcasevideo.BillingPendingLabel,
		"charged":               false,
		"billing_passed":        false,
		"production_authorized": false,
		"show_video":            false,
		"play_still":            false,
		"video_url":             "",
		"delivery_readiness":    showcasevideo.ReadinessInternal,
		"honesty":               showcasevideo.HonestyNotice,
	})
}

func (s *Server) handleCreateShowcaseVideo(w http.ResponseWriter, r *http.Request) {
	proj, ok := s.showcaseProject(w, r)
	if !ok {
		return
	}
	var req struct {
		ProductImageID string `json:"product_image_id"`
		CameraMove     string `json:"camera_move"`
		QuotedAmount   string `json:"quoted_amount"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	q, err := showcasevideo.Open(showcasevideo.OpenInput{
		TenantID: proj.TenantID, ProjectID: proj.ID, ImageID: req.ProductImageID, CameraMove: req.CameraMove,
		BillingConnected: false, QuotedAmount: "",
	})
	if errors.Is(err, showcasevideo.ErrImageRequired) {
		writeErr(w, http.StatusBadRequest, "image_required", err.Error())
		return
	}
	if errors.Is(err, showcasevideo.ErrMoveNotAllowed) {
		writeErr(w, http.StatusBadRequest, "camera_move_not_allowed", err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	kind, err := s.productImageKind(r.Context(), proj, q.ImageID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "image_not_found", "没有这张产品图，不能发起展示视频")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if existing, ferr := s.St.FindShowcaseVideoByFingerprint(r.Context(), proj.TenantID, q.Fingerprint); ferr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(existing, true)})
		return
	} else if !errors.Is(ferr, store.ErrNotFound) {
		writeStoreErr(w, ferr)
		return
	}
	job, err := s.St.InsertShowcaseVideoJob(r.Context(), store.ShowcaseVideoJob{
		TenantID: proj.TenantID, ProjectID: proj.ID, ProductImageID: q.ImageID, ImageKind: kind,
		CameraMove: q.CameraMove, Fingerprint: q.Fingerprint, QuoteStatus: q.QuoteStatus,
		BillingLabel: q.BillingLabel, JobStatus: showcasevideo.StatusQuoted, Quality: showcasevideo.QualityUnknown,
		Pending: []string{"没有真实展示视频"},
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": s.showcaseVideoView(job, false)})
}

func (s *Server) handleListShowcaseVideo(w http.ResponseWriter, r *http.Request) {
	proj, ok := s.showcaseProject(w, r)
	if !ok {
		return
	}
	jobs, err := s.St.ListShowcaseVideoJobs(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取展示视频请求失败")
		return
	}
	views := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, s.showcaseVideoView(job, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs": views, "billing_label": showcasevideo.BillingPendingLabel,
		"show_video": false, "play_still": false, "video_url": "",
	})
}

func (s *Server) handleConfirmShowcaseVideo(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadShowcaseVideo(w, r)
	if !ok {
		return
	}
	if job.JobStatus != showcasevideo.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
		return
	}
	job.QuoteStatus = showcasevideo.QuoteConfirmed
	if err := s.St.UpdateShowcaseVideoJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, false)})
}

func (s *Server) handleSubmitShowcaseVideo(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadShowcaseVideo(w, r)
	if !ok {
		return
	}
	if job.JobStatus == showcasevideo.StatusFailed || job.JobStatus == showcasevideo.StatusUnknown {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
		return
	}
	if job.QuoteStatus != showcasevideo.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", showcasevideo.ErrQuoteUnconfirmed.Error())
		return
	}
	if job.JobStatus != showcasevideo.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
		return
	}
	claimed, err := s.St.ClaimShowcaseVideoSubmit(r.Context(), job.TenantID, job.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !claimed {
		latest, lerr := s.St.GetShowcaseVideoJob(r.Context(), job.TenantID, job.ProjectID, job.ID)
		if lerr != nil {
			writeStoreErr(w, lerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(latest, true)})
		return
	}
	job.JobStatus = showcasevideo.StatusSubmitting
	genSt, _ := s.Cfg.GenerationUsable()
	if genSt != 0 {
		job.JobStatus = showcasevideo.StatusFailed
		job.Quality = showcasevideo.QualityUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingShowcase(job.Pending, "没有真实视频供应商，未执行生成")
		if err := s.St.UpdateShowcaseVideoJob(r.Context(), job); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, false)})
		return
	}
	res, err := s.Tasks.Submit(r.Context(), platform.SubmitRequest{
		IdempotencyKey: job.Fingerprint,
		Kind:           "product-image-showcase-video",
		Payload: map[string]any{
			"project_id": job.ProjectID, "product_image_id": job.ProductImageID,
			"camera_move": job.CameraMove, "tenant_id": job.TenantID,
		},
	})
	if err != nil || strings.TrimSpace(res.TaskID) == "" {
		job.JobStatus = showcasevideo.StatusUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingShowcase(job.Pending, "供应商结果待确认")
		if uerr := s.St.UpdateShowcaseVideoJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, false)})
		return
	}
	job.JobStatus = showcasevideo.StatusUnknown
	job.PlatformTaskID = res.TaskID
	job.OutputAssetID = ""
	job.Pending = appendPendingShowcase(job.Pending, "供应商已受理，展示视频仍待确认")
	if err := s.St.UpdateShowcaseVideoJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, false)})
}

func (s *Server) handleRefreshShowcaseVideo(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadShowcaseVideo(w, r)
	if !ok {
		return
	}
	if job.JobStatus == showcasevideo.StatusFailed {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
		return
	}
	if job.PlatformTaskID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
		return
	}
	st, err := s.Tasks.Status(r.Context(), job.PlatformTaskID)
	if err != nil {
		job.JobStatus = showcasevideo.StatusUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingShowcase(job.Pending, "供应商结果待确认")
		if uerr := s.St.UpdateShowcaseVideoJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
		return
	}
	if st.Status == "failed" {
		job.JobStatus = showcasevideo.StatusFailed
	} else {
		job.JobStatus = showcasevideo.StatusUnknown
	}
	job.OutputAssetID = ""
	job.Pending = appendPendingShowcase(job.Pending, "填上的输出编号不算展示视频")
	if err := s.St.UpdateShowcaseVideoJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.showcaseVideoView(job, true)})
}

func (s *Server) handleClaimShowcaseVideo(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadShowcaseVideo(w, r)
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
	rec := showcasevideo.Record{ID: job.ID, Status: job.JobStatus, Quality: job.Quality, BillingLabel: job.BillingLabel}
	noted := showcasevideo.NoteOutputID(rec, req.OutputAssetID)
	if _, err := showcasevideo.ApplyClientClaim(noted, req.Claim); err != nil {
		writeErr(w, http.StatusConflict, "relabel_forbidden", showcasevideo.ErrRelabelForbidden.Error())
		return
	}
	writeErr(w, http.StatusConflict, "relabel_forbidden", showcasevideo.ErrRelabelForbidden.Error())
}

func (s *Server) productImageKind(ctx context.Context, proj store.Project, imageID string) (string, error) {
	if _, err := s.St.GetInput(ctx, proj.TenantID, proj.ID, imageID); err == nil {
		return "input", nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	out, err := s.St.GetOutput(ctx, proj.TenantID, imageID)
	if err != nil {
		return "", err
	}
	if out.ProjectID != proj.ID {
		return "", store.ErrNotFound
	}
	return "output", nil
}

func (s *Server) loadShowcaseVideo(w http.ResponseWriter, r *http.Request) (store.ShowcaseVideoJob, bool) {
	proj, ok := s.showcaseProject(w, r)
	if !ok {
		return store.ShowcaseVideoJob{}, false
	}
	job, err := s.St.GetShowcaseVideoJob(r.Context(), proj.TenantID, proj.ID, r.PathValue("jobId"))
	if err != nil {
		writeStoreErr(w, err)
		return store.ShowcaseVideoJob{}, false
	}
	return job, true
}

func (s *Server) showcaseProject(w http.ResponseWriter, r *http.Request) (store.Project, bool) {
	p, _ := principalFrom(r.Context())
	proj, err := s.projectFor(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return store.Project{}, false
	}
	return proj, true
}

func (s *Server) showcaseVideoView(job store.ShowcaseVideoJob, idempotent bool) map[string]any {
	if job.BillingLabel == "" {
		job.BillingLabel = showcasevideo.BillingPendingLabel
	}
	statusLabel := "待确认"
	if job.JobStatus == showcasevideo.StatusFailed {
		statusLabel = "失败"
	} else if job.JobStatus == showcasevideo.StatusQuoted {
		statusLabel = "待确认报价"
	}
	return map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "product_image_id": job.ProductImageID,
		"image_kind": job.ImageKind, "camera_move": job.CameraMove, "image_required": true,
		"fingerprint": job.Fingerprint, "quote_status": job.QuoteStatus,
		"billing_label": job.BillingLabel, "job_status": job.JobStatus,
		"status_label": statusLabel, "quality": job.Quality,
		"platform_task_id": job.PlatformTaskID, "output_asset_id": "",
		"output_is_receipt": false, "show_video": false, "play_still": false, "video_url": "",
		"charged": false, "billing_passed": false, "production_authorized": false,
		"pending": job.Pending, "delivery_readiness": showcasevideo.ReadinessInternal,
		"honesty": showcasevideo.HonestyNotice, "idempotent": idempotent,
	}
}

func appendPendingShowcase(items []string, item string) []string {
	for _, have := range items {
		if have == item {
			return items
		}
	}
	return append(items, item)
}
