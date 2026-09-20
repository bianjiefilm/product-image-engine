package httpapi

// 批量生成批次编排(HUI-1704 / FEAT-0205)。
//
// 本文件是批次编排层:批量创建工程/输入 → 批量提交生成 → 批量收集。
// 红线(票面+拍板):
//   - 生成语义沿 I0 既有链路:task 提交信封(SUBMIT PROVISIONAL 形状)不变更,
//     场景作为 payload 参数携带;成果登记复用既有 outputs 路径;变体复用 1703 幂等。
//   - 全部服务端状态机;幂等键 = batch_id + item 输入 sha256(store 唯一键)+
//     attempt 序号(task 幂等键;重试递增 → 新 task 引用)。
//   - 并发上限 4(进程内信号量),超出排队。
//   - worker/task 不可用时批次如实失败/阻塞,绝不伪造成功。
//   - 租户隔离:批次/项目按 principal 租户作用域;取消批次=owner。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sizeadapt"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// 批次编排常量(拍板/自决,REPORT 记录)。
const (
	// batchSubmitConcurrency 拍板:单批提交并发上限(进程内信号量)。
	batchSubmitConcurrency = 4
	// batchMaxItems 单批次文件数上限(验收场景 8 项;防失控请求)。
	batchMaxItems = 50
	// batchMaxTotalBytes 请求体上限:≤50 文件 × 8MiB × base64 膨胀(~1.37)+JSON。
	batchMaxTotalBytes = 96 << 20
)

// ---- 纯函数状态机 -----------------------------------------------------------

// batchOpenItem 未到终态的 item(pending/submitted 及未知态一律视为在途,宁保守)。
func batchOpenItem(status string) bool {
	switch status {
	case "succeeded", "failed", "blocked", "cancelled":
		return false
	}
	return true
}

// nextBatchStatus 由 items 聚合批次终态(拍板枚举内);仍有在途项时返回 ""(保持现态)。
// 聚合口径:全 succeeded→completed;有 succeeded 有 failed/blocked→completed_with_failures;
// 全 cancelled→cancelled;其余(全 failed/blocked)→failed。
func nextBatchStatus(items []store.BatchItem) string {
	var succeeded, failedLike, cancelled, open int
	for _, it := range items {
		switch it.Status {
		case "succeeded":
			succeeded++
		case "failed", "blocked":
			failedLike++
		case "cancelled":
			cancelled++
		default:
			open++
		}
	}
	if open > 0 {
		return ""
	}
	switch {
	case cancelled > 0 && succeeded == 0 && failedLike == 0:
		return "cancelled"
	case succeeded > 0 && failedLike == 0:
		return "completed"
	case succeeded > 0:
		return "completed_with_failures"
	default:
		return "failed"
	}
}

// ---- 视图 -------------------------------------------------------------------

// batchPresetSummary preset_summary JSON 的规范形状(所选场景×尺寸意图)。
type batchPresetSummary struct {
	Scenes               []string `json:"scenes"`
	Presets              []string `json:"presets"`
	UsageNote            string   `json:"usage_note"`
	FinalizeWithVariants bool     `json:"finalize_with_variants"`
}

