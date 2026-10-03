package sourceimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// QualityReport is ordinary-image evidence, never product fidelity or adoption.
func QualityReport(r Run) ([]byte, error) {
	if r.Owner != Owner || r.Intent.Mode != ModeTextGenerate && r.Intent.Mode != ModePlateLock {
		return nil, ErrInvalid
	}
	if _, e := Fingerprint(r.Intent); e != nil {
		return nil, e
	}
	if r.Quote != nil && ValidateQuote(r, *r.Quote) != nil || r.Task != nil && (r.TaskID != r.Task.ID || ValidateTaskFact(r, *r.Task) != nil) || r.TaskID != "" && r.Task == nil || r.Bill != nil && ValidateSettlement(r, *r.Bill) != nil || r.ChargedBill != nil && ValidateChargedHistory(r, *r.ChargedBill) != nil || r.Output != nil && ValidateOutputAssociation(r, *r.Output) != nil {
		return nil, ErrInvariant
	}
	if r.Intent.Mode == ModePlateLock {
		// The real v2 report needs the derived composite and lives in the
		// derivation record. This marks the run's own snapshot as not yet decided.
		return []byte(`{"report_version":"product-source-quality/v2","state":"derivation_pending"}`), nil
	}
	sum := sha256.Sum256([]byte(r.Intent.Prompt))
	report := map[string]any{"report_version": "product-source-quality/v1", "mode": r.Intent.Mode, "fidelity": "not_applicable", "visual_quality": "unknown", "human_adoption": "NOT_RUN", "run_id": r.ID, "model": r.Intent.Model, "provider": r.Intent.Provider, "pricing_version": r.Intent.PricingVersion, "prompt_sha256": hex.EncodeToString(sum[:]), "task_id": r.TaskID, "content_verification": "NOT_RUN", "limitations": []string{"ordinary conceptual image; product preservation is not verified", "no human adoption evidence; visual quality unknown"}}
	if r.Quote != nil {
		report["usage_id"] = r.Quote.UsageID
		report["quote_id"] = r.Quote.QuoteID
	}
	if r.ChargedBill != nil {
		report["original_charge_id"] = r.ChargedBill.ChargeID
	}
	if r.Bill != nil {
		report["latest_financial_status"] = r.Bill.Status
	}
	if r.Output != nil {
		report["asset_id"] = r.Output.AssetID
		report["reference_id"] = r.Output.ReferenceID
		report["output_sha256"] = r.Output.SHA256
	}
	return json.Marshal(report)
}
