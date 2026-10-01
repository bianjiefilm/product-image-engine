package platform

import (
	"context"
	"encoding/json"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
	"net/http"
	"time"
)

type SourceClients struct {
	Bill    *sourceBillClient
	Tasks   *sourceTaskClient
	Context *sourceContextClient
	Assets  *sourceAssetClient
}
type sourceBillClient struct{ c *pc.Client }
type sourceTaskClient struct{ c *pc.Client }
type sourceContextClient struct{ c *pc.Client }
type sourceAssetClient struct {
	cfg  config.Config
	http *http.Client
}

func NewSourceClients(cfg config.Config, h *http.Client) (*SourceClients, error) {
	// Copy the caller's client: SDK injection preserves CheckRedirect otherwise.
	// Never repeat a credentialed request at a redirected authority.
	if h == nil {
		h = &http.Client{}
	}
	guarded := *h
	guarded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	h = &guarded

	if cfg.AppID == "" || cfg.AppID != cfg.IdentityAppID {
		return nil, si.ErrUnconfigured
	}
	tokens := map[string]bool{}
	for _, t := range []string{cfg.BillingToken, cfg.TaskToken, cfg.IdentityToken, cfg.UploadToken} {
		if t == "" || tokens[t] {
			return nil, si.ErrUnconfigured
		}
		tokens[t] = true
	}
	for _, base := range []string{cfg.BillingBaseURL, cfg.TaskBaseURL, cfg.IdentityBaseURL, cfg.UploadBaseURL} {
		if base == "" {
			return nil, si.ErrUnconfigured
		}
	}
	mk := func(token string) *pc.Client {
		return &pc.Client{Caller: pc.Caller{Mode: "app", AppID: cfg.AppID, Token: token}, Bases: pc.Bases{Identity: cfg.IdentityBaseURL, Billing: cfg.BillingBaseURL, Task: cfg.TaskBaseURL, Upload: cfg.UploadBaseURL}, HTTP: h}
	}
	return &SourceClients{Bill: &sourceBillClient{mk(cfg.BillingToken)}, Tasks: &sourceTaskClient{mk(cfg.TaskToken)}, Context: &sourceContextClient{mk(cfg.IdentityToken)}, Assets: &sourceAssetClient{cfg, h}}, nil
}
func sourceCall(ctx context.Context, c *pc.Client, call pc.Call, absentCode string) ([]byte, bool, error) {
	res, e := c.Call(ctx, call)
	if e != nil || !res.Sent {
		return nil, false, si.ErrUnavailable
	}
	if res.StatusCode == 404 && absentCode != "" {
		var m map[string]json.RawMessage
		if DecodeSourceJSON(res.Body, &m) != nil {
			return nil, false, si.ErrInvariant
		}
		code := sourceString(m, "code")
		if code == "" {
			var nested map[string]json.RawMessage
			if DecodeSourceJSON(m["error"], &nested) == nil {
				code = sourceString(nested, "code")
			}
		}
		if code == absentCode {
			return nil, true, nil
		}
		return nil, false, si.ErrInvariant
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, false, si.ErrUnavailable
	}
	return res.Body, false, nil
}
func sourcePayer(r si.Run) map[string]string {
	s := r.Intent.Scope
	return map[string]string{"app_id": s.AppID, "payer_account_id": s.PayerAccountID, "payer_user_id": s.UserID}
}
func sourceTaskScope(r si.Run) map[string]string {
	q := sourcePayer(r)
	q["project_id"] = r.Intent.Scope.ProjectID
	return q
}
func sourceRunValid(c *pc.Client, r si.Run) bool {
	_, e := si.Fingerprint(r.Intent)
	return e == nil && r.Owner == si.Owner && r.Intent.Scope.AppID == c.Caller.AppID && r.UsageKey != "" && r.TaskKey != "" && r.BusinessRef == "product-image:run:"+r.ID
}
func (c *sourceBillClient) Lookup(ctx context.Context, r si.Run) (si.BillFact, bool, error) {
	if !sourceRunValid(c.c, r) {
		return si.BillFact{}, false, si.ErrInvalid
	}
	q := sourcePayer(r)
	q["idempotency_key"] = r.UsageKey
	raw, absent, e := sourceCall(ctx, c.c, pc.Call{Operation: "billing.usage_lookup", Query: q}, "billing_unified_not_found")
	if e != nil || absent {
		return si.BillFact{}, false, e
	}
	f, e := decodeSourceBill(raw, r)
	return f, e == nil, e
}
func (c *sourceBillClient) Get(ctx context.Context, r si.Run) (si.BillFact, bool, error) {
	if !sourceRunValid(c.c, r) {
		return si.BillFact{}, false, si.ErrInvalid
	}
	usageID := ""
	if r.Quote != nil {
		if si.ValidateQuote(r, *r.Quote) != nil {
			return si.BillFact{}, false, si.ErrInvalid
		}
		usageID = r.Quote.UsageID
	}
	if r.Bill != nil {
		b := r.Bill
		idScope := r.Intent.Scope
		idScope.ProjectID = b.UsageID
		if !idScope.Valid() || b.Scope != r.Intent.Scope || b.UsageKey != r.UsageKey || b.Capability != r.Intent.Capability || b.PricingVersion != r.Intent.PricingVersion || b.Quantity != r.Intent.Quantity || b.BusinessRef != r.BusinessRef || usageID != "" && usageID != b.UsageID {
			return si.BillFact{}, false, si.ErrInvariant
		}
		if b.Quote != nil && (si.ValidateQuote(r, *b.Quote) != nil || b.Quote.UsageID != b.UsageID || r.Quote != nil && *b.Quote != *r.Quote) {
			return si.BillFact{}, false, si.ErrInvariant
		}
		usageID = b.UsageID
	}
	if usageID == "" {
		return si.BillFact{}, false, si.ErrInvalid
	}
	raw, absent, e := sourceCall(ctx, c.c, pc.Call{Operation: "billing.usage_get", PathParams: map[string]string{"id": usageID}, Query: sourcePayer(r)}, "billing_unified_not_found")
	if e != nil || absent {
		return si.BillFact{}, false, e
	}
	f, e := decodeSourceBill(raw, r)
	return f, e == nil, e
}

