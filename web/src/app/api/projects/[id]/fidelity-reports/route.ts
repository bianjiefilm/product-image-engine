import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// GET /api/projects/[id]/fidelity-reports — 主体保真记录。开关关闭时上游 404。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}/fidelity-reports`, {
      accessToken: access,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST 只转发保护范围与检查。不附加结论、授权或扣费字段。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body || typeof body !== "object") {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/projects/${id}/fidelity-reports`, {
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
