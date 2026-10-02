-- Additive. Frozen input and write-once derived output of background_plate_lock
-- runs. Old runs, tables and rows are untouched; rolling back means turning the
-- feature flag off, never downgrading the binary once such rows exist.
CREATE TABLE product_plate_inputs (
 run_id TEXT PRIMARY KEY REFERENCES product_source_runs(id) ON DELETE RESTRICT,
 frozen_json TEXT NOT NULL CHECK(json_valid(frozen_json)),
 digest TEXT NOT NULL CHECK(length(digest)=64),
 created_at INTEGER NOT NULL
);
CREATE TRIGGER product_plate_inputs_immutable BEFORE UPDATE ON product_plate_inputs
BEGIN SELECT RAISE(ABORT,'immutable plate input'); END;
CREATE TRIGGER product_plate_inputs_keep BEFORE DELETE ON product_plate_inputs
BEGIN SELECT RAISE(ABORT,'plate input cannot be deleted'); END;
CREATE TABLE product_plate_derivations (
 run_id TEXT PRIMARY KEY REFERENCES product_plate_inputs(run_id) ON DELETE RESTRICT,
 state TEXT NOT NULL CHECK(state IN('derived','blocked')),
 blocked_reason TEXT NOT NULL DEFAULT '',
 derived_png BLOB,
 derived_sha256 TEXT NOT NULL DEFAULT '',
 size_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(size_bytes)='integer' AND size_bytes>=0),
 width INTEGER NOT NULL DEFAULT 0 CHECK(typeof(width)='integer' AND width>=0),
 height INTEGER NOT NULL DEFAULT 0 CHECK(typeof(height)='integer' AND height>=0),
 report_json TEXT NOT NULL DEFAULT '',
 report_sha256 TEXT NOT NULL DEFAULT '',
 candidate_state TEXT NOT NULL CHECK(candidate_state IN('limited_candidate','pending','not_usable_candidate','blocked')),
 created_at INTEGER NOT NULL
);
CREATE TRIGGER product_plate_derivations_immutable BEFORE UPDATE ON product_plate_derivations
BEGIN SELECT RAISE(ABORT,'derivation is write-once'); END;
CREATE TRIGGER product_plate_derivations_keep BEFORE DELETE ON product_plate_derivations
BEGIN SELECT RAISE(ABORT,'derivation cannot be deleted'); END;
