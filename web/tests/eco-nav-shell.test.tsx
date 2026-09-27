import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import EcoTopNav from "@/components/eco-nav/EcoTopNav";

describe("EcoTopNav 标记", () => {
  it("登录壳标出当前应用、人民币徽标和历史仍可看，且不宣称已成熟", () => {
    const html = renderToStaticMarkup(
      createElement(EcoTopNav, {
        scope: { kind: "personal", tenantId: null, display: "个人" },
        badgeText: "¥12.50",
        historyVisible: true,
        returnTo: {
          label: "活动 A",
          href: "https://campaign.example/c/A",
          title: "返回来源",
        },
        switchable: [],
        apps: [
          { appId: "product-image", display: "产品图", href: null, current: true },
          {
            appId: "goboost",
            display: "GoBoost",
            href: "https://apps.example/goboost",
            current: false,
          },
        ],
        onSwitchScope: () => {},
        onLogout: () => {},
      })
    );
    expect(html).toContain('data-eco-topnav="true"');
    expect(html).toContain('data-app="product-image"');
    expect(html).toContain('data-history-visible="true"');
    expect(html).toContain('data-useful-mature="false"');
    expect(html).toContain("产品图");
    expect(html).toContain("个人");
    expect(html).toContain("¥12.50");
    expect(html).toContain("返回 活动 A");
    expect(html).toContain('rel="noreferrer"');
    expect(html).not.toContain("project_id");
    expect(html).not.toMatch(/点数|修点/);
  });
});
