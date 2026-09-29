export const BILLING_PENDING = "计费待确认";

const HONEST_LABELS = new Set([
  BILLING_PENDING,
  "配置缺失",
  "未实现",
  "作品完成待核对",
  "已包含额度",
  "待确认",
]);

export function honestBillingLabel(raw?: string | null): string {
  const text = (raw ?? "").trim();
  if (HONEST_LABELS.has(text)) return text;
  if (text.startsWith("本次预计 ¥") || text.startsWith("-本次预计 ¥")) return text;
  return BILLING_PENDING;
}

export function canStartWithText(prompt: string): boolean {
  return prompt.trim().length > 0;
}

export function presentTextJob(job: {
  id?: string;
  job_status?: string;
  billing_label?: string;
  show_image?: boolean;
  subject_protected?: boolean;
  billing_passed?: boolean;
  production_authorized?: boolean;
  output_asset_id?: string;
}): {
  showImage: false;
  billingLabel: string;
  subjectLabel: "主体保护待确认";
  billingPassed: false;
  productionAuthorized: false;
  headline: string;
} {
  const failed = (job.job_status ?? "") === "failed";
  return {
    showImage: false,
    billingLabel: honestBillingLabel(job.billing_label),
    subjectLabel: "主体保护待确认",
    billingPassed: false,
    productionAuthorized: false,
    headline: failed ? "未能生成，没有真实产品图" : "结果待确认，没有真实产品图",
  };
}

export function sameRecord(currentId: string, previousId: string): boolean {
  return currentId !== "" && currentId === previousId;
}
