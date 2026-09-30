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
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
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
	if capBody["billing_label"] != "配置缺失" || capBody["production_generation_passed"] != false || capBody["billing_passed"] != false || capBody["settlement"] != "unknown" {
		t.Fatalf("能力接口不得宣称出图或计费已通过: %#v", capBody)
	}
	if capBody["creative_available"] != false {
		t.Fatalf("默认不得开放创意模式: %#v", capBody)
	}
	if capBody["real_generation_completed"] != false || capBody["real_generation_notice"] != bgreplace.RealGenerationIncomplete {
		t.Fatalf("没有真实模型凭证不得宣称出图完成: %#v", capBody)
	}

	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements", tok, map[string]any{
		"input_id": in, "background_intent": "室内棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["billing_label"] != "配置缺失" || job["mode"] != "fidelity" || job["quality"] != "unknown" || job["settlement"] != "unknown" || job["billing_passed"] != false {
		t.Fatalf("报价不诚实: %#v", job)
	}
	if job["protected_region"] != bgreplace.ProtectionScope || job["real_generation_notice"] != bgreplace.RealGenerationIncomplete {
		t.Fatalf("保护范围或真实出图说明不对: %#v", job)
	}
	wantFP := bgreplace.Fingerprint(deriveTenant("a@x.com"), proj, in, job["input_version"].(string), "fidelity", bgreplace.ProtectionScope, "室内棚拍")
	if job["fingerprint"] != wantFP {
		t.Fatalf("指纹未绑定模式、保护范围和输入版本: got %v want %s", job["fingerprint"], wantFP)
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
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	againJob, _ := again["job"].(map[string]any)
	if st != http.StatusOK || againJob["quality"] != "fail" || againJob["idempotent"] != true {
		t.Fatalf("再次提交不得清掉失败结论: %d %#v", st, againJob)
	}
	st, unknown := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/quality", tok, map[string]any{"verdict": "unknown"})
	if st != http.StatusConflict {
		t.Fatalf("失败不能改回未知, got %d %v", st, unknown)
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

func TestBackgroundReplaceRejectedSubmitDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad", http.StatusBadRequest)
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
		t.Fatalf("拒绝应答应停在未知且不留下可重提状态, got %d %#v", st, job)
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if calls.Load() != 1 {
		t.Fatalf("4xx 后不得再次提交任务, calls=%d", calls.Load())
	}
}

func TestBackgroundReplaceConfirmRejectsStaleForm(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements", tok, map[string]any{
		"input_id": in, "background_intent": "室内",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/confirm", tok, map[string]any{
		"mode": "fidelity", "input_id": in, "background_intent": "户外",
	})
	if st != http.StatusConflict {
		t.Fatalf("意图变化应拒绝确认, got %d %v", st, conf)
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

func TestBackgroundReplaceQuotaFailureRefreshAndModelFailureStayOnOriginalTask(t *testing.T) {
	var posts, polls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"error":"insufficient_quota","message":"额度不足"}`))
			return
		}
		polls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"task_id":"should-not-poll","status":"succeeded","result":{"asset_id":"asset_sneak"}}`))
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
	if st != http.StatusOK || job["job_status"] != bgreplace.StatusQuotaInsufficient || job["mode"] != "fidelity" {
		t.Fatalf("额度不足应停在原任务: %d %#v", st, job)
	}
	if job["real_generation_completed"] != false || job["deliverable"] != false {
		t.Fatalf("额度不足不得写成出图成功: %#v", job)
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/refresh", tok, nil)
	if posts.Load() != 1 || polls.Load() != 0 {
		t.Fatalf("额度不足后不得重试或改查成功, posts=%d polls=%d", posts.Load(), polls.Load())
	}

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"model_failed"}`))
			return
		}
		polls.Add(1)
		_, _ = w.Write([]byte(`{"status":"succeeded","result":{"asset_id":"asset_sneak"}}`))
	}))
	t.Cleanup(failSrv.Close)
	f.srv.Tasks = &platform.TaskClient{BaseURL: failSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok = f.loginOK(t)
	proj, in = f.projectWithInput(t, tok)
	jobID = f.openAndConfirm(t, tok, proj, in)
	before := posts.Load()
	st, failed := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ = failed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["mode"] != "fidelity" {
		t.Fatalf("模型失败应恢复原任务: %d %#v", st, job)
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/refresh", tok, nil)
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if posts.Load() != before+1 || polls.Load() != 0 {
		t.Fatalf("模型失败不得重生成或被刷新改成成功, posts=%d polls=%d", posts.Load(), polls.Load())
	}
}

func TestBackgroundReplaceInspectBlocksSimilarityMaskAndReusesExport(t *testing.T) {
	var posts atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"task_bg_real","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task_bg_real","status":"succeeded","result":{"asset_id":"asset_out","model_ref":"model_bg","fee_ref":"fee_bg"}}`))
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
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("提交 %d %v", st, submitted)
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/refresh", tok, nil)
	job, _ := refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "completed" || job["output_asset_id"] != "asset_out" {
		t.Fatalf("应记下真实引用但未完成出图声明: %d %#v", st, job)
	}
	if job["model_ref"] != "model_bg" || job["platform_task_id"] != "task_bg_real" || job["fee_ref"] != "fee_bg" {
		t.Fatalf("模型/任务/费用引用缺失: %#v", job)
	}
	if job["real_generation_completed"] != false || job["real_generation_notice"] != bgreplace.RealGenerationIncomplete || job["production_generation_passed"] != false || job["deliverable"] != false {
		t.Fatalf("没有凭证不得伪造出图成功: %#v", job)
	}
	st, only := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/inspect", tok, map[string]any{
		"similarity_only": true, "similarity": 0.99,
	})
	job, _ = only["job"].(map[string]any)
	if st != http.StatusOK || job["quality"] != "unknown" || job["deliverable"] != false {
		t.Fatalf("只有相似度不得通过: %d %#v", st, job)
	}
	st, selected := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/select", tok, nil)
	job, _ = selected["job"].(map[string]any)
	if st != http.StatusOK || job["selection"] != "selected" || job["deliverable"] != false {
		t.Fatalf("未知结果可以选定同一版本,但不能当可交付: %d %#v", st, job)
	}
	st, exported := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/export", tok, nil)
	job, _ = exported["job"].(map[string]any)
	if st != http.StatusOK || job["output_asset_id"] != "asset_out" || job["output_version"] == "" || job["starts_generation"] != false {
		t.Fatalf("导出必须复用同一结果: %d %#v", st, job)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/export", tok, nil)
	job, _ = again["job"].(map[string]any)
	if st != http.StatusOK || job["export_count"] != float64(2) || job["output_asset_id"] != "asset_out" {
		t.Fatalf("再次导出仍应是同一资产: %d %#v", st, job)
	}

	st, masked := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/inspect", tok, map[string]any{
		"logo": "pass", "packaging_text": "fail", "spec": "pass", "structure": "fail", "similarity": 0.99,
	})
	job, _ = masked["job"].(map[string]any)
	if st != http.StatusOK || job["quality"] != "fail" || job["mode"] != "fidelity" || job["deliverable"] != false {
		t.Fatalf("商品错误不得被相似度掩盖: %d %#v", st, job)
	}
	st, blocked := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/select", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("失败候选不能选定, got %d %v", st, blocked)
	}
	st, blockedExport := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/export", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("失败候选不能导出为可交付, got %d %v", st, blockedExport)
	}
	if posts.Load() != 1 {
		t.Fatalf("检查和导出不得再次生成, posts=%d", posts.Load())
	}
}

