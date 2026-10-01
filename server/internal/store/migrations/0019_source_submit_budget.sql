-- Persist the first consumer request budget, not proof of remote completion.
-- Historical attempted rows retain unknown zero pair and are lookup-only.
ALTER TABLE product_source_runs ADD COLUMN submit_started_at INTEGER NOT NULL DEFAULT 0 CHECK(typeof(submit_started_at)='integer' AND submit_started_at>=0);
ALTER TABLE product_source_runs ADD COLUMN submit_budget_seconds INTEGER NOT NULL DEFAULT 0 CHECK(typeof(submit_budget_seconds)='integer' AND submit_budget_seconds IN(0,20));
CREATE TRIGGER product_source_submit_budget_insert BEFORE INSERT ON product_source_runs
WHEN NOT ((NEW.submit_started_at=0 AND NEW.submit_budget_seconds=0)
 OR (NEW.submit_started_at>0 AND NEW.submit_started_at<=9223372036854775787 AND NEW.submit_budget_seconds=20 AND NEW.submit_attempted=1))
BEGIN SELECT RAISE(ABORT,'invalid source submit budget'); END;
CREATE TRIGGER product_source_submit_budget_immutable BEFORE UPDATE ON product_source_runs
WHEN NOT ((NEW.submit_started_at=0 AND NEW.submit_budget_seconds=0)
 OR (NEW.submit_started_at>0 AND NEW.submit_started_at<=9223372036854775787 AND NEW.submit_budget_seconds=20 AND NEW.submit_attempted=1))
 OR (OLD.submit_started_at>0 AND (NEW.submit_started_at!=OLD.submit_started_at OR NEW.submit_budget_seconds!=OLD.submit_budget_seconds))
 OR (OLD.submit_attempted=1 AND OLD.submit_started_at=0 AND NEW.submit_started_at>0)
BEGIN SELECT RAISE(ABORT,'immutable source submit budget'); END;
