// HUI-2624 第一片：只给已经存在的用户页面记账。
// 14 个模式编号冻结。每条页面声明必须带 primary_action、status、empty。
// 不渲染页面，不改路由。编辑器密度和公开页导航都不在这一片。
// 不能据此把 HUI-2624、HUI-2627、HUI-2596、HUI-2232 或 HUI-2619 标 Done。

export const PATTERN_IDS = Object.freeze([
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
] as const);

export type PatternId = (typeof PATTERN_IDS)[number];

export const REQUIRED_REGIONS = Object.freeze(["primary_action", "status", "empty"] as const);

export type PageDeclaration = {
  route: string;
  id: string;
  regions: string[];
};

export type AccountProblem =
  | { kind: "missing_page"; route: string }
  | { kind: "extra_page"; route: string }
  | { kind: "duplicate_route"; route: string }
  | { kind: "unknown_id"; route: string; id: string }
  | { kind: "missing_region"; route: string; id: string; regions: string[] };

const PAGE_FILE = /^page\.(tsx|ts|jsx|js)$/;

export class UnknownPatternError extends Error {
  readonly id: string;

  constructor(id: string) {
    super(`未知模式编号: ${id === "" ? "<empty>" : id}`);
    this.name = "UnknownPatternError";
    this.id = id;
  }
}

export class MissingRegionError extends Error {
  readonly id: string;
  readonly regions: string[];

  constructor(id: string, regions: readonly string[]) {
    const label = id === "" ? "<empty>" : id;
    super(`模式 ${label} 缺少必带区域 ${regions.join(",")}`);
    this.name = "MissingRegionError";
    this.id = id;
    this.regions = [...regions];
  }
}

export function patternIDs(): string[] {
  return [...PATTERN_IDS];
}

export function requiredRegions(): string[] {
  return [...REQUIRED_REGIONS];
}

function knownPattern(id: string): id is PatternId {
  return (PATTERN_IDS as readonly string[]).includes(id);
}

// 未知编号先失败。区域名逐字比较，前后空白和大小写都算不同。多余区域名忽略。
export function validatePageDeclaration(declaration: {
  id: string;
  regions?: readonly string[] | null;
}): void {
  if (!knownPattern(declaration.id)) {
    throw new UnknownPatternError(declaration.id);
  }
  const have = new Set(declaration.regions ?? []);
  const missing = REQUIRED_REGIONS.filter((region) => !have.has(region));
  if (missing.length > 0) {
    throw new MissingRegionError(declaration.id, missing);
  }
}

function account(route: string, id: PatternId): PageDeclaration {
  return {
    route,
    id,
    regions: [...REQUIRED_REGIONS],
  };
}

// 一条路由一条账。没有单独页面的模式不占路由。
const PAGE_ACCOUNTS: readonly PageDeclaration[] = Object.freeze([
  account("/", "portal"),
  account("/login", "login"),
  account("/start", "creation"),
  account("/projects", "list"),
  account("/projects/[id]", "detail"),
  account("/batches", "list"),
  // 交接页决定是否接受并采用来源差异，不是成片对比页。
  account("/handoff", "review"),
]);

export function pageAccounts(): PageDeclaration[] {
  return PAGE_ACCOUNTS.map((row) => ({
    route: row.route,
    id: row.id,
    regions: [...row.regions],
  }));
}

// files 相对 src/app。只认页面文件；app/api 下的路由处理器不是用户页面。
// 目录名原样进入路由，动态段保留方括号。
export function routesFromPageFiles(files: readonly string[]): string[] {
  const routes: string[] = [];
  for (const file of files) {
    const parts = file
      .replaceAll("\\", "/")
      .replace(/^\//, "")
      .split("/")
      .filter((part) => part.length > 0 && part !== ".");
    const base = parts.at(-1);
    if (!base || !PAGE_FILE.test(base)) continue;
    if (parts[0] === "api") continue;
    const dirs = parts.slice(0, -1);
    routes.push(dirs.length === 0 ? "/" : `/${dirs.join("/")}`);
  }
  return routes;
}

function counts(routes: readonly string[]): Map<string, number> {
  const out = new Map<string, number>();
  for (const route of routes) out.set(route, (out.get(route) ?? 0) + 1);
  return out;
}

// 缺页：磁盘有页面、账上没有。多页：账上有、磁盘没有。
// 同一路由不是恰好一条也失败。未知编号或漏区域失败。
export function accountPages(
  declarations: readonly PageDeclaration[],
  routesOnDisk: readonly string[],
): AccountProblem[] {
  const problems: AccountProblem[] = [];
  const declaredCount = counts(declarations.map((row) => row.route));
  const diskCount = counts(routesOnDisk);
  const declaredRoutes = [...declaredCount.keys()].sort();
  const diskRoutes = [...diskCount.keys()].sort();

  for (const route of declaredRoutes) {
    if ((declaredCount.get(route) ?? 0) !== 1) problems.push({ kind: "duplicate_route", route });
  }
  for (const route of diskRoutes) {
    if ((diskCount.get(route) ?? 0) !== 1) problems.push({ kind: "duplicate_route", route });
  }
  for (const route of diskRoutes) {
    if (!declaredCount.has(route)) problems.push({ kind: "missing_page", route });
  }
  for (const route of declaredRoutes) {
    if (!diskCount.has(route)) problems.push({ kind: "extra_page", route });
  }
  for (const row of declarations) {
    try {
      validatePageDeclaration(row);
    } catch (error) {
      if (error instanceof UnknownPatternError) {
        problems.push({ kind: "unknown_id", route: row.route, id: row.id });
        continue;
      }
      if (error instanceof MissingRegionError) {
        problems.push({
          kind: "missing_region",
          route: row.route,
          id: row.id,
          regions: [...error.regions],
        });
        continue;
      }
      throw error;
    }
  }
  return problems;
}
