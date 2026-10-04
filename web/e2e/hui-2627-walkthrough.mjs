// HUI-2627 media-first workbench walkthrough (Root 2026-10-04). Zero-spend:
// reuses attempt-9 paid plate runs and the V1/V2 revision ledger from the
// HUI-2596 walkthrough; only reads, selects, adopts/rolls back and exports.
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright-core";

const WEB = process.env.REAL_WEB_URL ?? "http://localhost:32321";
const OUT = process.env.E2E_OUT;
const STATE = process.env.E2E_STORAGE_STATE;
const PROJECT = process.env.E2E_PROJECT;
const SSH_args = JSON.parse(process.env.SSH_ARGS ?? '["-o","ControlPath=none","-F","/Users/jimfu/.codex/qa-private-nine-projects-20261001/ssh-client/config","root@120.26.69.202"]');
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const WIDTHS = [1920, 1440, 1024, 430, 390];
const save = (name, data) => fs.writeFileSync(path.join(OUT, name), typeof data === "string" ? data : JSON.stringify(data, null, 1));
const shot = async (page, name, w) => page.screenshot({ path: path.join(OUT, "browser", `2627-${name}-${w}.png`), fullPage: w === 1440 });
const ssh = (...cmd) => execFileSync("ssh", [...SSH_args, ...cmd], { encoding: "utf8" });

fs.mkdirSync(path.join(OUT, "browser"), { recursive: true });
const record = { project: PROJECT, started_at: new Date().toISOString(), findings: [] };
const note = (kind, what) => record.findings.push({ kind, what });

// A. Logged-out: /login keeps a media-first page and / redirects to it.
const anon = await chromium.launchPersistentContext(path.join(OUT, "..", "..", "..", "tmp", "anon-2627") /* throwaway */, { executablePath: CHROME, headless: true, viewport: { width: 1440, height: 900 }, args: ["--no-first-run"] }).catch(() => null);
const anonDir = "/tmp/painuo-pie-accept/profile/anon-2627";
if (anon) await anon.close();
fs.rmSync(anonDir, { recursive: true, force: true });
const anonCtx = await chromium.launchPersistentContext(anonDir, { executablePath: CHROME, headless: true, viewport: { width: 1440, height: 900 }, args: ["--no-first-run"] });
const anonPage = anonCtx.pages()[0] ?? (await anonCtx.newPage());
await anonPage.goto(`${WEB}/`);
await anonPage.waitForURL(/\/(login|start)/, { timeout: 30_000 }).catch(() => {});
record.anon_root_lands_on = new URL(anonPage.url()).pathname;
await anonPage.goto(`${WEB}/login`);
await anonPage.waitForLoadState("networkidle").catch(() => {});
await anonPage.screenshot({ path: path.join(OUT, "browser", "2627-login-1440.png"), fullPage: true });
const loginFormTop = await anonPage.evaluate(() => {
  const btn = [...document.querySelectorAll("button, [role=button], input[type=submit]")].find((b) => /登录|开始/.test(b.textContent ?? ""));
  const form = btn?.closest("form");
  if (!btn || !form) return { found: false };
  const formTop = form.getBoundingClientRect().top, btnTop = btn.getBoundingClientRect().top;
  const inputs = [...form.querySelectorAll("input")].filter((i) => i.type !== "hidden");
  const firstInput = inputs[0]?.getBoundingClientRect().top ?? btnTop;
  return { found: true, button_before_first_input: btnTop <= firstInput + 8, label: btn.textContent.trim() };
});
save("login-primary-action.json", loginFormTop);
if (!loginFormTop.found || !loginFormTop.button_before_first_input) note("fail", "login primary action is not at the top of the form");
await anonCtx.close();

// B. Logged in.
const cookies = JSON.parse(fs.readFileSync(STATE, "utf8")).cookies;
const dir = "/tmp/painuo-pie-accept/profile/walk-2627";
fs.rmSync(dir, { recursive: true, force: true });
const ctx = await chromium.launchPersistentContext(dir, { executablePath: CHROME, headless: true, args: ["--no-first-run"] });
await ctx.addCookies(cookies.map((c) => ({ ...c })));
const page = ctx.pages()[0] ?? (await ctx.newPage());
const jget = async (p) => { const r = await ctx.request.get(WEB + p); return { status: r.status(), body: await r.json().catch(() => null) }; };

