// 批量生成 BFF 测试(HUI-1704):鉴权 401 / 纯透传(建批 201、动作 200)/
// 错误原样回传(403 not_owner、409 invalid_state)/ 服务不可达 503 中文。
// BFF 零业务逻辑:断言请求体与路径原样转发,状态码跟随上游语义。
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

import { GET as listGET, POST as createPOST } from "@/app/api/batches/route";
import { GET as detailGET } from "@/app/api/batches/[id]/route";
import { POST as submitPOST } from "@/app/api/batches/[id]/submit/route";
import { POST as collectPOST } from "@/app/api/batches/[id]/collect/route";
import { POST as retryPOST } from "@/app/api/batches/[id]/retry-failed/route";
import { POST as cancelPOST } from "@/app/api/batches/[id]/cancel/route";

type Ctx = { params: Promise<{ id: string }> };
const ctxOf = (id: string): Ctx => ({ params: Promise.resolve({ id }) });

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
    batches?: unknown[];
    batch?: { id?: string; counts?: { total?: number } };
    note?: string;
  } | null;
}

describe("批次列表/建批 BFF", () => {
  it("无 Cookie → 401,不触达上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await listGET(req("/api/batches"));
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("列表透传:携带内部凭据 + 用户令牌", async () => {
    let seen: Record<string, string> = {};
    let seenURL = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        seenURL = String(url);
        seen = Object.fromEntries(new Headers(init?.headers).entries());
        return new Response(JSON.stringify({ batches: [{ id: "b-1" }] }), {
          status: 200,
        });
      })
    );
    const res = await listGET(authed("/api/batches"));
    expect(res.status).toBe(200);
    expect(seenURL).toContain("/api/v1/batches");
    expect(seen["x-product-internal-token"]).toBe("test-internal-token");
    expect(seen["authorization"]).toBe("Bearer good-token");
    const b = await bodyOf(res);
    expect(b?.batches?.length).toBe(1);
  });

  it("建批:请求体原样转发,成功回 201", async () => {
    let lastURL = "";
    let lastBody = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        lastURL = String(url);
        lastBody = String(init?.body ?? "");
        return new Response(
          JSON.stringify({
            batch: { id: "b-1", status: "pending", counts: { total: 2 } },
            items: [{ id: "i-1" }, { id: "i-2" }],
            duplicates: ["a.png"],
          }),
          { status: 201 }
        );
      })
    );
    const payload = {
      name: "秋季批次",
      scenes: ["白底主图"],
      presets: ["taobao_main"],
      finalize_with_variants: true,
      items: [{ file_name: "a.png", data_b64: "aGVsbG8=" }],
    };
    const res = await createPOST(
      authed("/api/batches", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(payload),
      })
    );
    expect(res.status).toBe(201);
    expect(lastURL).toContain("/api/v1/batches");
    expect(JSON.parse(lastBody)).toEqual(payload);
    const b = await bodyOf(res);
    expect(b?.batch?.id).toBe("b-1");
    expect(b?.batch?.counts?.total).toBe(2);
  });

  it("非法请求体(非 JSON)→ 400,不触达上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await createPOST(
      authed("/api/batches", { method: "POST", body: "not-json" })
    );
    expect(res.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("批次动作 BFF(透传)", () => {
  it("详情/提交/收集/重试/取消:路径与方法正确转发,成功回 200", async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        calls.push(String(url));
        return new Response(
          JSON.stringify({ batch: { id: "b-9", status: "running" }, items: [] }),
          { status: 200 }
        );
      })
    );
    const detail = await detailGET(authed("/api/batches/b-9"), ctxOf("b-9"));
    expect(detail.status).toBe(200);
    const submit = await submitPOST(
      authed("/api/batches/b-9/submit", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(submit.status).toBe(200);
    const collect = await collectPOST(
      authed("/api/batches/b-9/collect?auto=true", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(collect.status).toBe(200);
    const retry = await retryPOST(
      authed("/api/batches/b-9/retry-failed", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(retry.status).toBe(200);
    const cancel = await cancelPOST(
      authed("/api/batches/b-9/cancel", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(cancel.status).toBe(200);
    expect(calls.some((u) => u.endsWith("/api/v1/batches/b-9"))).toBe(true);
    expect(calls.some((u) => u.endsWith("/api/v1/batches/b-9/submit"))).toBe(true);
    expect(calls.some((u) => u.includes("/api/v1/batches/b-9/collect?auto=true"))).toBe(true);
    expect(calls.some((u) => u.endsWith("/api/v1/batches/b-9/retry-failed"))).toBe(true);
    expect(calls.some((u) => u.endsWith("/api/v1/batches/b-9/cancel"))).toBe(true);
  });

  it("403 not_owner / 409 invalid_state 原样透传(码与文案不变)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "not_owner", message: "只有批次创建者可以取消" },
          }),
          { status: 403 }
        )
      )
    );
    const res = await cancelPOST(
      authed("/api/batches/b-9/cancel", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(res.status).toBe(403);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("not_owner");

    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "invalid_state", message: "已提交批次不可取消(任务不撤单)" },
          }),
          { status: 409 }
        )
      )
    );
    const res9 = await cancelPOST(
      authed("/api/batches/b-9/cancel", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(res9.status).toBe(409);
  });

  it("服务不可达 → 503 中文;无 Cookie → 401", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("fetch failed");
      })
    );
    const res = await submitPOST(
      authed("/api/batches/b-9/submit", { method: "POST" }),
      ctxOf("b-9")
    );
    expect(res.status).toBe(503);
    const b = await bodyOf(res);
    expect(b?.error?.message).toContain("产品服务暂不可达");

    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const noAuth = await detailGET(req("/api/batches/b-9"), ctxOf("b-9"));
    expect(noAuth.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
