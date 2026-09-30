import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  BILLING_PENDING,
  HONESTY,
  billingLine,
  creativeChoice,
  quoteStale,
  showCreative,
  subjectLockPreview,
  PROTECTION_SCOPE,
  REAL_GENERATION_INCOMPLETE,
  SUBJECT_LOCK_ORIGIN,
} from "@/lib/bg-replace";

describe("背景替换诚实文案", () => {
  it("没有报价金额时只显示计费待确认", () => {
    expect(billingLine(undefined)).toBe(BILLING_PENDING);
    expect(billingLine("")).toBe(BILLING_PENDING);
    expect(billingLine("  ")).toBe(BILLING_PENDING);
    expect(billingLine(BILLING_PENDING)).toBe(BILLING_PENDING);
  });

  it("创意路径未接通时不能选,界面也不渲染该选项", () => {
    expect(creativeChoice(false)).toEqual({
      enabled: false,
      reason: "创意模式尚未接通真实生成路径",
    });
    expect(creativeChoice(true).enabled).toBe(true);
    expect(showCreative(false)).toBe(false);
    expect(showCreative(true)).toBe(true);
  });

  it("改模式、改输入或改背景方向使报价失效", () => {
    const prev = { mode: "fidelity", inputId: "pin_1", intent: "室内" };
    expect(quoteStale(prev, { mode: "creative", inputId: "pin_1", intent: "室内" })).toBe(true);
    expect(quoteStale(prev, { mode: "fidelity", inputId: "pin_2", intent: "室内" })).toBe(true);
    expect(quoteStale(prev, { mode: "fidelity", inputId: "pin_1", intent: "户外" })).toBe(true);
    expect(quoteStale(prev, prev)).toBe(false);
  });

  it("没有人民币符号的费用文案不当成已报价", () => {
    expect(billingLine("已扣费")).toBe(BILLING_PENDING);
    expect(billingLine("本次预计 ¥1.20")).toBe("本次预计 ¥1.20");
    expect(billingLine("¥9.90")).toBe(BILLING_PENDING);
  });

  it("配置缺失、未实现和待核对要原样显示", () => {
    expect(billingLine("配置缺失")).toBe("配置缺失");
    expect(billingLine("未实现")).toBe("未实现");
    expect(billingLine("作品完成待核对")).toBe("作品完成待核对");
    expect(billingLine("已包含额度")).toBe("已包含额度");
    expect(billingLine("待确认")).toBe("待确认");
  });

  it("面板使用诚实文案,不宣称生产出图或计费已通过", () => {
    const lib = readFileSync(path.join(process.cwd(), "src/lib/bg-replace.ts"), "utf8");
    const panel = readFileSync(
      path.join(process.cwd(), "src/components/background-replace/Panel.tsx"),
      "utf8"
    );
    expect(lib).toContain(BILLING_PENDING);
    expect(lib).toContain(HONESTY);
    expect(lib).toContain(REAL_GENERATION_INCOMPLETE);
    expect(lib).toContain(PROTECTION_SCOPE);
    expect(panel).toContain("HONESTY");
    expect(panel).toContain("BILLING_PENDING");
    expect(panel).toContain("REAL_GENERATION_INCOMPLETE");
    expect(panel).toContain("PROTECTION_SCOPE");
    expect(panel).toContain("showCreative");
    expect(panel).toContain("/api/photos");
    expect(panel).toContain("/api/projects/");
    expect(panel).not.toContain("未接通");
    expect(panel).not.toContain("/internal/");
    expect(panel).toContain("@/components/ui/button");
    expect(panel).not.toContain("计费已通过");
    expect(panel).not.toContain("生产出图已经通过");
    expect(panel).toContain("锁定主体并换背景");
    expect(panel).toContain("subjectLockPreview");
    expect(panel).toContain("/content");
    expect(panel).not.toContain("9.9");
  });

  it("只有主体锁定结果才显示预览", () => {
    expect(subjectLockPreview(SUBJECT_LOCK_ORIGIN)).toBe(true);
    expect(subjectLockPreview("model_http")).toBe(false);
    expect(subjectLockPreview("")).toBe(false);
    expect(subjectLockPreview(undefined)).toBe(false);
  });
});
