# HUI-2232 限定保真工程 Gate 设计

总控已批准 A，范围仅 `model_plate_lock`。执行分支 `feat/hui-2232-execution-20261001`，基线 `42c2abfabf8b56e8dbbbf2525bc53d65bc5ae4e1`。不启动服务，不调用真实供应商，不修改其它树。真实验收由总控供给非生产栈后执行。

## 当前检查点

本次仅完成计划 Tasks 1–3 的消费协议、执行事实存储和纯覆盖验证组件。HTTP/UI 尚未接入这些组件，原 model-plate 直接供应商并发风险仍未修复。整票与真实生成/费用验收保持未完成；不能把新组件单测通过当成功路径已可达。

## 目标与边界

沿正常 UI 完成授权原图、服务器冻结的主体覆盖证据、不可变报价确认、一次平台 `image.generate` Task、可恢复查询、平台真实 Asset 解码与哈希核对、主体锁回、受限选择与真实下载。质量、供应商、资金、发布、真人采用分别记录。仅有像素相同不证明覆盖完整；仅有 Task 成功不证明产品保真；仅有 hold_id 不证明已结算。

复用已有平台 Task 作为预占/结算/退款 owner，禁止产品图直接扣费或新增直接供应商旁路。保留既有其它模式行为，不能借本票扩大其成熟度。

## 消费端契约

1. 新的限定图像调用使用 SDK v0.1.0 `task.submit`：POST `/internal/v1/tasks`，`app_id/account_id/project_id/capability=image.generate/idempotency_key/params/provider/billing`。付款人仅来自 `serverPayer` 已验证 principal；模型和 provider 来自服务端已核实配置；费用只来自 Bills.Quote 平台事实。
2. 状态读取使用 `task.get` 并显式 app_id。解析平台真实 `result_json`（JSON string）、大写状态和 hold_id；拒绝响应 task_id 不匹配。旧 `result` 只作历史测试兼容，不代替真实资产解析。
3. 先将原图 hash、mask hash、sample evidence hash、model/provider/size、payer、背景意图、quote ID/amount/version 与 Task 幂等键持久化，再确认、提交。任何字段改变需要重新报价；提交之后不可覆盖。
4. 数据库 CAS 从 confirmed→submitting，只有唯一获胜者调用平台。失败/超时记录 UNKNOWN；已知 task_id 只能 GET 恢复。无 task_id 的未知提交必须使用正式平台幂等键查询；如果平台尚无该操作，保持 UNKNOWN 并报告缺少契约，不盲目新提交。
5. 平台成功后只打开该 Task 声明的真实 asset，经 Upload 元数据、下载和 SHA256 核对。保留 background-task asset 与确定性 composite asset 的区别，上传 composite 不能反向证明 Task 成功。无可信资产结果时不通过、不重生成。

## 主体覆盖与质量

冻结服务端样本清单绑定 source/license/approval_ref、原图 SHA256、蒙版 SHA256、四轴关键区域和主体覆盖说明。浏览器只能选择已授权原图并上传匹配蒙版，不能自报通过或写入清单。服务端检查每个冻结区域均被 alpha>=128 覆盖、完整主体 mask 与冻结 hash 一致、原图和 mask 尺寸匹配。未在清单中的样本不允许宣称限定保真通过。

合成后再次检查主体像素完全一致、背景实际变化、尺寸等于冻结画布。结果记录明确适用样本条件、来源版本、模型及质量限制。只有全部必要事实成立才开放限定候选；Billing UNKNOWN 不可整体工程通过，但不会掩盖已核验质量结果。

## 持久化与 UI

在既有 SQLite 新增背景执行事实表，按 tenant/project/job 唯一记录不可变请求和恢复状态。它是产品任务关联，不是资金账本。报价前保存蒙版与样本证据摘要；UI 分成准备蒙版并获取报价、确认报价、提交平台任务、刷新、选择、下载，提交按钮不再直连 imagemodel。

新接口沿既有 background-replacements 动作路由，不新增 app_id/服务/端口/数据库。已有输入、输出和来源快照契约保留；D9 新适配另由总控集成。

## 已核实平台缺口与依赖

- public-ai 主树 `internal/task/provider_xingmo.go` 有 image.generate→`/internal/v1/relay/images` 路由和 params 透传；MaaS 模式仅 chat/completions，尚不能确认真实图像模型绑定。
- HTTP 只有 task_id GET，无幂等键 lookup；store 有 GetByIdempotency。需要平台 owner 确认正式补丁与操作名后才能实现无 task_id 的 GET-first 恢复。
- 平台 Task summary 只有 result_json/hold_id，不提供经账本核对的结算状态；费用 PASS 需要平台对应事实，不能从成功状态推导。
- 真实 provider result 的 asset_id/hash/model/usage 契约及 Catalog 模型字段由总控核实。不得用自造 fixture shape 冒充正式契约。

## 验收

工程测试：并发同任务一次 POST；确认前零 POST；quote/payer/输入改变拒绝；跨租户404；POST响应丢失无二次生成；GET恢复大写成功及result_json；错Task ID/坏哈希/非图片/无asset拒绝；漏保护区域/坏蒙版/无许可拒绝；合法四轴覆盖+真实字节组合成功；选择与刷新持久化且不触发生成。

真实验收：总控 TaskSpace31 UI+非生产平台+真实供应商，使用冻结授权样本集，同一次任务关联前后端SHA、quote/payer/Task/provider/asset/hash/settlement。httptest仅证明工程契约；Provider/Billing/Browser未运行仍UNKNOWN/NOT_RUN。
