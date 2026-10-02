# HUI-2232 产品图工程质量 Gate（C2 轮）：限定保真 `background_plate_lock` 的真实闭环设计

状态：2026-10-03 修订稿（Rev2）。在 2026-10-02 设计稿（提交 `18b17f9`）与同日实施计划（`c83d779`）之上修订，保留既有 Root 裁定 R1–R21，只做精确化与补充（见 §0.1）；全部决定由 Root 章程（`.claude-orchestration/20261002/CHARTER.md` §1）自答并留痕；未授权实现，Root 另行编排 writing-plans。
基线：`claude/integration-20261002` = `origin/main` `bf4ec7c7d8fd08576a8b3ac314798209fd355b90`（PR #38 合入后；2026-10-03 复核，集成分支未前进）。分支 `claude/hui-2232-quality-gate`，worktree `product-image-engine/.worktrees/hui-2232-c2`，merge-base 恰为 `bf4ec7c`，领先 2 个仅文档提交，工作区干净，端口 32320–32329 无监听。
基线实测：server `go test -p 2 -count=1 ./...` 27 包 ok；web `npm test` 26 文件 201 测试、`tsc --noEmit` 均 0 退出（证据 `RUN/evidence/HUI-2232/baseline-*.log`）。2026-10-03 复跑定向包（sourceimage、sourceflow、bgreplace、fidelity、qualitygate、httpapi、cmd/server 均 `ok`）与 web（201/201、tsc 通过），证据 `RUN/evidence/HUI-2232/intake2-*.log`（该日志的退出码字段未被捕获，以逐包 `ok` 行与测试计数为准）；worktree 内 `GOWORK=off go build ./cmd/server` 成功。两个额外提交只含文档，被测代码与 `bf4ec7c` 相同。无既有失败。
本稿只新增/修订文档，不改产品代码、不起服务、不调供应商、不产生真实费用。

## 0. 与已批准文档的关系（修订而非推倒重来）

| 已有文档 | 本稿处理 |
|---|---|
| `2026-10-01-hui-2232-source-quality-gate-design.md`（Root 已批准方案 A、D1 个人先闭环/组织无 E7 则关闭、D2 归档保留引用与明确删除 outbox、D3 三主体冻结输入/逐轴质量/真人 NOT_RUN） | 全部保留。普通 `text_generate` 段已由 PR #38 落地。本稿实施其“第二段：主体锁定底板复合模式”，并改动其 §9 第 2 行的付费路径（见下表与 R5）。 |
| `2026-10-01-hui-2232-limited-fidelity.md`（Tasks 1–3 已合入；Task 4 草案已删除、不可执行） | 其“平台 Task 拥有预占/结算、本地覆盖验证、本地锁回主体”的原则保留；其 bg-replacements job + `bg_plate_executions` + 旧 `task.submit` 的承载方式由本稿的 source run 模式取代。 |
| 已合入基线：#34 像素锁、#35 模型底板+锁、#36 冻结执行身份/CAS/覆盖验证器、#37 未计费 claim、#26 恒不通过 recorder | 仅当基线与可复用组件，不当本票通过证据。 |

本稿对旧文的具体改动：

| 旧文位置 | 旧说法 | 本稿 |
|---|---|---|
| source-quality §9 行 2 | `background_plate_lock` 需要“正式复合 Task”：平台在 capture 前完成底板+确定性合成并 durable | 底板走**已存在的** `text_generate` source Task（同一 Bill 报价/hold/charge、同一 durable Upload）；合成在产品侧做，产出标为“派生输出”，引用底板 Task/Asset/输入与蒙版哈希（R5） |
| source-quality §9 报告名 | 新增 `product-source-quality/v1` | v1 已随 #38 为 `text_generate` 发布并被测试冻结；per-mode 证据报告用 `product-source-quality/v2`（R9） |
| limited-fidelity | `POST .../background-replacements/{job}/model-plate` | 该路由因装载器上线会变得可达而又不经 Bill，故**关闭**（R6），能力改由 `source-image-runs` 的 `background_plate_lock` 模式提供 |

另有两份文档是本稿的直接前身：`2026-10-02-hui-2232-quality-gate-design.md` 的 Rev1（提交 `18b17f9`，本文件的前一版）与 `docs/superpowers/plans/2026-10-02-hui-2232-quality-gate.md`（`c83d779`，六个任务；设计门后 Rev3 重组为四个任务，见 R34）。Rev1 的 R1–R21 全部保留；计划阶段对 Rev1 的 8 处精确化/偏离（计划里记作 P1–P8）在 Rev2 并入本稿并重编号为 R22–R29，使 spec 与 plan 不再互相矛盾。

### 0.1 Rev2 修订摘要（相对 Rev1）

| 编号 | 变化 | 落点 |
|---|---|---|
| R22 | 派生不再经 Upload 读原图，改用冻结样本集里的原图/蒙版字节；`original_unavailable` 终态取消，换成 `sample_set_changed` | §4.1、§4.4、F17、§9 |
| R23 | 派生记录写一次、不可变；`selected/returned_output_id/revision` 不进派生表 | §4.4、§4.7 |
| R24 | 迁移拆成 `0020`（冻结输入 + 派生）与 `0021`（回流质量绑定） | §4.4、§4.7 |
| R25 | `supported_input_ids` 只读本地照片登记，GET 不访问远端、不写库 | §4.3 |
| R26 | 构建身份必须显式注入（worktree 里 Go 会盖错 VCS 戳，已实测） | §4.5、§4.12 |
| R27 | 浏览器一律用 `http://localhost:<port>`（BFF 同源校验） | §4.8、§4.12 |
| R28 | 画布适配的确定性按“同构建同架构”承诺，已存派生记录才是权威 | §4.4、F18 |
| R29 | `/start` 个人路径收敛为一条付费主路径；文字生成为折叠的次要入口 | §4.8 |
| R30 | 新增：每个样本冻结的背景方向须与样本自身背景显著反差，避免合法背景被阈值误判 | §4.2 |
| R31 | 新增：边角输入与恢复要求（原计划 Review Focus 五条）提升为 spec 要求 | §4.12 |
| R32 | 新增：`blocked` 终态与 `pending` 两义的澄清 | §4.5 |
| R33 | 新增：工程环境约束（`GOWORK=off`、缓存、端口、重任务节流、浏览器二进制）写入 spec | §4.12 |
| R34 | 设计门后：计划任务数由 6 合并为 4（每个任务内保留可独立提交的部分 1A/1B、2A/2B） | §5 |
| R35 | 设计门后：verdict 推导按层（fidelity / generation / export / billing_recovery / cross_app 等）从 run-record 真实字段推导，`cross_app` 为 `NOT_RUN` 时硬封顶 `PARTIAL` | §8、§4.9 |
| R36 | 设计门后：M3 第四个探针如实命名为“新浏览器上下文复用同一份登录态”，不称“退出重登” | §4.9 |
| R37 | 设计门后：R30 用 samplegen 测试钉死（方向代表色与样本底色 RGB 距离 >= 100）；M2 驱动选择器与现有按钮文案一致 | §4.2、§4.9 |

## 1. 意图回写（我对需求的理解，供 Root 纠正）

要什么：给“限定保真换背景”建立一套可复验的工程质量 Gate——冻结授权样本与阈值、运行时装载、正式计费链路、服务端正向质量裁决（PASS/FAIL/UNKNOWN/N/A 分开）、固定反例矩阵、正常 UI 全路径与恢复、订单/Campaign 受限回流、真实付费隔离验收、其它模式如实收口。
为谁：产品负责人/Root 据此决定“哪个模式、哪个模型、哪种样本条件”可称已验证；最终用户经正常 UI 得到可信、可恢复、费用可解释的结果。
成功：票内 8 条原通过条件与 12 条剩余范围逐条有“刚在本 worktree 跑出来”的证据，或被如实标 UNMET/NEEDS_*（见 §11、§12）。
我的假设（均已在 §13 自答）：A1 “限定保真”只对冻结样本集成立，任意用户照片不宣称保真；A2 合成样本是授权测试商品，不是实物照片；A3 QA 栈、测试账号、额度、定价版本由 Root 经环境变量引用供给；A4 无真人，需“人判”处一律 SIMULATED-HUMAN 且不进入裁决；A5 不改任何公共合同（order-receipt/v1、platform SDK、public-ai）。

## 2. 基线事实与缺口（均已读码/读台账核实）

