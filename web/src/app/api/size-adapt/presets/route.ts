import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// GET /api/size-adapt/presets — 公开常见规格预设表(BFF 零业务逻辑)。
// FEATURE_SIZE_ADAPT off → 上游 404 原样透传(前端据此隐藏面板)。
export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const data = await callServer(`/api/v1/size-adapt/presets`, {
      accessToken: access,
    });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
