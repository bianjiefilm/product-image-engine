import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  BILLING_PENDING,
  HONESTY,
  billingLine,
  creativeChoice,
  quoteStale,
} from "@/lib/bg-replace";

describe("背景替换诚实文案", () => {
  it("没有报价金额时只显示计费待确认", () => {
    expect(billingLine(undefined)).toBe(BILLING_PENDING);
    expect(billingLine("")).toBe(BILLING_PENDING);
    expect(billingLine("  ")).toBe(BILLING_PENDING);
    expect(billingLine(BILLING_PENDING)).toBe(BILLING_PENDING);
  });

  it("创意路径未接通时不能选", () => {
    expect(creativeChoice(false)).toEqual({
      enabled: false,
      reason: "创意模式尚未接通真实生成路径",
    });
    expect(creativeChoice(true).enabled).toBe(true);
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
  });

  it("面板使用诚实文案,不宣称生产出图或计费已通过", () => {
    const lib = readFileSync(path.join(process.cwd(), "src/lib/bg-replace.ts"), "utf8");
    const panel = readFileSync(
      path.join(process.cwd(), "src/components/background-replace/Panel.tsx"),
      "utf8"
    );
    expect(lib).toContain(BILLING_PENDING);
    expect(lib).toContain(HONESTY);
    expect(panel).toContain("HONESTY");
    expect(panel).toContain("BILLING_PENDING");
    expect(panel).toContain("@/components/ui/button");
    expect(panel).not.toContain("计费已通过");
    expect(panel).not.toContain("生产出图已经通过");
  });
});
