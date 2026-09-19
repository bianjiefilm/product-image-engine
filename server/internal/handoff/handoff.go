// Package handoff 实现订单交接契约 order-handoff/v1(冻结线格式,HUI-655)
// 及其 source_profile/v1 扩展(HUI-1724 SOURCE-PROFILE)的解析与语义校验。
//
// 本包是 HUI-1724 契约的消费端实现(产品图应用侧),与 public-ai 仓库的
// internal/handoffcontract 同语义;因本仓不引外部模块依赖,按冻结 README
// + SOURCE-PROFILE + 共享向量(testdata/source-profile-vectors.json)在本仓
// 实现同规则,向量测试钉住一致性。
//
// 红线(冻结):严格 JSON(≤1 MiB、重复键/未知字段/null/尾随内容拒绝、
// 嵌套≤16)、字符串 1–256 code points 非全空白(固定 White_Space 集合)、
// 时间严格 YYYY-MM-DDTHH:mm:ssZ、sha256/proof_digest 64 小写 hex、
// scopes 封闭枚举 1–4(无生成/付费 scope)、issued_at < expires_at ≤ +900s、
// 二分规则:顶层含 source_profile 键 → 扩展文档;source_kind=order 禁止;
// standalone/campaign 文档 order_ref/stage_ref 必须缺席;租户精确比对
// fail-closed;垃圾 expires_at fail-closed 视为已过期。
package handoff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
	"unicode/utf8"
)

// 契约版本常量(冻结/扩展版本化:未识别版本显式拒绝,不做 best-effort)。
const (
	SchemaVersionV1       = "order-handoff/v1"
	ProfileVersionV1      = "source-profile/v1"
	MaxDocumentBytes      = 1 << 20 // 1 MiB
	MaxDepth              = 16
	MaxStringCodePoints   = 256
	MaxDescriptionCodePts = 4096
	MaxArrayItems         = 100
	MaxOpenGates          = 100
	MaxGrantSeconds       = 900
)

// ScopesV1 封闭枚举(v1 冻结:无生成/付费 scope——接受交接绝不自动创建任务或扣费)。
var ScopesV1 = []string{"project.resume", "asset.import", "receipt.write", "receipt.read"}

// Error 结构化契约错误:Code 机器可读,Detail 中文说明。
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return "handoff: " + e.Code + ": " + e.Detail }

// 错误码(v1 冻结 + 1724 扩展)。
const (
	CodeUnsupportedSchemaVersion = "unsupported_schema_version"
	CodeInvalidDocument          = "invalid_document"
	CodeUnsupportedProfile       = "unsupported_profile"
	CodeSourceKindMismatch       = "source_kind_mismatch"
)

