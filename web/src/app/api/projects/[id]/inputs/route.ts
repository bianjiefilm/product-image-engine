import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// POST /api/projects/[id]/inputs — 挂素材引用(平台 asset_id + 本地快照元数据)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/projects/${id}/inputs`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 201 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
