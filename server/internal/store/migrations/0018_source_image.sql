-- Additive product request journal; no wallet, funds mutation or legacy rewrite.
CREATE TABLE product_source_runs (
 id TEXT PRIMARY KEY,
 owner TEXT NOT NULL CHECK(owner='product_source_v1'),
 app_id TEXT NOT NULL,user_id TEXT NOT NULL,tenant_id TEXT NOT NULL,project_id TEXT NOT NULL,
 payer_account_id TEXT NOT NULL,principal_account_id TEXT NOT NULL,
 request_key TEXT NOT NULL,fingerprint TEXT NOT NULL,
 intent_json TEXT NOT NULL CHECK(json_valid(intent_json)),
 usage_key TEXT NOT NULL UNIQUE,task_key TEXT NOT NULL UNIQUE,business_ref TEXT NOT NULL UNIQUE,
 usage_id TEXT UNIQUE,task_id TEXT UNIQUE,
 quote_json TEXT CHECK(quote_json IS NULL OR json_valid(quote_json)),
 confirmation_hash TEXT NOT NULL DEFAULT '',confirmed_at INTEGER NOT NULL DEFAULT 0 CHECK(confirmed_at>=0),
 phase TEXT NOT NULL DEFAULT 'intent',
 observation_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(observation_json)),
 events_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(events_json)),
 quality_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(quality_json)),
 revision INTEGER NOT NULL DEFAULT 0 CHECK(typeof(revision)='integer' AND revision>=0),
 lease_epoch INTEGER NOT NULL DEFAULT 0 CHECK(typeof(lease_epoch)='integer' AND lease_epoch>=0),
 lease_until INTEGER NOT NULL DEFAULT 0 CHECK(typeof(lease_until)='integer' AND lease_until>=0),
 retry_at INTEGER NOT NULL DEFAULT 0 CHECK(typeof(retry_at)='integer' AND retry_at>=0),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(typeof(attempts)='integer' AND attempts>=0),
 deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN(0,1)),selected INTEGER NOT NULL DEFAULT 0 CHECK(selected IN(0,1)),
 cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK(cancel_requested IN(0,1)),submit_attempted INTEGER NOT NULL DEFAULT 0 CHECK(submit_attempted IN(0,1)),
 created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,
 UNIQUE(app_id,user_id,project_id,request_key)
);
CREATE INDEX product_source_runs_due ON product_source_runs(retry_at,lease_until,id);
CREATE TRIGGER product_source_runs_immutable BEFORE UPDATE ON product_source_runs
WHEN NEW.id!=OLD.id OR NEW.owner!=OLD.owner OR NEW.app_id!=OLD.app_id OR NEW.user_id!=OLD.user_id
 OR NEW.tenant_id!=OLD.tenant_id OR NEW.project_id!=OLD.project_id OR NEW.payer_account_id!=OLD.payer_account_id
 OR NEW.principal_account_id!=OLD.principal_account_id OR NEW.request_key!=OLD.request_key OR NEW.fingerprint!=OLD.fingerprint
 OR NEW.intent_json!=OLD.intent_json OR NEW.usage_key!=OLD.usage_key OR NEW.task_key!=OLD.task_key
 OR NEW.business_ref!=OLD.business_ref OR NEW.created_at!=OLD.created_at
 OR (OLD.usage_id IS NOT NULL AND NEW.usage_id IS NOT OLD.usage_id)
 OR (OLD.task_id IS NOT NULL AND NEW.task_id IS NOT OLD.task_id)
 OR (OLD.quote_json IS NOT NULL AND NEW.quote_json IS NOT OLD.quote_json)
 OR (OLD.confirmation_hash!='' AND (NEW.confirmation_hash!=OLD.confirmation_hash OR NEW.confirmed_at!=OLD.confirmed_at))
 OR NEW.deleted<OLD.deleted OR NEW.cancel_requested<OLD.cancel_requested OR NEW.submit_attempted<OLD.submit_attempted
 OR NEW.lease_epoch<OLD.lease_epoch OR NEW.revision<OLD.revision
BEGIN SELECT RAISE(ABORT,'immutable source request'); END;
CREATE TABLE product_source_reference_outbox (
 id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES product_source_runs(id) ON DELETE RESTRICT,
 scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),output_json TEXT NOT NULL CHECK(json_valid(output_json)),
 app_id TEXT NOT NULL,user_id TEXT NOT NULL,asset_id TEXT NOT NULL,project_id TEXT NOT NULL,reference_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action='release'),reason TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(typeof(attempts)='integer' AND attempts>=0),
 retry_at INTEGER NOT NULL DEFAULT 0 CHECK(typeof(retry_at)='integer' AND retry_at>=0),
 lease_epoch INTEGER NOT NULL DEFAULT 0 CHECK(typeof(lease_epoch)='integer' AND lease_epoch>=0),
 lease_until INTEGER NOT NULL DEFAULT 0 CHECK(typeof(lease_until)='integer' AND lease_until>=0),
 last_error TEXT NOT NULL DEFAULT '',
 UNIQUE(app_id,user_id,asset_id,project_id,reference_id,action)
);
CREATE INDEX product_source_reference_outbox_due ON product_source_reference_outbox(state,retry_at,lease_until);
CREATE TRIGGER product_source_outbox_immutable BEFORE UPDATE ON product_source_reference_outbox
WHEN NEW.id!=OLD.id OR NEW.run_id!=OLD.run_id OR NEW.scope_json!=OLD.scope_json OR NEW.output_json!=OLD.output_json
 OR NEW.app_id!=OLD.app_id OR NEW.user_id!=OLD.user_id OR NEW.asset_id!=OLD.asset_id
 OR NEW.project_id!=OLD.project_id OR NEW.reference_id!=OLD.reference_id OR NEW.action!=OLD.action OR NEW.reason!=OLD.reason
 OR NEW.attempts<OLD.attempts OR NEW.lease_epoch<OLD.lease_epoch
 OR (OLD.state='done' AND NEW.state!='done')
BEGIN SELECT RAISE(ABORT,'immutable source reference'); END;
