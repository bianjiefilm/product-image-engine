import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST 原样转发。失败或未知只入账，已采用的版本不动。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    const data = await callServer(`/api/v1/projects/${id}/revisions/outcome`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
