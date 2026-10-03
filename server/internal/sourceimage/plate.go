package sourceimage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PlateInput is the immutable, server-derived input of a background_plate_lock
// run. Nothing here comes from the browser except InputID and BackgroundIntent,
// and both are checked against the frozen sample set before it is stored.
type PlateInput struct {
	CaseID            string `json:"case_id"`
	SetID             string `json:"set_id"`
	SetSHA256         string `json:"set_sha256"`
	InputID           string `json:"input_id"`
	OriginalSHA256    string `json:"original_sha256"`
	MaskSHA256        string `json:"mask_sha256"`
	CoverageSHA256    string `json:"coverage_sha256"`
	CanvasW           int    `json:"canvas_w"`
	CanvasH           int    `json:"canvas_h"`
	BackgroundIntent  string `json:"background_intent"`
	FitID             string `json:"fit_id"`
	ComposeID         string `json:"compose_id"`
	ThresholdsVersion string `json:"thresholds_version"`
}

// PlateDigest is stored in Intent.PlateDigest and re-checked on every read.
func PlateDigest(p PlateInput) string { return digest(p) }

func (p PlateInput) Valid() bool {
	for _, d := range []string{p.SetSHA256, p.OriginalSHA256, p.MaskSHA256, p.CoverageSHA256} {
		if !validDigest(d) {
			return false
		}
	}
	return identifier(p.CaseID) && identifier(p.SetID) && identifier(p.InputID) && p.CanvasW > 0 && p.CanvasH > 0 && p.CanvasW <= 4096 && p.CanvasH <= 4096 &&
		ValidIntentText(p.BackgroundIntent) && identifier(p.FitID) && identifier(p.ComposeID) && identifier(p.ThresholdsVersion)
}

// ValidIntentText is the one rule for a user's background direction: 1 to 200
// characters, trimmed, no control characters. fidelitysamples.ValidIntent must
// agree with it (pinned by a sourceflow test).
func ValidIntentText(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 200 || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

const (
	DerivationDerived = "derived"
	DerivationBlocked = "blocked"

	CandidateLimited   = "limited_candidate"
	CandidateNotUsable = "not_usable_candidate"
	CandidatePending   = "pending"
	CandidateBlocked   = "blocked"

	BlockedSampleSetChanged = "sample_set_changed"
)

// PlateDerivation is write-once. PNG is the derived composite (empty only when
// the plate could not be composed, which is always not_usable_candidate).
type PlateDerivation struct {
	RunID, State, BlockedReason string
	PNG                         []byte
	SHA256                      string
	SizeBytes                   int64
	Width, Height               int
	ReportJSON, ReportSHA256    string
	CandidateState              string
	CreatedAt                   int64
}

func SHA256Hex(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// ValidatePlateDerivation is the store's integrity gate for both write and read.
func ValidatePlateDerivation(d PlateDerivation) error {
	if !identifier(d.RunID) {
		return ErrInvariant
	}
	switch d.State {
	case DerivationBlocked:
		if d.BlockedReason == "" || d.CandidateState != CandidateBlocked || len(d.PNG) != 0 || d.SHA256 != "" || d.ReportJSON != "" || d.ReportSHA256 != "" {
			return ErrInvariant
		}
		return nil
	case DerivationDerived:
	default:
		return ErrInvariant
	}
	switch d.CandidateState {
	case CandidateLimited, CandidateNotUsable, CandidatePending:
	default:
		return ErrInvariant
	}
	if d.ReportJSON == "" || d.ReportSHA256 != SHA256Hex([]byte(d.ReportJSON)) || d.BlockedReason != "" {
		return ErrInvariant
	}
	if len(d.PNG) == 0 {
		if d.SHA256 != "" || d.SizeBytes != 0 || d.CandidateState != CandidateNotUsable {
			return ErrInvariant
		}
		return nil
	}
	if d.SHA256 != SHA256Hex(d.PNG) || d.SizeBytes != int64(len(d.PNG)) || d.Width <= 0 || d.Height <= 0 || d.SizeBytes > 16<<20 {
		return ErrInvariant
	}
	return nil
}
