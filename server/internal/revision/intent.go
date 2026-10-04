package revision

import (
	"errors"
	"regexp"
	"strings"
)

const Schema = "product-revision-intent/v1"

const (
	SourceNaturalLanguage = "natural_language"
	SourceClick           = "click"
)

const (
	ActionSimplifyBackground  = "simplify_background"
	ActionOutdoor             = "outdoor"
	ActionBrightenKeepProduct = "brighten_keep_product"
	ActionRollback            = "rollback"
	ActionAdopt               = "adopt"
	ActionSendDownstream      = "send_downstream"
	ActionUnrecognized        = "unrecognized"
)

const (
	LockProduct       = "product"
	LockLogo          = "logo"
	LockPackagingText = "packaging_text"
)

const (
	RegionBackground = "background"
	RegionLight      = "light"
	RegionNone       = "none"
)

const (
	DownstreamDigitalHuman = "digital_human"
	DownstreamAICut        = "aicut"
	DownstreamMatrix       = "matrix"
)

var (
	ErrUnrecognized         = errors.New("revision: unrecognized")
	ErrPreserveSubject      = errors.New("revision: preserve_subject forbids a silent full redraw")
	ErrImpactUnacknowledged = errors.New("revision: impact must be acknowledged before execution")
	ErrExplicitVersion      = errors.New("revision: explicit version required")
	ErrNotExportable        = errors.New("revision: version is not exportable")
	ErrBadImage             = errors.New("revision: image rejected")
	ErrBadBrief             = errors.New("revision: brief rejected")
	ErrUnknownDownstream    = errors.New("revision: unknown downstream")
)

// Intent 是自然语言和点击动作的同一个修改意图。
type Intent struct {
	Schema          string   `json:"schema"`
	Source          string   `json:"source"`
	Action          string   `json:"action"`
	PreserveSubject bool     `json:"preserve_subject"`
	Locks           []string `json:"locks"`
	Region          string   `json:"region"`
	TargetVersion   string   `json:"target_version,omitempty"`
	Downstream      string   `json:"downstream,omitempty"`
	Text            string   `json:"text"`
}

func lockedIntent(source, text string) Intent {
	return Intent{
		Schema:          Schema,
		Source:          source,
		PreserveSubject: true,
		Locks:           []string{LockProduct, LockLogo, LockPackagingText},
		Region:          RegionNone,
		Text:            text,
	}
}

var rollbackPattern = regexp.MustCompile(`^回到\s*[Vv](\d+)$`)

// ParseNaturalLanguage 把固定说法收成意图。认不出时不执行，也不整图重画。
func ParseNaturalLanguage(text string) (Intent, error) {
	raw := strings.TrimSpace(text)
	normalized := strings.ReplaceAll(raw, ",", "，")
	intent := lockedIntent(SourceNaturalLanguage, raw)
	if m := rollbackPattern.FindStringSubmatch(normalized); m != nil {
		if m[1] == "0" {
			intent.Action = ActionUnrecognized
			return intent, nil
		}
		intent.Action = ActionRollback
		intent.TargetVersion = "V" + m[1]
		return intent, nil
	}
	switch normalized {
	case "背景简单一点":
		intent.Action = ActionSimplifyBackground
		intent.Region = RegionBackground
	case "换成户外":
		intent.Action = ActionOutdoor
		intent.Region = RegionBackground
	case "保留商品，只调亮":
		intent.Action = ActionBrightenKeepProduct
		intent.Region = RegionLight
	case "采用这个版本":
		intent.Action = ActionAdopt
	case "送数字人":
		intent.Action = ActionSendDownstream
		intent.Downstream = DownstreamDigitalHuman
	case "送AiCut":
		intent.Action = ActionSendDownstream
		intent.Downstream = DownstreamAICut
	case "送矩阵":
		intent.Action = ActionSendDownstream
		intent.Downstream = DownstreamMatrix
	default:
		intent.Action = ActionUnrecognized
	}
	return intent, nil
}

// FromClick 用与自然语言相同的字段。
func FromClick(action, targetVersion, downstream string) (Intent, error) {
	intent := lockedIntent(SourceClick, action)
	switch action {
	case ActionSimplifyBackground:
		intent.Action = action
		intent.Region = RegionBackground
	case ActionOutdoor:
		intent.Action = action
		intent.Region = RegionBackground
	case ActionBrightenKeepProduct:
		intent.Action = action
		intent.Region = RegionLight
	case ActionRollback:
		label, err := NormalizeVersion(targetVersion)
		if err != nil {
			return Intent{}, err
		}
		intent.Action = action
		intent.TargetVersion = label
	case ActionAdopt:
		intent.Action = action
	case ActionSendDownstream:
		if !KnownDownstream(downstream) {
			return Intent{}, ErrUnknownDownstream
		}
		intent.Action = action
		intent.Downstream = downstream
	default:
		return Intent{}, ErrUnrecognized
	}
	return intent, nil
}

// NormalizeVersion 接受 Vn 或 vn，拒绝空版本和 V0。
func NormalizeVersion(v string) (string, error) {
	v = strings.TrimSpace(v)
	if len(v) < 2 || (v[0] != 'V' && v[0] != 'v') {
		return "", ErrExplicitVersion
	}
	n := v[1:]
	if n == "" || n[0] == '0' {
		return "", ErrExplicitVersion
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return "", ErrExplicitVersion
		}
	}
	return "V" + n, nil
}

// KnownDownstream 只接受三个生态动作。
func KnownDownstream(target string) bool {
	return target == DownstreamDigitalHuman || target == DownstreamAICut || target == DownstreamMatrix
}

// SameIntent 忽略通道和原文，只比较会执行的字段。
func SameIntent(a, b Intent) bool {
	if a.Schema != b.Schema || a.Action != b.Action || a.PreserveSubject != b.PreserveSubject ||
		a.Region != b.Region || a.TargetVersion != b.TargetVersion || a.Downstream != b.Downstream ||
		len(a.Locks) != len(b.Locks) {
		return false
	}
	for i := range a.Locks {
		if a.Locks[i] != b.Locks[i] {
			return false
		}
	}
	return true
}

func pixelAction(action string) bool {
	return action == ActionSimplifyBackground || action == ActionOutdoor || action == ActionBrightenKeepProduct
}
