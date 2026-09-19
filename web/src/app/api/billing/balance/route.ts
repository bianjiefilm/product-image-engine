import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// GET /api/billing/balance — 费用事实只读(开关 off / 计费不可达 → 503 中文原因)。
export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const data = await callServer("/api/v1/billing/balance", {
      accessToken: access,
    });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}
