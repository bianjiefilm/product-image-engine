package sizeadapt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- 工具 ------------------------------------------------------------------

func mustPreset(t *testing.T, s *PresetSet, name string) Preset {
	t.Helper()
	p, ok := s.Get(name)
	if !ok {
		t.Fatalf("预设 %s 应存在", name)
	}
	return p
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func solidRGBA(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func decodeOut(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出必须是合法 PNG: %v", err)
	}
	return img
}

func at(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

var white = color.RGBA{255, 255, 255, 255}

func defaultsForTest(t *testing.T) *PresetSet {
	t.Helper()
	const raw = `{"presets":[
		{"name":"tiny_pad_10","width":10,"height":10,"mode":"pad","label":"10 方图"},
		{"name":"tiny_pad_20","width":20,"height":20,"mode":"pad","label":"20 方图"},
		{"name":"tall_pad_30x80","width":30,"height":80,"mode":"pad","label":"竖画布"},
		{"name":"square_cover_100","width":100,"height":100,"mode":"cover","label":"方裁切"},
		{"name":"square_pad_40","width":40,"height":40,"mode":"pad","label":"40 方图"},
		{"name":"square_pad_64","width":64,"height":64,"mode":"pad","label":"64 方图"},
		{"name":"fit_w_100","width":100,"height":0,"mode":"pad","label":"等宽"}
	]}`
	s, err := LoadPresets([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ---- 变换:尺寸与模式像素级断言 ---------------------------------------------

func TestPadUpscalesSmallImageOntoWhiteCanvas(t *testing.T) {
	// 小图放大:2×1 纯红 → pad 到 10×10:画布 10×10,四角白底,中心红。
	src := solidRGBA(2, 1, color.RGBA{200, 0, 0, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "tiny_pad_10"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 10 || h != 10 {
		t.Fatalf("pad 输出尺寸应 10×10, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	if b := img.Bounds(); b.Dx() != 10 || b.Dy() != 10 {
		t.Fatalf("PNG 边界应 10×10: %v", b)
	}
	for _, pt := range []image.Point{{0, 0}, {9, 0}, {0, 9}, {9, 9}} {
		if got := at(img, pt.X, pt.Y); got != white {
			t.Fatalf("角 %v 应为白底, got %v", pt, got)
		}
	}
	if got := at(img, 5, 5); got.R != 200 || got.G != 0 || got.B != 0 || got.A != 255 {
		t.Fatalf("中心应为红色, got %v", got)
	}
}

func TestPadDownscalesLargeImage(t *testing.T) {
	// 大图缩小:100×50 纯蓝 → pad 到 20×20:输出 20×20,中心蓝,角白。
	src := solidRGBA(100, 50, color.RGBA{0, 0, 180, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "tiny_pad_20"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 20 || h != 20 {
		t.Fatalf("应 20×20, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	if got := at(img, 10, 10); got.B != 180 || got.R != 0 || got.G != 0 {
		t.Fatalf("中心应为蓝, got %v", got)
	}
	if got := at(img, 0, 0); got != white {
		t.Fatalf("角应白底, got %v", got)
	}
}

func TestPadNonProportionalCanvasCentersContent(t *testing.T) {
	// 非等比画布:10×40 纯绿 → pad 到 30×80(竖画布):scale=min(3,2)=2 →
	// 20×80 内容,左右各留 5px 白边,纵向满幅。
	src := solidRGBA(10, 40, color.RGBA{10, 200, 10, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "tall_pad_30x80"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 30 || h != 80 {
		t.Fatalf("应 30×80, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	green := func(c color.RGBA) bool { return c.R == 10 && c.G == 200 && c.B == 10 }
	if got := at(img, 15, 40); !green(got) {
		t.Fatalf("中心应为绿, got %v", got)
	}
	if got := at(img, 2, 40); got != white {
		t.Fatalf("左缘应白底, got %v", got)
	}
	if got := at(img, 27, 40); got != white {
		t.Fatalf("右缘应白底, got %v", got)
	}
	// 纵向满幅:上下缘在内容列内均为绿。
	if got := at(img, 15, 0); !green(got) {
		t.Fatalf("上缘内容列应为绿, got %v", got)
	}
	if got := at(img, 15, 79); !green(got) {
		t.Fatalf("下缘内容列应为绿, got %v", got)
	}
}

func TestCoverFillsAndCropsCenter(t *testing.T) {
	// cover:200×100 纯绿 → 100×100:等比缩放填满后居中裁切,整幅绿、无白边。
	src := solidRGBA(200, 100, color.RGBA{0, 160, 0, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "square_cover_100"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 100 || h != 100 {
		t.Fatalf("应 100×100, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	green := func(c color.RGBA) bool { return c.R == 0 && c.G == 160 && c.B == 0 }
	for _, pt := range []image.Point{{0, 0}, {99, 0}, {0, 99}, {99, 99}, {50, 50}} {
		if got := at(img, pt.X, pt.Y); !green(got) {
			t.Fatalf("cover 应填满无白边(含四角), %v got %v", pt, got)
		}
	}
}

func TestCoverDownscalesWideToSquare(t *testing.T) {
	// 大图缩小 cover:400×200 红 → 100×100:全幅红。
	src := solidRGBA(400, 200, color.RGBA{180, 0, 0, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "square_cover_100"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 100 || h != 100 {
		t.Fatalf("应 100×100, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	if got := at(img, 0, 0); got.R != 180 {
		t.Fatalf("角应红: %v", got)
	}
	if got := at(img, 99, 99); got.R != 180 {
		t.Fatalf("对角应红: %v", got)
	}
}

func TestPadFlattensTransparencyOntoWhite(t *testing.T) {
	// 透明通道:左半透明、右半不透明红,pad 到方形 → 左半白、右半红。
	src := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			if x < 10 {
				src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 0}) // 全透明
			} else {
				src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255}) // 不透明红
			}
		}
	}
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "square_pad_40"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 40 || h != 40 {
		t.Fatalf("应 40×40, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	if got := at(img, 12, 20); got != white {
		t.Fatalf("透明区应合成白底, got %v", got)
	}
	if got := at(img, 27, 20); got.R != 255 || got.G != 0 || got.B != 0 || got.A != 255 {
		t.Fatalf("不透明区应保持红色, got %v", got)
	}
}

func TestFitWidthKeepsProportionalHeight(t *testing.T) {
	// height=0 fit-width:100×50 → 100×50 等比直出(测试预设 1:1),
	// 真实矩阵见 presets_test 的 tmall_detail_w790 语义。
	src := solidRGBA(100, 50, color.RGBA{0, 0, 90, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "fit_w_100"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 100 || h != 50 {
		t.Fatalf("fit-width 应 100×50, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	if got := at(img, 0, 0); got.B != 90 {
		t.Fatalf("fit-width 无填充, 角应原色: %v", got)
	}
}

// ---- 输出门:格式 / 确定性 ---------------------------------------------------

func TestJPEGOutputFixedQuality(t *testing.T) {
	src := solidRGBA(64, 64, color.RGBA{120, 30, 200, 255})
	s := defaultsForTest(t)
	out, w, h, err := Adapt(src, mustPreset(t, s, "square_pad_64"), FormatJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 64 || h != 64 {
		t.Fatalf("应 64×64, got %dx%d", w, h)
	}
	if string(out[:3]) != "\xff\xd8\xff" {
		t.Fatalf("应是 JPEG 魔数: % x", out[:4])
	}
}

func TestDeterministicSameInputSameBytes(t *testing.T) {
	src := solidRGBA(33, 47, color.RGBA{1, 2, 3, 255})
	s := defaultsForTest(t)
	p := mustPreset(t, s, "tall_pad_30x80")
	sha := func(x []byte) string { h := sha256.Sum256(x); return hex.EncodeToString(h[:]) }
	a, _, _, err := Adapt(src, p, FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	b, _, _, err := Adapt(src, p, FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if sha(a) != sha(b) {
		t.Fatal("同输入同预设两次变换应字节级相同(确定性)")
	}
}

// ---- 输入门:拒绝路径 --------------------------------------------------------

func TestDecodePNGRejectsBadMagicAndCorruptData(t *testing.T) {
	if _, err := DecodePNG([]byte("not a png at all")); err != ErrBadMagic {
		t.Fatalf("非 PNG 魔数应 ErrBadMagic: %v", err)
	}
	// 魔数正确但内容损坏
	corrupt := append([]byte{}, encodePNG(t, solidRGBA(4, 4, color.RGBA{1, 2, 3, 255}))...)
	corrupt[len(corrupt)-10] ^= 0xFF
	corrupt[len(corrupt)-11] ^= 0xFF
	if _, err := DecodePNG(corrupt); err == nil {
		t.Fatal("损坏 PNG 应拒绝")
	}
}

func TestDecodePNGRejectsOversizeBytes(t *testing.T) {
	big := make([]byte, MaxInputBytes+1)
	copy(big, "\x89PNG\r\n\x1a\n")
	if _, err := DecodePNG(big); err != ErrTooLarge {
		t.Fatalf("超 8MiB 应 ErrTooLarge: %v", err)
	}
}

func TestDecodePNGRejectsHugeDeclaredPixels(t *testing.T) {
	// 字节很小但解码后 17000×1:单边超 16384 → 拒绝(防解压炸弹)。
	if _, err := DecodePNG(encodePNG(t, solidRGBA(17000, 1, color.RGBA{1, 1, 1, 255}))); err == nil {
		t.Fatal("解码后超大像素应拒绝")
	}
}

// ---- 预设集:加载 / 校验 / 内嵌矩阵 ------------------------------------------

func TestLoadPresetsValidates(t *testing.T) {
	good := `{"presets":[
		{"name":"a_main","width":800,"height":800,"mode":"pad","label":"A 主图"},
		{"name":"b_ad","width":1080,"height":1440,"mode":"cover","label":"B 广告"}
	]}`
	s, err := LoadPresets([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 2 {
		t.Fatalf("应有 2 个预设: %d", len(s.List()))
	}
	if _, ok := s.Get("a_main"); !ok {
		t.Fatal("应能按名取预设")
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("未知预设应 miss")
	}
	bad := []string{
		`{"presets":[]}`,
		`{"presets":[{"name":"A-Bad","width":800,"height":800,"mode":"pad"}]}`,
		`{"presets":[{"name":"x","width":0,"height":800,"mode":"pad"}]}`,
		`{"presets":[{"name":"x","width":99999,"height":800,"mode":"pad"}]}`,
		`{"presets":[{"name":"x","width":800,"height":800,"mode":"crop"}]}`,
		`{"presets":[{"name":"x","width":790,"height":0,"mode":"cover"}]}`,
		`{"presets":[{"name":"x","width":800,"height":0,"mode":"pad"},{"name":"x","width":800,"height":0,"mode":"pad"}]}`,
		`{garbage}`,
	}
	for i, raw := range bad {
		if _, err := LoadPresets([]byte(raw)); err == nil {
			t.Fatalf("非法预设集 #%d 应拒绝: %s", i, raw)
		}
	}
}

func TestDefaultPresetsMatrix(t *testing.T) {
	s, err := DefaultPresets()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		w, h int
		mode Mode
	}{
		"taobao_main":         {800, 800, ModePad},
		"tmall_main":          {800, 800, ModePad},
		"tmall_detail_w790":   {790, 0, ModePad},
		"jd_main":             {800, 800, ModePad},
		"pdd_main":            {750, 750, ModePad},
		"douyin_ad":           {1080, 1440, ModeCover},
		"xhs_cover":           {1080, 1440, ModeCover},
		"generic_square_1024": {1024, 1024, ModePad},
	}
	if len(s.List()) != len(want) {
		t.Fatalf("内嵌预设应 %d 个, got %d", len(want), len(s.List()))
	}
	for name, w := range want {
		p, ok := s.Get(name)
		if !ok {
			t.Fatalf("缺预设 %s", name)
		}
		if p.Width != w.w || p.Height != w.h || p.Mode != w.mode {
			t.Fatalf("%s 应为 %dx%d %s, got %+v", name, w.w, w.h, w.mode, p)
		}
	}
	// fit-width 语义实测:100×50 → tmall_detail_w790 = 790×395。
	out, w, h, err := Adapt(solidRGBA(100, 50, color.RGBA{9, 9, 9, 255}), mustPreset(t, s, "tmall_detail_w790"), FormatPNG)
	if err != nil {
		t.Fatal(err)
	}
	if w != 790 || h != 395 {
		t.Fatalf("tmall_detail_w790 等比应 790×395, got %dx%d", w, h)
	}
	img := decodeOut(t, out)
	if got := at(img, 0, 0); got.R != 9 {
		t.Fatalf("fit-width 无填充, 角应原色: %v", got)
	}
}

// ---- 零耦合红线 -------------------------------------------------------------

func TestNoForbiddenImports(t *testing.T) {
	// sizeadapt 包绝不 import platform/task/billing 相关包;
	// 允许面 = 标准库 + golang.org/x/image/*。go/parser 全量解析本包源文件。
	if err := checkImports("."); err != nil {
		t.Fatal(err)
	}
}

func checkImports(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if err := allowImport(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func allowImport(path string) error {
	if !strings.Contains(path, ".") { // 标准库首段不含点
		return nil
	}
	if path == "golang.org/x/image/draw" || path == "golang.org/x/image/math/f64" {
		return nil
	}
	for _, banned := range []string{"platform", "task", "billing"} {
		if strings.Contains(path, banned) {
			return fmt.Errorf("sizeadapt 禁止 import %s(零耦合红线)", path)
		}
	}
	return fmt.Errorf("sizeadapt import 白名单外: %s", path)
}
