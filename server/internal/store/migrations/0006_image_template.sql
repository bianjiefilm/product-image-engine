-- 0006_image_template.sql — 产品图模板库(HUI-1705 / FEAT-0206)
-- 全部 additive;模板=确定性预设+参数引用集,库内只存「引用与纯数值」,
-- 零生成语义字段(校验在 imagetmpl 包,含生成参数字段即拒)。
-- 套用=登记制:留痕行 + 工程行参数引用写点;同 (租户,工程,模板,版本) 幂等唯一。

PRAGMA foreign_keys = ON;

-- 租户自定义模板(内置模板是只读代码种子,见 imagetmpl/builtins.json,不入库)。
CREATE TABLE IF NOT EXISTS image_templates (
    id           TEXT PRIMARY KEY,   -- tpl_<hex>
    tenant_scope TEXT NOT NULL,
    name         TEXT NOT NULL,
    industry     TEXT NOT NULL DEFAULT '',  -- beauty|electronics_3c|apparel|food|''
    label        TEXT NOT NULL DEFAULT '',
    version      INTEGER NOT NULL DEFAULT 1, -- 每次更新 +1(套用留痕按版本钉死)
    params_json  TEXT NOT NULL,      -- 已过 imagetmpl 校验的参数引用集 JSON
    created_by   TEXT NOT NULL,      -- 平台 principal(user_id)
    created_at   TEXT NOT NULL,      -- RFC3339 UTC
    updated_at   TEXT NOT NULL,
    UNIQUE (tenant_scope, name)
);
CREATE INDEX IF NOT EXISTS idx_image_templates_tenant ON image_templates(tenant_scope, updated_at DESC);

-- 套用留痕:哪个模板哪版本套到哪个工程、何时、谁。唯一键即幂等键:
-- 同工程重复套用同模板同版本 → 冲突即幂等命中,不重复写。
CREATE TABLE IF NOT EXISTS image_template_applies (
    id               TEXT PRIMARY KEY,  -- apply_<hex>
    tenant_scope     TEXT NOT NULL,
    project_id       TEXT NOT NULL REFERENCES projects(id),
    template_id      TEXT NOT NULL,     -- tplbi_*(内置)或 tpl_*(自定义)
    template_version INTEGER NOT NULL,
    params_json      TEXT NOT NULL,     -- 套用时参数引用快照(只写一次)
    applied_by       TEXT NOT NULL,     -- 平台 principal(user_id)
    created_at       TEXT NOT NULL,
    UNIQUE (tenant_scope, project_id, template_id, template_version)
);
CREATE INDEX IF NOT EXISTS idx_image_template_applies_project ON image_template_applies(project_id, created_at);

-- 工程配置写点:当前生效的模板参数引用(登记制只写引用,零触发生成/零扣费,
-- 不改变工程既有产物)。写入只发生在模板套用端点,普通 PATCH 不开放。
ALTER TABLE projects ADD COLUMN applied_tpl_id TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN applied_tpl_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN applied_tpl_params TEXT NOT NULL DEFAULT '';
