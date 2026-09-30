package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PlateRequest is frozen before confirmation. Mask bytes are never returned in UI views.
type PlateRequest struct {
	PayerID, QuoteID                     string
	TaskIdempotencyKey, PricingVersion   string
	AmountMinor                          int64
	OriginalHash, MaskHash, CoverageHash string
	Model, Provider, Size, ParamsJSON    string
	SampleID                             string
	Mask                                 []byte
}

type PlateExecution struct {
	TenantID, ProjectID, JobID, Fingerprint string
	Request                                 PlateRequest
	State, TaskID, HoldID                   string
}

// PlateFingerprint binds the complete authorization, content and quote tuple.
// It does not rely on the older platform fingerprint, which omits payer/project.
func PlateFingerprint(in PlateExecution) string {
	raw, _ := json.Marshal(struct {
		Tenant, Project, Job string
		Request              PlateRequest
	}{in.TenantID, in.ProjectID, in.JobID, in.Request})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) PreparePlateExecution(ctx context.Context, in PlateExecution) (PlateExecution, error) {
	r := in.Request
	if in.TenantID == "" || in.ProjectID == "" || in.JobID == "" || r.PayerID == "" || r.QuoteID == "" || !r.hasSubmissionIdentity() || r.AmountMinor <= 0 || r.OriginalHash == "" || r.MaskHash == "" || r.CoverageHash == "" || r.Model == "" || r.Provider == "" || r.Size == "" || len(r.Mask) == 0 || !json.Valid([]byte(r.ParamsJSON)) {
		return PlateExecution{}, ErrValidation
	}
	fingerprint := PlateFingerprint(in)
	if in.Fingerprint != "" && in.Fingerprint != fingerprint {
		return PlateExecution{}, ErrValidation
	}
	in.Fingerprint = fingerprint
	raw, err := json.Marshal(r)
	if err != nil {
		return PlateExecution{}, err
	}
	if _, err := s.GetBgJob(ctx, in.TenantID, in.ProjectID, in.JobID); err != nil {
		return PlateExecution{}, err
	}
	// Same immutable request replays; only an unconfirmed quote can be replaced.
	_, err = s.db.ExecContext(ctx, `INSERT INTO bg_plate_executions (tenant_id,project_id,job_id,fingerprint,request_json,state,updated_at) VALUES (?,?,?,?,?,'quoted',?)
 ON CONFLICT(tenant_id,project_id,job_id) DO UPDATE SET fingerprint=excluded.fingerprint,request_json=excluded.request_json,updated_at=excluded.updated_at
 WHERE bg_plate_executions.state='quoted'`, in.TenantID, in.ProjectID, in.JobID, in.Fingerprint, string(raw), Now().Format(time.RFC3339Nano))
	if err != nil {
		return PlateExecution{}, err
	}
	got, err := s.GetPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID)
	if err != nil {
		return PlateExecution{}, err
	}
	kept, _ := json.Marshal(got.Request)
	if got.Fingerprint != in.Fingerprint || string(kept) != string(raw) {
		return PlateExecution{}, fmt.Errorf("%w: confirmed inputs cannot change", ErrConflict)
	}
	return got, nil
}

func (s *Store) GetPlateExecution(ctx context.Context, tenant, project, job string) (PlateExecution, error) {
	out := PlateExecution{TenantID: tenant, ProjectID: project, JobID: job}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT fingerprint,request_json,state,task_id,hold_id FROM bg_plate_executions WHERE tenant_id=? AND project_id=? AND job_id=?`, tenant, project, job).Scan(&out.Fingerprint, &raw, &out.State, &out.TaskID, &out.HoldID)
	if err == sql.ErrNoRows {
		return PlateExecution{}, ErrNotFound
	}
	if err != nil {
		return PlateExecution{}, err
	}
	if err = json.Unmarshal([]byte(raw), &out.Request); err != nil {
		return PlateExecution{}, err
	}
	if !out.Request.hasSubmissionIdentity() {
		return PlateExecution{}, ErrValidation
	}
	return out, nil
}

func (r PlateRequest) hasSubmissionIdentity() bool {
	return strings.TrimSpace(r.TaskIdempotencyKey) != "" && strings.TrimSpace(r.PricingVersion) != ""
}

func (s *Store) ConfirmPlateExecution(ctx context.Context, tenant, project, job, fingerprint string) error {
	if _, err := s.GetPlateExecution(ctx, tenant, project, job); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE bg_plate_executions SET state='confirmed',updated_at=? WHERE tenant_id=? AND project_id=? AND job_id=? AND fingerprint=? AND state IN ('quoted','confirmed')`, Now().Format(time.RFC3339Nano), tenant, project, job, fingerprint)
	return requirePlateChange(res, err)
}

func (s *Store) ClaimPlateExecution(ctx context.Context, tenant, project, job string) (bool, error) {
	if _, err := s.GetPlateExecution(ctx, tenant, project, job); err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE bg_plate_executions SET state='submitting',updated_at=? WHERE tenant_id=? AND project_id=? AND job_id=? AND state='confirmed' AND task_id=''`, Now().Format(time.RFC3339Nano), tenant, project, job)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// UpdatePlateTask never changes a known task/hold identity or reopens a terminal execution.
func (s *Store) UpdatePlateTask(ctx context.Context, tenant, project, job, state, task, hold string) error {
	switch state {
	case "unknown", "queued", "running", "completed", "failed":
	default:
		return ErrValidation
	}
	res, err := s.db.ExecContext(ctx, `UPDATE bg_plate_executions SET state=?,task_id=CASE WHEN task_id='' THEN ? ELSE task_id END,hold_id=CASE WHEN hold_id='' THEN ? ELSE hold_id END,updated_at=?
 WHERE tenant_id=? AND project_id=? AND job_id=? AND state IN ('submitting','unknown','queued','running') AND (task_id='' OR task_id=?) AND (hold_id='' OR ?='' OR hold_id=?)`, state, task, hold, Now().Format(time.RFC3339Nano), tenant, project, job, task, hold, hold)
	return requirePlateChange(res, err)
}

func requirePlateChange(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