// B1. /start: no legacy form fields; capability matrix; primary actions.
await page.setViewportSize({ width: 1440, height: 900 });
await page.goto(`${WEB}/start`);
await page.waitForLoadState("networkidle").catch(() => {});
const startChecks = await page.evaluate(() => {
  const text = document.body.innerText;
  const inputs = [...document.querySelectorAll("input")].map((i) => i.type + ":" + (i.placeholder ?? i.name ?? ""));
  return {
    has_legacy_fields: /商品名称|用途|自定义尺寸|模型选择|生成数量/.test(text),
    input_kinds: inputs,
    has_capability_matrix: !!document.querySelector("[data-testid=\"capability-matrix\"], section[aria-label*='能力'], #capability-matrix") || /能力/.test(text),
    technical_open: [...document.querySelectorAll("details[data-technical]")].filter((d) => d.open).length,
    technical_count: document.querySelectorAll("details[data-technical]").length,
    leaks_provider: /qwen|modelxing|sha256|run_id|debug=/i.test(text),
  };
});
save("start-checks.json", startChecks);
if (startChecks.has_legacy_fields) note("fail", "start page still shows legacy form fields");
if (startChecks.leaks_provider) note("fail", "start page leaks provider/debug wording");
await shot(page, "start", 1440);

// B2. Screenshot matrix of six routes at five widths (HUI-2625 input).
const routes = [["start", "/start"], ["projects", "/projects"], ["detail", `/projects/${PROJECT}`], ["batches", "/batches"], ["handoff", "/handoff"]];
for (const w of WIDTHS) {
  await page.setViewportSize({ width: w, height: 900 });
  for (const [name, route] of routes) {
    await page.goto(`${WEB}${route}`);
    await page.waitForLoadState("networkidle").catch(() => {});
    await new Promise((r) => setTimeout(r, 600));
    await page.screenshot({ path: path.join(OUT, "browser", `2627-matrix-${name}-${w}.png`), fullPage: false });
    const overflow = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
    if (overflow > 1) note("overflow", `${name}@${w} scrollWidth +${overflow}px`);
  }
}
save("matrix-widths.json", WIDTHS);

// B3. Detail page at 1440: chips, rail flow, honest copies, technical red lines.
await page.setViewportSize({ width: 1440, height: 900 });
await page.goto(`${WEB}/projects/${PROJECT}`);
await page.locator('section[data-revision="open"]').waitFor({ timeout: 30_000 });
await page.waitForLoadState("networkidle").catch(() => {});
const detailChecks = await page.evaluate(() => {
  const text = document.body.innerText;
  return {
    // The detail workbench stage is intentionally empty until new work happens;
    // the paid run's result card lives on the run view (/start?project=&plate_run=).
    stage_empty_copy: /结果会显示在这里/.test(text),
    rail_versions: [...document.querySelectorAll('#revision li strong')].map((s) => s.textContent.trim()),
    leaks_provider: /qwen|modelxing|run_id|debug=/i.test(text),
    recovery_strip: !!document.querySelector("#recovery, section[aria-label*='恢复'], section[id*=recovery]"),
    // SubjectFidelityPanel: chips when reports exist, honest closed copy when not.
    fidelity_chips: !!document.querySelector('[aria-label="主体保真结论"]'),
    fidelity_closed_copy: /主体保真记录未开启。真实生成保真仍为待确认/.test(text),
  };
});
save("detail-checks.json", detailChecks);
if (!detailChecks.stage_empty_copy) note("warn", "detail stage does not show the empty-state copy");
if (detailChecks.leaks_provider) note("fail", "detail page leaks provider/debug wording");
if (!detailChecks.fidelity_chips && !detailChecks.fidelity_closed_copy) note("fail", "SubjectFidelityPanel shows neither chips nor the honest closed copy");
note(detailChecks.fidelity_chips ? "ok" : "info", detailChecks.fidelity_chips ? "fidelity chips present on detail" : "fidelity panel honest closed copy (reports endpoint 404 on this stack)");
await shot(page, "detail", 1440);

