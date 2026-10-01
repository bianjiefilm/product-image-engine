package store

import (
	"context"
	"database/sql"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// CreateSourceRunForProject is the authorized product entry. The caller supplies
// live Context proof; this owned transaction checks original project ownership
// and archive/deletion again before even replaying or creating an intent.
func (s *Store) CreateSourceRunForProject(ctx context.Context, in si.Intent) (si.Run, bool, error) {
	return s.createSourceRun(ctx, in, true)
}
func sourceProjectActive(ctx context.Context, c *sql.Conn, scope si.Scope) error {
	var tenant, creator, status string
	e := c.QueryRowContext(ctx, `SELECT tenant_id,created_by,status FROM projects WHERE id=? AND tenant_id=?`, scope.ProjectID, scope.TenantID).Scan(&tenant, &creator, &status)
	if errors.Is(e, sql.ErrNoRows) || status == "deleted" {
		return si.ErrNotFound
	}
	if e != nil {
		return e
	}
	personal := tenant == scope.PrincipalAccountID || tenant == "personal/default:"+scope.PrincipalAccountID
	if personal && creator != scope.UserID || !personal && privateSourceTenant(tenant) {
		return si.ErrNotFound
	}
	if status == "archived" {
		return si.ErrForbidden
	}
	return nil
}

// ListSourceRunsPage only transports a stable created_at/id position. Every SQL
// read uses the complete authorization scope; a cursor never grants access.
func (s *Store) ListSourceRunsPage(ctx context.Context, scope si.Scope, limit int, beforeAt int64, beforeID string) (runs []si.Run, nextAt int64, nextID string, err error) {
	if !scope.Valid() || limit < 1 || limit > 100 || beforeAt < 0 || (beforeAt == 0) != (beforeID == "") {
		return nil, 0, "", si.ErrInvalid
	}
	query := `SELECT ` + sourceColumns + ` FROM product_source_runs WHERE ` + sourceScopeSQL
	args := sourceScopeArgs(scope)
	if beforeID != "" {
		query += ` AND (created_at<? OR (created_at=? AND id<?))`
		args = append(args, beforeAt, beforeAt, beforeID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, e := s.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, 0, "", e
	}
	runs = []si.Run{}
	for rows.Next() {
		r, e := scanSource(rows)
		if e != nil {
			rows.Close()
			return nil, 0, "", e
		}
		runs = append(runs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, "", e
	}
	if len(runs) > limit {
		runs = runs[:limit]
		nextID = runs[len(runs)-1].ID
		args = append([]any{nextID}, sourceScopeArgs(scope)...)
		err = s.db.QueryRowContext(ctx, `SELECT created_at FROM product_source_runs WHERE id=? AND `+sourceScopeSQL, args...).Scan(&nextAt)
	}
	return
}
