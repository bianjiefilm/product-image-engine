"use client";

import { Component, type ReactNode } from "react";

// HUI-2627 fix2:路由页三态声明(loading/empty/error)的 error 腿。
// 页面源用本边界显式声明 error 态:渲染崩溃时给 data-state="error" 的
// 产品语句回退(uifinish-scan missing_loading_empty_error 口径),
// 不把堆栈或后端字符串裸给用户。
export class SurfaceErrorBoundary extends Component<
  { children: ReactNode; fallback: ReactNode },
  { failed: boolean }
> {
  state: { failed: boolean } = { failed: false };

  static getDerivedStateFromError(_error: unknown): { failed: boolean } {
    return { failed: true };
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}