| # | 事实 | 锚点 | 对设计的约束 |
|---|---|---|---|
| G1 | 限定保真旧路径直调图像模型，仅产品内 claim；无平台 Task、无 Bill hold/结算 | `httpapi/bgreplace.go` `handleModelPlate`→`imagemodel.Generate` | 不能让它因装载器上线而可达（R6） |
| G2 | `Server.PlateCoverage` 在 `cmd/server` 从未赋值，`model-plate` 恒 422 `coverage_rejected` | `httpapi/server.go:56` | 需运行时装载冻结样本；装载后旧路由若仍存在即成“无计费直调”漏洞 |
| G3 | 仓库内无冻结授权样本；`AcceptanceSamples()` 是两条占位；真实 carton/glass/handled-metal 与蒙版只在私有 QA 目录（章程禁读） | `bgreplace/bgreplace.go:276` | 需在仓库内生成可复验的合成样本（R3） |
| G4 | source Task 链路已含 usage/quote/确认/hold/charge/durable Asset/outbox，但 `Fingerprint()` 只接受 `text_generate`+1024*1024+qwen-image-2.0+n=1 | `sourceimage/validate.go` | 底板复用这条链，须扩展 mode 而不改 provider/价格画像 |
| G5 | `Intent` 是可比较 struct（多处 `!=`），其 JSON 参与指纹；旧行 JSON 必须字节稳定 | `sourceimage/types.go`、`sourceflow/output.go` | 新增字段只能是 `omitempty` 字符串；冻结输入放同事务不可变侧记录并用摘要绑定 |
| G6 | `SubjectPixelChecks` 在“蒙版内像素 0 差”时把 logo/text/spec/structure 全标 pass，不核对蒙版是否覆盖这些区域 | `bgreplace/lock.go:149` | 只有在冻结覆盖验证通过的前提下才可采信（本稿裁决函数强制） |
| G7 | `FitPlate` 把底板按最近邻**拉伸**到原图尺寸（非等比） | `bgreplace/lock.go:85` | 三种画布均须等比覆盖裁切（R8） |
| G8 | 回执文档 `order-receipt/v1` 冻结，字段无质量/限制；`BlocksVerifiedReceipt` 在“无报告”时放行 | `receiptdoc/receiptdoc.go`、`fidelity/fidelity.go:268` | 限制不能写进回执（R12）；引擎产出须有服务端质量绑定 |
| G9 | source 输出已被 `HasVerifiedSourceOutput` 挡在通用 `/outputs` 之外，因此目前**没有**从 source/底板输出到订单回流的路径 | `httpapi/handoff.go:542+` | 回流需要服务端发起的“登记为工程输出”动作（§4.7） |
| G10 | `qualitygate`、`engwalk` 包无任何生产引用（只被自身测试引用） | `grep` 无结果 | 不修改；新 Gate 独立，不用“恒不通过”recorder 造成功 |
| G11 | 前端无 Playwright 依赖、无 Painuo Token（HUI-2621 尚未进集成分支）；`/start` 的“选择商品照片”按钮 `disabled`；页面含“商品照片编号”“Task”“NOT_RUN”等开发者措辞 | `web/src/components/first-image/StartScreen.tsx:69`、`app/start/ui.tsx`、`components/source-image/Panel.tsx` | UI 需实现真实上传、人话文案、Playwright 取证（§4.8） |
| G12 | SDK 固定 v0.1.1；v0.1.1 已含 `upload.durable_*`、`task.source_*`、`billing.usage*/source_quote`；v0.1.3 仅增 `task.source_execution`、`identity.personal_principal.query` | `public-ai` 集成 worktree `operations.json`（只读） | 本稿所需操作 v0.1.1 全部具备，不升 SDK（R16） |
| G13 | 组织付款无真实 E7 委托与 `org_payers` 生产者 | 既有 Root 台账 | 组织路径保持默认关闭、组织 Gate 未完成 |
| G14 | 上轮 QA 栈在 32950–32958 监听，不属本票 | 任务描述 | 只经 Root 提供的环境变量引用连接，不触碰其进程 |
| G15 | 在本 worktree 里 `go build` 出的二进制 `vcs.revision` 是**主仓** HEAD（`6219124…`，`vcs.modified=true`），不是 worktree HEAD（2026-10-03 实测：worktree HEAD `c83d779`） | `go version -m` 输出 | 构建身份不能只信工具链戳，必须显式注入（R26） |
| G16 | `/Volumes/MobileHD/Work/派诺生态/go.work` 存在，会让不在 workspace 内的 `go build/test` 报错 | 文件存在 | 所有 Go 命令 `GOWORK=off`（R33） |
| G17 | source BFF 的变更请求用 `Origin` 对比 Next 重建的 URL origin（host 恒为 `localhost`），`127.0.0.1` 会得到 403 | `web/src/app/api/projects/[id]/source-image-runs/proxy.ts:16` | 浏览器取证只用 `http://localhost:<port>`（R27） |
| G18 | 本机无 `~/Library/Caches/ms-playwright` 浏览器缓存，但已装 Google Chrome 与 Microsoft Edge；`playwright-core@1.63.0` 在 npm 存在 | 实测 | 复用本机 Chrome 二进制 + 独立 profile，不下载浏览器（R13/R33） |

## 3. 方案比较（付费与产出承载路径，R5）

| 方案 | 做法 | 评价 |
|---|---|---|
| **P（推荐）** | 底板=一次正式 `text_generate` source Task（现有报价/确认/hold/charge/durable Asset/恢复/outbox 全复用）；产品侧用冻结蒙版确定性合成并做服务端质量裁决；合成结果是“派生输出”，绑定底板 Task/Asset 与输入/蒙版/覆盖哈希，不冒充 Task 结果 | 复用既有底座、范围最小、可逆；不碰公共合同。代价：平台只对“底板”收费，合成未过检时已扣费（已知不对称，R11） |
| C | 向公共 owner 要“复合输出”合同：平台在 capture 前完成合成并 durable | 费用与最终成品同源，但需公共 runtime/SKU/合同变更，现无可发布版本，会无限期阻塞本票。仅在 Root 明确要求时升级为此方案（见 §10 升级条件） |
| D | 保留旧直调供应商 + 产品内 claim，补 Bill 旁路 | 违反“不新增直连供应商旁路”。拒绝 |

## 4. 设计

小节标题里的“任务 N”沿用 Rev1 任务单的编号，与实施计划里的 Task 1–4（含 1A/1B、2A/2B 部分）不是同一套；二者与 P1–P7 分段的对应见 §5 的表。

### 4.1 总体数据流

```
浏览器：登录 → 选原图(真实上传) → 选背景方向(文字) →「获取报价」→ 看到金额/付款人/到期/样本范围 →「确认并生成」
Next BFF（不转发身份/金额/样本/通过声明）→ Go：POST /projects/{id}/source-image-runs
                                              {request_key, mode:"background_plate_lock", input_id, background_intent}
 1 服务端按本地登记的原图 SHA256 查冻结样本 → 经 Upload 取字节再核对 SHA/尺寸（只此一次）→ VerifyCoverage（失败：422，0 远端调用）
 2 冻结 intent（含服务端合成的 prompt、PlateDigest）→ 既有 usage/quote 流程 → 返回报价（0 Task）
 3 用户确认 quote → 既有 source Task（image.generate，1024*1024，单张）
 4 Task 成功 + Bill charged + Upload ready/哈希核对（既有 ObserveOutput 链路）→ 底板输出已证实
 5 派生：流式取底板并核对 SHA/大小 → 用冻结样本集里的原图/蒙版字节（不再读 Upload 原图，R22）→ 等比覆盖适配到画布 → 锁回主体 → platelock.Evaluate → 写一次派生记录+报告
 6 candidate_state：limited_candidate 可选/导出/回流；not_usable_candidate、pending、blocked 一律不可
```
任何一步丢响应、超时、重启、刷新、重登都沿原 run 的原键恢复，不产生第二个 usage/quote/Task/charge；GET 为纯快照不写（沿用 #38 约束）。

### 4.2 冻结样本集（任务 1，R3）

位置与产物：
- `fixtures/frozen-samples/v1/`（仓库根，供 server 测试、运行时装载与 web 取证共用）：`manifest.json`、`originals/*.png`、`masks/*.png`、`README.md`。
- 生成器 `server/cmd/samplegen`：纯 Go（`image/png`、`golang.org/x/image/font/basicfont`、`draw`，均为现有依赖），无随机、无时间；`-check` 模式重算像素并与清单比对。**提交的是冻结的 PNG 字节**（编码器版本变化不影响已冻结文件），清单同时记录 `sha256`（文件字节）与 `pixel_sha256`（解码后 NRGBA+尺寸），生成器只保证像素复现。
- 不读取、不复制私有 QA 目录里的任何文件或数值。

样本矩阵：3 类 × 3 画布 = 9 个 case。

| kind | material_class | 冻结关键点（用于覆盖区与人判，非 OCR） | 说明 |
|---|---|---|---|
| `carton` | opaque | Logo 图形、主文字 `PAINUO`、规格行 `NET 250G`、批号行 | 包装文字 + Logo |
| `handled_metal` | reflective | 杯体、把手环（含通孔：孔内为背景，蒙版为 0）、杯口边 | 明确结构，覆盖“孔洞不被保护” |
| `glass_bottle` | transparent | 瓶身轮廓、瓶标文字、瓶盖 | 透明材质：主体像素被锁，透射/接触阴影不可机检 |

画布：`1024x1024`、`1024x1280`、`768x1024`。每个 case 至少 2 个、至多 8 个背景方向（冻结在清单里，用于测试与取证；生产环境背景方向仍是用户文字，经同一 `ValidIntent` 规则：1–200 字符、无控制字符、前后无空白）。冻结方向必须与样本自身的背景在明度与色相上**显著反差**（样本原背景为中性浅色，方向用深色、高饱和或明显不同的场景），使一个合法的新背景不会因接近原背景色而被阈值 5 误判（R30）；这是选方向的规则，**不是**放宽阈值的理由。主体为硬边渲染，蒙版 = 主体轮廓（alpha 255/0），因此样本集不覆盖“真实照片边缘羽化/光晕”，该限制写入报告 `limits`。

清单 `frozen-sample-set/v1` 字段：`set_id`、`version`、`generator{name,version}`、`source="deterministic-generator"`、`license`（原创合成作品，项目内部授权）、`approval_ref="HUI-2232 C2 spec R3"`、`thresholds_version="bg-lock-thresholds/v1"`、`set_sha256`；每个 case：`case_id`、`kind`、`material_class`、`canvas{w,h}`、`original{path,sha256,pixel_sha256}`、`mask{path,sha256,pixel_sha256}`、`coverage{logo,packaging_text,spec,structure: [[x0,y0,x1,y1]...]}`（映射到现有 `bgreplace.CoverageSample.Regions`，直接复用 `VerifyCoverage`，不重写）、`product_count`、`expected_keypoints{text[],logo,structure[]}`、`intents[≥2]`、`human_axes[]`（该样本需要人判而机器 UNKNOWN 的轴）。

冻结阈值 `bg-lock-thresholds/v1`（常量，改动须升新版本号，只允许有证据地收紧）：
1. 蒙版内（alpha≥128）合成结果与原图 NRGBA **逐点 0 差异**（含 alpha）。
2. 四类覆盖区（logo/packaging_text/spec/structure）每个像素都在蒙版内，且合成后与原图 0 差异。
3. `product_count`：蒙版 alpha≥128 的 4 连通域（面积≥64px）个数 = 冻结值。
4. 输出尺寸 = case 画布（精确）；底板尺寸 = 1024×1024（Task 画像）。
5. 真背景变化：在蒙版外像素 U 上，`max(|ΔR|,|ΔG|,|ΔB|) ≥ 16` 的像素占比 ≥ 0.80，且 U 上 RGB 平均绝对差 ≥ 12。该数值在计划阶段用 §4.6 的正反例标定（底板=原图、±3 噪声、纯异色底），只能收紧，不能放宽。
6. 底板字节 SHA256 ≠ 原图 SHA256，且底板解码像素在 U 上不等于原图。

### 4.3 运行时装载与配置（任务 2，R4）

