package bgreplace

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

const (
	// OriginSubjectLock 是保真合成结果。主体像素来自原图，不是模型出图，也不是可交付候选。
	OriginSubjectLock = "subject_lock"
	// SubjectLockNotice 说明像素已锁回，商品检查仍未完成。
	SubjectLockNotice = "主体像素已锁回，商品检查未完成"
	// SubjectLockNotModel 禁止把本地合成说成模型出图。
	SubjectLockNotModel = "这不是模型出图"
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

func decodePNG(raw []byte) (image.Image, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errorsNew("图片不是 PNG")
	}
	return img, nil
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
