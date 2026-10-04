package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bianjiefilm/product-image-engine/server/internal/motionref"
)

// PutMotionDeclaration 写入一条引用。同一修订再次声明返回原行，不改主体，也不清参数。
// created 为 true 表示这次新插入。
func (s *Store) PutMotionDeclaration(ctx context.Context, tenantID, projectID string, d motionref.Declaration) (motionref.Declaration, bool, error) {
	if err := d.Intact(); err != nil {
		return motionref.Declaration{}, false, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return motionref.Declaration{}, false, err
	}
	existing, err := s.motionByRevision(ctx, tenantID, projectID, d.RevisionID)
	switch {
	case err == nil:
		if existing.AssetID != d.AssetID || existing.ContentHash != d.ContentHash || existing.Digest != d.Digest {
			return motionref.Declaration{}, false, fmt.Errorf("%w: motion reference subject", ErrConflict)
		}
		return existing, false, nil
	case !errors.Is(err, ErrNotFound):
		return motionref.Declaration{}, false, err
	}
	d.ID = newID("mref")
	if err := d.Intact(); err != nil {
		return motionref.Declaration{}, false, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	raw, err := json.Marshal(d.Parameters)
	if err != nil {
		return motionref.Declaration{}, false, fmt.Errorf("%w: parameters", ErrValidation)
	}
	now := Now().Format("2006-01-02T15:04:05Z")
	if _, err := s.db.ExecContext(ctx, `INSERT INTO motion_product_refs (
		tenant_id, project_id, id, asset_id, content_sha256, revision_id, digest, subject_sha256,
		parameters_json, product_regeneration, model_call_count, video_generated, supplier_called,
		verified, dynamic_ad, finished_copy, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,0,0,0,0,0,0,?,?,?)`,
		tenantID, projectID, d.ID, d.AssetID, d.ContentHash, d.RevisionID, d.Digest, d.SubjectHash,
		string(raw), motionref.NotFinishedCopy, now, now); err != nil {
		return motionref.Declaration{}, false, fmt.Errorf("store: 写入 Motion 引用失败: %w", err)
	}
	saved, err := s.GetMotionDeclaration(ctx, tenantID, projectID, d.ID)
	return saved, true, err
}

// RecordMotionParameter 只更新参数 JSON。主体列由触发器锁住。
func (s *Store) RecordMotionParameter(ctx context.Context, tenantID, projectID, id, name, value string) (motionref.Declaration, error) {
	current, err := s.GetMotionDeclaration(ctx, tenantID, projectID, id)
	if err != nil {
		return motionref.Declaration{}, err
	}
	next, err := motionref.RecordParameter(current, name, value)
	if err != nil {
		return motionref.Declaration{}, err
	}
	raw, err := json.Marshal(next.Parameters)
	if err != nil {
		return motionref.Declaration{}, fmt.Errorf("%w: parameters", ErrValidation)
	}
	now := Now().Format("2006-01-02T15:04:05Z")
	res, err := s.db.ExecContext(ctx, `UPDATE motion_product_refs
		SET parameters_json = ?, updated_at = ?
		WHERE tenant_id = ? AND project_id = ? AND id = ?`,
		string(raw), now, tenantID, projectID, id)
	if err != nil {
		return motionref.Declaration{}, fmt.Errorf("store: 记录 Motion 参数失败: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return motionref.Declaration{}, err
	}
	if n != 1 {
		return motionref.Declaration{}, ErrNotFound
	}
	return s.GetMotionDeclaration(ctx, tenantID, projectID, id)
}

// GetMotionDeclaration 按租户读取一条引用。跨租户与不存在同为 ErrNotFound。
func (s *Store) GetMotionDeclaration(ctx context.Context, tenantID, projectID, id string) (motionref.Declaration, error) {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return motionref.Declaration{}, err
	}
	row := s.db.QueryRowContext(ctx, motionSelect+` AND id = ?`, tenantID, projectID, id)
	d, err := scanMotion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return motionref.Declaration{}, ErrNotFound
	}
	if err != nil {
		return motionref.Declaration{}, err
	}
	return d, nil
}

// ListMotionDeclarations 列出本工程的引用声明。
func (s *Store) ListMotionDeclarations(ctx context.Context, tenantID, projectID string) ([]motionref.Declaration, error) {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, motionSelect+` ORDER BY created_at, id`, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出 Motion 引用失败: %w", err)
	}
	defer rows.Close()
	out := []motionref.Declaration{}
	for rows.Next() {
		d, err := scanMotion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

const motionSelect = `SELECT id, asset_id, content_sha256, revision_id, digest, subject_sha256,
	parameters_json, product_regeneration, model_call_count, video_generated, supplier_called,
	verified, dynamic_ad, finished_copy
	FROM motion_product_refs WHERE tenant_id = ? AND project_id = ?`

type motionScanner interface {
	Scan(dest ...any) error
}

func (s *Store) motionByRevision(ctx context.Context, tenantID, projectID, revisionID string) (motionref.Declaration, error) {
	row := s.db.QueryRowContext(ctx, motionSelect+` AND revision_id = ?`, tenantID, projectID, revisionID)
	d, err := scanMotion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return motionref.Declaration{}, ErrNotFound
	}
	if err != nil {
		return motionref.Declaration{}, err
	}
	return d, nil
}

func scanMotion(sc motionScanner) (motionref.Declaration, error) {
	var d motionref.Declaration
	var params string
	var regeneration, calls, video, supplier, verified, dynamic int
	if err := sc.Scan(&d.ID, &d.AssetID, &d.ContentHash, &d.RevisionID, &d.Digest, &d.SubjectHash,
		&params, &regeneration, &calls, &video, &supplier, &verified, &dynamic, &d.FinishedCopy); err != nil {
		return motionref.Declaration{}, err
	}
	if regeneration != 0 || calls != 0 || video != 0 || supplier != 0 || verified != 0 || dynamic != 0 {
		return motionref.Declaration{}, fmt.Errorf("%w: motion reference claimed a result", ErrConflict)
	}
	d.Schema = motionref.Schema
	d.Parameters = []motionref.Parameter{}
	if err := json.Unmarshal([]byte(params), &d.Parameters); err != nil {
		return motionref.Declaration{}, fmt.Errorf("store: Motion 参数损坏: %w", err)
	}
	if d.Parameters == nil {
		d.Parameters = []motionref.Parameter{}
	}
	if err := d.Intact(); err != nil {
		return motionref.Declaration{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return d, nil
}
