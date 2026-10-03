import { describe, expect, it } from "vitest";
import { MAX_PHOTO_BYTES, uploadAndAttach } from "@/components/plate-lock/PhotoUpload";

type Call = { url: string; method: string; body?: unknown };
function fetcher(calls: Call[], opts: { photoOk?: boolean; projectOk?: boolean; attachOk?: boolean } = {}): typeof fetch {
  const { photoOk = true, projectOk = true, attachOk = true } = opts;
  return (async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? "GET", body: init?.body });
    if (url.startsWith("/api/photos")) return photoOk ? Response.json({ photo: { platform_asset_id: "asset_1", original_name: "a.png", size_bytes: 3, media_type: "image/png" } }, { status: 201 }) : Response.json({ error: { message: "图片内容无法解码" } }, { status: 422 });
    if (url === "/api/projects") return projectOk ? Response.json({ project: { id: "proj_new" } }, { status: 201 }) : new Response("{}", { status: 500 });
    return attachOk ? Response.json({ input: { id: "pin_1" } }, { status: 201 }) : new Response("{}", { status: 500 });
  }) as typeof fetch;
}
const file = (size = 3, name = "a.png") => new File([new Uint8Array(size).fill(1)], name, { type: "image/png" });

describe("photo upload through the existing BFF routes", () => {
  it("with no project: uploads, creates a personal project, then attaches the input", async () => {
    const calls: Call[] = [];
    expect(await uploadAndAttach(file(), undefined, fetcher(calls))).toBe("proj_new");
    expect(calls.map(c => `${c.method} ${c.url.split("?")[0]}`)).toEqual(["POST /api/photos", "POST /api/projects", "POST /api/projects/proj_new/inputs"]);
    expect(calls[0].url).toContain("purpose=project_photo");
    expect(JSON.parse(String(calls[1].body))).toEqual({ name: "个人商品图", usage_kind: "电商主图", width_px: 1024, height_px: 1024, source_type: "standalone" });
    expect(JSON.parse(String(calls[2].body))).toMatchObject({ platform_asset_id: "asset_1", snapshot_content_type: "image/png" });
  });

  it("with a project: never creates another one", async () => {
    const calls: Call[] = [];
    expect(await uploadAndAttach(file(), "proj_a", fetcher(calls))).toBe("proj_a");
    expect(calls.some(c => c.url === "/api/projects")).toBe(false);
  });

  it("the server's rejection text reaches the user and nothing is attached", async () => {
    const calls: Call[] = [];
    await expect(uploadAndAttach(file(), "proj_a", fetcher(calls, { photoOk: false }))).rejects.toThrow("图片内容无法解码");
    expect(calls).toHaveLength(1);
  });

  it("refuses empty and oversized files before any request", async () => {
    const calls: Call[] = [];
    await expect(uploadAndAttach(file(0), undefined, fetcher(calls))).rejects.toThrow("文件是空的");
    await expect(uploadAndAttach({ size: MAX_PHOTO_BYTES + 1, arrayBuffer: async () => new ArrayBuffer(0), name: "x.png" } as unknown as File, undefined, fetcher(calls))).rejects.toThrow("20 MB");
    expect(calls).toEqual([]);
  });

  it("a failed project or attach step is reported, not swallowed", async () => {
    await expect(uploadAndAttach(file(), undefined, fetcher([], { projectOk: false }))).rejects.toThrow("创建工程没有成功");
    await expect(uploadAndAttach(file(), "proj_a", fetcher([], { attachOk: false }))).rejects.toThrow("没有加入工程");
  });
});
