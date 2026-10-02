import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { readStoredZip, writeStoredZip } from "../e2e/zip.mjs";
import { ledgerProblems, openLedger, readLedger, spentMinor } from "../e2e/ledger.mjs";
import { validateReview } from "../e2e/review-validate.mjs";
import { assess } from "../e2e/verdict.mjs";

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), "hui2232-"));
const sha = "c".repeat(64);
const goodReview = () => ({ schema: "review.simulated_human.v1", kind: "SIMULATED-HUMAN", reviewer_context: "fresh", items: [{ case_id: "carton-1024x1024", composite_sha256: sha, axes: { background_extra_objects: { opinion: "cannot_tell" }, visual_composite_quality: { opinion: "looks_acceptable" } } }] });

describe("evidence helpers", () => {
  it("stored zip round trips every entry byte for byte", () => {
    const files = { "image.png": Buffer.from([137, 80, 78, 71, 0, 255]), "quality.json": Buffer.from('{"report_version":"product-source-quality/v2"}'), "snapshot.json": Buffer.from("{}") };
    const read = readStoredZip(writeStoredZip(files));
    expect([...read.keys()]).toEqual(Object.keys(files));
    for (const [name, data] of Object.entries(files)) expect(Buffer.compare(read.get(name)!, data)).toBe(0);
    expect(() => readStoredZip(Buffer.from("not a zip"))).toThrow();
  });

  it("the ledger refuses an observation without a prior intent and flags an unobserved intent as unknown", () => {
    const dir = tmp(), l = openLedger(path.join(dir, "ledger.jsonl"));
    l.observed("A", { payment_status: "charged", charged_minor: "25" });
    expect(ledgerProblems(readLedger(l.file)).join("|")).toContain("observation without a prior intent: A");
    const dir2 = tmp(), l2 = openLedger(path.join(dir2, "ledger.jsonl"));
    l2.intent("B", { expected_amount_minor: "25" });
    expect(ledgerProblems(readLedger(l2.file)).join("|")).toContain("intent never observed (outcome UNKNOWN): B");
    l2.observed("B", { payment_status: "charged", charged_minor: "25" });
    l2.intent("C", {}); l2.observed("C", { payment_status: "held", charged_minor: null });
    const entries = readLedger(l2.file);
    expect(ledgerProblems(entries)).toEqual([]);
    expect(spentMinor(entries)).toBe(25);
    expect(entries.every(e => e.phase === "intent" || e.supplier_expense_ledger_verified === false)).toBe(true);
  });

  it("a SIMULATED-HUMAN review is accepted only when it claims nothing more", () => {
    expect(validateReview(goodReview())).toEqual([]);
    const bad: Array<[string, (d: Record<string, unknown>) => void]> = [
      ["kind", d => { d.kind = "HUMAN"; }], ["context", d => { d.reviewer_context = "same"; }], ["empty items", d => { d.items = []; }],
      ["verdict field", d => { (d.items as Array<Record<string, unknown>>)[0].verdict = "PASS"; }],
      ["axis opinion", d => { (d.items as Array<{ axes: Record<string, unknown> }>)[0].axes.visual_composite_quality = { opinion: "PASS" }; }],
      ["real person claim", d => { d.note = "a real person confirmed this"; }],
    ];
    for (const [name, mutate] of bad) { const d = goodReview() as Record<string, unknown>; mutate(d); expect(validateReview(d).length, name).toBeGreaterThan(0); }
  });
});

