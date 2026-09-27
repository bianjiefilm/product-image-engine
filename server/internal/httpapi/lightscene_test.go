package httpapi

import (
	"net/http"
	"net/http/httptest"
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
