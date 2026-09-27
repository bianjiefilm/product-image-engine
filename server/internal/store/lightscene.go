package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LightJob 是光影场景的一条逻辑任务。已验证产品图与计费通过与否不由客户端写入。
type LightJob struct {
	ID              string
	TenantID        string
	ProjectID       string
	InputID         string
	InputVersion    string
	Mode            string
	ProtectedRegion string
	LightingIntent  string
	Fingerprint     string
	QuoteStatus     string
	BillingLabel    string
	JobStatus       string
	Quality         string
	Evidence        string
	PlatformTaskID  string
	OutputAssetID   string
	OutputVersion   string
	VendorReceiptID string
	Selection       string
	ExportCount     int
	Deliverable     bool
	VerifiedProduct bool
	Pending         []string
	AllowedUses     []string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Store) InsertLightJob(ctx context.Context, job LightJob) (LightJob, error) {
	if strings.TrimSpace(job.Fingerprint) == "" || strings.TrimSpace(job.ProjectID) == "" {
		return LightJob{}, fmt.Errorf("%w: 光影场景指纹与工程不能为空", ErrValidation)
	}
	now := Now()
	job.ID = newID("ltj")
	job.CreatedAt, job.UpdatedAt = now, now
	if job.Pending == nil {
		job.Pending = []string{}
	}
	if job.AllowedUses == nil {
		job.AllowedUses = []string{}
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return LightJob{}, err
	}
	uses, err := json.Marshal(job.AllowedUses)
	if err != nil {
		return LightJob{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO light_scene_jobs (
		id, tenant_id, project_id, input_id, input_version, mode, protected_region, lighting_intent,
		fingerprint, quote_status, billing_label, job_status, quality, evidence, platform_task_id,
		output_asset_id, output_version, vendor_receipt_id, selection, export_count, deliverable,
		verified_product, pending_json, allowed_uses_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.TenantID, job.ProjectID, job.InputID, job.InputVersion, job.Mode, job.ProtectedRegion,
		job.LightingIntent, job.Fingerprint, job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality,
		job.Evidence, job.PlatformTaskID, job.OutputAssetID, job.OutputVersion, job.VendorReceiptID,
		job.Selection, job.ExportCount, boolInt(job.Deliverable), boolInt(job.VerifiedProduct),
		string(pending), string(uses), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return LightJob{}, fmt.Errorf("store: 写入光影场景失败: %w", err)
	}
	return job, nil
}

func (s *Store) UpdateLightJob(ctx context.Context, job LightJob) error {
	now := Now()
	job.UpdatedAt = now
	if job.Pending == nil {
		job.Pending = []string{}
	}
	if job.AllowedUses == nil {
		job.AllowedUses = []string{}
	}
	pending, err := json.Marshal(job.Pending)
	if err != nil {
		return err
	}
	uses, err := json.Marshal(job.AllowedUses)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE light_scene_jobs SET
		quote_status=?, billing_label=?, job_status=?, quality=?, evidence=?, platform_task_id=?,
		output_asset_id=?, output_version=?, vendor_receipt_id=?, selection=?, export_count=?,
		deliverable=?, verified_product=?, pending_json=?, allowed_uses_json=?, mode=?, updated_at=?
		WHERE id=? AND tenant_id=?`,
		job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality, job.Evidence, job.PlatformTaskID,
		job.OutputAssetID, job.OutputVersion, job.VendorReceiptID, job.Selection, job.ExportCount,
		boolInt(job.Deliverable), boolInt(job.VerifiedProduct), string(pending), string(uses),
		job.Mode, now.Format(time.RFC3339), job.ID, job.TenantID)
	if err != nil {
		return fmt.Errorf("store: 更新光影场景失败: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetLightJob(ctx context.Context, tenantID, projectID, id string) (LightJob, error) {
	row := s.db.QueryRowContext(ctx, lightJobSelect+` WHERE id=? AND tenant_id=? AND project_id=?`, id, tenantID, projectID)
	job, err := scanLightJob(row)
	if err == sql.ErrNoRows {
		return LightJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) FindLightJobByFingerprint(ctx context.Context, tenantID, fingerprint string) (LightJob, error) {
	row := s.db.QueryRowContext(ctx, lightJobSelect+` WHERE tenant_id=? AND fingerprint=?`, tenantID, fingerprint)
	job, err := scanLightJob(row)
	if err == sql.ErrNoRows {
		return LightJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) ListLightJobs(ctx context.Context, tenantID, projectID string) ([]LightJob, error) {
	rows, err := s.db.QueryContext(ctx, lightJobSelect+` WHERE tenant_id=? AND project_id=? ORDER BY created_at DESC`, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LightJob
	for rows.Next() {
		job, err := scanLightJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// LatestLightJobForInput 返回该输入最近一条光影任务,没有则为 ErrNotFound。
func (s *Store) LatestLightJobForInput(ctx context.Context, tenantID, projectID, inputID string) (LightJob, error) {
	row := s.db.QueryRowContext(ctx, lightJobSelect+` WHERE tenant_id=? AND project_id=? AND input_id=? ORDER BY created_at DESC LIMIT 1`,
		tenantID, projectID, inputID)
	job, err := scanLightJob(row)
	if err == sql.ErrNoRows {
		return LightJob{}, ErrNotFound
	}
	return job, err
}

// ClaimLightSubmit 把已确认且仍为 quoted 的任务抢成 submitting。
func (s *Store) ClaimLightSubmit(ctx context.Context, tenantID, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE light_scene_jobs SET job_status=?, updated_at=?
		WHERE id=? AND tenant_id=? AND job_status=? AND quote_status=?`,
		"submitting", Now().Format(time.RFC3339), id, tenantID, "quoted", "confirmed")
	if err != nil {
		return false, fmt.Errorf("store: 抢占光影场景提交失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) InvalidateOpenLightQuotes(ctx context.Context, tenantID, projectID, inputID, keepFingerprint string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE light_scene_jobs SET quote_status='invalid', updated_at=?
		WHERE tenant_id=? AND project_id=? AND input_id=? AND fingerprint!=? AND job_status='quoted'`,
		Now().Format(time.RFC3339), tenantID, projectID, inputID, keepFingerprint)
	return err
}

const lightJobSelect = `SELECT id, tenant_id, project_id, input_id, input_version, mode, protected_region,
	lighting_intent, fingerprint, quote_status, billing_label, job_status, quality, evidence,
	platform_task_id, output_asset_id, output_version, vendor_receipt_id, selection, export_count,
	deliverable, verified_product, pending_json, allowed_uses_json, created_at, updated_at FROM light_scene_jobs`

func scanLightJob(sc bgScanner) (LightJob, error) {
	var job LightJob
	var deliverable, verified int
	var pending, uses, created, updated string
	err := sc.Scan(&job.ID, &job.TenantID, &job.ProjectID, &job.InputID, &job.InputVersion, &job.Mode,
		&job.ProtectedRegion, &job.LightingIntent, &job.Fingerprint, &job.QuoteStatus, &job.BillingLabel,
		&job.JobStatus, &job.Quality, &job.Evidence, &job.PlatformTaskID, &job.OutputAssetID, &job.OutputVersion,
		&job.VendorReceiptID, &job.Selection, &job.ExportCount, &deliverable, &verified, &pending, &uses, &created, &updated)
	if err != nil {
		return LightJob{}, err
	}
	job.Deliverable = deliverable != 0
	job.VerifiedProduct = verified != 0
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
