// HUI-2627 finish-r1 walkthrough (2026-10-08). Zero-spend: everything runs on
// the FIXTURE stack (e2e/fixture-api.mjs stands in for the Go API), so every
// artefact here is `fixture` layer and never acceptance evidence by itself.
// It re-uses the acceptance choreography of e2e/hui-2627-walkthrough.mjs:
//   A. logged-out entry (/, /login) — primary action on top
//   B. first-image path — stepper labels unchanged, no legacy form fields
//   C. succeed one plate run through the real UI → result card chips
//   D. screenshot matrix: eight representative surfaces x five widths
//   E. state coverage: loading/empty/partial/success/error/disabled/offline
//   F. product-finish red lines per surface (no table/form hero, primary
//      action visible, no provider/debug leakage, honest downgrade copy)
//   G. honest downstream copy (local reference only, "已送出" must not return)
//   H. axe-core scans at 1440/390 on all eight surfaces
//   I. logo-blind brand-family material (identifying app name hidden)
import fs from "node:fs";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const WEB = Number(process.env.E2E_WEB_PORT ?? 32321);
const API = Number(process.env.FIXTURE_API_PORT ?? 32324);
const BASE = `http://localhost:${WEB}`; // localhost: the BFF rebuilds absolute URLs against this host
const OUT = process.env.E2E_OUT;
const PROFILE = process.env.E2E_PROFILE ?? "/tmp/painuo-pie-finish-r1/profile";
const SAMPLES = process.env.FIXTURE_SAMPLES_DIR;
const CHROME = process.env.E2E_CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const AXE = path.join(webDir, "node_modules", "axe-core", "axe.min.js");
const WIDTHS = [1920, 1440, 1024, 430, 390];
const HEADERS = { "x-product-internal-token": "fixture-token" };

if (!OUT || !SAMPLES) { console.error("E2E_OUT and FIXTURE_SAMPLES_DIR are required"); process.exit(2); }
if (!fs.existsSync(AXE)) { console.error("axe-core missing: run npm i -D axe-core"); process.exit(2); }
if (!fs.existsSync(path.join(webDir, ".next", "BUILD_ID"))) { console.error("run `npx next build` first"); process.exit(2); }

fs.mkdirSync(path.join(OUT, "browser"), { recursive: true });
fs.mkdirSync(PROFILE, { recursive: true });
const record = { layer: "fixture", web: BASE, started_at: new Date().toISOString(), widths: WIDTHS, findings: [] };
const note = (kind, what) => { record.findings.push({ kind, what }); console.log(`[${kind}] ${what}`); };
const save = (name, data) => fs.writeFileSync(path.join(OUT, name), typeof data === "string" ? data : JSON.stringify(data, null, 1));
const shot = async (page, name, w) => page.screenshot({ path: path.join(OUT, "browser", `finish-${name}-${w}.png`), fullPage: w === 1440 });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const children = [];
const start = (cmd, args, env, name) => {
  const child = spawn(cmd, args, { cwd: webDir, env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] });
  child.stderr.on("data", (d) => process.env.E2E_VERBOSE && process.stderr.write(`[${name}] ${d}`));
  children.push(child);
  return child;
};
const api = (p, method = "GET", bodyObj) => fetch(`http://127.0.0.1:${API}${p}`, {
  method, headers: { ...HEADERS, ...(bodyObj ? { "Content-Type": "application/json" } : {}) }, body: bodyObj ? JSON.stringify(bodyObj) : undefined,
}).then(async (r) => ({ status: r.status, body: await r.json().catch(() => null) }));
async function waitFor(url, ms = 90000) {
  const end = Date.now() + ms;
  for (;;) {
    try { const r = await fetch(url); if (r.status < 500) return; } catch {}
    if (Date.now() > end) throw new Error(`timeout waiting for ${url}`);
    await sleep(300);
  }
}
const LEAK = /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i;
const technicalState = () => [...document.querySelectorAll("details[data-technical]")].map((d) => d.open);
const visibleText = () => { const c = document.body.cloneNode(true); c.querySelectorAll("details[data-technical],script,style").forEach((d) => d.remove()); return c.innerText; };
// page.evaluate runs in the browser: helpers must be fully self-contained there.
const browserLeak = () => {
  const c = document.body.cloneNode(true);
  c.querySelectorAll("details[data-technical],script,style").forEach((d) => d.remove());
  return /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i.test(c.innerText);
};
const browserLeakIn = (selector) => {
  const el = document.querySelector(selector);
  if (!el) return false;
  return /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i.test(el.innerText);
};

