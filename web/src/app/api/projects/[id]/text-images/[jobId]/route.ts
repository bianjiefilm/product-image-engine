import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string; jobId: string }> };

export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, jobId } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}/text-images/${jobId}`, { accessToken: access });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
