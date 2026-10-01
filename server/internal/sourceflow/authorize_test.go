package sourceflow

import (
	"context"
	"errors"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"path/filepath"
	"testing"
)

type authContext struct {
	fact    si.ContextFact
	err     error
	queries []si.ContextQuery
}

func (c *authContext) Resolve(ctx context.Context, q si.ContextQuery) (si.ContextFact, error) {
	c.queries = append(c.queries, q)
	return c.fact, c.err
}
func authStore(t *testing.T) *store.Store {
	t.Helper()
	s, e := store.Open(filepath.Join(t.TempDir(), "source-auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func authProject(t *testing.T, s *store.Store, tenant, user string) store.Project {
	t.Helper()
	p, e := s.CreateProject(t.Context(), store.Project{TenantID: tenant, CreatedBy: user, Name: "source auth", WidthPx: 800, HeightPx: 800, SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestSourceAuthPersonalNoOrganizationClaims(t *testing.T) {
	s := authStore(t)
	ctx := &authContext{err: si.ErrForbidden}
	a := &Authorizer{Store: s, Context: ctx, AppID: "product-image"}
	actor := Actor{UserID: "usr-a", AccountID: "acct-a"}
	for _, tenant := range []string{"acct-a", "personal/default:acct-a"} {
		p := authProject(t, s, tenant, actor.UserID)
		got, e := a.ForProject(t.Context(), actor, p.ID, true)
		if e != nil || got.Scope.UserID != actor.UserID || got.Scope.PayerAccountID != actor.AccountID || got.PayerSource != "personal" {
			t.Fatal(got, e)
		}
	}
	if len(ctx.queries) != 0 {
		t.Fatal("personal requires nonexistent org membership", ctx.queries)
	}
}
func TestSourceAuthOtherPersonalOwnerIsHidden(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "personal/default:acct-other", "usr-other")
	a := &Authorizer{Store: s, AppID: "product-image"}
	if _, e := a.ForProject(t.Context(), Actor{UserID: "usr-a", AccountID: "acct-a"}, p.ID, false); !errors.Is(e, si.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestSourceAuthOrgFactsAndReadSelector(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "org-a", "usr-owner")
	ctx := &authContext{fact: si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr-a", TenantID: "org-a", Role: "editor", PayerAccountID: "org-payer", PayerSource: "delegation", MemberRole: "payer"}}
	a := &Authorizer{Store: s, Context: ctx, AppID: "product-image"}
	actor := Actor{UserID: "usr-a", AccountID: "acct-a"}
	for _, write := range []bool{false, true} {
		got, e := a.ForProject(t.Context(), actor, p.ID, write)
		if e != nil || got.Scope.TenantID != "org-a" || got.Scope.PayerAccountID != "org-payer" {
			t.Fatal(got, e)
		}
		q := ctx.queries[len(ctx.queries)-1]
		if q.ReadOnly != !write || q.SourceRef != "" || q.TenantID != map[bool]string{false: "", true: "org-a"}[write] {
			t.Fatal("selector discipline", q)
		}
	}
}
func TestSourceAuthOrgMissingRevokedOrForeignFactsAreHidden(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "org-a", "usr-owner")
	valid := si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr-a", TenantID: "org-a", Role: "editor", PayerAccountID: "org-payer", PayerSource: "delegation", MemberRole: "payer"}
	edits := []func(*si.ContextFact){func(f *si.ContextFact) { f.Resolved = false }, func(f *si.ContextFact) { f.PayerKnown = false }, func(f *si.ContextFact) { f.PayerSource = "personal" }, func(f *si.ContextFact) { f.TenantID = "org-b" }, func(f *si.ContextFact) { f.UserID = "usr-other" }, func(f *si.ContextFact) { f.AppID = "other-app" }, func(f *si.ContextFact) { f.Role = "viewer" }, func(f *si.ContextFact) { f.MemberRole = "viewer" }, func(f *si.ContextFact) { f.PayerAccountID = "" }, func(f *si.ContextFact) { f.Role = "unknown" }}
	for i, edit := range edits {
		f := valid
		edit(&f)
		ctx := &authContext{fact: f}
		a := &Authorizer{Store: s, Context: ctx, AppID: "product-image"}
		if _, e := a.ForProject(t.Context(), Actor{UserID: "usr-a", AccountID: "acct-a"}, p.ID, true); !errors.Is(e, si.ErrNotFound) {
			t.Fatal(i, e)
		}
	}
	a := &Authorizer{Store: s, AppID: "product-image"}
	if _, e := a.ForProject(t.Context(), Actor{UserID: "usr-a", AccountID: "acct-a"}, p.ID, true); !errors.Is(e, si.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestSourceAuthArchiveKeepsRead(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "acct-a", "usr-a")
	status := "archived"
	if _, e := s.UpdateProject(t.Context(), p.TenantID, p.ID, store.ProjectUpdate{Status: &status}); e != nil {
		t.Fatal(e)
	}
	a := &Authorizer{Store: s, AppID: "product-image"}
	actor := Actor{UserID: "usr-a", AccountID: "acct-a"}
	if _, e := a.ForProject(t.Context(), actor, p.ID, false); e != nil {
		t.Fatal(e)
	}
	if _, e := a.ForProject(t.Context(), actor, p.ID, true); !errors.Is(e, si.ErrForbidden) {
		t.Fatal(e)
	}
}
func TestSourceAuthProfileDefaultClosed(t *testing.T) {
	for _, source := range []string{"personal", "delegation", "unknown"} {
		if (Profile{}).CanCreate(source) {
			t.Fatal(source)
		}
	}
	p := Profile{Enabled: true, PricingVersion: "price-a", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Quantity: 1}
	if !p.CanCreate("personal") || p.CanCreate("delegation") {
		t.Fatal(p)
	}
	p.OrgEnabled = true
	if !p.CanCreate("delegation") || p.CanCreate("unknown") {
		t.Fatal(p)
	}
}
func TestSourceAuthSameAccountDifferentCreatorIsHidden(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "acct-a", "usr-other")
	ctx := &authContext{fact: si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr-a", TenantID: "acct-a", Role: "owner", PayerAccountID: "acct-a", PayerSource: "delegation", MemberRole: "owner"}}
	a := &Authorizer{Store: s, Context: ctx, AppID: "product-image"}
	if _, e := a.ForProject(t.Context(), Actor{UserID: "usr-a", AccountID: "acct-a"}, p.ID, false); !errors.Is(e, si.ErrNotFound) || len(ctx.queries) != 0 {
		t.Fatal("creator mismatch fell into org", e, len(ctx.queries))
	}
}
func TestSourceAuthReadOnlyHistoricalPayerDoesNotRetarget(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "org-a", "usr-owner")
	scope := si.Scope{AppID: "product-image", TenantID: "org-a", ProjectID: p.ID, UserID: "usr-a", PrincipalAccountID: "acct-a", PayerAccountID: "payer-original"}
	r, _, e := s.CreateSourceRun(t.Context(), si.Intent{Scope: scope, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝纸盒", PricingVersion: "price-a", Quantity: 1})
	if e != nil {
		t.Fatal(e)
	}
	ctx := &authContext{fact: si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr-a", TenantID: "org-a", Role: "editor", PayerAccountID: "payer-new", PayerSource: "delegation", MemberRole: "payer"}}
	a := &Authorizer{Store: s, Context: ctx, AppID: "product-image"}
	svc := &Service{Store: s, Auth: a}
	actor := Actor{UserID: "usr-a", AccountID: "acct-a"}
	got, e := svc.LoadRun(t.Context(), actor, p.ID, r.ID, false)
	if e != nil || got.Intent.Scope.PayerAccountID != "payer-original" {
		t.Fatal(got, e)
	}
	if _, e = svc.LoadRun(t.Context(), actor, p.ID, r.ID, true); !errors.Is(e, si.ErrConflict) {
		t.Fatal("payer silently switched", e)
	}
}
func TestSourceAuthTypedNilContextIsClosed(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "org-a", "usr-owner")
	var c *authContext
	a := &Authorizer{Store: s, Context: c, AppID: "product-image"}
	if _, e := a.ForProject(t.Context(), Actor{UserID: "usr-a", AccountID: "acct-a"}, p.ID, true); !errors.Is(e, si.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestSourceAuthProfileConfigRequiresAllGates(t *testing.T) {
	c := config.Config{AppID: "product-image", IdentityAppID: "product-image", SourceImagesEnabled: true, SourceOrgImagesEnabled: true, BillingEnabled: true, GenerationEnabled: true, SourcePricingVersion: "price-a", SourceDownloadHosts: "sample.oss-cn-hangzhou.aliyuncs.com", IdentityBaseURL: "http://127.0.0.1:18101", BillingBaseURL: "http://127.0.0.1:18102", TaskBaseURL: "http://127.0.0.1:18103", UploadBaseURL: "http://127.0.0.1:18104", IdentityToken: "identity-test", BillingToken: "bill-test", TaskToken: "task-test", UploadToken: "upload-test"}
	if !NewProfile(c).CanCreate("personal") || !NewProfile(c).CanCreate("delegation") {
		t.Fatal("explicit full profile unavailable")
	}
	mutations := []func(*config.Config){func(c *config.Config) { c.SourceImagesEnabled = false }, func(c *config.Config) { c.GenerationEnabled = false }, func(c *config.Config) { c.BillingEnabled = false }, func(c *config.Config) { c.SourcePricingVersion = "" }, func(c *config.Config) { c.AppID = "other-app" }, func(c *config.Config) { c.BillingToken = c.TaskToken }, func(c *config.Config) { c.IdentityToken = "" }, func(c *config.Config) { c.UploadBaseURL = "" }, func(c *config.Config) { c.SourceDownloadHosts = "" }}
	for i, edit := range mutations {
		bad := c
		edit(&bad)
		if NewProfile(bad).CanCreate("personal") {
			t.Fatal(i, "unsafe readiness")
		}
	}
}
func TestSourceAuthLoadRunRejectsAnotherInitiatingUser(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "org-a", "usr-owner")
	scope := si.Scope{AppID: "product-image", TenantID: "org-a", ProjectID: p.ID, UserID: "usr-original", PrincipalAccountID: "acct-original", PayerAccountID: "org-payer"}
	r, _, e := s.CreateSourceRun(t.Context(), si.Intent{Scope: scope, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝纸盒", PricingVersion: "price-a", Quantity: 1})
	if e != nil {
		t.Fatal(e)
	}
	ctx := &authContext{fact: si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr-other", TenantID: "org-a", Role: "viewer", PayerAccountID: "org-payer", PayerSource: "delegation", MemberRole: "viewer"}}
	svc := &Service{Store: s, Auth: &Authorizer{Store: s, Context: ctx, AppID: "product-image"}}
	if _, e = svc.LoadRun(t.Context(), Actor{UserID: "usr-other", AccountID: "acct-other"}, p.ID, r.ID, false); !errors.Is(e, si.ErrNotFound) {
		t.Fatal("another member read original owner", e)
	}
}
func TestSourceAuthMalformedActorNeverQueriesContext(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "org-a", "usr-owner")
	ctx := &authContext{}
	a := &Authorizer{Store: s, Context: ctx, AppID: "product-image"}
	for _, actor := range []Actor{{UserID: "", AccountID: "acct-a"}, {UserID: "usr-a", AccountID: ""}, {UserID: "usr-a\n", AccountID: "acct-a"}} {
		if _, e := a.ForProject(t.Context(), actor, p.ID, false); !errors.Is(e, si.ErrNotFound) {
			t.Fatal(e)
		}
	}
	if len(ctx.queries) != 0 {
		t.Fatal(ctx.queries)
	}
}

func TestSourceAuthForeignCanonicalPersonalCannotBecomeOrganization(t *testing.T) {
	s := authStore(t)
	foreignAccount := "acct_0123456789abcdef"
	p := authProject(t, s, foreignAccount, "usr_0123456789abcdef")
	port := &authContext{fact: si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr_aabbccddeeff0011", TenantID: foreignAccount, Role: "editor", MemberRole: "payer", PayerAccountID: "org_payer", PayerSource: "delegation"}}
	a := &Authorizer{Store: s, Context: port, AppID: "product-image"}
	for _, write := range []bool{false, true} {
		got, e := a.ForProject(t.Context(), Actor{UserID: "usr_aabbccddeeff0011", AccountID: "acct_aabbccddeeff0011"}, p.ID, write)
		if !errors.Is(e, si.ErrNotFound) {
			t.Errorf("foreign personal became organization write=%v auth=%+v error=%v", write, got, e)
		}
	}
	if len(port.queries) != 0 {
		t.Errorf("foreign personal entered organization Context %v", port.queries)
	}
}
func TestSourceAuthAccountPrefixOrganizationIsAllowed(t *testing.T) {
	s := authStore(t)
	p := authProject(t, s, "acct_business_team", "usr-org-owner")
	port := &authContext{fact: si.ContextFact{Resolved: true, PayerKnown: true, AppID: "product-image", UserID: "usr-a", TenantID: p.TenantID, Role: "editor", MemberRole: "payer", PayerAccountID: "org-payer", PayerSource: "delegation"}}
	a := &Authorizer{Store: s, Context: port, AppID: "product-image"}
	for _, write := range []bool{false, true} {
		got, e := a.ForProject(t.Context(), Actor{UserID: "usr-a", AccountID: "acct-a"}, p.ID, write)
		if e != nil || got.Scope.PayerAccountID != "org-payer" {
			t.Fatal(got, e)
		}
	}
	if len(port.queries) != 2 {
		t.Fatal(port.queries)
	}
}

func TestSourceAuthCanonicalPersonalStillRequiresAccountAndCreator(t *testing.T) {
	s := authStore(t)
	actor := Actor{UserID: "usr_aabbccddeeff0011", AccountID: "acct_aabbccddeeff0011"}
	port := &authContext{err: si.ErrUnavailable}
	a := &Authorizer{Store: s, Context: port, AppID: "product-image"}
	own := authProject(t, s, actor.AccountID, actor.UserID)
	for _, write := range []bool{false, true} {
		if got, e := a.ForProject(t.Context(), actor, own.ID, write); e != nil || got.Scope.PayerAccountID != actor.AccountID {
			t.Fatal(got, e)
		}
	}
	for _, p := range []store.Project{authProject(t, s, actor.AccountID, "usr-other"), authProject(t, s, "acct_0123456789abcdef", actor.UserID)} {
		for _, write := range []bool{false, true} {
			if _, e := a.ForProject(t.Context(), actor, p.ID, write); !errors.Is(e, si.ErrNotFound) {
				t.Fatal(p, e)
			}
		}
	}
	if len(port.queries) != 0 {
		t.Fatal("private accounts queried Context", port.queries)
	}
	runs, e := s.ListSourceRuns(t.Context(), si.Scope{AppID: a.AppID, TenantID: actor.AccountID, ProjectID: own.ID, UserID: actor.UserID, PrincipalAccountID: actor.AccountID, PayerAccountID: actor.AccountID})
	if e != nil || len(runs) != 0 {
		t.Fatal("authorization created business facts", runs, e)
	}
}
