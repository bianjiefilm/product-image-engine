// Package matrixhandoff 把已选定的产品图做成版本化交接文档。
// 矩阵只是接口。夹具客户端不是在线矩阵服务。
// 未注入已发布的矩阵草稿契约时，交接不写入草稿，也不表示已发布。
// 本包不转码、不上传、不扣费，也不标记已发布，也不改产品图导出状态。
// 图片不是视频。needs_video 不是成功。保真 fail 不能写成真实商品无改动。
package matrixhandoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const SchemaVersion = "matrix-handoff/v1"

const (
	MediaImage = "image"
	MediaVideo = "video"
)

const (
	VerdictPass    = "pass"
	VerdictFail    = "fail"
	VerdictUnknown = "unknown"
)

const (
	PlacementIllustration = "illustration"
	PlacementCover        = "cover"
)

const (
	StatusDraft        = "draft"
	StatusNeedsVideo   = "needs_video"
	statusNotPublished = "not_published"
)

const (
	ServiceNotVerified      = "NOT_VERIFIED"
	BillingNotVerified      = "NOT_VERIFIED"
	ProductionNotAuthorized = "NOT_AUTHORIZED"
	HumanUnknown            = "UNKNOWN"
)

const (
	// NeedsVideoMessage 是仅视频夹具在没有已有视频时的结果。
	// needs_video must not be described as success.
	NeedsVideoMessage = "needs_video must not be described as success"
	// FailCopy 留在 fail 文档上。fidelity fail must not become 真实商品无改动。
	FailCopy                 = "fidelity fail must not become 真实商品无改动"
	UnknownCopy              = "quality verdict unknown must not become 真实商品无改动"
	PassCopy                 = "quality verdict pass; handoff is not a publish claim"
	ForbiddenUnchangedCopy   = "真实商品无改动"
	draftMessage             = "draft recorded; image was not published"
	DraftNotPersistedMessage = "draft was not persisted; image was not published; 草稿未落库，图片未发布"
)

var (
	ErrCallerRevoked         = errors.New("matrixhandoff: caller_revoked")
	ErrCrossTenant           = errors.New("matrixhandoff: cross_tenant")
	ErrInvalidDocument       = errors.New("matrixhandoff: invalid_document")
	ErrImageIsNotVideo       = errors.New("matrixhandoff: an image is not a video")
	ErrUnknownPlatform       = errors.New("matrixhandoff: unknown_platform")
	ErrUnsupportedPlacement  = errors.New("matrixhandoff: unsupported_placement")
	ErrCoverRequiresVideo    = errors.New("matrixhandoff: cover_requires_existing_video")
	ErrVideoOnlyRejectsImage = errors.New("matrixhandoff: video_only_rejects_image")
	ErrDraftNotPersisted     = errors.New("matrixhandoff: " + DraftNotPersistedMessage)
)

var contentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Caller 是发起交接的人。Revoked 为真时整次调用拒绝。
type Caller struct {
	TenantID string
	CallerID string
	Revoked  bool
}

// SelectedImage 是已经选定的一张图片，不是视频。
type SelectedImage struct {
	TenantID            string
	BrandID             string
	AssetID             string
	Version             string
	ContentHash         string
	QualityVerdict      string
	ProductBriefVersion string
	License             string
	ExactSource         string
	AIDisclosure        string
	MediaKind           string
}

// Document 是版本化交接文档。发布、扣费、上传和转码恒为否。
type Document struct {
	Schema              string `json:"schema"`
	DraftRef            string `json:"draft_ref"`
	DraftVersion        int    `json:"draft_version"`
	TenantID            string `json:"tenant_id"`
	BrandID             string `json:"brand_id"`
	AssetID             string `json:"asset_id"`
	AssetVersion        string `json:"asset_version"`
	ContentHash         string `json:"content_hash"`
	QualityVerdict      string `json:"quality_verdict"`
	ProductBriefVersion string `json:"product_brief_version"`
	License             string `json:"license"`
	ExactSource         string `json:"exact_source"`
	AIDisclosure        string `json:"ai_disclosure"`
	MediaKind           string `json:"media_kind"`
	Copy                string `json:"copy"`
	Published           bool   `json:"published"`
	Charged             bool   `json:"charged"`
	Transcoded          bool   `json:"transcoded"`
	Uploaded            bool   `json:"uploaded"`
	VideoCreated        bool   `json:"video_created"`
	Service             string `json:"service"`
	Billing             string `json:"billing"`
	Production          string `json:"production"`
	Human               string `json:"human"`
}

