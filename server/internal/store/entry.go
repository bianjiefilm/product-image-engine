package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EntrySession 是一次「开始做产品图」的可恢复任务。
// 没有真实输出时 output_asset_id 保持空，不能靠它制造完成感。
type EntrySession struct {
	ID               string    `json:"id"`
	AccountID        string    `json:"account_id"`
	WorkspaceKind    string    `json:"workspace_kind"`
	WorkspaceScope   string    `json:"workspace_scope"`
	StorageTenant    string    `json:"storage_tenant"`
	ProjectID        string    `json:"project_id"`
	SourceType       string    `json:"source_type"`
	SourceRef        string    `json:"source_ref"`
	ReturnHref       string    `json:"return_href"`
	Fingerprint      string    `json:"fingerprint"`
	Status           string    `json:"status"`
	PlatformTaskID   string    `json:"platform_task_id"`
	OutputAssetID    string    `json:"output_asset_id"`
	QuoteLabel       string    `json:"quote_label"`
	Charged          bool      `json:"charged"`
	DegradedReason   string    `json:"degraded_reason"`
	StepCount        int       `json:"step_count"`
	InputCount       int       `json:"input_count"`
	ReuploadCount    int       `json:"reupload_count"`
	RecoveryCount    int       `json:"recovery_count"`
	AuthorizedAssets []string  `json:"authorized_assets"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type EntryPatch struct {
	Status         *string
	PlatformTaskID *string
	OutputAssetID  *string
	QuoteLabel     *string
	Charged        *bool
	DegradedReason *string
	StepCount      *int
	InputCount     *int
	ReuploadCount  *int
	RecoveryCount  *int
}

func (s *Store) InsertEntrySession(ctx context.Context, in EntrySession) (EntrySession, error) {
	if strings.TrimSpace(in.AccountID) == "" || strings.TrimSpace(in.StorageTenant) == "" || strings.TrimSpace(in.Fingerprint) == "" || strings.TrimSpace(in.ProjectID) == "" {
		return EntrySession{}, fmt.Errorf("%w: 入口会话缺少账号、租户、工程或指纹", ErrValidation)
	}
	in.ID = newID("entry")
	now := Now()
	in.CreatedAt, in.UpdatedAt = now, now
	if in.QuoteLabel == "" {
		in.QuoteLabel = "待确认"
	}
	if in.AuthorizedAssets == nil {
		in.AuthorizedAssets = []string{}
	}
	rawAssets, err := json.Marshal(in.AuthorizedAssets)
	if err != nil {
		return EntrySession{}, fmt.Errorf("store: 序列化授权素材失败: %w", err)
	}
	charged := 0
	if in.Charged {
		charged = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO entry_sessions (
		id, account_id, workspace_kind, workspace_scope, storage_tenant, project_id,
		source_type, source_ref, return_href, fingerprint, status, platform_task_id,
		output_asset_id, quote_label, charged, degraded_reason, step_count, input_count,
		reupload_count, recovery_count, authorized_assets, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.ID, in.AccountID, in.WorkspaceKind, in.WorkspaceScope, in.StorageTenant, in.ProjectID,
		in.SourceType, in.SourceRef, in.ReturnHref, in.Fingerprint, in.Status, in.PlatformTaskID,
		in.OutputAssetID, in.QuoteLabel, charged, in.DegradedReason, in.StepCount, in.InputCount,
		in.ReuploadCount, in.RecoveryCount, string(rawAssets),
		in.CreatedAt.Format(time.RFC3339), in.UpdatedAt.Format(time.RFC3339))
	if isUniqueErr(err) {
		return EntrySession{}, fmt.Errorf("%w: 同一任务已存在", ErrConflict)
	}
	if err != nil {
		return EntrySession{}, fmt.Errorf("store: 写入入口会话失败: %w", err)
	}
	return in, nil
}

func (s *Store) ListEntrySessions(ctx context.Context, accountID, workspaceScope string) ([]EntrySession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryCols+`
		FROM entry_sessions WHERE account_id = ? AND workspace_scope = ? ORDER BY updated_at DESC`,
		accountID, workspaceScope)
	if err != nil {
		return nil, fmt.Errorf("store: 列出入口会话失败: %w", err)
	}
	defer rows.Close()
	var out []EntrySession
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetEntryByFingerprint(ctx context.Context, storageTenant, fingerprint string) (EntrySession, error) {
	item, err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryCols+`
		FROM entry_sessions WHERE storage_tenant = ? AND fingerprint = ?`, storageTenant, fingerprint))
	if errors.Is(err, sql.ErrNoRows) {
		return EntrySession{}, ErrNotFound
	}
	if err != nil {
		return EntrySession{}, fmt.Errorf("store: 查询入口会话失败: %w", err)
	}
	return item, nil
}

func (s *Store) LatestEntryForProject(ctx context.Context, storageTenant, projectID string) (EntrySession, error) {
	item, err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryCols+`
		FROM entry_sessions WHERE storage_tenant = ? AND project_id = ? ORDER BY rowid DESC LIMIT 1`,
		storageTenant, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return EntrySession{}, ErrNotFound
	}
	if err != nil {
		return EntrySession{}, fmt.Errorf("store: 查询工程入口会话失败: %w", err)
	}
	return item, nil
}

func (s *Store) FindProjectBySource(ctx context.Context, tenantID, sourceType, sourceRef string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+`
		FROM projects WHERE tenant_id = ? AND source_type = ? AND source_ref = ? ORDER BY updated_at DESC LIMIT 1`,
		tenantID, sourceType, sourceRef))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("store: 按来源查询工程失败: %w", err)
	}
	return p, nil
}

func (s *Store) UpdateEntrySession(ctx context.Context, storageTenant, id string, patch EntryPatch) (EntrySession, error) {
	current, err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryCols+`
		FROM entry_sessions WHERE id = ? AND storage_tenant = ?`, id, storageTenant))
	if errors.Is(err, sql.ErrNoRows) {
		return EntrySession{}, ErrNotFound
	}
	if err != nil {
		return EntrySession{}, fmt.Errorf("store: 查询入口会话失败: %w", err)
	}
	if patch.Status != nil {
		current.Status = *patch.Status
	}
	if patch.PlatformTaskID != nil {
		current.PlatformTaskID = *patch.PlatformTaskID
	}
	if patch.OutputAssetID != nil {
		current.OutputAssetID = *patch.OutputAssetID
	}
	if patch.QuoteLabel != nil {
		current.QuoteLabel = *patch.QuoteLabel
	}
	if patch.Charged != nil {
		current.Charged = *patch.Charged
	}
	if patch.DegradedReason != nil {
		current.DegradedReason = *patch.DegradedReason
	}
	if patch.StepCount != nil {
		current.StepCount = *patch.StepCount
	}
	if patch.InputCount != nil {
		current.InputCount = *patch.InputCount
	}
	if patch.ReuploadCount != nil {
		current.ReuploadCount = *patch.ReuploadCount
	}
	if patch.RecoveryCount != nil {
		current.RecoveryCount = *patch.RecoveryCount
	}
	current.UpdatedAt = Now()
	charged := 0
	if current.Charged {
		charged = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE entry_sessions SET
		status = ?, platform_task_id = ?, output_asset_id = ?, quote_label = ?, charged = ?,
		degraded_reason = ?, step_count = ?, input_count = ?, reupload_count = ?, recovery_count = ?, updated_at = ?
		WHERE id = ? AND storage_tenant = ?`,
		current.Status, current.PlatformTaskID, current.OutputAssetID, current.QuoteLabel, charged,
		current.DegradedReason, current.StepCount, current.InputCount, current.ReuploadCount, current.RecoveryCount,
		current.UpdatedAt.Format(time.RFC3339), id, storageTenant)
	if err != nil {
		return EntrySession{}, fmt.Errorf("store: 更新入口会话失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return EntrySession{}, ErrNotFound
	}
	return current, nil
}

