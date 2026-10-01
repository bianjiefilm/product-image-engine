import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { POST as startPOST } from "@/app/api/entry/start/route";

function req(url: string, init: NonNullable<ConstructorParameters<typeof NextRequest>[1]> = {}): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, init);
}

beforeEach(() => {
  vi.restoreAllMocks();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("入口 BFF", () => {
  it("未登录不能创建个人入口，也不打上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await startPOST(
      req("/api/entry/start", {
        method: "POST",
        body: JSON.stringify({ name: "春季主图" }),
      })
    );
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
