package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 来源绑定 / 交接快照 / 一次性兑换 / 输出登记 / 定向回执(HUI-1745 I1)。
// 硬不变式:
//   - 绑定唯一键 = tenant_scope+source_app+source_ref+target_app+purpose(票面原文);
//     任何绑定查询都按五元组或 (tenant_scope, id),绝不凭 source_ref 单字段或工程 ID 反查。
//   - 快照不可变:同 handoff_id 同内容幂等;异内容冲突且保留原快照;内容列只写一次。
//   - 回执 payload 不可变;重传只重发既有 payload。

// ErrConflict 唯一键/内容冲突(调用方映射 409)。
var ErrConflict = errors.New("store: 内容冲突")

// SourceBinding 稳定绑定(同五元组只绑定一个工程)。
type SourceBinding struct {
	ID                string    `json:"id"`
	TenantScope       string    `json:"tenant_scope"`
	SourceApp         string    `json:"source_app"`
	SourceRef         string    `json:"source_ref"`
	TargetApp         string    `json:"target_app"`
	Purpose           string    `json:"purpose"`
	ProjectID         string    `json:"project_id"`
	CurrentSnapshotID string    `json:"current_snapshot_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// BindingKey 绑定唯一键五元组(票面「幂等与租户键补充」原文的代码化)。
type BindingKey struct {
	TenantScope string
	SourceApp   string
	SourceRef   string
	TargetApp   string
	Purpose     string
}

const bindingCols = `id, tenant_scope, source_app, source_ref, target_app, purpose, project_id, current_snapshot_id, created_at, updated_at`

func scanBinding(row interface{ Scan(...any) error }) (SourceBinding, error) {
	var b SourceBinding
	var created, updated string
	if err := row.Scan(&b.ID, &b.TenantScope, &b.SourceApp, &b.SourceRef, &b.TargetApp, &b.Purpose,
		&b.ProjectID, &b.CurrentSnapshotID, &created, &updated); err != nil {
		return SourceBinding{}, err
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339, created)
	b.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return b, nil
}

// CreateBinding 创建绑定;五元组已存在 → ErrConflict(并发/重复只绑定一次由 UNIQUE 保证)。
func (s *Store) CreateBinding(ctx context.Context, b SourceBinding) (SourceBinding, error) {
	if strings.TrimSpace(b.Purpose) == "" {
		return SourceBinding{}, fmt.Errorf("%w: 绑定 purpose 不能为空", ErrValidation)
	}
	b.ID = newID("bind")
	now := Now()
	b.CreatedAt, b.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_bindings
		(id, tenant_scope, source_app, source_ref, target_app, purpose, project_id, current_snapshot_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.TenantScope, b.SourceApp, b.SourceRef, b.TargetApp, b.Purpose,
		b.ProjectID, b.CurrentSnapshotID, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if isUniqueErr(err) {
		return SourceBinding{}, fmt.Errorf("%w: 该来源与用途已绑定工程(同键只绑定一次)", ErrConflict)
	}
	if err != nil {
		return SourceBinding{}, fmt.Errorf("store: 创建绑定失败: %w", err)
	}
	return s.GetBinding(ctx, b.TenantScope, b.ID)
}

// FindBindingByKey 按五元组唯一键查绑定(唯一合法查询路径)。
func (s *Store) FindBindingByKey(ctx context.Context, k BindingKey) (SourceBinding, error) {
	b, err := scanBinding(s.db.QueryRowContext(ctx,
		`SELECT `+bindingCols+` FROM source_bindings
		 WHERE tenant_scope = ? AND source_app = ? AND source_ref = ? AND target_app = ? AND purpose = ?`,
		k.TenantScope, k.SourceApp, k.SourceRef, k.TargetApp, k.Purpose))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceBinding{}, ErrNotFound
	}
	if err != nil {
		return SourceBinding{}, fmt.Errorf("store: 查询绑定失败: %w", err)
	}
	return b, nil
}

// GetBinding 按 (租户, id) 取绑定;跨租户视为不存在。
func (s *Store) GetBinding(ctx context.Context, tenantScope, id string) (SourceBinding, error) {
	b, err := scanBinding(s.db.QueryRowContext(ctx,
		`SELECT `+bindingCols+` FROM source_bindings WHERE id = ? AND tenant_scope = ?`, id, tenantScope))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceBinding{}, ErrNotFound
	}
	if err != nil {
		return SourceBinding{}, fmt.Errorf("store: 查询绑定失败: %w", err)
	}
	return b, nil
}

// GetBindingByProject 按 (租户, project_id) 取绑定(工程→来源展示用;一工程至多一条)。
func (s *Store) GetBindingByProject(ctx context.Context, tenantScope, projectID string) (SourceBinding, error) {
	b, err := scanBinding(s.db.QueryRowContext(ctx,
		`SELECT `+bindingCols+` FROM source_bindings WHERE project_id = ? AND tenant_scope = ?`, projectID, tenantScope))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceBinding{}, ErrNotFound
	}
	if err != nil {
		return SourceBinding{}, fmt.Errorf("store: 查询绑定失败: %w", err)
	}
	return b, nil
}

// ListBindings 租户内全部绑定(重登/重启恢复入口)。
func (s *Store) ListBindings(ctx context.Context, tenantScope string) ([]SourceBinding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+bindingCols+` FROM source_bindings WHERE tenant_scope = ? ORDER BY updated_at DESC`, tenantScope)
	if err != nil {
		return nil, fmt.Errorf("store: 列出绑定失败: %w", err)
	}
	defer rows.Close()
	var out []SourceBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描绑定失败: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AdoptBindingSnapshot 采用新快照:只移动 current_snapshot_id;不触碰工程字段与输出。
func (s *Store) AdoptBindingSnapshot(ctx context.Context, tenantScope, id, snapshotID string) (SourceBinding, error) {
	if _, err := s.GetSnapshot(ctx, tenantScope, snapshotID); err != nil {
		return SourceBinding{}, err
	}
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE source_bindings SET current_snapshot_id = ?, updated_at = ? WHERE id = ? AND tenant_scope = ?`,
		snapshotID, now, id, tenantScope)
	if err != nil {
		return SourceBinding{}, fmt.Errorf("store: 采用快照失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return SourceBinding{}, ErrNotFound
	}
	return s.GetBinding(ctx, tenantScope, id)
}

