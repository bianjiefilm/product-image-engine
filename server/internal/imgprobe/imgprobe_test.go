package imgprobe

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// 测试夹具纪律(HUI-1697 拍板):最小合法图片字节,绝不入真实照片。
// jpeg/png 由标准库编码器生成 1x1;webp 用公开的最小 VP8L 无损字节(34 字节)。

// minWebP1x1 公开已知的最小 1x1 webp(VP8L 无损),已验证可被 golang.org/x/image/webp 解码。
var minWebP1x1, _ = base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")

func encode1x1(t *testing.T, f string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0x7f, G: 0x88, B: 0x99, A: 0xff})
	var buf bytes.Buffer
	var err error
	if f == "jpeg" {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatalf("编码 %s 夹具失败: %v", f, err)
	}
	return buf.Bytes()
}

func TestSniffMagicBytes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
		ok   bool
	}{
		{"jpeg 头", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg", true},
		{"png 头", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}, "image/png", true},
		{"webp 头", []byte("RIFF\xf0\x0d\x00\x00WEBPVK\xBA"), "image/webp", true},
		{"纯文本", []byte("hello world, definitely not an image"), "", false},
		{"空字节", nil, "", false},
		{"短伪 JPEG", []byte{0xFF, 0xD8}, "", false},
		{"RIFF 但非 WEBP", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Sniff(c.data)
			if ok != c.ok || got != c.want {
				t.Fatalf("Sniff = (%q, %v), 期望 (%q, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestProbeDecodesMinimalFixtures(t *testing.T) {
	cases := []struct {
		name      string
		data      []byte
		mediaType string
	}{
		{"最小 jpeg", encode1x1(t, "jpeg"), "image/jpeg"},
		{"最小 png", encode1x1(t, "png"), "image/png"},
		{"最小 webp", minWebP1x1, "image/webp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta, err := Probe(c.data)
			if err != nil {
				t.Fatalf("Probe 失败: %v", err)
			}
			if meta.MediaType != c.mediaType {
				t.Fatalf("MediaType = %q, 期望 %q", meta.MediaType, c.mediaType)
			}
			if meta.WidthPx != 1 || meta.HeightPx != 1 {
				t.Fatalf("尺寸 = %dx%d, 期望 1x1", meta.WidthPx, meta.HeightPx)
			}
			t.Logf("夹具证据: size=%d sha256=%s media=%s",
				len(c.data), sha256Hex(c.data), meta.MediaType)
		})
	}
}

func TestProbeRejectsForgedContent(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"文本冒充 .jpg(扩展名不作数)", []byte("<html>not an image at all</html>"), ErrUnsupported},
		{"jpeg 魔数+坏字节", append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte{0x00}, 64)...), ErrUndecodable},
		{"png 魔数+坏字节", append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0xAA}, 64)...), ErrUndecodable},
		{"webp 头+损坏载荷", append([]byte("RIFF\x24\x00\x00\x00WEBPVP8L\x16\x00\x00\x00"), bytes.Repeat([]byte{0x5A}, 64)...), ErrUndecodable},
		{"空上传", nil, ErrUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Probe(c.data)
			if err == nil {
				t.Fatalf("期望拒绝,实际通过")
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("错误分类 = %v, 期望属于 %v", err, c.want)
			}
		})
	}
}

// TestProbeSniffDecodeMismatch 魔数与实际格式不符(如 JPEG 头接 PNG 体)→ 拒绝。
func TestProbeSniffDecodeMismatch(t *testing.T) {
	data := append([]byte{0xFF, 0xD8, 0xFF}, encode1x1(t, "png")...)
	if _, err := Probe(data); err == nil {
		t.Fatalf("魔数声明与实际解码格式不一致,期望拒绝")
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