func TestBackgroundReplaceFidelityFailureBlocksDeliverable(t *testing.T) {
	var posts atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"task_bg_fid","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task_bg_fid","status":"succeeded","result":{"asset_id":"asset_fid"}}`))
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
	if st, body := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil); st != http.StatusOK {
		t.Fatalf("提交 %d %v", st, body)
	}
	if st, body := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/refresh", tok, nil); st != http.StatusOK {
		t.Fatalf("刷新 %d %v", st, body)
	}
	rep, err := fidelity.Evaluate(fidelity.Input{
		Mode: fidelity.ModeFidelity, MethodVersion: fidelity.MethodVersionV1,
		InputRef: in, InputVersion: "v-test", OutputRef: "", MaskRef: "mask-1",
		AllowedRegions: []string{"background"}, Protected: fidelity.RequiredProtected,
		SampleClass: fidelity.SampleLogoText, MachineScope: "边缘", HumanScope: "包装文字",
		Checks: []fidelity.Check{
			{Name: fidelity.CheckProductCount, Result: fidelity.ResultPass},
			{Name: fidelity.CheckLogoText, Result: fidelity.ResultFail},
			{Name: fidelity.CheckShape, Result: fidelity.ResultPass},
			{Name: fidelity.CheckKeyTexture, Result: fidelity.ResultPass},
			{Name: fidelity.CheckDeclaredColor, Result: fidelity.ResultPass},
		},
	})
	if err != nil || rep.Verdict != fidelity.VerdictFail {
		t.Fatalf("保真样本应失败: %v %#v", err, rep)
	}
	if _, _, err := f.st.SaveFidelityReport(context.Background(), deriveTenant("a@x.com"), proj, rep); err != nil {
		t.Fatal(err)
	}
	st, selected := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/select", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("主体保真失败不能选定, got %d %v", st, selected)
	}
	if posts.Load() != 1 {
		t.Fatalf("保真拦截不得重生成, posts=%d", posts.Load())
	}
}

func assertBgBillingAndProductionStayFalse(t *testing.T, job map[string]any) {
	t.Helper()
	if job["billing_passed"] != false || job["production_generation_passed"] != false || job["production_authorized"] != false {
		t.Fatalf("计费与生产必须保持未通过: %#v", job)
	}
	if job["real_generation_completed"] != false || job["real_generation_notice"] != bgreplace.RealGenerationIncomplete {
		t.Fatalf("真实出图文案必须保持未完成: %#v", job)
	}
	if job["deliverable"] != false || job["quality"] == "pass" {
		t.Fatalf("解码字节不是可交付保真通过: %#v", job)
	}
}

type bgModelRT struct {
	n      atomic.Int32
	png    []byte
	status int
	auth   string
	path   string
	host   string
	body   string
}

func (m *bgModelRT) RoundTrip(r *http.Request) (*http.Response, error) {
	m.n.Add(1)
	m.auth = r.Header.Get("Authorization")
	m.path = r.URL.Path
	m.host = r.URL.Host
	raw, _ := io.ReadAll(r.Body)
	m.body = string(raw)
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

type denyModelSecret struct {
	t      *testing.T
	secret string
	base   http.RoundTripper
}

func (d denyModelSecret) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.Contains(r.Header.Get("Authorization"), d.secret) || strings.Contains(r.Header.Get("X-PilotSeaView-Internal-Token"), d.secret) {
		d.t.Fatal("模型凭证不能跟到取原图的主机")
	}
	return d.base.RoundTrip(r)
}

func (f *fixture) armCreativeBg(t *testing.T, taskURL, uploadURL string, mutate func(*config.Config)) {
	t.Helper()
	f.srv.Cfg.BgReplaceEnabled = true
	f.srv.Cfg.BgCreativeEnabled = true
	f.srv.Cfg.GenerationEnabled = true
	f.srv.Cfg.TaskBaseURL = taskURL
	f.srv.Cfg.TaskToken = "task-tok"
	f.srv.Cfg.UploadBaseURL = uploadURL
	f.srv.Cfg.UploadToken = "up-tok"
	if mutate != nil {
		mutate(&f.srv.Cfg)
	}
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskURL, AppID: "product-image", Token: "task-tok"}
	f.srv.Uploads = &platform.UploadClient{BaseURL: uploadURL, AppID: "product-image", Token: "up-tok", HTTP: &http.Client{Transport: denyModelSecret{
		t: t, secret: f.srv.Cfg.BgModelCredential, base: http.DefaultTransport,
	}}}
	f.rearm(t)
}

func (f *fixture) openAndConfirmCreative(t *testing.T, tok, proj, in, intent string) string {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements", tok, map[string]any{
		"input_id": in, "mode": "creative", "background_intent": intent, "explicit_creative": true,
	})
	if st != http.StatusCreated {
		t.Fatalf("开创意报价 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d %v", st, conf)
	}
	return jobID
}

func TestBackgroundReplaceCreativeModelStoresDecodedBytesWithoutBilling(t *testing.T) {
	const secret = "secret-model-key"
	original := tinyPNG(t, 4, 3)
	out := tinyPNG(t, 6, 5)
	var tasks atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"task_should_not","status":"pending"}`))
	}))
	t.Cleanup(taskSrv.Close)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: out}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, taskSrv.URL, up.srv.URL, func(c *config.Config) {
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "浅灰棚拍")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "completed" || job["origin"] != "model_http" || job["origin"] == "supplier" {
		t.Fatalf("创意背景应存下解码字节: %d %#v", st, job)
	}
	assertBgBillingAndProductionStayFalse(t, job)
	if tasks.Load() != 0 || rt.n.Load() != 1 || rt.auth != "Bearer "+secret || rt.host != "images.example" || !strings.HasSuffix(rt.path, "/images/generations") {
		t.Fatalf("应只向生成端点提交一次: tasks=%d n=%d auth=%q host=%s path=%s", tasks.Load(), rt.n.Load(), rt.auth, rt.host, rt.path)
	}
	if !strings.Contains(rt.body, "qwen-image-2.0-pro") || !strings.Contains(rt.body, "浅灰棚拍") || !strings.Contains(rt.body, "data:image/png;base64,") {
		t.Fatalf("请求应带模型名、意图和原图 data URL: %s", rt.body)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rt.body), &body); err != nil {
		t.Fatal(err)
	}
	images, _ := body["image"].([]any)
	if len(images) != 1 {
		t.Fatalf("image 应为数组: %#v", body["image"])
	}
	pngURL, _ := images[0].(string)
	gotRef, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pngURL, "data:image/png;base64,"))
	if err != nil || !bytes.Equal(gotRef, original) {
		t.Fatal("参考图应是已保存的原图")
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains([]byte(rt.body), []byte(secret)) {
		t.Fatal("凭证不能出现在应答或生成 body")
	}
	st, hdr, content := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/content", tok, nil)
	if st != http.StatusOK || !bytes.Equal(content, out) || hdr.Get("X-Bg-Origin") != "model_http" || hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("应读回模型字节: %d origin=%s ct=%s n=%d", st, hdr.Get("X-Bg-Origin"), hdr.Get("Content-Type"), len(content))
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	againJob, _ := again["job"].(map[string]any)
	if st != http.StatusOK || againJob["idempotent"] != true || againJob["output_asset_id"] != job["output_asset_id"] || rt.n.Load() != 1 {
		t.Fatalf("已完成记录不能第二次调用模型: n=%d %#v", rt.n.Load(), againJob)
	}
	assertBgBillingAndProductionStayFalse(t, againJob)
}

