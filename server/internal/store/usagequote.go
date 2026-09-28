package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// UsageQuote 是一次产品图用量报价的确认状态。
// 金额不落库：presentation 只保留给用户看的句子，余额留在统一 Billing。
type UsageQuote struct {
	ID              string
	StorageTenant   string
	ProjectID       string
	PayerKind       string
	PayerAccountRef string
	PayerDisplay    string
	ImageCount      int
	Resolution      string
	Capability      string
	PricingVersion  string
	Fingerprint     string
	QuoteRef        string
	Presentation    string
	IdempotencyKey  string
	TaskID          string
	ChargeRef       string
	Confirmed       bool
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (q UsageQuote) validate() error {
	if strings.TrimSpace(q.StorageTenant) == "" || strings.TrimSpace(q.ProjectID) == "" ||
		strings.TrimSpace(q.PayerAccountRef) == "" || strings.TrimSpace(q.Fingerprint) == "" ||
		strings.TrimSpace(q.IdempotencyKey) == "" || q.ImageCount <= 0 {
		return ErrValidation
	}
	return nil
}

// SaveUsageQuote 按租户和工程保存当前报价。指纹变化时取消确认。
func (s *Store) SaveUsageQuote(ctx context.Context, in UsageQuote) (UsageQuote, error) {
	if err := in.validate(); err != nil {
		return UsageQuote{}, err
	}
	existing, err := s.GetUsageQuoteByProject(ctx, in.StorageTenant, in.ProjectID)
	if err != nil && err != ErrNotFound {
		return UsageQuote{}, err
	}
	now := Now()
	if err == ErrNotFound {
		in.ID = newID("uq")
		in.CreatedAt = now
		in.UpdatedAt = now
		in.Confirmed = false
		if in.Presentation == "" {
			in.Presentation = "待确认"
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO image_usage_quotes (
				id, storage_tenant, project_id, payer_kind, payer_account_ref, payer_display,
				image_count, resolution, capability, pricing_version, fingerprint, quote_ref,
				presentation, idempotency_key, task_id, charge_ref, confirmed, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
			in.ID, in.StorageTenant, in.ProjectID, in.PayerKind, in.PayerAccountRef, in.PayerDisplay,
			in.ImageCount, in.Resolution, in.Capability, in.PricingVersion, in.Fingerprint, in.QuoteRef,
			in.Presentation, in.IdempotencyKey, in.TaskID, in.ChargeRef, in.Status,
			in.CreatedAt.Format(time.RFC3339), in.UpdatedAt.Format(time.RFC3339))
		if err != nil {
			return UsageQuote{}, err
		}
		return in, nil
	}
	in.ID = existing.ID
	in.CreatedAt = existing.CreatedAt
	in.UpdatedAt = now
	if in.Fingerprint != existing.Fingerprint {
		in.Confirmed = false
		if in.Status == "" || in.Status == "confirmed" {
			in.Status = "stale"
		}
	} else if in.Status == "" {
		in.Confirmed = existing.Confirmed
		in.Status = existing.Status
	}
	if in.TaskID == "" {
		in.TaskID = existing.TaskID
	}
	if in.ChargeRef == "" {
		in.ChargeRef = existing.ChargeRef
	}
	if in.Presentation == "" {
		in.Presentation = existing.Presentation
	}
	confirmed := 0
	if in.Confirmed {
		confirmed = 1
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE image_usage_quotes SET
			payer_kind=?, payer_account_ref=?, payer_display=?, image_count=?, resolution=?,
			capability=?, pricing_version=?, fingerprint=?, quote_ref=?, presentation=?,
			idempotency_key=?, task_id=?, charge_ref=?, confirmed=?, status=?, updated_at=?
		WHERE id=? AND storage_tenant=?`,
		in.PayerKind, in.PayerAccountRef, in.PayerDisplay, in.ImageCount, in.Resolution,
		in.Capability, in.PricingVersion, in.Fingerprint, in.QuoteRef, in.Presentation,
		in.IdempotencyKey, in.TaskID, in.ChargeRef, confirmed, in.Status, in.UpdatedAt.Format(time.RFC3339),
		in.ID, in.StorageTenant)
	return in, err
}

func (s *Store) GetUsageQuote(ctx context.Context, tenant, id string) (UsageQuote, error) {
	row := s.db.QueryRowContext(ctx, usageQuoteSelect+` WHERE id=? AND storage_tenant=?`, id, tenant)
	return scanUsageQuote(row)
}

func (s *Store) GetUsageQuoteByProject(ctx context.Context, tenant, projectID string) (UsageQuote, error) {
	row := s.db.QueryRowContext(ctx, usageQuoteSelect+` WHERE storage_tenant=? AND project_id=?`, tenant, projectID)
	return scanUsageQuote(row)
}

const usageQuoteSelect = `
	SELECT id, storage_tenant, project_id, payer_kind, payer_account_ref, payer_display,
		image_count, resolution, capability, pricing_version, fingerprint, quote_ref,
		presentation, idempotency_key, task_id, charge_ref, confirmed, status, created_at, updated_at
	FROM image_usage_quotes`

func scanUsageQuote(row *sql.Row) (UsageQuote, error) {
	var q UsageQuote
	var confirmed int
	var created, updated string
	err := row.Scan(
		&q.ID, &q.StorageTenant, &q.ProjectID, &q.PayerKind, &q.PayerAccountRef, &q.PayerDisplay,
		&q.ImageCount, &q.Resolution, &q.Capability, &q.PricingVersion, &q.Fingerprint, &q.QuoteRef,
		&q.Presentation, &q.IdempotencyKey, &q.TaskID, &q.ChargeRef, &confirmed, &q.Status, &created, &updated,
	)
	if err == sql.ErrNoRows {
		return UsageQuote{}, ErrNotFound
	}
	if err != nil {
		return UsageQuote{}, err
	}
	q.Confirmed = confirmed == 1
	q.CreatedAt, _ = time.Parse(time.RFC3339, created)
	q.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return q, nil
}
