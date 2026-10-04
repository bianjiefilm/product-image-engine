-- HUI-2746 motion reference. A row is a declaration, not a rendered ad.
-- Price and color updates may change parameters_json only. The subject stays put.
-- HUI-2732 is unfinished, so this table has no batch-rerun execution ledger.

CREATE TABLE motion_product_refs (
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64),
    revision_id TEXT NOT NULL,
    digest TEXT NOT NULL CHECK (length(digest) = 71 AND substr(digest, 1, 7) = 'sha256:'),
    subject_sha256 TEXT NOT NULL CHECK (subject_sha256 = content_sha256),
    parameters_json TEXT NOT NULL CHECK (json_valid(parameters_json)),
    product_regeneration INTEGER NOT NULL CHECK (product_regeneration = 0),
    model_call_count INTEGER NOT NULL CHECK (model_call_count = 0),
    video_generated INTEGER NOT NULL CHECK (video_generated = 0),
    supplier_called INTEGER NOT NULL CHECK (supplier_called = 0),
    verified INTEGER NOT NULL CHECK (verified = 0),
    dynamic_ad INTEGER NOT NULL CHECK (dynamic_ad = 0),
    finished_copy TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, project_id, id),
    UNIQUE (tenant_id, project_id, revision_id)
);

CREATE TRIGGER motion_product_refs_lock_subject
BEFORE UPDATE ON motion_product_refs
WHEN NEW.asset_id != OLD.asset_id
  OR NEW.content_sha256 != OLD.content_sha256
  OR NEW.revision_id != OLD.revision_id
  OR NEW.digest != OLD.digest
  OR NEW.subject_sha256 != OLD.subject_sha256
  OR NEW.product_regeneration != 0
  OR NEW.model_call_count != 0
  OR NEW.video_generated != 0
  OR NEW.supplier_called != 0
  OR NEW.verified != 0
  OR NEW.dynamic_ad != 0
  OR NEW.finished_copy != OLD.finished_copy
  OR NEW.id != OLD.id
  OR NEW.tenant_id != OLD.tenant_id
  OR NEW.project_id != OLD.project_id
BEGIN
  SELECT RAISE(ABORT, 'motion reference subject is immutable');
END;
