import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST 原样转发。影响范围和整数分都由 Go 产生。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    const data = await callServer(`/api/v1/projects/${id}/revisions/plan`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
