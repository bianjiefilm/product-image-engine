package httpapi

import (
	"bytes"
	"strconv"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// The public projection is deliberately a whitelist. It never serializes Run,
// intent params, private provider observations, retry events or platform URLs.
func sourceView(r si.Run) map[string]any {
	payerSource := "delegation"
	if r.Intent.Scope.TenantID == r.Intent.Scope.PrincipalAccountID || r.Intent.Scope.TenantID == "personal/default:"+r.Intent.Scope.PrincipalAccountID {
		payerSource = "personal"
	}
	payerLabel := "组织委托账户"
	if payerSource == "personal" {
		payerLabel = "个人账户"
	}
	payment := map[string]any{"currency": "CNY", "payer_source": payerSource, "payer_label": payerLabel, "payer_account_id": r.Intent.Scope.PayerAccountID, "payer_user_id": r.Intent.Scope.UserID, "status": "unknown", "charged_minor": nil, "original_charged_minor": nil, "refund_amount_minor": nil, "refresh_status": "not_refreshed", "observation_time_known": false, "last_verified_at": nil}
	if r.LastError != "" {
		payment["refresh_status"] = "unknown"
	}
	if r.Bill != nil {
		payment["status"] = r.Bill.Status
		payment["hold_id"] = r.Bill.HoldID
		payment["charge_id"] = r.Bill.ChargeID
		payment["release_id"] = r.Bill.ReleaseID
		payment["refund_id"] = r.Bill.RefundID
		if r.Bill.ChargedMinor != nil {
			payment["charged_minor"] = strconv.FormatInt(*r.Bill.ChargedMinor, 10)
		}
	}
	if r.ChargedBill != nil && r.ChargedBill.ChargedMinor != nil {
		payment["original_charged_minor"] = strconv.FormatInt(*r.ChargedBill.ChargedMinor, 10)
		payment["original_charge_id"] = r.ChargedBill.ChargeID
	}
	view := map[string]any{"run_id": r.ID, "project_id": r.Intent.Scope.ProjectID, "request_key": r.Intent.RequestKey, "owner_version": r.Owner, "mode": r.Intent.Mode, "model": r.Intent.Model, "provider": r.Intent.Provider, "size": r.Intent.Size, "quantity": "1", "phase": r.Phase, "task_id": r.TaskID, "selected": r.Selected, "deleted": r.Deleted, "cancel_requested": r.CancelRequested, "confirmed": r.ConfirmationHash != "", "payment": payment, "quote": nil, "output": nil, "snapshot_updated_at": r.UpdatedAt, "fidelity": "not_applicable", "visual_quality": "unknown", "human_adoption": "NOT_RUN", "automatic_output_recovery_ready": false, "content_availability": "not_checked"}
	if r.Task != nil {
		view["task_phase"] = r.Task.Phase
		view["task_status"] = r.Task.Status
	}
	if r.Quote != nil {
		q := r.Quote
		view["quote"] = map[string]any{"quote_id": q.QuoteID, "quote_fingerprint": si.QuoteHash(*q), "usage_id": q.UsageID, "currency": q.Currency, "quantity": strconv.FormatInt(q.Quantity, 10), "unit_price_minor": strconv.FormatInt(q.UnitPriceMinor, 10), "amount_minor": strconv.FormatInt(q.AmountMinor, 10), "quoted_at_unix": q.QuotedAtUnix, "expires_at_unix": q.ExpiresAtUnix, "pricing_version": q.PricingVersion}
	}
	if r.Output != nil && !r.Deleted {
		view["output"] = map[string]any{"asset_id": r.Output.AssetID, "reference_id": r.Output.ReferenceID, "sha256": r.Output.SHA256, "content_type": r.Output.ContentType, "size_bytes": strconv.FormatInt(r.Output.SizeBytes, 10), "width_px": 1024, "height_px": 1024, "association_verified": true}
	}
	if r.Intent.Mode == si.ModePlateLock {
		// The Upload asset is only the unverified model plate. The result is the
		// derived composite under "plate"; never advertise the raw asset.
		view["output"] = nil
		view["fidelity"] = "frozen_sample_only"
	}
	return view
}

func sourceFrozenQuality(r si.Run) ([]byte, error) {
	// The original attachment report is deterministic and scoped. Require the
	// exact frozen report, rather than exporting a raw JSON document that can hide
	// duplicates, URLs or private strings in nominally public field names.
	expected, e := si.QualityReport(r)
	if e != nil || !bytes.Equal([]byte(r.QualityJSON), expected) {
		return nil, si.ErrInvariant
	}
	return []byte(r.QualityJSON), nil
}
