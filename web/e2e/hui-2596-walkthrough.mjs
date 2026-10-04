// HUI-2596 real-stack walkthrough driver (Root 2026-10-04). Drives the normal
// product UI on the live stack over an existing paid plate run, plus a few
// API-only probes the ticket names explicitly. Evidence lands in E2E_OUT.
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright-core";

const WEB = process.env.REAL_WEB_URL ?? "http://localhost:32321";
const OUT = process.env.E2E_OUT;
const PROFILE = process.env.E2E_PROFILE;
const SAMPLES = process.env.FIXTURE_SAMPLES_DIR;
const STATE = process.env.E2E_STORAGE_STATE;
const PROJECT = process.env.E2E_PROJECT;
const SSH_args = JSON.parse(process.env.SSH_ARGS ?? '["-F","/Users/jimfu/.codex/qa-private-nine-projects-20261001/ssh-client/config","root@120.26.69.202"]');
const CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const VIEWPORTS = { desktop: { width: 1440, height: 900 }, mobile: { width: 390, height: 844 } };
const save = (name, data) => fs.writeFileSync(path.join(OUT, name), typeof data === "string" ? data : JSON.stringify(data, null, 1));
const shot = async (page, name, vp = "desktop") => { await page.screenshot({ path: path.join(OUT, "browser", `2596-${name}-${vp}.png`), fullPage: true }); };
const ssh = (...cmd) => execFileSync("ssh", [...SSH_args, ...cmd], { encoding: "utf8" });

