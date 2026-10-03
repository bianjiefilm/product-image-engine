// FIXTURE ONLY. A tiny in-process stand-in for the product Go API, used to drive
// the real web app in a real browser through states that are expensive or unsafe
// to produce on the real stack (errors, slow runs, unusable candidates).
// Evidence taken against it is labelled `fixture` and is never acceptance evidence.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { writeStoredZip } from "./zip.mjs";

const PORT = Number(process.env.FIXTURE_API_PORT ?? 32324);
const SAMPLES = process.env.FIXTURE_SAMPLES_DIR;
if (!SAMPLES) throw new Error("FIXTURE_SAMPLES_DIR is required");
const manifest = JSON.parse(fs.readFileSync(path.join(SAMPLES, "manifest.json"), "utf8"));
const frozen = new Map(manifest.cases.map((c) => [c.original.sha256, c]));
const TOKEN = "fixture-token";
const ACCESS = "fixture-access";
const sha = (b) => crypto.createHash("sha256").update(b).digest("hex");
const PASS = ["mask_pixels_identical", "coverage_logo", "coverage_packaging_text", "coverage_spec", "coverage_structure", "product_count", "output_size", "plate_size", "plate_is_new_background", "background_changed", "plate_task_succeeded", "plate_asset_hash_verified", "plate_charged_observed"];

const state = { scenario: "limited", photos: new Map(), inputs: [], runs: new Map(), stats: { creates: 0, confirms: 0, selects: 0, returns: 0, receipts: 0, uploads: 0, projects: 0 }, serial: 0 };
const reset = () => { state.photos.clear(); state.inputs = []; state.runs.clear(); Object.keys(state.stats).forEach((k) => (state.stats[k] = 0)); state.serial = 0; };

