package fidelitysamples

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
)

const (
	maxManifestBytes = 1 << 20
	maxImageBytes    = 8 << 20
	maxCases         = 64
	minComponentArea = 64
)

var (
	caseIDPattern = regexp.MustCompile(`^[a-z0-9_]+-[0-9]+x[0-9]+$`)
	humanAxes     = map[string]bool{"background_extra_objects": true, "background_intent_followed": true, "transmission_edge": true, "visual_composite_quality": true}
	kindMaterial  = map[string]string{KindCarton: MaterialOpaque, KindHandledMetal: MaterialReflective, KindGlassBottle: MaterialTransparent}
)

// LoadedCase carries the verified bytes. Everything here was recomputed from
// disk; nothing is trusted from the manifest alone.
type LoadedCase struct {
	Case
	OriginalBytes, MaskBytes []byte
	Sample                   bgreplace.CoverageSample
	Evidence                 bgreplace.CoverageEvidence
}

type Set struct {
	Manifest Manifest
	cases    []*LoadedCase
	bySHA    map[string]*LoadedCase
}

func (s *Set) Cases() []*LoadedCase { return s.cases }
func (s *Set) ID() string           { return s.Manifest.SetID }
func (s *Set) SHA256() string       { return s.Manifest.SetSHA256 }

// BySHA finds the case whose frozen original has exactly this file digest.
func (s *Set) BySHA(sha string) (*LoadedCase, bool) { c, ok := s.bySHA[sha]; return c, ok }

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Load verifies the whole set. Any failure rejects the set as a unit: a
// partially trusted sample set could let a mismatched mask protect the wrong
// pixels, so there is no per-case fallback.
func Load(dir string) (*Set, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, invalid("directory not configured")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, invalid("directory unreadable: %v", err)
	}
	raw, err := readRegular(filepath.Join(root, "manifest.json"), root, maxManifestBytes)
	if err != nil {
		return nil, invalid("manifest: %v", err)
	}
	var m Manifest
	if err := decodeStrict(raw, &m); err != nil {
		return nil, invalid("manifest: %v", err)
	}
	if m.SetID != SetID || m.Version != SetVersion || m.Source != SourceGenerator || m.License != License || m.ApprovalRef != ApprovalRef || m.ThresholdsVersion != ThresholdsVersion || strings.TrimSpace(m.Generator.Name) == "" || strings.TrimSpace(m.Generator.Version) == "" {
		return nil, invalid("manifest header does not match the approved frozen set")
	}
	if len(m.Cases) == 0 || len(m.Cases) > maxCases {
		return nil, invalid("case count %d out of range", len(m.Cases))
	}
	set := &Set{Manifest: m, bySHA: map[string]*LoadedCase{}}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		lc, err := loadCase(root, c)
		if err != nil {
			return nil, invalid("case %q: %v", c.CaseID, err)
		}
		if seen[c.CaseID] {
			return nil, invalid("duplicate case id %q", c.CaseID)
		}
		seen[c.CaseID] = true
		if _, dup := set.bySHA[c.Original.SHA256]; dup {
			return nil, invalid("case %q: original digest already used by another case", c.CaseID)
		}
		set.bySHA[c.Original.SHA256] = lc
		set.cases = append(set.cases, lc)
	}
	if ComputeSetSHA256(m.Cases) != m.SetSHA256 {
		return nil, invalid("set_sha256 does not match the case digests")
	}
	return set, nil
}

func loadCase(root string, c Case) (*LoadedCase, error) {
	if !caseIDPattern.MatchString(c.CaseID) || len(c.CaseID) > 64 {
		return nil, fmt.Errorf("case id shape")
	}
	if want, ok := kindMaterial[c.Kind]; !ok || c.MaterialClass != want {
		return nil, fmt.Errorf("kind/material_class pair not approved")
	}
	if c.Canvas.W < 1 || c.Canvas.H < 1 || c.Canvas.W > 4096 || c.Canvas.H > 4096 {
		return nil, fmt.Errorf("canvas out of range")
	}
	if c.ProductCount < 1 || c.ProductCount > 8 {
		return nil, fmt.Errorf("product_count out of range")
	}
	if len(c.Intents) < 2 || len(c.Intents) > 8 {
		return nil, fmt.Errorf("need 2..8 background intents")
	}
	for _, in := range c.Intents {
		if !validIntent(in) {
			return nil, fmt.Errorf("background intent invalid")
		}
	}
	if len(c.HumanAxes) == 0 {
		return nil, fmt.Errorf("human_axes missing")
	}
	for _, a := range c.HumanAxes {
		if !humanAxes[a] {
			return nil, fmt.Errorf("unknown human axis %q", a)
		}
	}
	orig, _, err := loadImage(root, c.Original, c.Canvas)
	if err != nil {
		return nil, fmt.Errorf("original: %w", err)
	}
	mask, maskImg, err := loadImage(root, c.Mask, c.Canvas)
	if err != nil {
		return nil, fmt.Errorf("mask: %w", err)
	}
	sample := c.CoverageSample()
	evidence, err := bgreplace.VerifyCoverage(orig, mask, sample)
	if err != nil {
		return nil, fmt.Errorf("coverage: %v", err)
	}
	if n := countComponents(maskImg); n != c.ProductCount {
		return nil, fmt.Errorf("mask has %d product components, manifest says %d", n, c.ProductCount)
	}
	return &LoadedCase{Case: c, OriginalBytes: orig, MaskBytes: mask, Sample: sample, Evidence: evidence}, nil
}

// ValidIntent is the single rule for a background direction, used for frozen
// intents here and for the user's text at the HTTP boundary.
func ValidIntent(s string) bool { return validIntent(s) }

func validIntent(s string) bool {
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

func loadImage(root string, f File, canvas Canvas) ([]byte, image.Image, error) {
	if !filepath.IsLocal(filepath.FromSlash(f.Path)) || strings.Contains(f.Path, `\`) {
		return nil, nil, fmt.Errorf("path escapes the sample directory")
	}
	raw, err := readRegular(filepath.Join(root, filepath.FromSlash(f.Path)), root, maxImageBytes)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != f.SHA256 {
		return nil, nil, fmt.Errorf("sha256 differs from manifest")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != canvas.W || cfg.Height != canvas.H {
		return nil, nil, fmt.Errorf("not a PNG of the case canvas")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("PNG undecodable")
	}
	if PixelSHA256(img) != f.PixelSHA256 {
		return nil, nil, fmt.Errorf("pixel_sha256 differs from manifest")
	}
	return raw, img, nil
}

// readRegular reads a regular file that really lives under root (no symlink
// hops out of the directory).
func readRegular(path, root string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if rel, err := filepath.Rel(root, real); err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("resolves outside the sample directory")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit || len(raw) == 0 {
		return nil, fmt.Errorf("size out of range")
	}
	return raw, nil
}

// decodeStrict rejects unknown fields, duplicate keys at any depth and trailing
// data. encoding/json alone accepts all three.
func decodeStrict(raw []byte, dst any) error {
	if err := rejectDuplicateKeys(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the manifest object")
	}
	return nil
}

func rejectDuplicateKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return err
				}
				k := kt.(string)
				if keys[k] {
					return fmt.Errorf("duplicate key %q", k)
				}
				keys[k] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		_, err = d.Token()
		return err
	}
	return walk()
}

// countComponents counts 4-connected regions of protected pixels
// (alpha >= 128) with at least minComponentArea pixels.
func countComponents(img image.Image) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	prot := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			prot[y*w+x] = a>>8 >= 128
		}
	}
	count := 0
	stack := make([]int, 0, 1024)
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
