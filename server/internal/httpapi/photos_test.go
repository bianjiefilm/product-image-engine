package httpapi

// HUI-1697 FEAT-0198:产品照片上传 端到端机器测试。
// 覆盖:登记制开关、服务端单点校验链(主体/租户/用途/大小/魔数/真解码)、
// 内容幂等(不重复登记不重复计费)、A/B 租户交叉拒绝、工程挂接+重登恢复、
// 跨应用版本引用经平台设施解析(不读他库)、完整性复检、日志脱敏。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
)

// ---- 桩:platform-upload(真实 session 面) ---------------------------------

type photoUpStub struct {
	srv *httptest.Server

	mu           sync.Mutex
	sessions     int
	creates      []map[string]any
	lastHeaders  http.Header
	chunks       map[int][]byte
	ready        map[string][]byte
	metaSha      map[string]string
	metaCt       map[string]string
	aborts       int
	completes    int
	failComplete bool
	dlFetches    int
}

func newPhotoUpStub(t *testing.T) *photoUpStub {
	t.Helper()
	st := &photoUpStub{chunks: map[int][]byte{}, ready: map[string][]byte{},
		metaSha: map[string]string{}, metaCt: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/upload/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.sessions++
		n := st.sessions
		st.creates = append(st.creates, body)
		st.lastHeaders = r.Header.Clone()
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "asset_id": "ast_" + strconv.Itoa(n),
			"session_id": "ses_" + strconv.Itoa(n), "part_size": 1 << 20,
		})
	})
	mux.HandleFunc("POST /internal/v1/upload/sessions/{sid}/parts/{n}/sign", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "part_number": n,
			"upload_url": st.srv.URL + "/oss/" + r.PathValue("sid") + "/" + strconv.Itoa(n),
			"headers":    map[string]string{},
		})
	})
	mux.HandleFunc("PUT /oss/{sid}/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		b, _ := io.ReadAll(r.Body)
		st.mu.Lock()
		st.chunks[n] = b
		st.mu.Unlock()
		w.Header().Set("ETag", strconv.Quote(strconv.Itoa(n)))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /internal/v1/upload/sessions/{sid}/complete", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.failComplete {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"upload_sha_mismatch"}}`))
			return
		}
		var body struct {
			SHA256 string `json:"sha256"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.completes++
		assetID := "ast_" + strconv.Itoa(st.sessions)
		var data []byte
		for i := 1; ; i++ {
			ch, ok := st.chunks[i]
			if !ok {
				break
			}
			data = append(data, ch...)
		}
		ct, _ := st.creates[st.sessions-1]["content_type"].(string)
		st.ready[assetID] = data
		st.metaSha[assetID] = body.SHA256
		st.metaCt[assetID] = ct
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": map[string]any{"AssetID": assetID, "Status": "ready"}})
	})
	mux.HandleFunc("POST /internal/v1/upload/sessions/{sid}/abort", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.aborts++
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /internal/v1/upload/assets/{aid}", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		id := r.PathValue("aid")
		data, ok := st.ready[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"upload_not_found"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": map[string]any{
			"AssetID": id, "SHA256": st.metaSha[id], "SizeBytes": len(data),
			"ContentType": st.metaCt[id], "Status": "ready",
		}})
	})
	mux.HandleFunc("POST /internal/v1/upload/assets/{aid}/download-url", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("aid")
		st.mu.Lock()
		data, size := st.ready[id], int64(0)
		if b, ok := st.ready[id]; ok {
			size = int64(len(b))
		}
		_ = data
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "download_url": st.srv.URL + "/dl/" + id,
			"content_type": st.metaCt[id], "content_length": size,
		})
	})
	mux.HandleFunc("GET /dl/{aid}", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.dlFetches++
		data, ok := st.ready[r.PathValue("aid")]
		st.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
	st.srv = httptest.NewServer(mux)
	t.Cleanup(st.srv.Close)
	return st
}

// Preload 模拟“他应用经授权登记过的资产”(跨应用版本引用解析用)。
func (st *photoUpStub) Preload(id string, data []byte, ct string) {
	sum := sha256.Sum256(data)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.ready[id] = data
	st.metaSha[id] = hex.EncodeToString(sum[:])
	st.metaCt[id] = ct
}

func photoFixture(t *testing.T, mutate func(*config.Config)) (*fixture, *photoUpStub) {
	t.Helper()
	stub := newPhotoUpStub(t)
	f := newFixture(t, func(c *config.Config) {
		c.PhotoUploadEnabled = true
		c.UploadBaseURL = stub.srv.URL
		if mutate != nil {
			mutate(c)
		}
	})
	return f, stub
}

func (f *fixture) doBytes(t *testing.T, method, path, bearer string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.ts.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Product-Internal-Token", "it-test")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, raw
}

func codeOf(t *testing.T, raw []byte) string {
	t.Helper()
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if e, ok := m["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	return ""
}

// minWebP 最小 1x1 webp(VP8L),与 imgprobe 夹具同源。
var minWebP, _ = base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")

func tinyImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	return img
}

func encodeTinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tinyImage()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeTinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, tinyImage(), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tenantOf 与身份桩派生公式一致(acct_ + sha16("account:"+email))。
func tenantOf(email string) string {
	sum := sha256.Sum256([]byte("account:" + email))
	return "acct_" + hex.EncodeToString(sum[:])[:16]
}

func shaHexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---- 开关与门 ---------------------------------------------------------------

func TestPhotoRoutesHiddenWhenSwitchOff(t *testing.T) {
	f := newFixture(t, nil) // FEATURE_PHOTO_UPLOAD 默认 off
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	// off → 路由不注册 = 404 不可见(与 FEATURE_SIZE_ADAPT 同语义)。
	code, _, _ := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok, []byte("x"))
	if code != http.StatusNotFound {
		t.Fatalf("开关 off 照片上传应 404 不可见, got %d", code)
	}
}

func TestPhotoUploadRequiresUploadServiceConfigured(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.PhotoUploadEnabled = true
		c.UploadBaseURL = ""
		c.UploadToken = ""
	})
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	code, _, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok, []byte("x"))
	if code != http.StatusServiceUnavailable || codeOf(t, raw) != "upload_unconfigured" {
		t.Fatalf("上传服务未配置应 503 upload_unconfigured, got %d %s", code, raw)
	}
}

func TestPhotoUploadRequiresAuthenticatedPrincipal(t *testing.T) {
	f, _ := photoFixture(t, nil)
	code, _, _ := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", "", []byte("x"))
	if code != http.StatusUnauthorized {
		t.Fatalf("未登录必须 401, got %d", code)
	}
}

// ---- 服务端单点校验链 --------------------------------------------------------

func TestPhotoUploadValidationChain(t *testing.T) {
	f, stub := photoFixture(t, nil)
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	png := encodeTinyPNG(t)

	// 无 order_id 的独立上传是一等路径(standalone)。
	code, hdr, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=产品图.png", tok, png)
	if code != http.StatusCreated {
		t.Fatalf("合法 png 独立上传应 201, got %d %s", code, raw)
	}
	var resp struct {
		Photo      map[string]any `json:"photo"`
		Idempotent bool           `json:"idempotent"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("应答必须是 JSON: %v", err)
	}
	if resp.Idempotent {
		t.Fatal("首次上传 idempotent 必须为 false")
	}
	photo := resp.Photo
	if photo["media_type"] != "image/png" || photo["size_bytes"] != float64(len(png)) ||
		photo["sha256"] != shaHexOf(png) || photo["purpose"] != "project_photo" ||
		photo["width_px"] != float64(1) || photo["height_px"] != float64(1) ||
		photo["platform_asset_id"] == "" || photo["id"] == "" {
		t.Fatalf("照片核验事实不符: %v", photo)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("应答 Content-Type 异常: %q", ct)
	}
	// 平台会话申报必须来自服务端事实(sha/size/属主 principal),绝不采信客户端扩展名。
	if len(stub.creates) != 1 {
		t.Fatalf("平台创建会话次数 = %d", len(stub.creates))
	}
	principal, _ := pair["principal"].(map[string]any)
	usrID, _ := principal["user_id"].(string)
	create := stub.creates[0]
	if create["sha256"] != shaHexOf(png) || create["size_bytes"] != float64(len(png)) ||
		create["content_type"] != "image/png" || create["principal_id"] != usrID {
		t.Fatalf("平台会话申报不符: %v", create)
	}

	// purpose 必须在枚举内。
	for _, q := range []string{"", "?purpose=free_form", "?purpose=..%2Fetc"} {
		code, _, raw = f.doBytes(t, "POST", "/api/v1/photos"+q, tok, png)
		if code != http.StatusBadRequest || codeOf(t, raw) != "invalid_purpose" {
			t.Fatalf("purpose %q 必须 400 invalid_purpose, got %d %s", q, code, raw)
		}
	}
	// 空体。
	code, _, raw = f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok, nil)
	if code != http.StatusBadRequest || codeOf(t, raw) != "empty_body" {
		t.Fatalf("空体必须 400 empty_body, got %d %s", code, raw)
	}
	// 文本冒充 .jpg:扩展名不作数。
	forged := []byte("this is definitely not an image, marker=UNIQUE-CONTENT-MARKER-7f3a")
	code, _, raw = f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=forged.jpg", tok, forged)
	if code != http.StatusUnsupportedMediaType || codeOf(t, raw) != "unsupported_media_type" {
		t.Fatalf("伪造扩展名必须 415, got %d %s", code, raw)
	}
	// 脱敏:错误应答绝不回显文件内容原文。
	if strings.Contains(string(raw), "UNIQUE-CONTENT-MARKER-7f3a") {
		t.Fatalf("错误应答泄漏了文件内容原文: %s", raw)
	}
	// 坏字节:魔数合法但解不开。
	code, _, raw = f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=bad.png", tok,
		append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0xAA}, 64)...))
	if code != http.StatusUnprocessableEntity || codeOf(t, raw) != "undecodable_image" {
		t.Fatalf("坏字节必须 422 undecodable_image, got %d %s", code, raw)
	}
	// webp 真解码通过。
	code, _, _ = f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=a.webp", tok, minWebP)
	if code != http.StatusCreated {
		t.Fatalf("最小 webp 必须解码通过 201, got %d", code)
	}
	// jpeg 真解码通过(标准库编码的最小 1x1)。
	jpegData := encodeTinyJPEG(t)
	code, _, raw = f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=a.jpg", tok, jpegData)
	if code != http.StatusCreated {
		t.Fatalf("最小 jpeg 必须解码通过 201, got %d %s", code, raw)
	}
	var jresp struct {
		Photo map[string]any `json:"photo"`
	}
	_ = json.Unmarshal(raw, &jresp)
	if jresp.Photo["media_type"] != "image/jpeg" {
		t.Fatalf("jpeg 媒体类型事实错误: %v", jresp.Photo)
	}
}

