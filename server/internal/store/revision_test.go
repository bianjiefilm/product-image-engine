package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/revision"
)

func TestRevisionLedgerSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rev.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := s.CreateProject(context.Background(), Project{TenantID: "acct_a", Name: "锁定", CreatedBy: "usr_a", SourceType: "standalone"})
	if err != nil {
		t.Fatal(err)
	}
	led := revision.NewLedger()
	bg, err := revision.ParseNaturalLanguage("背景简单一点")
	if err != nil {
		t.Fatal(err)
	}
	outdoor, err := revision.ParseNaturalLanguage("换成户外")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := led.PutCandidate([]byte("png-v1"), bg, "subject_lock", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v1.Label); err != nil {
		t.Fatal(err)
	}
	v2, err := led.PutCandidate([]byte("png-v2"), outdoor, "subject_lock", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v2.Label); err != nil {
		t.Fatal(err)
	}
	if err := led.Rollback(v1.Label); err != nil {
		t.Fatal(err)
	}
	if _, err := led.RecordUnresolved(revision.StatusFailed, outdoor); err != nil {
		t.Fatal(err)
	}
	if _, err := led.ProposeBrief("b1", map[string]string{"place": "studio"}); err != nil {
		t.Fatal(err)
	}
	diff, err := led.ProposeBrief("b2", map[string]string{"place": "outdoor"})
	if err != nil {
		t.Fatal(err)
	}
	if !diff.AdoptedUnchanged || led.AdoptedID != "V1" {
		t.Fatalf("brief moved adopted before save: %+v", diff)
	}
	if err := s.SaveRevisionLedger(context.Background(), "acct_a", proj.ID, led.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	loaded, err := reopened.LoadRevisionLedger(context.Background(), "acct_a", proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := revision.Restore(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if again.AdoptedID != "V1" || again.AdoptedHash() != v1.ContentHash {
		t.Fatalf("reopen lost rollback: id=%s hash=%s", again.AdoptedID, again.AdoptedHash())
	}
	pair, err := again.SideBySide("V1", "V2")
	if err != nil || pair.Left.ContentHash != v1.ContentHash || pair.Right.ContentHash != v2.ContentHash {
		t.Fatalf("side-by-side after reopen: %+v %v", pair, err)
	}
	if err := again.Adopt("V3"); !errors.Is(err, revision.ErrNotExportable) {
		t.Fatalf("failed version adopted after reopen: %v", err)
	}
	if _, err := reopened.LoadRevisionLedger(context.Background(), "acct_b", proj.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant err=%v", err)
	}
}
