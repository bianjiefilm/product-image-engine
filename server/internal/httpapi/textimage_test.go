package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/billassemble"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
)

func TestTextImageHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, nil)
	_, tok := f.loginOK(t)
	st, _ := f.do(t, "GET", "/api/v1/text-images/capabilities", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("开关关闭应 404, got %d", st)
	}
}

func TestTextImageStartsFromPromptWithoutPhoto(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)

	st, capBody := f.do(t, "GET", "/api/v1/text-images/capabilities", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("能力接口 %d %v", st, capBody)
	}
	if capBody["billing_label"] != "配置缺失" || capBody["photo_required"] != false ||
		capBody["billing_passed"] != false || capBody["production_authorized"] != false ||
		capBody["subject_protected"] != false || capBody["charged"] != nil || capBody["show_image"] != false ||
		capBody["settlement"] != "unknown" {
		t.Fatalf("能力接口不得宣称已出图或已扣费: %#v", capBody)
	}

	st, denied := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{"prompt": "  "})
	if st != http.StatusBadRequest {
		t.Fatalf("空描述应拒绝, got %d %v", st, denied)
	}

	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{
		"prompt": "白色陶瓷杯，浅灰棚拍", "quoted_amount": "¥9.90", "subject_protected": true,
	})
	if st != http.StatusCreated {
		t.Fatalf("开文字请求 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["billing_label"] != "配置缺失" || job["photo_required"] != false || job["show_image"] != false || job["settlement"] != "unknown" {
		t.Fatalf("文字入口不诚实: %#v", job)
	}
	if job["charged"] != nil || job["billing_passed"] != false || job["production_authorized"] != false || job["subject_protected"] != false || strings.Contains(mapString(job), "¥9.90") {
		t.Fatalf("客户端不能自称已保护或已扣费: %#v", job)
	}
	id, _ := job["id"].(string)
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{
		"prompt": "白色陶瓷杯，浅灰棚拍",
	})
	againJob, _ := again["job"].(map[string]any)
	if st != http.StatusOK || againJob["id"] != id || againJob["idempotent"] != true {
		t.Fatalf("同一描述应回到原记录: %d %#v", st, againJob)
	}
}

func TestTextImageSubmitWithoutSupplierStaysFailed(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "红色水壶")

	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["show_image"] != false || job["subject_protected"] != false {
		t.Fatalf("没有供应商时应失败且不出图: %d %#v", st, job)
	}
	if job["billing_passed"] != false || job["production_authorized"] != false || job["charged"] != nil || job["billing_label"] != "配置缺失" {
		t.Fatalf("失败不能标成计费或生产通过: %#v", job)
	}
	st, claimed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/claim", tok, map[string]any{
		"claim": "subject_protected", "output_asset_id": "filled-output",
	})
	if st != http.StatusConflict {
		t.Fatalf("客户端不能声称主体已保护, got %d %v", st, claimed)
	}
	st, passed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/claim", tok, map[string]any{"claim": "success"})
	if st != http.StatusConflict {
		t.Fatalf("失败不能改成成功, got %d %v", st, passed)
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, map[string]any{
		"output_asset_id": "filled-output", "job_status": "succeeded",
	})
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["id"] != jobID || job["job_status"] != "failed" || job["show_image"] != false || job["output_is_receipt"] != false {
		t.Fatalf("刷新后仍应是同一条失败记录: %d %#v", st, job)
	}
}

