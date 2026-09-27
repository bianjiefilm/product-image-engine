import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { GET as capsGET } from "@/app/api/background-replacements/capabilities/route";

function authed(url: string): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, {
    headers: { cookie: "pia_access=good-token" },
  });
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("背景替换 BFF", () => {
  it("未登录不打上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await capsGET(new NextRequest("http://localhost:3000/api/background-replacements/capabilities"));
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("开关关闭时透传 404", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ error: { code: "not_found", message: "资源不存在" } }), { status: 404 }))
    );
    const res = await capsGET(authed("/api/background-replacements/capabilities"));
    expect(res.status).toBe(404);
  });

  it("透传计费待确认,不改写成已通过", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            billing_label: "计费待确认",
            production_generation_passed: false,
            billing_passed: false,
          }),
          { status: 200 }
        )
      )
    );
    const res = await capsGET(authed("/api/background-replacements/capabilities"));
    const body = await res.json();
    expect(body.billing_label).toBe("计费待确认");
    expect(body.production_generation_passed).toBe(false);
    expect(body.billing_passed).toBe(false);
  });
});
