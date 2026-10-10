// HUI-2627 三个面板的残差。只读组件源码，切在 return ( 之后的 JSX，
// 请求体和类型字段里的名字不算页面文字。
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function technicalStackSplit(source: string): { outside: string; tablesOutside: number; tablesInside: number; technicalInner: string } {
  const src = stripComments(source);
  const tech: boolean[] = [];
  let outside = "";
  let technicalInner = "";
  let tablesOutside = 0;
  let tablesInside = 0;
  let i = 0;
  const inTech = () => tech.some(Boolean);
  while (i < src.length) {
    if (src.startsWith("<details", i)) {
      const end = src.indexOf(">", i);
      const open = src.slice(i, end + 1);
      tech.push(/data-technical\s*=\s*["']true["']/.test(open));
      if (!inTech()) outside += open;
      if (open.endsWith("/>")) tech.pop();
      i = end + 1;
      continue;
    }
    if (src.startsWith("</details>", i)) {
      const closingTech = inTech();
      tech.pop();
      if (!closingTech) outside += "</details>";
      i += "</details>".length;
      continue;
    }
    if (src.startsWith("<table", i)) {
      if (inTech()) tablesInside += 1;
      else tablesOutside += 1;
    }
    if (inTech()) technicalInner += src[i];
    else outside += src[i];
    i += 1;
  }
  return { outside, tablesOutside, tablesInside, technicalInner };
}

function stripAllDetails(source: string): string {
  const src = stripComments(source);
  let depth = 0;
  let out = "";
  let i = 0;
  while (i < src.length) {
    if (src.startsWith("<details", i)) {
      const end = src.indexOf(">", i);
      const open = src.slice(i, end + 1);
      depth += 1;
      if (open.endsWith("/>")) depth -= 1;
      i = end + 1;
      continue;
    }
    if (src.startsWith("</details>", i)) {
      depth = Math.max(0, depth - 1);
      i += "</details>".length;
      continue;
    }
    if (depth === 0) out += src[i];
    i += 1;
  }
  return out;
}

function jsxOf(rel: string): { source: string; jsx: string } {
  const source = readFileSync(path.join(process.cwd(), rel), "utf8");
  // 清理函数的 return () => 不是界面。取最后一次 return (，那是面板 JSX。
  const at = source.lastIndexOf("return (");
  expect(at).toBeGreaterThan(0);
  return { source, jsx: source.slice(at) };
}

function expectCollapsedTechnicalTables(jsx: string, tables: number) {
  const view = technicalStackSplit(jsx);
  expect(view.tablesOutside).toBe(0);
  expect(view.tablesInside).toBe(tables);
  const tags = stripComments(jsx).match(/<details\b[^>]*>/g) ?? [];
  const technical = tags.filter((tag) => /data-technical\s*=\s*["']true["']/.test(tag));
  expect(technical.length).toBeGreaterThan(0);
  for (const tag of technical) expect(tag).not.toMatch(/\sopen[\s=>]/);
  expect(view.technicalInner).not.toMatch(/\srequired[\s=>]/);
  expect(view.outside.toLowerCase()).not.toContain("asset_id");
  return view;
}

describe("主体保真面板主按钮不在闭合详情里", () => {
  const { source, jsx } = jsxOf("src/components/SubjectFidelityPanel.tsx");

  it("历史表只在默认折叠的技术详情里，记录按钮在所有详情外面", () => {
    expectCollapsedTechnicalTables(jsx, 1);
    const visible = stripAllDetails(jsx);
    expect(visible).toContain("记录主体保护");
    expect(visible).toContain("<form");
    expect(visible).toContain('verdict === "fail"');
    expect(visible).not.toContain("<table");
  });

  it("失败和未扣费说明留在折叠外，提交路径不变", () => {
    const visible = stripAllDetails(jsx);
    for (const fact of ["真实生成保真待确认", "计费待确认", "未通过", "不调用生成模型", "也不扣费"]) {
      expect(visible, fact).toContain(fact);
    }
    expect(source).toContain("fetch(`/api/projects/${projectId}/fidelity-reports`)");
    expect(source).toContain('fetch(`/api/projects/${projectId}/fidelity-reports`, {');
    expect(source).toContain('method: "POST"');
  });
});

describe("背景替换面板样本表不在默认外层", () => {
  const { source, jsx } = jsxOf("src/components/background-replace/Panel.tsx");

  it("样本表只在默认折叠的技术详情里，对照引用也不在外层", () => {
    const view = expectCollapsedTechnicalTables(jsx, 1);
    expect(view.outside).not.toContain("input_id");
    const visible = stripAllDetails(jsx);
    for (const label of [
      "生成报价",
      "确认报价",
      "生成背景",
      "锁定主体并换背景",
      "检查商品",
      "选定此候选",
      "导出同一版本",
      "刷新并恢复原任务",
      "恢复 ",
    ]) {
      expect(visible, label).toContain(label);
    }
  });

  it("费用、待确认和诚实说明留在折叠外，恢复不重新生成", () => {
    const visible = stripAllDetails(jsx);
    for (const fact of [
      "输入版本",
      "模式",
      "状态",
      "费用",
      "BILLING_PENDING",
      "待确认",
      "REAL_GENERATION_INCOMPLETE",
      "生产出图与计费均未标记为通过",
      "相似度分(不能单独通过)",
      "Logo、包装文字、规格和结构不能被背景生成改写",
    ]) {
      expect(visible, fact).toContain(fact);
    }
    expect(source).toContain("已恢复原任务,未重新生成");
    expect(source).toContain('fetch("/api/background-replacements/capabilities")');
    expect(source).toContain("fetch(`/api/projects/${projectId}/background-replacements`)");
    expect(source).toContain('method: "POST"');
  });
});

describe("光影场景面板样本表不在默认外层", () => {
  const { source, jsx } = jsxOf("src/components/light-scene/Panel.tsx");

  it("样本表只在默认折叠的技术详情里，对照引用也不在外层", () => {
    const view = expectCollapsedTechnicalTables(jsx, 1);
    expect(view.outside).not.toContain("input_id");
    const visible = stripAllDetails(jsx);
    for (const label of ["生成报价", "确认报价", "提交光影场景", "选定此候选", "导出同一版本", "核对任务"]) {
      expect(visible, label).toContain(label);
    }
  });

  it("费用、待确认和诚实说明留在折叠外，请求路径不变", () => {
    const visible = stripAllDetails(jsx);
    for (const fact of [
      "输入版本",
      "模式",
      "状态",
      "BILLING_PENDING",
      "待确认",
      "真实出图未完成",
      "生产出图与计费均未标记为通过",
      "REAL_GENERATION_INCOMPLETE",
    ]) {
      expect(visible, fact).toContain(fact);
    }
    expect(source).toContain('fetch("/api/light-scenes/capabilities")');
    expect(source).toContain("fetch(`/api/projects/${projectId}/light-scenes`)");
    expect(source).toContain('"POST"');
  });
});
