package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound 资源不存在(含跨租户不可见——为避免租户探测,两者同型)。
var ErrNotFound = errors.New("store: 资源不存在")

// ErrValidation 输入不合法。
var ErrValidation = errors.New("store: 输入不合法")

var validSourceTypes = map[string]bool{"": true, "standalone": true, "order": true, "campaign": true}

// Project 制作工程。source_type 可空:standalone 是一等来源,无订单必须可用。
type Project struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	UsageKind  string    `json:"usage_kind"`
	WidthPx    int       `json:"width_px"`
	HeightPx   int       `json:"height_px"`
	SourceType string    `json:"source_type"`
	SourceRef  string    `json:"source_ref"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ProjectUpdate 局部更新;nil 表示不改。
type ProjectUpdate struct {
	Name       *string
	Status     *string
	UsageKind  *string
	WidthPx    *int
	HeightPx   *int
	SourceType *string
	SourceRef  *string
}

func (s *Store) CreateProject(ctx context.Context, p Project) (Project, error) {
	p.ID = newID("proj")
	if strings.TrimSpace(p.Name) == "" {
		return Project{}, fmt.Errorf("%w: 工程名称不能为空", ErrValidation)
	}
	if !validSourceTypes[p.SourceType] {
		return Project{}, fmt.Errorf("%w: source_type 非法: %q", ErrValidation, p.SourceType)
	}
	if p.Status == "" {
		p.Status = "draft"
	}
	now := Now()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects
		(id, tenant_id, name, status, usage_kind, width_px, height_px, source_type, source_ref, created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.TenantID, p.Name, p.Status, p.UsageKind, p.WidthPx, p.HeightPx,
		p.SourceType, p.SourceRef, p.CreatedBy,
		p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return Project{}, fmt.Errorf("store: 创建工程失败: %w", err)
	}
	return p, nil
}

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var created, updated string
	err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Status, &p.UsageKind, &p.WidthPx, &p.HeightPx,
		&p.SourceType, &p.SourceRef, &p.CreatedBy, &created, &updated)
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return p, nil
}

const projectCols = `id, tenant_id, name, status, usage_kind, width_px, height_px, source_type, source_ref, created_by, created_at, updated_at`

// GetProject 按(租户, id)取工程;跨租户视为不存在。
func (s *Store) GetProject(ctx context.Context, tenantID, id string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE id = ? AND tenant_id = ?`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("store: 查询工程失败: %w", err)
	}
	return p, nil
}

