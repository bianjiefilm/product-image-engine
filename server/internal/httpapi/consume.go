package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/billconsume"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// ConsumeAdvisor 只提供本地可证明的报价与权益。它不是账本，也不执行扣款。
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
	Books           []struct {
		AccountRef string `json:"account_ref"`
		Label      string `json:"label"`
	} `json:"books"`
}

func (s *Server) handleConsumeQuote(w http.ResponseWriter, r *http.Request) {
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return
	}
	if err := billconsume.RejectPaymentSource(req.PaymentSource); err != nil {
		writeErr(w, http.StatusConflict, "order_funds", "不能用订单服务款扣图像费")
		return
	}
	payer, err := resolveConsumePayer(p, req)
	if err != nil {
		writeConsumeErr(w, err)
		return
	}
	fact, err := billconsume.NewUsageFact(req.ImageCount, req.Resolution, req.Capability, req.PricingVersion)
	if err != nil {
		writeConsumeErr(w, err)
		return
	}
	proj, err := s.projectForConsume(r.Context(), p, req.ProjectID, payer)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := billconsume.Allow(fact.Capability, s.capabilityGranted(fact.Capability), 0); err != nil {
		writeErr(w, http.StatusConflict, "entitlement_missing", "有现金也不表示已开通该能力")
		return
	}
	view := s.quoteView(fact)
	label, err := billconsume.Present(view)
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
	saved, err := s.St.SaveUsageQuote(r.Context(), store.UsageQuote{
		StorageTenant: proj.TenantID, ProjectID: proj.ID,
		PayerKind: payer.Kind, PayerAccountRef: payer.AccountRef, PayerDisplay: payer.Display,
		ImageCount: fact.ImageCount, Resolution: fact.Resolution, Capability: fact.Capability,
		PricingVersion: fact.PricingVersion, Fingerprint: fp, QuoteRef: quoteRef(fp),
		Presentation: label, IdempotencyKey: fp, Status: quoteStatus(label, s.fundsShort()),
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	body := s.consumeBody(payer, fact, saved, req, mustRequote, false)
	if s.fundsShort() {
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
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return
	}
	payer, fact, _, saved, ok := s.currentQuote(w, r, p, req)
	if !ok {
		return
	}
	fp := billconsume.Fingerprint(payer, fact)
	if saved.Fingerprint != fp || saved.Presentation == "待确认" || s.fundsShort() {
		writeJSON(w, http.StatusConflict, s.consumeBody(payer, fact, saved, req, true, false))
		return
	}
	saved.Confirmed = true
	saved.Status = "confirmed"
	saved, err := s.St.SaveUsageQuote(r.Context(), saved)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.consumeBody(payer, fact, saved, req, false, true))
}

func (s *Server) handleConsumeReturn(w http.ResponseWriter, r *http.Request) {
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return
	}
	if _, err := s.projectForConsume(r.Context(), p, req.ProjectID, billconsume.Payer{}); err != nil {
		writeStoreErr(w, err)
		return
	}
	decision := billconsume.AfterTopupPage(req.Page, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"generate_allowed":      decision.GenerateAllowed,
		"reason":                decision.Reason,
		"charged":               false,
		"billing_pass":          billconsume.Levels().Billing,
		"production_authorized": billconsume.Levels().ProductionAuthorized,
	})
}

