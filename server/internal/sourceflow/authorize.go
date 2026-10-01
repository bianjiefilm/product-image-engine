package sourceflow

import (
	"context"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"reflect"
	"strings"
)

type Authorizer struct {
	Store   *store.Store
	Context ContextPort
	AppID   string
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// canonicalPersonalStorage matches Identity's account key: acct_ followed by
// sixteen lowercase hexadecimal characters. Other acct_* names remain eligible
// for real organization Context validation; a private account never does.
func canonicalPersonalStorage(tenant string) bool {
	if len(tenant) != len("acct_")+16 || !strings.HasPrefix(tenant, "acct_") {
		return false
	}
	for _, c := range tenant[len("acct_"):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (a *Authorizer) ForProject(ctx context.Context, actor Actor, id string, write bool) (Authorization, error) {
	if a == nil || a.Store == nil || id == "" {
		return Authorization{}, si.ErrNotFound
	}
	probe := si.Scope{AppID: a.AppID, TenantID: actor.AccountID, ProjectID: id, UserID: actor.UserID, PrincipalAccountID: actor.AccountID, PayerAccountID: actor.AccountID}
	if !probe.Valid() {
		return Authorization{}, si.ErrNotFound
	}
	for _, tenant := range []string{actor.AccountID, "personal/default:" + actor.AccountID} {
		p, e := a.Store.GetProject(ctx, tenant, id)
		if e != nil {
			if errors.Is(e, store.ErrNotFound) {
				continue
			}
			return Authorization{}, si.ErrUnavailable
		}
		if p.CreatedBy != actor.UserID {
			return Authorization{}, si.ErrNotFound
		}
		if p.Status == "deleted" {
			return Authorization{}, si.ErrNotFound
		}
		if write && p.Status == "archived" {
			return Authorization{}, si.ErrForbidden
		}
		probe.TenantID = p.TenantID
		return Authorization{Scope: probe, PayerSource: "personal", Role: "owner"}, nil
	}
	p, e := a.Store.GetProjectForSourceAuthorization(ctx, id)
	if e != nil {
		return Authorization{}, si.ErrNotFound
	}
	if canonicalPersonalStorage(p.TenantID) || strings.HasPrefix(p.TenantID, "personal/default:") || p.Status == "deleted" || nilPort(a.Context) {
		return Authorization{}, si.ErrNotFound
	}
	q := si.ContextQuery{AppID: a.AppID, UserID: actor.UserID, ReadOnly: !write}
	if write {
		q.TenantID = p.TenantID
	}
	f, e := a.Context.Resolve(ctx, q)
	if e != nil || !f.Resolved || !f.PayerKnown || f.AppID != a.AppID || f.UserID != actor.UserID || f.TenantID != p.TenantID || f.PayerSource != "delegation" || f.PayerAccountID == "" {
		return Authorization{}, si.ErrNotFound
	}
	allowedRole := f.Role == "owner" || f.Role == "admin" || f.Role == "editor"
	allowedMember := f.MemberRole == "owner" || f.MemberRole == "payer"
	if !write {
		allowedRole = allowedRole || f.Role == "member" || f.Role == "viewer"
		allowedMember = allowedMember || f.MemberRole == "viewer"
	}
	if !allowedRole || !allowedMember {
		return Authorization{}, si.ErrNotFound
	}
	if write && p.Status == "archived" {
		return Authorization{}, si.ErrForbidden
	}
	probe.TenantID = p.TenantID
	probe.PayerAccountID = f.PayerAccountID
	if !probe.Valid() {
		return Authorization{}, si.ErrNotFound
	}
	return Authorization{Scope: probe, PayerSource: f.PayerSource, Role: f.Role}, nil
}
func (s *Service) LoadRun(ctx context.Context, actor Actor, project, id string, write bool) (si.Run, error) {
	if s == nil || s.Store == nil || s.Auth == nil {
		return si.Run{}, si.ErrNotFound
	}
	auth, e := s.Auth.ForProject(ctx, actor, project, write)
	if e != nil {
		return si.Run{}, e
	}
	r, e := s.Store.GetSourceRunForRecovery(ctx, id)
	if e != nil {
		return si.Run{}, si.ErrNotFound
	}
	frozen := r.Intent.Scope
	current := auth.Scope
	if frozen.AppID != current.AppID || frozen.TenantID != current.TenantID || frozen.ProjectID != current.ProjectID || frozen.UserID != current.UserID || frozen.PrincipalAccountID != current.PrincipalAccountID {
		return si.Run{}, si.ErrNotFound
	}
	if write && frozen.PayerAccountID != current.PayerAccountID {
		return si.Run{}, si.ErrConflict
	}
	return s.Store.GetSourceRun(ctx, frozen, id)
}
