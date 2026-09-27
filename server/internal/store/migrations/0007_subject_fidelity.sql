CREATE TABLE subject_fidelity_reports (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    input_ref TEXT NOT NULL,
    input_version TEXT NOT NULL,
    output_ref TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    body_sha256 TEXT NOT NULL,
    verdict TEXT NOT NULL,
    exact_product INTEGER NOT NULL,
    document TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (tenant_id, project_id, input_ref, input_version, output_ref, mode, body_sha256)
);

CREATE INDEX idx_fidelity_project ON subject_fidelity_reports (tenant_id, project_id, created_at);
CREATE INDEX idx_fidelity_output ON subject_fidelity_reports (tenant_id, output_ref);
