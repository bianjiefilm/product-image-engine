import { sourceProxy } from "../source-image-runs/proxy";
type Ctx = { params: Promise<{ id: string }> };
export async function GET(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "capabilities"); }
