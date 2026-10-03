-- Additive. Binds a returned project output to the quality verdict of the plate
-- derivation it came from. Only a limited_candidate can ever be bound.
CREATE TABLE project_output_quality (
 output_id TEXT PRIMARY KEY REFERENCES project_outputs(id) ON DELETE RESTRICT,
 tenant_scope TEXT NOT NULL,
 project_id TEXT NOT NULL,
 run_id TEXT NOT NULL UNIQUE REFERENCES product_plate_derivations(run_id) ON DELETE RESTRICT,
 result_sha256 TEXT NOT NULL CHECK(length(result_sha256)=64),
 candidate_state TEXT NOT NULL CHECK(candidate_state='limited_candidate'),
 original_charge_id TEXT NOT NULL CHECK(original_charge_id!=''),
 scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),
 limits_json TEXT NOT NULL CHECK(json_valid(limits_json)),
 report_sha256 TEXT NOT NULL CHECK(length(report_sha256)=64),
 created_at INTEGER NOT NULL
);
CREATE INDEX project_output_quality_project ON project_output_quality(tenant_scope,project_id);
CREATE TRIGGER project_output_quality_immutable BEFORE UPDATE ON project_output_quality
BEGIN SELECT RAISE(ABORT,'output quality binding is immutable'); END;
CREATE TRIGGER project_output_quality_keep BEFORE DELETE ON project_output_quality
BEGIN SELECT RAISE(ABORT,'output quality binding cannot be deleted'); END;
