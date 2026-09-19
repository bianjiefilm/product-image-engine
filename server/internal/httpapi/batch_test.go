package httpapi

// 批次编排 E2E 验收(HUI-1704 / FEAT-0205)。
// 验收矩阵(票面):
//   1. 批次 8 项 stub worker 全成功 → completed,outputs 全登记;
//   2. 2 项失败 → completed_with_failures 逐 item error_code;重试 → 新 task 引用;
//   3. task 不可达 → 全 blocked 批次如实,不伪造;开关 off → 创建成功但提交全 blocked;
//   4. 幂等:同 batch+同输入 sha → 去重成一项;重复 collect 幂等;变体重复 finalize 幂等;
//   5. 并发提交 ≤4 断言;
//   6. 取消:pending 批次取消 → items 全 cancelled;A/B 租户隔离。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// ---- 桩:platform-upload / platform-task -------------------------------------

// batchUploadStub 登记桩:计数并返回递增 asset_id;fail=true 模拟登记服务故障(5xx)。
type batchUploadStub struct {
	srv   *httptest.Server
	calls atomic.Int64
	fail  atomic.Bool
}

func newBatchUploadStub(t *testing.T) *batchUploadStub {
	t.Helper()
	us := &batchUploadStub{}
	us.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/upload/assets" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if us.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		n := us.calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"asset_id": fmt.Sprintf("asset_%d", n)})
	}))
	t.Cleanup(us.srv.Close)
	return us
}

// taskStub 任务桩:submit 发号 task_<n>;decide 决定该号任务的终态与结果载荷
// (status: succeeded|failed|running);GET /internal/v1/tasks/{id} 按记录应答,
// 可用 setStatus 事后推进(模拟异步)。inFlight/maxInFlight 用于并发上限断言。
type taskStub struct {
	srv         *httptest.Server
	submits     atomic.Int64
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
	latency     time.Duration
	mu          sync.Mutex
	statuses    map[string]string
	results     map[string]map[string]any
	decide      func(n int64) (string, map[string]any)
}

func newTaskStub(t *testing.T, decide func(n int64) (string, map[string]any)) *taskStub {
	t.Helper()
	ts := &taskStub{statuses: map[string]string{}, results: map[string]map[string]any{}, decide: decide}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/internal/v1/tasks/submit":
			cur := ts.inFlight.Add(1)
			for {
				max := ts.maxInFlight.Load()
				if cur <= max || ts.maxInFlight.CompareAndSwap(max, cur) {
					break
				}
			}
			defer ts.inFlight.Add(-1)
			if ts.latency > 0 {
				time.Sleep(ts.latency)
			}
			n := ts.submits.Add(1)
			st, result := "succeeded", map[string]any(nil)
			if ts.decide != nil {
				st, result = ts.decide(n)
			}
			id := fmt.Sprintf("task_%d", n)
			ts.mu.Lock()
			ts.statuses[id] = st
			if result != nil {
				ts.results[id] = result
			}
			ts.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"task_id": id, "status": "queued"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/internal/v1/tasks/"):
			id := strings.TrimPrefix(r.URL.Path, "/internal/v1/tasks/")
			ts.mu.Lock()
			st := ts.statuses[id]
			result := ts.results[id]
			ts.mu.Unlock()
			if st == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"task_id": id, "status": st, "result": result})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

// setStatus 事后推进任务状态(模拟异步完成)。
func (ts *taskStub) setStatus(id, status string, result map[string]any) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.statuses[id] = status
	if result != nil {
		ts.results[id] = result
	}
}

// ---- 测试工具 ------------------------------------------------------------------

// tinyPNG 生成确定性小 PNG(尺寸参与内容,保证不同参数字节不同)。
func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 17), G: uint8(y * 29), B: uint8(x + y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成 PNG: %v", err)
	}
	return buf.Bytes()
}

func batchFilesPayload(files [][]byte) []map[string]any {
	out := make([]map[string]any, 0, len(files))
	for i, b := range files {
		out = append(out, map[string]any{
			"file_name": fmt.Sprintf("product_%02d.png", i+1),
			"data_b64":  base64.StdEncoding.EncodeToString(b),
		})
	}
	return out
}

// createBatch 建批助手:返回 (status, body)。
func (f *fixture) createBatch(t *testing.T, tok string, files [][]byte, extra map[string]any) (int, map[string]any) {
	t.Helper()
	body := map[string]any{
		"name":  "秋季批次",
		"items": batchFilesPayload(files),
	}
	for k, v := range extra {
		body[k] = v
	}
	return f.do(t, "POST", "/api/v1/batches", tok, body)
}

