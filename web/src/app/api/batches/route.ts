import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// GET /api/batches — 批次列表(纯透传,BFF 零业务逻辑)。
export async function GET(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const data = await callServer(`/api/v1/batches`, { accessToken: access });
    return NextResponse.json(data, { status: 200 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

// POST /api/batches — 建批(多文件 base64 + 场景/预设意图原样转发;
// 输入门/去重/登记全部在 Go server,这里不复制逻辑)。
export async function POST(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const body = await req.json().catch(() => null);
    if (!body) {
      throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
    }
    const data = await callServer(`/api/v1/batches`, {
      method: "POST",
      accessToken: access,
      body,
    });
    return NextResponse.json(data, { status: 201 });
  } catch (e) {
    return toErrorResponse(e);
  }
}

export const dynamic = "force-dynamic";
