package matrixhandoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func testImage() SelectedImage {
	return SelectedImage{
		TenantID:            "tenant-a",
		BrandID:             "brand-1",
		AssetID:             "asset-img-1",
		Version:             "v3",
		ContentHash:         hashOf("pixels-v3"),
		QualityVerdict:      VerdictPass,
		ProductBriefVersion: "brief-9",
		License:             "license-fixture-nd",
		ExactSource:         "project:p1/output:o3/version:v3",
		AIDisclosure:        "本图含 AI 生成内容",
		MediaKind:           MediaImage,
	}
}

func testCaller() Caller {
	return Caller{TenantID: "tenant-a", CallerID: "caller-1"}
}

func TestHandoffDocumentCarriesSelectedImage(t *testing.T) {
	img := testImage()
	svc := NewService(NewFixtureCatalog(), &FixtureClient{})
	res, err := svc.Handoff(context.Background(), Request{
		Caller:     testCaller(),
		Image:      img,
		PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") {
		t.Fatalf("success copy: %v %q", err, res.Message)
	}
	stored, verr := svc.Versions(testCaller(), img.TenantID, img.BrandID, img.AssetID, img.Version)
	if verr != nil {
		t.Fatal(verr)
	}
	if len(stored) != 0 {
		t.Fatalf("unpublished handoff stored %d drafts", len(stored))
	}
	doc := res.Document
	if doc.Schema != SchemaVersion {
		t.Fatalf("schema %q", doc.Schema)
	}
	if doc.AssetID != img.AssetID || doc.AssetVersion != img.Version {
		t.Fatalf("asset identity %+v", doc)
	}
	if doc.ContentHash != img.ContentHash || doc.QualityVerdict != img.QualityVerdict {
		t.Fatalf("hash or verdict changed: %+v", doc)
	}
	if doc.ProductBriefVersion != img.ProductBriefVersion || doc.License != img.License {
		t.Fatalf("brief or license missing: %+v", doc)
	}
	if doc.ExactSource != img.ExactSource || doc.AIDisclosure != img.AIDisclosure {
		t.Fatalf("source or disclosure missing: %+v", doc)
	}
	if doc.MediaKind != MediaImage || doc.Transcoded || doc.VideoCreated {
		t.Fatalf("image was treated as video: %+v", doc)
	}
	if doc.Published || doc.Charged || doc.Uploaded || res.Published || res.Charged || res.Uploaded {
		t.Fatalf("draft was published or charged: %+v", res)
	}
	if doc.Service != ServiceNotVerified || doc.Billing != BillingNotVerified ||
		doc.Production != ProductionNotAuthorized || doc.Human != HumanUnknown {
		t.Fatalf("honesty labels %+v", doc)
	}
	if strings.Contains(res.Message, "发布成功") || strings.Contains(doc.Copy, "发布成功") {
		t.Fatalf("must not say 发布成功: %+v", res)
	}
}

func TestImageIsNotAVideoAndIsNotTranscoded(t *testing.T) {
	svc := NewService(NewFixtureCatalog(), &FixtureClient{})
	img := testImage()
	img.MediaKind = MediaVideo
	_, err := svc.Handoff(context.Background(), Request{
		Caller:     testCaller(),
		Image:      img,
		PlatformID: FixtureVideoOnly,
	})
	if !errors.Is(err, ErrImageIsNotVideo) {
		t.Fatalf("video media: %v", err)
	}
	vers, err := svc.Versions(testCaller(), img.TenantID, img.BrandID, img.AssetID, img.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("rejected video stored a draft: %+v", vers)
	}

	img = testImage()
	fc := &FixtureClient{}
	svc = NewService(NewFixtureCatalog(), fc)
	_, err = svc.Handoff(context.Background(), Request{
		Caller:               testCaller(),
		Image:                img,
		PlatformID:           FixtureCover,
		ExistingVideoAssetID: img.AssetID,
	})
	if !errors.Is(err, ErrImageIsNotVideo) {
		t.Fatalf("image aliased as video: %v", err)
	}
	if len(fc.Submits) != 0 {
		t.Fatal("aliased image was submitted")
	}
}

func TestVideoOnlyImageNeedsVideoWithoutUploadOrPublish(t *testing.T) {
	fc := &FixtureClient{}
	svc := NewService(NewFixtureCatalog(), fc)
	img := testImage()
	img.QualityVerdict = VerdictFail
	res, err := svc.Handoff(context.Background(), Request{
		Caller:     testCaller(),
		Image:      img,
		PlatformID: FixtureVideoOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusNeedsVideo || !res.NeedsVideo {
		t.Fatalf("status %+v", res)
	}
	if res.Message != NeedsVideoMessage {
		t.Fatalf("message %q", res.Message)
	}
	if !strings.Contains(res.Message, "needs_video must not be described as success") {
		t.Fatalf("needs_video described as success: %q", res.Message)
	}
	if res.Published || res.Uploaded || res.VideoCreated || res.Charged || res.Transcoded {
		t.Fatalf("needs_video had a side effect: %+v", res)
	}
	if res.Role != "" || res.Document.MediaKind != MediaImage {
		t.Fatalf("image became something else: %+v", res)
	}
	if res.Document.QualityVerdict != VerdictFail || res.Document.Copy != FailCopy {
		t.Fatalf("verdict upgraded: %+v", res.Document)
	}
	if res.Document.Copy == ForbiddenUnchangedCopy {
		t.Fatal("fidelity fail became 真实商品无改动")
	}
	if len(fc.Submits) != 0 {
		t.Fatal("needs_video uploaded or submitted")
	}
	if strings.EqualFold(res.Status, "success") || strings.Contains(res.Message, "发布成功") {
		t.Fatalf("described as success: %+v", res)
	}
}

func TestArticleFixtureAcceptsIllustration(t *testing.T) {
	fc := &FixtureClient{}
	svc := NewService(NewFixtureCatalog(), fc)
	img := testImage()
	res, err := svc.Handoff(context.Background(), Request{
		Caller:     testCaller(),
		Image:      img,
		PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") || res.Status == StatusDraft {
		t.Fatalf("illustration was recorded: %+v err=%v", res, err)
	}
	if res.Role != PlacementIllustration || res.NeedsVideo {
		t.Fatalf("illustration %+v", res)
	}
	if res.Published || res.Uploaded || res.VideoCreated || res.Charged || res.Document.Published {
		t.Fatalf("illustration published: %+v", res)
	}
	if len(fc.Submits) != 0 {
		t.Fatalf("unpublished illustration submitted: %+v", fc.Submits)
	}
	if res.Document.AIDisclosure != img.AIDisclosure {
		t.Fatal("disclosure dropped")
	}
	if res.Document.MediaKind != MediaImage {
		t.Fatal("image was transcoded")
	}
	vers, verr := svc.Versions(testCaller(), img.TenantID, img.BrandID, img.AssetID, img.Version)
	if verr != nil {
		t.Fatal(verr)
	}
	if len(vers) != 0 {
		t.Fatalf("illustration stored a draft: %+v", vers)
	}
}

func TestCoverRequiresExistingVideoAsset(t *testing.T) {
	fc := &FixtureClient{}
	svc := NewService(NewFixtureCatalog(), fc)
	_, err := svc.Handoff(context.Background(), Request{
		Caller:     testCaller(),
		Image:      testImage(),
		PlatformID: FixtureCover,
	})
	if !errors.Is(err, ErrCoverRequiresVideo) {
		t.Fatalf("missing video: %v", err)
	}
	if len(fc.Submits) != 0 {
		t.Fatal("cover without video was submitted")
	}
	vers, err := svc.Versions(testCaller(), "tenant-a", "brand-1", "asset-img-1", "v3")
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("rejected cover stored a draft: %+v", vers)
	}

	res, err := svc.Handoff(context.Background(), Request{
		Caller:               testCaller(),
		Image:                testImage(),
		PlatformID:           FixtureCover,
		Placement:            PlacementCover,
		ExistingVideoAssetID: "video-asset-9",
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatalf("cover persist: %v", err)
	}
	if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") {
		t.Fatalf("cover success copy: %+v err=%v", res, err)
	}
	if res.Role != PlacementCover || res.NeedsVideo {
		t.Fatalf("cover %+v", res)
	}
	if res.Published || res.Uploaded || res.VideoCreated || res.Transcoded || res.Document.Published {
		t.Fatalf("cover created a video or published: %+v", res)
	}
	if res.Document.MediaKind != MediaImage || res.ExistingVideoAssetID != "video-asset-9" {
		t.Fatalf("cover payload %+v", res)
	}
	if len(fc.Submits) != 0 {
		t.Fatalf("submit %+v", fc.Submits)
	}
	vers, err = svc.Versions(testCaller(), "tenant-a", "brand-1", "asset-img-1", "v3")
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("cover stored a draft: %+v", vers)
	}
}

func TestUnpublishedHandoffDoesNotAppendDraftVersions(t *testing.T) {
	svc := NewService(NewFixtureCatalog(), &FixtureClient{})
	other := NewService(NewFixtureCatalog(), &FixtureClient{})
	req := Request{Caller: testCaller(), Image: testImage(), PlatformID: FixtureArticle}
	first, err := svc.Handoff(context.Background(), req)
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	second, err := svc.Handoff(context.Background(), req)
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	again, err := other.Handoff(context.Background(), req)
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	if strings.Contains(first.Message, "draft recorded") || strings.Contains(second.Message, "draft recorded") || strings.Contains(again.Message, "draft recorded") {
		t.Fatal("replay used the success copy")
	}
	if first.Document.DraftRef != "" || first.Document.DraftVersion != 0 || second.Document.DraftRef != "" || again.Document.DraftRef != "" {
		t.Fatalf("unpublished handoff recorded a ref: first=%+v second=%+v again=%+v", first.Document, second.Document, again.Document)
	}
	if first.Published || second.Published || again.Published {
		t.Fatal("unpublished handoff published")
	}

	changed := testImage()
	changed.ContentHash = hashOf("pixels-v3-b")
	if _, err = svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: changed, PlatformID: FixtureArticle,
	}); !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}

	failed := changed
	failed.QualityVerdict = VerdictFail
	verdict, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: failed, PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	if verdict.Document.QualityVerdict != VerdictFail || verdict.Document.Copy != FailCopy || verdict.Document.Copy == ForbiddenUnchangedCopy {
		t.Fatalf("new verdict overwrote honesty: %+v", verdict.Document)
	}
	if verdict.Document.Published || verdict.Published || verdict.Charged || verdict.Uploaded {
		t.Fatalf("verdict published: %+v", verdict)
	}

	vers, err := svc.Versions(testCaller(), "tenant-a", "brand-1", "asset-img-1", "v3")
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("versions stored %+v", vers)
	}

	otherVersion := testImage()
	otherVersion.Version = "v4"
	moved, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: otherVersion, PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	movedVers, err := svc.Versions(testCaller(), otherVersion.TenantID, otherVersion.BrandID, otherVersion.AssetID, otherVersion.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(movedVers) != 0 || moved.Document.DraftRef != "" || moved.Document.Published {
		t.Fatalf("other version stored %+v %+v", moved.Document, movedVers)
	}
}

