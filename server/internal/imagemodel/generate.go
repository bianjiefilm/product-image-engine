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
	"strings"
	"time"
)

// Request 是一次图像生成。凭证只放在发往生成端点的请求头里。
type Request struct {
	Endpoint   string
	Credential string
	Model      string
	Prompt     string
	Size       string
}

// Result 是解码后的图片。BillingPassed 恒为 false。
type Result struct {
	Bytes         []byte
	SHA256        string
	MediaType     string
	HostLive      bool
	BillingPassed bool
	StatusCode    int
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
	payload, err := json.Marshal(map[string]any{
		"model":           strings.TrimSpace(req.Model),
		"prompt":          req.Prompt,
		"size":            size,
		"n":               1,
		"response_format": "b64_json",
	})
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
	sum := sha256.Sum256(picture)
	return Result{
		Bytes:         picture,
		SHA256:        hex.EncodeToString(sum[:]),
		MediaType:     media,
		HostLive:      !loopbackHost(host),
		BillingPassed: false,
		StatusCode:    resp.StatusCode,
	}, nil
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
