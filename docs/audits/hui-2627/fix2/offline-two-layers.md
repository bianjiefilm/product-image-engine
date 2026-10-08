# Offline 三层口径说明(HUI-2627 fix2,回应 2625 gate-r2 unknowns)

2625 gate-r2 对 product-image-web 的 offline 态留了一条 unknown:「gate-r2 断网探针
(真实浏览器离线)渲染浏览器错误页;2627 包内 offline 为应用内态(fixture 网络层);
两者口径差异需 2627 说明」。本页把三层口径写清,并补上缺失的那一层。

## 三层口径

| 层 | 触发方式 | 行为 | 证据 |
| --- | --- | --- | --- |
| ① 应用内网络故障(fixture 路由层) | finish-r1 驱动 `page.route("**/api/**") abort` —— 页面已加载,产品 API 不可达 | 页面渲染诚实错误态(ErrorBanner:产品语句 + 重试按钮),不裸奔后端字符串 | finish-r1 `state-offline.json` + `finish-state-offline-1440.png`(62a0d70a 包内,未被本轮推翻) |
| ② 真实浏览器离线 · 会话中断网(第三层,fix2 新补) | 真实 Chrome `context.setOffline(true)`,页面已加载 | 应用监听浏览器 `offline/online` 事件,底部浮出 `OfflineBanner`(role=status):「网络已断开:已打开的页面还能看,但保存与生成会失败。恢复网络后这里会自动消失。」+ 重试探针;网络恢复自动消失 | fix2 `state-offline-browser.json` + `browser/fix2-state-offline-browser-1440.png` |
| ③ 真实浏览器离线 · 冷加载 | 断网后直接打开 URL(document 都拿不到) | 浏览器自己的错误页(Chrome dinosaur 一类)。**任何 SPA/服务端渲染应用都不可能在这一层渲染应用内 UI**——HTML/JS 本身就在拿不到的网络对端。行业常规做法是 PRA/Service Worker 离线缓存壳,属新功能不在本轮修复范围 | gate-r2 断网探针所见即本层 |

## 为什么 ② 之前缺、现在补

gate-r2 探针走的是 ③(先断网再冷加载),而 finish-r1 的证据是 ①(route-abort 模拟
产品服务不可达)。两层都真实,但中间缺了「已加载后真断网」的 ②——它不需要 mock
路由层,直接由浏览器网络栈触发,是用户在地铁/电梯里真实遇到的场景。

fix2 的 `OfflineBanner`(web/src/components/ui/offline-banner.tsx)挂在根 layout:
- 只读浏览器 `online/offline` 事件与 `navigator.onLine`,不发明网络探测假象;
- 重试按钮做一次真实 fetch 探针,拿到任何 HTTP 裁决(含 401)才收横幅;
- 拿不到就保留横幅,等 `online` 事件自动消失——不伪造「已恢复」。

## 残留与边界

- ③ 冷加载层维持浏览器原生错误页,本轮不做 Service Worker(新功能,且与 2619
  终审的 detector 口径议题无关);如业主要求 PWA 离线壳,另开票。
- OfflineBanner 是全局壳层组件,不影响任何页面的既有交互(HUI-2596 /
  RevisionRail / eco-nav 均未触碰)。
