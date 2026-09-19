// HUI-1745 I1:BFF 路由三态测试(鉴权 401 / 透传 / 503 不可达)。
// 覆盖:handoffs/accept、bindings(列表/聚合/adopt/return-target)、
// outputs(列表/登记)、receipt(回执/重传)。BFF 不做权限判断,只转发。
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

import { POST as acceptPOST } from "@/app/api/handoffs/accept/route";
import { GET as bindingsGET } from "@/app/api/bindings/route";
import { GET as bindingGET } from "@/app/api/bindings/[id]/route";
import { POST as adoptPOST } from "@/app/api/bindings/[id]/adopt/route";
import { GET as returnTargetGET } from "@/app/api/bindings/[id]/return-target/route";
import {
  GET as outputsGET,
  POST as outputsPOST,
} from "@/app/api/projects/[id]/outputs/route";
import { POST as receiptPOST } from "@/app/api/projects/[id]/outputs/[outputId]/receipt/route";
import { GET as receiptsGET } from "@/app/api/projects/[id]/receipts/route";
import { POST as resendPOST } from "@/app/api/receipts/[id]/resend/route";

function req(url: string, init: RequestInit = {}): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, init);
}

function authed(url: string, init: RequestInit = {}): NextRequest {
  const headers = new Headers(init.headers);
  headers.set("cookie", "pia_access=good-token");
  return req(url, { ...init, headers });
}

function jsonInit(body: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  };
}

async function bodyOf(res: Response) {
  return (await res.json().catch(() => null)) as Record<
    string,
    { code?: string; message?: string }
  > | null;
}

const okUpstream = (data: unknown, status = 200) =>
  vi.fn(async () => new Response(JSON.stringify(data), { status }));

beforeEach(() => vi.restoreAllMocks());
afterEach(() => vi.unstubAllGlobals());

describe("BFF 鉴权失败态(不触达上游)", () => {
  const cases: [string, () => Promise<Response>][] = [
    ["handoffs/accept", () => acceptPOST(req("/api/handoffs/accept", jsonInit({})))],
    ["bindings", () => bindingsGET(req("/api/bindings"))],
    [
      "binding detail",
      () => bindingGET(req("/api/bindings/b1"), { params: Promise.resolve({ id: "b1" }) }),
    ],
    [
      "adopt",
      () =>
        adoptPOST(req("/api/bindings/b1/adopt", jsonInit({})), {
          params: Promise.resolve({ id: "b1" }),
        }),
    ],
    [
      "return-target",
      () =>
        returnTargetGET(req("/api/bindings/b1/return-target"), {
          params: Promise.resolve({ id: "b1" }),
        }),
    ],
    [
      "outputs list",
      () => outputsGET(req("/api/projects/p1/outputs"), { params: Promise.resolve({ id: "p1" }) }),
    ],
    [
      "outputs register",
      () =>
        outputsPOST(req("/api/projects/p1/outputs", jsonInit({})), {
          params: Promise.resolve({ id: "p1" }),
        }),
    ],
    [
      "receipt",
      () =>
        receiptPOST(req("/api/projects/p1/outputs/o1/receipt", jsonInit({})), {
          params: Promise.resolve({ id: "p1", outputId: "o1" }),
        }),
    ],
    [
      "receipts list",
      () => receiptsGET(req("/api/projects/p1/receipts"), { params: Promise.resolve({ id: "p1" }) }),
    ],
    [
      "resend",
      () =>
        resendPOST(req("/api/receipts/r1/resend", jsonInit({})), {
          params: Promise.resolve({ id: "r1" }),
        }),
    ],
  ];
  for (const [name, call] of cases) {
    it(`${name}:无 Cookie → 401 且不触达上游`, async () => {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);
      const res = await call();
      expect(res.status).toBe(401);
      const b = await bodyOf(res);
      expect(b?.error?.code).toBe("unauthenticated");
      expect(fetchMock).not.toHaveBeenCalled();
    });
  }
});

