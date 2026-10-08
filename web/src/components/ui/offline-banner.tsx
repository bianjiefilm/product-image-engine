"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";

// HUI-2627 fix2:真实浏览器离线(第三层口径)的应用内提示。
// gate-r2 断网探针发现:离线后冷加载只能拿到浏览器错误页(SPA 无从渲染);
// 但「页面已加载后断网」是可以也应该由应用自己说话的场景——监听浏览器
// online/offline 事件,断网时给产品语句 + 重试探针,恢复后自动消失。
// initialOffline 仅供 SSR 快照与测试种子;运行时一律由事件驱动。
export function OfflineBanner({ initialOffline = false }: { initialOffline?: boolean }) {
  const [offline, setOffline] = useState(initialOffline);
  const [checking, setChecking] = useState(false);

  useEffect(() => {
    const update = () => setOffline(!navigator.onLine);
    update();
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);

  if (!offline) return null;

  async function retry() {
    setChecking(true);
    try {
      // 能拿到任何 HTTP 裁决(含 401)都说明网络已恢复;401 由各页面自行跳登录。
      const res = await fetch("/api/auth/session", { cache: "no-store" });
      if (res.status >= 200) setOffline(false);
    } catch {
      // 仍然不可达:横幅保留,继续等 online 事件。
    } finally {
      setChecking(false);
    }
  }

  return (
    <div className="offline-banner" role="status" data-state="offline">
      <span>
        网络已断开:已打开的页面还能看,但保存与生成会失败。恢复网络后这里会自动消失。
      </span>
      <Button type="button" size="sm" data-testid="offline-retry" disabled={checking} onClick={() => void retry()}>
        {checking ? "检查中…" : "重试"}
      </Button>
    </div>
  );
}