func TestTextImageUnknownRefreshDoesNotRegenerate(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "蓝色水壶")
	st, first := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	job, _ := first["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" || job["show_image"] != false {
		t.Fatalf("供应商不可达应为待确认: %d %#v", st, job)
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	if calls.Load() != 1 {
		t.Fatalf("待确认后不得再次提交, calls=%d", calls.Load())
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, nil)
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["id"] != jobID || job["job_status"] != "unknown" || job["show_image"] != false {
		t.Fatalf("刷新后仍是同一条待确认: %d %#v", st, job)
	}
}

func TestTextImageSupplierAssetDoesNotVerifyFailure(t *testing.T) {
	var gets atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"task_id":"task_text_1","status":"pending"}`))
			return
		}
		if gets.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"task_id":"task_text_1","status":"failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task_text_1","status":"succeeded","result":{"asset_id":"filled-output"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "金属水壶")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["platform_task_id"] != "task_text_1" || job["show_image"] != false || job["production_authorized"] != false {
		t.Fatalf("受理任务不能当成已出图: %d %#v", st, job)
	}
	st, failed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, nil)
	job, _ = failed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["show_image"] != false {
		t.Fatalf("供应商失败应保持失败: %d %#v", st, job)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, nil)
	job, _ = again["job"].(map[string]any)
	if st != http.StatusOK || job["id"] != jobID || job["job_status"] != "failed" || job["show_image"] != false || job["output_is_receipt"] != false {
		t.Fatalf("失败后不能借输出编号改成成功: %d %#v", st, job)
	}
	if gets.Load() != 1 {
		t.Fatalf("失败记录不应再次向供应商核对, gets=%d", gets.Load())
	}
}

func TestTextImageFromPersonalEntryDoesNotNeedPhoto(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
	_, tok := f.loginOK(t)
	st, started := f.do(t, "POST", "/api/v1/entry/start", tok, map[string]any{
		"name": "文字描述产品图", "usage_kind": "文生图", "width_px": 800, "height_px": 800,
	})
	if st != http.StatusCreated {
		t.Fatalf("文字入口建工程 %d %v", st, started)
	}
	proj := started["project"].(map[string]any)["id"].(string)
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{
		"prompt": "白色陶瓷杯，浅灰棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("个人工程上的文字请求 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["photo_required"] != false || job["show_image"] != false || job["billing_label"] != "配置缺失" || job["charged"] != nil || job["billing_passed"] != false {
		t.Fatalf("个人文字入口不诚实: %#v", job)
	}
}

func TestTextImageConfiguredWithoutPortIsUnimplemented(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.BillingEnabled = true
	})
	_, tok := f.loginOK(t)
	st, capBody := f.do(t, "GET", "/api/v1/text-images/capabilities", tok, nil)
	if st != http.StatusOK || capBody["billing_label"] != "未实现" || capBody["charged"] != nil || capBody["show_image"] != false || capBody["billing_passed"] != false {
		t.Fatalf("配了计费但没有端口应报未实现，仍不出图: %d %#v", st, capBody)
	}
}

func TestTextImageFixtureIsListedDetailedAndDownloaded(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageFixtureRegister = true
	})
	spy := &textFundsSpy{}
	f.srv.Bills = spy
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "fixture 白色陶瓷杯")
	png := tinyPNG(t, 8, 8)
	body := f.registerTextFixture(t, tok, proj, jobID, png, http.StatusCreated)
	job, _ := body["job"].(map[string]any)
	if !trustedFixtureJob(t, job) {
		t.Fatalf("夹具登记后应可展示且未结算: %#v", job)
	}
	asset, _ := job["output_asset_id"].(string)
	st, listed := f.do(t, "GET", "/api/v1/projects/"+proj+"/text-images", tok, nil)
	jobs, _ := listed["jobs"].([]any)
	if st != http.StatusOK || len(jobs) != 1 || !trustedFixtureJob(t, jobs[0].(map[string]any)) {
		t.Fatalf("列表应打开夹具: %d %#v", st, listed)
	}
	st, detail := f.do(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID, tok, nil)
	detailJob, _ := detail["job"].(map[string]any)
	if st != http.StatusOK || !trustedFixtureJob(t, detailJob) || detailJob["output_asset_id"] != asset {
		t.Fatalf("详情应打开同一夹具: %d %#v", st, detailJob)
	}
	st, hdr, raw := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(raw, png) || !strings.HasPrefix(hdr.Get("Content-Type"), "image/png") || !strings.Contains(hdr.Get("Content-Disposition"), "fixture") {
		t.Fatalf("下载应是登记字节: %d type=%s disp=%s n=%d", st, hdr.Get("Content-Type"), hdr.Get("Content-Disposition"), len(raw))
	}
	if spy.hold.Load() != 0 || spy.release.Load() != 0 || spy.settle.Load() != 0 {
		t.Fatalf("夹具路径不能调用资金动作 hold=%d release=%d settle=%d", spy.hold.Load(), spy.release.Load(), spy.settle.Load())
	}
}

func TestTextImageClientClaimAndForeignAssetStayHidden(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageFixtureRegister = true
	})
	_, tok := f.loginOK(t)
	stLogin, logged := f.login(t, "b@x.com", "right-pass")
	tokB, _ := logged["access_token"].(string)
	if stLogin != http.StatusOK || tokB == "" {
		t.Fatalf("第二租户登录失败: %d %v", stLogin, logged)
	}
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "fixture 金属壶")
	png := tinyPNG(t, 6, 6)
	body := f.registerTextFixture(t, tok, proj, jobID, png, http.StatusCreated)
	job, _ := body["job"].(map[string]any)
	asset, _ := job["output_asset_id"].(string)
	st, claimed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/claim", tok, map[string]any{
		"claim": "success", "output_asset_id": "filled-output",
	})
	if st != http.StatusConflict {
		t.Fatalf("客户端不能把夹具改成模型成功: %d %v", st, claimed)
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, map[string]any{
		"output_asset_id": "filled-output", "job_status": "succeeded",
	})
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["output_asset_id"] != asset || job["show_image"] != true || job["origin"] != "fixture" || job["real_generation_completed"] != false {
		t.Fatalf("任意资产编号不能换掉夹具: %d %#v", st, job)
	}
	st, foreign := f.do(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID, tokB, nil)
	if st != http.StatusNotFound {
		t.Fatalf("别的租户不能看详情: %d %v", st, foreign)
	}
	st, _, _ = f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tokB, nil)
	if st != http.StatusNotFound {
		t.Fatalf("别的租户不能下载: %d", st)
	}
}

func TestTextImageSameKeyReturnsFixtureWithoutNewBytes(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageFixtureRegister = true
	})
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	prompt := "fixture 蓝色水壶"
	jobID := f.openAndConfirmText(t, tok, proj, prompt)
	png := tinyPNG(t, 5, 5)
	first := f.registerTextFixture(t, tok, proj, jobID, png, http.StatusCreated)
	asset := first["job"].(map[string]any)["output_asset_id"]
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{"prompt": prompt})
	job, _ := again["job"].(map[string]any)
	if st != http.StatusOK || job["id"] != jobID || job["idempotent"] != true || job["show_image"] != true || job["output_asset_id"] != asset {
		t.Fatalf("应答丢失后应按原键查回: %d %#v", st, job)
	}
	second := f.registerTextFixture(t, tok, proj, jobID, png, http.StatusOK)
	if second["idempotent"] != true || second["job"].(map[string]any)["output_asset_id"] != asset {
		t.Fatalf("同一夹具应幂等: %#v", second)
	}
}

func TestTextImageLateFixtureDoesNotReplaceSelection(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageFixtureRegister = true
	})
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "fixture 选定版本")
	firstPNG := tinyPNG(t, 4, 4)
	latePNG := tinyPNG(t, 7, 7)
	body := f.registerTextFixture(t, tok, proj, jobID, firstPNG, http.StatusCreated)
	asset := body["job"].(map[string]any)["output_asset_id"]
	st, selected := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/select", tok, nil)
	job, _ := selected["job"].(map[string]any)
	if st != http.StatusOK || job["selected_version"] == "" || job["selected_version"] != job["result_version"] || job["output_asset_id"] != asset {
		t.Fatalf("选定应锁住当前夹具: %d %#v", st, job)
	}
	late := f.registerTextFixture(t, tok, proj, jobID, latePNG, http.StatusOK)
	if late["late_result_ignored"] != true || late["job"].(map[string]any)["output_asset_id"] != asset {
		t.Fatalf("晚到结果不能覆盖选定版本: %#v", late)
	}
	st, _, raw := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(raw, firstPNG) {
		t.Fatalf("下载仍应是选定前的原字节: %d n=%d", st, len(raw))
	}
}

func TestTextImageCredentialDoesNotPaintModelImage(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageModelCredential = "secret-model-key"
	})
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "红色水壶")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["show_image"] != false || job["output_asset_id"] != "" || job["real_generation_completed"] != false || job["real_generation_notice"] != "真实出图未完成" {
		t.Fatalf("有凭证字符串也不能画模型图: %d %#v", st, job)
	}
	if bytes.Contains(raw, []byte("secret-model-key")) {
		t.Fatal("凭证不能出现在应答里")
	}
}

type modelRoundTrip struct {
	n        atomic.Int32
	png      []byte
	status   int
	lastAuth string
	lastPath string
	lastBody string
}

func (m *modelRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	m.n.Add(1)
	m.lastAuth = r.Header.Get("Authorization")
	m.lastPath = r.URL.Path
	raw, _ := io.ReadAll(r.Body)
	m.lastBody = string(raw)
	code := m.status
	if code == 0 {
		code = http.StatusOK
	}
	payload := `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(m.png) + `"}]}`
	if code != http.StatusOK {
		payload = `{"error":{"message":"denied"}}`
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(payload)),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func TestTextImageModelPostStoresDecodedBytesWithoutBilling(t *testing.T) {
	png := tinyPNG(t, 7, 5)
	rt := &modelRoundTrip{png: png}
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageModelCredential = "secret-model-key"
		c.TextImageModelURL = "https://images.example/v1"
		c.TextImageModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "白色陶瓷杯棚拍")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "completed" || job["origin"] != "model_http" || job["show_image"] != true || job["supplier_image_decoded"] != true {
		t.Fatalf("模型字节应可查看: %d %#v", st, job)
	}
	if job["real_generation_completed"] != false || job["real_generation_notice"] != "真实出图未完成" || job["billing_passed"] != false || job["production_authorized"] != false || job["charged"] != nil {
		t.Fatalf("解码不是保真、计费或生产通过: %#v", job)
	}
	if job["status_label"] != "供应商图片已解码" || job["honesty"] != "图像模型已返回字节。主体保真未核实，计费未通过，生产未授权。" || job["fixture_notice"] != "" {
		t.Fatalf("模型来源文案不对: %#v", job)
	}
	asset, _ := job["output_asset_id"].(string)
	if !strings.HasPrefix(asset, "img_") || len(asset) != 24 {
		t.Fatalf("资产编号应来自摘要: %q", asset)
	}
	if rt.n.Load() != 1 || rt.lastAuth != "Bearer secret-model-key" || !strings.HasSuffix(rt.lastPath, "/images/generations") || !strings.Contains(rt.lastBody, "qwen-image-2.0-pro") {
		t.Fatalf("应只向生成端点提交一次: n=%d auth=%q path=%s body=%s", rt.n.Load(), rt.lastAuth, rt.lastPath, rt.lastBody)
	}
	if bytes.Contains(raw, []byte("secret-model-key")) {
		t.Fatal("凭证不能出现在应答里")
	}
	st, hdr, body := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(body, png) || hdr.Get("X-Text-Image-Origin") != "model_http" || !strings.Contains(hdr.Get("Content-Disposition"), "model.png") {
		t.Fatalf("下载应是模型字节: %d origin=%s disp=%s n=%d", st, hdr.Get("X-Text-Image-Origin"), hdr.Get("Content-Disposition"), len(body))
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	againJob, _ := again["job"].(map[string]any)
	if st != http.StatusOK || againJob["id"] != jobID || againJob["output_asset_id"] != asset || rt.n.Load() != 1 {
		t.Fatalf("再次提交不能重画: n=%d %#v", rt.n.Load(), againJob)
	}
}

func TestTextImageProjectCanvasSizeIsRequestedAndMismatchIsNotAPass(t *testing.T) {
	png := tinyPNG(t, 5, 4)
	rt := &modelRoundTrip{png: png}
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageModelCredential = "secret-model-key"
		c.TextImageModelURL = "https://images.example/v1"
		c.TextImageModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{
		"name": "电商文字", "source_type": "standalone", "width_px": 800, "height_px": 800,
	})
	if st != http.StatusCreated {
		t.Fatalf("建工程 %d %v", st, created)
	}
	proj := created["project"].(map[string]any)["id"].(string)
	jobID := f.openAndConfirmText(t, tok, proj, "白色陶瓷杯，800 主图")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	pending, _ := json.Marshal(job["pending"])
	if st != http.StatusOK || !strings.Contains(rt.lastBody, `"size":"800x800"`) {
		t.Fatalf("文字生成应按项目画布请求尺寸: %d body=%s", st, rt.lastBody)
	}
	if !bytes.Contains(pending, []byte("输出尺寸与项目不一致")) || job["real_generation_completed"] != false || job["billing_passed"] != false || job["production_authorized"] != false {
		t.Fatalf("尺寸不一致不能写成出图或计费通过: pending=%s %#v", pending, job)
	}
}

