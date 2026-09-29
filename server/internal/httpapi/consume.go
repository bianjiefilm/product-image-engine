package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/billassemble"
	"github.com/bianjiefilm/product-image-engine/server/internal/billconsume"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// ConsumeAdvisor 只保留权益回退。金额、余额和扣款都不从这里来。
type ConsumeAdvisor struct {
	Granted           []string
	AmountMinor       *int64
	IncludedAllowance bool
	FundsShort        bool
}

type consumeReq struct {
	ProjectID       string `json:"project_id"`
	SourceType      string `json:"source_type"`
	SourceRef       string `json:"source_ref"`
	SourceTenantID  string `json:"source_tenant_id"`
	ImageCount      int    `json:"image_count"`
	Resolution      string `json:"resolution"`
	Capability      string `json:"capability"`
	PricingVersion  string `json:"pricing_version"`
	Phone           string `json:"phone"`
	PaymentSource   string `json:"payment_source"`
	PayerAccountRef string `json:"payer_account_ref"`
	Page            string `json:"page"`
	Status          string `json:"status"`
	TaskID          string `json:"task_id"`
	ChargeRef       string `json:"charge_ref"`
	ReplacementKey  string `json:"replacement_key"`
	WorkComplete    bool   `json:"work_complete"`
	Books           []struct {
		AccountRef string `json:"account_ref"`
		Label      string `json:"label"`
	} `json:"books"`
}

