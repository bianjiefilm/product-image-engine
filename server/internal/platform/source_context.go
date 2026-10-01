package platform

import (
	"context"
	"encoding/json"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

func (c *sourceContextClient) Resolve(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
	if q.AppID != c.c.Caller.AppID || q.UserID == "" || q.SourceRef != "" {
		return si.ContextFact{}, si.ErrInvalid
	}
	m := map[string]any{"app_id": q.AppID, "principal_id": q.UserID}
	if !q.ReadOnly {
		if q.SourceRef != "" {
			return si.ContextFact{}, si.ErrInvalid
		}
		if q.TenantID != "" {
			m["tenant_id"] = q.TenantID
		}
	}
	raw, _ := json.Marshal(m)
	body, _, e := sourceCall(ctx, c.c, pc.Call{Operation: "identity.context.resolve", Body: raw}, "")
	if e != nil {
		return si.ContextFact{}, e
	}
	return decodeSourceContext(body, q)
}
func decodeSourceContext(raw []byte, q si.ContextQuery) (si.ContextFact, error) {
	m, e := sourceObject(raw, "ok resolved reason context", "")
	if e != nil {
		return si.ContextFact{}, e
	}
	ok, e := sourceBool(m, "ok")
	if e != nil || !ok {
		return si.ContextFact{}, si.ErrInvariant
	}
	resolved, e := sourceBool(m, "resolved")
	if e != nil {
		return si.ContextFact{}, e
	}
	v, e := sourceObject(m["context"], "principal_id app_id switchability", "display_org current_role source_task_or_object payer_summary")
	if e != nil || sourceString(v, "principal_id") != q.UserID || sourceString(v, "app_id") != q.AppID {
		return si.ContextFact{}, si.ErrInvariant
	}
	f := si.ContextFact{Resolved: resolved, AppID: q.AppID, UserID: q.UserID, Reason: sourceString(m, "reason")}
	if !resolved {
		return f, nil
	}
	if sourcePresent(v["display_org"]) {
		org, e := sourceObject(v["display_org"], "tenant_id role source", "")
		if e != nil {
			return f, e
		}
		f.TenantID = sourceString(org, "tenant_id")
		f.Role = sourceString(org, "role")
	}
	if sourcePresent(v["payer_summary"]) {
		p, e := sourceObject(v["payer_summary"], "known", "reason payer_ref payer_source member_role account")
		if e != nil {
			return f, e
		}
		f.PayerKnown, e = sourceBool(p, "known")
		if e != nil {
			return f, e
		}
		f.PayerAccountID = sourceString(p, "payer_ref")
		f.PayerSource = sourceString(p, "payer_source")
		f.MemberRole = sourceString(p, "member_role")
		if !f.PayerKnown {
			f.Reason = sourceString(p, "reason")
		} else {
			a, e := sourceObject(p["account"], "account_id kind display_name status cash_balance_minor created_at_unix updated_at_unix", "")
			if e != nil || sourceString(a, "account_id") != f.PayerAccountID || sourceString(a, "status") != "active" || f.PayerAccountID == "" {
				return f, si.ErrInvariant
			}
		}
	}
	return f, nil
}
