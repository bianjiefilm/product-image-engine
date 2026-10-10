-- HUI-1745 campaign forward. Explicit selected version only.
-- Registering an output, opening a page, or downloading a file does not insert a row.
-- One pointer per tenant and project. Reselect moves the pointer; receipts stay immutable.

CREATE TABLE campaign_selected_versions (
    id              TEXT PRIMARY KEY,
    tenant_scope    TEXT NOT NULL,
    project_id      TEXT NOT NULL REFERENCES projects(id),
    output_id       TEXT NOT NULL REFERENCES project_outputs(id),
    snapshot_id     TEXT NOT NULL REFERENCES handoff_snapshots(id),
    campaign_ref    TEXT NOT NULL,
    brief_version   TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (tenant_scope, project_id)
);
