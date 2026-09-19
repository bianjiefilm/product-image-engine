import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  serverHeaders,
  SERVER_BASE,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string; variantId: string }> };

// GET /api/projects/[id]/size-adapt/[variantId]/download — 变体字节流代理。
// 二进制透传(不走 callServer):错误(JSON)按状态码透传,成功流式回传
// Content-Type / Content-Disposition(attachment)。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, variantId } = await ctx.params;
    let res: Response;
    try {
      res = await fetch(
        `${SERVER_BASE}/api/v1/projects/${id}/size-adapt/${variantId}/download`,
        { headers: serverHeaders(access), cache: "no-store" }
      );
    } catch {
      throw new UpstreamError(
        503,
        "product_server_unavailable",
        "产品服务暂不可达,请稍后重试"
      );
    }
    if (!res.ok) {
      let code = "upstream_error";
      let message = "产品服务返回错误";
      try {
        const body = (await res.json()) as {
          error?: { code?: string; message?: string };
        };
        code = body.error?.code ?? code;
        message = body.error?.message ?? message;
      } catch {
        // 非 JSON 错误体:保留默认文案与状态码。
      }
      throw new UpstreamError(res.status, code, message);
    }
    return new Response(res.body, {
      status: 200,
      headers: {
        "Content-Type":
          res.headers.get("content-type") ?? "application/octet-stream",
        "Content-Disposition":
          res.headers.get("content-disposition") ?? "attachment",
      },
    });
  } catch (e) {
    return toErrorResponse(e);
  }
}
