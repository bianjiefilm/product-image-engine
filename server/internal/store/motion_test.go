package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/motionref"
)

func TestMotionDeclarationKeepsSubjectWhenParametersChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "motion.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := s.CreateProject(context.Background(), Project{TenantID: "acct_a", Name: "引用", CreatedBy: "usr_a", SourceType: "standalone"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := motionref.Declare(motionref.AdoptedAsset{
		AssetID: "revision-version/V1@0123456789ab", ContentHash: strings.Repeat("cd", 32),
		RevisionID: "V1", Adopted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, created, err := s.PutMotionDeclaration(context.Background(), proj.TenantID, proj.ID, d)
	if err != nil || !created || saved.ID == "" {
		t.Fatalf("put created=%v err=%v id=%s", created, err, saved.ID)
	}
	again, created, err := s.PutMotionDeclaration(context.Background(), proj.TenantID, proj.ID, d)
	if err != nil || created || again.ID != saved.ID {
		t.Fatalf("idempotent created=%v id=%s err=%v", created, again.ID, err)
	}
	if _, err := s.RecordMotionParameter(context.Background(), proj.TenantID, proj.ID, saved.ID, "price", "12"); err != nil {
		t.Fatal(err)
	}
	colored, err := s.RecordMotionParameter(context.Background(), proj.TenantID, proj.ID, saved.ID, "color", "红")
	if err != nil {
		t.Fatal(err)
	}
	if colored.ProductRegeneration || colored.ModelCallCount != 0 {
		t.Fatalf("calls changed %#v", colored)
	}
	if colored.SubjectHash != saved.SubjectHash || colored.Digest != saved.Digest || colored.ContentHash != saved.ContentHash {
		t.Fatalf("subject moved %#v", colored)
	}
	if colored.DynamicAd || colored.Verified || colored.VideoGenerated || !strings.Contains(colored.FinishedCopy, "还没生成成片") {
		t.Fatalf("stored claim %#v", colored)
	}
	s.Close()

	re, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	got, err := re.GetMotionDeclaration(context.Background(), proj.TenantID, proj.ID, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != saved.Digest || got.SubjectHash != saved.SubjectHash || len(got.Parameters) != 2 {
		t.Fatalf("reopen %#v", got)
	}
	if _, err := re.GetMotionDeclaration(context.Background(), "acct_b", proj.ID, saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant %v", err)
	}
}
