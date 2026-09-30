package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/billconsume"
	"github.com/bianjiefilm/product-image-engine/server/internal/imagemodel"
	"github.com/bianjiefilm/product-image-engine/server/internal/imgprobe"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"github.com/bianjiefilm/product-image-engine/server/internal/textimage"
)

func (s *Server) handleTextImageCapabilities(w http.ResponseWriter, r *http.Request) {
	label, settlement := s.shownBilling("", "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":               true,
		"photo_required":        false,
		"billing_label":         label,
		"settlement":            settlement,
		"charged":               nil,
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
	// 客户端金额不进入领域层。展示文案按真实连接状态覆盖，BillingConnected 不是资金证明。
	if existing, ferr := s.St.FindTextImageByFingerprint(r.Context(), proj.TenantID, q.Fingerprint); ferr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), existing, true)})
		return
	} else if !errors.Is(ferr, store.ErrNotFound) {
		writeStoreErr(w, ferr)
		return
	}
	p, _ := principalFrom(r.Context())
	payer, payerErr := serverPayer(p, proj)
	if payerErr != nil {
		payer = billconsume.Payer{}
	}
	label, _ := s.captionFor(r.Context(), payer, "image.text", 1, proj.WidthPx, proj.HeightPx, proj.ID, q.Fingerprint)
	q.BillingLabel = label
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
	writeJSON(w, http.StatusCreated, map[string]any{"job": s.textImageView(r.Context(), job, false)})
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
		views = append(views, s.textImageView(r.Context(), job, false))
	}
	label, _ := s.shownBilling("", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views, "billing_label": label, "show_image": false, "charged": nil})
}

func (s *Server) handleConfirmTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	if job.JobStatus != textimage.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	job.QuoteStatus = textimage.QuoteConfirmed
	if err := s.St.UpdateTextImageJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
}

func (s *Server) handleSubmitTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	if job.JobStatus == textimage.StatusFailed || job.JobStatus == textimage.StatusUnknown {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	if job.QuoteStatus != textimage.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", textimage.ErrQuoteUnconfirmed.Error())
		return
	}
	if job.JobStatus != textimage.StatusQuoted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
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
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), latest, true)})
		return
	}
	job.JobStatus = textimage.StatusSubmitting
	if s.textModelReady() {
		s.finishTextImageModel(w, r, job)
		return
	}
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
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
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
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
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
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
}

func (s *Server) handleRefreshTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	if job.JobStatus == textimage.StatusFailed || job.JobStatus == textimage.StatusCompleted {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	if job.PlatformTaskID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
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
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	if strings.EqualFold(st.Status, "failed") {
		job.JobStatus = textimage.StatusFailed
		job.OutputAssetID = ""
		job.Pending = appendPendingText(job.Pending, "填上的输出编号不算核销凭证")
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	asset := assetFromTask(st)
	payload, payloadOK, payloadBad := serverTaskImage(st.Result)
	imageBytes := []byte(nil)
	haveBytes := false
	if payloadOK {
		imageBytes = payload
		haveBytes = true
	} else if !payloadBad && strings.TrimSpace(asset) != "" {
		painted, perr := s.paintTextImage(r.Context(), job.Prompt)
		if perr == nil {
			if _, probeErr := imgprobe.Probe(painted); probeErr == nil {
				imageBytes = painted
				haveBytes = true
			}
		}
	}
	decision := textimage.AcceptServerTask(textimage.ServerTaskInput{
		Status: st.Status, AssetID: asset, ServerBytes: haveBytes,
		CredentialConfigured: s.Cfg.TextImageModelConfigured(),
	})
	if !decision.Accept {
		job.JobStatus = textimage.StatusUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingText(job.Pending, "填上的输出编号不算核销凭证")
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	meta, err := imgprobe.Probe(imageBytes)
	if err != nil {
		job.JobStatus = textimage.StatusUnknown
		job.OutputAssetID = ""
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, true)})
		return
	}
	updated, already, err := s.St.CompleteTextImageServer(r.Context(), store.TextImageServerResult{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		AssetID: decision.AssetID, MediaType: meta.MediaType, Bytes: imageBytes,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), updated, already)})
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

func (s *Server) handleGetTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
}