- 新包 `internal/fidelitysamples`：读取 `PRODUCT_FIDELITY_SAMPLES_DIR` 下清单；严格 JSON（拒绝未知字段、重复键、尾随内容）；路径只允许清单目录内的相对路径、拒绝符号链接逃逸；逐 case 重算原图/蒙版 SHA256、尺寸，调用 `VerifyCoverage`，并检查蒙版 `0<保护像素<全图`、连通域数=`product_count`、`intents≥2`、`set_sha256` 自洽。任一失败 → 该功能整体不可用并给出原因，**不让进程 fatal**（沿用 `/readyz` 不因可选功能 503 的现有策略）。
- 新配置（默认全关）：`FEATURE_BG_PLATE_LOCK=0`、`PRODUCT_FIDELITY_SAMPLES_DIR=`。仅当 `FEATURE_BG_PLATE_LOCK=1` 且 source 运行时就绪（`FEATURE_SOURCE_IMAGES=1`、`FEATURE_GENERATION_ENABLED=1`、`ECO_BILLING_ENABLED=1`、定价版本与下载主机已配置、输出恢复 ready）且样本集装载成功且付款人为个人时，`source-image-capabilities` 才把 `background_plate_lock` 报为 `ready`。原因码：`plate_lock_disabled`、`sample_set_unconfigured`、`sample_set_invalid`、`plate_org_not_supported`（付款人不是个人）、`source_unconfigured`、`project_write_forbidden`。关开关只禁止**新建**与**新确认**（含对尚未确认的报价的确认）；已确认 run 的恢复、重放、读取、导出、选定、取消与删除照常（§6）。
- 样本由服务端按“工程输入的本地登记 SHA256 → 冻结集合”查找；创建时再经 Upload 取字节核对。浏览器请求体是封闭对象，只允许 `request_key、mode、input_id、background_intent`；任何 `sample_id`、`mask*`、`coverage*`、`quality*`、`passed`、`supplier_authorized`、金额字段 → 400。蒙版取自服务端冻结文件，浏览器不上传蒙版。
- `capabilities` 对当前工程回报 `supported_input_ids`（由本地照片登记的 SHA 与冻结集合求交，GET 不访问远端、不写库，R25），UI 据此启用或给出“该照片不在已验证范围”。本地登记与 Upload 真实内容不一致时，能力端点可能多列一项，创建时的字节核对会以 422 `sample_not_frozen` 拒绝（零费用，仅体验问题）。
- 旧字段 `Server.PlateCoverage` 与 `handleModelPlate` 的直调路径退役（R6）。

### 4.4 `background_plate_lock` 模式与付费路径（任务 3，R5–R8、R11）

沿用 #38 的 `product_source_runs` 与 `sourceflow`，改动面如下（均为加法）：

1. **Intent**：`Mode` 允许 `background_plate_lock`；provider/model/capability/size/quantity/pricing 仍等于现有画像（同一 SKU、同一价格版本）。新增 `omitempty` 字符串字段 `PlateDigest`（冻结输入摘要）。`Prompt` 为服务端按 `bgreplace.ModelPlatePrompt(background_intent)` 合成并冻结，浏览器不能提交 prompt。背景方向文字：1–200 字符、无控制字符。
2. **冻结输入侧记录**（迁移 `0020_plate_lock.sql`，加法，R24）：`product_plate_inputs(run_id PK→product_source_runs(id) RESTRICT, frozen_json, digest)`，含 case_id、set_sha256、原图 asset_id/版本/SHA256、蒙版 SHA256、覆盖 SHA256、画布、背景方向原文、`fit_id`、`compose_id`、`thresholds_version`；与 run 同事务写入，不可变触发器；读取时校验 `digest == Intent.PlateDigest`，不符为 `ErrInvariant`。旧 `text_generate` 行的 Intent JSON 与指纹字节保持不变（新增字段 `omitempty`；以改动前代码算出的黄金指纹测试固定，防止已落库行读出 `ErrInvariant`）。
3. **派生输出，写一次、不可变（R23）**：`product_plate_derivations(run_id PK→product_plate_inputs, state, blocked_reason, derived_png BLOB, derived_sha256, size_bytes, width, height, report_json, report_sha256, candidate_state, created_at)`，由触发器禁止 UPDATE/DELETE 与二次 INSERT。**没有** `selected`、`returned_output_id`、`revision` 这类可变列：选定仍是 source run 自己的 `selected`，回流绑定落在 `0021` 的 `project_output_quality`（§4.7）——可变列会让“结果与报告被事后改写”成为可能，写一次 + 读时校验 SHA 是更强的不变量。派生字节存产品库（沿用 `bg_result_blobs` 的“读时校验 SHA”模式），**不**上传为平台 Asset、**不**声称是 Task 结果；视图字段 `result_kind="derived_composite"`，底板事实在 `plate_task{task_id,asset_id,reference_id,sha256}`；原始底板永不作为结果展示（`output=null`）。
4. **派生触发（R22）**：Task 成功、Bill `charged`、Upload ready 且哈希核对后（既有 `ObserveOutput` 之后），由恢复协调器或显式 `reconcile` 驱动一次幂等派生：先核对冻结输入摘要，再用冻结样本集里的原图/蒙版字节（**不再经 Upload 读原图**——“登记的原图 SHA = 冻结原图 SHA”已在创建时经 Upload 字节核对；之后再依赖 Upload 保留期只会制造 `original_unavailable`，旧 handled-metal 已出现过）；流式取底板（既有受限下载，不暴露签名 URL）并核对大小与 SHA → `FitPlateCover` → `LockSubject`（复用）→ `platelock.Evaluate` → 事务写入。CAS/租约沿用 source run 的 lease；同一 run 的派生全程互斥，并发重复调用返回同一条记录。若样本集在计价后被换（case 摘要与冻结输入不一致）→ 终态 `blocked / sample_set_changed`：不下载、不扣费、不重生成，底板引用保留。底板下载中断或暂不可达 → 退避重试、保持 `pending`，永不换新 Task。
5. **适配 R8**：底板固定 1024×1024（沿用 Task 画像）。`1024x1024` 原样；`1024x1280` 先等比放大到 1280×1280（`x/image/draw` CatmullRom）再居中裁宽到 1024；`768x1024` 居中裁宽到 768（无缩放）。标识 `fit-cover-center-catmullrom/v1`，写入报告；不使用 `FitPlate` 拉伸。确定性（R28）按“同构建、同架构”承诺：1024×1280 的 CatmullRom 是浮点核，跨 arm64/amd64 可能末位不同；已存派生记录才是权威，测试只断言同进程/同机重复派生字节一致。
6. **选择/导出/内容/删除**：沿用 source 路由。`select`/`export`/`return` 仅在 `candidate_state=limited_candidate` 且**当次**重新核对 Bill 仍 `charged` 时放行；`content` 对 `limited_candidate`、`not_usable_candidate` 均可看（后者带 `X-Quality-Verdict: fail` 头与界面横幅），因为用户有权看到为何不可用；`pending`（尚无派生记录）与 `blocked`（无合成字节）返回 409。画布取冻结 case 的尺寸，不取工程宽高。导出 zip 含 `image.png`（派生合成）、`quality.json`（冻结的 v2 报告，必须与重算逐字节一致）、`snapshot.json`。删除沿用 `DELETE .../output`：同事务 tombstone + 底板引用 release outbox，不退款、不宣称物理删除。
7. **旧路径退役 R6**：`POST .../background-replacements/{jobId}/model-plate` 返回 `410 direct_supplier_route_retired`，不读请求体、不调用 `imagemodel`、不写库；Web BFF 白名单同步移除；`BackgroundReplacePanel` 删除其触发入口。`store` 的 `bg_plate_executions`/`PreparePlateExecution` 等保留为历史（不迁移、不删表，避免无关改动）。
8. **组织**：本模式仅个人付款；即使 `FEATURE_SOURCE_ORG_IMAGES=1` 也不开放，能力原因码 `plate_org_not_supported`，创建时同样拒绝（组织 Gate 未完成，R16）。

计价与不对称（R11）：报价、确认、hold、charge 全是**底板**的；界面报价处明示“费用按生成背景图计；生成后的商品检查未通过时该结果标记为不可用，不会自动重做或再次扣费”。产品无退款入口，不通过产品代替平台 release/refund。

### 4.5 服务端质量证据报告（任务 4，R9–R10）

新包 `internal/platelock`（纯函数、无 I/O）：`FitPlateCover`、`Compose`、`Evaluate`、`ReportJSON`。`Evaluate` 的输入**只能**来自服务端：冻结样本 case、原图字节、冻结蒙版字节、底板字节与其 Upload 事实、合成字节、run 事实（Task/Usage/Quote/Charge/Asset/Reference）、构建信息。输入类型里**没有**任何分数、浏览器布尔、`supplier_authorized`、相似度字段；报告 JSON 解码用 `DisallowUnknownFields`，加入分数字段的文档被拒绝。

报告 `product-source-quality/v2`（`text_generate` 的 v1 不变）：

- `scope`：`mode, provider, model, pricing_version, sample_set_id, case_id, canvas, material_class, claim="frozen_synthetic_sample_only"`。
- `axes[]`：`{name, state, machine_checked, evidence}`，`state ∈ {PASS, FAIL, UNKNOWN, N/A}` 四值永不互相折叠。
  - 必需机检轴（任一非 PASS 即不可用）：`mask_pixels_identical`、`coverage_logo`、`coverage_packaging_text`、`coverage_spec`、`coverage_structure`、`product_count`、`output_size`、`plate_size`、`plate_is_new_background`（底板≠原图）、`background_changed`、`plate_task_succeeded`、`plate_asset_hash_verified`、`plate_charged_observed`。
  - 列为限制的人判/不可机检轴（只能 UNKNOWN 或 N/A，不得由机器置 PASS）：`background_extra_objects`、`background_intent_followed`、`visual_composite_quality`；玻璃样本另有 `transmission_edge`（其余样本 N/A）。
