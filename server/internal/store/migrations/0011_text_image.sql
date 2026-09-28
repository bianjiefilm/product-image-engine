-- 0011_text_image.sql — HUI-1701 文字描述生成入口。
-- 一行是一次文字请求。同租户指纹唯一，刷新仍是同一条记录。
-- 本表不存图片字节，也不记录资金金额。失败状态不能改成成功。

CREATE TABLE IF NOT EXISTS text_image_jobs (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    project_id            TEXT NOT NULL,
    prompt                TEXT NOT NULL,
    photo_asset_id        TEXT NOT NULL DEFAULT '',
    fingerprint           TEXT NOT NULL,
    quote_status          TEXT NOT NULL,
    billing_label         TEXT NOT NULL,
    job_status            TEXT NOT NULL,
    quality               TEXT NOT NULL DEFAULT 'unknown',
    platform_task_id      TEXT NOT NULL DEFAULT '',
    output_asset_id       TEXT NOT NULL DEFAULT '',
    charged               INTEGER NOT NULL DEFAULT 0,
    billing_passed        INTEGER NOT NULL DEFAULT 0,
    production_authorized INTEGER NOT NULL DEFAULT 0,
    subject_protected     INTEGER NOT NULL DEFAULT 0,
    pending_json          TEXT NOT NULL DEFAULT '[]',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    UNIQUE (tenant_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS idx_text_image_jobs_project ON text_image_jobs(tenant_id, project_id, updated_at DESC);
