import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { StartScreen } from "@/components/first-image/StartScreen";

describe("开始做产品图首屏", () => {
  it("独立入口展示制作步骤，不展示生态目录", () => {
    const html = renderToStaticMarkup(
      createElement(StartScreen, {
        title: "开始做产品图",
        showsCatalog: false,
        scope: "personal/default",
        steps: ["上传或选择商品照片", "选择使用场景和尺寸", "背景方向", "费用确认", "生成"],
        status: "failed",
        statusLabel: "未能生成",
        outputAssetId: "",
        showImage: false,
        quoteLabel: "待确认",
        usefulProven: false,
        fidelityLabel: "待确认",
        headline: "真实成片仍未生成",
        uploadRequired: true,
        taskId: "run_1",
      })
    );
    expect(html).toContain("开始做产品图");
    expect(html).toContain('data-catalog="false"');
    expect(html).toContain('data-scope="personal/default"');
    expect(html).toContain('data-task-id="run_1"');
    expect(html).toContain('data-fake-output="false"');
    expect(html).toContain('data-useful-proven="false"');
    expect(html).toContain("待确认");
    expect(html).not.toContain("模板市场");
    expect(html).not.toContain("生态应用");
  });

  it("来源深链展示已授权素材和精确返回，不再要求上传", () => {
    const html = renderToStaticMarkup(
      createElement(StartScreen, {
        title: "开始做产品图",
        showsCatalog: false,
        scope: "org_customer",
        steps: ["背景方向", "费用确认", "生成"],
        status: "queued",
        statusLabel: "排队中",
        outputAssetId: "",
        showImage: false,
        quoteLabel: "待确认",
        usefulProven: false,
        fidelityLabel: "待确认",
        headline: "真实成片仍未生成",
        uploadRequired: false,
        taskId: "run_src",
        sourceLabel: "订单 ord_9",
        returnHref: "https://order.example/orders/ord_9",
        authorizedAssets: ["asset_ord"],
      })
    );
    expect(html).toContain('data-upload-required="false"');
    expect(html).toContain('data-scope="org_customer"');
    expect(html).toContain("https://order.example/orders/ord_9");
    expect(html).toContain("asset_ord");
    expect(html).toContain("订单 ord_9");
    expect(html).not.toContain('data-scope="personal/default"');
  });
});