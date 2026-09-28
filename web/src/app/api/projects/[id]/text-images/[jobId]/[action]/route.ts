import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

const actions = new Set(["confirm", "submit", "refresh", "claim"]);

type Ctx = { params: Promise<{ id: string; jobId: string; action: string }> };

export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, jobId, action } = await ctx.params;
    if (!actions.has(action)) {
      throw new UpstreamError(404, "not_found", "未知的文字生成动作");
    }
    const body = await req.json().catch(() => ({}));
    const data = await callServer(`/api/v1/projects/${id}/text-images/${jobId}/${action}`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