const entryCols = `id, account_id, workspace_kind, workspace_scope, storage_tenant, project_id,
	source_type, source_ref, return_href, fingerprint, status, platform_task_id, output_asset_id,
	quote_label, charged, degraded_reason, step_count, input_count, reupload_count, recovery_count,
	authorized_assets, created_at, updated_at`

func scanEntry(row interface{ Scan(...any) error }) (EntrySession, error) {
	var item EntrySession
	var charged int
	var assets, created, updated string
	err := row.Scan(&item.ID, &item.AccountID, &item.WorkspaceKind, &item.WorkspaceScope, &item.StorageTenant, &item.ProjectID,
		&item.SourceType, &item.SourceRef, &item.ReturnHref, &item.Fingerprint, &item.Status, &item.PlatformTaskID, &item.OutputAssetID,
		&item.QuoteLabel, &charged, &item.DegradedReason, &item.StepCount, &item.InputCount, &item.ReuploadCount, &item.RecoveryCount,
		&assets, &created, &updated)
	if err != nil {
		return EntrySession{}, err
	}
	item.Charged = charged != 0
	if assets != "" {
		_ = json.Unmarshal([]byte(assets), &item.AuthorizedAssets)
	}
	if item.AuthorizedAssets == nil {
		item.AuthorizedAssets = []string{}
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return item, nil
}
