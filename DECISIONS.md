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

## D13 清零扫描口径:裸 hex=0 硬断言,raw control/inline style 按基线防回潮
- 事实:实测 web/src 裸 hex=0;inline style 72 / raw button 45 / raw input 53 / raw table 14。全部清零=整面重写七个页,必触 HUI-2596 交互(禁区),超出本轮 P0;page-census markerBook 本身就是「稳定记账」而非清零口径。
- 拍板:tests/finish-hygiene.test.ts —— 裸 hex 必须 0;其余四类钉在 2026-10-08 基线「只许下降」;逐文件账目入 hygiene-scan.json。「无 table/form 主视觉」红线由驱动在真实渲染层把关(list 页允许 Work Pattern 数据表)。

## D14 驱动两处修复与 code-review 结果
- 修复 1:axe/blind 的 390 腿曾误用 1440 视口(batches blind 1440/390 截图哈希相同实锤)——驱动内切视口修复,复跑两连 0 fail。
- 修复 2:axe label(critical) 两处(RevisionRail 原图/遮罩 input、详情页 size-adapt 成果 input)——htmlFor/id 显式关联,TDD 先红后绿;HUI-2596 交互未动。
- code-review(双轴内联,任务书禁派孙代理):Standards 0 Major/3 Minor(ErrorBanner 显式 role 契约钉死、基线在测试与 JSON 双处出现、驱动与 walkthrough 驱动惯用式重复——均有既定理由);Spec 0 Major/1 partial(「1440 与 390 完成核心流程」中 390 全流程证据继承自 10-04 真实栈验收,fixture 层 390 覆盖为矩阵+单栏断言,PR 如实标注)。

---

# gate-r2 修复轮(2026-10-08,fix2,接棒续写)

2625 Finish Gate 首裁 product-image-web=fail,修复清单 5 条回流;业主不在线,以下继续自行拍板。

## D15 fix2 判读基线:210 是「route 级」计数,[id] 页 91 条 × Detail/Result 两角色
- 事实:复扫 62a0d70a 的 210 = login 8 + start 1×2 角色 + projects 18 + projects/[id] 91×2 角色;与 fix-list「projects/[id] 91 findings ×2 角色(inline 71 全仓大头)」一致。逐 rule:inline 71、unstyled 65、off_ds 50、raw_json 16、missing 6、hand_filled 2。
- 拍板:逐条修复一律以 route 级计数对账;复扫产物入 fix2 包(detector-rescan.json),不重算口径。

## D16 修复1 双语徽章:映射收进 lib,契约外枚举不裸奔
- 拍板:新建 web/src/lib/project-labels.ts(TDD 先红后绿):draft/active/archived → 草稿/进行中/已归档;standalone/order/campaign → 独立制作/挂接订单/挂接活动(与新建工程表单下拉文案同族);未知枚举给「状态未知/来源未知」产品语句,不回显英文。projects 列表状态/来源两列换 .pill 徽章(与批次「部分失败」同款);[id] 页来源 pill 与状态下拉 option 文案同步同族词汇(值不变)。盲评锚点只点了 projects 列表,[id] 页同族词汇一并收敛属同一修复面,已在 PR 注明。
- 遗留:[id] 页 versions 表的生成状态(adopted→已采用,其余裸英文)属 first-image STATUS_LABEL 词汇族,不在本条锚点内,记遗留不改。

## D17 修复3 inline 71:38 处页面行内样式全部「真改」,计算布局白名单记账 0 条
- 事实:逐条核对 38 处(login 1 / projects 4 / [id] 33),全部是 alignItems/flex/margin/gap 一类静态版式事实,**没有一处**是动态测量布局(width/height/top/left 随数据计算)——即没有一条符合 data-uifinish-computed-layout 白名单记账的前提。
- 拍板:全部真改为 globals.css 版式工具类(.row-center/.flex-0/.flex-2/.row-title/.mt-4/8/12/.mb-0/.m-0/.banner-action/.row-gap-8/.flex-none);不虚报记账、不虚报清零。工具类只承载既有行内样式的版式事实,颜色字重仍走语义 token;`.row > div` 特异性冲突以 `.row > .flex-0` 等显式战胜,渲染结果与改动前逐像素等价(flex:1/min-width:140px 行为保持)。
- 结果:inline 71 → 0(route 级);全 src inline_style 72 → 34。

## D18 修复3 raw_json 16:请求体序列化收敛到共享客户端,错误文案本身已合规
- 事实:16 条(7+7 同文件双角色 + login 1 + projects 1)全部是 `body: JSON.stringify(...)` **请求体构造**,不是用户可见的错误文案;页面错误面在 finish-r1 已是产品语句 + 恢复动作(ErrorBanner)。
- 拍板:新建 web/src/lib/api-client.ts(TDD):jsonBody/jsonHeaders 冻结导出;8 处请求点全部改走共享客户端。这是真实的结构收敛(单一序列化点、类型化出口),页面源不再内联 JSON 处理;不是把字符串拆开躲正则。
- 结果:raw_json 16 → 0(route 级)。