func errf(code, format string, a ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// SourceKind 来源种类(order=纯 v1;standalone/campaign=profile 扩展)。
type SourceKind string

const (
	KindOrder      SourceKind = "order"
	KindStandalone SourceKind = "standalone"
	KindCampaign   SourceKind = "campaign"
)

type Actor struct {
	Issuer  string `json:"issuer"`
	AppID   string `json:"app_id"`
	Subject string `json:"subject"`
}

type Binding struct {
	BindingRef  string `json:"binding_ref"`
	ProofDigest string `json:"proof_digest"`
}

type Gating struct {
	PolicyVersion string   `json:"policy_version"`
	OpenGates     []string `json:"open_gates"`
}

type Asset struct {
	AssetRef  string `json:"asset_ref"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	MediaType string `json:"media_type"`
}

type DeliverySpec struct {
	MediaType   string `json:"media_type"`
	Description string `json:"description"`
}

// SourceProfile 非订单来源扩展段(v1 封套零变化)。
type SourceProfile struct {
	ProfileVersion string   `json:"profile_version"`
	SourceKind     string   `json:"source_kind"`
	TenantScope    string   `json:"tenant_scope"`
	Capabilities   []string `json:"capabilities"`
	Constraints    []string `json:"constraints"`
	ReturnTargetID string   `json:"return_target_id"`
	CampaignRef    string   `json:"campaign_ref"`
}

// Handoff 解析后的交接文档。OrderRef/StageRef 用指针承担
// "存在性而非空串"裁决(standalone/campaign 必须缺席)。
type Handoff struct {
	SchemaVersion    string         `json:"schema_version"`
	HandoffID        string         `json:"handoff_id"`
	SourceApp        string         `json:"source_app"`
	TargetApp        string         `json:"target_app"`
	PrincipalID      string         `json:"principal_id"`
	OrderRef         *string        `json:"order_ref"`
	StageRef         *string        `json:"stage_ref"`
	BriefVersion     string         `json:"brief_version"`
	SourceProjectRef string         `json:"source_project_ref"`
	SourceRevision   string         `json:"source_revision"`
	Actor            Actor          `json:"actor"`
	Binding          Binding        `json:"binding"`
	Gating           Gating         `json:"gating"`
	Assets           []Asset        `json:"assets"`
	DeliverySpec     DeliverySpec   `json:"delivery_spec"`
	Scopes           []string       `json:"scopes"`
	IssuedAt         string         `json:"issued_at"`
	ExpiresAt        string         `json:"expires_at"`
	SourceProfile    *SourceProfile `json:"source_profile"`

	// 派生(非线格式字段):kind 与已解析时间。
	kind     SourceKind
	issuedT  time.Time
	expiresT time.Time
}

// Kind 返回来源种类(解析后确定;order=纯 v1)。
func (h *Handoff) Kind() SourceKind {
	if h == nil {
		return ""
	}
	if h.kind == "" {
		return KindOrder
	}
	return h.kind
}

// IssuedAtTime / ExpiresAtTime 返回已校验的时间。
func (h *Handoff) IssuedAtTime() time.Time  { return h.issuedT }
func (h *Handoff) ExpiresAtTime() time.Time { return h.expiresT }

// ---- 严格 JSON 基础设施 -------------------------------------------------

// strictDecode 严格解码:重复键拒绝、null 拒绝、嵌套≤MaxDepth、
// 未知字段拒绝、尾随内容拒绝、孤立 surrogate 拒绝。
func strictDecode(raw []byte, out any) error {
	if len(raw) > MaxDocumentBytes {
		return errf(CodeInvalidDocument, "文档超过 %d 字节上限", MaxDocumentBytes)
	}
	if err := rejectLoneSurrogates(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	walkDepth := 0
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return errf(CodeInvalidDocument, "JSON 解析失败: %v", err)
		}
		if d, ok := tok.(json.Delim); ok {
			walkDepth++
			if walkDepth > MaxDepth {
				return errf(CodeInvalidDocument, "嵌套超过 %d 层上限", MaxDepth)
			}
			switch d {
			case '{':
				seen := map[string]bool{}
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return errf(CodeInvalidDocument, "JSON 解析失败: %v", err)
					}
					key, ok := kt.(string)
					if !ok {
						return errf(CodeInvalidDocument, "对象键非字符串")
					}
					if seen[key] {
						return errf(CodeInvalidDocument, "重复键 %q", key)
					}
					seen[key] = true
					if err := walk(); err != nil {
						return err
					}
				}
				if _, err := dec.Token(); err != nil { // 收 '}'
					return errf(CodeInvalidDocument, "JSON 解析失败: %v", err)
				}
			case '[':
				for dec.More() {
					if err := walk(); err != nil {
						return err
					}
				}
				if _, err := dec.Token(); err != nil { // 收 ']'
					return errf(CodeInvalidDocument, "JSON 解析失败: %v", err)
				}
			}
			walkDepth--
			return nil
		}
		if tok == nil {
			return errf(CodeInvalidDocument, "文档含 null 值(全部属性必填)")
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	// 顶层只允许一个 JSON 值(尾随内容拒绝)。
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errf(CodeInvalidDocument, "存在尾随内容")
	}
	// 结构解码:未知字段拒绝。
	d2 := json.NewDecoder(bytes.NewReader(raw))
	d2.DisallowUnknownFields()
	if err := d2.Decode(out); err != nil {
		return errf(CodeInvalidDocument, "结构校验失败: %v", err)
	}
	return nil
}

// rejectLoneSurrogates 拒绝孤立 surrogate escape(\uDC00 单独出现等);
// 合法 surrogate pair 必须解析为同一字符(encoding/json 默认行为已保证)。
func rejectLoneSurrogates(raw []byte) error {
	s := string(raw)
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '\\' || s[i+1] != 'u' {
			continue
		}
		if i+6 > len(s) {
			return errf(CodeInvalidDocument, "孤立 surrogate escape")
		}
		r, ok := parseHex4(s[i+2 : i+6])
		if !ok {
			return errf(CodeInvalidDocument, "非法 \\u escape")
		}
		if r >= 0xD800 && r <= 0xDBFF {
			// 高代理:后随必须紧跟低代理 escape。
			if i+12 > len(s) || s[i+6] != '\\' || s[i+7] != 'u' {
				return errf(CodeInvalidDocument, "孤立高代理 surrogate")
			}
			lo, ok := parseHex4(s[i+8 : i+12])
			if !ok || lo < 0xDC00 || lo > 0xDFFF {
				return errf(CodeInvalidDocument, "孤立高代理 surrogate")
			}
			i += 6
			continue
		}
		if r >= 0xDC00 && r <= 0xDFFF {
			return errf(CodeInvalidDocument, "孤立低代理 surrogate")
		}
	}
	return nil
}

func parseHex4(s string) (rune, bool) {
	if len(s) != 4 {
		return 0, false
	}
	v, err := strconvParseUint16(s)
	if err != nil {
		return 0, false
	}
	return rune(v), true
}

// ---- 字符串/数组纪律 ----------------------------------------------------

// isWhiteSpace 固定 Unicode White_Space 集合(冻结;不用 \s/unicode.IsSpace)。
func isWhiteSpace(r rune) bool {
	switch r {
	case 0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x0085, 0x00A0,
		0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
		0x2007, 0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

func validCodeString(s string, max int) bool {
	n := utf8.RuneCountInString(s)
	if n < 1 || n > max {
		return false
	}
	for _, r := range s {
		if isWhiteSpace(r) {
			continue
		}
		return true // 至少一个非空白
	}
	return false // 全空白
}

var (
	sha256Re = regexp.MustCompile(`^[a-f0-9]{64}$`)
	tsRe     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
)

func validSHA256(s string) bool { return sha256Re.MatchString(s) }

// parseTimestamp 严格 UTC YYYY-MM-DDTHH:mm:ssZ,真实日期,年 0001–9999,无小数秒。
func parseTimestamp(s string) (time.Time, bool) {
	if !tsRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", s)
	if err != nil {
		return time.Time{}, false
	}
	if t.Year() < 1 {
		return time.Time{}, false // 公历年份 0001–9999(拒绝 year 0000)
	}
	// time.Parse 对 2026-02-30 这类假日期会报错,这里再复核字段一致。
	if t.Format("2006-01-02T15:04:05Z") != s {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// IsExpired 交接是否已过期(垃圾时间 fail-closed:视为已过期)。
func IsExpired(now time.Time, expiresAtRaw string) bool {
	t, ok := parseTimestamp(expiresAtRaw)
	if !ok {
		return true
	}
	return !now.Before(t) // now >= expires_at → 过期(边界等号过期)
}

// TenantScopeMatches 租户精确比对(任一侧为空 fail-closed,不 trim 不归一)。
func TenantScopeMatches(declared, expected string) bool {
	if declared == "" || expected == "" {
		return false
	}
	return declared == expected
}

// ---- Parse ---------------------------------------------------------------

// Parse 解析并校验交接文档(二分规则:顶层含 source_profile 键 → 扩展。
// 本应用是 profile 感知消费者:结构体以指针字段承载该键,解码后按
// SourceProfile 是否存在二分;纯 v1 形态下该键缺席即订单来源)。
func Parse(raw []byte) (*Handoff, error) {
	var h Handoff
	if err := strictDecode(raw, &h); err != nil {
		return nil, err
	}
	if h.SchemaVersion != SchemaVersionV1 {
		return nil, errf(CodeUnsupportedSchemaVersion, "schema_version %q 不受支持(仅 order-handoff/v1)", h.SchemaVersion)
	}
	if h.SourceProfile != nil {
		if err := validateProfile(h.SourceProfile); err != nil {
			return nil, err
		}
		if h.SourceProfile.SourceKind == string(KindOrder) {
			return nil, errf(CodeSourceKindMismatch, "扩展文档声明 source_kind=order 禁止(订单来源只有纯 v1 一种线形式)")
		}
		// standalone/campaign:order_ref/stage_ref 必须缺席(键级检查,指针存在即拒)。
		if h.OrderRef != nil {
			return nil, errf(CodeInvalidDocument, "非订单来源文档含 order_ref(standalone 不造假订单)")
		}
		if h.StageRef != nil {
			return nil, errf(CodeInvalidDocument, "非订单来源文档含 stage_ref(standalone 不造假订单)")
		}
		h.kind = SourceKind(h.SourceProfile.SourceKind)
	} else {
		// 纯 v1(order):order_ref/stage_ref 必填。
		if h.OrderRef == nil || !validCodeString(*h.OrderRef, MaxStringCodePoints) {
			return nil, errf(CodeInvalidDocument, "订单来源文档必须含非空 order_ref")
		}
		if h.StageRef == nil || !validCodeString(*h.StageRef, MaxStringCodePoints) {
			return nil, errf(CodeInvalidDocument, "订单来源文档必须含非空 stage_ref")
		}
		h.kind = KindOrder
	}
	if err := validateCommon(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

func validateProfile(p *SourceProfile) error {
	if p.ProfileVersion != ProfileVersionV1 {
		return errf(CodeUnsupportedProfile, "profile_version %q 不受支持(仅 source-profile/v1)", p.ProfileVersion)
	}
	switch p.SourceKind {
	case string(KindStandalone):
		if p.CampaignRef != "" {
			return errf(CodeInvalidDocument, "standalone 文档不得含 campaign_ref")
		}
	case string(KindCampaign):
		if !validCodeString(p.CampaignRef, MaxStringCodePoints) {
			return errf(CodeInvalidDocument, "campaign 来源必须含非空 campaign_ref")
		}
	case string(KindOrder):
		// 冻结:订单来源只有纯 v1 一种线形式,杜绝双形式漂移。
		return errf(CodeSourceKindMismatch, "扩展文档声明 source_kind=order 禁止")
	default:
		return errf(CodeInvalidDocument, "source_kind 非法 %q(仅 standalone|campaign)", p.SourceKind)
	}
	if !validCodeString(p.TenantScope, MaxStringCodePoints) {
		return errf(CodeInvalidDocument, "source_profile.tenant_scope 必填(1–256 code points)")
	}
	if !validCodeString(p.ReturnTargetID, MaxStringCodePoints) {
		return errf(CodeInvalidDocument, "source_profile.return_target_id 必填(App Registry 登记 target)")
	}
	if len(p.Capabilities) < 1 || len(p.Capabilities) > MaxArrayItems {
		return errf(CodeInvalidDocument, "capabilities 须 1..%d 项", MaxArrayItems)
	}
	if err := validDedupStrings(p.Capabilities, "capabilities"); err != nil {
		return err
	}
	if len(p.Constraints) > MaxArrayItems {
		return errf(CodeInvalidDocument, "constraints 超过 %d 项上限", MaxArrayItems)
	}
	return validDedupStrings(p.Constraints, "constraints")
}

func validateCommon(h *Handoff) error {
	for name, v := range map[string]string{
		"handoff_id": h.HandoffID, "source_app": h.SourceApp, "target_app": h.TargetApp,
		"principal_id": h.PrincipalID, "brief_version": h.BriefVersion,
		"source_project_ref": h.SourceProjectRef, "source_revision": h.SourceRevision,
		"actor.issuer": h.Actor.Issuer, "actor.app_id": h.Actor.AppID, "actor.subject": h.Actor.Subject,
		"binding.binding_ref": h.Binding.BindingRef, "delivery_spec.media_type": h.DeliverySpec.MediaType,
		"gating.policy_version": h.Gating.PolicyVersion,
	} {
		if !validCodeString(v, MaxStringCodePoints) {
			return errf(CodeInvalidDocument, "%s 必填(1–256 code points,非全空白)", name)
		}
	}
	if utf8.RuneCountInString(h.DeliverySpec.Description) > MaxDescriptionCodePts {
		return errf(CodeInvalidDocument, "delivery_spec.description 超过 %d code points 上限", MaxDescriptionCodePts)
	}
	if !validSHA256(h.Binding.ProofDigest) {
		return errf(CodeInvalidDocument, "binding.proof_digest 须 64 小写 hex")
	}
	if len(h.Gating.OpenGates) > MaxOpenGates {
		return errf(CodeInvalidDocument, "gating.open_gates 超过 %d 项上限", MaxOpenGates)
	}
	if err := validDedupStrings(h.Gating.OpenGates, "gating.open_gates"); err != nil {
		return err
	}
	// actor.app_id = source_app(冻结语义)。
	if h.Actor.AppID != h.SourceApp {
		return errf(CodeInvalidDocument, "actor.app_id 必须等于 source_app")
	}
	// assets:0..100;每项字段纪律。
	if len(h.Assets) > MaxArrayItems {
		return errf(CodeInvalidDocument, "assets 超过 %d 项上限", MaxArrayItems)
	}
	for i, a := range h.Assets {
		if !validCodeString(a.AssetRef, MaxStringCodePoints) {
			return errf(CodeInvalidDocument, "assets[%d].asset_ref 必填", i)
		}
		if !validSHA256(a.SHA256) {
			return errf(CodeInvalidDocument, "assets[%d].sha256 须 64 小写 hex", i)
		}
		if a.SizeBytes <= 0 {
			return errf(CodeInvalidDocument, "assets[%d].size_bytes 须正整数", i)
		}
		if !validCodeString(a.MediaType, MaxStringCodePoints) {
			return errf(CodeInvalidDocument, "assets[%d].media_type 必填", i)
		}
	}
	// scopes:v1 封闭枚举非空去重子集(1–4);无生成/付费 scope。
	if len(h.Scopes) < 1 || len(h.Scopes) > len(ScopesV1) {
		return errf(CodeInvalidDocument, "scopes 须 1..4 项")
	}
	seen := map[string]bool{}
	for _, sc := range h.Scopes {
		valid := false
		for _, s := range ScopesV1 {
			if sc == s {
				valid = true
				break
			}
		}
		if !valid {
			return errf(CodeInvalidDocument, "scopes 含封闭枚举之外的值 %q", sc)
		}
		if seen[sc] {
			return errf(CodeInvalidDocument, "scopes 含重复项 %q", sc)
		}
		seen[sc] = true
	}
	// 时间窗:issued_at < expires_at ≤ issued_at+900s。
	it, ok := parseTimestamp(h.IssuedAt)
	if !ok {
		return errf(CodeInvalidDocument, "issued_at 非法(须 YYYY-MM-DDTHH:mm:ssZ 真实日期)")
	}
	et, ok := parseTimestamp(h.ExpiresAt)
	if !ok {
		return errf(CodeInvalidDocument, "expires_at 非法(须 YYYY-MM-DDTHH:mm:ssZ 真实日期)")
	}
	if !et.After(it) {
		return errf(CodeInvalidDocument, "时间窗非法:要求 issued_at < expires_at")
	}
	if et.Sub(it) > MaxGrantSeconds*time.Second {
		return errf(CodeInvalidDocument, "时间窗非法:expires_at 超过 issued_at+%ds", MaxGrantSeconds)
	}
	h.issuedT, h.expiresT = it, et
	return nil
}

func validDedupStrings(items []string, field string) error {
	seen := map[string]bool{}
	for _, v := range items {
		if !validCodeString(v, MaxStringCodePoints) {
			return errf(CodeInvalidDocument, "%s 含非法项(1–256 code points,非全空白)", field)
		}
		if seen[v] {
			return errf(CodeInvalidDocument, "%s 含重复项 %q", field, v)
		}
		seen[v] = true
	}
	return nil
}

// ---- 来源上下文快照 -------------------------------------------------------

// SourceContext 快照化的来源上下文(不可变;展示用:来源/素材/用途尺寸/需求版本/
// 付款主体/授权范围)。注意:SourceText/描述类字段只是数据——本产品任何执行路径
// (提示词、任务参数、回执)都不拼接来源文本,杜绝指令注入。
type SourceContext struct {
	SourceKind     string       `json:"source_kind"`
	SourceApp      string       `json:"source_app"`
	TenantScope    string       `json:"tenant_scope,omitempty"`
	PrincipalID    string       `json:"principal_id"` // 付款/绑定主体
	OrderRef       string       `json:"order_ref,omitempty"`
	StageRef       string       `json:"stage_ref,omitempty"`
	CampaignRef    string       `json:"campaign_ref,omitempty"`
	ReturnTargetID string       `json:"return_target_id,omitempty"`
	Capabilities   []string     `json:"capabilities,omitempty"`
	Constraints    []string     `json:"constraints,omitempty"`
	Scopes         []string     `json:"scopes"`
	Assets         []Asset      `json:"assets"`
	DeliverySpec   DeliverySpec `json:"delivery_spec"`
	GatingPolicy   string       `json:"gating_policy_version"`
	OpenGates      []string     `json:"open_gates,omitempty"`
	IssuedAt       string       `json:"issued_at"`
	ExpiresAt      string       `json:"expires_at"`
}

// SourceContext 从已解析文档提取来源上下文(快照 profile_json 的结构)。
func (h *Handoff) SourceContext() SourceContext {
	ctx := SourceContext{
		SourceKind:   string(h.Kind()),
		SourceApp:    h.SourceApp,
		PrincipalID:  h.PrincipalID,
		Scopes:       h.Scopes,
		Assets:       h.Assets,
		DeliverySpec: h.DeliverySpec,
		GatingPolicy: h.Gating.PolicyVersion,
		OpenGates:    h.Gating.OpenGates,
		IssuedAt:     h.IssuedAt,
		ExpiresAt:    h.ExpiresAt,
	}
	if h.OrderRef != nil {
		ctx.OrderRef = *h.OrderRef
	}
	if h.StageRef != nil {
		ctx.StageRef = *h.StageRef
	}
	if p := h.SourceProfile; p != nil {
		ctx.TenantScope = p.TenantScope
		ctx.CampaignRef = p.CampaignRef
		ctx.ReturnTargetID = p.ReturnTargetID
		ctx.Capabilities = p.Capabilities
		ctx.Constraints = p.Constraints
	}
	return ctx
}

// ---- 指纹 ---------------------------------------------------------------

// ContentFingerprint 接受内容指纹:对解析后语义字段的规范序列化
// (Go 结构体 marshal 字段序稳定)取 SHA-256。同 handoff_id 异内容由此裁决。
func (h *Handoff) ContentFingerprint() (string, error) {
	b, err := json.Marshal(h)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ---- 规范化错误判定辅助 ---------------------------------------------------

// ErrCode 提取结构化错误码(非 *Error 返回空串)。
func ErrCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// strconvParseUint16 独立小函数(避免引入 strconv 命名冲突歧义)。
func strconvParseUint16(s string) (uint64, error) {
	if len(s) != 4 {
		return 0, fmt.Errorf("bad hex4")
	}
	var v uint64
	for i := 0; i < 4; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			v = v*16 + uint64(c-'0')
		case c >= 'a' && c <= 'f':
			v = v*16 + uint64(c-'a'+10)
		case c >= 'A' && c <= 'F':
			v = v*16 + uint64(c-'A'+10)
		default:
			return 0, fmt.Errorf("bad hex4")
		}
	}
	return v, nil
}