func TestCrossTenantAndRevokedCallerRejected(t *testing.T) {
	fc := &FixtureClient{}
	svc := NewService(NewFixtureCatalog(), fc)
	img := testImage()
	img.TenantID = "tenant-b"
	_, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: img, PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross tenant: %v", err)
	}
	revoked := testCaller()
	revoked.Revoked = true
	_, err = svc.Handoff(context.Background(), Request{
		Caller: revoked, Image: testImage(), PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrCallerRevoked) {
		t.Fatalf("revoked: %v", err)
	}
	if len(fc.Submits) != 0 {
		t.Fatal("rejected caller submitted")
	}
	vers, err := svc.Versions(testCaller(), "tenant-a", "brand-1", "asset-img-1", "v3")
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("rejected handoff stored a draft: %+v", vers)
	}
	_, err = svc.Versions(revoked, "tenant-a", "brand-1", "asset-img-1", "v3")
	if !errors.Is(err, ErrCallerRevoked) {
		t.Fatalf("revoked read: %v", err)
	}
}

func TestFailAndUnknownStayHonestThroughFixture(t *testing.T) {
	cases := []struct {
		verdict string
		copy    string
	}{
		{VerdictPass, PassCopy},
		{VerdictFail, FailCopy},
		{VerdictUnknown, UnknownCopy},
	}
	for _, tc := range cases {
		t.Run(tc.verdict, func(t *testing.T) {
			fc := &FixtureClient{}
			svc := NewService(NewFixtureCatalog(), fc)
			img := testImage()
			img.QualityVerdict = tc.verdict
			res, err := svc.Handoff(context.Background(), Request{
				Caller: testCaller(), Image: img, PlatformID: FixtureArticle,
			})
			if !errors.Is(err, ErrDraftNotPersisted) {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") {
				t.Fatalf("success copy: %v %q", err, res.Message)
			}
			if res.Document.QualityVerdict != tc.verdict || res.Document.Copy != tc.copy {
				t.Fatalf("copy upgraded: %+v", res.Document)
			}
			if res.Document.Copy == ForbiddenUnchangedCopy || res.Published || res.Document.Published {
				t.Fatal("became 真实商品无改动 or published")
			}
			if res.Document.AIDisclosure != img.AIDisclosure {
				t.Fatal("disclosure dropped")
			}
			if len(fc.Submits) != 0 {
				t.Fatalf("unpublished fixture was called: %+v", fc.Submits)
			}
		})
	}
	if FailCopy != "fidelity fail must not become 真实商品无改动" {
		t.Fatalf("fail copy %q", FailCopy)
	}
	if FailCopy == ForbiddenUnchangedCopy || UnknownCopy == ForbiddenUnchangedCopy {
		t.Fatal("honesty copy collapsed to the forbidden claim")
	}

	fc := &FixtureClient{}
	_, err := fc.SubmitDraft(context.Background(), Envelope{Document: Document{
		QualityVerdict: VerdictFail,
		Copy:           ForbiddenUnchangedCopy,
	}})
	if err == nil || !strings.Contains(err.Error(), "fidelity fail must not become 真实商品无改动") {
		t.Fatalf("fixture accepted the upgrade: %v", err)
	}
	if len(fc.Submits) != 0 {
		t.Fatal("forbidden copy was recorded")
	}
}