// Request 是一次交给夹具平台的尝试。
type Request struct {
	Caller               Caller
	Image                SelectedImage
	PlatformID           string
	Placement            string
	ExistingVideoAssetID string
}

// Result 是这次尝试的结果。Published 不会被置成已发布。
type Result struct {
	Document             Document
	Status               string
	Role                 string
	NeedsVideo           bool
	Published            bool
	Uploaded             bool
	VideoCreated         bool
	Charged              bool
	Transcoded           bool
	Message              string
	Service              string
	Billing              string
	Production           string
	Human                string
	LiveMatrix           bool
	Fixture              bool
	PlatformID           string
	ExistingVideoAssetID string
}

// Service 在进程内保存草稿版本。它不写产品图库，也不改导出状态。
type Service struct {
	Catalog *Catalog
	Client  Client
	mu      sync.Mutex
	drafts  map[string][]Document
}

// NewService 使用给定目录和矩阵接口。client 可以是夹具，不是在线服务。
func NewService(cat *Catalog, client Client) *Service {
	return &Service{
		Catalog: cat,
		Client:  client,
		drafts:  map[string][]Document{},
	}
}

// Handoff 生成或复用草稿，再按平台内容类型决定能否交给矩阵。
// 客户端返回错误时，原样交回该错误，且不把草稿写成已发布。
func (s *Service) Handoff(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		return Result{}, ErrInvalidDocument
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := validateCaller(req.Caller); err != nil {
		return Result{}, err
	}
	if err := validateImage(req.Image); err != nil {
		return Result{}, err
	}
	if req.Caller.TenantID != req.Image.TenantID {
		return Result{}, ErrCrossTenant
	}
	if req.ExistingVideoAssetID != "" {
		if !canonicalID(req.ExistingVideoAssetID) {
			return Result{}, ErrInvalidDocument
		}
		if req.ExistingVideoAssetID == req.Image.AssetID {
			return Result{}, ErrImageIsNotVideo
		}
	}
	if s == nil || s.Catalog == nil {
		return Result{}, ErrUnknownPlatform
	}
	platform, ok := s.Catalog.Platform(req.PlatformID)
	if !ok {
		return Result{}, ErrUnknownPlatform
	}
	role, needsVideo, err := decide(platform, req.Placement, req.ExistingVideoAssetID)
	if err != nil {
		return Result{}, err
	}
	doc, err := buildDocument(req.Image)
	if err != nil {
		return Result{}, err
	}
	if !publishedContractsInjected(s) {
		return s.refuseUnpublished(doc, platform, role, req.ExistingVideoAssetID, needsVideo)
	}
	doc = s.remember(doc)
	if needsVideo {
		return resultFrom(doc, platform, "", StatusNeedsVideo, NeedsVideoMessage, "", true), nil
	}
	if err := ctx.Err(); err != nil {
		return resultFrom(doc, platform, role, statusNotPublished, "context ended; published status was not set", req.ExistingVideoAssetID, false), err
	}
	if s.Client == nil {
		return resultFrom(doc, platform, role, statusNotPublished, "matrix client is missing; published status was not set", req.ExistingVideoAssetID, false), ErrInvalidDocument
	}
	_, err = s.Client.SubmitDraft(ctx, Envelope{
		Document:             doc,
		Role:                 role,
		ExistingVideoAssetID: req.ExistingVideoAssetID,
	})
	if err != nil {
		return resultFrom(doc, platform, role, statusNotPublished, "matrix client returned an error; published status was not set", req.ExistingVideoAssetID, false), err
	}
	return resultFrom(doc, platform, role, StatusDraft, draftMessage, req.ExistingVideoAssetID, false), nil
}

