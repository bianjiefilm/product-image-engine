import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { POST as intentPOST } from "@/app/api/projects/[id]/revisions/intent/route";
import { POST as exportPOST } from "@/app/api/projects/[id]/revisions/[label]/export/route";
import { POST as downstreamPOST } from "@/app/api/projects/[id]/revisions/[label]/downstream/route";

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

describe("局部修改 BFF", () => {
  it("无 Cookie 不转发", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await intentPOST(req("/api/projects/p1/revisions/intent", { method: "POST", body: "{}" }), {
      params: Promise.resolve({ id: "p1" }),
    });
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("文本原样转发，不在网关加费用", async () => {
    let seen = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        seen = String(init?.body ?? "");
        return new Response(JSON.stringify({ intent: { action: "simplify_background" }, incremental_cost_cents: 40 }), {
          status: 200,
        });
      })
    );
    const payload = { text: "背景简单一点" };
    const res = await intentPOST(
      authed("/api/projects/p1/revisions/intent", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(payload),
      }),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res.status).toBe(200);
    expect(JSON.parse(seen)).toEqual(payload);
    expect(seen).not.toContain("incremental_cost_cents");
    const body = await res.json();
    expect(body.incremental_cost_cents).toBe(40);
  });

  it("导出和下游只转发版本引用", async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"} ${url} ${init?.body ?? ""}`);
        return new Response(
          JSON.stringify({
            export: { version_id: "V2", requires_reupload: false, asset_ref: "revision-version/V2@abc" },
            downstream: { version_id: "V2", target: "matrix", requires_reupload: false },
          }),
          { status: 200 }
        );
      })
    );
    const exported = await exportPOST(authed("/api/projects/p1/revisions/V2/export", { method: "POST", body: "{}" }), {
      params: Promise.resolve({ id: "p1", label: "V2" }),
    });
    const down = await downstreamPOST(
      authed("/api/projects/p1/revisions/V2/downstream", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ target: "matrix" }),
      }),
      { params: Promise.resolve({ id: "p1", label: "V2" }) }
    );
    expect(exported.status).toBe(200);
    expect(down.status).toBe(200);
    expect(calls[0]).toContain("/api/v1/projects/p1/revisions/V2/export");
    expect(calls[1]).toContain("/api/v1/projects/p1/revisions/V2/downstream");
    expect(calls[1]).toContain("\"target\":\"matrix\"");
    expect(calls.join("\n")).not.toMatch(/upload|data_b64/);
  });
});
