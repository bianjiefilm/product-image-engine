// HUI-2627 fix2 evidence driver (2026-10-08). Zero-spend: FIXTURE stack only
// (e2e/fixture-api.mjs stands in for the Go API), same conventions as
// e2e/hui-2627-finish-r1.mjs (ports 32321/32324, E2E_OUT/FIXTURE_SAMPLES_DIR).
// Produces, into $E2E_OUT:
//   touch-targets.json         — per-control w/h at 390/430 (2626 同形态 + surface/pass)
//   browser/touch-targets-{w}.png
//   badges-projects-{w}.png    — 状态/来源中文徽章对照(fix-list 1)
//   state-offline-browser.json — REAL browser offline (context.setOffline):
//                                banner appears offline, hides back online
//   browser/fix2-state-offline-browser-{w}.png
import fs from "node:fs";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const WEB = Number(process.env.E2E_WEB_PORT ?? 32321);
const API = Number(process.env.FIXTURE_API_PORT ?? 32324);
const BASE = `http://localhost:${WEB}`;
const OUT = process.env.E2E_OUT;
const PROFILE = process.env.E2E_PROFILE ?? "/tmp/painuo-pie-fix2/profile";
const SAMPLES = process.env.FIXTURE_SAMPLES_DIR;
const CHROME = process.env.E2E_CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const WIDTHS = [430, 390];
const MIN_TARGET = 44; // WCAG 2.5.8 / 2626 同款 44px 下限

if (!OUT || !SAMPLES) { console.error("E2E_OUT and FIXTURE_SAMPLES_DIR are required"); process.exit(2); }
if (!fs.existsSync(path.join(webDir, ".next", "BUILD_ID"))) { console.error("run `npx next build` first"); process.exit(2); }

fs.mkdirSync(path.join(OUT, "browser"), { recursive: true });
fs.mkdirSync(PROFILE, { recursive: true });
const record = { layer: "fixture", web: BASE, started_at: new Date().toISOString(), widths: WIDTHS, min_target: MIN_TARGET, findings: [] };
const note = (kind, what) => { record.findings.push({ kind, what }); console.log(`[${kind}] ${what}`); };
const save = (name, data) => fs.writeFileSync(path.join(OUT, name), JSON.stringify(data, null, 1));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const children = [];
const start = (cmd, args, env, name) => {
  const child = spawn(cmd, args, { cwd: webDir, env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] });
  child.stderr.on("data", (d) => process.env.E2E_VERBOSE && process.stderr.write(`[${name}] ${d}`));
  children.push(child);
  return child;
};
const api = (p) => fetch(`http://127.0.0.1:${API}${p}`, { headers: { "x-product-internal-token": "fixture-token" } })
  .then(async (r) => ({ status: r.status, body: await r.json().catch(() => null) }));
async function waitFor(url, ms = 90000) {
  const end = Date.now() + ms;
  for (;;) {
    try { const r = await fetch(url); if (r.status < 500) return; } catch {}
    if (Date.now() > end) throw new Error(`timeout waiting for ${url}`);
    await sleep(300);
  }
}

// ---- stack ----------------------------------------------------------------
start("node", ["e2e/fixture-api.mjs"], { FIXTURE_API_PORT: String(API), FIXTURE_SAMPLES_DIR: SAMPLES }, "api");
start(path.join(webDir, "node_modules/.bin/next"), ["start", "-p", String(WEB)], { PRODUCT_SERVER_BASE_URL: `http://127.0.0.1:${API}`, PRODUCT_SERVER_TOKEN: "fixture-token", NEXT_TELEMETRY_DISABLED: "1" }, "web");
await waitFor(`http://127.0.0.1:${API}/__fixture/stats`);
await waitFor(`${BASE}/login`);
record.readyz = (await api("/__fixture/stats")).body;