// ---- the verdict derivation -------------------------------------------------
// A complete, honest real-stack record: three limited candidates through the UI, one
// of them through the fault proxy, four zero-cost probes, one ordinary text image,
// a clean build identity. Every counter-example below mutates ONE fact of it.
const PROBES = ["confirm_again_with_the_original_quote", "reconcile", "reload_page", "new_context_same_login_state"];
const REV = "f".repeat(40);
const GATES = { go_test: 0, go_vet: 0, go_race: 0, web_test: 0, web_tsc: 0, next_build: 0, samplegen_check: 0 };
const goodRun = (layer = "real") => ({
  layer, frontend_git_sha: REV, tree_dirty: false, backend_build: { vcs_revision: REV, vcs_modified: false },
  runs: [0, 1, 2].map((i) => ({
    label: `M1-case${i}`, layer, outcome: "OBSERVED", task_id: `t${i}`, candidate_state: "limited_candidate", payment_status: "charged", charge_id: `ch${i}`,
    plate: { asset_id: `a${i}` }, composite_sha256: sha, selected: true,
    export: { image_sha_matches_view: true, report_bound_to_image: true, report_version: "product-source-quality/v2" },
    fault: i === 1 ? { submits_seen: 2, dropped: [2] } : null,
  })),
  probes: PROBES.map((name) => ({ name, equal: true })),
  text_run: { label: "M2-text_generate", layer, outcome: "OBSERVED", image_loaded: true },
});
type Stage = { mutate?: (r: any) => void; ledger?: boolean; review?: boolean | "invalid"; cross?: "pass" | "blocked" | "incomplete" | "none"; gates?: Record<string, number>; layer?: string };
function stage(o: Stage = {}) {
  const dir = tmp();
  for (const d of ["real-e2e", "cross-app"]) fs.mkdirSync(path.join(dir, d), { recursive: true });
  const write = (f: string, v: unknown) => fs.writeFileSync(path.join(dir, f), JSON.stringify(v));
  write("gates.json", { ...GATES, ...o.gates });
  const run = goodRun(o.layer ?? "real"); o.mutate?.(run); write("real-e2e/run-record.json", run);
  if (o.ledger !== false) {
    const l = openLedger(path.join(dir, "real-e2e", "charge-ledger.jsonl"));
    for (const label of [...run.runs.map((r: { label: string }) => r.label), "M2-text_generate"]) { l.intent(label, {}); l.observed(label, { payment_status: "charged", charged_minor: "25" }); }
  }
  if (o.review !== false) { const r = goodReview(); if (o.review === "invalid") (r.items[0] as Record<string, unknown>).verdict = "PASS"; write("review.simulated_human.v1.json", r); }
  const cross = o.cross ?? "pass";
  if (cross === "pass") write("cross-app/return-record.json", { layer: "real", status: "PASS", quality_binding_checked: true, receipt_id: "rcpt_1" });
  if (cross === "incomplete") write("cross-app/return-record.json", { layer: "real", status: "PASS" });
  if (cross === "blocked") write("cross-app/BLOCKED.json", { reason: "no_order_stack" });
  return dir;
}
const layerOf = (dir: string, key: string) => (assess(dir).layers as Record<string, string>)[key];
const isPass = (v: string) => v.startsWith("PASS");

