// HUI-2627 fix2:SurfaceErrorBoundary = 页面源 error 态声明的载体。
// 静态渲染断言(与 ui-error-banner.test.tsx 同口径,node 环境)。
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SurfaceErrorBoundary } from "@/components/ui/surface-error-boundary";

describe("SurfaceErrorBoundary", () => {
  it("正常时原样渲染 children,不产生额外包裹", () => {
    const html = renderToStaticMarkup(
      createElement(SurfaceErrorBoundary, {
        fallback: createElement("p", null, "回退"),
        children: createElement("span", null, "工作台内容"),
      })
    );
    expect(html).toContain("工作台内容");
    expect(html).not.toContain("回退");
  });

  it("getDerivedStateFromError 把边界置为 failed", () => {
    expect(SurfaceErrorBoundary.getDerivedStateFromError(new Error("boom"))).toEqual({ failed: true });
  });

  it("failed 时渲染 data-state=\"error\" 的产品语句回退", () => {
    class Broken extends SurfaceErrorBoundary {
      state: { failed: boolean } = { failed: true };
    }
    const html = renderToStaticMarkup(
      createElement(Broken, {
        fallback: createElement(
          "div",
          { className: "card", "data-state": "error" },
          "这个页面没有打开。你的成品不会丢失,可以返回工程列表继续。"
        ),
        children: createElement("span", null, "工作台内容"),
      })
    );
    expect(html).toContain('data-state="error"');
    expect(html).toContain("这个页面没有打开");
    expect(html).not.toContain("工作台内容");
  });
});
