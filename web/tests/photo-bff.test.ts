// 照片上传 BFF 测试(HUI-1697 FEAT-0198):受限中继断言——
// 鉴权 401 / 字节与查询原样透传 / 幂等 200 与新建 201 透传 /
// 机器错误码(413/415/422/404)原样透传 / 服务不可达 503 / 二进制解析透传。
// 纪律:BFF 零业务判断——本文件同时钉住“BFF 不改写错误码、不吞幂等语义”。
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

import { POST as uploadPOST } from "@/app/api/photos/route";
import { GET as photoContentGET } from "@/app/api/photos/[id]/content/route";
import { GET as inputContentGET } from "@/app/api/projects/[id]/inputs/[inputId]/content/route";

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

// 最小合法 PNG 头 + 填充(内容形状由服务端核验,BFF 不作数)。
const png = new Uint8Array([
  0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4,
]);

describe("照片上传 BFF(受限中继)", () => {
  it("无 Cookie → 401,不触达上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await uploadPOST(
      req("/api/photos?purpose=project_photo", { method: "POST", body: png })
    );
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("原始字节+查询串原样透传,并携带内部凭据与会话头", async () => {
    let seenURL = "";
    let seenHeaders: Record<string, string> = {};
    let seenBody = new Uint8Array();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        seenURL = url;
        seenHeaders = Object.fromEntries(new Headers(init?.headers).entries());
        seenBody = new Uint8Array(await (init?.body as ArrayBuffer));
        return new Response(
          JSON.stringify({
            photo: { id: "photo_1", platform_asset_id: "ast_1", sha256: "ab" },
            idempotent: false,
          }),
          { status: 201 }
        );
      })
    );
    const res = await uploadPOST(
      authed("/api/photos?purpose=project_photo&filename=%E4%B8%BB%E5%9B%BE.png", {
        method: "POST",
        body: png.buffer.slice(0),
      })
    );
    expect(res.status).toBe(201);
    expect(seenURL).toContain("http://127.0.0.1:18220/api/v1/photos?purpose=project_photo&filename=%E4%B8%BB%E5%9B%BE.png");
    expect(seenHeaders["x-product-internal-token"]).toBe("test-internal-token");
    expect(seenHeaders["authorization"]).toBe("Bearer good-token");
    expect(Array.from(seenBody)).toEqual(Array.from(png)); // 字节原样,不改写
    const b = (await res.json()) as { photo?: { id?: string }; idempotent?: boolean };
    expect(b.photo?.id).toBe("photo_1");
    expect(b.idempotent).toBe(false);
  });

  it("幂等重传:上游 200+idempotent=true 原样透传(不吞幂等语义)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({ photo: { id: "photo_1" }, idempotent: true }),
          { status: 200 }
        )
      )
    );
    const res = await uploadPOST(
      authed("/api/photos?purpose=project_photo", { method: "POST", body: png.buffer.slice(0) })
    );
    expect(res.status).toBe(200);
    const b = (await res.json()) as { idempotent?: boolean };
    expect(b.idempotent).toBe(true);
  });

  it("服务端机器错误码(413/415/422)原样透传,不改写", async () => {
    const cases: Array<[number, string]> = [
      [413, "photo_too_large"],
      [415, "unsupported_media_type"],
      [422, "undecodable_image"],
      [400, "invalid_purpose"],
    ];
    for (const [status, code] of cases) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          new Response(JSON.stringify({ error: { code, message: "中文原因" } }), { status })
        )
      );
      const res = await uploadPOST(
        authed("/api/photos?purpose=project_photo", { method: "POST", body: png.buffer.slice(0) })
      );
      expect(res.status).toBe(status);
      const b = (await res.json()) as { error?: { code?: string } };
      expect(b.error?.code).toBe(code);
    }
  });

  it("服务不可达 → 503 中文", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("fetch failed");
      })
    );
    const res = await uploadPOST(
      authed("/api/photos?purpose=project_photo", { method: "POST", body: png.buffer.slice(0) })
    );
    expect(res.status).toBe(503);
    const b = (await res.json()) as { error?: { message?: string } };
    expect(b.error?.message).toContain("产品服务暂不可达");
  });
});

describe("照片/输入内容解析 BFF(二进制透传)", () => {
  it("照片内容:Content-Type 透传,字节一致", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(png, {
          status: 200,
          headers: { "Content-Type": "image/png" },
        })
      )
    );
    const res = await photoContentGET(authed("/api/photos/photo_1/content"), {
      params: Promise.resolve({ id: "photo_1" }),
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/png");
    const got = new Uint8Array(await res.arrayBuffer());
    expect(Array.from(got)).toEqual(Array.from(png));
  });

  it("输入内容:上游 404(跨租户不可见)→ 状态码与错误体透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "not_found", message: "资源不存在(或不属于当前账号)" },
          }),
          { status: 404 }
        )
      )
    );
    const res = await inputContentGET(
      authed("/api/projects/p1/inputs/pin-x/content"),
      { params: Promise.resolve({ id: "p1", inputId: "pin-x" }) }
    );
    expect(res.status).toBe(404);
    const b = (await res.json()) as { error?: { code?: string } };
    expect(b.error?.code).toBe("not_found");
  });

  it("完整性失败(integrity_failed)原样透传", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: "integrity_failed", message: "平台资产事实与本地登记不符" },
          }),
          { status: 502 }
        )
      )
    );
    const res = await photoContentGET(authed("/api/photos/photo_1/content"), {
      params: Promise.resolve({ id: "photo_1" }),
    });
    expect(res.status).toBe(502);
    const b = (await res.json()) as { error?: { code?: string } };
    expect(b.error?.code).toBe("integrity_failed");
  });

  it("服务不可达 → 503 中文;无 Cookie → 401 不触上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const noAuth = await photoContentGET(req("/api/photos/photo_1/content"), {
      params: Promise.resolve({ id: "photo_1" }),
    });
    expect(noAuth.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();

    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("fetch failed");
      })
    );
    const res = await inputContentGET(
      authed("/api/projects/p1/inputs/pin-x/content"),
      { params: Promise.resolve({ id: "p1", inputId: "pin-x" }) }
    );
    expect(res.status).toBe(503);
    const b = (await res.json()) as { error?: { message?: string } };
    expect(b.error?.message).toContain("产品服务暂不可达");
  });
});