const ctx = await chromium.launchPersistentContext(path.join(PROFILE, "main"), { executablePath: CHROME, headless: true, viewport: { width: 1440, height: 900 }, args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
await ctx.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
const page = ctx.pages()[0] ?? (await ctx.newPage());
const goto = async (route) => { await page.goto(`${BASE}${route}`); await page.waitForLoadState("networkidle").catch(() => {}); await sleep(400); };

// 交互控件全量测量:button/a/input/select/textarea/summary/[role=button]。
// label 包裹的 checkbox 命中区在 label 上,原生 input 记 1×1 例外(2626 同款)。
const SURFACES = [
  { name: "login", route: "/login", anon: false },
  { name: "start", route: "/start", anon: false },
  { name: "projects", route: "/projects", anon: false },
  { name: "project-detail", route: "/projects/proj_fx", anon: false },
  { name: "batches", route: "/batches", anon: false },
  { name: "handoff", route: "/handoff", anon: false },
];

try {
  // ---- 1. 徽章对照证据(fix-list 1:状态/来源中文徽章) ------------------------
  await api("/__fixture/scenario", "POST", { name: "limited", reset: true });
  await goto("/projects");
  const badgeCheck = await page.evaluate(() => {
    const text = document.body.innerText;
    return {
      no_raw_status_enum: !/\b(draft|active|archived)\b/.test(text),
      no_raw_source_enum: !/\bstandalone\b/.test(text),
      has_chinese_status: /草稿|进行中|已归档/.test(text),
      has_chinese_source: /独立制作/.test(text),
    };
  });
  save("badges-projects.json", badgeCheck);
  if (!badgeCheck.no_raw_status_enum || !badgeCheck.no_raw_source_enum) note("fail", `projects list still shows raw enums: ${JSON.stringify(badgeCheck)}`);
  else if (!badgeCheck.has_chinese_status || !badgeCheck.has_chinese_source) note("fail", `projects list lacks Chinese badges: ${JSON.stringify(badgeCheck)}`);
  else note("ok", "projects list shows Chinese status/source badges only");
  await page.setViewportSize({ width: 1440, height: 900 });
  await sleep(300);
  await page.screenshot({ path: path.join(OUT, "browser", "fix2-badges-projects-1440.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await sleep(300);
  await page.screenshot({ path: path.join(OUT, "browser", "fix2-badges-projects-390.png"), fullPage: true });

  // ---- 2. 触控目标逐控件测量(fix-list 2) ------------------------------------
  const touch = {};
  for (const width of WIDTHS) {
    touch[String(width)] = [];
    for (const surface of SURFACES) {
      await page.setViewportSize({ width, height: 932 });
      await goto(surface.route);
      const controls = await page.evaluate(({ min }) => {
        const selector = "button, a, input, select, textarea, summary, [role=button]";
        return [...document.querySelectorAll(selector)].filter((el) => {
          const r = el.getBoundingClientRect();
          const s = getComputedStyle(el);
          return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none";
        }).map((el) => {
          const r = el.getBoundingClientRect();
          const label = (el.getAttribute("aria-label") || el.textContent || el.getAttribute("placeholder") || el.getAttribute("value") || el.tagName).trim().replace(/\s+/g, " ").slice(0, 40);
          const inlineTextLink = el.tagName === "A" && el.parentElement && el.parentElement.tagName === "P";
          return {
            label: label || el.tagName,
            tag: el.tagName.toLowerCase(),
            cls: String(el.className && typeof el.className === "string" ? el.className : "").split(/\s+/).filter(Boolean).slice(0, 3).join(" "),
            w: Math.round(r.width),
            h: Math.round(r.height),
            min,
            pass: el.tagName === "INPUT" && el.type === "checkbox" ? "inline-exception"
              : inlineTextLink ? "inline-exception"
              : (r.width >= min && r.height >= min ? "pass" : "fail"),
          };
        });
      }, { min: MIN_TARGET });
      for (const c of controls) touch[String(width)].push({ surface: surface.name, ...c });
      await page.screenshot({ path: path.join(OUT, "browser", `fix2-touch-targets-${surface.name}-${width}.png`), fullPage: false });
    }
    const fails = touch[String(width)].filter((c) => c.pass === "fail");
    const exceptions = touch[String(width)].filter((c) => c.pass === "inline-exception");
    note(fails.length ? "fail" : "ok", `@${width}: ${touch[String(width)].length} controls, ${fails.length} below ${MIN_TARGET}px, ${exceptions.length} inline exceptions`);
    if (fails.length) for (const f of fails.slice(0, 12)) console.log(`    [miss] ${f.surface} <${f.tag} class="${f.cls}"> "${f.label}" ${f.w}x${f.h}`);
  }
  save("touch-targets.json", touch);

  // ---- 3. 真实浏览器离线(fix-list 4:第三层口径) ------------------------------
  await page.setViewportSize({ width: 1440, height: 900 });
  await goto("/projects");
  await ctx.setOffline(true);
  await sleep(600); // 等 offline 事件横幅
  const offlineCheck = await page.evaluate(() => {
    const banner = document.querySelector(".offline-banner");
    return {
      banner_visible: !!banner,
      copy: banner ? banner.textContent.trim().slice(0, 80) : "",
      has_retry: !!document.querySelector('[data-testid="offline-retry"]'),
    };
  });
  await page.screenshot({ path: path.join(OUT, "browser", "fix2-state-offline-browser-1440.png"), fullPage: false });
  await ctx.setOffline(false);
  await sleep(600);
  const onlineCheck = await page.evaluate(() => ({ banner_gone: !document.querySelector(".offline-banner") }));
  save("state-offline-browser.json", { offline: offlineCheck, back_online: onlineCheck });
  if (!offlineCheck.banner_visible || !offlineCheck.has_retry) note("fail", `real-browser offline banner missing: ${JSON.stringify(offlineCheck)}`);
  else if (!onlineCheck.banner_gone) note("fail", "offline banner did not clear after network restored");
  else note("ok", "real-browser offline: banner shows with retry, clears on recovery");
} finally {
  for (const child of children) child.kill();
  await ctx.close().catch(() => {});
}

fs.writeFileSync(path.join(OUT, "fix2-run-record.json"), JSON.stringify(record, null, 1));
const fails = record.findings.filter((f) => f.kind === "fail");
console.log(fails.length ? `DONE with ${fails.length} fail findings` : "DONE all ok");
process.exit(0);
