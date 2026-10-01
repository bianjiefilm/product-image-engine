package store

import (
	"context"
	"database/sql"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"math"
	"reflect"
)

func sourceProjectWritable(ctx context.Context, c *sql.Conn, r si.Run) error {
	scope := r.Intent.Scope
	var tenant, creator, status string
	e := c.QueryRowContext(ctx, `SELECT tenant_id,created_by,status FROM projects WHERE id=? AND tenant_id=?`, scope.ProjectID, scope.TenantID).Scan(&tenant, &creator, &status)
	if errors.Is(e, sql.ErrNoRows) {
		return si.ErrNotFound
	}
	if e != nil {
		return e
	}
	if status == "deleted" {
		return si.ErrNotFound
	}
	if status == "archived" {
		return si.ErrForbidden
	}
	personal := tenant == scope.PrincipalAccountID || tenant == "personal/default:"+scope.PrincipalAccountID
	if personal && creator != scope.UserID || !personal && privateSourceTenant(tenant) {
		return si.ErrNotFound
	}
	return nil
}
func validateSourceObservation(ctx context.Context, c *sql.Conn, r, in si.Run) error {
	allowed := r
	allowed.LastError = in.LastError
	allowed.RetryAt = in.RetryAt
	allowed.Attempts = in.Attempts
	allowed.TaskID = in.TaskID
	allowed.Task = in.Task
	allowed.Phase = in.Phase
	allowed.SubmitAttempted = in.SubmitAttempted
	allowed.LeaseUntil = in.LeaseUntil
	if !reflect.DeepEqual(in, allowed) || r.SubmitAttempted && !in.SubmitAttempted {
		return si.ErrConflict
	}
	if in.Task == nil {
		if r.Task != nil || in.TaskID != "" {
			return si.ErrConflict
		}
	} else {
		if in.TaskID != in.Task.ID || r.ConfirmationHash == "" || !in.SubmitAttempted || si.ValidateTaskFact(r, *in.Task) != nil {
			return si.ErrInvariant
		}
	}
	if !r.SubmitAttempted && in.SubmitAttempted && in.Task == nil {
		return si.ErrConflict
	}

	if in.LeaseUntil != r.LeaseUntil && (in.LeaseUntil != 0 || in.Task == nil) {
		return si.ErrConflict
	}
	if in.Phase != r.Phase {
		switch in.Phase {
		case "task_submit_pending":
			if !in.SubmitAttempted || in.Task != nil {
				return si.ErrConflict
			}
		case "task_linked":
			if in.Task == nil || (in.Task.Status != "queued" && in.Task.Status != "running") {
				return si.ErrConflict
			}
		case "asset_pending":
			if in.Task == nil || in.Task.Status != "succeeded" {
				return si.ErrConflict
			}
		case "cancel_pending":
			if !r.CancelRequested || !in.SubmitAttempted {
				return si.ErrConflict
			}
		case "provider_unknown":
			if in.LastError == "" {
				return si.ErrConflict
			}
		case "review_required":
			if in.Task == nil || (in.Task.Status != "failed" && in.Task.Status != "canceled") {
				return si.ErrConflict
			}
		default:
			return si.ErrConflict
		}
	}
	return nil
}
func sourceTaskIDValue(id string) any {
	if id == "" {
		return nil
	}
	return id
}
func (s *Store) CancelSourceRun(ctx context.Context, scope si.Scope, id string) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceOnConn(ctx, c, scope, id)
		if e != nil {
			return e
		}
		if e = sourceProjectWritable(ctx, c, r); e != nil {
			return e
		}
		if r.CancelRequested {
			out = r
			return nil
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		phase := "cancel_pending"
		lease := r.LeaseUntil
		if !r.SubmitAttempted && r.TaskID == "" {
			phase = "not_dispatched"
			lease = 0
		}
		now := Now().Unix()
		events, e := sourceEvents(r, phase, now)
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET cancel_requested=1,phase=?,events_json=?,lease_until=?,retry_at=0,revision=revision+1,updated_at=? WHERE id=?`, phase, events, lease, now, r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, scope, id)
		return e
	})
	return
}
func (s *Store) ListDueSourceRuns(ctx context.Context, now int64, limit int) ([]si.Run, error) {
	if now <= 0 || limit <= 0 || limit > 10 {
		return nil, si.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE lease_until<=? AND retry_at<=? AND phase NOT IN('succeeded','not_dispatched','canceled','failed') AND (confirmation_hash!='' OR (quote_json IS NULL AND deleted=0 AND cancel_requested=0)) ORDER BY retry_at,id LIMIT ?`, now, now, limit)
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

// MarkSourceSubmitAttempt commits the first request budget and original intent.
// Budget expiry is only a local request bound. Formal same-key Task idempotency
// and original Usage ownership remain the upstream duplicate-dispatch authority.
func (s *Store) MarkSourceSubmitAttempt(ctx context.Context, in si.Run, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		now := Now().Unix()
		if r.ConfirmationHash == "" || r.Quote == nil || r.TaskID != "" || r.CancelRequested || r.Deleted || now < r.Quote.QuotedAtUnix || now >= r.Quote.ExpiresAtUnix {
			return si.ErrConflict
		}
		if e = sourceProjectWritable(ctx, c, r); e != nil {
			return e
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		started, budget := r.SubmitStartedAt, r.SubmitBudgetSeconds
		if !r.SubmitAttempted {
			if now <= 0 || now > math.MaxInt64-si.TaskRequestBudgetSeconds {
				return si.ErrInvariant
			}
			started = now
			budget = si.TaskRequestBudgetSeconds
		} else {
			if started <= 0 || budget != si.TaskRequestBudgetSeconds {
				return si.ErrInvariant
			}
			if now < started+budget {
				return si.ErrConflict
			}
		}
		events, e := sourceEvents(r, "task_submit_pending", now)
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET submit_attempted=1,submit_started_at=?,submit_budget_seconds=?,phase='task_submit_pending',events_json=?,revision=revision+1,updated_at=? WHERE id=?`, started, budget, events, now, r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, r.Intent.Scope, r.ID)
		return e
	})
	return
}
