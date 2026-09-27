// HUI-2223：产品图登录壳的 EcoTopNav 视图。
// 这是本应用的壳层策略，不是生成质量门。USEFUL_IMAGE_MATURE 恒为 false：
// 顶栏完成不能用来宣称 First Useful Image 或 1698/1699 已通过。
// 不读取、不展示私有点数；没有真实计费应答时只显示「计费待确认」。

export const CURRENT_APP = { appId: "product-image", display: "产品图" } as const;
export const USEFUL_IMAGE_MATURE = false;
export const WORKSPACE_PRIMARY_NOTE =
  "返回来源。专业制作区仍以当前工程为主，不会把商品、提示词或工程带到其他应用。";

export const NAV_HEIGHT = { full: 52, compact: 40 } as const;
const NARROW_BELOW = 900;

const PRIVATE_POINTS = /点数|修点|image[\s_-]?coin|credits?/i;
const LEAKED_QUERY = [
  "project",
  "project_id",
  "prompt",
  "product",
  "product_id",
  "asset",
  "asset_id",
  "sku",
];

export type PayerScope = {
  kind: "personal" | "organization";
  tenantId: string | null;
  display: string;
};

export type OwnedRecord = {
  id: string;
  kind: "project" | "asset" | "result";
  owner: PayerScope;
};

export type Quote = {
  id: string;
  status: "unsubmitted" | "invalid" | "submitted";
  payer: PayerScope;
  mustRequote?: boolean;
};

export type CompletedResult = {
  id: string;
  owner: PayerScope;
  status: "completed";
};

export type QuoteBook = {
  quotes: Quote[];
  results: CompletedResult[];
};

export function shellShowsEcoTopNav(pathname: string): boolean {
  const path = pathname.split("?")[0] || "/";
  if (path === "/" || path === "/login" || path === "/login/") return false;
  if (path === "/share" || path.startsWith("/share/")) return false;
  return true;
}

export function sourceLabel(input: {
  sourceType: string;
  sourceRef: string;
  campaignRef?: string;
  orderRef?: string;
}): string {
  if (input.sourceType === "campaign") {
    const ref = input.campaignRef || input.sourceRef;
    return ref ? `活动 ${ref}` : "";
  }
  if (input.sourceType === "order") {
    const ref = input.orderRef || input.sourceRef;
    return ref ? `订单 ${ref}` : "";
  }
  return "";
}

export function resolveScope(input: {
  sourceType: string;
  sourceLabel: string;
  sourceTenantId?: string | null;
  enterpriseAssets: boolean;
}): PayerScope {
  const sourced = input.sourceType === "order" || input.sourceType === "campaign";
  if (sourced || input.enterpriseAssets) {
    return {
      kind: "organization",
      tenantId: input.sourceTenantId ?? null,
      display: input.sourceLabel || "组织",
    };
  }
  return { kind: "personal", tenantId: null, display: "个人" };
}

// 个人主线不增加强制组织选择。深链进入时组织已经确定，也不再弹一次选择器。
export function requiresOrgSelection(
  _scope: PayerScope,
  _action: "upload" | "generate"
): boolean {
  return false;
}

export function scopeKey(scope: PayerScope | null): string | null {
  if (!scope) return null;
  return scope.kind === "personal" ? "personal" : `org:${scope.tenantId ?? ""}`;
}

export function manualAppSwitch(input: { launchUrl: string; appId: string }): {
  appId: string;
  href: string | null;
  createsHandoff: false;
  intent: "manual_switch";
} {
  return {
    appId: input.appId,
    href: safeLaunchHref(input.launchUrl),
    createsHandoff: false,
    intent: "manual_switch",
  };
}

function safeLaunchHref(raw: string): string | null {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.username || url.password) return null;
  const loopback = url.hostname === "127.0.0.1" || url.hostname === "localhost";
  if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) return null;
  for (const key of LEAKED_QUERY) url.searchParams.delete(key);
  url.searchParams.sort();
  const query = url.searchParams.toString();
  return `${url.origin}${url.pathname}${query ? `?${query}` : ""}`;
}

export function returnChip(input: {
  sourceType: string;
  sourceLabel: string;
  returnHref: string | null;
}): { label: string; href: string; title: string } | null {
  if (input.sourceType !== "order" && input.sourceType !== "campaign") return null;
  if (!input.sourceLabel.trim() || !input.returnHref) return null;
  const href = safeLaunchHref(input.returnHref);
  if (!href) return null;
  return { label: input.sourceLabel, href, title: WORKSPACE_PRIMARY_NOTE };
}

export type BillingBadge = {
  text: string;
  kind: "cny" | "allowance" | "quota" | "unknown";
  historyRemainsVisible: true;
  privatePointsRejected: boolean;
};

export function billingBadgeFromPayload(payload: unknown, httpStatus: number): BillingBadge {
  const base = {
    historyRemainsVisible: true as const,
    privatePointsRejected: false,
  };
  if (httpStatus !== 200 || !payload || typeof payload !== "object") {
    return { ...base, text: "计费待确认", kind: "unknown" };
  }
  const raw = payload as Record<string, unknown>;
  const privatePointsRejected = hasPrivatePoints(raw);
  if (typeof raw.balance_cny === "number" && Number.isFinite(raw.balance_cny)) {
    return {
      ...base,
      text: formatCny(raw.balance_cny),
      kind: "cny",
      privatePointsRejected,
    };
  }
  if (typeof raw.allowance_cny === "number" && Number.isFinite(raw.allowance_cny)) {
    return {
      ...base,
      text: `津贴 ${formatCny(raw.allowance_cny)}`,
      kind: "allowance",
      privatePointsRejected,
    };
  }
  if (typeof raw.quota_remaining === "number" && Number.isFinite(raw.quota_remaining)) {
    return {
      ...base,
      text: `额度 ${raw.quota_remaining}`,
      kind: "quota",
      privatePointsRejected,
    };
  }
  return { ...base, text: "计费待确认", kind: "unknown", privatePointsRejected: true };
}

function hasPrivatePoints(raw: Record<string, unknown>): boolean {
  for (const key of Object.keys(raw)) {
    if (PRIVATE_POINTS.test(key)) return true;
  }
  for (const value of Object.values(raw)) {
    if (typeof value === "string" && PRIVATE_POINTS.test(value)) return true;
  }
  return false;
}

function formatCny(amount: number): string {
  return `¥${amount.toFixed(2)}`;
}

export function applyPayerSwitch(book: QuoteBook, next: PayerScope): QuoteBook {
  const nextKey = scopeKey(next);
  const changed = book.quotes.some(
    (q) => q.status === "unsubmitted" && scopeKey(q.payer) !== nextKey
  );
  if (!changed) return book;
  return {
    quotes: book.quotes.map((q) =>
      q.status === "unsubmitted" && scopeKey(q.payer) !== nextKey
        ? { ...q, status: "invalid" as const, mustRequote: true }
        : q
    ),
    results: book.results.map((r) => ({ ...r, owner: { ...r.owner } })),
  };
}

export function visibleOwned(records: OwnedRecord[], scope: PayerScope): OwnedRecord[] {
  const key = scopeKey(scope);
  return records.filter((r) => scopeKey(r.owner) === key);
}

export function navLayout(widthPx: number): { variant: "full" | "compact"; heightPx: number } {
  if (widthPx >= NARROW_BELOW) return { variant: "full", heightPx: NAV_HEIGHT.full };
  return { variant: "compact", heightPx: NAV_HEIGHT.compact };
}
