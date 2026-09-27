import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  CURRENT_APP,
  USEFUL_IMAGE_MATURE,
  WORKSPACE_PRIMARY_NOTE,
  applyPayerSwitch,
  billingBadgeFromPayload,
  manualAppSwitch,
  navLayout,
  requiresOrgSelection,
  resolveScope,
  returnChip,
  shellShowsEcoTopNav,
  sourceLabel,
  visibleOwned,
  type OwnedRecord,
  type PayerScope,
  type QuoteBook,
} from "@/lib/eco-nav";

const personal: PayerScope = { kind: "personal", tenantId: null, display: "个人" };
const orgA: PayerScope = { kind: "organization", tenantId: "org_a", display: "活动 A" };
const orgB: PayerScope = { kind: "organization", tenantId: "org_b", display: "活动 B" };

describe("登录壳是否显示 EcoTopNav", () => {
  it("匿名营销页、登录页和分享结果页不显示", () => {
    expect(shellShowsEcoTopNav("/")).toBe(false);
    expect(shellShowsEcoTopNav("/login")).toBe(false);
    expect(shellShowsEcoTopNav("/login/")).toBe(false);
    expect(shellShowsEcoTopNav("/share")).toBe(false);
    expect(shellShowsEcoTopNav("/share/result-1")).toBe(false);
  });

  it("登录后的制作壳显示", () => {
    expect(shellShowsEcoTopNav("/projects")).toBe(true);
    expect(shellShowsEcoTopNav("/projects/p1")).toBe(true);
    expect(shellShowsEcoTopNav("/batches")).toBe(true);
    expect(shellShowsEcoTopNav("/handoff")).toBe(true);
  });
});

describe("Personal / Org scope", () => {
  it("独立制作默认个人，上传主线不强制选组织", () => {
    const scope = resolveScope({
      sourceType: "standalone",
      sourceLabel: "",
      enterpriseAssets: false,
    });
    expect(scope).toEqual(personal);
    expect(requiresOrgSelection(scope, "upload")).toBe(false);
    expect(requiresOrgSelection(scope, "generate")).toBe(false);
  });

  it("订单或活动深链显示来源组织，而不是个人", () => {
    expect(
      resolveScope({
        sourceType: "campaign",
        sourceLabel: "活动 A",
        sourceTenantId: "org_a",
        enterpriseAssets: false,
      })
    ).toEqual(orgA);
    expect(
      resolveScope({
        sourceType: "order",
        sourceLabel: "订单 100",
        sourceTenantId: "org_b",
        enterpriseAssets: false,
      }).kind
    ).toBe("organization");
  });

  it("用户主动使用企业资产时显示组织", () => {
    expect(
      resolveScope({
        sourceType: "standalone",
        sourceLabel: "企业素材",
        sourceTenantId: "org_a",
        enterpriseAssets: true,
      }).kind
    ).toBe("organization");
  });

  it("来源文案", () => {
    expect(sourceLabel({ sourceType: "campaign", sourceRef: "A", campaignRef: "A" })).toBe("活动 A");
    expect(sourceLabel({ sourceType: "order", sourceRef: "9", orderRef: "9" })).toBe("订单 9");
    expect(sourceLabel({ sourceType: "standalone", sourceRef: "" })).toBe("");
  });
});

describe("应用切换不带走当前制作", () => {
  it("当前应用是产品图，手工切换不建交接、不带工程或商品", () => {
    const plan = manualAppSwitch({
      launchUrl: "https://apps.example/goboost?project_id=p1&prompt=red&asset_id=img",
      appId: "goboost",
    });
    expect(CURRENT_APP).toEqual({ appId: "product-image", display: "产品图" });
    expect(plan.createsHandoff).toBe(false);
    expect(plan.intent).toBe("manual_switch");
    expect(plan.href).toBe("https://apps.example/goboost");
    expect(plan.href).not.toContain("project");
    expect(plan.href).not.toContain("prompt");
    expect(plan.href).not.toContain("asset");
  });

  it("拒绝非 http(s) 与带用户信息的地址", () => {
    expect(manualAppSwitch({ launchUrl: "javascript:alert(1)", appId: "x" }).href).toBeNull();
    expect(
      manualAppSwitch({ launchUrl: "https://user:pass@apps.example/a", appId: "x" }).href
    ).toBeNull();
  });
});