func batchItems(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("响应缺 items: %v", body)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func batchOf(body map[string]any) map[string]any {
	b, _ := body["batch"].(map[string]any)
	return b
}

func countsOf(body map[string]any) map[string]any {
	c, _ := body["batch"].(map[string]any)["counts"].(map[string]any)
	return c
}

// batchFixture 装配:生成开 + 尺寸适配开 + upload/task 桩。
func batchFixture(t *testing.T, us *batchUploadStub, ts *taskStub) *fixture {
	return newFixture(t, func(c *config.Config) {
		c.GenerationEnabled = true
		c.SizeAdaptEnabled = true
		c.UploadBaseURL = us.srv.URL
		c.TaskBaseURL = ts.srv.URL
	})
}

// ---- 状态机纯函数单测 -----------------------------------------------------------

func TestNextBatchStatusAggregation(t *testing.T) {
	item := func(st string) store.BatchItem { return store.BatchItem{Status: st} }
	// 空批次无成功事实 → failed(如实,不给空批次判 completed)
	if got := nextBatchStatus(nil); got != "failed" {
		t.Fatalf("空批次应 failed, got %q", got)
	}
	allOK := []store.BatchItem{item("succeeded"), item("succeeded")}
	if got := nextBatchStatus(allOK); got != "completed" {
		t.Fatalf("全成功应 completed, got %q", got)
	}
	mixed := []store.BatchItem{item("succeeded"), item("failed"), item("succeeded")}
	if got := nextBatchStatus(mixed); got != "completed_with_failures" {
		t.Fatalf("混合应 completed_with_failures, got %q", got)
	}
	allBad := []store.BatchItem{item("failed"), item("blocked")}
	if got := nextBatchStatus(allBad); got != "failed" {
		t.Fatalf("全失败/阻塞应 failed, got %q", got)
	}
	allCancelled := []store.BatchItem{item("cancelled"), item("cancelled")}
	if got := nextBatchStatus(allCancelled); got != "cancelled" {
		t.Fatalf("全取消应 cancelled, got %q", got)
	}
	inFlight := []store.BatchItem{item("succeeded"), item("submitted")}
	if got := nextBatchStatus(inFlight); got != "" {
		t.Fatalf("在途应保持现态(空串), got %q", got)
	}
	cancelledWithSuccess := []store.BatchItem{item("cancelled"), item("succeeded")}
	if got := nextBatchStatus(cancelledWithSuccess); got != "completed" {
		t.Fatalf("取消+成功应 completed(取消不算失败,不夸大损伤), got %q", got)
	}
}

// ---- E2E 场景 ------------------------------------------------------------------

// 验收 1:批次 8 项全成功 → completed,outputs 全登记;变体幂等;重复 collect 幂等。
func TestBatchEightItemsAllSucceedComplete(t *testing.T) {
	us := newBatchUploadStub(t)
	// 每项成果字节不同(同工程同内容成果会被既有 UNIQUE(project,result_sha256) 幂等去重)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		return "succeeded", map[string]any{
			"data_b64":  base64.StdEncoding.EncodeToString(tinyPNG(t, 10+int(n), 10)),
			"file_name": fmt.Sprintf("gen_%02d.png", n),
		}
	})
	f := batchFixture(t, us, ts)

	st, pair := f.login(t, "jia@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)

	// 8 个不同内容文件(尺寸不同→字节不同),带场景×尺寸意图
	files := make([][]byte, 8)
	for i := range files {
		files[i] = tinyPNG(t, 8+i, 6+i)
	}
	st, created := f.createBatch(t, tok, files, map[string]any{
		"scenes":                 []string{"白底主图", "场景图"},
		"presets":                []string{"taobao_main", "douyin_ad"},
		"finalize_with_variants": true,
		"usage_note":             "秋季上新",
	})
	if st != 201 {
		t.Fatalf("建批应 201, got %d %v", st, created)
	}
	batch := batchOf(created)
	batchID, _ := batch["id"].(string)
	projID, _ := batch["project_id"].(string)
	if batch["status"] != "pending" || projID == "" {
		t.Fatalf("建批初态异常: %v", batch)
	}
	if got := countsOf(created); got["total"] != float64(8) || got["pending"] != float64(8) {
		t.Fatalf("counts 应 8/8 pending: %v", got)
	}
	ps, _ := batch["preset_summary"].(map[string]any)
	if len(ps["scenes"].([]any)) != 2 || ps["finalize_with_variants"] != true {
		t.Fatalf("preset_summary 应记录场景×尺寸意图: %v", ps)
	}
	// 批次 = 一个工程,8 个输入
	_, detail := f.do(t, "GET", "/api/v1/projects/"+projID, tok, nil)
	if inputs, _ := detail["inputs"].([]any); len(inputs) != 8 {
		t.Fatalf("批次工程应有 8 个输入: %d", len(inputs))
	}

	// 提交:8 项全部 submitted
	st, submitted := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	if st != 200 {
		t.Fatalf("提交应 200, got %d %v", st, submitted)
	}
	if batchOf(submitted)["status"] != "running" {
		t.Fatalf("提交后批次应 running: %v", batchOf(submitted)["status"])
	}
	for i, it := range batchItems(t, submitted) {
		if it["status"] != "submitted" || it["task_ref"] == "" {
			t.Fatalf("item[%d] 应 submitted 且有 task_ref: %v", i, it)
		}
	}
	if ts.submits.Load() != 8 {
		t.Fatalf("应恰好提交 8 个任务: %d", ts.submits.Load())
	}

	// 收集前:任务仍在跑 → collect 保持 submitted,批次 running
	for _, id := range []string{"task_1", "task_2", "task_3", "task_4", "task_5", "task_6", "task_7", "task_8"} {
		ts.setStatus(id, "running", nil)
	}
	st, partial := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 || batchOf(partial)["status"] != "running" {
		t.Fatalf("任务在跑时收集应保持 running: %d %v", st, batchOf(partial)["status"])
	}
	for i, it := range batchItems(t, partial) {
		if it["status"] != "submitted" {
			t.Fatalf("任务在跑 item[%d] 应保持 submitted: %v", i, it)
		}
	}

	// 任务完成 → 收集:全 succeeded → completed;成果+变体全登记
	for i := 1; i <= 8; i++ {
		ts.setStatus(fmt.Sprintf("task_%d", i), "succeeded", map[string]any{
			"data_b64":  base64.StdEncoding.EncodeToString(tinyPNG(t, 10+i, 10)),
			"file_name": fmt.Sprintf("gen_%02d.png", i),
		})
	}
	st, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 {
		t.Fatalf("收集应 200, got %d %v", st, collected)
	}
	if batchOf(collected)["status"] != "completed" {
		t.Fatalf("收集后批次应 completed: %v", batchOf(collected))
	}
	for i, it := range batchItems(t, collected) {
		if it["status"] != "succeeded" || it["output_id"] == "" {
			t.Fatalf("item[%d] 应 succeeded 且有 output: %v", i, it)
		}
		ids, _ := it["size_variant_ids"].([]any)
		if len(ids) != 2 {
			t.Fatalf("item[%d] 应有 2 个尺寸变体: %v", i, it)
		}
	}
	// 登记计数:8 输入 + 8 成果 + 16 变体
	if got := us.calls.Load(); got != 32 {
		t.Fatalf("平台登记应 32 次(8输入+8成果+16变体), got %d", got)
	}
	// 成果落在工程 outputs(8 条 result)
	_, outs := f.do(t, "GET", "/api/v1/projects/"+projID+"/outputs", tok, nil)
	if list, _ := outs["outputs"].([]any); len(list) != 8 {
		t.Fatalf("工程应登记 8 条成果: %d", len(list))
	}
	// 变体列表 16 条
	_, vs := f.do(t, "GET", "/api/v1/projects/"+projID+"/size-adapt", tok, nil)
	if vl, _ := vs["variants"].([]any); len(vl) != 16 {
		t.Fatalf("应有 16 个尺寸变体: %d", len(vl))
	}

	// 幂等:重复 collect 不重复登记、不变更 item 事实
	st, again := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 || batchOf(again)["status"] != "completed" {
		t.Fatalf("重复收集应 200/completed: %d %v", st, batchOf(again)["status"])
	}
	if got := us.calls.Load(); got != 32 {
		t.Fatalf("重复收集不得再登记: %d", got)
	}
	firstItems := batchItems(t, collected)
	secondItems := batchItems(t, again)
	for i := range firstItems {
		if firstItems[i]["output_id"] != secondItems[i]["output_id"] {
			t.Fatalf("重复收集 output_id 应不变: item[%d]", i)
		}
	}
	// 变体重复 finalize 幂等(1703 唯一键):数量不变
	_, vs2 := f.do(t, "GET", "/api/v1/projects/"+projID+"/size-adapt", tok, nil)
	if vl, _ := vs2["variants"].([]any); len(vl) != 16 {
		t.Fatalf("重复收集后变体仍应 16 个: %d", len(vl))
	}
	// 列表 + 详情 counts
	st, list := f.do(t, "GET", "/api/v1/batches", tok, nil)
	if st != 200 {
		t.Fatalf("批次列表应 200: %d", st)
	}
	if raws, _ := list["batches"].([]any); len(raws) != 1 {
		t.Fatalf("批次列表应 1 条: %v", list)
	}
	st, got := f.do(t, "GET", "/api/v1/batches/"+batchID, tok, nil)
	if st != 200 || batchOf(got)["status"] != "completed" {
		t.Fatalf("详情应 200/completed: %d %v", st, batchOf(got)["status"])
	}
	if c := countsOf(got); c["succeeded"] != float64(8) {
		t.Fatalf("详情 counts 应 8 成功: %v", c)
	}
}

