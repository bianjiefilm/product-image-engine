// Package sizeadapt 实现电商尺寸适配(HUI-1703 / FEAT-0204)。
//
// 纯确定性图像处理:输入合法 PNG,按预设 {width,height,mode} 三元组做
// pad(等比缩放+白底填充)或 cover(等比缩放+居中裁切)变换,输出 PNG/JPEG。
// 不调用任何 AI 模型、不产生任务/计费调用边、不触碰生成开关——本包
// import 白名单由 TestNoForbiddenImports 用 go/parser 钉死(零耦合红线)。
package sizeadapt

import (
	"bytes"
	_ "embed" // presets.json 内嵌
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"regexp"

	xdraw "golang.org/x/image/draw"
)

// Mode 变换模式。
type Mode string

const (
	// ModePad 等比缩放 + 白底填充到目标画布(商品主图常用)。
	ModePad Mode = "pad"
	// ModeCover 等比缩放 + 居中裁切填满(广告位常用)。
	ModeCover Mode = "cover"
)

// Format 输出格式(PNG 默认;JPEG quality 固定 90)。
type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
)

// jpegQuality 拍板:固定 90,不可配(避免又一个自由度)。
const jpegQuality = 90

// 输入门(fail-closed;口径与 I1 outputs 域一致 + 防解压炸弹)。
const (
	MaxInputBytes = 8 << 20  // 源 PNG ≤ 8 MiB(与成果登记同口径)
	MaxDimension  = 16384    // 解码后单边上限
	MaxPixels     = 40 << 20 // 解码后总像素上限
)

// 可分类错误(HTTP 层据此映射 400/413)。
var (
	ErrBadMagic   = errors.New("sizeadapt: 仅接受合法 PNG(魔数校验失败)")
	ErrTooLarge   = errors.New("sizeadapt: 图片超过 8 MiB 上限")
	ErrCorrupt    = errors.New("sizeadapt: PNG 解码失败")
	ErrHugePixels = errors.New("sizeadapt: 解码后像素尺寸超限")
	ErrBadFormat  = errors.New("sizeadapt: 输出格式仅支持 png/jpeg")
)

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// DecodePNG 输入门:魔数 → 大小 → 解码 → 解码后尺寸上限。
func DecodePNG(data []byte) (image.Image, error) {
	if len(data) > MaxInputBytes {
		return nil, ErrTooLarge
	}
	if len(data) < len(pngMagic) || string(data[:len(pngMagic)]) != string(pngMagic) {
		return nil, ErrBadMagic
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > MaxDimension || h > MaxDimension || w*h > MaxPixels {
		return nil, fmt.Errorf("%w: %dx%d", ErrHugePixels, w, h)
	}
	return img, nil
}

// Adapt 确定性变换:同输入+同预设+同格式 → 字节级相同输出。
// 返回编码字节与输出宽高。
func Adapt(src image.Image, p Preset, f Format) (out []byte, w, h int, err error) {
	if f != FormatPNG && f != FormatJPEG {
		return nil, 0, 0, ErrBadFormat
	}
	dst := transform(src, p)
	var buf bytes.Buffer
	switch f {
	case FormatJPEG:
		// 画布已与白底合成(不透明),JPEG 编码安全。
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality})
	default:
		err = png.Encode(&buf, dst)
	}
	if err != nil {
		return nil, 0, 0, fmt.Errorf("sizeadapt: 编码失败: %w", err)
	}
	b := dst.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

