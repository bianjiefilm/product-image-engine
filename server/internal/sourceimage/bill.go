package sourceimage

// ValidateQuoteBill validates a pre-dispatch observation, never a funding grant.
// The transport adapter also validates the full public wire contract. This pure
// boundary binds local ports and persisted facts to the original immutable run.
func ValidateQuoteBill(r Run, f BillFact) error {
	if !identifier(f.UsageID) || f.Scope != r.Intent.Scope || f.UsageKey != r.UsageKey || f.Capability != r.Intent.Capability || f.Quantity != r.Intent.Quantity || f.PricingVersion != r.Intent.PricingVersion || f.BusinessRef != r.BusinessRef {
		return ErrInvariant
	}
	if f.HoldID != "" || f.ChargeID != "" || f.ReleaseID != "" || f.RefundID != "" || f.ChargedMinor != nil {
		return ErrInvariant
	}
	if r.Bill != nil && (r.Bill.UsageID != f.UsageID || r.Bill.Quote != nil && (f.Quote == nil || *r.Bill.Quote != *f.Quote)) {
		return ErrInvariant
	}
	if f.Status == "ingested" {
		if f.Quote != nil || r.Quote != nil {
			return ErrInvariant
		}
		return nil
	}
	if f.Status != "quoted" || f.Quote == nil || f.Quote.UsageID != f.UsageID || ValidateQuote(r, *f.Quote) != nil {
		return ErrInvariant
	}
	if r.Quote != nil && *r.Quote != *f.Quote {
		return ErrInvariant
	}
	return nil
}
