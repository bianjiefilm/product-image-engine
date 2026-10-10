package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CampaignSelection 活动工程上用户明确选定的输出版本。
// 登记成果、打开页面、下载文件都不写这张表。
type CampaignSelection struct {
	ID             string    `json:"id"`
	TenantScope    string    `json:"tenant_scope"`
	ProjectID      string    `json:"project_id"`
	OutputID       string    `json:"output_id"`
	SnapshotID     string    `json:"snapshot_id"`
	CampaignRef    string    `json:"campaign_ref"`
	BriefVersion   string    `json:"brief_version"`
	SourceRevision string    `json:"source_revision"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const campaignSelCols = `id, tenant_scope, project_id, output_id, snapshot_id, campaign_ref, brief_version, source_revision, created_at, updated_at`

func scanCampaignSelection(row interface{ Scan(...any) error }) (CampaignSelection, error) {
	var v CampaignSelection
	var created, updated string
	if err := row.Scan(&v.ID, &v.TenantScope, &v.ProjectID, &v.OutputID, &v.SnapshotID, &v.CampaignRef,
		&v.BriefVersion, &v.SourceRevision, &created, &updated); err != nil {
		return CampaignSelection{}, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339, created)
	v.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return v, nil
}

// GetCampaignSelection 按 (租户, 工程) 读取选定版本。跨租户与未选定同为 ErrNotFound。
func (s *Store) GetCampaignSelection(ctx context.Context, tenantScope, projectID string) (CampaignSelection, error) {
	v, err := scanCampaignSelection(s.db.QueryRowContext(ctx,
		`SELECT `+campaignSelCols+` FROM campaign_selected_versions WHERE tenant_scope = ? AND project_id = ?`,
		tenantScope, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return CampaignSelection{}, ErrNotFound
	}
	if err != nil {
		return CampaignSelection{}, fmt.Errorf("store: 查询活动选定版本失败: %w", err)
	}
	return v, nil
}

// PutCampaignSelection 写入或移动选定指针。
// 同一输出且快照、活动引用、需求版本、来源修订都相同 → same=true，不改 updated_at。
// 换输出只移动指针，不改写已有回执。
func (s *Store) PutCampaignSelection(ctx context.Context, sel CampaignSelection) (CampaignSelection, bool, error) {
	if sel.TenantScope == "" || sel.ProjectID == "" || sel.OutputID == "" || sel.SnapshotID == "" ||
		sel.CampaignRef == "" || sel.BriefVersion == "" || sel.SourceRevision == "" {
		return CampaignSelection{}, false, fmt.Errorf("%w: 活动选定版本字段不完整", ErrValidation)
	}
	existing, err := s.GetCampaignSelection(ctx, sel.TenantScope, sel.ProjectID)
	if err == nil {
		if existing.OutputID == sel.OutputID && existing.SnapshotID == sel.SnapshotID &&
			existing.CampaignRef == sel.CampaignRef && existing.BriefVersion == sel.BriefVersion &&
			existing.SourceRevision == sel.SourceRevision {
			return existing, true, nil
		}
		now := Now().Format(time.RFC3339)
		res, uerr := s.db.ExecContext(ctx, `UPDATE campaign_selected_versions
			SET output_id = ?, snapshot_id = ?, campaign_ref = ?, brief_version = ?, source_revision = ?, updated_at = ?
			WHERE tenant_scope = ? AND project_id = ?`,
			sel.OutputID, sel.SnapshotID, sel.CampaignRef, sel.BriefVersion, sel.SourceRevision, now,
			sel.TenantScope, sel.ProjectID)
		if uerr != nil {
			return CampaignSelection{}, false, fmt.Errorf("store: 更新活动选定版本失败: %w", uerr)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return CampaignSelection{}, false, ErrNotFound
		}
		got, gerr := s.GetCampaignSelection(ctx, sel.TenantScope, sel.ProjectID)
		return got, false, gerr
	}
	if !errors.Is(err, ErrNotFound) {
		return CampaignSelection{}, false, err
	}
	sel.ID = newID("csel")
	now := Now()
	sel.CreatedAt, sel.UpdatedAt = now, now
	_, err = s.db.ExecContext(ctx, `INSERT INTO campaign_selected_versions
		(id, tenant_scope, project_id, output_id, snapshot_id, campaign_ref, brief_version, source_revision, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		sel.ID, sel.TenantScope, sel.ProjectID, sel.OutputID, sel.SnapshotID, sel.CampaignRef,
		sel.BriefVersion, sel.SourceRevision, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return CampaignSelection{}, false, fmt.Errorf("store: 写入活动选定版本失败: %w", err)
	}
	got, gerr := s.GetCampaignSelection(ctx, sel.TenantScope, sel.ProjectID)
	return got, false, gerr
}
