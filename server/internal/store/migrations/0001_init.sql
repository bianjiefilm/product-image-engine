-- 0001_init.sql — 产品图领域模型(I0 基线)
-- 每行带 tenant 作用域:tenant 取自平台身份(JWT tenant_id,空则 account_id)。
-- principal 不在本库建用户表——身份事实永远属于 platform-identity。

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,            -- proj_<hex>
    tenant_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft', -- draft|active|archived
    usage_kind  TEXT NOT NULL DEFAULT '',      -- 用途(如 主图/详情页/广告)
    width_px    INTEGER NOT NULL DEFAULT 0,
    height_px   INTEGER NOT NULL DEFAULT 0,
    source_type TEXT NOT NULL DEFAULT '',      -- ''|standalone|order|campaign(无订单必须可用)
    source_ref  TEXT NOT NULL DEFAULT '',      -- 来源私有引用,本产品不解释其语义
    created_by  TEXT NOT NULL,                 -- 平台 principal(user_id, usr_…)
    created_at  TEXT NOT NULL,                 -- RFC3339 UTC
    updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projects_tenant_updated ON projects(tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS project_inputs (
    id                   TEXT PRIMARY KEY,   -- pin_<hex>
    project_id           TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tenant_id            TEXT NOT NULL,
    platform_asset_id    TEXT NOT NULL,      -- 引用平台 asset_id(本库不存文件)
    snapshot_name        TEXT NOT NULL DEFAULT '',
    snapshot_size        INTEGER NOT NULL DEFAULT 0,
    snapshot_content_type TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_project_inputs_project ON project_inputs(project_id);

CREATE TABLE IF NOT EXISTS project_versions (
    id                TEXT PRIMARY KEY,      -- ver_<hex>
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tenant_id         TEXT NOT NULL,
    version_no        INTEGER NOT NULL,
    platform_task_id  TEXT NOT NULL DEFAULT '',
    platform_asset_id TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending', -- pending|running|succeeded|failed
    created_at        TEXT NOT NULL,
    UNIQUE (project_id, version_no)
);
CREATE INDEX IF NOT EXISTS idx_project_versions_project ON project_versions(project_id, version_no DESC);