- `verdicts`：`fidelity`（仅由必需机检轴中的主体轴决定）、`generation`（Task/Asset/哈希事实）、`billing`（仅按派生时已冻结的原 charge 事实判定；当前 Bill 状态随时间变化，不进入冻结报告，由选择/导出/内容时的实时核对与视图单独给出），各为 `PASS|FAIL|UNKNOWN`；`human_usefulness="NOT_RUN"`。
- `candidate_state`（R32）：任一必需机检轴 FAIL → `not_usable_candidate`；无 FAIL 但有必需轴 UNKNOWN → `pending`；必需轴全 PASS → `limited_candidate`（`limits[]` 必须非空：样本范围、边缘光晕未覆盖、背景内容未机检、材质特定项）。**没有任何输入能把存在 FAIL 的结果抬成 `limited_candidate`**，也没有“综合美观分”入口。`pending` 在本模式里有两个出处，语义一致——“暂时不能使用”：(a) 报告层：必需轴因事实缺失为 UNKNOWN；(b) 视图层：派生记录尚不存在。另有派生层终态 `blocked`（样本集在计价后变更，§4.4 第 4 点）：无合成字节、无报告，视图的 `candidate_state="blocked"` 并带 `blocked_reason`，同样不可选/导出/回传；`blocked` 不是报告里的 `candidate_state` 取值，因为 `blocked` 时没有报告。
- `facts`：`usage_id, quote_id, task_id, original_charge_id, plate{asset_id,reference_id,sha256}, original_sha256, mask_sha256, coverage_sha256, composite_sha256, fit_id, compose_id, thresholds_version`、`build{vcs_revision, vcs_modified}`（来自 `internal/buildinfo`：优先取 ldflags 注入值，未注入时回退 `runtime/debug.ReadBuildInfo`，见下）。
- `review`（可选、仅供参考）：SIMULATED-HUMAN 评审摘要的引用与哈希，**不参与** `candidate_state`；显式标 `kind="SIMULATED-HUMAN"`。
- 报告字节确定性（固定字段序），存 `report_json` + `report_sha256`；导出时与重算逐字节比对，不一致 `ErrInvariant`。

前后端 SHA（R26）：后端 SHA 来自报告 `build` 与 `/readyz` 新增的 `build` 字段；前端 SHA 由取证脚本记录 `git rev-parse HEAD` 与 `next build` 所用 worktree，二者在 evidence 的 `run-record.json` 里与同一 run 的 usage/quote/task/charge/asset/hash 联接。**构建身份必须显式注入**：新包 `internal/buildinfo` 暴露 `revision`、`modified` 两个由 `-ldflags "-X …buildinfo.revision=… -X …buildinfo.modified=…"` 注入的变量，验收构建同时用 `-buildvcs=false`（G15：worktree 里 Go 会把主仓 HEAD 盖成 `vcs.revision`）；未注入时才回退到工具链戳，且验收预检会因“二进制自报 revision ≠ worktree `git rev-parse HEAD`”而 BLOCKED，不会放行。`vcs_modified=true` 或预检不一致的构建不得作为验收证据。

### 4.6 反例矩阵（任务 5）

每行都是自动化测试，位置按层归档；“远端副作用”列是断言项。

| ID | 触发 | 期望 | 层 | 远端副作用断言 |
|---|---|---|---|---|
| F01 | 蒙版挖掉 `packaging_text` 区 | `coverage_rejected` 422 | fidelitysamples / httpapi | usage=0 quote=0 task=0 |
| F02 | 全透明蒙版 | 装载失败（功能不可用）；运行期不可达 | fidelitysamples | 0 |
| F03 | 全覆盖蒙版（无可换背景） | 同上（`MaskReady` 拒绝） | fidelitysamples | 0 |
| F04 | 浏览器上传非冻结尺寸/内容的照片 | `sample_not_frozen` 422，能力里不在 `supported_input_ids` | httpapi / web | 0 |
| F05 | 冻结原图任意 1 像素被改 | 哈希不符 → `sample_not_frozen` | httpapi | 0 |
| F06 | 底板尺寸错（非 1024×1024） | `plate_size`=FAIL → `not_usable_candidate` | platelock | 已扣费事实保留，0 新 Task |
| F07 | 背景未变（底板=原图；±3 噪声） | `background_changed`=FAIL | platelock | 同上 |
| F08 | 伪造供应商字节：(a) Task 成功但 Upload 哈希不符；(b) 字节非 PNG；(c) 回放原图当底板 | (a) `ErrInvariant` 且无派生；(b) 解码失败轴 FAIL；(c)=F07 | sourceflow / platelock | 无第二 Task/charge |
| F09 | 同 job 并发：16 路同 `request_key` create / confirm / derive | 1 个 run、1 次 submit、1 个派生且字节一致 | store / sourceflow（含 `-race`） | usage=1 task=1 charge≤1 |
| F10 | submit 响应丢失、`provider_unknown`、Bill/Upload 不可达、下载中断 | 沿原键恢复，不换键、不新报价 | sourceflow / httpapi 线级 | dispatch=1 charge=1 |
| F11 | 跨用户/租户/工程/app 访问 list/get/content/export/select/return/delete | 404/403，不泄漏他人原图或样本命中 | httpapi | 0 |
| F12 | 需求版本/输入版本已变后回流旧输出；新 run 不覆盖旧输出行 | `stale_requirement_version` 409；旧行 SHA 不变 | httpapi / store | 0 |
| F13 | `not_usable_candidate` | select/export/return/通用 `/outputs` 重登记均 409 `candidate_not_usable`；无回执 | httpapi | 0 |
| F14 | 浏览器携带 `sample_id/mask/passed/quality/coverage/supplier_authorized/amount/prompt`；背景文字边角输入（空、仅空白、含换行/控制字符、201 字、前后空格、缺/重复键） | 400，拒绝前无任何付费调用 | httpapi / web BFF | 0 |
| F15 | 价格/模型/定价版本变化后复用旧确认 | 409（沿用 text_generate 同一校验） | sourceflow | 0 |
| F16 | 样本目录缺失、清单非法、任一文件哈希不符 | `sample_set_invalid`，能力 disabled，`/readyz` 非 fatal | fidelitysamples / cmd | 0 |
| F17 | 样本集在计价后被换（case 摘要与冻结输入不一致）；另：Upload 里的原图是否还在与派生无关（R22） | 终态 `blocked/sample_set_changed`，不下载、不扣费，底板引用保留，不重生成；原图在 Upload 已清理时派生照常完成 | sourceflow | 0 新 Task、0 下载 |
| F18 | 重启后重复派生（同构建同机，R28） | 返回已存记录，同 `composite_sha256`；reopen 后记录不变 | platelock / store | 0 |
| F19 | 把 `not_usable` 合成图的 PNG 字节 POST `/outputs` | 409 `source_output_owned`/`candidate_not_usable` | httpapi | 0 |
| F20 | `text_generate` 与创意输出 | 视图/报告永不含主体保真轴，`fidelity="not_applicable"` | httpapi / web | — |
| F21 | 旧 `model-plate` 路由，且 `BgModelReady()` 为真 | 410，0 次 `imagemodel` 调用 | httpapi | 0 |
| F22 | `Evaluate` 性质测试：任意轴状态组合 | `limited_candidate` ⇔ 必需轴全 PASS 且无 FAIL；加入分数字段的报告被拒 | platelock | — |

### 4.7 订单/Campaign 受限回流（任务 7，R12）

- 新增服务端发起动作 `POST /projects/{id}/source-image-runs/{runId}/return`（仅 `background_plate_lock`、需 `selected` 与 `limited_candidate`、工程存在来源绑定，否则 409 `no_source_binding`，与现有回执路由一致）：把**派生合成字节**经既有 `registerOutputBytes` 同一事实路径登记为 `kind=result` 的工程输出，并写 `project_output_quality(output_id PK, run_id, result_sha256, candidate_state, scope_json, limits_json, report_sha256)`（迁移 `0021_plate_output_quality.sql`，仅新增，随回流任务独立提交与验证，R24）。这是该字节唯一合法的登记入口；通用 `/outputs` 对其 SHA 一律 409。
- 回执 `POST /outputs/{outputId}/receipt`：存在质量绑定时，仅 `limited_candidate` 放行，否则 409 `candidate_not_usable`；`run_id` 取 source run 号，`billing_fact_refs` 取该 run 的**原 charge id**（不再扫账本）；`assets[].sha256` 即“结果版本”。旧需求版本负例沿用现有 `stale_requirement_version` 校验。
- 冻结合同的限制：`order-receipt/v1` 没有质量字段，本票不改公共合同。限制通过产品侧两条通道随结果走：`GET /projects/{id}/outputs` 与 `bindings/{id}/return-target` 视图带质量绑定（状态、范围、限制、报告 SHA）；导出 zip 的 `quality.json`。界面在回传前显示限制并要求用户明确确认“下游收到的回执不含该限制说明”。向下游传递限制需要跨仓合同扩展（建议 `order-receipt` 增可选 `quality_ref`），记为 Root 事项，不在本票实现。
- 测试：F12、F13、F19，加回执 HMAC 帧由本地契约接收桩验证；若 Root 提供真实 guanlan-order 栈引用则追加真实回流（Level B），否则该项记 NOT_RUN。

### 4.8 Web 与 UI（任务 6、9，R13）

范围：
- **`/start`（R29）**：把 `disabled` 的“选择商品照片”改为真实文件选择（`<input type=file accept>` + 键盘可达），走既有 `/api/photos` 上传与工程挂接（尚无工程时先创建个人工程再上传）；移除默认路径上的“商品照片编号”等开发者字段。个人路径收敛为**一条付费主路径**：真实上传 → 限定保真换背景；文字生成（`text_generate`）作为折叠的次要入口保留（M2 经此入口跑），不与主路径并列；旧 first-image 表单与 `ConsumePanel` 仅保留给订单/Campaign 深链（`source=order|campaign`），不再出现在个人路径（它们没有真实成功证据，不得作为可点击的个人付费入口）。个人工程靠 `?project=<id>` 在刷新/重登后找回。
- **新 `PlateLockPanel`**（挂在 `/start` 与 `/projects/[id]`）：步骤“选照片 → 背景方向 → 报价 → 确认 → 生成中 → 质量结果 → 选定 → 导出/回传”。复用 `components/ui/*`；只新增必要的小型无障碍原语（Field、Alert、Skeleton、Stepper），按 shadcn 做法拷入并用仓库语义色变量着色，不引入新 UI 库。Painuo Token 尚未进集成分支，故颜色走 `globals.css` 的语义变量层（`--surface/--text/--accent/--danger/--warn/--ok`），Token 到位后只改这一处。
- **文案规则**：主视图不出现 `Task/Bill/Hold/hash/provider/model` 名、阶段码、`NOT_RUN/UNKNOWN` 原词、内部 ID；这些放进每个结果下的折叠“技术详情”。测试对渲染结果做禁词扫描。文案集中在一个 `copy` 模块。质量结果用中文四态（通过/未通过/待核实/不适用）并写明“已验证范围：冻结合成样本（纸盒/金属把手杯/玻璃瓶）”。
- **状态覆盖**：空、加载、错误、禁用并给原因、成功、`pending`、`not_usable_candidate`；键盘：全部操作可 Tab/Enter/Space 到达，报价展示后焦点移到“确认”，错误进入 `role=alert`，状态用 `aria-live=polite`，可见焦点环；桌面 1280×800 与手机 390×844 无横向滚动。
- **能力矩阵（任务 9）**：模式卡片由服务端能力驱动：限定保真换背景（可用范围=冻结样本）、文字生成概念图（明示不保真）、创意换背景、光影场景、展示视频、参考图编辑（后四项在无真实成功证据期间“未开放/未通过”，按钮禁用并给原因）。不为这四项新增任何后端。
- **取证**：`web/e2e/` 下 Playwright 脚本；`playwright-core` 作 devDependency，精确固定 `1.63.0`（已核实 npm 存在，不带 `^`/`~`）；浏览器用本机已安装的 Google Chrome 可执行文件（`executablePath`，G18：本机无 Playwright 浏览器缓存），**独立** `--user-data-dir`（`RUN/caches/HUI-2232/pw-profile`）、headless、端口仅 32320–32329；访问地址只用 `http://localhost:<port>`（G17/R27，用 `127.0.0.1` 会被 BFF 同源校验 403）；不使用用户正在运行的 Chrome、不使用 Browser pane。本机 Chrome 二进制不可用时，该步记 BLOCKED，需 Root 批准才可下载 Playwright 浏览器到本票缓存，不得自行下载。截图分两类：**fixture 栈**（进程内伪平台，仅覆盖空/错/加载/不可用状态与键盘焦点，证据标 `fixture`，不当验收）与**真实栈**（成功路径与刷新/重登恢复）。
- 端口分配：32320 产品 Go server；32321 Next web；32322 回执接收桩；32323 平台故障代理（仅真实栈取证用）；32324 fixture 栈伪平台；32325–32329 备用。

