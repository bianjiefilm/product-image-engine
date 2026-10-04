import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const left = req.nextUrl.searchParams.get("left") ?? "";
    const right = req.nextUrl.searchParams.get("right") ?? "";
    const data = await callServer(
      `/api/v1/projects/${id}/revisions/compare?left=${encodeURIComponent(left)}&right=${encodeURIComponent(right)}`,
      { accessToken: access }
    );
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