func (s *Server) handleDownloadTextImage(w http.ResponseWriter, r *http.Request) {
	proj, ok := s.textProject(w, r)
	if !ok {
		return
	}
	job, err := s.St.GetTextImageJob(r.Context(), proj.TenantID, proj.ID, r.PathValue("jobId"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	media, body, err := s.St.OpenTextImageDownload(r.Context(), proj.TenantID, proj.ID, job.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "没有可打开的夹具图")
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, "integrity_failed", "文件字节与登记不一致")
		return
	}
	sum := sha256.Sum256(body)
	decision := textimage.Admit(textimage.AdmitInput{
		Status: job.JobStatus, JobTenant: job.TenantID, JobProject: job.ProjectID,
		ActorTenant: proj.TenantID, ActorProject: proj.ID,
		InputVersion: job.InputVersion, AssetInputVersion: job.InputVersion,
		AssetID: job.OutputAssetID, Registered: true,
		RegisteredSHA: job.OutputSHA256, FileSHA: hex.EncodeToString(sum[:]),
		Origin: job.Origin, SelectedVersion: job.SelectedVersion, ResultVersion: job.ResultVersion,
	})
	if !decision.Show {
		writeErr(w, http.StatusNotFound, "not_found", "没有可打开的夹具图")
		return
	}
	filename := "fixture.png"
	switch job.Origin {
	case textimage.OriginModel:
		filename = "model.png"
	case textimage.OriginServer:
		filename = "server.png"
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	w.Header().Set("X-Text-Image-Origin", job.Origin)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleSelectTextImage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	// 模型字节可以下载查看。主体保真未核实，不能选定为可用候选。
	if job.Origin == textimage.OriginModel {
		rejectUnusableCandidate(w)
		return
	}
	selected, err := s.St.SelectTextImageVersion(r.Context(), job.TenantID, job.ProjectID, job.ID)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "selection_blocked", err.Error())
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), selected, job.SelectedVersion != "")})
}

