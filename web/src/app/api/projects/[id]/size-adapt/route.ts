import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// GET /api/projects/[id]/size-adapt — 工程下尺寸变体列表。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}/size-adapt`, {
      accessToken: access,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST /api/projects/[id]/size-adapt — 生成尺寸变体(纯确定性变换;同
// 源+预设+模式 幂等返回既有变体;BFF 零业务逻辑,原样转发)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/projects/${id}/size-adapt`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, {
      status: data.duplicate === true ? 200 : 201,
    });
  } catch (e) {
    return toErrorResponse(e);
  }
}