func TestTextImageLoopbackModelBytesStayUnknown(t *testing.T) {
	png := tinyPNG(t, 4, 4)
	rt := &modelRoundTrip{png: png}
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageModelCredential = "secret-model-key"
		c.TextImageModelURL = "http://127.0.0.1:9/v1"
		c.TextImageModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "回环不能算供应商")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	pending, _ := json.Marshal(job["pending"])
	if st != http.StatusOK || job["job_status"] != "unknown" || job["show_image"] != false || job["output_asset_id"] != "" || job["supplier_image_decoded"] != false {
		t.Fatalf("回环字节不能展示: %d %#v", st, job)
	}
	if !bytes.Contains(pending, []byte("回环地址不能算供应商出图")) || bytes.Contains(raw, []byte("secret-model-key")) || rt.n.Load() != 1 {
		t.Fatalf("回环应记下原因且只调用一次: n=%d %s", rt.n.Load(), pending)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	if st != http.StatusOK || rt.n.Load() != 1 {
		t.Fatalf("未知结果再次提交不能重画: %d n=%d %#v", st, rt.n.Load(), again)
	}
	st, _, _ = f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("回环结果不能下载: %d", st)
	}
}

type textPaintSpy struct {
	n   atomic.Int32
	png []byte
}

func (p *textPaintSpy) Paint(context.Context, string) ([]byte, error) {
	p.n.Add(1)
	return append([]byte(nil), p.png...), nil
}