func TestBackgroundReplaceModelReadyDoesNotNeedPlatformGeneration(t *testing.T) {
	const secret = "secret-model-key"
	original := tinyPNG(t, 4, 3)
	out := tinyPNG(t, 5, 4)
	var tasks atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks.Add(1)
		http.Error(w, "platform task must not run", http.StatusBadGateway)
	}))
	t.Cleanup(taskSrv.Close)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: out}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, taskSrv.URL, up.srv.URL, func(c *config.Config) {
		c.GenerationEnabled = false
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "不靠平台任务")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "completed" || job["origin"] != "model_http" || tasks.Load() != 0 || rt.n.Load() != 1 {
		t.Fatalf("配齐背景模型后不应再等平台生成开关: %d tasks=%d n=%d %#v", st, tasks.Load(), rt.n.Load(), job)
	}
	assertBgBillingAndProductionStayFalse(t, job)
}

func TestBackgroundReplaceProjectCanvasSizeIsRequestedAndMismatchStaysUndeliverable(t *testing.T) {
	const secret = "secret-model-key"
	original := tinyPNG(t, 4, 3)
	out := tinyPNG(t, 5, 4)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: out}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, "http://127.0.0.1:1", up.srv.URL, func(c *config.Config) {
		c.GenerationEnabled = false
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{
		"name": "电商背景", "source_type": "standalone", "width_px": 800, "height_px": 800,
	})
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
	inputID := in["input"].(map[string]any)["id"].(string)
	jobID := f.openAndConfirmCreative(t, tok, proj, inputID, "电商主图背景")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	pending, _ := json.Marshal(job["pending"])
	if st != http.StatusOK || job["origin"] != "model_http" || !strings.Contains(rt.body, `"size":"800x800"`) {
		t.Fatalf("应按项目画布请求尺寸: %d body=%s %#v", st, rt.body, job)
	}
	if !bytes.Contains(pending, []byte("输出尺寸与项目不一致")) || job["deliverable"] != false || job["real_generation_completed"] != false {
		t.Fatalf("尺寸不一致不能当成电商尺寸通过: pending=%s %#v", pending, job)
	}
	assertBgBillingAndProductionStayFalse(t, job)
}

