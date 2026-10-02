package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// OutputQuality ties a returned project output to its plate derivation.
type OutputQuality struct {
	OutputID, TenantScope, ProjectID, RunID, ResultSHA256 string
	CandidateState, OriginalChargeID                      string
	ScopeJSON, LimitsJSON, ReportSHA256                   string
	CreatedAt                                             int64
}

const outputQualityColumns = `output_id,tenant_scope,project_id,run_id,result_sha256,candidate_state,original_charge_id,scope_json,limits_json,report_sha256,created_at`

func scanOutputQuality(row interface{ Scan(...any) error }) (OutputQuality, error) {
	var q OutputQuality
	e := row.Scan(&q.OutputID, &q.TenantScope, &q.ProjectID, &q.RunID, &q.ResultSHA256, &q.CandidateState, &q.OriginalChargeID, &q.ScopeJSON, &q.LimitsJSON, &q.ReportSHA256, &q.CreatedAt)
	return q, e
}

// SaveOutputQuality is idempotent: a second save for the same output returns
// the stored binding and never replaces it. It refuses anything but a binding
// that exactly matches the output row and its derivation.
func (s *Store) SaveOutputQuality(ctx context.Context, q OutputQuality) (OutputQuality, error) {
	if q.CandidateState != si.CandidateLimited || q.OutputID == "" || q.RunID == "" || q.OriginalChargeID == "" {
		return OutputQuality{}, si.ErrNotUsable
	}
	err := s.withSourceWrite(ctx, func(c *sql.Conn) error {
		var sha, tenant, project string
		e := c.QueryRowContext(ctx, `SELECT result_sha256,tenant_scope,project_id FROM project_outputs WHERE id=?`, q.OutputID).Scan(&sha, &tenant, &project)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		var dsha, dstate, dreport string
		e = c.QueryRowContext(ctx, `SELECT derived_sha256,candidate_state,report_sha256 FROM product_plate_derivations WHERE run_id=?`, q.RunID).Scan(&dsha, &dstate, &dreport)
		if errors.Is(e, sql.ErrNoRows) {
			return si.ErrInvariant
		}
		if e != nil {
			return e
		}
		if sha != q.ResultSHA256 || dsha != q.ResultSHA256 || dstate != si.CandidateLimited || dreport != q.ReportSHA256 || tenant != q.TenantScope || project != q.ProjectID {
			return si.ErrInvariant
		}
		_, e = c.ExecContext(ctx, `INSERT OR IGNORE INTO project_output_quality(`+outputQualityColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			q.OutputID, q.TenantScope, q.ProjectID, q.RunID, q.ResultSHA256, q.CandidateState, q.OriginalChargeID, q.ScopeJSON, q.LimitsJSON, q.ReportSHA256, Now().Unix())
		return e
	})
	if err != nil {
		return OutputQuality{}, err
	}
	got, ok, e := s.GetOutputQuality(ctx, q.TenantScope, q.OutputID)
	if e != nil || !ok {
		return OutputQuality{}, fmt.Errorf("store: output quality not readable after save: %w", si.ErrInvariant)
	}
	return got, nil
}

func (s *Store) GetOutputQuality(ctx context.Context, tenant, outputID string) (OutputQuality, bool, error) {
	q, e := scanOutputQuality(s.db.QueryRowContext(ctx, `SELECT `+outputQualityColumns+` FROM project_output_quality WHERE tenant_scope=? AND output_id=?`, tenant, outputID))
	if errors.Is(e, sql.ErrNoRows) {
		return OutputQuality{}, false, nil
	}
	return q, e == nil, e
}

func (s *Store) ListOutputQualities(ctx context.Context, tenant, project string) ([]OutputQuality, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+outputQualityColumns+` FROM project_output_quality WHERE tenant_scope=? AND project_id=? ORDER BY created_at DESC,output_id`, tenant, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []OutputQuality{}
	for rows.Next() {
		q, e := scanOutputQuality(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// PlateDerivationOwnsSHA reports the run whose derived composite has this
// digest in this project. Such bytes may only enter project outputs through
// the server-side return action.
func (s *Store) PlateDerivationOwnsSHA(ctx context.Context, tenant, project, sha string) (string, bool, error) {
	var run string
	e := s.db.QueryRowContext(ctx, `SELECT d.run_id FROM product_plate_derivations d JOIN product_source_runs r ON r.id=d.run_id WHERE r.tenant_id=? AND r.project_id=? AND d.derived_sha256=? AND d.derived_sha256!=''`, tenant, project, sha).Scan(&run)
	if errors.Is(e, sql.ErrNoRows) {
		return "", false, nil
	}
	return run, e == nil, e
}
