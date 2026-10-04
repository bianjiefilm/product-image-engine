// REAL-STACK acceptance driver (HUI-2232 M1-M3). It drives the normal product UI
// in a real headless Chrome (own profile, own ports) against a stack the
// operator already started: real Go server + real Next web + Root-provided QA
// platform. It never types credentials: login state comes from a Root-provided
// Playwright storage-state file (E2E_STORAGE_STATE) => login is ROOT_ASSISTED.
//
// REAL_DRY_RUN_FIXTURE=1 runs the very same steps against the FIXTURE stack so
// the driver itself can be verified for free. Its output is then labelled
// `fixture-dry-run` and is never acceptance evidence.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";
import { openLedger, readLedger, ledgerProblems, spentMinor } from "./ledger.mjs";
import { readStoredZip } from "./zip.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const DRY = process.env.REAL_DRY_RUN_FIXTURE === "1";
const LAYER = DRY ? "fixture-dry-run" : "real";
const WEB = process.env.REAL_WEB_URL ?? "http://localhost:32321"; // always "localhost": the source BFF compares Origin with it
const API = process.env.REAL_API_URL ?? "http://127.0.0.1:32320";
const need = (n) => { const v = process.env[n]; if (!v) { console.error(`${n} is required`); process.exit(2); } return v; };
const OUT = path.join(need("E2E_OUT"), "real-e2e"), PROFILE = need("E2E_PROFILE"), SAMPLES = need("FIXTURE_SAMPLES_DIR");
const CHROME = process.env.E2E_CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const BUDGET = Number(process.env.E2E_BUDGET_MINOR ?? "0"); // remaining minor units Root supplied; 0 = unknown => refuse to spend
const RUN_TEXT = process.env.REAL_RUN_TEXT === "1" || (!DRY && process.env.REAL_RUN_TEXT !== "0");
const FAULT_LOG = process.env.REAL_FAULT_LOG;
const FAULT_RUN = Number(process.env.REAL_FAULT_RUN_INDEX ?? "1"); // which M1 run goes through the fault proxy
const WAIT_MS = Number(process.env.REAL_WAIT_MS ?? String(8 * 60_000));
const manifest = JSON.parse(fs.readFileSync(path.join(SAMPLES, "manifest.json"), "utf8"));
const sha = (b) => crypto.createHash("sha256").update(b).digest("hex");
const VIEWPORTS = { desktop: { width: 1280, height: 800 }, mobile: { width: 390, height: 844 } };
fs.mkdirSync(path.join(OUT, "exports"), { recursive: true });
const ledger = openLedger(path.join(OUT, "charge-ledger.jsonl"));

function stop(reason, details, code = 3) {
  fs.writeFileSync(path.join(OUT, "BLOCKED.json"), JSON.stringify({ layer: LAYER, reason, details, at: new Date().toISOString(), note: "No fixture value stands in for a missing prerequisite." }, null, 2));
  console.error(`BLOCKED: ${reason}`); process.exit(code);
}
const git = (...a) => execFileSync("git", a, { cwd: webDir }).toString().trim();

// ---- preflight -------------------------------------------------------------
let cookies;
if (DRY) cookies = [{ name: "pia_access", value: "fixture-access", domain: "localhost", path: "/", httpOnly: false, secure: false, sameSite: "Lax", expires: -1 }];
else {
  const state = process.env.E2E_STORAGE_STATE;
  if (!state || !fs.existsSync(state)) stop("no_storage_state", { need: "E2E_STORAGE_STATE (Root-provided Playwright storage state; login is ROOT_ASSISTED)" });
  cookies = JSON.parse(fs.readFileSync(state, "utf8")).cookies ?? [];
  if (!cookies.length) stop("empty_storage_state", {});
}
const frontendSha = (() => { try { return git("rev-parse", "HEAD"); } catch { return "unknown"; } })();
const dirty = (() => { try { return git("status", "--porcelain").length > 0; } catch { return true; } })();
let ready = null;
if (!DRY) {
  try { ready = await (await fetch(`${API}/readyz`)).json(); } catch { stop("backend_unreachable", { api: API }); }
  if (!ready.ok || ready.gates?.plate_lock?.usable !== true) stop("plate_lock_not_ready", { fatal: ready.fatal, plate_lock: ready.gates?.plate_lock });
  if (ready.build?.vcs_revision !== frontendSha || ready.build?.vcs_modified || dirty) stop("build_identity_mismatch", { backend: ready.build, frontend: frontendSha, tree_dirty: dirty });
  if (!(BUDGET > 0)) stop("no_budget_supplied", { need: "E2E_BUDGET_MINOR (remaining minor units Root granted)" });
}

