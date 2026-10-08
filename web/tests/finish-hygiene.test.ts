// HUI-2627 finish-r1:web/src 全树清零/防回潮扫描(page-census 同正则口径)。
// 裸 hex 必须 0(颜色一律走语义 token);inline style / raw button / raw input /
// raw table 按 2026-10-08 既有记账水平「只许下降」(fix2 修复轮把路由页行内样式
// 全部收敛为 globals.css 版式工具类,inline_style 基线随之降至 34;raw 控件维持
// D13 结构基线)。基线明细:docs/audits/hui-2627/finish-r1/hygiene-scan.json 与
// docs/audits/hui-2627/fix2/hygiene-scan.json。
// 「无 table/form 主视觉」红线由 e2e/hui-2627-finish-r1.mjs 在真实渲染层断言。
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

function walk(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(path.join(dir, e.name)) : [path.join(dir, e.name)],
  ).filter((f) => /\.(tsx|ts)$/.test(f) && !f.endsWith(".d.ts"));
}

const srcDir = path.join(process.cwd(), "src");
const files = walk(srcDir).map((f) => ({ file: path.relative(srcDir, f), source: readFileSync(f, "utf8") }));

const RULES = {
  bare_hex: /(?:^|[^A-Za-z0-9_])#(?:[0-9A-Fa-f]{6}|[0-9A-Fa-f]{3})(?![0-9A-Fa-f])/g,
  inline_style: /(?:^|[^A-Za-z0-9_])style\s*=/g,
  raw_button: /(?:^|[^A-Za-z0-9_])<button/g,
  raw_input: /(?:^|[^A-Za-z0-9_])<input/g,
  raw_table: /(?:^|[^A-Za-z0-9_])<table/g,
} as const;

// 2026-10-08 finish-r1 基线(全 src 合计),fix2 修复轮后 inline_style 收敛至 34
// (行内样式→globals.css 版式工具类,见 docs/audits/hui-2627/fix2/hygiene-scan.json)。
// 只许下降,不许增长。
const BASELINE: Record<keyof typeof RULES, number> = {
  bare_hex: 0,
  inline_style: 34,
  raw_button: 45,
  raw_input: 53,
  raw_table: 14,
};

function count(rule: keyof typeof RULES): number {
  const re = new RegExp(RULES[rule].source, "g");
  return files.reduce((acc, f) => acc + (f.source.match(re)?.length ?? 0), 0);
}

describe("finish-r1 hygiene(清零 + 防回潮)", () => {
  it("web/src 全树没有裸 hex,颜色全部来自语义 token", () => {
    const offenders = files.filter((f) => /(?:^|[^A-Za-z0-9_])#(?:[0-9A-Fa-f]{6}|[0-9A-Fa-f]{3})(?![0-9A-Fa-f])/.test(f.source));
    expect(offenders.map((f) => f.file)).toEqual([]);
  });

  it("inline style / raw control 不超过 2026-10-08 基线(只许下降)", () => {
    for (const rule of ["inline_style", "raw_button", "raw_input", "raw_table"] as const) {
      const total = count(rule);
      expect(total, `${rule} grew past the finish-r1 baseline ${BASELINE[rule]}`).toBeLessThanOrEqual(BASELINE[rule]);
    }
  });
});