func (s *Server) handleConsumeQuote(w http.ResponseWriter, r *http.Request) {
	req, proj, payer, fact, ok := s.openConsume(w, r)
	if !ok {
		return
	}
	q, err := s.quotePort(r.Context(), payer, fact, proj)
	if err != nil {
		s.writeQuoteErr(w, payer, fact, err)
		return
	}
	if err := s.allowCapability(fact.Capability, q); err != nil {
		writeErr(w, http.StatusConflict, "entitlement_missing", "有现金也不表示已开通该能力")
		return
	}
	label, err := presentQuote(q)
	if err != nil {
		writeConsumeErr(w, err)
		return
	}
	fp := billconsume.Fingerprint(payer, fact)
	existing, findErr := s.St.GetUsageQuoteByProject(r.Context(), proj.TenantID, proj.ID)
	if findErr != nil && !errors.Is(findErr, store.ErrNotFound) {
		writeStoreErr(w, findErr)
		return
	}
	mustRequote := findErr == nil && existing.Confirmed && existing.Fingerprint != fp
	taskID, chargeRef := "", ""
	if findErr == nil {
		taskID, chargeRef = existing.TaskID, existing.ChargeRef
	}
	saved, err := s.St.SaveUsageQuote(r.Context(), store.UsageQuote{
		StorageTenant: proj.TenantID, ProjectID: proj.ID,
		PayerKind: payer.Kind, PayerAccountRef: payer.AccountRef, PayerDisplay: payer.Display,
		ImageCount: fact.ImageCount, Resolution: fact.Resolution, Capability: fact.Capability,
		PricingVersion: fact.PricingVersion, Fingerprint: fp, QuoteRef: usageRef(q, fp),
		Presentation: label, IdempotencyKey: fp, TaskID: taskID, ChargeRef: chargeRef,
		Status: quoteStatus(label, q.FundsShort),
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	body := s.consumeBody(payer, fact, saved, req, mustRequote, false, q)
	if q.FundsShort {
		hand, handErr := s.topup(saved.QuoteRef)
		if handErr != nil {
			writeConsumeErr(w, handErr)
			return
		}
		body["topup"] = hand
		body["generate_allowed"] = false
		writeJSON(w, http.StatusPaymentRequired, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleConsumeConfirm(w http.ResponseWriter, r *http.Request) {
	req, proj, payer, fact, ok := s.openConsume(w, r)
	if !ok {
		return
	}
	saved, err := s.St.GetUsageQuoteByProject(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	q, err := s.quotePort(r.Context(), payer, fact, proj)
	if err != nil {
		s.writeQuoteErr(w, payer, fact, err)
		return
	}
	s.refreshBalance(r.Context(), payer, &q)
	if err := s.allowCapability(fact.Capability, q); err != nil {
		writeErr(w, http.StatusConflict, "entitlement_missing", "有现金也不表示已开通该能力")
		return
	}
	label, err := presentQuote(q)
	if err != nil {
		writeConsumeErr(w, err)
		return
	}
	fp := billconsume.Fingerprint(payer, fact)
	saved.Presentation = label
	if q.UsageID != "" {
		saved.QuoteRef = q.UsageID
	}
	if saved.Fingerprint != fp || !realQuote(label) {
		body := s.consumeBody(payer, fact, saved, req, true, false, q)
		body["reason"] = "must_requote"
		writeJSON(w, http.StatusConflict, body)
		return
	}
	if q.FundsShort {
		body := s.consumeBody(payer, fact, saved, req, false, false, q)
		hand, handErr := s.topup(saved.QuoteRef)
		if handErr != nil {
			writeConsumeErr(w, handErr)
			return
		}
		body["topup"] = hand
		body["reason"] = "funds_short"
		writeJSON(w, http.StatusPaymentRequired, body)
		return
	}
	if !q.IncludedAllowance && !q.BalanceKnown {
		body := s.consumeBody(payer, fact, saved, req, false, false, q)
		body["reason"] = "balance_unknown"
		writeJSON(w, http.StatusConflict, body)
		return
	}
	saved.Confirmed = true
	saved.Status = "confirmed"
	saved, err = s.St.SaveUsageQuote(r.Context(), saved)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.consumeBody(payer, fact, saved, req, false, true, q))
}

func (s *Server) handleConsumeReturn(w http.ResponseWriter, r *http.Request) {
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return
	}
	proj, err := s.projectFor(r.Context(), p, req.ProjectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	payer, err := serverPayer(p, proj)
	if err != nil {
		writeConsumeErr(w, err)
		return
	}
	decision := billconsume.AfterTopupPage(req.Page, false)
	levels := billconsume.Levels()
	body := map[string]any{
		"generate_allowed":      false,
		"reason":                decision.Reason,
		"charged":               nil,
		"balance_known":         false,
		"settlement":            billassemble.SettlementUnknown,
		"billing_pass":          levels.Billing,
		"production_authorized": levels.ProductionAuthorized,
	}
	if s.Bills != nil {
		bal, balErr := s.Bills.ReadBalance(r.Context(), payer.AccountRef)
		if balErr == nil {
			body["balance_known"] = bal.Known
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleConsumeRecover(w http.ResponseWriter, r *http.Request) {
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return
	}
	proj, err := s.projectFor(r.Context(), p, req.ProjectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	saved, err := s.St.GetUsageQuoteByProject(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if req.TaskID != "" && req.TaskID != saved.TaskID {
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
		return
	}
	if req.ChargeRef != "" && req.ChargeRef != saved.ChargeRef {
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
		return
	}
	if req.ReplacementKey != "" && req.ReplacementKey != saved.IdempotencyKey {
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
		return
	}
	if strings.TrimSpace(saved.TaskID) == "" || s.Bills == nil {
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
		return
	}
	look, err := s.Bills.Lookup(r.Context(), billassemble.LookupCall{TaskID: saved.TaskID, UsageID: saved.QuoteRef})
	if err != nil {
		if errors.Is(err, billassemble.ErrUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "billing_unavailable", "计费服务不可达，未报价")
			return
		}
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
		return
	}
	if !look.FoundTask {
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
		return
	}
	result := billassemble.Reconcile(req.WorkComplete, look)
	chargeRef := saved.ChargeRef
	if look.FoundCharge && look.ChargeRef != "" {
		chargeRef = look.ChargeRef
	}
	levels := billconsume.Levels()
	writeJSON(w, http.StatusOK, map[string]any{
		"resubmit": false, "recharge": false, "regenerate": result.Regenerate,
		"task_id": saved.TaskID, "charge_ref": chargeRef,
		"idempotency_key": saved.IdempotencyKey,
		"charged":         nil, "settlement": result.Settlement,
		"billing_passed":        result.BillingPassed,
		"billing_pass":          levels.Billing,
		"production_authorized": levels.ProductionAuthorized,
	})
}

func (s *Server) readConsume(w http.ResponseWriter, r *http.Request) (consumeReq, platform.Principal, bool) {
	p, _ := principalFrom(r.Context())
	var req consumeReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return consumeReq{}, p, false
	}
	req.Phone = ""
	req.PricingVersion = ""
	return req, p, true
}

func (s *Server) openConsume(w http.ResponseWriter, r *http.Request) (consumeReq, store.Project, billconsume.Payer, billconsume.UsageFact, bool) {
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return consumeReq{}, store.Project{}, billconsume.Payer{}, billconsume.UsageFact{}, false
	}
	if err := billconsume.RejectPaymentSource(req.PaymentSource); err != nil {
		writeErr(w, http.StatusConflict, "order_funds", "不能用订单服务款扣图像费")
		return consumeReq{}, store.Project{}, billconsume.Payer{}, billconsume.UsageFact{}, false
	}
	proj, err := s.projectFor(r.Context(), p, req.ProjectID)
	if err != nil {
		writeStoreErr(w, err)
		return consumeReq{}, store.Project{}, billconsume.Payer{}, billconsume.UsageFact{}, false
	}
	payer, err := serverPayer(p, proj)
	if err != nil {
		writeConsumeErr(w, err)
		return consumeReq{}, store.Project{}, billconsume.Payer{}, billconsume.UsageFact{}, false
	}
	if err := rejectBodyPayer(payer, req); err != nil {
		writeConsumeErr(w, err)
		return consumeReq{}, store.Project{}, billconsume.Payer{}, billconsume.UsageFact{}, false
	}
	fact, err := billconsume.NewUsageFact(req.ImageCount, req.Resolution, req.Capability, billassemble.PricingVersion)
	if err != nil {
		writeConsumeErr(w, err)
		return consumeReq{}, store.Project{}, billconsume.Payer{}, billconsume.UsageFact{}, false
	}
	return req, proj, payer, fact, true
}

func serverPayer(p platform.Principal, proj store.Project) (billconsume.Payer, error) {
	allowed := make([]billconsume.OrgPayer, 0, len(p.OrgPayers))
	var verified *billconsume.OrgPayer
	for _, item := range p.OrgPayers {
		org := billconsume.OrgPayer{TenantID: item.TenantID, AccountRef: item.AccountRef, Display: item.Display}
		allowed = append(allowed, org)
		if verified == nil && item.TenantID == proj.TenantID {
			copy := org
			verified = &copy
		}
	}
	return billconsume.ResolvePayer(billconsume.Context{
		SourceType: proj.SourceType, SourceRef: proj.SourceRef,
		PersonalAccountRef: p.AccountID, VerifiedOrg: verified, AuthorizedOrgs: allowed,
	})
}

func rejectBodyPayer(payer billconsume.Payer, req consumeReq) error {
	if looksLikeAccountPhone(req.PayerAccountRef) {
		return billconsume.ErrPhoneNotAccount
	}
	if req.PayerAccountRef != "" && req.PayerAccountRef != payer.AccountRef {
		return billconsume.ErrPayerNotAuthorized
	}
	return nil
}

func (s *Server) billLink() billassemble.LinkConfig {
	return billassemble.LinkConfig{
		Enabled: s.Cfg.BillingEnabled, BaseURL: s.Cfg.BillingBaseURL,
		Token: s.Cfg.BillingToken, AppID: s.Cfg.IdentityAppID,
	}
}

func (s *Server) quotePort(ctx context.Context, payer billconsume.Payer, fact billconsume.UsageFact, proj store.Project) (billassemble.QuoteFact, error) {
	if s.Bills == nil {
		if billassemble.Connection(s.billLink()) == billassemble.ConnMissing {
			return billassemble.QuoteFact{Connection: billassemble.ConnMissing}, billassemble.ErrConfigMissing
		}
		return billassemble.QuoteFact{Connection: billassemble.ConnUnimplemented}, billassemble.ErrUnimplemented
	}
	return s.Bills.Quote(ctx, billassemble.QuoteCall{
		PayerAccountID: payer.AccountRef, Capability: fact.Capability,
		Quantity: int64(fact.ImageCount), PricingVersion: billassemble.PricingVersion,
		BusinessRef: proj.ID, IdempotencyKey: billconsume.Fingerprint(payer, fact),
	})
}

func (s *Server) refreshBalance(ctx context.Context, payer billconsume.Payer, q *billassemble.QuoteFact) {
	if s.Bills == nil || q.IncludedAllowance {
		return
	}
	bal, err := s.Bills.ReadBalance(ctx, payer.AccountRef)
	if err != nil {
		q.BalanceKnown = false
		q.FundsShort = false
		return
	}
	q.BalanceKnown = bal.Known
	q.BalanceMinor = bal.Minor
	q.FundsShort = bal.Known && bal.Minor < q.AmountMinor
}

func (s *Server) allowCapability(capability string, q billassemble.QuoteFact) error {
	entitled := false
	if q.EntitlementsKnown {
		for _, item := range q.Entitlements {
			if item == capability {
				entitled = true
				break
			}
		}
	} else {
		entitled = s.capabilityGranted(capability)
	}
	return billconsume.Allow(capability, entitled, 0)
}

func (s *Server) writeQuoteErr(w http.ResponseWriter, payer billconsume.Payer, fact billconsume.UsageFact, err error) {
	switch {
	case errors.Is(err, billassemble.ErrConfigMissing):
		writeJSON(w, http.StatusOK, s.honestConsume(payer, fact, billassemble.ConnMissing))
	case errors.Is(err, billassemble.ErrUnimplemented):
		writeJSON(w, http.StatusOK, s.honestConsume(payer, fact, billassemble.ConnUnimplemented))
	case errors.Is(err, billassemble.ErrUnavailable):
		writeErr(w, http.StatusServiceUnavailable, "billing_unavailable", "计费服务不可达，未报价")
	default:
		writeConsumeErr(w, err)
	}
}

func presentQuote(q billassemble.QuoteFact) (string, error) {
	if q.IncludedAllowance {
		return billconsume.Present(billconsume.QuoteView{IncludedAllowance: true})
	}
	if q.UsageID == "" {
		return "待确认", nil
	}
	amount := q.AmountMinor
	currency := q.Currency
	if currency == "" {
		currency = "CNY"
	}
	return billconsume.Present(billconsume.QuoteView{AmountMinor: &amount, Currency: currency})
}

func realQuote(label string) bool {
	return label == "已包含额度" || strings.HasPrefix(label, "本次预计 ¥") || strings.HasPrefix(label, "-本次预计 ¥")
}

func (s *Server) capabilityGranted(capability string) bool {
	if s.Consume == nil {
		return false
	}
	for _, item := range s.Consume.Granted {
		if item == capability {
			return true
		}
	}
	return false
}

func (s *Server) consumeBody(payer billconsume.Payer, fact billconsume.UsageFact, saved store.UsageQuote, req consumeReq, mustRequote, generate bool, q billassemble.QuoteFact) map[string]any {
	levels := billconsume.Levels()
	books := billconsume.VisibleBooks(payer, toBooks(req.Books))
	visible := make([]map[string]string, 0, len(books))
	for _, book := range books {
		visible = append(visible, map[string]string{"account_ref": book.AccountRef, "label": book.Label})
	}
	if generate && (mustRequote || !saved.Confirmed || !realQuote(saved.Presentation)) {
		generate = false
	}
	return map[string]any{
		"payer_display": payer.Display, "payer_kind": payer.Kind, "payer_account_ref": payer.AccountRef,
		"presentation": saved.Presentation, "must_requote": mustRequote, "generate_allowed": generate,
		"charged": nil, "quoted": realQuote(saved.Presentation), "quote_ref": saved.QuoteRef,
		"connection": q.Connection, "settlement": billassemble.SettlementUnknown,
		"balance_known":         q.BalanceKnown,
		"usage_fact":            fact.Report(payer.AccountRef, saved.IdempotencyKey),
		"visible_books":         visible,
		"implementation_ready":  levels.ImplementationReady,
		"integration":           levels.Integration,
		"billing_pass":          levels.Billing,
		"channel_billing":       levels.ChannelBilling,
		"production_authorized": levels.ProductionAuthorized,
	}
}

func (s *Server) honestConsume(payer billconsume.Payer, fact billconsume.UsageFact, connection string) map[string]any {
	levels := billconsume.Levels()
	return map[string]any{
		"payer_display": payer.Display, "payer_kind": payer.Kind, "payer_account_ref": payer.AccountRef,
		"presentation": connection, "must_requote": false, "generate_allowed": false,
		"charged": nil, "quoted": false, "quote_ref": "",
		"connection": connection, "settlement": billassemble.SettlementUnknown,
		"usage_fact":            fact.Report(payer.AccountRef, ""),
		"visible_books":         []any{},
		"implementation_ready":  levels.ImplementationReady,
		"integration":           levels.Integration,
		"billing_pass":          levels.Billing,
		"channel_billing":       levels.ChannelBilling,
		"production_authorized": levels.ProductionAuthorized,
	}
}

func (s *Server) topup(quoteRef string) (billconsume.Handoff, error) {
	allowed := s.ownLaunchTargets()
	target := ""
	if len(allowed) > 0 {
		target = allowed[0]
	}
	return billconsume.TopupHandoff(billconsume.TopupInput{
		QuoteRef: quoteRef, ReturnTarget: target, Allowed: allowed, FundsShort: true,
	})
}

func (s *Server) ownLaunchTargets() []string {
	if s.Registry == nil {
		return nil
	}
	app, ok := s.Registry.LookupApp(s.Cfg.AppID)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(app.LaunchTargets))
	for _, target := range app.LaunchTargets {
		if target.Kind == appregistry.KindLaunch {
			out = append(out, target.TargetID)
		}
	}
	return out
}

func toBooks(in []struct {
	AccountRef string `json:"account_ref"`
	Label      string `json:"label"`
}) []billconsume.Book {
	out := make([]billconsume.Book, 0, len(in))
	for _, item := range in {
		out = append(out, billconsume.Book{AccountRef: item.AccountRef, Label: item.Label})
	}
	return out
}

func usageRef(q billassemble.QuoteFact, fingerprint string) string {
	if q.UsageID != "" {
		return q.UsageID
	}
	return quoteRef(fingerprint)
}

func quoteRef(fingerprint string) string {
	if len(fingerprint) > 16 {
		fingerprint = fingerprint[:16]
	}
	return "qte_" + fingerprint
}

func quoteStatus(label string, fundsShort bool) string {
	if fundsShort {
		return "funds_short"
	}
	if !realQuote(label) {
		return "draft"
	}
	return "quoted"
}

func writeConsumeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billconsume.ErrPayerNotAuthorized):
		writeErr(w, http.StatusForbidden, "payer_not_authorized", "不能使用这个付款主体")
	case errors.Is(err, billconsume.ErrPhoneNotAccount):
		writeErr(w, http.StatusBadRequest, "phone_not_account", "手机号不是平台账号")
	case errors.Is(err, billconsume.ErrOrderFunds):
		writeErr(w, http.StatusConflict, "order_funds", "不能用订单服务款扣图像费")
	case errors.Is(err, billconsume.ErrEntitlementMissing):
		writeErr(w, http.StatusConflict, "entitlement_missing", "有现金也不表示已开通该能力")
	case errors.Is(err, billconsume.ErrLookupRequired), errors.Is(err, billconsume.ErrIdempotencyChanged):
		writeErr(w, http.StatusConflict, "lookup_required", "先查原任务和原扣费，不能重新生成或重新扣费")
	case errors.Is(err, billconsume.ErrReturnTarget):
		writeErr(w, http.StatusConflict, "return_target_rejected", "返回位置不在本应用登记表")
	default:
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	}
}

func looksLikeAccountPhone(raw string) bool {
	raw = strings.TrimSpace(raw)
	if len(raw) != 11 {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
