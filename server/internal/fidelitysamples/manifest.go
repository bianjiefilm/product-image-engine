// Package fidelitysamples loads the frozen, server-controlled sample set that
// bounds what "limited fidelity background replacement" may claim. The set is
// authored by cmd/samplegen, committed under fixtures/frozen-samples/v1, and
// re-verified byte by byte at load time. Browsers never supply any of it.
package fidelitysamples

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
)

const (
	SetID             = "frozen-sample-set"
	SetVersion        = "v1"
	ThresholdsVersion = "bg-lock-thresholds/v1"
	SourceGenerator   = "deterministic-generator"
	License           = "原创合成作品，项目内部授权"
	ApprovalRef       = "HUI-2232 C2 spec R3"
	// Scope is the only claim the set can support.
	Scope = "frozen_synthetic_sample_only"

	KindCarton       = "carton"
	KindHandledMetal = "handled_metal"
	KindGlassBottle  = "glass_bottle"

	MaterialOpaque      = "opaque"
	MaterialReflective  = "reflective"
	MaterialTransparent = "transparent"
)

// ErrInvalid wraps every reason the set cannot be used. The message is for
// operators; HTTP surfaces only expose the stable reason code.
var ErrInvalid = errors.New("fidelitysamples: invalid sample set")

type Rect = bgreplace.CoverageRegion

type Generator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type Canvas struct {
	W int `json:"w"`
	H int `json:"h"`
}
type File struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	PixelSHA256 string `json:"pixel_sha256"`
}
type Coverage struct {
	Logo          []Rect `json:"logo"`
	PackagingText []Rect `json:"packaging_text"`
	Spec          []Rect `json:"spec"`
	Structure     []Rect `json:"structure"`
}
type Keypoints struct {
	Text      []string `json:"text"`
	Logo      string   `json:"logo"`
	Structure []string `json:"structure"`
}
type Case struct {
	CaseID            string    `json:"case_id"`
	Kind              string    `json:"kind"`
	MaterialClass     string    `json:"material_class"`
	Canvas            Canvas    `json:"canvas"`
	Original          File      `json:"original"`
	Mask              File      `json:"mask"`
	Coverage          Coverage  `json:"coverage"`
	ProductCount      int       `json:"product_count"`
	ExpectedKeypoints Keypoints `json:"expected_keypoints"`
	Intents           []string  `json:"intents"`
	HumanAxes         []string  `json:"human_axes"`
}
type Manifest struct {
	SetID             string    `json:"set_id"`
	Version           string    `json:"version"`
	Generator         Generator `json:"generator"`
	Source            string    `json:"source"`
	License           string    `json:"license"`
	ApprovalRef       string    `json:"approval_ref"`
	ThresholdsVersion string    `json:"thresholds_version"`
	SetSHA256         string    `json:"set_sha256"`
	Cases             []Case    `json:"cases"`
}

// ComputeSetSHA256 binds case ids and file digests in manifest order.
func ComputeSetSHA256(cases []Case) string {
	h := sha256.New()
	for _, c := range cases {
		h.Write([]byte(c.CaseID + "\n" + c.Original.SHA256 + "\n" + c.Mask.SHA256 + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PixelSHA256 hashes decoded NRGBA pixels plus dimensions, independent of the
// PNG encoder, so the generator can prove it still reproduces the frozen pixels.
func PixelSHA256(img image.Image) string {
	b := img.Bounds()
	h := sha256.New()
	h.Write([]byte{byte(b.Dx() >> 8), byte(b.Dx()), byte(b.Dy() >> 8), byte(b.Dy())})
	row := make([]byte, 0, b.Dx()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row = row[:0]
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			row = append(row, c.R, c.G, c.B, c.A)
		}
		h.Write(row)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CoverageSample converts one case to the existing server-controlled coverage
// annotation so VerifyCoverage is reused rather than reimplemented.
func (c Case) CoverageSample() bgreplace.CoverageSample {
	return bgreplace.CoverageSample{
		ID: c.CaseID, Source: SourceGenerator, License: License, ApprovalRef: ApprovalRef, Scope: Scope,
		OriginalSHA256: c.Original.SHA256, MaskSHA256: c.Mask.SHA256, Width: c.Canvas.W, Height: c.Canvas.H,
		Regions: map[string][]bgreplace.CoverageRegion{
			bgreplace.AxisLogo: c.Coverage.Logo, bgreplace.AxisPackagingText: c.Coverage.PackagingText,
			bgreplace.AxisSpec: c.Coverage.Spec, bgreplace.AxisStructure: c.Coverage.Structure,
		},
	}
}

// FileSHA256 is the digest recorded for committed file bytes.
func FileSHA256(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