// 验收 2:2 项失败 → completed_with_failures 逐 item error_code;重试 → 新 task 引用。
func TestBatchTwoFailuresCompletedWithFailuresThenRetry(t *testing.T) {
	us := newBatchUploadStub(t)
	okPayload := tinyPNG(t, 10, 10)
	failSet := map[int64]bool{2: true, 7: true}
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		if failSet[n] {
			return "failed", map[string]any{"error_code": "worker_gpu_oom"}
		}
		return "succeeded", map[string]any{
			"data_b64": base64.StdEncoding.EncodeToString(okPayload), "file_name": "out.png",
		}
	})
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)

	files := make([][]byte, 8)
	for i := range files {
		files[i] = tinyPNG(t, 20+i, 20)
	}
	_, created := f.createBatch(t, tok, files, nil)
	batchID, _ := batchOf(created)["id"].(string)
	f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	st, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 {
		t.Fatalf("收集应 200: %d %v", st, collected)
	}
	if batchOf(collected)["status"] != "completed_with_failures" {
		t.Fatalf("应 completed_with_failures: %v", batchOf(collected)["status"])
	}
	if got := countsOf(collected); got["succeeded"] != float64(6) || got["failed"] != float64(2) {
		t.Fatalf("counts 应 6 成功 2 失败: %v", got)
	}
	var failedTaskRefs []string
	for _, it := range batchItems(t, collected) {
		switch it["status"] {
		case "failed":
			if it["error_code"] != "worker_gpu_oom" {
				t.Fatalf("失败项应带逐项 error_code: %v", it)
			}
			failedTaskRefs = append(failedTaskRefs, it["task_ref"].(string))
		case "succeeded":
		default:
			t.Fatalf("不应有其他状态: %v", it)
		}
	}
	if len(failedTaskRefs) != 2 {
		t.Fatalf("应 2 个失败项: %v", failedTaskRefs)
	}
	// 已完结批次(有成功)不能直接 submit
	if st, _ := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil); st != 409 {
		t.Fatalf("completed_with_failures 再 submit 应 409, got %d", st)
	}
	// 重试失败项:新 task 引用
	st, retried := f.do(t, "POST", "/api/v1/batches/"+batchID+"/retry-failed", tok, nil)
	if st != 200 {
		t.Fatalf("重试应 200: %d %v", st, retried)
	}
	newRefs := map[string]bool{}
	for _, it := range batchItems(t, retried) {
		if it["status"] == "submitted" && it["task_ref"] != "" {
			newRefs[it["task_ref"].(string)] = true
		}
	}
	if len(newRefs) != 2 {
		t.Fatalf("重试应产生 2 个新 task 引用: %v", newRefs)
	}
	for _, old := range failedTaskRefs {
		if newRefs[old] {
			t.Fatalf("重试不得复用旧 task 引用: %s", old)
		}
	}
	// 重试成功 → completed
	_, done := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if batchOf(done)["status"] != "completed" {
		t.Fatalf("重试成功后应 completed: %v", batchOf(done)["status"])
	}
}

