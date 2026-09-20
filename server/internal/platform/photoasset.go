package platform

// HUI-1697 FEAT-0198:产品照片上传(经 I0 既有 UploadClient 配置与 fail-closed 纪律)。
// 接入面按 public-ai platform-upload 真实内部形状(2026-09-20 侦察):
//   1. POST /internal/v1/upload/sessions   —— 申报 sha256/size/属主 principal 创建会话;
//   2. POST …/parts/{n}/sign + PUT 预签名地址 —— 服务端整文件分片中继(v1 不做客户端断点续传,
//      失败重传走本产品内容幂等,见 httpapi 层);
//   3. POST …/complete                     —— 平台按对象头核验 sha256/size,不符明确拒绝;
//   4. 解析侧:GET assets/{id} + POST download-url + 预签名 GET 拉取真实字节。
// 属主 principal 来自本产品已验证身份(JWT sub),绝不采信客户端自报。
// 本文件不改 clients.go 的 I0/I1 PROVISIONAL 面(关系见票报告 deferred)。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PhotoPrincipal 平台资产属主 principal(本产品只转发已验证平台身份事实)。
type PhotoPrincipal struct {
	Type string // 平台侧 principal_type(当前口径 "user")
	ID   string // 平台侧 principal_id(JWT sub,usr_…)
}

// PhotoSession 创建会话应答事实。
type PhotoSession struct {
	AssetID   string
	SessionID string
	PartSize  int64
}

// UploadPart 单分片登记事实(平台 complete 线格式)。
type UploadPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

// PhotoAssetMeta 资产元数据(平台以 Go 字段名返回)。
type PhotoAssetMeta struct {
	AssetID     string
	SHA256      string
	SizeBytes   int64
	ContentType string
	Status      string
}

func (c *UploadClient) photoHTTP() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// 服务端整文件中继:按内容上限放宽超时(20MB 量级,内网 loopback)。
	return &http.Client{Timeout: 120 * time.Second}
}

func (c *UploadClient) photoHeaders(p PhotoPrincipal) (http.Header, error) {
	if c.BaseURL == "" || c.Token == "" {
		return nil, fmt.Errorf("%w: upload 未配置(BASE_URL/TOKEN 为空)", ErrUnavailable)
	}
	if p.Type == "" || p.ID == "" {
		return nil, fmt.Errorf("photo upload: 属主 principal 缺失(绝不采信客户端自报之外的空值)")
	}
	h := http.Header{}
	h.Set("X-App-ID", c.AppID)
	h.Set(internalTokenHeader, c.Token)
	h.Set("X-Principal-Type", p.Type)
	h.Set("X-Principal-ID", p.ID)
	return h, nil
}