// ---- helpers ---------------------------------------------------------------
async function session(name, vp = "desktop") {
  const dir = path.join(PROFILE, `real-${name}-${vp}`); fs.rmSync(dir, { recursive: true, force: true });
  const ctx = await chromium.launchPersistentContext(dir, { executablePath: CHROME, headless: true, viewport: VIEWPORTS[vp], args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
  await ctx.addCookies(cookies.map((c) => (DRY ? c : { ...c })));
  return { ctx, page: ctx.pages()[0] ?? (await ctx.newPage()) };
}
const jget = async (ctx, p) => { const r = await ctx.request.get(WEB + p); return { status: r.status(), body: await r.json().catch(() => null) }; };
const jpost = async (ctx, p, body) => { const r = await ctx.request.post(WEB + p, { data: body, headers: { origin: WEB, "content-type": "application/json" } }); return { status: r.status(), body: await r.json().catch(() => null) }; };
const balance = async (ctx) => { const r = await jget(ctx, "/api/billing/balance"); return r.status === 200 ? { balance_cny: String(r.body?.balance_cny ?? ""), items: (r.body?.items ?? []).length } : { unavailable: r.status }; };
const urlParam = (page, k) => new URL(page.url()).searchParams.get(k);
const shot = async (page, name) => { const f = `${LAYER}-${name}.png`; await page.screenshot({ path: path.join(OUT, f), fullPage: true }); return f; };
async function waitDerived(ctx, project, run) {
  const end = Date.now() + WAIT_MS; let last;
  while (Date.now() < end) {
    last = (await jget(ctx, `/api/projects/${project}/source-image-runs/${run}`)).body;
    if (last?.plate && last.plate.derivation_state !== "pending") return { view: last, timedOut: false };
    if (["refunded", "released"].includes(last?.payment?.status)) return { view: last, timedOut: false };
    await new Promise((r) => setTimeout(r, 3000));
  }
  return { view: last, timedOut: true };
}
const facts = (v) => ({ run_id: v.run_id, project_id: v.project_id, request_key: v.request_key, usage_id: v.quote?.usage_id, quote_id: v.quote?.quote_id, task_id: v.task_id, hold_id: v.payment?.hold_id ?? null, charge_id: v.payment?.original_charge_id ?? v.payment?.charge_id ?? null, amount_minor: v.quote?.amount_minor, charged_minor: v.payment?.charged_minor, payment_status: v.payment?.status, candidate_state: v.plate?.candidate_state, plate: v.plate?.plate_task ?? null, composite_sha256: v.plate?.composite_sha256 ?? null, report_sha256: v.plate?.report_sha256 ?? null, verdicts: v.plate?.verdicts ?? null });

// ---- M1: paid, limited-fidelity, through the normal UI ----------------------
const plan = [["carton-1024x1024"], ["handled_metal-1024x1280"], ["glass_bottle-768x1024"]];
const runs = [];
async function plateRun(index, caseId) {
  const label = `M1-${caseId}`;
  const c = manifest.cases.find((x) => x.case_id === caseId); const { ctx, page } = await session(label);
  try {
    await page.goto(`${WEB}/start`);
    await page.locator("#photo-file").setInputFiles(path.join(SAMPLES, c.original.path));
    await page.getByRole("button", { name: "上传并使用" }).click();
    await page.getByTestId("plate-form").waitFor({ timeout: 90_000 });
    const project = urlParam(page, "project");
    if (index === 0) fs.writeFileSync(path.join(OUT, "capabilities-snapshot.json"), JSON.stringify((await jget(ctx, `/api/projects/${project}/source-image-capabilities`)).body, null, 2));
    await page.getByLabel("背景描述").fill(c.intents[0]);
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-quote").waitFor({ timeout: 90_000 });
    const runId = urlParam(page, "plate_run");
    const before = (await jget(ctx, `/api/projects/${project}/source-image-runs/${runId}`)).body;
    const amount = Number(before.quote.amount_minor), bal0 = await balance(ctx);
    if (!DRY && spentMinor(readLedger(ledger.file)) + amount > BUDGET) { return { label, skipped: "budget", would_spend_minor: amount }; }
    ledger.intent(label, { case_id: caseId, project_id: project, run_id: runId, usage_id: before.quote.usage_id, quote_id: before.quote.quote_id, expected_amount_minor: String(amount), balance_before: bal0, fault_injected: index === FAULT_RUN && !!FAULT_LOG });
    const screens = [await shot(page, `${label}-quote-desktop`)];
    await page.getByTestId("plate-confirm").click();
    const { view, timedOut } = await waitDerived(ctx, project, runId);
    const bal1 = await balance(ctx);
    ledger.observed(label, { ...facts(view), timed_out: timedOut, balance_after: bal1 });
    if (timedOut || !view.plate || view.plate.derivation_state === "pending") return { label, case_id: caseId, background_intent: c.intents[0], layer: LAYER, outcome: "UNKNOWN", ...facts(view), screenshots: screens };
    // If the confirm response was lost (fault leg), the backend recovers along the
    // original key but the panel keeps the idempotent confirm armed ("状态待核实").
    // A person clicks that same button again; the driver does exactly that, once,
    // and only while the button is actually there.
    const resultShown = await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 15_000 }).then(() => true).catch(() => false);
    if (!resultShown) {
      const again = page.getByTestId("plate-confirm");
      if (await again.isVisible().catch(() => false)) await again.click().catch(() => {});
      await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 60_000 });
    }
    screens.push(await shot(page, `${label}-result-desktop`));
    await page.setViewportSize(VIEWPORTS.mobile); screens.push(await shot(page, `${label}-result-mobile`)); await page.setViewportSize(VIEWPORTS.desktop);
    let exportCheck = null, selected = false;
    if (view.plate.candidate_state === "limited_candidate") {
      await page.getByTestId("plate-select").click(); await page.getByRole("button", { name: "已选定" }).waitFor({ timeout: 60_000 }); selected = true;
      const res = await ctx.request.get(`${WEB}/api/projects/${project}/source-image-runs/${runId}/export`); assert.equal(res.status(), 200, "export");
      const buf = await res.body(); fs.writeFileSync(path.join(OUT, "exports", `${label}.zip`), buf);
      const zip = readStoredZip(buf), image = zip.get("image.png"), quality = JSON.parse(zip.get("quality.json").toString());
      exportCheck = { image_sha_matches_view: sha(image) === view.plate.composite_sha256, report_version: quality.report_version, report_bound_to_image: quality.facts?.composite_sha256 === sha(image), entries: [...zip.keys()] };
      assert.ok(exportCheck.image_sha_matches_view && exportCheck.report_bound_to_image && exportCheck.report_version === "product-source-quality/v2", "export must be the displayed composite with its frozen report");
    }
    const fault = index === FAULT_RUN && FAULT_LOG && fs.existsSync(FAULT_LOG) ? JSON.parse(fs.readFileSync(FAULT_LOG, "utf8")) : null;
    return { label, case_id: caseId, background_intent: c.intents[0], layer: LAYER, outcome: "OBSERVED", ...facts(view), quote_fingerprint: before.quote.quote_fingerprint, selected, export: exportCheck, fault, balance_before: bal0, balance_after: bal1, screenshots: screens, frontend_git_sha: frontendSha };
  } finally { await ctx.close(); }
}

