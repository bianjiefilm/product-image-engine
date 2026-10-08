# HUI-2627 finish-r1 回归复核记录(2026-10-08)

基线:10-04 Root 真实栈验收,readyz build 身份 8f3dd4c,四条验收 PASS。
本次复核对象:origin/main 8b231ac(= 8f3dd4c 之后合入 PR#46 尾部 + PR#47)。

> 接棒增补(2026-10-08 晚):本文写就时驱动尚未跑通,下文「动态断言在 finish-r1 驱动执行」各条现已在 fixture 栈真实执行并落盘:40 张矩阵(八面×五宽)+ 状态/红线/axe/去Logo 证据见本目录 README.md 与 browser/;四条 PASS 的动态对应 = `login-primary-action.json`(步骤一)、`result-card-chips.json`(figcaption 三 chip 原文)、`walkthrough-record.json`(矩阵 0 overflow、无旧版表单字段)、`hygiene-scan.json`(token 清零)。接棒代理逐条抽验通过。

## 提交漂移账目(8f3dd4c → 8b231ac)

| 提交 | 内容 | 触及四条 PASS 面? |
|---|---|---|
| e97b19b 及更早 | (已含在 8f3dd4c 验收构建内) | — |
| d2e0180 | PlateLockScreen.tsx 一行:生成占位文案仅在 `v.confirmed` 后显示「正在生成背景并检查商品……」,否则回落「结果会显示在这里……步骤没有增加。」 | 结果卡区域(PASS-2 相邻),属诚实性强化,非回归 |
| a0e9368 | 新增 BFF 只读代理 revisions/outcome、revisions/briefs(纯新增文件)+ hui-2596 driver 扩展 | 不改既有页面视觉/流程 |
| c798692 | revision content 代理透传 X-Revision-Sha256 头(可选,不存在时不设) | 无视觉/流程影响 |
| ca3c4ec / 841a903 / 8b231ac | e2e driver + 合并提交 | 非产品代码 |

## 四条 PASS 复核结论

1. **首图路径 4 步不增** — 静态:三个提交均未触及 first-image/start 步骤结构;测试层 `first-image*.test.ts`、`page-map`/`page-census` 全绿。动态断言在 finish-r1 驱动执行(stepper 恰 4 步、无旧版表单字段)。→ **PASS 保持**。
2. **结果卡状态 chip** — d2e0180 只改 stage-empty 占位分支,figure/figcaption chip 渲染路径未动;`plate-lock-screen` 相关测试全绿。动态断言(figcaption 三 chip)在驱动执行。→ **PASS 保持**。
3. **1440/390 核心流程** — 三个提交均未触及布局 CSS;40 张矩阵+390 单栏断言在驱动执行。→ **PASS 保持**。
4. **品牌族 token** — 无 globals.css/eco-nav 变更;清零扫描(hygiene test)机器断言。→ **PASS 保持**。

## 全量测试(本次实测)

`npx vitest run --pool=forks`:**38 文件 / 320 用例全过**(10-04 基线 37/316;增量为 a0e9368 自带 driver 测试)。无失败,无需修复。

## 结论

四条 PASS **零回归**;回归面三个提交均不削弱 10-04 结论,d2e0180 属诚实性强化。
