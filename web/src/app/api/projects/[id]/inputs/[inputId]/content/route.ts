// HUI-1697 FEAT-0198:工程输入内容解析 BFF(受限二进制中继)。
// 覆盖本租户照片与跨应用授权版本引用:解析事实全部由 Go server
// 经平台设施完成,本层零业务判断,仅流式回传字节。
import { NextRequest } from "next/server";
import {
  accessTokenOf,
  serverHeaders,
  SERVER_BASE,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string; inputId: string }> };

export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, inputId } = await ctx.params;
    let res: Response;
    try {
      res = await fetch(
        `${SERVER_BASE}/api/v1/projects/${id}/inputs/${inputId}/content`,
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
        "Content-Type": res.headers.get("content-type") ?? "application/octet-stream",
        "Cache-Control": "no-store",
      },
    });
  } catch (e) {
    return toErrorResponse(e);
  }
}
