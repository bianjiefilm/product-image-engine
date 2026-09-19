// Package receiptdoc 构造并发送成果回执:order-receipt/v1 冻结文档
// (public-ai docs/contracts/order-handoff/v1/receipt.schema.json)+ §8 HMAC
// 传输帧(README §8 冻结文本;与 guanlan-order backend/internal/handoffv1/frame.go
// 同语义的发送端实现)。
//
// 红线:回执只陈述"成果可取"的事实(不可变结果引用/来源版本/task/费用事实),
// 不带内部路径或长期下载密钥;billing_fact_refs 只引用既有计费事实,绝不触发
// 扣费;回执失败不追加生成任务。
package receiptdoc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SchemaVersionV1 回执冻结版本。
const SchemaVersionV1 = "order-receipt/v1"

// 帧头(§8 冻结线格式常量)。
const (
	HeaderKeyID     = "X-Handoff-Key-Id"
	HeaderTimestamp = "X-Handoff-Timestamp"
	HeaderSignature = "X-Handoff-Signature"
	HeaderAttempt   = "X-Handoff-Attempt"
	SignaturePrefix = "sha256="
)

// MaxTimestampSkew 冻结时钟偏差上界。
const MaxTimestampSkew = 300 * time.Second

// Receipt 回执文档(方向与 handoff 相反:source_app=制作工具,target_app=来源)。
type Receipt struct {
	SchemaVersion   string   `json:"schema_version"`
	EventID         string   `json:"event_id"`
	RunID           string   `json:"run_id"`
	HandoffID       string   `json:"handoff_id"`
	SourceApp       string   `json:"source_app"`
	TargetApp       string   `json:"target_app"`
	PrincipalID     string   `json:"principal_id"`
	ProjectRef      string   `json:"project_ref"`
	ProjectRevision string   `json:"project_revision"`
	BriefVersion    string   `json:"brief_version"`
	Sequence        int64    `json:"sequence"`
	Status          string   `json:"status"`
	Assets          []Asset  `json:"assets"`
	BillingFactRefs []string `json:"billing_fact_refs"`
	OccurredAt      string   `json:"occurred_at"`
}

// Asset 不可变结果引用(无内部路径/无下载密钥)。
type Asset struct {
	AssetRef  string `json:"asset_ref"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	MediaType string `json:"media_type"`
}

var (
	tsRe     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
	sha256Re = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// isWhite 固定 Unicode White_Space 集合(冻结纪律)。
func isWhite(r rune) bool {
	switch r {
	case 0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x0085, 0x00A0,
		0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
		0x2007, 0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

// validateString 1–256 code points、至少一个非空白字符(White_Space 集合)。
func validateString(s string) bool {
	n := 0
	nonBlank := false
	for _, r := range s {
		n++
		if n > 256 {
			return false
		}
		if !isWhite(r) {
			nonBlank = true
		}
	}
	return n >= 1 && nonBlank
}

// Validate 构造前校验(所有必填字段 + 枚举 + 时间 + hex)。
func (r *Receipt) Validate() error {
	if r.SchemaVersion != SchemaVersionV1 {
		return errors.New("receiptdoc: schema_version 必须为 " + SchemaVersionV1)
	}
	fields := map[string]string{
		"event_id": r.EventID, "run_id": r.RunID, "handoff_id": r.HandoffID,
		"source_app": r.SourceApp, "target_app": r.TargetApp, "principal_id": r.PrincipalID,
		"project_ref": r.ProjectRef, "project_revision": r.ProjectRevision, "brief_version": r.BriefVersion,
	}
	for name, v := range fields {
		if !validateString(v) {
			return errors.New("receiptdoc: " + name + " 非法(1–256 code points 非全空白)")
		}
	}
	if r.Sequence < 1 {
		return errors.New("receiptdoc: sequence 须正整数")
	}
	switch r.Status {
	case "running", "succeeded", "failed", "cancelled":
	default:
		return errors.New("receiptdoc: status 非法")
	}
	if len(r.Assets) > 100 {
		return errors.New("receiptdoc: assets 超过 100 项上限")
	}
	for i, a := range r.Assets {
		if !validateString(a.AssetRef) || !validateString(a.MediaType) {
			return errors.New("receiptdoc: assets 字段非法(asset_ref/media_type)")
		}
		if !sha256Re.MatchString(a.SHA256) {
			return errors.New("receiptdoc: assets[" + strconv.Itoa(i) + "].sha256 须 64 小写 hex")
		}
		if a.SizeBytes <= 0 {
			return errors.New("receiptdoc: assets[" + strconv.Itoa(i) + "].size_bytes 须正整数")
		}
	}
	if len(r.BillingFactRefs) > 100 {
		return errors.New("receiptdoc: billing_fact_refs 超过 100 项上限")
	}
	for _, ref := range r.BillingFactRefs {
		if !validateString(ref) {
			return errors.New("receiptdoc: billing_fact_refs 含非法引用")
		}
	}
	if !tsRe.MatchString(r.OccurredAt) {
		return errors.New("receiptdoc: occurred_at 须 YYYY-MM-DDTHH:mm:ssZ")
	}
	return nil
}

// frameSigningString §8 签名串(attempt 有意不作为输入)。
func frameSigningString(timestamp int64, sourceApp, targetApp, eventID string, rawBody []byte) []byte {
	b := make([]byte, 0, 64+len(rawBody))
	b = strconv.AppendInt(b, timestamp, 10)
	b = append(b, '\n')
	b = append(b, sourceApp...)
	b = append(b, '\n')
	b = append(b, targetApp...)
	b = append(b, '\n')
	b = append(b, eventID...)
	b = append(b, '\n')
	b = append(b, rawBody...)
	return b
}

// FrameSign 返回 X-Handoff-Signature 线格式值(重投刷新 timestamp 与签名,raw_body 不变)。
func FrameSign(secret []byte, timestamp int64, sourceApp, targetApp, eventID string, rawBody []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(frameSigningString(timestamp, sourceApp, targetApp, eventID, rawBody))
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// FrameVerify §8 帧校验(对端语义;E2E stub 侧同规则自检用)。
func FrameVerify(secret []byte, timestampHeader, signatureHeader, sourceApp, targetApp, eventID string, rawBody []byte, now time.Time) error {
	if strings.ContainsAny(timestampHeader, " \t-+.eE") || timestampHeader == "" {
		return errors.New("receiptdoc: frame timestamp encoding")
	}
	ts, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil || ts < 0 {
		return errors.New("receiptdoc: frame timestamp encoding")
	}
	if skew := now.Sub(time.Unix(ts, 0)); skew > MaxTimestampSkew || skew < -MaxTimestampSkew {
		return errors.New("receiptdoc: frame timestamp skew beyond 300s")
	}
	if !strings.HasPrefix(signatureHeader, SignaturePrefix) {
		return errors.New("receiptdoc: frame signature encoding")
	}
	provided, err := hex.DecodeString(signatureHeader[len(SignaturePrefix):])
	if err != nil || len(provided) != sha256.Size {
		return errors.New("receiptdoc: frame signature encoding")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(frameSigningString(ts, sourceApp, targetApp, eventID, rawBody))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("receiptdoc: frame signature invalid")
	}
	return nil
}
