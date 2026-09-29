package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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
