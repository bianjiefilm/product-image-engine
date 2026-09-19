import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// GET /api/projects — 租户内工程列表。
export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const data = await callServer("/api/v1/projects", { accessToken: access });
    return NextResponse.json(data);
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST /api/projects — 新建工程(无订单必须可用:source_type 可为 standalone/空)。
export async function POST(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer("/api/v1/projects", {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 201 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
