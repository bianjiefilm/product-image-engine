package platelock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"

	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

const (
	ReportVersion = "product-source-quality/v2"
	Claim         = fs.Scope

	Pass, Fail, Unknown, NA = "PASS", "FAIL", "UNKNOWN", "N/A"

	CandidateLimited   = "limited_candidate"
	CandidatePending   = "pending"
	CandidateNotUsable = "not_usable_candidate"

	// bg-lock-thresholds/v1. They may only be tightened, with evidence, and
	// every change needs a new fs.ThresholdsVersion.
	ChangedChannelDelta     = 16  // a pixel counts as changed at >= this per-channel difference
	MinChangedPermille      = 800 // >= 80.0% of background pixels must be changed
	MinMeanAbsDiffPerChanel = 12  // mean absolute RGB difference over the background
	minComponentArea        = 64
)

// Machine-checked axes. Every one must be PASS for a limited candidate.
const (
	AxisMaskPixels   = "mask_pixels_identical"
	AxisLogo         = "coverage_logo"
	AxisText         = "coverage_packaging_text"
	AxisSpec         = "coverage_spec"
	AxisStructure    = "coverage_structure"
	AxisProductCount = "product_count"
	AxisOutputSize   = "output_size"
	AxisPlateSize    = "plate_size"
	AxisNewPlate     = "plate_is_new_background"
	AxisBackground   = "background_changed"
	AxisTask         = "plate_task_succeeded"
	AxisAssetHash    = "plate_asset_hash_verified"
	AxisCharged      = "plate_charged_observed"
)

// Limit axes cannot be machine checked: they are only ever UNKNOWN or N/A.
const (
	AxisExtraObjects = "background_extra_objects"
	AxisIntent       = "background_intent_followed"
	AxisVisual       = "visual_composite_quality"
	AxisTransmission = "transmission_edge"
)

var subjectAxes = []string{AxisMaskPixels, AxisLogo, AxisText, AxisSpec, AxisStructure, AxisProductCount}
var requiredAxes = append(append([]string{}, subjectAxes...), AxisOutputSize, AxisPlateSize, AxisNewPlate, AxisBackground, AxisTask, AxisAssetHash, AxisCharged)
var limitAxes = []string{AxisExtraObjects, AxisIntent, AxisVisual, AxisTransmission}

type Axis struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	MachineChecked bool   `json:"machine_checked"`
	Evidence       string `json:"evidence"`
}
type Scope struct {
	Mode           string `json:"mode"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	PricingVersion string `json:"pricing_version"`
	SampleSetID    string `json:"sample_set_id"`
	CaseID         string `json:"case_id"`
	Canvas         string `json:"canvas"`
	MaterialClass  string `json:"material_class"`
	Claim          string `json:"claim"`
}
type Verdicts struct {
	Fidelity        string `json:"fidelity"`
	Generation      string `json:"generation"`
	Billing         string `json:"billing"`
	HumanUsefulness string `json:"human_usefulness"`
}
type PlateFacts struct {
	AssetID     string `json:"asset_id"`
	ReferenceID string `json:"reference_id"`
	SHA256      string `json:"sha256"`
}
type ReportFacts struct {
	UsageID          string     `json:"usage_id"`
	QuoteID          string     `json:"quote_id"`
	TaskID           string     `json:"task_id"`
	OriginalChargeID string     `json:"original_charge_id"`
	Plate            PlateFacts `json:"plate"`
	OriginalSHA256   string     `json:"original_sha256"`
	MaskSHA256       string     `json:"mask_sha256"`
	CoverageSHA256   string     `json:"coverage_sha256"`
	CompositeSHA256  string     `json:"composite_sha256"`
	FitID            string     `json:"fit_id"`
	ComposeID        string     `json:"compose_id"`
	ThresholdsVer    string     `json:"thresholds_version"`
}
type Build struct {
	VCSRevision string `json:"vcs_revision"`
	VCSModified bool   `json:"vcs_modified"`
}

// Review is an optional pointer to a SIMULATED-HUMAN review. It is carried for
// reference only and never read by StateOf.
type Review struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256"`
}
type Report struct {
	ReportVersion  string      `json:"report_version"`
	Scope          Scope       `json:"scope"`
	Axes           []Axis      `json:"axes"`
	Verdicts       Verdicts    `json:"verdicts"`
	CandidateState string      `json:"candidate_state"`
	Limits         []string    `json:"limits"`
	Facts          ReportFacts `json:"facts"`
	Build          Build       `json:"build"`
	Review         *Review     `json:"review,omitempty"`
}