describe("来源返回条", () => {
  it("活动来源显示精确返回，独立制作不显示", () => {
    expect(
      returnChip({
        sourceType: "campaign",
        sourceLabel: "活动 A",
        returnHref: "https://campaign.example/c/A",
      })
    ).toEqual({
      label: "活动 A",
      href: "https://campaign.example/c/A",
      title: WORKSPACE_PRIMARY_NOTE,
    });
    expect(
      returnChip({ sourceType: "standalone", sourceLabel: "", returnHref: null })
    ).toBeNull();
  });

  it("拒绝不安全返回地址", () => {
    expect(
      returnChip({
        sourceType: "order",
        sourceLabel: "订单 1",
        returnHref: "javascript:alert(1)",
      })
    ).toBeNull();
  });
});

describe("计费徽标", () => {
  it("显示人民币，余额不足仍可看历史", () => {
    const badge = billingBadgeFromPayload(
      { balance_cny: 0, availability: "exhausted" },
      200
    );
    expect(badge.text).toBe("¥0.00");
    expect(badge.kind).toBe("cny");
    expect(badge.historyRemainsVisible).toBe(true);
    expect(badge.text).not.toMatch(/点数|修点|image coin/i);
  });

  it("渠道额度与津贴可显示，私有点数不显示", () => {
    expect(billingBadgeFromPayload({ quota_remaining: 3 }, 200).text).toBe("额度 3");
    expect(billingBadgeFromPayload({ allowance_cny: 8.5 }, 200).text).toBe("津贴 ¥8.50");
    const hidden = billingBadgeFromPayload({ points: 99, label: "修点 99" }, 200);
    expect(hidden.text).toBe("计费待确认");
    expect(hidden.text).not.toContain("99");
    expect(hidden.privatePointsRejected).toBe(true);
  });

  it("计费未开启时诚实待确认，不写假余额", () => {
    const badge = billingBadgeFromPayload(
      { error: { message: "计费开关未开启" } },
      503
    );
    expect(badge.text).toBe("计费待确认");
    expect(badge.historyRemainsVisible).toBe(true);
  });
});

describe("付款主体切换", () => {
  const book: QuoteBook = {
    quotes: [
      {
        id: "q1",
        status: "unsubmitted",
        payer: orgA,
      },
    ],
    results: [
      {
        id: "r1",
        owner: orgA,
        status: "completed",
      },
    ],
  };

  it("未提交报价失效并要求重算，已完成结果不改归属", () => {
    const next = applyPayerSwitch(book, orgB);
    expect(next.quotes[0]).toMatchObject({
      status: "invalid",
      mustRequote: true,
      payer: orgA,
    });
    expect(next.results[0].owner).toEqual(orgA);
    expect(next.results[0].status).toBe("completed");
  });

  it("同一付款主体不把报价打成失效", () => {
    const next = applyPayerSwitch(book, orgA);
    expect(next.quotes[0].status).toBe("unsubmitted");
  });
});

describe("A/B 组织隔离", () => {
  const records: OwnedRecord[] = [
    { id: "pA", kind: "project", owner: orgA },
    { id: "pB", kind: "project", owner: orgB },
    { id: "assetA", kind: "asset", owner: orgA },
    { id: "me", kind: "project", owner: personal },
  ];

  it("切到 B 后看不见 A 的工程和素材，个人工程也不混入", () => {
    const visible = visibleOwned(records, orgB).map((r) => r.id);
    expect(visible).toEqual(["pB"]);
  });
});

describe("顶栏不挤压主按钮", () => {
  it("宽屏 52px，窄屏 40px，且不宣称产品图已成熟", () => {
    expect(navLayout(1280)).toEqual({ variant: "full", heightPx: 52 });
    expect(navLayout(390)).toEqual({ variant: "compact", heightPx: 40 });
    expect(USEFUL_IMAGE_MATURE).toBe(false);
    const css = readFileSync(
      path.resolve(__dirname, "../src/components/eco-nav/eco-nav.module.css"),
      "utf8"
    );
    expect(css).toContain("height: 52px");
    expect(css).toContain("height: 40px");
    expect(css).toContain("max-width: 899px");
  });
});
