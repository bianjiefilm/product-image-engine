package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func selectionMissing(t *testing.T, f *fixture, tenant, projectID string) {
	t.Helper()
	_, err := f.st.GetCampaignSelection(context.Background(), tenant, projectID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("打开、下载或登记不得写入活动选定版本: %v", err)
	}
}

func deriveUser(email string) string {
	sum := sha256.Sum256([]byte("user:" + strings.ToLower(email)))
	return "usr_" + hex.EncodeToString(sum[:])[:16]
}

func campaignScope(tenant, userID, projectID string) si.Scope {
	return si.Scope{
		AppID: "product-image", TenantID: tenant, ProjectID: projectID,
		UserID: userID, PrincipalAccountID: tenant, PayerAccountID: tenant,
	}
}

func freezeCampaignRun(t *testing.T, f *fixture, tenant, userID, projectID string) si.Run {
	t.Helper()
	run, dup, err := f.st.CreateSourceRun(context.Background(), si.Intent{
		Scope: campaignScope(tenant, userID, projectID), RequestKey: "camp-req-frozen",
		Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0",
		Capability: "image.generate", Size: "1024*1024", Prompt: "same product facts",
		PricingVersion: "price-frozen", Quantity: 1,
	})
	if err != nil || dup || run.TaskKey == "" || run.UsageKey == "" || run.Phase != "intent" || run.Quote != nil || run.TaskID != "" {
		t.Fatalf("冻结生成身份失败: dup=%v err=%v phase=%s task=%s", dup, err, run.Phase, run.TaskKey)
	}
	return run
}

func assertRunFrozen(t *testing.T, f *fixture, tenant, userID, projectID string, want si.Run) {
	t.Helper()
	scope := campaignScope(tenant, userID, projectID)
	got, err := f.st.FindSourceRun(context.Background(), scope, "camp-req-frozen")
	if err != nil {
		t.Fatalf("读取生成身份: %v", err)
	}
	if got.ID != want.ID || got.Intent.RequestKey != "camp-req-frozen" || got.TaskKey != want.TaskKey || got.UsageKey != want.UsageKey || got.Phase != "intent" || got.Quote != nil || got.TaskID != "" {
		t.Fatalf("恢复或回执改了任务键或幂等键: id %s/%s request %s task %s/%s usage %s/%s phase %s quoted %v taskID %q",
			got.ID, want.ID, got.Intent.RequestKey, got.TaskKey, want.TaskKey, got.UsageKey, want.UsageKey, got.Phase, got.Quote != nil, got.TaskID)
	}
	runs, err := f.st.ListSourceRuns(context.Background(), scope)
	if err != nil || len(runs) != 1 {
		t.Fatalf("不得新开生成或报价记录: n=%d err=%v", len(runs), err)
	}
}

func assertNoCampaignReceipt(t *testing.T, f *fixture, tenant, outputID string) {
	t.Helper()
	_, err := f.st.FindReceiptByOutput(context.Background(), tenant, outputID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("目标未登记不得落下活动回执: %v", err)
	}
}

