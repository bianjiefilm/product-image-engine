import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}/motion-refs`, { accessToken: access });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST 原样转发修订编号。网关不生成视频，也不把引用改成动态广告。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body || typeof body !== "object") throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    const data = await callServer(`/api/v1/projects/${id}/motion-refs`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: data.created === true ? 201 : 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