func TestClientErrorDoesNotPublish(t *testing.T) {
	sentinel := errors.New("matrix fixture unavailable")
	fc := &FixtureClient{Err: sentinel}
	svc := NewService(NewFixtureCatalog(), fc)
	res, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: testImage(), PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) || errors.Is(err, sentinel) {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") {
		t.Fatalf("success copy: %v %q", err, res.Message)
	}
	if res.Published || res.Document.Published || res.Uploaded || res.Charged {
		t.Fatalf("error set published: %+v", res)
	}
	if res.Status == "published" || strings.Contains(res.Message, "发布成功") {
		t.Fatalf("error described as publish success: %+v", res)
	}
	if len(fc.Submits) != 0 {
		t.Fatal("unpublished handoff called the client")
	}
	vers, err := svc.Versions(testCaller(), "tenant-a", "brand-1", "asset-img-1", "v3")
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("stored %+v", vers)
	}
}

type lyingClient struct{}

func (lyingClient) SubmitDraft(context.Context, Envelope) (Ack, error) {
	return Ack{
		Accepted:       true,
		Published:      true,
		Uploaded:       true,
		VideoCreated:   true,
		Role:           PlacementIllustration,
		Copy:           ForbiddenUnchangedCopy,
		QualityVerdict: VerdictPass,
		Service:        "PASS",
		LiveMatrix:     true,
	}, nil
}

