import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { GET as capsGET } from "@/app/api/text-images/capabilities/route";
import { POST as createPOST } from "@/app/api/projects/[id]/text-images/route";
import { GET as detailGET } from "@/app/api/projects/[id]/text-images/[jobId]/route";
import { GET as downloadGET } from "@/app/api/projects/[id]/text-images/[jobId]/download/route";
import { POST as actionPOST } from "@/app/api/projects/[id]/text-images/[jobId]/[action]/route";

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

  it("夹具登记原样透传，不改成计费通过", async () => {
    const fetchMock = vi.fn(async () =>
      new Response(
        JSON.stringify({
          job: {
            id: "txt_fixture",
            job_status: "completed",
            origin: "fixture",
            show_image: true,
            billing_passed: false,
            production_authorized: false,
            real_generation_completed: false,
          },
          idempotent: false,
        }),
        { status: 201 }
      )
    );
    vi.stubGlobal("fetch", fetchMock);
    const res = await actionPOST(
      authed("/api/projects/prj/text-images/txt_fixture/fixture", {
        method: "POST",
        body: JSON.stringify({ fixture: "fixture", data_b64: "aGk=" }),
      }),
      { params: Promise.resolve({ id: "prj", jobId: "txt_fixture", action: "fixture" }) }
    );
    const body = await res.json();
    expect(res.status).toBe(200);
    expect(body.job.origin).toBe("fixture");
    expect(body.job.billing_passed).toBe(false);
    expect(body.job.real_generation_completed).toBe(false);
    const called = String(fetchMock.mock.calls[0]?.[0] ?? "");
    expect(called).toContain("/text-images/txt_fixture/fixture");
  });

  it("未知动作不打上游", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await actionPOST(
      authed("/api/projects/prj/text-images/txt_1/nope", {
        method: "POST",
        body: JSON.stringify({}),
      }),
      { params: Promise.resolve({ id: "prj", jobId: "txt_1", action: "nope" }) }
    );
    expect(res.status).toBe(404);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("详情透传夹具事实", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ job: { id: "txt_fixture", show_image: true, origin: "fixture" } }), { status: 200 })
      )
    );
    const res = await detailGET(authed("/api/projects/prj/text-images/txt_fixture"), {
      params: Promise.resolve({ id: "prj", jobId: "txt_fixture" }),
    });
    const body = await res.json();
    expect(res.status).toBe(200);
    expect(body.job.origin).toBe("fixture");
    expect(body.job.show_image).toBe(true);
  });

  it("未登录不下载夹具", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await downloadGET(new NextRequest("http://localhost:3000/api/projects/prj/text-images/txt/download"), {
      params: Promise.resolve({ id: "prj", jobId: "txt" }),
    });
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("下载透传夹具字节", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(new Uint8Array([1, 2, 3]), {
            status: 200,
            headers: { "Content-Type": "image/png", "Content-Disposition": 'inline; filename="fixture.png"' },
          })
      )
    );
    const res = await downloadGET(authed("/api/projects/prj/text-images/txt/download"), {
      params: Promise.resolve({ id: "prj", jobId: "txt" }),
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toContain("image/png");
    expect(res.headers.get("content-disposition")).toContain("fixture");
    expect(Array.from(new Uint8Array(await res.arrayBuffer()))).toEqual([1, 2, 3]);
  });
});
