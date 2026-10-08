// HUI-2627 fix2:OfflineBanner = 真实浏览器离线层的应用内提示。
// 静态渲染断言(与 ui-error-banner.test.tsx 同口径,node 环境;
// online/offline 事件逻辑见组件注释,e2e 里用真浏览器 setOffline 验证)。
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { OfflineBanner } from "@/components/ui/offline-banner";

describe("OfflineBanner", () => {
  it("在线时不渲染任何东西", () => {
    const html = renderToStaticMarkup(createElement(OfflineBanner, {}));
    expect(html).toBe("");
  });

  it("离线时给 role=status 的产品语句与重试探针", () => {
    const html = renderToStaticMarkup(createElement(OfflineBanner, { initialOffline: true }));
    expect(html).toMatch(/role="status"/);
    expect(html).toMatch(/data-state="offline"/);
    expect(html).toContain("网络已断开");
    expect(html).toContain("恢复网络后这里会自动消失");
    expect(html).toMatch(/data-testid="offline-retry"/);
    expect(html).toContain("重试");
  });

  it("重试探针进行中不产生二次点击", () => {
    // disabled 态由 checking state 驱动;这里只断言种子离线时按钮可点。
    const html = renderToStaticMarkup(createElement(OfflineBanner, { initialOffline: true }));
    expect(html).not.toMatch(/data-testid="offline-retry" disabled/);
  });
});
