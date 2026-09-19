package store

// 批次编排持久层(HUI-1704 / FEAT-0205)。
// 硬不变式:每行按 tenant_id 作用域过滤——跨租户不可见;幂等键
// UNIQUE(batch_id, input_sha256)(拍板:同批次同输入内容只有一个 item)。
// 批次状态机推进用守卫式更新(UPDATE … WHERE status IN (…)),不匹配即 ErrConflict。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Batch 批次:一批=一个制作工程,多文件=工程多输入。
type Batch struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	CreatedBy     string    `json:"created_by"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	PresetSummary string    `json:"preset_summary"` // JSON 文本:所选场景×尺寸意图
	ErrorCode     string    `json:"error_code"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// BatchItemSpec 建批入参:平台 asset 登记事实(httpapi 先经 upload 门拿 asset_id)。
type BatchItemSpec struct {
	AssetID     string
	Name        string
	Size        int64
	SHA256      string
	ContentType string
}

// BatchItem 批次项:一个输入文件的编排事实。
type BatchItem struct {
	ID             string    `json:"id"`
	BatchID        string    `json:"batch_id"`
	TenantID       string    `json:"tenant_id"`
	ProjectID      string    `json:"project_id"`
	InputRef       string    `json:"input_ref"`
	InputSHA256    string    `json:"input_sha256"`
	InputName      string    `json:"input_name"`
	InputSize      int64     `json:"input_size"`
	TaskRef        string    `json:"task_ref"`
	Status         string    `json:"status"`
	ErrorCode      string    `json:"error_code"`
	OutputID       string    `json:"output_id"`
	SizeVariantIDs string    `json:"size_variant_ids"` // JSON 数组文本或空
	VariantsNote   string    `json:"variants_note"`
	Attempts       int       `json:"attempts"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreateBatch 单事务建齐:工程(未给 ProjectID 时新建)+ inputs + batch + items。
// asset 登记是网络调用,必须在事务外完成后再进来——事务内绝不触网。
func (s *Store) CreateBatch(ctx context.Context, b Batch, specs []BatchItemSpec) (Batch, []BatchItem, error) {
	if len(specs) == 0 {
		return Batch{}, nil, fmt.Errorf("%w: 批次至少要有一个输入文件", ErrValidation)
	}
	if strings.TrimSpace(b.Name) == "" {
		return Batch{}, nil, fmt.Errorf("%w: 批次名称不能为空", ErrValidation)
	}
	for i, sp := range specs {
		if strings.TrimSpace(sp.AssetID) == "" || strings.TrimSpace(sp.SHA256) == "" {
			return Batch{}, nil, fmt.Errorf("%w: specs[%d] 缺 asset_id/sha256", ErrValidation, i)
		}
	}
	now := Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Batch{}, nil, fmt.Errorf("store: 开事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1) 工程:未显式给则建一个(拍板:一批次=一工程,文件=工程多输入)。
	if b.ProjectID == "" {
		p := Project{TenantID: b.TenantID, Name: b.Name, Status: "active", CreatedBy: b.CreatedBy}
		p.ID = newID("proj")
		p.CreatedAt, p.UpdatedAt = now, now
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects
			(id, tenant_id, name, status, usage_kind, width_px, height_px, source_type, source_ref, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.TenantID, p.Name, p.Status, p.UsageKind, p.WidthPx, p.HeightPx,
			p.SourceType, p.SourceRef, p.CreatedBy, p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339)); err != nil {
			return Batch{}, nil, fmt.Errorf("store: 建批次工程失败: %w", err)
		}
		b.ProjectID = p.ID
	} else {
		// 指定工程:必须在 tx 连接上校验(库 MaxOpenConns=1,事务内再经 *Store 取
		// 连接会自锁)。跨租户/不存在 → ErrNotFound 同型。
		var one int
		if err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM projects WHERE id = ? AND tenant_id = ?`, b.ProjectID, b.TenantID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Batch{}, nil, ErrNotFound
			}
			return Batch{}, nil, fmt.Errorf("store: 校验指定工程失败: %w", err)
		}
	}

	// 2) 批次行。
	b.ID = newID("batch")
	if b.Status == "" {
		b.Status = "pending"
	}
	b.CreatedAt, b.UpdatedAt = now, now
	if _, err := tx.ExecContext(ctx, `INSERT INTO batches
		(id, tenant_id, created_by, project_id, name, status, preset_summary, error_code, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.TenantID, b.CreatedBy, b.ProjectID, b.Name, b.Status, b.PresetSummary, b.ErrorCode,
		b.CreatedAt.Format(time.RFC3339), b.UpdatedAt.Format(time.RFC3339)); err != nil {
		return Batch{}, nil, fmt.Errorf("store: 建批次失败: %w", err)
	}

	// 3) inputs + items。
	items := make([]BatchItem, 0, len(specs))
	for _, sp := range specs {
		in := ProjectInput{ID: newID("pin"), ProjectID: b.ProjectID, TenantID: b.TenantID,
			PlatformAssetID: sp.AssetID, SnapshotName: sp.Name, SnapshotSize: sp.Size,
			SnapshotContentType: sp.ContentType, CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_inputs
			(id, project_id, tenant_id, platform_asset_id, snapshot_name, snapshot_size, snapshot_content_type, created_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			in.ID, in.ProjectID, in.TenantID, in.PlatformAssetID, in.SnapshotName, in.SnapshotSize,
			in.SnapshotContentType, in.CreatedAt.Format(time.RFC3339)); err != nil {
			return Batch{}, nil, fmt.Errorf("store: 建批次输入失败: %w", err)
		}
		it := BatchItem{ID: newID("bitem"), BatchID: b.ID, TenantID: b.TenantID, ProjectID: b.ProjectID,
			InputRef: in.ID, InputSHA256: sp.SHA256, InputName: sp.Name, InputSize: sp.Size,
			Status: "pending", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO batch_items
			(id, batch_id, tenant_id, project_id, input_ref, input_sha256, input_name, input_size,
			 task_ref, status, error_code, output_id, size_variant_ids, variants_note, attempts, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.BatchID, it.TenantID, it.ProjectID, it.InputRef, it.InputSHA256, it.InputName, it.InputSize,
			it.TaskRef, it.Status, it.ErrorCode, it.OutputID, it.SizeVariantIDs, it.VariantsNote, it.Attempts,
			it.CreatedAt.Format(time.RFC3339), it.UpdatedAt.Format(time.RFC3339)); err != nil {
			if isUniqueErr(err) {
				return Batch{}, nil, fmt.Errorf("%w: 同批次已有相同内容的输入(幂等键 batch_id+input_sha256)", ErrConflict)
			}
			return Batch{}, nil, fmt.Errorf("store: 建批次项失败: %w", err)
		}
		items = append(items, it)
	}
	if err := tx.Commit(); err != nil {
		return Batch{}, nil, fmt.Errorf("store: 提交批次事务失败: %w", err)
	}
	return b, items, nil
}

// InsertBatchItem 单独追加批次项(幂等键冲突 → ErrConflict)。当前建批一次性给齐;
// 保留入口供未来增量追加与唯一键兜底测试。
func (s *Store) InsertBatchItem(ctx context.Context, it BatchItem) (BatchItem, error) {
	if strings.TrimSpace(it.InputSHA256) == "" {
		return BatchItem{}, fmt.Errorf("%w: input_sha256 不能为空", ErrValidation)
	}
	now := Now()
	it.ID = newID("bitem")
	if it.Status == "" {
		it.Status = "pending"
	}
	it.CreatedAt, it.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO batch_items
		(id, batch_id, tenant_id, project_id, input_ref, input_sha256, input_name, input_size,
		 task_ref, status, error_code, output_id, size_variant_ids, variants_note, attempts, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.BatchID, it.TenantID, it.ProjectID, it.InputRef, it.InputSHA256, it.InputName, it.InputSize,
		it.TaskRef, it.Status, it.ErrorCode, it.OutputID, it.SizeVariantIDs, it.VariantsNote, it.Attempts,
		it.CreatedAt.Format(time.RFC3339), it.UpdatedAt.Format(time.RFC3339))
	if isUniqueErr(err) {
		return BatchItem{}, fmt.Errorf("%w: 同批次已有相同内容的输入", ErrConflict)
	}
	if err != nil {
		return BatchItem{}, fmt.Errorf("store: 追加批次项失败: %w", err)
	}
	return s.getBatchItem(ctx, it.TenantID, it.ID)
}

const batchCols = `id, tenant_id, created_by, project_id, name, status, preset_summary, error_code, created_at, updated_at`

func scanBatch(row interface{ Scan(...any) error }) (Batch, error) {
	var b Batch
	var created, updated string
	if err := row.Scan(&b.ID, &b.TenantID, &b.CreatedBy, &b.ProjectID, &b.Name, &b.Status,
		&b.PresetSummary, &b.ErrorCode, &created, &updated); err != nil {
		return Batch{}, err
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339, created)
	b.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return b, nil
}

// GetBatch 按(租户, id)取批次;跨租户视为不存在。
func (s *Store) GetBatch(ctx context.Context, tenantID, id string) (Batch, error) {
	b, err := scanBatch(s.db.QueryRowContext(ctx,
		`SELECT `+batchCols+` FROM batches WHERE id = ? AND tenant_id = ?`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("store: 查询批次失败: %w", err)
	}
	return b, nil
}

// GetBatchWithItems 批次 + 全部 items(创建序)。
func (s *Store) GetBatchWithItems(ctx context.Context, tenantID, id string) (Batch, []BatchItem, error) {
	b, err := s.GetBatch(ctx, tenantID, id)
	if err != nil {
		return Batch{}, nil, err
	}
	items, err := s.ListBatchItems(ctx, tenantID, id)
	if err != nil {
		return Batch{}, nil, err
	}
	return b, items, nil
}

// ListBatches 租户内全部批次(新→旧)。
func (s *Store) ListBatches(ctx context.Context, tenantID string) ([]Batch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+batchCols+` FROM batches WHERE tenant_id = ? ORDER BY created_at DESC, id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出批次失败: %w", err)
	}
	defer rows.Close()
	var out []Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描批次失败: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

