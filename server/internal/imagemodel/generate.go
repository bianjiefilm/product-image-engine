// Package imagemodel 向已配置的图像端点提交一次生成，并只接受能解码的图片字节。
// 回环地址上的字节可以用于测试，但不能算供应商出图。本包不写账，也不把调用标成已扣费。
package imagemodel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	minCanvasPixels = 262144
	maxCanvasPixels = 4194304

	// SizeMismatchNote 只说明输出像素和项目画布不一致。它不是保真通过，也不是电商尺寸验收。
	SizeMismatchNote = "输出尺寸与项目不一致"
	// SizeOutOfDomainNote 说明项目尺寸不在模型像素域，请求没有改用这个尺寸。
	SizeOutOfDomainNote = "项目尺寸不在模型像素域"
)

// CanvasSize 只在宽高都为正，且像素数落在模型自由像素域时返回 WxH。
// 域外尺寸返回空串，调用方继续使用端点默认尺寸，不能把空串说成已按电商尺寸出图。
func CanvasSize(w, h int) string {
	if w < 1 || h < 1 || w > 4096 || h > 4096 {
		return ""
	}
	px := w * h
	if px < minCanvasPixels || px > maxCanvasPixels {
		return ""
	}
	return strconv.Itoa(w) + "x" + strconv.Itoa(h)
}

// SizeNotes 只在项目写了尺寸时返回说明。尺寸一致时返回空，不表示保真、计费或生产通过。
func SizeNotes(projectW, projectH, outputW, outputH int) []string {
	if projectW < 1 || projectH < 1 {
		return nil
	}
	if CanvasSize(projectW, projectH) == "" {
		return []string{SizeOutOfDomainNote}
	}
	if outputW != projectW || outputH != projectH {
		return []string{SizeMismatchNote}
	}
	return nil
}

// Request 是一次图像生成。凭证只放在发往生成端点的请求头里。
// Refs 是可选参考图。没有参考图时 JSON 不带 image；有则 image 为 data URL 数组。
type Request struct {
	Endpoint   string
	Credential string
	Model      string
	Prompt     string
	Size       string
	Refs       [][]byte
}

// Result 是解码后的图片。BillingPassed 恒为 false。
type Result struct {
	Bytes         []byte
	SHA256        string
	MediaType     string
	HostLive      bool
	BillingPassed bool
	StatusCode    int
	WidthPx       int
	HeightPx      int
}

// Generate 向端点 POST 一次。端点路径不是 /images/generations 时补上这一段。
// 响应给 url 时再去取字节，且不把凭证带到另一个主机。
func Generate(ctx context.Context, client *http.Client, req Request) (Result, error) {
	if strings.TrimSpace(req.Credential) == "" {
		return Result{}, errors.New("配置缺失")
	}
	if strings.TrimSpace(req.Model) == "" {
		return Result{}, errors.New("模型名缺失")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, errors.New("提示词为空")
	}
	endpoint, host, err := generationURL(req.Endpoint)
	if err != nil {
		return Result{}, err
	}
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	size := strings.TrimSpace(req.Size)
	if size == "" {
		size = "1024x1024"
	}
	images, err := referenceDataURLs(req.Refs)
	if err != nil {
		return Result{}, err
	}
	body := map[string]any{
		"model":           strings.TrimSpace(req.Model),
		"prompt":          req.Prompt,
		"size":            size,
		"n":               1,
		"response_format": "b64_json",
	}
	if len(images) > 0 {
		body["image"] = images
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{}, errors.New("端点缺失")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.Credential))
	resp, err := client.Do(httpReq)
	if err != nil {
		return Result{}, errors.New("供应商不可达")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return Result{}, errors.New("供应商响应读失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("供应商拒绝: http_%d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Data) == 0 {
		return Result{}, errors.New("供应商没有返回图片")
	}
	item := parsed.Data[0]
	var picture []byte
	switch {
	case strings.TrimSpace(item.B64JSON) != "":
		picture, err = base64.StdEncoding.DecodeString(strings.TrimSpace(item.B64JSON))
		if err != nil {
			return Result{}, errors.New("图片无法解码")
		}
	case strings.TrimSpace(item.URL) != "":
		picture, err = fetchImage(ctx, client, strings.TrimSpace(item.URL))
		if err != nil {
			return Result{}, err
		}
	default:
		return Result{}, errors.New("供应商没有返回图片")
	}
	media, err := sniffImage(picture)
	if err != nil {
		return Result{}, err
	}
	width, height := 0, 0
	if cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(picture)); cfgErr == nil {
		width, height = cfg.Width, cfg.Height
	}
	sum := sha256.Sum256(picture)
	return Result{
		Bytes:         picture,
		SHA256:        hex.EncodeToString(sum[:]),
		MediaType:     media,
		HostLive:      !loopbackHost(host),
		BillingPassed: false,
		StatusCode:    resp.StatusCode,
		WidthPx:       width,
		HeightPx:      height,
	}, nil
}

func referenceDataURLs(refs [][]byte) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		media, err := sniffImage(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, "data:"+media+";base64,"+base64.StdEncoding.EncodeToString(ref))
	}
	return out, nil
}

func generationURL(endpoint string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", errors.New("端点缺失")
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/images/generations") {
		u.Path = strings.TrimRight(u.Path, "/") + "/images/generations"
	}
	return u.String(), u.Host, nil
}

func fetchImage(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("供应商没有返回图片")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("供应商没有返回图片")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("供应商不可达")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, errors.New("供应商响应读失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || len(body) == 0 {
		return nil, errors.New("供应商没有返回图片")
	}
	return body, nil
}

func sniffImage(raw []byte) (string, error) {
	if _, err := png.Decode(bytes.NewReader(raw)); err == nil {
		return "image/png", nil
	}
	if _, err := jpeg.Decode(bytes.NewReader(raw)); err == nil {
		return "image/jpeg", nil
	}
	if _, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
		return "image/png", nil
	}
	return "", errors.New("图片无法解码")
}

func loopbackHost(host string) bool {
	h := host
	if name, _, err := net.SplitHostPort(host); err == nil {
		h = name
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
