package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"reflect"
	"strings"
)

func sourceLeaseOnConn(ctx context.Context, c *sql.Conn, in si.Run, rev, epoch int64) (si.Run, error) {
	r, e := sourceOnConn(ctx, c, in.Intent.Scope, in.ID)
	if e != nil {
		return r, e
	}
	if epoch <= 0 || r.LeaseEpoch != epoch || r.Revision != rev || in.Revision != rev || in.LeaseEpoch != epoch || r.LeaseUntil <= Now().Unix() || !reflect.DeepEqual(r, in) {
		return r, si.ErrConflict
	}
	return r, nil
}
func (s *Store) SaveSourceBillObservation(ctx context.Context, in si.Run, b si.BillFact, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		if r.Deleted || r.CancelRequested || r.SubmitAttempted || r.ConfirmationHash != "" || r.TaskID != "" {
			return si.ErrConflict
		}
		if e = si.ValidateQuoteBill(r, b); e != nil {
			return e
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		obs, _ := json.Marshal(sourceObservation{Bill: &b, Task: r.Task, Output: r.Output})
		now := Now().Unix()
		events, e := sourceEvents(r, "quote_pending", now)
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET usage_id=?,observation_json=?,phase='quote_pending',events_json=?,revision=revision+1,updated_at=? WHERE id=?`, b.UsageID, string(obs), events, now, r.ID)
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
func (s *Store) SaveSourceQuoteUnderLease(ctx context.Context, in si.Run, q si.Quote, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		if r.Deleted || r.CancelRequested || r.SubmitAttempted || r.ConfirmationHash != "" {
			return si.ErrConflict
		}
		if si.ValidateQuote(r, q) != nil || r.Bill == nil || r.Bill.UsageID != q.UsageID {
			return si.ErrInvariant
		}
		if r.Bill.Quote != nil && *r.Bill.Quote != q {
			return si.ErrConflict
		}
		if r.Quote != nil && *r.Quote != q {
			return si.ErrConflict
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		now := Now().Unix()
		raw, _ := json.Marshal(q)
		events, e := sourceEvents(r, "quoted", now)
		if e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET usage_id=?,quote_json=?,phase='quoted',events_json=?,lease_until=0,retry_at=0,revision=revision+1,updated_at=? WHERE id=?`, q.UsageID, string(raw), events, now, r.ID)
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
func privateSourceTenant(tenant string) bool {
	if strings.HasPrefix(tenant, "personal/default:") {
		return true
	}
	if len(tenant) != 21 || !strings.HasPrefix(tenant, "acct_") {
		return false
	}
	for _, v := range tenant[5:] {
		if !(v >= '0' && v <= '9' || v >= 'a' && v <= 'f') {
			return false
		}
	}
	return true
}
func (s *Store) ConfirmSourceRunForProject(ctx context.Context, in si.Run, hash string, now, rev, epoch int64) (out si.Run, err error) {
	err = s.withSourceWrite(ctx, func(c *sql.Conn) error {
		r, e := sourceLeaseOnConn(ctx, c, in, rev, epoch)
		if e != nil {
			return e
		}
		scope := r.Intent.Scope
		var tenant, creator, status string
		e = c.QueryRowContext(ctx, `SELECT tenant_id,created_by,status FROM projects WHERE id=? AND tenant_id=?`, scope.ProjectID, scope.TenantID).Scan(&tenant, &creator, &status)
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
		if r.Quote == nil || hash != si.QuoteHash(*r.Quote) || r.Deleted || r.CancelRequested {
			return si.ErrConflict
		}
		if r.ConfirmationHash == "" && (r.Phase != "quoted" || r.SubmitAttempted || now < r.Quote.QuotedAtUnix || now >= r.Quote.ExpiresAtUnix) {
			return si.ErrConflict
		}
		if now <= 0 {
			return si.ErrInvalid
		}
		if e = sourceRevision(r); e != nil {
			return e
		}
		confirmed := r.ConfirmedAt
		if confirmed == 0 {
			confirmed = now
		}
		phase := r.Phase
		events := r.EventsJSON
		if r.ConfirmationHash == "" {
			phase = "confirmed"
			events, e = sourceEvents(r, "confirmed", now)
			if e != nil {
				return e
			}
		}
		obs, _ := json.Marshal(sourceObservation{Bill: r.Bill, Task: r.Task, Output: r.Output})
		_, e = c.ExecContext(ctx, `UPDATE product_source_runs SET confirmation_hash=?,confirmed_at=?,phase=?,events_json=?,observation_json=?,lease_until=0,retry_at=0,revision=revision+1,updated_at=? WHERE id=?`, hash, confirmed, phase, events, string(obs), now, r.ID)
		if e != nil {
			return e
		}
		out, e = sourceOnConn(ctx, c, scope, r.ID)
		return e
	})
	return
}
