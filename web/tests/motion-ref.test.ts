import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { dynamicAdLabel, isMotionDigest, NOT_FINISHED_COPY } from "@/lib/motion-ref";

const digest = `sha256:${"ab".repeat(32)}`;

describe("Motion 引用不是成片", () => {
  it("摘要是 sha256 加 64 位十六进制", () => {
    expect(isMotionDigest(digest)).toBe(true);
    expect(isMotionDigest(digest.slice(0, -1))).toBe(false);
    expect(isMotionDigest("ab".repeat(32))).toBe(false);
  });

  it("未核验引用不能当成动态广告", () => {
    expect(dynamicAdLabel({ finished_copy: NOT_FINISHED_COPY, verified: false, dynamic_ad: false })).toContain("还没生成成片");
    expect(() => dynamicAdLabel({ finished_copy: NOT_FINISHED_COPY, dynamic_ad: true })).toThrow(/动态广告/);
    expect(() => dynamicAdLabel({ finished_copy: "已经做好了", verified: false })).toThrow(/还没生成成片/);
  });

  it("页面文案写明还没生成成片，并且没有编辑器或批量重跑", () => {
    const src = readFileSync(path.join(process.cwd(), "src/components/motion-ref/MotionRefNote.tsx"), "utf8");
    expect(src).toContain("还没生成成片");
    expect(src).toContain("HUI-2732");
    expect(src).not.toContain("<video");
    expect(src).not.toContain("batch-rerun");
    expect(src).not.toContain("已生成成片");
    expect(src).not.toContain("动态广告已完成");
    expect(src).not.toContain("timeline");
    expect(src).not.toContain("keyframe");
    const page = readFileSync(path.join(process.cwd(), "src/app/projects/[id]/page.tsx"), "utf8");
    const start = readFileSync(path.join(process.cwd(), "src/app/start/ui.tsx"), "utf8");
    expect(page).toContain("MotionRefNote");
    expect(start).toContain("MotionRefNote");
  });
});