### 4.9 真实付费隔离验收（任务 8，R14）

前置（全部由 Root 以环境变量/文件路径**引用**提供，本票不读取、不打印、不落盘任何凭据值）：可用的 QA 平台栈（Identity/Billing/Task/Upload，Task 侧已接 ModelXing 并含定价 `qa-20261001-qwen-image2-cny25-v1`）、测试账号与付款额度、`PRODUCT_SOURCE_PRICING_VERSION`、`PRODUCT_SOURCE_DOWNLOAD_HOSTS`。缺任一项则对应取证记 BLOCKED，不以 fixture 代替。

最小真实矩阵（必须）：
- **M1** 3 次付费限定保真：carton@1024×1024、handled_metal@1024×1280、glass_bottle@768×1024，背景方向互异且取自清单里冻结的方向（反差规则 R30）；冻结原图来自 `fixtures/frozen-samples/v1/originals/`，经页面文件选择器真实上传；全程经正常 UI（登录→真实上传冻结原图→选背景→报价→确认→生成→质量结果→选定→导出）。真实模型返回的背景若因阈值被判 `not_usable_candidate`，如实记为 FAIL 与一笔已发生的费用，不放宽阈值、不换样本凑通过。
- **M2** 1 次付费 `text_generate` 经正常 UI（任务 9：普通文生图与 1701+1991 正向证据对应）。
- **M3** 在上述 run 上做的零新增费用恢复探针：重复点击确认、刷新、新浏览器上下文复用同一份登录态（不是退出重登，R36）、经故障代理丢弃首个 submit 响应、显式 reconcile；断言 usage/quote/Task/charge 计数不变。
- 可选扩展（有额度才做）：每样本第二个背景方向、一次“背景几乎不变”的真实反例（预期 `background_changed`=FAIL）。
上轮 QA 证据显示单价 25 minor、单付款人额度约 100 minor（实际额度以 Root 本轮供给为准）；按此估算 M1+M2 共 4 笔恰用 100 minor。额度不足以覆盖时请 Root 另供付款人，扩展项跳过并如实记录，不得用 fixture 补数。

记录：每次付费动作**之前**先追加一行意图到 `RUN/evidence/HUI-2232/real-e2e/charge-ledger.jsonl`，动作后追加观测（usage_id/quote_id/task_id/hold/charge、客户价 amount_minor、现金与补贴/额度分项、剩余额度）；供应商成本只记 SKU 声明值并标 `declared`，供应商账本未经 Root 提供则 `supplier_expense_ledger_verified=false`，不推导成已核账。`run-record.json` 联接：前后端 SHA、同一 run 的 usage/quote/task/charge、底板 asset/hash、合成 hash、报告 hash、截图。登录凭据只经 Root 引用；若执行策略不允许代理自动键入，登录步骤记为 `ROOT_ASSISTED`（由 Root 提供预认证 storage state），Browser 层不得因此写 PASS。
SIMULATED-HUMAN：由独立评审代理（全新上下文）对照原图、合成图与冻结关键点输出结构化评审，写 `review.simulated_human.v1` 并显式标注；仅作辅助，不改变任何裁决，Human 层保持 `NOT_RUN`/`NEEDS_REAL_HUMAN`。

### 4.10 其它模式收口（任务 9）

| 模式 | 本票结论 |
|---|---|
| 普通 `text_generate` | M2 跑一条真实正常 UI 成功路径；已有 Root 的 SDK 级实网 PASS（a1/a2，`normal_web_acceptance=false`、供应商账本未核）仅作背景，不代替 M2 |
| 限定保真 `background_plate_lock` | M1 |
| 创意换背景（旧直调路径）、光影场景、展示视频、参考图编辑、旧 first-image | 不实现；UI 显示“未开放/未通过”且不可点；handoff 矩阵逐项写 `disabled/not_passed` 与原因；创意与文生图输出永不出现 Logo/结构保真字样（F20） |

### 4.11 SDK 与组织（任务 10，R16）

SDK 保持 v0.1.1，不写 `replace`、不复制 internal、不造 tag。触发升级的唯一条件：计划阶段证实某个必需操作 v0.1.1 缺失（目前核实无）；若 Root 要求 Task 执行侧供应商成本取证（`task.source_execution`，v0.1.2+），作为独立显式决定并单独验证。组织付款路径保持默认关闭，组织 Gate 记为未完成，除非出现真实 E7 委托。

### 4.12 边角输入、恢复要求与工程环境约束（R31、R33）

**边角输入与恢复要求**——上文没有逐条写出、但最可能咬到真实使用者的条件；计划阶段每条都必须有一条钉死它的自动化测试：

1. 用户上传的是普通照片而不是冻结样本：能力端点不列它、创建返回 422 `sample_not_frozen`、**零 Billing/Task 调用**、界面用人话说明已验证范围（F04）。
2. 模型返回的“新背景”几乎等于原图（纯白落在原背景色附近、返回原图、±3 噪声）：`background_changed` FAIL → `not_usable_candidate`，已扣费事实保留，**不自动重做、不再扣费**，界面说清失败项（F06、F07）。
3. 生成过程中双击/刷新/退出重登/响应丢失：沿原键恢复同一 run，usage/quote/Task/charge 各恒为 1（F09、F10；真实栈上由 M3 探针复验）。
4. 背景文字边角输入与浏览器夹带字段：400，**拒绝发生在任何付费调用之前**（F14）。
5. 关开关或回滚发生在生成中途，或样本集在计价后被换：关开关只关新工作，历史读取/导出/选定/删除/恢复照常；样本集被换 → `blocked/sample_set_changed`，不下载、不扣费（F17、§6）。

**工程环境约束（R33）**——对所有任务隐含生效：

- 所有 Go 命令 `GOWORK=off`（G16）；构建身份用 `-buildvcs=false` + ldflags 注入（R26）。
- 缓存：`GOCACHE=$RUN/caches/HUI-2232/go-build`、`npm_config_cache=$RUN/caches/HUI-2232/npm`；`GOMODCACHE` 共享；不使用 `PLAYWRIGHT_BROWSERS_PATH`（不下载浏览器，G18）。
- 重任务（全量 `go test ./...`、`-race`、`next build`、浏览器取证、全量 vitest）一律 `RUN/bin/heavy -- <cmd>`；单条命令不无输出挂起超 20 分钟，可能挂住的命令（服务启动、网络、浏览器）用 `perl -e 'alarm N; exec @ARGV' -- <cmd>` 包裹。
- 浏览器只用 `http://localhost:<port>`（R27），端口只用 32320–32329（分配见 §4.8）；独立 profile 目录，不碰用户 Chrome 与 Browser pane。
- 16GB 内存：不同时起多个 dev server 与浏览器；结束前停掉本票自起进程（只按端口段 32320–32329 与本票 profile 路径识别）、删除 `RUN/caches/HUI-2232` 与临时数据目录；`web/next-env.d.ts` 若被 Next 生成则不提交。
- 交付文件位置固定：runbook `docs/runbooks/hui-2232-plate-lock.md`（随分支提交）、证据 `RUN/evidence/HUI-2232/`、交接 `RUN/handoffs/HUI-2232.md`。

## 5. 建议实施分段与门禁（供 writing-plans 排序）

| 段 | 计划任务 | 内容 | 门禁 |
|---|---|---|---|
| P1 | Task 1（1A） | 样本生成器、冻结集、清单 schema、阈值常量、`fidelitysamples` 装载器、配置（`FEATURE_BG_PLATE_LOCK`、`PRODUCT_FIDELITY_SAMPLES_DIR`）、`buildinfo` 与 `/readyz` build 字段 | F01–F05、F16；`samplegen -check`；`go test`/`vet` |
| P2 | Task 1（1B） | `platelock`：适配、合成、`Evaluate`、`product-source-quality/v2`、阈值标定与性质测试 | F06–F08、F18、F22；`-race` |
| P3 | Task 2（2A + 2B 前半） | 2A：迁移 `0020`、`background_plate_lock` 模式、冻结输入、写一次派生、恢复与幂等；2B 前半：HTTP 面与能力端点（含 `supported_input_ids`）、旧 `model-plate` 路由 410 与 `PlateCoverage` 退役、`cmd/server` 接线 | F09–F11、F14–F15、F17、F20–F21；迁移保值与 reopen 测试 |
| P4 | Task 2（2B 后半） | 迁移 `0021` 回流质量绑定、`return` 动作、回执门控（仅 `limited_candidate` 放行）、通用 `/outputs` 登记守卫 | F12–F13、F19；回执契约桩 |
| P5 | Task 3 | 前端：`/start` 真实上传、`PlateLockPanel`、能力矩阵、人话文案与禁词扫描、Playwright 取证（fixture 栈，桌面+手机） | web `npm test`、`tsc --noEmit`、`next build`；截图 |
| P6 | Task 4（前半） | 真实栈 M1–M3 + SIMULATED-HUMAN | 证据包；Root 供给前置 |
| P7 | Task 4（后半） | 模式收口审计、runbook、handoff（分层+验收矩阵+verdict）、清理 | 清理缓存与自起进程 |

