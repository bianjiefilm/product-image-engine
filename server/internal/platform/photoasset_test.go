package platform

// HUI-1697 FEAT-0198:照片上传 client 测试。
// 桩按 public-ai platform-upload 真实内部面形状实现(侦察 2026-09-20):
//   POST /internal/v1/upload/sessions                      创建会话(申报 sha256/size+属主 principal)
//   POST /internal/v1/upload/sessions/{sid}/parts/{n}/sign 预签名分片
//   PUT  {upload_url}                                      分片字节直传(OSS 语义,响应 ETag)
//   POST /internal/v1/upload/sessions/{sid}/complete       完成核验(平台侧比对对象头 sha/size)
//   POST /internal/v1/upload/sessions/{sid}/abort          失败中止
//   GET  /internal/v1/upload/assets/{aid}                  资产元数据(Go 字段名形状)
//   POST /internal/v1/upload/assets/{aid}/download-url     受限下载地址

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

const (
	stubAppID   = "product-image"
	stubToken   = "upload-app-token"
	stubUsrType = "user"
	stubUsrID   = "usr_abc123"
)

type photoStub struct {
	srv *httptest.Server

	mu         sync.Mutex
	sessions   int
	creates    []map[string]any
	chunks     map[int][]byte
	etags      map[int]string
	completes  int
	aborts     int
	failStage  string // "sign"|"complete"|"" — 注入平台故障
	lastCreate struct{ pt, pid, app, tok, ptyp, pidhdr string }
	dlFetches  int
}

func newPhotoStub(t *testing.T) *photoStub {
	t.Helper()
	st := &photoStub{chunks: map[int][]byte{}, etags: map[int]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/upload/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.sessions++
		st.creates = append(st.creates, body)
		st.lastCreate.app = r.Header.Get("X-App-ID")
		st.lastCreate.tok = r.Header.Get(internalTokenHeader)
		st.lastCreate.ptyp = r.Header.Get("X-Principal-Type")
		st.lastCreate.pidhdr = r.Header.Get("X-Principal-ID")
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "asset_id": fmt.Sprintf("ast_%d", st.sessions),
			"session_id": fmt.Sprintf("ses_%d", st.sessions), "part_size": 16, "expires_at": "2026-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("POST /internal/v1/upload/sessions/ses_1/parts/{n}/sign", func(w http.ResponseWriter, r *http.Request) {
		if st.failStage == "sign" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		n, _ := strconv.Atoi(r.PathValue("n"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "part_number": n,
			"upload_url": st.srv.URL + "/oss-part/" + strconv.Itoa(n),
			"headers":    map[string]string{"X-Stub": "1"}, "expires_at": "2026-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("PUT /oss-part/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		b, _ := io.ReadAll(r.Body)
		st.mu.Lock()
		st.chunks[n] = b
		etag := fmt.Sprintf(`"etag-%d"`, n)
		st.etags[n] = etag
		st.mu.Unlock()
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /internal/v1/upload/sessions/ses_1/complete", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.failStage == "complete" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"upload_sha_mismatch"}}`))
			return
		}
		st.completes++
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": map[string]any{
			"AssetID": "ast_1", "SHA256": "deadbeef", "SizeBytes": 42, "ContentType": "image/png", "Status": "ready",
		}})
	})
	mux.HandleFunc("POST /internal/v1/upload/sessions/ses_1/abort", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.aborts++
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /internal/v1/upload/assets/ast_1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": map[string]any{
			"AssetID": "ast_1", "AppID": stubAppID, "PrincipalType": stubUsrType, "PrincipalID": stubUsrID,
			"SHA256": "deadbeef", "SizeBytes": 42, "ContentType": "image/png", "Status": "ready",
		}})
	})
	mux.HandleFunc("POST /internal/v1/upload/assets/ast_1/download-url", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "download_url": st.srv.URL + "/dl", "expires_at": "2026-01-01T00:00:00Z",
			"content_type": "image/png", "content_length": 42,
		})
	})
	mux.HandleFunc("GET /dl", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.dlFetches++
		st.mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte{0x89, 0x50}, 21))
	})
	st.srv = httptest.NewServer(mux)
	t.Cleanup(st.srv.Close)
	return st
}

func newPhotoClient(stubURL string) *UploadClient {
	return &UploadClient{BaseURL: stubURL, AppID: stubAppID, Token: stubToken}
}

