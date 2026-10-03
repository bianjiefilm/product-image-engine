package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// CreateSourcePlateRunForProject creates the run and its frozen input in one
// transaction. A repeated request_key with the same frozen input returns the
// original run; a different input (different digest, hence fingerprint) is a
// conflict. The original run's quote, Task and charge are never touched here.
func (s *Store) CreateSourcePlateRunForProject(ctx context.Context, in si.Intent, frozen si.PlateInput) (si.Run, bool, error) {
	if in.Mode != si.ModePlateLock || !frozen.Valid() || in.PlateDigest != si.PlateDigest(frozen) {
		return si.Run{}, false, si.ErrInvalid
	}
	raw, _ := json.Marshal(frozen)
	return s.createSourceRun(ctx, in, true, func(c *sql.Conn, runID string, now int64) error {
		_, e := c.ExecContext(ctx, `INSERT INTO product_plate_inputs(run_id,frozen_json,digest,created_at) VALUES(?,?,?,?)`, runID, string(raw), in.PlateDigest, now)
		return e
	})
}

// GetPlateInput returns the frozen input after proving it still hashes to the
// digest bound into the run's immutable intent.
func (s *Store) GetPlateInput(ctx context.Context, run si.Run) (si.PlateInput, error) {
	var raw, digest string
	e := s.db.QueryRowContext(ctx, `SELECT frozen_json,digest FROM product_plate_inputs WHERE run_id=?`, run.ID).Scan(&raw, &digest)
	if errors.Is(e, sql.ErrNoRows) {
		return si.PlateInput{}, si.ErrInvariant
	}
	if e != nil {
		return si.PlateInput{}, e
	}
	var p si.PlateInput
	if json.Unmarshal([]byte(raw), &p) != nil || !p.Valid() {
		return si.PlateInput{}, si.ErrInvariant
	}
	again, _ := json.Marshal(p)
	if string(again) != raw || si.PlateDigest(p) != digest || digest != run.Intent.PlateDigest || run.Intent.Mode != si.ModePlateLock {
		return si.PlateInput{}, si.ErrInvariant
	}
	return p, nil
}

const plateDerivationColumns = `run_id,state,blocked_reason,derived_png,derived_sha256,size_bytes,width,height,report_json,report_sha256,candidate_state,created_at`

func scanPlateDerivation(row interface{ Scan(...any) error }) (si.PlateDerivation, error) {
	var d si.PlateDerivation
	e := row.Scan(&d.RunID, &d.State, &d.BlockedReason, &d.PNG, &d.SHA256, &d.SizeBytes, &d.Width, &d.Height, &d.ReportJSON, &d.ReportSHA256, &d.CandidateState, &d.CreatedAt)
	if e != nil {
		return d, e
	}
	if si.ValidatePlateDerivation(d) != nil {
		return si.PlateDerivation{}, si.ErrInvariant
	}
	return d, nil
}

// GetPlateDerivation returns (record, true) or (zero, false). Bytes are
// re-verified against their stored digests on every read.
func (s *Store) GetPlateDerivation(ctx context.Context, runID string) (si.PlateDerivation, bool, error) {
	d, e := scanPlateDerivation(s.db.QueryRowContext(ctx, `SELECT `+plateDerivationColumns+` FROM product_plate_derivations WHERE run_id=?`, runID))
	if errors.Is(e, sql.ErrNoRows) {
		return si.PlateDerivation{}, false, nil
	}
	return d, e == nil, e
}

// SavePlateDerivation inserts the write-once record. When a record already
// exists (a concurrent or repeated derivation) the existing one wins; the
// caller's differing bytes are never stored.
func (s *Store) SavePlateDerivation(ctx context.Context, d si.PlateDerivation) (si.PlateDerivation, error) {
	if e := si.ValidatePlateDerivation(d); e != nil {
		return si.PlateDerivation{}, e
	}
	err := s.withSourceWrite(ctx, func(c *sql.Conn) error {
		var one int
		if e := c.QueryRowContext(ctx, `SELECT 1 FROM product_plate_inputs WHERE run_id=?`, d.RunID).Scan(&one); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return si.ErrInvariant
			}
			return e
		}
		_, e := c.ExecContext(ctx, `INSERT OR IGNORE INTO product_plate_derivations(`+plateDerivationColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.RunID, d.State, d.BlockedReason, nullBytes(d.PNG), d.SHA256, d.SizeBytes, d.Width, d.Height, d.ReportJSON, d.ReportSHA256, d.CandidateState, Now().Unix())
		return e
	})
	if err != nil {
		return si.PlateDerivation{}, err
	}
	stored, ok, e := s.GetPlateDerivation(ctx, d.RunID)
	if e != nil || !ok {
		return si.PlateDerivation{}, si.ErrInvariant
	}
	return stored, nil
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// ListPlateRunsAwaitingDerivation lists plate runs whose original output is
// attached but that have no derivation record yet. Runs named in exclude are
// skipped so a caller that is backing off from failing runs still reaches newer
// ones instead of re-reading the same oldest rows forever.
func (s *Store) ListPlateRunsAwaitingDerivation(ctx context.Context, limit int, exclude []string) ([]si.Run, error) {
	if limit <= 0 || limit > 10 || len(exclude) > 500 {
		return nil, si.ErrInvalid
	}
	q := `SELECT ` + sourceColumns + ` FROM product_source_runs WHERE deleted=0 AND phase='succeeded' AND json_extract(observation_json,'$.Output') IS NOT NULL AND id IN (SELECT run_id FROM product_plate_inputs) AND id NOT IN (SELECT run_id FROM product_plate_derivations)`
	args := make([]any, 0, len(exclude)+1)
	if len(exclude) > 0 {
		q += ` AND id NOT IN (?` + strings.Repeat(`,?`, len(exclude)-1) + `)`
		for _, id := range exclude {
			args = append(args, id)
		}
	}
	rows, e := s.db.QueryContext(ctx, q+` ORDER BY updated_at,id LIMIT ?`, append(args, limit)...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []si.Run{}
	for rows.Next() {
		r, e := scanSource(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
