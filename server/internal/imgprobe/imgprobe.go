// Package imgprobe 对上传图片做服务端内容核验:魔数嗅探 + 真解码。
// 纪律(HUI-1697 FEAT-0198):客户端声明的扩展名/Content-Type 一律不作数,
// 格式与尺寸事实只认服务端嗅探与实际解码结果;坏文件明确拒绝(机器错误码)。
package imgprobe

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	_ "golang.org/x/image/webp" // 注册 webp 解码器(纯 Go)
	_ "image/jpeg"              // 注册 jpeg 解码器
	_ "image/png"               // 注册 png 解码器
)

var (
	// ErrUnsupported 魔数不属于受支持格式(jpeg/png/webp),或空内容。
	ErrUnsupported = errors.New("imgprobe: 不支持的图片格式")
	// ErrUndecodable 魔数受支持但内容无法完整解码,或解码格式与魔数不符。
	ErrUndecodable = errors.New("imgprobe: 图片内容无法解码")
)

// Meta 服务端核验得到的图片事实。
type Meta struct {
	MediaType string // image/jpeg | image/png | image/webp
	Format    string // jpeg | png | webp
	WidthPx   int
	HeightPx  int
}

// Sniff 按魔数判别格式;不认识一律 false(嗅探前缀不足以判定时同样拒绝)。
func Sniff(head []byte) (mediaType string, ok bool) {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "image/jpeg", true
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png", true
	case len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return "image/webp", true
	default:
		return "", false
	}
}

// Probe 全链核验:魔数嗅探 → 真解码 → 格式一致性比对。
// 任何一步失败都返回机器可分类错误(ErrUnsupported / ErrUndecodable)。
func Probe(data []byte) (Meta, error) {
	mediaType, ok := Sniff(data)
	if !ok {
		return Meta{}, ErrUnsupported
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %v", ErrUndecodable, err)
	}
	if "image/"+format != mediaType {
		return Meta{}, fmt.Errorf("%w: 魔数声明 %s 但实际解码为 %s", ErrUndecodable, mediaType, format)
	}
	bounds := img.Bounds()
	return Meta{
		MediaType: mediaType,
		Format:    format,
		WidthPx:   bounds.Dx(),
		HeightPx:  bounds.Dy(),
	}, nil
}