func (c *sourceBillClient) Usage(ctx context.Context, r si.Run) error {
	if !sourceRunValid(c.c, r) {
		return si.ErrInvalid
	}
	q := sourcePayer(r)
	m := map[string]any{}
	for k, v := range q {
		m[k] = v
	}
	m["funding_version"] = "source_reservation_v1"
	m["capability"] = r.Intent.Capability
	m["quantity"] = r.Intent.Quantity
	m["pricing_version"] = r.Intent.PricingVersion
	m["business_ref"] = r.BusinessRef
	m["idempotency_key"] = r.UsageKey
	raw, _ := json.Marshal(m)
	body, _, e := sourceCall(ctx, c.c, pc.Call{Operation: "billing.usage", Body: raw}, "")
	if e != nil {
		return e
	}
	return validateSourceUsageReply(body, r)
}
func (c *sourceBillClient) Quote(ctx context.Context, r si.Run) error {
	if !sourceRunValid(c.c, r) || r.Bill == nil || r.Bill.UsageID == "" {
		return si.ErrInvalid
	}
	m := map[string]any{}
	for k, v := range sourcePayer(r) {
		m[k] = v
	}
	m["funding_version"] = "source_reservation_v1"
	m["usage_id"] = r.Bill.UsageID
	raw, _ := json.Marshal(m)
	body, _, e := sourceCall(ctx, c.c, pc.Call{Operation: "billing.source_quote", Body: raw}, "")
	if e != nil {
		return e
	}
	return validateSourceQuoteReply(body, r)
}
func (c *sourceTaskClient) Lookup(ctx context.Context, r si.Run) (si.TaskFact, bool, error) {
	if !sourceRunValid(c.c, r) || r.Quote == nil {
		return si.TaskFact{}, false, si.ErrInvalid
	}
	q := sourceTaskScope(r)
	q["idempotency_key"] = r.TaskKey
	raw, absent, e := sourceCall(ctx, c.c, pc.Call{Operation: "task.source_lookup", Query: q}, "task_not_found")
	if e != nil || absent {
		return si.TaskFact{}, false, e
	}
	f, e := decodeSourceTask(raw, r)
	return f, e == nil, e
}
func (c *sourceTaskClient) Get(ctx context.Context, r si.Run) (si.TaskFact, bool, error) {
	if !sourceRunValid(c.c, r) || r.Quote == nil || r.TaskID == "" {
		return si.TaskFact{}, false, si.ErrInvalid
	}
	raw, absent, e := sourceCall(ctx, c.c, pc.Call{Operation: "task.source_get", PathParams: map[string]string{"id": r.TaskID}, Query: sourceTaskScope(r)}, "task_not_found")
	if e != nil || absent {
		return si.TaskFact{}, false, e
	}
	f, e := decodeSourceTask(raw, r)
	return f, e == nil, e
}
func (c *sourceTaskClient) Submit(ctx context.Context, r si.Run) error {
	if !sourceRunValid(c.c, r) || r.Quote == nil || si.ValidateQuote(r, *r.Quote) != nil {
		return si.ErrInvalid
	}
	q := r.Quote
	billing := map[string]any{"funding_version": "source_reservation_v1", "usage_id": q.UsageID, "usage_idempotency_key": q.UsageKey, "quote_id": q.QuoteID, "quantity": q.Quantity, "pricing_version": q.PricingVersion, "business_ref": q.BusinessRef, "unit_price_minor": q.UnitPriceMinor, "amount_minor": q.AmountMinor, "quoted_at_unix": q.QuotedAtUnix, "expires_at_unix": q.ExpiresAtUnix}
	m := map[string]any{}
	for k, v := range sourceTaskScope(r) {
		m[k] = v
	}
	m["capability"] = r.Intent.Capability
	m["provider"] = r.Intent.Provider
	m["idempotency_key"] = r.TaskKey
	m["params"] = map[string]any{"model": r.Intent.Model, "prompt": r.Intent.Prompt, "n": r.Intent.Quantity, "size": r.Intent.Size}
	m["billing"] = billing
	raw, _ := json.Marshal(m)
	body, _, e := sourceCall(ctx, c.c, pc.Call{Operation: "task.source_submit", Body: raw, QuotedAt: time.Unix(q.QuotedAtUnix, 0), QuoteTTL: 300 * time.Second}, "")
	if e != nil {
		return e
	}
	_, e = decodeSourceTask(body, r)
	return e
}
func (c *sourceTaskClient) mutate(ctx context.Context, r si.Run, op string) error {
	if !sourceRunValid(c.c, r) || r.TaskID == "" || r.Quote == nil {
		return si.ErrInvalid
	}
	raw, _, e := sourceCall(ctx, c.c, pc.Call{Operation: op, PathParams: map[string]string{"id": r.TaskID}, Query: sourceTaskScope(r)}, "")
	if e != nil {
		return e
	}
	_, e = decodeSourceTask(raw, r)
	return e
}
func (c *sourceTaskClient) Cancel(ctx context.Context, r si.Run) error {
	return c.mutate(ctx, r, "task.source_cancel")
}
func (c *sourceTaskClient) Reconcile(ctx context.Context, r si.Run) error {
	return c.mutate(ctx, r, "task.source_reconcile")
}
