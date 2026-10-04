import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { GET, POST } from "@/app/api/projects/[id]/motion-refs/route";
import { POST as paramPOST } from "@/app/api/projects/[id]/motion-refs/[refId]/parameters/route";
import { POST as batchPOST } from "@/app/api/projects/[id]/motion-refs/batch-rerun/route";

function req(url: string, init: NonNullable<ConstructorParameters<typeof NextRequest>[1]> = {}): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, init);
}

function authed(url: string, init: NonNullable<ConstructorParameters<typeof NextRequest>[1]> = {}): NextRequest {
  const headers = new Headers(init.headers);
  headers.set("cookie", "pia_access=good-token");
  return req(url, { ...init, headers });
}

beforeEach(() => {
  vi.restoreAllMocks();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Motion 引用 BFF", () => {
  it("无 Cookie 不转发", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await POST(req("/api/projects/p1/motion-refs", { method: "POST", body: "{}" }), {
      params: Promise.resolve({ id: "p1" }),
    });
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("声明和参数原样转发，不补生成字段", async () => {
    const seen: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        seen.push(`${init?.method ?? "GET"} ${url} ${init?.body ?? ""}`);
        return new Response(
          JSON.stringify({
            created: true,
            declaration: {
              asset_id: "revision-version/V1@abc",
              digest: `sha256:${"ab".repeat(32)}`,
              product_regeneration: false,
              model_call_count: 0,
              finished_copy: "还没生成成片。这条只是已采用商品的 Motion 引用声明，不是动态广告。",
            },
          }),
          { status: 201 }
        );
      })
    );
    const declared = await POST(
      authed("/api/projects/p1/motion-refs", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ revision_id: "V1" }),
      }),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(declared.status).toBe(201);
    expect(seen[0]).toContain('"revision_id":"V1"');
    expect(seen[0]).not.toContain("video");
    expect(seen[0]).not.toContain("supplier");
    expect(seen[0]).not.toContain("product_regeneration");

    const priced = await paramPOST(
      authed("/api/projects/p1/motion-refs/mref_1/parameters", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ name: "price", value: "19" }),
      }),
      { params: Promise.resolve({ id: "p1", refId: "mref_1" }) }
    );
    expect(priced.status).toBe(200);
    expect(seen[1]).toContain("/motion-refs/mref_1/parameters");
    expect(seen[1]).toContain('"name":"price"');
    expect(seen[1]).not.toContain("model_call_count");
  });

  it("批量重跑的拒绝原样返回，不重试", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ error: { code: "batch_rerun_unproven", message: "还没生成成片" } }), { status: 409 }));
    vi.stubGlobal("fetch", fetchMock);
    const res = await batchPOST(
      authed("/api/projects/p1/motion-refs/batch-rerun", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ variants: [{ id: "b", execution_count: 0 }] }),
      }),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res.status).toBe(409);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const body = await res.json();
    expect(body.error.code).toBe("batch_rerun_unproven");
  });

  it("列表只转发", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ declarations: [] }), { status: 200 }))
    );
    const res = await GET(authed("/api/projects/p1/motion-refs"), { params: Promise.resolve({ id: "p1" }) });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.declarations).toEqual([]);
  });
});
