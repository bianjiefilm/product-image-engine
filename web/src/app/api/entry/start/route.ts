import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

export async function POST(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const body = await req.json().catch(() => null);
    if (!body) throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    const data = await callServer("/api/v1/entry/start", {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
