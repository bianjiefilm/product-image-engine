import { NextRequest, NextResponse } from "next/server";
import {
  callServer,
  setAuthCookies,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// POST /api/auth/refresh — 续签会话(平台红线:refresh 必带 app_id,由 Go server 注入)。
export async function POST(req: NextRequest) {
  try {
    const body = (await req.json().catch(() => null)) as {
      refresh_token?: string;
    } | null;
    if (!body?.refresh_token) {
      throw new UpstreamError(400, "invalid_request", "缺少 refresh_token");
    }
    const data = await callServer("/api/v1/auth/refresh", {
      method: "POST",
      body: { refresh_token: body.refresh_token },
    });
    const pair = data as unknown as {
      access_token: string;
      refresh_token: string;
      principal: unknown;
    };
    const res = NextResponse.json({ principal: pair.principal });
    setAuthCookies(res, pair.access_token, pair.refresh_token);
    return res;
  } catch (e) {
    return toErrorResponse(e);
  }
}