// transform 核心:缩放 + 模式化摆放(纯函数;白底合成压平透明通道)。
func transform(src image.Image, p Preset) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	fitWidth := p.Mode == ModePad && p.Height == 0

	var scale float64
	switch {
	case fitWidth:
		scale = float64(p.Width) / float64(sw)
	case p.Mode == ModeCover:
		scale = math.Max(float64(p.Width)/float64(sw), float64(p.Height)/float64(sh))
	default: // pad
		scale = math.Min(float64(p.Width)/float64(sw), float64(p.Height)/float64(sh))
	}
	dw := int(math.Round(float64(sw) * scale))
	dh := int(math.Round(float64(sh) * scale))
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}

	// 等比缩放到 (dw,dh)(CatmullRom;同输入恒定同结果)。
	scaled := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, draw.Over, nil)

	// 白底画布:pad → 目标画布居中摆放;fit-width → 画布高=内容高;
	// cover → 画布即目标,居中裁切取缩放结果中部。透明区一律压平为白。
	cw, ch := p.Width, p.Height
	if fitWidth {
		ch = dh
	}
	canvas := newWhiteCanvas(cw, ch)
	switch {
	case fitWidth:
		draw.Draw(canvas, canvas.Bounds(), scaled, scaled.Bounds().Min, draw.Over)
	case p.Mode == ModeCover:
		cx := (dw - cw) / 2
		cy := (dh - ch) / 2
		draw.Draw(canvas, canvas.Bounds(), scaled, image.Point{X: cx, Y: cy}, draw.Over)
	default: // pad
		ox := (cw - dw) / 2
		oy := (ch - dh) / 2
		draw.Draw(canvas, image.Rect(ox, oy, ox+dw, oy+dh), scaled, scaled.Bounds().Min, draw.Over)
	}
	return canvas
}

// newWhiteCanvas 建立不透明白底画布(合成基准)。
func newWhiteCanvas(w, h int) *image.RGBA {
	c := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(c, c.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	return c
}

// ---- 预设矩阵 ---------------------------------------------------------------

// Preset 预设三元组(width/height/mode)+ 展示名。
// height==0 仅限 pad:fit-width 语义(宽缩放到精确值,高按源等比,不纵向填充),
// 用于「详情页 790 宽等比」类规格——schema 仍为三元组,哨兵值已文档化。
type Preset struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Mode   Mode   `json:"mode"`
	Label  string `json:"label,omitempty"`
}

type presetFile struct {
	Presets []Preset `json:"presets"`
}

//go:embed presets.json
var embeddedPresets []byte

// PresetSet 已校验预设集(不可变;Get/List 只读)。
type PresetSet struct {
	byName map[string]Preset
	list   []Preset
}

// DefaultPresets 内嵌公开常见规格整理(README:商家可自定义覆盖,以各平台当时官方要求为准)。
func DefaultPresets() (*PresetSet, error) { return LoadPresets(embeddedPresets) }

// LoadPresets 加载并校验预设集;任何非法项 fail-closed 整体拒绝。
func LoadPresets(raw []byte) (*PresetSet, error) {
	var f presetFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("sizeadapt: 预设集 JSON 解析失败: %w", err)
	}
	if len(f.Presets) == 0 {
		return nil, errors.New("sizeadapt: 预设集为空(至少一个预设)")
	}
	s := &PresetSet{byName: make(map[string]Preset, len(f.Presets))}
	for i, p := range f.Presets {
		if err := validatePreset(p); err != nil {
			return nil, fmt.Errorf("sizeadapt: 预设 #%d(%q)非法: %w", i, p.Name, err)
		}
		if _, dup := s.byName[p.Name]; dup {
			return nil, fmt.Errorf("sizeadapt: 预设名重复: %q", p.Name)
		}
		s.byName[p.Name] = p
		s.list = append(s.list, p)
	}
	return s, nil
}

var presetNameRE = regexp.MustCompile(`^[a-z0-9_]{2,64}$`)

func validatePreset(p Preset) error {
	if !presetNameRE.MatchString(p.Name) {
		return errors.New("name 须为 [a-z0-9_]{2,64}")
	}
	if p.Width < 1 || p.Width > MaxDimension {
		return fmt.Errorf("width 须 1..%d", MaxDimension)
	}
	if p.Height < 0 || p.Height > MaxDimension {
		return fmt.Errorf("height 须 0..%d", MaxDimension)
	}
	switch p.Mode {
	case ModePad:
		// height ∈ [0..Max];0 = fit-width 哨兵(宽精确、高等比、不填充)。
		return nil
	case ModeCover:
		if p.Height < 1 {
			return errors.New("cover 须 height ≥1(fit-width 仅支持 pad)")
		}
		return nil
	default:
		return errors.New("mode 须 pad|cover")
	}
}

// Get 按名取预设。
func (s *PresetSet) Get(name string) (Preset, bool) {
	p, ok := s.byName[name]
	return p, ok
}

// List 全部预设(内嵌顺序)。
func (s *PresetSet) List() []Preset { return append([]Preset(nil), s.list...) }
