// Package videofixture 是版本化的内存图生视频夹具。
// 它不联网、不占端口、不访问数据库、不调用供应商，也不生成视频字节。
// 夹具不是真实视频。状态保持「真实视频未验证」。
package videofixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

const SchemaVersion = "image-to-video-fixture/v1"

const (
	StatusUnverified        = "真实视频未验证"
	ProductionNotAuthorized = "NOT_AUTHORIZED"
	NoticeNotRealVideo      = "夹具不是真实视频"
)

const (
	LabelEngineering = "Engineering"
	LabelBrowser     = "Browser NOT_RUN"
	LabelService     = "Service NOT_VERIFIED"
	LabelBilling     = "Billing NOT_VERIFIED"
	LabelProduction  = "Production NOT_AUTHORIZED"
	LabelHuman       = "Human UNKNOWN"
)

var (
	ErrConfigMissing  = errors.New("配置缺失")
	ErrStillImage     = errors.New("静图不能当成视频")
	ErrPlaceholderURL = errors.New("占位地址不能当成视频")
	ErrEmptyByteClaim = errors.New("空字节声明被拒绝")
	ErrSuccessPhrase  = errors.New("调用方口径被拒绝")
	ErrKeptUnverified = errors.New("夹具保持未验证")
)

// Input 是一次图生视频夹具请求。字节和地址都不会被当成视频。
type Input struct {
	RequestID      string
	TenantID       string
	ProjectID      string
	ImageDigest    string
	Still          bool
	MediaType      string
	PlaceholderURL string
	ByteCount      *int
	ClaimedBytes   []byte
	CallerPhrase   string
}

// Document 是未验证夹具。ShowVideo、PlayStill 与 RealVideo 保持 false。
type Document struct {
	SchemaVersion       string   `json:"schema_version"`
	RequestID           string   `json:"request_id"`
	TenantID            string   `json:"tenant_id"`
	ProjectID           string   `json:"project_id"`
	ImageDigest         string   `json:"image_digest"`
	Status              string   `json:"status"`
	ShowVideo           bool     `json:"show_video"`
	PlayStill           bool     `json:"play_still"`
	VideoURL            string   `json:"video_url"`
	ProviderCalls       int      `json:"provider_calls"`
	GenerationTriggered bool     `json:"generation_triggered"`
	BillingPassed       bool     `json:"billing_passed"`
	Charged             *string  `json:"charged"`
	ProductionStatus    string   `json:"production_status"`
	RealVideo           bool     `json:"real_video"`
	Notice              string   `json:"notice"`
	Labels              []string `json:"labels"`
}

// GateLabels 按固定顺序返回六枚核对标签。
func GateLabels() []string {
	return []string{
		LabelEngineering,
		LabelBrowser,
		LabelService,
		LabelBilling,
		LabelProduction,
		LabelHuman,
	}
}

// LogicalRequestID 是回放用的逻辑请求编号。显式编号优先，否则由租户、工程和摘要派生。
func LogicalRequestID(in Input) string {
	if id := strings.TrimSpace(in.RequestID); id != "" {
		return id
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(in.TenantID),
		strings.TrimSpace(in.ProjectID),
		strings.TrimSpace(in.ImageDigest),
	}, "\n")))
	return hex.EncodeToString(sum[:])
}

// Build 产出一份未验证夹具，或在配置缺失、静图、占位地址、空字节、成功口径时返回 nil。
func Build(in Input) (*Document, error) {
	tenant := strings.TrimSpace(in.TenantID)
	project := strings.TrimSpace(in.ProjectID)
	digest := strings.TrimSpace(in.ImageDigest)
	if tenant == "" || project == "" || digest == "" {
		return nil, ErrConfigMissing
	}
	requestID := strings.TrimSpace(in.RequestID)
	if containsBanned(tenant) || containsBanned(project) || containsBanned(digest) || containsBanned(requestID) {
		return nil, ErrSuccessPhrase
	}
	if in.Still || isStillMedia(in.MediaType) {
		return nil, ErrStillImage
	}
	if strings.TrimSpace(in.PlaceholderURL) != "" {
		return nil, ErrPlaceholderURL
	}
	if emptyByteClaim(in) {
		return nil, ErrEmptyByteClaim
	}
	if claimsVideoSucceeded(in.CallerPhrase) {
		return nil, ErrSuccessPhrase
	}
	return &Document{
		SchemaVersion:       SchemaVersion,
		RequestID:           LogicalRequestID(in),
		TenantID:            tenant,
		ProjectID:           project,
		ImageDigest:         digest,
		Status:              StatusUnverified,
		ShowVideo:           false,
		PlayStill:           false,
		VideoURL:            "",
		ProviderCalls:       0,
		GenerationTriggered: false,
		BillingPassed:       false,
		Charged:             nil,
		ProductionStatus:    ProductionNotAuthorized,
		RealVideo:           false,
		Notice:              NoticeNotRealVideo,
		Labels:              GateLabels(),
	}, nil
}