// 验收 3:task 不可达 → 全 blocked,批次如实 failed,不伪造;修复后 submit 可恢复。
func TestBatchTaskUnreachableAllBlockedThenRecover(t *testing.T) {
	us := newBatchUploadStub(t)
	okPayload := tinyPNG(t, 10, 10)
	// 恢复期任务即成功并回传合法 PNG(首阶段不可达不会触到该 decide)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		return "succeeded", map[string]any{
			"data_b64":  base64.StdEncoding.EncodeToString(okPayload),
			"file_name": fmt.Sprintf("gen_%02d.png", n),
		}
	})
	f := batchFixture(t, us, ts)
	// 拔掉 task(指向封闭端口)
	f.srv.Tasks.BaseURL = "http://127.0.0.1:1"

	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4), tinyPNG(t, 5, 5), tinyPNG(t, 6, 6)}, nil)
	batchID, _ := batchOf(created)["id"].(string)

	st, submitted := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	if st != 200 {
		t.Fatalf("提交应 200(如实落终态): %d %v", st, submitted)
	}
	if batchOf(submitted)["status"] != "failed" || batchOf(submitted)["error_code"] != "task_unavailable" {
		t.Fatalf("批次应 failed+task_unavailable: %v / %v", batchOf(submitted)["status"], batchOf(submitted)["error_code"])
	}
	for i, it := range batchItems(t, submitted) {
		if it["status"] != "blocked" || it["error_code"] != "task_unavailable" || it["task_ref"] != "" {
			t.Fatalf("item[%d] 应 blocked 且无 task 引用(不伪造): %v", i, it)
		}
	}
	if ts.submits.Load() != 0 {
		t.Fatalf("不可达不得产生任务: %d", ts.submits.Load())
	}
	// 恢复:指向可用 task 桩,再次 submit → blocked 项重试成功
	f.srv.Tasks.BaseURL = ts.srv.URL
	st, again := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	if st != 200 || batchOf(again)["status"] != "running" {
		t.Fatalf("修复后提交应 running: %d %v", st, batchOf(again)["status"])
	}
	_, done := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if batchOf(done)["status"] != "completed" {
		t.Fatalf("恢复后应 completed: %v", batchOf(done)["status"])
	}
}

