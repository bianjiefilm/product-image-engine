package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

type sourceObservation struct {
	LastError   string
	Bill        *si.BillFact
	ChargedBill *si.BillFact
	Task        *si.TaskFact
	Output      *si.Output
}
type sourceEvent struct {
	Phase string
	At    int64
}

const sourceColumns = `id,owner,app_id,user_id,tenant_id,project_id,payer_account_id,principal_account_id,request_key,fingerprint,intent_json,usage_key,task_key,business_ref,usage_id,task_id,quote_json,confirmation_hash,confirmed_at,phase,observation_json,events_json,quality_json,revision,lease_epoch,lease_until,retry_at,attempts,deleted,selected,cancel_requested,submit_attempted,updated_at,submit_started_at,submit_budget_seconds`
const sourceScopeSQL = `app_id=? AND user_id=? AND tenant_id=? AND project_id=? AND payer_account_id=? AND principal_account_id=?`

func sourceScopeArgs(s si.Scope) []any {
	return []any{s.AppID, s.UserID, s.TenantID, s.ProjectID, s.PayerAccountID, s.PrincipalAccountID}
}
func scanSource(row interface{ Scan(...any) error }) (si.Run, error) {
	var r si.Run
	var scope si.Scope
	var requestKey, intentRaw, observationRaw string
	var usage, task, quote sql.NullString
	err := row.Scan(&r.ID, &r.Owner, &scope.AppID, &scope.UserID, &scope.TenantID, &scope.ProjectID, &scope.PayerAccountID, &scope.PrincipalAccountID, &requestKey, &r.Fingerprint, &intentRaw, &r.UsageKey, &r.TaskKey, &r.BusinessRef, &usage, &task, &quote, &r.ConfirmationHash, &r.ConfirmedAt, &r.Phase, &observationRaw, &r.EventsJSON, &r.QualityJSON, &r.Revision, &r.LeaseEpoch, &r.LeaseUntil, &r.RetryAt, &r.Attempts, &r.Deleted, &r.Selected, &r.CancelRequested, &r.SubmitAttempted, &r.UpdatedAt, &r.SubmitStartedAt, &r.SubmitBudgetSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return r, si.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if r.Owner != si.Owner || json.Unmarshal([]byte(intentRaw), &r.Intent) != nil || r.Intent.Scope != scope || r.Intent.RequestKey != requestKey {
		return si.Run{}, si.ErrInvariant
	}
	fp, err := si.Fingerprint(r.Intent)
	if err != nil || fp != r.Fingerprint || r.UsageKey == "" || r.TaskKey == "" || r.BusinessRef != "product-image:run:"+r.ID {
		return si.Run{}, si.ErrInvariant
	}
	var obs sourceObservation
	if decodeSourceObservation([]byte(observationRaw), &obs) != nil {
		return si.Run{}, si.ErrInvariant
	}
	r.LastError, r.Bill, r.Task, r.Output = obs.LastError, obs.Bill, obs.Task, obs.Output
	r.ChargedBill = obs.ChargedBill
	if quote.Valid {
		r.Quote = &si.Quote{}
		if json.Unmarshal([]byte(quote.String), r.Quote) != nil || si.ValidateQuote(r, *r.Quote) != nil || !usage.Valid || usage.String != r.Quote.UsageID {
			return si.Run{}, si.ErrInvariant
		}
	} else if usage.Valid {
		if r.Bill == nil || r.Bill.UsageID != usage.String || si.ValidateQuoteBill(r, *r.Bill) != nil {
			return si.Run{}, si.ErrInvariant
		}
	}
	if r.Bill != nil {
		b := r.Bill
		if !usage.Valid || b.UsageID != usage.String || b.Scope != r.Intent.Scope || b.UsageKey != r.UsageKey || b.Capability != r.Intent.Capability || b.PricingVersion != r.Intent.PricingVersion || b.Quantity != r.Intent.Quantity || b.BusinessRef != r.BusinessRef {
			return si.Run{}, si.ErrInvariant
		}
		if b.Quote != nil && (si.ValidateQuote(r, *b.Quote) != nil || b.Quote.UsageID != usage.String || r.Quote != nil && *r.Quote != *b.Quote) {
			return si.Run{}, si.ErrInvariant
		}
	}
	if r.ConfirmationHash != "" && (r.Quote == nil || r.ConfirmationHash != si.QuoteHash(*r.Quote) || r.ConfirmedAt <= 0) {
		return si.Run{}, si.ErrInvariant
	}
	if !(r.SubmitStartedAt == 0 && r.SubmitBudgetSeconds == 0 || r.SubmitStartedAt > 0 && r.SubmitStartedAt <= math.MaxInt64-si.TaskRequestBudgetSeconds && r.SubmitBudgetSeconds == si.TaskRequestBudgetSeconds && r.SubmitAttempted) {
		return si.Run{}, si.ErrInvariant
	}
	r.TaskID = task.String
	if r.Task != nil && (r.Task.ID != r.TaskID || si.ValidateTaskFact(r, *r.Task) != nil) || r.TaskID != "" && r.Task == nil {
		return si.Run{}, si.ErrInvariant
	}
	if r.Task != nil && r.Bill != nil && (r.Bill.Status == "held" || r.Bill.Status == "charged" || r.Bill.Status == "refunded" || r.Bill.Status == "released" || r.Bill.Status == "reconciling") && si.ValidateSettlement(r, *r.Bill) != nil {
		return si.Run{}, si.ErrInvariant
	}
	if r.ChargedBill != nil && (si.ValidateChargedHistory(r, *r.ChargedBill) != nil || r.Bill == nil || (r.Bill.Status != "charged" && r.Bill.Status != "refunded") || si.ValidateSettlement(r, *r.Bill) != nil) {
		return si.Run{}, si.ErrInvariant
	}
	if r.Output != nil && si.ValidateOutputAssociation(r, *r.Output) != nil {
		return si.Run{}, si.ErrInvariant
	}
	return r, nil
}
func (s *Store) withSourceWrite(ctx context.Context, fn func(*sql.Conn) error) error {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err = c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer c.ExecContext(context.Background(), "ROLLBACK")
	if err = fn(c); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "COMMIT")
	return err
}
func sourceOnConn(ctx context.Context, c *sql.Conn, scope si.Scope, id string) (si.Run, error) {
	args := append([]any{id}, sourceScopeArgs(scope)...)
	return scanSource(c.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE id=? AND `+sourceScopeSQL, args...))
}
func (s *Store) CreateSourceRun(ctx context.Context, in si.Intent) (si.Run, bool, error) {
	return s.createSourceRun(ctx, in, false)
}
func (s *Store) createSourceRun(ctx context.Context, in si.Intent, projectCheck bool) (out si.Run, duplicate bool, err error) {
	fp, err := si.Fingerprint(in)
	if err != nil {
		return out, false, err
	}
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		if projectCheck {
			if e := sourceProjectActive(ctx, c, in.Scope); e != nil {
				return e
			}
		}
		existing, e := scanSource(c.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE app_id=? AND user_id=? AND project_id=? AND request_key=?`, in.Scope.AppID, in.Scope.UserID, in.Scope.ProjectID, in.RequestKey))
		if e == nil {
			if existing.Fingerprint != fp {
				return si.ErrConflict
			}
			out = existing
			duplicate = true
			return nil
		}
		if !errors.Is(e, si.ErrNotFound) {
			return e
		}
		id := newID("sir")
		raw, _ := json.Marshal(in)
		now := Now().Unix()
		events, _ := json.Marshal([]sourceEvent{{"intent", now}})
		_, e = c.ExecContext(ctx, `INSERT INTO product_source_runs(id,owner,app_id,user_id,tenant_id,project_id,payer_account_id,principal_account_id,request_key,fingerprint,intent_json,usage_key,task_key,business_ref,created_at,updated_at,events_json) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, si.Owner, in.Scope.AppID, in.Scope.UserID, in.Scope.TenantID, in.Scope.ProjectID, in.Scope.PayerAccountID, in.Scope.PrincipalAccountID, in.RequestKey, fp, string(raw), "product-image:usage:"+id, "product-image:task:"+id, "product-image:run:"+id, now, now, string(events))
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, in.Scope, id)
		return e
	})
	return
}
func (s *Store) GetSourceRun(ctx context.Context, scope si.Scope, id string) (si.Run, error) {
	if !scope.Valid() {
		return si.Run{}, si.ErrInvalid
	}
	args := append([]any{id}, sourceScopeArgs(scope)...)
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE id=? AND `+sourceScopeSQL, args...))
}

