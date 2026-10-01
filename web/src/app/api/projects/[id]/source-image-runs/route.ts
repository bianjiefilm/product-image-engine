import { sourceProxy } from "./proxy";
type Ctx = { params: Promise<{ id: string }> };
export async function GET(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "list"); }
export async function POST(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "list"); }