// Input holds only server-held facts. There is deliberately no field that can
// carry a score, a browser boolean, or a supplier authorization.
type Input struct {
	Case           fs.Case
	Original       []byte // frozen original from the loaded set
	Mask           []byte // frozen mask from the loaded set
	CoverageSHA256 string // evidence digest from fidelitysamples.LoadedCase
	Plate          []byte // model plate bytes downloaded from the verified Upload asset
	Facts          Facts
	Build          Build
}

// Facts are the run facts the derivation trigger already established.
type Facts struct {
	Mode, Provider, Model, PricingVersion        string
	UsageID, QuoteID, TaskID, OriginalChargeID   string
	PlateAssetID, PlateReferenceID, PlateSHA256  string
	TaskSucceeded, PlateHashVerified, ChargeSeen bool
}

func sha(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func compositeSHA(raw []byte) string {
	if raw == nil {
		return ""
	}
	return sha(raw)
}

func axis(name, state string, machine bool, evidence string) Axis {
	return Axis{Name: name, State: state, MachineChecked: machine, Evidence: evidence}
}
func pf(ok bool) string {
	if ok {
		return Pass
	}
	return Fail
}
func known(ok bool) string {
	if ok {
		return Pass
	}
	return Unknown
}

// StateOf is the only place a candidate state is decided. A FAIL on any
// required axis wins over everything; anything not PASS is pending.
func StateOf(axes []Axis) string {
	byName := map[string]string{}
	for _, a := range axes {
		byName[a.Name] = a.State
	}
	for _, name := range requiredAxes {
		if byName[name] == Fail {
			return CandidateNotUsable
		}
	}
	for _, name := range requiredAxes {
		if byName[name] != Pass {
			return CandidatePending
		}
	}
	return CandidateLimited
}

func verdicts(axes []Axis) Verdicts {
	by := map[string]string{}
	for _, a := range axes {
		by[a.Name] = a.State
	}
	all := func(names ...string) string {
		out := Pass
		for _, n := range names {
			switch by[n] {
			case Fail:
				return Fail
			case Pass:
			default:
				out = Unknown
			}
		}
		return out
	}
	return Verdicts{Fidelity: all(subjectAxes...), Generation: all(AxisTask, AxisAssetHash), Billing: all(AxisCharged), HumanUsefulness: "NOT_RUN"}
}

func limitsFor(c fs.Case) []string {
	out := []string{
		"已验证范围仅限冻结合成样本（纸盒/金属把手杯/玻璃瓶），不代表任意商品照片",
		"样本为硬边渲染，未覆盖真实照片的边缘羽化与光晕",
		"背景内容未经机器检查：可能出现多余物体，也可能与所写方向不符",
		"整体观感未经人工评审",
	}
	if c.MaterialClass == fs.MaterialTransparent {
		out = append(out, "玻璃透射与接触阴影不可机检：主体像素被锁定，但透射效果不保证")
	}
	return out
}

// Evaluate measures a composite against the frozen sample. It never returns an
// error: undecodable or mis-sized inputs become FAIL axes on the record.
func Evaluate(in Input, fitted, composite []byte) Report {
	c := in.Case
	axes := []Axis{}
	mask, merr := decode(in.Mask)
	orig, oerr := decode(in.Original)
	comp, cerr := decode(composite)
	haveSubject := merr == nil && oerr == nil && cerr == nil &&
		comp.Bounds().Dx() == c.Canvas.W && comp.Bounds().Dy() == c.Canvas.H &&
		orig.Bounds().Dx() == c.Canvas.W && orig.Bounds().Dy() == c.Canvas.H &&
		mask.Bounds().Dx() == c.Canvas.W && mask.Bounds().Dy() == c.Canvas.H

	protected := func(x, y int) bool { return mask.NRGBAAt(x, y).A >= 128 }
	same := func(x, y int) bool { return orig.NRGBAAt(x, y) == comp.NRGBAAt(x, y) }
	if haveSubject {
		prot, bad := 0, 0
		for y := 0; y < c.Canvas.H; y++ {
			for x := 0; x < c.Canvas.W; x++ {
				if protected(x, y) {
					prot++
					if !same(x, y) {
						bad++
					}
				}
			}
		}
		axes = append(axes, axis(AxisMaskPixels, pf(prot > 0 && bad == 0), true, fmt.Sprintf("protected=%d mismatched=%d", prot, bad)))
	} else {
		axes = append(axes, axis(AxisMaskPixels, Fail, true, "composite, original or mask unusable or not the case canvas"))
	}
	regionAxis := func(name string, regions []fs.Rect) Axis {
		if !haveSubject {
			return axis(name, Fail, true, "subject unusable")
		}
		inside, total := 0, 0
		for _, r := range regions {
			for y := r[1]; y < r[3]; y++ {
				for x := r[0]; x < r[2]; x++ {
					total++
					if x >= 0 && y >= 0 && x < c.Canvas.W && y < c.Canvas.H && protected(x, y) && same(x, y) {
						inside++
					}
				}
			}
		}
		return axis(name, pf(total > 0 && inside == total), true, fmt.Sprintf("regions=%d pixels=%d preserved=%d", len(regions), total, inside))
	}
	axes = append(axes,
		regionAxis(AxisLogo, c.Coverage.Logo), regionAxis(AxisText, c.Coverage.PackagingText),
		regionAxis(AxisSpec, c.Coverage.Spec), regionAxis(AxisStructure, c.Coverage.Structure))
	if merr == nil {
		n := components(mask)
		axes = append(axes, axis(AxisProductCount, pf(n == c.ProductCount), true, fmt.Sprintf("components=%d expected=%d", n, c.ProductCount)))
	} else {
		axes = append(axes, axis(AxisProductCount, Fail, true, "mask unusable"))
	}
	axes = append(axes, axis(AxisOutputSize, pf(cerr == nil && comp.Bounds().Dx() == c.Canvas.W && comp.Bounds().Dy() == c.Canvas.H), true, fmt.Sprintf("canvas=%dx%d", c.Canvas.W, c.Canvas.H)))
	plate, perr := decode(in.Plate)
	plateOK := perr == nil && plate.Bounds().Dx() == PlateSide && plate.Bounds().Dy() == PlateSide
	if perr != nil {
		axes = append(axes, axis(AxisPlateSize, Fail, true, "plate is not a PNG"))
	} else {
		axes = append(axes, axis(AxisPlateSize, pf(plateOK), true, fmt.Sprintf("plate=%dx%d expected=%dx%d", plate.Bounds().Dx(), plate.Bounds().Dy(), PlateSide, PlateSide)))
	}
	fit, ferr := decode(fitted)
	if haveSubject && ferr == nil && fit.Bounds().Dx() == c.Canvas.W && fit.Bounds().Dy() == c.Canvas.H {
		u, changed, sum := 0, 0, 0
		for y := 0; y < c.Canvas.H; y++ {
			for x := 0; x < c.Canvas.W; x++ {
				if protected(x, y) {
					continue
				}
				u++
				o, p := orig.NRGBAAt(x, y), fit.NRGBAAt(x, y)
				dr, dg, db := absDiff(o.R, p.R), absDiff(o.G, p.G), absDiff(o.B, p.B)
				sum += dr + dg + db
				if max(dr, dg, db) >= ChangedChannelDelta {
					changed++
				}
			}
		}
		anyDiff := sum > 0
		newPlate := sha(in.Plate) != sha(in.Original) && anyDiff
		axes = append(axes, axis(AxisNewPlate, pf(newPlate), true, fmt.Sprintf("plate_sha_differs=%v background_pixels_differ=%v", sha(in.Plate) != sha(in.Original), anyDiff)))
		ok := u > 0 && changed*1000 >= MinChangedPermille*u && sum >= MinMeanAbsDiffPerChanel*u*3
		axes = append(axes, axis(AxisBackground, pf(ok), true, fmt.Sprintf("background=%d changed=%d mean_abs_x1000=%d", u, changed, sum*1000/max(u*3, 1))))
	} else {
		axes = append(axes, axis(AxisNewPlate, Fail, true, "fitted plate unusable"), axis(AxisBackground, Fail, true, "fitted plate unusable"))
	}
	axes = append(axes,
		axis(AxisTask, known(in.Facts.TaskSucceeded), true, "task_id="+in.Facts.TaskID),
		axis(AxisAssetHash, known(in.Facts.PlateHashVerified), true, "plate_sha256="+in.Facts.PlateSHA256),
		axis(AxisCharged, known(in.Facts.ChargeSeen), true, "original_charge_id="+in.Facts.OriginalChargeID))
	transmission := NA
	if c.MaterialClass == fs.MaterialTransparent {
		transmission = Unknown
	}
	axes = append(axes,
		axis(AxisExtraObjects, Unknown, false, "not machine checkable"),
		axis(AxisIntent, Unknown, false, "not machine checkable"),
		axis(AxisVisual, Unknown, false, "not machine checkable"),
		axis(AxisTransmission, transmission, false, "not machine checkable"))
	state := StateOf(axes)
	rep := Report{
		ReportVersion: ReportVersion,
		Scope: Scope{Mode: in.Facts.Mode, Provider: in.Facts.Provider, Model: in.Facts.Model, PricingVersion: in.Facts.PricingVersion,
			SampleSetID: fs.SetID, CaseID: c.CaseID, Canvas: fmt.Sprintf("%dx%d", c.Canvas.W, c.Canvas.H), MaterialClass: c.MaterialClass, Claim: Claim},
		Axes: axes, Verdicts: verdicts(axes), CandidateState: state, Limits: []string{},
		Facts: ReportFacts{
			UsageID: in.Facts.UsageID, QuoteID: in.Facts.QuoteID, TaskID: in.Facts.TaskID, OriginalChargeID: in.Facts.OriginalChargeID,
			Plate:          PlateFacts{AssetID: in.Facts.PlateAssetID, ReferenceID: in.Facts.PlateReferenceID, SHA256: in.Facts.PlateSHA256},
			OriginalSHA256: sha(in.Original), MaskSHA256: sha(in.Mask), CoverageSHA256: in.CoverageSHA256, CompositeSHA256: compositeSHA(composite),
			FitID: FitID, ComposeID: ComposeID, ThresholdsVer: fs.ThresholdsVersion,
		},
		Build: in.Build,
	}
	if state == CandidateLimited {
		rep.Limits = limitsFor(c)
	}
	return rep
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func components(img *image.NRGBA) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	prot := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			prot[y*w+x] = img.NRGBAAt(x, y).A >= 128
		}
	}
	count := 0
	var stack []int
	for start := range prot {
		if !prot[start] {
			continue
		}
		area := 0
		prot[start] = false
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			area++
			x, y := p%w, p/w
			for _, n := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if n[0] < 0 || n[1] < 0 || n[0] >= w || n[1] >= h {
					continue
				}
				if q := n[1]*w + n[0]; prot[q] {
					prot[q] = false
					stack = append(stack, q)
				}
			}
		}
		if area >= minComponentArea {
			count++
		}
	}
	return count
}

