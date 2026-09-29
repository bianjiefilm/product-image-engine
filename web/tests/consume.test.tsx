import { describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { POST as quotePOST } from "@/app/api/billing/consume/quote/route";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ConsumePanel } from "@/components/billing/ConsumePanel";
import {
  afterTopupPage,
  canGenerate,
  completionLevels,
  configurationLabel,
  estimateShown,
  payerLabel,
  presentQuote,
  quoteFingerprint,
  visibleBooks,
} from "@/lib/consume";

describe("产品图消费报价", () => {
  it("独立使用默认个人付款，活动入口只显示已验证组织", () => {
    expect(payerLabel({ sourceType: "", personalDisplay: "个人付款" })).toBe("个人付款");
    expect(
      payerLabel({
        sourceType: "campaign",
        personalDisplay: "个人付款",
        verifiedOrgDisplay: "A商家付款",
        identityConfigured: false,
      })
    ).toBe("配置缺失");
    expect(
      payerLabel({
        sourceType: "campaign",
        personalDisplay: "个人付款",
        verifiedOrgDisplay: "A商家付款",
      })
    ).toBe("A商家付款");
    expect(
      visibleBooks("acct_org_a", [
        { accountRef: "acct_org_a", label: "A商家付款" },
        { accountRef: "acct_org_b", label: "B商家付款" },
      ]).map((book) => book.label)
    ).toEqual(["A商家付款"]);
  });

  it("用人民币或套餐额度报价，不发明点数", () => {
    expect(presentQuote({ amountMinor: 150, currency: "CNY" })).toBe("本次预计 ¥1.50");
    expect(presentQuote({ includedAllowance: true })).toBe("已包含额度");
    expect(presentQuote({})).toBe("待确认");
    expect(() => presentQuote({ privatePoints: "120点数" })).toThrow(/点数/);
  });

  it("切换付款主体、模型、数量或分辨率后必须重新确认", () => {
    const base = {
      confirmedFingerprint: "fp-1",
      currentFingerprint: "fp-1",
      entitled: true,
      fundsShort: false,
      priced: true,
    };
    expect(canGenerate(base)).toEqual({ allowed: true, mustRequote: false });
    for (const currentFingerprint of ["payer", "model", "count", "resolution"]) {
      expect(canGenerate({ ...base, currentFingerprint }).allowed).toBe(false);
    }
    expect(canGenerate({ ...base, entitled: false, cashMinor: 999900 }).allowed).toBe(false);
  });

  it("充值成功页不能直接生成，完成等级不标生产通过", () => {
    expect(afterTopupPage("success", false)).toEqual({
      allowed: false,
      reason: "requote_required",
    });
    const levels = completionLevels();
    expect(levels.billing).toBe("UNKNOWN");
    expect(levels.productionAuthorized).toBe("NOT_AUTHORIZED");
    expect(levels.channelBilling).toBe("NOT_APPLICABLE");
  });

  it("界面给出统一充值入口，并藏起其他商家", () => {
    const html = renderToStaticMarkup(
      createElement(ConsumePanel, {
        payerDisplay: "A商家付款",
        presentation: "本次预计 ¥1.50",
        generateAllowed: false,
        mustRequote: true,
        topup: {
          entry: "billing_center",
          returnTarget: "ti-product-image-engine-web",
          quoteRef: "qte_1",
        },
        levels: completionLevels(),
        connection: "配置缺失",
        settlement: "作品完成待核对",
      })
    );
    expect(html).toContain("配置缺失");
    expect(html).toContain("作品完成待核对");
    expect(html).toContain("生成完成不等于已结算");
    expect(html).toContain("A商家付款");
    expect(html).toContain("本次预计 ¥1.50");
    expect(html).toContain("重新确认报价");
    expect(html).toContain("billing_center");
    expect(html).toContain("ti-product-image-engine-web");
    expect(html).toContain("qte_1");
    expect(html).toContain("UNKNOWN");
    expect(html).toContain("NOT_AUTHORIZED");
    expect(html).not.toContain("B商家");
    expect(html).not.toContain("点数");
    expect(html).toContain('data-generate="false"');
    expect(html).not.toContain("<input");
    expect(html).toContain('data-payer-source="server"');
  });

  it("没有报价对象时不显示人民币，夹具报价不是 Billing PASS", () => {
    expect(estimateShown("本次预计 ¥1.50", false)).toBe("待确认");
    expect(estimateShown("本次预计 ¥1.50", true)).toBe("本次预计 ¥1.50");
    expect(configurationLabel({ identityConfigured: false, billingConfigured: true, adapterPresent: true })).toBe("配置缺失");
    expect(configurationLabel({ identityConfigured: true, billingConfigured: false, adapterPresent: true })).toBe("配置缺失");
    expect(configurationLabel({ identityConfigured: true, billingConfigured: true, adapterPresent: false })).toBe("未实现");
    expect(configurationLabel({ identityConfigured: false, billingConfigured: false, adapterPresent: true })).not.toBe("代码未实现");
    const base = quoteFingerprint({
      payerAccountRef: "acct_a",
      imageCount: 1,
      resolution: "800x800",
      capability: "image.generate.standard",
      pricingVersion: "pricing-2026-09",
      model: "standard",
      size: "800x800",
    });
    expect(
      quoteFingerprint({
        payerAccountRef: "acct_a",
        imageCount: 1,
        resolution: "800x800",
        capability: "image.generate.standard",
        pricingVersion: "pricing-2026-09",
        model: "premium",
        size: "800x800",
      })
    ).not.toBe(base);
    const html = renderToStaticMarkup(
      createElement(ConsumePanel, {
        payerDisplay: "个人付款",
        presentation: "本次预计 ¥1.50",
        generateAllowed: false,
        mustRequote: true,
        quoted: true,
        quoteOrigin: "fixture",
        fundsState: "not_sent",
        connection: "已配置",
        levels: completionLevels(),
      })
    );
    expect(html).toContain("本次预计 ¥1.50");
    expect(html).toContain('data-quote-origin="fixture"');
    expect(html).toContain("not_sent");
    expect(html).toContain('data-billing-pass="UNKNOWN"');
    expect(html).not.toContain("Billing PASS");
    expect(html).not.toContain("<input");
  });

  it("活动入口在服务端回答前不自称组织付款", () => {
    const start = require("node:fs").readFileSync(
      require("node:path").join(process.cwd(), "src/app/start/ui.tsx"),
      "utf8"
    );
    expect(start).not.toContain("组织付款");
    expect(start).toContain("/api/billing/consume/return");
    expect(start).toContain("个人付款");
    expect(start).not.toContain("payer_account_ref");
    expect(start).not.toContain("代码未实现");
    expect(start).toContain("配置缺失");
    expect(start).toContain("funds_state");
    expect(start).toContain('model: form.model');
    expect(start).not.toContain("topupPage === \"success\" ? false : quotedGenerate && generate");
  });
});

describe("消费报价 BFF", () => {
  it("未登录不转发到产品服务", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const res = await quotePOST(
      new NextRequest("http://localhost/api/billing/consume/quote", {
        method: "POST",
        body: JSON.stringify({ project_id: "prj" }),
      })
    );
    expect(res.status).toBe(401);
    expect(fetchMock).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
