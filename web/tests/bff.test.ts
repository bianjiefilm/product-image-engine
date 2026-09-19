// BFF 路由三态测试:鉴权失败(401)/ 服务不可达(503)/ 开关 off(503)。
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

import { GET as sessionGET } from "@/app/api/auth/session/route";
import { POST as loginPOST } from "@/app/api/auth/login/route";
import { GET as projectsGET } from "@/app/api/projects/route";
import { POST as versionsPOST } from "@/app/api/projects/[id]/versions/route";
import { GET as balanceGET } from "@/app/api/billing/balance/route";

function req(url: string, init: RequestInit = {}): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, init);
}

function authed(url: string, init: RequestInit = {}): NextRequest {
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

async function bodyOf(res: Response) {
  return (await res.json().catch(() => null)) as {
    error?: { code?: string; message?: string };
  } | null;
}

describe("BFF 鉴权失败态", () => {
  it("无 Cookie 访问 session → 401,且不触达上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await sessionGET(req("/api/auth/session"));
    expect(res.status).toBe(401);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("unauthenticated");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("上游返回 401(令牌无效)→ 原样 401 透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "unauthenticated", message: "登录状态无效或已过期,请重新登录" },
          }),
          { status: 401 }
        )
      )
    );
    const res = await projectsGET(authed("/api/projects"));
    expect(res.status).toBe(401);
    const b = await bodyOf(res);
    expect(b?.error?.message).toContain("登录状态无效");
  });
});

describe("BFF 登录(正常链路)", () => {
  it("登录成功 → 200,写入 httpOnly 会话 Cookie,并携带内部凭据头", async () => {
    let seenHeaders: Record<string, string> = {};
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        seenHeaders = Object.fromEntries(
          new Headers(init?.headers).entries()
        );
        return new Response(
          JSON.stringify({
            access_token: "acc-1",
            refresh_token: "ref-1",
            token_type: "Bearer",
            expires_in: 600,
            principal: { user_id: "usr_1", account_id: "acct_1", tenant_id: "acct_1" },
          }),
          { status: 200 }
        );
      })
    );
    const res = await loginPOST(
      req("/api/auth/login", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ email: "a@b.com", password: "pw" }),
      })
    );
    expect(res.status).toBe(200);
    expect(seenHeaders["x-product-internal-token"]).toBe("test-internal-token");
    expect(seenHeaders["authorization"]).toBeUndefined(); // 登录前无会话
    expect(res.cookies.get("pia_access")?.value).toBe("acc-1");
    expect(res.cookies.get("pia_refresh")?.value).toBe("ref-1");
    const data = (await res.json()) as { principal?: { user_id?: string } };
    expect(data.principal?.user_id).toBe("usr_1");
  });
});

describe("BFF 服务不可达态", () => {
  it("上游连接失败 → 503 + 中文原因", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("fetch failed: connect ECONNREFUSED");
      })
    );
    const res = await projectsGET(authed("/api/projects"));
    expect(res.status).toBe(503);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("product_server_unavailable");
    expect(b?.error?.message).toContain("产品服务暂不可达");
  });
});

describe("BFF 开关 off 态", () => {
  it("提交生成:上游返回 FEATURE_GENERATION_ENABLED 关闭 → 503 + 开关名透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "generation_disabled",
              message:
                "生成能力开关未开启(FEATURE_GENERATION_ENABLED=0),生成任务不可用",
            },
          }),
          { status: 503 }
        )
      )
    );
    const res = await versionsPOST(
      authed("/api/projects/p1/versions", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      }),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res.status).toBe(503);
    const b = await bodyOf(res);
    expect(b?.error?.message).toContain("FEATURE_GENERATION_ENABLED");
  });

  it("费用事实:计费开关关闭 → 503 + ECO_BILLING_ENABLED 透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "billing_disabled",
              message: "计费开关未开启(ECO_BILLING_ENABLED=0),费用信息不可用",
            },
          }),
          { status: 503 }
        )
      )
    );
    const res = await balanceGET(authed("/api/billing/balance"));
    expect(res.status).toBe(503);
    const b = await bodyOf(res);
    expect(b?.error?.message).toContain("ECO_BILLING_ENABLED");
  });
});
