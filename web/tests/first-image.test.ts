import { describe, expect, it } from "vitest";
import {
  PERSONAL_SCOPE,
  firstScreen,
  loggedInHome,
  presentResult,
  resumeTask,
  statusLabel,
} from "@/lib/first-image";

describe("首次做产品图的入口", () => {
  it("登录后进入开始做产品图，而不是工程目录", () => {
    expect(loggedInHome()).toBe("/start");
    const screen = firstScreen();
    expect(screen.title).toBe("开始做产品图");
    expect(screen.showsCatalog).toBe(false);
    expect(screen.steps.length).toBeGreaterThanOrEqual(4);
  });

  it("个人使用的范围是 personal/default", () => {
    expect(PERSONAL_SCOPE).toBe("personal/default");
  });
});

describe("生成状态和诚实降级", () => {
  it("区分排队、进行、未知、失败和完成", () => {
    expect(statusLabel("queued")).toBe("排队中");
    expect(statusLabel("running")).toBe("生成中");
    expect(statusLabel("unknown")).toBe("结果未知");
    expect(statusLabel("failed")).toBe("未能生成");
    expect(statusLabel("completed")).toBe("已返回候选");
  });

  it("没有真实资产时不展示成片，计费显示待确认", () => {
    const view = presentResult({
      status: "failed",
      outputAssetId: "",
      billingConnected: false,
    });
    expect(view.showImage).toBe(false);
    expect(view.quoteLabel).toBe("待确认");
    expect(view.charged).toBe(false);
    expect(view.usefulProven).toBe(false);
    expect(view.fidelityLabel).toBe("待确认");
    expect(view.headline).toContain("真实成片仍未生成");
  });

  it("上游返回了资产也不宣称可售", () => {
    const view = presentResult({
      status: "completed",
      outputAssetId: "asset_real",
      billingConnected: false,
    });
    expect(view.showImage).toBe(true);
    expect(view.usefulProven).toBe(false);
    expect(view.charged).toBe(false);
    expect(view.quoteLabel).toBe("待确认");
    expect(view.headline).toContain("仍未证明");
  });

  it("完成但没有资产不能当成图", () => {
    const view = presentResult({
      status: "completed",
      outputAssetId: "",
      billingConnected: false,
    });
    expect(view.showImage).toBe(false);
    expect(view.usefulProven).toBe(false);
  });

  it("刷新回来仍是同一个任务", () => {
    expect(resumeTask("run_1", "run_1")).toBe(true);
    expect(resumeTask("run_2", "run_1")).toBe(false);
  });
});
