export type ShowcaseQuote = {
  id?: string;
  job_status?: string;
  quote_status?: string;
};

// 报价未确认，或同一条记录已经失败、待确认时，不能再提交。
export function maySubmitShowcase(job: ShowcaseQuote | null | undefined): boolean {
  return Boolean(job?.id) && job?.quote_status === "confirmed" && job?.job_status === "quoted";
}