func TestRegisterPhotoSinglePart(t *testing.T) {
	st := newPhotoStub(t)
	c := newPhotoClient(st.srv.URL)
	data := bytes.Repeat([]byte{0x50, 0x4E}, 10) // 20 字节 < part_size 16 → 2 分片
	assetID, err := c.RegisterPhoto(context.Background(), PhotoPrincipal{stubUsrType, stubUsrID},
		"photo.png", "image/png", data, "deadbeef")
	if err != nil {
		t.Fatalf("RegisterPhoto 失败: %v", err)
	}
	if assetID != "ast_1" {
		t.Fatalf("assetID = %q", assetID)
	}
	// 创建会话必须带属主 principal 与申报事实(平台红线:头/体一致)。
	if st.lastCreate.app != stubAppID || st.lastCreate.tok != stubToken {
		t.Fatalf("内部凭据头缺失: %+v", st.lastCreate)
	}
	if st.lastCreate.ptyp != stubUsrType || st.lastCreate.pidhdr != stubUsrID {
		t.Fatalf("principal 头缺失: %+v", st.lastCreate)
	}
	if len(st.creates) != 1 {
		t.Fatalf("创建会话次数 = %d", len(st.creates))
	}
	body := st.creates[0]
	if body["principal_type"] != stubUsrType || body["principal_id"] != stubUsrID ||
		body["sha256"] != "deadbeef" || body["size_bytes"] != float64(len(data)) ||
		body["content_type"] != "image/png" || body["filename"] != "photo.png" {
		t.Fatalf("会话申报体不符: %v", body)
	}
	// 分片重组必须等于原字节(内容完整性)。
	var got []byte
	for i := 1; ; i++ {
		ch, ok := st.chunks[i]
		if !ok {
			break
		}
		got = append(got, ch...)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("分片重组不符: %d bytes vs 原 %d", len(got), len(data))
	}
	if st.completes != 1 {
		t.Fatalf("complete 次数 = %d", st.completes)
	}
}

func TestRegisterPhotoFailsThenAborts(t *testing.T) {
	st := newPhotoStub(t)
	c := newPhotoClient(st.srv.URL)
	st.failStage = "complete"
	_, err := c.RegisterPhoto(context.Background(), PhotoPrincipal{stubUsrType, stubUsrID},
		"photo.png", "image/png", bytes.Repeat([]byte{0x01}, 8), "deadbeef")
	if err == nil {
		t.Fatalf("complete 失败必须向上报错")
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("平台 4xx 拒绝不是不可用,应按平台拒绝处理: %v", err)
	}
	if st.aborts != 1 {
		t.Fatalf("失败后必须中止会话,abort 次数 = %d", st.aborts)
	}

	st2 := newPhotoStub(t)
	c2 := newPhotoClient(st2.srv.URL)
	st2.failStage = "sign"
	_, err = c2.RegisterPhoto(context.Background(), PhotoPrincipal{stubUsrType, stubUsrID},
		"photo.png", "image/png", bytes.Repeat([]byte{0x01}, 8), "deadbeef")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("平台 5xx 必须分类为不可用: %v", err)
	}
	if st2.aborts != 1 {
		t.Fatalf("失败后必须中止会话,abort 次数 = %d", st2.aborts)
	}
}

func TestPhotoAssetAndDownloadURL(t *testing.T) {
	st := newPhotoStub(t)
	c := newPhotoClient(st.srv.URL)
	meta, err := c.PhotoAsset(context.Background(), PhotoPrincipal{stubUsrType, stubUsrID}, "ast_1")
	if err != nil {
		t.Fatalf("PhotoAsset 失败: %v", err)
	}
	if meta.SHA256 != "deadbeef" || meta.SizeBytes != 42 || meta.ContentType != "image/png" || meta.Status != "ready" {
		t.Fatalf("资产元数据不符: %+v", meta)
	}
	url, ct, size, err := c.PhotoDownloadURL(context.Background(), PhotoPrincipal{stubUsrType, stubUsrID}, "ast_1", 0)
	if err != nil {
		t.Fatalf("PhotoDownloadURL 失败: %v", err)
	}
	if ct != "image/png" || size != 42 || url == "" {
		t.Fatalf("下载地址事实不符: %q %q %d", url, ct, size)
	}
	rc, gotCT, gotSize, err := c.OpenPhotoContent(context.Background(), url)
	if err != nil {
		t.Fatalf("OpenPhotoContent 失败: %v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if gotCT != "image/png" || gotSize != 42 || len(b) != 42 {
		t.Fatalf("内容事实不符: ct=%q size=%d len=%d", gotCT, gotSize, len(b))
	}
}

func TestRegisterPhotoEmptyData(t *testing.T) {
	c := newPhotoClient("http://127.0.0.1:1") // 不可达;空数据在本地即拒,不触网
	if _, err := c.RegisterPhoto(context.Background(), PhotoPrincipal{stubUsrType, stubUsrID},
		"x.png", "image/png", nil, "00"); err == nil {
		t.Fatalf("空数据必须本地拒绝")
	}
}
