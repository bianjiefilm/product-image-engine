import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string; refId: string }> };

// POST 只转发价格或颜色参数。不在网关重做商品，也不补模型调用。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, refId } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body || typeof body !== "object") throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    const data = await callServer(`/api/v1/projects/${id}/motion-refs/${refId}/parameters`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
