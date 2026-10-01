package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"math"
	"reflect"
	"strings"
)

func sourceObservationJSON(r si.Run) string {
	raw, _ := json.Marshal(sourceObservation{LastError: r.LastError, Bill: r.Bill, ChargedBill: r.ChargedBill, Task: r.Task, Output: r.Output})
	return string(raw)
}
func (s *Store) SaveSourceSettlementObservation(ctx context.Context, in si.Run, b si.BillFact, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		if e = si.ValidateSettlement(r, b); e != nil {
			return e
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		r.Bill = &b
		r.LastError = ""
		if b.Status == "charged" && r.ChargedBill == nil {
			snapshot := b
			r.ChargedBill = &snapshot
		}
		phase := r.Phase
		if b.Status == "refunded" {
			phase = "review_required"
			r.Selected = false
		}
		events := r.EventsJSON
		if phase != r.Phase {
			events, e = sourceEvents(r, phase, Now().Unix())
			if e != nil {
				return e
			}
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET observation_json=?,phase=?,selected=?,events_json=?,revision=revision+1,updated_at=? WHERE id=?`, sourceObservationJSON(r), phase, r.Selected, events, Now().Unix(), r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, r.Intent.Scope, r.ID)
		return e
	})
	return
}
func sourceQueueRelease(ctx context.Context, c *sql.Conn, r si.Run, o si.Output, reason string) error {
	if r.Owner != si.Owner || r.Task == nil || r.Task.Output == nil {
		return si.ErrInvariant
	}
	expected := *r.Task.Output
	expected.Status = "ready"
	expected.ReferenceState = "active"
	if expected != o {
		return si.ErrInvariant
	}
	scope, _ := json.Marshal(r.Intent.Scope)
	output, _ := json.Marshal(o)
	_, e := c.ExecContext(ctx, `INSERT INTO product_source_reference_outbox(id,run_id,scope_json,output_json,app_id,user_id,asset_id,project_id,reference_id,action,reason) VALUES(?,?,?,?,?,?,?,?,?,'release',?) ON CONFLICT(app_id,user_id,asset_id,project_id,reference_id,action) DO NOTHING`, newID("siro"), r.ID, string(scope), string(output), r.Intent.Scope.AppID, r.Intent.Scope.UserID, o.AssetID, o.ProjectID, o.ReferenceID, reason)
	return e
}
func (s *Store) AttachSourceOutput(ctx context.Context, in si.Run, o si.Output, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		if r.Deleted {
			return si.ErrConflict
		}
		if e = si.ValidateReadyOutput(r, o); e != nil {
			return e
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		var tenant, status string
		e = c.QueryRowContext(ctx, `SELECT tenant_id,status FROM projects WHERE id=?`, r.Intent.Scope.ProjectID).Scan(&tenant, &status)
		if errors.Is(e, sql.ErrNoRows) {
			return si.ErrNotFound
		}
		if e != nil {
			return e
		}
		if tenant != r.Intent.Scope.TenantID {
			return si.ErrInvariant
		}
		if status == "deleted" && !r.Deleted {
			return si.ErrNotFound
		}
		phase := "succeeded"
		if r.Output != nil && *r.Output != o {
			return si.ErrInvariant
		}
		r.Output = &o
		quality, e := si.QualityReport(r)
		if e != nil {
			return e
		}
		events, e := sourceEvents(r, phase, Now().Unix())
		if e != nil {
			return e
		}
		r.LastError = ""
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET observation_json=?,quality_json=?,phase=?,events_json=?,selected=?,lease_until=0,retry_at=0,revision=revision+1,updated_at=? WHERE id=?`, sourceObservationJSON(r), string(quality), phase, events, r.Selected, Now().Unix(), r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, r.Intent.Scope, r.ID)
		return e
	})
	return
}
func (s *Store) DeleteSourceOutput(ctx context.Context, scope si.Scope, id string) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceOnConn(ctx, c, scope, id)
		if e != nil {
			return e
		}
		if e = sourceProjectWritable(ctx, c, r); e != nil {
			return e
		}
		if r.Deleted {
			out = r
			return nil
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		phase := r.Phase
		if r.Output != nil {
			if e = sourceQueueRelease(ctx, c, r, *r.Output, "output_deleted"); e != nil {
				return e
			}
			phase = "output_deleted"
		}
		events, e := sourceEvents(r, phase, Now().Unix())
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET deleted=1,selected=0,phase=?,events_json=?,revision=revision+1,updated_at=? WHERE id=?`, phase, events, Now().Unix(), r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, scope, id)
		return e
	})
	return
}

const sourceOutboxColumns = `id,run_id,scope_json,output_json,app_id,user_id,asset_id,project_id,reference_id,action,state,reason,last_error,attempts,retry_at,lease_epoch,lease_until`

func scanSourceOutbox(row interface{ Scan(...any) error }) (si.Outbox, error) {
	var o si.Outbox
	var scope, output, app, user, asset, project, ref, action string
	e := row.Scan(&o.ID, &o.RunID, &scope, &output, &app, &user, &asset, &project, &ref, &action, &o.State, &o.Reason, &o.LastError, &o.Attempts, &o.RetryAt, &o.LeaseEpoch, &o.LeaseUntil)
	if errors.Is(e, sql.ErrNoRows) {
		return o, si.ErrNotFound
	}
	if e != nil {
		return o, e
	}
	if decodeSourceSnapshot([]byte(scope), &o.Scope) != nil || decodeSourceSnapshot([]byte(output), &o.Output) != nil || !o.Scope.Valid() || action != "release" || o.Scope.AppID != app || o.Scope.UserID != user || o.Scope.ProjectID != project || o.Output.AssetID != asset || o.Output.ReferenceID != ref || o.Output.ProjectID != project || o.Output.AppID != app || o.Output.PrincipalID != user || o.Output.PrincipalType != "user" || o.Output.Status != "ready" || o.Output.ReferenceState != "active" || o.Output.ContentType != "image/png" || !sha256Pattern.MatchString(o.Output.SHA256) || o.Output.SizeBytes <= 0 || o.Output.SizeBytes > 16<<20 || o.State != "pending" && o.State != "done" {
		return si.Outbox{}, si.ErrInvariant
	}
	return o, nil
}
func sourceOutboxOnConn(ctx context.Context, c *sql.Conn, id string) (si.Outbox, error) {
	return scanSourceOutbox(c.QueryRowContext(ctx, `SELECT `+sourceOutboxColumns+` FROM product_source_reference_outbox WHERE id=?`, id))
}
func (s *Store) ListDueSourceOutbox(ctx context.Context, now int64, limit int) ([]si.Outbox, error) {
	if now <= 0 || limit <= 0 || limit > 10 {
		return nil, si.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+sourceOutboxColumns+` FROM product_source_reference_outbox WHERE state='pending' AND retry_at<=? AND lease_until<=? ORDER BY retry_at,id LIMIT ?`, now, now, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []si.Outbox{}
	for rows.Next() {
		item, e := scanSourceOutbox(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Store) ClaimSourceOutbox(ctx context.Context, id string, now, ttl int64) (out si.Outbox, claimed bool, err error) {
	if now <= 0 || ttl <= 0 || ttl > 60 || now > math.MaxInt64-ttl {
		return out, false, si.ErrInvalid
	}
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		item, e := sourceOutboxOnConn(ctx, c, id)
		if e != nil {
			return e
		}
		out = item
		if item.State != "pending" || item.LeaseUntil > now || item.RetryAt > now {
			return nil
		}
		if item.LeaseEpoch == math.MaxInt64 {
			return si.ErrInvariant
		}
		// The immutable row must still be associated with its original source owner.
		r, e := sourceOnConn(ctx, c, item.Scope, item.RunID)
		if e != nil {
			return e
		}
		if !r.Deleted || r.Task == nil || r.Task.Output == nil {
			return si.ErrInvariant
		}
		expected := *r.Task.Output
		expected.Status = "ready"
		expected.ReferenceState = "active"
		if expected != item.Output {
			return si.ErrInvariant
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_reference_outbox SET lease_epoch=lease_epoch+1,lease_until=? WHERE id=?`, now+ttl, id)
		if e != nil {
			return e
		}
		out, e = sourceOutboxOnConn(ctx, c, id)
		claimed = e == nil
		return e
	})
	return
}
func sourceOutboxLease(ctx context.Context, c *sql.Conn, item si.Outbox, epoch int64, released bool) (si.Outbox, error) {
	r, e := sourceOutboxOnConn(ctx, c, item.ID)
	if e != nil {
		return r, e
	}
	if epoch <= 0 || r.LeaseEpoch != epoch || r.LeaseUntil <= Now().Unix() || r.State != "pending" {
		return r, si.ErrConflict
	}
	if released {
		if item.Output.ReferenceState != "released" {
			return r, si.ErrInvariant
		}
		item.Output.ReferenceState = r.Output.ReferenceState
	}
	if !reflect.DeepEqual(r, item) {
		return r, si.ErrConflict
	}
	return r, nil
}
func (s *Store) SaveSourceOutboxError(ctx context.Context, item si.Outbox, epoch int64, code string, retry int64) error {
	if code == "" || len(code) > 128 || strings.ContainsAny(code, "\r\n") || retry < 0 {
		return si.ErrInvalid
	}
	return s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceOutboxLease(ctx, c, item, epoch, false)
		if e != nil {
			return e
		}
		if r.Attempts == math.MaxInt64 {
			return si.ErrInvariant
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_reference_outbox SET attempts=attempts+1,retry_at=?,last_error=? WHERE id=?`, retry, code, item.ID)
		return e
	})
}
func (s *Store) FinishSourceRelease(ctx context.Context, item si.Outbox, epoch int64) error {
	return s.withSourceWrite(ctx, func(c *sql.Conn) error {
		if _, e := sourceOutboxLease(ctx, c, item, epoch, true); e != nil {
			return e
		}
		_, e := c.ExecContext(ctx, `UPDATE product_source_reference_outbox SET state='done',lease_until=0,retry_at=0,last_error='' WHERE id=?`, item.ID)
		return e
	})
}

// QueueDeletedSourceReference maintains only a tombstone's original reference.
// Full verified refund facts never grant usable output or reconstruct a charge.
func (s *Store) QueueDeletedSourceReference(ctx context.Context, in si.Run, o si.Output, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		if !r.Deleted || r.Selected || r.Output != nil || r.Bill == nil || (r.Bill.Status != "charged" && r.Bill.Status != "refunded") || si.ValidateSettlement(r, *r.Bill) != nil || r.Task == nil || r.Task.Status != "succeeded" || r.Task.Output == nil {
			return si.ErrInvariant
		}
		expected := *r.Task.Output
		expected.Status = "ready"
		expected.ReferenceState = "active"
		if o != expected {
			return si.ErrInvariant
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		if e = sourceQueueRelease(ctx, c, r, o, "late_output_after_delete"); e != nil {
			return e
		}
		events, e := sourceEvents(r, "output_deleted", Now().Unix())
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET phase='output_deleted',events_json=?,lease_until=0,retry_at=0,revision=revision+1,updated_at=? WHERE id=?`, events, Now().Unix(), r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, r.Intent.Scope, r.ID)
		return e
	})
	return
}
func (s *Store) SelectSourceOutput(ctx context.Context, in si.Run, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		if e = sourceProjectWritable(ctx, c, r); e != nil {
			return e
		}
		if r.Deleted || r.Output == nil || r.LastError != "" || si.ValidateReadyOutput(r, *r.Output) != nil {
			return si.ErrConflict
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET selected=1,lease_until=0,retry_at=0,revision=revision+1,updated_at=? WHERE id=?`, Now().Unix(), r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, r.Intent.Scope, r.ID)
		return e
	})
	return
}
