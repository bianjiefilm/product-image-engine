package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func sourceIntent() si.Intent {
	return si.Intent{Scope: si.Scope{AppID: "product-image", TenantID: "acct_u", ProjectID: "prj_a", UserID: "usr_u", PrincipalAccountID: "acct_u", PayerAccountID: "acct_u"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝纸盒", PricingVersion: "test-price-v1", Quantity: 1}
}
func sourceQuote(r si.Run) si.Quote {
	return si.Quote{UsageID: "usg_a", UsageKey: r.UsageKey, QuoteID: "quote_a", PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: 1000, ExpiresAtUnix: 1300}
}
func TestSourceRunReplayScopeAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	in := sourceIntent()
	a, dup, e := s.CreateSourceRun(t.Context(), in)
	if e != nil || dup {
		t.Fatal(a, dup, e)
	}
	b, dup, e := s.CreateSourceRun(t.Context(), in)
	if e != nil || !dup || a.ID != b.ID {
		t.Fatal(b, dup, e)
	}
	in.Prompt = "changed"
	if _, _, e = s.CreateSourceRun(t.Context(), in); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	for _, change := range []func(*si.Scope){func(s *si.Scope) { s.UserID = "other" }, func(s *si.Scope) { s.PayerAccountID = "other" }, func(s *si.Scope) { s.AppID = "other" }, func(s *si.Scope) { s.TenantID = "other" }, func(s *si.Scope) { s.ProjectID = "other" }, func(s *si.Scope) { s.PrincipalAccountID = "other" }} {
		scope := a.Intent.Scope
		change(&scope)
		if _, e = s.GetSourceRun(t.Context(), scope, a.ID); !errors.Is(e, si.ErrNotFound) {
			t.Fatal(scope, e)
		}
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b, e = s.GetSourceRun(t.Context(), a.Intent.Scope, a.ID)
	if e != nil || b.Fingerprint != a.Fingerprint || b.TaskKey != a.TaskKey || b.Owner != si.Owner {
		t.Fatal(b, e)
	}
	found, e := s.FindSourceRun(t.Context(), a.Intent.Scope, a.Intent.RequestKey)
	if e != nil || found.ID != a.ID {
		t.Fatal(found, e)
	}
	list, e := s.ListSourceRuns(t.Context(), a.Intent.Scope)
	if e != nil || len(list) != 1 {
		t.Fatal(list, e)
	}
}
func TestSourceQuoteImmutableConfirmation(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	q := sourceQuote(r)
	a, e := s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
	if e != nil || b.Quote.ExpiresAtUnix != 1300 {
		t.Fatal(b, e)
	}
	changed := q
	changed.QuoteID = "quote_b"
	if _, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, changed); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.ConfirmSourceRun(t.Context(), r.Intent.Scope, r.ID, "wrong", 1100); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	a, e = s.ConfirmSourceRun(t.Context(), r.Intent.Scope, r.ID, si.QuoteHash(q), 1100)
	if e != nil || a.ConfirmedAt != 1100 {
		t.Fatal(a, e)
	}
	a, e = s.ConfirmSourceRun(t.Context(), r.Intent.Scope, r.ID, si.QuoteHash(q), 1400)
	if e != nil || a.ConfirmedAt != 1100 {
		t.Fatal(a, e)
	}
	in := sourceIntent()
	in.RequestKey = "request-b"
	r, _, _ = s.CreateSourceRun(t.Context(), in)
	q = sourceQuote(r)
	q.UsageID = "usg_b"
	q.QuoteID = "q_b"
	_, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfirmSourceRun(t.Context(), r.Intent.Scope, r.ID, si.QuoteHash(q), 1300); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
}
func TestSourceClaimTwoStoresAndStaleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	a, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	r, _, e := a.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	type result struct {
		run     si.Run
		claimed bool
		err     error
	}
	ch := make(chan result, 2)
	var wg sync.WaitGroup
	for _, s := range []*Store{a, b} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			x, ok, e := s.ClaimSourceRun(context.Background(), r.Intent.Scope, r.ID, Now().Unix(), 30)
			ch <- result{x, ok, e}
		}(s)
	}
	wg.Wait()
	close(ch)
	var winner si.Run
	count := 0
	for x := range ch {
		if x.err != nil {
			t.Fatal(x.err)
		}
		if x.claimed {
			winner = x.run
			count++
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
	newer, ok, e := b.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix()+31, 30)
	if e != nil || !ok || newer.LeaseEpoch <= winner.LeaseEpoch {
		t.Fatal(newer, ok, e)
	}
	winner.LastError = "stale"
	if _, e = a.SaveSourceObservation(t.Context(), winner, winner.Revision, winner.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	newer.LastError = "network_unknown"
	saved, e := b.SaveSourceObservation(t.Context(), newer, newer.Revision, newer.LeaseEpoch)
	if e != nil || saved.LastError != "network_unknown" {
		t.Fatal(saved, e)
	}
	saved.Intent.Scope.PayerAccountID = "other"
	if _, e = b.SaveSourceObservation(t.Context(), saved, saved.Revision, saved.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
}
func TestSourceRunSQLCannotRewriteOwnerTupleOrQuote(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, sourceQuote(r))
	if e != nil {
		t.Fatal(e)
	}
	for _, set := range []string{"owner='legacy'", "payer_account_id='other'", "intent_json='{}'", "task_key='other'", "quote_json='{}'", "usage_id='other'"} {
		if _, e = s.db.Exec("UPDATE product_source_runs SET "+set+" WHERE id=?", r.ID); e == nil {
			t.Fatal(set)
		}
	}
}
func TestSourceMigrationAtomicPreservesLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	entries, e := migrationsFS.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)`)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range entries {
		if f.Name() >= "0018_source_image.sql" {
			continue
		}
		raw, _ := migrationsFS.ReadFile("migrations/" + f.Name())
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(f.Name(), e)
		}
		version := f.Name()[:len(f.Name())-4]
		if _, e = db.Exec(`INSERT INTO schema_migrations VALUES (?, 'prior')`, version); e != nil {
			t.Fatal(e)
		}
	}
	_, e = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE legacy_audit(id INTEGER PRIMARY KEY, value TEXT); CREATE TABLE legacy_child(parent INTEGER REFERENCES legacy_audit(id), value BLOB); CREATE INDEX legacy_value ON legacy_audit(value); INSERT INTO legacy_audit VALUES(19,'unchanged'); INSERT INTO legacy_child VALUES(19,X'0011FF'); CREATE TRIGGER source_migration_failure BEFORE INSERT ON schema_migrations WHEN NEW.version='0018_source_image' BEGIN SELECT RAISE(ABORT,'injected migration interruption'); END;`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO projects(id,tenant_id,name,created_by,created_at,updated_at) VALUES ('prj_old','acct_old','old','usr_old','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
INSERT INTO text_image_jobs(id,tenant_id,project_id,prompt,fingerprint,quote_status,billing_label,job_status,created_at,updated_at) VALUES ('txt_old','acct_old','prj_old','old prompt','fp-old','quoted','pending','quoted','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
INSERT INTO image_usage_quotes(id,storage_tenant,project_id,payer_kind,payer_account_ref,payer_display,image_count,resolution,capability,pricing_version,fingerprint,idempotency_key,status,created_at,updated_at) VALUES ('uq_old','acct_old','prj_old','personal','acct_old','personal',1,'800x800','old','v-old','fp-old','key-old','draft','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');`); e != nil {
		t.Fatal(e)
	}
	before := sourceLegacySnapshot(t, db)
	db.Close()
	if bad, e := Open(path); e == nil {
		bad.Close()
		t.Fatal("migration unexpectedly committed")
	}
	db, e = sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	if e = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='product_source_runs'`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	for key, want := range before {
		if got := sourceLegacySnapshot(t, db)[key]; got != want {
			t.Fatalf("rollback altered %s", key)
		}
	}
	if _, e = db.Exec(`DROP TRIGGER source_migration_failure`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	after := sourceLegacySnapshot(t, s.db)
	for key, want := range before {
		if key == "object:source_migration_failure" {
			continue
		}
		if after[key] != want {
			t.Fatalf("migration altered %s", key)
		}
	}
	if old, e := s.GetTextImageJob(t.Context(), "acct_old", "prj_old", "txt_old"); e != nil || old.Prompt != "old prompt" {
		t.Fatal(old, e)
	}
	if old, e := s.GetUsageQuoteByProject(t.Context(), "acct_old", "prj_old"); e != nil || old.PricingVersion != "v-old" {
		t.Fatal(old, e)
	}
	var value, hex string
	if e = s.db.QueryRow(`SELECT value FROM legacy_audit WHERE rowid=19`).Scan(&value); e != nil || value != "unchanged" {
		t.Fatal(value, e)
	}
	if e = s.db.QueryRow(`SELECT hex(value) FROM legacy_child WHERE parent=19`).Scan(&hex); e != nil || hex != "0011FF" {
		t.Fatal(hex, e)
	}
	if e = s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	rows, e := s.db.Query(`PRAGMA foreign_key_check`)
	if e != nil {
		t.Fatal(e)
	}
	if rows.Next() {
		t.Fatal("FK violation")
	}
	rows.Close()
	if _, e = s.db.Exec(`INSERT INTO legacy_child VALUES(88,X'00')`); e == nil {
		t.Fatal("FK disabled")
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='legacy_value'`).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}

func TestSourceRunLeaseExpiryAndProtectedObservation(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	r, ok, e := s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix()-100, 1)
	if e != nil || !ok {
		t.Fatal(e)
	}
	r.LastError = "network_unknown"
	if _, e = s.SaveSourceObservation(t.Context(), r, r.Revision, r.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("expired lease wrote: %v", e)
	}
	r, ok, e = s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix(), 30)
	if e != nil || !ok {
		t.Fatal(e)
	}
	for _, change := range []func(*si.Run){func(r *si.Run) { r.Owner = "legacy" }, func(r *si.Run) { r.Deleted = true }, func(r *si.Run) { r.Selected = true }, func(r *si.Run) { r.Phase = "succeeded" }, func(r *si.Run) { r.TaskID = "task_fake" }, func(r *si.Run) { r.SubmitAttempted = true }, func(r *si.Run) { r.Output = &si.Output{AssetID: "fake"} }} {
		bad := r
		change(&bad)
		if _, e = s.SaveSourceObservation(t.Context(), bad, bad.Revision, bad.LeaseEpoch); !errors.Is(e, si.ErrConflict) {
			t.Fatal(bad, e)
		}
	}
	for i, set := range []string{"revision=9223372036854775807", "lease_epoch=9223372036854775807"} {
		input := sourceIntent()
		input.RequestKey = fmt.Sprintf("overflow-%d", i)
		isolated, _, err := s.CreateSourceRun(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		r = isolated
		if _, e = s.db.Exec(`UPDATE product_source_runs SET lease_until=0,`+set+` WHERE id=?`, r.ID); e != nil {
			t.Fatal(e)
		}
		if _, _, e = s.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, Now().Unix(), 30); !errors.Is(e, si.ErrInvariant) {
			t.Fatal(set, e)
		}
	}
}
func TestSourceQuoteUsageUniqueAndNoDowngrade(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	q := sourceQuote(r)
	_, e = s.SaveSourceQuote(t.Context(), r.Intent.Scope, r.ID, q)
	if e != nil {
		t.Fatal(e)
	}
	in := sourceIntent()
	in.RequestKey = "another"
	other, _, e := s.CreateSourceRun(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	q2 := sourceQuote(other)
	if _, e = s.SaveSourceQuote(t.Context(), other.Intent.Scope, other.ID, q2); !errors.Is(e, si.ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.GetSourceRunForRecovery(t.Context(), r.ID); e != nil {
		t.Fatal(e)
	}
	// Simulate a wrong-owner historical record despite the CHECK, preserving the trigger.
	if _, e = s.db.Exec(`PRAGMA ignore_check_constraints=ON; DROP TRIGGER product_source_runs_immutable; UPDATE product_source_runs SET owner='legacy' WHERE id=?`, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetSourceRunForRecovery(t.Context(), r.ID); !errors.Is(e, si.ErrInvariant) {
		t.Fatal(e)
	}
}
func TestSourceOutboxIdentityAndForeignKey(t *testing.T) {
	s := openTest(t)
	r, _, e := s.CreateSourceRun(t.Context(), sourceIntent())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec(`INSERT INTO product_source_reference_outbox(id,run_id,scope_json,output_json,app_id,user_id,asset_id,project_id,reference_id,action,reason) VALUES ('o',?,'{}','{}','product-image','usr_u','asset_a','prj_a','ref_a','release','deleted')`, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, set := range []string{"reference_id='ref_b'", "user_id='usr_other'", "scope_json='{\"changed\":true}'", "run_id='other'"} {
		if _, e = s.db.Exec(`UPDATE product_source_reference_outbox SET ` + set + ` WHERE id='o'`); e == nil {
			t.Fatal(set)
		}
	}
	if _, e = s.db.Exec(`DELETE FROM product_source_runs WHERE id=?`, r.ID); e == nil {
		t.Fatal("lost outbox FK parent")
	}
}

// Captures every existing user table's rowid + values and every schema object.
func sourceLegacySnapshot(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	tables := []string{}
	rows, e := db.Query(`SELECT type,name,coalesce(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND name NOT LIKE 'product_source_%' AND name!='schema_migrations' ORDER BY name`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var kind, name, ddl string
		if e = rows.Scan(&kind, &name, &ddl); e != nil {
			t.Fatal(e)
		}
		out["object:"+name] = ddl
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	for _, name := range tables {
		r, e := db.Query(`SELECT rowid,* FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" ORDER BY rowid`)
		if e != nil {
			t.Fatal(e)
		}
		cols, _ := r.Columns()
		data := [][]any{}
		for r.Next() {
			v := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range v {
				ptr[i] = &v[i]
			}
			if e = r.Scan(ptr...); e != nil {
				t.Fatal(e)
			}
			data = append(data, v)
		}
		if e = r.Err(); e != nil {
			t.Fatal(e)
		}
		r.Close()
		raw, e := json.Marshal(data)
		if e != nil {
			t.Fatal(e)
		}
		out["rows:"+name] = string(raw)
	}
	return out
}
