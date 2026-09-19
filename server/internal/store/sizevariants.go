package store

// 尺寸适配变体仓储(HUI-1703 / FEAT-0204)。
//
// 硬不变式:
//   - 变体是 project_outputs 行,kind='size_variant';唯一键 = 拍板三元
//     (source_output_id, preset_name, variant_mode)(部分索引;result 行不参与)。
//   - 变体内容字节只随 CreateSizeVariant 一次性写入;幂等/并发重复由唯一键兜底,
//     调用方以 FindSizeVariantByKey 裁决后返回既有变体。
//   - 全部查询按 tenant_scope 过滤;跨租户不可见(ErrNotFound)。
//   - GetOutputUnscoped/GetProjectUnscoped 仅用于 HTTP 层 403/404 分类,
//     绝不能把非属主租户的行数据写回响应(红线)。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// OutputKind outputs 行类别。
const (
	OutputKindResult      = "result"
	OutputKindSizeVariant = "size_variant"
)

// CreateSizeVariant 登记变体行 + 内容字节(单事务,内容只写一次)。
// kind 强制 size_variant(调用方无需也不应另设);唯一键冲突 → ErrConflict
// (并发/重复由调用方重读裁决)。
func (s *Store) CreateSizeVariant(ctx context.Context, o ProjectOutput, content []byte) (ProjectOutput, error) {
	o.Kind = OutputKindSizeVariant
	if o.SourceOutputID == "" || o.PresetName == "" || o.VariantMode == "" {
		return ProjectOutput{}, fmt.Errorf("%w: 变体三元组(source_output_id/preset_name/variant_mode)不能为空", ErrValidation)
	}
	if len(content) == 0 {
		return ProjectOutput{}, fmt.Errorf("%w: 变体内容不能为空", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 开事务失败: %w", err)
	}
	defer tx.Rollback()
	o.ID = newID("out")
	o.CreatedAt = Now()
	_, err = tx.ExecContext(ctx, `INSERT INTO project_outputs
		(id, tenant_scope, project_id, platform_asset_id, result_sha256, result_size, media_type, file_name, snapshot_id, brief_version, source_revision, platform_task_id, created_at,
		 kind, source_output_id, preset_name, variant_mode, variant_width, variant_height)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.TenantScope, o.ProjectID, o.PlatformAssetID, o.ResultSHA256, o.ResultSize, o.MediaType,
		o.FileName, o.SnapshotID, o.BriefVersion, o.SourceRevision, o.PlatformTaskID, o.CreatedAt.Format(time.RFC3339),
		OutputKindSizeVariant, o.SourceOutputID, o.PresetName, o.VariantMode, o.VariantWidth, o.VariantHeight)
	if isUniqueErr(err) {
		return ProjectOutput{}, fmt.Errorf("%w: 同键变体已存在", ErrConflict)
	}
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 登记变体失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO size_variant_blobs (variant_id, content) VALUES (?,?)`, o.ID, content); err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 写入变体内容失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 提交变体失败: %w", err)
	}
	return s.GetOutput(ctx, o.TenantScope, o.ID)
}

// FindSizeVariantByKey 变体唯一键幂等查询(唯一合法查询路径)。
func (s *Store) FindSizeVariantByKey(ctx context.Context, tenantScope, sourceOutputID, preset, mode string) (ProjectOutput, error) {
	o, err := scanOutput(s.db.QueryRowContext(ctx,
		`SELECT `+outputCols+` FROM project_outputs
		 WHERE tenant_scope = ? AND kind = 'size_variant' AND source_output_id = ? AND preset_name = ? AND variant_mode = ?`,
		tenantScope, sourceOutputID, preset, mode))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectOutput{}, ErrNotFound
	}
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 查询变体失败: %w", err)
	}
	return o, nil
}

// ListSizeVariants 工程下全部变体(新→旧)。
func (s *Store) ListSizeVariants(ctx context.Context, tenantScope, projectID string) ([]ProjectOutput, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+outputCols+` FROM project_outputs
		 WHERE tenant_scope = ? AND project_id = ? AND kind = 'size_variant' ORDER BY created_at DESC, rowid DESC`,
		tenantScope, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出变体失败: %w", err)
	}
	defer rows.Close()
	var out []ProjectOutput
	for rows.Next() {
		o, err := scanOutput(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描变体失败: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetVariantBlob 按 (租户, id) 取变体字节(下载;内容不可变)。
func (s *Store) GetVariantBlob(ctx context.Context, tenantScope, variantID string) ([]byte, error) {
	o, err := s.GetOutput(ctx, tenantScope, variantID)
	if err != nil {
		return nil, err
	}
	if o.Kind != OutputKindSizeVariant {
		return nil, ErrNotFound // result 行没有本地字节
	}
	var content []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT content FROM size_variant_blobs WHERE variant_id = ?`, variantID).Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: 读取变体内容失败: %w", err)
	}
	return content, nil
}

// GetOutputUnscoped 按输出 ID 全局取行(不带租户过滤)。
// **仅用于 HTTP 层 403/404 分类**(区分「不存在」与「存在但非你的租户」);
// 返回值禁止直接写回非属主响应。任何正常读取走 GetOutput(租户作用域)。
func (s *Store) GetOutputUnscoped(ctx context.Context, id string) (ProjectOutput, error) {
	o, err := scanOutput(s.db.QueryRowContext(ctx,
		`SELECT `+outputCols+` FROM project_outputs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectOutput{}, ErrNotFound
	}
	if err != nil {
		return ProjectOutput{}, fmt.Errorf("store: 查询输出(全局)失败: %w", err)
	}
	return o, nil
}

// GetProjectUnscoped 按工程 ID 全局取行(语义同 GetOutputUnscoped,红线同上)。
func (s *Store) GetProjectUnscoped(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("store: 查询工程(全局)失败: %w", err)
	}
	return p, nil
}
