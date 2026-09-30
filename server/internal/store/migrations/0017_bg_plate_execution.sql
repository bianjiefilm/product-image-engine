-- Immutable producer request and recovery facts; no wallet or balance ledger.
CREATE TABLE bg_plate_executions (
 tenant_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 job_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 request_json TEXT NOT NULL,
 state TEXT NOT NULL,
 task_id TEXT NOT NULL DEFAULT '',
 hold_id TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL,
 PRIMARY KEY (tenant_id, project_id, job_id)
);
