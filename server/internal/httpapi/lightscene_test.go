package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
)

func TestLightSceneHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, nil)
	_, tok := f.loginOK(t)
	st, _ := f.do(t, "GET", "/api/v1/light-scenes/capabilities", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("开关关闭应 404, got %d", st)
	}
}

func TestLightSceneQuoteBillingPendingAndNoFakePass(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.LightSceneEnabled = true })
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)

	st, capBody := f.do(t, "GET", "/api/v1/light-scenes/capabilities", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("能力接口 %d %v", st, capBody)
	}
	if capBody["billing_label"] != "计费待确认" || capBody["production_generation_passed"] != false || capBody["billing_passed"] != false {
		t.Fatalf("能力接口不得宣称出图或计费已通过: %#v", capBody)
	}
	if capBody["creative_available"] != false || capBody["default_mode"] != "fidelity" {
		t.Fatalf("默认不得开放创意模式: %#v", capBody)
	}

	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "lighting_intent": "侧光棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["billing_label"] != "计费待确认" || job["mode"] != "fidelity" || job["quality"] != "unknown" {
		t.Fatalf("报价不诚实: %#v", job)
	}
	if job["verified_product"] != false || job["production_generation_passed"] != false || job["billing_passed"] != false {
		t.Fatalf("报价宣称已验证: %#v", job)
	}

	st, denied := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "mode": "creative", "lighting_intent": "戏剧光",
	})
	if st != http.StatusUnprocessableEntity {
		t.Fatalf("创意模式应拒绝, got %d %v", st, denied)
	}
}

func TestLightSceneSubmitWithoutVendorStaysFailed(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.LightSceneEnabled = true })
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)

	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["verified_product"] != false {
		t.Fatalf("没有供应商时应失败而非假成功: %d %#v", st, job)
	}
	st, q := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/quality", tok, map[string]any{"verdict": "pass"})
	if st != http.StatusConflict {
		t.Fatalf("无证据不得标 pass, got %d %v", st, q)
	}
	st, failed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/quality", tok, map[string]any{"verdict": "fail"})
	job, _ = failed["job"].(map[string]any)
	if st != http.StatusOK || job["quality"] != "fail" || job["mode"] != "fidelity" || job["verified_product"] != false {
		t.Fatalf("失败应留在保真模式: %d %#v", st, job)
	}
	st, unknown := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/quality", tok, map[string]any{"verdict": "unknown"})
	if st != http.StatusConflict {
		t.Fatalf("失败不能改回未知, got %d %v", st, unknown)
	}
	st, sel := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/select", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("失败不能选定已验证产品图, got %d %v", st, sel)
	}
}

