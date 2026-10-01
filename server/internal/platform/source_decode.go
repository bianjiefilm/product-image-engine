package platform

import (
	"bytes"
	"encoding/json"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"io"
	"reflect"
	"strings"
)

// DecodeSourceJSON preserves integer tokens and requires one spelling and one
// meaning for every object key, including nested arbitrary provider objects.
func DecodeSourceJSON(raw []byte, dst any) error {
	if len(raw) == 0 || len(raw) > 1<<20 || dst == nil {
		return si.ErrInvariant
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	count := 0
	var parse func(int) (any, error)
	parse = func(depth int) (any, error) {
		count++
		if depth > 32 || count > 10000 {
			return nil, si.ErrInvariant
		}
		tok, err := d.Token()
		if err != nil {
			return nil, si.ErrInvariant
		}
		if n, ok := tok.(json.Number); ok {
			if _, err := si.ParseMinor(json.RawMessage(n)); err != nil {
				return nil, si.ErrInvariant
			}
			return n, nil
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return tok, nil
		}
		switch delim {
		case '{':
			obj := map[string]any{}
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, si.ErrInvariant
				}
				key, ok := k.(string)
				if !ok || key != strings.ToLower(key) || seen[strings.ToLower(key)] {
					return nil, si.ErrInvariant
				}
				seen[strings.ToLower(key)] = true
				obj[key], err = parse(depth + 1)
				if err != nil {
					return nil, err
				}
			}
			if end, e := d.Token(); e != nil || end != json.Delim('}') {
				return nil, si.ErrInvariant
			}
			return obj, nil
		case '[':
			var arr []any
			for d.More() {
				v, e := parse(depth + 1)
				if e != nil {
					return nil, e
				}
				arr = append(arr, v)
			}
			if end, e := d.Token(); e != nil || end != json.Delim(']') {
				return nil, si.ErrInvariant
			}
			return arr, nil
		}
		return nil, si.ErrInvariant
	}
	v, err := parse(0)
	if err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		return si.ErrInvariant
	}
	if !sourceNullValid(v, reflect.TypeOf(dst)) {
		return si.ErrInvariant
	}
	normalized, err := json.Marshal(v)
	if err != nil || json.Unmarshal(normalized, dst) != nil {
		return si.ErrInvariant
	}
	return nil
}
func sourceNullValid(v any, t reflect.Type) bool {
	if t == nil {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			if child, exists := m[name]; exists && !sourceNullValid(child, f.Type) {
				return false
			}
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint64:
		if v == nil {
			return false
		}
	case reflect.Map:
		if m, ok := v.(map[string]any); ok {
			for _, child := range m {
				if !sourceNullValid(child, t.Elem()) {
					return false
				}
			}
		}
	}
	return true
}

