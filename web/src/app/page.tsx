import { redirect } from "next/navigation";
import { cookies } from "next/headers";
import { loggedInHome } from "@/lib/first-image";
import { ACCESS_COOKIE, callServer } from "@/lib/server";

// 首页:已登录 → 开始做产品图;未登录 → 登录页。
export default async function Home() {
  const jar = await cookies();
  const access = jar.get(ACCESS_COOKIE)?.value;
  if (access) {
    try {
      await callServer("/api/v1/auth/session", {
        accessToken: access,
      });
      redirect(loggedInHome());
    } catch {
      // 会话失效则落登录页
    }
  }
  redirect("/login");
}
