package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BgJob 是背景替换的一条逻辑任务。生产出图与计费通过与否不存放在本行。
type BgJob struct {
	ID               string
	TenantID         string
	ProjectID        string
	InputID          string
	InputVersion     string
	Mode             string
	ProtectedRegion  string
	BackgroundIntent string
	Fingerprint      string
	QuoteStatus      string
	BillingLabel     string
	JobStatus        string
	Quality          string
	Evidence         string
	PlatformTaskID   string
	OutputAssetID    string
	OutputVersion    string
	Selection        string
	ExportCount      int
	Deliverable      bool
	Pending          []string
	AllowedUses      []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

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
		created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.TenantID, job.ProjectID, job.InputID, job.InputVersion, job.Mode, job.ProtectedRegion,
		job.BackgroundIntent, job.Fingerprint, job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality,
		job.Evidence, job.PlatformTaskID, job.OutputAssetID, job.OutputVersion, job.Selection, job.ExportCount,
		boolInt(job.Deliverable), string(pending), string(uses), now.Format(time.RFC3339), now.Format(time.RFC3339))
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
		pending_json=?, allowed_uses_json=?, mode=?, updated_at=?
		WHERE id=? AND tenant_id=?`,
		job.QuoteStatus, job.BillingLabel, job.JobStatus, job.Quality, job.Evidence, job.PlatformTaskID,
		job.OutputAssetID, job.OutputVersion, job.Selection, job.ExportCount, boolInt(job.Deliverable),
		string(pending), string(uses), job.Mode, now.Format(time.RFC3339), job.ID, job.TenantID)
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

// InvalidateOpenBgQuotes 使尚未提交的其他报价失效。
func (s *Store) InvalidateOpenBgQuotes(ctx context.Context, tenantID, projectID, inputID, keepFingerprint string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bg_replace_jobs SET quote_status='invalid', updated_at=?
		WHERE tenant_id=? AND project_id=? AND input_id=? AND fingerprint!=? AND job_status='quoted'`,
		Now().Format(time.RFC3339), tenantID, projectID, inputID, keepFingerprint)
	return err
}

const bgJobSelect = `SELECT id, tenant_id, project_id, input_id, input_version, mode, protected_region,
	background_intent, fingerprint, quote_status, billing_label, job_status, quality, evidence,
	platform_task_id, output_asset_id, output_version, selection, export_count, deliverable,
	pending_json, allowed_uses_json, created_at, updated_at FROM bg_replace_jobs`

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
		&job.Selection, &job.ExportCount, &deliverable, &pending, &uses, &created, &updated)
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

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