func TestPhotoUploadOversizeRejected413(t *testing.T) {
	f, _ := photoFixture(t, func(c *config.Config) { c.PhotoMaxBytes = 64 })
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	big := encodeTinyPNG(t)
	if len(big) <= 64 {
		t.Fatalf("夹具应大于上限, got %d", len(big))
	}
	code, _, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok, big)
	if code != http.StatusRequestEntityTooLarge || codeOf(t, raw) != "photo_too_large" {
		t.Fatalf("超限必须 413 photo_too_large, got %d %s", code, raw)
	}
}

// ---- 内容幂等与计费安全 ------------------------------------------------------

func TestPhotoUploadIdempotentByContent(t *testing.T) {
	f, stub := photoFixture(t, nil)
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	png := encodeTinyPNG(t)

	code, _, first := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=a.png", tok, png)
	if code != http.StatusCreated {
		t.Fatalf("首次上传应 201, got %d", code)
	}
	// 断网重传语义(v1 不做分片续传,整文件重传走内容幂等):同内容不同文件名。
	code, _, second := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=renamed.png", tok, png)
	if code != http.StatusOK {
		t.Fatalf("同内容重传应幂等 200, got %d %s", code, second)
	}
	var firstResp, secondResp struct {
		Photo      map[string]any `json:"photo"`
		Idempotent bool           `json:"idempotent"`
	}
	_ = json.Unmarshal(first, &firstResp)
	_ = json.Unmarshal(second, &secondResp)
	if !secondResp.Idempotent {
		t.Fatal("重传应答必须标记 idempotent=true")
	}
	if firstResp.Photo["id"] != secondResp.Photo["id"] ||
		firstResp.Photo["platform_asset_id"] != secondResp.Photo["platform_asset_id"] {
		t.Fatalf("幂等返回必须同一资产引用: %v vs %v", firstResp.Photo, secondResp.Photo)
	}
	// 幂等路径绝不重复触平台登记(不重复登记不重复计费)。
	stub.mu.Lock()
	n := stub.sessions
	stub.mu.Unlock()
	if n != 1 {
		t.Fatalf("平台创建会话次数必须为 1(无重复登记), got %d", n)
	}
	// 不同租户同内容:独立入库(跨租户不共享引用)。
	_, pairB := f.login(t, "yi@x.com", "right-pass")
	tokB, _ := pairB["access_token"].(string)
	code, _, _ = f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tokB, png)
	if code != http.StatusCreated {
		t.Fatalf("租户 B 同内容应独立 201, got %d", code)
	}
	stub.mu.Lock()
	n = stub.sessions
	stub.mu.Unlock()
	if n != 2 {
		t.Fatalf("租户 B 上传必须独立登记, 平台会话次数 = %d", n)
	}
}