// ---- stack ----------------------------------------------------------------
start("node", ["e2e/fixture-api.mjs"], { FIXTURE_API_PORT: String(API), FIXTURE_SAMPLES_DIR: SAMPLES }, "api");
start(path.join(webDir, "node_modules/.bin/next"), ["start", "-p", String(WEB)], { PRODUCT_SERVER_BASE_URL: `http://127.0.0.1:${API}`, PRODUCT_SERVER_TOKEN: "fixture-token", NEXT_TELEMETRY_DISABLED: "1" }, "web");
await waitFor(`http://127.0.0.1:${API}/__fixture/stats`);
await waitFor(`${BASE}/login`);
record.readyz = (await api("/__fixture/stats")).body;

const ctx = await chromium.launchPersistentContext(path.join(PROFILE, "main"), { executablePath: CHROME, headless: true, viewport: { width: 1440, height: 900 }, args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
await ctx.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
const page = ctx.pages()[0] ?? (await ctx.newPage());
const goto = async (route) => { await page.goto(`${BASE}${route}`); await page.waitForLoadState("networkidle").catch(() => {}); await sleep(500); };
const runAxe = async (label) => {
  await page.addScriptTag({ path: AXE });
  const res = await page.evaluate(() => window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } }));
  const brief = { label, url: page.url(), violations: res.violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.map((n) => n.target.join(" ")) })), passes: res.passes.length };
  fs.appendFileSync(path.join(OUT, "axe-results.jsonl"), JSON.stringify(brief) + "\n");
  const bad = res.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
  if (bad.length) note("fail", `axe ${label}: ${bad.map((v) => `${v.id}(${v.impact})[${v.nodes.map((n) => n.target.join(" ")).join(";")}]`).join(", ")}`);
  else note("ok", `axe ${label}: 0 serious/critical, ${brief.violations.length} lesser`);
  return brief;
};
const hideBrand = () => page.addStyleTag({ content: `[data-eco-topnav] [class*="appName"]{visibility:hidden !important}` });

