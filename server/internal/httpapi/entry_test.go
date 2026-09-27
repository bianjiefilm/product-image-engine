package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

func TestPersonalEntrySkipsCatalogAndOrder(t *testing.T) {
	f := newFixture(t, nil)
	tok := f.stub.mintExtra("jia@x.com", "product-image", jwt.MapClaims{
		"org_memberships": []string{"org_a", "org_b"},
	})
	st, body := f.do(t, "POST", "/api/v1/entry/start", tok, map[string]any{
		"name": "春季主图", "usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍", "photo_asset_id": "asset_mine",
	})
	if st != http.StatusCreated {
		t.Fatalf("独立入口应创建: %d %v", st, body)
	}
	screen, _ := body["screen"].(map[string]any)
	if screen["title"] != "开始做产品图" || screen["shows_catalog"] != false {
		t.Fatalf("首屏不是生态目录: %v", screen)
	}
	ws, _ := body["workspace"].(map[string]any)
	if ws["kind"] != "personal" || ws["scope"] != "personal/default" {
		t.Fatalf("应进入 personal/default: %v", ws)
	}
	proj, _ := body["project"].(map[string]any)
	if proj["source_type"] != "standalone" || proj["source_ref"] != "" {
		t.Fatalf("个人入口不能要求订单: %v", proj)
	}
	journey, _ := body["journey"].(map[string]any)
	if journey["sla_promised"] != false || journey["input_count"] != float64(1) {
		t.Fatalf("应记录输入且不承诺 SLA: %v", journey)
	}
	if body["useful_proven"] != false {
		t.Fatalf("不能宣称成片已证明: %v", body["useful_proven"])
	}
	againSt, again := f.do(t, "POST", "/api/v1/entry/start", tok, map[string]any{
		"name": "春季主图", "usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍", "photo_asset_id": "asset_mine",
	})
	againProj, _ := again["project"].(map[string]any)
	if againSt != http.StatusOK || againProj["id"] != proj["id"] {
		t.Fatalf("重进应是同一工程: %d %v", againSt, again)
	}
	relogin := f.stub.mintExtra("jia@x.com", "product-image", jwt.MapClaims{
		"org_memberships": []string{"org_a", "org_b"},
	})
	listSt, list := f.do(t, "GET", "/api/v1/entry/personal", relogin, nil)
	sessions, _ := list["sessions"].([]any)
	if listSt != 200 || len(sessions) != 1 {
		t.Fatalf("重登应恢复个人工程: %d %v", listSt, list)
	}
	id, _ := proj["id"].(string)
	if got, _ := f.do(t, "GET", "/api/v1/projects/"+id, relogin, nil); got != 200 {
		t.Fatalf("本人应能打开个人工程: %d", got)
	}
	other := f.stub.mint("yi@x.com", "product-image")
	if got, _ := f.do(t, "GET", "/api/v1/projects/"+id, other, nil); got != 404 {
		t.Fatalf("他人不能打开该个人工程: %d", got)
	}
}

