package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BgJob 是背景替换的一条逻辑任务。生产出图与计费通过与否不存放在本行。
type BgJob struct {
	ID                string
	TenantID          string
	ProjectID         string
	InputID           string
	InputVersion      string
	Mode              string
	ProtectedRegion   string
	BackgroundIntent  string
	Fingerprint       string
	QuoteStatus       string
	BillingLabel      string
	JobStatus         string
	Quality           string
	Evidence          string
	PlatformTaskID    string
	OutputAssetID     string
	OutputVersion     string
	Selection         string
	ExportCount       int
	Deliverable       bool
	Pending           []string
	AllowedUses       []string
	ModelRef          string
	FeeRef            string
	ProductChecksJSON string
	Origin            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// bgResultOrigin 是解码后的模型字节。不是供应商成功，也不是计费通过。
const bgResultOrigin = "model_http"

func (s *Store) GetInput(ctx context.Context, tenantID, projectID, id string) (ProjectInput, error) {
	var in ProjectInput
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, project_id, tenant_id, platform_asset_id,
		snapshot_name, snapshot_size, snapshot_content_type, created_at
		FROM project_inputs WHERE id = ? AND project_id = ? AND tenant_id = ?`,
		id, projectID, tenantID).Scan(&in.ID, &in.ProjectID, &in.TenantID, &in.PlatformAssetID,
		&in.SnapshotName, &in.SnapshotSize, &in.SnapshotContentType, &created)
	if err == sql.ErrNoRows {
		return ProjectInput{}, ErrNotFound
	}
	if err != nil {
		return ProjectInput{}, fmt.Errorf("store: 读取输入失败: %w", err)
	}
	in.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return in, nil
}

func (s *Store) InsertBgJob(ctx context.Context, job BgJob) (BgJob, error) {
	if strings.TrimSpace(job.Fingerprint) == "" || strings.TrimSpace(job.ProjectID) == "" {
		return BgJob{}, fmt.Errorf("%w: 背景替换指纹与工程不能为空", ErrValidation)
	}
	now := Now()
	job.ID = newID("bgj")
	job.CreatedAt, job.UpdatedAt = now, now
	if job.Pending == nil {
		job.Pending = []string{}
	}
	if job.AllowedUses == nil {
		job.AllowedUses = []string{}
	}
	if strings.TrimSpace(job.ProductChecksJSON) == "" {
		job.ProductChecksJSON = "{}"
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return BgJob{}, err
	}
	uses, err := json.Marshal(job.AllowedUses)
	if err != nil {
		return BgJob{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO bg_replace_jobs (
		id, tenant_id, project_id, input_id, input_version, mode, protected_region, background_intent,
		fingerprint, quote_status, billing_label, job_status, quality, evidence, platform_task_id,
		output_asset_id, output_version, selection, export_count, deliverable, pending_json, allowed_uses_json,
		model_ref, fee_ref, product_checks_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.TenantID, job.ProjectID, job.InputID, job.InputVersion, job.Mode, job.ProtectedRegion,
		job.BackgroundIntent, job.Fingerprint, job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality,
		job.Evidence, job.PlatformTaskID, job.OutputAssetID, job.OutputVersion, job.Selection, job.ExportCount,
		boolInt(job.Deliverable), string(pending), string(uses), job.ModelRef, job.FeeRef, job.ProductChecksJSON,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return BgJob{}, fmt.Errorf("store: 写入背景替换失败: %w", err)
	}
	return job, nil
}

func (s *Store) UpdateBgJob(ctx context.Context, job BgJob) error {
	now := Now()
	job.UpdatedAt = now
	if job.Pending == nil {
		job.Pending = []string{}
	}
	if job.AllowedUses == nil {
		job.AllowedUses = []string{}
	}
	if strings.TrimSpace(job.ProductChecksJSON) == "" {
		job.ProductChecksJSON = "{}"
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return err
	}
	uses, err := json.Marshal(job.AllowedUses)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE bg_replace_jobs SET
		quote_status=?, billing_label=?, job_status=?, quality=?, evidence=?, platform_task_id=?,
		output_asset_id=?, output_version=?, selection=?, export_count=?, deliverable=?,
		pending_json=?, allowed_uses_json=?, mode=?, model_ref=?, fee_ref=?, product_checks_json=?, updated_at=?
		WHERE id=? AND tenant_id=?`,
		job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality, job.Evidence, job.PlatformTaskID,
		job.OutputAssetID, job.OutputVersion, job.Selection, job.ExportCount, boolInt(job.Deliverable),
		string(pending), string(uses), job.Mode, job.ModelRef, job.FeeRef, job.ProductChecksJSON,
		now.Format(time.RFC3339), job.ID, job.TenantID)
	if err != nil {
		return fmt.Errorf("store: 更新背景替换失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetBgJob(ctx context.Context, tenantID, projectID, id string) (BgJob, error) {
	row := s.db.QueryRowContext(ctx, bgJobSelect+` WHERE id=? AND tenant_id=? AND project_id=?`, id, tenantID, projectID)
	job, err := scanBgJob(row)
	if err == sql.ErrNoRows {
		return BgJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) FindBgJobByFingerprint(ctx context.Context, tenantID, fingerprint string) (BgJob, error) {
	row := s.db.QueryRowContext(ctx, bgJobSelect+` WHERE tenant_id=? AND fingerprint=?`, tenantID, fingerprint)
	job, err := scanBgJob(row)
	if err == sql.ErrNoRows {
		return BgJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) ListBgJobs(ctx context.Context, tenantID, projectID string) ([]BgJob, error) {
	rows, err := s.db.QueryContext(ctx, bgJobSelect+` WHERE tenant_id=? AND project_id=? ORDER BY created_at DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BgJob
	for rows.Next() {
		job, err := scanBgJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// LatestBgJobForInput 返回该输入最近一条任务,没有则为 ErrNotFound。
func (s *Store) LatestBgJobForInput(ctx context.Context, tenantID, projectID, inputID string) (BgJob, error) {
	row := s.db.QueryRowContext(ctx, bgJobSelect+` WHERE tenant_id=? AND project_id=? AND input_id=? ORDER BY created_at DESC LIMIT 1`,
		tenantID, projectID, inputID)
	job, err := scanBgJob(row)
	if err == sql.ErrNoRows {
		return BgJob{}, ErrNotFound
	}
	return job, err
}

// ClaimBgSubmit 把已确认且仍为 quoted 的任务抢成 submitting。
// 只有抢到的请求可以调用平台任务。
func (s *Store) ClaimBgSubmit(ctx context.Context, tenantID, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE bg_replace_jobs SET job_status=?, updated_at=?
		WHERE id=? AND tenant_id=? AND job_status=? AND quote_status=?`,
		"submitting", Now().Format(time.RFC3339), id, tenantID, "quoted", "confirmed")
	if err != nil {
		return false, fmt.Errorf("store: 抢占背景替换提交失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
func (s *Store) InvalidateOpenBgQuotes(ctx context.Context, tenantID, projectID, inputID, keepFingerprint string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bg_replace_jobs SET quote_status='invalid', updated_at=?
		WHERE tenant_id=? AND project_id=? AND input_id=? AND fingerprint!=? AND job_status='quoted'`,
		Now().Format(time.RFC3339), tenantID, projectID, inputID, keepFingerprint)
	return err
}

const bgJobSelect = `SELECT id, tenant_id, project_id, input_id, input_version, mode, protected_region,
	background_intent, fingerprint, quote_status, billing_label, job_status, quality, evidence,
	platform_task_id, output_asset_id, output_version, selection, export_count, deliverable,
	pending_json, allowed_uses_json, model_ref, fee_ref, product_checks_json, origin, created_at, updated_at
	FROM bg_replace_jobs`

type bgScanner interface {
	Scan(dest ...any) error
}

func scanBgJob(sc bgScanner) (BgJob, error) {
	var job BgJob
	var deliverable int
	var pending, uses, created, updated string
	err := sc.Scan(&job.ID, &job.TenantID, &job.ProjectID, &job.InputID, &job.InputVersion, &job.Mode,
		&job.ProtectedRegion, &job.BackgroundIntent, &job.Fingerprint, &job.QuoteStatus, &job.BillingLabel,
		&job.JobStatus, &job.Quality, &job.Evidence, &job.PlatformTaskID, &job.OutputAssetID, &job.OutputVersion,
		&job.Selection, &job.ExportCount, &deliverable, &pending, &uses, &job.ModelRef, &job.FeeRef,
		&job.ProductChecksJSON, &job.Origin, &created, &updated)
	if err != nil {
		return BgJob{}, err
	}
	job.Deliverable = deliverable != 0
	_ = json.Unmarshal([]byte(pending), &job.Pending)
	_ = json.Unmarshal([]byte(uses), &job.AllowedUses)
	job.CreatedAt, _ = time.Parse(time.RFC3339, created)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if job.Pending == nil {
		job.Pending = []string{}
	}
	if job.AllowedUses == nil {
		job.AllowedUses = []string{}
	}
	return job, nil
}

// BgModelBytes 是一次已解码的背景模型结果。来源由存储固定，调用方不能写成供应商成功。
type BgModelBytes struct {
	TenantID  string
	ProjectID string
	JobID     string
	MediaType string
	Bytes     []byte
	Pending   []string
}

// SaveBgModelBytes 把解码字节写进背景结果。已完成的记录保持原字节。
// 失败记录不能改成完成。不写计费通过，deliverable 保持 0。
func (s *Store) SaveBgModelBytes(ctx context.Context, in BgModelBytes) (BgJob, bool, error) {
	switch in.MediaType {
	case "image/png", "image/jpeg":
	default:
		return BgJob{}, false, fmt.Errorf("%w: 图片媒体类型不受支持", ErrValidation)
	}
	if len(in.Bytes) == 0 {
		return BgJob{}, false, fmt.Errorf("%w: 图片字节为空", ErrValidation)
	}
	current, err := s.GetBgJob(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err != nil {
		return BgJob{}, false, err
	}
	if current.JobStatus == "failed" || current.JobStatus == "quota_insufficient" {
		return BgJob{}, false, fmt.Errorf("%w: 失败记录不能改成完成", ErrConflict)
	}
	if current.OutputAssetID != "" || current.JobStatus == "completed" {
		return current, true, nil
	}
	if current.JobStatus != "submitting" {
		return BgJob{}, false, fmt.Errorf("%w: 当前背景任务不能写入模型字节", ErrConflict)
	}
	sum := sha256.Sum256(in.Bytes)
	shaHex := hex.EncodeToString(sum[:])
	assetID := newID("bgimg")
	pending := in.Pending
	if pending == nil {
		pending = current.Pending
	}
	pendingJSON, err := json.Marshal(pending)
	if err != nil {
		return BgJob{}, false, err
	}
	now := Now().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BgJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO bg_result_blobs (
		asset_id, tenant_id, project_id, job_id, sha256, media_type, origin, content, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		assetID, current.TenantID, current.ProjectID, current.ID, shaHex, in.MediaType, bgResultOrigin, in.Bytes, now); err != nil {
		return BgJob{}, false, fmt.Errorf("store: 写入背景字节失败: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE bg_replace_jobs SET
		job_status=?, output_asset_id=?, output_version=?, deliverable=0, origin=?, pending_json=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=? AND job_status=? AND output_asset_id=''`,
		"completed", assetID, assetID, bgResultOrigin, string(pendingJSON), now,
		current.ID, current.TenantID, current.ProjectID, "submitting")
	if err != nil {
		return BgJob{}, false, fmt.Errorf("store: 写入背景结果失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return BgJob{}, false, fmt.Errorf("%w: 背景结果没有写上原任务", ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return BgJob{}, false, err
	}
	saved, err := s.GetBgJob(ctx, current.TenantID, current.ProjectID, current.ID)
	return saved, false, err
}

// OpenBgResult 在租户和字节哈希一致时返回已解码背景。不是可交付生产结果。
func (s *Store) OpenBgResult(ctx context.Context, tenantID, projectID, jobID string) (string, []byte, error) {
	job, err := s.GetBgJob(ctx, tenantID, projectID, jobID)
	if err != nil {
		return "", nil, err
	}
	if job.JobStatus != "completed" || job.OutputAssetID == "" || !openableBgOrigin(job.Origin) {
		return "", nil, ErrNotFound
	}
	var blobTenant, blobProject, blobJob, blobSHA, media, origin string
	var content []byte
	err = s.db.QueryRowContext(ctx, `SELECT tenant_id, project_id, job_id, sha256, media_type, origin, content
		FROM bg_result_blobs WHERE asset_id=? AND tenant_id=? AND project_id=?`,
		job.OutputAssetID, tenantID, projectID).Scan(&blobTenant, &blobProject, &blobJob, &blobSHA, &media, &origin, &content)
	if err == sql.ErrNoRows {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("store: 读取背景字节失败: %w", err)
	}
	sum := sha256.Sum256(content)
	fileSHA := hex.EncodeToString(sum[:])
	if blobTenant != tenantID || blobProject != projectID || blobJob != job.ID || origin != job.Origin || fileSHA != blobSHA {
		return "", nil, fmt.Errorf("%w: 文件字节与登记不一致", ErrConflict)
	}
	return media, content, nil
}

func openableBgOrigin(origin string) bool {
	return origin == bgResultOrigin || origin == bgSubjectLockOrigin
}

// bgSubjectLockOrigin 是保真合成字节。不是模型出图，也不是计费通过。
const bgSubjectLockOrigin = "subject_lock"

// SaveBgLockedBytes 把主体锁定后的 PNG 写进原任务。已锁定的记录保持原字节。
// 只接受尚未提交的报价或生成不可用记录，不写计费，deliverable 保持 0。
func (s *Store) SaveBgLockedBytes(ctx context.Context, in BgModelBytes) (BgJob, bool, error) {
	if in.MediaType != "image/png" {
		return BgJob{}, false, fmt.Errorf("%w: 图片媒体类型不受支持", ErrValidation)
	}
	if len(in.Bytes) == 0 {
		return BgJob{}, false, fmt.Errorf("%w: 图片字节为空", ErrValidation)
	}
	current, err := s.GetBgJob(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err != nil {
		return BgJob{}, false, err
	}
	if current.Origin == bgSubjectLockOrigin && current.OutputAssetID != "" {
		return current, true, nil
	}
	if current.JobStatus == "failed" || current.JobStatus == "quota_insufficient" {
		return BgJob{}, false, fmt.Errorf("%w: 失败记录不能改成完成", ErrConflict)
	}
	if current.JobStatus != "quoted" && current.JobStatus != "generation_unavailable" {
		return BgJob{}, false, fmt.Errorf("%w: 当前背景任务不能锁定", ErrConflict)
	}
	if current.OutputAssetID != "" || current.Origin != "" {
		return BgJob{}, false, fmt.Errorf("%w: 当前背景任务不能锁定", ErrConflict)
	}
	sum := sha256.Sum256(in.Bytes)
	shaHex := hex.EncodeToString(sum[:])
	assetID := newID("bgimg")
	pending := in.Pending
	if pending == nil {
		pending = current.Pending
	}
	pendingJSON, err := json.Marshal(pending)
	if err != nil {
		return BgJob{}, false, err
	}
	now := Now().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BgJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO bg_result_blobs (
		asset_id, tenant_id, project_id, job_id, sha256, media_type, origin, content, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		assetID, current.TenantID, current.ProjectID, current.ID, shaHex, in.MediaType, bgSubjectLockOrigin, in.Bytes, now); err != nil {
		return BgJob{}, false, fmt.Errorf("store: 写入背景字节失败: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE bg_replace_jobs SET
		job_status=?, output_asset_id=?, output_version=?, deliverable=0, origin=?, pending_json=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=? AND output_asset_id='' AND origin='' AND job_status IN ('quoted','generation_unavailable')`,
		"completed", assetID, assetID, bgSubjectLockOrigin, string(pendingJSON), now,
		current.ID, current.TenantID, current.ProjectID)
	if err != nil {
		return BgJob{}, false, fmt.Errorf("store: 写入背景结果失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return BgJob{}, false, fmt.Errorf("%w: 背景结果没有写上原任务", ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return BgJob{}, false, err
	}
	saved, err := s.GetBgJob(ctx, current.TenantID, current.ProjectID, current.ID)
	return saved, false, err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