func TestLightSceneUnknownDoesNotRegenerate(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightModelCredential = "configured"
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	st, first := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := first["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" {
		t.Fatalf("任务不可达应为待核对, got %d %#v", st, job)
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	if calls.Load() != 1 {
		t.Fatalf("待核对后不得再次提交任务, calls=%d", calls.Load())
	}
}

func TestLightSceneRefreshIgnoresFilledOutputID(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"task_id":"task_light_1","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task_light_1","status":"succeeded","result":{"asset_id":"filled-output"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightModelCredential = "configured"
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["platform_task_id"] != "task_light_1" || job["verified_product"] != false {
		t.Fatalf("受理任务不能写成已验证产品图: %d %#v", st, job)
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "unknown" || job["output_asset_id"] != "" || job["verified_product"] != false {
		t.Fatalf("填好的输出编号不能当回执: %d %#v", st, job)
	}
	if calls.Load() != 2 {
		t.Fatalf("提交与核对各一次, got %d", calls.Load())
	}
	st, exported := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/export", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("未验证结果不能导出, got %d %v", st, exported)
	}
}

func TestLightSceneFailedRefreshDoesNotRelabel(t *testing.T) {
	var gets atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"task_id":"task_light_fail","status":"pending"}`))
			return
		}
		if gets.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"task_id":"task_light_fail","status":"failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task_light_fail","status":"succeeded","result":{"asset_id":"filled-output","vendor_receipt_id":"vr_filled"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightModelCredential = "configured"
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "queued" {
		t.Fatalf("应先排队, got %d %#v", st, job)
	}
	st, failed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = failed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["verified_product"] != false {
		t.Fatalf("供应商失败应保持失败: %d %#v", st, job)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = again["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["output_asset_id"] != "" || job["verified_product"] != false || job["quality"] == "pass" {
		t.Fatalf("失败后不能借输出编号或回执改标: %d %#v", st, job)
	}
	if gets.Load() != 1 {
		t.Fatalf("失败记录不应再次向供应商核对, gets=%d", gets.Load())
	}
}

func TestLightSceneCreativeAfterFailureIgnoresModeFlag(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.LightSceneEnabled = true })
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" {
		t.Fatalf("没有供应商时应失败, got %d %#v", st, job)
	}
	st, blocked := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "mode": "creative", "lighting_intent": "戏剧光", "explicit_creative": true,
	})
	if st != http.StatusConflict {
		t.Fatalf("选中创意不能当成失败后的明确确认, got %d %v", st, blocked)
	}
	st, confirmed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "mode": "creative", "lighting_intent": "戏剧光",
		"confirm_creative_after_failure": true,
	})
	if st != http.StatusUnprocessableEntity {
		t.Fatalf("单独确认也不能在创意路径未接通时出图, got %d %v", st, confirmed)
	}
}

func TestLightSceneSubmitRejectsInvalidQuote(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.LightSceneEnabled = true })
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "lighting_intent": "侧光",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d", st)
	}
	st, next := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "lighting_intent": "逆光",
	})
	if st != http.StatusCreated {
		t.Fatalf("新报价 %d %v", st, next)
	}
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	msg := ""
	if errBody, ok := submitted["error"].(map[string]any); ok {
		msg, _ = errBody["message"].(string)
	}
	if st != http.StatusConflict || msg != "报价已失效，请重新确认" {
		t.Fatalf("失效报价应明确失效, got %d %#v", st, submitted)
	}
}

func TestLightSceneConfirmRejectsStaleIntent(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.LightSceneEnabled = true })
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "lighting_intent": "侧光",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/confirm", tok, map[string]any{
		"mode": "fidelity", "input_id": in, "lighting_intent": "逆光",
	})
	if st != http.StatusConflict {
		t.Fatalf("光影意图变化应拒绝确认, got %d %v", st, conf)
	}
}

func TestLightSceneFidelityFailureBlocksSelection(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.SubjectFidelityEnabled = true
	})
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	st, report := f.do(t, "POST", "/api/v1/projects/"+proj+"/fidelity-reports", tok, map[string]any{
		"mode": "fidelity", "input_ref": in, "input_version": "v1",
		"mask_ref": "mask", "sample_class": "logo_text",
		"protected": []string{"product_count", "logo_text", "shape", "key_texture", "declared_color"},
		"checks": []map[string]string{
			{"name": "product_count", "result": "pass"},
			{"name": "logo_text", "result": "fail"},
			{"name": "shape", "result": "pass"},
			{"name": "key_texture", "result": "pass"},
			{"name": "declared_color", "result": "pass"},
		},
	})
	if st != http.StatusCreated && st != http.StatusOK {
		t.Fatalf("主体保护失败报告 %d %v", st, report)
	}
	st, sel := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/select", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("主体保护失败不能选定, got %d %v", st, sel)
	}
}

func TestLightSceneNoCredentialStaysClosed(t *testing.T) {
	var taskCalls atomic.Int32
	var billCalls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		taskCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"task_should_not_run","status":"pending"}`))
	}))
	t.Cleanup(taskSrv.Close)
	billSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		billCalls.Add(1)
		http.Error(w, "no billing client", http.StatusBadGateway)
	}))
	t.Cleanup(billSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightCreativeEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.TaskToken = "task-tok"
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
		c.BillingEnabled = false
		c.BillingBaseURL = billSrv.URL
		c.BillingToken = "bill-tok"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.srv.Billing = &platform.BillingClient{BaseURL: billSrv.URL, AppID: "product-image", Token: "bill-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)

	st, capBody := f.do(t, "GET", "/api/v1/light-scenes/capabilities", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("能力 %d %v", st, capBody)
	}
	if capBody["creative_available"] != false {
		t.Fatalf("没有凭证时创意必须关闭: %#v", capBody)
	}
	assertLightClosed(t, capBody)

	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "lighting_intent": "侧光棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["quote_status"] != "unconfirmed" || job["mode"] != "fidelity" {
		t.Fatalf("选择光影后应为未确认保真报价: %#v", job)
	}
	if job["fingerprint"] == "" || job["input_version"] == "" {
		t.Fatalf("版本和任务指纹必须可追溯: %#v", job)
	}
	if job["model_ref"] != "" || job["model_fingerprint"] != "" {
		t.Fatalf("没有模型调用时不得编造模型指纹: %#v", job)
	}
	assertLightClosed(t, job)
	jobID, _ := job["id"].(string)
	fp, _ := job["fingerprint"].(string)
	version, _ := job["input_version"].(string)

	st, early := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	if st != http.StatusConflict || taskCalls.Load() != 0 || billCalls.Load() != 0 {
		t.Fatalf("确认前不得出图或扣费: %d calls=%d bills=%d %#v", st, taskCalls.Load(), billCalls.Load(), early)
	}

	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/confirm", tok, nil)
	job, _ = conf["job"].(map[string]any)
	if st != http.StatusOK || job["quote_status"] != "confirmed" || taskCalls.Load() != 0 {
		t.Fatalf("确认只改报价状态: %d calls=%d %#v", st, taskCalls.Load(), job)
	}
	assertLightClosed(t, job)

	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ = submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || taskCalls.Load() != 0 || billCalls.Load() != 0 {
		t.Fatalf("没有凭证时应失败关闭且不调用供应商: %d calls=%d bills=%d %#v", st, taskCalls.Load(), billCalls.Load(), job)
	}
	assertLightClosed(t, job)
	notice := lightPendingText(job)
	if job["real_generation_notice"] != "真实出图未完成" || !strings.Contains(notice, "真实出图未完成") {
		t.Fatalf("文案必须写明未完成: %#v", job)
	}
	if job["fingerprint"] != fp || job["input_version"] != version || job["mode"] != "fidelity" {
		t.Fatalf("失败不得改版本、模式或任务指纹: %#v", job)
	}
	if job["output_asset_id"] != "" {
		t.Fatalf("失败不得写入输出: %#v", job)
	}

	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || taskCalls.Load() != 0 || billCalls.Load() != 0 || job["job_status"] != "failed" {
		t.Fatalf("刷新和重试不得重复调用或扣费: %d calls=%d bills=%d %#v", st, taskCalls.Load(), billCalls.Load(), job)
	}
	if job["fingerprint"] != fp || job["input_version"] != version || job["quote_status"] != "confirmed" {
		t.Fatalf("刷新不得改指纹、版本或已确认报价: %#v", job)
	}
	assertLightClosed(t, job)
}

