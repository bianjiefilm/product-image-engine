"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import EcoTopNav, { type NavAppLink } from "@/components/eco-nav/EcoTopNav";
import { useWorkbenchFact, WorkbenchProvider } from "@/components/eco-nav/workbench";
import {
  CURRENT_APP,
  applyPayerSwitch,
  billingBadgeFromPayload,
  manualAppSwitch,
  resolveScope,
  returnChip,
  scopeKey,
  shellShowsEcoTopNav,
  type PayerScope,
  type QuoteBook,
} from "@/lib/eco-nav";

type SessionFact = {
  user_id?: string;
  account_id?: string;
  tenant_id?: string;
  memberships?: { tenant_id: string; display: string }[];
};

function ShellFrame({ children }: { children: React.ReactNode }) {
  const path = usePathname() || "/";
  const router = useRouter();
  const show = shellShowsEcoTopNav(path);
  const workbench = useWorkbenchFact();
  const [badgeText, setBadgeText] = useState("计费待确认");
  const [historyVisible, setHistoryVisible] = useState(true);
  const [chosen, setChosen] = useState<PayerScope | null>(null);
  const [memberships, setMemberships] = useState<PayerScope[]>([]);
  const [book, setBook] = useState<QuoteBook>({ quotes: [], results: [] });

  useEffect(() => {
    if (!show) return;
    let cancelled = false;
    (async () => {
      const sessionRes = await fetch("/api/auth/session");
      const session = (await sessionRes.json().catch(() => null)) as {
        principal?: SessionFact;
      } | null;
      if (!cancelled) {
        const listed = session?.principal?.memberships ?? [];
        setMemberships(
          listed
            .filter((m) => m.tenant_id && m.display)
            .map((m) => ({
              kind: "organization" as const,
              tenantId: m.tenant_id,
              display: m.display,
            }))
        );
      }
      const billingRes = await fetch("/api/billing/balance");
      const payload = await billingRes.json().catch(() => null);
      if (!cancelled) {
        const badge = billingBadgeFromPayload(payload, billingRes.status);
        setBadgeText(badge.text);
        setHistoryVisible(badge.historyRemainsVisible);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [show]);

  const resolved = resolveScope({
    sourceType: workbench?.sourceType || "standalone",
    sourceLabel: workbench?.sourceLabel || "",
    sourceTenantId: workbench?.sourceTenantId,
    enterpriseAssets: Boolean(workbench?.enterpriseAssets),
  });
  const scope = chosen ?? resolved;
  const diverged = chosen !== null && scopeKey(chosen) !== scopeKey(resolved);
  const chip = returnChip({
    sourceType: workbench?.sourceType || "",
    sourceLabel: workbench?.sourceLabel || "",
    returnHref: workbench?.returnHref ?? null,
  });
  const apps: NavAppLink[] = [
    {
      appId: CURRENT_APP.appId,
      display: CURRENT_APP.display,
      href: null,
      current: true,
    },
  ];
  const rawLaunches = process.env.NEXT_PUBLIC_ECO_APP_LAUNCHES;
  if (rawLaunches) {
    try {
      const parsed = JSON.parse(rawLaunches) as { appId: string; display: string; launchUrl: string }[];
      for (const item of parsed) {
        if (item.appId === CURRENT_APP.appId) continue;
        const plan = manualAppSwitch({ launchUrl: item.launchUrl, appId: item.appId });
        apps.push({
          appId: item.appId,
          display: item.display,
          href: plan.href,
          current: false,
        });
      }
    } catch {
      // 目录不是合法 JSON 时不伪造跳转地址。
    }
  }
  const switchable = memberships.filter(
    (item) => item.tenantId !== scope.tenantId || item.kind !== scope.kind
  );

  async function logout() {
    await fetch("/api/auth/logout", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    router.replace("/login");
  }

  function onSwitchScope(next: PayerScope) {
    setBook((current) => applyPayerSwitch(current, next));
    setChosen(next);
  }

  return (
    <>
      {show ? (
        <EcoTopNav
          scope={scope}
          badgeText={badgeText}
          historyVisible={historyVisible}
          returnTo={chip}
          switchable={switchable}
          apps={apps}
          onSwitchScope={onSwitchScope}
          onLogout={logout}
        />
      ) : null}
      <main className="main" data-quote-count={book.quotes.length}>
        {diverged ? (
          <p className="banner warn">
            已切换付款主体。为避免工程、素材和未提交报价串到另一个组织，当前内容已隐藏。已完成结果不会改归属。
          </p>
        ) : (
          children
        )}
      </main>
    </>
  );
}

export function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <WorkbenchProvider>
      <ShellFrame>{children}</ShellFrame>
    </WorkbenchProvider>
  );
}
