-- 0008_light_scene.sql — HUI-1700 光影场景逻辑任务。
-- 一行是一次报价/执行。同租户指纹唯一,避免重复生成。
-- 本表不记录资金金额;没有平台报价时接口只返回「计费待确认」。
-- verified_product 默认 0。填好的输出编号不能写成已验证产品图。

CREATE TABLE IF NOT EXISTS light_scene_jobs (
    id                 TEXT PRIMARY KEY,
    tenant_id          TEXT NOT NULL,
    project_id         TEXT NOT NULL,
    input_id           TEXT NOT NULL,
    input_version      TEXT NOT NULL,
    mode               TEXT NOT NULL,
    protected_region   TEXT NOT NULL,
    lighting_intent    TEXT NOT NULL DEFAULT '',
    fingerprint        TEXT NOT NULL,
    quote_status       TEXT NOT NULL,
    billing_label      TEXT NOT NULL,
    job_status         TEXT NOT NULL,
    quality            TEXT NOT NULL DEFAULT 'unknown',
    evidence           TEXT NOT NULL DEFAULT 'none',
    platform_task_id   TEXT NOT NULL DEFAULT '',
    output_asset_id    TEXT NOT NULL DEFAULT '',
    output_version     TEXT NOT NULL DEFAULT '',
    vendor_receipt_id  TEXT NOT NULL DEFAULT '',
    selection          TEXT NOT NULL DEFAULT '',
    export_count       INTEGER NOT NULL DEFAULT 0,
    deliverable        INTEGER NOT NULL DEFAULT 0,
    verified_product   INTEGER NOT NULL DEFAULT 0,
    pending_json       TEXT NOT NULL DEFAULT '[]',
    allowed_uses_json  TEXT NOT NULL DEFAULT '[]',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    UNIQUE (tenant_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS idx_light_jobs_project ON light_scene_jobs(tenant_id, project_id, updated_at DESC);
