import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { GET as capsGET } from "@/app/api/showcase-videos/capabilities/route";
import { POST as createPOST } from "@/app/api/projects/[id]/showcase-videos/route";

function authed(url: string, init?: NonNullable<ConstructorParameters<typeof NextRequest>[1]>): NextRequest {
  return new NextRequest(`http://localhost:3000${url}`, {
    ...init,
    headers: { cookie: "pia_access=good-token", ...(init?.headers ?? {}) },
  });
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("展示视频 BFF", () => {
  it("未登录不打上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await capsGET(new NextRequest("http://localhost:3000/api/showcase-videos/capabilities"));
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("透传失败事实，不把静图或输出编号改成视频", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            job: {
              id: "vid_1",
              job_status: "failed",
              billing_label: "计费待确认",
              show_video: false,
              play_still: false,
              video_url: "",
              billing_passed: false,
              production_authorized: false,
              output_is_receipt: false,
            },
          }),
          { status: 200 }
        )
      )
    );
    const res = await createPOST(
      authed("/api/projects/prj/showcase-videos", {
        method: "POST",
        body: JSON.stringify({ product_image_id: "in_cup", camera_move: "360" }),
      }),
      { params: Promise.resolve({ id: "prj" }) }
    );
    const body = await res.json();
    expect(body.job.job_status).toBe("failed");
    expect(body.job.show_video).toBe(false);
    expect(body.job.play_still).toBe(false);
    expect(body.job.video_url).toBe("");
    expect(body.job.billing_label).toBe("计费待确认");
    expect(body.job.billing_passed).toBe(false);
    expect(body.job.production_authorized).toBe(false);
    expect(body.job.output_is_receipt).toBe(false);
  });
});