func TestBackgroundReplaceOutOfDomainCanvasKeepsDefaultSize(t *testing.T) {
	original := tinyPNG(t, 3, 3)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: tinyPNG(t, 2, 2)}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, "http://127.0.0.1:1", up.srv.URL, func(c *config.Config) {
		c.GenerationEnabled = false
		c.BgModelCredential = "secret-model-key"
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{
		"name": "过小画布", "source_type": "standalone", "width_px": 100, "height_px": 100,
	})
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
	jobID := f.openAndConfirmCreative(t, tok, proj, in["input"].(map[string]any)["id"].(string), "域外尺寸")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	pending, _ := json.Marshal(job["pending"])
	if st != http.StatusOK || strings.Contains(rt.body, "100x100") || !strings.Contains(rt.body, `"size":"1024x1024"`) {
		t.Fatalf("域外尺寸不能送出: %d body=%s", st, rt.body)
	}
	if !bytes.Contains(pending, []byte("项目尺寸不在模型像素域")) || job["deliverable"] != false {
		t.Fatalf("应记下未按项目尺寸请求: pending=%s %#v", pending, job)
	}
	assertBgBillingAndProductionStayFalse(t, job)
}

func TestModelBackgroundBytesStayUnusableCandidates(t *testing.T) {
	const secret = "secret-model-key"
	original := tinyPNG(t, 4, 3)
	out := tinyPNG(t, 6, 5)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: out}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, "http://127.0.0.1:1", up.srv.URL, func(c *config.Config) {
		c.GenerationEnabled = false
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "未保真背景")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["origin"] != "model_http" || job["deliverable"] != false {
		t.Fatalf("模型字节应留下但不可交付: %d %#v", st, job)
	}
	st, selected := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/select", tok, nil)
	assertNotUsableCandidate(t, st, selected)
	st, exported := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/export", tok, nil)
	assertNotUsableCandidate(t, st, exported)
	st, listed := f.do(t, "GET", "/api/v1/projects/"+proj+"/background-replacements", tok, nil)
	kept := bgJobByID(t, listed, jobID)
	if st != http.StatusOK || kept["selection"] != "" || kept["export_count"] != float64(0) {
		t.Fatalf("拒绝后不能留下选定或导出: %d %#v", st, kept)
	}
	st, hdr, body := f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/content", tok, nil)
	if st != http.StatusOK || !bytes.Equal(body, out) || hdr.Get("X-Bg-Origin") != "model_http" {
		t.Fatalf("查看字节仍然允许: %d origin=%s n=%d", st, hdr.Get("X-Bg-Origin"), len(body))
	}
	eco := newEcoStub(t)
	f.srv.Uploads = &platform.UploadClient{BaseURL: eco.srv.URL, AppID: "product-image", Token: "up-tok"}
	st, registered := f.do(t, "POST", "/api/v1/projects/"+proj+"/outputs", tok, map[string]any{
		"file_name": "model.png", "content_type": "image/png", "data_b64": b64(out),
	})
	assertNotUsableCandidate(t, st, registered)
	if eco.uploads.Load() != 0 {
		t.Fatalf("不保真字节不能登记到素材服务: uploads=%d", eco.uploads.Load())
	}
	st, other := f.do(t, "POST", "/api/v1/projects/"+proj+"/outputs", tok, map[string]any{
		"file_name": "other.png", "content_type": "image/png", "data_b64": b64(tinyPNG(t, 2, 2)),
	})
	if st != http.StatusCreated || eco.uploads.Load() != 1 {
		t.Fatalf("其他图片仍可登记: %d uploads=%d %#v", st, eco.uploads.Load(), other)
	}
	st, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if st != http.StatusOK || rt.n.Load() != 1 {
		t.Fatalf("挡住候选不能再次出图: n=%d %d", rt.n.Load(), st)
	}
	assertBgBillingAndProductionStayFalse(t, kept)
}

