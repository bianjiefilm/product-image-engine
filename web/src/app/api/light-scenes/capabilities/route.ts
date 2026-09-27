import { NextRequest, NextResponse } from "next/server";
import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";

async function proxy(req: NextRequest, path: string, method: string) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const body = method === "GET" ? undefined : await req.json().catch(() => ({}));
    const data = await callServer(path, { method, accessToken: access, body });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}

export async function GET(req: NextRequest) {
  return proxy(req, "/api/v1/light-scenes/capabilities", "GET");
}
