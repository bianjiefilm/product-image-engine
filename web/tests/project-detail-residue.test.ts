// HUI-2627 项目页残差：主路径不再是管理表。表只许留在默认折叠的技术详情里。
// 素材、任务、费用和失败事实仍要在源码里。不改请求路径。
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const pagePath = path.join(process.cwd(), "src/app/projects/[id]/page.tsx");

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
      const isTech = /data-technical\s*=\s*["']true["']/.test(open);
      tech.push(isTech);
      if (inTech()) outside += "";
      else outside += open;
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

function fromBench(source: string): string {
  const at = source.indexOf("const benchMaterials");
  expect(at).toBeGreaterThan(0);
  return source.slice(at);
}

describe("项目页主路径不是管理表", () => {
  const source = readFileSync(pagePath, "utf8");
  const view = technicalStackSplit(fromBench(source));

  it("table 只出现在默认折叠的 data-technical 详情里，并且至少还留一张", () => {
    expect(view.tablesOutside).toBe(0);
    expect(view.tablesInside).toBeGreaterThan(0);
    const tags = stripComments(source).match(/<details\b[^>]*>/g) ?? [];
    const technical = tags.filter((tag) => /data-technical\s*=\s*["']true["']/.test(tag));
    expect(technical.length).toBeGreaterThan(0);
    for (const tag of technical) {
      const withoutBound = tag.replace(/\sopen=\{attachOpen\}/g, "");
      expect(withoutBound).not.toMatch(/\sopen[\s=>]/);
    }
  });

  it("默认视图不放 provider、模型、sha256、run_id 和内部引用", () => {
    for (const token of ["sha256", "provider", "qwen", "run_id", "principal_id", "source_kind", "asset_id"]) {
      expect(view.outside.toLowerCase(), token).not.toContain(token);
    }
    expect(view.outside).not.toContain("回执 {");
  });

  it("任务、来源、成果、尺寸的主视觉是列表，表在详情之后", () => {
    for (const [heading, until, listTag] of [
      ["任务与费用事实", "来源信息", "<ul"],
      ["来源信息", "成果登记", "<dl"],
      ["成果登记", "尺寸适配", "<ul"],
      ["尺寸适配", "", "<ul"],
    ] as const) {
      const start = view.outside.indexOf(heading);
      const end = until ? view.outside.indexOf(until, start + heading.length) : view.outside.length;
      expect(start, heading).toBeGreaterThanOrEqual(0);
      expect(end, heading).toBeGreaterThan(start);
      const slice = view.outside.slice(start, end);
      expect(slice, heading).toContain(listTag);
      expect(slice, heading).not.toContain("<table");
    }
  });

  it("空编号挂接不靠折叠里的 required，外侧说明不含 asset_id，并打开该技术详情", () => {
    const hint = "请先填写素材编号，再挂接。";
    expect(hint.toLowerCase()).not.toContain("asset_id");
    expect(stripAllDetails(source)).toContain(hint);
    const outside = stripAllDetails(fromBench(source));
    expect(outside).toContain("{attachHint}");
    expect(outside.toLowerCase()).not.toContain("asset_id");
    const src = stripComments(source);
    const formAt = src.indexOf('id="proj-attach-form"');
    expect(formAt).toBeGreaterThan(0);
    const form = src.slice(formAt, src.indexOf("</form>", formAt));
    expect(form).not.toMatch(/\srequired\b/);
    const detailsAt = src.lastIndexOf("<details", formAt);
    const detailsTag = src.slice(detailsAt, src.indexOf(">", detailsAt) + 1);
    expect(detailsTag).toContain('data-technical="true"');
    expect(detailsTag).toContain("open={attachOpen}");
    expect(src).toContain("const [attachOpen, setAttachOpen] = useState(false)");
  });

  it("去掉所有 details 内部之后，保存简报和挂接素材仍在", () => {
    const stripped = stripAllDetails(source);
    // 「尚未挂接素材」含同一子串，必须认按钮文本本身。
    expect(stripped).toContain(">挂接素材<");
    expect(stripped).toContain("保存简报");
  });

  it("主按钮在表单字段之前", () => {
    expect(source.indexOf("保存简报")).toBeLessThan(source.indexOf('id="proj-name"'));
    expect(source.indexOf("上传并挂接")).toBeLessThan(source.indexOf('id="proj-photo-upload"'));
    expect(source.indexOf("挂接素材")).toBeLessThan(source.indexOf("平台 asset_id"));
    expect(source.indexOf("登记成果")).toBeLessThan(source.indexOf('id="output-file"'));
    expect(source.indexOf("个变体")).toBeLessThan(source.indexOf('id="sa-source"'));
    const facts = source.indexOf("<h2>任务与费用事实</h2>");
    expect(facts).toBeGreaterThan(0);
    expect(source.indexOf("提交生成", facts)).toBeLessThan(source.indexOf("<table", facts));
    expect(source.indexOf("读取费用事实", facts)).toBeLessThan(source.indexOf("<table", facts));
  });

  it("诚实的失败、未知和费用事实还在，请求路径没改", () => {
    for (const fact of [
      "已采用",
      "未采用",
      "状态未知",
      "暂无输出版本",
      "尚未挂接素材",
      "尚未登记成果",
      "尚无尺寸变体",
      "独立制作",
      "这个工程没有打开",
      "不会重新做图，也不会再扣一次",
      "保存失败",
      "挂素材失败",
      "上传失败",
      "提交失败",
      "采用失败",
      "登记失败",
      "回执失败",
      "重传失败",
      "生成失败",
      "费用待确认",
      "加载失败",
      "data-state=\"loading\"",
      "data-state=\"empty\"",
      "data-state=\"error\"",
      "sourceTenantId: null",
      "/api/projects/${id}",
      "/api/billing/balance",
      "/adopt",
      "/return-target",
      "/outputs",
      "/receipt",
      "/resend",
      "/size-adapt",
      "method: \"PATCH\"",
      "method: \"DELETE\"",
    ]) {
      expect(source, fact).toContain(fact);
    }
  });

  it("触控目标样式只属于这一页，并且至少 44px", () => {
    const imported = source.match(/import\s+\w+\s+from\s+"(\.\/[^"]+\.module\.css)"/);
    expect(imported).not.toBeNull();
    const rel = imported?.[1] ?? "";
    const cssPath = path.join(path.dirname(pagePath), rel);
    const css = readFileSync(cssPath, "utf8");
    expect(css).toMatch(/min-height:\s*44px/);
    expect(css).toMatch(/min-width:\s*44px/);
    const srcRoot = path.join(process.cwd(), "src");
    const hits: string[] = [];
    const walk = (dir: string) => {
      for (const ent of readdirSync(dir, { withFileTypes: true })) {
        const abs = path.join(dir, ent.name);
        if (ent.isDirectory()) walk(abs);
        else if (/\.(tsx|ts|css|jsx|js)$/.test(ent.name)) {
          if (abs === cssPath) continue;
          if (readFileSync(abs, "utf8").includes(path.basename(cssPath))) hits.push(abs);
        }
      }
    };
    walk(srcRoot);
    expect(hits).toEqual([pagePath]);
  });
});