type batchView struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	CreatedBy     string          `json:"created_by"`
	ProjectID     string          `json:"project_id"`
	Name          string          `json:"name"`
	Status        string          `json:"status"`
	PresetSummary json.RawMessage `json:"preset_summary"`
	ErrorCode     string          `json:"error_code"`
	Counts        map[string]int  `json:"counts"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type batchItemView struct {
	ID             string    `json:"id"`
	BatchID        string    `json:"batch_id"`
	ProjectID      string    `json:"project_id"`
	InputRef       string    `json:"input_ref"`
	InputName      string    `json:"input_name"`
	InputSHA256    string    `json:"input_sha256"`
	InputSize      int64     `json:"input_size"`
	TaskRef        string    `json:"task_ref"`
	Status         string    `json:"status"`
	ErrorCode      string    `json:"error_code"`
	OutputID       string    `json:"output_id"`
	SizeVariantIDs []string  `json:"size_variant_ids"`
	VariantsNote   string    `json:"variants_note"`
	Attempts       int       `json:"attempts"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func newBatchView(ctx context.Context, st *store.Store, b store.Batch) batchView {
	counts := map[string]int{"total": 0, "pending": 0, "submitted": 0, "succeeded": 0, "failed": 0, "blocked": 0, "cancelled": 0}
	if c, err := st.CountBatchItemsByStatus(ctx, b.ID); err == nil {
		total := 0
		for k, n := range c {
			total += n
			if _, ok := counts[k]; ok {
				counts[k] = n
			}
		}
		counts["total"] = total
	}
	ps := json.RawMessage(b.PresetSummary)
	if !json.Valid(ps) {
		ps = json.RawMessage(`{}`)
	}
	return batchView{
		ID: b.ID, TenantID: b.TenantID, CreatedBy: b.CreatedBy, ProjectID: b.ProjectID,
		Name: b.Name, Status: b.Status, PresetSummary: ps, ErrorCode: b.ErrorCode,
		Counts: counts, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

func newBatchItemView(it store.BatchItem) batchItemView {
	var ids []string
	if it.SizeVariantIDs != "" {
		_ = json.Unmarshal([]byte(it.SizeVariantIDs), &ids)
	}
	return batchItemView{
		ID: it.ID, BatchID: it.BatchID, ProjectID: it.ProjectID, InputRef: it.InputRef,
		InputName: it.InputName, InputSHA256: it.InputSHA256, InputSize: it.InputSize,
		TaskRef: it.TaskRef, Status: it.Status, ErrorCode: it.ErrorCode, OutputID: it.OutputID,
		SizeVariantIDs: ids, VariantsNote: it.VariantsNote, Attempts: it.Attempts,
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

func newBatchItemViews(items []store.BatchItem) []batchItemView {
	out := make([]batchItemView, 0, len(items))
	for _, it := range items {
		out = append(out, newBatchItemView(it))
	}
	return out
}

// ---- POST /api/v1/batches ---------------------------------------------------

type batchCreateFile struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	DataB64     string `json:"data_b64"`
}

type batchCreateRequest struct {
	Name                 string            `json:"name"`
	Scenes               []string          `json:"scenes"`
	Presets              []string          `json:"presets"`
	UsageNote            string            `json:"usage_note"`
	FinalizeWithVariants bool              `json:"finalize_with_variants"`
	SourceType           string            `json:"source_type"`
	SourceRef            string            `json:"source_ref"`
	ProjectID            string            `json:"project_id"` // 可选:复用既有工程(缺省新建)
	Items                []batchCreateFile `json:"items"`
}

// handleCreateBatch 批量上传产品建批:每个文件过既有 upload 输入门(合法 PNG≤8MiB,
// 真解码)→ 平台 asset 登记(网络调用在事务外)→ 单事务建 工程+inputs+batch+items。
// 不做生成门:开关 off 也要能创建(提交步才 blocked,如实)。
func (s *Server) handleCreateBatch(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant := p.Tenant()
	var req batchCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, batchMaxTotalBytes)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON(items[].data_b64 为合法 PNG ≤8MiB)")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "批次名称不能为空")
		return
	}
	if (req.SourceType == "order" || req.SourceType == "campaign") && strings.TrimSpace(req.SourceRef) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "挂接 order/campaign 来源时必须提供 source_ref")
		return
	}
	if len(req.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "批次至少要有一个输入文件")
		return
	}
	if len(req.Items) > batchMaxItems {
		writeErr(w, http.StatusBadRequest, "too_many_items", fmt.Sprintf("单批次文件数上限 %d", batchMaxItems))
		return
	}
	// 预设校验:尺寸适配开 → 先验预设名(fail fast);关 → 收集时如实标注 skipped。
	if s.Cfg.SizeAdaptEnabled && len(req.Presets) > 0 {
		if s.Presets == nil {
			writeErr(w, http.StatusServiceUnavailable, "presets_unavailable", "尺寸适配预设集未装配,拒绝服务(fail-closed)")
			return
		}
		for _, name := range req.Presets {
			if _, ok := s.Presets.Get(strings.TrimSpace(name)); !ok {
				writeErr(w, http.StatusBadRequest, "unknown_preset", "未知预设:"+name+"(GET /api/v1/size-adapt/presets 查看可用列表)")
				return
			}
		}
	}
	// upload 可用性 fail-closed:批次输入必须成为真实平台 asset 引用(不伪造引用)。
	if s.Uploads == nil || s.Cfg.UploadBaseURL == "" || s.Cfg.UploadToken == "" {
		writeErr(w, http.StatusServiceUnavailable, "upload_not_configured",
			"素材登记服务未配置(缺少 PLATFORM_UPLOAD_BASE_URL 或 PLATFORM_UPLOAD_TOKEN),不伪造登记成功")
		return
	}
	// 逐文件:解码 base64 → 既有输入门(魔数+8MiB+真解码+像素上限)→ 去重(同批次同内容
	// 只成一项,幂等键 batch_id+sha256)→ 平台登记。
	specs := make([]store.BatchItemSpec, 0, len(req.Items))
	seen := map[string]bool{}
	var duplicates []string
	for i, f := range req.Items {
		data, err := base64.StdEncoding.DecodeString(f.DataB64)
		if err != nil || len(data) == 0 {
			writeErr(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("items[%d].data_b64 必须是非空 base64", i))
			return
		}
		if _, err := sizeadapt.DecodePNG(data); err != nil {
			writeSizeInputErr(w, err)
			return
		}
		sum := sha256.Sum256(data)
		shaHex := hex.EncodeToString(sum[:])
		if seen[shaHex] {
			duplicates = append(duplicates, f.FileName)
			continue
		}
		seen[shaHex] = true
		reg, err := s.Uploads.RegisterAsset(r.Context(), platform.RegisterAssetRequest{
			FileName: f.FileName, ContentType: "image/png", SizeBytes: int64(len(data)),
			SHA256: shaHex, ContentB64: base64.StdEncoding.EncodeToString(data),
		})
		if err != nil {
			// 素材登记失败:批次不建(此前已登记的平台 asset 为惰性引用,无副作用)。
			platformErrToApiErr("素材登记", err).write(w)
			return
		}
		name := f.FileName
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("batch-item-%02d.png", i+1)
		}
		specs = append(specs, store.BatchItemSpec{
			AssetID: reg.AssetID, Name: name, Size: int64(len(data)), SHA256: shaHex, ContentType: "image/png",
		})
	}
	ps := batchPresetSummary{Scenes: req.Scenes, Presets: req.Presets, UsageNote: req.UsageNote, FinalizeWithVariants: req.FinalizeWithVariants}
	psRaw, err := json.Marshal(ps)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "服务内部错误")
		return
	}
	batch, items, err := s.St.CreateBatch(r.Context(), store.Batch{
		TenantID: tenant, CreatedBy: p.UserID, ProjectID: req.ProjectID,
		Name: req.Name, PresetSummary: string(psRaw),
	}, specs)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	resp := map[string]any{
		"batch": newBatchView(r.Context(), s.St, batch),
		"items": newBatchItemViews(items),
	}
	if len(duplicates) > 0 {
		resp["duplicates"] = duplicates
	}
	writeJSON(w, http.StatusCreated, resp)
}

