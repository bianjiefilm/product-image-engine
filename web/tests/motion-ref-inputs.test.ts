import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

function motionRefNoteSource(): string {
  return readFileSync(path.join(process.cwd(), "src/components/motion-ref/MotionRefNote.tsx"), "utf8");
}

function rowComponent(src: string): string {
  const start = src.indexOf("function MotionRefRow");
  expect(start).toBeGreaterThan(-1);
  return src.slice(start);
}

describe("Motion 引用行内输入各自持有", () => {
  it("父组件不再用一对共享 price/color state 喂给所有引用行", () => {
    const src = motionRefNoteSource();
    const noteStart = src.indexOf("export function MotionRefNote");
    const rowStart = src.indexOf("function MotionRefRow");
    expect(noteStart).toBeGreaterThan(-1);
    expect(rowStart).toBeGreaterThan(-1);
    const parent = src.slice(noteStart, rowStart);
    expect(parent).not.toContain("setPrice");
    expect(parent).not.toContain("setColor");
  });

  it("行子组件自己持有价格与颜色两个输入，互不借用", () => {
    const row = rowComponent(motionRefNoteSource());
    expect((row.match(/useState\(/g) ?? []).length).toBe(2);
    expect(row).toContain("value={price}");
    expect(row).toContain("value={color}");
    expect(row).toContain("setPrice(e.target.value)");
    expect(row).toContain("setColor(e.target.value)");
  });

  it("rows.map 渲染行组件，而不是把父级 state 复用进每一行", () => {
    const src = motionRefNoteSource();
    expect(src.indexOf("function MotionRefRow")).toBeGreaterThan(-1);
    const mapStart = src.indexOf("rows.map(");
    expect(mapStart).toBeGreaterThan(-1);
    const afterMap = src.slice(mapStart);
    const stops = ["function MotionRefRow", "function acceptDeclaration"]
      .map((marker) => afterMap.indexOf(marker))
      .filter((index) => index > -1);
    const mapBody = stops.length > 0 ? afterMap.slice(0, Math.min(...stops)) : afterMap;
    expect(mapBody).toContain("<MotionRefRow");
    expect(mapBody).not.toContain("value={price}");
    expect(mapBody).not.toContain("value={color}");
  });

  it("行内文案与逐条记录按钮保持不变，仍按引用 id 逐条提交", () => {
    const row = rowComponent(motionRefNoteSource());
    expect(row).toContain("价格");
    expect(row).toContain("颜色");
    expect(row).toContain("只记录价格");
    expect(row).toContain("只记录颜色");
    expect(row).toContain('onRecord("price"');
    expect(row).toContain('onRecord("color"');
    expect(row).toContain("row.id");
    const src = motionRefNoteSource();
    expect(src).toContain("/parameters");
    expect(src).toContain("encodeURIComponent(id)");
  });
});
