import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST /api/batches/[id]/retry-failed — 只重试 failed 项(新任务引用);
// blocked 项不盲目重试,提示文案由 Go server 给出,原样透传。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/batches/${id}/retry-failed`, {
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
