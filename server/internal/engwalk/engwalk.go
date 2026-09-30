// Package engwalk 对着 HTTP 提供者走查产品图工程门。
// 合同图来自一次 POST 的响应字节。没有成对的模型 URL 时不拨号。
// 商业状态在真实供应商未通过时保持「真实出图未完成」。
package engwalk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
	"github.com/bianjiefilm/product-image-engine/server/internal/textimage"
)

const (
	StatusIncomplete = "真实出图未完成"
	StatusLiveImage  = "供应商图片已解码"
	ModeTextImage    = "text_image"

	OutcomeStored  = "stored"
	OutcomeFailed  = "failed"
	OutcomeUnknown = "unknown"

	fixtureAmount = "1.00"
)

var ErrSubmitIncomplete = errors.New("提交不完整")

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

var contractClient = &http.Client{Timeout: 5 * time.Second}

// Submit 是一次走查提交。相似度不能改写像素轴。
type Submit struct {
	Fingerprint string
	Mode        string
	Endpoint    string
	Similarity  float64
}

// Money 把一笔金额和它的标签分开。夹具成本不是用户价，也不是补贴。
type Money struct {
	Label  string `json:"label"`
	Amount string `json:"amount"`
}

// ChargeLine 是一条记账。三个金额字段始终分开。
type ChargeLine struct {
	Fingerprint  string `json:"fingerprint"`
	SupplierCost Money  `json:"supplier_cost"`
	UserPrice    Money  `json:"user_price"`
	Subsidy      Money  `json:"subsidy"`
}

// Axes 是从像素读出的四个轴。
type Axes struct {
	Logo          string
	PackagingText string
	Spec          string
	Structure     string
}

// Layer 是六层里的一层。
type Layer struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Note   string `json:"note"`
}

// Result 是一次提交的走查结果。
type Result struct {
	Status                     string          `json:"status"`
	ContractPassed             bool            `json:"contract_passed"`
	LivePassed                 bool            `json:"live_passed"`
	ProductionGenerationPassed bool            `json:"production_generation_passed"`
	BillingPassed              bool            `json:"billing_passed"`
	Generation                 bool            `json:"generation"`
	Outcome                    string          `json:"outcome"`
	FailureClass               string          `json:"failure_class"`
	Mode                       string          `json:"mode"`
	Asset                      []byte          `json:"-"`
	SHA256                     string          `json:"sha256"`
	ByteLen                    int             `json:"byte_len"`
	ProviderCalls              int             `json:"provider_calls"`
	ChargeCount                int             `json:"charge_count"`
	Charges                    []ChargeLine    `json:"charges"`
	Intact                     fidelity.Report `json:"intact"`
	Broken                     fidelity.Report `json:"broken"`
	IntactAxes                 Axes            `json:"intact_axes"`
	BrokenAxes                 Axes            `json:"broken_axes"`
	Similarity                 float64         `json:"similarity"`
	Export                     map[string]any  `json:"export"`
	Layers                     []Layer         `json:"layers"`
	Live                       LiveFact        `json:"live"`
	TaskCredible               bool            `json:"task_credible"`
}

// Gate 按指纹保存第一次提交。
type Gate struct {
	mu   sync.Mutex
	jobs map[string]Result
}

// NewGate 返回空账本。
func NewGate() *Gate {
	return &Gate{jobs: map[string]Result{}}
}

// Submit 走查一次。同一指纹的再次提交不再请求提供者。
func (g *Gate) Submit(ctx context.Context, in Submit) (Result, error) {
	fp := strings.TrimSpace(in.Fingerprint)
	mode := strings.TrimSpace(in.Mode)
	endpoint := strings.TrimSpace(in.Endpoint)
	if fp == "" || mode == "" || endpoint == "" {
		return Result{}, ErrSubmitIncomplete
	}
	in.Fingerprint = fp
	in.Mode = mode
	in.Endpoint = endpoint
	g.mu.Lock()
	defer g.mu.Unlock()
	if existing, ok := g.jobs[fp]; ok {
		return cloneResult(existing), nil
	}
	res := evaluate(ctx, in)
	g.jobs[fp] = cloneResult(res)
	return cloneResult(res), nil
}

