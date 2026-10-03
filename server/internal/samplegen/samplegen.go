package samplegen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

const GeneratorName, GeneratorVersion = "samplegen", "1"

// Canvases are the common e-commerce outputs the gate covers.
var Canvases = []fs.Canvas{{W: 1024, H: 1024}, {W: 1024, H: 1280}, {W: 768, H: 1024}}

type kindSpec struct {
	kind, material string
	draw           func(w, h int) drawn
	keypoints      fs.Keypoints
	intents        []string
	humanAxes      []string
}

var kinds = []kindSpec{
	{fs.KindCarton, fs.MaterialOpaque, drawCarton,
		fs.Keypoints{Text: []string{"PAINUO", "NET 250G", "LOT 20261002"}, Logo: "circle-emblem", Structure: []string{"top-flap", "body"}},
		[]string{"深蓝色渐变摄影棚背景，柔和顶光", "暖色原木桌面，窗边自然光"},
		[]string{"background_extra_objects", "background_intent_followed", "visual_composite_quality"}},
	{fs.KindHandledMetal, fs.MaterialReflective, drawHandledMetal,
		fs.Keypoints{Text: []string{"PAINUO", "350ML"}, Logo: "circle-emblem", Structure: []string{"rim", "handle-hole"}},
		[]string{"深灰色大理石台面，冷色侧光", "绿色植物墙背景，浅景深"},
		[]string{"background_extra_objects", "background_intent_followed", "visual_composite_quality"}},
	{fs.KindGlassBottle, fs.MaterialTransparent, drawGlassBottle,
		fs.Keypoints{Text: []string{"PAINUO", "500ML"}, Logo: "circle-emblem", Structure: []string{"cap", "neck", "body"}},
		[]string{"黑色亚克力台面，细长高光", "海边沙滩与蓝天，明亮日光"},
		[]string{"background_extra_objects", "background_intent_followed", "transmission_edge", "visual_composite_quality"}},
}

// Case is one generated sample before encoding.
type Case struct {
	Meta           fs.Case
	Original, Mask *image.NRGBA
}

// Build returns the nine cases (3 kinds x 3 canvases) in manifest order.
func Build() []Case {
	var out []Case
	for _, k := range kinds {
		for _, cv := range Canvases {
			d := k.draw(cv.W, cv.H)
			id := fmt.Sprintf("%s-%dx%d", k.kind, cv.W, cv.H)
			out = append(out, Case{
				Original: d.original, Mask: d.mask,
				Meta: fs.Case{
					CaseID: id, Kind: k.kind, MaterialClass: k.material, Canvas: cv,
					Original:     fs.File{Path: "originals/" + id + ".png", PixelSHA256: fs.PixelSHA256(d.original)},
					Mask:         fs.File{Path: "masks/" + id + ".png", PixelSHA256: fs.PixelSHA256(d.mask)},
					Coverage:     d.cov,
					ProductCount: 1, ExpectedKeypoints: k.keypoints,
					Intents: append([]string(nil), k.intents...), HumanAxes: append([]string(nil), k.humanAxes...),
				},
			})
		}
	}
	return out
}

func encode(img *image.NRGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func fileSHA(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func manifestFor(cases []Case, fileShas map[string]string) fs.Manifest {
	m := fs.Manifest{SetID: fs.SetID, Version: fs.SetVersion, Generator: fs.Generator{Name: GeneratorName, Version: GeneratorVersion},
		Source: fs.SourceGenerator, License: fs.License, ApprovalRef: fs.ApprovalRef, ThresholdsVersion: fs.ThresholdsVersion}
	for _, c := range cases {
		meta := c.Meta
		meta.Original.SHA256 = fileShas[meta.Original.Path]
		meta.Mask.SHA256 = fileShas[meta.Mask.Path]
		m.Cases = append(m.Cases, meta)
	}
	m.SetSHA256 = fs.ComputeSetSHA256(m.Cases)
	return m
}

func marshal(m fs.Manifest) []byte {
	raw, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		panic(err)
	}
	return append(raw, '\n')
}

// Write encodes every PNG, writes the manifest and the README under dir.
func Write(dir string) error {
	cases := Build()
	shas := map[string]string{}
	for _, c := range cases {
		for _, f := range []struct {
			path string
			img  *image.NRGBA
		}{{c.Meta.Original.Path, c.Original}, {c.Meta.Mask.Path, c.Mask}} {
			raw := encode(f.img)
			full := filepath.Join(dir, filepath.FromSlash(f.path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(full, raw, 0o644); err != nil {
				return err
			}
			shas[f.path] = fileSHA(raw)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), marshal(manifestFor(cases, shas)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644)
}

// Check proves the committed set is still exactly what the generator draws:
// committed bytes match the manifest digests, decoded pixels match the
// manifest, regenerated pixels match the manifest, and every non-digest field
// equals the regenerated manifest. It never rewrites anything.
func Check(dir string) error {
	rawManifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var committed fs.Manifest
	if err := json.Unmarshal(rawManifest, &committed); err != nil {
		return err
	}
	cases := Build()
	if len(committed.Cases) != len(cases) {
		return fmt.Errorf("case count %d != generated %d", len(committed.Cases), len(cases))
	}
	shas := map[string]string{}
	for i, c := range cases {
		cm := committed.Cases[i]
		for _, f := range []struct {
			file  fs.File
			pixel string
		}{{cm.Original, c.Meta.Original.PixelSHA256}, {cm.Mask, c.Meta.Mask.PixelSHA256}} {
			raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.file.Path)))
			if err != nil {
				return err
			}
			if fileSHA(raw) != f.file.SHA256 {
				return fmt.Errorf("%s: committed bytes differ from manifest sha256", f.file.Path)
			}
			img, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				return fmt.Errorf("%s: %w", f.file.Path, err)
			}
			if got := fs.PixelSHA256(img); got != f.file.PixelSHA256 || got != f.pixel {
				return fmt.Errorf("%s: pixels differ from generator (manifest %s, decoded %s, generated %s)", f.file.Path, f.file.PixelSHA256, got, f.pixel)
			}
			shas[f.file.Path] = f.file.SHA256
		}
	}
	if want := marshal(manifestFor(cases, shas)); !bytes.Equal(want, rawManifest) {
		return fmt.Errorf("manifest.json differs from the generator's manifest (fields other than file digests)")
	}
	return nil
}

const readme = `# 冻结授权样本集 v1（HUI-2232 C2）

原创合成商品图，由 server/cmd/samplegen 确定性生成，项目内部授权，仅用于验证“限定保真换背景”。
它们不是实物照片：主体为硬边渲染，蒙版即主体轮廓，不覆盖真实照片的边缘羽化/光晕。

- 3 类商品（纸盒/带把金属杯/玻璃瓶）x 3 个画布（1024x1024、1024x1280、768x1024）= 9 个 case。
- manifest.json 记录每个文件的 sha256（文件字节）与 pixel_sha256（解码像素），改一个像素都会被发现。
- 校验：cd server && go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1
- 重新生成（仅在有意修改样本并升版本时）：go run ./cmd/samplegen -write ../fixtures/frozen-samples/v1
- 已验证范围只到本集合；任意用户照片不在范围内。
`
