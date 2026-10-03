import { describe, it, expect, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";
import { GET as caps } from "@/app/api/projects/[id]/source-image-capabilities/route";
import { POST as create } from "@/app/api/projects/[id]/source-image-runs/route";
import { GET, POST } from "@/app/api/projects/[id]/source-image-runs/[runId]/[action]/route";
import { POST as bgAction } from "@/app/api/projects/[id]/background-replacements/[jobId]/[action]/route";
import { plateCapabilities, plateDone, plateQuoted } from "./source-image-fixture";

const base = "http://localhost:3000";
const path = "/api/projects/proj_a/source-image-runs";
const ctx = (action?: string) => ({ params: Promise.resolve({ id: "proj_a", runId: "sir_plate", action: action ?? "" }) });
type ReqInit = NonNullable<ConstructorParameters<typeof NextRequest>[1]>;
const request = (suffix = "", init: ReqInit = {}) => new NextRequest(base + path + suffix, { ...init, headers: { cookie: "pia_access=good-token", origin: base, "content-type": "application/json", ...init.headers } });
const plateBody = { request_key: "request-plate", mode: "background_plate_lock", input_id: "pin_a", background_intent: "深蓝色渐变摄影棚背景" };
beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("limited-fidelity BFF", () => {
  it("forwards the closed plate create body byte for byte", async () => {
    const calls: Array<[unknown, RequestInit]> = [];
    vi.stubGlobal("fetch", async (u: unknown, i: RequestInit) => { calls.push([u, i]); return Response.json(plateQuoted()); });
    const body = JSON.stringify(plateBody);
    const res = await create(request("", { method: "POST", body }), { params: Promise.resolve({ id: "proj_a" }) });
    expect(res.status).toBe(200);
    expect(calls[0][0]).toBe("http://127.0.0.1:18220/api/v1/projects/proj_a/source-image-runs");
    expect(calls[0][1].body).toBe(body);
    expect((await res.json()).plate.derivation_state).toBe("pending");
  });

  it("rejects every browser-supplied quality, sample, mask, price or prompt field before upstream", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(plateQuoted()); });
    const bad: Array<Record<string, unknown>> = [
      { ...plateBody, sample_id: "carton-1024x1024" }, { ...plateBody, mask_png_base64: "AAAA" }, { ...plateBody, passed: "true" },
      { ...plateBody, quality: "pass" }, { ...plateBody, coverage: "x" }, { ...plateBody, supplier_authorized: "true" }, { ...plateBody, amount_minor: "1" },
      { ...plateBody, prompt: "draw" }, { ...plateBody, size: "1024*1024" },
      { request_key: "request-plate", mode: "background_plate_lock", input_id: "pin_a" },
      { ...plateBody, input_id: "../x" }, { ...plateBody, input_id: 7 },
      { ...plateBody, background_intent: "" }, { ...plateBody, background_intent: " leading" }, { ...plateBody, background_intent: "a\nb" }, { ...plateBody, background_intent: "界".repeat(201) },
      { ...plateBody, request_key: "a\u0000b" },
    ];
    for (const b of bad) expect((await create(request("", { method: "POST", body: JSON.stringify(b) }), { params: Promise.resolve({ id: "proj_a" }) })).status, JSON.stringify(b)).toBe(400);
    expect(calls).toEqual([]);
  });

  it("text creation stays closed to plate fields", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(plateQuoted()); });
    const body = JSON.stringify({ request_key: "r", mode: "text_generate", prompt: "x", size: "1024*1024", input_id: "pin_a" });
    expect((await create(request("", { method: "POST", body }), { params: Promise.resolve({ id: "proj_a" }) })).status).toBe(400);
    expect(calls).toEqual([]);
  });

  it("the return action is POST only with an empty body, and select/confirm stay as before", async () => {
    const calls: Array<[unknown, RequestInit]> = [];
    vi.stubGlobal("fetch", async (u: unknown, i: RequestInit) => { calls.push([u, i]); return Response.json({ output: { id: "out_a" } }); });
    expect((await GET(request("/sir_plate/return"), ctx("return"))).status).toBe(405);
    expect((await POST(request("/sir_plate/return", { method: "POST", body: '{"output_id":"out_x"}' }), ctx("return"))).status).toBe(400);
    expect(calls).toEqual([]);
  });

  it("passes the stable limited-fidelity error codes, each only with its own status, and masks everything else", async () => {
    const reply = (status: number, code: string) => { vi.stubGlobal("fetch", async () => Response.json({ error: { code, message: "internal detail https://private.invalid?token=secret" }, run_id: "sir_plate" }, { status })); };
    for (const [status, code] of [[422, "sample_not_frozen"], [409, "candidate_not_usable"], [409, "not_selected"], [409, "no_source_binding"], [503, "plate_lock_disabled"], [503, "sample_set_invalid"]] as const) {
      reply(status, code);
      const res = await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"));
      const text = await res.text();
      expect(res.status, code).toBe(status);
      expect(JSON.parse(text).error.code).toBe(code);
      expect(text).not.toContain("private.invalid");
      expect(text).not.toContain("secret");
    }
    reply(409, "sample_not_frozen"); // right code, wrong status: masked to the generic conflict
    expect(JSON.parse(await (await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).text()).error.code).toBe("conflict");
    reply(500, "plate_lock_disabled");
    expect(JSON.parse(await (await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).text()).error.code).toBe("source_unavailable");
  });

  it("decodes plate capabilities and refuses a malformed plate entry", async () => {
    vi.stubGlobal("fetch", async () => Response.json(plateCapabilities(true)));
    const ok = await caps(new NextRequest(base + "/api/projects/proj_a/source-image-capabilities", { headers: { cookie: "pia_access=good-token" } }), { params: Promise.resolve({ id: "proj_a" }) });
    expect(ok.status).toBe(200);
    expect((await ok.json()).modes.find((m: { mode: string }) => m.mode === "background_plate_lock").supported_input_ids).toEqual(["pin_a"]);
    const broken = plateCapabilities(true);
    (broken.modes.find(m => m.mode === "background_plate_lock") as { supported_input_ids: unknown }).supported_input_ids = "pin_a";
    vi.stubGlobal("fetch", async () => Response.json(broken));
    const bad = await caps(new NextRequest(base + "/api/projects/proj_a/source-image-capabilities", { headers: { cookie: "pia_access=good-token" } }), { params: Promise.resolve({ id: "proj_a" }) });
    expect(bad.status).toBe(503);
  });

  it("refuses a plate run that smuggles the raw plate as an output, and never echoes an unknown score field", async () => {
    const smuggled = JSON.parse(JSON.stringify(plateDone())) as Record<string, unknown>;
    smuggled.output = { asset_id: "a", reference_id: "r", sha256: "a".repeat(64), content_type: "image/png", size_bytes: "1", width_px: 1024, height_px: 1024, association_verified: true };
    vi.stubGlobal("fetch", async () => Response.json(smuggled));
    expect((await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).status).toBe(503);
    const scored = JSON.parse(JSON.stringify(plateDone())) as { plate: Record<string, unknown> };
    scored.plate.similarity = 99;
    vi.stubGlobal("fetch", async () => Response.json(scored));
    const res = await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"));
    expect(res.status).toBe(200);
    expect(JSON.stringify(await res.json())).not.toContain("similarity");
    // A fractional score is not even valid wire JSON for this contract.
    vi.stubGlobal("fetch", async () => new Response(JSON.stringify(scored).replace('"similarity":99', '"similarity":0.99'), { status: 200, headers: { "content-type": "application/json" } }));
    expect((await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).status).toBe(503);
  });

  it("retires the model-plate action at the background-replacement BFF", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json({}); });
    const res = await bgAction(new NextRequest(base + "/api/projects/proj_a/background-replacements/job_a/model-plate", { method: "POST", headers: { cookie: "pia_access=good-token" }, body: "{}" }), { params: Promise.resolve({ id: "proj_a", jobId: "job_a", action: "model-plate" }) });
    expect(res.status).toBe(404);
    expect(calls).toEqual([]);
  });
});
