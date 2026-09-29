export const BILLING_PENDING = "计费待确认";
export const HONESTY = "生成质量未验证，不代表生产出图已通过";
export const PROTECTION_SCOPE = "logo,packaging_text,spec,structure";
export const REAL_GENERATION_INCOMPLETE = "真实出图未完成";

// billingLine 只转述服务端文案。没有金额时固定为计费待确认,不编造人民币。
export function billingLine(label?: string | null): string {
  const text = (label ?? "").trim();
  if (!text || text === BILLING_PENDING) return BILLING_PENDING;
  if (!text.includes("¥")) return BILLING_PENDING;
  return text;
}

export function creativeChoice(available: boolean): { enabled: boolean; reason: string } {
  if (!available) {
    return { enabled: false, reason: "创意模式尚未接通真实生成路径" };
  }
  return { enabled: true, reason: "" };
}

// showCreative 只有真实路径存在时才把创意模式放进界面。未接通时不渲染可选项。
export function showCreative(available: boolean): boolean {
  return creativeChoice(available).enabled;
}

export function quoteStale(
  prev: { mode: string; inputId: string; intent?: string },
  next: { mode: string; inputId: string; intent?: string }
): boolean {
  return prev.mode !== next.mode || prev.inputId !== next.inputId || (prev.intent ?? "") !== (next.intent ?? "");
}
