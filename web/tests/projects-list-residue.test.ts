// HUI-2627 工程列表残差：主路径不是管理表。表只许留在默认折叠的技术详情里。
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const pagePath = path.join(process.cwd(), "src/app/projects/page.tsx");

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

describe("工程列表主路径不是管理表", () => {
  const source = readFileSync(pagePath, "utf8");
  const view = technicalStackSplit(source);

  it("table 只出现在默认折叠的 data-technical 详情里", () => {
    expect(view.tablesOutside).toBe(0);
    expect(view.tablesInside).toBeGreaterThan(0);
    const tags = stripComments(source).match(/<details\b[^>]*>/g) ?? [];
    const technical = tags.filter((tag) => /data-technical\s*=\s*["']true["']/.test(tag));
    expect(technical.length).toBeGreaterThan(0);
    for (const tag of technical) expect(tag).not.toMatch(/\sopen[\s=>]/);
  });

  it("工程事实是列表，新建和创建都不在闭合详情里", () => {
    const outside = stripAllDetails(source);
    expect(outside).toContain("<ul");
    expect(outside).not.toContain("<table");
    expect(outside).toContain("新建工程");
    expect(source.indexOf('创建中…" : "创建"')).toBeLessThan(source.indexOf("工程名称"));
    expect(source.indexOf('创建中…" : "创建"')).toBeGreaterThan(0);
  });

  it("空态、失败和请求路径还在", () => {
    for (const fact of [
      "加载失败",
      "创建失败",
      "网络不可用,暂时无法确认登录状态,请稍后重试。",
      "网络不可用,暂时拿不到工程列表,请稍后重试。",
      "还没有工程。",
      'data-state="loading"',
      'data-state="empty"',
      'data-state="error"',
      'fetch("/api/projects")',
      'fetch("/api/auth/session")',
      'method: "POST"',
    ]) {
      expect(source, fact).toContain(fact);
    }
  });
});
