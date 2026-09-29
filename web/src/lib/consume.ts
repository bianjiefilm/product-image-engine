export type Book = { accountRef: string; label: string };

export type QuoteInput = {
  amountMinor?: number;
  currency?: string;
  includedAllowance?: boolean;
  privatePoints?: string;
};

export type GenerateInput = {
  confirmedFingerprint: string;
  currentFingerprint: string;
  entitled: boolean;
  fundsShort: boolean;
  priced: boolean;
  cashMinor?: number;
};

export type CompletionLevels = {
  implementationReady: "PASS";
  integration: "UNKNOWN";
  billing: "UNKNOWN";
  channelBilling: "NOT_APPLICABLE";
  productionAuthorized: "NOT_AUTHORIZED";
};

const PRIVATE_POINTS = /点数|修点|image[\s_-]?coin/i;

export function payerLabel(input: {
  sourceType: string;
  personalDisplay: string;
  verifiedOrgDisplay?: string;
  identityConfigured?: boolean;
}): string {
  if (input.identityConfigured === false) return "配置缺失";
  if ((input.sourceType === "order" || input.sourceType === "campaign") && input.verifiedOrgDisplay) {
    return input.verifiedOrgDisplay;
  }
  return input.personalDisplay || "个人付款";
}

// configurationLabel 区分配置缺失和端口没装上。适配器在时缺配置不是「代码未实现」。
export function configurationLabel(input: {
  identityConfigured: boolean;
  billingConfigured: boolean;
  adapterPresent: boolean;
}): string {
  if (!input.identityConfigured || !input.billingConfigured) return "配置缺失";
  if (!input.adapterPresent) return "未实现";
  return "已配置";
}

// estimateShown 只有报价对象存在时才保留人民币估价。
export function estimateShown(presentation: string, quoted?: boolean): string {
  const text = presentation.trim();
  if (quoted === false && (text.startsWith("本次预计 ¥") || text.startsWith("-本次预计 ¥"))) {
    return "待确认";
  }
  return text || "待确认";
}

export function visibleBooks(accountRef: string, books: Book[]): Book[] {
  return books.filter((book) => book.accountRef === accountRef);
}

export function presentQuote(input: QuoteInput): string {
  if (input.privatePoints && PRIVATE_POINTS.test(input.privatePoints)) {
    throw new Error("不使用图像点数");
  }
  if (input.includedAllowance) return "已包含额度";
  if (typeof input.amountMinor === "number" && Number.isFinite(input.amountMinor)) {
    const minor = Math.trunc(input.amountMinor);
    const sign = minor < 0 ? "-" : "";
    const abs = Math.abs(minor);
    return `${sign}本次预计 ¥${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, "0")}`;
  }
  return "待确认";
}

export function canGenerate(input: GenerateInput): { allowed: boolean; mustRequote: boolean } {
  const same = input.confirmedFingerprint !== "" && input.confirmedFingerprint === input.currentFingerprint;
  if (!input.entitled || input.fundsShort || !input.priced || !same) {
    return { allowed: false, mustRequote: !same };
  }
  return { allowed: true, mustRequote: false };
}

export function afterTopupPage(page: string, quoteFresh: boolean): { allowed: boolean; reason: string } {
  if (!quoteFresh || page === "success") {
    if (page === "success" && !quoteFresh) {
      return { allowed: false, reason: "requote_required" };
    }
  }
  if (!quoteFresh) return { allowed: false, reason: "requote_required" };
  return { allowed: true, reason: "" };
}

export function completionLevels(): CompletionLevels {
  return {
    implementationReady: "PASS",
    integration: "UNKNOWN",
    billing: "UNKNOWN",
    channelBilling: "NOT_APPLICABLE",
    productionAuthorized: "NOT_AUTHORIZED",
  };
}

export function quoteFingerprint(input: {
  payerAccountRef: string;
  imageCount: number;
  resolution: string;
  capability: string;
  pricingVersion: string;
  model?: string;
  size?: string;
}): string {
  const model = input.model || input.capability;
  const size = input.size || input.resolution;
  return [input.payerAccountRef, String(input.imageCount), input.resolution, input.capability, input.pricingVersion, model, size].join("\n");
}