// 验收 4:生成开关 off → 创建成功但提交全 blocked(与 I0 语义一致)。
func TestBatchGenerationOffCreateOkSubmitAllBlocked(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, nil)
	// 生成开关 off(fixture 缺省 false),upload 可用(登记输入不触生成门)
	f := newFixture(t, func(c *config.Config) {
		c.UploadBaseURL = us.srv.URL
		c.TaskBaseURL = ts.srv.URL
	})
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	st, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4), tinyPNG(t, 5, 5)}, nil)
	if st != 201 {
		t.Fatalf("开关 off 建批应 201: %d %v", st, created)
	}
	batchID, _ := batchOf(created)["id"].(string)
	if batchOf(created)["status"] != "pending" {
		t.Fatalf("建批应 pending: %v", batchOf(created))
	}
	st, submitted := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	if st != 200 {
		t.Fatalf("提交应 200(如实落 blocked 终态): %d %v", st, submitted)
	}
	if batchOf(submitted)["status"] != "failed" || batchOf(submitted)["error_code"] != "generation_disabled" {
		t.Fatalf("批次应 failed+generation_disabled: %v", batchOf(submitted))
	}
	for i, it := range batchItems(t, submitted) {
		if it["status"] != "blocked" || it["error_code"] != "generation_disabled" {
			t.Fatalf("item[%d] 应 blocked(点名开关): %v", i, it)
		}
	}
	if ts.submits.Load() != 0 {
		t.Fatalf("开关 off 不得提交任何任务: %d", ts.submits.Load())
	}
	// 单工程提交端点仍 503(I0 语义不劣化)
	projID, _ := batchOf(created)["project_id"].(string)
	st, msg := f.do(t, "POST", "/api/v1/projects/"+projID+"/versions", tok, map[string]any{})
	if st != 503 || !strings.Contains(errMsg(msg), "FEATURE_GENERATION_ENABLED") {
		t.Fatalf("单工程提交仍应 503 点名开关: %d %v", st, msg)
	}
}

// 验收 5:同批次同输入 sha 去重成一项(幂等键 batch_id+input_sha256)。
func TestBatchInputShaDedupeWithinBatch(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, nil)
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	// 两个文件内容完全相同 + 一个不同 → 只成 2 项
	same := tinyPNG(t, 9, 9)
	st, created := f.createBatch(t, tok, [][]byte{same, same, tinyPNG(t, 12, 12)}, nil)
	if st != 201 {
		t.Fatalf("建批应 201: %d %v", st, created)
	}
	items := batchItems(t, created)
	if len(items) != 2 {
		t.Fatalf("同批次同内容应去重为 2 项: %d", len(items))
	}
	if got := countsOf(created); got["total"] != float64(2) {
		t.Fatalf("counts total 应 2: %v", got)
	}
	if dups, ok := created["duplicates"].([]any); !ok || len(dups) != 1 {
		t.Fatalf("应如实报告去重文件: %v", created["duplicates"])
	}
	// 同内容、不同批次 → 各自成项(幂等键含 batch_id)
	st, second := f.createBatch(t, tok, [][]byte{same}, map[string]any{"name": "第二批"})
	if st != 201 {
		t.Fatalf("异批次同内容应可建: %d %v", st, second)
	}
	if len(batchItems(t, second)) != 1 {
		t.Fatalf("新批次同内容应独立成项")
	}
}

