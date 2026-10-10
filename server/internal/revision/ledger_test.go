package revision

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestLedgerKeepsAdoptedVersionWhenOutcomeFails(t *testing.T) {
	led := NewLedger()
	intent, err := ParseNaturalLanguage("背景简单一点")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("png-v1")
	v1, err := led.PutCandidate(png, intent, "subject_lock", "brief-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v1.Label); err != nil {
		t.Fatal(err)
	}
	before := led.AdoptedHash()
	if _, err := led.RecordUnresolved(StatusFailed, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := led.RecordUnresolved(StatusUnknown, intent); err != nil {
		t.Fatal(err)
	}
	if led.AdoptedID != v1.Label || led.AdoptedHash() != before {
		t.Fatalf("adopted moved: id=%s hash=%s want %s %s", led.AdoptedID, led.AdoptedHash(), v1.Label, before)
	}
	if err := led.Adopt("V2"); !errors.Is(err, ErrNotExportable) && err == nil {
		t.Fatalf("failed version must not become adopted, err=%v", err)
	}
}

func TestBriefDiffDoesNotReplaceAdoptedResult(t *testing.T) {
	led := NewLedger()
	intent, _ := ParseNaturalLanguage("换成户外")
	v1, err := led.PutCandidate([]byte("outdoor-v1"), intent, "subject_lock", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v1.Label); err != nil {
		t.Fatal(err)
	}
	if _, err := led.ProposeBrief("b1", map[string]string{"color": "blue", "place": "studio"}); err != nil {
		t.Fatal(err)
	}
	hash := led.AdoptedHash()
	diff, err := led.ProposeBrief("b2", map[string]string{"color": "green", "season": "summer"})
	if err != nil {
		t.Fatal(err)
	}
	if !diff.AdoptedUnchanged || diff.AdoptedVersionID != v1.Label || diff.AdoptedContentHash != hash {
		t.Fatalf("diff overwrote adopted: %+v", diff)
	}
	if led.AdoptedHash() != hash || led.AdoptedID != v1.Label {
		t.Fatal("adopted bytes changed")
	}
	if !sameStrings(diff.Changed, []string{"color"}) || !sameStrings(diff.Added, []string{"season"}) || !sameStrings(diff.Removed, []string{"place"}) {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestSideBySideRollbackAndExplicitDownstream(t *testing.T) {
	led := NewLedger()
	bg, _ := ParseNaturalLanguage("背景简单一点")
	outdoor, _ := ParseNaturalLanguage("换成户外")
	v1, err := led.PutCandidate([]byte("v1-bytes"), bg, "subject_lock", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v1.Label); err != nil {
		t.Fatal(err)
	}
	v2, err := led.PutCandidate([]byte("v2-bytes"), outdoor, "subject_lock", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v2.Label); err != nil {
		t.Fatal(err)
	}
	pair, err := led.SideBySide(v1.Label, v2.Label)
	if err != nil || pair.Left.ContentHash != v1.ContentHash || pair.Right.ContentHash != v2.ContentHash {
		t.Fatalf("pair = %+v %v", pair, err)
	}
	if led.AdoptedID != v2.Label {
		t.Fatal("compare must not change the adopted pointer")
	}
	if err := led.Rollback(v1.Label); err != nil {
		t.Fatal(err)
	}
	if led.AdoptedID != v1.Label || led.AdoptedHash() != v1.ContentHash {
		t.Fatalf("rollback adopted %s %s", led.AdoptedID, led.AdoptedHash())
	}
	again, err := led.SideBySide("V1", "V2")
	if err != nil || again.Left.ContentHash != pair.Left.ContentHash {
		t.Fatal("side-by-side unstable after rollback")
	}
	exported, err := led.Export("V2")
	if err != nil || exported.VersionID != "V2" || exported.RequiresReupload || exported.AssetRef == "" {
		t.Fatalf("export = %+v %v", exported, err)
	}
	exportedAgain, err := led.Export("V2")
	raw, mErr := json.Marshal(exported)
	if err != nil || exportedAgain != exported || mErr != nil || bytes.Contains(raw, []byte("draft")) || bytes.Contains(raw, []byte("matrix")) {
		t.Fatalf("export is a version ref, not a draft: %s %+v %v", raw, exportedAgain, err)
	}
	if _, err := led.Export(""); !errors.Is(err, ErrExplicitVersion) {
		t.Fatalf("missing version err=%v", err)
	}
	down, err := led.Downstream("V2", DownstreamMatrix)
	if err != nil || down.RequiresReupload || down.VersionID != "V2" || down.ContentHash != v2.ContentHash {
		t.Fatalf("downstream = %+v %v", down, err)
	}
	if _, err := led.Downstream("V2", "publish-live"); err == nil {
		t.Fatal("unknown downstream accepted")
	}
	snap := led.Snapshot()
	restored, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AdoptedID != "V1" || restored.AdoptedHash() != v1.ContentHash {
		t.Fatalf("restore lost the rollback: %+v", restored.Snapshot())
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