type textTaskScript struct {
	mu     sync.Mutex
	status string
	asset  string
	data   string
	polls  atomic.Int32
}

func (s *textTaskScript) set(status, asset, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.asset, s.data = status, asset, data
}

func (s *textTaskScript) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		_, _ = w.Write([]byte(`{"task_id":"task_text_real","status":"pending"}`))
		return
	}
	s.polls.Add(1)
	s.mu.Lock()
	status, asset, data := s.status, s.asset, s.data
	s.mu.Unlock()
	result := map[string]any{}
	if asset != "" {
		result["platform_asset_id"] = asset
	}
	if data != "" {
		result["data_b64"] = data
	}
	raw, _ := json.Marshal(map[string]any{"task_id": "task_text_real", "status": status, "result": result})
	_, _ = w.Write(raw)
}

func (f *fixture) armTextTask(t *testing.T, script *textTaskScript, paint *textPaintSpy) {
	t.Helper()
	taskSrv := httptest.NewServer(http.HandlerFunc(script.serve))
	t.Cleanup(taskSrv.Close)
	f.srv.Cfg.TextToImageEnabled = true
	f.srv.Cfg.GenerationEnabled = true
	f.srv.Cfg.TaskBaseURL = taskSrv.URL
	f.srv.Cfg.UploadBaseURL = "http://127.0.0.1:1"
	f.srv.Cfg.UploadToken = "up"
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.srv.TextPaint = paint
	f.rearm(t)
}