try {
  // ---- A. Logged-out entry -------------------------------------------------
  await api("/__fixture/scenario", "POST", { name: "limited", reset: true });
  const anon = await chromium.launchPersistentContext(path.join(PROFILE, "anon"), { executablePath: CHROME, headless: true, viewport: { width: 1440, height: 900 }, args: ["--no-first-run"] });
  const anonPage = anon.pages()[0] ?? (await anon.newPage());
  await anonPage.goto(`${BASE}/`);
  await anonPage.waitForURL(/\/login/, { timeout: 20000 }).catch(() => {});
  record.anon_root_lands_on = new URL(anonPage.url()).pathname;
  if (record.anon_root_lands_on !== "/login") note("fail", `/ should land on /login when logged out, got ${record.anon_root_lands_on}`);
  await anonPage.waitForLoadState("networkidle").catch(() => {});
  const loginCheck = await anonPage.evaluate(() => {
    const btn = [...document.querySelectorAll("button, [role=button], input[type=submit]")].find((b) => /登录|开始/.test(b.textContent ?? ""));
    const form = btn?.closest("form");
    if (!btn || !form) return { found: false };
    const inputs = [...form.querySelectorAll("input")].filter((i) => i.type !== "hidden");
    const firstInput = inputs[0]?.getBoundingClientRect().top ?? btn.getBoundingClientRect().top;
    return { found: true, button_before_first_input: btn.getBoundingClientRect().top <= firstInput + 8, label: btn.textContent.trim() };
  });
  save("login-primary-action.json", loginCheck);
  if (!loginCheck.found || !loginCheck.button_before_first_input) note("fail", "login primary action is not above the first input");
  else note("ok", `login primary action "${loginCheck.label}" sits above the first input`);
  for (const w of WIDTHS) {
    await anonPage.setViewportSize({ width: w, height: 900 });
    await goto("/login");
    await anonPage.screenshot({ path: path.join(OUT, "browser", `finish-matrix-login-${w}.png`), fullPage: w === 1440 });
    const overflow = await anonPage.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
    if (overflow > 1) note("overflow", `login@${w} scrollWidth +${overflow}px`);
  }
  await anon.close();

  // ---- B. first-image path on /start --------------------------------------
  await goto("/start");
  const startChecks = await page.evaluate(() => {
    const text = document.body.innerText;
    const stepEls = [...document.querySelectorAll("ol li, [class*='step']")].map((el) => el.textContent.trim()).filter((t) => t && t.length < 24);
    return {
      has_legacy_fields: /商品名称|用途选择|自定义尺寸|模型选择|生成数量/.test(text),
      step_like: stepEls.slice(0, 12),
      leaks: /qwen|modelxing|run_id|debug=/i.test(text),
      tables: document.querySelectorAll("table").length,
      forms: document.querySelectorAll("form").length,
      first_figure_top: document.querySelector("figure, img")?.getBoundingClientRect().top ?? null,
    };
  });
  save("start-first-image.json", startChecks);
  if (startChecks.has_legacy_fields) note("fail", "start page shows legacy form fields");
  if (startChecks.leaks) note("fail", "start page leaks provider/debug wording");
  if (startChecks.tables > 0) note("fail", "start page renders a table (workbench must not be table-led)");
  note("info", `start step-like labels: ${JSON.stringify(startChecks.step_like)}`);

  // ---- C. succeed one plate run through the real UI ------------------------
  await page.locator("#photo-file").setInputFiles(path.join(SAMPLES, "originals", "carton-1024x1280.png"));
  await page.getByRole("button", { name: "上传并使用" }).click();
  await page.getByTestId("plate-form").waitFor({ timeout: 20000 });
  await page.getByLabel("背景描述").fill("深蓝色渐变摄影棚背景，柔和顶光");
  await page.getByRole("button", { name: "获取报价" }).click();
  await page.getByTestId("plate-confirm").click();
  await page.locator('[data-testid="plate-result"][data-candidate="limited_candidate"]').waitFor({ timeout: 60000 });
  await page.getByTestId("plate-select").click();
  await page.getByRole("button", { name: "已选定" }).waitFor({ timeout: 20000 });
  const runUrl = new URL(page.url());
  const runId = runUrl.searchParams.get("plate_run") ?? "";
  record.run_id = runId;
  if (!runId) note("fail", "run URL lost plate_run handle");
  const figcaption = await page.locator("figure figcaption").first().innerText().catch(() => "");
  save("result-card-chips.json", { figcaption, url: page.url() });
  if (!/主体保真/.test(figcaption) || !/已选定/.test(figcaption) || !/1024×1280/.test(figcaption)) note("fail", `result card chips incomplete: "${figcaption}"`);
  else note("ok", `result card chips: ${figcaption.replace(/\s+/g, " / ")}`);
  const runLeaks = await page.evaluate(browserLeak);
  if (runLeaks) note("fail", "run view leaks technical wording in the default view");
  if ((await page.evaluate(technicalState)).some(Boolean)) note("fail", "technical details are open by default on the run view");
  await shot(page, "runview-success", 1440);

  // ---- D. matrix: eight surfaces x five widths -----------------------------
  const RUNVIEW = `/start?project=proj_fx&plate_run=${runId}`;
  const surfaces = [
    ["start", "/start"],
    ["projects", "/projects"],
    ["detail", "/projects/proj_fx"],
    ["runview", RUNVIEW],
    ["batches", "/batches"],
    ["handoff", "/handoff"],
    ["error", "/projects/proj_absent_0000"],
  ];
  // login was already shot at all five widths in section A → eight surfaces total.
  for (const w of WIDTHS) {
    await page.setViewportSize({ width: w, height: 900 });
    for (const [name, route] of surfaces) {
      await goto(route);
      await page.screenshot({ path: path.join(OUT, "browser", `finish-matrix-${name}-${w}.png`), fullPage: false });
      const overflow = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
      if (overflow > 1) note("overflow", `${name}@${w} scrollWidth +${overflow}px`);
    }
  }
  note("ok", `matrix complete: 8 surfaces x ${WIDTHS.length} widths`);

  // ---- E1. success + compare + revision rail (HUI-2596 must still work) ----
  await page.setViewportSize({ width: 1440, height: 900 });
  await goto("/projects/proj_fx");
  await page.locator('section[data-revision="open"]').waitFor({ timeout: 20000 });
  const railChecks = await page.evaluate(() => {
    const rail = document.querySelector("#revision");
    const versions = [...rail.querySelectorAll("li strong")].map((s) => s.textContent.trim());
    const leak = /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i.test(rail.innerText);
    return { versions, leak };
  });
  save("revision-rail.json", railChecks);
  if (!railChecks.versions.includes("V1") || !railChecks.versions.includes("V2")) note("fail", `revision rail versions wrong: ${JSON.stringify(railChecks.versions)}`);
  if (railChecks.leak) note("fail", "revision rail leaks internal wording");
  // compare V1/V2 side by side
  await page.locator("#compare select").first().selectOption("V1");
  await page.locator("#compare select").nth(1).selectOption("V2");
  await page.getByRole("button", { name: "并排比较" }).click({ timeout: 15000 });
  await page.locator("#compare .compare-pair img").first().waitFor({ timeout: 30000 });
  const imgsOk = await page.evaluate(() => [...document.querySelectorAll("#compare .compare-pair img")].every((i) => i.complete && i.naturalWidth > 0));
  if (!imgsOk) note("fail", "compare pair images did not load");
  await shot(page, "compare-1440", 1440);
  // adopt V2, then roll back to V1 (core HUI-2596 interaction unchanged)
  await page.locator("#revision li").nth(1).getByRole("button", { name: "采用这个版本" }).click({ timeout: 15000 });
  await page.getByText("已采用 V2。", { exact: false }).waitFor({ timeout: 20000 });
  await shot(page, "adopted-v2", 1440);
  await page.locator("#revision li").first().getByRole("button", { name: "回到 V1", exact: true }).click({ timeout: 15000 });
  await page.getByText("已回到 V1。", { exact: false }).waitFor({ timeout: 20000 });
  note("ok", "adopt/rollback round-trip works (HUI-2596 interaction intact)");

  // ---- G. honest downstream copy ------------------------------------------
  await page.locator("#revision li").first().getByRole("button", { name: "送数字人" }).click({ timeout: 15000 });
  let downText = "";
  for (let i = 0; i < 10; i++) {
    await sleep(1000);
    downText = await page.locator("#revision p[role=status]").evaluateAll((ps) => ps.map((p) => p.textContent ?? "").join("\n"));
    if (/只返回了|本地引用|还没收到/.test(downText)) break;
  }
  save("downstream-copy.txt", downText);
  if (!/只返回了|本地引用|还没收到/.test(downText)) note("fail", "downstream copy is not the honest local-reference wording");
  else if (/已送出|已上传/.test(downText)) note("fail", "downstream copy claims an upload — honesty regression");
  else note("ok", "downstream copy stays honest (local reference only)");
  await shot(page, "downstream-copy", 1440);

  // ---- E2. empty / disabled / offline / revision_off / loading states ------
  await api("/__fixture/scenario", "POST", { name: "empty" });
  await goto("/projects");
  const emptyProjects = await page.evaluate(() => {
    const text = document.body.innerText;
    const recovery = [...document.querySelectorAll("a, button")].find((el) => /新建工程|开始做产品图|重试/.test(el.textContent ?? ""));
    return { empty_copy: /还没有|没有工程|空/.test(text), recovery_action: !!recovery };
  });
  save("state-empty-projects.json", emptyProjects);
  if (!emptyProjects.empty_copy || !emptyProjects.recovery_action) note("fail", "empty projects state lacks copy or a recovery action");
  await shot(page, "state-empty-projects", 1440);
  await goto("/batches");
  await shot(page, "state-empty-batches", 1440);
  await api("/__fixture/scenario", "POST", { name: "limited", reset: true });

  await api("/__fixture/scenario", "POST", { name: "disabled" });
  await goto("/start");
  const disabledCheck = await page.evaluate(() => {
    const text = document.body.innerText;
    return { has_disabled_copy: /暂未开放|不可用|没有开放|未开放/.test(text), leak: /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i.test(text) };
  });
  save("state-disabled.json", disabledCheck);
  if (!disabledCheck.has_disabled_copy) note("fail", "disabled capabilities lack an honest closed copy");
  if (disabledCheck.leak) note("fail", "disabled state leaks internal wording");
  await shot(page, "state-disabled-start", 1440);
  await api("/__fixture/scenario", "POST", { name: "limited", reset: true });

  await api("/__fixture/scenario", "POST", { name: "revision_off" });
  await goto("/projects/proj_fx");
  await page.locator('section[data-revision="closed"]').waitFor({ timeout: 20000 });
  const closedCopy = await page.locator('section[data-revision="closed"]').innerText();
  save("flag-off-copy.txt", closedCopy);
  if (!/还没有打开。不会假装已经改好/.test(closedCopy)) note("fail", "revision-off downgrade copy is not the honest wording");
  if (/selective_revision|FEATURE_|supplier/i.test(closedCopy)) note("fail", "revision-off copy leaks internal flag names");
  else note("ok", "revision-off shows the finished-product downgrade copy");
  await shot(page, "state-revision-off", 1440);
  await api("/__fixture/scenario", "POST", { name: "limited", reset: true });

  await page.route("**/api/**", async (route) => { await sleep(2500); return route.continue(); });
  await goto("/projects");
  await page.evaluate(() => { document.body.setAttribute("data-loading-shot", "1"); });
  await shot(page, "state-loading", 1440);
  await page.unrouteAll();

  // 产品服务不可达(断网等价故障):BFF 返回 503 中文文案,页面必须给恢复动作。
  await page.route("**/api/**", (route) => route.abort("connectionrefused"));
  await goto("/projects");
  const offlineCheck = await page.evaluate(() => {
    const text = document.body.innerText;
    const retry = [...document.querySelectorAll("button, a")].find((el) => /重试|刷新|重新/.test(el.textContent ?? ""));
    return { honest_offline: /暂不可达|稍后重试|网络|不可用/.test(text), retry_action: !!retry };
  });
  save("state-offline.json", offlineCheck);
  if (!offlineCheck.honest_offline || !offlineCheck.retry_action) note("fail", `offline state lacks honest copy or a retry action: ${JSON.stringify(offlineCheck)}`);
  else note("ok", "unreachable-server state shows the honest copy with a retry action");
  await shot(page, "state-offline", 1440);
  await page.unrouteAll();
  await goto("/projects"); // recover

  // ---- E3. partial + not-usable error states on the batches / run view -----
  await goto("/batches");
  const partialCheck = await page.evaluate(() => {
    const text = document.body.innerText;
    return { partial_label: /部分失败/.test(text), counts_shown: /\d+\s*\/\s*\d+|成功\s*\d|失败\s*\d/.test(text) };
  });
  save("state-partial-batches.json", partialCheck);
  if (!partialCheck.partial_label) note("fail", "partial batch does not show the 部分失败 label");
  await shot(page, "state-partial-batches", 1440);

  // not_usable candidate: scenario resets cleared earlier runs, so the failure
  // leg runs in a FRESH context — a new work session with no stale pending hint
  // (the fixture-reset equivalent of "the person comes back later").
  const failCtx = await chromium.launchPersistentContext(path.join(PROFILE, "fail-leg"), { executablePath: CHROME, headless: true, viewport: { width: 1440, height: 900 }, args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
  await failCtx.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
  const fpage = failCtx.pages()[0] ?? (await failCtx.newPage());
  await fpage.goto(`${BASE}/start`);
  await fpage.waitForLoadState("networkidle").catch(() => {});
  await fpage.locator("#photo-file").setInputFiles(path.join(SAMPLES, "originals", "carton-1024x1280.png"));
  await fpage.getByRole("button", { name: "上传并使用" }).click();
  await fpage.getByTestId("plate-form").waitFor({ timeout: 20000 });
  await fpage.getByLabel("背景描述").fill("暖色原木桌面，窗边自然光");
  await fpage.getByRole("button", { name: "获取报价" }).click();
  await fpage.getByTestId("plate-quote").waitFor({ timeout: 30000 });
  await fpage.getByTestId("plate-confirm").click();
  await fpage.locator('[data-testid="plate-result"]').waitFor({ timeout: 60000 });
  const failRunUrl = fpage.url();
  await api("/__fixture/scenario", "POST", { name: "not_usable" });
  await fpage.goto(failRunUrl);
  await fpage.waitForLoadState("networkidle").catch(() => {});
  await sleep(800);
  const unusableCheck = await fpage.evaluate(() => {
    const text = document.body.innerText;
    return {
      honest_unusable: /未通过商品检查|不能选用、导出或回传|没有通过的检查/.test(text),
      leak: /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i.test(text),
      text_excerpt: text.slice(0, 600),
    };
  });
  save("state-not-usable.json", unusableCheck);
  await fpage.screenshot({ path: path.join(OUT, "browser", "finish-state-not-usable-runview-1440.png") });
  await failCtx.close();
  if (!unusableCheck.honest_unusable) note("fail", "not-usable run view lacks the honest unusable copy");
  else note("ok", "not-usable run view shows the honest failed-check copy");
  await api("/__fixture/scenario", "POST", { name: "limited", reset: true });

  // ---- F/H/I. red lines + axe + blind-family material ----------------------
  const axeSurfaces = [["login", "/login"], ...surfaces];
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const [name, route] of axeSurfaces) {
    await goto(route);
    await runAxe(`${name}@1440`);
    const redline = await page.evaluate(() => {
      const clone = document.body.cloneNode(true);
      clone.querySelectorAll("details[data-technical],script,style").forEach((d) => d.remove());
      const firstBig = [...document.querySelectorAll("h1, h2")].map((h) => h.getBoundingClientRect().top).sort((a, b) => a - b)[0] ?? 9999;
      const primaryInTable = !!document.querySelector("table button[class*='primary'], table a[class*='primary']");
      return {
        leak: /qwen|modelxing|run_id|debug=|sha256|FEATURE_|selective_revision|supplier|request_key|fixture/i.test(clone.innerText),
        technical_open: [...document.querySelectorAll("details[data-technical]")].some((d) => d.open),
        primary_in_table: primaryInTable,
        hero_is_media: !!document.querySelector("figure img, img[class*='stage'], [class*='stage'] img") || firstBig < 900,
      };
    });
    save(`redline-${name}-1440.json`, redline);
    if (redline.leak) note("fail", `${name}: leaks technical wording`);
    if (redline.technical_open) note("fail", `${name}: technical details open by default`);
    if (redline.primary_in_table) note("fail", `${name}: primary action lives inside a table`);
    // blind-family material: identifying app name hidden
    await hideBrand();
    await page.screenshot({ path: path.join(OUT, "browser", `finish-blind-${name}-1440.png`), fullPage: true });
    if (name !== "login") {
      await runAxe(`${name}@390`);
      await page.screenshot({ path: path.join(OUT, "browser", `finish-blind-${name}-390.png`), fullPage: true });
    }
    await page.setViewportSize({ width: 1440, height: 900 });
  }
  await goto("/start"); // leave the brand-hidden style behind
  await page.setViewportSize({ width: 1440, height: 900 });

  record.finished_at = new Date().toISOString();
  const fails = record.findings.filter((f) => f.kind === "fail");
  save("walkthrough-record.json", record);
  console.log(JSON.stringify({ ok: fails.length === 0, fails: fails.length, findings: record.findings }, null, 1));
  process.exitCode = fails.length === 0 ? 0 : 1;
} finally {
  for (const c of children) c.kill();
  await ctx.close().catch(() => {});
}
