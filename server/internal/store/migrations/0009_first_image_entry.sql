-- HUI-1894 首次做产品图的入口会话。
-- 个人空间的 storage_tenant 是 personal/default:<account>，客户来源用组织租户。
-- 同租户同指纹只保留一条任务，刷新不另起一行。

CREATE TABLE IF NOT EXISTS entry_sessions (
    id                 TEXT PRIMARY KEY,
    account_id         TEXT NOT NULL,
    workspace_kind     TEXT NOT NULL,
    workspace_scope    TEXT NOT NULL,
    storage_tenant     TEXT NOT NULL,
    project_id         TEXT NOT NULL,
    source_type        TEXT NOT NULL DEFAULT '',
    source_ref         TEXT NOT NULL DEFAULT '',
    return_href        TEXT NOT NULL DEFAULT '',
    fingerprint        TEXT NOT NULL,
    status             TEXT NOT NULL,
    platform_task_id   TEXT NOT NULL DEFAULT '',
    output_asset_id    TEXT NOT NULL DEFAULT '',
    quote_label        TEXT NOT NULL DEFAULT '待确认',
    charged            INTEGER NOT NULL DEFAULT 0,
    degraded_reason    TEXT NOT NULL DEFAULT '',
    step_count         INTEGER NOT NULL DEFAULT 0,
    input_count        INTEGER NOT NULL DEFAULT 0,
    reupload_count     INTEGER NOT NULL DEFAULT 0,
    recovery_count     INTEGER NOT NULL DEFAULT 0,
    authorized_assets  TEXT NOT NULL DEFAULT '[]',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    UNIQUE (storage_tenant, fingerprint)
);
CREATE INDEX IF NOT EXISTS idx_entry_sessions_account_scope
    ON entry_sessions(account_id, workspace_scope, updated_at DESC);
