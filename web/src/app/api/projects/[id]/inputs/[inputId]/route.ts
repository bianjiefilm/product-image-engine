import { NextRequest, NextResponse } from "next/server";
import {
  accessTokenOf,
  callServer,
  toErrorResponse,
  UpstreamError,
} from "@/lib/server";

type Ctx = { params: Promise<{ id: string; inputId: string }> };

// DELETE /api/projects/[id]/inputs/[inputId] — 移除素材引用。
export async function DELETE(req: NextRequest, ctx: Ctx) {
  try {
    const access = accessTokenOf(req);
    if (!access) throw new UpstreamError(401, "unauthenticated", "未登录");
    const { id, inputId } = await ctx.params;
    await callServer(`/api/v1/projects/${id}/inputs/${inputId}`, {
      method: "DELETE",
      accessToken: access,
    });
    return new NextResponse(null, { status: 204 });
  } catch (e) {
    return toErrorResponse(e);
  }
}