func evaluate(ctx context.Context, in Submit) Result {
	live := liveFromEnv()
	genPassed, billPassed := productFlags(live.Passed)
	res := Result{
		Status:                     commercialStatus(live.Passed),
		LivePassed:                 live.Passed,
		ProductionGenerationPassed: genPassed,
		BillingPassed:              billPassed,
		Similarity:                 in.Similarity,
		Live:                       live,
		Mode:                       in.Mode,
	}
	if in.Mode != ModeTextImage {
		res.Outcome = OutcomeUnknown
		res.Layers = layersOf(res)
		return res
	}
	status, body, err := postTask(ctx, in.Endpoint, in.Mode, in.Fingerprint)
	if err != nil {
		res.Outcome = OutcomeFailed
		res.FailureClass = "transport"
		res.Layers = layersOf(res)
		return res
	}
	res.ProviderCalls = 1
	if status != http.StatusOK {
		res.Outcome = OutcomeFailed
		res.FailureClass = httpFailure(status)
		res.Layers = layersOf(res)
		return res
	}
	if len(body) == 0 {
		res.Outcome = OutcomeFailed
		res.FailureClass = "empty_body"
		res.Layers = layersOf(res)
		return res
	}
	img, err := openPNG(body)
	if err != nil {
		res.Outcome = OutcomeUnknown
		res.FailureClass = "unknown"
		res.Layers = layersOf(res)
		return res
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	res.Asset = append([]byte(nil), body...)
	res.SHA256 = sha
	res.ByteLen = len(body)
	res.Outcome = OutcomeStored
	res.Generation = true
	res.TaskCredible = true
	res.IntactAxes = observe(img)
	brokenImg := stripMarker(img)
	res.BrokenAxes = observe(brokenImg)
	intact, err := evalCase(res.IntactAxes, img, in.Similarity, in.Fingerprint, sha)
	if err != nil {
		return rejectStored(res)
	}
	broken, err := evalCase(res.BrokenAxes, brokenImg, in.Similarity, in.Fingerprint, sha+":marker-cleared")
	if err != nil {
		return rejectStored(res)
	}
	res.Intact = intact
	res.Broken = broken
	doc := intact.ExportDocument()
	doc["sha256"] = sha
	doc["byte_len"] = len(res.Asset)
	doc["openable"] = true
	res.Export = doc
	res.Charges = []ChargeLine{fixtureLine(in.Fingerprint)}
	res.ChargeCount = 1
	res.ContractPassed = len(res.Asset) > 0 && res.ByteLen == len(res.Asset) && res.SHA256 != "" && brokenRefused(res)
	res.Layers = layersOf(res)
	return res
}

func rejectStored(res Result) Result {
	res.Outcome = OutcomeUnknown
	res.FailureClass = "unknown"
	res.Generation = false
	res.TaskCredible = false
	res.ContractPassed = false
	res.Asset = nil
	res.SHA256 = ""
	res.ByteLen = 0
	res.Export = nil
	res.Charges = nil
	res.ChargeCount = 0
	res.Layers = layersOf(res)
	return res
}

func productFlags(livePassed bool) (bool, bool) {
	if textimage.RealGenerationCompleted(livePassed, textimage.OriginServer) {
		return false, false
	}
	return false, false
}

func commercialStatus(livePassed bool) string {
	if livePassed {
		return StatusLiveImage
	}
	return StatusIncomplete
}

func postTask(ctx context.Context, endpoint, mode, fingerprint string) (int, []byte, error) {
	payload, err := json.Marshal(map[string]string{
		"mode":        mode,
		"fingerprint": fingerprint,
	})
	if err != nil {
		return 0, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := contractClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func httpFailure(status int) string {
	return "http_" + strconv.Itoa(status)
}

func openPNG(body []byte) (image.Image, error) {
	if len(body) < len(pngSignature) || !bytes.Equal(body[:len(pngSignature)], pngSignature) {
		return nil, errors.New("not png")
	}
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func observe(img image.Image) Axes {
	return Axes{
		Logo:          axisAt(img, 0, 0, color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}),
		PackagingText: axisAt(img, 1, 0, color.NRGBA{R: 0x44, G: 0x55, B: 0x66, A: 0xFF}),
		Spec:          axisAt(img, 2, 0, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xFF}),
		Structure:     axisAt(img, 3, 0, color.NRGBA{R: 0x01, G: 0x02, B: 0x03, A: 0xFF}),
	}
}

func axisAt(img image.Image, x, y int, want color.NRGBA) string {
	if img == nil || !image.Pt(x, y).In(img.Bounds()) {
		return fidelity.ResultFail
	}
	if nrgbaAt(img, x, y) != want {
		return fidelity.ResultFail
	}
	return fidelity.ResultPass
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	return c
}

func stripMarker(src image.Image) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	dst.SetNRGBA(0, 0, color.NRGBA{})
	return dst
}

func evalCase(axes Axes, img image.Image, similarity float64, fingerprint, outputRef string) (fidelity.Report, error) {
	logo := fidelity.ResultFail
	if axes.Logo == fidelity.ResultPass && axes.PackagingText == fidelity.ResultPass {
		logo = fidelity.ResultPass
	}
	return fidelity.Evaluate(fidelity.Input{
		Mode:               fidelity.ModeFidelity,
		MethodVersion:      fidelity.MethodVersionV1,
		InputRef:           fingerprint,
		InputVersion:       "1",
		OutputRef:          outputRef,
		MaskRef:            "measured-pixels",
		AllowedRegions:     []string{"background"},
		Protected:          append([]string(nil), fidelity.RequiredProtected...),
		SampleClass:        fidelity.SampleLogoText,
		MachineScope:       "像素轴",
		HumanScope:         "Human UNKNOWN",
		SupplierAuthorized: false,
		SimilarityOnly:     similarity > 0,
		Checks: []fidelity.Check{
			{Name: fidelity.CheckProductCount, Result: productCount(img)},
			{Name: fidelity.CheckLogoText, Result: logo},
			{Name: fidelity.CheckShape, Result: axes.Structure},
			{Name: fidelity.CheckKeyTexture, Result: keyTexture(img)},
			{Name: fidelity.CheckDeclaredColor, Result: axes.Spec},
		},
	})
}

func productCount(img image.Image) string {
	if img == nil || img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		return fidelity.ResultFail
	}
	return fidelity.ResultPass
}