// ---- GET /api/v1/batches, GET /api/v1/batches/{id} ---------------------------

func (s *Server) handleListBatches(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	list, err := s.St.ListBatches(r.Context(), p.Tenant())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取批次列表失败")
		return
	}
	views := make([]batchView, 0, len(list))
	for _, b := range list {
		views = append(views, newBatchView(r.Context(), s.St, b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": views})
}

// handleGetBatch 批次详情(零副作用:查询自动推进不做,collect 手动/面板轮询,拍板)。
func (s *Server) handleGetBatch(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	batch, items, err := s.St.GetBatchWithItems(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"batch": newBatchView(r.Context(), s.St, batch), "items": newBatchItemViews(items),
	})
}

// ---- POST /api/v1/batches/{id}/submit ----------------------------------------

// submitOutcome 单项提交事实。
type submitOutcome struct {
	status    string // submitted|blocked|failed
	taskRef   string
	errorCode string
}

// submitSemaphore 批量提交信号量(拍板:并发上限 4;惰性初始化,进程内共享)。
func (s *Server) submitSemaphore() chan struct{} {
	s.semMu.Lock()
	defer s.semMu.Unlock()
	if s.batchSubmitSem == nil {
		s.batchSubmitSem = make(chan struct{}, batchSubmitConcurrency)
	}
	return s.batchSubmitSem
}

// submitBatchItems 逐项提交(信封沿 I0 PROVISIONAL 形状;场景作为 payload 参数)。
// task 幂等键 = batch-{batchID}-{sha256前16}-a{attempts}:同批同输入同序号平台侧幂等,
// 重试递增 attempts → 新 task 引用。并发受信号量限制,超出排队。
// 用 WithoutCancel 派生上下文:客户端中途断开不得让「已发出的任务」被误记为
// 失败/拒绝(那会诱导重复重试、重复生成)——要么如实 submitted,要么如实 blocked。
func (s *Server) submitBatchItems(ctx context.Context, tenant string, batch store.Batch, items []store.BatchItem) map[string]submitOutcome {
	assetByRef := map[string]string{}
	if ins, err := s.St.ListInputs(ctx, tenant, batch.ProjectID); err == nil {
		for _, in := range ins {
			assetByRef[in.ID] = in.PlatformAssetID
		}
	}
	var ps batchPresetSummary
	_ = json.Unmarshal([]byte(batch.PresetSummary), &ps)
	detached := context.WithoutCancel(ctx)

	sem := s.submitSemaphore()
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := make(map[string]submitOutcome, len(items))
	for _, it := range items {
		wg.Add(1)
		sem <- struct{}{} // 超出并发上限在此排队
		go func(it store.BatchItem) {
			defer wg.Done()
			defer func() { <-sem }()
			attempts, err := s.St.IncrementBatchItemAttempt(detached, tenant, it.ID)
			if err != nil {
				mu.Lock()
				out[it.ID] = submitOutcome{status: "failed", errorCode: "internal"}
				mu.Unlock()
				return
			}
			res, err := s.Tasks.Submit(detached, platform.SubmitRequest{
				IdempotencyKey: fmt.Sprintf("batch-%s-%s-a%d", batch.ID, shortHex(it.InputSHA256), attempts),
				Kind:           "product-image-generate",
				Payload: map[string]any{
					"project_id":        it.ProjectID,
					"platform_asset_id": assetByRef[it.InputRef],
					"usage_note":        ps.UsageNote,
					"scenes":            ps.Scenes,
					"batch_id":          batch.ID,
					"batch_item_id":     it.ID,
				},
			})
			switch {
			case err == nil:
				mu.Lock()
				out[it.ID] = submitOutcome{status: "submitted", taskRef: res.TaskID}
				mu.Unlock()
			case errors.Is(err, platform.ErrUnavailable):
				mu.Lock()
				out[it.ID] = submitOutcome{status: "blocked", errorCode: "task_unavailable"}
				mu.Unlock()
			default:
				mu.Lock()
				out[it.ID] = submitOutcome{status: "failed", errorCode: "task_rejected"}
				mu.Unlock()
			}
		}(it)
	}
	wg.Wait()
	return out
}

func shortHex(sha string) string {
	if len(sha) > 16 {
		return sha[:16]
	}
	return sha
}

// applySubmitOutcomes 把提交事实守卫式落列;返回落列后的 items。
func (s *Server) applySubmitOutcomes(ctx context.Context, tenant, batchID string, items []store.BatchItem, outcomes map[string]submitOutcome) []store.BatchItem {
	for _, it := range items {
		oc, ok := outcomes[it.ID]
		if !ok {
			continue
		}
		_, err := s.St.UpdateBatchItemStatus(ctx, tenant, it.ID, []string{it.Status}, oc.status, oc.taskRef, oc.errorCode)
		if err != nil {
			// 守卫冲突(状态已被并发推进):保留现状,如实不覆盖。
			log.Printf("batch item 状态推进冲突(保留现状): batch=%s item=%s: %v", batchID, it.ID, err)
		}
	}
	fresh, err := s.St.ListBatchItems(ctx, tenant, batchID)
	if err != nil {
		return items
	}
	return fresh
}

// settleBatch 依据 items 聚合推进批次状态(提交/收集后调用):
// 有终态聚合 → 落终态(failed 时附批次级原因);否则提交/收集进行中的批次归位 running。
func (s *Server) settleBatch(ctx context.Context, tenant string, batch store.Batch, items []store.BatchItem, failCode string) store.Batch {
	if next := nextBatchStatus(items); next != "" {
		code := ""
		if next == "failed" {
			code = failCode
		}
		if b, err := s.St.UpdateBatchStatus(ctx, tenant, batch.ID,
			[]string{"pending", "submitting", "running", "completing", "failed"}, next, code); err == nil {
			return b
		}
	} else if batch.Status == "pending" || batch.Status == "submitting" || batch.Status == "completing" {
		if b, err := s.St.UpdateBatchStatus(ctx, tenant, batch.ID, []string{batch.Status}, "running", ""); err == nil {
			return b
		}
	}
	if b, err := s.St.GetBatch(ctx, tenant, batch.ID); err == nil {
		return b
	}
	return batch
}

// handleSubmitBatch 批量提交:
//   - 生成门不过(FEATURE_GENERATION_ENABLED off / 配置缺失)→ 全部 pending item 置
//     blocked(点名原因),批次 failed——如实落终态并返回 200(不伪造、不静默)。
//   - 门过 → 批次 submitting,逐项提交(并发≤4),受理 submitted / 不可达 blocked /
//     拒绝 failed;结束后批次归位(running 或终态)。
//   - 已提交项不动(幂等);blocked 项在门修复后可再次 submit(非盲目,先过门)。
func (s *Server) handleSubmitBatch(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant := p.Tenant()
	ctx := r.Context()
	batchID := r.PathValue("id")
	batch, err := s.St.GetBatch(ctx, tenant, batchID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	switch batch.Status {
	case "completed", "completed_with_failures", "cancelled":
		writeErr(w, http.StatusConflict, "invalid_state",
			"批次已完结("+batch.Status+"),不能提交;失败项请用 retry-failed")
		return
	}
	if st, msg := s.Cfg.GenerationUsable(); st != 0 {
		// 门不过:全部 pending → blocked,批次 failed,如实记录(不伪造成功)。
		items, lerr := s.St.ListBatchItems(ctx, tenant, batchID)
		if lerr != nil {
			writeErr(w, http.StatusInternalServerError, "internal", "读取批次项失败")
			return
		}
		for _, it := range items {
			if it.Status == "pending" {
				_, _ = s.St.UpdateBatchItemStatus(ctx, tenant, it.ID, []string{"pending"}, "blocked", "", "generation_disabled")
			}
		}
		if fresh, lerr := s.St.ListBatchItems(ctx, tenant, batchID); lerr == nil {
			items = fresh
		}
		batch = s.settleBatch(ctx, tenant, batch, items, "generation_disabled")
		writeJSON(w, http.StatusOK, map[string]any{
			"batch": newBatchView(ctx, s.St, batch), "items": newBatchItemViews(items),
			"note": msg,
		})
		return
	}
	// 批次 → submitting(允许从 pending/running/failed(all-blocked)推进:重入提交)
	if _, err := s.St.UpdateBatchStatus(ctx, tenant, batchID, []string{batch.Status}, "submitting", ""); err != nil {
		writeErr(w, http.StatusConflict, "invalid_state", "批次状态推进冲突,请重试")
		return
	}
	items, err := s.St.ListBatchItems(ctx, tenant, batchID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取批次项失败")
		return
	}
	var due []store.BatchItem
	for _, it := range items {
		if it.Status == "pending" || it.Status == "blocked" {
			due = append(due, it)
		}
	}
	outcomes := s.submitBatchItems(ctx, tenant, batch, due)
	items = s.applySubmitOutcomes(ctx, tenant, batchID, items, outcomes)
	batch, _ = s.St.GetBatch(ctx, tenant, batchID)
	batch = s.settleBatch(ctx, tenant, batch, items, "task_unavailable")
	writeJSON(w, http.StatusOK, map[string]any{
		"batch": newBatchView(ctx, s.St, batch), "items": newBatchItemViews(items),
	})
}

// ---- POST /api/v1/batches/{id}/collect ----------------------------------------

// handleCollectBatch 批量收集:按 task 状态推进 item(手动触发;web 面板轮询之)。
//   - succeeded → 既有 outputs 登记路径落成果(+可选尺寸变体,复用 1703 幂等)→ succeeded;
//   - failed → item failed(error_code 取任务侧,缺省 task_failed);
//   - 仍在跑 → 保持 submitted;查询不可达 → 保持现态(不伪造,稍后再 collect);
//   - 任务成功但成果不合法 → failed(invalid_result),绝不伪造;
//   - 成果登记环境故障 → blocked(output_register_failed;collect 可重试登记)。
func (s *Server) handleCollectBatch(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant := p.Tenant()
	ctx := r.Context()
	batchID := r.PathValue("id")
	batch, err := s.St.GetBatch(ctx, tenant, batchID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	items, err := s.St.ListBatchItems(ctx, tenant, batchID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取批次项失败")
		return
	}
	var due []store.BatchItem
	for _, it := range items {
		if it.Status == "submitted" || (it.Status == "blocked" && it.TaskRef != "") {
			due = append(due, it)
		}
	}
	if len(due) > 0 {
		// 批次 → completing(展示收集进行中;终态由 settle 决定)
		_, _ = s.St.UpdateBatchStatus(ctx, tenant, batchID,
			[]string{"pending", "submitting", "running", "failed"}, "completing", "")
		for _, it := range due {
			s.collectItem(ctx, tenant, batch, it)
		}
		if fresh, lerr := s.St.ListBatchItems(ctx, tenant, batchID); lerr == nil {
			items = fresh
		}
	}
	batch, _ = s.St.GetBatch(ctx, tenant, batchID)
	batch = s.settleBatch(ctx, tenant, batch, items, "task_unavailable")
	writeJSON(w, http.StatusOK, map[string]any{
		"batch": newBatchView(ctx, s.St, batch), "items": newBatchItemViews(items),
	})
}

// collectItem 单项收集:查任务 → 推进 item。查询失败保持现态(如实)。
func (s *Server) collectItem(ctx context.Context, tenant string, batch store.Batch, it store.BatchItem) {
	res, err := s.Tasks.Status(ctx, it.TaskRef)
	if err != nil {
		return // 不可达/5xx:保持现态,稍后再 collect(不伪造状态)
	}
	switch res.Status {
	case "succeeded":
		s.finalizeItem(ctx, tenant, batch, it, res.Result)
	case "failed":
		code := "task_failed"
		if res.Result != nil {
			if c, ok := res.Result["error_code"].(string); ok && strings.TrimSpace(c) != "" {
				code = c
			}
		}
		_, _ = s.St.UpdateBatchItemResult(ctx, tenant, it.ID,
			store.ItemResultUpdate{Status: "failed", ErrorCode: code})
	default:
		// pending/running/queued:保持 submitted
	}
}

// finalizeItem 任务成功收尾:成果经既有 outputs 登记路径落库(+可选尺寸变体)。
func (s *Server) finalizeItem(ctx context.Context, tenant string, batch store.Batch, it store.BatchItem, result map[string]any) {
	dataB64, _ := result["data_b64"].(string)
	fileName, _ := result["file_name"].(string)
	if strings.TrimSpace(fileName) == "" {
		fileName = it.InputName
	}
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil || len(data) == 0 {
		// 任务成功但成果不可用:如实失败,绝不伪造。
		_, _ = s.St.UpdateBatchItemResult(ctx, tenant, it.ID,
			store.ItemResultUpdate{Status: "failed", ErrorCode: "invalid_result"})
		return
	}
	out, dup, aerr := s.registerOutputBytes(ctx, tenant, it.ProjectID, fileName, "image/png", data, nil)
	if aerr != nil {
		switch aerr.code {
		case "upload_not_configured", "upstream_unavailable":
			// 环境不可用:阻塞(保留 task_ref,collect 可重试登记)。
			_, _ = s.St.UpdateBatchItemResult(ctx, tenant, it.ID,
				store.ItemResultUpdate{Status: "blocked", ErrorCode: "output_register_failed"})
		default:
			// 成果内容不合法(超限/非 PNG):如实失败。
			_, _ = s.St.UpdateBatchItemResult(ctx, tenant, it.ID,
				store.ItemResultUpdate{Status: "failed", ErrorCode: "invalid_result"})
		}
		return
	}
	_ = dup // 同内容已登记(重复 collect/同工程同内容):幂等,仍视为成功
	varIDs, note := s.finalizeVariants(ctx, tenant, batch, it, out, data)
	idsJSON := ""
	if len(varIDs) > 0 {
		if b, merr := json.Marshal(varIDs); merr == nil {
			idsJSON = string(b)
		}
	}
	_, _ = s.St.UpdateBatchItemResult(ctx, tenant, it.ID, store.ItemResultUpdate{
		Status: "succeeded", OutputID: out.ID, SizeVariantIDs: idsJSON, VariantsNote: note,
	})
}

// finalizeVariants 「多尺寸」收尾(拍板 3):finalize_with_variants=true 且
// FEATURE_SIZE_ADAPT on → 按所选预设逐一生成变体(复用 1703 幂等,重复 collect 幂等);
// 否则如实标注 skipped/失败原因(item 本身仍 succeeded:生成与登记是事实)。
func (s *Server) finalizeVariants(ctx context.Context, tenant string, batch store.Batch, it store.BatchItem, src store.ProjectOutput, data []byte) ([]string, string) {
	var ps batchPresetSummary
	_ = json.Unmarshal([]byte(batch.PresetSummary), &ps)
	if !ps.FinalizeWithVariants || len(ps.Presets) == 0 {
		return nil, ""
	}
	if !s.Cfg.SizeAdaptEnabled || s.Presets == nil {
		return nil, "skipped: FEATURE_SIZE_ADAPT off(或预设集未装配),变体未生成"
	}
	img, err := sizeadapt.DecodePNG(data)
	if err != nil {
		return nil, "skipped: 成果不可解码为 PNG,变体未生成"
	}
	var ids []string
	var notes []string
	for _, name := range ps.Presets {
		preset, aerr := s.resolvePreset(strings.TrimSpace(name))
		if aerr != nil {
			notes = append(notes, name+": skipped(未知预设)")
			continue
		}
		v, _, aerr := s.adaptRegisterVariant(ctx, tenant, it.ProjectID, src, preset, sizeadapt.FormatPNG, img)
		if aerr != nil {
			notes = append(notes, fmt.Sprintf("%s: failed(%s)", preset.Name, aerr.code))
			continue
		}
		ids = append(ids, v.ID)
	}
	if len(notes) > 0 {
		return ids, strings.Join(notes, "; ")
	}
	if len(ids) > 0 {
		return ids, fmt.Sprintf("%d 变体", len(ids))
	}
	return nil, ""
}

// ---- POST /api/v1/batches/{id}/retry-failed -----------------------------------

// handleRetryFailedBatch 失败项重试:仅 failed(新 task 引用);blocked 不盲目重试,
// 响应附「先修配置」提示;生成门不过 → 503 点名开关(提示先修配置)。
func (s *Server) handleRetryFailedBatch(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant := p.Tenant()
	ctx := r.Context()
	batchID := r.PathValue("id")
	batch, err := s.St.GetBatch(ctx, tenant, batchID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if st, msg := s.Cfg.GenerationUsable(); st != 0 {
		writeErr(w, st, "generation_disabled", msg+"(请先修复配置再重试)")
		return
	}
	switch batch.Status {
	case "cancelled", "completed":
		writeErr(w, http.StatusConflict, "invalid_state", "批次已完结("+batch.Status+"),无失败项可重试")
		return
	}
	items, err := s.St.ListBatchItems(ctx, tenant, batchID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取批次项失败")
		return
	}
	var due []store.BatchItem
	blockedCount := 0
	for _, it := range items {
		switch it.Status {
		case "failed":
			due = append(due, it)
		case "blocked":
			blockedCount++
		}
	}
	if len(due) == 0 {
		writeErr(w, http.StatusConflict, "invalid_state", "没有 failed 项可重试")
		return
	}
	if _, err := s.St.UpdateBatchStatus(ctx, tenant, batchID, []string{batch.Status}, "submitting", ""); err != nil {
		writeErr(w, http.StatusConflict, "invalid_state", "批次状态推进冲突,请重试")
		return
	}
	outcomes := s.submitBatchItems(ctx, tenant, batch, due)
	items = s.applySubmitOutcomes(ctx, tenant, batchID, items, outcomes)
	batch, _ = s.St.GetBatch(ctx, tenant, batchID)
	batch = s.settleBatch(ctx, tenant, batch, items, "task_unavailable")
	resp := map[string]any{
		"batch": newBatchView(ctx, s.St, batch), "items": newBatchItemViews(items),
		"retried": len(due),
	}
	if blockedCount > 0 {
		resp["note"] = fmt.Sprintf("存在 %d 个 blocked 项:不盲目重试,请先修复配置(生成开关/上游服务地址)后用 submit 重试", blockedCount)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- POST /api/v1/batches/{id}/cancel -----------------------------------------

// handleCancelBatch 取消批次:owner only;仅 pending 批次,或 failed 且全部 item
// 均为 blocked/cancelled(即「全阻塞」失败态)可取消 → items 全 cancelled。
// 已提交任务不撤单(本规则下可取消的批次没有 submitted 项;若未来出现由收集按事实对齐)。
func (s *Server) handleCancelBatch(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	tenant := p.Tenant()
	ctx := r.Context()
	batchID := r.PathValue("id")
	batch, err := s.St.GetBatch(ctx, tenant, batchID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if batch.CreatedBy != p.UserID {
		writeErr(w, http.StatusForbidden, "not_owner", "只有批次创建者可以取消")
		return
	}
	items, err := s.St.ListBatchItems(ctx, tenant, batchID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取批次项失败")
		return
	}
	allowed := batch.Status == "pending"
	if !allowed && batch.Status == "failed" && len(items) > 0 {
		allBlocked := true
		for _, it := range items {
			if it.Status != "blocked" && it.Status != "cancelled" {
				allBlocked = false
				break
			}
		}
		allowed = allBlocked
	}
	if !allowed {
		writeErr(w, http.StatusConflict, "invalid_state",
			"批次已提交或已完结,不能取消(已提交任务不撤单)")
		return
	}
	for _, it := range items {
		if it.Status == "pending" || it.Status == "blocked" {
			_, _ = s.St.UpdateBatchItemStatus(ctx, tenant, it.ID, []string{it.Status}, "cancelled", "", "cancelled")
		}
	}
	batch, err = s.St.UpdateBatchStatus(ctx, tenant, batchID, []string{"pending", "failed"}, "cancelled", "")
	if err != nil {
		writeErr(w, http.StatusConflict, "invalid_state", "批次状态推进冲突,请重试")
		return
	}
	if fresh, lerr := s.St.ListBatchItems(ctx, tenant, batchID); lerr == nil {
		items = fresh
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"batch": newBatchView(ctx, s.St, batch), "items": newBatchItemViews(items),
	})
}