// ReportJSON is the canonical byte form that gets hashed and frozen.
func ReportJSON(r Report) ([]byte, error) { return json.Marshal(r) }

// DecodeReport accepts only a canonical, internally consistent report: no
// unknown field (so no score), no duplicate axis, every state recomputed.
func DecodeReport(raw []byte) (Report, error) {
	var r Report
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return Report{}, fmt.Errorf("platelock: report: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return Report{}, fmt.Errorf("platelock: report: trailing data")
	}
	if r.ReportVersion != ReportVersion || r.Scope.Claim != Claim || r.Verdicts.HumanUsefulness != "NOT_RUN" {
		return Report{}, fmt.Errorf("platelock: report: header")
	}
	seen := map[string]bool{}
	want := map[string]bool{}
	for _, n := range requiredAxes {
		want[n] = true
	}
	for _, n := range limitAxes {
		want[n] = false
	}
	for _, a := range r.Axes {
		machine, ok := want[a.Name]
		if !ok || seen[a.Name] || a.MachineChecked != machine {
			return Report{}, fmt.Errorf("platelock: report: axis %q", a.Name)
		}
		seen[a.Name] = true
		switch a.State {
		case Pass, Fail, Unknown, NA:
		default:
			return Report{}, fmt.Errorf("platelock: report: axis %q state %q", a.Name, a.State)
		}
		if !machine && (a.State == Pass || a.State == Fail) {
			return Report{}, fmt.Errorf("platelock: report: axis %q cannot be machine decided", a.Name)
		}
	}
	if len(seen) != len(want) {
		return Report{}, fmt.Errorf("platelock: report: axes incomplete")
	}
	if r.CandidateState != StateOf(r.Axes) || r.Verdicts != verdicts(r.Axes) {
		return Report{}, fmt.Errorf("platelock: report: state does not follow from axes")
	}
	if (r.CandidateState == CandidateLimited) != (len(r.Limits) > 0) {
		return Report{}, fmt.Errorf("platelock: report: limits")
	}
	if canonical, err := ReportJSON(r); err != nil || !bytes.Equal(canonical, raw) {
		return Report{}, fmt.Errorf("platelock: report: not canonical")
	}
	return r, nil
}

// Output is what a derivation produces.
type Output struct {
	Composite []byte // nil when the plate could not be fitted or composed
	Report    Report
}

// Derive runs fit, compose and evaluate. Deterministic for equal inputs on one
// build; the stored record, not a recomputation, is the authority afterwards.
func Derive(in Input) Output {
	fitted, ferr := FitPlateCover(in.Plate, in.Case.Canvas.W, in.Case.Canvas.H)
	var composite []byte
	if ferr == nil {
		composite, _ = Compose(in.Original, fitted, in.Mask)
	}
	return Output{Composite: composite, Report: Evaluate(in, fitted, composite)}
}
