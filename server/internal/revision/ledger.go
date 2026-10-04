package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	StatusCandidate = "candidate"
	StatusFailed    = "failed"
	StatusUnknown   = "unknown"
)

// Version 是不可变的一版结果。采用与否由账本指针决定。
type Version struct {
	Label                string   `json:"label"`
	No                   int      `json:"no"`
	Outcome              string   `json:"outcome"`
	BriefVersion         string   `json:"brief_version,omitempty"`
	ContentHash          string   `json:"content_hash"`
	AssetRef             string   `json:"asset_ref,omitempty"`
	Origin               string   `json:"origin,omitempty"`
	IncrementalCostCents int64    `json:"incremental_cost_cents"`
	ParentLabel          string   `json:"parent_label,omitempty"`
	Action               string   `json:"action,omitempty"`
	Locks                []string `json:"locks,omitempty"`
	Region               string   `json:"region,omitempty"`
	TargetVersion        string   `json:"target_version,omitempty"`
	Downstream           string   `json:"downstream,omitempty"`
	PNG                  []byte   `json:"png,omitempty"`
}

// Ledger 保存版本和采用指针。失败和未知只追加，不移动指针。
type Ledger struct {
	Versions            []Version
	AdoptedID           string
	BriefVersion        string
	BriefFacts          map[string]string
	PendingBriefVersion string
	PendingFacts        map[string]string
}

// Snapshot 是可持久化的账本副本。
type Snapshot struct {
	Versions            []Version
	AdoptedID           string
	BriefVersion        string
	BriefFacts          map[string]string
	PendingBriefVersion string
	PendingFacts        map[string]string
}

// BriefDiff 只描述事实键的差异，不替换已采用像素。
type BriefDiff struct {
	OldVersion         string   `json:"old_version"`
	NewVersion         string   `json:"new_version"`
	Added              []string `json:"added"`
	Removed            []string `json:"removed"`
	Changed            []string `json:"changed"`
	AdoptedVersionID   string   `json:"adopted_version_id"`
	AdoptedContentHash string   `json:"adopted_content_hash"`
	AdoptedUnchanged   bool     `json:"adopted_unchanged"`
}

// Comparison 是两版的只读对照。
type Comparison struct {
	Left  Version `json:"left"`
	Right Version `json:"right"`
}

// ExportRef 绑定一个明确版本，不要求重新上传。
type ExportRef struct {
	VersionID        string `json:"version_id"`
	ContentHash      string `json:"content_hash"`
	AssetRef         string `json:"asset_ref"`
	RequiresReupload bool   `json:"requires_reupload"`
}

// DownstreamRef 把同一版本引用交给下一个生态动作。
type DownstreamRef struct {
	Target           string `json:"target"`
	VersionID        string `json:"version_id"`
	ContentHash      string `json:"content_hash"`
	AssetRef         string `json:"asset_ref"`
	RequiresReupload bool   `json:"requires_reupload"`
}

// NewLedger 返回空账本。
func NewLedger() *Ledger {
	return &Ledger{BriefFacts: map[string]string{}, PendingFacts: map[string]string{}}
}

// PutCandidate 追加一版有像素的候选，不改采用指针。
func (l *Ledger) PutCandidate(png []byte, intent Intent, origin, brief string) (Version, error) {
	if len(png) == 0 {
		return Version{}, ErrBadImage
	}
	cost, err := IncrementalCostCents(intent)
	if err != nil {
		return Version{}, err
	}
	sum := sha256.Sum256(png)
	hash := hex.EncodeToString(sum[:])
	no := len(l.Versions) + 1
	label := fmt.Sprintf("V%d", no)
	v := Version{
		Label: label, No: no, Outcome: StatusCandidate, BriefVersion: brief,
		ContentHash: hash, AssetRef: AssetRef(label, hash), Origin: origin,
		IncrementalCostCents: cost, ParentLabel: l.AdoptedID, Action: intent.Action,
		Locks: append([]string{}, intent.Locks...), Region: intent.Region,
		TargetVersion: intent.TargetVersion, Downstream: intent.Downstream,
		PNG: append([]byte{}, png...),
	}
	l.Versions = append(l.Versions, v)
	return v, nil
}

// RecordUnresolved 追加 failed 或 unknown，已采用版本保持不动。
func (l *Ledger) RecordUnresolved(status string, intent Intent) (Version, error) {
	if status != StatusFailed && status != StatusUnknown {
		return Version{}, ErrUnrecognized
	}
	no := len(l.Versions) + 1
	label := fmt.Sprintf("V%d", no)
	raw := "unresolved:" + status + ":" + label + ":" + intent.Action
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	v := Version{
		Label: label, No: no, Outcome: status, ContentHash: hash, Action: intent.Action,
		Locks: append([]string{}, intent.Locks...), IncrementalCostCents: 0,
	}
	l.Versions = append(l.Versions, v)
	return v, nil
}

// Adopt 把指针移到有像素的候选。失败和未知不能被采用。
func (l *Ledger) Adopt(label string) error {
	v, ok := l.find(label)
	if !ok {
		return ErrExplicitVersion
	}
	if v.Outcome != StatusCandidate || len(v.PNG) == 0 {
		return ErrNotExportable
	}
	l.AdoptedID = v.Label
	return nil
}

// Rollback 把采用指针移回仍在册的有像素版本。
func (l *Ledger) Rollback(label string) error {
	return l.Adopt(label)
}

// AdoptedHash 是当前采用版本的内容哈希。没有采用版本时为空。
func (l *Ledger) AdoptedHash() string {
	v, ok := l.find(l.AdoptedID)
	if !ok {
		return ""
	}
	return v.ContentHash
}

