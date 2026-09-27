import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  BILLING_PENDING,
  HONESTY,
  billingLine,
  creativeChoice,
  quoteStale,
  subjectProtectionLabel,
} from "@/lib/light-scene";

describe("光影场景诚实文案", () => {
  it("没有报价金额时只显示计费待确认", () => {
    expect(billingLine(undefined)).toBe(BILLING_PENDING);
    expect(billingLine("")).toBe(BILLING_PENDING);
    expect(billingLine(BILLING_PENDING)).toBe(BILLING_PENDING);
  });

  it("创意路径未接通时不能选", () => {
    expect(creativeChoice(false)).toEqual({
      enabled: false,
      reason: "创意模式尚未接通真实生成路径",
    });
  });

  it("改模式、改输入或改光影意图使报价失效", () => {
    const prev = { mode: "fidelity", inputId: "pin_1", intent: "侧光" };
    expect(quoteStale(prev, { mode: "creative", inputId: "pin_1", intent: "侧光" })).toBe(true);
    expect(quoteStale(prev, { mode: "fidelity", inputId: "pin_2", intent: "侧光" })).toBe(true);
    expect(quoteStale(prev, { mode: "fidelity", inputId: "pin_1", intent: "逆光" })).toBe(true);
    expect(quoteStale(prev, prev)).toBe(false);
  });

  it("客户端不能把主体保护说成通过", () => {
    expect(subjectProtectionLabel("fail")).toBe("主体保护未通过");
    expect(subjectProtectionLabel("pass")).toBe("主体保护待确认");
    expect(subjectProtectionLabel(undefined)).toBe("主体保护待确认");
  });

  it("面板不宣称主体保护或生产出图已通过", () => {
    const panel = readFileSync(
      path.join(process.cwd(), "src/components/light-scene/Panel.tsx"),
      "utf8"
    );
    expect(panel).toContain("BILLING_PENDING");
    expect(panel).toContain("HONESTY");
    expect(panel).toContain("主体保护");
    expect(panel).toContain("@/components/ui/button");
    expect(panel).not.toContain("计费已通过");
    expect(panel).not.toContain("生产出图已经通过");
    expect(panel).not.toContain("主体保护已通过");
    expect(panel).not.toContain("explicit_creative");
    expect(panel).not.toContain("允许受限导出");
  });
});