计划共 **4 个任务**（设计门上限 4，R34）：Task 1 = P1+P2（样本集 / 装载器 / buildinfo / 配置，再 platelock 引擎）；Task 2 = P3+P4（迁移 0020/0021、run 流水线、HTTP 面、旧 model-plate 410、cmd/server 接线、回流质量绑定）；Task 3 = P5（Web 与 fixture 栈 Playwright 取证）；Task 4 = P6+P7（真实验收、runbook、handoff、清理）。每个任务内部按部分（1A/1B、2A/2B）保留各自的 Step 与提交点，合并只是减少任务数，**不是**一次性大提交。

每段结束跑 `RUN/bin/heavy -- go test -p 2 -count=1 ./...`、涉及包的 `-race`、`go vet ./...`（P5/P7 加 web 全量）；证据放 `RUN/evidence/HUI-2232/`。每段内小步提交，英文 conventional commit，不 `--no-verify`，不推送、不开 PR。

## 6. 数据迁移、默认关闭与回退

迁移 `0020_plate_lock.sql` 与 `0021_plate_output_quality.sql`（R24）仅新增表/索引/触发器；旧表与行不变，沿用既有“失败回滚、reopen 保值、外键/rowcount”测试范式。两个开关（`FEATURE_BG_PLATE_LOCK`、`PRODUCT_FIDELITY_SAMPLES_DIR`）默认关闭/为空，关闭只禁止新建与新确认（含对尚未确认的报价的确认），不影响已确认 run 的重放、历史读、恢复、cancel、delete 与 outbox。**回退 = 关开关，不降级二进制**：旧二进制无法解析 `background_plate_lock` 的 run 行（指纹/校验会失败），一旦存在此类行就不能回滚到旧二进制；回滚不得覆盖真实已扣费事实。旧 `model-plate` 的关闭不可回退到直调。

## 7. 非目标

组织 E7 付款与组织 Gate；`reference_edit` 与公共“复合输出”合同；把派生合成上传为平台 durable Asset；真实人类验收；生产发布与任何生产动作；创意/光影/视频/参考图编辑的真实成功路径；退款入口；删除死代码 `qualitygate`/`engwalk` 与旧 `bg_plate_executions`；SDK 升级；Painuo Token 接入；样本之外任意照片的保真承诺；`order-receipt` 合同扩展。

## 8. 验收与 verdict 规则

- 每个声称都要有本 worktree 刚跑出的命令输出；handoff 写命令、退出码、关键输出、日期。验收矩阵每条标准一行：标准 → 证据路径或 `UNMET`。
- 分层记录：Engineering / Browser(fixture) / Browser(real) / Fidelity / Generation / Export / Billing-Recovery / Service-Provider / Cross-app（订单·Campaign 回流）/ Production / Human，各自独立、各自从 run-record 的真实字段推导（不是“有记录即通过”）；N/A、UNKNOWN、FAIL、PASS 不混。
- Verdict：全部标准有真实证据且仅人判为 SIMULATED-HUMAN → `DONE_WITH_SIMULATION`，其必要条件**同时**包括：所有 M1 run 为 `OBSERVED` 且 `limited_candidate`（`not_usable_candidate` / `UNKNOWN` 一律不算）、M3 四个探针全部 `equal`、M2 `text_generate` 真实跑出并加载了图、故障注入真的发生且每个 run 恰好扣费一次、导出 zip 与展示合成图逐字节一致、构建身份干净且一致、订单 / Campaign 真实回流（A7 Level B）有 `PASS` 证据；任何一项缺失 → `PARTIAL`，其中 `cross_app` 为 `NOT_RUN`（无 guanlan-order 栈）时**硬封顶 `PARTIAL`**。组织付款 Gate（E7）不在本票范围（§7），不构成通过依据，只作为 `open_items` 如实列出。工程与取证齐全、仅缺 Root 上线 → `NEEDS_PROD_GO`；其余 → `PARTIAL` 并逐条列剩余（预期最可能的结果：真实栈前置缺口、组织 Gate 未完成、真实回流栈缺席）。无论如何不得宣称关闭父票，不批准公共生产发布。

## 9. 风险与外部依赖

1. QA 额度：单付款人 100 minor 仅够 M1+M2；扩展矩阵需 Root 追加付款人。
2. 登录凭据与自动键入：可能需 Root 预认证 storage state（`ROOT_ASSISTED`）。
3. 本机浏览器：已核实本机装有 Google Chrome 与 Microsoft Edge、无 Playwright 浏览器缓存（G18）；复用 Chrome 二进制 + 独立 profile，若被否决则该步 BLOCKED，需 Root 批准才下载 Playwright 浏览器。
4. 计费与质量不对称：底板已扣费而合成未过检时无自动补偿（R11）；彻底解决需公共“复合输出”合同（方案 C，另票）。
5. 回执合同冻结：限制无法进入 `order-receipt/v1`，下游看不到；需跨仓合同扩展，Root 事项。
6. 背景变化阈值（4.2 第 5 条）为首版冻结值，计划阶段用正反例标定，只收紧不放宽。真实模型对“白色/浅灰”类背景可能因接近原背景色而被判 `background_changed=FAIL`——这是保守方向；靠冻结方向与原背景反差（R30）规避，而不是放宽阈值。
7. 合成样本不等于实物照片：硬边、无羽化，报告 `limits` 与 UI 范围文案必须写明；扩大范围需另行授权实物样本。
8. Upload 保留期：Upload 可能清理用户上传的原图（旧 handled-metal 已出现）。Rev2 起派生不再依赖 Upload 里的原图（R22），该风险只剩两处：创建时必须能读到原图字节（读不到则 422/503，零费用）；底板输出由 source run 的引用保持 active，删除 run 输出时才 release。底板在派生完成前不可达则保持 `pending` 并退避重试，永不换新 Task；若平台保留策略仍使其永久不可达，已扣费结果无法转为候选——属平台事项，登记给 Root，产品不重生成。
9. 供应商成本只能记声明值：账本核验需 Root 供给；否则 `supplier_expense_ledger_verified=false`。
10. `/start` 重做触及既有测试（`first-image-screen`、`source-image-*`）；P5 需成组更新，且不得降低既有 source 恢复测试强度。
11. 机器约束：16GB 内存，禁止同时多个 dev server+浏览器；重任务一律 `RUN/bin/heavy`。
12. 真实阶段不由本稿授权：任何真实服务/费用动作需 Root 供给前置并在 handoff 记录。
13. 跨架构确定性：1024×1280 的 CatmullRom 放大在 arm64/amd64 上可能末位不同（R28）；已存派生记录与其 SHA 绑定是权威，不在别的架构上重算后覆盖。

## 10. 升级条件（方案 P → C）

仅当 Root 明确要求“费用与最终合成成品同源”时，改走公共复合输出合同：本稿 §4.4 的 1–4 点（Intent 摘要、侧记录、派生触发）作废，改为消费平台复合 Task 的最终输出并按 durable Asset 验证；§4.2、§4.3、§4.5、§4.6、§4.7、§4.8 基本不变。该升级需公共 owner 先发布含该能力的 SDK tag 与部署清单，产品不得抢先猜测。

## 11. 验收标准（映射到票的 12 条剩余范围）

| # | 标准 | 主要证据 |
|---|---|---|
| A1 | 仓库内冻结样本集（9 case）、清单、生成器 `-check`、阈值常量；字节与 SHA 可复验 | P1 测试与 `samplegen -check` 输出 |
| A2 | 运行时装载器默认关、失败关闭；`PlateCoverage` 旧字段退役；能力端点如实报告 | F01–F05、F16 |
| A3 | `background_plate_lock` 经正式 source Task 付费，无直连供应商旁路；旧 `model-plate` 410 | F09–F11、F21、线级测试 |
| A4 | `product-source-quality/v2` 正向裁决，PASS/FAIL/UNKNOWN/N/A 分开，浏览器无法声称通过，无分数入口 | F22、报告重算一致性 |
| A5 | 反例矩阵 F01–F22 全部自动化并通过 | 测试名与输出 |
| A6 | 正常 UI 全路径含键盘/焦点/空/错/加载态，刷新与重登恢复同一 job，`/start` 上传按钮可用、无开发者措辞 | Playwright 桌面+手机截图、禁词扫描测试 |
| A7 | 回流：结果版本与限制不丢，未保真结果不能升级为已验证 | F12–F13、F19、回执契约桩；真实回流为 Level B |
| A8 | 真实付费隔离验收 M1–M3 + SIMULATED-HUMAN；每笔真实费用有账；供应商成本与平台补贴分开记录 | `real-e2e/` 证据包 |
| A9 | 其它模式如实收口（跑通或列为禁用/未通过，UI 一致） | 能力矩阵与 handoff |
| A10 | SDK 仍 v0.1.1；组织路径默认关闭 | `go.mod`、能力端点 |
| A11 | spec/plan/runbook/handoff（分层+verdict）齐备 | 文件 |
| A12 | 收尾清理：自起进程已停，`RUN/caches/HUI-2232` 与临时数据已删 | 清理记录 |

### 11.1 票内勾选条件的覆盖（避免 A1–A12 与票原文脱节）

