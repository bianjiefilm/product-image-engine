export const BILLING_PENDING = "计费待确认";
export const HONESTY = "生成质量未验证，不代表生产出图已通过";

// billingLine 只转述服务端文案。没有金额时固定为计费待确认,不编造人民币。
export function billingLine(label?: string | null): string {
  const text = (label ?? "").trim();
  if (!text) return BILLING_PENDING;
  return text;
}

export function creativeChoice(available: boolean): { enabled: boolean; reason: string } {
  if (!available) {
    return { enabled: false, reason: "创意模式尚未接通真实生成路径" };
  }
  return { enabled: true, reason: "" };
}

export function quoteStale(
  prev: { mode: string; inputId: string },
  next: { mode: string; inputId: string }
): boolean {
  return prev.mode !== next.mode || prev.inputId !== next.inputId;
}
