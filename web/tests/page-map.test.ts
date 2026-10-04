import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  MissingRegionError,
  PATTERN_IDS,
  REQUIRED_REGIONS,
  UnknownPatternError,
  accountPages,
  pageAccounts,
  patternIDs,
  requiredRegions,
  routesFromPageFiles,
  validatePageDeclaration,
  type PageDeclaration,
} from "@/lib/page-map";

const regions = ["primary_action", "status", "empty"] as const;

function listAppFiles(root: string): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.name.startsWith(".")) continue;
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      out.push(path.relative(root, full).split(path.sep).join("/"));
    }
  };
  walk(root);
  return out;
}

function row(route: string, id: string, regionNames: readonly string[] = regions): PageDeclaration {
  return { route, id, regions: [...regionNames] };
}

describe("冻结的页面模式编号", () => {
  it("14 个编号一个不能增删，顺序固定", () => {
    expect(patternIDs()).toEqual([
      "portal",
      "login",
      "workspace",
      "list",
      "detail",
      "creation",
      "media_result",
      "compare",
      "review",
      "billing",
      "settings",
      "recovery",
      "public_consumer",
      "editor_shell",
    ]);
    expect(PATTERN_IDS).toHaveLength(14);
    const copy = patternIDs();
    copy[0] = "mutated";
    expect(patternIDs()[0]).toBe("portal");
  });

  it("必带区域是 primary_action、status、empty，比较逐字", () => {
    expect(requiredRegions()).toEqual(["primary_action", "status", "empty"]);
    const copy = requiredRegions();
    copy[0] = "mutated";
    expect(requiredRegions()[0]).toBe("primary_action");
  });

  it("漏掉任一必带区域就失败，缺的名字按必带顺序", () => {
    for (const id of patternIDs()) {
      for (const drop of REQUIRED_REGIONS) {
        const kept = REQUIRED_REGIONS.filter((region) => region !== drop);
        expect(() => validatePageDeclaration({ id, regions: kept })).toThrow(MissingRegionError);
        try {
          validatePageDeclaration({ id, regions: kept });
        } catch (error) {
          expect(error).toBeInstanceOf(MissingRegionError);
          expect(error).toMatchObject({ id, regions: [drop] });
        }
      }
      expect(() => validatePageDeclaration({ id })).toThrow(MissingRegionError);
      try {
        validatePageDeclaration({ id, regions: [] });
      } catch (error) {
        expect(error).toMatchObject({ id, regions: [...REQUIRED_REGIONS] });
      }
    }
  });

  it("未知编号失败，而且不报成漏区域", () => {
    for (const id of ["", "Portal", "portal-landing", "goboost", "editor-shell", "media-result", "wizard"]) {
      expect(() => validatePageDeclaration({ id, regions })).toThrow(UnknownPatternError);
      try {
        validatePageDeclaration({ id, regions });
      } catch (error) {
        expect(error).toBeInstanceOf(UnknownPatternError);
        expect(error).not.toBeInstanceOf(MissingRegionError);
        expect(error).toMatchObject({ id });
      }
    }
  });

  it("区域名多空白或大小写不同算缺，多余区域名忽略", () => {
    expect(() =>
      validatePageDeclaration({
        id: "portal",
        regions: [" primary_action", "Status", "empty"],
      }),
    ).toThrow(MissingRegionError);
    try {
      validatePageDeclaration({
        id: "detail",
        regions: [" primary_action", "Status", "empty"],
      });
    } catch (error) {
      expect(error).toMatchObject({ id: "detail", regions: ["primary_action", "status"] });
    }
    expect(() =>
      validatePageDeclaration({ id: "detail", regions: [...regions, "header"] }),
    ).not.toThrow();
  });
});

