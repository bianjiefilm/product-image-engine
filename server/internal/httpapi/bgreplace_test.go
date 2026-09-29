package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	if capBody["billing_label"] != "计费待确认" || capBody["production_generation_passed"] != false || capBody["billing_passed"] != false {
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
	if job["billing_label"] != "计费待确认" || job["mode"] != "fidelity" || job["quality"] != "unknown" {
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
