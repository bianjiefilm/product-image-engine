import { NextRequest } from "next/server";
import { accessTokenOf, serverHeaders, SERVER_BASE, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string; jobId: string }> };

// GET 夹具字节代理。只在服务端已经允许展示时才会拿到图片。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, jobId } = await ctx.params;
    let res: Response;
    try {
      res = await fetch(`${SERVER_BASE}/api/v1/projects/${id}/text-images/${jobId}/download`, {
        headers: serverHeaders(access),
        cache: "no-store",
      });
    } catch {
      throw new UpstreamError(503, "product_server_unavailable", "产品服务暂不可达,请稍后重试");
    }
    if (!res.ok) {
      let code = "upstream_error";
      let message = "产品服务返回错误";
      try {
        const body = (await res.json()) as { error?: { code?: string; message?: string } };
        code = body.error?.code ?? code;
        message = body.error?.message ?? message;
      } catch {
        // 非 JSON 错误体保留状态码。
      }
      throw new UpstreamError(res.status, code, message);
    }
    return new Response(res.body, {
      status: 200,
      headers: {
        "Content-Type": res.headers.get("content-type") ?? "application/octet-stream",
        "Content-Disposition": res.headers.get("content-disposition") ?? 'inline; filename="fixture.png"',
      },
    });
  } catch (e) {
    return toErrorResponse(e);
  }
}