## D19 修复5 三态声明:按现行 detector 口径在页面源补字面量,载体全部是真实分支
- 拍板:6/6 路由(4 个页面源)补 data-state="loading|empty|error" 静态声明,一律挂在真实状态分支上,不做无中生有的空壳:
  - projects:加载中/还没有工程/ErrorBanner 三分支(真实条件渲染);
  - login:busy 时渲染的「登录中…」span(loading)、stage-empty 空台(empty)、错误 banner(error);
  - [id]:loadError 早退分支(error)、!project 加载分支(loading)、「尚未挂接素材」空分支(empty);
  - start:Suspense 回退(loading)、新增 SurfaceErrorBoundary 回退(error,产品语句+返回入口)、工作台壳声明 empty(StartScreen 的候选空台 stage-empty 常驻渲染,页面注释如实写明这一语义)。
- 配套:tests/surface-states.test.ts 以 detector 同口径正则防回潮;改判读口径归 2619,本票不预判。

## D20 修复4 offline:三层口径说明 + 补真实浏览器离线的应用内 UI
- 拍板:不满足于「写说明」——新增 OfflineBanner(根 layout 挂载)覆盖「已加载后真断网」层:浏览器 online/offline 事件驱动、重试为真实 fetch 探针、恢复自动消失,零伪造;真实 Chrome setOffline 证据入包。三层口径(① fixture 路由层 ② 真断网会话中 ③ 真断网冷加载=浏览器错误页,SPA 物理边界)写入 fix2/offline-two-layers.md;③ 层不做 Service Worker(新功能,留票)。

## D21 修复2 触控账目:探针入仓,实测 0 不达标 + 3 行内例外
- 拍板:探针 e2e/hui-2627-fix2.mjs 入仓(对标 2626 touch-targets.json 形态:viewport 键 + label/tag/cls/w/h,增列 surface/pass 便于对账),六代表面 × {430,390} 全量 100 控件/腿。首轮实测 21 处 <44px,逐处修尺寸:壳导航按钮(eco-nav menuBtn/switchBtn 44×32)以 globals.css ≤430 兜底提升到 44(2626 同款做法,不改共享组件源);.row 头部链接加水平命中补足(批量 28→44+);表格行主链接(工程名)在移动卡片版式下提升为 44px 命中行。余 3 处段落内文字链接(去开始做产品图/已有制作工程/接受跨应用交接)按 WCAG 2.5.8 行内例外记账——其中 1 处在 RevisionRail 内,纪律不动其源文件,CSS 亦不越权改它的版式。
- 结果:@430/@390 均 0 fail + 3 inline-exception;账目 touch-targets.json + 12 张走查截图入包。

## D22 剩余 117 findings 记账:off_ds 50 + unstyled 65 + hand_filled 2,维持 D13 结构基线
- 事实:消化后复扫 210 → 117(Δ-93);剩余三类不动的理由:D13 已裁决「raw 控件全清零=整面重写、必触 HUI-2596 禁区」,本轮无权推翻;hand_filled 2 条是 `sourceTenantId: null` 字段名正则误报(值恒为 null,字段名属 eco-nav workbench 契约,改名牵动共享壳)。
- 拍板:剩余按 D13 记账原样保留,fix2 README 逐 rule 对账;防回潮测试(census markerBook + finish-hygiene)随真实页面收缩同步收紧:markerBook 三条路由的 inline style 标记移除、inline_style 基线 72→34、raw_button 维持 45(新组件一律走设计系统 Button,OfflineBanner 即如此;login 的 loading 声明用条件 span 不加第二枚按钮)。账目:fix2/hygiene-scan.json(与 finish-r1 同格式对照)。

## D23 证据与验证账目
- vitest:基线 41 文件/333 用例 → fix2 46 文件/349 用例全绿(新增 project-labels 4、api-client 2、surface-error-boundary 3、ui-offline-banner 3、surface-states 4;0 破坏)。next build exit 0。
- 证据包 docs/audits/hui-2627/fix2/:双语徽章断言+1440/390 截图(before 引 finish-r1 包 finish-blind-projects-390.png,不推翻原证据)、touch-targets.json+12 截图、state-offline-browser.json+截图、hygiene-scan.json、detector-rescan.json、offline-two-layers.md、fix2-run-record.json、SHA256SUMS。
- fixture/端口:沿用 32321(web)/32324(fixture-api)惯例;零第三方付费;真实 Chrome(headless)。
- HUI-2596 交互、RevisionRail、HUI-2622、D2 全栈、Motion Consumer、Redis:均未触碰(grep 复核 diff 文件清单)。
