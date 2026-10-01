package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	"github.com/bianjiefilm/product-image-engine/server/internal/billconsume"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
	"github.com/bianjiefilm/product-image-engine/server/internal/imagemodel"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func (s *Server) handleBgCapabilities(w http.ResponseWriter, r *http.Request) {
	label, settlement := s.shownBilling("", "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                      true,
		"default_mode":                 string(bgreplace.ModeFidelity),
		"creative_available":           s.Cfg.CreativePathAvailable(),
		"protection_scope":             bgreplace.ProtectionScope,
		"billing_label":                label,
		"settlement":                   settlement,
		"charged":                      nil,
		"production_generation_passed": false,
		"billing_passed":               false,
		"production_authorized":        false,
		"real_generation_completed":    false,
		"real_generation_notice":       bgreplace.RealGenerationIncomplete,
		"delivery_readiness":           bgreplace.ReadinessInternal,
		"honesty":                      bgreplace.HonestyNotice,
	})
}

func (s *Server) handleBgAcceptance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"samples":                      bgreplace.AcceptanceSamples(),
		"production_generation_passed": false,
		"billing_passed":               false,
		"production_authorized":        false,
		"real_generation_completed":    false,
		"real_generation_notice":       bgreplace.RealGenerationIncomplete,
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
	proj, err := s.St.GetProject(r.Context(), p.Tenant(), projectID)
	if err != nil {
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
	if strings.TrimSpace(req.ProtectedRegion) == "" {
		req.ProtectedRegion = bgreplace.ProtectionScope
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
	// 客户端金额不进入领域层。展示文案按真实连接状态覆盖，BillingConnected 不是资金证明。
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
	payer, payerErr := serverPayer(p, proj)
	if payerErr != nil {
		payer = billconsume.Payer{}
	}
	label, _ := s.captionFor(r.Context(), payer, "image.generate.background", 1, proj.WidthPx, proj.HeightPx, proj.ID, q.Fingerprint)
	q.BillingLabel = label
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
	label, _ := s.shownBilling("", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views, "billing_label": label, "charged": nil})
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
	if job.Mode == string(bgreplace.ModeCreative) && !s.Cfg.CreativePathAvailable() {
		writeErr(w, http.StatusUnprocessableEntity, "creative_unavailable", bgreplace.ErrCreativeUnavailable.Error())
		return
	}
	originalMode := job.Mode
	if job.Quality == bgreplace.QualityFail {
		job.Mode = originalMode
		job.Deliverable = false
		job.Selection = ""
		if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
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
	if s.Cfg.BgModelReady() && job.Mode == string(bgreplace.ModeCreative) {
		s.finishCreativeBgModel(w, r, p, job)
		return
	}
	genSt, _ := s.Cfg.GenerationUsable()
	if genSt != 0 {
		job.JobStatus = bgreplace.StatusGenerationUnavailable
		job.Mode = originalMode
		job.Evidence = bgreplace.EvidenceNone
		job.Quality = keepBgFail(job.Quality)
		job.Deliverable = false
		job.Pending = bgreplace.AppendPending(job.Pending, "生成质量未验证")
		job.Pending = bgreplace.AppendPending(job.Pending, bgreplace.RealGenerationIncomplete)
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
	if err != nil || strings.TrimSpace(res.TaskID) == "" || strings.EqualFold(res.Status, "failed") {
		kind := bgreplace.ClassifySubmitFailure(err)
		if err == nil && strings.EqualFold(res.Status, "failed") {
			kind = bgreplace.StatusFailed
		}
		if strings.TrimSpace(res.TaskID) != "" {
			job.PlatformTaskID = res.TaskID
		}
		restored := bgreplace.Restore(executionFromJob(job), kind)
		job.JobStatus = restored.Status
		job.Mode = originalMode
		job.Quality = restored.Quality
		job.Evidence = bgreplace.EvidenceNone
		job.Deliverable = false
		job.Pending = restored.Pending
		job.AllowedUses = restored.AllowedUses
		if uerr := s.St.UpdateBgJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
		return
	}
	job.JobStatus = bgreplace.StatusQueued
	job.Mode = originalMode
	job.PlatformTaskID = res.TaskID
	job.Evidence = bgreplace.EvidencePlatformTask
	job.Quality = keepBgFail(job.Quality)
	job.Deliverable = false
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

// projectCanvas 返回可送进模型的尺寸，以及项目上记录的宽高。查不到工程时三者都是空。
func (s *Server) projectCanvas(ctx context.Context, tenant, projectID string) (string, int, int) {
	proj, err := s.St.GetProject(ctx, tenant, projectID)
	if err != nil {
		return "", 0, 0
	}
	return imagemodel.CanvasSize(proj.WidthPx, proj.HeightPx), proj.WidthPx, proj.HeightPx
}

// finishCreativeBgModel 在已抢到提交之后，用已保存的原图调用背景模型。
// 回环或计费标记为真时不保存字节。成功来源只记 model_http，不记供应商成功。
func (s *Server) finishCreativeBgModel(w http.ResponseWriter, r *http.Request, p platform.Principal, job store.BgJob) {
	original, err := s.readBgOriginal(r.Context(), p, job)
	if err != nil {
		s.closeBgModel(w, r, job, bgreplace.StatusFailed, "原图缺失")
		return
	}
	size, projectW, projectH := s.projectCanvas(r.Context(), job.TenantID, job.ProjectID)
	result, err := imagemodel.Generate(r.Context(), s.ImageHTTP, imagemodel.Request{
		Endpoint:   s.Cfg.BgModelURL,
		Credential: s.Cfg.BgModelCredential,
		Model:      s.Cfg.BgModelName,
		Prompt:     job.BackgroundIntent,
		Size:       size,
		Refs:       [][]byte{original},
	})
	if err != nil {
		s.closeBgModel(w, r, job, bgreplace.StatusFailed, scrubModelSecret(err.Error(), s.Cfg.BgModelCredential))
		return
	}
	if !result.HostLive || result.BillingPassed {
		job.OutputAssetID = ""
		job.OutputVersion = ""
		job.Deliverable = false
		s.closeBgModel(w, r, job, bgreplace.StatusUnknown, "回环地址不能算供应商出图")
		return
	}
	pending := job.Pending
	for _, note := range imagemodel.SizeNotes(projectW, projectH, result.WidthPx, result.HeightPx) {
		pending = bgreplace.AppendPending(pending, note)
	}
	pending = bgreplace.AppendPending(pending, result.UsageNote)
	pending = bgreplace.AppendPending(pending, bgreplace.RealGenerationIncomplete)
	saved, already, err := s.St.SaveBgModelBytes(r.Context(), store.BgModelBytes{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		MediaType: result.MediaType, Bytes: result.Bytes, Pending: pending,
	})
	if err != nil {
		s.closeBgModel(w, r, job, bgreplace.StatusFailed, scrubModelSecret(err.Error(), s.Cfg.BgModelCredential))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(saved, already)})
}

func (s *Server) closeBgModel(w http.ResponseWriter, r *http.Request, job store.BgJob, status, note string) {
	job.JobStatus = status
	job.OutputAssetID = ""
	job.OutputVersion = ""
	job.Deliverable = false
	job.Evidence = bgreplace.EvidenceNone
	job.Quality = keepBgFail(job.Quality)
	job.ModelRef = ""
	job.FeeRef = ""
	job.PlatformTaskID = ""
	job.Pending = bgreplace.AppendPending(job.Pending, note)
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) readBgOriginal(ctx context.Context, p platform.Principal, job store.BgJob) ([]byte, error) {
	in, err := s.St.GetInput(ctx, p.Tenant(), job.ProjectID, job.InputID)
	if err != nil || strings.TrimSpace(in.PlatformAssetID) == "" || s.Uploads == nil {
		return nil, errors.New("原图缺失")
	}
	principal := platform.PhotoPrincipal{Type: "user", ID: p.UserID}
	meta, err := s.Uploads.PhotoAsset(ctx, principal, in.PlatformAssetID)
	if err != nil || strings.TrimSpace(meta.AssetID) == "" {
		return nil, errors.New("原图缺失")
	}
	if meta.Status != "" && !strings.EqualFold(meta.Status, "ready") {
		return nil, errors.New("原图缺失")
	}
	local, lerr := s.St.GetPhotoAssetByAssetID(ctx, p.Tenant(), in.PlatformAssetID)
	if lerr == nil {
		if !strings.EqualFold(meta.SHA256, local.SHA256) || (meta.SizeBytes > 0 && meta.SizeBytes != local.SizeBytes) {
			return nil, errors.New("原图缺失")
		}
	} else if !errors.Is(lerr, store.ErrNotFound) {
		return nil, errors.New("原图缺失")
	}
	dlURL, _, _, err := s.Uploads.PhotoDownloadURL(ctx, principal, in.PlatformAssetID, 0)
	if err != nil {
		return nil, errors.New("原图缺失")
	}
	rc, _, _, err := s.Uploads.OpenPhotoContent(ctx, dlURL)
	if err != nil {
		return nil, errors.New("原图缺失")
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, (20<<20)+1))
	if err != nil || len(body) == 0 || len(body) > 20<<20 {
		return nil, errors.New("原图缺失")
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if meta.SHA256 != "" && !strings.EqualFold(meta.SHA256, got) {
		return nil, errors.New("原图缺失")
	}
	if meta.SizeBytes > 0 && meta.SizeBytes != int64(len(body)) {
		return nil, errors.New("原图缺失")
	}
	return body, nil
}

func (s *Server) handleLockBgReplace(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	if job.Origin == bgreplace.OriginSubjectLock && job.OutputAssetID != "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	if job.Mode != string(bgreplace.ModeFidelity) {
		writeErr(w, http.StatusUnprocessableEntity, "creative_cannot_lock", "创意模式不能走主体锁定")
		return
	}
	if job.QuoteStatus != bgreplace.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", bgreplace.ErrQuoteUnconfirmed.Error())
		return
	}
	if job.PlatformTaskID != "" || (job.JobStatus != bgreplace.StatusQuoted && job.JobStatus != bgreplace.StatusGenerationUnavailable) {
		writeErr(w, http.StatusConflict, "lock_after_submit", "已提交的任务不能再锁定")
		return
	}
	var req struct {
		PlatePNGBase64 string `json:"plate_png_base64"`
		MaskPNGBase64  string `json:"mask_png_base64"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 48<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "锁定请求只能带背景图和主体蒙版")
		return
	}
	plate, err := decodeLockedPNG(req.PlatePNGBase64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "背景图不是 PNG")
		return
	}
	mask, err := decodeLockedPNG(req.MaskPNGBase64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "主体蒙版不是 PNG")
		return
	}
	original, err := s.readBgOriginal(r.Context(), p, job)
	if err != nil {
		writeErr(w, http.StatusConflict, "original_missing", "原图缺失")
		return
	}
	locked, err := bgreplace.LockSubject(original, plate, mask)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "lock_rejected", err.Error())
		return
	}
	pending := job.Pending
	pending = bgreplace.AppendPending(pending, bgreplace.SubjectLockNotice)
	pending = bgreplace.AppendPending(pending, bgreplace.SubjectLockNotModel)
	pending = bgreplace.AppendPending(pending, bgreplace.RealGenerationIncomplete)
	saved, already, err := s.St.SaveBgLockedBytes(r.Context(), store.BgModelBytes{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		MediaType: "image/png", Bytes: locked, Pending: pending,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(saved, already)})
}

func (s *Server) handleModelPlate(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	if job.Origin == bgreplace.OriginModelPlate && job.OutputAssetID != "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	if job.Mode != string(bgreplace.ModeFidelity) {
		writeErr(w, http.StatusUnprocessableEntity, "creative_cannot_model_plate", "创意模式不能走保真模型背景")
		return
	}
	if job.QuoteStatus != bgreplace.QuoteConfirmed {
		writeErr(w, http.StatusConflict, "quote_blocked", bgreplace.ErrQuoteUnconfirmed.Error())
		return
	}
	if job.PlatformTaskID != "" {
		writeErr(w, http.StatusConflict, "lock_after_submit", "已提交的任务不能再锁定")
		return
	}
	if job.JobStatus == bgreplace.StatusUnknown || job.JobStatus == bgreplace.StatusQuotaInsufficient {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	if job.JobStatus != bgreplace.StatusQuoted && job.JobStatus != bgreplace.StatusGenerationUnavailable && job.JobStatus != bgreplace.StatusFailed {
		writeErr(w, http.StatusConflict, "lock_after_submit", "已提交的任务不能再锁定")
		return
	}
	if !s.Cfg.BgModelReady() {
		writeErr(w, http.StatusConflict, "config_missing", "配置缺失")
		return
	}
	var req struct {
		MaskPNGBase64 string `json:"mask_png_base64"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "模型背景请求只能带主体蒙版")
		return
	}
	mask, err := decodeLockedPNG(req.MaskPNGBase64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "主体蒙版不是 PNG")
		return
	}
	original, err := s.readBgOriginal(r.Context(), p, job)
	if err != nil {
		writeErr(w, http.StatusConflict, "original_missing", "原图缺失")
		return
	}
	if err := bgreplace.MaskReady(original, mask); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "lock_rejected", err.Error())
		return
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(original))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "lock_rejected", "图片不是 PNG")
		return
	}
	if s.PlateCoverage == nil {
		writeErr(w, http.StatusUnprocessableEntity, "coverage_rejected", "没有已冻结的主体覆盖")
		return
	}
	evidence, err := bgreplace.VerifyCoverage(original, mask, *s.PlateCoverage)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "coverage_rejected", err.Error())
		return
	}
	amount, amountKnown, err := plateHoldMinor(quoteFromJob(job))
	if err != nil {
		writeErr(w, http.StatusConflict, "quote_blocked", "报价没有可冻结的金额")
		return
	}
	proj, err := s.St.GetProject(r.Context(), p.Tenant(), job.ProjectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	payer, err := serverPayer(p, proj)
	if err != nil || strings.TrimSpace(payer.AccountRef) == "" {
		writeErr(w, http.StatusConflict, "quote_blocked", "报价没有付款主体")
		return
	}
	size := imagemodel.CanvasSize(cfg.Width, cfg.Height)
	if size == "" {
		size = "1024x1024"
	}
	params, err := json.Marshal(struct {
		Prompt string `json:"prompt"`
	}{Prompt: bgreplace.ModelPlatePrompt(job.BackgroundIntent)})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "保存模型背景参数失败")
		return
	}
	taskKey, pricing := plateSubmissionID(job, bgreplace.ImageSHA(original), bgreplace.ImageSHA(mask), evidence.CoverageSHA256, s.Cfg.BgModelName, size, amount)
	plate := store.PlateExecution{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		Request: store.PlateRequest{
			PayerID: payer.AccountRef, QuoteID: job.Fingerprint,
			TaskIdempotencyKey: taskKey, PricingVersion: pricing,
			AmountMinor: amount, BillingPassed: quoteFromJob(job).BillingPassed, StoredAmountKnown: amountKnown,
			OriginalHash: bgreplace.ImageSHA(original), MaskHash: bgreplace.ImageSHA(mask), CoverageHash: evidence.CoverageSHA256,
			Model: s.Cfg.BgModelName, Provider: plateRecordProvider, Size: size, ParamsJSON: string(params),
			SampleID: evidence.SampleID, Mask: mask,
		},
	}
	won, err := s.claimModelPlate(r.Context(), plate)
	if errors.Is(err, store.ErrConflict) || (err == nil && !won) {
		s.replyPlateNotClaimed(w, r, job)
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if job.JobStatus == bgreplace.StatusFailed {
		job.JobStatus = bgreplace.StatusQuoted
		if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
			_ = s.St.UpdatePlateTask(r.Context(), job.TenantID, job.ProjectID, job.ID, "unknown", "", "")
			writeStoreErr(w, err)
			return
		}
	}
	result, err := imagemodel.Generate(r.Context(), s.ImageHTTP, imagemodel.Request{
		Endpoint:   s.Cfg.BgModelURL,
		Credential: s.Cfg.BgModelCredential,
		Model:      s.Cfg.BgModelName,
		Prompt:     bgreplace.ModelPlatePrompt(job.BackgroundIntent),
		Size:       size,
	})
	if err != nil {
		note := scrubModelSecret(err.Error(), s.Cfg.BgModelCredential)
		if supplierHTTPRejected(err) {
			s.stopPlateAttempt(w, r, job, bgreplace.StatusFailed, note, true)
			return
		}
		s.stopPlateAttempt(w, r, job, bgreplace.StatusUnknown, note, false)
		return
	}
	if !result.HostLive || result.BillingPassed {
		s.stopPlateAttempt(w, r, job, bgreplace.StatusUnknown, "回环地址不能算供应商出图", false)
		return
	}
	fitted, err := bgreplace.FitPlate(result.Bytes, cfg.Width, cfg.Height)
	if err != nil {
		s.stopPlateAttempt(w, r, job, bgreplace.StatusFailed, err.Error(), false)
		return
	}
	locked, err := bgreplace.LockSubject(original, fitted, mask)
	if err != nil {
		s.stopPlateAttempt(w, r, job, bgreplace.StatusFailed, err.Error(), false)
		return
	}
	checks, err := bgreplace.SubjectPixelChecks(original, locked, mask)
	if err != nil {
		s.stopPlateAttempt(w, r, job, bgreplace.StatusFailed, err.Error(), false)
		return
	}
	rawChecks, err := json.Marshal(checks)
	if err != nil {
		_ = s.St.UpdatePlateTask(r.Context(), job.TenantID, job.ProjectID, job.ID, "unknown", "", "")
		writeErr(w, http.StatusInternalServerError, "internal", "保存商品检查失败")
		return
	}
	pending := job.Pending
	pending = bgreplace.AppendPending(pending, result.UsageNote)
	pending = bgreplace.AppendPending(pending, bgreplace.ModelPlateNotice)
	pending = bgreplace.AppendPending(pending, bgreplace.RealGenerationIncomplete)
	saved, already, err := s.St.SaveBgPlateBytes(r.Context(), store.BgModelBytes{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		MediaType: "image/png", Bytes: locked, Pending: pending,
	}, s.Cfg.BgModelName, string(rawChecks))
	if err != nil {
		_ = s.St.UpdatePlateTask(r.Context(), job.TenantID, job.ProjectID, job.ID, "unknown", "", "")
		writeStoreErr(w, err)
		return
	}
	if err := s.St.UpdatePlateTask(r.Context(), job.TenantID, job.ProjectID, job.ID, "completed", "", ""); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(saved, already)})
}

