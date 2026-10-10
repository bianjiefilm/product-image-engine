package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func TestShowcaseVideoHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.ShowcaseVideoEnabled = false })
	_, tok := f.loginOK(t)
	st, _ := f.do(t, "GET", "/api/v1/showcase-videos/capabilities", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("开关关闭应 404, got %d", st)
	}
}

func TestShowcaseVideoRequiresExistingImageAndKnownMove(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.ShowcaseVideoEnabled = true })
	_, tok := f.loginOK(t)
	proj, imageID := f.projectWithInput(t, tok)

	st, capBody := f.do(t, "GET", "/api/v1/showcase-videos/capabilities", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("能力接口 %d %v", st, capBody)
	}
	if capBody["billing_label"] != "计费待确认" || capBody["image_required"] != true ||
		capBody["billing_passed"] != false || capBody["production_authorized"] != false ||
		capBody["show_video"] != false || capBody["play_still"] != false {
		t.Fatalf("能力接口不得宣称已出视频或已扣费: %#v", capBody)
	}
	moves, _ := capBody["camera_moves"].([]any)
	if len(moves) != 2 || moves[0] != "360" || moves[1] != "scene" {
		t.Fatalf("运镜只应公开 360 与场景: %#v", capBody["camera_moves"])
	}

	st, denied := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"camera_move": "360",
	})
	if st != http.StatusBadRequest {
		t.Fatalf("没有产品图应拒绝, got %d %v", st, denied)
	}
	st, missing := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": "missing-image", "camera_move": "scene",
	})
	if st != http.StatusNotFound {
		t.Fatalf("不存在的产品图应拒绝, got %d %v", st, missing)
	}
	st, badMove := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": imageID, "camera_move": "zoom",
	})
	if st != http.StatusBadRequest {
		t.Fatalf("未列出的运镜应拒绝, got %d %v", st, badMove)
	}

	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": imageID, "camera_move": "360", "quoted_amount": "¥9.90",
	})
	if st != http.StatusCreated {
		t.Fatalf("开展示视频 %d %v", st, created)
	}
	job, _ := created["job"].(map[string]any)
	if job["billing_label"] != "计费待确认" || job["show_video"] != false || job["play_still"] != false || job["video_url"] != "" {
		t.Fatalf("展示视频入口不诚实: %#v", job)
	}
	if job["charged"] != false || job["billing_passed"] != false || job["production_authorized"] != false {
		t.Fatalf("客户端不能自称已扣费或已授权: %#v", job)
	}
	if job["camera_move"] != "360" || job["product_image_id"] != imageID {
		t.Fatalf("应记下产品图与运镜: %#v", job)
	}
	id, _ := job["id"].(string)
	st, again := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": imageID, "camera_move": "360",
	})
	againJob, _ := again["job"].(map[string]any)
	if st != http.StatusOK || againJob["id"] != id || againJob["idempotent"] != true {
		t.Fatalf("同一产品图和运镜应回到原记录: %d %#v", st, againJob)
	}
}

func TestShowcaseVideoSubmitWithoutSupplierStaysFailed(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.ShowcaseVideoEnabled = true })
	_, tok := f.loginOK(t)
	proj, imageID := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmShowcase(t, tok, proj, imageID, "scene")

	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["show_video"] != false || job["play_still"] != false || job["video_url"] != "" {
		t.Fatalf("没有供应商时应失败且不出视频: %d %#v", st, job)
	}
	if job["billing_passed"] != false || job["production_authorized"] != false || job["charged"] != false || job["billing_label"] != "计费待确认" {
		t.Fatalf("失败不能标成计费或生产通过: %#v", job)
	}
	st, passed := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/claim", tok, map[string]any{
		"claim": "success", "output_asset_id": "filled-video",
	})
	if st != http.StatusConflict {
		t.Fatalf("失败不能改成成功, got %d %v", st, passed)
	}
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/refresh", tok, map[string]any{
		"output_asset_id": "filled-video", "job_status": "succeeded", "video_url": "https://example.invalid/fake.mp4",
	})
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["id"] != jobID || job["job_status"] != "failed" || job["show_video"] != false || job["play_still"] != false || job["output_is_receipt"] != false || job["video_url"] != "" {
		t.Fatalf("刷新后仍应是同一条失败记录: %d %#v", st, job)
	}
}