// 验收 6:并发提交 ≤4(进程内信号量,超出排队)。
func TestBatchSubmitConcurrencyCappedAtFour(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, nil)
	ts.latency = 60 * time.Millisecond // 拉长在途窗口,让排队可观测
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	files := make([][]byte, 10)
	for i := range files {
		files[i] = tinyPNG(t, 4+i, 4)
	}
	_, created := f.createBatch(t, tok, files, nil)
	batchID, _ := batchOf(created)["id"].(string)
	st, submitted := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	if st != 200 {
		t.Fatalf("提交应 200: %d %v", st, submitted)
	}
	max := ts.maxInFlight.Load()
	if max > 4 {
		t.Fatalf("并发上限应 ≤4, 实测峰值 %d", max)
	}
	if max < 2 {
		t.Fatalf("并发应实际发生(峰值>1), 实测 %d", max)
	}
	if ts.submits.Load() != 10 {
		t.Fatalf("10 项应全部提交: %d", ts.submits.Load())
	}
}

// 验收 7:pending 批次取消 → items 全 cancelled;重复取消 409;已提交批次不可取消。
func TestBatchCancelPendingAndGuards(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, nil)
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)

	// pending 批次直接取消
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4), tinyPNG(t, 5, 5)}, nil)
	batchID, _ := batchOf(created)["id"].(string)
	st, cancelled := f.do(t, "POST", "/api/v1/batches/"+batchID+"/cancel", tok, nil)
	if st != 200 || batchOf(cancelled)["status"] != "cancelled" {
		t.Fatalf("pending 批次应可取消: %d %v", st, batchOf(cancelled)["status"])
	}
	for i, it := range batchItems(t, cancelled) {
		if it["status"] != "cancelled" || it["error_code"] != "cancelled" {
			t.Fatalf("item[%d] 应 cancelled: %v", i, it)
		}
	}
	// 重复取消 → 409;再提交 → 409
	if st, _ := f.do(t, "POST", "/api/v1/batches/"+batchID+"/cancel", tok, nil); st != 409 {
		t.Fatalf("重复取消应 409, got %d", st)
	}
	if st, _ := f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil); st != 409 {
		t.Fatalf("已取消批次提交应 409, got %d", st)
	}

	// 已提交(running)批次不可取消(已提交任务不撤单)
	_, created2 := f.createBatch(t, tok, [][]byte{tinyPNG(t, 7, 7)}, nil)
	batchID2, _ := batchOf(created2)["id"].(string)
	f.do(t, "POST", "/api/v1/batches/"+batchID2+"/submit", tok, nil)
	if st, msg := f.do(t, "POST", "/api/v1/batches/"+batchID2+"/cancel", tok, nil); st != 409 || errMsg(msg) == "" {
		t.Fatalf("running 批次取消应 409: %d %v", st, msg)
	}
}

// 验收 8:A/B 租户隔离:乙看不到也动不了甲的批次(与不存在同型 404)。
func TestBatchTenantIsolation(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, nil)
	f := batchFixture(t, us, ts)
	_, pairA := f.login(t, "jia@x.com", "right-pass")
	tokA, _ := pairA["access_token"].(string)
	_, pairB := f.login(t, "yi@x.com", "right-pass")
	tokB, _ := pairB["access_token"].(string)

	_, created := f.createBatch(t, tokA, [][]byte{tinyPNG(t, 4, 4)}, nil)
	batchID, _ := batchOf(created)["id"].(string)

	if st, list := f.do(t, "GET", "/api/v1/batches", tokB, nil); st != 200 {
		raws, _ := list["batches"].([]any)
		if st != 200 || len(raws) != 0 {
			t.Fatalf("乙的批次列表应为空: %d %v", st, list)
		}
	}
	for _, op := range []struct{ method, path string }{
		{"GET", "/api/v1/batches/" + batchID},
		{"POST", "/api/v1/batches/" + batchID + "/submit"},
		{"POST", "/api/v1/batches/" + batchID + "/collect"},
		{"POST", "/api/v1/batches/" + batchID + "/retry-failed"},
		{"POST", "/api/v1/batches/" + batchID + "/cancel"},
	} {
		if st, _ := f.do(t, op.method, op.path, tokB, nil); st != 404 {
			t.Fatalf("乙 %s %s 应 404(与不存在同型), got %d", op.method, op.path, st)
		}
	}
}