const batchItemCols = `id, batch_id, tenant_id, project_id, input_ref, input_sha256, input_name, input_size,
	task_ref, status, error_code, output_id, size_variant_ids, variants_note, attempts, created_at, updated_at`

func scanBatchItem(row interface{ Scan(...any) error }) (BatchItem, error) {
	var it BatchItem
	var created, updated string
	if err := row.Scan(&it.ID, &it.BatchID, &it.TenantID, &it.ProjectID, &it.InputRef, &it.InputSHA256,
		&it.InputName, &it.InputSize, &it.TaskRef, &it.Status, &it.ErrorCode, &it.OutputID,
		&it.SizeVariantIDs, &it.VariantsNote, &it.Attempts, &created, &updated); err != nil {
		return BatchItem{}, err
	}
	it.CreatedAt, _ = time.Parse(time.RFC3339, created)
	it.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return it, nil
}

// ListBatchItems 批次下全部 items(创建序)。
func (s *Store) ListBatchItems(ctx context.Context, tenantID, batchID string) ([]BatchItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+batchItemCols+` FROM batch_items WHERE batch_id = ? AND tenant_id = ? ORDER BY created_at, id`,
		batchID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出批次项失败: %w", err)
	}
	defer rows.Close()
	var out []BatchItem
	for rows.Next() {
		it, err := scanBatchItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描批次项失败: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) getBatchItem(ctx context.Context, tenantID, id string) (BatchItem, error) {
	it, err := scanBatchItem(s.db.QueryRowContext(ctx,
		`SELECT `+batchItemCols+` FROM batch_items WHERE id = ? AND tenant_id = ?`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return BatchItem{}, ErrNotFound
	}
	if err != nil {
		return BatchItem{}, fmt.Errorf("store: 查询批次项失败: %w", err)
	}
	return it, nil
}

// GetBatchItem 按(租户, id)取批次项。
func (s *Store) GetBatchItem(ctx context.Context, tenantID, id string) (BatchItem, error) {
	return s.getBatchItem(ctx, tenantID, id)
}

// UpdateBatchStatus 守卫式推进批次状态:仅当当前 status ∈ from 才更新;不匹配 → ErrConflict。
// errorCode 非空时写入批次级原因(如实失败)。
func (s *Store) UpdateBatchStatus(ctx context.Context, tenantID, id string, from []string, to, errorCode string) (Batch, error) {
	if len(from) == 0 {
		return Batch{}, fmt.Errorf("%w: from 状态列表不能为空", ErrValidation)
	}
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE batches SET status = ?, error_code = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND status IN (`+
			strings.Repeat("?,", len(from)-1)+"?)",
		append([]any{to, errorCode, now, id, tenantID}, toAny(from)...)...)
	if err != nil {
		return Batch{}, fmt.Errorf("store: 推进批次状态失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 区分「不存在/跨租户」与「状态不匹配」:跨租户与不存在同型(包不变式)。
		if _, gerr := s.GetBatch(ctx, tenantID, id); gerr != nil {
			return Batch{}, ErrNotFound
		}
		return Batch{}, fmt.Errorf("%w: 批次状态不是期望的 %s", ErrConflict, strings.Join(from, "|"))
	}
	return s.GetBatch(ctx, tenantID, id)
}

// UpdateBatchItemStatus 守卫式推进 item 状态;taskRef 只在提交语义(调用方给非空)时落列。
func (s *Store) UpdateBatchItemStatus(ctx context.Context, tenantID, id string, from []string, to, taskRef, errorCode string) (BatchItem, error) {
	if len(from) == 0 {
		return BatchItem{}, fmt.Errorf("%w: from 状态列表不能为空", ErrValidation)
	}
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE batch_items SET status = ?, task_ref = ?, error_code = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ? AND status IN (`+strings.Repeat("?,", len(from)-1)+"?)",
		append([]any{to, taskRef, errorCode, now, id, tenantID}, toAny(from)...)...)
	if err != nil {
		return BatchItem{}, fmt.Errorf("store: 推进批次项状态失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 区分「不存在/跨租户」与「状态不匹配」:跨租户与不存在同型(包不变式)。
		if _, gerr := s.getBatchItem(ctx, tenantID, id); gerr != nil {
			return BatchItem{}, ErrNotFound
		}
		return BatchItem{}, fmt.Errorf("%w: 批次项状态不是期望的 %s", ErrConflict, strings.Join(from, "|"))
	}
	return s.getBatchItem(ctx, tenantID, id)
}

// ItemResultUpdate 收集阶段的成果字段更新(status 变更 + 登记事实)。
type ItemResultUpdate struct {
	Status         string
	TaskRef        string // 可选:语义上收集不改 task_ref,保留缺省不改
	OutputID       string
	SizeVariantIDs string
	VariantsNote   string
	ErrorCode      string
}

// UpdateBatchItemResult 收集推进:写状态与成果登记事实。
func (s *Store) UpdateBatchItemResult(ctx context.Context, tenantID, id string, u ItemResultUpdate) (BatchItem, error) {
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE batch_items SET status = ?, output_id = ?, size_variant_ids = ?,
		variants_note = ?, error_code = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		u.Status, u.OutputID, u.SizeVariantIDs, u.VariantsNote, u.ErrorCode, now, id, tenantID)
	if err != nil {
		return BatchItem{}, fmt.Errorf("store: 写批次项成果失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return BatchItem{}, ErrNotFound
	}
	return s.getBatchItem(ctx, tenantID, id)
}

// IncrementBatchItemAttempt 提交序号原子自增(task 幂等键组成部分),返回自增后的值。
func (s *Store) IncrementBatchItemAttempt(ctx context.Context, tenantID, id string) (int, error) {
	now := Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE batch_items SET attempts = attempts + 1, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		now, id, tenantID)
	if err != nil {
		return 0, fmt.Errorf("store: 自增提交序号失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	it, err := s.getBatchItem(ctx, tenantID, id)
	if err != nil {
		return 0, err
	}
	return it.Attempts, nil
}

// CountBatchItemsByStatus 批次内按状态计数(counts 派生源:读时计算,不落列不漂移)。
func (s *Store) CountBatchItemsByStatus(ctx context.Context, batchID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM batch_items WHERE batch_id = ? GROUP BY status`, batchID)
	if err != nil {
		return nil, fmt.Errorf("store: 统计批次项失败: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, fmt.Errorf("store: 扫描批次计数失败: %w", err)
		}
		out[st] = n
	}
	return out, rows.Err()
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, v := range ss {
		out[i] = v
	}
	return out
}