func TestPhotoUploadPlatformFailureFailsClosed(t *testing.T) {
	f, stub := photoFixture(t, nil)
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	png := encodeTinyPNG(t)

	// 平台 complete 拒绝(如 sha 不符):502 + 本地零落库 + 会话中止。
	stub.mu.Lock()
	stub.failComplete = true
	stub.mu.Unlock()
	code, _, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok, png)
	if code != http.StatusBadGateway || codeOf(t, raw) != "upstream_rejected" {
		t.Fatalf("平台拒绝必须 502 upstream_rejected, got %d %s", code, raw)
	}
	stub.mu.Lock()
	aborts := stub.aborts
	stub.mu.Unlock()
	if aborts != 1 {
		t.Fatalf("失败必须中止平台会话, abort=%d", aborts)
	}
	if _, err := f.st.GetPhotoAssetBySHA(context.Background(), tenantOf("jia@x.com"), shaHexOf(png)); err == nil {
		t.Fatalf("登记失败的资产绝不能落库(未完成资产不可成为生成输入)")
	}
	// 平台不可达:503。
	dead := newFixture(t, func(c *config.Config) {
		c.PhotoUploadEnabled = true
		c.UploadBaseURL = "http://127.0.0.1:1"
	})
	_, pair2 := dead.login(t, "jia@x.com", "right-pass")
	tok2, _ := pair2["access_token"].(string)
	code, _, raw = dead.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok2, png)
	if code != http.StatusServiceUnavailable || codeOf(t, raw) != "upstream_unavailable" {
		t.Fatalf("平台不可达必须 503 upstream_unavailable, got %d %s", code, raw)
	}
}

