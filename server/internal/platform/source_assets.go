package platform

import (
	"context"
	"encoding/json"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
	"time"
)

func (c *sourceAssetClient) client(s si.Scope) (*pc.Client, error) {
	if !s.Valid() || s.AppID != c.cfg.AppID {
		return nil, si.ErrInvalid
	}
	return &pc.Client{Caller: pc.Caller{Mode: "app", AppID: s.AppID, Token: c.cfg.UploadToken, PrincipalType: "user", PrincipalID: s.UserID}, Bases: pc.Bases{Upload: c.cfg.UploadBaseURL}, HTTP: c.http}, nil
}
func sourceUploadScope(s si.Scope) map[string]string {
	return map[string]string{"asset_version": "durable_asset_v1", "app_id": s.AppID, "principal_type": "user", "principal_id": s.UserID}
}
func sourceOutputScoped(s si.Scope, o si.Output) bool {
	return o.AssetID != "" && o.ReferenceID != "" && o.ProjectID == s.ProjectID && o.AppID == s.AppID && o.PrincipalType == "user" && o.PrincipalID == s.UserID && o.ContentType == "image/png" && o.SizeBytes > 0 && o.SizeBytes <= 16<<20 && sourceSHA(o.SHA256)
}
func (c *sourceAssetClient) Verify(ctx context.Context, r si.Run) (si.Output, error) {
	if r.Owner != si.Owner || r.Task == nil || r.Task.Output == nil {
		return si.Output{}, si.ErrInvariant
	}
	o := *r.Task.Output
	s := r.Intent.Scope
	if !sourceOutputScoped(s, o) {
		return si.Output{}, si.ErrInvariant
	}
	cl, e := c.client(s)
	if e != nil {
		return o, e
	}
	body, _, e := sourceCall(ctx, cl, pc.Call{Operation: "upload.durable_asset_get", PathParams: map[string]string{"asset_id": o.AssetID}, Query: sourceUploadScope(s)}, "")
	if e != nil {
		return o, e
	}
	env, e := sourceObject(body, "ok asset", "")
	if e != nil {
		return o, e
	}
	ok, e := sourceBool(env, "ok")
	if e != nil || !ok {
		return o, si.ErrInvariant
	}
	a, e := sourceObject(env["asset"], "asset_id app_id principal_type principal_id asset_version filename content_type sha256 status size_bytes active_reference_count", "completed_at retention_until deleted_at cleanup_state")
	if e != nil {
		return o, e
	}
	for k, v := range map[string]string{"asset_id": o.AssetID, "app_id": s.AppID, "principal_type": "user", "principal_id": s.UserID, "asset_version": "durable_asset_v1", "content_type": o.ContentType, "sha256": o.SHA256, "status": "ready"} {
		if sourceString(a, k) != v {
			return o, si.ErrInvariant
		}
	}
	size, e := sourceInt(a, "size_bytes")
	if e != nil || size != o.SizeBytes {
		return o, si.ErrInvariant
	}
	n, e := sourceInt(a, "active_reference_count")
	if e != nil || n < 1 {
		return o, si.ErrInvariant
	}
	state, e := c.Reference(ctx, si.Outbox{Scope: s, Output: o})
	if e != nil {
		return o, e
	}
	if state != "active" {
		return o, si.ErrInvariant
	}
	o.Status = "ready"
	o.ReferenceState = state
	return o, nil
}
func (c *sourceAssetClient) Reference(ctx context.Context, item si.Outbox) (string, error) {
	cl, e := c.client(item.Scope)
	if e != nil || !sourceOutputScoped(item.Scope, item.Output) {
		return "", si.ErrInvalid
	}
	q := sourceUploadScope(item.Scope)
	q["project_id"] = item.Scope.ProjectID
	body, _, e := sourceCall(ctx, cl, pc.Call{Operation: "upload.durable_reference_get", PathParams: map[string]string{"asset_id": item.Output.AssetID, "reference_id": item.Output.ReferenceID}, Query: q}, "")
	if e != nil {
		return "", e
	}
	return sourceReferenceReply(body, item)
}
func (c *sourceAssetClient) Release(ctx context.Context, item si.Outbox) error {
	cl, e := c.client(item.Scope)
	if e != nil || !sourceOutputScoped(item.Scope, item.Output) {
		return si.ErrInvalid
	}
	m := map[string]string{}
	for k, v := range sourceUploadScope(item.Scope) {
		m[k] = v
	}
	m["project_id"] = item.Scope.ProjectID
	raw, _ := json.Marshal(m)
	body, _, e := sourceCall(ctx, cl, pc.Call{Operation: "upload.durable_release", PathParams: map[string]string{"asset_id": item.Output.AssetID, "reference_id": item.Output.ReferenceID}, Body: raw}, "")
	if e != nil {
		return e
	}
	state, e := sourceReferenceReply(body, item)
	if e != nil || state != "released" {
		return si.ErrInvariant
	}
	return nil
}
func sourceReferenceReply(body []byte, item si.Outbox) (string, error) {
	env, e := sourceObject(body, "ok reference", "")
	if e != nil {
		return "", e
	}
	ok, e := sourceBool(env, "ok")
	if e != nil || !ok {
		return "", si.ErrInvariant
	}
	m, e := sourceObject(env["reference"], "asset_version app_id principal_type principal_id asset_id project_id reference_id state created_at", "released_at")
	if e != nil {
		return "", e
	}
	for k, v := range map[string]string{"asset_version": "durable_asset_v1", "app_id": item.Scope.AppID, "principal_type": "user", "principal_id": item.Scope.UserID, "asset_id": item.Output.AssetID, "project_id": item.Scope.ProjectID, "reference_id": item.Output.ReferenceID} {
		if sourceString(m, k) != v {
			return "", si.ErrInvariant
		}
	}
	state := sourceString(m, "state")
	if state != "active" && state != "released" {
		return "", si.ErrInvariant
	}
	return state, nil
}