func TestShowcaseVideoUnknownRefreshDoesNotRegenerate(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.ShowcaseVideoEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, imageID := f.projectWithInput(t, tok)
	jobID := f.openAndConfirmShowcase(t, tok, proj, imageID, "360")
	st, first := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/submit", tok, map[string]any{
		"placeholder_url": "https://example.invalid/fake.mp4",
		"still":           true,
		"video_bytes":     "",
	})
	job, _ := first["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["show_video"] != false || job["play_still"] != false || job["video_url"] != "" {
		t.Fatalf("平台任务不是图生视频供应商，应失败且不播放: %d %#v", st, job)
	}
	if job["charged"] != false || job["billing_passed"] != false || job["production_authorized"] != false || job["platform_task_id"] != "" {
		t.Fatalf("失败不能扣费或留下任务: %#v", job)
	}
	if job["honesty"] != "没有真实展示视频，不能播放静图或假视频" {
		t.Fatalf("失败文案被弱化: %#v", job["honesty"])
	}
	_, _ = f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/submit", tok, nil)
	st, refreshed := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/refresh", tok, map[string]any{
		"video_url": "https://example.invalid/fake.mp4", "job_status": "succeeded",
	})
	job, _ = refreshed["job"].(map[string]any)
	if st != http.StatusOK || job["id"] != jobID || job["job_status"] != "failed" || job["show_video"] != false || job["play_still"] != false || job["video_url"] != "" {
		t.Fatalf("刷新后仍应是同一条失败记录: %d %#v", st, job)
	}
	if calls.Load() != 0 {
		t.Fatalf("没有图生视频供应商时不得调用平台任务, calls=%d", calls.Load())
	}
}

func TestShowcaseVideoSupplierAssetDoesNotVerifyFailure(t *testing.T) {
	var calls atomic.Int32
	taskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"task_id":"task_video_1","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task_video_1","status":"succeeded","result":{"asset_id":"filled-video","url":"https://example.invalid/fake.mp4"}}`))
	}))
	t.Cleanup(taskSrv.Close)
	f := newFixture(t, func(c *config.Config) {
		c.ShowcaseVideoEnabled = true
		c.GenerationEnabled = true
		c.TaskBaseURL = taskSrv.URL
		c.UploadBaseURL = "http://127.0.0.1:1"
		c.UploadToken = "up"
	})
	f.srv.Tasks = &platform.TaskClient{BaseURL: taskSrv.URL, AppID: "product-image", Token: "task-tok"}
	f.rearm(t)
	_, tok := f.loginOK(t)
	proj, imageID := f.projectWithInput(t, tok)
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": imageID, "camera_move": "scene",
	})
	if st != http.StatusCreated {
		t.Fatalf("开展示视频 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, early := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/submit", tok, nil)
	if st != http.StatusConflict {
		t.Fatalf("未确认报价不得提交, got %d %v", st, early)
	}
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d %v", st, conf)
	}
	st, submitted := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/submit", tok, nil)
	job, _ := submitted["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["platform_task_id"] != "" || job["show_video"] != false || job["production_authorized"] != false || job["play_still"] != false || job["video_url"] != "" || job["charged"] != false {
		t.Fatalf("平台任务返回的地址不能当成图生视频: %d %#v", st, job)
	}
	st, failed := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/refresh", tok, nil)
	job, _ = failed["job"].(map[string]any)
	if st != http.StatusOK || job["job_status"] != "failed" || job["show_video"] != false || job["video_url"] != "" || job["id"] != jobID {
		t.Fatalf("刷新不得把占位结果核销成视频: %d %#v", st, job)
	}
	if calls.Load() != 0 {
		t.Fatalf("不得向平台任务核对展示视频, calls=%d", calls.Load())
	}
}

func TestShowcaseVideoAcceptsRegisteredOutput(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.ShowcaseVideoEnabled = true })
	_, tok := f.loginOK(t)
	proj, _ := f.projectWithInput(t, tok)
	st, got := f.do(t, "GET", "/api/v1/projects/"+proj, tok, nil)
	if st != http.StatusOK {
		t.Fatalf("读工程 %d %v", st, got)
	}
	project, _ := got["project"].(map[string]any)
	tenant, _ := project["tenant_id"].(string)
	out, err := f.st.CreateOutput(t.Context(), store.ProjectOutput{
		TenantScope: tenant, ProjectID: proj, PlatformAssetID: "asset_out",
		ResultSHA256: "abc", ResultSize: 4, MediaType: "image/png", FileName: "cup.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": out.ID, "camera_move": "scene",
	})
	job, _ := created["job"].(map[string]any)
	if st != http.StatusCreated || job["product_image_id"] != out.ID || job["show_video"] != false || job["play_still"] != false {
		t.Fatalf("已登记产品图应能发起且不出视频: %d %#v", st, job)
	}
}

func (f *fixture) openAndConfirmShowcase(t *testing.T, tok, proj, imageID, move string) string {
	t.Helper()
	st, created := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos", tok, map[string]any{
		"product_image_id": imageID, "camera_move": move,
	})
	if st != http.StatusCreated {
		t.Fatalf("开展示视频 %d %v", st, created)
	}
	jobID := created["job"].(map[string]any)["id"].(string)
	st, conf := f.do(t, "POST", "/api/v1/projects/"+proj+"/showcase-videos/"+jobID+"/confirm", tok, nil)
	if st != http.StatusOK {
		t.Fatalf("确认 %d %v", st, conf)
	}
	return jobID
}