func TestLyingAckCannotUpgradeVerdictCopyOrPublish(t *testing.T) {
	svc := NewService(NewFixtureCatalog(), lyingClient{})
	img := testImage()
	img.QualityVerdict = VerdictFail
	res, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: img, PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") {
		t.Fatalf("success copy: %v %q", err, res.Message)
	}
	if res.Published || res.Uploaded || res.VideoCreated || res.Document.Published {
		t.Fatalf("lie published: %+v", res)
	}
	if res.Document.QualityVerdict != VerdictFail || res.Document.Copy != FailCopy {
		t.Fatalf("lie upgraded the document: %+v", res.Document)
	}
	if res.Document.Copy == ForbiddenUnchangedCopy || res.Service == "PASS" || res.LiveMatrix {
		t.Fatalf("lie adopted: %+v", res)
	}
	vers, err := svc.Versions(testCaller(), img.TenantID, img.BrandID, img.AssetID, img.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 0 {
		t.Fatalf("stored lie %+v", vers)
	}
}

func TestFixtureIDIsNotServicePass(t *testing.T) {
	cat := NewFixtureCatalog()
	video, ok := cat.Platform(FixtureVideoOnly)
	if !ok || !video.Fixture || !video.Allows(ContentVideo) || video.Allows(ContentArticle) || video.Allows(ContentCover) {
		t.Fatalf("video fixture %+v", video)
	}
	article, ok := cat.Platform(FixtureArticle)
	if !ok || !article.Fixture || !article.Allows(ContentArticle) {
		t.Fatalf("article fixture %+v", article)
	}
	cover, ok := cat.Platform(FixtureCover)
	if !ok || !cover.Fixture || !cover.Allows(ContentCover) {
		t.Fatalf("cover fixture %+v", cover)
	}
	if _, ok := cat.Platform("live.matrix"); ok {
		t.Fatal("catalog invented a live platform")
	}

	fc := &FixtureClient{}
	svc := NewService(cat, fc)
	res, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: testImage(), PlatformID: FixtureArticle,
	})
	if !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") || len(fc.Submits) != 0 {
		t.Fatalf("fixture recorded a draft: %+v submits=%d err=%v", res, len(fc.Submits), err)
	}
	if !res.Fixture || res.LiveMatrix || fc.Live() {
		t.Fatalf("fixture treated as live: %+v live=%v", res, fc.Live())
	}
	if res.Service != ServiceNotVerified || res.Billing != BillingNotVerified ||
		res.Production != ProductionNotAuthorized || res.Human != HumanUnknown {
		t.Fatalf("labels %+v", res)
	}
	if strings.Contains(res.Service, "PASS") || res.PlatformID != FixtureArticle {
		t.Fatalf("fixture id became service pass: %+v", res)
	}
}