describe("the verdict never rises above what the evidence supports", () => {
  it("a complete honest record is the only way to DONE_WITH_SIMULATION, and production / org gate stay open", () => {
    const out = assess(stage());
    expect(out.verdict).toBe("DONE_WITH_SIMULATION");
    for (const k of ["engineering", "browser_real", "fidelity", "generation", "export", "billing_recovery", "cross_app"]) expect(isPass((out.layers as Record<string, string>)[k]), k).toBe(true);
    expect(out.layers.production).toContain("NOT_RUN");
    expect(out.layers.human).toContain("SIMULATED-HUMAN");
    expect(out.layers.billing_recovery).toContain("not audited");
    expect(out.open_items.join("|")).toMatch(/organization_gate/);
  });

  it("the minimal record the old verdict accepted (only task_id and plate.asset_id) is PARTIAL", () => {
    const dir = stage({ mutate: (r) => { r.runs = r.runs.map((x: any) => ({ layer: x.layer, task_id: x.task_id, plate: x.plate, label: x.label })); delete r.probes; delete r.text_run; } });
    expect(assess(dir).verdict).toBe("PARTIAL");
  });

  const cases: Array<[string, Stage, string, RegExp]> = [
    ["a not_usable_candidate run", { mutate: (r) => { r.runs[1].candidate_state = "not_usable_candidate"; r.runs[1].export = null; r.runs[1].selected = false; } }, "fidelity", /^FAIL/],
    ["a run whose candidate_state is missing", { mutate: (r) => { delete r.runs[2].candidate_state; } }, "fidelity", /^UNKNOWN/],
    ["a run that is not OBSERVED (timed out)", { mutate: (r) => { r.runs[0].outcome = "UNKNOWN"; } }, "fidelity", /^UNKNOWN/],
    ["a probe whose before/after differ", { mutate: (r) => { r.probes[2].equal = false; } }, "billing_recovery", /^FAIL/],
    ["a missing probe", { mutate: (r) => { r.probes.pop(); } }, "billing_recovery", /^UNKNOWN/],
    ["no fault injection exercised", { mutate: (r) => { r.runs[1].fault = null; } }, "billing_recovery", /^UNKNOWN/],
    ["a run that is not charged exactly once", { mutate: (r) => { r.runs[0].charge_id = null; } }, "billing_recovery", /^UNKNOWN/],
    ["an incomplete ledger", { ledger: false }, "billing_recovery", /^UNKNOWN/],
    ["no M2 text_generate record", { mutate: (r) => { r.text_run = null; } }, "generation", /^UNKNOWN/],
    ["M2 image that did not load", { mutate: (r) => { r.text_run = { layer: "real", outcome: "UNKNOWN", image_loaded: false }; } }, "generation", /^UNKNOWN/],
    ["an M1 run without a plate asset", { mutate: (r) => { r.runs[0].plate = null; } }, "generation", /^UNKNOWN/],
    ["an export that differs from the displayed composite", { mutate: (r) => { r.runs[0].export.image_sha_matches_view = false; } }, "export", /^FAIL/],
    ["a dirty working tree", { mutate: (r) => { r.tree_dirty = true; } }, "browser_real", /^FAIL/],
    ["a backend build that is not the frontend revision", { mutate: (r) => { r.backend_build.vcs_revision = "0".repeat(40); } }, "browser_real", /^FAIL/],
    ["a run skipped for budget", { mutate: (r) => { r.runs[2] = { layer: "real", label: "M1-case2", skipped: "budget" }; } }, "browser_real", /^NOT_RUN/],
    ["a fixture dry run", { layer: "fixture-dry-run" }, "browser_real", /^NOT_RUN/],
    ["a failing engineering gate", { gates: { go_race: 1 } }, "engineering", /^FAIL/],
    ["no order / Campaign return evidence (Level B NOT_RUN)", { cross: "none" }, "cross_app", /^NOT_RUN/],
    ["a blocked return stack", { cross: "blocked" }, "cross_app", /^BLOCKED/],
    ["a return record without the quality binding check", { cross: "incomplete" }, "cross_app", /^UNKNOWN/],
    ["no simulated review", { review: false }, "human", /^NOT_RUN/],
    ["an invalid simulated review", { review: "invalid" }, "human", /^NOT_RUN/],
  ];
  it.each(cases)("%s caps the verdict at PARTIAL", (_name, opts, layer, want) => {
    const dir = stage(opts);
    expect(layerOf(dir, layer)).toMatch(want);
    expect(assess(dir).verdict).toBe("PARTIAL");
  });

  it("a BLOCKED.json reports every real layer as BLOCKED, never as a fixture value", () => {
    const dir = stage(); fs.writeFileSync(path.join(dir, "real-e2e", "BLOCKED.json"), JSON.stringify({ reason: "no_storage_state" }));
    const out = assess(dir);
    expect(out.layers.browser_real).toBe("BLOCKED");
    expect(out.verdict).toBe("PARTIAL");
  });
});
