package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
)

// SavedFidelityReport 租户内不可变的主体保真报告。
type SavedFidelityReport struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
	fidelity.Report
}

// SaveFidelityReport 写入报告。同一输入指纹再写且结论一致 → 幂等；
// 指纹相同但结论被改写 → ErrConflict。不提供更新接口。
func (s *Store) SaveFidelityReport(ctx context.Context, tenantID, projectID string, rep fidelity.Report) (SavedFidelityReport, bool, error) {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return SavedFidelityReport{}, false, err
	}
	if rep.BodySHA256 == "" || rep.Verdict == "" {
		return SavedFidelityReport{}, false, fmt.Errorf("%w: 保真报告不完整", ErrValidation)
	}
	existing, err := s.findFidelityByBody(ctx, tenantID, projectID, rep)
	if err == nil {
		if existing.Verdict != rep.Verdict || existing.ExactProduct != rep.ExactProduct || existing.Deliverable != rep.Deliverable {
			return SavedFidelityReport{}, false, fmt.Errorf("%w: 同一评估不能改写结论", ErrConflict)
		}
		return existing, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SavedFidelityReport{}, false, err
	}
	doc, err := json.Marshal(rep)
	if err != nil {
		return SavedFidelityReport{}, false, fmt.Errorf("store: 序列化保真报告失败: %w", err)
	}
	row := SavedFidelityReport{
		ID: newID("fid"), TenantID: tenantID, ProjectID: projectID,
		CreatedAt: Now(), Report: rep,
	}
	exact := 0
	if rep.ExactProduct {
		exact = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO subject_fidelity_reports
		(id, tenant_id, project_id, input_ref, input_version, output_ref, mode, body_sha256, verdict, exact_product, document, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, tenantID, projectID, rep.InputRef, rep.InputVersion, rep.OutputRef, rep.Mode,
		rep.BodySHA256, rep.Verdict, exact, string(doc), row.CreatedAt.Format(time.RFC3339))
	if isUniqueErr(err) {
		existing, ferr := s.findFidelityByBody(ctx, tenantID, projectID, rep)
		if ferr == nil && existing.Verdict == rep.Verdict && existing.ExactProduct == rep.ExactProduct && existing.Deliverable == rep.Deliverable {
			return existing, true, nil
		}
		return SavedFidelityReport{}, false, fmt.Errorf("%w: 同一评估不能改写结论", ErrConflict)
	}
	if err != nil {
		return SavedFidelityReport{}, false, fmt.Errorf("store: 写入保真报告失败: %w", err)
	}
	return row, false, nil
}

func (s *Store) findFidelityByBody(ctx context.Context, tenantID, projectID string, rep fidelity.Report) (SavedFidelityReport, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, project_id, document, created_at
		FROM subject_fidelity_reports
		WHERE tenant_id = ? AND project_id = ? AND input_ref = ? AND input_version = ? AND output_ref = ? AND mode = ? AND body_sha256 = ?`,
		tenantID, projectID, rep.InputRef, rep.InputVersion, rep.OutputRef, rep.Mode, rep.BodySHA256)
	got, err := scanFidelity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SavedFidelityReport{}, ErrNotFound
	}
	return got, err
}

// ListFidelityReports 工程内报告，新的在前。
func (s *Store) ListFidelityReports(ctx context.Context, tenantID, projectID string) ([]SavedFidelityReport, error) {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, project_id, document, created_at
		FROM subject_fidelity_reports WHERE tenant_id = ? AND project_id = ? ORDER BY created_at DESC`,
		tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 列出保真报告失败: %w", err)
	}
	defer rows.Close()
	return collectFidelity(rows)
}

// ListFidelityReportsByOutput 某输出版本上的报告。output_ref 为空时不匹配任何行。
func (s *Store) ListFidelityReportsByOutput(ctx context.Context, tenantID, outputRef string) ([]SavedFidelityReport, error) {
	if outputRef == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, project_id, document, created_at
		FROM subject_fidelity_reports WHERE tenant_id = ? AND output_ref = ? ORDER BY created_at DESC`,
		tenantID, outputRef)
	if err != nil {
		return nil, fmt.Errorf("store: 按输出列出保真报告失败: %w", err)
	}
	defer rows.Close()
	return collectFidelity(rows)
}

// GetFidelityReport 按租户取一份报告。
func (s *Store) GetFidelityReport(ctx context.Context, tenantID, id string) (SavedFidelityReport, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, project_id, document, created_at
		FROM subject_fidelity_reports WHERE id = ? AND tenant_id = ?`, id, tenantID)
	got, err := scanFidelity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SavedFidelityReport{}, ErrNotFound
	}
	return got, err
}

func collectFidelity(rows *sql.Rows) ([]SavedFidelityReport, error) {
	var out []SavedFidelityReport
	for rows.Next() {
		got, err := scanFidelity(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 扫描保真报告失败: %w", err)
		}
		out = append(out, got)
	}
	return out, rows.Err()
}

type fidelityScanner interface{ Scan(...any) error }

func scanFidelity(row fidelityScanner) (SavedFidelityReport, error) {
	var got SavedFidelityReport
	var doc, created string
	if err := row.Scan(&got.ID, &got.TenantID, &got.ProjectID, &doc, &created); err != nil {
		return SavedFidelityReport{}, err
	}
	if err := json.Unmarshal([]byte(doc), &got.Report); err != nil {
		return SavedFidelityReport{}, fmt.Errorf("store: 解析保真报告失败: %w", err)
	}
	got.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return got, nil
}
