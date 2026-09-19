import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST /api/bindings/[id]/adopt — 用户确认差异后采用新需求版本(仅移动指针)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/bindings/${id}/adopt`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
