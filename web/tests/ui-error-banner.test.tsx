// HUI-2627 finish-r1:错误态必须有恢复动作。ErrorBanner = 危险 Alert + 重试按钮。
// 静态渲染断言(与 first-image-screen.test.tsx 同口径,node 环境)。
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ErrorBanner } from "@/components/ui/error-banner";

describe("ErrorBanner", () => {
  it("renders the message as a danger alert with a retry action", () => {
    const html = renderToStaticMarkup(createElement(ErrorBanner, { message: "产品服务暂不可达,请稍后重试", onRetry: () => {} }));
    expect(html).toMatch(/role="alert"/);
    expect(html).toContain("产品服务暂不可达,请稍后重试");
    expect(html).toMatch(/data-testid="error-retry"/);
    expect(html).toContain("重试");
  });

  it("never renders anything when there is no message", () => {
    const html = renderToStaticMarkup(createElement(ErrorBanner, { message: "", onRetry: () => {} }));
    expect(html).toBe("");
  });
});