// GetSourceRunForRecovery is for the internal coordinator only. HTTP must use scoped reads.
func (s *Store) GetSourceRunForRecovery(ctx context.Context, id string) (si.Run, error) {
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE id=?`, id))
}
func (s *Store) FindSourceRun(ctx context.Context, scope si.Scope, key string) (si.Run, error) {
	if !scope.Valid() {
		return si.Run{}, si.ErrInvalid
	}
	args := append(sourceScopeArgs(scope), key)
	return scanSource(s.db.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE `+sourceScopeSQL+` AND request_key=?`, args...))
}
func (s *Store) ListSourceRuns(ctx context.Context, scope si.Scope) ([]si.Run, error) {
	if !scope.Valid() {
		return nil, si.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE `+sourceScopeSQL+` ORDER BY created_at DESC,id DESC LIMIT 100`, sourceScopeArgs(scope)...)
	if err != nil {
		return nil, err
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
func sourceRevision(r si.Run) error {
	if r.Revision >= math.MaxInt64 {
		return si.ErrInvariant
	}
	return nil
}
func sourceEvents(r si.Run, phase string, now int64) (string, error) {
	var events []sourceEvent
	if json.Unmarshal([]byte(r.EventsJSON), &events) != nil {
		return "", si.ErrInvariant
	}
	events = append(events, sourceEvent{phase, now})
	raw, err := json.Marshal(events)
	return string(raw), err
}
func (s *Store) SaveSourceQuote(ctx context.Context, scope si.Scope, id string, q si.Quote) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceOnConn(ctx, c, scope, id)
		if e != nil {
			return e
		}
		if e = si.ValidateQuote(r, q); e != nil {
			return e
		}
		if r.Quote != nil {
			if *r.Quote != q {
				return si.ErrConflict
			}
			out = r
			return nil
		}
		if r.Deleted || r.CancelRequested || r.SubmitAttempted || r.Phase != "intent" {
			return si.ErrConflict
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		raw, _ := json.Marshal(q)
		now := Now().Unix()
		events, e := sourceEvents(r, "quoted", now)
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET usage_id=?,quote_json=?,phase='quoted',events_json=?,updated_at=?,revision=revision+1 WHERE id=?`, q.UsageID, string(raw), events, now, id)
		if e != nil {
			if isUniqueErr(e) {
				return si.ErrConflict
			}
			return e
		}
		out, e = sourceOnConn(ctx, c, scope, id)
		return e
	})
	return
}
func (s *Store) ConfirmSourceRun(ctx context.Context, scope si.Scope, id, hash string, now int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceOnConn(ctx, c, scope, id)
		if e != nil {
			return e
		}
		if r.Quote == nil || hash != si.QuoteHash(*r.Quote) || r.Deleted || r.CancelRequested {
			return si.ErrConflict
		}
		if r.ConfirmationHash != "" {
			out = r
			return nil
		}
		if now < r.Quote.QuotedAtUnix || now >= r.Quote.ExpiresAtUnix || r.Phase != "quoted" || r.SubmitAttempted {
			return si.ErrConflict
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		events, e := sourceEvents(r, "confirmed", now)
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET confirmation_hash=?,confirmed_at=?,phase='confirmed',events_json=?,updated_at=?,revision=revision+1 WHERE id=?`, hash, now, events, now, id)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, scope, id)
		return e
	})
	return
}
func (s *Store) ClaimSourceRun(ctx context.Context, scope si.Scope, id string, now, ttl int64) (out si.Run, claimed bool, err error) {
	if now <= 0 || ttl <= 0 || ttl > 60 || now > math.MaxInt64-ttl {
		return out, false, si.ErrInvalid
	}
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceOnConn(ctx, c, scope, id)
		if e != nil {
			return e
		}
		out = r
		if r.LeaseUntil > now {
			return nil
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		if r.LeaseEpoch == math.MaxInt64 {
			return si.ErrInvariant
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET lease_epoch=lease_epoch+1,lease_until=?,revision=revision+1,updated_at=? WHERE id=?`, now+ttl, now, id)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, scope, id)
		claimed = e == nil
		return e
	})
	return
}