// ---- 工程挂接 / 跨租户 / 重登恢复 ---------------------------------------------

func TestPhotoAttachProjectResolveAfterRelogin(t *testing.T) {
	f, _ := photoFixture(t, nil)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	png := encodeTinyPNG(t)

	code, _, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo&filename=主图.png", tok, png)
	if code != http.StatusCreated {
		t.Fatalf("上传应 201, got %d", code)
	}
	var up struct {
		Photo map[string]any `json:"photo"`
	}
	_ = json.Unmarshal(raw, &up)
	photoID, _ := up.Photo["id"].(string)
	assetID, _ := up.Photo["platform_asset_id"].(string)

	// 创建 standalone 工程并挂接版本引用(保存引用,不搬字节)。
	_, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{
		"name": "主图工程", "source_type": "standalone",
	})
	proj, _ := created["project"].(map[string]any)
	projID, _ := proj["id"].(string)
	st, attach := f.do(t, "POST", "/api/v1/projects/"+projID+"/inputs", tok, map[string]any{
		"platform_asset_id": assetID, "snapshot_name": up.Photo["original_name"],
		"snapshot_size": up.Photo["size_bytes"], "snapshot_content_type": up.Photo["media_type"],
	})
	if st != 201 {
		t.Fatalf("挂接应 201, got %d %v", st, attach)
	}
	input, _ := attach["input"].(map[string]any)
	inputID, _ := input["id"].(string)
	st, detail := f.do(t, "GET", "/api/v1/projects/"+projID, tok, nil)
	if st != 200 {
		t.Fatalf("工程详情应 200, got %d", st)
	}
	inputs, _ := detail["inputs"].([]any)
	if len(inputs) != 1 {
		t.Fatalf("工程应挂 1 个输入: %v", detail)
	}

	// 重登(新会话令牌)后:照片引用解析回真实可打开字节;工程内引用同样可解析。
	st, pair2 := f.login(t, "jia@x.com", "right-pass")
	tok2, _ := pair2["access_token"].(string)
	code, hdr, body := f.doBytes(t, "GET", "/api/v1/photos/"+photoID+"/content", tok2, nil)
	if code != http.StatusOK {
		t.Fatalf("重登后照片内容应 200, got %d", code)
	}
	if !bytes.Equal(body, png) {
		t.Fatalf("解析字节必须与上传一致: %d vs %d", len(body), len(png))
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "image/png") {
		t.Fatalf("内容类型必须为真实解码事实: %q", hdr.Get("Content-Type"))
	}
	code, hdr, body = f.doBytes(t, "GET", "/api/v1/projects/"+projID+"/inputs/"+inputID+"/content", tok2, nil)
	if code != http.StatusOK || !bytes.Equal(body, png) {
		t.Fatalf("工程输入解析必须还原字节: code=%d len=%d", code, len(body))
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "image/png") {
		t.Fatalf("输入内容类型异常: %q", hdr.Get("Content-Type"))
	}
}

