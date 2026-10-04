// FIXTURE-STACK browser run. Drives the real Next.js app in a real headless
// Chrome (own profile, own ports) against e2e/fixture-api.mjs. Everything it
// produces is labelled `fixture`; it proves UI states, never real generation.
import { spawn, execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const WEB = Number(process.env.E2E_WEB_PORT ?? 32321), API = Number(process.env.FIXTURE_API_PORT ?? 32324);
const OUT = required("E2E_OUT"), PROFILE = required("E2E_PROFILE"), SAMPLES = required("FIXTURE_SAMPLES_DIR");
const CHROME = process.env.E2E_CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
// Browse via "localhost": the source BFF compares the request Origin with the URL Next rebuilds, which is localhost.
const BASE = `http://localhost:${WEB}`;
const VIEWPORTS = { desktop: { width: 1280, height: 800 }, mobile: { width: 390, height: 844 } };
const results = [];
const children = [];

function required(name) { const v = process.env[name]; if (!v) { console.error(`${name} is required`); process.exit(2); } return v; }
function crc(buf) { let c, crcv = 0xffffffff; for (let n = 0; n < buf.length; n++) { c = (crcv ^ buf[n]) & 0xff; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; crcv = (crcv >>> 8) ^ c; } return (crcv ^ 0xffffffff) >>> 0; }
function chunk(type, data) { const len = Buffer.alloc(4); len.writeUInt32BE(data.length); const td = Buffer.concat([Buffer.from(type), data]); const c = Buffer.alloc(4); c.writeUInt32BE(crc(td)); return Buffer.concat([len, td, c]); }
function tinyPng() { const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(2, 0); ihdr.writeUInt32BE(2, 4); ihdr[8] = 8; ihdr[9] = 2; const raw = Buffer.from([0, 200, 30, 30, 200, 30, 30, 0, 30, 200, 30, 30, 200, 30]); return Buffer.concat([Buffer.from("89504e470d0a1a0a", "hex"), chunk("IHDR", ihdr), chunk("IDAT", zlib.deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]); }

function start(cmd, args, env, name) {
  const child = spawn(cmd, args, { cwd: webDir, env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] });
  child.stdout.on("data", (d) => process.env.E2E_VERBOSE && process.stdout.write(`[${name}] ${d}`));
  child.stderr.on("data", (d) => process.env.E2E_VERBOSE && process.stderr.write(`[${name}] ${d}`));
  children.push(child);
  return child;
}
async function waitFor(url, ms = 60000) { const end = Date.now() + ms; for (;;) { try { const r = await fetch(url); if (r.status < 500) return; } catch {} if (Date.now() > end) throw new Error(`timeout waiting for ${url}`); await new Promise((r) => setTimeout(r, 300)); } }
const api = (p, body) => fetch(`http://127.0.0.1:${API}${p}`, body ? { method: "POST", body: JSON.stringify(body) } : {}).then((r) => r.json());

async function open(name, vp) {
  const dir = path.join(PROFILE, `${name}-${vp}`);
  fs.rmSync(dir, { recursive: true, force: true });
  const context = await chromium.launchPersistentContext(dir, { executablePath: CHROME, headless: true, viewport: VIEWPORTS[vp], args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
  await context.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
  const page = context.pages()[0] ?? (await context.newPage());
  return { context, page };
}
const shot = (page, name, vp) => page.screenshot({ path: path.join(OUT, `fixture-${name}-${vp}.png`), fullPage: true });
const noHorizontalScroll = (page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1);
const visibleText = (page) => page.evaluate(() => { const clone = document.body.cloneNode(true); clone.querySelectorAll("details").forEach((d) => d.remove()); clone.querySelectorAll("script,style").forEach((d) => d.remove()); return clone.innerText; });

async function run(name, vps, fn) {
  for (const vp of vps) {
    const label = `${name}@${vp}`;
    const { context, page } = await open(name, vp);
    try { await fn({ page, context, vp }); results.push({ name: label, layer: "fixture", ok: true }); console.log(`PASS ${label}`); }
    catch (e) { results.push({ name: label, layer: "fixture", ok: false, error: String(e.message ?? e) }); console.log(`FAIL ${label}: ${e.message}`); try { await shot(page, `${name}-FAILED`, vp); } catch {} }
    finally { await context.close(); }
  }
}

const frozenCarton = path.join(SAMPLES, "originals", "carton-1024x1280.png");
async function chooseFrozen(page) { await page.locator("#photo-file").setInputFiles(frozenCarton); await page.getByRole("button", { name: "上传并使用" }).click(); await page.getByTestId("plate-form").waitFor({ timeout: 20000 }); }

async function main() {
  fs.mkdirSync(OUT, { recursive: true });
  for (const f of fs.readdirSync(OUT)) if (f.startsWith("fixture-")) fs.rmSync(path.join(OUT, f));
  start("node", ["e2e/fixture-api.mjs"], { FIXTURE_API_PORT: String(API), FIXTURE_SAMPLES_DIR: SAMPLES }, "api");
  start(path.join(webDir, "node_modules/.bin/next"), ["start", "-p", String(WEB)], { PRODUCT_SERVER_BASE_URL: `http://127.0.0.1:${API}`, PRODUCT_SERVER_TOKEN: "fixture-token", NEXT_TELEMETRY_DISABLED: "1" }, "web");
  await waitFor(`http://127.0.0.1:${API}/__fixture/stats`);
  await waitFor(`${BASE}/login`);

  await run("start-empty", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    await page.getByRole("heading", { name: "开始做产品图" }).waitFor();
    await page.locator("#photo-file").waitFor();
    assert.equal(await page.getByRole("button", { name: "选择商品照片" }).count(), 0, "the dead disabled button must be gone");
    const text = await visibleText(page);
    for (const banned of ["商品照片编号", "Task", "NOT_RUN", "UNKNOWN", "provider", "hash"]) assert.ok(!text.includes(banned), `developer wording on /start: ${banned}`);
    assert.ok(await noHorizontalScroll(page), "horizontal scroll");
    await shot(page, "start-empty", vp);
  });

  await run("photo-not-in-range", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    const png = path.join(PROFILE, "tiny.png"); fs.writeFileSync(png, tinyPng());
    await page.locator("#photo-file").setInputFiles(png);
    await page.getByRole("button", { name: "上传并使用" }).click();
    await page.getByTestId("plate-no-photo").waitFor({ timeout: 20000 });
    assert.equal(await page.getByTestId("plate-form").count(), 0, "no paid form for a photo outside the verified range");
    assert.equal((await api("/__fixture/stats")).creates, 0);
    await shot(page, "photo-not-in-range", vp);
  });

  await run("limited-flow-keyboard", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    const bg = page.getByLabel("背景描述");
    await bg.focus();
    await page.keyboard.type("深蓝色渐变摄影棚背景，柔和顶光");
    await page.keyboard.press("Tab");
    assert.equal(await page.evaluate(() => document.activeElement?.textContent), "获取报价", "Tab reaches the quote button");
    await page.keyboard.press("Enter");
    await page.getByTestId("plate-quote").waitFor();
    await page.getByText("¥0.25").first().waitFor();
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("data-testid")), "plate-confirm", "focus moves to the confirm button");
    const ring = await page.evaluate(() => { const s = getComputedStyle(document.activeElement); return `${s.outlineStyle} ${s.outlineWidth}`; });
    assert.ok(ring.startsWith("solid") && !ring.endsWith(" 0px"), `visible focus ring: ${ring}`);
    await shot(page, "quote", vp);
    await page.keyboard.press("Enter");
    await page.locator('[data-testid="plate-result"][data-candidate="limited_candidate"]').waitFor({ timeout: 40000 });
    // The composite lives in the stage figure next to the checks section (media-first
    // layout), so address the image by its stable alt text, not by the section.
    assert.ok(await page.getByRole("img", { name: "换背景后的商品图" }).evaluate((img) => img.complete && img.naturalWidth > 0), "the result image really loads");
    assert.ok(await page.getByText("商品像素与原图完全一致").first().isVisible());
    assert.ok(await page.getByText("使用限制").first().isVisible());
    await page.getByTestId("plate-select").click();
    await page.getByRole("button", { name: "已选定" }).waitFor();
    assert.match(await page.getByTestId("plate-export").getAttribute("href"), /\/export$/);
    assert.ok(await noHorizontalScroll(page), "horizontal scroll");
    const text = await visibleText(page);
    for (const banned of ["Task", "NOT_RUN", "UNKNOWN", "sha256", "provider"]) assert.ok(!text.includes(banned), `internal word in the primary view: ${banned}`);
    await shot(page, "result-limited", vp);
    const stats = await api("/__fixture/stats");
    assert.equal(stats.creates, 1); assert.equal(stats.confirms, 1);
  });

  await run("refresh-and-relogin-restore-same-run", ["desktop"], async ({ page, context, vp }) => {
    await api("/__fixture/scenario", { name: "slow", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    await page.getByLabel("背景描述").fill("暖色原木桌面，窗边自然光");
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-confirm").click();
    await page.getByText("正在生成背景并检查商品").first().waitFor();
    const url = page.url();
    assert.match(url, /plate_run=sir_/);
    await page.reload();
    await page.getByText("正在生成背景并检查商品").first().waitFor({ timeout: 20000 });
    await context.clearCookies();
    await context.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
    await page.goto(url);
    await page.getByText("正在生成背景并检查商品").first().waitFor({ timeout: 20000 });
    await shot(page, "restored-pending", vp);
    const stats = await api("/__fixture/stats");
    assert.equal(stats.creates, 1, "refresh and re-login must not create a second run"); assert.equal(stats.confirms, 1);
    await api("/__fixture/advance", {});
    await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 30000 });
  });

  await run("not-usable-candidate", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "not_usable", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    await page.getByLabel("背景描述").fill("纯白背景");
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-confirm").click();
    const result = page.locator('[data-testid="plate-result"][data-candidate="not_usable_candidate"]');
    await result.waitFor({ timeout: 40000 });
    assert.ok(await result.getByRole("alert").first().isVisible());
    assert.ok(await result.getByText("背景相比原图有明显变化").first().isVisible());
    assert.equal(await page.getByTestId("plate-select").isDisabled(), true);
    assert.equal(await page.getByTestId("plate-export").count(), 0);
    await shot(page, "result-not-usable", vp);
  });

  await run("errors-and-disabled", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "create_503", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    await page.getByLabel("背景描述").fill("深蓝");
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-notice").waitFor({ timeout: 20000 });
    assert.equal(await page.getByTestId("plate-confirm").count(), 0);
    await shot(page, "error-create", vp);
    await api("/__fixture/scenario", { name: "disabled" });
    await page.goto(`${BASE}/start?project=${new URL(page.url()).searchParams.get("project")}`);
    await page.getByTestId("plate-unavailable").waitFor({ timeout: 20000 });
    assert.ok((await page.getByTestId("plate-unavailable").innerText()).includes("暂未开放"));
    assert.equal(await page.getByRole("button", { name: "获取报价" }).count(), 0);
    await shot(page, "disabled", vp);
  });

  await run("capability-matrix", ["desktop"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    const matrix = page.getByTestId("capability-matrix");
    await matrix.waitFor();
    for (const id of ["creative_background", "light_scene", "showcase_video", "reference_edit", "first_image"]) assert.equal(await matrix.locator(`[data-mode="${id}"]`).getAttribute("data-state"), "not_opened");
    assert.equal(await matrix.locator("button").count(), 0);
    await shot(page, "capability-matrix", vp);
  });
}

let failed = false;
try { await main(); } catch (e) { console.error(e); failed = true; }
finally {
  for (const c of children) c.kill("SIGTERM");
  let sha = "unknown"; try { sha = execFileSync("git", ["rev-parse", "HEAD"], { cwd: webDir }).toString().trim(); } catch {}
  fs.writeFileSync(path.join(OUT, "fixture-run-record.json"), JSON.stringify({ layer: "fixture", note: "UI states against a fake API; not acceptance evidence", frontend_git_sha: sha, chrome: CHROME, results }, null, 2));
}
if (failed || results.some((r) => !r.ok)) process.exit(1);
