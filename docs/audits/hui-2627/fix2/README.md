# HUI-2627 fix2 · gate-r2 修复轮证据包(2026-10-08)

分支 `codex/20261008-hui2627-fix2`(基于 origin/main 62a0d70a,含 finish-r1)。
对应 2625 gate-r2 fix-lists.md「→ HUI-2627」5 条;逐条状态、判读与拍板见仓根
`DECISIONS.md` D15–D23。裁决权在 2625 门;本包只交修复与证据。

## 0. 结果速览

| # | 修复项 | 状态 | 结果 |
| --- | --- | --- | --- |
| 1 | 状态词汇双语统一(blind fail) | **done** | 裸英文枚举清零,中文徽章(.pill 同族) |
| 2 | 触控目标账目(a11y fail) | **done** | @430/@390 各 100 控件,0 不达标 + 3 行内例外(2.5.8) |
| 3 | detector 大头(inline 71) | **done** | inline 71→0、raw_json 16→0;其余按 D13 记账 |
| 4 | offline 口径 | **done** | 三层口径说明 + 补真实浏览器离线 UI(OfflineBanner) |
| 5 | missing_loading_empty_error 6/6 | **done** | 6/6 路由页面源三态声明,detector 判 pass |

detector 复扫(route 级):**210 → 117(Δ-93)**;剩余 117 = off_design_system 50 +
unstyled_native_control 65 + hand_filled 2,逐类记账见 §3(维持 D13 结构基线,
不虚报清零)。

## 1. 逐条对照(对应 fix-lists.md → HUI-2627)

### 1. 状态词汇双语统一
- 修复:`web/src/lib/project-labels.ts`(TDD)把 draft/active/archived、
  standalone/order/campaign 映射为与新建表单、批次「部分失败」同族的中文徽章;
  projects 列表状态/来源两列改 `.pill` 徽章;[id] 页来源 pill 与状态下拉 option
  同族收敛(值不变);未知枚举给「状态未知/来源未知」,不回显英文。
- 证据:`badges-projects.json`(断言:无裸枚举 + 有中文徽章)、
  `browser/fix2-badges-projects-1440.png`(进行中/独立制作 徽章可见)、
  `browser/fix2-badges-projects-390.png`。before 对照:finish-r1 包
  `browser/finish-blind-projects-390.png`(裸英文,原证据不推翻、直接引用)。

### 2. 触控目标账目
- 修复:探针 `web/e2e/hui-2627-fix2.mjs` 入仓(2626 touch-targets.json 同形态);
  壳导航按钮与行内/表格主链接以 globals.css ≤430 媒体查询提到 ≥44px。
- 实测:六代表面(login/start/projects/project-detail/batches/handoff)×
  {430,390} 全量 100 控件每腿:**0 低于 44px**,3 处段落内文字链接记 WCAG 2.5.8
  行内例外(其中 1 处在 RevisionRail 内,纪律不动)。
- 证据:`touch-targets.json`(每控件 label/tag/cls/w/h/pass)、
  `browser/fix2-touch-targets-*-{430,390}.png` 12 张、`fix2-run-record.json`。

### 3. detector 大头(inline 71 等)
- inline_style 71 → **0**:38 处页面行内样式全部真改为 globals.css 版式工具类;
  没有一处是动态测量布局,故 data-uifinish-computed-layout 白名单记账 0 条、
  不虚报(D17)。
- raw_json_or_http_error 16 → **0**:请求体序列化收敛到共享 `lib/api-client.ts`
  (TDD);页面错误面文案本就是产品语句(D18)。
- unstyled_native_control 65、off_design_system_table_form_button 50、
  hand_filled 2:**维持 D13 结构基线记账**(全清零=整面重写,触 HUI-2596 禁区;
  hand_filled 为 `sourceTenantId: null` 字段名误报)。
- 防回潮:census markerBook 与 finish-hygiene 基线随真实页面同步收紧
  (inline_style 72→34 只许下降);`hygiene-scan.json` 与 finish-r1 同格式对照。

### 4. offline 口径
- 修复:新增 `OfflineBanner`(根 layout 挂载)——浏览器 online/offline 事件驱动、
  重试为真实 fetch 探针、恢复自动消失;补上「已加载后真断网」这一缺失层。
- 说明:三层口径(fixture 路由层 / 真断网会话中 / 真断网冷加载=浏览器错误页的
  SPA 物理边界)见 `offline-two-layers.md`。
- 证据:`state-offline-browser.json`(横幅出现→恢复消失)、
  `browser/fix2-state-offline-browser-1440.png`;① 层证据引 finish-r1 包
  `state-offline.json`(route-abort 层,原证据不推翻)。

### 5. missing_loading_empty_error 6/6 路由
- 修复:4 个页面源全部补 data-state="loading|empty|error" 静态声明,载体全部是
  真实状态分支(projects 三分支、login busy span/stage-empty/banner、[id]
  loadError/!project/尚未挂接素材、start Suspense 回退/新增
  SurfaceErrorBoundary/候选空台语义声明,页面源注释写明)。
- 结果:复扫 6/6 路由该 rule **0 findings**(/start 两条路由整体转 pass)。
- 防回潮:`tests/surface-states.test.ts` 以 detector 同口径正则钉住 4 页三态字面量。
- 改判读口径议题归 2619 终审,本票未动口径。

## 2. detector 复扫 delta(route 级,cmd/uifinish-scan 实析本分支)

| rule | 62a0d70a | fix2 | Δ | 处置 |
| --- | --- | --- | --- | --- |
| inline_style | 71 | 0 | -71 | 真改(D17) |
| raw_json_or_http_error | 16 | 0 | -16 | 共享客户端收敛(D18) |
| missing_loading_empty_error | 6 | 0 | -6 | 三态静态声明(D19) |
| off_design_system_table_form_button | 50 | 50 | 0 | D13 记账(D22) |
| unstyled_native_control | 65 | 65 | 0 | D13 记账(D22) |
| hand_filled_tenant_or_internal_id | 2 | 2 | 0 | 字段名误报记账(D22) |
| **合计** | **210** | **117** | **-93** | |

明细:`detector-rescan.json`(uifinish-route-scan/v1 原生产物)。

## 3. 测试与构建

- vitest:46 文件 / **349 用例全绿**(修复前基线 41/333,只增不破;新增
  project-labels 4、api-client 2、surface-error-boundary 3、ui-offline-banner 3、
  surface-states 4)。
- `npx next build` exit 0。
- 纪律:HUI-2596 交互、RevisionRail、HUI-2622、D2 全栈、Motion Consumer、Redis
  零触碰;fixture 栈端口 32321/32324;零第三方付费。

## 4. 文件清单

- 断言/账目:badges-projects.json、touch-targets.json、state-offline-browser.json、
  hygiene-scan.json、detector-rescan.json、fix2-run-record.json
- 文档:offline-two-layers.md、本 README
- 截图:browser/(16 张;`fix2-project-detail-1440.png` 为行内样式收敛后的桌面
  等价性证据:flex-0/flex-2 按钮组、成果登记右对齐按钮、尺寸适配版式与
  finish-r1 包 finish-matrix-detail-1440.png 同构)
- 校验:SHA256SUMS
- 代码:web/src(lib/project-labels、lib/api-client、components/ui/{offline-banner,
  surface-error-boundary}、路由页 4 文件、globals.css、layout.tsx、page-census
  markerBook)+ web/tests(5 个新测试 + 基线记账更新)+ web/e2e/hui-2627-fix2.mjs