// plateRecordProvider is stored on the execution only. This slice still calls the configured image endpoint and does not submit a platform task.
const plateRecordProvider = "xingmo"

// plateHoldMinor copies a positive minor amount when the confirmed quote has one.
// This quote has no amount field. BillingPassed false stores 0, which means no hold.
func plateHoldMinor(q bgreplace.Quote) (int64, bool, error) {
	if q.BillingPassed {
		return 0, false, errors.New("计费标记为通过，但报价没有金额")
	}
	return 0, false, nil
}

func plateSubmissionID(job store.BgJob, originalHash, maskHash, coverageHash, model, size string, amount int64) (string, string) {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		job.TenantID, job.ProjectID, job.ID, job.Fingerprint,
		originalHash, maskHash, coverageHash, model, size, strconv.FormatInt(amount, 10),
	}, "\n")))
	digest := hex.EncodeToString(sum[:])
	return "plate-task-" + digest, "plate-pricing-" + digest
}

func supplierHTTPRejected(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "供应商拒绝: http_")
}

func (s *Server) claimModelPlate(ctx context.Context, plate store.PlateExecution) (bool, error) {
	prepared, err := s.St.PreparePlateExecution(ctx, plate)
	if err != nil {
		return false, err
	}
	if err := s.St.ConfirmPlateExecution(ctx, plate.TenantID, plate.ProjectID, plate.JobID, prepared.Fingerprint); err != nil {
		return false, err
	}
	return s.St.ClaimPlateExecution(ctx, plate.TenantID, plate.ProjectID, plate.JobID)
}

