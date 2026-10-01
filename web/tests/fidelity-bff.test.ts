import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

import {
  GET,
  POST,
} from "@/app/api/projects/[id]/fidelity-reports/route";

function authed(url: string, init: NonNullable<ConstructorParameters<typeof NextRequest>[1]> = {}): NextRequest {
  const headers = new Headers(init.headers);
  headers.set("cookie", "pia_access=good-token");
  return new NextRequest(`http://localhost:3000${url}`, { ...init, headers });
}

const ctx = { params: Promise.resolve({ id: "proj_1" }) };

beforeEach(() => {
  vi.restoreAllMocks();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("主体保真 BFF", () => {
  it("未登录不转发", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await GET(
      new NextRequest("http://localhost:3000/api/projects/proj_1/fidelity-reports"),
      ctx
    );
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("开关关闭时原样 404，不把记录写成已通过", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("nope", { status: 404 }))
    );
    const res = await GET(
      authed("/api/projects/proj_1/fidelity-reports"),
      ctx
    );
    expect(res.status).toBe(404);
  });

  it("转发记录时不附加结论或授权字段", async () => {
    let seen = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        seen = String(init?.body ?? "");
        return new Response(
          JSON.stringify({
            report: { verdict: "unknown", exact_product: false },
            duplicate: false,
          }),
          { status: 201 }
        );
      })
    );
    const res = await POST(
      authed("/api/projects/proj_1/fidelity-reports", {
        method: "POST",
        body: JSON.stringify({
          mode: "fidelity",
          input_ref: "asset-in",
          mask_ref: "mask-1",
        }),
      }),
      ctx
    );
    expect(res.status).toBe(201);
    expect(seen).not.toContain("supplier_authorized");
    expect(seen).not.toContain("verdict");
    const data = (await res.json()) as { report: { verdict: string } };
    expect(data.report.verdict).toBe("unknown");
  });
});
