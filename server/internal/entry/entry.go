package entry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// 首屏入口与恢复的纯判定。存储和 HTTP 只消费这里的结果。
// 本包不证明真实成片可售。

const (
	PersonalDefault   = "personal/default"
	ScopePersonal     = "personal"
	ScopeOrganization = "organization"

	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusUnknown   = "unknown"
	StatusFailed    = "failed"
	StatusCompleted = "completed"

	QuotePending = "待确认"
)

var (
	ErrCustomerAssetInPersonal = errors.New("entry: 客户资产不能落入个人空间")
	ErrSourceTenantRequired    = errors.New("entry: 来源缺少已验证租户")
	ErrTenantMismatch          = errors.New("entry: 来源租户与当前账号不匹配")
	ErrReturnNotAllowed        = errors.New("entry: 当前入口没有可返回的来源")
	ErrReturnInvalid           = errors.New("entry: 返回地址不可用")
)

type ResolveInput struct {
	AccountID        string
	SessionTenantID  string
	Memberships      []string
	SourceType       string
	SourceRef        string
	SourceTenantID   string
	EnterpriseChosen bool
	ChosenTenantID   string
}

type Workspace struct {
	Kind               string
	Scope              string
	StorageTenant      string
	ForcedOrgSelection bool
	RequiresOrder      bool
}

type AuthorizedAsset struct {
	Ref         string
	OwnerTenant string
}

type DecideInput struct {
	GenerationEnabled   bool
	BillingEnabled      bool
	Fingerprint         string
	ExistingTaskID      string
	ExistingFingerprint string
	ExistingStatus      string
	ExistingOutputID    string
	UpstreamReachable   *bool
	UpstreamStatus      string
	UpstreamAssetID     string
}

type Decision struct {
	Status         string
	TaskID         string
	SameTask       bool
	RegisterOutput bool
	OutputAssetID  string
	QuoteLabel     string
	Charged        bool
	DegradedReason string
	CallPlatform   bool
	Resubmit       bool
	UsefulProven   bool
	SLAPromised    bool
}

type PathFacts struct {
	HasAuthorizedAssets bool
	UploadedNew         bool
	FilledScene         bool
	FilledSize          bool
	FilledBackground    bool
	ConfirmedQuote      bool
	Submitted           bool
	Recovered           bool
}

type Journey struct {
	Steps                 int
	InputCount            int
	CrossProductReuploads int
	Recoveries            int
	SLAPromised           bool
}

type Screen struct {
	Title        string
	ShowsCatalog bool
	Steps        []string
}

func Fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func ResolveWorkspace(in ResolveInput) (Workspace, error) {
	if strings.TrimSpace(in.AccountID) == "" {
		return Workspace{}, ErrTenantMismatch
	}
	sourced := in.SourceType == "order" || in.SourceType == "campaign"
	if sourced {
		if strings.TrimSpace(in.SourceRef) == "" || strings.TrimSpace(in.SourceTenantID) == "" {
			return Workspace{}, ErrSourceTenantRequired
		}
		if !tenantAllowed(in, in.SourceTenantID) {
			return Workspace{}, ErrTenantMismatch
		}
		return Workspace{
			Kind:          ScopeOrganization,
			Scope:         in.SourceTenantID,
			StorageTenant: in.SourceTenantID,
		}, nil
	}
	if in.EnterpriseChosen {
		if strings.TrimSpace(in.ChosenTenantID) == "" || !tenantAllowed(in, in.ChosenTenantID) {
			return Workspace{}, ErrTenantMismatch
		}
		return Workspace{
			Kind:          ScopeOrganization,
			Scope:         in.ChosenTenantID,
			StorageTenant: in.ChosenTenantID,
		}, nil
	}
	return Workspace{
		Kind:          ScopePersonal,
		Scope:         PersonalDefault,
		StorageTenant: PersonalDefault + ":" + in.AccountID,
	}, nil
}

func tenantAllowed(in ResolveInput, tenant string) bool {
	if tenant == "" {
		return false
	}
	if tenant == in.AccountID || tenant == in.SessionTenantID {
		return true
	}
	for _, m := range in.Memberships {
		if m == tenant {
			return true
		}
	}
	return false
}

