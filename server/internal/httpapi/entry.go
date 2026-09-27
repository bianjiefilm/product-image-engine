package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/entry"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// 首次做产品图的入口和恢复(HUI-1894)。
// 没开真实生成时只记失败或未知，不登记成果、不扣费。
// useful_proven 在本接口恒为 false：真实成片仍未证明。

type entryAssetReq struct {
	AssetRef    string `json:"asset_ref"`
	OwnerTenant string `json:"owner_tenant"`
}

func (s *Server) handleStartEntry(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		Name                string `json:"name"`
		UsageKind           string `json:"usage_kind"`
		WidthPx             int    `json:"width_px"`
		HeightPx            int    `json:"height_px"`
		BackgroundDirection string `json:"background_direction"`
		PhotoAssetID        string `json:"photo_asset_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.UsageKind = strings.TrimSpace(req.UsageKind)
	req.BackgroundDirection = strings.TrimSpace(req.BackgroundDirection)
	req.PhotoAssetID = strings.TrimSpace(req.PhotoAssetID)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "工程名称不能为空")
		return
	}
	ws, err := entry.ResolveWorkspace(entry.ResolveInput{
		AccountID: p.AccountID, SessionTenantID: p.TenantID, Memberships: p.OrgMemberships,
	})
	if err != nil {
		writeEntryErr(w, err)
		return
	}
	if req.PhotoAssetID != "" {
		if err := entry.PlaceAssets(ws, []entry.AuthorizedAsset{{Ref: req.PhotoAssetID, OwnerTenant: ws.StorageTenant}}); err != nil {
			writeEntryErr(w, err)
			return
		}
	}
	fp := entry.Fingerprint("start", ws.StorageTenant, req.Name, req.UsageKind, strconv.Itoa(req.WidthPx), strconv.Itoa(req.HeightPx), req.BackgroundDirection, req.PhotoAssetID)
	if existing, err := s.St.GetEntryByFingerprint(r.Context(), ws.StorageTenant, fp); err == nil {
		proj, gerr := s.St.GetProject(r.Context(), ws.StorageTenant, existing.ProjectID)
		if gerr != nil {
			writeStoreErr(w, gerr)
			return
		}
		writeJSON(w, http.StatusOK, entryView(ws, proj, existing.ReturnHref, entry.Measure(entry.PathFacts{
			UploadedNew: req.PhotoAssetID != "", FilledScene: req.UsageKind != "",
			FilledSize: req.WidthPx > 0 && req.HeightPx > 0, FilledBackground: req.BackgroundDirection != "",
		}), existing, false))
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeStoreErr(w, err)
		return
	}
	proj, err := s.St.CreateProject(r.Context(), store.Project{
		TenantID: ws.StorageTenant, Name: req.Name, UsageKind: req.UsageKind,
		WidthPx: req.WidthPx, HeightPx: req.HeightPx, SourceType: "standalone", CreatedBy: p.UserID,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if req.PhotoAssetID != "" {
		if _, err := s.St.AddInput(r.Context(), store.ProjectInput{
			ProjectID: proj.ID, TenantID: ws.StorageTenant, PlatformAssetID: req.PhotoAssetID,
			SnapshotName: req.PhotoAssetID, SnapshotContentType: "image/*",
		}); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	saved, err := s.St.InsertEntrySession(r.Context(), store.EntrySession{
		AccountID: p.AccountID, WorkspaceKind: ws.Kind, WorkspaceScope: ws.Scope,
		StorageTenant: ws.StorageTenant, ProjectID: proj.ID, SourceType: "standalone",
		Fingerprint: fp, Status: "draft", QuoteLabel: entry.QuotePending,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entryView(ws, proj, "", entry.Measure(entry.PathFacts{
		UploadedNew: req.PhotoAssetID != "", FilledScene: req.UsageKind != "",
		FilledSize: req.WidthPx > 0 && req.HeightPx > 0, FilledBackground: req.BackgroundDirection != "",
	}), saved, true))
}

func (s *Server) handleSourceEntry(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req struct {
		SourceType          string          `json:"source_type"`
		SourceRef           string          `json:"source_ref"`
		SourceTenantID      string          `json:"source_tenant_id"`
		ReturnHref          string          `json:"return_href"`
		AuthorizedAssets    []entryAssetReq `json:"authorized_assets"`
		UsageKind           string          `json:"usage_kind"`
		WidthPx             int             `json:"width_px"`
		HeightPx            int             `json:"height_px"`
		BackgroundDirection string          `json:"background_direction"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	req.SourceType = strings.TrimSpace(req.SourceType)
	req.SourceRef = strings.TrimSpace(req.SourceRef)
	req.SourceTenantID = strings.TrimSpace(req.SourceTenantID)
	req.UsageKind = strings.TrimSpace(req.UsageKind)
	req.BackgroundDirection = strings.TrimSpace(req.BackgroundDirection)
	ws, err := entry.ResolveWorkspace(entry.ResolveInput{
		AccountID: p.AccountID, SessionTenantID: p.TenantID, Memberships: p.OrgMemberships,
		SourceType: req.SourceType, SourceRef: req.SourceRef, SourceTenantID: req.SourceTenantID,
	})
	if err != nil {
		writeEntryErr(w, err)
		return
	}
	href, err := entry.ExactReturn(req.SourceType, req.ReturnHref)
	if err != nil {
		writeEntryErr(w, err)
		return
	}
	assets := make([]entry.AuthorizedAsset, 0, len(req.AuthorizedAssets))
	refs := make([]string, 0, len(req.AuthorizedAssets))
	for _, asset := range req.AuthorizedAssets {
		ref := strings.TrimSpace(asset.AssetRef)
		if ref == "" {
			continue
		}
		assets = append(assets, entry.AuthorizedAsset{Ref: ref, OwnerTenant: strings.TrimSpace(asset.OwnerTenant)})
		refs = append(refs, ref)
	}
	if err := entry.PlaceAssets(ws, assets); err != nil {
		writeEntryErr(w, err)
		return
	}
	created := false
	proj, err := s.St.FindProjectBySource(r.Context(), ws.StorageTenant, req.SourceType, req.SourceRef)
	if errors.Is(err, store.ErrNotFound) {
		name := req.UsageKind
		if name == "" {
			name = req.SourceRef
		}
		proj, err = s.St.CreateProject(r.Context(), store.Project{
			TenantID: ws.StorageTenant, Name: name, UsageKind: req.UsageKind,
			WidthPx: req.WidthPx, HeightPx: req.HeightPx,
			SourceType: req.SourceType, SourceRef: req.SourceRef, CreatedBy: p.UserID,
		})
		created = true
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.ensureInputs(r.Context(), proj, refs); err != nil {
		writeStoreErr(w, err)
		return
	}
	fp := entry.Fingerprint("source", ws.StorageTenant, req.SourceType, req.SourceRef, req.UsageKind, strconv.Itoa(req.WidthPx), strconv.Itoa(req.HeightPx), req.BackgroundDirection, strings.Join(refs, ","))
	session, err := s.St.GetEntryByFingerprint(r.Context(), ws.StorageTenant, fp)
	if errors.Is(err, store.ErrNotFound) {
		session, err = s.St.InsertEntrySession(r.Context(), store.EntrySession{
			AccountID: p.AccountID, WorkspaceKind: ws.Kind, WorkspaceScope: ws.Scope,
			StorageTenant: ws.StorageTenant, ProjectID: proj.ID,
			SourceType: req.SourceType, SourceRef: req.SourceRef, ReturnHref: href,
			Fingerprint: fp, Status: "draft", QuoteLabel: entry.QuotePending,
			AuthorizedAssets: refs,
		})
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	facts := entry.PathFacts{
		HasAuthorizedAssets: len(refs) > 0,
		FilledScene:         req.UsageKind != "",
		FilledSize:          req.WidthPx > 0 && req.HeightPx > 0,
		FilledBackground:    req.BackgroundDirection != "",
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	body := entryView(ws, proj, href, entry.Measure(facts), session, created)
	body["return_href"] = href
	writeJSON(w, status, body)
}

func (s *Server) handleListPersonalEntries(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	list, err := s.St.ListEntrySessions(r.Context(), p.AccountID, entry.PersonalDefault)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if list == nil {
		list = []store.EntrySession{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list, "useful_proven": false})
}

func (s *Server) handleSubmitFirstImage(w http.ResponseWriter, r *http.Request) {
	s.writeFirstImage(w, r, false)
}

func (s *Server) handleRefreshFirstImage(w http.ResponseWriter, r *http.Request) {
	s.writeFirstImage(w, r, true)
}

func (s *Server) handleGetFirstImage(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	proj, err := s.projectFor(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	session, err := s.St.LatestEntryForProject(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, firstImageView(proj, session))
}

func (s *Server) writeFirstImage(w http.ResponseWriter, r *http.Request, refresh bool) {
	p, _ := principalFrom(r.Context())
	proj, err := s.projectFor(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var req struct {
		UsageKind           string `json:"usage_kind"`
		WidthPx             int    `json:"width_px"`
		HeightPx            int    `json:"height_px"`
		BackgroundDirection string `json:"background_direction"`
		InputAssetID        string `json:"input_asset_id"`
	}
	if !refresh && r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
			return
		}
	}
	req.UsageKind = strings.TrimSpace(req.UsageKind)
	req.BackgroundDirection = strings.TrimSpace(req.BackgroundDirection)
	req.InputAssetID = strings.TrimSpace(req.InputAssetID)
	if refresh {
		session, err := s.St.LatestEntryForProject(r.Context(), proj.TenantID, proj.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		session = s.pollFirstImage(r.Context(), session)
		recovery := session.RecoveryCount + 1
		updated, err := s.St.UpdateEntrySession(r.Context(), session.StorageTenant, session.ID, store.EntryPatch{
			Status: &session.Status, PlatformTaskID: &session.PlatformTaskID, OutputAssetID: &session.OutputAssetID,
			DegradedReason: &session.DegradedReason, QuoteLabel: &session.QuoteLabel, RecoveryCount: &recovery,
		})
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		updated.Charged = false
		writeJSON(w, http.StatusOK, firstImageView(proj, updated))
		return
	}
	if req.UsageKind == "" {
		req.UsageKind = proj.UsageKind
	}
	if req.WidthPx == 0 {
		req.WidthPx = proj.WidthPx
	}
	if req.HeightPx == 0 {
		req.HeightPx = proj.HeightPx
	}
	fp := entry.Fingerprint("generate", proj.TenantID, proj.ID, req.UsageKind, strconv.Itoa(req.WidthPx), strconv.Itoa(req.HeightPx), req.BackgroundDirection, req.InputAssetID)
	if existing, err := s.St.GetEntryByFingerprint(r.Context(), proj.TenantID, fp); err == nil {
		recovery := existing.RecoveryCount + 1
		updated, uerr := s.St.UpdateEntrySession(r.Context(), existing.StorageTenant, existing.ID, store.EntryPatch{RecoveryCount: &recovery})
		if uerr != nil {
			writeStoreErr(w, uerr)
			return
		}
		writeJSON(w, http.StatusOK, firstImageView(proj, updated))
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeStoreErr(w, err)
		return
	}
	decision := s.decideFirstImage(r.Context(), fp, req.InputAssetID, proj)
	quote := entry.QuotePending
	charged := false
	saved, err := s.St.InsertEntrySession(r.Context(), store.EntrySession{
		AccountID: p.AccountID, WorkspaceKind: workspaceKind(proj), WorkspaceScope: workspaceScope(proj),
		StorageTenant: proj.TenantID, ProjectID: proj.ID, SourceType: proj.SourceType, SourceRef: proj.SourceRef,
		Fingerprint: fp, Status: decision.Status, PlatformTaskID: decision.TaskID,
		OutputAssetID: decision.OutputAssetID, QuoteLabel: quote, Charged: charged,
		DegradedReason: decision.DegradedReason,
	})
	if errors.Is(err, store.ErrConflict) {
		saved, err = s.St.GetEntryByFingerprint(r.Context(), proj.TenantID, fp)
		if err == nil {
			writeJSON(w, http.StatusOK, firstImageView(proj, saved))
			return
		}
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, firstImageView(proj, saved))
}

func (s *Server) decideFirstImage(ctx context.Context, fingerprint, assetID string, proj store.Project) entry.Decision {
	if !s.Cfg.GenerationEnabled {
		return entry.Decide(entry.DecideInput{
			GenerationEnabled: false, BillingEnabled: s.Cfg.BillingEnabled, Fingerprint: fingerprint,
		})
	}
	res, err := s.Tasks.Submit(ctx, platform.SubmitRequest{
		IdempotencyKey: fingerprint,
		Kind:           "product-image-generate",
		Payload: map[string]any{
			"project_id": proj.ID, "platform_asset_id": assetID, "source_type": proj.SourceType,
		},
	})
	reachable := err == nil
	if err != nil && !errors.Is(err, platform.ErrUnavailable) {
		reachable = false
	}
	decision := entry.Decide(entry.DecideInput{
		GenerationEnabled: true, BillingEnabled: s.Cfg.BillingEnabled, Fingerprint: fingerprint,
		UpstreamReachable: &reachable, UpstreamStatus: res.Status,
	})
	if reachable {
		decision.TaskID = res.TaskID
	}
	decision.Charged = false
	decision.QuoteLabel = entry.QuotePending
	if !decision.RegisterOutput {
		decision.OutputAssetID = ""
	}
	return decision
}

func (s *Server) pollFirstImage(ctx context.Context, session store.EntrySession) store.EntrySession {
	session.Charged = false
	session.QuoteLabel = entry.QuotePending
	canPoll := session.PlatformTaskID != "" && (session.Status == entry.StatusQueued || session.Status == entry.StatusRunning || session.Status == entry.StatusUnknown)
	if !canPoll {
		if session.OutputAssetID != "" && session.Status != entry.StatusCompleted {
			session.OutputAssetID = ""
		}
		return session
	}
	res, err := s.Tasks.Status(ctx, session.PlatformTaskID)
	reachable := err == nil
	asset := assetFromTask(res)
	decision := entry.Decide(entry.DecideInput{
		GenerationEnabled: true, BillingEnabled: false, Fingerprint: session.Fingerprint,
		UpstreamReachable: &reachable, UpstreamStatus: res.Status, UpstreamAssetID: asset,
	})
	session.Status = decision.Status
	session.DegradedReason = decision.DegradedReason
	if decision.RegisterOutput {
		session.OutputAssetID = decision.OutputAssetID
	} else {
		session.OutputAssetID = ""
	}
	return session
}

func assetFromTask(res platform.TaskStatusResult) string {
	if res.Result == nil {
		return ""
	}
	for _, key := range []string{"platform_asset_id", "asset_id"} {
		if value, ok := res.Result[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) ensureInputs(ctx context.Context, proj store.Project, refs []string) error {
	existing, err := s.St.ListInputs(ctx, proj.TenantID, proj.ID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, item := range existing {
		have[item.PlatformAssetID] = true
	}
	for _, ref := range refs {
		if have[ref] {
			continue
		}
		if _, err := s.St.AddInput(ctx, store.ProjectInput{
			ProjectID: proj.ID, TenantID: proj.TenantID, PlatformAssetID: ref,
			SnapshotName: ref, SnapshotContentType: "image/*",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) projectFor(ctx context.Context, p platform.Principal, id string) (store.Project, error) {
	seen := map[string]bool{}
	tenants := []string{p.Tenant(), entry.PersonalDefault + ":" + p.AccountID}
	tenants = append(tenants, p.OrgMemberships...)
	var last = store.ErrNotFound
	for _, tenant := range tenants {
		if tenant == "" || seen[tenant] {
			continue
		}
		seen[tenant] = true
		proj, err := s.St.GetProject(ctx, tenant, id)
		if err == nil {
			return proj, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.Project{}, err
		}
		last = err
	}
	return store.Project{}, last
}

func workspaceKind(proj store.Project) string {
	if strings.HasPrefix(proj.TenantID, entry.PersonalDefault+":") {
		return entry.ScopePersonal
	}
	return entry.ScopeOrganization
}

func workspaceScope(proj store.Project) string {
	if strings.HasPrefix(proj.TenantID, entry.PersonalDefault+":") {
		return entry.PersonalDefault
	}
	return proj.TenantID
}

func entryView(ws entry.Workspace, proj store.Project, href string, journey entry.Journey, session store.EntrySession, created bool) map[string]any {
	screen := entry.FirstScreen()
	return map[string]any{
		"screen": map[string]any{
			"title": screen.Title, "shows_catalog": screen.ShowsCatalog, "steps": screen.Steps,
		},
		"workspace": map[string]any{
			"kind": ws.Kind, "scope": ws.Scope, "storage_tenant": ws.StorageTenant,
		},
		"project":       proj,
		"return_href":   href,
		"session":       session,
		"resumed":       !created,
		"useful_proven": false,
		"journey": map[string]any{
			"steps": journey.Steps, "input_count": journey.InputCount,
			"reupload_count": journey.CrossProductReuploads, "recoveries": journey.Recoveries,
			"sla_promised": journey.SLAPromised,
		},
	}
}

func firstImageView(proj store.Project, session store.EntrySession) map[string]any {
	session.Charged = false
	if session.QuoteLabel == "" {
		session.QuoteLabel = entry.QuotePending
	}
	return map[string]any{
		"project": proj, "session": session, "useful_proven": false,
		"quote_label": session.QuoteLabel, "charged": false,
	}
}

func writeEntryErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, entry.ErrTenantMismatch), errors.Is(err, entry.ErrCustomerAssetInPersonal):
		writeErr(w, http.StatusForbidden, "tenant_mismatch", "来源租户与当前账号不匹配，客户资产不会写入个人空间")
	case errors.Is(err, entry.ErrSourceTenantRequired):
		writeErr(w, http.StatusBadRequest, "source_tenant_required", "订单或活动入口缺少已验证的组织，不能改落到个人空间")
	case errors.Is(err, entry.ErrReturnInvalid), errors.Is(err, entry.ErrReturnNotAllowed):
		writeErr(w, http.StatusBadRequest, "invalid_return", "返回地址不可用")
	default:
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	}
}
