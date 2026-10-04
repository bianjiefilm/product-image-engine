import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { pageAccounts, patternIDs } from "../src/lib/page-map";
import {
  classifySurface,
  markerKindList,
  pageCensus,
  scanMarkers,
  surfaceList,
  type Marker,
} from "../src/lib/page-census";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function pagePath(route: string): string {
  return route === "/" ? "src/app/page.tsx" : `src/app${route}/page.tsx`;
}

function readPage(file: string): string {
  return readFileSync(path.join(webRoot, file), "utf8");
}

function rescan(source: string): Marker[] {
  const found: Marker[] = [];
  if (/(?:^|[^A-Za-z0-9_])TODO(?![A-Za-z0-9_])/.test(source)) found.push("TODO");
  if (/(?:^|[^A-Za-z0-9_])FIXME(?![A-Za-z0-9_])/.test(source)) found.push("FIXME");
  if (/(?:^|[^A-Za-z0-9_])style\s*=/.test(source)) found.push("inline style");
  if (source.includes("接口说明")) found.push("接口说明");
  if (source.includes("架构说明")) found.push("架构说明");
  return found;
}

describe("账上路由恰好一条普查", () => {
  it("和账本同一组路由，一条不多一条不少", () => {
    const ledger = pageAccounts().map((page) => page.route);
    const census = pageCensus();
    expect(ledger).toEqual(["/", "/login", "/start", "/projects", "/projects/[id]", "/batches", "/handoff"]);
    expect(census.map((page) => page.route)).toEqual(ledger);
    expect(new Set(census.map((page) => page.route)).size).toBe(census.length);
    expect(census.some((page) => page.route.startsWith("/api"))).toBe(false);

    const again = pageCensus();
    again[0].route = "/mutated";
    again[0].markers.push("TODO");
    again[0].id = "login";
    expect(pageCensus()[0].route).toBe("/");
    expect(pageCensus()[0].markers).toEqual([]);
    expect(pageCensus()[0].id).toBe("portal");
  });

  it("每条都指向自己的 page 文件，不指向组件", () => {
    for (const page of pageCensus()) {
      expect(page.file).toBe(pagePath(page.route));
      expect(page.file).toMatch(/\/page\.tsx$/);
      expect(page.file).not.toContain("/ui.tsx");
      expect(page.file).not.toContain("components/");
      const ledger = pageAccounts().find((item) => item.route === page.route);
      expect(page.id).toBe(ledger?.id);
    }
  });
});

describe("surface 归类", () => {
  it("只许 portal、work、editor、public、admin", () => {
    expect([...surfaceList]).toEqual(["portal", "work", "editor", "public", "admin"]);
    for (const page of pageCensus()) {
      expect(surfaceList).toContain(page.surface);
    }
  });

  it("路径含 /admin 优先，然后才按模式编号", () => {
    expect(classifySurface("/admin", "workspace")).toBe("admin");
    expect(classifySurface("/admin/users", "login")).toBe("admin");
    expect(classifySurface("/teams/admin/roles", "public_consumer")).toBe("admin");
    expect(classifySurface("/administrator", "list")).toBe("admin");
    expect(classifySurface("/admin", "public_consumer")).toBe("admin");
    expect(classifySurface("/admin", "editor_shell")).toBe("admin");
    expect(classifySurface("/admin", "portal")).toBe("admin");
    expect(classifySurface("/admin", "login")).toBe("admin");
    expect(classifySurface("/shop", "public_consumer")).toBe("public");
    expect(classifySurface("/editor", "editor_shell")).toBe("editor");
    expect(classifySurface("/login", "editor_shell")).toBe("editor");
    expect(classifySurface("/", "portal")).toBe("portal");
    expect(classifySurface("/login", "login")).toBe("portal");
    expect(classifySurface("/gate", "login")).toBe("portal");
    expect(classifySurface("/", "workspace")).toBe("work");
    expect(classifySurface("/login", "list")).toBe("work");
    expect(classifySurface("/start", "creation")).toBe("work");
    expect(classifySurface("/projects", "list")).toBe("work");
    expect(classifySurface("/projects/[id]", "detail")).toBe("work");
    expect(classifySurface("/batches", "list")).toBe("work");
    expect(classifySurface("/handoff", "review")).toBe("work");
    expect(classifySurface("/Admin", "workspace")).toBe("work");
    expect(classifySurface("admin", "list")).toBe("work");

    for (const id of patternIDs()) {
      for (const route of ["/", "/admin", "/admin/users", "/projects", "/projects/[id]", "/start", "/editor"]) {
        const surface = classifySurface(route, id);
        expect(surfaceList).toContain(surface);
        if (route.includes("/admin")) expect(surface).toBe("admin");
        else if (id === "public_consumer") expect(surface).toBe("public");
        else if (id === "editor_shell") expect(surface).toBe("editor");
        else if (id === "portal" || id === "login") expect(surface).toBe("portal");
        else expect(surface).toBe("work");
      }
    }
  });

  it("账上每一条都按这个规则归类", () => {
    const want = new Map<string, string>([
      ["/", "portal"],
      ["/login", "portal"],
      ["/start", "work"],
      ["/projects", "work"],
      ["/projects/[id]", "work"],
      ["/batches", "work"],
      ["/handoff", "work"],
    ]);
    expect(want.size).toBe(7);
    for (const page of pageCensus()) {
      expect(page.surface).toBe(classifySurface(page.route, page.id));
      expect(page.surface).toBe(want.get(page.route));
    }
  });
});

