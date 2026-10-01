import { sourceProxy } from "../../proxy";
type Ctx = { params: Promise<{ id: string; runId: string; action: string }> };
export async function GET(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "action"); }
export async function POST(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "action"); }
export async function DELETE(req: Request, ctx: Ctx) { return sourceProxy(req, await ctx.params, "action"); }