func TestCampaignRestoreRequiresExplicitSelection(t *testing.T) {
	eco := newEcoStub(t)
	f := newFixture(t, func(c *config.Config) {
		c.UploadBaseURL = eco.srv.URL
		c.UploadToken = "up-tok"
		c.ReceiptKeys = "orders:" + testSecret
		c.PhotoUploadEnabled = true
	})
	// 与正式登记表一致:活动没有 receipt target。不把桩地址写成活动目标。
	attachRegistry(t, f, mustRegistry(t, eco.srv.URL+"/internal/v1/handoff/receipts"))

	_, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	tenant := deriveTenant("jia@x.com")
	userID := deriveUser("jia@x.com")

	doc := handoffDoc("h-camp-fwd-1", "touch-engine", "campaign", tenant, "src-camp", "bind-camp-fwd-1", "rev-1", "brief-1")
	st, accepted := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
	if st != 200 {
		t.Fatalf("活动接受应 200: %d %v", st, accepted)
	}
	proj, _ := accepted["project"].(map[string]any)
	projID, _ := proj["id"].(string)
	if proj["created_by"] != userID {
		t.Fatalf("生成身份必须挂在当前登录用户上: %v", proj["created_by"])
	}

	st, page := f.do(t, "GET", "/api/v1/projects/"+projID, tok, nil)
	inputs, _ := page["inputs"].([]any)
	if st != 200 || len(inputs) != 1 {
		t.Fatalf("打开页面应只读交接素材: %d %v", st, page)
	}
	selectionMissing(t, f, tenant, projID)
	in0, _ := inputs[0].(map[string]any)
	st, downloaded := f.do(t, "GET", "/api/v1/projects/"+projID+"/inputs/"+in0["id"].(string)+"/content", tok, nil)
	if st != 502 && st != 503 {
		t.Fatalf("下载未接通的交接素材应失败关闭: %d %v", st, downloaded)
	}
	selectionMissing(t, f, tenant, projID)

	beforeRuns, err := f.st.ListSourceRuns(context.Background(), campaignScope(tenant, userID, projID))
	if err != nil || len(beforeRuns) != 0 {
		t.Fatalf("接受与打开不得生成: %d %v", len(beforeRuns), err)
	}
	frozen := freezeCampaignRun(t, f, tenant, userID, projID)

	st, restore := f.do(t, "GET", "/api/v1/projects/"+projID+"/campaign-restore", tok, nil)
	if st != 200 {
		t.Fatalf("活动来源恢复应 200: %d %v", st, restore)
	}
	if restore["campaign_ref"] != "camp-2026-091" || restore["source_kind"] != "campaign" || restore["source_ref"] != "src-camp" {
		t.Fatalf("应恢复活动来源事实: %v", restore)
	}
	if restore["brief_version"] != "brief-1" || restore["source_revision"] != "rev-1" {
		t.Fatalf("应恢复已保存的需求版本: %v", restore)
	}
	if restore["selected_version"] != nil || restore["receipt"] != nil {
		t.Fatalf("未选定前不得带版本或回执: %v", restore)
	}
	auth, _ := restore["authorization"].(map[string]any)
	scopes, _ := auth["scopes"].([]any)
	caps, _ := auth["capabilities"].([]any)
	if len(scopes) != 3 || len(caps) != 1 || caps[0] != "image.generate" {
		t.Fatalf("授权范围应随来源恢复: %v", auth)
	}
	selectionMissing(t, f, tenant, projID)
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	st, againAccept := f.do(t, "POST", "/api/v1/handoffs/accept", tok, acceptBody(doc, "主图"))
	againProj, _ := againAccept["project"].(map[string]any)
	if st != 200 || againAccept["resolution"] != "restored" || againProj["id"] != projID {
		t.Fatalf("同一交接重放只恢复原工程: %d %v", st, againAccept)
	}
	selectionMissing(t, f, tenant, projID)
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	if _, listed := f.do(t, "GET", "/api/v1/projects/"+projID+"/outputs", tok, nil); listed == nil {
		t.Fatal("列出成果失败")
	}
	binding, _ := accepted["binding"].(map[string]any)
	if _, detail := f.do(t, "GET", "/api/v1/bindings/"+binding["id"].(string), tok, nil); detail == nil {
		t.Fatal("读取绑定失败")
	}
	selectionMissing(t, f, tenant, projID)

	png1 := pngBytes(t, 2, 2, 7)
	uploadsBefore := eco.uploads.Load()
	st, registered := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "chosen.png", "content_type": "image/png", "data_b64": b64(png1),
	})
	if st != 201 {
		t.Fatalf("登记成果应 201: %d %v", st, registered)
	}
	if eco.uploads.Load() != uploadsBefore+1 {
		t.Fatalf("首次登记应上传一次: %d", eco.uploads.Load())
	}
	out, _ := registered["output"].(map[string]any)
	outID, _ := out["id"].(string)
	selectionMissing(t, f, tenant, projID)
	uploadsAfter := eco.uploads.Load()
	st, dupOut := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok, map[string]any{
		"file_name": "chosen.png", "content_type": "image/png", "data_b64": b64(png1),
	})
	if st != 200 || dupOut["duplicate"] != true || eco.uploads.Load() != uploadsAfter {
		t.Fatalf("同一成果不得重复上传或变成选定: %d %v uploads=%d", st, dupOut, eco.uploads.Load())
	}
	selectionMissing(t, f, tenant, projID)
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	st, denied := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+outID+"/receipt", tok, nil)
	if st != 409 {
		t.Fatalf("未选定不得回执: %d %v", st, denied)
	}
	if code, _ := errCodeOf(denied); code != "version_not_selected" {
		t.Fatalf("错误码应为 version_not_selected: %v", denied)
	}
	if eco.eventCount() != 0 {
		t.Fatalf("拒绝回执不得投递: %d", eco.eventCount())
	}
	assertNoCampaignReceipt(t, f, tenant, outID)

	st, selected := f.do(t, "POST", "/api/v1/projects/"+projID+"/campaign-selection", tok, map[string]any{"output_id": outID})
	if st != 201 || selected["duplicate"] == true {
		t.Fatalf("首次选定应 201: %d %v", st, selected)
	}
	st, again := f.do(t, "POST", "/api/v1/projects/"+projID+"/campaign-selection", tok, map[string]any{"output_id": outID})
	if st != 200 || again["duplicate"] != true {
		t.Fatalf("同版重选应幂等: %d %v", st, again)
	}
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	_, pair2 := f.login(t, "jia@x.com", "right-pass")
	tok2, _ := pair2["access_token"].(string)
	st, restored := f.do(t, "GET", "/api/v1/projects/"+projID+"/campaign-restore", tok2, nil)
	if st != 200 {
		t.Fatalf("重登恢复应 200: %d %v", st, restored)
	}
	ver, _ := restored["selected_version"].(map[string]any)
	if ver["output_id"] != outID || ver["stale"] == true || restored["receipt"] != nil {
		t.Fatalf("重登应看到选定版本且尚无回执: %v", restored)
	}
	if restored["campaign_ref"] != "camp-2026-091" || restored["brief_version"] != "brief-1" || restored["source_revision"] != "rev-1" {
		t.Fatalf("重登应读回已保存来源: %v", restored)
	}
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	st, forged := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+outID+"/receipt", tok2, map[string]any{
		"receipt_url": "https://touch-engine.example.invalid/api/receipts",
	})
	if st != 409 {
		t.Fatalf("调用方给出的地址不得把活动回执变成成功: %d %v", st, forged)
	}
	if code, _ := errCodeOf(forged); code != "receipt_target_unregistered" {
		t.Fatalf("未登记目标应保持 receipt_target_unregistered: %v", forged)
	}
	assertNoCampaignReceipt(t, f, tenant, outID)
	if eco.eventCount() != 0 {
		t.Fatalf("失败关闭不得投递: %d", eco.eventCount())
	}
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	doc2 := handoffDoc("h-camp-fwd-2", "touch-engine", "campaign", tenant, "src-camp", "bind-camp-fwd-2", "rev-2", "brief-2")
	st, next := f.do(t, "POST", "/api/v1/handoffs/accept", tok2, acceptBody(doc2, "主图"))
	if st != 200 {
		t.Fatalf("新版活动交接应 200: %d %v", st, next)
	}
	pending, _ := next["pending_snapshot"].(map[string]any)
	bindID, _ := binding["id"].(string)
	st, adopted := f.do(t, "POST", "/api/v1/bindings/"+bindID+"/adopt", tok2, map[string]any{"snapshot_id": pending["id"]})
	if st != 200 || adopted["adopted"] != true {
		t.Fatalf("采用新版失败: %d %v", st, adopted)
	}
	st, staleView := f.do(t, "GET", "/api/v1/projects/"+projID+"/campaign-restore", tok2, nil)
	staleVer, _ := staleView["selected_version"].(map[string]any)
	if st != 200 || staleVer["output_id"] != outID || staleVer["stale"] != true || staleView["receipt"] != nil {
		t.Fatalf("新版采用后旧选定应保留并标为过期,不得清掉: %v", staleView)
	}
	st, keepOld := f.do(t, "POST", "/api/v1/projects/"+projID+"/campaign-selection", tok2, map[string]any{"output_id": outID})
	if st != 409 {
		t.Fatalf("过期成果不得再次选定: %d %v", st, keepOld)
	}
	if code, _ := errCodeOf(keepOld); code != "output_not_current" {
		t.Fatalf("过期选定错误码应为 output_not_current: %v", keepOld)
	}
	st, oldAgain := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+outID+"/receipt", tok2, nil)
	if st != 409 {
		t.Fatalf("过期成果不得回执: %d %v", st, oldAgain)
	}
	if code, _ := errCodeOf(oldAgain); code != "stale_requirement_version" {
		t.Fatalf("过期回执错误码应为 stale_requirement_version: %v", oldAgain)
	}
	assertNoCampaignReceipt(t, f, tenant, outID)
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	st, registered2 := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs", tok2, map[string]any{
		"file_name": "next.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 2, 2, 9)),
	})
	if st != 201 {
		t.Fatalf("新版登记应 201: %d %v", st, registered2)
	}
	out2, _ := registered2["output"].(map[string]any)
	out2ID, _ := out2["id"].(string)
	selectionStill, selErr := f.st.GetCampaignSelection(context.Background(), tenant, projID)
	if selErr != nil || selectionStill.OutputID != outID {
		t.Fatalf("登记新成果不得移动选定指针: %v %+v", selErr, selectionStill)
	}
	st, blocked := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out2ID+"/receipt", tok2, nil)
	if st != 409 {
		t.Fatalf("未改选的新成果不得回执: %d %v", st, blocked)
	}
	if code, _ := errCodeOf(blocked); code != "version_not_selected" {
		t.Fatalf("新成果错误码应为 version_not_selected: %v", blocked)
	}
	assertNoCampaignReceipt(t, f, tenant, out2ID)
	st, moved := f.do(t, "POST", "/api/v1/projects/"+projID+"/campaign-selection", tok2, map[string]any{"output_id": out2ID})
	if st != 200 || moved["replaced"] != true {
		t.Fatalf("改选应移动指针: %d %v", st, moved)
	}
	st, now := f.do(t, "GET", "/api/v1/projects/"+projID+"/campaign-restore", tok2, nil)
	nowVer, _ := now["selected_version"].(map[string]any)
	if nowVer["output_id"] != out2ID || nowVer["stale"] == true || now["receipt"] != nil {
		t.Fatalf("改选后恢复应指向新版且不继承回执: %v", now)
	}
	st, stillClosed := f.do(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out2ID+"/receipt", tok2, nil)
	if st != 409 {
		t.Fatalf("选定后目标仍未登记,回执必须失败关闭: %d %v", st, stillClosed)
	}
	if code, _ := errCodeOf(stillClosed); code != "receipt_target_unregistered" {
		t.Fatalf("选定后错误码仍应为 receipt_target_unregistered: %v", stillClosed)
	}
	assertNoCampaignReceipt(t, f, tenant, out2ID)
	_, receipts := f.do(t, "GET", "/api/v1/projects/"+projID+"/receipts", tok2, nil)
	rows, _ := receipts["receipts"].([]any)
	if len(rows) != 0 || eco.eventCount() != 0 {
		t.Fatalf("活动回执不得投递或落库: %v events=%d", receipts, eco.eventCount())
	}
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	orderDoc := handoffDoc("h-order-fwd-1", "orders", "order", tenant, "src-order", "bind-order-fwd-1", "rev-1", "brief-1")
	st, orderAccepted := f.do(t, "POST", "/api/v1/handoffs/accept", tok2, acceptBody(orderDoc, "主图"))
	if st != 200 {
		t.Fatalf("订单接受应 200: %d %v", st, orderAccepted)
	}
	orderProj, _ := orderAccepted["project"].(map[string]any)
	orderID, _ := orderProj["id"].(string)
	st, orderRestore := f.do(t, "GET", "/api/v1/projects/"+orderID+"/campaign-restore", tok2, nil)
	if st != 404 {
		t.Fatalf("订单不得走活动恢复: %d %v", st, orderRestore)
	}
	if code, _ := errCodeOf(orderRestore); code != "not_campaign" {
		t.Fatalf("订单恢复错误码应为 not_campaign: %v", orderRestore)
	}
	st, orderOut := f.do(t, "POST", "/api/v1/projects/"+orderID+"/outputs", tok2, map[string]any{
		"file_name": "order.png", "content_type": "image/png", "data_b64": b64(pngBytes(t, 2, 2, 3)),
	})
	if st != 201 {
		t.Fatalf("订单登记应 201: %d %v", st, orderOut)
	}
	orderOutput, _ := orderOut["output"].(map[string]any)
	st, orderReceipt := f.do(t, "POST", "/api/v1/projects/"+orderID+"/outputs/"+orderOutput["id"].(string)+"/receipt", tok2, nil)
	if st != 201 {
		t.Fatalf("订单回执不要求活动选定: %d %v", st, orderReceipt)
	}
	selectionMissing(t, f, tenant, orderID)
	if eco.eventCount() != 1 {
		t.Fatalf("只有订单回执可以投递: %d", eco.eventCount())
	}
	eco.mu.Lock()
	ev := eco.events[0]
	eco.mu.Unlock()
	if ev["schema_version"] != "order-receipt/v1" || ev["target_app"] != "orders" {
		t.Fatalf("订单回执契约不得改成活动投递: %v", ev)
	}
	if _, ok := ev["campaign_ref"]; ok {
		t.Fatalf("活动字段不得写入 order-receipt/v1: %v", ev)
	}
	assertRunFrozen(t, f, tenant, userID, projID, frozen)

	_, other := f.login(t, "other@x.com", "right-pass")
	otherTok, _ := other["access_token"].(string)
	st, hidden := f.do(t, "GET", "/api/v1/projects/"+projID+"/campaign-restore", otherTok, nil)
	if st != 404 {
		t.Fatalf("跨租户活动恢复应不可见: %d %v", st, hidden)
	}
}

func TestDefaultManifestKeepsCampaignReceiptClosed(t *testing.T) {
	m, err := appregistry.DefaultManifest()
	if err != nil {
		t.Fatalf("正式登记表: %v", err)
	}
	if _, err := m.FirstReceiptTarget("touch-engine"); err == nil {
		t.Fatal("正式登记表不得写入活动 receipt target")
	}
	orders, err := m.FirstReceiptTarget("orders")
	if err != nil || orders.TargetID != "rc-orders-main" {
		t.Fatalf("订单回执目标应保持原登记: %v %+v", err, orders)
	}
}