// HandoffSnapshot 交接不可变快照。
type HandoffSnapshot struct {
	ID             string    `json:"id"`
	TenantScope    string    `json:"tenant_scope"`
	BindingID      string    `json:"binding_id"`
	HandoffID      string    `json:"handoff_id"`
	ContentFP      string    `json:"content_fp"`
	SourceKind     string    `json:"source_kind"`
	SourceApp      string    `json:"source_app"`
	PrincipalID    string    `json:"principal_id"`
	SourceRevision string    `json:"source_revision"`
	BriefVersion   string    `json:"brief_version"`
	ProfileJSON    string    `json:"profile_json"`
	CreatedAt      time.Time `json:"created_at"`
}

// snapshotCols:binding_id 列可空(先建快照后建绑定),读出统一为 ”。
const snapshotCols = `id, tenant_scope, COALESCE(binding_id,'') AS binding_id, handoff_id, content_fp, source_kind, source_app, principal_id, source_revision, brief_version, profile_json, created_at`

func scanSnapshot(row interface{ Scan(...any) error }) (HandoffSnapshot, error) {
	var v HandoffSnapshot
	var created string
	if err := row.Scan(&v.ID, &v.TenantScope, &v.BindingID, &v.HandoffID, &v.ContentFP, &v.SourceKind,
		&v.SourceApp, &v.PrincipalID, &v.SourceRevision, &v.BriefVersion, &v.ProfileJSON, &created); err != nil {
		return HandoffSnapshot{}, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return v, nil
}

// CreateSnapshot 写入新快照;(tenant, handoff_id) 已存在 → ErrConflict
// (调用方必须先 FindSnapshotByHandoff 裁决 SAME/CONFLICT/NEW,冲突时保留原快照)。
func (s *Store) CreateSnapshot(ctx context.Context, v HandoffSnapshot) (HandoffSnapshot, error) {
	v.ID = newID("snap")
	v.CreatedAt = Now()
	var bindingID any // 未绑定时 NULL(FK 对 NULL 不生效;绑定建立后补链)
	if v.BindingID != "" {
		bindingID = v.BindingID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO handoff_snapshots
		(id, tenant_scope, binding_id, handoff_id, content_fp, source_kind, source_app, principal_id, source_revision, brief_version, profile_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.TenantScope, bindingID, v.HandoffID, v.ContentFP, v.SourceKind, v.SourceApp,
		v.PrincipalID, v.SourceRevision, v.BriefVersion, v.ProfileJSON, v.CreatedAt.Format(time.RFC3339))
	if isUniqueErr(err) {
		return HandoffSnapshot{}, fmt.Errorf("%w: 同 handoff_id 快照已存在", ErrConflict)
	}
	if err != nil {
		return HandoffSnapshot{}, fmt.Errorf("store: 写入快照失败: %w", err)
	}
	return s.GetSnapshot(ctx, v.TenantScope, v.ID)
}

// FindSnapshotByHandoff 同 handoff 幂等裁决入口:命中 → 比对 content_fp 分流
// SNAPSHOT_SAME(同内容)/SNAPSHOT_CONFLICT(异内容,保留原快照);未命中 → SNAPSHOT_NEW。
func (s *Store) FindSnapshotByHandoff(ctx context.Context, tenantScope, handoffID string) (HandoffSnapshot, error) {
	v, err := scanSnapshot(s.db.QueryRowContext(ctx,
		`SELECT `+snapshotCols+` FROM handoff_snapshots WHERE tenant_scope = ? AND handoff_id = ?`, tenantScope, handoffID))
	if errors.Is(err, sql.ErrNoRows) {
		return HandoffSnapshot{}, ErrNotFound
	}
	if err != nil {
		return HandoffSnapshot{}, fmt.Errorf("store: 查询快照失败: %w", err)
	}
	return v, nil
}

// GetSnapshot 按 (租户, id) 取快照。
func (s *Store) GetSnapshot(ctx context.Context, tenantScope, id string) (HandoffSnapshot, error) {
	v, err := scanSnapshot(s.db.QueryRowContext(ctx,
		`SELECT `+snapshotCols+` FROM handoff_snapshots WHERE id = ? AND tenant_scope = ?`, id, tenantScope))
	if errors.Is(err, sql.ErrNoRows) {
		return HandoffSnapshot{}, ErrNotFound
	}
	if err != nil {
		return HandoffSnapshot{}, fmt.Errorf("store: 查询快照失败: %w", err)
	}
	return v, nil
}

// UpdateSnapshotBinding 快照归属补链(创建时绑定未知,绑定建立后一次性回填;
// 幂等:已归属的快照不动)。
func (s *Store) UpdateSnapshotBinding(ctx context.Context, tenantScope, id, bindingID string) (HandoffSnapshot, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE handoff_snapshots SET binding_id = ? WHERE id = ? AND tenant_scope = ? AND (binding_id IS NULL OR binding_id = '')`,
		bindingID, id, tenantScope)
	if err != nil {
		return HandoffSnapshot{}, fmt.Errorf("store: 回填快照归属失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 已归属或其他租户不可见:返回当前行供校验。
		return s.GetSnapshot(ctx, tenantScope, id)
	}
	return s.GetSnapshot(ctx, tenantScope, id)
}

// ListSnapshotsForBinding 绑定下的全部快照(新→旧)。
func (s *Store) ListSnapshotsForBinding(ctx context.Context, tenantScope, bindingID string) ([]HandoffSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+snapshotCols+` FROM handoff_snapshots WHERE tenant_scope = ? AND binding_id = ? ORDER BY created_at DESC, rowid DESC`,
		tenantScope, bindingID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出快照失败: %w", err)
	}
	defer rows.Close()
	var out []HandoffSnapshot
	for rows.Next() {
		v, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描快照失败: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Redemption 一次性兑换记录(应用内语义)。
type Redemption struct {
	BindingRef  string    `json:"binding_ref"`
	TenantScope string    `json:"tenant_scope"`
	ContentFP   string    `json:"content_fp"`
	SnapshotID  string    `json:"snapshot_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// FindRedemption 兑换幂等查询(按 binding_ref)。
func (s *Store) FindRedemption(ctx context.Context, bindingRef string) (Redemption, error) {
	var r Redemption
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT binding_ref, tenant_scope, content_fp, snapshot_id, created_at FROM handoff_redemptions WHERE binding_ref = ?`,
		bindingRef).Scan(&r.BindingRef, &r.TenantScope, &r.ContentFP, &r.SnapshotID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Redemption{}, ErrNotFound
	}
	if err != nil {
		return Redemption{}, fmt.Errorf("store: 查询兑换失败: %w", err)
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return r, nil
}

// CreateRedemption 记录兑换;binding_ref 已消费 → ErrConflict(一次性)。
func (s *Store) CreateRedemption(ctx context.Context, r Redemption) (Redemption, error) {
	r.CreatedAt = Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO handoff_redemptions (binding_ref, tenant_scope, content_fp, snapshot_id, created_at) VALUES (?,?,?,?,?)`,
		r.BindingRef, r.TenantScope, r.ContentFP, r.SnapshotID, r.CreatedAt.Format(time.RFC3339))
	if isUniqueErr(err) {
		return Redemption{}, fmt.Errorf("%w: 绑定凭据已消费(一次性兑换)", ErrConflict)
	}
	if err != nil {
		return Redemption{}, fmt.Errorf("store: 记录兑换失败: %w", err)
	}
	return r, nil
}

// ProjectOutput 用户明确选择的输出版本(登记产出事实)。
type ProjectOutput struct {
	ID              string    `json:"id"`
	TenantScope     string    `json:"tenant_scope"`
	ProjectID       string    `json:"project_id"`
	PlatformAssetID string    `json:"platform_asset_id"`
	ResultSHA256    string    `json:"result_sha256"`
	ResultSize      int64     `json:"result_size"`
	MediaType       string    `json:"media_type"`
	FileName        string    `json:"file_name"`
	SnapshotID      string    `json:"snapshot_id"`
	BriefVersion    string    `json:"brief_version"`
	SourceRevision  string    `json:"source_revision"`
	PlatformTaskID  string    `json:"platform_task_id"`
	CreatedAt       time.Time `json:"created_at"`
}

const outputCols = `id, tenant_scope, project_id, platform_asset_id, result_sha256, result_size, media_type, file_name, snapshot_id, brief_version, source_revision, platform_task_id, created_at`

func scanOutput(row interface{ Scan(...any) error }) (ProjectOutput, error) {
	var o ProjectOutput
	var created string
	if err := row.Scan(&o.ID, &o.TenantScope, &o.ProjectID, &o.PlatformAssetID, &o.ResultSHA256, &o.ResultSize,
		&o.MediaType, &o.FileName, &o.SnapshotID, &o.BriefVersion, &o.SourceRevision, &o.PlatformTaskID, &created); err != nil {
		return ProjectOutput{}, err
	}
	o.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return o, nil
}

// CreateOutput 登记输出;(project, result_sha256) 已存在 → ErrConflict(重复提交幂等裁决入口)。
func (s *Store) CreateOutput(ctx context.Context, o ProjectOutput) (ProjectOutput, error) {
	o.ID = newID("out")
	o.CreatedAt = Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_outputs
		(id, tenant_scope, project_id, platform_asset_id, result_sha256, result_size, media_type, file_name, snapshot_id, brief_version, source_revision, platform_task_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.TenantScope, o.ProjectID, o.PlatformAssetID, o.ResultSHA256, o.ResultSize, o.MediaType,
		o.FileName, o.SnapshotID, o.BriefVersion, o.SourceRevision, o.PlatformTaskID, o.CreatedAt.Format(time.RFC3339))
	if isUniqueErr(err) {
		return ProjectOutput{}, fmt.Errorf("%w: 同内容产出已登记", ErrConflict)
	}
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 登记输出失败: %w", err)
	}
	return s.GetOutput(ctx, o.TenantScope, o.ID)
}

// GetOutput 按 (租户, id) 取输出。
func (s *Store) GetOutput(ctx context.Context, tenantScope, id string) (ProjectOutput, error) {
	o, err := scanOutput(s.db.QueryRowContext(ctx,
		`SELECT `+outputCols+` FROM project_outputs WHERE id = ? AND tenant_scope = ?`, id, tenantScope))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectOutput{}, ErrNotFound
	}
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 查询输出失败: %w", err)
	}
	return o, nil
}

