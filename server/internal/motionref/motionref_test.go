package motionref

import (
	"errors"
	"strings"
	"testing"
)

func adopted() AdoptedAsset {
	return AdoptedAsset{
		AssetID:     "revision-version/V2@0123456789ab",
		ContentHash: strings.Repeat("ab", 32),
		RevisionID:  "V2",
		Adopted:     true,
	}
}

func TestDeclareRequiresAdoptedAsset(t *testing.T) {
	if _, err := Declare(AdoptedAsset{Adopted: false}); !errors.Is(err, ErrNotAdopted) {
		t.Fatalf("unadopted err = %v", err)
	}
	d, err := Declare(adopted())
	if err != nil {
		t.Fatal(err)
	}
	if d.AssetID != adopted().AssetID || d.ContentHash != adopted().ContentHash || d.RevisionID != "V2" {
		t.Fatalf("identity = %#v", d)
	}
	if d.Digest != Digest(d.AssetID, d.ContentHash, d.RevisionID) || len(d.Digest) != len("sha256:")+64 {
		t.Fatalf("digest = %s", d.Digest)
	}
	if !strings.HasPrefix(d.Digest, "sha256:") {
		t.Fatalf("digest prefix = %s", d.Digest)
	}
	if d.SubjectHash != d.ContentHash || d.ProductRegeneration || d.ModelCallCount != 0 {
		t.Fatalf("generation flags = %#v", d)
	}
	if d.VideoGenerated || d.SupplierCalled || d.Verified || d.DynamicAd || d.MotionEditorCopied || d.BatchRerun {
		t.Fatalf("claim flags = %#v", d)
	}
	if !strings.Contains(d.FinishedCopy, "还没生成成片") {
		t.Fatalf("copy = %s", d.FinishedCopy)
	}
	if err := AsDynamicAd(d); !errors.Is(err, ErrUnverifiedAd) {
		t.Fatalf("dynamic ad err = %v", err)
	}
}

func TestPriceAndColorOnlyRecordParameters(t *testing.T) {
	d, err := Declare(adopted())
	if err != nil {
		t.Fatal(err)
	}
	priced, err := RecordParameter(d, "price", "19.9")
	if err != nil {
		t.Fatal(err)
	}
	both, err := RecordParameter(priced, "color", "暖白")
	if err != nil {
		t.Fatal(err)
	}
	if both.ProductRegeneration || both.ModelCallCount != 0 {
		t.Fatalf("regeneration = %v calls = %d", both.ProductRegeneration, both.ModelCallCount)
	}
	if both.SubjectHash != d.SubjectHash || both.ContentHash != d.ContentHash || both.Digest != d.Digest {
		t.Fatalf("subject moved: before %s %s after %s %s", d.SubjectHash, d.Digest, both.SubjectHash, both.Digest)
	}
	if both.VideoGenerated || both.SupplierCalled || both.DynamicAd || both.Verified {
		t.Fatalf("parameter wrote a claim: %#v", both)
	}
	if len(both.Parameters) != 2 || both.Parameters[0].Name != "color" || both.Parameters[1].Value != "19.9" {
		t.Fatalf("parameters = %#v", both.Parameters)
	}
	if _, err := RecordParameter(both, "prompt", "重画商品"); !errors.Is(err, ErrBadParameter) {
		t.Fatalf("prompt err = %v", err)
	}
	if err := AsDynamicAd(both); !errors.Is(err, ErrUnverifiedAd) {
		t.Fatalf("still not an ad: %v", err)
	}
}

func TestBatchRerunStaysRefusedWhenCountsLookZero(t *testing.T) {
	zero := 0
	cases := [][]VariantExecution{
		nil,
		{{ID: "v2", ExecutionCount: nil}},
		{{ID: "v2", ExecutionCount: &zero}, {ID: "v3", ExecutionCount: &zero}},
	}
	for _, others := range cases {
		if err := BatchRerun(others); !errors.Is(err, ErrBatchUnproven) {
			t.Fatalf("others %#v err = %v", others, err)
		}
	}
}
