import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { GET as capsGET } from "@/app/api/text-images/capabilities/route";
import { POST as createPOST } from "@/app/api/projects/[id]/text-images/route";

function authed(url: string, init?: RequestInit): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, {
    ...init,
    headers: { cookie: "pia_access=good-token", ...(init?.headers ?? {}) },
  });
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("文字生成 BFF", () => {
  it("未登录不打上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await capsGET(new NextRequest("http://localhost:3000/api/text-images/capabilities"));
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("透传失败事实，不把输出编号改成成功图", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            job: {
              id: "txt_1",
              job_status: "failed",
              billing_label: "计费待确认",
              show_image: false,
              billing_passed: false,
              production_authorized: false,
              subject_protected: false,
              output_is_receipt: false,
            },
          }),
          { status: 200 }
        )
      )
    );
    const res = await createPOST(
      authed("/api/projects/prj/text-images", {
        method: "POST",
        body: JSON.stringify({ prompt: "白色陶瓷杯" }),
      }),
      { params: Promise.resolve({ id: "prj" }) }
    );
    const body = await res.json();
    expect(body.job.job_status).toBe("failed");
    expect(body.job.show_image).toBe(false);
    expect(body.job.billing_label).toBe("计费待确认");
    expect(body.job.billing_passed).toBe(false);
    expect(body.job.production_authorized).toBe(false);
    expect(body.job.output_is_receipt).toBe(false);
  });
});