func keyTexture(img image.Image) string {
	if img == nil {
		return fidelity.ResultFail
	}
	b := img.Bounds()
	var first color.NRGBA
	seen := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := nrgbaAt(img, x, y)
			if seen == 0 {
				first = c
				seen = 1
				continue
			}
			if c != first {
				return fidelity.ResultPass
			}
		}
	}
	return fidelity.ResultFail
}

func brokenRefused(res Result) bool {
	if res.BrokenAxes.Logo != fidelity.ResultFail || logoCheck(res.Broken) != fidelity.ResultFail {
		return false
	}
	if res.Broken.Verdict == fidelity.VerdictPass || res.Broken.ExactProduct || res.Broken.Deliverable {
		return false
	}
	blocked, code, _ := fidelity.BlocksVerifiedReceipt([]fidelity.Report{res.Broken})
	return blocked && code == "fidelity_failed"
}

func logoCheck(rep fidelity.Report) string {
	for _, c := range rep.Checks {
		if c.Name == fidelity.CheckLogoText {
			return c.Result
		}
	}
	return ""
}

func fixtureLine(fingerprint string) ChargeLine {
	return ChargeLine{
		Fingerprint:  fingerprint,
		SupplierCost: Money{Label: "fixture", Amount: fixtureAmount},
		UserPrice:    Money{Label: "user_price"},
		Subsidy:      Money{Label: "subsidy"},
	}
}