const json = (res, status, body) => { res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" }); res.end(JSON.stringify(body)); };
const err = (res, status, code, message = code) => json(res, status, { error: { code, message } });
const body = (req) => new Promise((resolve) => { const chunks = []; req.on("data", (c) => chunks.push(c)); req.on("end", () => resolve(Buffer.concat(chunks))); });

function supported() {
  return state.inputs.filter((i) => state.photos.get(i.platform_asset_id)?.frozen).map((i) => ({ input_id: i.id, kind: state.photos.get(i.platform_asset_id).frozen.kind, canvas: { width_px: state.photos.get(i.platform_asset_id).frozen.canvas.w, height_px: state.photos.get(i.platform_asset_id).frozen.canvas.h } }));
}
function capabilities() {
  const disabled = state.scenario === "disabled";
  const list = supported();
  return {
    historical_read: true, automatic_output_recovery_ready: true,
    modes: [
      { mode: "text_generate", ready: false, disabled: true, can_quote: false, can_confirm: false, authorization_reason: "", runtime_reason: "", reason: "source_unconfigured", size: "1024*1024", model: "qwen-image-2.0", fidelity: "not_applicable", visual_quality: "unknown" },
      { mode: "background_plate_lock", ready: !disabled, disabled: disabled, can_quote: !disabled, can_confirm: !disabled, reason: disabled ? "plate_lock_disabled" : "", size: "1024*1024", model: "qwen-image-2.0", claim: "frozen_synthetic_sample_only", fidelity: "frozen_sample_only", visual_quality: "unknown", supported_input_ids: disabled ? [] : list.map((i) => i.input_id), supported_inputs: disabled ? [] : list },
      { mode: "reference_edit", ready: false, disabled: true, reason: "formal_contract_unconfigured" },
    ],
  };
}
const report = (run) => JSON.stringify({ report_version: "product-source-quality/v2", facts: { composite_sha256: sha(run.png) } });
function view(run) {
  const done = run.derived;
  const candidate = !done ? "pending" : state.scenario === "not_usable" ? "not_usable_candidate" : "limited_candidate";
  const axes = [...PASS.map((name) => ({ name, state: candidate === "not_usable_candidate" && name === "background_changed" ? "FAIL" : "PASS", machine_checked: true, evidence: "fixture" })),
    ...["background_extra_objects", "background_intent_followed", "visual_composite_quality"].map((name) => ({ name, state: "UNKNOWN", machine_checked: false, evidence: "not machine checkable" })), { name: "transmission_edge", state: "N/A", machine_checked: false, evidence: "not machine checkable" }];
  const now = Math.floor(Date.now() / 1000);
  const plate = { result_kind: "derived_composite", claim: "frozen_synthetic_sample_only", human_usefulness: "NOT_RUN", derivation_state: done ? "derived" : "pending", candidate_state: candidate, sample_set_id: "frozen-sample-set", case_id: run.case.case_id, background_intent: run.intent, canvas: { width_px: run.case.canvas.w, height_px: run.case.canvas.h } };
  if (done) Object.assign(plate, { verdicts: { fidelity: "PASS", generation: "PASS", billing: "PASS", human_usefulness: "NOT_RUN" }, axes, limits: candidate === "limited_candidate" ? ["已验证范围仅限冻结合成样本（纸盒/金属把手杯/玻璃瓶），不代表任意商品照片", "样本为硬边渲染，未覆盖真实照片的边缘羽化与光晕", "背景内容未经机器检查：可能出现多余物体，也可能与所写方向不符", "整体观感未经人工评审"] : [], report_sha256: sha(Buffer.from(report(run))), composite_sha256: sha(run.png), result: { content_type: "image/png", size_bytes: String(run.png.length), width_px: run.case.canvas.w, height_px: run.case.canvas.h, sha256: sha(run.png) } });
  return {
    run_id: run.id, project_id: run.project, request_key: run.key, owner_version: "product_source_v1", mode: "background_plate_lock", model: "qwen-image-2.0", provider: "modelxing-qwen-image-2.0-v1", size: "1024*1024", quantity: "1",
    phase: done ? "succeeded" : run.confirmed ? "task_linked" : "quoted", task_id: run.confirmed ? "task_fx" : "", selected: run.selected, deleted: false, cancel_requested: false, confirmed: run.confirmed,
    payment: { currency: "CNY", payer_source: "personal", payer_label: "个人账户", payer_account_id: "acct_fixture", payer_user_id: "usr_fixture", status: done ? "charged" : "quoted", charged_minor: done ? "25" : null, original_charged_minor: done ? "25" : null, refund_amount_minor: null, refresh_status: "not_refreshed", observation_time_known: false, last_verified_at: null, ...(done ? { charge_id: "charge_fx", original_charge_id: "charge_fx" } : {}) },
    quote: { quote_id: "quote_fx", quote_fingerprint: "a".repeat(64), usage_id: "usage_fx", currency: "CNY", quantity: "1", unit_price_minor: "25", amount_minor: "25", quoted_at_unix: run.quotedAt, expires_at_unix: run.quotedAt + 300, pricing_version: "fixture-price-v1" },
    output: null, snapshot_updated_at: now, fidelity: "frozen_sample_only", visual_quality: "unknown", human_adoption: "NOT_RUN", automatic_output_recovery_ready: true, content_availability: "not_checked", plate,
  };
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${PORT}`);
  const p = url.pathname;
  if (p === "/__fixture/scenario" && req.method === "POST") { const b = JSON.parse((await body(req)).toString() || "{}"); state.scenario = b.name ?? "limited"; if (b.reset) reset(); return json(res, 200, { scenario: state.scenario }); }
  if (p === "/__fixture/stats") return json(res, 200, { ...state.stats, scenario: state.scenario });
  if (p === "/__fixture/advance" && req.method === "POST") { for (const r of state.runs.values()) if (r.confirmed) r.derived = true; return json(res, 200, {}); }
  if (req.headers["x-product-internal-token"] !== TOKEN) return err(res, 401, "unauthenticated");
  if (req.headers.authorization !== `Bearer ${ACCESS}`) return err(res, 401, "unauthenticated");
  if (p === "/api/v1/auth/session") return json(res, 200, { principal: { user_id: "usr_fixture", account_id: "acct_fixture", tenant_id: "acct_fixture" } });
  if (p === "/api/v1/entry/personal") return json(res, 200, { sessions: [] });
  if (p === "/api/v1/billing/balance") return json(res, 200, { balance_cny: "100.00", items: [], account_id: "acct_fixture" });
  if (p === "/api/v1/photos" && req.method === "POST") {
    const buf = await body(req);
    if (buf.subarray(0, 8).toString("hex") !== "89504e470d0a1a0a") return err(res, 415, "unsupported_media_type", "内容不是受支持的图片格式");
    const sha = crypto.createHash("sha256").update(buf).digest("hex");
    state.stats.uploads++;
    const asset = "asset_" + sha.slice(0, 12);
    const photo = { id: "pho_" + sha.slice(0, 8), platform_asset_id: asset, original_name: url.searchParams.get("filename") ?? "photo.png", size_bytes: buf.length, media_type: "image/png", sha256: sha };
    state.photos.set(asset, { ...photo, frozen: frozen.get(sha) ?? null });
    return json(res, 201, { photo, idempotent: false });
  }
  if (p === "/api/v1/projects" && req.method === "POST") { state.stats.projects++; return json(res, 201, { project: { id: "proj_fx", name: "个人商品图", status: "draft", width_px: 1024, height_px: 1024, source_type: "standalone" } }); }
  let m;
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/inputs$/)) && req.method === "POST") {
    const b = JSON.parse((await body(req)).toString());
    const input = { id: "pin_" + ++state.serial, platform_asset_id: b.platform_asset_id, snapshot_name: b.snapshot_name };
    state.inputs.push(input);
    return json(res, 201, { input });
  }
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/source-image-capabilities$/))) return json(res, 200, capabilities());
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/source-image-runs(?:\/([^/]+))?(?:\/([^/]+))?$/))) {
    const [, project, runId, action] = m;
    if (!runId) {
      if (req.method === "GET") return json(res, 200, { runs: [...state.runs.values()].map(view), next_cursor: "" });
      const b = JSON.parse((await body(req)).toString());
      if (state.scenario === "create_503") return err(res, 503, "sample_set_invalid");
      const input = state.inputs.find((i) => i.id === b.input_id);
      const photo = input && state.photos.get(input.platform_asset_id);
      if (!photo?.frozen) return err(res, 422, "sample_not_frozen");
      const existing = [...state.runs.values()].find((r) => r.key === b.request_key);
      if (existing) return json(res, 200, view(existing));
      state.stats.creates++;
      const c = photo.frozen;
      const run = { id: "sir_fx" + state.serial++, project, key: b.request_key, intent: b.background_intent, case: c, quotedAt: Math.floor(Date.now() / 1000) - 1, confirmed: false, derived: false, selected: false, polls: 0, png: fs.readFileSync(path.join(SAMPLES, c.original.path)) };
      state.runs.set(run.id, run);
      return json(res, 200, view(run));
    }
    if (runId === "by-request") { const r = [...state.runs.values()].find((x) => x.key === url.searchParams.get("request_key")); return r ? json(res, 200, view(r)) : err(res, 404, "not_found"); }
    const run = state.runs.get(runId);
    if (!run) return err(res, 404, "not_found");
    if (!action && req.method === "GET") { if (run.confirmed && state.scenario !== "slow" && ++run.polls >= 2) run.derived = true; return json(res, 200, view(run)); }
    if (action === "confirm") { await body(req); state.stats.confirms++; run.confirmed = true; return json(res, 200, view(run)); }
    if (action === "reconcile") { await body(req); if (run.confirmed && state.scenario !== "slow") run.derived = true; return json(res, 200, view(run)); }
    if (action === "select") { await body(req); if (view(run).plate.candidate_state !== "limited_candidate") return err(res, 409, "candidate_not_usable"); state.stats.selects++; run.selected = true; return json(res, 200, view(run)); }
    if (action === "return") { await body(req); state.stats.returns++; return json(res, 200, { output: { id: "out_fx" } }); }
    if (action === "content") { res.writeHead(200, { "Content-Type": "image/png", "Content-Length": run.png.length, "X-Quality-Verdict": "limited" }); return res.end(run.png); }
    if (action === "export") { const zip = writeStoredZip({ "image.png": run.png, "quality.json": Buffer.from(report(run)), "snapshot.json": Buffer.from("{}") }); res.writeHead(200, { "Content-Type": "application/zip", "Content-Length": zip.length }); return res.end(zip); }
  }
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/outputs\/([^/]+)\/receipt$/)) && req.method === "POST") { state.stats.receipts++; return json(res, 201, { receipt: { status: "delivered" } }); }
  return err(res, 404, "not_found");
});
server.listen(PORT, "127.0.0.1", () => console.log(`fixture api listening on ${PORT}`));