// Rail: compare V1/V2, adopt V2, rollback V1, honest downstream copy.
// Baseline first: re-runs land with an arbitrary adopted pointer; bring it
// back to V1 through the UI so the flow below is deterministic.
const railStep = async (label, fn) => {
  try { await fn(); } catch (e) {
    await new Promise((r) => setTimeout(r, 2000));
    try { await fn(); } catch (e2) {
      const btns = await page.evaluate(() => [...document.querySelectorAll("#revision button")].slice(0, 14).map((b) => ({ t: b.textContent.trim().slice(0, 12), d: b.disabled })));
      throw new Error(`rail step ${label} failed: ${String(e2).split("\n")[0]} buttons=${JSON.stringify(btns)}`);
    }
  }
  await new Promise((r) => setTimeout(r, 800));
};
const adoptedNow = (await jget(`/api/projects/${PROJECT}/revisions`)).body?.adopted_label;
if (adoptedNow && adoptedNow !== "V1") {
  // Rollback targets the card of the version being restored; the adopted
  // version's own 回到 button is disabled by design.
  await railStep(`baseline-rollback-from-${adoptedNow}`, () =>
    page.locator('#revision li').first().getByRole("button", { name: "回到 V1", exact: true }).click({ timeout: 15_000 }));
}
await railStep("select-left", () => page.locator("#compare select").first().selectOption("V1"));
await railStep("select-right", () => page.locator("#compare select").nth(1).selectOption("V2"));
await railStep("compare", () => page.getByRole("button", { name: "并排比较" }).click({ timeout: 15_000 }));
await page.locator("#compare .compare-pair img").first().waitFor({ timeout: 30_000 });
await shot(page, "compare-pair", 1440);
await railStep("adopt-v2", () => page.locator('#revision li').nth(1).getByRole("button", { name: "采用这个版本" }).click({ timeout: 15_000 }));
await page.getByText("已采用 V2。", { exact: false }).waitFor({ timeout: 30_000 });
await shot(page, "adopted-v2", 1440);
await railStep("rollback-v1", () => page.locator('#revision li').first().getByRole("button", { name: "回到 V1", exact: true }).click({ timeout: 15_000 }));
await page.getByText("已回到 V1。", { exact: false }).waitFor({ timeout: 30_000 });
await railStep("downstream", () => page.locator('#revision li').first().getByRole("button", { name: "送数字人" }).click({ timeout: 15_000 }));
await page
  .locator("#revision p[role=status]")
  .evaluateAll((ps) => ps.map((p) => p.textContent ?? "").join("\n"))
  .then(async (first) => {
    let text = first;
    for (let i = 0; i < 10 && !/只返回了|没有送出|还没有/.test(text); i++) {
      await new Promise((r) => setTimeout(r, 1000));
      text = await page.locator("#revision p[role=status]").evaluateAll((ps) => ps.map((p) => p.textContent ?? "").join("\n"));
    }
    return text;
  })
  .then((text) => {
    fs.writeFileSync(path.join(OUT, "downstream-copy.txt"), text);
    if (!/只返回了|本地引用|还没收到/.test(text) || /selective_revision|FEATURE_|supplier/i.test(text)) note("fail", "downstream copy not the honest downgrade wording: " + text);
  });
await shot(page, "downstream-copy", 1440);

// B3b. Run view (/start?project=&plate_run=): the result card with chips,
// select, export, fidelity panel and technical details lives here. The panel
// restores a finished run only when the address carries plate_run (design:
// the address is the restore handle), so resolve the charged run first.
const runsBody = (await jget(`/api/projects/${PROJECT}/source-image-runs`)).body;
const runs = Array.isArray(runsBody) ? runsBody : Array.isArray(runsBody?.runs) ? runsBody.runs : [];
const charged = runs.find((r) => r.mode === "background_plate_lock" && r.phase === "succeeded" && !r.deleted) ?? null;
save("charged-run.json", charged ?? runsBody);
if (!charged?.run_id) note("fail", "no succeeded plate run to restore");
await page.goto(`${WEB}/start?project=${PROJECT}&plate_run=${charged?.run_id ?? ""}`);
await page.waitForLoadState("networkidle").catch(() => {});
await page.locator("figure img").first().waitFor({ timeout: 30_000 }).catch(() => {});
await new Promise((r) => setTimeout(r, 1000));
const runViewChecks = await page.evaluate(() => {
  const text = document.body.innerText;
  return {
    result_img: !!document.querySelector("figure img"),
    figcaption_text: document.querySelector("figure figcaption")?.textContent?.trim() ?? "",
    charged_line: /已扣费/.test(text),
    fidelity_panel: !!document.querySelector('[aria-label="主体保真结论"]'),
    technical_count: document.querySelectorAll("details[data-technical]").length,
    technical_closed: [...document.querySelectorAll("details[data-technical]")].every((d) => !d.open),
    leaks_provider: /qwen|modelxing|run_id|debug=/i.test(text),
    export_button: !!document.querySelector('[data-testid="plate-export"]'),
  };
});
save("run-view-checks.json", runViewChecks);
if (!runViewChecks.result_img) note("fail", "run view has no result figure");
if (!runViewChecks.charged_line) note("fail", "run view does not show the charged line");
if (runViewChecks.technical_count > 0 && !runViewChecks.technical_closed) note("fail", "technical details not collapsed by default");
if (runViewChecks.leaks_provider) note("fail", "run view leaks provider/debug wording");
await shot(page, "run-view", 1440);

// 选定这张 (free) then screenshot the selected state.
await page.setViewportSize({ width: 1440, height: 900 });
await page.goto(`${WEB}/start?project=${PROJECT}&plate_run=${charged?.run_id ?? ""}`);
await page.waitForLoadState("networkidle").catch(() => {});
const selectBtn = page.getByTestId("plate-select");
if (await selectBtn.isVisible().catch(() => false)) {
  if (!(await selectBtn.isDisabled().catch(() => true))) { await selectBtn.click(); await new Promise((r) => setTimeout(r, 1500)); }
  await shot(page, "run-view-selected", 1440);
}

