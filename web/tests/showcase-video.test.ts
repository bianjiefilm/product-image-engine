import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  BILLING_PENDING,
  CAMERA_MOVES,
  canStartShowcase,
  presentShowcaseJob,
  sameRecord,
} from "@/lib/showcase-video";

describe("展示视频入口", () => {
  it("只有已有产品图，并且运镜是 360 度或场景运镜时才能发起", () => {
    expect(CAMERA_MOVES.map((item) => item.id)).toEqual(["360", "scene"]);
    expect(canStartShowcase("in_cup", "360")).toBe(true);
    expect(canStartShowcase("out_pack", "scene")).toBe(true);
    expect(canStartShowcase("", "360")).toBe(false);
    expect(canStartShowcase("  ", "scene")).toBe(false);
    expect(canStartShowcase("in_cup", "zoom")).toBe(false);
    expect(canStartShowcase("in_cup", "")).toBe(false);
  });

  it("没有真实视频时不播放画面，计费和生产授权保持未通过", () => {
    const view = presentShowcaseJob({
      id: "vid_1",
      job_status: "failed",
      show_video: true,
      play_still: true,
      video_url: "https://example.invalid/fake.mp4",
      output_asset_id: "filled-video",
      billing_passed: true,
      production_authorized: true,
      billing_label: "¥9.90",
    });
    expect(view.showVideo).toBe(false);
    expect(view.playStill).toBe(false);
    expect(view.videoUrl).toBe("");
    expect(view.billingLabel).toBe(BILLING_PENDING);
    expect(view.billingPassed).toBe(false);
    expect(view.productionAuthorized).toBe(false);
    expect(view.headline).toContain("没有真实展示视频");
  });

  it("待确认刷新后仍是同一条记录", () => {
    expect(sameRecord("vid_1", "vid_1")).toBe(true);
    expect(sameRecord("vid_2", "vid_1")).toBe(false);
    const view = presentShowcaseJob({ id: "vid_1", job_status: "unknown" });
    expect(view.showVideo).toBe(false);
    expect(view.playStill).toBe(false);
    expect(view.headline).toContain("待确认");
  });

  it("界面不放静图或假视频，也不把计费或生产授权标成通过", () => {
    const panel = readFileSync(path.join(process.cwd(), "src/components/showcase-video/Panel.tsx"), "utf8");
    const page = readFileSync(path.join(process.cwd(), "src/app/projects/[id]/page.tsx"), "utf8");
    expect(panel).toContain("展示视频");
    expect(panel).toContain("360 度");
    expect(panel).toContain("场景运镜");
    expect(panel).toContain(BILLING_PENDING);
    expect(panel).toContain("Billing 未通过");
    expect(panel).toContain("Production 未授权");
    expect(panel).not.toContain("<video");
    expect(panel).not.toContain("<img");
    expect(panel).not.toContain(".mp4");
    expect(panel).not.toContain("Billing PASS");
    expect(panel).not.toContain("Production Authorized");
    expect(page).toContain("ShowcaseVideoPanel");
  });
});