func sourceObject(raw []byte, required, optional string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if DecodeSourceJSON(raw, &m) != nil || m == nil {
		return nil, si.ErrInvariant
	}
	allowed := map[string]bool{}
	for _, k := range strings.Fields(required) {
		if _, ok := m[k]; !ok {
			return nil, si.ErrInvariant
		}
		allowed[k] = true
	}
	for _, k := range strings.Fields(optional) {
		allowed[k] = true
	}
	for k, raw := range m {
		if !allowed[k] {
			return nil, si.ErrInvariant
		}
		if strings.HasSuffix(k, "_minor") || strings.HasSuffix(k, "_unix") || k == "quantity" || k == "size_bytes" || k == "active_reference_count" {
			if _, e := si.ParseMinor(raw); e != nil {
				return nil, si.ErrInvariant
			}
		}
		if strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_key") || strings.HasSuffix(k, "_version") || strings.HasSuffix(k, "_ref") || k == "status" || k == "phase" || k == "state" || k == "capability" || k == "provider" || k == "sha256" || k == "content_type" || k == "principal_type" || k == "result_json" || k == "reason" || k == "role" || k == "source" || k == "payer_source" || k == "member_role" {
			var v string
			if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
				return nil, si.ErrInvariant
			}
		}
	}
	return m, nil
}
func sourceString(m map[string]json.RawMessage, k string) string {
	var s string
	if json.Unmarshal(m[k], &s) != nil {
		return ""
	}
	return s
}
func sourceInt(m map[string]json.RawMessage, k string) (int64, error) {
	n, e := si.ParseMinor(m[k])
	if e != nil {
		return 0, si.ErrInvariant
	}
	return n, nil
}
func sourceBool(m map[string]json.RawMessage, k string) (bool, error) {
	if string(m[k]) != "true" && string(m[k]) != "false" {
		return false, si.ErrInvariant
	}
	return string(m[k]) == "true", nil
}
func sourcePresent(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }
func sourceUsage(raw []byte, r si.Run) (si.BillFact, error) {
	m, e := sourceObject(raw, "funding_version payer_user_id idempotency_key quote_id quoted_unit_price_minor quoted_at_unix quote_expires_at_unix usage_id app_id capability quantity pricing_version business_ref payer_account_id quoted_amount_minor hold_id status created_at_unix updated_at_unix", "")
	if e != nil {
		return si.BillFact{}, e
	}
	s := r.Intent.Scope
	if sourceString(m, "funding_version") != "source_reservation_v1" || sourceString(m, "app_id") != s.AppID || sourceString(m, "payer_account_id") != s.PayerAccountID || sourceString(m, "payer_user_id") != s.UserID || sourceString(m, "idempotency_key") != r.UsageKey || sourceString(m, "capability") != r.Intent.Capability || sourceString(m, "pricing_version") != r.Intent.PricingVersion || sourceString(m, "business_ref") != r.BusinessRef || sourceString(m, "usage_id") == "" {
		return si.BillFact{}, si.ErrInvariant
	}
	quantity, e := sourceInt(m, "quantity")
	if e != nil || quantity != r.Intent.Quantity {
		return si.BillFact{}, si.ErrInvariant
	}
	f := si.BillFact{Scope: s, UsageID: sourceString(m, "usage_id"), UsageKey: r.UsageKey, Capability: r.Intent.Capability, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Quantity: quantity, Status: sourceString(m, "status"), HoldID: sourceString(m, "hold_id")}
	switch f.Status {
	case "ingested", "quoted", "held", "charged", "refunded", "reconciling", "released":
	default:
		return si.BillFact{}, si.ErrInvariant
	}
	if r.Quote != nil && f.UsageID != r.Quote.UsageID {
		return si.BillFact{}, si.ErrInvariant
	}
	quoteID := sourceString(m, "quote_id")
	if quoteID != "" {
		q := si.Quote{UsageID: f.UsageID, UsageKey: f.UsageKey, QuoteID: quoteID, PricingVersion: f.PricingVersion, BusinessRef: f.BusinessRef, Currency: "CNY", Quantity: quantity}
		var err error
		q.UnitPriceMinor, err = sourceInt(m, "quoted_unit_price_minor")
		if err != nil {
			return f, err
		}
		q.AmountMinor, err = sourceInt(m, "quoted_amount_minor")
		if err != nil {
			return f, err
		}
		q.QuotedAtUnix, err = sourceInt(m, "quoted_at_unix")
		if err != nil {
			return f, err
		}
		q.ExpiresAtUnix, err = sourceInt(m, "quote_expires_at_unix")
		if err != nil {
			return f, err
		}
		if si.ValidateQuote(r, q) != nil || r.Quote != nil && *r.Quote != q {
			return f, si.ErrInvariant
		}
		f.Quote = &q
	}
	return f, nil
}
func sourceQuote(raw []byte, r si.Run, usageID string) (si.Quote, error) {
	m, e := sourceObject(raw, "quote_id quoted_at_unix expires_at_unix usage_id amount_minor unit_price_minor quantity pricing_version", "")
	if e != nil {
		return si.Quote{}, e
	}
	q := si.Quote{UsageID: sourceString(m, "usage_id"), UsageKey: r.UsageKey, QuoteID: sourceString(m, "quote_id"), PricingVersion: sourceString(m, "pricing_version"), BusinessRef: r.BusinessRef, Currency: "CNY"}
	for k, p := range map[string]*int64{"quoted_at_unix": &q.QuotedAtUnix, "expires_at_unix": &q.ExpiresAtUnix, "amount_minor": &q.AmountMinor, "unit_price_minor": &q.UnitPriceMinor, "quantity": &q.Quantity} {
		*p, e = sourceInt(m, k)
		if e != nil {
			return q, e
		}
	}
	if q.UsageID != usageID || si.ValidateQuote(r, q) != nil || r.Quote != nil && q != *r.Quote {
		return q, si.ErrInvariant
	}
	return q, nil
}
func validateSourceUsageReply(raw []byte, r si.Run) error {
	m, e := sourceObject(raw, "ok fact", "replayed")
	if e != nil {
		return e
	}
	ok, e := sourceBool(m, "ok")
	if e != nil || !ok {
		return si.ErrInvariant
	}
	_, e = sourceUsage(m["fact"], r)
	return e
}
func validateSourceQuoteReply(raw []byte, r si.Run) error {
	m, e := sourceObject(raw, "ok quote", "")
	if e != nil {
		return e
	}
	ok, e := sourceBool(m, "ok")
	if e != nil || !ok || r.Bill == nil {
		return si.ErrInvariant
	}
	_, e = sourceQuote(m["quote"], r, r.Bill.UsageID)
	return e
}
func decodeSourceBill(raw []byte, r si.Run) (si.BillFact, error) {
	envelope, e := sourceObject(raw, "ok facts", "")
	if e != nil {
		return si.BillFact{}, e
	}
	ok, e := sourceBool(envelope, "ok")
	if e != nil || !ok {
		return si.BillFact{}, si.ErrInvariant
	}
	m, e := sourceObject(envelope["facts"], "usage quote hold charge release refund", "")
	if e != nil {
		return si.BillFact{}, e
	}
	f, e := sourceUsage(m["usage"], r)
	if e != nil {
		return f, e
	}
	if sourcePresent(m["quote"]) {
		q, e := sourceQuote(m["quote"], r, f.UsageID)
		if e != nil || f.Quote == nil || q != *f.Quote {
			return f, si.ErrInvariant
		}
	} else if f.Quote != nil {
		return f, si.ErrInvariant
	}
	holdStatus := ""
	var holdSpent int64
	if sourcePresent(m["hold"]) {
		h, e := sourceObject(m["hold"], "hold_id usage_id funding_version payer_user_id quote_id status idempotency_key amount_minor spent_minor created_at_unix closed_at_unix allocations", "")
		if e != nil || f.Quote == nil || sourceString(h, "hold_id") != f.HoldID || f.HoldID == "" || sourceString(h, "usage_id") != f.UsageID || sourceString(h, "funding_version") != "source_reservation_v1" || sourceString(h, "payer_user_id") != r.Intent.Scope.UserID || sourceString(h, "quote_id") != f.Quote.QuoteID {
			return f, si.ErrInvariant
		}
		amount, e := sourceInt(h, "amount_minor")
		holdSpent, e = sourceInt(h, "spent_minor")
		holdStatus = sourceString(h, "status")
		if e != nil || amount != f.Quote.AmountMinor || (holdStatus != "open" && holdStatus != "spent" && holdStatus != "refunded" && holdStatus != "released") {
			return f, si.ErrInvariant
		}
	} else if f.HoldID != "" {
		return f, si.ErrInvariant
	}
	chargeSources := map[string]int64{}
	if sourcePresent(m["charge"]) {
		c, e := sourceObject(m["charge"], "charge_id usage_id payer_account_id app_id capability quantity pricing_version amount_minor promo_minor allowance_minor cash_minor deduction_version status reason created_at_unix idempotency_key allocations", "")
		if e != nil || f.Quote == nil || sourceString(c, "usage_id") != f.UsageID || sourceString(c, "payer_account_id") != r.Intent.Scope.PayerAccountID || sourceString(c, "app_id") != r.Intent.Scope.AppID || sourceString(c, "capability") != r.Intent.Capability || sourceString(c, "pricing_version") != r.Intent.PricingVersion || sourceString(c, "charge_id") == "" {
			return f, si.ErrInvariant
		}
		amount, e := sourceInt(c, "amount_minor")
		quantity, eq := sourceInt(c, "quantity")
		if e != nil || eq != nil || amount != f.Quote.AmountMinor || quantity != f.Quantity {
			return f, si.ErrInvariant
		}
		if sourceString(c, "status") != f.Status || (f.Status != "charged" && f.Status != "refunded") || holdSpent != amount || (holdStatus != "spent" && holdStatus != "refunded") {
			return f, si.ErrInvariant
		}
		remaining := amount
		for _, key := range []string{"promo_minor", "allowance_minor", "cash_minor"} {
			n, e := sourceInt(c, key)
			if e != nil || n > remaining {
				return f, si.ErrInvariant
			}
			chargeSources[key] = n
			remaining -= n
		}
		if remaining != 0 {
			return f, si.ErrInvariant
		}
		f.ChargeID = sourceString(c, "charge_id")
		f.ChargedMinor = &amount
	}
	if sourcePresent(m["release"]) {
		rel, e := sourceObject(m["release"], "release_id usage_id hold_id status idempotency_key reason resolution evidence_ref amount_minor released_at_unix", "")
		if e != nil || sourceString(rel, "usage_id") != f.UsageID || sourceString(rel, "hold_id") != f.HoldID || sourceString(rel, "release_id") == "" || f.Quote == nil {
			return f, si.ErrInvariant
		}
		amount, e := sourceInt(rel, "amount_minor")
		if e != nil || amount != f.Quote.AmountMinor || f.ChargeID != "" || f.Status != "released" || holdStatus != "released" || holdSpent != 0 || sourceString(rel, "status") != "released" || sourceString(rel, "idempotency_key") == "" || sourceString(rel, "evidence_ref") == "" {
			return f, si.ErrInvariant
		}
		switch sourceString(rel, "resolution") {
		case "not_dispatched", "provider_failed", "provider_canceled":
		default:
			return f, si.ErrInvariant
		}
		f.ReleaseID = sourceString(rel, "release_id")
	}
	if sourcePresent(m["refund"]) {
		ref, e := sourceObject(m["refund"], "refund_id charge_id idempotency_key reason promo_minor allowance_minor cash_minor created_at_unix", "")
		if e != nil || sourceString(ref, "charge_id") != f.ChargeID || f.ChargeID == "" || sourceString(ref, "refund_id") == "" {
			return f, si.ErrInvariant
		}
		if f.Status != "refunded" || holdStatus != "refunded" || sourceString(ref, "idempotency_key") == "" || f.Quote == nil {
			return f, si.ErrInvariant
		}
		remaining := f.Quote.AmountMinor
		for _, key := range []string{"promo_minor", "allowance_minor", "cash_minor"} {
			n, e := sourceInt(ref, key)
			if e != nil || n != chargeSources[key] || n > remaining {
				return f, si.ErrInvariant
			}
			remaining -= n
		}
		if remaining != 0 {
			return f, si.ErrInvariant
		}
		f.RefundID = sourceString(ref, "refund_id")
	}
	if f.Status == "charged" && (f.ChargeID == "" || f.ReleaseID != "" || f.RefundID != "") || f.Status == "refunded" && f.RefundID == "" || f.Status == "released" && f.ReleaseID == "" {
		return f, si.ErrInvariant
	}
	return f, nil
}
func decodeSourceTask(raw []byte, r si.Run) (si.TaskFact, error) {
	m, e := sourceObject(raw, "task_id funding_version app_id payer_account_id payer_user_id project_id idempotency_key capability provider status phase usage_id quote_id quantity unit_price_minor amount_minor pricing_version business_ref hold_id charge_id release_id provider_result_received output_ready result_json error_code next_retry_at created_at", "")
	if e != nil || r.Quote == nil {
		return si.TaskFact{}, si.ErrInvariant
	}
	s := r.Intent.Scope
	q := r.Quote
	for k, v := range map[string]string{"funding_version": "source_reservation_v1", "app_id": s.AppID, "payer_account_id": s.PayerAccountID, "payer_user_id": s.UserID, "project_id": s.ProjectID, "idempotency_key": r.TaskKey, "capability": r.Intent.Capability, "provider": r.Intent.Provider, "usage_id": q.UsageID, "quote_id": q.QuoteID, "pricing_version": q.PricingVersion, "business_ref": q.BusinessRef} {
		if sourceString(m, k) != v {
			return si.TaskFact{}, si.ErrInvariant
		}
	}
	for k, v := range map[string]int64{"quantity": q.Quantity, "unit_price_minor": q.UnitPriceMinor, "amount_minor": q.AmountMinor} {
		n, e := sourceInt(m, k)
		if e != nil || n != v {
			return si.TaskFact{}, si.ErrInvariant
		}
	}
	f := si.TaskFact{HoldID: sourceString(m, "hold_id"), ChargeID: sourceString(m, "charge_id"), ReleaseID: sourceString(m, "release_id"), ID: sourceString(m, "task_id"), IdempotencyKey: r.TaskKey, Phase: sourceString(m, "phase"), Status: sourceString(m, "status"), Provider: r.Intent.Provider, Capability: r.Intent.Capability, Scope: s, Quote: *q}
	if f.ID == "" || f.Phase == "" || r.TaskID != "" && r.TaskID != f.ID {
		return f, si.ErrInvariant
	}
	switch f.Status {
	case "queued", "running", "succeeded", "failed", "canceled":
	default:
		return f, si.ErrInvariant
	}
	ready, e := sourceBool(m, "output_ready")
	if e != nil {
		return f, e
	}
	if _, e = sourceBool(m, "provider_result_received"); e != nil {
		return f, e
	}
	result := sourceString(m, "result_json")
	if result != "" {
		out, e := sourceObject([]byte(result), "asset_id content_type sha256 size_bytes reference_id project_id", "")
		if e != nil {
			return f, e
		}
		size, e := sourceInt(out, "size_bytes")
		if e != nil || size <= 0 || size > 16<<20 || sourceString(out, "asset_id") == "" || sourceString(out, "reference_id") == "" || sourceString(out, "project_id") != s.ProjectID || sourceString(out, "content_type") != "image/png" || !sourceSHA(sourceString(out, "sha256")) {
			return f, si.ErrInvariant
		}
		f.Output = &si.Output{AssetID: sourceString(out, "asset_id"), ReferenceID: sourceString(out, "reference_id"), ProjectID: s.ProjectID, AppID: s.AppID, PrincipalType: "user", PrincipalID: s.UserID, SHA256: sourceString(out, "sha256"), ContentType: "image/png", SizeBytes: size}
	}
	if f.Output != nil && !ready || f.Status == "succeeded" && (!ready || f.Output == nil || f.HoldID == "" || f.ChargeID == "" || f.ReleaseID != "") {
		return f, si.ErrInvariant
	}
	return f, nil
}
func sourceSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}
