package store

import (
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"reflect"
	"testing"
)

func TestSourceCancelLocalTerminalWithNullTaskIsImmutable(t *testing.T) {
	for _, phase := range []string{"succeeded", "failed", "canceled", "not_dispatched"} {
		t.Run(phase, func(t *testing.T) {
			s := openTest(t)
			r := sourceConfirmedForTask(t, s, "local-terminal")
			if _, e := s.db.Exec(`UPDATE product_source_runs SET phase=? WHERE id=?`, phase, r.ID); e != nil {
				t.Fatal(e)
			}
			before, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if e != nil || before.Task != nil {
				t.Fatal(before, e)
			}
			out, e := s.CancelSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if !errors.Is(e, si.ErrConflict) || out.ID != r.ID {
				t.Fatal("local terminal cancellation allowed", out, e)
			}
			after, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("local terminal changed", after, e)
			}
		})
	}
}
func TestSourceCancelTerminalStillChecksScopeAndProject(t *testing.T) {
	for _, kind := range []string{"wrong-scope", "archived", "deleted", "other-owner"} {
		t.Run(kind, func(t *testing.T) {
			s := openTest(t)
			r := sourceConfirmedForTask(t, s, "terminal-authority")
			if _, e := s.db.Exec(`UPDATE product_source_runs SET phase='succeeded' WHERE id=?`, r.ID); e != nil {
				t.Fatal(e)
			}
			before, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			scope := r.Intent.Scope
			want := si.ErrNotFound
			switch kind {
			case "wrong-scope":
				scope.UserID = "other"
			case "archived", "deleted":
				if _, e = s.UpdateProject(t.Context(), scope.TenantID, scope.ProjectID, ProjectUpdate{Status: &kind}); e != nil {
					t.Fatal(e)
				}
				if kind == "archived" {
					want = si.ErrForbidden
				}
			case "other-owner":
				if _, e = s.db.Exec(`UPDATE projects SET created_by='other' WHERE id=?`, scope.ProjectID); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.CancelSourceRun(t.Context(), scope, r.ID); !errors.Is(e, want) {
				t.Fatal("terminal guard bypassed current authority", kind, e)
			}
			after, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal(after, e)
			}
		})
	}
}