func TestVersionsDoNotLeakAcrossTenants(t *testing.T) {
	svc := NewService(NewFixtureCatalog(), &FixtureClient{})
	if _, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: testImage(), PlatformID: FixtureArticle,
	}); !errors.Is(err, ErrDraftNotPersisted) {
		t.Fatal(err)
	}
	own, err := svc.Versions(testCaller(), "tenant-a", "brand-1", "asset-img-1", "v3")
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != 0 {
		t.Fatalf("stored before the leak check: %+v", own)
	}
	other := testCaller()
	other.TenantID = "tenant-b"
	other.CallerID = "caller-b"
	_, err = svc.Versions(other, "tenant-a", "brand-1", "asset-img-1", "v3")
	if !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("leak: %v", err)
	}
}

func TestCancelledContextDoesNotSubmit(t *testing.T) {
	fc := &FixtureClient{}
	svc := NewService(NewFixtureCatalog(), fc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Handoff(ctx, Request{
		Caller: testCaller(), Image: testImage(), PlatformID: FixtureArticle,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if len(fc.Submits) != 0 {
		t.Fatal("cancelled call submitted")
	}
}

func TestUnpublishedHandoffLeavesStorageEmpty(t *testing.T) {
	cases := []struct {
		name    string
		request Request
	}{
		{
			name: "article",
			request: Request{
				Caller: testCaller(), Image: testImage(), PlatformID: FixtureArticle,
			},
		},
		{
			name: "cover_with_existing_video",
			request: Request{
				Caller: testCaller(), Image: testImage(), PlatformID: FixtureCover,
				Placement: PlacementCover, ExistingVideoAssetID: "video-asset-9",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &FixtureClient{}
			svc := NewService(NewFixtureCatalog(), fc)
			res, err := svc.Handoff(context.Background(), tc.request)
			again, againErr := svc.Handoff(context.Background(), tc.request)
			img := tc.request.Image
			vers, verr := svc.Versions(testCaller(), img.TenantID, img.BrandID, img.AssetID, img.Version)
			if verr != nil {
				t.Fatal(verr)
			}
			if len(vers) != 0 {
				t.Fatalf("storage length %d, want 0", len(vers))
			}
			if err == nil || againErr == nil {
				t.Fatalf("unpublished handoff succeeded: first=%v message=%q again=%v message=%q", err, res.Message, againErr, again.Message)
			}
			if strings.Contains(err.Error(), "draft recorded") || strings.Contains(res.Message, "draft recorded") ||
				strings.Contains(againErr.Error(), "draft recorded") || strings.Contains(again.Message, "draft recorded") {
				t.Fatalf("success copy draft recorded: err=%v message=%q", err, res.Message)
			}
			if !strings.Contains(err.Error(), "草稿未落库") || !strings.Contains(err.Error(), "图片未发布") {
				t.Fatalf("error does not say the draft was not persisted and the image was not published: %v", err)
			}
			if res.Published || res.Uploaded || res.Charged || res.VideoCreated || res.Transcoded || res.Document.Published || res.Document.Uploaded {
				t.Fatalf("refusal published or uploaded: %+v", res)
			}
			if len(fc.Submits) != 0 {
				t.Fatalf("unpublished handoff submitted: %+v", fc.Submits)
			}
		})
	}

	fc := &FixtureClient{}
	svc := NewService(NewFixtureCatalog(), fc)
	img := testImage()
	img.QualityVerdict = VerdictFail
	res, err := svc.Handoff(context.Background(), Request{
		Caller: testCaller(), Image: img, PlatformID: FixtureVideoOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	vers, verr := svc.Versions(testCaller(), img.TenantID, img.BrandID, img.AssetID, img.Version)
	if verr != nil {
		t.Fatal(verr)
	}
	if len(vers) != 0 {
		t.Fatalf("needs_video storage length %d, want 0", len(vers))
	}
	if strings.Contains(res.Message, "draft recorded") || res.Message != NeedsVideoMessage {
		t.Fatalf("needs_video message %q", res.Message)
	}
	if len(fc.Submits) != 0 || res.Published || res.Uploaded || res.VideoCreated || res.Charged || res.Transcoded {
		t.Fatalf("needs_video had a side effect: %+v submits=%d", res, len(fc.Submits))
	}
}

func TestRefusalPathDoesNotRemember(t *testing.T) {
	src := productionSource(t)
	handoff := functionBody(t, src, "func (s *Service) Handoff")
	refuseAt := strings.Index(handoff, "refuseUnpublished(")
	rememberAt := strings.Index(handoff, "s.remember(")
	if refuseAt < 0 || rememberAt < 0 || refuseAt > rememberAt {
		t.Fatalf("unpublished refusal must return before remember saves; refuse=%d remember=%d", refuseAt, rememberAt)
	}
	body := functionBody(t, src, "func (s *Service) refuseUnpublished")
	for _, banned := range []string{
		"s.remember(",
		"s.drafts",
		"SubmitDraft",
		"draft recorded",
		"/v1/matrix-drafts",
		"os.Getenv",
		"http.NewRequest",
		"sql.Open",
		"os.WriteFile",
		"os.Create",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("refusal path contains %q", banned)
		}
	}
	if strings.Contains(src, "/v1/matrix-drafts") || strings.Contains(src, "os.Getenv") {
		t.Fatal("package calls the unpublished draft API or reads the environment")
	}
}

func productionSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		src.Write(body)
		src.WriteByte('\n')
	}
	return src.String()
}

func functionBody(t *testing.T, src, signature string) string {
	t.Helper()
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("missing %s", signature)
	}
	rest := src[start:]
	next := strings.Index(rest[len(signature):], "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:len(signature)+next]
}

func TestPackageSourceDoesNotPublishOrTouchExport(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		src.Write(body)
		src.WriteByte('\n')
	}
	text := src.String()
	for _, banned := range []string{
		"发布成功",
		"internal/store",
		"internal/httpapi",
		"internal/export",
		"ffmpeg",
		"access_token",
		"api_key",
		"password",
		"Uploaded: true",
		"Published: true",
		"Charged: true",
		"VideoCreated: true",
		"Transcoded: true",
		`Service: "PASS"`,
		"/v1/matrix-drafts",
		"os.Getenv",
		"handlePhotoUpload",
		"handlePhotoContent",
		"handleSourceExport",
		"handleRevisionExport",
		"handlePlateExport",
		"internal/httpapi/photos",
		"internal/store/photos",
	} {
		if strings.Contains(text, banned) {
			t.Fatalf("package contains %q", banned)
		}
	}
	for _, need := range []string{
		"needs_video must not be described as success",
		"fidelity fail must not become 真实商品无改动",
		ServiceNotVerified,
		BillingNotVerified,
		ProductionNotAuthorized,
		HumanUnknown,
	} {
		if !strings.Contains(text, need) {
			t.Fatalf("package missing %q", need)
		}
	}
}
