import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { displayCostCents } from "@/lib/revision";

describe("局部修改费用只展示服务端整数分", () => {
  it("原样返回整数，不接受再加一次", () => {
    expect(displayCostCents(40)).toBe(40);
    expect(displayCostCents(20)).toBe(20);
    expect(displayCostCents(0)).toBe(0);
    expect(() => displayCostCents(40.5)).toThrow(/整数分/);
    expect(() => displayCostCents(-1)).toThrow(/整数分/);
  });

  it("前端模块没有第二套加法", () => {
    const src = readFileSync(path.join(process.cwd(), "src/lib/revision.ts"), "utf8");
    expect(src).not.toMatch(/centsBackground|IncrementalCostCents|simplify_background/);
    expect(src).not.toMatch(/\+\s*(20|40)/);
  });

  it("下游成功句只说明本地引用，不说已经送出", () => {
    const src = readFileSync(path.join(process.cwd(), "src/components/revision/RevisionRail.tsx"), "utf8");
    const lines = src.split("\n");
    const index = lines.findIndex((item) => item.includes("requires_reupload"));
    const note = lines.slice(index, index + 2).join("\n");
    expect(note).toContain("这一版还不能直接沿用，需要重新准备文件。");
    expect(note).toContain("只返回了");
    expect(note).toContain("本地引用");
    expect(note).toContain("没有上传文件");
    expect(note).toContain("数字人、AiCut 和矩阵都还没收到");
    expect(note).not.toContain("已送出");
    expect(note).not.toContain("已经收到");
  });
});
