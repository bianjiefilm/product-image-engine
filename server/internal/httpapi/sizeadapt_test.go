package httpapi

// 尺寸适配 HTTP 验收(HUI-1703 / FEAT-0204):
//   off → 四条路由 404 不可见;on → 预设表 / 生成(201,重复幂等 200) /
//   列表 / 下载(字节=变体内容);跨租户 403;输入门 400/413;上传未配置 503;
//   FEATURE_GENERATION_ENABLED off 不影响本域(零耦合行为断言)。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
)

// ---- 测试装配 ----------------------------------------------------------------

// newUploadStub 平台 upload 桩:POST /internal/v1/upload/assets → asset_id。
func newUploadStub(t *testing.T) *httptest.Server {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/internal/v1/upload/assets" {
			n++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"asset_id":"asset-sz-%d"}`, n)))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newSizeFixture 开关 on + upload 指向桩(生成链路可用;生成开关仍 off)。
func newSizeFixture(t *testing.T, extra func(*config.Config)) *fixture {
	t.Helper()
	up := newUploadStub(t)
	f := newFixture(t, func(c *config.Config) {
		c.SizeAdaptEnabled = true
		if extra != nil {
			extra(c)
		}
	})
	f.srv.Uploads.BaseURL = up.URL
	return f
}

// sizeProject 建 standalone 工程并登记一个 10×40 PNG 成果,返回 (工程ID, 成果ID, PNG字节)。
func sizeProject(t *testing.T, f *fixture, tok string) (string, string, []byte) {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{
		"name": "尺寸适配", "source_type": "standalone",
	})
	if st != 201 {
		t.Fatalf("建工程应 201: %d %v", st, created)
	}
	proj, _ := created["project"].(map[string]any)
	projID, _ := proj["id"].(string)
	png := pngBytes(t, 10, 40, 7)
	outID := sizeRegisterOutput(t, f, tok, projID, png)
	return projID, outID, png
}

// sizeRegisterOutput 登记成果(要求 upload 桩已接)。
func sizeRegisterOutput(t *testing.T, f *fixture, tok, projID string, data []byte) string {
	t.Helper()
	st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "source.png", "content_type": "image/png", "data_b64": b64(data),
	})
	if st != 201 {
		t.Fatalf("登记成果应 201: %d %v", st, resp)
	}
	out, _ := resp["output"].(map[string]any)
	id, _ := out["id"].(string)
	return id
}

func sizeBody(srcOutputID, preset string, data []byte) map[string]any {
	return map[string]any{"source_output_id": srcOutputID, "preset_name": preset, "data_b64": b64(data)}
}

// doRaw 同 do 但返回原始状态/头/字节(下载断言用)。
func (f *fixture) doRaw(t *testing.T, method, path, bearer string, body any) (int, http.Header, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Product-Internal-Token", "it-test")
	req.Header.Set("Content-Type", "application/json")
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

// ---- 开关 off:路由不注册 = 404 不可见 ----------------------------------------

func TestSizeAdaptDisabledRoutesInvisible404(t *testing.T) {
	f := newFixture(t, nil) // FEATURE_SIZE_ADAPT 默认 off
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)

	if st, _ := f.do(t, "GET", "/api/v1/size-adapt/presets", tok, nil); st != 404 {
		t.Fatalf("off 时预设表应 404: %d", st)
	}
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "P", "source_type": "standalone"})
	proj, _ := created["project"].(map[string]any)
	id, _ := proj["id"].(string)
	if st, _ := f.do(t, "POST", "/api/v1/projects/"+id+"/size-adapt", tok, sizeBody("x", "taobao_main", []byte("y"))); st != 404 {
		t.Fatalf("off 时生成应 404: %d", st)
	}
	if st, _ := f.do(t, "GET", "/api/v1/projects/"+id+"/size-adapt", tok, nil); st != 404 {
		t.Fatalf("off 时列表应 404: %d", st)
	}
	if st, _, _ := f.doRaw(t, "GET", "/api/v1/projects/"+id+"/size-adapt/v1/download", tok, nil); st != 404 {
		t.Fatalf("off 时下载应 404: %d", st)
	}
	// off 不影响 readyz(ok 仍 true),gates 里可见 size_adapt.enabled=false。
	st, ready := f.do(t, "GET", "/readyz", "", nil)
	gates, _ := ready["gates"].(map[string]any)
	sa, _ := gates["size_adapt"].(map[string]any)
	if st != 200 || ready["ok"] != true || sa["enabled"] != false {
		t.Fatalf("readyz 应报告 size_adapt off 且服务仍 ok: %d %v", st, ready)
	}
}

// ---- 开关 on:预设 / 生成幂等 / 列表 / 下载 / 语义尺寸 ------------------------

func TestSizeAdaptGenerateIdempotentListDownload(t *testing.T) {
	f := newSizeFixture(t, nil)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	projID, srcID, srcPNG := sizeProject(t, f, tok)

	// 预设表:8 个公开常见规格。
	st, pres := f.do(t, "GET", "/api/v1/size-adapt/presets", tok, nil)
	if st != 200 {
		t.Fatalf("预设表应 200: %d %v", st, pres)
	}
	list, _ := pres["presets"].([]any)
	if len(list) != 8 {
		t.Fatalf("应 8 个预设: %v", pres)
	}

	// 生成:10×40 → taobao_main(800×800 pad)。
	st, r1 := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "taobao_main", srcPNG))
	if st != 201 {
		t.Fatalf("首次生成应 201: %d %v", st, r1)
	}
	v1, _ := r1["variant"].(map[string]any)
	vid, _ := v1["id"].(string)
	if r1["duplicate"] != false || v1["kind"] != "size_variant" ||
		v1["preset_name"] != "taobao_main" || v1["variant_mode"] != "pad" ||
		v1["variant_width"] != float64(800) || v1["variant_height"] != float64(800) ||
		v1["source_output_id"] != srcID {
		t.Fatalf("变体字段不符: %v", v1)
	}

	// 幂等:同 (源,预设,模式) 重复请求 → 200 + 同一变体,不重复生成。
	st, r2 := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "taobao_main", srcPNG))
	if st != 200 || r2["duplicate"] != true {
		t.Fatalf("重复请求应 200 duplicate:true: %d %v", st, r2)
	}
	v2, _ := r2["variant"].(map[string]any)
	if v2["id"] != vid {
		t.Fatalf("幂等应返回同一变体: %v vs %v", v1["id"], v2["id"])
	}

	// 列表。
	st, li := f.do(t, "GET", "/api/v1/projects/"+projID+"/size-adapt", tok, nil)
	variants, _ := li["variants"].([]any)
	if st != 200 || len(variants) != 1 {
		t.Fatalf("列表应 1 条: %d %v", st, li)
	}

	// 下载:字节是合法 PNG 且 800×800。
	st, hdr, raw := f.doRaw(t, "GET", "/api/v1/projects/"+projID+"/size-adapt/"+vid+"/download", tok, nil)
	if st != 200 {
		t.Fatalf("下载应 200: %d %s", st, raw)
	}
	if ct := hdr.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type 应 image/png: %q", ct)
	}
	if !bytes.HasPrefix(raw, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("下载字节应为 PNG: %x", raw[:8])
	}
	cfgImg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfgImg.Width != 800 || cfgImg.Height != 800 {
		t.Fatalf("变体应 800×800 PNG: %v %dx%d", err, cfgImg.Width, cfgImg.Height)
	}
	if cd := hdr.Get("Content-Disposition"); cd == "" || !bytes.Contains([]byte(cd), []byte("attachment")) {
		t.Fatalf("应有 attachment Content-Disposition: %q", cd)
	}

	// fit-width:100×50 源 → tmall_detail_w790(790 宽等比)→ 790×395。
	srcW := sizeRegisterOutput(t, f, tok, projID, pngBytes(t, 100, 50, 3))
	st, r3 := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcW, "tmall_detail_w790", pngBytes(t, 100, 50, 3)))
	if st != 201 {
		t.Fatalf("fit-width 生成应 201: %d %v", st, r3)
	}
	v3, _ := r3["variant"].(map[string]any)
	if v3["variant_width"] != float64(790) || v3["variant_height"] != float64(395) {
		t.Fatalf("fit-width 应 790×395: %v", v3)
	}

	// cover:100×50 源 → douyin_ad(1080×1440 cover)→ 1080×1440。
	st, r4 := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcW, "douyin_ad", pngBytes(t, 100, 50, 3)))
	if st != 201 {
		t.Fatalf("cover 生成应 201: %d %v", st, r4)
	}
	v4, _ := r4["variant"].(map[string]any)
	if v4["variant_width"] != float64(1080) || v4["variant_height"] != float64(1440) ||
		v4["variant_mode"] != "cover" {
		t.Fatalf("cover 应 1080×1440/pad≠cover: %v", v4)
	}

	// 零耦合行为断言:生成开关仍 off,生成端点照旧 503,本域不受影响。
	st, msg := f.do(t, "POST", "/api/v1/projects/"+projID+"/versions", tok, map[string]any{})
	if st != 503 || !bytes.Contains([]byte(errMsg(msg)), []byte("FEATURE_GENERATION_ENABLED")) {
		t.Fatalf("生成开关 off 应不受尺寸适配影响: %d %v", st, msg)
	}
}

// ---- 负例矩阵:输入门 / 门控 / fail-closed ------------------------------------

func TestSizeAdaptNegatives(t *testing.T) {
	f := newSizeFixture(t, nil)
	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	projID, srcID, srcPNG := sizeProject(t, f, tok)
	st, created2 := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "P2", "source_type": "standalone"})
	if st != 201 {
		t.Fatalf("建第二工程应 201: %d %v", st, created2)
	}
	proj2, _ := created2["project"].(map[string]any)
	proj2ID, _ := proj2["id"].(string)

	t.Run("未知预设 400", func(t *testing.T) {
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "no_such_preset", srcPNG))
		if code, _ := errCodeOf(resp); st != 400 || code != "unknown_preset" {
			t.Fatalf("未知预设应 400 unknown_preset: %d %v", st, resp)
		}
	})
	t.Run("sha 不符 400", func(t *testing.T) {
		other := pngBytes(t, 10, 40, 9) // 同尺寸不同内容
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "taobao_main", other))
		if code, _ := errCodeOf(resp); st != 400 || code != "sha_mismatch" {
			t.Fatalf("sha 不符应 400: %d %v", st, resp)
		}
	})
	t.Run("源不属于该工程 404", func(t *testing.T) {
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody("out-nope", "taobao_main", srcPNG))
		if st != 404 {
			t.Fatalf("不存在源应 404: %d %v", st, resp)
		}
		st, _ = f.do(t, "POST", "/api/v1/projects/"+proj2ID+"/size-adapt", tok, sizeBody(srcID, "taobao_main", srcPNG))
		if st != 404 {
			t.Fatalf("他工程源应 404: %d", st)
		}
	})
	t.Run("超 8MiB 413", func(t *testing.T) {
		big := make([]byte, 8<<20+64)
		st, _ := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "taobao_main", big))
		if st != 413 {
			t.Fatalf("超限应 413: %d", st)
		}
	})
	t.Run("合法魔数但损坏 PNG 400", func(t *testing.T) {
		// 登记门只查魔数;尺寸适配解码更深 → 必须在此被拒(纵深防御)。
		corrupt := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x00}, 64)...)
		corruptID := sizeRegisterOutput(t, f, tok, projID, corrupt)
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(corruptID, "taobao_main", corrupt))
		if code, _ := errCodeOf(resp); st != 400 || code != "invalid_image" {
			t.Fatalf("损坏 PNG 应 400 invalid_image: %d %v", st, resp)
		}
	})
	t.Run("变体不可再派生 400", func(t *testing.T) {
		st, r := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "pdd_main", srcPNG))
		variant, _ := r["variant"].(map[string]any)
		if st != 201 {
			t.Fatalf("前置生成应 201: %d %v", st, r)
		}
		vid, _ := variant["id"].(string)
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(vid, "taobao_main", srcPNG))
		if st != 400 { // 源必须是 kind=result;变体不可再级联派生
			t.Fatalf("变体作源应 400: %d %v", st, resp)
		}
	})
	t.Run("跨租户 403(生成/列表/下载)", func(t *testing.T) {
		_, pairB := f.login(t, "yi@x.com", "right-pass")
		tokB, _ := pairB["access_token"].(string)
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tokB, sizeBody(srcID, "taobao_main", srcPNG))
		if code, _ := errCodeOf(resp); st != 403 || code != "tenant_mismatch" {
			t.Fatalf("跨租户生成应 403: %d %v", st, resp)
		}
		if st, resp := f.do(t, "GET", "/api/v1/projects/"+projID+"/size-adapt", tokB, nil); st != 403 {
			t.Fatalf("跨租户列表应 403: %d %v", st, resp)
		}
		st, li, _ := f.doRaw(t, "GET", "/api/v1/projects/"+projID+"/size-adapt/"+srcID+"/download", tokB, nil)
		_ = li
		if st != 403 {
			t.Fatalf("跨租户下载应 403: %d", st)
		}
	})
	t.Run("不存在工程 404", func(t *testing.T) {
		st, _ := f.do(t, "GET", "/api/v1/projects/proj-nope/size-adapt", tok, nil)
		if st != 404 {
			t.Fatalf("不存在工程列表应 404: %d", st)
		}
	})
	t.Run("下载不存在变体 404", func(t *testing.T) {
		st, _, _ := f.doRaw(t, "GET", "/api/v1/projects/"+projID+"/size-adapt/out-nope/download", tok, nil)
		if st != 404 {
			t.Fatalf("下载不存在变体应 404: %d", st)
		}
	})
	t.Run("upload 未配置 fail-closed 503", func(t *testing.T) {
		f.srv.Uploads = nil // 模拟装配缺失(与 main 不装配一致)
		st, resp := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "jd_main", srcPNG))
		if code, _ := errCodeOf(resp); st != 503 || code != "upload_not_configured" {
			t.Fatalf("upload 未配置应 503: %d %v", st, resp)
		}
		// 不落任何变体行。
		_, li := f.do(t, "GET", "/api/v1/projects/"+projID+"/size-adapt", tok, nil)
		variants, _ := li["variants"].([]any)
		for _, v := range variants {
			if m, _ := v.(map[string]any); m["preset_name"] == "jd_main" {
				t.Fatalf("fail-closed 不得落变体: %v", li)
			}
		}
	})
}

// ---- JPEG 格式:quality 90 + 首个变体格式胜出(格式不在唯一键) ----------------

func TestSizeAdaptJPEGAndFirstFormatWins(t *testing.T) {
	f := newSizeFixture(t, nil)
	st, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	projID, srcID, srcPNG := sizeProject(t, f, tok)

	// jpeg 变体:Content-Type 与魔数。
	st, r := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok,
		map[string]any{"source_output_id": srcID, "preset_name": "pdd_main", "format": "jpeg", "data_b64": b64(srcPNG)})
	if st != 201 {
		t.Fatalf("jpeg 生成应 201: %d %v", st, r)
	}
	v, _ := r["variant"].(map[string]any)
	vid, _ := v["id"].(string)
	if v["media_type"] != "image/jpeg" {
		t.Fatalf("jpeg 变体 media_type: %v", v)
	}
	st, hdr, raw := f.doRaw(t, "GET", "/api/v1/projects/"+projID+"/size-adapt/"+vid+"/download", tok, nil)
	if st != 200 || hdr.Get("Content-Type") != "image/jpeg" || !bytes.HasPrefix(raw, []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatalf("jpeg 下载异常: %d %v %x", st, hdr.Get("Content-Type"), raw[:3])
	}

	// 同三元组改请求 jpeg:格式不在唯一键 → 仍返回既有(png)变体,不重建。
	st, r2 := f.do(t, "POST", "/api/v1/projects/"+projID+"/size-adapt", tok, sizeBody(srcID, "pdd_main", srcPNG))
	if st != 200 || r2["duplicate"] != true {
		t.Fatalf("重复请求应幂等: %d %v", st, r2)
	}
	v2, _ := r2["variant"].(map[string]any)
	if v2["id"] != vid || v2["media_type"] != "image/jpeg" {
		t.Fatalf("首个格式胜出: %v", v2)
	}
}
