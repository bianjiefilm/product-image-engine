import { sourceProxy } from "../proxy";
type Ctx = { params: Promise<{ id: string; runId: string }> };
export async function GET(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "get"); }
