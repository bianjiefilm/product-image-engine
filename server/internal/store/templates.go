package store

// 模板库仓储(HUI-1705 / FEAT-0206)。
//
// 硬不变式:
//   - 自定义模板行按 tenant_scope 过滤;跨租户不可见(ErrNotFound);
//   - 库内只存参数引用与纯数值(params_json 已过 imagetmpl 校验);
//   - 套用 = 登记制:留痕行(image_template_applies)+ 工程行参数引用写点,
//     同 (租户,工程,模板,版本) 唯一键幂等——重复套用不新增行、不改写写点;
//   - 本层绝不产生生成/计费调用边,工程既有产物零改动。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/imagetmpl"
)

// ImageTemplateApply 套用留痕(不可变:同键命中即原样返回)。
type ImageTemplateApply struct {
	ID              string          `json:"id"`
	TenantScope     string          `json:"-"`
	ProjectID       string          `json:"project_id"`
	TemplateID      string          `json:"template_id"`
	TemplateVersion int             `json:"template_version"`
	Params          json.RawMessage `json:"params"`
	AppliedBy       string          `json:"applied_by"`
	CreatedAt       time.Time       `json:"created_at"`
}

// ---- 自定义模板 CRUD --------------------------------------------------------

// CreateImageTemplate 创建租户自定义模板(version 固定从 1 起;同租户重名 → ErrConflict)。
func (s *Store) CreateImageTemplate(ctx context.Context, t imagetmpl.Template) (imagetmpl.Template, error) {
	t.Source = imagetmpl.SourceCustom
	t.TenantScope = strings.TrimSpace(t.TenantScope)
	t.Name = strings.TrimSpace(t.Name)
	t.Version = 1
	if t.TenantScope == "" {
		return imagetmpl.Template{}, fmt.Errorf("%w: tenant_scope 不能为空", ErrValidation)
	}
	if err := imagetmpl.ValidateTemplate(t); err != nil {
		return imagetmpl.Template{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	t.ID = newID("tpl")
	now := Now()
	t.CreatedAt, t.UpdatedAt = now, now
	paramsJSON, err := json.Marshal(t.Params)
	if err != nil {
		return imagetmpl.Template{}, fmt.Errorf("store: 序列化模板参数失败: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO image_templates
		(id, tenant_scope, name, industry, label, version, params_json, created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.TenantScope, t.Name, t.Industry, t.Label, t.Version, string(paramsJSON),
		t.CreatedBy, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		if isUniqueErr(err) {
			return imagetmpl.Template{}, fmt.Errorf("%w: 同租户已有同名模板", ErrConflict)
		}
		return imagetmpl.Template{}, fmt.Errorf("store: 创建模板失败: %w", err)
	}
	return s.GetImageTemplate(ctx, t.TenantScope, t.ID)
}

const templateCols = `id, tenant_scope, name, industry, label, version, params_json, created_by, created_at, updated_at`

func scanTemplate(row interface{ Scan(...any) error }) (imagetmpl.Template, error) {
	var t imagetmpl.Template
	var paramsJSON, created, updated string
	if err := row.Scan(&t.ID, &t.TenantScope, &t.Name, &t.Industry, &t.Label, &t.Version,
		&paramsJSON, &t.CreatedBy, &created, &updated); err != nil {
		return imagetmpl.Template{}, err
	}
	if err := json.Unmarshal([]byte(paramsJSON), &t.Params); err != nil {
		return imagetmpl.Template{}, fmt.Errorf("store: 模板参数损坏: %w", err)
	}
	t.Source = imagetmpl.SourceCustom
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return t, nil
}

// GetImageTemplate 按 (租户, id) 取自定义模板;跨租户视为不存在。
func (s *Store) GetImageTemplate(ctx context.Context, tenantScope, id string) (imagetmpl.Template, error) {
	t, err := scanTemplate(s.db.QueryRowContext(ctx,
		`SELECT `+templateCols+` FROM image_templates WHERE id = ? AND tenant_scope = ?`, id, tenantScope))
	if errors.Is(err, sql.ErrNoRows) {
		return imagetmpl.Template{}, ErrNotFound
	}
	if err != nil {
		return imagetmpl.Template{}, fmt.Errorf("store: 查询模板失败: %w", err)
	}
	return t, nil
}

// ListImageTemplates 租户内全部自定义模板(新→旧)。
func (s *Store) ListImageTemplates(ctx context.Context, tenantScope string) ([]imagetmpl.Template, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+templateCols+` FROM image_templates WHERE tenant_scope = ? ORDER BY updated_at DESC, rowid DESC`,
		tenantScope)
	if err != nil {
		return nil, fmt.Errorf("store: 列出模板失败: %w", err)
	}
	defer rows.Close()
	var out []imagetmpl.Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描模板失败: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ImageTemplateUpdate 局部更新;nil 表示不改。每次成功更新 version+1
// (套用留痕按版本钉死,任何字段变更都必须推进版本)。
type ImageTemplateUpdate struct {
	Name   *string
	Label  *string
	Params *imagetmpl.Params
}

// UpdateImageTemplate 更新自定义模板并返回新值(版本 +1);跨租户 → ErrNotFound。
func (s *Store) UpdateImageTemplate(ctx context.Context, tenantScope, id string, u ImageTemplateUpdate) (imagetmpl.Template, error) {
	cur, err := s.GetImageTemplate(ctx, tenantScope, id)
	if err != nil {
		return imagetmpl.Template{}, err
	}
	if u.Name != nil {
		cur.Name = strings.TrimSpace(*u.Name)
	}
	if u.Label != nil {
		cur.Label = *u.Label
	}
	if u.Params != nil {
		cur.Params = *u.Params
	}
	if err := imagetmpl.ValidateTemplate(cur); err != nil {
		return imagetmpl.Template{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	cur.Version++
	paramsJSON, err := json.Marshal(cur.Params)
	if err != nil {
		return imagetmpl.Template{}, fmt.Errorf("store: 序列化模板参数失败: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE image_templates SET name = ?, label = ?, version = ?, params_json = ?, updated_at = ?
		 WHERE id = ? AND tenant_scope = ?`,
		cur.Name, cur.Label, cur.Version, string(paramsJSON),
		Now().Format(time.RFC3339), id, tenantScope)
	if err != nil {
		if isUniqueErr(err) {
			return imagetmpl.Template{}, fmt.Errorf("%w: 同租户已有同名模板", ErrConflict)
		}
		return imagetmpl.Template{}, fmt.Errorf("store: 更新模板失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return imagetmpl.Template{}, ErrNotFound
	}
	return s.GetImageTemplate(ctx, tenantScope, id)
}

// DeleteImageTemplate 删除自定义模板;跨租户/不存在 → ErrNotFound。
// 已产生的套用留痕是历史事实,不级联删除。
func (s *Store) DeleteImageTemplate(ctx context.Context, tenantScope, id string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM image_templates WHERE id = ? AND tenant_scope = ?`, id, tenantScope)
	if err != nil {
		return fmt.Errorf("store: 删除模板失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- 套用(登记制:留痕 + 工程参数引用写点) ---------------------------------

// ApplyImageTemplate 套用模板到工程:写一条留痕 + 把参数引用落到工程行
// (单事务)。同 (租户,工程,模板,版本) 已有留痕 → 幂等返回既有记录
// (dup=true),不重复写、不改写工程写点。工程跨租户/不存在 → ErrNotFound。
func (s *Store) ApplyImageTemplate(ctx context.Context, a ImageTemplateApply) (ImageTemplateApply, bool, error) {
	a.TenantScope = strings.TrimSpace(a.TenantScope)
	if a.TenantScope == "" || a.ProjectID == "" || a.TemplateID == "" || a.AppliedBy == "" {
		return ImageTemplateApply{}, false, fmt.Errorf("%w: tenant_scope/project_id/template_id/applied_by 不能为空", ErrValidation)
	}
	if a.TemplateVersion < 1 {
		return ImageTemplateApply{}, false, fmt.Errorf("%w: template_version 须 ≥1", ErrValidation)
	}
	var paramsCheck map[string]any
	if err := json.Unmarshal(a.Params, &paramsCheck); err != nil || paramsCheck == nil {
		return ImageTemplateApply{}, false, fmt.Errorf("%w: params 必须是参数引用 JSON 对象", ErrValidation)
	}
	// 工程必须在租户内存在(跨租户不可见是 store 硬不变式)。
	if _, err := s.GetProject(ctx, a.TenantScope, a.ProjectID); err != nil {
		return ImageTemplateApply{}, false, err
	}
	// 幂等预检:同键已有留痕 → 原样返回,零写放大。
	existing, err := s.FindTemplateApplyByKey(ctx, a.TenantScope, a.ProjectID, a.TemplateID, a.TemplateVersion)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ImageTemplateApply{}, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImageTemplateApply{}, false, fmt.Errorf("store: 开事务失败: %w", err)
	}
	defer tx.Rollback()
	a.ID = newID("apply")
	a.CreatedAt = Now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO image_template_applies
		(id, tenant_scope, project_id, template_id, template_version, params_json, applied_by, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		a.ID, a.TenantScope, a.ProjectID, a.TemplateID, a.TemplateVersion,
		string(a.Params), a.AppliedBy, a.CreatedAt.Format(time.RFC3339)); err != nil {
		if isUniqueErr(err) { // 并发兜底:冲突即重读幂等返回
			if ex, gerr := s.FindTemplateApplyByKey(ctx, a.TenantScope, a.ProjectID, a.TemplateID, a.TemplateVersion); gerr == nil {
				return ex, true, nil
			}
			return ImageTemplateApply{}, false, fmt.Errorf("%w: 同键套用并发写入冲突", ErrConflict)
		}
		return ImageTemplateApply{}, false, fmt.Errorf("store: 写套用留痕失败: %w", err)
	}
	// 工程配置写点:只写参数引用(登记制;不触产物、不触生成)。
	if _, err := tx.ExecContext(ctx,
		`UPDATE projects SET applied_tpl_id = ?, applied_tpl_version = ?, applied_tpl_params = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		a.TemplateID, a.TemplateVersion, string(a.Params),
		Now().Format(time.RFC3339), a.ProjectID, a.TenantScope); err != nil {
		return ImageTemplateApply{}, false, fmt.Errorf("store: 落工程模板引用失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ImageTemplateApply{}, false, fmt.Errorf("store: 提交套用失败: %w", err)
	}
	return a, false, nil
}

// FindTemplateApplyByKey 套用唯一键查询(唯一合法查询路径)。
func (s *Store) FindTemplateApplyByKey(ctx context.Context, tenantScope, projectID, templateID string, templateVersion int) (ImageTemplateApply, error) {
	a, err := scanApply(s.db.QueryRowContext(ctx,
		`SELECT id, tenant_scope, project_id, template_id, template_version, params_json, applied_by, created_at
		 FROM image_template_applies
		 WHERE tenant_scope = ? AND project_id = ? AND template_id = ? AND template_version = ?`,
		tenantScope, projectID, templateID, templateVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return ImageTemplateApply{}, ErrNotFound
	}
	if err != nil {
		return ImageTemplateApply{}, fmt.Errorf("store: 查询套用留痕失败: %w", err)
	}
	return a, nil
}

// ListTemplateApplies 工程套用留痕(时间正序:留痕是历史事实)。
func (s *Store) ListTemplateApplies(ctx context.Context, tenantScope, projectID string) ([]ImageTemplateApply, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_scope, project_id, template_id, template_version, params_json, applied_by, created_at
		 FROM image_template_applies WHERE tenant_scope = ? AND project_id = ? ORDER BY created_at, rowid`,
		tenantScope, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出套用留痕失败: %w", err)
	}
	defer rows.Close()
	var out []ImageTemplateApply
	for rows.Next() {
		a, err := scanApply(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描套用留痕失败: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanApply(row interface{ Scan(...any) error }) (ImageTemplateApply, error) {
	var a ImageTemplateApply
	var paramsJSON, created string
	if err := row.Scan(&a.ID, &a.TenantScope, &a.ProjectID, &a.TemplateID, &a.TemplateVersion,
		&paramsJSON, &a.AppliedBy, &created); err != nil {
		return ImageTemplateApply{}, err
	}
	a.Params = json.RawMessage(paramsJSON)
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return a, nil
}