// ---- M3: zero-new-cost recovery probes on a finished run ---------------------
async function probes(r0) {
  const { ctx, page } = await session("probes");
  try {
    const base = `/api/projects/${r0.project_id}/source-image-runs/${r0.run_id}`;
    const snap = async () => { const v = (await jget(ctx, base)).body, list = (await jget(ctx, `/api/projects/${r0.project_id}/source-image-runs`)).body; return { ...facts(v), runs_in_project: list.runs.length, balance: await balance(ctx) }; };
    const baseline = await snap(), out = [];
    const open = async (p) => { await p.goto(`${WEB}/start?project=${r0.project_id}&plate_run=${r0.run_id}`); await p.locator('[data-testid="plate-result"]').waitFor({ timeout: 90_000 }); };
    const steps = [
      ["confirm_again_with_the_original_quote", () => jpost(ctx, `${base}/confirm`, { quote_id: r0.quote_id, quote_fingerprint: r0.quote_fingerprint })],
      ["reconcile", () => jpost(ctx, `${base}/reconcile`, {})],
      ["reload_page", async () => { await open(page); await page.reload(); await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 90_000 }); }],
      // NOT a sign-out / sign-in: a fresh browser context reusing the SAME stored login state. Evidence and handoff must say exactly that.
      ["new_context_same_login_state", async () => { const s2 = await session("probes-newctx"); try { await open(s2.page); } finally { await s2.ctx.close(); } }],
    ];
    for (const [name, step] of steps) { await step(); const after = await snap(); out.push({ name, equal: JSON.stringify(after) === JSON.stringify(baseline), before: baseline, after }); }
    return out;
  } finally { await ctx.close(); }
}