// SideBySide 只读两版，不移动采用指针。
func (l *Ledger) SideBySide(a, b string) (Comparison, error) {
	left, ok := l.find(a)
	if !ok {
		return Comparison{}, ErrExplicitVersion
	}
	right, ok := l.find(b)
	if !ok {
		return Comparison{}, ErrExplicitVersion
	}
	left.PNG = nil
	right.PNG = nil
	return Comparison{Left: left, Right: right}, nil
}

// ProposeBrief 计算差异并记下待显示的 Brief。不改已采用像素。
func (l *Ledger) ProposeBrief(version string, facts map[string]string) (BriefDiff, error) {
	version = strings.TrimSpace(version)
	if version == "" || facts == nil {
		return BriefDiff{}, ErrBadBrief
	}
	for k, val := range facts {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k+val, "\r\n") {
			return BriefDiff{}, ErrBadBrief
		}
	}
	old := l.BriefFacts
	diff := BriefDiff{
		OldVersion: l.BriefVersion, NewVersion: version,
		Added: []string{}, Removed: []string{}, Changed: []string{},
		AdoptedVersionID: l.AdoptedID, AdoptedContentHash: l.AdoptedHash(),
		AdoptedUnchanged: true,
	}
	keys := map[string]struct{}{}
	for k := range old {
		keys[k] = struct{}{}
	}
	for k := range facts {
		keys[k] = struct{}{}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		ov, ook := old[k]
		nv, nok := facts[k]
		switch {
		case ook && nok && ov != nv:
			diff.Changed = append(diff.Changed, k)
		case ook && !nok:
			diff.Removed = append(diff.Removed, k)
		case !ook && nok:
			diff.Added = append(diff.Added, k)
		}
	}
	copied := map[string]string{}
	for k, v := range facts {
		copied[k] = v
	}
	l.PendingBriefVersion = version
	l.PendingFacts = copied
	l.BriefVersion = version
	l.BriefFacts = copied
	return diff, nil
}

// Export 要求点名一个有像素的版本。
func (l *Ledger) Export(label string) (ExportRef, error) {
	if strings.TrimSpace(label) == "" {
		return ExportRef{}, ErrExplicitVersion
	}
	v, ok := l.find(label)
	if !ok {
		return ExportRef{}, ErrExplicitVersion
	}
	if v.Outcome != StatusCandidate || len(v.PNG) == 0 || v.AssetRef == "" {
		return ExportRef{}, ErrNotExportable
	}
	return ExportRef{
		VersionID: v.Label, ContentHash: v.ContentHash, AssetRef: v.AssetRef, RequiresReupload: false,
	}, nil
}

// Downstream 把导出引用交给数字人、AiCut 或矩阵。不上传文件。
func (l *Ledger) Downstream(label, target string) (DownstreamRef, error) {
	if !KnownDownstream(target) {
		return DownstreamRef{}, ErrUnknownDownstream
	}
	exp, err := l.Export(label)
	if err != nil {
		return DownstreamRef{}, err
	}
	return DownstreamRef{
		Target: target, VersionID: exp.VersionID, ContentHash: exp.ContentHash,
		AssetRef: exp.AssetRef, RequiresReupload: false,
	}, nil
}

// Snapshot 复制账本，供关闭后重新打开。
func (l *Ledger) Snapshot() Snapshot {
	versions := make([]Version, len(l.Versions))
	for i, v := range l.Versions {
		v.PNG = append([]byte{}, v.PNG...)
		v.Locks = append([]string{}, v.Locks...)
		versions[i] = v
	}
	return Snapshot{
		Versions: versions, AdoptedID: l.AdoptedID,
		BriefVersion: l.BriefVersion, BriefFacts: copyFacts(l.BriefFacts),
		PendingBriefVersion: l.PendingBriefVersion, PendingFacts: copyFacts(l.PendingFacts),
	}
}

// Restore 核对候选像素哈希后恢复指针。
func Restore(s Snapshot) (*Ledger, error) {
	led := NewLedger()
	seen := map[string]bool{}
	for _, v := range s.Versions {
		if v.Label == "" || seen[v.Label] {
			return nil, ErrExplicitVersion
		}
		seen[v.Label] = true
		if v.Outcome == StatusCandidate && len(v.PNG) > 0 {
			sum := sha256.Sum256(v.PNG)
			if hex.EncodeToString(sum[:]) != v.ContentHash {
				return nil, ErrBadImage
			}
		}
		v.PNG = append([]byte{}, v.PNG...)
		v.Locks = append([]string{}, v.Locks...)
		led.Versions = append(led.Versions, v)
	}
	if s.AdoptedID != "" {
		if _, ok := led.find(s.AdoptedID); !ok {
			return nil, ErrExplicitVersion
		}
	}
	led.AdoptedID = s.AdoptedID
	led.BriefVersion = s.BriefVersion
	led.BriefFacts = copyFacts(s.BriefFacts)
	led.PendingBriefVersion = s.PendingBriefVersion
	led.PendingFacts = copyFacts(s.PendingFacts)
	return led, nil
}

// AssetRef 是版本引用，不是新上传的文件编号。
func AssetRef(label, hash string) string {
	prefix := hash
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return "revision-version/" + label + "@" + prefix
}

func (l *Ledger) find(label string) (Version, bool) {
	for _, v := range l.Versions {
		if v.Label == label {
			return v, true
		}
	}
	return Version{}, false
}

func copyFacts(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
