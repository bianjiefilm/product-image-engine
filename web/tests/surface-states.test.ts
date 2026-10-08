// HUI-2627 fix2:六条正式路由(4 个页面源)的 loading/empty/error 静态声明防回潮。
// 口径 = uifinish-scan renderedHasState:data-state="loading|empty|error" 字面量
// 必须存在于路由页面源(2625 gate-r2 missing_loading_empty_error 6/6 修复项)。
// 改判读口径归 HUI-2619;在那之前本测试钉住现行口径。
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

// routes-p0.json product-image-web 六条路由去重后的页面源。
const PAGES = [
  "src/app/login/page.tsx",
  "src/app/start/page.tsx",
  "src/app/projects/page.tsx",
  "src/app/projects/[id]/page.tsx",
] as const;

const STATES = ["loading", "empty", "error"] as const;

function hasStateLiteral(source: string, state: string): boolean {
  // 与 internal/uifinish stateAttrPattern 同口径:属性字面量或 { "state" } 形式。
  const re = new RegExp(`\\bdata-state\\s*=\\s*(?:["']${state}["']|\\{\\s*["']${state}["']\\s*\\})`, "i");
  return re.test(source);
}

describe("六条正式路由的三态静态声明(防回潮)", () => {
  for (const page of PAGES) {
    it(`${page} 声明 loading/empty/error 三态`, () => {
      const source = readFileSync(path.join(webRoot, page), "utf8");
      for (const state of STATES) {
        expect(hasStateLiteral(source, state), `${page} 缺少 data-state="${state}" 静态声明`).toBe(true);
      }
    });
  }
});