func TestTextImageServerTaskAssetIsShownWithoutSettlement(t *testing.T) {
	png := tinyPNG(t, 8, 6)
	paint := &textPaintSpy{png: png}
	script := &textTaskScript{status: "succeeded", asset: "asset_real"}
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageModelCredential = "secret-model-key"
	})
	funds := &textFundsSpy{}
	f.srv.Bills = funds
	f.armTextTask(t, script, paint)
	_, tok := f.loginOK(t)
	_, logged := f.login(t, "b@x.com", "right-pass")
	tokB, _ := logged["access_token"].(string)
	proj := f.standaloneProject(t, tok)
	const prompt = "服务端任务白色陶瓷杯"
	jobID := f.openAndConfirmText(t, tok, proj, prompt)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" || job["show_image"] != false || job["platform_task_id"] != "task_text_real" || paint.n.Load() != 0 {
		t.Fatalf("受理任务还不能出图: %d %#v paints=%d", st, job, paint.n.Load())
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, map[string]any{
		"output_asset_id": "filled-output", "job_status": "success",
	})
	raw, _ := json.Marshal(refreshed)
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "completed" || job["origin"] != "server" || job["output_asset_id"] != "asset_real" || job["show_image"] != true {
		t.Fatalf("服务端资产应可展示: %d %#v", st, job)
	}
	if job["billing_passed"] != false || job["production_authorized"] != false || job["charged"] != nil || job["production_authorization"] != "NOT_AUTHORIZED" || job["settlement"] != "作品完成待核对" {
		t.Fatalf("生成完成不是结算: %#v", job)
	}
	if job["real_generation_completed"] != false || job["real_generation_notice"] != "真实出图未完成" || job["subject_protected"] != false || !strings.Contains(fmtAny(job["subject_notice"]), "不宣称主体保真") {
		t.Fatalf("本地测试图不是模型出图或主体保真: %#v", job)
	}
	if bytes.Contains(raw, []byte("secret-model-key")) || paint.n.Load() != 1 {
		t.Fatalf("凭证不能回传，本地出图应只调用一次: paints=%d", paint.n.Load())
	}
	st, hdr, body := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(body, png) || hdr.Get("X-Text-Image-Origin") != "server" {
		t.Fatalf("下载应是服务端保存的字节: %d origin=%s n=%d", st, hdr.Get("X-Text-Image-Origin"), len(body))
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{"prompt": prompt})
	againJob, _ := again["job"].(map[string]any)
	if st != http.StatusOK || againJob["id"] != jobID || againJob["idempotent"] != true || againJob["output_asset_id"] != "asset_real" {
		t.Fatalf("同一描述应回到原任务: %d %#v", st, againJob)
	}
	st, selected := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/select", tok, nil)
	selectedJob, _ := selected["job"].(map[string]any)
	if st != http.StatusOK || selectedJob["selected_version"] != "asset_real" || selectedJob["output_asset_id"] != "asset_real" {
		t.Fatalf("选定应锁住服务端资产: %d %#v", st, selectedJob)
	}
	script.set("succeeded", "asset_late", base64.StdEncoding.EncodeToString(tinyPNG(t, 2, 2)))
	st, late := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, nil)
	lateJob, _ := late["job"].(map[string]any)
	if st != http.StatusOK || lateJob["output_asset_id"] != "asset_real" || lateJob["show_image"] != true || script.polls.Load() != 1 || paint.n.Load() != 1 {
		t.Fatalf("晚到结果不能覆盖选定版本: polls=%d paints=%d %#v", script.polls.Load(), paint.n.Load(), lateJob)
	}
	st, _, kept := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(kept, png) {
		t.Fatalf("下载仍应是选定前的字节: %d n=%d", st, len(kept))
	}
	if funds.hold.Load() != 0 || funds.release.Load() != 0 || funds.settle.Load() != 0 {
		t.Fatalf("展示不能调用资金动作")
	}
	st, _, _ = f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tokB, nil)
	if st != http.StatusNotFound {
		t.Fatalf("别的租户不能下载: %d", st)
	}
}