func (s *Server) replyPlateNotClaimed(w http.ResponseWriter, r *http.Request, job store.BgJob) {
	latest, err := s.St.GetBgJob(r.Context(), job.TenantID, job.ProjectID, job.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if latest.Origin == bgreplace.OriginModelPlate && latest.OutputAssetID != "" {
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(latest, true)})
		return
	}
	writeErr(w, http.StatusConflict, "plate_in_flight", "模型背景正在提交，不能再次调用")
}

func (s *Server) stopPlateAttempt(w http.ResponseWriter, r *http.Request, job store.BgJob, status, note string, release bool) {
	if release {
		if err := s.St.ReleasePlateClaim(r.Context(), job.TenantID, job.ProjectID, job.ID); err != nil {
			_ = s.St.UpdatePlateTask(r.Context(), job.TenantID, job.ProjectID, job.ID, "unknown", "", "")
			s.closeBgModel(w, r, job, bgreplace.StatusUnknown, note)
			return
		}
		s.closeBgModel(w, r, job, status, note)
		return
	}
	_ = s.St.UpdatePlateTask(r.Context(), job.TenantID, job.ProjectID, job.ID, "unknown", "", "")
	s.closeBgModel(w, r, job, status, note)
}

func decodeLockedPNG(raw string) ([]byte, error) {
	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(body) == 0 || len(body) > 20<<20 {
		return nil, errors.New("图片不是 PNG")
	}
	return body, nil
}