func TestPhotoCrossTenantDenied(t *testing.T) {
	f, _ := photoFixture(t, nil)
	st, pairA := f.login(t, "jia@x.com", "right-pass")
	tokA, _ := pairA["access_token"].(string)
	_, pairB := f.login(t, "yi@x.com", "right-pass")
	tokB, _ := pairB["access_token"].(string)

	code, _, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tokA, encodeTinyPNG(t))
	if code != http.StatusCreated {
		t.Fatalf("甲上传应 201, got %d", code)
	}
	var up struct {
		Photo map[string]any `json:"photo"`
	}
	_ = json.Unmarshal(raw, &up)
	photoID, _ := up.Photo["id"].(string)

	// 乙读甲的照片:跨租户与不存在同型 404(防探测红线)。
	code, _, _ = f.doBytes(t, "GET", "/api/v1/photos/"+photoID+"/content", tokB, nil)
	if code != http.StatusNotFound {
		t.Fatalf("乙读甲照片必须 404, got %d", code)
	}
	// 乙挂甲的工程被拒(工程域既有租户门)。
	_, createdA := f.do(t, "POST", "/api/v1/projects", tokA, map[string]any{"name": "甲工程", "source_type": "standalone"})
	projA, _ := createdA["project"].(map[string]any)
	projAID, _ := projA["id"].(string)
	st, _ = f.do(t, "POST", "/api/v1/projects/"+projAID+"/inputs", tokB, map[string]any{
		"platform_asset_id": up.Photo["platform_asset_id"],
	})
	if st != http.StatusNotFound {
		t.Fatalf("乙挂甲工程必须 404, got %d", st)
	}
}

func TestInputContentResolvesCrossAppReferenceViaPlatform(t *testing.T) {
	f, stub := photoFixture(t, nil)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)

	// 模拟他应用(如订单域)经授权登记过的资产:只存在平台侧,本租户无 photo 行。
	extBytes := encodeTinyPNG(t)
	stub.Preload("ast_from_order_app", extBytes, "image/png")

	_, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "跨应用工程", "source_type": "standalone"})
	proj, _ := created["project"].(map[string]any)
	projID, _ := proj["id"].(string)
	st, attach := f.do(t, "POST", "/api/v1/projects/"+projID+"/inputs", tok, map[string]any{
		"platform_asset_id": "ast_from_order_app", "snapshot_name": "order-src.png",
		"snapshot_size": len(extBytes), "snapshot_content_type": "image/png",
	})
	if st != 201 {
		t.Fatalf("挂接跨应用引用应 201, got %d %v", st, attach)
	}
	input, _ := attach["input"].(map[string]any)
	inputID, _ := input["id"].(string)

	// 解析走平台设施(下载地址+字节流),绝不读其他应用 SQLite/共享目录。
	before := stub.dlFetches
	code, _, body := f.doBytes(t, "GET", "/api/v1/projects/"+projID+"/inputs/"+inputID+"/content", tok, nil)
	if code != http.StatusOK || !bytes.Equal(body, extBytes) {
		t.Fatalf("跨应用版本引用必须经平台解析还原字节: code=%d len=%d", code, len(body))
	}
	stub.mu.Lock()
	fetches := stub.dlFetches
	stub.mu.Unlock()
	if fetches <= before {
		t.Fatal("跨应用解析必须经过平台下载设施")
	}
}

func TestPhotoContentIntegrityRecheckFails(t *testing.T) {
	f, stub := photoFixture(t, nil)
	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	png := encodeTinyPNG(t)
	code, _, raw := f.doBytes(t, "POST", "/api/v1/photos?purpose=project_photo", tok, png)
	if code != http.StatusCreated {
		t.Fatalf("上传应 201, got %d", code)
	}
	var up struct {
		Photo map[string]any `json:"photo"`
	}
	_ = json.Unmarshal(raw, &up)
	photoID, _ := up.Photo["id"].(string)

	// 平台侧资产事实与本地登记不符(模拟对象被篡改/串引用)→ integrity_failed,拒绝解析。
	stub.mu.Lock()
	stub.metaSha["ast_1"] = strings.Repeat("ff", 32)
	stub.mu.Unlock()
	code, _, raw = f.doBytes(t, "GET", "/api/v1/photos/"+photoID+"/content", tok, nil)
	if code != http.StatusBadGateway || codeOf(t, raw) != "integrity_failed" {
		t.Fatalf("完整性不符必须 502 integrity_failed, got %d %s", code, raw)
	}
}
