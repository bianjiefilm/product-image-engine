import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// GET /api/bindings?project_id=… — 来源绑定列表(可按工程过滤)。
export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const qs = req.nextUrl.search; // 原样透传 ?project_id=…
    const data = await callServer(`/api/v1/bindings${qs}`, {
      accessToken: access,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
