-- 0014_text_image_fixture.sql — HUI-1701 服务端夹具字节。
-- 通用更新仍不能把失败写成成功。只有登记函数写入 completed 与 origin=fixture。
-- 下载时用文件字节再对一次哈希。本表不记录资金金额。

ALTER TABLE text_image_jobs ADD COLUMN input_version TEXT NOT NULL DEFAULT '';
ALTER TABLE text_image_jobs ADD COLUMN output_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE text_image_jobs ADD COLUMN result_version TEXT NOT NULL DEFAULT '';
ALTER TABLE text_image_jobs ADD COLUMN selected_version TEXT NOT NULL DEFAULT '';
ALTER TABLE text_image_jobs ADD COLUMN origin TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS text_image_blobs (
    asset_id      TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    project_id    TEXT NOT NULL,
    job_id        TEXT NOT NULL,
    input_version TEXT NOT NULL,
    sha256        TEXT NOT NULL,
    media_type    TEXT NOT NULL,
    origin        TEXT NOT NULL,
    content       BLOB NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_text_image_blobs_job ON text_image_blobs(tenant_id, project_id, job_id);
