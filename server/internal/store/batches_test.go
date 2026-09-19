package store

import (
	"context"
	"errors"
	"testing"
)

// 批次编排仓储测试(HUI-1704 / FEAT-0205)。
// 硬不变式:租户隔离;幂等键 UNIQUE(batch_id, input_sha256);状态守卫更新。

func createBatchFixture(t *testing.T, s *Store, tenant, usr string) (Batch, []BatchItem) {
	t.Helper()
	ctx := context.Background()
	specs := []BatchItemSpec{
		{AssetID: "asset_1", Name: "a.png", Size: 100, SHA256: "aa01", ContentType: "image/png"},
		{AssetID: "asset_2", Name: "b.png", Size: 200, SHA256: "bb02", ContentType: "image/png"},
	}
	b, items, err := s.CreateBatch(ctx, Batch{
		TenantID: tenant, CreatedBy: usr, Name: "秋季批次",
		PresetSummary: `{"scenes":["白底主图"],"presets":[],"finalize_with_variants":false}`,
	}, specs)
	if err != nil {
		t.Fatalf("建批: %v", err)
	}
	return b, items
}

func TestCreateBatchAtomicProjectInputsAndItems(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	b, items := createBatchFixture(t, s, "acct_a", "usr_a1")

	if b.ID == "" || b.Status != "pending" || b.ProjectID == "" {
		t.Fatalf("批次默认值异常: %+v", b)
	}
	if len(items) != 2 {
		t.Fatalf("应 2 个 item, got %d", len(items))
	}
	for i, it := range items {
		if it.ID == "" || it.BatchID != b.ID || it.ProjectID != b.ProjectID ||
			it.Status != "pending" || it.TenantID != "acct_a" || it.InputRef == "" {
			t.Fatalf("item[%d] 异常: %+v", i, it)
		}
	}
	// 单事务落齐:工程 + inputs + batch + items
	if _, err := s.GetProject(ctx, "acct_a", b.ProjectID); err != nil {
		t.Fatalf("批次工程应已建: %v", err)
	}
	inputs, err := s.ListInputs(ctx, "acct_a", b.ProjectID)
	if err != nil || len(inputs) != 2 {
		t.Fatalf("工程输入应 2 条: %v %d", err, len(inputs))
	}
	// item.input_ref 与工程输入对得上
	if items[0].InputRef != inputs[0].ID && items[1].InputRef != inputs[0].ID {
		t.Fatalf("input_ref 未指向工程输入: %+v %+v", items, inputs)
	}
	// 读回
	got, its, err := s.GetBatchWithItems(ctx, "acct_a", b.ID)
	if err != nil || got.ID != b.ID || len(its) != 2 {
		t.Fatalf("读回批次: %v %+v %d", err, got, len(its))
	}
	list, err := s.ListBatches(ctx, "acct_a")
	if err != nil || len(list) != 1 {
		t.Fatalf("列批次: %v %d", err, len(list))
	}
}

func TestBatchTenantIsolation(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	b, _ := createBatchFixture(t, s, "acct_a", "usr_a1")

	if _, _, err := s.GetBatchWithItems(ctx, "acct_b", b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户读批应 ErrNotFound, got %v", err)
	}
	if lb, _ := s.ListBatches(ctx, "acct_b"); len(lb) != 0 {
		t.Fatalf("乙的批次列表应为空: %+v", lb)
	}
	it, err := s.UpdateBatchItemStatus(ctx, "acct_b", "nope", []string{"pending"}, "submitted", "", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户改 item 应 ErrNotFound, got %v %v", it, err)
	}
}

func TestBatchItemUniqueKeyBatchSha(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	b, items := createBatchFixture(t, s, "acct_a", "usr_a1")

	// 同批次同输入 sha 再插 → ErrConflict(幂等键拍板:batch_id+input_sha256)
	dup := BatchItem{BatchID: b.ID, TenantID: "acct_a", ProjectID: b.ProjectID,
		InputRef: items[0].InputRef, InputSHA256: "aa01", Status: "pending"}
	if _, err := s.InsertBatchItem(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("同批同 sha 应 ErrConflict, got %v", err)
	}
	// 同 sha 不同批次:允许(不同批次各自成项)
	b2, _, err := s.CreateBatch(ctx, Batch{TenantID: "acct_a", CreatedBy: "usr_a1", ProjectID: b.ProjectID, Name: "第二批"}, []BatchItemSpec{
		{AssetID: "asset_1", Name: "a.png", Size: 100, SHA256: "aa01", ContentType: "image/png"},
	})
	if err != nil || b2.ID == "" {
		t.Fatalf("异批次同内容应可建: %v", err)
	}
	if b2.ProjectID != b.ProjectID {
		t.Fatalf("显式给 project_id 时应复用工程: %v vs %v", b2.ProjectID, b.ProjectID)
	}
}

func TestBatchStatusGuards(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	b, items := createBatchFixture(t, s, "acct_a", "usr_a1")

	// 守卫更新:from 不匹配 → ErrConflict
	if _, err := s.UpdateBatchStatus(ctx, "acct_a", b.ID, []string{"running"}, "completed", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("状态不匹配应 ErrConflict, got %v", err)
	}
	// pending → submitting
	got, err := s.UpdateBatchStatus(ctx, "acct_a", b.ID, []string{"pending"}, "submitting", "")
	if err != nil || got.Status != "submitting" {
		t.Fatalf("推进状态: %v %+v", err, got)
	}
	// item: pending → submitted(带 task_ref)
	it, err := s.UpdateBatchItemStatus(ctx, "acct_a", items[0].ID, []string{"pending"}, "submitted", "task_9", "")
	if err != nil || it.Status != "submitted" || it.TaskRef != "task_9" {
		t.Fatalf("item 推进: %v %+v", err, it)
	}
	// 不匹配的 from → ErrConflict
	if _, err := s.UpdateBatchItemStatus(ctx, "acct_a", items[0].ID, []string{"pending"}, "succeeded", "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("item 守卫应 ErrConflict, got %v", err)
	}
	// 计数
	cnt, err := s.CountBatchItemsByStatus(ctx, b.ID)
	if err != nil || cnt["submitted"] != 1 || cnt["pending"] != 1 {
		t.Fatalf("计数: %v %+v", err, cnt)
	}
	// 更新 item 成果字段
	up, err := s.UpdateBatchItemResult(ctx, "acct_a", items[0].ID, ItemResultUpdate{
		Status: "succeeded", OutputID: "out_1", SizeVariantIDs: `["v1"]`, VariantsNote: "2 变体",
	})
	if err != nil || up.Status != "succeeded" || up.OutputID != "out_1" {
		t.Fatalf("写成果: %v %+v", err, up)
	}
	// attempts 原子自增
	n, err := s.IncrementBatchItemAttempt(ctx, "acct_a", items[0].ID)
	if err != nil || n != 1 {
		t.Fatalf("attempts 自增: %v %d", err, n)
	}
	if n, err = s.IncrementBatchItemAttempt(ctx, "acct_a", items[0].ID); err != nil || n != 2 {
		t.Fatalf("attempts 再自增: %v %d", err, n)
	}
}
