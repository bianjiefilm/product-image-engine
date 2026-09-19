// 尺寸适配 BFF 测试(HUI-1703):鉴权 401 / 开关 off 404 透传 / 生成幂等
// (duplicate→200, 新建→201)/ 错误透传 / 下载二进制透传 / 服务不可达 503。
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

import { GET as presetsGET } from "@/app/api/size-adapt/presets/route";
import {
  GET as listGET,
  POST as generatePOST,
} from "@/app/api/projects/[id]/size-adapt/route";
import { GET as downloadGET } from "@/app/api/projects/[id]/size-adapt/[variantId]/download/route";

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
    duplicate?: boolean;
    presets?: unknown[];
  } | null;
}

describe("尺寸适配预设表 BFF", () => {
  it("无 Cookie → 401,不触达上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await presetsGET(req("/api/size-adapt/presets"));
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("开关 off:上游 404 → 原样透传(前端据此隐藏面板)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("404 page not found", { status: 404 }))
    );
    const res = await presetsGET(authed("/api/size-adapt/presets"));
    expect(res.status).toBe(404);
  });

  it("开关 on:预设表透传并携带内部凭据头", async () => {
    let seen: Record<string, string> = {};
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        seen = Object.fromEntries(new Headers(init?.headers).entries());
        return new Response(
          JSON.stringify({ presets: [{ name: "taobao_main", width: 800, height: 800, mode: "pad" }] }),
          { status: 200 }
        );
      })
    );
    const res = await presetsGET(authed("/api/size-adapt/presets"));
    expect(res.status).toBe(200);
    expect(seen["x-product-internal-token"]).toBe("test-internal-token");
    expect(seen["authorization"]).toBe("Bearer good-token");
    const b = await bodyOf(res);
    expect(b?.presets?.length).toBe(1);
  });
});

describe("尺寸适配生成/列表 BFF", () => {
  it("无 Cookie → 401;服务不可达 → 503 中文", async () => {
    vi.stubGlobal("fetch", vi.fn());
    const noAuth = await listGET(req("/api/projects/p1/size-adapt"), {
      params: Promise.resolve({ id: "p1" }),
    });
    expect(noAuth.status).toBe(401);

    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("fetch failed");
      })
    );
    const res = await generatePOST(
      authed("/api/projects/p1/size-adapt", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      }),
      { params: Promise.resolve({ id: "p1" }) }
    );
    expect(res.status).toBe(503);
    const b = await bodyOf(res);
    expect(b?.error?.message).toContain("产品服务暂不可达");
  });

  it("生成:新建 → 201;重复 → 200 duplicate(请求体原样转发)", async () => {
    const upstream: Array<{ status: number; body: unknown }> = [];
    let lastBody = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        lastBody = String(init?.body ?? "");
        const nxt = upstream.shift() ?? { status: 500, body: {} };
        return new Response(JSON.stringify(nxt.body), { status: nxt.status });
      })
    );
    const call = () =>
      generatePOST(
        authed("/api/projects/p1/size-adapt", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            source_output_id: "out-1",
            preset_name: "taobao_main",
            format: "png",
            data_b64: "aGVsbG8=",
          }),
        }),
        { params: Promise.resolve({ id: "p1" }) }
      );
    upstream.push({
      status: 201,
      body: { variant: { id: "v-1", preset_name: "taobao_main" }, duplicate: false },
    });
    const first = await call();
    expect(first.status).toBe(201);
    expect(JSON.parse(lastBody).preset_name).toBe("taobao_main");
    upstream.push({
      status: 200,
      body: { variant: { id: "v-1", preset_name: "taobao_main" }, duplicate: true },
    });
    const repeat = await call();
    expect(repeat.status).toBe(200);
    const b = await bodyOf(repeat);
    expect(b?.duplicate).toBe(true);
  });

  it("跨租户 403 原样透传(错误码/文案不变)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "tenant_mismatch", message: "工程不属于当前账号(拒绝,无串数据)" },
          }),
          { status: 403 }
        )
      )
    );
    const res = await listGET(authed("/api/projects/p1/size-adapt"), {
      params: Promise.resolve({ id: "p1" }),
    });
    expect(res.status).toBe(403);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("tenant_mismatch");
  });
});

describe("尺寸适配下载 BFF(二进制透传)", () => {
  it("成功:Content-Type/Disposition 透传,字节一致", async () => {
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(png, {
          status: 200,
          headers: {
            "Content-Type": "image/png",
            "Content-Disposition": 'attachment; filename="source-taobao_main.png"',
          },
        })
      )
    );
    const res = await downloadGET(
      authed("/api/projects/p1/size-adapt/v-1/download"),
      { params: Promise.resolve({ id: "p1", variantId: "v-1" }) }
    );
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/png");
    expect(res.headers.get("content-disposition")).toContain("attachment");
    const got = new Uint8Array(await res.arrayBuffer());
    expect(Array.from(got)).toEqual(Array.from(png));
  });

  it("上游 404(变体不存在)→ 状态码与错误体透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({ error: { code: "not_found", message: "尺寸变体不存在(或不属于该工程)" } }),
          { status: 404 }
        )
      )
    );
    const res = await downloadGET(
      authed("/api/projects/p1/size-adapt/v-nope/download"),
      { params: Promise.resolve({ id: "p1", variantId: "v-nope" }) }
    );
    expect(res.status).toBe(404);
    const b = await bodyOf(res);
    expect(b?.error?.code).toBe("not_found");
  });
});
