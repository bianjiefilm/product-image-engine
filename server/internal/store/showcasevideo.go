package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/showcasevideo"
)

// ShowcaseVideoJob 是一次展示视频请求。计费通过和生产授权不能由客户端写成真。
type ShowcaseVideoJob struct {
	ID                   string
	TenantID             string
	ProjectID            string
	ProductImageID       string
	ImageKind            string
	CameraMove           string
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
	Pending              []string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (s *Store) InsertShowcaseVideoJob(ctx context.Context, job ShowcaseVideoJob) (ShowcaseVideoJob, error) {
	if strings.TrimSpace(job.Fingerprint) == "" || strings.TrimSpace(job.ProjectID) == "" || strings.TrimSpace(job.ProductImageID) == "" {
		return ShowcaseVideoJob{}, fmt.Errorf("%w: 产品图、指纹与工程不能为空", ErrValidation)
	}
	now := Now()
	job.ID = newID("vid")
	job.CreatedAt, job.UpdatedAt = now, now
	job.Charged = false
	job.BillingPassed = false
	job.ProductionAuthorized = false
	if job.Pending == nil {
		job.Pending = []string{}
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return ShowcaseVideoJob{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO showcase_video_jobs (
		id, tenant_id, project_id, product_image_id, image_kind, camera_move, fingerprint, quote_status,
		billing_label, job_status, quality, platform_task_id, output_asset_id, charged, billing_passed,
		production_authorized, pending_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.TenantID, job.ProjectID, job.ProductImageID, job.ImageKind, job.CameraMove, job.Fingerprint,
		job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality, job.PlatformTaskID, job.OutputAssetID,
		0, 0, 0, string(pending), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if isUniqueErr(err) {
		return ShowcaseVideoJob{}, fmt.Errorf("%w: 同一展示视频请求已存在", ErrConflict)
	}
	if err != nil {
		return ShowcaseVideoJob{}, fmt.Errorf("store: 写入展示视频请求失败: %w", err)
	}
	return job, nil
}

func (s *Store) UpdateShowcaseVideoJob(ctx context.Context, job ShowcaseVideoJob) error {
	current, err := s.GetShowcaseVideoJob(ctx, job.TenantID, job.ProjectID, job.ID)
	if err != nil {
		return err
	}
	if current.JobStatus == showcasevideo.StatusFailed && job.JobStatus != showcasevideo.StatusFailed {
		return fmt.Errorf("%w: 失败记录不能改成成功", ErrConflict)
	}
	if job.JobStatus == "succeeded" || job.JobStatus == "completed" || job.JobStatus == "success" || job.Quality == "pass" {
		return fmt.Errorf("%w: 没有真实展示视频，不能写成成功", ErrConflict)
	}
	now := Now()
	if job.Pending == nil {
		job.Pending = []string{}
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE showcase_video_jobs SET
		quote_status=?, billing_label=?, job_status=?, quality=?, platform_task_id=?, output_asset_id=?,
		charged=0, billing_passed=0, production_authorized=0, pending_json=?, updated_at=?
		WHERE id=? AND tenant_id=? AND project_id=?`,
		job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality, job.PlatformTaskID, job.OutputAssetID,
		string(pending), now.Format(time.RFC3339), job.ID, job.TenantID, job.ProjectID)
	if err != nil {
		return fmt.Errorf("store: 更新展示视频请求失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetShowcaseVideoJob(ctx context.Context, tenantID, projectID, id string) (ShowcaseVideoJob, error) {
	row := s.db.QueryRowContext(ctx, showcaseVideoSelect+` WHERE id=? AND tenant_id=? AND project_id=?`, id, tenantID, projectID)
	job, err := scanShowcaseVideo(row)
	if err == sql.ErrNoRows {
		return ShowcaseVideoJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) FindShowcaseVideoByFingerprint(ctx context.Context, tenantID, fingerprint string) (ShowcaseVideoJob, error) {
	row := s.db.QueryRowContext(ctx, showcaseVideoSelect+` WHERE tenant_id=? AND fingerprint=?`, tenantID, fingerprint)
	job, err := scanShowcaseVideo(row)
	if err == sql.ErrNoRows {
		return ShowcaseVideoJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) ListShowcaseVideoJobs(ctx context.Context, tenantID, projectID string) ([]ShowcaseVideoJob, error) {
	rows, err := s.db.QueryContext(ctx, showcaseVideoSelect+` WHERE tenant_id=? AND project_id=? ORDER BY created_at DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShowcaseVideoJob
	for rows.Next() {
		job, err := scanShowcaseVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) ClaimShowcaseVideoSubmit(ctx context.Context, tenantID, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE showcase_video_jobs SET job_status=?, updated_at=?
		WHERE id=? AND tenant_id=? AND job_status=? AND quote_status=?`,
		showcasevideo.StatusSubmitting, Now().Format(time.RFC3339), id, tenantID, showcasevideo.StatusQuoted, showcasevideo.QuoteConfirmed)
	if err != nil {
		return false, fmt.Errorf("store: 抢占展示视频提交失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

const showcaseVideoSelect = `SELECT id, tenant_id, project_id, product_image_id, image_kind, camera_move, fingerprint,
	quote_status, billing_label, job_status, quality, platform_task_id, output_asset_id, charged, billing_passed,
	production_authorized, pending_json, created_at, updated_at FROM showcase_video_jobs`

func scanShowcaseVideo(sc bgScanner) (ShowcaseVideoJob, error) {
	var job ShowcaseVideoJob
	var charged, billingPassed, productionAuthorized int
	var pending, created, updated string
	err := sc.Scan(&job.ID, &job.TenantID, &job.ProjectID, &job.ProductImageID, &job.ImageKind, &job.CameraMove,
		&job.Fingerprint, &job.QuoteStatus, &job.BillingLabel, &job.JobStatus, &job.Quality, &job.PlatformTaskID,
		&job.OutputAssetID, &charged, &billingPassed, &productionAuthorized, &pending, &created, &updated)
	if err != nil {
		return ShowcaseVideoJob{}, err
	}
	job.Charged = false
	job.BillingPassed = false
	job.ProductionAuthorized = false
	_ = json.Unmarshal([]byte(pending), &job.Pending)
	if job.Pending == nil {
		job.Pending = []string{}
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339, created)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return job, nil
}
