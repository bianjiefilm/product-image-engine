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

	"github.com/bianjiefilm/product-image-engine/server/internal/textimage"
)

// TextImageJob 是一次文字描述请求。计费通过、生产授权和主体保护不能由客户端写成真。
type TextImageJob struct {
	ID                   string
	TenantID             string
	ProjectID            string
	Prompt               string
	PhotoAssetID         string
	Fingerprint          string
	QuoteStatus          string
	BillingLabel         string
	JobStatus            string
	Quality              string
	PlatformTaskID       string
	OutputAssetID        string
	InputVersion         string
	OutputSHA256         string
	ResultVersion        string
	SelectedVersion      string
	Origin               string
	Charged              bool
	BillingPassed        bool
	ProductionAuthorized bool
	SubjectProtected     bool
	Pending              []string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (s *Store) InsertTextImageJob(ctx context.Context, job TextImageJob) (TextImageJob, error) {
	if strings.TrimSpace(job.Fingerprint) == "" || strings.TrimSpace(job.ProjectID) == "" || strings.TrimSpace(job.Prompt) == "" {
		return TextImageJob{}, fmt.Errorf("%w: 文字描述、指纹与工程不能为空", ErrValidation)
	}
	now := Now()
	job.ID = newID("txt")
	job.CreatedAt, job.UpdatedAt = now, now
	job.Charged = false
	job.BillingPassed = false
	job.ProductionAuthorized = false
	job.SubjectProtected = false
	if strings.TrimSpace(job.InputVersion) == "" {
		job.InputVersion = job.Fingerprint
	}
	if job.Pending == nil {
		job.Pending = []string{}
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return TextImageJob{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO text_image_jobs (
		id, tenant_id, project_id, prompt, photo_asset_id, fingerprint, quote_status, billing_label,
		job_status, quality, platform_task_id, output_asset_id, charged, billing_passed,
		production_authorized, subject_protected, pending_json, created_at, updated_at, input_version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.TenantID, job.ProjectID, job.Prompt, job.PhotoAssetID, job.Fingerprint, job.QuoteStatus,
		job.BillingLabel, job.JobStatus, job.Quality, job.PlatformTaskID, job.OutputAssetID,
		0, 0, 0, 0, string(pending), now.Format(time.RFC3339), now.Format(time.RFC3339), job.InputVersion)
	if isUniqueErr(err) {
		return TextImageJob{}, fmt.Errorf("%w: 同一文字请求已存在", ErrConflict)
	}
	if err != nil {
		return TextImageJob{}, fmt.Errorf("store: 写入文字请求失败: %w", err)
	}
	return job, nil
}

func (s *Store) UpdateTextImageJob(ctx context.Context, job TextImageJob) error {
	current, err := s.GetTextImageJob(ctx, job.TenantID, job.ProjectID, job.ID)
	if err != nil {
		return err
	}
	if current.JobStatus == textimage.StatusFailed && job.JobStatus != textimage.StatusFailed {
		return fmt.Errorf("%w: 失败记录不能改成成功", ErrConflict)
	}
	if job.JobStatus == "succeeded" || job.JobStatus == "completed" || job.JobStatus == "success" || job.Quality == "pass" {
		return fmt.Errorf("%w: 没有真实生成图，不能写成成功", ErrConflict)
	}
	now := Now()
	if job.Pending == nil {
		job.Pending = []string{}
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE text_image_jobs SET
		quote_status=?, billing_label=?, job_status=?, quality=?, platform_task_id=?, output_asset_id=?,
		charged=0, billing_passed=0, production_authorized=0, subject_protected=0, pending_json=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=?`,
		job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality, job.PlatformTaskID, job.OutputAssetID,
		string(pending), now.Format(time.RFC3339), job.ID, job.TenantID, job.ProjectID)
	if err != nil {
		return fmt.Errorf("store: 更新文字请求失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetTextImageJob(ctx context.Context, tenantID, projectID, id string) (TextImageJob, error) {
	row := s.db.QueryRowContext(ctx, textImageSelect+` WHERE id=? AND tenant_id=? AND project_id=?`, id, tenantID, projectID)
	job, err := scanTextImage(row)
	if err == sql.ErrNoRows {
		return TextImageJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) FindTextImageByFingerprint(ctx context.Context, tenantID, fingerprint string) (TextImageJob, error) {
	row := s.db.QueryRowContext(ctx, textImageSelect+` WHERE tenant_id=? AND fingerprint=?`, tenantID, fingerprint)
	job, err := scanTextImage(row)
	if err == sql.ErrNoRows {
		return TextImageJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) ListTextImageJobs(ctx context.Context, tenantID, projectID string) ([]TextImageJob, error) {
	rows, err := s.db.QueryContext(ctx, textImageSelect+` WHERE tenant_id=? AND project_id=? ORDER BY created_at DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TextImageJob
	for rows.Next() {
		job, err := scanTextImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) ClaimTextImageSubmit(ctx context.Context, tenantID, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE text_image_jobs SET job_status=?, updated_at=?
		WHERE id=? AND tenant_id=? AND job_status=? AND quote_status=?`,
		textimage.StatusSubmitting, Now().Format(time.RFC3339), id, tenantID, textimage.StatusQuoted, textimage.QuoteConfirmed)
	if err != nil {
		return false, fmt.Errorf("store: 抢占文字请求提交失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

const textImageSelect = `SELECT id, tenant_id, project_id, prompt, photo_asset_id, fingerprint, quote_status,
	billing_label, job_status, quality, platform_task_id, output_asset_id, charged, billing_passed,
	production_authorized, subject_protected, pending_json, created_at, updated_at,
	input_version, output_sha256, result_version, selected_version, origin FROM text_image_jobs`

func scanTextImage(sc bgScanner) (TextImageJob, error) {
	var job TextImageJob
	var charged, billingPassed, productionAuthorized, subjectProtected int
	var pending, created, updated string
	err := sc.Scan(&job.ID, &job.TenantID, &job.ProjectID, &job.Prompt, &job.PhotoAssetID, &job.Fingerprint,
		&job.QuoteStatus, &job.BillingLabel, &job.JobStatus, &job.Quality, &job.PlatformTaskID, &job.OutputAssetID,
		&charged, &billingPassed, &productionAuthorized, &subjectProtected, &pending, &created, &updated,
		&job.InputVersion, &job.OutputSHA256, &job.ResultVersion, &job.SelectedVersion, &job.Origin)
	if err != nil {
		return TextImageJob{}, err
	}
	job.Charged = false
	job.BillingPassed = false
	job.ProductionAuthorized = false
	job.SubjectProtected = false
	_ = json.Unmarshal([]byte(pending), &job.Pending)
	if job.Pending == nil {
		job.Pending = []string{}
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339, created)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return job, nil
}

// TextImageFixture 是服务端自己登记的夹具字节。客户端资产编号不在这里。
type TextImageFixture struct {
	TenantID     string
	ProjectID    string
	JobID        string
	FixtureLabel string
	SHA256       string
	MediaType    string
	Bytes        []byte
}

// RegisterTextImageFixture 写入夹具并标记完成。已有资产时返回原任务，不覆盖字节。
// 失败或待确认不能借此改成完成。
func (s *Store) RegisterTextImageFixture(ctx context.Context, in TextImageFixture) (TextImageJob, bool, error) {
	if !strings.Contains(in.FixtureLabel, "fixture") {
		return TextImageJob{}, false, fmt.Errorf("%w: 夹具数据必须标明 fixture", ErrValidation)
	}
	switch in.MediaType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return TextImageJob{}, false, fmt.Errorf("%w: 夹具媒体类型不受支持", ErrValidation)
	}
	if len(in.Bytes) == 0 {
		return TextImageJob{}, false, fmt.Errorf("%w: 夹具字节为空", ErrValidation)
	}
	sum := sha256.Sum256(in.Bytes)
	gotSHA := hex.EncodeToString(sum[:])
	if gotSHA != strings.ToLower(strings.TrimSpace(in.SHA256)) {
		return TextImageJob{}, false, fmt.Errorf("%w: 夹具字节与申报哈希不一致", ErrValidation)
	}
	current, err := s.GetTextImageJob(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err != nil {
		return TextImageJob{}, false, err
	}
	if current.JobStatus == textimage.StatusFailed || current.JobStatus == textimage.StatusUnknown {
		return TextImageJob{}, false, fmt.Errorf("%w: 失败或待确认记录不能改成夹具完成", ErrConflict)
	}
	if current.OutputAssetID != "" {
		return current, true, nil
	}
	if current.JobStatus != textimage.StatusQuoted || strings.TrimSpace(current.InputVersion) == "" {
		return TextImageJob{}, false, fmt.Errorf("%w: 当前文字请求不能登记夹具", ErrConflict)
	}
	assetID := newID("txfix")
	now := Now().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TextImageJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO text_image_blobs (
		asset_id, tenant_id, project_id, job_id, input_version, sha256, media_type, origin, content, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		assetID, current.TenantID, current.ProjectID, current.ID, current.InputVersion, gotSHA,
		in.MediaType, textimage.OriginFixture, in.Bytes, now); err != nil {
		return TextImageJob{}, false, fmt.Errorf("store: 写入夹具字节失败: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE text_image_jobs SET
		job_status=?, output_asset_id=?, output_sha256=?, result_version=?, origin=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=? AND job_status=? AND output_asset_id=''`,
		textimage.StatusCompleted, assetID, gotSHA, assetID, textimage.OriginFixture, now,
		current.ID, current.TenantID, current.ProjectID, textimage.StatusQuoted)
	if err != nil {
		return TextImageJob{}, false, fmt.Errorf("store: 登记夹具失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return TextImageJob{}, false, fmt.Errorf("%w: 夹具登记没有写上原任务", ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return TextImageJob{}, false, err
	}
	saved, err := s.GetTextImageJob(ctx, current.TenantID, current.ProjectID, current.ID)
	return saved, false, err
}

// SelectTextImageVersion 把当前夹具版本记为用户选定。已选定则保持，不改资产。
func (s *Store) SelectTextImageVersion(ctx context.Context, tenantID, projectID, jobID string) (TextImageJob, error) {
	current, err := s.GetTextImageJob(ctx, tenantID, projectID, jobID)
	if err != nil {
		return TextImageJob{}, err
	}
	if current.JobStatus != textimage.StatusCompleted || !trustedTextOrigin(current.Origin) || current.ResultVersion == "" || current.OutputAssetID == "" {
		return TextImageJob{}, fmt.Errorf("%w: 没有可选定的版本", ErrConflict)
	}
	if current.SelectedVersion == current.ResultVersion {
		return current, nil
	}
	if current.SelectedVersion != "" {
		return TextImageJob{}, fmt.Errorf("%w: 晚到结果不能覆盖已选定版本", ErrConflict)
	}
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE text_image_jobs SET selected_version=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=? AND selected_version='' AND result_version=?`,
		current.ResultVersion, now, current.ID, current.TenantID, current.ProjectID, current.ResultVersion)
	if err != nil {
		return TextImageJob{}, fmt.Errorf("store: 选定文字版本失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return TextImageJob{}, fmt.Errorf("%w: 选定版本没有写上", ErrConflict)
	}
	return s.GetTextImageJob(ctx, tenantID, projectID, jobID)
}

// OpenTextImageDownload 在租户、版本和字节哈希都一致时返回夹具字节。
func (s *Store) OpenTextImageDownload(ctx context.Context, tenantID, projectID, jobID string) (string, []byte, error) {
	job, err := s.GetTextImageJob(ctx, tenantID, projectID, jobID)
	if err != nil {
		return "", nil, err
	}
	if job.OutputAssetID == "" || !trustedTextOrigin(job.Origin) || job.JobStatus != textimage.StatusCompleted {
		return "", nil, ErrNotFound
	}
	if job.SelectedVersion != "" && job.SelectedVersion != job.ResultVersion {
		return "", nil, fmt.Errorf("%w: 晚到结果不能覆盖已选定版本", ErrConflict)
	}
	var blobTenant, blobProject, blobJob, blobVersion, blobSHA, media, origin string
	var content []byte
	err = s.db.QueryRowContext(ctx, `SELECT tenant_id, project_id, job_id, input_version, sha256, media_type, origin, content
		FROM text_image_blobs WHERE asset_id=? AND tenant_id=? AND project_id=?`,
		job.OutputAssetID, tenantID, projectID).Scan(&blobTenant, &blobProject, &blobJob, &blobVersion, &blobSHA, &media, &origin, &content)
	if err == sql.ErrNoRows {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("store: 读取夹具字节失败: %w", err)
	}
	sum := sha256.Sum256(content)
	fileSHA := hex.EncodeToString(sum[:])
	if blobTenant != tenantID || blobProject != projectID || blobJob != job.ID || origin != job.Origin || !trustedTextOrigin(origin) ||
		blobVersion != job.InputVersion || fileSHA != job.OutputSHA256 || fileSHA != blobSHA {
		return "", nil, fmt.Errorf("%w: 文件字节与登记不一致", ErrConflict)
	}
	return media, content, nil
}

func trustedTextOrigin(origin string) bool {
	return origin == textimage.OriginFixture || origin == textimage.OriginServer
}

// TextImageServerResult 是服务端自己取得并保存的任务字节。资产编号不来自客户端。
type TextImageServerResult struct {
	TenantID  string
	ProjectID string
	JobID     string
	AssetID   string
	MediaType string
	Bytes     []byte
}

// CompleteTextImageServer 把服务端字节写成完成。已有资产或已选版本时返回原任务。
// 失败、报价和通用更新都不能走到这里。不写计费通过。
func (s *Store) CompleteTextImageServer(ctx context.Context, in TextImageServerResult) (TextImageJob, bool, error) {
	assetID := strings.TrimSpace(in.AssetID)
	if assetID == "" {
		return TextImageJob{}, false, fmt.Errorf("%w: 服务端资产编号不能为空", ErrValidation)
	}
	switch in.MediaType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return TextImageJob{}, false, fmt.Errorf("%w: 图片媒体类型不受支持", ErrValidation)
	}
	if len(in.Bytes) == 0 {
		return TextImageJob{}, false, fmt.Errorf("%w: 服务端图片字节为空", ErrValidation)
	}
	sum := sha256.Sum256(in.Bytes)
	gotSHA := hex.EncodeToString(sum[:])
	current, err := s.GetTextImageJob(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err != nil {
		return TextImageJob{}, false, err
	}
	if current.JobStatus == textimage.StatusFailed {
		return TextImageJob{}, false, fmt.Errorf("%w: 失败记录不能改成完成", ErrConflict)
	}
	if current.OutputAssetID != "" || current.SelectedVersion != "" {
		return current, true, nil
	}
	if !openTextTaskStatus(current.JobStatus) || strings.TrimSpace(current.InputVersion) == "" {
		return TextImageJob{}, false, fmt.Errorf("%w: 当前文字请求不能写入服务端结果", ErrConflict)
	}
	now := Now().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TextImageJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO text_image_blobs (
		asset_id, tenant_id, project_id, job_id, input_version, sha256, media_type, origin, content, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		assetID, current.TenantID, current.ProjectID, current.ID, current.InputVersion, gotSHA,
		in.MediaType, textimage.OriginServer, in.Bytes, now); err != nil {
		return TextImageJob{}, false, fmt.Errorf("store: 写入服务端图片失败: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE text_image_jobs SET
		job_status=?, output_asset_id=?, output_sha256=?, result_version=?, origin=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=? AND output_asset_id='' AND selected_version=''
		AND job_status IN (?,?,?)`,
		textimage.StatusCompleted, assetID, gotSHA, assetID, textimage.OriginServer, now,
		current.ID, current.TenantID, current.ProjectID,
		textimage.StatusUnknown, textimage.StatusSubmitting, textimage.StatusQueued)
	if err != nil {
		return TextImageJob{}, false, fmt.Errorf("store: 写入服务端任务完成失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return TextImageJob{}, false, fmt.Errorf("%w: 服务端结果没有写上原任务", ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return TextImageJob{}, false, err
	}
	saved, err := s.GetTextImageJob(ctx, current.TenantID, current.ProjectID, current.ID)
	return saved, false, err
}

func openTextTaskStatus(status string) bool {
	switch status {
	case textimage.StatusUnknown, textimage.StatusSubmitting, textimage.StatusQueued:
		return true
	default:
		return false
	}
}