describe("BFF 正常转发(带内部凭据与会话)", () => {
  it("接受交接:转发 body 并携带内部凭据头", async () => {
    let seen: { url: string; headers: Record<string, string>; body?: string } =
      { url: "", headers: {} };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        seen = {
          url: String(url),
          headers: Object.fromEntries(new Headers(init?.headers).entries()),
          body: init?.body as string | undefined,
        };
        return new Response(
          JSON.stringify({ resolution: "created", project: { id: "p1" } }),
          { status: 200 }
        );
      })
    );
    const handoff = { schema_version: "order-handoff/v1", handoff_id: "h1" };
    const res = await acceptPOST(
      authed("/api/handoffs/accept", jsonInit({ handoff, purpose: "主图" }))
    );
    expect(res.status).toBe(200);
    expect(seen.url).toContain("/api/v1/handoffs/accept");
    expect(seen.headers["x-product-internal-token"]).toBe("test-internal-token");
    expect(seen.headers["authorization"]).toBe("Bearer good-token");
    expect(JSON.parse(seen.body ?? "{}").purpose).toBe("主图");
    const data = await res.json();
    expect(data.resolution).toBe("created");
  });

  it("绑定列表:查询串原样透传(project_id 过滤)", async () => {
    let seenURL = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        seenURL = String(url);
        return new Response(JSON.stringify({ bindings: [] }), { status: 200 });
      })
    );
    const res = await bindingsGET(
      authed("/api/bindings?project_id=p1")
    );
    expect(res.status).toBe(200);
    expect(seenURL).toContain("/api/v1/bindings?project_id=p1");
  });

  it("登记成果:201;重复登记:200", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ output: { id: "o1" } }), { status: 201 })
      )
    );
    const res = await outputsPOST(
      authed("/api/projects/p1/outputs", jsonInit({ data_b64: "aGk=" })),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res.status).toBe(201);

    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({ output: { id: "o1" }, duplicate: true }),
          { status: 200 }
        )
      )
    );
    const res2 = await outputsPOST(
      authed("/api/projects/p1/outputs", jsonInit({ data_b64: "aGk=" })),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res2.status).toBe(200);
  });

  it("回执:201 delivered;重传:200", async () => {
    vi.stubGlobal(
      "fetch",
      okUpstream({ receipt: { id: "r1", status: "delivered" } }, 201)
    );
    const res = await receiptPOST(
      authed("/api/projects/p1/outputs/o1/receipt", jsonInit({})),
      { params: Promise.resolve({ id: "p1", outputId: "o1" }) }
    );
    expect(res.status).toBe(201);

    vi.stubGlobal(
      "fetch",
      okUpstream({ resent: true, receipt: { status: "delivered" } })
    );
    const res2 = await resendPOST(authed("/api/receipts/r1/resend", jsonInit({})), {
      params: Promise.resolve({ id: "r1" }),
    });
    expect(res2.status).toBe(200);
  });
});

describe("BFF 服务不可达态(503 透传语义)", () => {
  it("登记成果上游不可达 → 503 + 中文原因", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("fetch failed");
      })
    );
    const res = await outputsPOST(
      authed("/api/projects/p1/outputs", jsonInit({ data_b64: "aGk=" })),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res.status).toBe(503);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("product_server_unavailable");
  });

  it("回执上游 409(stale_requirement_version)→ 原样 409 透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "stale_requirement_version", message: "来源需求已有新版本" },
          }),
          { status: 409 }
        )
      )
    );
    const res = await receiptPOST(
      authed("/api/projects/p1/outputs/o1/receipt", jsonInit({})),
      { params: Promise.resolve({ id: "p1", outputId: "o1" }) }
    );
    expect(res.status).toBe(409);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("stale_requirement_version");
    expect(b?.error?.message).toContain("新版本");
  });
});
