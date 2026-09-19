import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST /api/batches/[id]/submit — 批次提交(生成门/并发上限都在 Go server;
// 门不过时上游如实落 blocked 并 200 返回,BFF 原样透传)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/batches/${id}/submit`, {
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
