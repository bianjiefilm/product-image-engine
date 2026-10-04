// HUI-2596 evidence addendum (Root 2026-10-04): the plan response (supplier_note),
// the three downstream bodies, and asserts over the already-saved create body.
import fs from "node:fs";
import path from "node:path";
import { chromium } from "playwright-core";

const WEB = "http://localhost:32321";
const OUT = process.env.E2E_OUT;
const PROJECT = "proj_a2727f0245095f0c";
const cookies = JSON.parse(fs.readFileSync(process.env.E2E_STORAGE_STATE, "utf8")).cookies;
const save = (name, data) => fs.writeFileSync(path.join(OUT, name), JSON.stringify(data, null, 1));
const ctx = await chromium.launchPersistentContext("/tmp/painuo-pie-accept/profile/rev-probe", { executablePath: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", headless: true });
await ctx.addCookies(cookies.map((c) => ({ ...c })));
const jpost = async (p, body) => { const r = await ctx.request.post(WEB + p, { data: body, headers: { origin: WEB, "content-type": "application/json" } }); return { status: r.status(), body: await r.json().catch(() => null) }; };
const jget = async (p) => { const r = await ctx.request.get(WEB + p); return { status: r.status(), body: await r.json().catch(() => null) }; };

// Plan response carries supplier_note verbatim; planning never touches pixels or billing.
const plan = await jpost(`/api/projects/${PROJECT}/revisions/plan`, { click: "simplify_background" });
save("revision-plan-full-response.json", plan);
const note = plan.body?.plan?.supplier_note ?? "";
if (!/真实供应商局部编辑仍是 UNKNOWN/.test(note)) throw new Error("plan lacks SupplierUnknownNote: " + note);
if (plan.body?.plan?.calls_supplier !== false) throw new Error("plan calls_supplier not false");

// Downstream on the adopted version: local references only, no reupload.
const st = await jget(`/api/projects/${PROJECT}/revisions`);
const adopted = st.body.adopted_label;
const down = {};
for (const target of ["digital_human", "aicut", "matrix"]) {
  down[target] = await jpost(`/api/projects/${PROJECT}/revisions/${adopted}/downstream`, { target });
  const d = down[target].body?.downstream;
  if (!d || d.requires_reupload !== false) throw new Error(target + " downstream not requires_reupload=false");
  if (!/^revision-version\/V\d+@[0-9a-f]{12}$/.test(d.asset_ref ?? "")) throw new Error(target + " asset_ref shape: " + d.asset_ref);
}
save("revisions-downstream-bodies.json", { adopted, ...down });
console.log(JSON.stringify({ ok: true, adopted, supplier_note: note }, null, 1));
await ctx.close();