| 票内条件（来源节） | 对应 | 本票能给的结论 |
|---|---|---|
| 每个宣称支持的模式都从正常 UI 得到可信 Task 结果/真实 Asset/可打开文件/持久选择/实际导出（R2 补充） | A3、A6、A8、A9 | 限定保真 = M1；文生图 = M2；其余模式声明“未开放/未通过”，不宣称支持 |
| 仅验收限定保真时不强等文生图/视频；声明文生图可用须引 1701+1991 正向证据；创意不假称保真（R2 补充） | A9、F20 | M2 之外不扩大；创意与文生图永不带保真字样 |
| 汇总各模式 Implementation/Provider/fidelity/Billing/recovery/export-return；N/A 与 UNKNOWN 不混同；固定失败反例（R2 补充） | A4、A5、A11 | 分层记录 + F01–F22 |
| trace、task/asset/hash/报价/结算事实与前后端 SHA 对应同一组实际任务（R2 补充） | A8、R18、R26 | `run-record.json` 联接 |
| 冻结授权商品集，覆盖包装文字/Logo、结构、材质、不同背景、常用尺寸；逐项阈值与错误样例（当前 Gate） | A1、A5 | 9 个合成 case，**不是实物授权商品**，范围限定见 R2/R3 |
| 正常 UI 登录→上传→选背景/确认报价→真实模型→质量检查→选择→导出；另跑订单/Campaign 受限回流（当前 Gate） | A6、A7、A8 | UI/M1 可达；真实回流 Level B 无 guanlan-order 栈则 `NOT_RUN` |
| HUI-1991 真实平台接线与账务恢复为完成前置；失败/unknown/重复/刷新不重生成或双扣；供应商成本与平台补贴分开（当前 Gate） | A8（M3、台账） | 本票只消费既有平台接线；供应商成本仅声明值，补贴项平台未返回即 UNKNOWN；HUI-1991 自身不在本票关闭 |
| 自动化选用只算测试动作，独立模型评审只辅助；human usefulness 标未开展（当前 Gate） | R10、§4.9 | SIMULATED-HUMAN 不进裁决；`human_usefulness=NOT_RUN` |
| 发布可用范围精确到模式/模型/样本条件（当前 Gate） | R2、报告 `scope` | `claim=frozen_synthetic_sample_only` |
| 无法保真的结果明确标记/阻止作为可用候选（原条件） | A4、F13 | 仅 `limited_candidate` 可选/导出/回传 |
| cost/quote/usage 可解释（原条件） | A8 | 报价金额/付款人/到期/范围展示 + 每笔台账 |
| 报告每份被采用图片的费用与**人工修补时间**（2026-09-27 补充） | A8 | 费用有记录；人工修补时间无真人 → `NOT_RUN` |
| 至少一组真人/目标用户确认实际可用（原条件，已被“当前 Gate”移出工程关闭条件） | — | `NEEDS_REAL_HUMAN`，不由本票声称 |

## 12. 总体边界重申

不 push、不开 PR、不合并远端 main、不碰生产与秘密、不用用户的 Chrome 或 Browser pane、不改邻仓与他人 worktree、不写 Linear；Linear 状态与评论由 Root 依据 handoff 回写。

## 13. Root 自答记录（章程 §1：所有本该问用户的问题）

Ruling: R1 范围与分段——本票按“架构级”处理，单份 spec、单份 plan，按 §5 分 P1–P7 七段，每段带独立门禁；不再拆子票 — 因为各段共享同一冻结样本、同一模式与同一报告模型，拆开会重复建模且验收难归属 — 如果错了的代价：P2/P3 之间接口返工，计划阶段按段评审可及时发现。

Ruling: R2 限定保真的含义——仅对“冻结样本集 × 本模式 × 本模型 × 本画像”成立；任意用户照片在 UI 显示“不在已验证范围”并禁用该模式，不付费、不出图 — 因为通用蒙版的覆盖无法由服务端验证（G6），票明确要求“发布可用范围精确到模式/模型/样本条件” — 如果错了的代价：功能对真实用户照片不可用；需要 Root 另批“用户画蒙版+人工评审”的更大范围。

Ruling: R3 样本来源与授权——仓库内用确定性生成器产出 3 类×3 画布共 9 个合成样本并冻结 PNG 字节与哈希；授权引用写 `HUI-2232 C2 spec R3`；不读取私有 QA 目录 — 因为章程禁读私有目录，且“冻结+可复验”要求样本可随仓库提交；合成样本不冒充实物照片，报告与 UI 都标 `frozen_synthetic_sample_only` — 如果错了的代价：若 Root 要求实物样本，需另获授权并新增 case，现有机制可直接扩容，已验证范围文案不会被误用。

Ruling: R4 装载与蒙版来源——服务端装载冻结目录，按本地登记 SHA 命中样本，蒙版取服务端冻结文件，浏览器只提交 `input_id` 与背景文字；默认关闭、失败关闭、不致 `/readyz` fatal — 因为“浏览器不得自报通过/样本”，且上传蒙版没有额外安全收益只增加摩擦 — 如果错了的代价：若 Root 要求用户自带蒙版，需在 §4.3 增上传与逐案哈希核对，主流程不变。

Ruling: R5 付费路径——选方案 P：底板走既有 `text_generate` source Task，合成在产品侧，派生输出引用底板 Task/Asset/输入与蒙版哈希；不加直连供应商旁路，不把本地合成冒充 Task 结果；**与旧 spec §9 第 2 行（要求复合 Task 合同）不同，已在 §0 记录** — 因为现无可发布的复合输出合同，P 复用已通过独立审查的底座，范围最小、可逆（任务描述的“Preferred”） — 如果错了的代价：Root 若坚持复合合同则按 §10 升级，§4.4 前四点作废，其余可复用。

Ruling: R6 旧直调路由——装载器会让旧 `model-plate` 变成可达的无计费直调，故将其永久关闭（410），而不是复用 — 因为“不新增直连供应商旁路”与资金安全优先于保留旧入口；旧入口生产上本就不可达（G2） — 如果错了的代价：删除/改写旧路由测试；若 Root 要保留直调做对照实验，需另开受限开关，本稿不提供。

Ruling: R7 派生输出存放——产品库 BLOB（读时校验 SHA），不上传为平台 Asset — 因为现无需要且 durable 创建链路重（多段签名上传）；底板引用在派生存在期间保持 active，删除派生时一并 release — 如果错了的代价：产品库体积增长（每个 ≈1–3MB）；日后可迁到 durable Asset，哈希绑定不变。

Ruling: R8 画布适配——底板固定 1024×1024，等比覆盖+居中裁切（CatmullRom），标识写入报告；不请求画布同尺寸底板 — 因为现有 SKU/画像只冻结 1024*1024，改尺寸需新价格与合同；`FitPlate` 拉伸会变形 — 如果错了的代价：1024×1280 的背景有 1.25× 放大软化；需要更高清背景时另申请 SKU。

Ruling: R9 报告命名与形态——新增 `product-source-quality/v2`，v1 保持给 `text_generate`；四态永不折叠；输入类型无任何分数字段 — 因为 v1 已发布并被测试冻结，同名异形会破坏消费者；“单一美观/相似度分不得覆盖失败检查”靠类型层面杜绝 — 如果错了的代价：若 Root 坚持沿用 v1 名，需给 v1 加 `mode` 判别并迁移既有测试。

Ruling: R10 “通过”的措辞——机检必需轴全 PASS 才是 `limited_candidate`（限定候选），不使用“已验证商品图/完全保真”；背景多余物体、背景意图、整体观感、玻璃透射为 UNKNOWN/限制，不由机器置 PASS；SIMULATED-HUMAN 仅作参考 — 因为这些轴无法被机检，且章程禁止冒充真人 — 如果错了的代价：状态偏保守，用户看到“限定候选”而非“通过”；真人评审到位后可新增报告字段而不改裁决。

Ruling: R11 已扣费但未过检——不自动重做、不二次扣费、不提供产品内退款；报价文案明示；用户可显式新请求 — 因为产品无退款入口且不得绕过 Task 的 hold/capture/release 所有权；合成为确定性，失败概率低（F06–F08 场景） — 如果错了的代价：真实用户可能为不可用结果付费；彻底消除需方案 C（公共复合输出合同）。

Ruling: R12 订单/Campaign 回流——回执只读冻结合同，不加字段；仅 `limited_candidate` 可回执；限制经产品侧质量绑定、视图与导出报告传递，回传前界面明示“下游回执不含限制说明”；跨仓合同扩展作为 Root 事项 — 因为 `order-receipt/v1` 是冻结公共合同，本票无权修改，同时必须保证未保真结果不能被升级 — 如果错了的代价：下游无法自动看到限制；扩展合同后只需在回执构造处加字段。

Ruling: R13 UI 实现方式——复用 `components/ui/*`，只拷入少量 shadcn 式无障碍原语并用语义变量着色；不引入新 UI 库；Painuo Token 未到位前走语义变量层；Playwright 作 devDependency，浏览器复用本机已安装二进制+独立 profile+headless+限定端口；fixture 栈截图明确标 `fixture` — 因为章程鼓励 React 前端用 shadcn 但禁止重型库并要求不用用户的 Chrome 会话；Token 名未发布不得臆造 — 如果错了的代价：Token 到位后改一处变量；若不许复用本机浏览器二进制则需 Root 批准下载 Playwright 浏览器。

Ruling: R14 真实验收口径——最小矩阵 M1（3 次限定保真）+M2（1 次文生图）+M3（零费用恢复探针）；每笔真实费用先登记后观测；供应商成本只记声明值；SIMULATED-HUMAN 只作辅助；凭据只经 Root 引用，必要时 `ROOT_ASSISTED` 登录 — 因为额度（100 minor/付款人）与章程“每次真实收费记入证据、不得用 fixture 冒充”— 如果错了的代价：矩阵偏小；Root 追加额度即可扩展，不影响结构。

Ruling: R15 其它模式——文生图跑真实正常 UI 一条；创意、光影、视频、参考图编辑、旧 first-image 不实现，UI 显示未开放/未通过，handoff 逐项记 `disabled/not_passed` — 因为票允许“跑到真实成功路径或明确列为未通过/禁用”，且不得借本票扩大成熟度 — 如果错了的代价：这些模式不可用；各自另票。

Ruling: R16 SDK 与组织——SDK 保持 v0.1.1；本模式只许个人付款，组织路径默认关闭，组织 Gate 记未完成 — 因为所需操作 v0.1.1 全具备，且无真实 E7 委托（D1 已裁定） — 如果错了的代价：若后续需 `task.source_execution` 或个人主体查询，再显式升级并单独验证。

Ruling: R17 verdict 预期——按 §8 规则，最可能 `PARTIAL`；仅在全部真实证据到位时才可能 `DONE_WITH_SIMULATION` 或 `NEEDS_PROD_GO` — 因为章程“宁可 PARTIAL 也不虚报 DONE” — 如果错了的代价：无，规则按证据自动落位。

Ruling: R18 前后端 SHA 联接——`/readyz` 增 `build{vcs_revision,vcs_modified}`；取证脚本记录前端构建 SHA；`vcs_modified=true` 的构建不作验收 — 因为票要求“trace、task/asset/hash/报价/结算事实和前后端 SHA 对应同一组实际任务” — 如果错了的代价：证据需补记构建指纹。

Ruling: R19 数据与回退——迁移加法、默认关闭、回退即关开关且不降级二进制 — 因为旧二进制无法解析新模式行（G5）并会误伤同工程 text_generate 列表 — 如果错了的代价：需要紧急降级时必须先处理 plate 行，runbook 已写明。

Ruling: R20 死代码与无关清理——`qualitygate`、`engwalk`、旧 `bg_plate_executions` 不动 — 因为 YAGNI，且“恒不通过 recorder”不应被改造成造成功的工具 — 如果错了的代价：留有少量无引用代码，可另票清理。

