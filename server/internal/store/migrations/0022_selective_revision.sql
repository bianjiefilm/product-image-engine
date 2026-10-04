-- HUI-2596 selective revision. Versions are insert-only. The head pointer
-- is the only adoption state. Failed and unknown rows cannot move it.

CREATE TABLE revision_versions (
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    label TEXT NOT NULL,
    version_no INTEGER NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('candidate', 'failed', 'unknown')),
    brief_version TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64),
    asset_ref TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL DEFAULT '',
    incremental_cost_cents INTEGER NOT NULL,
    parent_label TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    locks_json TEXT NOT NULL CHECK (json_valid(locks_json)),
    region TEXT NOT NULL DEFAULT '',
    target_version TEXT NOT NULL DEFAULT '',
    downstream TEXT NOT NULL DEFAULT '',
    png BLOB NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, project_id, label),
    UNIQUE (tenant_id, project_id, version_no)
);

CREATE TRIGGER revision_versions_immutable BEFORE UPDATE ON revision_versions
BEGIN SELECT RAISE(ABORT, 'revision version is immutable'); END;

CREATE TABLE revision_heads (
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    adopted_label TEXT NOT NULL DEFAULT '',
    adopted_sha256 TEXT NOT NULL DEFAULT '',
    brief_version TEXT NOT NULL DEFAULT '',
    brief_json TEXT NOT NULL CHECK (json_valid(brief_json)),
    pending_brief_version TEXT NOT NULL DEFAULT '',
    pending_brief_json TEXT NOT NULL CHECK (json_valid(pending_brief_json)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, project_id)
);