func isStillMedia(media string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(media)), "image/")
}

func emptyByteClaim(in Input) bool {
	if in.ByteCount != nil && *in.ByteCount <= 0 {
		return true
	}
	return in.ClaimedBytes != nil && len(in.ClaimedBytes) == 0
}

func claimsVideoSucceeded(phrase string) bool {
	p := strings.TrimSpace(phrase)
	if p == "" {
		return false
	}
	if containsBanned(p) || strings.Contains(p, "成功") {
		return true
	}
	lower := strings.ToLower(p)
	if strings.Contains(lower, "success") || strings.Contains(lower, "succeed") || strings.Contains(lower, "completed") {
		return true
	}
	switch lower {
	case "pass", "passed", "done", "ok":
		return true
	default:
		return false
	}
}

func containsBanned(s string) bool {
	if strings.Contains(s, "图生视频成功") || strings.Contains(s, "可播放") || strings.Contains(s, "真实出图完成") {
		return true
	}
	return strings.Contains(strings.ToLower(s), "billing pass")
}

// CanonicalJSON 编码诚实夹具。成功形态和 nil 文档都不产出字节。
func CanonicalJSON(doc *Document) ([]byte, error) {
	if err := doc.refuse(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (d *Document) refuse() error {
	if d == nil ||
		d.SchemaVersion != SchemaVersion ||
		d.Status != StatusUnverified ||
		d.ShowVideo ||
		d.PlayStill ||
		d.VideoURL != "" ||
		d.ProviderCalls != 0 ||
		d.GenerationTriggered ||
		d.BillingPassed ||
		d.Charged != nil ||
		d.ProductionStatus != ProductionNotAuthorized ||
		d.RealVideo ||
		d.Notice != NoticeNotRealVideo ||
		!sameLabels(d.Labels) ||
		containsBanned(d.joined()) {
		return ErrKeptUnverified
	}
	return nil
}

func sameLabels(labels []string) bool {
	want := GateLabels()
	if len(labels) != len(want) {
		return false
	}
	for i := range want {
		if labels[i] != want[i] {
			return false
		}
	}
	return true
}

func (d *Document) joined() string {
	parts := []string{
		d.SchemaVersion, d.RequestID, d.TenantID, d.ProjectID, d.ImageDigest,
		d.Status, d.VideoURL, d.ProductionStatus, d.Notice,
	}
	parts = append(parts, d.Labels...)
	return strings.Join(parts, "\n")
}

func (d *Document) clone() *Document {
	if d == nil {
		return nil
	}
	cp := *d
	if d.Labels != nil {
		cp.Labels = append([]string(nil), d.Labels...)
	}
	return &cp
}

// Registry 按逻辑请求编号保存第一份夹具。回放不建立第二个任务。
type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Document
}

// NewRegistry 返回空的内存登记簿。
func NewRegistry() *Registry {
	return &Registry{jobs: map[string]*Document{}}
}

// Jobs 是已保存的任务数。
func (r *Registry) Jobs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

// Submit 保存可接受的请求，或在同一逻辑编号上返回已有文档的副本。
func (r *Registry) Submit(in Input) (*Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := storedKey(in); ok {
		if existing := r.jobs[id]; existing != nil {
			return existing.clone(), nil
		}
	}
	doc, err := Build(in)
	if err != nil {
		return nil, err
	}
	if existing := r.jobs[doc.RequestID]; existing != nil {
		return existing.clone(), nil
	}
	r.jobs[doc.RequestID] = doc.clone()
	return doc.clone(), nil
}

func storedKey(in Input) (string, bool) {
	if id := strings.TrimSpace(in.RequestID); id != "" {
		return id, true
	}
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.ImageDigest) == "" {
		return "", false
	}
	return LogicalRequestID(in), true
}
