// HUI-2627 批次页残差：主路径不是管理表。表只许留在默认折叠的技术详情里。
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const pagePath = path.join(process.cwd(), "src/app/batches/page.tsx");

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

describe("批次页主路径不是管理表", () => {
  const source = readFileSync(pagePath, "utf8");
  const view = technicalStackSplit(source);

  it("两张表都只在默认折叠的技术详情里", () => {
    expect(view.tablesOutside).toBe(0);
    expect(view.tablesInside).toBe(2);
    const tags = stripComments(source).match(/<details\b[^>]*>/g) ?? [];
    const technical = tags.filter((tag) => /data-technical\s*=\s*["']true["']/.test(tag));
    expect(technical.length).toBeGreaterThan(0);
    for (const tag of technical) expect(tag).not.toMatch(/\sopen[\s=>]/);
  });

  it("批次和条目事实是列表，查看和提交不在闭合详情里", () => {
    const outside = stripAllDetails(source);
    expect(outside).toContain("<ul");
    expect(outside).not.toContain("<table");
    expect(outside).toContain("查看");
    expect(outside).toContain("提交批次");
    expect(outside).toContain("建批(");
    expect(source.indexOf("建批(")).toBeLessThan(source.indexOf('id="batch-name"'));
    expect(source).toMatch(/style\s*=/);
  });

  it("失败事实和请求路径还在", () => {
    for (const fact of [
      "部分失败",
      "失败",
      "已取消",
      "加载批次失败",
      "建批失败",
      "操作失败",
      "请选择至少一个 PNG 文件",
      "error_code",
      'fetch("/api/batches")',
      'fetch(`/api/batches/${batchId}`)',
      'fetch(`/api/batches/${id}/collect`',
      'method: "POST"',
      "retry-failed",
      "/api/auth/session",
      "/api/size-adapt/presets",
    ]) {
      expect(source, fact).toContain(fact);
    }
  });
});