// ListProjects 返回租户内全部工程,按更新时间倒序。
func (s *Store) ListProjects(ctx context.Context, tenantID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE tenant_id = ? ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出工程失败: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描工程失败: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject 局部更新并返回新值。
func (s *Store) UpdateProject(ctx context.Context, tenantID, id string, u ProjectUpdate) (Project, error) {
	if u.SourceType != nil && !validSourceTypes[*u.SourceType] {
		return Project{}, fmt.Errorf("%w: source_type 非法: %q", ErrValidation, *u.SourceType)
	}
	sets := []string{"updated_at = ?"}
	args := []any{Now().Format(time.RFC3339)}
	if u.Name != nil {
		if strings.TrimSpace(*u.Name) == "" {
			return Project{}, fmt.Errorf("%w: 工程名称不能为空", ErrValidation)
		}
		sets, args = append(sets, "name = ?"), append(args, *u.Name)
	}
	if u.Status != nil {
		sets, args = append(sets, "status = ?"), append(args, *u.Status)
	}
	if u.UsageKind != nil {
		sets, args = append(sets, "usage_kind = ?"), append(args, *u.UsageKind)
	}
	if u.WidthPx != nil {
		sets, args = append(sets, "width_px = ?"), append(args, *u.WidthPx)
	}
	if u.HeightPx != nil {
		sets, args = append(sets, "height_px = ?"), append(args, *u.HeightPx)
	}
	if u.SourceType != nil {
		sets, args = append(sets, "source_type = ?"), append(args, *u.SourceType)
	}
	if u.SourceRef != nil {
		sets, args = append(sets, "source_ref = ?"), append(args, *u.SourceRef)
	}
	args = append(args, id, tenantID)
	res, err := s.db.ExecContext(ctx,
		`UPDATE projects SET `+strings.Join(sets, ", ")+` WHERE id = ? AND tenant_id = ?`, args...)
	if err != nil {
		return Project{}, fmt.Errorf("store: 更新工程失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Project{}, ErrNotFound
	}
	return s.GetProject(ctx, tenantID, id)
}

// ProjectInput 素材引用:只存平台 asset_id 与本地快照元数据。
type ProjectInput struct {
	ID                  string    `json:"id"`
	ProjectID           string    `json:"project_id"`
	TenantID            string    `json:"tenant_id"`
	PlatformAssetID     string    `json:"platform_asset_id"`
	SnapshotName        string    `json:"snapshot_name"`
	SnapshotSize        int64     `json:"snapshot_size"`
	SnapshotContentType string    `json:"snapshot_content_type"`
	CreatedAt           time.Time `json:"created_at"`
}

func (s *Store) AddInput(ctx context.Context, in ProjectInput) (ProjectInput, error) {
	in.ID = newID("pin")
	if strings.TrimSpace(in.PlatformAssetID) == "" {
		return ProjectInput{}, fmt.Errorf("%w: platform_asset_id 不能为空", ErrValidation)
	}
	// 工程必须属于同租户,否则拒绝(防跨租户挂输入)。
	if _, err := s.GetProject(ctx, in.TenantID, in.ProjectID); err != nil {
		return ProjectInput{}, err
	}
	now := Now()
	in.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_inputs
		(id, project_id, tenant_id, platform_asset_id, snapshot_name, snapshot_size, snapshot_content_type, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		in.ID, in.ProjectID, in.TenantID, in.PlatformAssetID,
		in.SnapshotName, in.SnapshotSize, in.SnapshotContentType, now.Format(time.RFC3339))
	if err != nil {
		return ProjectInput{}, fmt.Errorf("store: 添加输入失败: %w", err)
	}
	return in, nil
}

func (s *Store) ListInputs(ctx context.Context, tenantID, projectID string) ([]ProjectInput, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, tenant_id, platform_asset_id,
		snapshot_name, snapshot_size, snapshot_content_type, created_at
		FROM project_inputs WHERE project_id = ? AND tenant_id = ? ORDER BY created_at`, projectID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出输入失败: %w", err)
	}
	defer rows.Close()
	var out []ProjectInput
	for rows.Next() {
		var in ProjectInput
		var created string
		if err := rows.Scan(&in.ID, &in.ProjectID, &in.TenantID, &in.PlatformAssetID,
			&in.SnapshotName, &in.SnapshotSize, &in.SnapshotContentType, &created); err != nil {
			return nil, fmt.Errorf("store: 扫描输入失败: %w", err)
		}
		in.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) DeleteInput(ctx context.Context, tenantID, projectID, inputID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM project_inputs WHERE id = ? AND project_id = ? AND tenant_id = ?`, inputID, projectID, tenantID)
	if err != nil {
		return fmt.Errorf("store: 删除输入失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ProjectVersion 输出版本:记录平台任务/资产事实(I0 只有占位路径会写入)。
type ProjectVersion struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	TenantID        string    `json:"tenant_id"`
	VersionNo       int       `json:"version_no"`
	PlatformTaskID  string    `json:"platform_task_id"`
	PlatformAssetID string    `json:"platform_asset_id"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Store) AddVersion(ctx context.Context, v ProjectVersion) (ProjectVersion, error) {
	if _, err := s.GetProject(ctx, v.TenantID, v.ProjectID); err != nil {
		return ProjectVersion{}, err
	}
	v.ID = newID("ver")
	if v.Status == "" {
		v.Status = "pending"
	}
	// version_no = 该工程内最大号 + 1
	var maxNo sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(version_no) FROM project_versions WHERE project_id = ?`, v.ProjectID).Scan(&maxNo); err != nil {
		return ProjectVersion{}, fmt.Errorf("store: 查询版本号失败: %w", err)
	}
	v.VersionNo = int(maxNo.Int64) + 1
	now := Now()
	v.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_versions
		(id, project_id, tenant_id, version_no, platform_task_id, platform_asset_id, status, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		v.ID, v.ProjectID, v.TenantID, v.VersionNo, v.PlatformTaskID, v.PlatformAssetID,
		v.Status, now.Format(time.RFC3339))
	if err != nil {
		return ProjectVersion{}, fmt.Errorf("store: 写入版本失败: %w", err)
	}
	return v, nil
}

func (s *Store) ListVersions(ctx context.Context, tenantID, projectID string) ([]ProjectVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, tenant_id, version_no,
		platform_task_id, platform_asset_id, status, created_at
		FROM project_versions WHERE project_id = ? AND tenant_id = ? ORDER BY version_no DESC`, projectID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出版本失败: %w", err)
	}
	defer rows.Close()
	var out []ProjectVersion
	for rows.Next() {
		var v ProjectVersion
		var created string
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.TenantID, &v.VersionNo,
			&v.PlatformTaskID, &v.PlatformAssetID, &v.Status, &created); err != nil {
			return nil, fmt.Errorf("store: 扫描版本失败: %w", err)
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, v)
	}
	return out, rows.Err()
}
