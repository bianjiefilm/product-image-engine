package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
)

func TestBackgroundReplaceHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, nil)
	_, tok := f.loginOK(t)
	st, _ := f.do(t, "GET", "/api/v1/background-replacements/capabilities", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("开关关闭应 404, got %d", st)
	}
}

func TestBackgroundReplaceQuoteBillingPendingAndNoFakePass(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)

	st, capBody := f.do(t, "GET", "/api/v1/background-replacements/capabilities", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("能力接口 %d %v", st, capBody)
	}
	if capBody["billing_label"] != "计费待确认" || capBody["production_generation_passed"] != false || capBody["billing_passed"] != false {
		t.Fatalf("能力接口不得宣称出图或计费已通过: %#v", capBody)
	}
	if capBody["creative_available"] != false {
		t.Fatalf("默认不得开放创意模式: %#v", capBody)
	}

	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements", tok, map[string]any{
		"input_id": in, "background_intent": "室内棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["billing_label"] != "计费待确认" || job["mode"] != "fidelity" || job["quality"] != "unknown" {
		t.Fatalf("报价不诚实: %#v", job)
	}
	if job["production_generation_passed"] != false || job["billing_passed"] != false {
		t.Fatalf("报价宣称已通过: %#v", job)
	}

	st, denied := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements", tok, map[string]any{
		"input_id": in, "mode": "creative",
	})
	if st != http.StatusUnprocessableEntity {
		t.Fatalf("创意模式应拒绝, got %d %v", st, denied)
	}
}

func TestBackgroundReplaceSubmitDoesNotDoubleChargeTask(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"task_bg_1","status":"pending"}`))
	}))
	t.Cleanup(taskSrv.Close)

	f := newFixture(t, func(c *config.Config) {
		c.BgReplaceEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirm(t, tok, proj, in)

	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, map[string]any{})
	if st != http.StatusOK {
		t.Fatalf("提交 %d %v", st, submitted)
	}
	job, _ := submitted["job"].(map[string]any)
	if job["job_status"] != "queued" || job["platform_task_id"] != "task_bg_1" || job["quality"] != "unknown" {
		t.Fatalf("提交后仍应质量未知: %#v", job)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, map[string]any{})
	if st != http.StatusOK {
		t.Fatalf("重复提交 %d %v", st, again)
	}
	if calls.Load() != 1 {
		t.Fatalf("同指纹只能调用一次平台任务, got %d", calls.Load())
	}
	againJob, _ := again["job"].(map[string]any)
	if againJob["idempotent"] != true {
		t.Fatalf("重复提交应标记幂等: %#v", againJob)
	}
}

func TestBackgroundReplaceUnknownDoesNotRegenerate(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.BgReplaceEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirm(t, tok, proj, in)
	st, first := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := first["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" {
		t.Fatalf("任务不可达应为 unknown, got %d %#v", st, job)
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if calls.Load() != 1 {
		t.Fatalf("unknown 后不得再次提交任务, calls=%d", calls.Load())
	}
}

func TestBackgroundReplaceFailureStaysFidelityAndExportReusesVersion(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirm(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "generation_unavailable" {
		t.Fatalf("生成关闭时应不可用而非假成功: %d %#v", st, job)
	}

	st, q := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/quality", tok, map[string]any{"verdict": "pass"})
	if st != http.StatusConflict {
		t.Fatalf("无证据不得标 pass, got %d %v", st, q)
	}
	st, failed := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/quality", tok, map[string]any{"verdict": "fail"})
	job, _ = failed["job"].(map[string]any)
	if st != http.StatusOK || job["quality"] != "fail" || job["mode"] != "fidelity" {
		t.Fatalf("失败应留在保真模式: %d %#v", st, job)
	}
	st, sel := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/select", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("失败不能选定交付, got %d %v", st, sel)
	}

	st, samples := f.do(t, "GET", "/api/v1/background-replacements/acceptance", tok, nil)
	list, _ := samples["samples"].([]any)
	if st != http.StatusOK || len(list) != 2 {
		t.Fatalf("验收样本 %d %#v", st, samples)
	}
}

func (f *fixture) loginOK(t *testing.T) (int, string) {
	t.Helper()
	st, resp := f.login(t, "a@x.com", "right-pass")
	if st != http.StatusOK {
		t.Fatalf("登录 %d %v", st, resp)
	}
	tok, _ := resp["access_token"].(string)
	return st, tok
}

func (f *fixture) projectWithInput(t *testing.T, tok string) (string, string) {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "背景", "source_type": "standalone"})
	if st != http.StatusCreated {
		t.Fatalf("建工程 %d %v", st, created)
	}
	proj := created["project"].(map[string]any)["id"].(string)
	st, in := f.do(t, "POST", "/api/v1/projects/"+proj+"/inputs", tok, map[string]any{
		"platform_asset_id": "asset_in", "snapshot_name": "pack.png",
	})
	if st != http.StatusCreated {
		t.Fatalf("挂输入 %d %v", st, in)
	}
	return proj, in["input"].(map[string]any)["id"].(string)
}

func (f *fixture) openAndConfirm(t *testing.T, tok, proj, in string) string {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements", tok, map[string]any{
		"input_id": in, "background_intent": "户外",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d %v", st, conf)
	}
	return jobID
}
