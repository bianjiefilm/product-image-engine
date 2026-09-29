// Package qualitygate 记录一次产品价值检查。
// 它不生成图片，不调用供应商，也不通过产品价值门。
// 入库记录的状态保持「真实出图未完成」。
package qualitygate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

const SchemaVersion = "product-value-check/v1"

const (
	StatusIncomplete        = "真实出图未完成"
	ProductionNotAuthorized = "NOT_AUTHORIZED"
	NoticeNotPassed         = "此记录不通过产品价值门"
)

const (
	LabelEngineering = "Engineering for the recorder test only"
	LabelBrowser     = "Browser NOT_RUN"
	LabelService     = "Service NOT_VERIFIED"
	LabelBilling     = "Billing NOT_VERIFIED"
	LabelProduction  = "Production NOT_AUTHORIZED"
	LabelHuman       = "Human UNKNOWN"
)

var (
	ErrConfigMissing  = errors.New("配置缺失")
	ErrCallerPhrase   = errors.New("调用方口径被拒绝")
	ErrKeptIncomplete = errors.New("记录保持未完成")
)

// Input 是调用方试图送入的检查。通过旗标、扣费、供应商计数、状态、生产和图片字节都不会被抄进记录。
type Input struct {
	LogicalID        string
	TenantID         string
	ProjectID        string
	Digest           string
	Quality          string
	Generation       string
	Fee              string
	QualityPassed    bool
	GenerationPassed bool
	BillingPassed    bool
	Charged          *string
	ProviderCalls    int
	Status           string
	Production       string
	CallerPhrase     string
	ImageBytes       []byte
}

// Record 是一次未通过的产品价值检查。
type Record struct {
	SchemaVersion    string   `json:"schema_version"`
	LogicalID        string   `json:"logical_id"`
	TenantID         string   `json:"tenant_id"`
	ProjectID        string   `json:"project_id"`
	Digest           string   `json:"digest"`
	Quality          string   `json:"quality"`
	Generation       string   `json:"generation"`
	Fee              string   `json:"fee"`
	Status           string   `json:"status"`
	QualityPassed    bool     `json:"quality_passed"`
	GenerationPassed bool     `json:"generation_passed"`
	BillingPassed    bool     `json:"billing_passed"`
	Charged          *string  `json:"charged"`
	ProviderCalls    int      `json:"provider_calls"`
	Production       string   `json:"production"`
	Notice           string   `json:"notice"`
	Labels           []string `json:"labels"`
}

// GateLabels 返回固定顺序的六枚证据标签。每次调用都是新切片。
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

// LogicalID 是显式编号；空编号时由租户、工程、摘要派生。观测不进入编号。
func LogicalID(in Input) string {
	if id := strings.TrimSpace(in.LogicalID); id != "" {
		return id
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(in.TenantID),
		strings.TrimSpace(in.ProjectID),
		strings.TrimSpace(in.Digest),
	}, "\n")))
	return hex.EncodeToString(sum[:])
}

// Build 产出未完成记录，或在配置缺失、禁用短语时返回 nil。
func Build(in Input) (*Record, error) {
	logical, tenant, project, digest, quality, generation, fee, ok := presentFields(in)
	if !ok {
		return nil, ErrConfigMissing
	}
	if bannedInput(logical, tenant, project, digest, quality, generation, fee, in.CallerPhrase) {
		return nil, ErrCallerPhrase
	}
	return &Record{
		SchemaVersion:    SchemaVersion,
		LogicalID:        LogicalID(in),
		TenantID:         tenant,
		ProjectID:        project,
		Digest:           digest,
		Quality:          quality,
		Generation:       generation,
		Fee:              fee,
		Status:           StatusIncomplete,
		QualityPassed:    false,
		GenerationPassed: false,
		BillingPassed:    false,
		Charged:          nil,
		ProviderCalls:    0,
		Production:       ProductionNotAuthorized,
		Notice:           NoticeNotPassed,
		Labels:           GateLabels(),
	}, nil
}

func presentFields(in Input) (logical, tenant, project, digest, quality, generation, fee string, ok bool) {
	logical = strings.TrimSpace(in.LogicalID)
	tenant = strings.TrimSpace(in.TenantID)
	project = strings.TrimSpace(in.ProjectID)
	digest = strings.TrimSpace(in.Digest)
	quality = strings.TrimSpace(in.Quality)
	generation = strings.TrimSpace(in.Generation)
	fee = strings.TrimSpace(in.Fee)
	ok = tenant != "" && project != "" && digest != "" && quality != "" && generation != "" && fee != ""
	return
}

func bannedInput(fields ...string) bool {
	for _, field := range fields {
		if containsBanned(field) {
			return true
		}
	}
	return false
}

func containsBanned(s string) bool {
	if strings.Contains(s, "工程通过") || strings.Contains(s, "质量通过") || strings.Contains(s, "真实出图完成") {
		return true
	}
	return strings.Contains(strings.ToLower(s), "billing pass")
}

// CanonicalJSON 只编码仍保持未完成的记录。成功形态返回 nil 字节。
func CanonicalJSON(rec *Record) ([]byte, error) {
	if err := rec.refuse(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (r *Record) refuse() error {
	if r == nil ||
		r.SchemaVersion != SchemaVersion ||
		r.LogicalID == "" ||
		r.TenantID == "" ||
		r.ProjectID == "" ||
		r.Digest == "" ||
		r.Quality == "" ||
		r.Generation == "" ||
		r.Fee == "" ||
		r.Status != StatusIncomplete ||
		r.QualityPassed ||
		r.GenerationPassed ||
		r.BillingPassed ||
		r.Charged != nil ||
		r.ProviderCalls != 0 ||
		r.Production != ProductionNotAuthorized ||
		r.Notice != NoticeNotPassed ||
		!sameLabels(r.Labels) ||
		containsBanned(r.joined()) {
		return ErrKeptIncomplete
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

func (r *Record) joined() string {
	parts := []string{
		r.SchemaVersion, r.LogicalID, r.TenantID, r.ProjectID, r.Digest,
		r.Quality, r.Generation, r.Fee, r.Status, r.Production, r.Notice,
	}
	parts = append(parts, r.Labels...)
	return strings.Join(parts, "\n")
}

func (r *Record) clone() *Record {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Labels != nil {
		cp.Labels = append([]string(nil), r.Labels...)
	}
	if r.Charged != nil {
		v := *r.Charged
		cp.Charged = &v
	}
	return &cp
}

// Registry 按逻辑编号保存第一份未完成记录。
type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Record
}

// NewRegistry 返回空登记簿。
func NewRegistry() *Registry {
	return &Registry{jobs: map[string]*Record{}}
}

// Checks 返回已入库的检查数。
func (r *Registry) Checks() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

// Submit 在配置缺失时不入库。同一逻辑编号再次提交返回第一份副本。
func (r *Registry) Submit(in Input) (*Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _, _, _, _, _, _, ok := presentFields(in)
	if !ok {
		return nil, ErrConfigMissing
	}
	if existing := r.jobs[LogicalID(in)]; existing != nil {
		return existing.clone(), nil
	}
	rec, err := Build(in)
	if err != nil {
		return nil, err
	}
	if existing := r.jobs[rec.LogicalID]; existing != nil {
		return existing.clone(), nil
	}
	r.jobs[rec.LogicalID] = rec.clone()
	return rec.clone(), nil
}
