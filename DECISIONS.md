# DECISIONS — HUI-2627 finish-r1(2026-10-08)

业主全程不在线,以下按任务书裁决预设自行拍板,逐条记录理由。

## D1 证据层 = 本机 fixture 栈,如实标注 `fixture`
- 事实:10-04 验收用的远程 QA 栈(120.26.69.202)SSH 客户端配置(`/Users/jimfu/.codex/qa-private-nine-projects-20261001/`)在本机不存在,`/tmp/painuo-pie-accept` 亦无历史存储态;真实栈复走不可行。
- 拍板:走查在本机 fixture 栈(fixture-api.mjs 替身 Go API + next start),一切浏览器证据标注 `fixture` 层;10-04 真实栈结论按「复用」引用,不冒充本次复走。真实栈复走记 NOT_RUN。
- 依据:任务书「第三方付费 API 一律 mock(不花钱)」「证据四层」要求。

## D2 不复现完整 Go server 本地栈
- 事实:完整栈需 platform-identity/upload/task/billing 四个平台服务;本机 18101-18104 全未监听,public-ai 仓的 identity 服务需独立用户库/密钥运维,且 identity mock 的 JWT/JWKS 造伪不属于本票范围。
- 拍板:web BFF 直指 fixture-api(既有 fixture-run.mjs 既有路径),不为走查新建平台服务 mock。

## D3 fixture-api.mjs 扩展缺口端点(TDD)
- 事实:fixture 现有面只覆盖 plate-lock 流;八代表页中 /projects 列表、/projects/[id] revision 轨、/batches、/handoff 的上游端点缺失,页面会落错误态(那本身是有效 error 态证据,但 success/partial 态无法到达)。
- 拍板:按真实 Go 契约形状补只读/零扣费端点(projects 列表、revisions 状态/plan/outcome/briefs/compare/rollback/downstream、batches 列表、handoffs),测试先行(web/tests/fixture-api.test.ts)。downstream 一律 `requires_reupload:false` + 本地 `asset_ref`,不发明「已上传」字段。

## D4 axe-core 经 npm 代理安装为 devDependency
- 事实:仓内无 axe 集成;2625 门要求 a11y 扫描。
- 拍板:`npm i -D axe-core`(走 127.0.0.1:7890 代理),驱动注入执行;serious/critical 违例必须 0,否则修复或书面豁免。

## D5 证据入 git(docs/audits/hui-2627/finish-r1/)
- 事实:任务书指定「存 worktree docs/audits/hui-2627/finish-r1/」。
- 拍板:截图/JSON/SHA256SUMS 全部入 PR;矩阵 8 页 × 5 宽 = 40 张 + 状态/去Logo/比较附加张,总账记 README。

## D6 四条 PASS 不推翻重做,回归面聚焦三个提交
- 事实:10-04 验收构建 8f3dd4c 之后 origin/main 仅三个 web 代码提交(d2e0180/a0e9368/c798692),其余为 e2e/docs。
- 拍板:静态 diff 精读 + 全量测试 + fixture 走查断言同口径复核;不复刻真实栈全流程。

## D7 端口
- 默认 web 32321 / fixture-api 32324(fixture-run 惯例);被占则 E2E_WEB_PORT/E2E_WEB_PORT 对应 29321/29324。JWT 30min 惯例在本 fixture 栈不适用(fixed fixture-access cookie),如实记录。

## D8 交付纪律
- push 分支 + gh pr create 到 main;不合并不改票不发评论;PR 描述含 NOT_RUN 账目与「2625 门未开,不标 Done」。

---

# 接棒段(2026-10-08,前任限流身故后由接棒代理续写)

## D9 WIP 全部保留续完,不丢弃
- 事实:前任 WIP 共 13 个文件,逐文件读懂后判定高度连贯且与 D3/D4/Task 4 一致:
  - fixture-api.mjs + fixture-api.test.ts:revision_off 场景 + 诚实 downstream/plan 字段(Task 3);
  - axe-core devDependency + lock(D4);lock 增量中 optional deps 的 `libc` 字段被本机 npm 修剪,registry 仍 npmmirror 与仓内既有 158 处一致,无 CI 风险;
  - login/handoff/batches/projects(列表+详情)五页 label htmlFor 关联(axe label 修复);
  - globals.css `p .link` 下划线(a11y link-in-text-block);
  - 新组件 ui/error-banner.tsx + ui-error-banner.test.tsx + projects 列表页接入(offline 诚实文案+重试,Task 4 Step 4 的 error/offline 恢复动作);
  - e2e/hui-2627-finish-r1.mjs 387 行驱动(Task 4,选择器已逐一对照 PlateLockScreen/PhotoUpload/RevisionRail/EcoTopNav 核实存在)。
- 拍板:全部保留,补齐后提交;无丢弃项。

## D10 回归复核「动态断言」由本次驱动执行兑现
- 事实:前任 regression-review.md 写「动态断言在 finish-r1 驱动执行」,但驱动当时未跑、审计目录无任何截图/JSON 证据——按任务书「证据文件实际存在才能引用」,该半句在接棒时**不成立**。
- 拍板:静态 diff 精读部分(d2e0180/a0e9368/c798692)已抽验与前任描述一致,沿用;动态部分(四步 stepper、figcaption 三 chip、40 张矩阵、390 单栏、token 清零)由接棒后真实驱动跑出的证据兑现,regression-review.md 增补一行指向证据文件。

## D11 测试基线更新
- 接棒时全量 `npx vitest run --pool=forks`:**40 文件 / 330 用例全过**(前任记录 38/320 → +ui-error-banner.test.tsx 2 例 + revision_off 1 例 + 中间增量)。四条 PASS 测试面全绿,无回归。

## D12 驱动缺口补齐清单(接棒判定)
- 驱动已含:八页×五宽矩阵、login 主操作置顶、首图四步、结果卡三 chip、2596 采用/回退、诚实 downstream 文案、empty/disabled/revision_off/loading/offline/not_usable 状态、axe(1440+390)、去Logo 品牌族材料。
- 接棒补齐:逐项核对后驱动不需要结构性改动;若跑动中出现 selector/时序失配,按最小改动修复并在此记录。