Ruling: R21 工作方式——plan 与实施按段提交，独立审查由 Root 编排；本子智能体只交付到“spec 写好并提交”，不调用 writing-plans — 因为任务指令与章程分工 — 如果错了的代价：无。

### Rev2 追加裁定（2026-10-03；R22–R29 来自计划阶段的精确化，R30–R33 为本次新增）

Ruling: R22 派生用冻结集合里的原图字节，不再经 Upload 读原图——创建时经 Upload 字节核对“登记 SHA = 冻结原图 SHA”，之后派生永不读 Upload 原图；样本集在计价后被换 → 终态 `blocked/sample_set_changed`（不下载、不扣费、底板引用保留），取代 Rev1 的 `original_unavailable` — 因为之后再依赖 Upload 保留期只会制造不可恢复的 `original_unavailable`（旧 handled-metal 已出现过），而冻结集合里的字节与登记 SHA 已被证明相同 — 如果错了的代价：样本集热替换后，旧 run 变 `blocked` 需重新报价；换来的是没有对 Upload 保留期的隐性依赖。

Ruling: R23 派生记录写一次、不可变；选定仍是 source run 的 `selected`，回流绑定落在 `0021` 的 `project_output_quality`，`selected/returned_output_id/revision` 不进派生表 — 因为可变列会让“结果与报告被事后改写”成为可能，写一次 + 读时校验 SHA 是更强的不变量 — 如果错了的代价：多一张表（0021），无其它。

Ruling: R24 迁移拆为 `0020`（冻结输入 + 派生）与 `0021`（回流质量绑定），各自随所属任务独立提交与验证，只加不改 — 因为两部分属于不同任务，混在一份迁移里无法独立回滚验证 — 如果错了的代价：无。

Ruling: R25 `supported_input_ids` 只读本地照片登记（SHA）与冻结集合求交，不访问远端、不写库；创建时才经 Upload 取字节核对 — 因为 GET 必须为纯读（沿用 #38 约束） — 如果错了的代价：本地登记与 Upload 真实内容不一致时，能力端点多列一项而创建时 422（零费用，仅体验问题）。

Ruling: R26 构建身份显式注入——`internal/buildinfo` 以 `-ldflags -X` 注入 revision/modified，验收构建加 `-buildvcs=false`，未注入回退工具链戳，且预检在“二进制自报 revision ≠ worktree HEAD”时 BLOCKED — 因为已实测（G15）worktree 里 Go 把主仓 HEAD 盖成 `vcs.revision` 且 `modified=true`，用它作“前后端 SHA 对应同一任务”的证据会造假 — 如果错了的代价：忘记注入 → 预检 BLOCKED（安全方向）。

Ruling: R27 浏览器一律用 `http://localhost:<port>` 访问 — 因为 source BFF 的同源校验拿 `Origin` 对比 Next 重建的 URL（host 恒为 `localhost`），`127.0.0.1` 会得到 403（G17） — 如果错了的代价：取证脚本里换一个 host 即可。

Ruling: R28 画布适配的确定性按“同构建、同架构”承诺；已存派生记录才是权威，测试只断言同进程/同机重复派生字节一致 — 因为 1024×1280 的 CatmullRom 是浮点核，跨架构可能末位不同，而不同架构本就不会重算已存记录 — 如果错了的代价：跨架构重算得到不同字节，但不影响已存记录与其 SHA 绑定。

Ruling: R29 `/start` 个人路径收敛为一条付费主路径（真实上传 → 限定保真换背景），文字生成折叠为次要入口并承载 M2，旧 first-image 表单与 `ConsumePanel` 仅保留给订单/Campaign 深链，个人工程靠 `?project=<id>` 找回 — 因为旧个人 first-image 入口没有真实成功证据，Rev1 §4.8/§4.10 要求关闭，且同页并列多条付费路径会让用户猜 — 如果错了的代价：旧个人入口消失；订单/Campaign 深链不受影响。

Ruling: R30 每个样本冻结的背景方向必须与样本自身背景在明度与色相上显著反差；阈值 5 不因真实 run 通过与否而放宽，只允许有证据地收紧 — 因为“白色/浅灰”类背景接近原背景色会被保守地判为未变，这不是缺陷而是约束；放宽阈值会让“登记产物冒充换背景”重新可通过 — 如果错了的代价：个别方向被判 `not_usable_candidate`；换一个更反差的冻结方向即可，不碰阈值。

Ruling: R31 Rev1 没逐条写出的五类边角输入与恢复条件（普通照片、背景几乎不变、双击/刷新/重登/响应丢失、背景文字边角输入与夹带字段、关开关/样本集被换）提升为 spec 要求（§4.12），计划阶段每条必须有测试 — 因为它们最可能咬到真实使用者，且都发生在付费调用之前或之后的资金安全边界上 — 如果错了的代价：计划里多几条测试。

Ruling: R32 `pending` 在报告层（必需轴 UNKNOWN）与视图层（尚无派生记录）同义为“暂时不能使用”；`blocked` 只是派生层终态（无合成字节、无报告），不是报告里的 `candidate_state` — 因为 Rev1 把 `pending` 用在两处而没说明，`blocked` 又需要一个不可被误读为“可用”的状态 — 如果错了的代价：视图枚举多一项，不改变任何放行规则（只有 `limited_candidate` 放行）。

Ruling: R33 工程环境约束（`GOWORK=off`、独立缓存、重任务经 `RUN/bin/heavy`、端口 32320–32329、本机 Chrome 二进制 + 独立 profile、清理）写入 spec §4.12 并对所有任务隐含生效 — 因为这些来自章程与已实测的环境事实（G15–G18），漏掉任意一条都会在执行阶段造成假失败或污染他票 — 如果错了的代价：无。

Ruling: R34 计划任务数合并为 4（设计门上限）：Task 1 = 原 Task 1+2，Task 2 = 原 Task 3+4，Task 3 = 原 Task 5，Task 4 = 原 Task 6；每个任务内按部分（1A/1B、2A/2B）保留各自的 Step 与提交点 — 因为设计门要求任务数 <= 4 且各自可独立提交验证，而合并不能变成一次性大提交 — 如果错了的代价：评审边界略粗，部分级提交点已保留。

Ruling: R35 verdict 推导器按层推导：新增 fidelity / generation / export / billing_recovery / cross_app 层，各自读 run-record 的真实字段；`DONE_WITH_SIMULATION` 需所有必需层 `PASS`，否则 `PARTIAL`，`cross_app = NOT_RUN` 硬封顶 `PARTIAL` — 因为章程 §6“宁可 PARTIAL 也不虚报 DONE”，且旧推导器只看记录存在与否会把 `not_usable_candidate`、探针不等、缺 M2 都当通过。组织 Gate（E7）不作为封顶项而作为 `open_items`：它在 §7 非目标内，若作封顶则 §8 的 `DONE_WITH_SIMULATION` 永远不可达 — 如果错了的代价：Root 若要求把 E7 也算封顶，只需在 `REQUIRED` 加一项。

Ruling: R36 M3 第四个探针命名为 `new_context_same_login_state`，证据 / runbook / handoff 一律写“新浏览器上下文复用同一份登录态”，不写“退出重登已验证” — 因为它没有登出也没有重新认证 — 如果错了的代价：无。

Ruling: R37 R30 用 `samplegen` 测试钉死（每个冻结方向有代表色，与样本底色的 RGB 欧氏距离 >= 100，当前最小约 126）；M2 驱动按钮选择器改为现有文案 `创建个人普通图工程` — 因为计划里原来的“概念图”文案与 `SourceImagePanel` 不符，真实付费前才会暴露 — 如果错了的代价：新增方向漏配代表色时测试失败（预期行为）。

## 14. 自查记录（Rev2，2026-10-03）

- 占位符：已扫描 TBD/TODO/待定/待补，无遗留空缺；唯一需计划阶段数值标定的是 4.2 第 5 条阈值，已冻结初值并规定标定方法与“只收紧不放宽”的规则（R30）。
- 矛盾：(1) 回流（G9）与既有 `source_output_owned` 守卫不冲突——底板字节与派生字节 SHA 不同，派生字节仅可经 §4.7 的服务端动作登记。(2) Rev1 §4.4 第 3 点的派生表可变列与“写一次”矛盾，已按 R23 删除。(3) Rev1 第 4 点的 `original_unavailable` 与“创建时已核对原图”矛盾，已按 R22 改为 `sample_set_changed`，F17、§9 第 8 条同步。(4) Rev1 §4.5 用 `runtime/debug.ReadBuildInfo` 作构建身份，与实测的 worktree VCS 戳错误（G15）矛盾，已按 R26 改为显式注入。(5) §4.8 “同一时刻只有一条可付费主路径”与文字生成入口并存已在 R29 厘清：主路径=限定保真，文字生成为折叠次要入口。
- 范围：单份计划可承载（Rev3 起计划为 4 个任务、覆盖 P1–P7）；组织 E7、公共复合输出合同、真人验收、生产发布、`order-receipt` 扩展均在 §7 非目标；§11.1 把票内勾选条件逐条映射，无遗漏也无镀金。
- 歧义：“限定保真通过”统一为 `limited_candidate`，“已验证商品图”一词在本模式下不使用；`pending` 两义与 `blocked` 已在 R32 明确；§4 小节标题里的“任务 N”与计划的 Task 1–4、P1–P7 分段的对应关系已在 §5 表中给出；计划阶段曾称 P1–P8 的裁定在本稿统一为 R22–R29，编号不再冲突。
- 已核实的事实锚点（2026-10-03）：`PlateCoverage` 仅在 `httpapi/server.go:56` 声明且 `cmd/server` 从未赋值；`model-plate` 路由仍在 `server.go:166`；`sourceimage/validate.go:34` 仍只接受 `text_generate`；`Intent` 为可比较 struct；`HasVerifiedSourceOutput` 在 `handoff.go:569/592` 守卫 `/outputs`；`StartScreen.tsx:69` 的上传按钮仍 `disabled`；SDK `go.mod` 为 v0.1.1 而 public-ai 已有 v0.1.0–v0.1.3 标签；仓库内无 `fixtures/`；`go.work` 存在；worktree 构建 VCS 戳错误（实测）；`playwright-core@1.63.0` 在 npm 存在；本机无 Playwright 浏览器缓存而有 Google Chrome。
- 本稿未做的事：未写实施代码、未起服务、未调供应商、未产生费用、未动他票 worktree、未写 Linear。
