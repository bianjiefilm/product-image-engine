import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

// GET /api/auth/session — 返回当前登录身份;未登录/失效 401;平台不可达 503。
export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) {
      throw new UpstreamError(401, "unauthenticated", "未登录");
    }
    const data = await callServer("/api/v1/auth/session", {
      accessToken: access,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
