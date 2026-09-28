import { NextRequest, NextResponse } from "next/server";
import { SERVER_BASE, accessTokenOf, serverHeaders, toErrorResponse, UpstreamError } from "@/lib/server";

async function proxy(req: NextRequest, path: string) {
  const access = accessTokenOf(req);
  if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
  const body = await req.json().catch(() => null);
  if (!body) throw new UpstreamError(400, "invalid_request", "请求体必须是 JSON");
  let res: Response;
  try {
    res = await fetch(SERVER_BASE + path, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...serverHeaders(access) },
      body: JSON.stringify(body),
      cache: "no-store",
    });
  } catch {
    throw new UpstreamError(503, "product_server_unavailable", "产品服务暂不可达,请稍后重试");
  }
  const data = (await res.json().catch(() => null)) ?? {};
  return NextResponse.json(data, { status: res.status });
}

export function consumeProxy(path: string) {
  return async function POST(req: NextRequest) {
    try {
      return await proxy(req, path);
    } catch (e) {
      return toErrorResponse(e);
    }
  };
}