// Versions 返回该资产版本的草稿，从旧到新。不泄漏其它租户的文档。
func (s *Service) Versions(caller Caller, tenantID, brandID, assetID, assetVersion string) ([]Document, error) {
	if err := validateCaller(caller); err != nil {
		return nil, err
	}
	if caller.TenantID != tenantID {
		return nil, ErrCrossTenant
	}
	if !canonicalID(brandID) || !canonicalID(assetID) || !canonicalID(assetVersion) {
		return nil, ErrInvalidDocument
	}
	if s == nil {
		return nil, ErrInvalidDocument
	}
	key := identityKey(tenantID, brandID, assetID, assetVersion)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.drafts[key]
	out := make([]Document, len(list))
	copy(out, list)
	return out, nil
}

func resultFrom(doc Document, platform Platform, role, status, message, existingVideo string, needsVideo bool) Result {
	doc.Published = false
	doc.Charged = false
	doc.Uploaded = false
	doc.VideoCreated = false
	doc.Transcoded = false
	return Result{
		Document:             doc,
		Status:               status,
		Role:                 role,
		NeedsVideo:           needsVideo,
		Published:            false,
		Uploaded:             false,
		VideoCreated:         false,
		Charged:              false,
		Transcoded:           false,
		Message:              message,
		Service:              ServiceNotVerified,
		Billing:              BillingNotVerified,
		Production:           ProductionNotAuthorized,
		Human:                HumanUnknown,
		LiveMatrix:           false,
		Fixture:              platform.Fixture,
		PlatformID:           platform.ID,
		ExistingVideoAssetID: existingVideo,
	}
}

func decide(p Platform, placement, existingVideo string) (string, bool, error) {
	switch placement {
	case "", PlacementIllustration, PlacementCover:
	default:
		return "", false, ErrUnsupportedPlacement
	}
	if placement == PlacementCover || (placement == "" && p.Allows(ContentCover) && !p.Allows(ContentArticle)) {
		if !p.Allows(ContentCover) {
			return "", false, ErrUnsupportedPlacement
		}
		if existingVideo == "" {
			return "", false, ErrCoverRequiresVideo
		}
		return PlacementCover, false, nil
	}
	if placement == PlacementIllustration || (placement == "" && p.Allows(ContentArticle)) {
		if !p.Allows(ContentArticle) {
			return "", false, ErrUnsupportedPlacement
		}
		return PlacementIllustration, false, nil
	}
	if p.VideoOnly() && placement == "" {
		if existingVideo == "" {
			return "", true, nil
		}
		return "", false, ErrVideoOnlyRejectsImage
	}
	return "", false, ErrUnsupportedPlacement
}

func buildDocument(img SelectedImage) (Document, error) {
	copyText, err := honestCopy(img.QualityVerdict)
	if err != nil {
		return Document{}, err
	}
	if copyText == ForbiddenUnchangedCopy {
		return Document{}, ErrInvalidDocument
	}
	return Document{
		Schema:              SchemaVersion,
		TenantID:            img.TenantID,
		BrandID:             img.BrandID,
		AssetID:             img.AssetID,
		AssetVersion:        img.Version,
		ContentHash:         img.ContentHash,
		QualityVerdict:      img.QualityVerdict,
		ProductBriefVersion: img.ProductBriefVersion,
		License:             img.License,
		ExactSource:         img.ExactSource,
		AIDisclosure:        img.AIDisclosure,
		MediaKind:           MediaImage,
		Copy:                copyText,
		Published:           false,
		Charged:             false,
		Transcoded:          false,
		Uploaded:            false,
		VideoCreated:        false,
		Service:             ServiceNotVerified,
		Billing:             BillingNotVerified,
		Production:          ProductionNotAuthorized,
		Human:               HumanUnknown,
	}, nil
}

func honestCopy(verdict string) (string, error) {
	switch verdict {
	case VerdictPass:
		return PassCopy, nil
	case VerdictFail:
		return FailCopy, nil
	case VerdictUnknown:
		return UnknownCopy, nil
	default:
		return "", ErrInvalidDocument
	}
}