// 验收 9:sizeadapt off → finalize_with_variants 被忽略并如实标注 skipped。
func TestBatchVariantsSkippedWhenSizeAdaptOff(t *testing.T) {
	us := newBatchUploadStub(t)
	okPayload := tinyPNG(t, 10, 10)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		return "succeeded", map[string]any{"data_b64": base64.StdEncoding.EncodeToString(okPayload)}
	})
	// SizeAdaptEnabled 缺省 off;FEATURE_GENERATION_ENABLED off 与尺寸适配零耦合
	f := newFixture(t, func(c *config.Config) {
		c.GenerationEnabled = true
		c.UploadBaseURL = us.srv.URL
		c.TaskBaseURL = ts.srv.URL
	})
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4)}, map[string]any{
		"presets":                []string{"taobao_main"},
		"finalize_with_variants": true,
	})
	batchID, _ := batchOf(created)["id"].(string)
	f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	st, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 || batchOf(collected)["status"] != "completed" {
		t.Fatalf("sizeadapt off 不影响批次完成: %d %v", st, batchOf(collected)["status"])
	}
	it := batchItems(t, collected)[0]
	if it["status"] != "succeeded" || it["output_id"] == "" {
		t.Fatalf("item 应 succeeded(生成与登记是事实): %v", it)
	}
	if ids, _ := it["size_variant_ids"].([]any); len(ids) != 0 {
		t.Fatalf("变体应未生成: %v", it["size_variant_ids"])
	}
	if note, _ := it["variants_note"].(string); !strings.Contains(note, "skipped") {
		t.Fatalf("应如实标注 skipped: %v", it["variants_note"])
	}
}

// 验收 10:成果登记服务故障 → item blocked(不伪造成功);修复后 collect 补登记成功。
func TestBatchCollectBlocksWhenOutputRegisterFailsThenRecovers(t *testing.T) {
	us := newBatchUploadStub(t)
	okPayload := tinyPNG(t, 10, 10)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		return "succeeded", map[string]any{"data_b64": base64.StdEncoding.EncodeToString(okPayload)}
	})
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4)}, nil)
	batchID, _ := batchOf(created)["id"].(string)
	f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	// 任务已成功,但成果登记服务故障
	us.fail.Store(true)
	st, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 {
		t.Fatalf("收集应 200: %d %v", st, collected)
	}
	it := batchItems(t, collected)[0]
	if it["status"] != "blocked" || it["error_code"] != "output_register_failed" {
		t.Fatalf("登记故障应 blocked: %v", it)
	}
	if it["task_ref"] == "" {
		t.Fatalf("blocked 项应保留 task_ref(供 collect 重试登记): %v", it)
	}
	// 修复登记服务 → 再次 collect 补登记 → succeeded
	us.fail.Store(false)
	_, done := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	it = batchItems(t, done)[0]
	if it["status"] != "succeeded" || it["output_id"] == "" {
		t.Fatalf("恢复后应 succeeded: %v", it)
	}
	if batchOf(done)["status"] != "completed" {
		t.Fatalf("批次应 completed: %v", batchOf(done)["status"])
	}
}

// 验收 11:任务成功但成果不合法(非 PNG)→ item failed(invalid_result),不伪造。
func TestBatchInvalidTaskResultFailsItem(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		return "succeeded", map[string]any{"data_b64": base64.StdEncoding.EncodeToString([]byte("not a png at all"))}
	})
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4)}, nil)
	batchID, _ := batchOf(created)["id"].(string)
	f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	st, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if st != 200 {
		t.Fatalf("收集应 200: %d %v", st, collected)
	}
	it := batchItems(t, collected)[0]
	if it["status"] != "failed" || it["error_code"] != "invalid_result" {
		t.Fatalf("不合法成果应 failed(invalid_result)不伪造: %v", it)
	}
	if batchOf(collected)["status"] != "failed" {
		t.Fatalf("批次应如实 failed: %v", batchOf(collected)["status"])
	}
}

// 验收 12:建批输入门:非 PNG / 超量文件 / 空文件 / 超 8MiB → 400/413;upload 不可用 503。
func TestBatchCreateInputGates(t *testing.T) {
	us := newBatchUploadStub(t)
	ts := newTaskStub(t, nil)
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)

	// 空 items
	if st, _ := f.do(t, "POST", "/api/v1/batches", tok, map[string]any{"name": "X", "items": []any{}}); st != 400 {
		t.Fatalf("空 items 应 400, got %d", st)
	}
	// 缺名称
	if st, _ := f.do(t, "POST", "/api/v1/batches", tok, map[string]any{"items": batchFilesPayload([][]byte{tinyPNG(t, 4, 4)})}); st != 400 {
		t.Fatalf("缺名称应 400, got %d", st)
	}
	// 非 PNG 内容(魔数不过)
	if st, _ := f.createBatch(t, tok, [][]byte{[]byte("GIF89a-not-png")}, nil); st != 400 {
		t.Fatalf("非 PNG 应 400, got %d", st)
	}
	// 损坏 PNG(有魔数但解码失败)
	bad := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("garbage")...)
	if st, _ := f.createBatch(t, tok, [][]byte{bad}, nil); st != 400 {
		t.Fatalf("损坏 PNG 应 400, got %d", st)
	}
	// 超过 8 MiB(输入门长度先判,ErrTooLarge → 413;与 sizeadapt 同口径)
	huge := make([]byte, 8<<20+1)
	copy(huge, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	if st, _ := f.createBatch(t, tok, [][]byte{huge}, nil); st != 413 {
		t.Fatalf("超 8MiB 应 413, got %d", st)
	}
	// upload 不可用 → 503 fail-closed,批次不建
	f3 := newFixture(t, func(c *config.Config) { c.GenerationEnabled = true }) // upload 指向 dead
	_, pair3 := f3.login(t, "a@x.com", "right-pass")
	tok3, _ := pair3["access_token"].(string)
	if st, msg := f3.createBatch(t, tok3, [][]byte{tinyPNG(t, 4, 4)}, nil); st != 503 || errMsg(msg) == "" {
		t.Fatalf("upload 不可用建批应 503: %d %v", st, msg)
	}
}

