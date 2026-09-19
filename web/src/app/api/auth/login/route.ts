import { NextRequest, NextResponse } from "next/server";
import {
  callServer,
  setAuthCookies,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// POST /api/auth/login — 邮箱+口令登录(走 platform-identity 标准路径)。
export async function POST(req: NextRequest) {
  try {
    const body = (await req.json().catch(() => null)) as {
      email?: string;
      password?: string;
    } | null;
    if (!body?.email?.trim() || !body?.password) {
      throw new UpstreamError(400, "invalid_request", "邮箱与口令不能为空");
    }
    const data = await callServer("/api/v1/auth/login", {
      method: "POST",
      body: { email: body.email.trim(), password: body.password },
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
