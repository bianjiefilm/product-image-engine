// HUI-2627 交接页残差：主路径不是管理表。表只许留在默认折叠的技术详情里。
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const pagePath = path.join(process.cwd(), "src/app/handoff/page.tsx");

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function technicalStackSplit(source: string): { outside: string; tablesOutside: number; tablesInside: number } {
  const src = stripComments(source);
  const tech: boolean[] = [];
  let outside = "";
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
    if (!inTech()) outside += src[i];
    i += 1;
  }
  return { outside, tablesOutside, tablesInside };
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

describe("交接页主路径不是管理表", () => {
  const source = readFileSync(pagePath, "utf8");
  const jsx = source.slice(source.indexOf("return ("));
  const view = technicalStackSplit(jsx);

  it("两张表都只在默认折叠的技术详情里", () => {
    expect(view.tablesOutside).toBe(0);
    expect(view.tablesInside).toBe(2);
    const tags = stripComments(source).match(/<details\b[^>]*>/g) ?? [];
    const technical = tags.filter((tag) => /data-technical\s*=\s*["']true["']/.test(tag));
    expect(technical.length).toBeGreaterThan(0);
    for (const tag of technical) expect(tag).not.toMatch(/\sopen[\s=>]/);
  });

  it("来源和差异是列表，主按钮不在闭合详情里，默认视图不放 principal_id", () => {
    const outside = stripAllDetails(jsx);
    expect(outside).toContain("<dl");
    expect(outside).toContain("<ul");
    expect(outside).not.toContain("<table");
    expect(outside).toContain("接受交接");
    expect(outside).toContain("确认差异并采用新版");
    expect(view.outside).not.toContain("principal_id");
    expect(source.indexOf("接受交接")).toBeLessThan(source.indexOf('id="handoff-doc"'));
    expect(source).toMatch(/style\s*=/);
  });

  it("失败事实和请求路径还在", () => {
    for (const fact of [
      "交接文档不是合法 JSON",
      "接受失败(HTTP",
      "采用失败(HTTP",
      "已采用新版需求(人工编辑与已选输出保留)",
      'fetch("/api/handoffs/accept"',
      'fetch(`/api/bindings/${result.binding.id}/adopt`',
      'method: "POST"',
      "/api/auth/session",
      "principal_id",
    ]) {
      expect(source, fact).toContain(fact);
    }
  });
});
