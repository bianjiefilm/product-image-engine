import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  clearAuthCookies,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// POST /api/auth/logout — 吊销平台会话并清 Cookie。
export async function POST(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) {
      throw new UpstreamError(401, "unauthenticated", "未登录");
    }
    const body = (await req.json().catch(() => ({}))) as {
      refresh_token?: string;
    };
    await callServer("/api/v1/auth/logout", {
      method: "POST",
      accessToken: access,
      body: { refresh_token: body.refresh_token ?? "" },
    });
    const res = new NextResponse(null, { status: 204 });
    clearAuthCookies(res);
    return res;
  } catch (e) {
    const res = toErrorResponse(e);
    if (res.status === 401) clearAuthCookies(res);
    return res;
  }
}
