-- 0004_batch.sql — 批量生成批次编排(HUI-1704 / FEAT-0205)
-- 全部 additive 新表;每行带 tenant_id 作用域(跨租户不可见是 store 包硬不变式)。
-- 本票是编排层:生成语义沿 I0 task 链路,变体复用 1703 幂等,本表只记批次事实。

PRAGMA foreign_keys = ON;

-- 批次:一批=一个制作工程(project_id),多文件=工程多输入。
-- status 拍板枚举:pending|submitting|running|completing|completed|completed_with_failures|failed|cancelled
-- (「批次=blocked 如实」由 failed+error_code 落地,不扩枚举;error_code 记批次级失败原因)。
CREATE TABLE IF NOT EXISTS batches (
    id             TEXT PRIMARY KEY,   -- batch_<hex>
    tenant_id      TEXT NOT NULL,
    created_by     TEXT NOT NULL,      -- 平台 principal(user_id);取消批次=owner 判定依据
    project_id     TEXT NOT NULL REFERENCES projects(id),
    name           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending',
    preset_summary TEXT NOT NULL DEFAULT '{}', -- JSON:所选场景×尺寸意图 {scenes,presets,usage_note,finalize_with_variants}
    error_code     TEXT NOT NULL DEFAULT '',   -- 批次级失败原因(如 generation_disabled/task_unavailable)
    created_at     TEXT NOT NULL,              -- RFC3339 UTC
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_batches_tenant_updated ON batches(tenant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_batches_project ON batches(project_id);

-- 批次项:一个输入文件一条。幂等键(拍板)= (batch_id, input_sha256) 唯一。
-- status 拍板枚举:pending|submitted|succeeded|failed|blocked|cancelled
CREATE TABLE IF NOT EXISTS batch_items (
    id               TEXT PRIMARY KEY,  -- bitem_<hex>
    batch_id         TEXT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    tenant_id        TEXT NOT NULL,
    project_id       TEXT NOT NULL REFERENCES projects(id),
    input_ref        TEXT NOT NULL DEFAULT '',  -- project_inputs.id
    input_sha256     TEXT NOT NULL,             -- 输入内容指纹(幂等键组成部分)
    input_name       TEXT NOT NULL DEFAULT '',
    input_size       INTEGER NOT NULL DEFAULT 0,
    task_ref         TEXT NOT NULL DEFAULT '',  -- platform task id(可空:未提交;重试=新引用)
    status           TEXT NOT NULL DEFAULT 'pending',
    error_code       TEXT NOT NULL DEFAULT '',  -- blocked/failed 原因,如 task_unavailable/invalid_result
    output_id        TEXT NOT NULL DEFAULT '',  -- collect 成功后经既有 outputs 路径登记的成果
    size_variant_ids TEXT NOT NULL DEFAULT '',  -- JSON 数组(kind=size_variant 的 project_outputs.id)或空
    variants_note    TEXT NOT NULL DEFAULT '',  -- 变体结果如实标注(如 skipped: FEATURE_SIZE_ADAPT off)
    attempts         INTEGER NOT NULL DEFAULT 0,-- 提交次数(task 幂等键组成部分:重试递增→新 task)
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    UNIQUE (batch_id, input_sha256)
);
CREATE INDEX IF NOT EXISTS idx_batch_items_batch ON batch_items(batch_id);
