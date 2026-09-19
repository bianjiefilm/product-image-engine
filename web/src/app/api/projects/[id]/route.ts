import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// GET /api/projects/[id] — 工程详情聚合(基本信息+输入+版本)。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}`, {
      accessToken: access,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}

// PATCH /api/projects/[id] — 保存工程编辑。
export async function PATCH(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/projects/${id}`, {
      method: "PATCH",
      accessToken: access,
      body,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