func layersOf(res Result) []Layer {
	assetOK := res.Outcome == OutcomeStored && len(res.Asset) > 0 && res.ByteLen == len(res.Asset)
	hashOK := assetOK && hashMatches(res.Asset, res.SHA256)
	exportOK := assetOK && res.Export != nil && res.Export["sha256"] == res.SHA256 && res.Export["openable"] == true && res.Export["byte_len"] == len(res.Asset)
	taskOK := assetOK && res.TaskCredible && res.Mode == ModeTextImage
	fidelityOK := assetOK && brokenRefused(res)
	return []Layer{
		{Name: "task", Passed: taskOK, Note: "受支持模式到达可核验任务"},
		{Name: "asset", Passed: assetOK, Note: "存下的字节可以按 PNG 打开"},
		{Name: "hash", Passed: hashOK, Note: "摘要等于存下的字节"},
		{Name: "export", Passed: exportOK, Note: "导出带同一摘要且可打开"},
		{Name: "fidelity", Passed: fidelityOK, Note: "Logo 失败不能被相似度写成已验证回执"},
		{Name: "charge", Passed: chargeHolds(res), Note: "供应商夹具成本、用户价、补贴分开，且不重复记账"},
	}
}

func hashMatches(asset []byte, sha string) bool {
	if len(asset) == 0 || sha == "" {
		return false
	}
	sum := sha256.Sum256(asset)
	return hex.EncodeToString(sum[:]) == sha
}

func chargeHolds(res Result) bool {
	if res.Outcome != OutcomeStored {
		return res.ChargeCount == 0 && len(res.Charges) == 0
	}
	if res.ChargeCount != 1 || len(res.Charges) != 1 {
		return false
	}
	line := res.Charges[0]
	if line.SupplierCost.Label != "fixture" || line.SupplierCost.Amount != fixtureAmount {
		return false
	}
	if line.UserPrice.Label != "user_price" || line.UserPrice.Amount == line.SupplierCost.Amount || line.UserPrice.Label == "fixture" {
		return false
	}
	if line.Subsidy.Label != "subsidy" || line.Subsidy.Amount == line.SupplierCost.Amount || line.Subsidy.Label == "fixture" {
		return false
	}
	return true
}

func cloneResult(in Result) Result {
	out := in
	if in.Asset != nil {
		out.Asset = append([]byte(nil), in.Asset...)
	}
	if in.Charges != nil {
		out.Charges = append([]ChargeLine(nil), in.Charges...)
	}
	if in.Layers != nil {
		out.Layers = append([]Layer(nil), in.Layers...)
	}
	if in.Export != nil {
		cp := make(map[string]any, len(in.Export))
		for k, v := range in.Export {
			cp[k] = v
		}
		out.Export = cp
	}
	out.Intact.Checks = append([]fidelity.Check(nil), in.Intact.Checks...)
	out.Broken.Checks = append([]fidelity.Check(nil), in.Broken.Checks...)
	out.Intact.Reasons = append([]string(nil), in.Intact.Reasons...)
	out.Broken.Reasons = append([]string(nil), in.Broken.Reasons...)
	out.Intact.Limits = append([]string(nil), in.Intact.Limits...)
	out.Broken.Limits = append([]string(nil), in.Broken.Limits...)
	out.Intact.Protected = append([]string(nil), in.Intact.Protected...)
	out.Broken.Protected = append([]string(nil), in.Broken.Protected...)
	out.Intact.AllowedRegions = append([]string(nil), in.Intact.AllowedRegions...)
	out.Broken.AllowedRegions = append([]string(nil), in.Broken.AllowedRegions...)
	return out
}