func (s *Server) handleConsumeRecover(w http.ResponseWriter, r *http.Request) {
	req, p, ok := s.readConsume(w, r)
	if !ok {
		return
	}
	proj, err := s.projectForConsume(r.Context(), p, req.ProjectID, billconsume.Payer{})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	saved, err := s.St.GetUsageQuoteByProject(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	recovered, err := billconsume.Recover(billconsume.Attempt{
		Status: req.Status, TaskID: req.TaskID, ChargeRef: req.ChargeRef,
		IdempotencyKey: saved.IdempotencyKey, ReplacementKey: req.ReplacementKey,
	})
	if err != nil {
		writeConsumeErr(w, err)
		return
	}
	saved.TaskID = recovered.TaskID
	saved.ChargeRef = recovered.ChargeRef
	saved.Status = "unknown"
	saved.Confirmed = false
	if _, err := s.St.SaveUsageQuote(r.Context(), saved); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resubmit": false, "recharge": false,
		"task_id": recovered.TaskID, "charge_ref": recovered.ChargeRef,
		"idempotency_key":       saved.IdempotencyKey,
		"charged":               false,
		"billing_pass":          billconsume.Levels().Billing,
		"production_authorized": billconsume.Levels().ProductionAuthorized,
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
	return req, p, true
}

func (s *Server) currentQuote(w http.ResponseWriter, r *http.Request, p platform.Principal, req consumeReq) (billconsume.Payer, billconsume.UsageFact, store.Project, store.UsageQuote, bool) {
	if err := billconsume.RejectPaymentSource(req.PaymentSource); err != nil {
		writeErr(w, http.StatusConflict, "order_funds", "不能用订单服务款扣图像费")
		return billconsume.Payer{}, billconsume.UsageFact{}, store.Project{}, store.UsageQuote{}, false
	}
	payer, err := resolveConsumePayer(p, req)
	if err != nil {
		writeConsumeErr(w, err)
		return billconsume.Payer{}, billconsume.UsageFact{}, store.Project{}, store.UsageQuote{}, false
	}
	fact, err := billconsume.NewUsageFact(req.ImageCount, req.Resolution, req.Capability, req.PricingVersion)
	if err != nil {
		writeConsumeErr(w, err)
		return billconsume.Payer{}, billconsume.UsageFact{}, store.Project{}, store.UsageQuote{}, false
	}
	if err := billconsume.Allow(fact.Capability, s.capabilityGranted(fact.Capability), 0); err != nil {
		writeErr(w, http.StatusConflict, "entitlement_missing", "有现金也不表示已开通该能力")
		return billconsume.Payer{}, billconsume.UsageFact{}, store.Project{}, store.UsageQuote{}, false
	}
	proj, err := s.projectForConsume(r.Context(), p, req.ProjectID, payer)
	if err != nil {
		writeStoreErr(w, err)
		return billconsume.Payer{}, billconsume.UsageFact{}, store.Project{}, store.UsageQuote{}, false
	}
	saved, err := s.St.GetUsageQuoteByProject(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeStoreErr(w, err)
		return billconsume.Payer{}, billconsume.UsageFact{}, store.Project{}, store.UsageQuote{}, false
	}
	return payer, fact, proj, saved, true
}

func resolveConsumePayer(p platform.Principal, req consumeReq) (billconsume.Payer, error) {
	if looksLikeAccountPhone(req.PayerAccountRef) {
		return billconsume.Payer{}, billconsume.ErrPhoneNotAccount
	}
	var verified *billconsume.OrgPayer
	for _, item := range p.OrgPayers {
		if item.TenantID == strings.TrimSpace(req.SourceTenantID) {
			copy := billconsume.OrgPayer{TenantID: item.TenantID, AccountRef: item.AccountRef, Display: item.Display}
			verified = &copy
			break
		}
	}
	allowed := make([]billconsume.OrgPayer, 0, len(p.OrgPayers))
	for _, item := range p.OrgPayers {
		allowed = append(allowed, billconsume.OrgPayer{TenantID: item.TenantID, AccountRef: item.AccountRef, Display: item.Display})
	}
	payer, err := billconsume.ResolvePayer(billconsume.Context{
		SourceType: req.SourceType, SourceRef: req.SourceRef,
		PersonalAccountRef: p.AccountID, VerifiedOrg: verified, AuthorizedOrgs: allowed,
	})
	if err != nil {
		return billconsume.Payer{}, err
	}
	if req.PayerAccountRef != "" && req.PayerAccountRef != payer.AccountRef {
		return billconsume.Payer{}, billconsume.ErrPayerNotAuthorized
	}
	return payer, nil
}

func (s *Server) projectForConsume(ctx context.Context, p platform.Principal, id string, payer billconsume.Payer) (store.Project, error) {
	if proj, err := s.projectFor(ctx, p, id); err == nil {
		return proj, nil
	}
	if payer.Kind == billconsume.KindOrganization && payer.TenantID != "" {
		return s.St.GetProject(ctx, payer.TenantID, id)
	}
	return store.Project{}, store.ErrNotFound
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

func (s *Server) fundsShort() bool {
	return s.Consume != nil && s.Consume.FundsShort
}

func (s *Server) quoteView(fact billconsume.UsageFact) billconsume.QuoteView {
	if s.Consume == nil {
		return billconsume.QuoteView{}
	}
	if s.Consume.IncludedAllowance {
		return billconsume.QuoteView{IncludedAllowance: true}
	}
	if s.Consume.AmountMinor == nil {
		return billconsume.QuoteView{}
	}
	amount := *s.Consume.AmountMinor * int64(fact.ImageCount)
	return billconsume.QuoteView{AmountMinor: &amount, Currency: "CNY"}
}

func (s *Server) consumeBody(payer billconsume.Payer, fact billconsume.UsageFact, saved store.UsageQuote, req consumeReq, mustRequote, generate bool) map[string]any {
	levels := billconsume.Levels()
	books := billconsume.VisibleBooks(payer, toBooks(req.Books))
	visible := make([]map[string]string, 0, len(books))
	for _, book := range books {
		visible = append(visible, map[string]string{"account_ref": book.AccountRef, "label": book.Label})
	}
	if generate && (mustRequote || !saved.Confirmed || saved.Presentation == "待确认") {
		generate = false
	}
	return map[string]any{
		"payer_display": payer.Display, "payer_kind": payer.Kind, "payer_account_ref": payer.AccountRef,
		"presentation": saved.Presentation, "must_requote": mustRequote, "generate_allowed": generate,
		"charged": false, "quote_ref": saved.QuoteRef,
		"usage_fact":            fact.Report(payer.AccountRef, saved.IdempotencyKey),
		"visible_books":         visible,
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
	if label == "待确认" {
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
