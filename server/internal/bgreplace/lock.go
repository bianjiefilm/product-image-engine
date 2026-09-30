package bgreplace

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
)

const (
	// OriginSubjectLock 是保真合成结果。主体像素来自原图，不是模型出图，也不是可交付候选。
	OriginSubjectLock = "subject_lock"
	// SubjectLockNotice 说明像素已锁回，商品检查仍未完成。
	SubjectLockNotice = "主体像素已锁回，商品检查未完成"
	// SubjectLockNotModel 禁止把本地合成说成模型出图。
	SubjectLockNotModel = "这不是模型出图"
	// OriginModelPlate 是模型背景板加上锁回的主体像素。不是计费通过，也不是生产出图通过。
	OriginModelPlate = "model_plate_lock"
	// ModelPlateNotice 说明背景来自模型，主体像素仍与原图一致。
	ModelPlateNotice = "模型背景已换上，主体像素与原图一致"
)

// LockSubject 用蒙版把原图主体锁回，只替换蒙版以外的像素。
// 蒙版 alpha >= 128 的像素保持原图。没有保护像素、尺寸不一致，或背景没有变化时拒绝。
func LockSubject(original, plate, mask []byte) ([]byte, error) {
	src, err := decodePNG(original)
	if err != nil {
		return nil, err
	}
	bg, err := decodePNG(plate)
	if err != nil {
		return nil, err
	}
	matte, err := decodePNG(mask)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	if bounds != bg.Bounds() || bounds != matte.Bounds() {
		return nil, errorsNew("尺寸不一致")
	}
	out := image.NewNRGBA(bounds)
	protected := 0
	changed := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			op := nrgbaAt(src, x, y)
			bp := nrgbaAt(bg, x, y)
			if nrgbaAt(matte, x, y).A >= 128 {
				protected++
				out.SetNRGBA(x, y, op)
				continue
			}
			out.SetNRGBA(x, y, bp)
			if bp != op {
				changed++
			}
		}
	}
	if protected == 0 {
		return nil, errorsNew("主体蒙版没有保护任何像素")
	}
	if changed == 0 {
		return nil, errorsNew("蒙版盖住了整张图，背景没有换")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ModelPlatePrompt 只描述背景。不把商品图交给模型重画。
func ModelPlatePrompt(intent string) string {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		intent = "纯色背景"
	}
	return "只生成背景，不要商品、包装、文字或标志。背景：" + intent
}

// FitPlate 把模型底板缩放到原图尺寸。尺寸已一致时返回原字节。
func FitPlate(plate []byte, width, height int) ([]byte, error) {
	if width < 1 || height < 1 {
		return nil, errorsNew("尺寸不一致")
	}
	src, format, err := decodePlate(plate)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	if format == "png" && bounds.Dx() == width && bounds.Dy() == height {
		return plate, nil
	}
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return nil, errorsNew("图片不是 PNG")
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := bounds.Min.Y + y*bounds.Dy()/height
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/width
			out.SetNRGBA(x, y, nrgbaAt(src, sx, sy))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MaskReady 在调用模型前确认蒙版能保住一部分主体，并且留出要替换的背景。
func MaskReady(original, mask []byte) error {
	src, err := decodePNG(original)
	if err != nil {
		return err
	}
	matte, err := decodePNG(mask)
	if err != nil {
		return err
	}
	if src.Bounds() != matte.Bounds() {
		return errorsNew("尺寸不一致")
	}
	protected := 0
	total := 0
	bounds := src.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			total++
			if nrgbaAt(matte, x, y).A >= 128 {
				protected++
			}
		}
	}
	if protected == 0 {
		return errorsNew("主体蒙版没有保护任何像素")
	}
	if protected == total {
		return errorsNew("蒙版盖住了整张图")
	}
	return nil
}

// SubjectPixelChecks 只在蒙版内像素与原图完全一致时把四项标成通过。不记录相似度。
func SubjectPixelChecks(original, result, mask []byte) (ProductChecks, error) {
	src, err := decodePNG(original)
	if err != nil {
		return ProductChecks{}, err
	}
	got, err := decodePNG(result)
	if err != nil {
		return ProductChecks{}, err
	}
	matte, err := decodePNG(mask)
	if err != nil {
		return ProductChecks{}, err
	}
	if src.Bounds() != got.Bounds() || src.Bounds() != matte.Bounds() {
		return ProductChecks{}, errorsNew("尺寸不一致")
	}
	protected := 0
	bounds := src.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if nrgbaAt(matte, x, y).A < 128 {
				continue
			}
			protected++
			if nrgbaAt(src, x, y) != nrgbaAt(got, x, y) {
				return ProductChecks{}, errorsNew("主体像素与原图不一致")
			}
		}
	}
	if protected == 0 {
		return ProductChecks{}, errorsNew("主体蒙版没有保护任何像素")
	}
	return ProductChecks{
		Logo: QualityPass, PackagingText: QualityPass, Spec: QualityPass, Structure: QualityPass,
	}, nil
}

func decodePNG(raw []byte) (image.Image, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errorsNew("图片不是 PNG")
	}
	return img, nil
}

func decodePlate(raw []byte) (image.Image, string, error) {
	if img, err := png.Decode(bytes.NewReader(raw)); err == nil {
		return img, "png", nil
	}
	if img, err := jpeg.Decode(bytes.NewReader(raw)); err == nil {
		return img, "jpeg", nil
	}
	return nil, "", errorsNew("图片不是 PNG")
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
