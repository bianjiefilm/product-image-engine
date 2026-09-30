-- 背景模型返回的可解码字节。
-- origin 固定为 model_http：不是供应商成功，也不是计费通过。deliverable 仍为 0。

ALTER TABLE bg_replace_jobs ADD COLUMN origin TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS bg_result_blobs (
    asset_id   TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    project_id TEXT NOT NULL,
    job_id     TEXT NOT NULL,
    sha256     TEXT NOT NULL,
    media_type TEXT NOT NULL,
    origin     TEXT NOT NULL,
    content    BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_bg_result_blobs_job ON bg_result_blobs(tenant_id, project_id, job_id);