// downloadGrant is private: Task6's bounded fetcher must reverify the reference
// then validate the origin and actual bytes before publishing any content.
func (c *sourceAssetClient) downloadGrant(ctx context.Context, r si.Run) (string, error) {
	if r.Owner != si.Owner || r.Output == nil || !sourceOutputScoped(r.Intent.Scope, *r.Output) {
		return "", si.ErrInvariant
	}
	o := r.Output
	cl, e := c.client(r.Intent.Scope)
	if e != nil {
		return "", e
	}
	raw, _ := json.Marshal(sourceUploadScope(r.Intent.Scope))
	body, _, e := sourceCall(ctx, cl, pc.Call{Operation: "upload.durable_download", PathParams: map[string]string{"asset_id": o.AssetID}, Body: raw}, "")
	if e != nil {
		return "", e
	}
	m, e := sourceObject(body, "ok download_url expires_at asset", "")
	if e != nil {
		return "", e
	}
	ok, e := sourceBool(m, "ok")
	if e != nil || !ok {
		return "", si.ErrInvariant
	}
	a, e := sourceObject(m["asset"], "asset_id app_id principal_type principal_id asset_version filename content_type sha256 status size_bytes active_reference_count", "completed_at retention_until deleted_at cleanup_state")
	if e != nil {
		return "", e
	}
	for k, v := range map[string]string{"asset_id": o.AssetID, "app_id": r.Intent.Scope.AppID, "principal_type": "user", "principal_id": r.Intent.Scope.UserID, "asset_version": "durable_asset_v1", "content_type": o.ContentType, "sha256": o.SHA256, "status": "ready"} {
		if sourceString(a, k) != v {
			return "", si.ErrInvariant
		}
	}
	size, e := sourceInt(a, "size_bytes")
	if e != nil || size != o.SizeBytes {
		return "", si.ErrInvariant
	}
	n, e := sourceInt(a, "active_reference_count")
	if e != nil || n < 1 {
		return "", si.ErrInvariant
	}
	u := sourceString(m, "download_url")
	expires, e := time.Parse(time.RFC3339Nano, sourceString(m, "expires_at"))
	if u == "" || e != nil || !expires.After(time.Now()) {
		return "", si.ErrInvariant
	}
	return u, nil
}