func TestLightSceneLateFailureKeepsSelectedVersion(t *testing.T) {
	const credential = "configured-not-a-secret"
	var posts, polls, bills atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"task_light_keep","status":"pending"}`))
			return
		}
		polls.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"task_light_keep","status":"failed","result":{"asset_id":"asset_late","model_ref":"model_side","output_version":"ver_late"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	billSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bills.Add(1)
		http.Error(w, "no billing client", http.StatusBadGateway)
	}))
	t.Cleanup(billSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightModelCredential = credential
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.TaskToken = "task-tok"
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
		c.BillingEnabled = false
		c.BillingBaseURL = billSrv.URL
		c.BillingToken = "bill-tok"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.srv.Billing = &platform.BillingClient{BaseURL: billSrv.URL, AppID: "product-image", Token: "bill-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)

	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "queued" || job["platform_task_id"] != "task_light_keep" || posts.Load() != 1 {
		t.Fatalf("有凭证时才受理任务: %d posts=%d %#v", st, posts.Load(), job)
	}
	assertLightClosed(t, job)
	if strings.Contains(fmt.Sprintf("%#v", submitted), credential) {
		t.Fatal("凭证不得回传")
	}

	p := f.principal(t, tok)
	saved, err := f.st.GetLightJob(t.Context(), p, proj, jobID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Selection = "selected"
	saved.OutputVersion = "ver_user"
	saved.OutputAssetID = "asset_user"
	if err := f.st.UpdateLightJob(t.Context(), saved); err != nil {
		t.Fatal(err)
	}

	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = refreshed["job"].(map[string]any)
	sum := sha256.Sum256([]byte("model_side"))
	wantFP := hex.EncodeToString(sum[:])
	if st != http.StatusOK || job["job_status"] != "failed" || job["selection"] != "selected" ||
		job["output_version"] != "ver_user" || job["output_asset_id"] != "asset_user" {
		t.Fatalf("失败不得覆盖后来选定的版本: %d %#v", st, job)
	}
	if job["model_ref"] != "model_side" || job["model_fingerprint"] != wantFP {
		t.Fatalf("模型引用和指纹必须可追溯: %#v", job)
	}
	assertLightClosed(t, job)
	if job["output_asset_id"] == "asset_late" || job["output_version"] == "ver_late" {
		t.Fatalf("晚到资产不能写成模型结果: %#v", job)
	}
	if polls.Load() != 1 || bills.Load() != 0 || posts.Load() != 1 {
		t.Fatalf("核对不得扣费或重复提交: posts=%d polls=%d bills=%d", posts.Load(), polls.Load(), bills.Load())
	}
	if strings.Contains(fmt.Sprintf("%#v", refreshed), credential) {
		t.Fatal("凭证不得回传")
	}

	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	_, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ = again["job"].(map[string]any)
	if polls.Load() != 1 || posts.Load() != 1 || bills.Load() != 0 ||
		job["output_version"] != "ver_user" || job["selection"] != "selected" || job["model_ref"] != "model_side" {
		t.Fatalf("刷新和重试不得重复扣费或改选定版本: posts=%d polls=%d bills=%d %#v", posts.Load(), polls.Load(), bills.Load(), job)
	}
}

func TestLightSceneUnknownKeepsSelectedVersion(t *testing.T) {
	const credential = "configured-not-a-secret"
	var posts, polls, bills atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"task_light_unknown","status":"pending"}`))
			return
		}
		polls.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"task_light_unknown","status":"succeeded","result":{"asset_id":"asset_unknown","model_ref":"model_unknown","output_version":"ver_unknown"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	billSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bills.Add(1)
		http.Error(w, "no billing client", http.StatusBadGateway)
	}))
	t.Cleanup(billSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightModelCredential = credential
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.TaskToken = "task-tok"
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
		c.BillingEnabled = false
		c.BillingBaseURL = billSrv.URL
		c.BillingToken = "bill-tok"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.srv.Billing = &platform.BillingClient{BaseURL: billSrv.URL, AppID: "product-image", Token: "bill-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "queued" || posts.Load() != 1 {
		t.Fatalf("有凭证时才受理任务: %d posts=%d %#v", st, posts.Load(), job)
	}
	p := f.principal(t, tok)
	saved, err := f.st.GetLightJob(t.Context(), p, proj, jobID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Selection = "selected"
	saved.OutputVersion = "ver_user"
	saved.OutputAssetID = "asset_user"
	if err := f.st.UpdateLightJob(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = refreshed["job"].(map[string]any)
	sum := sha256.Sum256([]byte("model_unknown"))
	wantFP := hex.EncodeToString(sum[:])
	if st != http.StatusOK || job["job_status"] != "unknown" || job["selection"] != "selected" ||
		job["output_version"] != "ver_user" || job["output_asset_id"] != "asset_user" {
		t.Fatalf("unknown 不得覆盖后来选定的版本: %d %#v", st, job)
	}
	if job["model_ref"] != "model_unknown" || job["model_fingerprint"] != wantFP {
		t.Fatalf("unknown 仍须留下模型指纹: %#v", job)
	}
	if job["output_version"] == "ver_unknown" || job["output_asset_id"] == "asset_unknown" {
		t.Fatalf("平台资产不能写成选定版本: %#v", job)
	}
	assertLightClosed(t, job)
	if bills.Load() != 0 || posts.Load() != 1 || polls.Load() != 1 {
		t.Fatalf("unknown 核对不得扣费: posts=%d polls=%d bills=%d", posts.Load(), polls.Load(), bills.Load())
	}
	if strings.Contains(fmt.Sprintf("%#v", refreshed), credential) {
		t.Fatal("凭证不得回传")
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	_, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ = again["job"].(map[string]any)
	if bills.Load() != 0 || posts.Load() != 1 || job["output_version"] != "ver_user" || job["job_status"] != "unknown" {
		t.Fatalf("刷新和重试不得重复扣费或改版本: posts=%d polls=%d bills=%d %#v", posts.Load(), polls.Load(), bills.Load(), job)
	}
}

func TestLightSceneRefreshWithoutCredentialDoesNotCallSupplier(t *testing.T) {
	const credential = "configured-not-a-secret"
	var posts, polls, bills atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"task_light_drop","status":"pending"}`))
			return
		}
		polls.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"task_light_drop","status":"failed","result":{"asset_id":"asset_late","model_ref":"model_late","output_version":"ver_late","image":"solid-placeholder"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	billSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bills.Add(1)
		http.Error(w, "no billing client", http.StatusBadGateway)
	}))
	t.Cleanup(billSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.LightSceneEnabled = true
		c.LightModelCredential = credential
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.TaskToken = "task-tok"
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
		c.BillingEnabled = false
		c.BillingBaseURL = billSrv.URL
		c.BillingToken = "bill-tok"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.srv.Billing = &platform.BillingClient{BaseURL: billSrv.URL, AppID: "product-image", Token: "bill-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, in := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmLight(t, tok, proj, in)
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "queued" || posts.Load() != 1 || polls.Load() != 0 {
		t.Fatalf("有凭证时才提交一次: %d posts=%d polls=%d %#v", st, posts.Load(), polls.Load(), job)
	}
	p := f.principal(t, tok)
	saved, err := f.st.GetLightJob(t.Context(), p, proj, jobID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Selection = "selected"
	saved.OutputVersion = "ver_user"
	saved.OutputAssetID = "asset_user"
	if err := f.st.UpdateLightJob(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	f.srv.Cfg.LightModelCredential = ""
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/refresh", tok, nil)
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || polls.Load() != 0 || bills.Load() != 0 || posts.Load() != 1 {
		t.Fatalf("撤掉凭证后刷新不得调用供应商或扣费: %d posts=%d polls=%d bills=%d %#v", st, posts.Load(), polls.Load(), bills.Load(), job)
	}
	if job["job_status"] != "queued" || job["selection"] != "selected" || job["output_version"] != "ver_user" || job["output_asset_id"] != "asset_user" {
		t.Fatalf("无凭证核对不得覆盖已选定版本: %#v", job)
	}
	if job["output_asset_id"] == "asset_late" || job["output_version"] == "ver_late" || job["verified_product"] == true {
		t.Fatalf("纯色占位不得写成模型结果: %#v", job)
	}
	assertLightClosed(t, job)
	if strings.Contains(fmt.Sprintf("%#v", refreshed), credential) {
		t.Fatal("凭证不得回传")
	}
	_, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/submit", tok, nil)
	job, _ = again["job"].(map[string]any)
	if posts.Load() != 1 || polls.Load() != 0 || bills.Load() != 0 || job["output_version"] != "ver_user" || job["selection"] != "selected" {
		t.Fatalf("无凭证再次提交不得重试扣费或改版本: posts=%d polls=%d bills=%d %#v", posts.Load(), polls.Load(), bills.Load(), job)
	}
}

func (f *fixture) principal(t *testing.T, tok string) string {
	t.Helper()
	st, body := f.do(t, "GET", "/api/v1/auth/session", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("session %d %v", st, body)
	}
	p, _ := body["principal"].(map[string]any)
	tenant, _ := p["tenant_id"].(string)
	if tenant == "" {
		tenant, _ = p["account_id"].(string)
	}
	if tenant == "" {
		t.Fatalf("会话没有租户: %#v", body)
	}
	return tenant
}

func assertLightClosed(t *testing.T, body map[string]any) {
	t.Helper()
	charged, ok := body["charged"]
	if !ok || charged != nil {
		t.Fatalf("charged 必须是 JSON null, ok=%v v=%#v", ok, charged)
	}
	if body["billing_passed"] != false || body["production_authorized"] != false {
		t.Fatalf("计费和生产授权必须保持未通过: %#v", body)
	}
	if body["production_authorization"] != "NOT_AUTHORIZED" {
		t.Fatalf("生产授权来源必须是 NOT_AUTHORIZED: %#v", body["production_authorization"])
	}
	if body["real_generation_completed"] != false || body["real_generation_notice"] != "真实出图未完成" {
		t.Fatalf("真实出图必须标明未完成: %#v", body)
	}
}

func lightPendingText(job map[string]any) string {
	items, _ := job["pending"].([]any)
	var b strings.Builder
	for _, item := range items {
		if s, ok := item.(string); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}

func (f *fixture) openAndConfirmLight(t *testing.T, tok, proj, in string) string {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes", tok, map[string]any{
		"input_id": in, "lighting_intent": "侧光棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("开报价 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/light-scenes/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d %v", st, conf)
	}
	return jobID
}
