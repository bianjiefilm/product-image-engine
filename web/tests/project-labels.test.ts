// HUI-2627 fix2:工程状态/来源词汇双语统一(2625 gate-r2 blind fail 项)。
// projects 列表曾裸渲染 `active`/`standalone` 等后端枚举;按盲评判据
// 「同族中文徽章(已采用/未采用/部分失败 同款)」收敛到本模块的映射。
import { describe, expect, it } from "vitest";
import { projectStatusLabel, projectSourceLabel } from "../src/lib/project-labels";

describe("projectStatusLabel(工程状态中文词汇)", () => {
  it("契约内三种状态全部映射为中文", () => {
    expect(projectStatusLabel("draft")).toBe("草稿");
    expect(projectStatusLabel("active")).toBe("进行中");
    expect(projectStatusLabel("archived")).toBe("已归档");
  });

  it("未知状态不泄漏英文枚举,给产品语句", () => {
    expect(projectStatusLabel("")).toBe("状态未知");
    expect(projectStatusLabel("someone_added_new")).toBe("状态未知");
  });
});

describe("projectSourceLabel(来源中文词汇)", () => {
  it("与新建工程表单下拉的三项文案一致", () => {
    expect(projectSourceLabel("standalone")).toBe("独立制作");
    expect(projectSourceLabel("order")).toBe("挂接订单");
    expect(projectSourceLabel("campaign")).toBe("挂接活动");
  });

  it("空来源返回空串(调用方渲染 —),未知来源不裸奔英文", () => {
    expect(projectSourceLabel("")).toBe("");
    expect(projectSourceLabel("mystery")).toBe("来源未知");
  });
});
