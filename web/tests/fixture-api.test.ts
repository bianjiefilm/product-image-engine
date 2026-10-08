// HUI-2627 finish-r1:fixture-api 八代表页缺口端点契约测试。
// 派生 fixture-api.mjs 子进程(冻结样本目录),断言新增端点的状态码、
// 形状与诚实字段(downstream 只返回本地引用,requires_reupload=false,
// plan 永远 calls_supplier=false 且 supplier_note 诚实)。全部零扣费。
import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it } from "vitest";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const SAMPLES = path.join(webDir, "..", "fixtures", "frozen-samples", "v1");
const PORT = 32384 + Math.floor(Math.random() * 40);
const BASE = `http://127.0.0.1:${PORT}`;

let child: ReturnType<typeof spawn> | null = null;

async function waitUp(ms = 15000): Promise<void> {
  const end = Date.now() + ms;
  for (;;) {
    try {
      const r = await fetch(`${BASE}/__fixture/stats`);
      if (r.ok) return;
    } catch {}
    if (Date.now() > end) throw new Error("fixture-api did not come up");
    await new Promise((r) => setTimeout(r, 200));
  }
}

async function start(): Promise<void> {
  child = spawn(process.execPath, [path.join(webDir, "e2e", "fixture-api.mjs")], {
    env: { ...process.env, FIXTURE_API_PORT: String(PORT), FIXTURE_SAMPLES_DIR: SAMPLES },
    stdio: "ignore",
  });
  await waitUp();
}

// 与 Next BFF 完全同构的两道头:内部凭据 + 用户 Bearer(fixture-access)。
const HEADERS = { "x-product-internal-token": "fixture-token", authorization: "Bearer fixture-access" };
const jget = (p: string) => fetch(`${BASE}${p}`, { headers: HEADERS }).then(async (r) => ({ status: r.status, body: await r.json().catch(() => null) }));
const jpost = (p: string, body?: unknown) =>
  fetch(`${BASE}${p}`, { method: "POST", headers: { "Content-Type": "application/json", ...HEADERS }, body: JSON.stringify(body ?? {}) }).then(async (r) => ({ status: r.status, body: await r.json().catch(() => null) }));

describe("fixture-api finish-r1 endpoints (eight representative pages)", () => {
  afterEach(() => { child?.kill(); child = null; });

  it("serves projects list, project detail, batches list for the list surfaces", async () => {
    await start();
    const list = await jget("/api/v1/projects");
    expect(list.status).toBe(200);
    const projects = list.body?.projects;
    expect(Array.isArray(projects)).toBe(true);
    expect(projects.length).toBeGreaterThan(0);
    expect(projects[0]).toMatchObject({ id: "proj_fx", name: expect.any(String), status: expect.any(String) });

    const detail = await jget("/api/v1/projects/proj_fx");
    expect(detail.status).toBe(200);
    expect(detail.body?.project).toMatchObject({ id: "proj_fx" });
    expect(Array.isArray(detail.body?.inputs)).toBe(true);
    expect(Array.isArray(detail.body?.versions)).toBe(true);

    const missing = await jget("/api/v1/projects/proj_absent_xyz");
    expect(missing.status).toBe(404);
    expect(missing.body?.error?.code).toBeTruthy();

    const batches = await jget("/api/v1/batches");
    expect(batches.status).toBe(200);
    const batch = batches.body?.batches?.[0];
    expect(batch).toBeTruthy();
    // partial 态:同批内既有成功也有失败,页面才有真实 partial 可拍。
    expect(batch.counts.succeeded).toBeGreaterThan(0);
    expect(batch.counts.failed).toBeGreaterThan(0);
  });

  it("empty scenario returns an empty projects list for the empty state", async () => {
    await start();
    await jpost("/__fixture/scenario", { name: "limited", reset: true });
    await jpost("/__fixture/scenario", { name: "empty" });
    const list = await jget("/api/v1/projects");
    expect(list.status).toBe(200);
    expect(list.body?.projects).toEqual([]);
    await jpost("/__fixture/scenario", { name: "limited", reset: true });
  });

  it("revision state follows the VersionView contract with V1/V2 and adopted V1", async () => {
    await start();
    const st = await jget("/api/v1/projects/proj_fx/revisions");
    expect(st.status).toBe(200);
    expect(st.body?.adopted_label).toBe("V1");
    const labels = (st.body?.versions ?? []).map((v: { label?: string }) => v.label);
    expect(labels).toEqual(["V1", "V2"]);
    for (const v of st.body?.versions ?? []) {
      expect(v).toMatchObject({ action: expect.any(String), region: expect.any(String), incremental_cost_cents: expect.any(Number) });
    }
  });

  it("plan never calls the supplier and carries the honest supplier note", async () => {
    await start();
    await jpost("/api/v1/projects/proj_fx/revisions/intent", { click: "simplify_background" });
    const plan = await jpost("/api/v1/projects/proj_fx/revisions/plan", { click: "simplify_background" });
    expect(plan.status).toBe(200);
    expect(plan.body?.plan?.calls_supplier).toBe(false);
    expect(String(plan.body?.plan?.supplier_note ?? "")).toMatch(/真实供应商局部编辑仍是 UNKNOWN/);
    expect(typeof plan.body?.plan?.incremental_cost_cents).toBe("number");
  });

  it("adopt and rollback return the full state and move adopted_label", async () => {
    await start();
    const adopt = await jpost("/api/v1/projects/proj_fx/revisions/V2/adopt");
    expect(adopt.status).toBe(200);
    expect(adopt.body?.adopted_label).toBe("V2");
    const back = await jpost("/api/v1/projects/proj_fx/revisions/rollback", { text: "回到V1" });
    expect(back.status).toBe(200);
    expect(back.body?.adopted_label).toBe("V1");
  });

  it("downstream returns a local reference only and never claims an upload", async () => {
    await start();
    for (const target of ["digital_human", "aicut", "matrix"]) {
      const d = await jpost("/api/v1/projects/proj_fx/revisions/V1/downstream", { target });
      expect(d.status).toBe(200);
      expect(d.body?.downstream?.requires_reupload).toBe(false);
      expect(String(d.body?.downstream?.asset_ref ?? "")).toMatch(/^revision-version\/V1@[0-9a-f]{12}$/);
      // 诚实账目:响应里不允许出现「已送出/已上传」类字段。
      expect(JSON.stringify(d.body)).not.toMatch(/已送出|已上传|uploaded|delivered|sent_at/);
    }
  });

  it("compare succeeds and version content serves image bytes with the hash header", async () => {
    await start();
    const cmp = await jget("/api/v1/projects/proj_fx/revisions/compare?left=V1&right=V2");
    expect(cmp.status).toBe(200);
    const content = await fetch(`${BASE}/api/v1/projects/proj_fx/revisions/V1/content`, { headers: HEADERS });
    expect(content.status).toBe(200);
    expect(content.headers.get("content-type")).toMatch(/image\/png/);
    expect((await content.arrayBuffer()).byteLength).toBeGreaterThan(100);
    expect(content.headers.get("X-Revision-Version")).toBe("V1");
    expect(content.headers.get("X-Revision-Sha256")).toMatch(/^[0-9a-f]{64}$/);
  });
});
