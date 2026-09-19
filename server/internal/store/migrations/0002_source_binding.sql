-- 0002_source_binding.sql — 跨应用续接与成果回流(HUI-1745 I1)
-- 全部 additive 新表;每行带 tenant_scope 作用域。
-- 绑定唯一键 = tenant_scope+source_app+source_ref+target_app+purpose(票面原文逐字落实)。

PRAGMA foreign_keys = ON;

-- 来源稳定绑定:同五元组只绑定一个工程(不得仅凭 source_ref 或工程 ID 查绑定)。
CREATE TABLE IF NOT EXISTS source_bindings (
    id                  TEXT PRIMARY KEY,   -- bind_<hex>
    tenant_scope        TEXT NOT NULL,
    source_app          TEXT NOT NULL,
    source_ref          TEXT NOT NULL,      -- handoff.source_project_ref(来源私有定位)
    target_app          TEXT NOT NULL,      -- 本应用 app_id
    purpose             TEXT NOT NULL,      -- 制作 purpose(用途)
    project_id          TEXT NOT NULL REFERENCES projects(id),
    current_snapshot_id TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    UNIQUE (tenant_scope, source_app, source_ref, target_app, purpose)
);
CREATE INDEX IF NOT EXISTS idx_source_bindings_project ON source_bindings(project_id);

-- 交接不可变快照:同 handoff_id 同内容幂等(SNAPSHOT_SAME);异内容冲突保留原快照
-- (SNAPSHOT_CONFLICT);新 handoff_id 新快照(SNAPSHOT_NEW)。内容列只写一次。
CREATE TABLE IF NOT EXISTS handoff_snapshots (
    id              TEXT PRIMARY KEY,   -- snap_<hex>
    tenant_scope    TEXT NOT NULL,
    binding_id      TEXT REFERENCES source_bindings(id), -- 先建快照后建绑定:NULL 起步,绑定时补链
    handoff_id      TEXT NOT NULL,
    content_fp      TEXT NOT NULL,      -- 接受内容规范序列化 SHA-256
    source_kind     TEXT NOT NULL,      -- order|standalone|campaign
    source_app      TEXT NOT NULL,
    principal_id    TEXT NOT NULL,      -- 付款/绑定主体(平台 principal)
    source_revision TEXT NOT NULL,
    brief_version   TEXT NOT NULL,
    profile_json    TEXT NOT NULL,      -- 来源上下文快照(素材/授权范围/用途等,不可变)
    created_at      TEXT NOT NULL,
    UNIQUE (tenant_scope, handoff_id)
);
CREATE INDEX IF NOT EXISTS idx_handoff_snapshots_binding ON handoff_snapshots(binding_id);

-- 一次性兑换(应用内语义;平台 identity 真兑换归 HUI-656 域):
-- 同 (binding_ref, tenant_scope, content_fp) 重放 → SAME_RESULT;异 tuple → 冲突。
CREATE TABLE IF NOT EXISTS handoff_redemptions (
    binding_ref  TEXT NOT NULL,
    tenant_scope TEXT NOT NULL,
    content_fp   TEXT NOT NULL,
    snapshot_id  TEXT NOT NULL REFERENCES handoff_snapshots(id),
    created_at   TEXT NOT NULL,
    UNIQUE (binding_ref)
);

-- 用户明确选择的输出版本(登记产出;选择即登记,不触发任何生成/扣费)。
CREATE TABLE IF NOT EXISTS project_outputs (
    id                TEXT PRIMARY KEY,  -- out_<hex>
    tenant_scope      TEXT NOT NULL,
    project_id        TEXT NOT NULL REFERENCES projects(id),
    platform_asset_id TEXT NOT NULL,     -- 经平台 upload 设施登记的 asset_id
    result_sha256     TEXT NOT NULL,
    result_size       INTEGER NOT NULL,
    media_type        TEXT NOT NULL,
    file_name         TEXT NOT NULL DEFAULT '',
    snapshot_id       TEXT NOT NULL DEFAULT '',  -- 选择时的需求版本上下文
    brief_version     TEXT NOT NULL DEFAULT '',
    source_revision   TEXT NOT NULL DEFAULT '',
    platform_task_id  TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    UNIQUE (project_id, result_sha256)
);

-- 定向回执(order-receipt/v1):payload 不可变;状态机 pending→delivered|failed。
CREATE TABLE IF NOT EXISTS source_receipts (
    id           TEXT PRIMARY KEY,   -- rcpt_<hex>
    tenant_scope TEXT NOT NULL,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    output_id    TEXT NOT NULL REFERENCES project_outputs(id),
    event_id     TEXT NOT NULL,      -- 来源事件幂等键(对端 UNIQUE)
    handoff_id   TEXT NOT NULL,
    target_app   TEXT NOT NULL,
    run_id       TEXT NOT NULL DEFAULT '',
    sequence     INTEGER NOT NULL,
    payload      TEXT NOT NULL,      -- 回执文档原文(重传只重发此内容)
    payload_fp   TEXT NOT NULL,      -- SHA-256(payload)
    status       TEXT NOT NULL DEFAULT 'pending', -- pending|delivered|failed
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (tenant_scope, event_id)
);
CREATE INDEX IF NOT EXISTS idx_source_receipts_project ON source_receipts(project_id);
