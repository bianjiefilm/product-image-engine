// HUI-2227 残差②：登记真源消费（/api/eco-nav/launches BFF + 纯函数映射）。
// 真源=public-ai production-manifest.json（web/src/lib/eco-nav/registry/ 逐字节
// vendored，见同目录 SOURCE.txt）。客户端不再内联 NEXT_PUBLIC_ECO_APP_LAUNCHES。
import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

import { GET as launchesGET } from "@/app/api/eco-nav/launches/route";
import { emittable, registeredLaunchRows } from "@/lib/eco-nav/registry";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (p: string) => readFileSync(path.join(webRoot, p), "utf8");

describe("登记真源（production-manifest.json vendored）", () => {
  it("vendored 文件与 public-ai 生产表一致（blob 对拍由 SOURCE.txt 记录，此处验结构）", () => {
    const raw = JSON.parse(read("src/lib/eco-nav/registry/production-manifest.json"));
    expect(raw.manifest_version).toBe("app-registry/v1");
    const ids = raw.apps.map((a: { app_id: string }) => a.app_id).sort();
    expect(ids).toEqual(["agentbuddy", "aicut", "dhuman", "goboost", "pilotseaview"]);
    // 生产表不含本应用与未部署行（诚实缺失）
    expect(ids).not.toContain("product-image");
    expect(ids).not.toContain("touch-engine");
    expect(ids).not.toContain("orders");
  });

  it("registeredLaunchRows 只含可发射的真实地址行；.invalid/本应用行不出现", () => {
    const rows = registeredLaunchRows();
    const byTarget = new Map(rows.map((r) => [r.target_id, r.url]));
    expect(byTarget.get("ti-goboost-web")).toBe("https://goboost.modelxing.com/projects");
    expect(byTarget.get("ti-aicut-web")).toBe("https://aicut.modelxing.com/");
    expect(byTarget.get("ti-dhuman-web")).toBe("https://dhuman.modelxing.com/");
    expect(byTarget.get("ti-psv-web")).toBe("https://modelxing.com/");
    expect(byTarget.get("ti-agentbuddy-web")).toBe("https://agentbuddy.modelxing.com/");
    expect(byTarget.has("ti-product-image-web")).toBe(false);
    const goboost = rows.find((r) => r.target_id === "ti-goboost-web");
    expect(goboost?.app_id).toBe("goboost");
    expect(goboost?.display_name).toBe("故事板与画布");
    for (const row of rows) {
      expect(row.url).not.toContain("invalid");
      expect(row.url.startsWith("https://")).toBe(true);
    }
  });

  it("emittable 与 public-ai econavmanifest 同口径（根路径合法；JS URL 规范化下无路径=根路径）", () => {
    expect(emittable("https://aicut.modelxing.com/")).toBe(true);
    expect(emittable("https://x.example.invalid/launch")).toBe(false);
    expect(emittable("http://x.modelxing.com/")).toBe(false);
    expect(emittable("https://localhost/app")).toBe(false);
    expect(emittable("https://x.modelxing.com/?a=b")).toBe(false);
    expect(emittable("https://x.modelxing.com/#f")).toBe(false);
    expect(emittable("https://u:p@x.modelxing.com/")).toBe(false);
    expect(emittable("not a url")).toBe(false);
  });
});

describe("GET /api/eco-nav/launches", () => {
  it("返回登记真源的可发射行（含 app_id/display_name），无 .invalid、无本应用行", async () => {
    const res = await launchesGET();
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      launches: { app_id: string; display_name: string; target_id: string; url: string }[];
    };
    const goboost = body.launches.find((r) => r.target_id === "ti-goboost-web");
    expect(goboost?.url).toBe("https://goboost.modelxing.com/projects");
    expect(goboost?.display_name).toBe("故事板与画布");
    expect(body.launches.some((r) => r.app_id === "product-image")).toBe(false);
    expect(JSON.stringify(body)).not.toContain("invalid");
  });
});

describe("AppShell 消费面（静态守卫）", () => {
  it("AppShell 不再内联 NEXT_PUBLIC_ECO_APP_LAUNCHES，改走 BFF", () => {
    const src = read("src/components/AppShell.tsx");
    expect(src).not.toContain("NEXT_PUBLIC_ECO_APP_LAUNCHES");
    expect(src).toContain("/api/eco-nav/launches");
  });
});
