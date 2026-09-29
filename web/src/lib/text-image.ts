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

const REAL_GENERATION = "真实出图未完成";
const CONCEPTUAL = "概念输出，不宣称主体保真";

export function presentTextJob(job: {
  id?: string;
  job_status?: string;
  origin?: string;
  billing_label?: string;
  settlement?: string;
  show_image?: boolean;
  subject_protected?: boolean;
  billing_passed?: boolean;
  production_authorized?: boolean;
  output_asset_id?: string;
  photo_asset_id?: string;
  real_generation_completed?: boolean;
}): {
  showImage: boolean;
  billingLabel: string;
  subjectLabel: string;
  billingPassed: false;
  productionAuthorized: false;
  realGeneration: "真实出图未完成";
  settlementLabel: string;
  headline: string;
} {
  const trusted =
    job.job_status === "completed" &&
    job.origin === "fixture" &&
    job.show_image === true &&
    job.real_generation_completed !== true;
  const failed = (job.job_status ?? "") === "failed";
  const noPhoto = !(job.photo_asset_id ?? "").trim();
  return {
    showImage: trusted,
    billingLabel: honestBillingLabel(job.billing_label),
    subjectLabel: noPhoto ? CONCEPTUAL : "有实物参考，主体保真仍未核实",
    billingPassed: false,
    productionAuthorized: false,
    realGeneration: REAL_GENERATION,
    settlementLabel: job.settlement === "作品完成待核对" ? "作品完成待核对" : "账单尚未核对",
    headline: trusted ? "夹具图可以查看，这不是模型出图" : failed ? "未能生成，没有真实产品图" : "结果待确认，没有真实产品图",
  };
}

export function sameRecord(currentId: string, previousId: string): boolean {
  return currentId !== "" && currentId === previousId;
}