// ---- M2: one paid ordinary text image through the existing text panel ---------
async function textRun() {
  const label = "M2-text_generate"; const { ctx, page } = await session(label);
  try {
    await page.goto(`${WEB}/start`);
    await page.locator("summary", { hasText: "普通文字生成" }).click();
    // matches SourceImagePanel.tsx today: "创建个人普通图工程（1024×1024）" (the text panel is unchanged by Task 3)
    await page.getByRole("button", { name: /创建个人普通图工程/ }).click();
    await page.getByLabel("文字描述").waitFor({ timeout: 90_000 });
    await page.getByLabel("文字描述").fill("蓝色纸盒，浅灰棚拍");
    await page.getByRole("button", { name: "获取报价" }).click();
    const confirm = page.getByRole("button", { name: /确认支付并生成/ }); await confirm.waitFor({ timeout: 90_000 });
    const text = await confirm.innerText(); const amount = Math.round(Number(text.match(/¥([0-9.]+)/)?.[1] ?? "0") * 100), bal0 = await balance(ctx);
    if (!DRY && spentMinor(readLedger(ledger.file)) + amount > BUDGET) return { label, skipped: "budget", would_spend_minor: amount };
    ledger.intent(label, { expected_amount_minor: String(amount), balance_before: bal0 });
    await confirm.click();
    const img = page.locator("figure img"); await img.first().waitFor({ timeout: WAIT_MS });
    const ok = await img.first().evaluate((i) => i.complete && i.naturalWidth > 0);
    // The paid facts come from the product's own run view, never from the DOM: charge count and amount must be auditable.
    const project = urlParam(page, "project");
    let view = null; const end = Date.now() + WAIT_MS;
    while (project && Date.now() < end) {
      const list = (await jget(ctx, `/api/projects/${project}/source-image-runs`)).body?.runs ?? [];
      view = list.find((r) => r.mode === "text_generate") ?? null;
      if (view && ["charged", "refunded", "released"].includes(view.payment?.status)) break;
      await new Promise((r) => setTimeout(r, 3000));
    }
    const bal1 = await balance(ctx), f = view ? facts(view) : {};
    ledger.observed(label, { ...f, run_id: f.run_id ?? null, payment_status: f.payment_status ?? "unobserved", charged_minor: f.charged_minor ?? null, balance_after: bal1, image_loaded: ok });
    const complete = ok && f.payment_status === "charged" && !!f.charge_id;
    return { label, layer: LAYER, outcome: complete ? "OBSERVED" : "UNKNOWN", image_loaded: ok, ...f, balance_before: bal0, balance_after: bal1, fidelity_claim: "none (ordinary concept image)", screenshot: await shot(page, `${label}-result`) };
  } finally { await ctx.close(); }
}

// ---- go ----------------------------------------------------------------------
let textRunRecord = null, probeRecord = [];
for (const [i, [caseId]] of plan.entries()) { const r = await plateRun(i, caseId); runs.push(r); if (r.skipped === "budget") break; }
const finished = runs.find((r) => r.outcome === "OBSERVED");
if (finished) probeRecord = await probes(finished);
if (RUN_TEXT) textRunRecord = await textRun();
const entries = readLedger(ledger.file);
const record = {
  layer: LAYER, frontend_git_sha: frontendSha, tree_dirty: dirty, backend_build: ready?.build ?? null, login: DRY ? "fixture cookie" : "ROOT_ASSISTED", ready_gates: ready?.gates?.plate_lock ?? null,
  runs, probes: probeRecord, text_run: textRunRecord, spent_minor: spentMinor(entries), ledger_problems: ledgerProblems(entries),
  supplier_expense_ledger_verified: false, notes: ["supplier cost is only a declared SKU value; no audited supplier ledger was available", "human usefulness: NOT_RUN", "probe new_context_same_login_state reuses the same stored login state in a new browser context; it is not a sign-out / sign-in", "cross_app (order / Campaign return, A7 Level B) is not driven by this script: it needs a real guanlan-order stack and its own evidence under cross-app/"],
};
fs.writeFileSync(path.join(OUT, "run-record.json"), JSON.stringify(record, null, 2));
const bad = runs.some((r) => r.outcome !== "OBSERVED") || probeRecord.some((p) => !p.equal) || record.ledger_problems.length > 0;
console.log(JSON.stringify({ layer: LAYER, runs: runs.map((r) => ({ label: r.label, outcome: r.outcome ?? r.skipped, candidate: r.candidate_state })), probes: probeRecord.map((p) => ({ name: p.name, equal: p.equal })), spent_minor: record.spent_minor }, null, 2));
process.exit(bad ? 1 : 0);