func (s *Server) handleBgContent(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	media, body, err := s.St.OpenBgResult(r.Context(), p.Tenant(), job.ProjectID, job.ID)
	if errors.Is(err, store.ErrNotFound) || (job.Origin != "model_http" && job.Origin != bgreplace.OriginSubjectLock && job.Origin != bgreplace.OriginModelPlate) {
		writeErr(w, http.StatusNotFound, "not_found", "没有可打开的背景结果")
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, "integrity_failed", "文件字节与登记不一致")
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("X-Bg-Origin", job.Origin)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleRefreshBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	mode := job.Mode
	if bgTerminal(job) {
		job.Mode = mode
		job.Deliverable = false
		job, ok = s.persistBgGuard(w, r, job)
		if !ok {
			return
		}
		job.Mode = mode
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	if job.PlatformTaskID == "" {
		job, ok = s.persistBgGuard(w, r, job)
		if !ok {
			return
		}
		job.Mode = mode
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	st, err := s.Tasks.Status(r.Context(), job.PlatformTaskID)
	if err != nil {
		restored := bgreplace.Restore(executionFromJob(job), bgreplace.ClassifySubmitFailure(err))
		job.JobStatus = restored.Status
		job.Mode = mode
		job.Quality = keepBgFail(restored.Quality)
		job.Deliverable = false
		job.Pending = restored.Pending
		if restored.AllowedUses != nil {
			job.AllowedUses = restored.AllowedUses
		}
		if uerr := s.St.UpdateBgJob(r.Context(), job); uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, true)})
		return
	}
	switch st.Status {
	case "succeeded":
		if job.Quality != bgreplace.QualityFail {
			job.JobStatus = bgreplace.StatusCompleted
		}
		absorbBgRefs(&job, st.Result)
	case "failed":
		restored := bgreplace.Restore(executionFromJob(job), bgreplace.StatusFailed)
		job.JobStatus = restored.Status
		job.Quality = keepBgFail(job.Quality)
		job.Pending = restored.Pending
	default:
		if job.JobStatus != bgreplace.StatusUnknown {
			job.JobStatus = bgreplace.StatusRunning
		}
	}
	job.Mode = mode
	job.Quality = keepBgFail(job.Quality)
	job.Deliverable = false
	job, ok = s.persistBgGuard(w, r, job)
	if !ok {
		return
	}
	job.Mode = mode
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