func TestOrderAndCampaignRestoreAssetsAndExactReturn(t *testing.T) {
	f := newFixture(t, nil)
	tok := f.stub.mintExtra("jia@x.com", "product-image", jwt.MapClaims{
		"org_memberships": []string{"org_customer", "org_other"},
	})
	orderBody := map[string]any{
		"source_type": "order", "source_ref": "ord_9", "source_tenant_id": "org_customer",
		"return_href": "https://order.example/orders/ord_9",
		"authorized_assets": []any{map[string]any{"asset_ref": "asset_ord", "owner_tenant": "org_customer"}},
		"usage_kind": "电商主图", "width_px": 800, "height_px": 800, "background_direction": "场景",
	}
	st, body := f.do(t, "POST", "/api/v1/entry/source", tok, orderBody)
	if st != http.StatusCreated {
		t.Fatalf("订单入口: %d %v", st, body)
	}
	if body["return_href"] != "https://order.example/orders/ord_9" {
		t.Fatalf("返回地址应精确: %v", body["return_href"])
	}
	ws, _ := body["workspace"].(map[string]any)
	if ws["scope"] != "org_customer" {
		t.Fatalf("应留在客户组织: %v", ws)
	}
	journey, _ := body["journey"].(map[string]any)
	if journey["input_count"] != float64(0) || journey["reupload_count"] != float64(0) {
		t.Fatalf("已授权素材不应要求再上传: %v", journey)
	}
	proj, _ := body["project"].(map[string]any)
	id, _ := proj["id"].(string)
	detailSt, detail := f.do(t, "GET", "/api/v1/projects/"+id, tok, nil)
	inputs, _ := detail["inputs"].([]any)
	if detailSt != 200 || len(inputs) != 1 {
		t.Fatalf("应带入已授权素材: %d %v", detailSt, detail)
	}
	in0, _ := inputs[0].(map[string]any)
	if in0["platform_asset_id"] != "asset_ord" {
		t.Fatalf("素材引用不对: %v", in0)
	}
	againSt, again := f.do(t, "POST", "/api/v1/entry/source", tok, orderBody)
	againProj, _ := again["project"].(map[string]any)
	if againSt != http.StatusOK || againProj["id"] != id {
		t.Fatalf("再次深链应恢复同一工程: %d %v", againSt, again)
	}
	personalSt, personal := f.do(t, "GET", "/api/v1/entry/personal", tok, nil)
	sessions, _ := personal["sessions"].([]any)
	if personalSt != 200 || len(sessions) != 0 {
		t.Fatalf("客户工程不能出现在个人空间: %d %v", personalSt, personal)
	}
	campSt, camp := f.do(t, "POST", "/api/v1/entry/source", tok, map[string]any{
		"source_type": "campaign", "source_ref": "cmp_3", "source_tenant_id": "org_other",
		"return_href": "https://campaign.example/c/cmp_3?stage=design",
		"authorized_assets": []any{map[string]any{"asset_ref": "asset_cmp", "owner_tenant": "org_other"}},
		"usage_kind": "活动主图", "width_px": 1080, "height_px": 1080, "background_direction": "节日",
	})
	if campSt != http.StatusCreated || camp["return_href"] != "https://campaign.example/c/cmp_3?stage=design" {
		t.Fatalf("活动入口: %d %v", campSt, camp)
	}
	deniedSt, denied := f.do(t, "POST", "/api/v1/entry/source", tok, map[string]any{
		"source_type": "order", "source_ref": "ord_x", "source_tenant_id": "org_stranger",
		"return_href": "https://order.example/orders/ord_x",
		"authorized_assets": []any{map[string]any{"asset_ref": "asset_x", "owner_tenant": "org_stranger"}},
		"usage_kind": "电商主图", "width_px": 800, "height_px": 800,
	})
	if deniedSt != http.StatusForbidden {
		t.Fatalf("未授权组织应拒绝: %d %v", deniedSt, denied)
	}
	if personalSt, personal := f.do(t, "GET", "/api/v1/entry/personal", tok, nil); personalSt != 200 {
		t.Fatal(personal)
	} else if sessions, _ := personal["sessions"].([]any); len(sessions) != 0 {
		t.Fatalf("拒绝后也不能把客户资产写入个人空间: %v", personal)
	}
}

func TestFirstImageDegradesHonestlyAndResumes(t *testing.T) {
	var billed atomic.Int32
	bill := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		billed.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"balance_cny":1}`))
	}))
	defer bill.Close()
	var submits atomic.Int32
	tasks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer tasks.Close()

	f := newFixture(t, func(c *config.Config) {
		c.BillingBaseURL = bill.URL
		c.TaskBaseURL = tasks.URL
	})
	tok := f.stub.mint("jia@x.com", "product-image")
	st, started := f.do(t, "POST", "/api/v1/entry/start", tok, map[string]any{
		"name": "诚实降级", "usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍",
	})
	if st != http.StatusCreated {
		t.Fatalf("start: %d %v", st, started)
	}
	proj, _ := started["project"].(map[string]any)
	id, _ := proj["id"].(string)
	genSt, gen := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image", tok, map[string]any{
		"usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍",
	})
	session, _ := gen["session"].(map[string]any)
	if genSt != http.StatusCreated || session["status"] != "failed" || session["degraded_reason"] != "generation_unavailable" {
		t.Fatalf("未开生成应失败降级: %d %v", genSt, gen)
	}
	if session["output_asset_id"] != "" || session["quote_label"] != "待确认" || session["charged"] != false || gen["useful_proven"] != false {
		t.Fatalf("不能登记假图或扣费: %v", gen)
	}
	if submits.Load() != 0 || billed.Load() != 0 {
		t.Fatalf("未接通时不能打任务或计费: tasks=%d billing=%d", submits.Load(), billed.Load())
	}
	againSt, again := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image", tok, map[string]any{
		"usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍",
	})
	againSession, _ := again["session"].(map[string]any)
	if againSt != http.StatusOK || againSession["id"] != session["id"] {
		t.Fatalf("刷新不能另起任务: %d %v", againSt, again)
	}
	leaveSt, left := f.do(t, "GET", "/api/v1/projects/"+id+"/first-image", f.stub.mint("jia@x.com", "product-image"), nil)
	leftSession, _ := left["session"].(map[string]any)
	if leaveSt != 200 || leftSession["id"] != session["id"] || leftSession["status"] != "failed" {
		t.Fatalf("离开再回来应是同一任务: %d %v", leaveSt, left)
	}
}

func TestUnknownTaskDoesNotResubmit(t *testing.T) {
	var submits atomic.Int32
	var polls atomic.Int32
	upstream := "queued"
	tasks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			submits.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		polls.Add(1)
		_ = upstream
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"task_id":"task_should_not_exist","status":"running"}`))
	}))
	defer tasks.Close()
	f := newFixture(t, func(c *config.Config) {
		c.GenerationEnabled = true
		c.TaskBaseURL = tasks.URL
	})
	tok := f.stub.mint("jia@x.com", "product-image")
	_, started := f.do(t, "POST", "/api/v1/entry/start", tok, map[string]any{
		"name": "未知任务", "usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍",
	})
	proj, _ := started["project"].(map[string]any)
	id, _ := proj["id"].(string)
	payload := map[string]any{
		"usage_kind": "电商主图", "width_px": 800, "height_px": 800, "background_direction": "浅灰棚拍",
	}
	st, first := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image", tok, payload)
	session, _ := first["session"].(map[string]any)
	if st != http.StatusCreated || session["status"] != "unknown" || session["output_asset_id"] != "" {
		t.Fatalf("不可达应为 unknown: %d %v", st, first)
	}
	if submits.Load() != 1 {
		t.Fatalf("首次应尝试一次任务服务: %d", submits.Load())
	}
	st, second := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image/refresh", tok, nil)
	secondSession, _ := second["session"].(map[string]any)
	if st != 200 || secondSession["id"] != session["id"] || secondSession["status"] != "unknown" || submits.Load() != 1 || polls.Load() != 0 {
		t.Fatalf("unknown 且没有平台任务号时不能再提交: submits=%d polls=%d %v", submits.Load(), polls.Load(), second)
	}
}

