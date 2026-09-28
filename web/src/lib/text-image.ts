export const BILLING_PENDING = "计费待确认";

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
  billingLabel: typeof BILLING_PENDING;
  subjectLabel: "主体保护待确认";
  billingPassed: false;
  productionAuthorized: false;
  headline: string;
} {
  const failed = (job.job_status ?? "") === "failed";
  return {
    showImage: false,
    billingLabel: BILLING_PENDING,
    subjectLabel: "主体保护待确认",
    billingPassed: false,
    productionAuthorized: false,
    headline: failed ? "未能生成，没有真实产品图" : "结果待确认，没有真实产品图",
  };
}

export function sameRecord(currentId: string, previousId: string): boolean {
  return currentId !== "" && currentId === previousId;
}
