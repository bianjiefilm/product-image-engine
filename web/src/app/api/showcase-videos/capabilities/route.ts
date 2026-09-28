import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const data = await callServer("/api/v1/showcase-videos/capabilities", { accessToken: access });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
