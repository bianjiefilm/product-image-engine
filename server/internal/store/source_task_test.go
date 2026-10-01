package store

import (
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"path/filepath"
	"testing"
	"time"
)

func sourceConfirmedForTask(t *testing.T, s *Store, key string) si.Run {
	t.Helper()
	in := sourceIntent()
	in.RequestKey = key
	p, e := s.CreateProject(t.Context(), Project{TenantID: in.Scope.TenantID, CreatedBy: in.Scope.UserID, Name: "source", WidthPx: 800, HeightPx: 800, SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	in.Scope.ProjectID = p.ID
	r, _, e := s.CreateSourceRun(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	q := sourceQuote(r)
	q.UsageID = "usage-" + key
	q.QuoteID = "quote-" + key
	q.QuotedAtUnix = Now().Unix()
	q.ExpiresAtUnix = q.QuotedAtUnix + 300
	r, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.ConfirmSourceRun(t.Context(), r.Intent.Scope, r.ID, si.QuoteHash(q), Now().Unix())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestSourceTaskMarkerAndCancelHaveOneWinner(t *testing.T) {
	s := openTest(t)
	r := sourceConfirmedForTask(t, s, "marker")
	r = leaseForQuote(t, s, r)
	marked, e := s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
	if e != nil || !marked.SubmitAttempted {
		t.Fatal(marked, e)
	}
	canceled, e := s.CancelSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || !canceled.CancelRequested || canceled.Phase != "cancel_pending" || !canceled.SubmitAttempted {
		t.Fatal(canceled, e)
	}
	r = sourceConfirmedForTask(t, s, "cancel-first")
	r = leaseForQuote(t, s, r)
	if _, e = s.CancelSourceRun(t.Context(), r.Intent.Scope, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("marker after cancel", e)
	}
}
func TestSourceTaskMarkerChecksProjectBeforeNetwork(t *testing.T) {
	for _, kind := range []string{"archived", "deleted", "creator-changed"} {
		t.Run(kind, func(t *testing.T) {
			s := openTest(t)
			r := sourceConfirmedForTask(t, s, kind)
			r = leaseForQuote(t, s, r)
			if kind == "creator-changed" {
				_, e := s.db.Exec(`UPDATE projects SET created_by='other' WHERE id=?`, r.Intent.Scope.ProjectID)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := s.UpdateProject(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, ProjectUpdate{Status: &kind}); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch); e == nil {
				t.Fatal("marker accepted invalid current project", kind)
			}
		})
	}
}
func TestSourceTaskObservationBindsCompleteFactAndUniqueID(t *testing.T) {
	s := openTest(t)
	r := sourceConfirmedForTask(t, s, "a")
	r = leaseForQuote(t, s, r)

	r, e := s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	fact := si.TaskFact{ID: "task-one", IdempotencyKey: r.TaskKey, Scope: r.Intent.Scope, Quote: *r.Quote, Provider: r.Intent.Provider, Capability: r.Intent.Capability, Status: "queued", Phase: "hold_pending"}
	next := r
	next.Task = &fact
	next.TaskID = fact.ID
	next.Phase = "task_linked"
	bound, e := s.SaveSourceObservation(t.Context(), next, r.Revision, r.LeaseEpoch)
	if e != nil || bound.TaskID != fact.ID {
		t.Fatal(bound, e)
	}
	stale := next
	stale.TaskID = "retarget"
	wrong := fact
	wrong.ID = "retarget"
	stale.Task = &wrong
	if _, e = s.SaveSourceObservation(t.Context(), stale, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("stale fact writer", e)
	}
	other := sourceConfirmedForTask(t, s, "b")
	other = leaseForQuote(t, s, other)

	other, e = s.MarkSourceSubmitAttempt(t.Context(), other, other.Revision, other.LeaseEpoch)
	if e != nil {
		t.Fatal(e)
	}
	next = other
	fact.IdempotencyKey = other.TaskKey
	fact.Scope = other.Intent.Scope
	fact.Quote = *other.Quote
	next.Task = &fact
	next.TaskID = fact.ID
	next.Phase = "task_linked"
	if _, e = s.SaveSourceObservation(t.Context(), next, other.Revision, other.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("one Task bound to two product runs", e)
	}
}
func TestSourceTaskObservationRejectsOutputAndTerminalPromotion(t *testing.T) {
	s := openTest(t)
	r := sourceConfirmedForTask(t, s, "promotion")
	r = leaseForQuote(t, s, r)
	for _, edit := range []func(*si.Run){func(r *si.Run) { r.Phase = "succeeded" }, func(r *si.Run) { r.Output = &si.Output{AssetID: "unverified"} }, func(r *si.Run) { r.Bill = &si.BillFact{ChargeID: "fake"} }, func(r *si.Run) { r.Selected = true }, func(r *si.Run) { r.ConfirmationHash = "other" }} {
		bad := r
		edit(&bad)
		if _, e := s.SaveSourceObservation(t.Context(), bad, r.Revision, r.LeaseEpoch); e == nil {
			t.Fatal("caller promoted output/funds without authority", bad)
		}
	}
}
func TestSourceTaskDueSkipsFrozenUnconfirmedAndLeased(t *testing.T) {
	s := openTest(t)
	r := sourceConfirmedForTask(t, s, "due")
	rows, e := s.ListDueSourceRuns(t.Context(), Now().Unix(), 10)
	if e != nil || len(rows) != 1 || rows[0].ID != r.ID {
		t.Fatal(rows, e)
	}
	r = leaseForQuote(t, s, r)
	rows, e = s.ListDueSourceRuns(t.Context(), Now().Unix(), 10)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	if _, e = s.ListDueSourceRuns(t.Context(), Now().Unix(), 1000); !errors.Is(e, si.ErrInvalid) {
		t.Fatal("unbounded pass", e)
	}
}

func TestSourceTaskMarkerFirstBudgetSurvivesReopenAndRetry(t *testing.T) {
	oldNow := Now
	clock := Now()
	Now = func() time.Time { return clock }
	t.Cleanup(func() { Now = oldNow })
	path := filepath.Join(t.TempDir(), "task-budget.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.Close() }()
	r := sourceConfirmedForTask(t, s, "budget")
	r = leaseForQuote(t, s, r)
	r, e = s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
	if e != nil || r.SubmitStartedAt != clock.Unix() || r.SubmitBudgetSeconds != 20 {
		t.Fatal(r, e)
	}
	first := r.SubmitStartedAt
	if _, e = s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal("resubmitted before original request budget ended", e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || r.SubmitStartedAt != first || r.SubmitBudgetSeconds != 20 {
		t.Fatal(r, e)
	}
	clock = clock.Add(31 * time.Second)
	r = leaseForQuote(t, s, r)
	again, e := s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
	if e != nil || again.SubmitStartedAt != first || again.SubmitBudgetSeconds != 20 {
		t.Fatal("retry refreshed original budget", again, e)
	}
	for _, set := range []string{"submit_started_at=submit_started_at+1", "submit_budget_seconds=0"} {
		if _, e = s.db.Exec(`UPDATE product_source_runs SET `+set+` WHERE id=?`, r.ID); e == nil {
			t.Fatal("SQL rewrote first request budget", set)
		}
	}
}
func TestSourceTaskOldAttemptWithoutBudgetCannotMintStart(t *testing.T) {
	s := openTest(t)
	r := sourceConfirmedForTask(t, s, "old-unknown")
	if _, e := s.db.Exec(`UPDATE product_source_runs SET submit_attempted=1 WHERE id=?`, r.ID); e != nil {
		t.Fatal(e)
	}
	r, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	r = leaseForQuote(t, s, r)
	if _, e = s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch); e == nil {
		t.Fatal("old unknown attempt gained invented start time")
	}
}

func TestSourceCancelRechecksArchivedProjectInTransaction(t *testing.T) {
	s := openTest(t)
	r := sourceConfirmedForTask(t, s, "archive-cancel")
	state := "archived"
	if _, e := s.UpdateProject(t.Context(), r.Intent.Scope.TenantID, r.Intent.Scope.ProjectID, ProjectUpdate{Status: &state}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CancelSourceRun(t.Context(), r.Intent.Scope, r.ID); !errors.Is(e, si.ErrForbidden) {
		t.Fatal("archived cancel allowed", e)
	}
	got, e := s.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
	if e != nil || got.CancelRequested || got.Revision != r.Revision {
		t.Fatal(got, e)
	}
}

func TestSourceTaskRepeatConfirmPreservesProgressedPhase(t *testing.T) {
	for _, status := range []string{"queued", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			s := openTest(t)
			r := sourceConfirmedForTask(t, s, "confirm-progress")
			r = leaseForQuote(t, s, r)
			r, e := s.MarkSourceSubmitAttempt(t.Context(), r, r.Revision, r.LeaseEpoch)
			if e != nil {
				t.Fatal(e)
			}
			fact := si.TaskFact{ID: "task-progress", IdempotencyKey: r.TaskKey, Scope: r.Intent.Scope, Quote: *r.Quote, Provider: r.Intent.Provider, Capability: r.Intent.Capability, Status: status, Phase: "provider_done"}
			phase := "task_linked"
			if status == "succeeded" {
				fact.HoldID = "hold-progress"
				fact.ChargeID = "charge-progress"
				fact.Output = &si.Output{AssetID: "asset-progress", ReferenceID: "ref-progress", ProjectID: r.Intent.Scope.ProjectID, PrincipalID: r.Intent.Scope.UserID, PrincipalType: "user", AppID: r.Intent.Scope.AppID, ContentType: "image/png", SizeBytes: 123, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
				phase = "asset_pending"
			}
			in := r
			in.Task = &fact
			in.TaskID = fact.ID
			in.Phase = phase
			in.LeaseUntil = 0
			r, e = s.SaveSourceObservation(t.Context(), in, r.Revision, r.LeaseEpoch)
			if e != nil {
				t.Fatal(e)
			}
			r = leaseForQuote(t, s, r)
			oldEvents := r.EventsJSON
			oldConfirmed := r.ConfirmedAt
			got, e := s.ConfirmSourceRunForProject(t.Context(), r, r.ConfirmationHash, Now().Unix(), r.Revision, r.LeaseEpoch)
			if e != nil || got.Phase != phase || got.EventsJSON != oldEvents || got.ConfirmedAt != oldConfirmed || got.TaskID != fact.ID {
				t.Fatal("confirm rewound task", got, e)
			}
		})
	}
}