func bgJobByID(t *testing.T, body map[string]any, id string) map[string]any {
	t.Helper()
	jobs, _ := body["jobs"].([]any)
	for _, item := range jobs {
		job, _ := item.(map[string]any)
		if job["id"] == id {
			return job
		}
	}
	t.Fatalf("列表里没有任务 %s: %#v", id, body)
	return nil
}

func assertNotUsableCandidate(t *testing.T, st int, body map[string]any) {
	t.Helper()
	code, ok := errCodeOf(body)
	msg := ""
	if e, isMap := body["error"].(map[string]any); isMap {
		msg, _ = e["message"].(string)
	}
	if st != http.StatusConflict || !ok || code != "not_usable_candidate" || msg != "不保真结果不能作为可用候选" {
		t.Fatalf("不保真结果应被挡住: %d %#v", st, body)
	}
}

func TestBackgroundReplaceLoopbackModelBytesStayHidden(t *testing.T) {
	const secret = "secret-model-key"
	original := tinyPNG(t, 3, 3)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: tinyPNG(t, 2, 2)}
	var tasks atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks.Add(1)
		http.Error(w, "no", http.StatusBadGateway)
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, taskSrv.URL, up.srv.URL, func(c *config.Config) {
		c.BgModelCredential = secret
		c.BgModelURL = "http://127.0.0.1:9/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "回环不能算供应商")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	pending, _ := json.Marshal(job["pending"])
	if st != http.StatusOK || job["job_status"] != "unknown" || job["output_asset_id"] != "" || tasks.Load() != 0 || rt.n.Load() != 1 {
		t.Fatalf("回环字节不能当供应商出图: %d tasks=%d n=%d %#v", st, tasks.Load(), rt.n.Load(), job)
	}
	if !bytes.Contains(pending, []byte("回环地址不能算供应商出图")) || bytes.Contains(raw, []byte(secret)) {
		t.Fatalf("应记下回环原因且不泄露凭证: %s", pending)
	}
	assertBgBillingAndProductionStayFalse(t, job)
	st, _, _ = f.doRaw(t, "GET", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/content", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("回环结果不能打开: %d", st)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if st != http.StatusOK || rt.n.Load() != 1 {
		t.Fatalf("未知记录不能第二次调用模型: %d n=%d %#v", st, rt.n.Load(), again)
	}
}