func TestQueuedTaskResumesUntilRealAsset(t *testing.T) {
	var submits atomic.Int32
	status := "queued"
	asset := ""
	tasks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			submits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"task_id":"task_real","status":"queued"}`))
			return
		}
		body := `{"task_id":"task_real","status":"` + status + `"}`
		if asset != "" {
			body = `{"task_id":"task_real","status":"` + status + `","result":{"platform_asset_id":"` + asset + `"}}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer tasks.Close()
	f := newFixture(t, func(c *config.Config) {
		c.GenerationEnabled = true
		c.TaskBaseURL = tasks.URL
	})
	tok := f.stub.mint("jia@x.com", "product-image")
	_, started := f.do(t, "POST", "/api/v1/entry/start", tok, map[string]any{
		"name": "排队任务", "usage_kind": "电商主图", "width_px": 800, "height_px": 800,
		"background_direction": "浅灰棚拍",
	})
	proj, _ := started["project"].(map[string]any)
	id, _ := proj["id"].(string)
	payload := map[string]any{
		"usage_kind": "电商主图", "width_px": 800, "height_px": 800, "background_direction": "浅灰棚拍",
	}
	st, queued := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image", tok, payload)
	session, _ := queued["session"].(map[string]any)
	if st != http.StatusCreated || session["status"] != "queued" || session["platform_task_id"] != "task_real" {
		t.Fatalf("应记下排队中的真实任务: %d %v", st, queued)
	}
	status = "running"
	st, running := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image/refresh", tok, nil)
	runningSession, _ := running["session"].(map[string]any)
	if st != 200 || runningSession["id"] != session["id"] || runningSession["status"] != "running" || submits.Load() != 1 {
		t.Fatalf("刷新应推进同一任务: submits=%d %v", submits.Load(), running)
	}
	status = "succeeded"
	st, bare := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image/refresh", tok, nil)
	bareSession, _ := bare["session"].(map[string]any)
	if st != 200 || bareSession["status"] != "unknown" || bareSession["output_asset_id"] != "" || bare["useful_proven"] != false {
		t.Fatalf("没有资产不能完成: %d %v", st, bare)
	}
	status = "succeeded"
	asset = "asset_real"
	st, done := f.do(t, "POST", "/api/v1/projects/"+id+"/first-image/refresh", tok, nil)
	doneSession, _ := done["session"].(map[string]any)
	if st != 200 || doneSession["id"] != session["id"] || doneSession["status"] != "completed" || doneSession["output_asset_id"] != "asset_real" {
		t.Fatalf("只接受上游真实资产: %d %v", st, done)
	}
	if done["useful_proven"] != false || doneSession["charged"] != false || doneSession["quote_label"] != "待确认" {
		t.Fatalf("有资产也不等于可售或已扣费: %v", done)
	}
	outSt, outs := f.do(t, "GET", "/api/v1/projects/"+id+"/outputs", tok, nil)
	outputs, _ := outs["outputs"].([]any)
	if outSt != 200 || len(outputs) != 0 {
		t.Fatalf("不能把候选登记成可回流成果: %d %v", outSt, outs)
	}
}