func (s *Server) handleRegisterTextImageFixture(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.TextImageFixtureRegister {
		writeErr(w, http.StatusNotFound, "not_found", "夹具登记未打开")
		return
	}
	job, ok := s.loadTextImage(w, r)
	if !ok {
		return
	}
	var req struct {
		Fixture string `json:"fixture"`
		DataB64 string `json:"data_b64"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	if !strings.Contains(req.Fixture, "fixture") {
		writeErr(w, http.StatusBadRequest, "fixture_required", "夹具数据必须标明 fixture")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.DataB64)
	if err != nil || len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "夹具字节无法解码")
		return
	}
	meta, err := imgprobe.Probe(raw)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "undecodable_image", "夹具内容不是可解码的图片")
		return
	}
	sum := sha256.Sum256(raw)
	shaHex := hex.EncodeToString(sum[:])
	updated, already, err := s.St.RegisterTextImageFixture(r.Context(), store.TextImageFixture{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		FixtureLabel: req.Fixture, SHA256: shaHex, MediaType: meta.MediaType, Bytes: raw,
	})
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "fixture_blocked", err.Error())
		return
	}
	if errors.Is(err, store.ErrValidation) {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	status := http.StatusCreated
	if already {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"job":                 s.textImageView(r.Context(), updated, already),
		"idempotent":          already,
		"late_result_ignored": already && updated.OutputSHA256 != shaHex,
	})
}

func (s *Server) textImageView(ctx context.Context, job store.TextImageJob, idempotent bool) map[string]any {
	label, settlement := s.shownBilling(job.BillingLabel, job.JobStatus, "")
	decision := s.admitTextImage(ctx, job)
	outputID := ""
	origin := ""
	if decision.Show {
		outputID = job.OutputAssetID
		origin = job.Origin
	}
	statusLabel := "待确认"
	switch job.JobStatus {
	case textimage.StatusFailed:
		statusLabel = "失败"
	case textimage.StatusQuoted:
		statusLabel = "待确认报价"
	case textimage.StatusCompleted:
		statusLabel = "夹具已登记"
		switch job.Origin {
		case textimage.OriginServer:
			statusLabel = "任务已完成"
		case textimage.OriginModel:
			statusLabel = "供应商图片已解码"
		}
	}
	subjectNotice := textimage.ConceptualNotice
	if strings.TrimSpace(job.PhotoAssetID) != "" {
		subjectNotice = "有实物参考，主体保真仍未在本结果中核实"
	}
	honesty := textimage.HonestyNotice
	fixtureNotice := textimage.FixtureNotice
	if job.Origin == textimage.OriginModel {
		honesty = textimage.ModelDecodedNotice
		fixtureNotice = ""
	}
	return map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "prompt": job.Prompt,
		"photo_asset_id": job.PhotoAssetID, "photo_required": false,
		"fingerprint": job.Fingerprint, "idempotency_key": job.Fingerprint, "quote_status": job.QuoteStatus,
		"billing_label": label, "settlement": settlement, "job_status": job.JobStatus,
		"status_label": statusLabel, "quality": job.Quality,
		"platform_task_id": job.PlatformTaskID, "output_asset_id": outputID,
		"origin": origin, "input_version": job.InputVersion,
		"result_version": job.ResultVersion, "selected_version": job.SelectedVersion,
		"output_is_receipt": false, "show_image": decision.Show,
		"charged": nil, "billing_passed": false, "production_authorized": false,
		"production_authorization":  textimage.ProductionNotAuthorized,
		"real_generation_completed": textimage.RealGenerationCompleted(s.Cfg.TextImageModelConfigured(), job.Origin),
		"real_generation_notice":    textimage.RealGenerationIncomplete,
		"supplier_image_decoded":    decision.Show && textimage.SupplierDecoded(job.Origin, job.OutputSHA256),
		"subject_protected":         false, "subject_notice": subjectNotice, "pending": job.Pending,
		"delivery_readiness": textimage.ReadinessInternal, "honesty": honesty,
		"fixture_notice": fixtureNotice, "idempotent": idempotent,
	}
}

func (s *Server) textModelReady() bool {
	return strings.TrimSpace(s.Cfg.TextImageModelCredential) != "" &&
		strings.TrimSpace(s.Cfg.TextImageModelURL) != "" &&
		strings.TrimSpace(s.Cfg.TextImageModelName) != ""
}

func (s *Server) finishTextImageModel(w http.ResponseWriter, r *http.Request, job store.TextImageJob) {
	size, projectW, projectH := s.projectCanvas(r.Context(), job.TenantID, job.ProjectID)
	result, err := imagemodel.Generate(r.Context(), s.ImageHTTP, imagemodel.Request{
		Endpoint:   s.Cfg.TextImageModelURL,
		Credential: s.Cfg.TextImageModelCredential,
		Model:      s.Cfg.TextImageModelName,
		Prompt:     job.Prompt,
		Size:       size,
	})
	if err != nil {
		job.JobStatus = textimage.StatusFailed
		job.Quality = textimage.QualityUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingText(job.Pending, scrubModelSecret(err.Error(), s.Cfg.TextImageModelCredential))
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
		return
	}
	if !result.HostLive || result.BillingPassed {
		job.JobStatus = textimage.StatusUnknown
		job.Quality = textimage.QualityUnknown
		job.OutputAssetID = ""
		job.Pending = appendPendingText(job.Pending, "回环地址不能算供应商出图")
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
		return
	}
	if len(result.SHA256) < 20 || len(result.Bytes) == 0 {
		job.JobStatus = textimage.StatusFailed
		job.OutputAssetID = ""
		job.Pending = appendPendingText(job.Pending, "图片无法解码")
		if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), job, false)})
		return
	}
	job.JobStatus = textimage.StatusSubmitting
	for _, note := range imagemodel.SizeNotes(projectW, projectH, result.WidthPx, result.HeightPx) {
		job.Pending = appendPendingText(job.Pending, note)
	}
	job.Pending = appendPendingText(job.Pending, result.UsageNote)
	job.Pending = appendPendingText(job.Pending, textimage.ModelDecodedNotice)
	if uerr := s.St.UpdateTextImageJob(r.Context(), job); uerr != nil {
		writeStoreErr(w, uerr)
		return
	}
	updated, already, err := s.St.CompleteTextImageServer(r.Context(), store.TextImageServerResult{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		AssetID: "img_" + result.SHA256[:20], MediaType: result.MediaType, Bytes: result.Bytes,
		Origin: textimage.OriginModel,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.textImageView(r.Context(), updated, already)})
}

func scrubModelSecret(message, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" || !strings.Contains(message, secret) {
		return message
	}
	return strings.ReplaceAll(message, secret, "已隐藏")
}

func (s *Server) admitTextImage(ctx context.Context, job store.TextImageJob) textimage.AdmitDecision {
	if job.OutputAssetID == "" {
		return textimage.Admit(textimage.AdmitInput{
			Status: job.JobStatus, JobTenant: job.TenantID, JobProject: job.ProjectID,
			ActorTenant: job.TenantID, ActorProject: job.ProjectID,
			InputVersion: job.InputVersion, Origin: job.Origin,
			SelectedVersion: job.SelectedVersion, ResultVersion: job.ResultVersion,
		})
	}
	_, body, err := s.St.OpenTextImageDownload(ctx, job.TenantID, job.ProjectID, job.ID)
	if err != nil {
		return textimage.AdmitDecision{Reason: "asset_hash"}
	}
	sum := sha256.Sum256(body)
	return textimage.Admit(textimage.AdmitInput{
		Status: job.JobStatus, JobTenant: job.TenantID, JobProject: job.ProjectID,
		ActorTenant: job.TenantID, ActorProject: job.ProjectID,
		InputVersion: job.InputVersion, AssetInputVersion: job.InputVersion,
		AssetID: job.OutputAssetID, Registered: true,
		RegisteredSHA: job.OutputSHA256, FileSHA: hex.EncodeToString(sum[:]),
		Origin: job.Origin, SelectedVersion: job.SelectedVersion, ResultVersion: job.ResultVersion,
	})
}

func (s *Server) paintTextImage(ctx context.Context, prompt string) ([]byte, error) {
	if textimage.ModelCallUsable(s.Cfg.TextImageModelCredential) {
		return nil, errors.New("图像模型端点未接入")
	}
	if s.TextPaint != nil {
		return s.TextPaint.Paint(ctx, prompt)
	}
	return textimage.PaintLocalTest(prompt)
}

func serverTaskImage(result map[string]any) (raw []byte, ok bool, bad bool) {
	if result == nil {
		return nil, false, false
	}
	value, present := result["data_b64"]
	if !present {
		return nil, false, false
	}
	text, _ := value.(string)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(decoded) == 0 {
		return nil, false, true
	}
	if _, err := imgprobe.Probe(decoded); err != nil {
		return nil, false, true
	}
	return decoded, true, false
}

func appendPendingText(items []string, item string) []string {
	for _, have := range items {
		if have == item {
			return items
		}
	}
	return append(items, item)
}