// Export the selected candidate (existing asset, no charge).
const expBtn = page.getByTestId("plate-export");
let exportFact = { button_present: await expBtn.isVisible().catch(() => false) };
if (exportFact.button_present) {
  try {
    const dlPromise = page.waitForEvent("download", { timeout: 30_000 });
    await expBtn.click();
    const dl = await dlPromise;
    const fp = path.join(OUT, "workbench-export.zip");
    await dl.saveAs(fp);
    exportFact = { ...exportFact, saved: path.basename(fp), sha256: execFileSync("shasum", ["-a", "256", fp], { encoding: "utf8" }).split(" ")[0] };
  } catch (e) { exportFact.error = String(e).slice(0, 200); note("warn", "export download failed: " + exportFact.error); }
}
save("export-fact.json", exportFact);

// B4. 390: compare pair single column and no overflow on detail.
await page.setViewportSize({ width: 390, height: 844 });
await page.goto(`${WEB}/projects/${PROJECT}`);
await page.locator('section[data-revision="open"]').waitFor({ timeout: 30_000 });
await page.locator("#compare select").first().selectOption("V1");
await page.locator("#compare select").nth(1).selectOption("V2");
await page.getByRole("button", { name: "并排比较" }).click();
await page.locator("#compare .compare-pair img").first().waitFor({ timeout: 30_000 });
const pairCols = await page.evaluate(() => {
  const figs = [...document.querySelectorAll("#compare .compare-pair figure")].map((f) => f.getBoundingClientRect());
  return { figures: figs.length, same_column: figs.length === 2 && Math.abs(figs[0].x - figs[1].x) < 8 };
});
save("compare-390.json", pairCols);
if (!pairCols.same_column) note("fail", "compare pair is not single-column at 390");
await shot(page, "detail-390", 390);
await page.goto(`${WEB}/start`);
await page.waitForLoadState("networkidle").catch(() => {});
await shot(page, "start-390", 390);

// B5. Error page for a missing project.
await page.setViewportSize({ width: 1440, height: 900 });
await page.goto(`${WEB}/projects/proj_does_not_exist_0000`);
await page.waitForLoadState("networkidle").catch(() => {});
const errText = await page.evaluate(() => document.body.innerText);
save("missing-project-copy.txt", errText.slice(0, 300));
if (!/没有打开|不存在|找不到/.test(errText)) note("fail", "missing-project page lacks honest error copy");
await shot(page, "missing-project", 1440);

// B6. Flag off: closed rail copy; then restore. Restart recorded.
record.readyz_before = JSON.parse(ssh("curl -s http://127.0.0.1:32320/readyz")).build;
ssh("cd /var/lib/pilotseaview/product-image-plate-20261004 && sed -i \"s/^FEATURE_SELECTIVE_REVISION=1/FEATURE_SELECTIVE_REVISION=0/\" product-server.env && setsid bash start.sh > start.log 2>&1 < /dev/null; sleep 6");
await page.goto(`${WEB}/projects/${PROJECT}`);
await page.locator('section[data-revision="closed"]').waitFor({ timeout: 30_000 });
const closedCopy = await page.locator('section[data-revision="closed"]').innerText();
save("flag-off-copy.txt", closedCopy);
if (!/还没有打开。不会假装已经改好/.test(closedCopy)) note("fail", "flag-off closed copy not the honest wording");
if (/selective_revision|FEATURE_/i.test(closedCopy)) note("fail", "flag-off copy leaks internal flag names");
await shot(page, "flag-off-closed", 1440);
ssh("cd /var/lib/pilotseaview/product-image-plate-20261004 && sed -i \"s/^FEATURE_SELECTIVE_REVISION=0/FEATURE_SELECTIVE_REVISION=1/\" product-server.env && setsid bash start.sh > start.log 2>&1 < /dev/null; sleep 6");
record.readyz_after = JSON.parse(ssh("curl -s http://127.0.0.1:32320/readyz")).build;
save("readyz.json", { before: record.readyz_before, after: record.readyz_after });
if (record.readyz_before?.vcs_revision !== record.readyz_after?.vcs_revision) note("warn", "build identity changed across restart");

// B7. Revision API state for the record.
save("revisions-final.json", (await jget(`/api/projects/${PROJECT}/revisions`)).body);

record.finished_at = new Date().toISOString();
fs.writeFileSync(path.join(OUT, "walkthrough-record.json"), JSON.stringify(record, null, 1));
console.log(JSON.stringify({ ok: record.findings.every((f) => f.kind !== "fail"), findings: record.findings, anon_root: record.anon_root_lands_on }, null, 1));
await ctx.close();