func TestTextImageRefreshIgnoresClientAssetAndBadPayload(t *testing.T) {
	png := tinyPNG(t, 4, 4)
	paint := &textPaintSpy{png: png}
	script := &textTaskScript{status: "succeeded"}
	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
	f.armTextTask(t, script, paint)
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "没有资产的成功")
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	st, bare := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, map[string]any{
		"output_asset_id": "filled-output", "job_status": "succeeded",
	})
	job, _ := bare["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" || job["show_image"] != false || job["output_asset_id"] != "" || paint.n.Load() != 0 {
		t.Fatalf("没有服务端资产时不能展示客户端编号: %d %#v", st, job)
	}
	script.set("succeeded", "asset_real", base64.StdEncoding.EncodeToString([]byte("not-an-image")))
	st, bad := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, nil)
	job, _ = bad["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" || job["show_image"] != false || job["output_asset_id"] != "" || paint.n.Load() != 0 {
		t.Fatalf("坏的任务载荷不能改用本地图: %d %#v paints=%d", st, job, paint.n.Load())
	}
}

func TestTextImageTaskBytesAreStoredInsteadOfLocalPaint(t *testing.T) {
	taskPNG := tinyPNG(t, 5, 3)
	paint := &textPaintSpy{png: tinyPNG(t, 9, 9)}
	script := &textTaskScript{status: "succeeded", asset: "asset_real", data: base64.StdEncoding.EncodeToString(taskPNG)}
	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
	f.armTextTask(t, script, paint)
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "任务自带字节")
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/refresh", tok, nil)
	job, _ := refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["show_image"] != true || job["origin"] != "server" || job["output_asset_id"] != "asset_real" || paint.n.Load() != 0 {
		t.Fatalf("任务字节应直接保存，不调用本地出图: %d %#v paints=%d", st, job, paint.n.Load())
	}
	st, _, body := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(body, taskPNG) || job["real_generation_completed"] != false {
		t.Fatalf("下载应是任务带回的字节，仍不是模型出图: %d n=%d %#v", st, len(body), job)
	}
}

func TestTextImageFixtureRouteHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "fixture 未开关注册")
	st, body := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/fixture", tok, map[string]any{
		"fixture": "fixture", "data_b64": base64.StdEncoding.EncodeToString(tinyPNG(t, 3, 3)),
	})
	if st != http.StatusNotFound {
		t.Fatalf("默认不能登记夹具: %d %v", st, body)
	}
	st, listed := f.do(t, "GET", "/api/v1/projects/"+proj+"/text-images", tok, nil)
	job := listed["jobs"].([]any)[0].(map[string]any)
	if st != http.StatusOK || job["show_image"] != false || job["job_status"] == "completed" {
		t.Fatalf("关闭开关时不能出现完成图: %d %#v", st, job)
	}
}

func TestModelTextImageBytesStayUnusableCandidates(t *testing.T) {
	png := tinyPNG(t, 7, 5)
	rt := &modelRoundTrip{png: png}
	f := newFixture(t, func(c *config.Config) {
		c.TextToImageEnabled = true
		c.TextImageModelCredential = "secret-model-key"
		c.TextImageModelURL = "https://images.example/v1"
		c.TextImageModelName = "qwen-image-2.0-pro"
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up-tok"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj := f.standaloneProject(t, tok)
	jobID := f.openAndConfirmText(t, tok, proj, "未保真文字图")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["origin"] != "model_http" || job["show_image"] != true || job["real_generation_completed"] != false {
		t.Fatalf("模型字节应可查看但未保真: %d %#v", st, job)
	}
	st, selected := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/select", tok, nil)
	assertNotUsableCandidate(t, st, selected)
	st, got := f.do(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID, tok, nil)
	gotJob, _ := got["job"].(map[string]any)
	if st != http.StatusOK || gotJob["selected_version"] != "" {
		t.Fatalf("拒绝后不能留下选定版本: %d %#v", st, gotJob)
	}
	st, hdr, body := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/download", tok, nil)
	if st != http.StatusOK || !bytes.Equal(body, png) || hdr.Get("X-Text-Image-Origin") != "model_http" {
		t.Fatalf("下载查看仍然允许: %d origin=%s n=%d", st, hdr.Get("X-Text-Image-Origin"), len(body))
	}
	eco := newEcoStub(t)
	f.srv.Uploads = &platform.UploadClient{BaseURL: eco.srv.URL, AppID: "product-image", Token: "up-tok"}
	st, registered := f.do(t, "POST", "/api/v1/projects/"+proj+"/outputs", tok, map[string]any{
		"file_name": "model.png", "content_type": "image/png", "data_b64": b64(png),
	})
	assertNotUsableCandidate(t, st, registered)
	if eco.uploads.Load() != 0 || rt.n.Load() != 1 {
		t.Fatalf("挡住候选不能登记或重画: uploads=%d n=%d", eco.uploads.Load(), rt.n.Load())
	}
	st, other := f.do(t, "POST", "/api/v1/projects/"+proj+"/outputs", tok, map[string]any{
		"file_name": "other.png", "content_type": "image/png", "data_b64": b64(tinyPNG(t, 3, 3)),
	})
	if st != http.StatusCreated || eco.uploads.Load() != 1 {
		t.Fatalf("其他图片仍可登记: %d uploads=%d %#v", st, eco.uploads.Load(), other)
	}
}

func trustedFixtureJob(t *testing.T, job map[string]any) bool {
	t.Helper()
	return job["show_image"] == true && job["origin"] == "fixture" && job["job_status"] == "completed" &&
		job["output_asset_id"] != "" && job["output_asset_id"] != "filled-output" &&
		job["charged"] == nil && job["billing_passed"] == false && job["production_authorized"] == false &&
		job["production_authorization"] == "NOT_AUTHORIZED" && job["settlement"] == "作品完成待核对" &&
		job["real_generation_completed"] == false && job["real_generation_notice"] == "真实出图未完成" &&
		job["subject_protected"] == false && strings.Contains(fmtAny(job["subject_notice"]), "不宣称主体保真") &&
		job["billing_label"] == "配置缺失"
}

func fmtAny(v any) string {
	s, _ := v.(string)
	return s
}

func (f *fixture) registerTextFixture(t *testing.T, tok, proj, jobID string, png []byte, want int) map[string]any {
	t.Helper()
	st, body := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/fixture", tok, map[string]any{
		"fixture": "fixture", "data_b64": base64.StdEncoding.EncodeToString(png),
	})
	if st != want {
		t.Fatalf("登记夹具 %d 期望 %d %v", st, want, body)
	}
	return body
}

