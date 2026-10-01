package store

import (
	"database/sql"
	"encoding/json"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceSubmitBudgetMigrationPreservesOldAttemptAndRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source-0018.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	entries, e := migrationsFS.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.Name() >= "0019_source_submit_budget.sql" {
			continue
		}
		raw, _ := migrationsFS.ReadFile("migrations/" + entry.Name())
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(entry.Name(), e)
		}
		if _, e = db.Exec(`INSERT INTO schema_migrations VALUES(?,'prior')`, strings.TrimSuffix(entry.Name(), ".sql")); e != nil {
			t.Fatal(e)
		}
	}
	in := sourceIntent()
	fp, _ := si.Fingerprint(in)
	raw, _ := json.Marshal(in)
	now := Now().Unix()
	if _, e = db.Exec(`INSERT INTO projects(id,tenant_id,name,created_by,created_at,updated_at) VALUES(?,?,'old source',?,'prior','prior')`, in.Scope.ProjectID, in.Scope.TenantID, in.Scope.UserID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO product_source_runs(id,owner,app_id,user_id,tenant_id,project_id,payer_account_id,principal_account_id,request_key,fingerprint,intent_json,usage_key,task_key,business_ref,submit_attempted,created_at,updated_at) VALUES('sir_old',?,?,?,?,?,?,?,?,?,?,'old-usage-key','old-task-key','product-image:run:sir_old',1,?,?)`, si.Owner, in.Scope.AppID, in.Scope.UserID, in.Scope.TenantID, in.Scope.ProjectID, in.Scope.PayerAccountID, in.Scope.PrincipalAccountID, in.RequestKey, fp, string(raw), now, now); e != nil {
		t.Fatal(e)
	}
	// Inject failure after both ALTERs and triggers, before migration commit.
	if _, e = db.Exec(`CREATE TRIGGER budget_migration_failure BEFORE INSERT ON schema_migrations WHEN NEW.version='0019_source_submit_budget' BEGIN SELECT RAISE(ABORT,'injected');END`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if bad, e := Open(path); e == nil {
		bad.Close()
		t.Fatal("migration committed through injected failure")
	}
	db, e = sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	var columns int
	if e = db.QueryRow(`SELECT count(*) FROM pragma_table_info('product_source_runs') WHERE name IN('submit_started_at','submit_budget_seconds')`).Scan(&columns); e != nil || columns != 0 {
		t.Fatal("partial migration", columns, e)
	}
	if _, e = db.Exec(`DROP TRIGGER budget_migration_failure`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.GetSourceRun(t.Context(), in.Scope, "sir_old")
	if e != nil || !r.SubmitAttempted || r.SubmitStartedAt != 0 || r.SubmitBudgetSeconds != 0 || r.Intent != in || r.Fingerprint != fp || r.TaskKey != "old-task-key" || r.UpdatedAt != now {
		t.Fatal("old intent changed", r, e)
	}
	if _, e = s.db.Exec(`UPDATE product_source_runs SET submit_started_at=?,submit_budget_seconds=20 WHERE id='sir_old'`, now); e == nil {
		t.Fatal("historical unknown budget was fabricated")
	}
}
func TestSourceSubmitBudgetRejectsMalformedAndOverflowPairs(t *testing.T) {
	for _, set := range []string{
		"submit_started_at=1", "submit_budget_seconds=20", "submit_started_at=1,submit_budget_seconds=20",
		"submit_attempted=1,submit_started_at=9223372036854775807,submit_budget_seconds=20",
		"submit_attempted=1,submit_started_at=1,submit_budget_seconds=19",
		"submit_attempted=1,submit_started_at=1.5,submit_budget_seconds=20",
	} {
		t.Run(set, func(t *testing.T) {
			s := openTest(t)
			r := sourceConfirmedForTask(t, s, "bad-budget")
			if _, e := s.db.Exec("UPDATE product_source_runs SET "+set+" WHERE id=?", r.ID); e == nil {
				t.Fatal("invalid budget accepted")
			}
		})
	}
}
