import { describe, it, expect, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";
import { GET as caps } from "@/app/api/projects/[id]/source-image-capabilities/route";
import { GET as list, POST as create } from "@/app/api/projects/[id]/source-image-runs/route";
import { GET as find } from "@/app/api/projects/[id]/source-image-runs/by-request/route";
import { GET as get } from "@/app/api/projects/[id]/source-image-runs/[runId]/route";
import { GET, POST, DELETE } from "@/app/api/projects/[id]/source-image-runs/[runId]/[action]/route";
import { quoted, capabilities } from "./source-image-fixture";
const base = "http://localhost:3000";
const path = "/api/projects/proj_a/source-image-runs";
const ctx = (action?: string, id = "proj_a", runId = "sir_a") => ({ params: Promise.resolve({ id, runId, action: action ?? "" }) });
type ReqInit=NonNullable<ConstructorParameters<typeof NextRequest>[1]>;
const request = (suffix = "", init: ReqInit = {}) => new NextRequest(base + path + suffix, { ...init, headers: { cookie: "pia_access=good-token", origin: base, "content-type": "application/json", ...init.headers } });
beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });
describe("safe formal source BFF", () => {
  it("actual route forwards strict original raw quote body with server-only headers", async () => {
    const body = '{ "request_key":"request-a", "mode":"text_generate", "prompt":"蓝纸盒", "size":"1024*1024" }';
    const calls: Array<[unknown, RequestInit]> = []; vi.stubGlobal("fetch", async (u: unknown, i: RequestInit) => { calls.push([u,i]); return Response.json(quoted()); });
    const res = await create(request("", { method: "POST", body }), ctx());
    expect(res.status).toBe(200); expect(calls.length).toBe(1); expect(calls[0][0]).toBe("http://127.0.0.1:18220/api/v1/projects/proj_a/source-image-runs");
    expect(calls[0][1].body).toBe(body); expect(calls[0][1].redirect).toBe("manual"); expect(calls[0][1].signal).toBeDefined();
    expect(calls[0][1].headers).toMatchObject({ Authorization: "Bearer good-token", "X-Product-Internal-Token": "test-internal-token" });
    expect(res.headers.get("cache-control")).toContain("no-store");
  });
  it("rejects absent/malformed/duplicated access cookies before upstream", async () => {
    const calls: string[] = []; vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(quoted()); });
    for (const cookie of ["", "pia_refresh=refresh", "pia_access=%ZZ", "pia_access=a; pia_access=b", "pia_access=%0d%0aevil", "pia_access=", "pia_access=a b"]) {
      const res = await get(request("/sir_a", { headers: { cookie } }), ctx()); expect(res.status).toBe(401);
    }
    expect(calls).toEqual([]);
  });
  it("same-origin mutation guard rejects missing/foreign/ambiguous origin and cross-site", async () => {
    const calls: string[] = []; vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(quoted()); });
    for (const headers of [{ origin: "" }, { origin: "https://evil.invalid" }, { origin: "null" }, { origin: base + "/" }, { origin: base, "sec-fetch-site": "cross-site" }, { origin: base, "sec-fetch-site": "same-site" }, { origin: base + ", " + base }] as Array<Record<string,string>>) {
      expect((await POST(request("/sir_a/confirm", { method: "POST", headers, body: '{}' }), ctx("confirm"))).status).toBe(403);
    }
    expect(calls).toEqual([]);
  });
  it("closes action/method and path injection before fetch", async () => {
    const calls: string[] = []; vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(quoted()); });
    expect((await POST(request("/sir_a/submit", { method: "POST", body: '{}' }), ctx("submit"))).status).toBe(404);
    expect((await GET(request("/sir_a/confirm"), ctx("confirm"))).status).toBe(405);
    expect((await DELETE(request("/sir_a/content", { method: "DELETE" }), ctx("content"))).status).toBe(405);
    for (const id of ["../other", "bad/id", "%2f", "bad\u0000"]) expect((await list(request(), ctx(undefined,id))).status).toBe(400);
    expect((await get(request("/sir_a/"), ctx())).status).toBe(400);
    expect(calls).toEqual([]);
  });
  it("closed request rejects malformed/duplicate/nested/null/identity/overflow/oversize without defaulting", async () => {
    const calls: string[] = []; vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(quoted()); });
    for (const body of ["{", "null", "[]", '{"quote_id":"a","quote_id":"b","quote_fingerprint":"h"}', '{"Quote_id":"a","quote_fingerprint":"h"}', '{"quote_id":null,"quote_fingerprint":"h"}', '{"quote_id":{"a":1,"a":2},"quote_fingerprint":"h"}', '{"quote_id":"a","quote_fingerprint":"h","payer_account_id":"acct_other"}', '{"quote_id":9223372036854775808,"quote_fingerprint":"h"}', '{}' + '{}', " ".repeat(32769)]) {
      expect((await POST(request("/sir_a/confirm", { method: "POST", body }), ctx("confirm"))).status).toBe(400);
    }
    expect(calls).toEqual([]);
  });
  it("query boundaries are strict and valid cursor is only transported", async () => {
    const calls: unknown[] = []; vi.stubGlobal("fetch", async (u: unknown) => { calls.push(u); return Response.json({ runs: [], next_cursor: "" }); });
    for (const suffix of ["?limit=01", "?limit=0", "?limit=101", "?limit=1&limit=2", "?payer=x", "?cursor=bad", "?request_key=a"]) expect((await list(request(suffix), ctx())).status).toBe(400);
    expect((await find(request("/by-request?request_key=a&request_key=b"), ctx())).status).toBe(400);
    expect((await get(request("/sir_a?token=x"), ctx())).status).toBe(400);
    const cursor = Buffer.from('{"v":1,"created_at":"1700000000","id":"sir_a"}').toString("base64url");
    expect((await list(request("?limit=1&cursor=" + cursor), ctx())).status).toBe(200);
    expect(calls).toEqual(["http://127.0.0.1:18220/api/v1/projects/proj_a/source-image-runs?limit=1&cursor=" + cursor]);
  });
  it("GET caps/history/find/detail never post and whitelist private response fields", async () => {
    const calls: RequestInit[] = []; vi.stubGlobal("fetch", async (u: string, i: RequestInit) => { calls.push(i); return Response.json(u.includes("capabilities") ? { ...capabilities(), token: "secret" } : u.includes("?request_key") || u.endsWith("sir_a") ? { ...quoted(), private_provider: "secret", url: "https://private.invalid" } : { runs: [{ ...quoted(), token: "secret" }], next_cursor: "" }); });
    const responses = [await caps(new NextRequest(base + "/api/projects/proj_a/source-image-capabilities", { headers: { cookie: "pia_access=good-token" } }), ctx()), await list(request(), ctx()), await find(request("/by-request?request_key=request-a"), ctx()), await get(request("/sir_a"), ctx())];
    for (const r of responses) { expect(r.status).toBe(200); expect(await r.text()).not.toMatch(/secret|private.invalid/); }
    expect(calls.map(i => i.method)).toEqual(["GET", "GET", "GET", "GET"]); expect(calls.every(i => i.body === undefined)).toBe(true);
  });
  it("upstream malformed amount/redirect/raw error are masked; authorized runID retained", async () => {
    vi.stubGlobal("fetch", async () => Response.json({ ...quoted(), quote: { ...quoted().quote, amount_minor: 25 } }));
    expect((await get(request("/sir_a"), ctx())).status).toBe(503);
    vi.stubGlobal("fetch", async () => new Response("private signed url", { status: 307, headers: { location: "https://private.invalid" } }));
    const redirected = await get(request("/sir_a"), ctx()); expect(redirected.status).toBe(503); expect(await redirected.text()).not.toContain("private");
    vi.stubGlobal("fetch", async () => Response.json({ run_id: "sir_a", error: { code: "provider_secret", message: "token=url" } }, { status: 503 }));
    const failed = await get(request("/sir_a"), ctx()); expect(failed.status).toBe(503); const body = await failed.json(); expect(body.run_id).toBe("sir_a"); expect(JSON.stringify(body)).not.toMatch(/provider_secret|token=url/);
  });
  it("content streams bounded bytes with safe headers and cancel propagates", async () => {
    let canceled = false; vi.stubGlobal("fetch", async () => new Response(new ReadableStream({ pull(c) { c.enqueue(new Uint8Array([1,2,3])); }, cancel() { canceled = true; } }), { headers: { "content-type": "image/png", "set-cookie": "secret", "content-disposition": "private url" } }));
    const res = await GET(request("/sir_a/content"), ctx("content")); expect(res.status).toBe(200); expect(res.headers.get("set-cookie")).toBeNull(); expect(res.headers.get("content-disposition")).toBe('inline; filename="source-image.png"');
    const reader = res.body!.getReader(); expect((await reader.read()).value).toEqual(new Uint8Array([1,2,3])); await reader.cancel(); expect(canceled).toBe(true);
  });
  it("download rejects unexpected MIME and announced oversize before streaming", async () => {
    for (const headers of [{ "content-type": "text/html" }, { "content-type": "image/png", "content-length": "16777217" }] as Array<Record<string,string>>) {
      vi.stubGlobal("fetch", async () => new Response("secret", { headers }));
      const res = await GET(request("/sir_a/content"), ctx("content")); expect(res.status).toBe(503); expect(await res.text()).not.toContain("secret");
    }
  });
  it("malicious next_cursor cannot carry private URL into JSON",async()=>{
    vi.stubGlobal("fetch",async()=>Response.json({runs:[],next_cursor:"https://private.invalid?token=secret"}));
    const r=await list(request(),ctx());expect(r.status).toBe(503);expect(await r.text()).not.toContain("secret");
  });
  it("a legal full 100-row page with bounded long IDs remains transportable",async()=>{
    const project="proj_"+"p".repeat(251), long="id_"+"a".repeat(253), run=quoted();
    run.project_id=project;run.run_id="sir_"+"r".repeat(252);run.request_key=long;run.task_id=long;run.task_phase=long;run.task_status=long;
    run.payment.payer_account_id=long;run.payment.payer_user_id=long;
    for(const key of ["hold_id","charge_id","release_id","refund_id","original_charge_id"] as const) run.payment[key]=long;
    run.quote!.quote_id=long;run.quote!.usage_id=long;run.quote!.pricing_version=long;
    run.output={asset_id:long,reference_id:long,sha256:"b".repeat(64),content_type:"image/png",size_bytes:"1024",width_px:1024,height_px:1024,association_verified:true};
    const raw=JSON.stringify({runs:Array.from({length:100},(_,i)=>{const unique="id_"+String(i).padStart(253,"a");return {...run,run_id:"sir_"+String(i).padStart(252,"r"),request_key:unique,task_id:unique,quote:{...run.quote!,usage_id:unique,quote_id:unique},payment:{...run.payment,hold_id:unique,charge_id:unique,original_charge_id:unique}};}),next_cursor:""});expect(new TextEncoder().encode(raw).length).toBeGreaterThan(524288);
    vi.stubGlobal("fetch",async()=>new Response(raw));
    const req=new NextRequest(base+`/api/projects/${project}/source-image-runs?limit=100`,{headers:{cookie:"pia_access=good-token"}});
    const res=await list(req,ctx(undefined,project));expect(res.status).toBe(200);expect((await res.json()).runs).toHaveLength(100);
  });
  it("stream refuses actual overlimit/length mismatch and upstream hang is canceled",async()=>{
    let canceled=false;
    vi.stubGlobal("fetch",async()=>new Response(new ReadableStream({start(c){c.enqueue(new Uint8Array(16777217));},cancel(){canceled=true;}}),{headers:{"content-type":"image/png"}}));
    const tooBig=await GET(request("/sir_a/content"),ctx("content"));await expect(tooBig.arrayBuffer()).rejects.toThrow();expect(canceled).toBe(true);
    vi.stubGlobal("fetch",async()=>new Response(new Uint8Array([1,2]),{headers:{"content-type":"image/png","content-length":"3"}}));
    const wrongLength=await GET(request("/sir_a/content"),ctx("content"));await expect(wrongLength.arrayBuffer()).rejects.toThrow();
    const abort=new AbortController();let hangCanceled=false;
    vi.stubGlobal("fetch",async()=>new Response(new ReadableStream({pull(){return new Promise(()=>{});},cancel(){hangCanceled=true;}}),{headers:{"content-type":"image/png"}}));
    const hanging=await GET(request("/sir_a/content",{signal:abort.signal}),ctx("content"));const reading=hanging.arrayBuffer();abort.abort();await expect(reading).rejects.toThrow();expect(hangCanceled).toBe(true);
  });
  it("invalid UTF8 and nonempty GET body reject without an upstream call",async()=>{
    const calls:string[]=[];vi.stubGlobal("fetch",async()=>{calls.push("upstream");return Response.json(quoted());});
    const invalid=new Request(base+path+"/sir_a/confirm",{method:"POST",headers:{cookie:"pia_access=good-token",origin:base,"content-type":"application/json"},body:new Uint8Array([123,34,255,34,58,49,125])});
    expect((await POST(invalid,ctx("confirm"))).status).toBe(400);
    const bodyGet=new Request(base+path+"/sir_a");Object.defineProperty(bodyGet,"body",{value:new ReadableStream({start(c){c.enqueue(new TextEncoder().encode("{}"));c.close();}})});
    Object.defineProperty(bodyGet,"headers",{value:new Headers({cookie:"pia_access=good-token"})});expect((await get(bodyGet,ctx())).status).toBe(400);expect(calls).toEqual([]);
  });
});