describe("stack 与未测量字段", () => {
  it("stack 全部是 next，五个测量字段都是字面量 not_measured", () => {
    const src = readFileSync(new URL("../src/lib/page-census.ts", import.meta.url), "utf8");
    expect(src).toMatch(/stack:\s*"next"/);
    for (const field of ["token_source", "states", "responsive", "accessibility", "screenshot"]) {
      expect(src).toMatch(new RegExp(`${field}:\\s*"not_measured"`));
    }
    for (const page of pageCensus()) {
      expect(page.stack).toBe("next");
      expect(page.token_source).toBe("not_measured");
      expect(page.states).toBe("not_measured");
      expect(page.responsive).toBe("not_measured");
      expect(page.accessibility).toBe("not_measured");
      expect(page.screenshot).toBe("not_measured");
    }
  });
});

describe("半成品标记", () => {
  it("只认 TODO、FIXME、inline style 和两个字面量，顺序固定", () => {
    expect([...markerKindList]).toEqual(["TODO", "FIXME", "inline style", "接口说明", "架构说明"]);
    expect(scanMarkers("")).toEqual([]);
    expect(scanMarkers("// TODO: later")).toEqual(["TODO"]);
    expect(scanMarkers("FIXME")).toEqual(["FIXME"]);
    expect(scanMarkers("todo")).toEqual([]);
    expect(scanMarkers("TODOS")).toEqual([]);
    expect(scanMarkers("MYTODO")).toEqual([]);
    expect(scanMarkers("FIXMES")).toEqual([]);
    expect(scanMarkers('<main style={{ maxWidth: 960 }}>')).toEqual(["inline style"]);
    expect(scanMarkers("<button style={btnStyle}>")).toEqual(["inline style"]);
    expect(scanMarkers("style = {btnStyle}")).toEqual(["inline style"]);
    expect(scanMarkers("lifestyle=1")).toEqual([]);
    expect(scanMarkers("styleName")).toEqual([]);
    expect(scanMarkers("<style>.a{}</style>")).toEqual([]);
    expect(scanMarkers("接口说明")).toEqual(["接口说明"]);
    expect(scanMarkers("架构说明")).toEqual(["架构说明"]);
    expect(scanMarkers("接口")).toEqual([]);
    expect(scanMarkers("架构")).toEqual([]);
    expect(scanMarkers("架构说明 style={{}} FIXME TODO 接口说明")).toEqual([
      "TODO",
      "FIXME",
      "inline style",
      "接口说明",
      "架构说明",
    ]);
    expect(scanMarkers("style={{}}\nstyle={btn}")).toEqual(["inline style"]);
  });

  it("记下的命中和重新扫描该页面文件一致", () => {
    const want = new Map<string, Marker[]>([
      ["/", []],
      ["/login", ["inline style"]],
      ["/start", []],
      ["/projects", ["inline style"]],
      ["/projects/[id]", ["inline style"]],
      ["/batches", ["inline style"]],
      ["/handoff", ["inline style"]],
    ]);
    const census = pageCensus();
    expect(census).toHaveLength(want.size);
    for (const page of census) {
      const source = readPage(page.file);
      const scanned = rescan(source);
      expect(scanMarkers(source), page.route).toEqual(scanned);
      expect(page.markers, page.route).toEqual(scanned);
      expect(page.markers, page.route).toEqual(want.get(page.route));
      for (const marker of page.markers) {
        expect(markerKindList).toContain(marker);
      }
    }
  });

  it("同目录其它文件不算进这条路由", () => {
    const start = pageCensus().find((page) => page.route === "/start");
    expect(start?.file).toBe("src/app/start/page.tsx");
    const ui = readFileSync(path.join(webRoot, "src/app/start/ui.tsx"), "utf8");
    const layout = readFileSync(path.join(webRoot, "src/app/layout.tsx"), "utf8");
    expect(start?.markers).toEqual(rescan(readPage(start?.file ?? "")));
    expect(start?.file).not.toContain("ui.tsx");
    expect(start?.file).not.toContain("layout.tsx");
    expect(rescan(ui)).toEqual([]);
    expect(rescan(layout)).toEqual([]);
    expect(readPage("src/app/page.tsx")).not.toContain("page-census");
    expect(readPage("src/app/start/page.tsx")).not.toContain("page-census");
  });
});

describe("这一片不接外部能力", () => {
  it("普查源码不包含 public-ai，也不包含 shadcn", () => {
    const src = readFileSync(new URL("../src/lib/page-census.ts", import.meta.url), "utf8");
    expect(src).not.toContain("public-ai");
    expect(src).not.toContain("shadcn");
  });
});