func validateCaller(c Caller) error {
	if c.Revoked {
		return ErrCallerRevoked
	}
	if !canonicalID(c.TenantID) || !canonicalID(c.CallerID) {
		return ErrInvalidDocument
	}
	return nil
}

func validateImage(img SelectedImage) error {
	if img.MediaKind == MediaVideo {
		return ErrImageIsNotVideo
	}
	if img.MediaKind != MediaImage {
		return ErrInvalidDocument
	}
	if !canonicalID(img.TenantID) || !canonicalID(img.BrandID) || !canonicalID(img.AssetID) || !canonicalID(img.Version) {
		return ErrInvalidDocument
	}
	if !canonicalID(img.ProductBriefVersion) {
		return ErrInvalidDocument
	}
	if !canonicalText(img.License, 512, false) || !canonicalText(img.ExactSource, 512, false) {
		return ErrInvalidDocument
	}
	if !canonicalText(img.AIDisclosure, 512, true) {
		return ErrInvalidDocument
	}
	if !contentHashPattern.MatchString(img.ContentHash) {
		return ErrInvalidDocument
	}
	switch img.QualityVerdict {
	case VerdictPass, VerdictFail, VerdictUnknown:
	default:
		return ErrInvalidDocument
	}
	return nil
}

func canonicalID(s string) bool {
	if s == "" || strings.TrimSpace(s) != s || utf8.RuneCountInString(s) > 256 {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func canonicalText(s string, max int, allowEmpty bool) bool {
	if s == "" {
		return allowEmpty
	}
	if strings.TrimSpace(s) != s {
		return false
	}
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= max
}

// publishedContractsInjected reports whether a real published AG10 draft
// contract and AG01 access contract were injected. Neither contract is
// published. A fixture client is not that contract, and this does not
// consult the process environment.
func publishedContractsInjected(*Service) bool { return false }

// refuseUnpublished returns without writing the draft map, a database, a
// file, or HTTP. needs_video stays an honest non-success and is not stored.
func (s *Service) refuseUnpublished(doc Document, platform Platform, role, existing string, needsVideo bool) (Result, error) {
	if needsVideo {
		return resultFrom(doc, platform, "", StatusNeedsVideo, NeedsVideoMessage, "", true), nil
	}
	return resultFrom(doc, platform, role, statusNotPublished, DraftNotPersistedMessage, existing, false), ErrDraftNotPersisted
}

func (s *Service) remember(doc Document) Document {
	doc.Published = false
	doc.Charged = false
	doc.Uploaded = false
	doc.VideoCreated = false
	doc.Transcoded = false
	doc.Service = ServiceNotVerified
	doc.Billing = BillingNotVerified
	doc.Production = ProductionNotAuthorized
	doc.Human = HumanUnknown
	doc.DraftRef = stableRef(doc.TenantID, doc.BrandID, doc.AssetID, doc.AssetVersion)
	key := identityKey(doc.TenantID, doc.BrandID, doc.AssetID, doc.AssetVersion)
	fp := fingerprint(doc)
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.drafts[key]
	if n := len(list); n > 0 {
		if fingerprint(list[n-1]) == fp {
			return list[n-1]
		}
		doc.DraftVersion = list[n-1].DraftVersion + 1
		s.drafts[key] = append(list, doc)
		return doc
	}
	doc.DraftVersion = 1
	s.drafts[key] = []Document{doc}
	return doc
}

func identityKey(tenant, brand, asset, version string) string {
	return tenant + "\x00" + brand + "\x00" + asset + "\x00" + version
}

func stableRef(tenant, brand, asset, version string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenant, brand, asset, version}, "\n")))
	return "mhd_" + hex.EncodeToString(sum[:16])
}

func fingerprint(d Document) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		d.TenantID,
		d.BrandID,
		d.AssetID,
		d.AssetVersion,
		d.ContentHash,
		d.QualityVerdict,
		d.ProductBriefVersion,
		d.License,
		d.ExactSource,
		d.AIDisclosure,
		d.MediaKind,
		d.Copy,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}
