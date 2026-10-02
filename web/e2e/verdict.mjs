// Computes the layer table and the verdict the evidence supports. It cannot raise a
// verdict; it can only refuse to. Layers are never merged, and each one is derived
// from the real run record's own fields, not from the mere existence of a record.
// DONE_WITH_SIMULATION needs EVERY required layer to say PASS; anything else is PARTIAL.
import fs from "node:fs";
import path from "node:path";
import { readLedger, ledgerProblems } from "./ledger.mjs";
import { validateReview } from "./review-validate.mjs";

const read = (f) => (fs.existsSync(f) ? JSON.parse(fs.readFileSync(f, "utf8")) : null);
const isPass = (v) => typeof v === "string" && v.startsWith("PASS");
// The four zero-cost recovery probes. The last one reuses the SAME stored login state in a
// fresh browser context; it is NOT a sign-out / sign-in and must never be reported as one.
export const PROBES = ["confirm_again_with_the_original_quote", "reconcile", "reload_page", "new_context_same_login_state"];
const REQUIRED = ["engineering", "browser_real", "fidelity", "generation", "export", "billing_recovery", "cross_app"];

export function assess(dir) {
  const gates = read(path.join(dir, "gates.json")) ?? {};
  const real = read(path.join(dir, "real-e2e", "run-record.json"));
  const blocked = read(path.join(dir, "real-e2e", "BLOCKED.json"));
  const fixture = read(path.join(dir, "browser", "fixture", "fixture-run-record.json"));
  const review = read(path.join(dir, "review.simulated_human.v1.json"));
  const crossRec = read(path.join(dir, "cross-app", "return-record.json"));
  const crossBlocked = read(path.join(dir, "cross-app", "BLOCKED.json"));
  const entries = readLedger(path.join(dir, "real-e2e", "charge-ledger.jsonl"));
  const gateNames = ["go_test", "go_vet", "go_race", "web_test", "web_tsc", "next_build", "samplegen_check"];
  const missingGates = gateNames.filter((g) => gates[g] !== 0);
  const problems = ledgerProblems(entries);
  const ledgerOk = problems.length === 0 && entries.length > 0;
  const reviewProblems = review ? validateReview(review) : null;

  const runs = Array.isArray(real?.runs) ? real.runs : [];
  const isReal = !!real && real.layer === "real" && runs.length >= 3 && runs.every((r) => r?.layer === "real" && !r.skipped);
  const buildOk = isReal && real.tree_dirty === false && real.backend_build?.vcs_modified === false && !!real.backend_build?.vcs_revision && real.backend_build.vcs_revision === real.frontend_git_sha;
  const observed = isReal && runs.every((r) => r.outcome === "OBSERVED");
  const none = blocked ? "BLOCKED" : "NOT_RUN";

  const fidelity = !isReal ? none
    : !observed ? "UNKNOWN (a limited-fidelity run is not OBSERVED)"
    : runs.some((r) => r.candidate_state === "not_usable_candidate") ? `FAIL (${runs.filter((r) => r.candidate_state === "not_usable_candidate").length} not_usable_candidate)`
    : runs.every((r) => r.candidate_state === "limited_candidate") ? "PASS (limited_candidate, frozen synthetic samples only)"
    : "UNKNOWN (candidate_state missing)";

  const m1Ok = observed && runs.every((r) => r.task_id && r.plate?.asset_id);
  const text = real?.text_run;
  const generation = !isReal ? none
    : !m1Ok ? "UNKNOWN (an M1 run has no Task id or plate asset)"
    : !text ? "UNKNOWN (M2 text_generate not run)"
    : text.layer !== "real" || text.outcome !== "OBSERVED" || text.image_loaded !== true ? "UNKNOWN (M2 image not observed)"
    : "PASS (3 plates + 1 text image produced and loaded)";

  const limited = runs.filter((r) => r.candidate_state === "limited_candidate");
  const exportOk = (r) => r.selected === true && r.export?.image_sha_matches_view === true && r.export?.report_bound_to_image === true && r.export?.report_version === "product-source-quality/v2";
  const exportLayer = !isReal ? none
    : limited.length === 0 ? "NOT_RUN (no limited_candidate to export)"
    : limited.every(exportOk) ? `PASS (${limited.length} export(s) byte-identical to the displayed composite)`
    : "FAIL (an export differs from the displayed composite or lacks its report)";

  const probes = Array.isArray(real?.probes) ? real.probes : [];
  const billing = !isReal ? none
    : !ledgerOk ? "UNKNOWN (ledger incomplete)"
    : probes.some((p) => p.equal !== true) ? "FAIL (a recovery probe changed usage / quote / Task / charge / balance)"
    : PROBES.some((n) => !probes.some((p) => p.name === n)) ? "UNKNOWN (a recovery probe is missing)"
    : !runs.every((r) => r.payment_status === "charged" && r.charge_id) ? "UNKNOWN (a run is not charged exactly once)"
    : !runs.some((r) => r.fault?.dropped?.length > 0) ? "UNKNOWN (fault injection was not exercised)"
    : "PASS (supplier cost declared, not audited)";

  const cross = crossBlocked ? "BLOCKED"
    : !crossRec ? "NOT_RUN (no real order / Campaign return stack; A7 Level B)"
    : crossRec.layer === "real" && crossRec.status === "PASS" && crossRec.quality_binding_checked === true && crossRec.receipt_id ? "PASS (Level B)"
    : "UNKNOWN (return record incomplete)";

  const layers = {
    engineering: missingGates.length === 0 ? "PASS" : `FAIL (${missingGates.join(", ")} not 0)`,
    browser_fixture: fixture ? (fixture.results.every((r) => r.ok) ? "PASS (fixture only)" : "FAIL") : "NOT_RUN",
    browser_real: blocked ? "BLOCKED" : !isReal ? "NOT_RUN" : !buildOk ? "FAIL (build identity: dirty tree, modified build or revision mismatch)" : "PASS",
    fidelity, generation, export: exportLayer, billing_recovery: billing,
    service_provider: generation.startsWith("PASS") ? "PASS" : generation,
    cross_app: cross,
    production: "NOT_RUN (needs Root go)",
    human: review && reviewProblems.length === 0 ? "SIMULATED-HUMAN (advisory)" : "NOT_RUN",
  };
  const unmet = REQUIRED.filter((k) => !isPass(layers[k])).map((k) => `${k}: ${layers[k]}`);
  if (!(review && reviewProblems.length === 0)) unmet.push("human: no valid SIMULATED-HUMAN review");
  const verdict = unmet.length === 0 ? "DONE_WITH_SIMULATION" : "PARTIAL";
  // Always reported, never a reason to claim more: the org payment gate (E7) is out of this ticket's scope and still open.
  const open_items = ["organization_gate: E7 not completed (out of scope, default closed)", "human: NEEDS_REAL_HUMAN", "production: NOT_RUN (needs Root go)"];
  return { layers, verdict, unmet, open_items, notes: { missingGates, blocked: blocked?.reason ?? null, ledgerProblems: problems, reviewProblems } };
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  console.log(JSON.stringify(assess(process.argv[2]), null, 2));
}