// FindOutputByContent 同工程同内容输出的幂等查询。
func (s *Store) FindOutputByContent(ctx context.Context, tenantScope, projectID, resultSHA256 string) (ProjectOutput, error) {
	o, err := scanOutput(s.db.QueryRowContext(ctx,
		`SELECT `+outputCols+` FROM project_outputs WHERE tenant_scope = ? AND project_id = ? AND result_sha256 = ?`,
		tenantScope, projectID, resultSHA256))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectOutput{}, ErrNotFound
	}
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 查询输出失败: %w", err)
	}
	return o, nil
}

// ListOutputs 工程下全部输出(新→旧)。
func (s *Store) ListOutputs(ctx context.Context, tenantScope, projectID string) ([]ProjectOutput, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+outputCols+` FROM project_outputs WHERE tenant_scope = ? AND project_id = ? ORDER BY created_at DESC`,
		tenantScope, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出输出失败: %w", err)
	}
	defer rows.Close()
	var out []ProjectOutput
	for rows.Next() {
		o, err := scanOutput(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描输出失败: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SourceReceipt 定向回执(payload 不可变;状态机 pending→delivered|failed)。
type SourceReceipt struct {
	ID          string    `json:"id"`
	TenantScope string    `json:"tenant_scope"`
	ProjectID   string    `json:"project_id"`
	OutputID    string    `json:"output_id"`
	EventID     string    `json:"event_id"`
	HandoffID   string    `json:"handoff_id"`
	TargetApp   string    `json:"target_app"`
	RunID       string    `json:"run_id"`
	Sequence    int64     `json:"sequence"`
	Payload     string    `json:"-"`
	PayloadFP   string    `json:"payload_fp"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	LastError   string    `json:"last_error"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const receiptCols = `id, tenant_scope, project_id, output_id, event_id, handoff_id, target_app, run_id, sequence, payload, payload_fp, status, attempts, last_error, created_at, updated_at`

func scanReceipt(row interface{ Scan(...any) error }) (SourceReceipt, error) {
	var r SourceReceipt
	var created, updated string
	if err := row.Scan(&r.ID, &r.TenantScope, &r.ProjectID, &r.OutputID, &r.EventID, &r.HandoffID, &r.TargetApp,
		&r.RunID, &r.Sequence, &r.Payload, &r.PayloadFP, &r.Status, &r.Attempts, &r.LastError, &created, &updated); err != nil {
		return SourceReceipt{}, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return r, nil
}

// CreateReceipt 写入回执(先持久后发送);(tenant, event_id) 已存在 → ErrConflict。
func (s *Store) CreateReceipt(ctx context.Context, r SourceReceipt) (SourceReceipt, error) {
	r.ID = newID("rcpt")
	if r.Status == "" {
		r.Status = "pending"
	}
	now := Now()
	r.CreatedAt, r.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_receipts
		(id, tenant_scope, project_id, output_id, event_id, handoff_id, target_app, run_id, sequence, payload, payload_fp, status, attempts, last_error, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.TenantScope, r.ProjectID, r.OutputID, r.EventID, r.HandoffID, r.TargetApp, r.RunID,
		r.Sequence, r.Payload, r.PayloadFP, r.Status, r.Attempts, r.LastError,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	if isUniqueErr(err) {
		return SourceReceipt{}, fmt.Errorf("%w: 同事件回执已存在", ErrConflict)
	}
	if err != nil {
		return SourceReceipt{}, fmt.Errorf("store: 写入回执失败: %w", err)
	}
	return s.GetReceipt(ctx, r.TenantScope, r.ID)
}

// GetReceipt 按 (租户, id) 取回执。
func (s *Store) GetReceipt(ctx context.Context, tenantScope, id string) (SourceReceipt, error) {
	r, err := scanReceipt(s.db.QueryRowContext(ctx,
		`SELECT `+receiptCols+` FROM source_receipts WHERE id = ? AND tenant_scope = ?`, id, tenantScope))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceReceipt{}, ErrNotFound
	}
	if err != nil {
		return SourceReceipt{}, fmt.Errorf("store: 查询回执失败: %w", err)
	}
	return r, nil
}

// FindReceiptByOutput 同输出的既有回执(重复提交返回原回执,不产生新事件)。
func (s *Store) FindReceiptByOutput(ctx context.Context, tenantScope, outputID string) (SourceReceipt, error) {
	r, err := scanReceipt(s.db.QueryRowContext(ctx,
		`SELECT `+receiptCols+` FROM source_receipts WHERE tenant_scope = ? AND output_id = ? ORDER BY created_at DESC LIMIT 1`,
		tenantScope, outputID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceReceipt{}, ErrNotFound
	}
	if err != nil {
		return SourceReceipt{}, fmt.Errorf("store: 查询回执失败: %w", err)
	}
	return r, nil
}

// ListReceipts 工程下全部回执(新→旧)。
func (s *Store) ListReceipts(ctx context.Context, tenantScope, projectID string) ([]SourceReceipt, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+receiptCols+` FROM source_receipts WHERE tenant_scope = ? AND project_id = ? ORDER BY created_at DESC`,
		tenantScope, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出回执失败: %w", err)
	}
	defer rows.Close()
	var out []SourceReceipt
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描回执失败: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FinishReceipt 更新投递结果(delivered/failed + attempts + last_error)。payload/事件事实不动。
func (s *Store) FinishReceipt(ctx context.Context, tenantScope, id, status, lastError string) (SourceReceipt, error) {
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE source_receipts SET status = ?, last_error = ?, attempts = attempts + 1, updated_at = ? WHERE id = ? AND tenant_scope = ?`,
		status, lastError, now, id, tenantScope)
	if err != nil {
		return SourceReceipt{}, fmt.Errorf("store: 更新回执失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return SourceReceipt{}, ErrNotFound
	}
	return s.GetReceipt(ctx, tenantScope, id)
}

// isUniqueErr SQLite 唯一键冲突(modernc.org/sqlite 错误串含 UNIQUE constraint)。
func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")
}
