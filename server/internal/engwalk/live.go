package engwalk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
)

// LiveFact 只记录状态码、字节长度、摘要和错误类。不含凭证。
type LiveFact struct {
	Passed     bool   `json:"passed"`
	StatusCode int    `json:"status_code"`
	ByteLen    int    `json:"byte_len"`
	SHA256     string `json:"sha256"`
	ErrorClass string `json:"error_class"`
}

var liveClient = &http.Client{Timeout: 10 * time.Second}

func liveFromEnv() LiveFact {
	return liveAttempt(config.Load())
}

func liveAttempt(cfg config.Config) LiveFact {
	text := strings.TrimSpace(cfg.TextImageModelCredential)
	bg := strings.TrimSpace(cfg.BgModelCredential)
	light := strings.TrimSpace(cfg.LightModelCredential)
	if text == "" && bg == "" && light == "" {
		return LiveFact{ErrorClass: "配置缺失"}
	}
	endpoint := pairedModelEndpoint(cfg)
	if endpoint == "" {
		return LiveFact{ErrorClass: "端点缺失"}
	}
	return performLive(context.Background(), endpoint)
}

// pairedModelEndpoint 返回第一组成对的模型地址。
// 凭证和地址必须同时非空，且属于同一模式。单独的地址或单独的凭证都不返回。
func pairedModelEndpoint(cfg config.Config) string {
	pairs := [][2]string{
		{cfg.TextImageModelCredential, cfg.TextImageModelURL},
		{cfg.BgModelCredential, cfg.BgModelURL},
		{cfg.LightModelCredential, cfg.LightModelURL},
	}
	for _, pair := range pairs {
		if strings.TrimSpace(pair[0]) != "" && strings.TrimSpace(pair[1]) != "" {
			return strings.TrimSpace(pair[1])
		}
	}
	return ""
}

func performLive(ctx context.Context, endpoint string) LiveFact {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return LiveFact{ErrorClass: "request"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return LiveFact{ErrorClass: "request"}
	}
	resp, err := liveClient.Do(req)
	if err != nil {
		return LiveFact{ErrorClass: "transport"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return LiveFact{StatusCode: resp.StatusCode, ErrorClass: "read"}
	}
	return judgeLive(u.Host, resp.StatusCode, body)
}

func judgeLive(host string, status int, body []byte) LiveFact {
	fact := LiveFact{StatusCode: status, ByteLen: len(body)}
	if len(body) > 0 {
		sum := sha256.Sum256(body)
		fact.SHA256 = hex.EncodeToString(sum[:])
	}
	if status != http.StatusOK || !decodablePNG(body) {
		fact.Passed = false
		switch {
		case len(body) == 0:
			fact.ErrorClass = "empty_body"
		case status != http.StatusOK:
			fact.ErrorClass = httpFailure(status)
		default:
			fact.ErrorClass = "undecodable"
		}
		return fact
	}
	if loopbackHost(host) {
		fact.Passed = false
		fact.ErrorClass = "loopback"
		return fact
	}
	fact.Passed = true
	return fact
}

func decodablePNG(body []byte) bool {
	_, err := openPNG(body)
	return err == nil
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