func (s *Server) handleInspectBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	var req bgreplace.ProductChecks
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "检查项必须是 JSON,不能自报通过或费用")
		return
	}
	mode := job.Mode
	next, checks, err := bgreplace.InspectProduct(executionFromJob(job), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	raw, err := json.Marshal(checks)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "保存商品检查失败")
		return
	}
	job.ProductChecksJSON = string(raw)
	job.Quality = next.Quality
	job.Mode = mode
	job.Deliverable = false
	job.Pending = next.Pending
	job.AllowedUses = next.AllowedUses
	job, ok = s.persistBgGuard(w, r, job)
	if !ok {
		return
	}
	job.Mode = mode
	writeJSON(w, http.StatusOK, map[string]any{"job": s.bgView(job, false)})
}

func (s *Server) handleSelectBgReplace(w http.ResponseWriter, r *http.Request) {
	job, ok := s.loadBgJob(w, r)
	if !ok {
		return
	}
	// 模型字节和主体锁定预览可以查看。商品检查未完成，不能选定为可用候选。
	if job.Origin == "model_http" || job.Origin == bgreplace.OriginSubjectLock {
		rejectUnusableCandidate(w)
		return
	}
	mode := job.Mode
	job, ok = s.persistBgGuard(w, r, job)
	if !ok {
		return
	}
	job.Mode = mode
	next, err := bgreplace.Select(executionFromJob(job))
	if err != nil {
		_ = s.St.UpdateBgJob(r.Context(), job)
		writeErr(w, http.StatusConflict, "not_deliverable", err.Error())
		return
	}
	job.Selection = next.Selection
	job.Mode = mode
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
	// 模型字节和主体锁定预览可以查看。商品检查未完成，不能导出为可用候选。
	if job.Origin == "model_http" || job.Origin == bgreplace.OriginSubjectLock {
		rejectUnusableCandidate(w)
		return
	}
	mode := job.Mode
	job, ok = s.persistBgGuard(w, r, job)
	if !ok {
		return
	}
	job.Mode = mode
	rec, err := bgreplace.Export(executionFromJob(job))
	if err != nil {
		_ = s.St.UpdateBgJob(r.Context(), job)
		writeErr(w, http.StatusConflict, "export_blocked", err.Error())
		return
	}
	job.ExportCount++
	job.Mode = mode
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
	label, settlement := s.shownBilling(job.BillingLabel, job.JobStatus, job.FeeRef)
	checks := bgreplace.ParseChecks(job.ProductChecksJSON)
	done, notice := bgreplace.RealGenerationState(bgreplace.RealGenerationInput{
		Credential: s.Cfg.BgRealModelConfigured(),
		ModelRef:   job.ModelRef,
		TaskID:     job.PlatformTaskID,
		AssetID:    job.OutputAssetID,
		FeeRef:     job.FeeRef,
		Quality:    job.Quality,
		Checks:     checks,
	})
	if !done {
		notice = bgreplace.RealGenerationIncomplete
	}
	return map[string]any{
		"id": job.ID, "project_id": job.ProjectID, "input_id": job.InputID,
		"input_version": job.InputVersion, "mode": job.Mode, "protected_region": job.ProtectedRegion,
		"protection_scope":  job.ProtectedRegion,
		"background_intent": job.BackgroundIntent, "fingerprint": job.Fingerprint,
		"quote_status": job.QuoteStatus, "billing_label": label, "settlement": settlement,
		"charged":    nil,
		"job_status": job.JobStatus, "quality": job.Quality, "evidence": job.Evidence,
		"platform_task_id": job.PlatformTaskID, "model_ref": job.ModelRef, "fee_ref": job.FeeRef,
		"output_asset_id": job.OutputAssetID,
		"output_version":  job.OutputVersion, "selection": job.Selection, "export_count": job.ExportCount,
		"deliverable": job.Deliverable, "pending": job.Pending, "allowed_uses": job.AllowedUses,
		"product_checks":               checks,
		"production_generation_passed": false, "billing_passed": false,
		"production_authorized":     false,
		"origin":                    job.Origin,
		"real_generation_completed": done, "real_generation_notice": notice,
		"delivery_readiness": bgreplace.ReadinessInternal, "honesty": bgreplace.HonestyNotice,
		"idempotent": idempotent,
	}
}

