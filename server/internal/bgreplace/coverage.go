package bgreplace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"strings"
)

// CoverageRegion uses exclusive right/bottom coordinates in the original canvas.
type CoverageRegion [4]int

// CoverageSample is a server-controlled frozen annotation, never a browser claim.
// The mask digest binds the complete approved silhouette; axis regions verify
// that the required product details are actually inside the protected area.
type CoverageSample struct {
	ID             string                      `json:"id"`
	Source         string                      `json:"source"`
	License        string                      `json:"license"`
	ApprovalRef    string                      `json:"approval_ref"`
	Scope          string                      `json:"scope"`
	OriginalSHA256 string                      `json:"original_sha256"`
	MaskSHA256     string                      `json:"mask_sha256"`
	Width          int                         `json:"width"`
	Height         int                         `json:"height"`
	Regions        map[string][]CoverageRegion `json:"regions"`
}

type CoverageEvidence struct {
	SampleID       string `json:"sample_id"`
	OriginalSHA256 string `json:"original_sha256"`
	MaskSHA256     string `json:"mask_sha256"`
	CoverageSHA256 string `json:"coverage_sha256"`
	Scope          string `json:"scope"`
}

func ImageSHA(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func VerifyCoverage(original, mask []byte, sample CoverageSample) (CoverageEvidence, error) {
	for _, v := range []string{sample.ID, sample.Source, sample.License, sample.ApprovalRef, sample.Scope} {
		if strings.TrimSpace(v) == "" {
			return CoverageEvidence{}, fmt.Errorf("sample authorization incomplete")
		}
	}
	if ImageSHA(original) != sample.OriginalSHA256 || ImageSHA(mask) != sample.MaskSHA256 {
		return CoverageEvidence{}, fmt.Errorf("sample content changed")
	}
	if sample.Width < 1 || sample.Height < 1 || sample.Width > 4096 || sample.Height > 4096 {
		return CoverageEvidence{}, fmt.Errorf("sample dimensions invalid")
	}
	for _, raw := range [][]byte{original, mask} {
		cfg, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || cfg.Width != sample.Width || cfg.Height != sample.Height {
			return CoverageEvidence{}, fmt.Errorf("sample dimensions changed")
		}
	}
	if err := MaskReady(original, mask); err != nil {
		return CoverageEvidence{}, err
	}
	matte, err := decodePNG(mask)
	if err != nil {
		return CoverageEvidence{}, err
	}
	if matte.Bounds().Dx() != sample.Width || matte.Bounds().Dy() != sample.Height {
		return CoverageEvidence{}, fmt.Errorf("sample dimensions changed")
	}
	for _, axis := range []string{AxisLogo, AxisPackagingText, AxisSpec, AxisStructure} {
		regions := sample.Regions[axis]
		if len(regions) == 0 {
			return CoverageEvidence{}, fmt.Errorf("missing %s coverage", axis)
		}
		for _, r := range regions {
			if r[0] < 0 || r[1] < 0 || r[2] <= r[0] || r[3] <= r[1] || r[2] > sample.Width || r[3] > sample.Height {
				return CoverageEvidence{}, fmt.Errorf("invalid %s region", axis)
			}
			for y := r[1]; y < r[3]; y++ {
				for x := r[0]; x < r[2]; x++ {
					if nrgbaAt(matte, x, y).A < 128 {
						return CoverageEvidence{}, fmt.Errorf("%s is outside protected subject", axis)
					}
				}
			}
		}
	}
	raw, err := json.Marshal(sample)
	if err != nil {
		return CoverageEvidence{}, err
	}
	return CoverageEvidence{SampleID: sample.ID, OriginalSHA256: sample.OriginalSHA256, MaskSHA256: sample.MaskSHA256, CoverageSHA256: ImageSHA(raw), Scope: sample.Scope}, nil
}
