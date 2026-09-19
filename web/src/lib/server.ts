// BFF 服务端工具:页面只打 /api/*;这里把请求转发到 Go server(loopback)。
// 平台密钥只在 Go server env;本层只持 PRODUCT_SERVER_TOKEN(web→Go 内部凭据)。
import { NextResponse } from "next/server";

export const SERVER_BASE =
  process.env.PRODUCT_SERVER_BASE_URL ?? "http://127.0.0.1:18220";
const SERVER_TOKEN = process.env.PRODUCT_SERVER_TOKEN ?? "";

export const ACCESS_COOKIE = "pia_access";
export const REFRESH_COOKIE = "pia_refresh";

export class UpstreamError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export interface ServerCallInit {
  method?: string;
  body?: unknown;
  accessToken?: string;
}

// serverHeaders 调 Go server 的公共头(内部凭据 + 可选用户访问令牌)。
// callServer 之外,二进制代理(尺寸变体下载)也复用。
export function serverHeaders(accessToken?: string): Record<string, string> {
  const headers: Record<string, string> = {
    "X-Product-Internal-Token": SERVER_TOKEN,
  };
  if (accessToken) headers["Authorization"] = `Bearer ${accessToken}`;
  return headers;
}

// callServer 调 Go server;网络失败 → 503(中文),非 2xx → 原样状态码+错误体。
export async function callServer(
  path: string,
  init: ServerCallInit = {}
): Promise<Record<string, unknown>> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...serverHeaders(init.accessToken),
  };
  let res: Response;
  try {
    res = await fetch(SERVER_BASE + path, {
      method: init.method ?? "GET",
      headers,
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
      cache: "no-store",
    });
  } catch {
    throw new UpstreamError(
      503,
      "product_server_unavailable",
      "产品服务暂不可达,请稍后重试"
    );
  }
  let data: Record<string, unknown> | null = null;
  try {
    data = (await res.json()) as Record<string, unknown>;
  } catch {
    data = null;
  }
  if (!res.ok) {
    const err = (data?.error ?? {}) as { code?: string; message?: string };
    throw new UpstreamError(
      res.status,
      err.code ?? "upstream_error",
      err.message ?? "产品服务返回错误"
    );
  }
  return data ?? {};
}

// toErrorResponse 统一把 UpstreamError 变成 JSON 应答。
export function toErrorResponse(e: unknown): NextResponse {
  if (e instanceof UpstreamError) {
    return NextResponse.json(
      { error: { code: e.code, message: e.message } },
      { status: e.status }
    );
  }
  return NextResponse.json(
    { error: { code: "internal", message: "网关内部错误" } },
    { status: 500 }
  );
}

export function setAuthCookies(
  res: NextResponse,
  accessToken: string,
  refreshToken: string
) {
  const secure = process.env.PRODUCT_COOKIE_SECURE === "1";
  res.cookies.set(ACCESS_COOKIE, accessToken, {
    httpOnly: true,
    sameSite: "lax",
    path: "/",
    secure,
    maxAge: 60 * 60 * 12,
  });
  res.cookies.set(REFRESH_COOKIE, refreshToken, {
    httpOnly: true,
    sameSite: "lax",
    path: "/",
    secure,
    maxAge: 60 * 60 * 24 * 7,
  });
}

export function clearAuthCookies(res: NextResponse) {
  res.cookies.set(ACCESS_COOKIE, "", { httpOnly: true, path: "/", maxAge: 0 });
  res.cookies.set(REFRESH_COOKIE, "", { httpOnly: true, path: "/", maxAge: 0 });
}

// accessTokenOf 从请求 Cookie 取访问令牌。
export function accessTokenOf(req: Request): string | undefined {
  const header = req.headers.get("cookie") ?? "";
  for (const part of header.split(";")) {
    const [k, ...rest] = part.trim().split("=");
    if (k === ACCESS_COOKIE) return decodeURIComponent(rest.join("="));
  }
  return undefined;
}
