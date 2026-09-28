import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { BILLING_PENDING, canStartWithText, presentTextJob, sameRecord } from "@/lib/text-image";

describe("文字描述入口", () => {
  it("只要有文字描述就可以开始，不要求实物照片", () => {
    expect(canStartWithText("白色陶瓷杯")).toBe(true);
    expect(canStartWithText("  ")).toBe(false);
  });

  it("没有真实生成图时不出图，计费保持待确认", () => {
    const view = presentTextJob({
      id: "txt_1",
      job_status: "failed",
      show_image: true,
      output_asset_id: "filled-output",
      subject_protected: true,
      billing_passed: true,
      production_authorized: true,
      billing_label: "¥9.90",
    });
    expect(view.showImage).toBe(false);
    expect(view.billingLabel).toBe(BILLING_PENDING);
    expect(view.subjectLabel).toBe("主体保护待确认");
    expect(view.billingPassed).toBe(false);
    expect(view.productionAuthorized).toBe(false);
    expect(view.headline).toContain("没有真实产品图");
  });

  it("待确认刷新后仍是同一条记录", () => {
    expect(sameRecord("txt_1", "txt_1")).toBe(true);
    expect(sameRecord("txt_2", "txt_1")).toBe(false);
    const view = presentTextJob({ id: "txt_1", job_status: "unknown" });
    expect(view.showImage).toBe(false);
    expect(view.headline).toContain("待确认");
  });

  it("界面不放假图，也不把计费或生产授权标成通过", () => {
    const panel = readFileSync(path.join(process.cwd(), "src/components/text-image/Panel.tsx"), "utf8");
    const start = readFileSync(path.join(process.cwd(), "src/app/start/ui.tsx"), "utf8");
    expect(panel).toContain("只用文字描述");
    expect(panel).toContain(BILLING_PENDING);
    expect(panel).toContain("主体保护待确认");
    expect(panel).not.toContain("<img");
    expect(panel).not.toContain("主体已保护");
    expect(panel).not.toContain("Billing PASS");
    expect(panel).not.toContain("Production Authorized");
    expect(panel).not.toContain("可售");
    expect(panel).toContain("@/components/ui/button");
    expect(start).toContain("TextImagePanel");
  });
});
