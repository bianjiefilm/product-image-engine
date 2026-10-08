import { Suspense } from "react";
import { SurfaceErrorBoundary } from "@/components/ui/surface-error-boundary";
import StartClient from "./ui";

// /start 三态声明(HUI-2627 fix2,uifinish-scan missing_loading_empty_error 口径):
// loading = Suspense 回退;error = SurfaceErrorBoundary 回退;
// empty = 工作台壳内的候选空台(StartScreen 的 stage-empty 常驻渲染,
// 「结果会显示在这里。还没有成片时,不会用别的图代替。」)。
export default function StartPage() {
  return (
    <Suspense
      fallback={
        <p className="muted" data-state="loading">正在打开开始做产品图…</p>
      }
    >
      <div data-state="empty">
        <SurfaceErrorBoundary
          fallback={
            <div className="card" data-state="error" role="alert">
              <h2>开始做产品图没有打开</h2>
              <p className="muted">
                已有工程不会丢失;从工程列表可以回到正在做的图。
              </p>
              <p>
                <a className="link" href="/projects">返回工程列表</a>
              </p>
            </div>
          }
        >
          <StartClient />
        </SurfaceErrorBoundary>
      </div>
    </Suspense>
  );
}