type textFundsSpy struct {
	hold, release, settle atomic.Int32
}

func (s *textFundsSpy) Quote(context.Context, billassemble.QuoteCall) (billassemble.QuoteFact, error) {
	return billassemble.QuoteFact{}, billassemble.ErrUnimplemented
}
func (s *textFundsSpy) Hold(context.Context, billassemble.HoldCall) (billassemble.HoldFact, error) {
	s.hold.Add(1)
	return billassemble.HoldFact{}, billassemble.ErrFundsNotExecuted
}
func (s *textFundsSpy) Release(context.Context, billassemble.ReleaseCall) (billassemble.ReleaseFact, error) {
	s.release.Add(1)
	return billassemble.ReleaseFact{}, billassemble.ErrFundsNotExecuted
}
func (s *textFundsSpy) Settle(context.Context, billassemble.SettleCall) (billassemble.SettleFact, error) {
	s.settle.Add(1)
	return billassemble.SettleFact{}, billassemble.ErrFundsNotExecuted
}
func (s *textFundsSpy) Lookup(context.Context, billassemble.LookupCall) (billassemble.LookupFact, error) {
	return billassemble.LookupFact{}, billassemble.ErrUnimplemented
}
func (s *textFundsSpy) ReadBalance(context.Context, string) (billassemble.Balance, error) {
	return billassemble.Balance{}, billassemble.ErrUnimplemented
}

func (f *fixture) standaloneProject(t *testing.T, tok string) string {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "文字", "source_type": "standalone"})
	if st != http.StatusCreated {
		t.Fatalf("建工程 %d %v", st, created)
	}
	return created["project"].(map[string]any)["id"].(string)
}

func (f *fixture) openAndConfirmText(t *testing.T, tok, proj, prompt string) string {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images", tok, map[string]any{"prompt": prompt})
	if st != http.StatusCreated {
		t.Fatalf("开文字请求 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/text-images/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d %v", st, conf)
	}
	return jobID
}
