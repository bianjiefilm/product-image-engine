import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// GET /api/projects/[id]/outputs — 用户明确选择的成果列表。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}/outputs`, {
      accessToken: access,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST /api/projects/[id]/outputs — 登记成果(合法 PNG → 平台资产设施;不触发生成)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/projects/${id}/outputs`, {
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
