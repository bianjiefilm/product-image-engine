import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string }> };

// GET /api/projects/[id]/versions — 输出版本列表(任务与费用事实)。
export async function GET(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const data = await callServer(`/api/v1/projects/${id}/versions`, {
      accessToken: access,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST /api/projects/[id]/versions — 提交生成(占位链路,开关关闭/服务不可达一律 503,不伪造成功)。
export async function POST(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id } = await ctx.params;
    const body = (await req.json().catch(() => ({}))) ?? {};
    const data = await callServer(`/api/v1/projects/${id}/versions`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 201 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
