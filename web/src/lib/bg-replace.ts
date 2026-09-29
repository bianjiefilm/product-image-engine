export const BILLING_PENDING = "计费待确认";
export const HONESTY = "生成质量未验证，不代表生产出图已通过";
export const PROTECTION_SCOPE = "logo,packaging_text,spec,structure";
export const REAL_GENERATION_INCOMPLETE = "真实出图未完成";

const HONEST_BILLING = new Set(["配置缺失", "未实现", "作品完成待核对", "已包含额度", "待确认"]);

// billingLine 只转述服务端文案。裸金额和「已扣费」都不当成报价。
export function billingLine(label?: string | null): string {
  const text = (label ?? "").trim();
  if (!text || text === BILLING_PENDING) return BILLING_PENDING;
  if (HONEST_BILLING.has(text)) return text;
  if (text.startsWith("本次预计 ¥") || text.startsWith("-本次预计 ¥")) return text;
  return BILLING_PENDING;
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