func TestBackgroundReplaceModelFailureDoesNotRetryOrLeakCredential(t *testing.T) {
	const secret = "secret-model-key"
	original := tinyPNG(t, 3, 2)
	up := newPhotoUpStub(t)
	up.Preload("asset_in", original, "image/png")
	rt := &bgModelRT{png: tinyPNG(t, 2, 2), status: http.StatusBadGateway}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, "http://127.0.0.1:9", up.srv.URL, func(c *config.Config) {
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "失败不能重试")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["output_asset_id"] != "" || rt.n.Load() != 1 {
		t.Fatalf("模型拒绝应失败且只调用一次: %d n=%d %#v", st, rt.n.Load(), job)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("错误文案不能包含凭证")
	}
	assertBgBillingAndProductionStayFalse(t, job)
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	if st != http.StatusOK || rt.n.Load() != 1 {
		t.Fatalf("失败记录不能第二次调用模型: %d n=%d %#v", st, rt.n.Load(), again)
	}
}

func TestBackgroundReplaceWithoutModelNameKeepsPlatformTask(t *testing.T) {
	const secret = "secret-model-key"
	var tasks atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"task_bg_name","status":"pending"}`))
	}))
	t.Cleanup(taskSrv.Close)
	rt := &bgModelRT{png: tinyPNG(t, 2, 2)}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, taskSrv.URL, "http://127.0.0.1:1", func(c *config.Config) {
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = ""
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "空模型名走原路径")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "queued" || job["platform_task_id"] != "task_bg_name" || tasks.Load() != 1 || rt.n.Load() != 0 {
		t.Fatalf("空模型名应保持平台任务路径: %d tasks=%d n=%d %#v", st, tasks.Load(), rt.n.Load(), job)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("凭证不能出现在应答里")
	}
	assertBgBillingAndProductionStayFalse(t, job)
}

func TestBackgroundReplaceFidelityIgnoresConfiguredModel(t *testing.T) {
	const secret = "secret-model-key"
	var tasks atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"task_bg_fid_model","status":"pending"}`))
	}))
	t.Cleanup(taskSrv.Close)
	rt := &bgModelRT{png: tinyPNG(t, 2, 2)}
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, taskSrv.URL, "http://127.0.0.1:1", func(c *config.Config) {
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirm(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["mode"] != "fidelity" || job["job_status"] != "queued" || job["platform_task_id"] != "task_bg_fid_model" || tasks.Load() != 1 || rt.n.Load() != 0 {
		t.Fatalf("保真模式不能改走背景模型: %d tasks=%d n=%d %#v", st, tasks.Load(), rt.n.Load(), job)
	}
	assertBgBillingAndProductionStayFalse(t, job)
}

func TestBackgroundReplaceMissingOriginalDoesNotCallModel(t *testing.T) {
	const secret = "secret-model-key"
	rt := &bgModelRT{png: tinyPNG(t, 2, 2)}
	var tasks atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"task_no","status":"pending"}`))
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) { c.BgReplaceEnabled = true })
	f.armCreativeBg(t, taskSrv.URL, "http://127.0.0.1:1", func(c *config.Config) {
		c.BgModelCredential = secret
		c.BgModelURL = "https://images.example/v1"
		c.BgModelName = "qwen-image-2.0-pro"
	})
	f.srv.ImageHTTP = &http.Client{Transport: rt}
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmCreative(t, tok, proj, in, "没有原图")
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/background-replacements/"+jobID+"/submit", tok, nil)
	raw, _ := json.Marshal(submitted)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || rt.n.Load() != 0 || tasks.Load() != 0 {
		t.Fatalf("没有原图不能调用模型或平台任务: %d tasks=%d n=%d %#v", st, tasks.Load(), rt.n.Load(), job)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("原图错误不能带上凭证")
	}
	assertBgBillingAndProductionStayFalse(t, job)
}