func PlaceAssets(ws Workspace, assets []AuthorizedAsset) error {
	for _, asset := range assets {
		owner := strings.TrimSpace(asset.OwnerTenant)
		if owner == "" {
			return ErrTenantMismatch
		}
		if ws.Kind == ScopePersonal && owner != ws.StorageTenant && owner != PersonalDefault {
			return ErrCustomerAssetInPersonal
		}
		if ws.Kind == ScopeOrganization && owner != ws.Scope {
			return ErrCustomerAssetInPersonal
		}
	}
	return nil
}

func Decide(in DecideInput) Decision {
	d := Decision{QuoteLabel: QuotePending}
	if in.BillingEnabled {
		// 接通后仍只在账本给出金额时改文案；本判定不扣费。
		d.Charged = false
	}
	same := in.ExistingTaskID != "" && in.ExistingFingerprint != "" && in.ExistingFingerprint == in.Fingerprint
	if same {
		d.SameTask = true
		d.TaskID = in.ExistingTaskID
		d.Status = in.ExistingStatus
		d.Resubmit = false
		d.CallPlatform = false
		if in.ExistingStatus == StatusCompleted && strings.TrimSpace(in.ExistingOutputID) != "" {
			d.RegisterOutput = true
			d.OutputAssetID = in.ExistingOutputID
		}
		if in.ExistingStatus == StatusCompleted && strings.TrimSpace(in.ExistingOutputID) == "" {
			d.Status = StatusUnknown
			d.RegisterOutput = false
			d.DegradedReason = "completed_without_asset"
		}
		return d
	}
	if !in.GenerationEnabled {
		d.Status = StatusFailed
		d.DegradedReason = "generation_unavailable"
		return d
	}
	if in.UpstreamReachable != nil && !*in.UpstreamReachable {
		d.Status = StatusUnknown
		d.DegradedReason = "task_unreachable"
		d.CallPlatform = true
		return d
	}
	status, asset, reason := mapUpstream(in.UpstreamStatus, in.UpstreamAssetID)
	d.Status = status
	d.CallPlatform = true
	d.DegradedReason = reason
	if status == StatusCompleted && asset != "" {
		d.RegisterOutput = true
		d.OutputAssetID = asset
	}
	return d
}

func mapUpstream(status, assetID string) (string, string, string) {
	assetID = strings.TrimSpace(assetID)
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending":
		return StatusQueued, "", ""
	case "running":
		return StatusRunning, "", ""
	case "failed":
		return StatusFailed, "", ""
	case "completed", "succeeded":
		if assetID == "" {
			return StatusUnknown, "", "completed_without_asset"
		}
		return StatusCompleted, assetID, ""
	default:
		return StatusUnknown, "", "unrecognized_status"
	}
}

func Measure(f PathFacts) Journey {
	j := Journey{}
	if f.UploadedNew {
		j.Steps++
		j.InputCount = 1
	}
	if f.FilledScene {
		j.Steps++
	}
	if f.FilledSize {
		j.Steps++
	}
	if f.FilledBackground {
		j.Steps++
	}
	if f.ConfirmedQuote {
		j.Steps++
	}
	if f.Submitted {
		j.Steps++
	}
	if f.HasAuthorizedAssets && f.UploadedNew {
		j.CrossProductReuploads = 1
	}
	if f.Recovered {
		j.Recoveries = 1
	}
	return j
}

func FirstScreen() Screen {
	return Screen{
		Title:        "开始做产品图",
		ShowsCatalog: false,
		Steps:        []string{"上传或选择商品照片", "选择使用场景和尺寸", "背景方向", "费用确认", "生成"},
	}
}

func ExactReturn(sourceType, href string) (string, error) {
	if sourceType != "order" && sourceType != "campaign" {
		return "", ErrReturnNotAllowed
	}
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return "", ErrReturnInvalid
	}
	loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", ErrReturnInvalid
	}
	return strings.TrimSpace(href), nil
}
