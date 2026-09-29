package textimage

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// PaintLocalTest 画出一张可解码的测试图。它由服务端调用，不是平台任务状态，也不是模型结果。
func PaintLocalTest(prompt string) ([]byte, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, ErrPromptRequired
	}
	sum := sha256.Sum256([]byte(prompt))
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: sum[x], G: sum[y], B: sum[(x+y)%len(sum)], A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
