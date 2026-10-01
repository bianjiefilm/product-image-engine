package sourceimage

// ValidateSettlement binds a read-only financial observation to the original
// Task/Usage/quote. It grants no funds or output availability.
func ValidateSettlement(r Run, b BillFact) error {
	if r.Owner != Owner || r.Task == nil || r.TaskID != r.Task.ID || ValidateTaskFact(r, *r.Task) != nil || r.Quote == nil || r.ConfirmationHash != QuoteHash(*r.Quote) || !identifier(b.UsageID) || b.Scope != r.Intent.Scope || b.UsageID != r.Quote.UsageID || b.UsageKey != r.UsageKey || b.Capability != r.Intent.Capability || b.Quantity != r.Intent.Quantity || b.PricingVersion != r.Intent.PricingVersion || b.BusinessRef != r.BusinessRef || b.Quote == nil || *b.Quote != *r.Quote || ValidateQuote(r, *b.Quote) != nil {
		return ErrInvariant
	}
	for _, id := range []string{b.HoldID, b.ChargeID, b.ReleaseID, b.RefundID} {
		if id != "" && !identifier(id) {
			return ErrInvariant
		}
	}
	for _, old := range []*BillFact{r.Bill, r.ChargedBill} {
		if old == nil {
			continue
		}
		if old.UsageID != b.UsageID || old.HoldID != "" && old.HoldID != b.HoldID || old.ChargeID != "" && old.ChargeID != b.ChargeID || old.ReleaseID != "" && old.ReleaseID != b.ReleaseID || old.RefundID != "" && old.RefundID != b.RefundID {
			return ErrInvariant
		}
		if old.Status == "refunded" && b.Status != "refunded" || old.Status == "released" && b.Status != "released" {
			return ErrInvariant
		}
	}
	t := r.Task
	if t.HoldID != "" && t.HoldID != b.HoldID || t.ChargeID != "" && t.ChargeID != b.ChargeID || t.ReleaseID != "" && t.ReleaseID != b.ReleaseID {
		return ErrInvariant
	}
	switch b.Status {
	case "quoted":
		if b.HoldID != "" || b.ChargeID != "" || b.ReleaseID != "" || b.RefundID != "" || b.ChargedMinor != nil {
			return ErrInvariant
		}
	case "held", "reconciling":
		if b.ChargeID != "" || b.ReleaseID != "" || b.RefundID != "" || b.ChargedMinor != nil {
			return ErrInvariant
		}
		if b.Status == "held" && b.HoldID == "" {
			return ErrInvariant
		}
	case "charged", "refunded":
		if b.HoldID == "" || b.ChargeID == "" || b.HoldID != t.HoldID || b.ChargeID != t.ChargeID || b.ReleaseID != "" || b.ChargedMinor == nil || *b.ChargedMinor != r.Quote.AmountMinor {
			return ErrInvariant
		}
		if b.Status == "charged" && b.RefundID != "" || b.Status == "refunded" && b.RefundID == "" {
			return ErrInvariant
		}
	case "released":
		if b.HoldID == "" || b.ReleaseID == "" || b.HoldID != t.HoldID || b.ReleaseID != t.ReleaseID || b.ChargeID != "" || b.RefundID != "" || b.ChargedMinor != nil {
			return ErrInvariant
		}
	default:
		return ErrInvariant
	}
	return nil
}
func ValidateChargedHistory(r Run, b BillFact) error {
	check := r
	check.Bill = nil
	check.ChargedBill = nil
	if b.Status != "charged" {
		return ErrInvariant
	}
	return ValidateSettlement(check, b)
}
func ValidateReadyOutput(r Run, o Output) error {
	if r.Bill == nil || r.Bill.Status != "charged" {
		return ErrInvariant
	}
	return ValidateOutputAssociation(r, o)
}

// An association is historical Upload proof. A current refund blocks use while
// retaining that immutable proof for deletion and legacy-owner isolation.
func ValidateOutputAssociation(r Run, o Output) error {
	if r.Task == nil || r.Task.Status != "succeeded" || r.Task.Output == nil || r.Bill == nil || (r.Bill.Status != "charged" && r.Bill.Status != "refunded") || r.ChargedBill == nil || ValidateSettlement(r, *r.Bill) != nil || ValidateChargedHistory(r, *r.ChargedBill) != nil {
		return ErrInvariant
	}
	expected := *r.Task.Output
	expected.Status = "ready"
	expected.ReferenceState = "active"
	if o != expected {
		return ErrInvariant
	}
	return nil
}
