import { NextRequest } from "next/server";
import { accessTokenOf, SERVER_BASE, serverHeaders, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string; label: string }> };

export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, label } = await ctx.params;
    let res: Response;
    try {
      res = await fetch(`${SERVER_BASE}/api/v1/projects/${id}/revisions/${label}/content`, {
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
    const headers = new Headers({
      "Content-Type": res.headers.get("content-type") ?? "image/png",
      "Cache-Control": "no-store",
      "X-Revision-Version": res.headers.get("X-Revision-Version") ?? label,
    });
    const sha = res.headers.get("X-Revision-Sha256");
    if (sha) headers.set("X-Revision-Sha256", sha);
    return new Response(res.body, { status: 200, headers });
  } catch (e) {
    return toErrorResponse(e);
  }
}
