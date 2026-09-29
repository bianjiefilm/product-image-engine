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
    expect(view.subjectLabel).toContain("不宣称主体保真");
    expect(view.billingPassed).toBe(false);
    expect(view.productionAuthorized).toBe(false);
    expect(view.realGeneration).toBe("真实出图未完成");
    expect(view.headline).toContain("没有真实产品图");
  });

  it("只有服务端夹具完成时才出图，并且不把它当成模型或已结算", () => {
    const view = presentTextJob({
      id: "txt_fixture",
      job_status: "completed",
      origin: "fixture",
      show_image: true,
      billing_label: "配置缺失",
      settlement: "作品完成待核对",
      real_generation_completed: false,
    });
    expect(view.showImage).toBe(true);
    expect(view.billingPassed).toBe(false);
    expect(view.productionAuthorized).toBe(false);
    expect(view.realGeneration).toBe("真实出图未完成");
    expect(view.settlementLabel).toBe("作品完成待核对");
    expect(view.headline).toContain("不是模型出图");
    expect(view.subjectLabel).toContain("不宣称主体保真");
    const forged = presentTextJob({
      job_status: "completed",
      origin: "supplier",
      show_image: true,
      real_generation_completed: true,
      billing_passed: true,
      production_authorized: true,
    });
    expect(forged.showImage).toBe(false);
    expect(forged.billingPassed).toBe(false);
    expect(forged.realGeneration).toBe("真实出图未完成");
  });

  it("待确认刷新后仍是同一条记录", () => {
    expect(sameRecord("txt_1", "txt_1")).toBe(true);
    expect(sameRecord("txt_2", "txt_1")).toBe(false);
    const view = presentTextJob({ id: "txt_1", job_status: "unknown" });
    expect(view.showImage).toBe(false);
    expect(view.headline).toContain("待确认");
  });

  it("只转述服务端的诚实计费文案，客户端金额不能当成报价", () => {
    for (const label of ["配置缺失", "未实现", "作品完成待核对", "已包含额度", "待确认", "本次预计 ¥1.50"]) {
      const view = presentTextJob({ billing_label: label, show_image: true, billing_passed: true });
      expect(view.billingLabel).toBe(label);
      expect(view.showImage).toBe(false);
      expect(view.billingPassed).toBe(false);
    }
    expect(presentTextJob({ billing_label: "¥9.90" }).billingLabel).toBe(BILLING_PENDING);
    expect(presentTextJob({ billing_label: "已扣费" }).billingLabel).toBe(BILLING_PENDING);
  });

  it("界面不放假图，也不把计费或生产授权标成通过", () => {
    const panel = readFileSync(path.join(process.cwd(), "src/components/text-image/Panel.tsx"), "utf8");
    const start = readFileSync(path.join(process.cwd(), "src/app/start/ui.tsx"), "utf8");
    const lib = readFileSync(path.join(process.cwd(), "src/lib/text-image.ts"), "utf8");
    expect(panel).toContain("只用文字描述");
    expect(panel).toContain(BILLING_PENDING);
    expect(panel).toContain("view.billingLabel");
    expect(panel).toContain("view.subjectLabel");
    expect(panel).toContain("view.realGeneration");
    expect(panel).toContain("view.showImage");
    expect(panel).toContain("不是模型出图");
    expect(panel).toContain("@/components/ui/card");
    expect(panel).not.toContain("已结算");
    expect(panel).not.toContain("主体已保护");
    expect(panel).not.toContain("Billing PASS");
    expect(panel).not.toContain("Production Authorized");
    expect(panel).not.toContain("可售");
    expect(panel).toContain("@/components/ui/button");
    expect(lib).toContain("真实出图未完成");
    expect(lib).toContain("概念输出，不宣称主体保真");
    expect(lib).toContain("作品完成待核对");
    expect(start).toContain("TextImagePanel");
  });
});
