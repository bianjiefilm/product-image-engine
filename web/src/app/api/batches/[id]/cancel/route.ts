import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST /api/batches/[id]/cancel — 取消(仅创建者、仅 pending/全 blocked 终态;
// 已提交任务不撤单的裁决在 Go server,这里纯透传,403/409 原样回传)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/batches/${id}/cancel`, {
      method: "POST",
      accessToken: access,
      body: {},
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

export const dynamic = "force-dynamic";