// 验收 13:重试 failed 时存在 blocked 项 → 附「先修配置」提示且不动 blocked 项。
func TestBatchRetryFailedHintsBlockedNotTouched(t *testing.T) {
	okPayload := tinyPNG(t, 10, 10)
	us := newBatchUploadStub(t)
	// task_1 成功(登记会失败→blocked),task_2 失败(task_failed)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		if n == 1 {
			return "succeeded", map[string]any{"data_b64": base64.StdEncoding.EncodeToString(okPayload)}
		}
		return "failed", map[string]any{"error_code": "task_failed"}
	})
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4), tinyPNG(t, 5, 5)}, nil)
	batchID, _ := batchOf(created)["id"].(string)
	// 登记故障:task_1 成果登记失败 → blocked;task_2 任务失败 → failed
	us.fail.Store(true)
	f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	_, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	var blockedID, failedID string
	for _, it := range batchItems(t, collected) {
		switch it["status"] {
		case "blocked":
			blockedID, _ = it["id"].(string)
		case "failed":
			failedID, _ = it["id"].(string)
		}
	}
	if blockedID == "" || failedID == "" {
		t.Fatalf("应形成 1 blocked + 1 failed: %v", batchItems(t, collected))
	}
	// 修复登记服务;重试 failed 项:新 task 引用;blocked 项不动并附提示
	us.fail.Store(false)
	st, retried := f.do(t, "POST", "/api/v1/batches/"+batchID+"/retry-failed", tok, nil)
	if st != 200 {
		t.Fatalf("重试应 200: %d %v", st, retried)
	}
	if note, _ := retried["note"].(string); !strings.Contains(note, "blocked") {
		t.Fatalf("应附 blocked 先修配置提示: %v", retried["note"])
	}
	for _, it := range batchItems(t, retried) {
		switch it["id"] {
		case blockedID:
			if it["status"] != "blocked" {
				t.Fatalf("blocked 项不得被盲目重试: %v", it)
			}
		case failedID:
			if it["status"] != "submitted" {
				t.Fatalf("failed 项应重试为 submitted: %v", it)
			}
		}
	}
}

// 验收 14:重试 failed 时生成门不过 → 503 点名开关(提示先修配置)。
func TestBatchRetryFailedGateOffIs503(t *testing.T) {
	us := newBatchUploadStub(t)
	okPayload := tinyPNG(t, 10, 10)
	ts := newTaskStub(t, func(n int64) (string, map[string]any) {
		if n == 1 {
			return "failed", map[string]any{"error_code": "task_failed"}
		}
		return "succeeded", map[string]any{"data_b64": base64.StdEncoding.EncodeToString(okPayload)}
	})
	f := batchFixture(t, us, ts)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	_, created := f.createBatch(t, tok, [][]byte{tinyPNG(t, 4, 4), tinyPNG(t, 5, 5)}, nil)
	batchID, _ := batchOf(created)["id"].(string)
	f.do(t, "POST", "/api/v1/batches/"+batchID+"/submit", tok, nil)
	_, collected := f.do(t, "POST", "/api/v1/batches/"+batchID+"/collect", tok, nil)
	if batchOf(collected)["status"] != "completed_with_failures" {
		t.Fatalf("前置:应 completed_with_failures: %v", batchOf(collected)["status"])
	}
	// 关掉生成开关再重试 → 503 点名开关
	f.srv.Cfg.GenerationEnabled = false
	if st, msg := f.do(t, "POST", "/api/v1/batches/"+batchID+"/retry-failed", tok, nil); st != 503 || !strings.Contains(errMsg(msg), "FEATURE_GENERATION_ENABLED") {
		t.Fatalf("门不过重试应 503 点名开关: %d %v", st, msg)
	}
}