// SaveSourceObservation permits fenced retry metadata and verified original
// Task facts. The typed submit marker owns the first request budget. Bill/quote/confirmation/output ownership stays
// with its dedicated operations; caller summaries cannot grant those facts.
func (s *Store) SaveSourceObservation(ctx context.Context, in si.Run, revision, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := scanSource(c.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE id=?`, in.ID))
		if e != nil {
			return e
		}
		if epoch <= 0 || r.LeaseUntil <= Now().Unix() || r.Revision != revision || r.LeaseEpoch != epoch || in.Revision != revision || in.LeaseEpoch != epoch {
			return si.ErrConflict
		}
		if e = validateSourceObservation(ctx, c, r, in); e != nil {
			return e
		}
		if len(in.LastError) > 128 || strings.ContainsAny(in.LastError, "\r\n") || in.RetryAt < 0 || in.Attempts < r.Attempts {
			return si.ErrInvalid
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		obs, _ := json.Marshal(sourceObservation{LastError: in.LastError, Bill: r.Bill, ChargedBill: r.ChargedBill, Task: in.Task, Output: r.Output})
		now := Now().Unix()
		events := r.EventsJSON
		if in.Phase != r.Phase {
			events, e = sourceEvents(r, in.Phase, now)
			if e != nil {
				return e
			}
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET observation_json=?,task_id=?,phase=?,submit_attempted=?,lease_until=?,events_json=?,retry_at=?,attempts=?,revision=revision+1,updated_at=? WHERE id=?`, string(obs), sourceTaskIDValue(in.TaskID), in.Phase, in.SubmitAttempted, in.LeaseUntil, events, in.RetryAt, in.Attempts, now, in.ID)
		if isUniqueErr(e) {
			return si.ErrConflict
		}
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, r.Intent.Scope, r.ID)
		return e
	})
	return
}