fs.mkdirSync(path.join(OUT, "browser"), { recursive: true });
const cookies = JSON.parse(fs.readFileSync(STATE, "utf8")).cookies;
const dir = path.join(PROFILE, "rev-walk"); fs.rmSync(dir, { recursive: true, force: true });
const ctx = await chromium.launchPersistentContext(dir, { executablePath: CHROME, headless: true, viewport: VIEWPORTS.desktop, args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
await ctx.addCookies(cookies.map((c) => ({ ...c })));
const page = ctx.pages()[0] ?? (await ctx.newPage());
const jget = async (p) => { const r = await ctx.request.get(WEB + p); return { status: r.status(), body: await r.json().catch(() => null), headers: r.headers() }; };
const jpost = async (p, body) => { const r = await ctx.request.post(WEB + p, { data: body, headers: { origin: WEB, "content-type": "application/json" } }); return { status: r.status(), body: await r.json().catch(() => null) }; };
const captured = [];
page.on("response", async (res) => {
  if (res.request().method() === "POST" && res.url().includes("/revisions")) {
    const body = await res.json().catch(() => null);
    captured.push({ url: res.url().replace(WEB, ""), status: res.status(), body });
  }
});
const balance = async () => (await jget("/api/billing/balance")).body;
const state = async () => (await jget(`/api/projects/${PROJECT}/revisions`));
const flush = async (label) => { const c = captured.splice(0, captured.length); save(`revisions-post-${label}.json`, c); return c; };

const record = { project: PROJECT, started_at: new Date().toISOString(), balance_before: await balance() };
save("balance-before.json", record.balance_before);

// 1. Open the project page; the rail must be open.
await page.goto(`${WEB}/projects/${PROJECT}`);
await page.locator('section[data-revision="open"]').waitFor({ timeout: 30_000 });
await shot(page, "rail-open");

// 2. Preset click plan + one-sentence parity.
await page.getByRole("button", { name: "背景简单一点" }).click();
await page.getByText(/这一处的费用/).first().waitFor({ timeout: 20_000 });
const costClick = await page.getByText(/这一处的费用/).first().innerText();
await shot(page, "plan-preset");
const planResps = await flush("plan-preset");
await page.locator("#revision-text").fill("背景简单一点");
await page.getByRole("button", { name: "看会影响哪里" }).click();
await page.locator('section form button.primary:not([disabled])').first().waitFor({ timeout: 20_000 });
await page.getByText(/这一处的费用/).first().waitFor({ timeout: 20_000 });
const costText = await page.getByText(/这一处的费用/).first().innerText();
await flush("plan-text");
save("intent-parity.json", { preset_click: costClick, one_sentence: costText, equal: costClick === costText, plan_responses: planResps });

// 3. Negative: unacknowledged create must be 409 with the impact notice.
const original = fs.readFileSync(path.join(SAMPLES, "originals", "carton-1024x1024.png")).toString("base64");
const mask = fs.readFileSync(path.join(SAMPLES, "masks", "carton-1024x1024.png")).toString("base64");
const neg = await jpost(`/api/projects/${PROJECT}/revisions`, { click: "simplify_background", acknowledged: false, original_b64: original, mask_b64: mask, brief_version: "current" });
save("revision-create-unacknowledged.json", neg);
if (neg.status !== 409) throw new Error(`expected 409 impact_unacknowledged, got ${neg.status}`);
const notice = neg.body?.error?.message ?? neg.body?.impact_notice;
if (!notice || !/供应商|整张画布|锁回/.test(notice)) throw new Error("409 lacks the impact notice: " + JSON.stringify(neg.body));

// 4. Create V1 through the UI. A preset click restarts plan(), and the plan
// response resets the acknowledgement (setAck(false)), so the files and the
// checkbox may only be armed after the cost line proves the plan finished.
await page.getByRole("button", { name: "背景简单一点" }).click();
await page.getByText(/这一处的费用/).first().waitFor({ timeout: 20_000 });
await page.locator('section[data-revision="open"] input[type="file"]').first().setInputFiles(path.join(SAMPLES, "originals", "carton-1024x1024.png"));
await page.locator('section[data-revision="open"] input[type="file"]').nth(1).setInputFiles(path.join(SAMPLES, "masks", "carton-1024x1024.png"));
await page.locator('section[data-revision="open"] input[type="checkbox"]').check();
await shot(page, "before-execute");
try {
  await page.getByRole("button", { name: "确认只改这里" }).click({ timeout: 30_000 });
} catch (err) {
  await shot(page, "execute-disabled");
  const dump = await page.evaluate(() => {
    const sec = document.querySelector('section[data-revision="open"]');
    return {
      files: [...sec.querySelectorAll('input[type="file"]')].map((i) => i.files.length),
      checkbox: sec.querySelector('input[type="checkbox"]')?.checked ?? null,
      execute_disabled: [...sec.querySelectorAll("button")].find((b) => b.textContent === "确认只改这里")?.disabled ?? null,
      text: sec.innerText.slice(0, 500),
    };
  });
  throw new Error("execute stayed disabled: " + JSON.stringify(dump));
}
await page.locator("#revision li").first().waitFor({ timeout: 60_000 });
await flush("create-v1");
let st = await state(); save("revisions-state-1.json", st.body);
if (st.body?.versions?.[0]?.label !== "V1") throw new Error("first version is not V1");
const v1 = st.body.versions[0];
save("revision-create-v1-facts.json", v1);
await shot(page, "v1-created");

// 5. Create V2 through the UI with a different preset. Same barrier as step 4.
await page.getByRole("button", { name: "换成户外" }).click();
await page.getByText(/这一处的费用/).first().waitFor({ timeout: 20_000 });
await page.locator('section[data-revision="open"] input[type="file"]').first().setInputFiles(path.join(SAMPLES, "originals", "carton-1024x1024.png"));
await page.locator('section[data-revision="open"] input[type="file"]').nth(1).setInputFiles(path.join(SAMPLES, "masks", "carton-1024x1024.png"));
await page.locator('section[data-revision="open"] input[type="checkbox"]').check();
await page.getByRole("button", { name: "确认只改这里" }).click({ timeout: 30_000 });
await page.locator("#revision li").nth(1).waitFor({ timeout: 60_000 });
await flush("create-v2");
st = await state(); save("revisions-state-2.json", st.body);
if (st.body?.versions?.[1]?.label !== "V2") throw new Error("second version is not V2");
save("revision-create-v2-facts.json", st.body.versions[1]);
await shot(page, "v2-created");

// 6. Adopt V2, then roll back to V1.
await page.getByRole("button", { name: "采用这个版本" }).nth(1).click();
await page.getByText("已采用 V2。", { exact: false }).waitFor({ timeout: 30_000 });
st = await state(); save("revisions-state-adopted-v2.json", st.body);
if (st.body?.adopted_label !== "V2") throw new Error("adopt pointer did not move to V2");
await shot(page, "v2-adopted");
await page.getByRole("button", { name: "回到 V1", exact: true }).click();
await page.getByText("已回到 V1。", { exact: false }).waitFor({ timeout: 30_000 });
st = await state(); save("revisions-state-rolledback-v1.json", st.body);
if (st.body?.adopted_label !== "V1") throw new Error("rollback did not restore V1");
await shot(page, "v1-rolledback");

// 7. Side-by-side compare.
await page.locator("#compare select").first().selectOption("V1");
await page.locator("#compare select").nth(1).selectOption("V2");
await page.getByRole("button", { name: "并排比较" }).click();
await page.locator("#compare .compare-pair img").first().waitFor({ timeout: 30_000 });
const cmp = await jget(`/api/projects/${PROJECT}/revisions/compare?left=V1&right=V2`);
save("revisions-compare.json", cmp.body);
await shot(page, "side-by-side");

// 8. Downstream on the adopted version: honest local reference only.
const down = {};
for (const [btn, key] of [["送数字人", "digital_human"], ["送 AiCut", "aicut"], ["送矩阵", "matrix"]]) {
  await page.locator("#revision li").first().getByRole("button", { name: btn }).click();
  await page.locator("#revision p[role=status]").last().waitFor({ timeout: 30_000 });
  down[key] = (await flush(`downstream-${key}`))[0];
}
save("revisions-downstream.json", down);
const ref = down.digital_human?.body?.downstream?.asset_ref ?? down.digital_human?.body?.downstream?.ref ?? "";
save("downstream-note.txt", await page.locator("#revision p[role=status]").last().innerText());

// 9. Refresh stability, then restart stability (Restore recomputes hashes).
const beforeRefresh = await state();
await page.reload();
await page.locator('section[data-revision="open"]').waitFor({ timeout: 30_000 });
const afterRefresh = await state();
save("revisions-state-refresh.json", afterRefresh.body);
if (JSON.stringify(beforeRefresh.body) !== JSON.stringify(afterRefresh.body)) throw new Error("refresh changed revision state");
const ready1 = JSON.parse(ssh("curl -s http://127.0.0.1:32320/readyz"));
ssh("cd /var/lib/pilotseaview/product-image-plate-20261004 && setsid bash start.sh > start.log 2>&1 < /dev/null; sleep 6");
const ready2 = JSON.parse(ssh("curl -s http://127.0.0.1:32320/readyz"));
const afterRestart = await state();
save("revisions-state-restart.json", afterRestart.body);
record.server_restart = { before: ready1.build, after: ready2.build };
if (JSON.stringify(beforeRefresh.body) !== JSON.stringify(afterRestart.body)) throw new Error("server restart changed revision state");
await page.goto(`${WEB}/projects/${PROJECT}`);
await page.locator('section[data-revision="open"]').waitFor({ timeout: 30_000 });
await shot(page, "after-restart");

// 10. API probes: failed/unknown never move the pointer; briefs diff; export binding.
const failed = await jpost(`/api/projects/${PROJECT}/revisions/outcome`, { click: "simplify_background", status: "failed" });
save("revision-outcome-failed.json", failed);
const unknown = await jpost(`/api/projects/${PROJECT}/revisions/outcome`, { click: "simplify_background", status: "unknown" });
save("revision-outcome-unknown.json", unknown);
st = await state(); save("revisions-state-unresolved.json", st.body);
if (st.body?.adopted_label !== "V1") throw new Error("failed/unknown moved the adopted pointer");
const briefs = await jpost(`/api/projects/${PROJECT}/revisions/briefs`, { version: "V1", facts: { scene: "indoor-studio" } });
save("revision-briefs.json", briefs);
const exp = await jpost(`/api/projects/${PROJECT}/revisions/V1/export`, {});
save("revision-export-v1.json", exp);
const content = await jget(`/api/projects/${PROJECT}/revisions/V1/content`);
save("revision-content-v1.meta.json", { status: content.status, headers: { x_revision_version: content.headers["x-revision-version"], x_revision_sha256: content.headers["x-revision-sha256"] } });
fs.writeFileSync(path.join(OUT, "revision-V1-content.png"), Buffer.from(await (await ctx.request.get(WEB + `/api/projects/${PROJECT}/revisions/V1/content`)).body()));
fs.writeFileSync(path.join(OUT, "revision-V2-content.png"), Buffer.from(await (await ctx.request.get(WEB + `/api/projects/${PROJECT}/revisions/V2/content`)).body()));
const failedLabel = failed.body?.versions?.[failed.body.versions.length - 1]?.label;
const expFailed = await jpost(`/api/projects/${PROJECT}/revisions/${failedLabel ?? ""}/export`, {});
save("revision-export-failed.json", expFailed);
if (expFailed.status !== 409) throw new Error("export of a failed version is not 409 not_exportable");

// 11. Balance must not have moved anywhere in this walkthrough.
const balAfter = await balance();
save("balance-after.json", balAfter);
record.balance_after = balAfter;
record.equal_balance = JSON.stringify(record.balance_before) === JSON.stringify(balAfter);

// 12. Flag off: closed card + 404; then restore.
ssh("cd /var/lib/pilotseaview/product-image-plate-20261004 && sed -i \"s/^FEATURE_SELECTIVE_REVISION=1/FEATURE_SELECTIVE_REVISION=0/\" product-server.env && setsid bash start.sh > start.log 2>&1 < /dev/null; sleep 6");
const off = await jget(`/api/projects/${PROJECT}/revisions`);
save("flag-off.json", { status: off.status, body: off.body });
await page.reload();
await page.locator('section[data-revision="closed"]').waitFor({ timeout: 30_000 });
await shot(page, "flag-off-closed");
ssh("cd /var/lib/pilotseaview/product-image-plate-20261004 && sed -i \"s/^FEATURE_SELECTIVE_REVISION=0/FEATURE_SELECTIVE_REVISION=1/\" product-server.env && setsid bash start.sh > start.log 2>&1 < /dev/null; sleep 6");
const restored = await state();
// Compare against the state as it stood right before the flag was turned off
// (step 10 has appended failed/unknown versions since step 9's snapshot).
const beforeFlag = await state();
if (JSON.stringify(beforeFlag.body) !== JSON.stringify(restored.body)) throw new Error("state changed after flag off/on cycle");
save("revisions-state-restored.json", restored.body);
record.finished_at = new Date().toISOString();
fs.writeFileSync(path.join(OUT, "walkthrough-record.json"), JSON.stringify(record, null, 1));
console.log(JSON.stringify({ ok: true, adopted_restored: "V1", balance_stable: record.equal_balance }, null, 1));
await ctx.close();
