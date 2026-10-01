package store

import (
	"context"
	"database/sql"
	"errors"
)

// GetProjectForSourceAuthorization discovers metadata only for the source
// Authorizer's live Context check. It grants no access and must never be used
// as an HTTP resource read or exposed before formal authorization succeeds.
func (s *Store) GetProjectForSourceAuthorization(ctx context.Context, id string) (Project, error) {
	p, e := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, e
}