// doJSON 平台内部面调用:网络/5xx → ErrUnavailable;其余非 2xx → 平台拒绝(带状态码)。
func (c *UploadClient) doJSON(ctx context.Context, method, path string, hdr http.Header, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.photoHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return fmt.Errorf("%w: %s: status %d", ErrUnavailable, path, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", path, res.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// CreatePhotoSession 预创建上传会话(申报事实:属主/文件名/媒体类型/sha256/size)。
func (c *UploadClient) CreatePhotoSession(ctx context.Context, p PhotoPrincipal,
	filename, mediaType string, size int64, sha256 string) (PhotoSession, error) {
	hdr, err := c.photoHeaders(p)
	if err != nil {
		return PhotoSession{}, err
	}
	raw, err := json.Marshal(map[string]any{
		"app_id": c.AppID, "principal_type": p.Type, "principal_id": p.ID,
		"filename": filename, "content_type": mediaType,
		"sha256": sha256, "size_bytes": size,
	})
	if err != nil {
		return PhotoSession{}, err
	}
	var out struct {
		AssetID   string `json:"asset_id"`
		SessionID string `json:"session_id"`
		PartSize  int64  `json:"part_size"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/internal/v1/upload/sessions", hdr, raw, &out); err != nil {
		return PhotoSession{}, err
	}
	if out.AssetID == "" || out.SessionID == "" || out.PartSize <= 0 {
		return PhotoSession{}, fmt.Errorf("%w: upload session 应答缺 asset_id/session_id/part_size(不伪造登记)", ErrUnavailable)
	}
	return PhotoSession{AssetID: out.AssetID, SessionID: out.SessionID, PartSize: out.PartSize}, nil
}

// signPhotoPart 取分片预签名地址与随附头。
func (c *UploadClient) signPhotoPart(ctx context.Context, hdr http.Header, sessionID string, n int) (string, map[string]string, error) {
	var out struct {
		UploadURL string            `json:"upload_url"`
		Headers   map[string]string `json:"headers"`
	}
	path := "/internal/v1/upload/sessions/" + url.PathEscape(sessionID) + "/parts/" + itoa(n) + "/sign"
	if err := c.doJSON(ctx, http.MethodPost, path, hdr, []byte("{}"), &out); err != nil {
		return "", nil, err
	}
	if out.UploadURL == "" {
		return "", nil, fmt.Errorf("%w: 分片预签名应答缺 upload_url", ErrUnavailable)
	}
	return out.UploadURL, out.Headers, nil
}

// putPhotoPart 把分片字节 PUT 到预签名地址,捕获 ETag(缺 ETag 视为失败,不伪造)。
func (c *UploadClient) putPhotoPart(ctx context.Context, uploadURL string, headers map[string]string, chunk []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(chunk))
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.photoHTTP().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: 分片 PUT: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("%w: 分片 PUT: status %d", ErrUnavailable, res.StatusCode)
	}
	etag := strings.TrimSpace(res.Header.Get("ETag"))
	if etag == "" {
		return "", fmt.Errorf("%w: 分片 PUT 应答缺 ETag(不伪造分片事实)", ErrUnavailable)
	}
	return etag, nil
}

// CompletePhotoSession 完成会话:平台核验对象头 sha256/size,不符即拒(明确失败,不静默)。
func (c *UploadClient) CompletePhotoSession(ctx context.Context, hdr http.Header,
	sessionID, sha256 string, parts []UploadPart) error {
	raw, err := json.Marshal(map[string]any{"parts": parts, "sha256": sha256})
	if err != nil {
		return err
	}
	path := "/internal/v1/upload/sessions/" + url.PathEscape(sessionID) + "/complete"
	return c.doJSON(ctx, http.MethodPost, path, hdr, raw, nil)
}

// AbortPhotoSession 尽力中止失败会话(错误只入日志语义,不影响主错误流)。
func (c *UploadClient) AbortPhotoSession(ctx context.Context, hdr http.Header, sessionID string) {
	path := "/internal/v1/upload/sessions/" + url.PathEscape(sessionID) + "/abort"
	_ = c.doJSON(ctx, http.MethodPost, path, hdr, []byte("{}"), nil)
}

// RegisterPhoto 服务端整文件中继登记:创建会话 → 按会话 part_size 分片直传 → 完成核验。
// 任一步失败:尽力中止会话并返回错误;资产在平台侧保持未完成状态(pending),
// 绝不会成为可解析内容(与"上传未完成不得成为生成输入"同构)。
// 返回平台 asset_id。
func (c *UploadClient) RegisterPhoto(ctx context.Context, p PhotoPrincipal,
	filename, mediaType string, data []byte, sha256Hex string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("photo register: 空内容本地即拒,不触网")
	}
	hdr, err := c.photoHeaders(p)
	if err != nil {
		return "", err
	}
	sess, err := c.CreatePhotoSession(ctx, p, filename, mediaType, int64(len(data)), sha256Hex)
	if err != nil {
		return "", err
	}
	parts := make([]UploadPart, 0, int64(len(data))/sess.PartSize+1)
	for off, n := 0, 1; off < len(data); off, n = off+int(sess.PartSize), n+1 {
		end := off + int(sess.PartSize)
		if end > len(data) {
			end = len(data)
		}
		chunk := data[off:end]
		sum := sha256.Sum256(chunk)
		uploadURL, extra, err := c.signPhotoPart(ctx, hdr, sess.SessionID, n)
		if err != nil {
			c.AbortPhotoSession(ctx, hdr, sess.SessionID)
			return "", err
		}
		etag, err := c.putPhotoPart(ctx, uploadURL, extra, chunk)
		if err != nil {
			c.AbortPhotoSession(ctx, hdr, sess.SessionID)
			return "", err
		}
		parts = append(parts, UploadPart{
			PartNumber: n, ETag: etag, SizeBytes: int64(len(chunk)),
			SHA256: hex.EncodeToString(sum[:]),
		})
	}
	if err := c.CompletePhotoSession(ctx, hdr, sess.SessionID, sha256Hex, parts); err != nil {
		c.AbortPhotoSession(ctx, hdr, sess.SessionID)
		return "", err
	}
	return sess.AssetID, nil
}

// PhotoAsset 读取资产元数据(受限解析第一步;属主之外平台侧明确拒绝)。
func (c *UploadClient) PhotoAsset(ctx context.Context, p PhotoPrincipal, assetID string) (PhotoAssetMeta, error) {
	hdr, err := c.photoHeaders(p)
	if err != nil {
		return PhotoAssetMeta{}, err
	}
	var out struct {
		Asset PhotoAssetMeta `json:"asset"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/internal/v1/upload/assets/"+url.PathEscape(assetID), hdr, nil, &out); err != nil {
		return PhotoAssetMeta{}, err
	}
	if out.Asset.AssetID == "" {
		return PhotoAssetMeta{}, fmt.Errorf("%w: 资产应答缺 AssetID(不伪造解析事实)", ErrUnavailable)
	}
	return out.Asset, nil
}

// PhotoDownloadURL 取受限下载地址(ttlSeconds<=0 用平台缺省)。
func (c *UploadClient) PhotoDownloadURL(ctx context.Context, p PhotoPrincipal, assetID string, ttlSeconds int) (string, string, int64, error) {
	hdr, err := c.photoHeaders(p)
	if err != nil {
		return "", "", 0, err
	}
	raw, _ := json.Marshal(map[string]any{"ttl_seconds": ttlSeconds})
	var out struct {
		DownloadURL   string `json:"download_url"`
		ContentType   string `json:"content_type"`
		ContentLength int64  `json:"content_length"`
	}
	path := "/internal/v1/upload/assets/" + url.PathEscape(assetID) + "/download-url"
	if err := c.doJSON(ctx, http.MethodPost, path, hdr, raw, &out); err != nil {
		return "", "", 0, err
	}
	if out.DownloadURL == "" {
		return "", "", 0, fmt.Errorf("%w: 下载地址应答缺 download_url", ErrUnavailable)
	}
	return out.DownloadURL, out.ContentType, out.ContentLength, nil
}

// OpenPhotoContent 拉取真实字节(经平台预签名地址);调用方负责 Close。
func (c *UploadClient) OpenPhotoContent(ctx context.Context, downloadURL string) (io.ReadCloser, string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	res, err := c.photoHTTP().Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("%w: 下载内容: %v", ErrUnavailable, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		return nil, "", 0, fmt.Errorf("%w: 下载内容: status %d", ErrUnavailable, res.StatusCode)
	}
	return res.Body, res.Header.Get("Content-Type"), res.ContentLength, nil
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
