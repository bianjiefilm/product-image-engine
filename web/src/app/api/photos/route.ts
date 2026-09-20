// HUI-1697 FEAT-0198:产品照片上传 BFF 路由。
// 受限中继纪律:本层零业务判断——只补登录会话与内部凭据头,
// 原始字节与查询参数(purpose/filename)原样透传给 Go server;
// 校验链(主体/租户/用途/大小/魔数/真解码/幂等)全部在服务端单点完成。
import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  serverHeaders,
  SERVER_BASE,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

// POST /api/photos?purpose=project_photo&filename=x.png — 原始字节中继。
export async function POST(req: NextRequest) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const query = req.nextUrl.search; // 原样透传(purpose/filename)
    const bytes = await req.arrayBuffer();
    let res: Response;
    try {
      res = await fetch(`${SERVER_BASE}/api/v1/photos${query}`, {
        method: "POST",
        headers: serverHeaders(access),
        body: bytes,
        cache: "no-store",
      });
    } catch {
      throw new UpstreamError(
        503,
        "product_server_unavailable",
        "产品服务暂不可达,请稍后重试"
      );
    }
    // JSON 应答(201 新建 / 200 幂等 / 4xx·5xx 机器错误码)原样透传。
    const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    return NextResponse.json(data, { status: res.status });
  } catch (e) {
    return toErrorResponse(e);
  }
}
