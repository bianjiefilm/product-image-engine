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
});