type bgForm struct {
	Mode             string `json:"mode"`
	InputID          string `json:"input_id"`
	BackgroundIntent string `json:"background_intent"`
	ProtectedRegion  string `json:"protected_region"`
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
	if req.ProtectedRegion != "" && req.ProtectedRegion != job.ProtectedRegion {
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

func (s *Server) persistBgGuard(w http.ResponseWriter, r *http.Request, job store.BgJob) (store.BgJob, bool) {
	mode := job.Mode
	failed, err := s.bgFidelityFailed(r, job)
	if err != nil {
		writeStoreErr(w, err)
		return job, false
	}
	next := bgreplace.ApplyProductGuard(executionFromJob(job), bgreplace.ParseChecks(job.ProductChecksJSON), failed)
	job.Mode = mode
	job.Quality = next.Quality
	job.Deliverable = false
	job.Pending = next.Pending
	if next.Quality == bgreplace.QualityFail {
		job.Selection = ""
		if len(next.AllowedUses) > 0 {
			job.AllowedUses = next.AllowedUses
		}
	}
	if err := s.St.UpdateBgJob(r.Context(), job); err != nil {
		writeStoreErr(w, err)
		return job, false
	}
	return job, true
}

func (s *Server) bgFidelityFailed(r *http.Request, job store.BgJob) (bool, error) {
	p, _ := principalFrom(r.Context())
	assetID := ""
	if in, err := s.St.GetInput(r.Context(), p.Tenant(), job.ProjectID, job.InputID); err == nil {
		assetID = in.PlatformAssetID
	}
	list, err := s.St.ListFidelityReports(r.Context(), p.Tenant(), job.ProjectID)
	if err != nil {
		return false, err
	}
	for _, row := range list {
		matches := row.InputRef == job.InputID || (assetID != "" && row.InputRef == assetID) ||
			(job.OutputAssetID != "" && row.OutputRef == job.OutputAssetID)
		if matches && row.Verdict == fidelity.VerdictFail {
			return true, nil
		}
	}
	return false, nil
}

func bgTerminal(job store.BgJob) bool {
	return job.JobStatus == bgreplace.StatusFailed ||
		job.JobStatus == bgreplace.StatusQuotaInsufficient ||
		job.Quality == bgreplace.QualityFail
}

func keepBgFail(quality string) string {
	if quality == bgreplace.QualityFail {
		return bgreplace.QualityFail
	}
	if quality == "" {
		return bgreplace.QualityUnknown
	}
	return quality
}

func absorbBgRefs(job *store.BgJob, result map[string]any) {
	if job.ModelRef == "" {
		job.ModelRef = firstResultString(result, "model_ref", "model")
	}
	if job.FeeRef == "" {
		job.FeeRef = firstResultString(result, "fee_ref", "billing_ref", "fee_id")
	}
	if job.OutputAssetID == "" {
		if id := firstResultString(result, "asset_id", "output_asset_id"); id != "" {
			job.OutputAssetID = id
			if job.OutputVersion == "" {
				job.OutputVersion = job.ID
			}
		}
	}
}

func firstResultString(result map[string]any, keys ...string) string {
	if result == nil {
		return ""
	}
	for _, key := range keys {
		if value, _ := result[key].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
