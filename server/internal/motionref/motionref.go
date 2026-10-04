// Package motionref 把已采用的商品资产声明成 Motion 引用。
// 本包不生成视频、不复制 Motion 编辑器、不调用供应商。
// 未核验的引用不是动态广告。HUI-2732 未完成，调用方报上的执行次数不是证明。
package motionref

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
)

const (
	Schema = "product-motion-ref/v1"
	// NotFinishedCopy 必须出现在引用上。未生成成片时不能写成动态广告。
	NotFinishedCopy = "还没生成成片。这条只是已采用商品的 Motion 引用声明，不是动态广告。"
)

var (
	ErrNotAdopted    = errors.New("motionref: product asset is not adopted")
	ErrBadAsset      = errors.New("motionref: adopted asset is incomplete")
	ErrBadParameter  = errors.New("motionref: parameter is not price or color")
	ErrUnverifiedAd  = errors.New("motionref: unverified reference is not a dynamic ad")
	ErrBatchUnproven = errors.New("motionref: other variant execution counts are not proven")
	ErrDrift         = errors.New("motionref: subject hash drifted")
	ErrDigest        = errors.New("motionref: digest is not sha256")

	contentHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	digestRe      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// AdoptedAsset 是已经采用的商品资产。未采用的不能形成引用。
type AdoptedAsset struct {
	AssetID     string
	ContentHash string
	RevisionID  string
	Adopted     bool
}

// Parameter 只记录价格或颜色，不触发商品重生成。
type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Declaration 是一条 Motion 引用声明，不是成片。
type Declaration struct {
	ID                  string      `json:"id,omitempty"`
	Schema              string      `json:"schema"`
	AssetID             string      `json:"asset_id"`
	ContentHash         string      `json:"content_hash"`
	RevisionID          string      `json:"revision_id"`
	Digest              string      `json:"digest"`
	SubjectHash         string      `json:"subject_hash"`
	Parameters          []Parameter `json:"parameters"`
	ProductRegeneration bool        `json:"product_regeneration"`
	ModelCallCount      int         `json:"model_call_count"`
	VideoGenerated      bool        `json:"video_generated"`
	SupplierCalled      bool        `json:"supplier_called"`
	Verified            bool        `json:"verified"`
	DynamicAd           bool        `json:"dynamic_ad"`
	FinishedCopy        string      `json:"finished_copy"`
	MotionEditorCopied  bool        `json:"motion_editor_copied"`
	BatchRerun          bool        `json:"batch_rerun"`
}

// VariantExecution 是调用方声称的其他变体执行次数。它本身不是 HUI-2732 的证明。
type VariantExecution struct {
	ID             string
	ExecutionCount *int
}

// Digest 绑定资产、内容哈希和修订。参数不参与，所以改价格或颜色不会改它。
func Digest(assetID, contentHash, revisionID string) string {
	raw := strings.Join([]string{Schema, assetID, contentHash, revisionID}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Declare 只从已采用资产形成引用。不生成视频，模型调用数为 0。
func Declare(asset AdoptedAsset) (Declaration, error) {
	if !asset.Adopted {
		return Declaration{}, ErrNotAdopted
	}
	asset.AssetID = strings.TrimSpace(asset.AssetID)
	asset.RevisionID = strings.TrimSpace(asset.RevisionID)
	asset.ContentHash = strings.TrimSpace(asset.ContentHash)
	if asset.AssetID == "" || asset.RevisionID == "" || !contentHashRe.MatchString(asset.ContentHash) {
		return Declaration{}, ErrBadAsset
	}
	if strings.ContainsAny(asset.AssetID+asset.RevisionID, "\r\n") {
		return Declaration{}, ErrBadAsset
	}
	d := Declaration{
		Schema: Schema, AssetID: asset.AssetID, ContentHash: asset.ContentHash,
		RevisionID: asset.RevisionID, Digest: Digest(asset.AssetID, asset.ContentHash, asset.RevisionID),
		SubjectHash: asset.ContentHash, Parameters: []Parameter{}, FinishedCopy: NotFinishedCopy,
	}
	if err := d.Intact(); err != nil {
		return Declaration{}, err
	}
	return d, nil
}

// RecordParameter 只记下价格或颜色。主体哈希、摘要和商品重生成标志都不变。
func RecordParameter(d Declaration, name, value string) (Declaration, error) {
	if err := d.Intact(); err != nil {
		return d, err
	}
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name != "price" && name != "color" {
		return d, ErrBadParameter
	}
	if value == "" || strings.ContainsAny(value, "\r\n") || len([]rune(value)) > 80 {
		return d, ErrBadParameter
	}
	params := append([]Parameter(nil), d.Parameters...)
	replaced := false
	for i := range params {
		if params[i].Name == name {
			params[i].Value = value
			replaced = true
		}
	}
	if !replaced {
		params = append(params, Parameter{Name: name, Value: value})
	}
	sort.Slice(params, func(i, j int) bool { return params[i].Name < params[j].Name })
	next := d
	next.Parameters = params
	next.ProductRegeneration = false
	next.ModelCallCount = 0
	next.VideoGenerated = false
	next.SupplierCalled = false
	next.Verified = false
	next.DynamicAd = false
	next.MotionEditorCopied = false
	next.BatchRerun = false
	next.SubjectHash = d.ContentHash
	next.FinishedCopy = NotFinishedCopy
	if err := next.Intact(); err != nil {
		return d, err
	}
	return next, nil
}

// AsDynamicAd 拒绝把引用说成动态广告。本片没有核验，因此没有成功路径。
func AsDynamicAd(d Declaration) error {
	if err := d.Intact(); err != nil {
		return ErrUnverifiedAd
	}
	return ErrUnverifiedAd
}

// BatchRerun 一律拒绝。HUI-2732 未完成，报上来的 0 也不能证明其他变体执行次数为 0。
func BatchRerun(others []VariantExecution) error {
	for _, v := range others {
		if v.ExecutionCount == nil || *v.ExecutionCount != 0 {
			return ErrBatchUnproven
		}
	}
	return ErrBatchUnproven
}

// Intact 核对引用仍是未核验声明，且主体哈希没有被改写。
func (d Declaration) Intact() error {
	if d.Schema != Schema || d.AssetID == "" || d.RevisionID == "" {
		return ErrBadAsset
	}
	if d.ID != "" && !strings.HasPrefix(d.ID, "mref_") {
		return ErrBadAsset
	}
	if !contentHashRe.MatchString(d.ContentHash) || d.SubjectHash != d.ContentHash {
		return ErrDrift
	}
	if d.Digest != Digest(d.AssetID, d.ContentHash, d.RevisionID) || !digestRe.MatchString(d.Digest) {
		return ErrDigest
	}
	if d.ProductRegeneration || d.ModelCallCount != 0 || d.VideoGenerated || d.SupplierCalled ||
		d.Verified || d.DynamicAd || d.MotionEditorCopied || d.BatchRerun {
		return ErrUnverifiedAd
	}
	if d.FinishedCopy != NotFinishedCopy || !strings.Contains(d.FinishedCopy, "还没生成成片") {
		return ErrUnverifiedAd
	}
	seen := map[string]bool{}
	for _, p := range d.Parameters {
		if (p.Name != "price" && p.Name != "color") || seen[p.Name] || strings.TrimSpace(p.Value) == "" {
			return ErrBadParameter
		}
		if strings.ContainsAny(p.Value, "\r\n") {
			return ErrBadParameter
		}
		seen[p.Name] = true
	}
	return nil
}
