import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { maySubmitShowcase } from "@/components/showcase-video/submit-guard";

describe("展示视频报价确认", () => {
  it("未确认报价、失败记录和空记录都不能提交", () => {
    expect(maySubmitShowcase(null)).toBe(false);
    expect(maySubmitShowcase({})).toBe(false);
    expect(maySubmitShowcase({ id: "vid_1", job_status: "quoted", quote_status: "unconfirmed" })).toBe(false);
    expect(maySubmitShowcase({ id: "vid_1", job_status: "quoted" })).toBe(false);
    expect(maySubmitShowcase({ id: "vid_1", job_status: "failed", quote_status: "confirmed" })).toBe(false);
    expect(maySubmitShowcase({ id: "vid_1", job_status: "unknown", quote_status: "confirmed" })).toBe(false);
    expect(maySubmitShowcase({ id: "vid_1", job_status: "quoted", quote_status: "confirmed" })).toBe(true);
  });

  it("面板只在报价已确认后提交，并且不播放静图或假视频", () => {
    const panel = readFileSync(path.join(process.cwd(), "src/components/showcase-video/Panel.tsx"), "utf8");
    expect(panel).toContain("maySubmitShowcase");
    expect(panel).toContain("没有真实展示视频，不能播放静图或假视频");
    expect(panel).not.toContain("<video");
    expect(panel).not.toContain("<img");
    expect(panel).not.toMatch(/文生视频|text-to-video|textToVideo/);
  });
});
