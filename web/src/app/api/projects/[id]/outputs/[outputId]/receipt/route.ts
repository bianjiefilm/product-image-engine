import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string; outputId: string }> };

// POST /api/projects/[id]/outputs/[outputId]/receipt — 定向回执(只回传既有成果)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, outputId } = await ctx.params;
    const data = await callServer(
      `/api/v1/projects/${id}/outputs/${outputId}/receipt`,
      { method: "POST", accessToken: access, body: {} }
    );
    return NextResponse.json(data, {
      status: data.duplicate === true ? 200 : 201,
    });
  } catch (e) {
    return toErrorResponse(e);
  }
}
