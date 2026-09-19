import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST /api/batches/[id]/collect — 手动收集(auto 查询参数原样转发,无业务加工)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const qs = req.nextUrl.search ? req.nextUrl.search : "";
    const data = await callServer(`/api/v1/batches/${id}/collect${qs}`, {
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