describe("用户页面账", () => {
  const appDir = path.join(process.cwd(), "src/app");

  it("扫描到的页面文件和账上的路由一一对应", () => {
    const files = listAppFiles(appDir);
    expect(files.some((file) => file.startsWith("api/") && file.endsWith("/route.ts"))).toBe(true);
    const routes = routesFromPageFiles(files);
    expect(routes.filter((route) => route === "/api" || route.startsWith("/api/"))).toEqual([]);
    expect(new Set(routes).size).toBe(routes.length);
    expect([...routes].sort()).toEqual([
      "/",
      "/batches",
      "/handoff",
      "/login",
      "/projects",
      "/projects/[id]",
      "/start",
    ]);
    expect(accountPages(pageAccounts(), routes)).toEqual([]);
    expect(pageAccounts().map((item) => item.route).sort()).toEqual([...routes].sort());
    const byRoute = new Map(pageAccounts().map((item) => [item.route, item]));
    expect(byRoute.get("/")?.id).toBe("portal");
    expect(byRoute.get("/login")?.id).toBe("login");
    expect(byRoute.get("/start")?.id).toBe("creation");
    expect(byRoute.get("/projects")?.id).toBe("list");
    expect(byRoute.get("/batches")?.id).toBe("list");
    expect(byRoute.get("/projects/[id]")?.id).toBe("detail");
    expect(byRoute.get("/handoff")?.id).toBe("review");
    for (const item of pageAccounts()) {
      expect(item.regions).toEqual([...regions]);
      expect(patternIDs()).toContain(item.id);
    }
  });

  it("返回的账是副本", () => {
    const first = pageAccounts();
    first[0].regions[0] = "mutated";
    first.push(row("/extra", "list"));
    expect(pageAccounts()[0].regions[0]).toBe("primary_action");
    expect(pageAccounts().some((item) => item.route === "/extra")).toBe(false);
  });

  it("缺页、多页、重复路由、未知编号、漏区域都失败", () => {
    const disk = ["/", "/login"];
    expect(accountPages([row("/", "portal")], disk)).toEqual([
      { kind: "missing_page", route: "/login" },
    ]);
    expect(accountPages([row("/", "portal"), row("/login", "login"), row("/missing-file", "list")], disk)).toEqual([
      { kind: "extra_page", route: "/missing-file" },
    ]);
    expect(accountPages([row("/login", "login"), row("/login", "login")], ["/login"])).toEqual([
      { kind: "duplicate_route", route: "/login" },
    ]);
    expect(accountPages([row("/login", "login")], ["/login", "/login"])).toEqual([
      { kind: "duplicate_route", route: "/login" },
    ]);
    expect(accountPages([row("/login", "editor-shell")], ["/login"])).toEqual([
      { kind: "unknown_id", route: "/login", id: "editor-shell" },
    ]);
    expect(accountPages([row("/login", "login", ["status", "empty"])], ["/login"])).toEqual([
      { kind: "missing_region", route: "/login", id: "login", regions: ["primary_action"] },
    ]);
    expect(
      accountPages([row("/projects/:id", "detail")], ["/projects/[id]"]),
    ).toEqual([
      { kind: "missing_page", route: "/projects/[id]" },
      { kind: "extra_page", route: "/projects/:id" },
    ]);
    expect(accountPages([row("/login", "login", [...regions, "header"])], ["/login"])).toEqual([]);
  });

  it("页面文件不引用这本账，账也不引用 public-ai 或页面", () => {
    const src = readFileSync(path.join(process.cwd(), "src/lib/page-map.ts"), "utf8");
    expect(src).not.toContain("public-ai");
    expect(src).not.toContain('from "react"');
    expect(src).not.toContain('from "next');
    expect(src).not.toContain("node:fs");
    const appFiles = listAppFiles(appDir).filter((file) => file.endsWith(".ts") || file.endsWith(".tsx"));
    for (const file of appFiles) {
      const text = readFileSync(path.join(appDir, file), "utf8");
      expect(text, file).not.toContain("page-map");
      expect(text, file).not.toContain("pageAccounts");
    }
  });
});
