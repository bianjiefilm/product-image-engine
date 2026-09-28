package store

import (
	"context"
	"database/sql"
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
		production_authorized, subject_protected, pending_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.TenantID, job.ProjectID, job.Prompt, job.PhotoAssetID, job.Fingerprint, job.QuoteStatus,
		job.BillingLabel, job.JobStatus, job.Quality, job.PlatformTaskID, job.OutputAssetID,
		0, 0, 0, 0, string(pending), now.Format(time.RFC3339), now.Format(time.RFC3339))
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
	production_authorized, subject_protected, pending_json, created_at, updated_at FROM text_image_jobs`

func scanTextImage(sc bgScanner) (TextImageJob, error) {
	var job TextImageJob
	var charged, billingPassed, productionAuthorized, subjectProtected int
	var pending, created, updated string
	err := sc.Scan(&job.ID, &job.TenantID, &job.ProjectID, &job.Prompt, &job.PhotoAssetID, &job.Fingerprint,
		&job.QuoteStatus, &job.BillingLabel, &job.JobStatus, &job.Quality, &job.PlatformTaskID, &job.OutputAssetID,
		&charged, &billingPassed, &productionAuthorized, &subjectProtected, &pending, &created, &updated)
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
