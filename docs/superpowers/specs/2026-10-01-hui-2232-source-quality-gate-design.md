# HUI-2232 产品图工程质量 Gate：正式 source 消费设计

状态：root 已裁定方案 A 与身份/生命周期/质量边界；本完整 spec 待逐行审批，未授权实现。2026-10-01。

## 1. 目标、基线与证据边界

产品仓基线 `de9e7acda5d3203929bc2ff2c75de61dac9cbbd3`，新隔离分支 `feat/hui-2232-source-quality-20261001`。原工作树与全部旧 WIP 保留。此稿只新增文档，不修改产品代码、服务或实际资金。现有 Go 全套基线测试通过。

目标是正常 Web 登录、选工程与付款上下文、取得正式报价、明确确认后，走唯一 source Task、真实 ModelXing、durable Upload、原 Bill 结算；退出重登仍恢复同一请求、结果、质量与费用。工程测试、真实服务、真实费用、视觉质量分别举证。普通文生图通过不能代表参考图编辑或主体保真通过。

冻结公共依赖参考：Upload 5a `6ea2c13c1edfb7259302aafb30cf96e20a32c445`；source12 `cdb79242d43634f56d760d88bcb2ec7a1e4bb5c1`。public-ai runtime Tasks3–5 的装配、正式输出 reference 元数据、运行配置仍由公共 owner 实施和独立审查。本产品不把分支代码当发布接口。当前 `server/go.mod` 仅依赖正式 SDK v0.1.0；后续需公共 owner 发布真实包含所需 operations/字段的版本后更新，禁止 replace 到邻仓、导入 internal、复制服务端实现。

默认 source 关闭。配置缺失显示“正式生成尚未配置/报价未验证”，不能回落旧直调、legacy Task、fixture、免费或默认 25 分。root 独占实际服务、供应商/付费调用与凭据。此稿不包含秘密、签名 URL 或私有 QA 行。

## 2. 当前实现的具体差距

| 证据锚（产品仓相对路径） | 已有事实 | 新链要求 |
|---|---|---|
| `web/src/components/text-image/Panel.tsx:81` | begin 一次点击创建、confirm、submit，恢复只取 jobs[0] | 报价与收费确认两个独立动作；明确 run ID 恢复 |
| `server/internal/httpapi/textimage.go:128` | confirm 只改本地标记；submit 可优先直调 imagemodel；refresh 旧分支可本地 paint | source 路由与持久 owner 隔离；禁止旧 adapter/fixture 处理 source |
| `server/internal/store/textimage.go:89` | 旧 updater 拒成功且强制 billing=false | 新 source 事实独立表/投影，不能粗暴放宽旧 updater |
| `server/internal/httpapi/consume.go:51`、`store/usagequote.go:45` | 先远程 quote 后保存；每工程覆盖当前报价，无完整整数 quote | 请求 intent 先持久；不可变多次 run/quote；历史费用不覆盖 |
| `server/internal/platform/imagetask.go` | billing.mode=hold + amount_cny 的 legacy submit | 仅 task.source_* 的完整整数 source 引用 |
| `server/internal/platform/identity.go:46`、`httpapi/consume.go:325` | 产品假定 org_memberships/org_payers JWT；standalone payer=personal | 真实 JWT principal + 正式 Context facts + Bill 授权，不捏造 claims |
| `server/internal/httpapi/bgreplace.go` model-plate、`store/plateexecution.go` | durable claim 已有；模型仅生成底板，再本地锁主体；quote 并非真实 Bill quote | 保留旧 owner；新复合模式必须独立正式 Task/输出合同，不能装作现 image.generate 已支持 |
| `server/internal/imagemodel/generate.go` | 一律 images/generations，参考图塞 image 数组 | 不等于 ModelXing JSON images/edits 契约 |
| `server/internal/store/projects.go:143`、`httpapi/handoff.go:542` | archive 是可改 status；输出走旧 RegisterAsset；无 durable release outbox | 保留旧输出，新增 source reference 生命周期和明确删除语义 |
| `server/internal/qualitygate/qualitygate.go` | recorder 刻意恒定未完成 | 不改旧 recorder 造成功；正式 evidence report 分版 |

公共源码证据：Identity `internal/identity/token.go` 实际 access JWT 无 org_memberships/org_payers；`internal/contextcontract/contract.go:567` payer 为 live E7 delegation 的 PayerPolicyRef 优先、personal 次之，并非 tenant 自动等于 payer。Context 只提供权威事实，产品资源授权与 Billing 成员/权益检查仍各司其职。

供应商一手源码（仅只读）：`pilotseaview/internal/gateway/public_modality_ingress.go`、`gateway.go:3489`、`internal/provider/types.go:224`、`openai_compatible.go:1432`。JSON edit 为 `/v1/images/edits`，model/prompt/image 或 image_url/n，size 由具体 model 合同决定；源代码出现 edit-max 路由，历史认证 metadata 另有 edit/edit-plus。不能仅根据 model 名或历史 metadata 选择现可调用/可收费版本。公开同步响应 data 中 URL/base64，没有可猜测的公共异步查任务协议。

## 3. 方案与推荐分段

A（推荐）：共同 source 请求/报价/恢复基础 + 首个普通文生图完整闭环；第二段正式主体锁定底板复合模式；第三段正式参考图编辑及三主体质量验证。每段包含正常 UI、资金、输出与恢复，不单交半协议 fixture。未通过的模式明确禁用，父票仍未完成。

B：一次同步三模式。重复变更公共 Provider、SKU、输入资产和质量合同，验收组合过多，问题很难归属。不推荐。

C：仅补工程 recorder 或复用现直调成功 PNG。可证明局部算法，无法证明正式资金/Task/保真目标，不作为本次交付方案。

A 的实施顺序：身份/请求与报价基础 → 普通文生图完整产品闭环及独立审查 → root 真 Web/收费/恢复验收 → 两个后续模式各先公共合同设计/发布，再消费与质量验收。后两个模式在此定义目标和依赖；其确切 provider/capability/价格须另冻结后写实施计划，不擅自扩当前公共 runtime。

## 4. Owner 与授权边界

BFF（现有 Next→Go 链）从验证过的 HttpOnly session JWT 取 principal_user_id（payer_user_id 恒等于此 initiating principal），不接收 UI user/app/account 的权威声明。App ID 来自服务配置。个人 principal account、组织 tenant、实际 payer account 三个字段独立；任何 alias/混用必须拒绝。

当前项目权限在产品库与正式 Identity Context facts 交叉验证；组织 membership 不从本地伪造 claims 补齐。Context 使用 SDK `identity.context.resolve`，principal/app 服务端填入，tenant/source 是明确待验证提示。resolved=false、payer known=false、组织请求却只解析成 personal：提示选择/未验证，0 usage/0 Task，不默默换付款人。

root 裁定：首链先以真实 personal 完成端到端，不让组织资源缺失阻塞该实现；组织能力仍须实现正式 Context resolve + live E7 payer delegation 的 Bill 校验。Context 返回匹配预期的 payer_ref/source，Bill 验证本人合法成员/权益；无合法 delegation 则组织路径关闭且组织 Gate 未完成，不能以 personal 代验组织。仅从 Billing 账户列表任选组织也不能证明工程与 payer 的业务关系。个人链使用 JWT 中验证的 account_id 与 Identity personal payer 事实一致性，未配置/未知不能 mint 一个账户补齐。

项目为共享组织时，首版 source run/资产/费用默认只供原 initiating user 操作及读取；同组织其他成员不自动获取另一 user 的 Upload owner 或账事实。共享读取/代办须另有正式 delegation。列表可隐藏他人 run，不替换 payer_user_id 来读取。撤权后禁止新 usage/确认/主动调用；已在 Task 中冻结 hold 的结算由 Task 继续，产品不截停资金 owner。

服务端重新验证工程归属/允许发起角色/非删除非归档状态，再新增请求；每次读取仍验证会话与工程权限。另一 stored scope 404，用户提交冲突身份字段 403/400；重复/大小写 alias JSON key、nested params duplicates、重复 query 字段、尾随 JSON 一律拒绝。GET 不产生 usage/quote/Task/retention 写。

个人工程资源仍用既有 personal_default/account 归属，不能因Context缺组织membership而阻断已验证个人所有权；组织工程才使用实时Context membership/delegation。个人payer以Identity验证account与Bill成员/权益验证，不能要求不存在的组织delegation。平台普通 app credentials 仅 Go 服务持有。UI 没有平台 token；BFF 不安装 Billing operator/mint key、ModelXing key 或 OSS key。BFF 负责 usage+source_quote；hold/capture/release 仅 Task source owner 执行。此产品不新增 refund 入口、不直接调用付款 hold 以抢 Task owner。

## 5. 新产品请求协议与数据库

使用现有产品 SQLite、迁移体系和进程；无新服务/端口/数据库。新增 `product_source_runs` 和 `product_source_reference_outbox`，不批改历史 text/bg/quote/output rows。旧 job 留旧路由；新 UI 首版复用文字卡片但调用显式 `/projects/{id}/source-image-runs` 资源，不能根据 malformed funding_version 回退到旧 decoder。

`product_source_runs` 主键 run_id；唯一 `(app_id,principal_user_id,project_id,request_key)`；Task ID、usage ID 建唯一绑定约束（空值使用 nullable）。字段冻结：schema/owner_version、storage_tenant、project_id、principal_user_id、principal_account_id、payer_user_id、payer_account_id、payer_kind/source、授权 context 摘要/查询时间、mode、provider/profile、model、capability、quantity、size、prompt/params canonical JSON/hash、business_ref、request_key、usage_key、task_key、pricing_version。输入模式另冻结 input asset/version/hash、mask hash、coverage version；第一段不接受这些字段。

独立 frozen quote：usage_id、quote_id、unit_price_minor、amount_minor、quantity、quoted_at_unix、expires_at_unix、currency；全部 Go int64，先检查非负/乘加溢出。浏览器展示金额用十进制字符串及格式化 CNY 文案，确认仅提交 quote_id + quote_fingerprint，不从 JS float 回填钱。所需上游 wire 保持 canonical integer，不改正式 SDK 类型。

另存 confirmation（确切 quote hash、原确认 user、时间）、phase/retry_at/attempts、submit claim、Task ID/观察状态、原 charge/hold/release IDs、Bill 最近成功观察 facts/时间、output asset/hash/MIME/size_bytes/reference/project（不重复保存供应商原 bytes）、result version、selected flag、quality report ID、deleted_at 与追加事件记录。quote 是不可变承诺，最新余额仅观察；本地金额不是余额或第二钱包。

request_key 为浏览器在用户点击“获取报价”前生成的稳定高熵键；Go 在任何远程请求前事务持久整个原始 tuple 并生成 run_id/usage_key/task_key。客户端丢响应用该 key lookup，服务端 scope 校验后返回原 run；同 key 异内容 409。明确“重新生成”必须用户另行动作、新 request key；未决原请求必须可见，不允许自动 fresh key。相同 prompt 不全局去重，避免把用户明确第二次生成错误恢复为第一条。

business_ref 固定 `product-image:run:<run_id>`；所有动作键从已存 run 生成一次，不从当前工程名/价格/可变状态重算。状态只能 CAS 前进，generic text/bg updater、retry、archive/unarchive 无权重置 submit claim/owner。多进程与重启依据 DB，不依内存 map。

## 6. BFF/API 与真实 SDK 调用

| 产品 Go 路径（Next `/api` 对应代理） | 行为 |
|---|---|
| GET `/api/v1/projects/{id}/source-image-capabilities` | 只读配置与可信 context 可用性；逐模式 ready/disabled/reason，绝不将未接线显示可生成 |
| POST `/api/v1/projects/{id}/source-image-runs` | 请求 `{request_key,mode,prompt,size}`；先落 intent，再 Bill GET-first/usage/quote；不产生 Task |
| GET `/api/v1/projects/{id}/source-image-runs/by-request?request_key=...` | 当前登录 scope 下找原 run，无自动恢复写 |
| GET `/api/v1/projects/{id}/source-image-runs[/{run_id}]` | 历史/原结果/最近费用观察，只读并 no-store |
| POST `.../{run_id}/confirm` | `{quote_id,quote_fingerprint}`；重验完整上下文+当前项目、GET 原 Bill facts、原 quote 未失效；CAS 确认并触发原 Task 提交 |
| POST `.../{run_id}/reconcile` | 只恢复原 usage/quote/Task/输出映射；有界 backoff，无新生成键 |
| POST `.../{run_id}/cancel` | Task 未创建前本地 CAS 阻止提交；已尝试提交先 lookup，再原 source_cancel；未知不能宣布取消/释放 |
| POST `.../{run_id}/select` | 只选择原已证实输出，记录质量语义；不重新登记另一 asset/不生成 |
| GET `.../{run_id}/content`、`.../export` | 原 scoped durable asset 与引用验证后提供实际 bytes/冻结结果报告；不生成 |
| DELETE `.../{run_id}/output` | 显式删除输出展示/选择，原 reference release outbox；不退款、不得宣称物理 object 已删 |

所有写 handler 自身验证 method。Cookie 代理写使用同源/Origin 防护，跨站不触发付费。confirm 与前端展示的 quote 严格绑定；confirm 不是调用一次 quotePort 重新定价。

正式 SDK operations：`billing.usage`、`billing.usage_lookup`、`billing.usage_get`、`billing.source_quote`；`task.source_submit/get/lookup/cancel/reconcile`（如提供 SSE 则仅 `task.source_events` scoped 代理）；`upload.durable_asset_get/download/reference_get/release`。输入/衍生结果后续增加 durable create/sign/complete/retain 所需操作，第一段不自己重传 Task 输出。

Bill source 请求字段：funding_version=`source_reservation_v1`、app_id、payer_account_id、payer_user_id；usage 再带 capability、quantity、pricing_version、business_ref、idempotency_key。lookup query 同 scope+idempotency_key；get scope+原 usage path ID；source_quote body scope+usage_id。响应完整验证其 tuple；不能以 quote_ref 文案代替真实 quote_id。

Task submit 顶层严格 `app_id,payer_account_id,payer_user_id,project_id,capability,provider,idempotency_key,params,billing`；billing 包含 funding_version、usage_id、usage_idempotency_key、quote_id、quantity、pricing_version、business_ref、unit_price_minor、amount_minor、quoted_at_unix、expires_at_unix。GET/lookup/cancel/reconcile/events 的 query 均完整 app/payer_account/payer_user/project；lookup 加原 Task key。不得 existing_hold/account_id/amount_cny/mode=hold alias。

Upload owner 固定 `(app_id,user,payer_user_id)`，不是组织 payer_account；SDK Caller 同一 principal。Task 新成功输出必须提供原 reference_id/project_id，消费者不猜 outputKey。图片代理只消费正式 Upload 返回的临时下载地址；不接受 browser URL，按显式配置的 OSS HTTPS host/region allowlist 校验、拒 userinfo/非标准端口/重定向、DNS 解析及实际连接地址安全固定，设置总时限与 body 上限（本 profile 原 PNG≤16MiB），流式校验实际大小/hash/MIME后才发布可用结果；签名URL不进日志/DB。可复用现有安全组件，但不能直接信任任意下游URL，开发本地 fake transport 与生产受限规则隔离。下载临时失败不把 ready 改 failed，不产生新资产。public runtime 尚未发布此字段时新 mode 判未配置，不能省略校验。

## 7. 报价、执行与未知恢复状态

`intent → usage_pending → quote_pending → quoted → confirmed → task_submit_pending → task_linked → output_ready/settlement_pending → succeeded` 为正常可观察阶段。`quote_expired/not_dispatched/cancel_pending/provider_unknown/asset_pending/settlement_unknown/review_required` 是具体事实，不都压成 failed。UI 展示 Task phase 与资金观察时间，不允许凭 HTTP200/图片出现标“已扣款/成功”。

usage 丢响应：GET 原 scoped usage_key；有事实绑定原 ID；明确 404 才可原 key+原 tuple 重试 usage，网络/权限/解析失败保留未知。quote 丢响应：GET 原 usage；已有 quote 冻结原返回（五分钟不延长）；明确未建 quote 才原 usage 再 source_quote。不存在新 quote key/静默重定价。quote 过期且证实未 hold/未派单后，仅显式新请求/重新报价再确认。

confirm 在事务留下原 quote 的确认与 submit intent。提交前先 original task.source_lookup；找到则映射，未找到可原 key+原 tuple 提交，原 Task 的全局同 usage 唯一绑定与 one-way claim 是唯一供应商防重复屏障。产品多协调者即便发送同键重复 submit，也不能生成第二个 Task；在本地仍 CAS 单协调者并有界超时。unknown 永不自动切换 provider/新 key/费用 owner。

Task 一旦 linked，产品只 GET 或显式 reconcile 原 Task；未配置真实 sink、派单 unknown、artifact GET失败、Upload故障、capture失联均沿原 Task 恢复，不重执行生成。Task 已成功时 BFF GET 原 Bill facts 核验 charged/amount/quote/charge ID 与原 tuple，再验证 Upload ready hash/MIME/size/owner/reference active/project；缺一个就 pending/invariant，不把缺字段补成成功。

若结果已 ready 而最新 Bill GET 不可达，保留“已有图、费用待核实”的已观察事实；不重收费，不把旧 charged 观察清零为免费。成功后的暂时服务失联不改历史 receipt，当前展示分开 last_verified 与 refresh_status。下载也不能偷偷 release/refund。

读纯快照；有界现有进程恢复 worker 或显式 reconcile 驱动写恢复，指数 backoff 上限60秒并带 jitter，每轮有限 budget/取消；不在 GET handler 轮询 Execute，不额外启动 daemon。重登按原 project/run URL 和服务端历史恢复，不依 localStorage 作为权威。

取消与确认/提交 CAS 竞争仅一 winner。submit 曾尝试但 Task lookup 未知时只 cancel_pending；查到后原 cancel。仅 Task 可以证明未派单或供应商明确终态并 release；产品不通过 refund 代替 release。已 dispatch unknown 不给“已取消未收费”假承诺。

## 8. Asset 引用与删除 outbox

新 Task 输出先验证原 active reference，原子保存本地 output association；select 复用同 asset，不重复 RegisterAsset、不再上传一份。输出的 input/result version、质量与 Bill IDs 均绑定不可变 run，交接回流不得混入另一工程/新需求版本。

root 已批准：归档是可恢复隐藏，保留引用、允许历史读取，禁止新生成；不因为 status=archived 释放不可复活引用。明确删除输出/输入才 tombstone+outbox。永久删除工程需要独立明确终结操作，不能借通用 PATCH status 偷触发物理资产删除。

outbox 同库事务写原 app/user/asset/project/reference、action=`release`、reason/原 run/output ID、state/attempts/next_retry/error；唯一 `(app,user,asset,project,reference,action)`。先 `upload.durable_reference_get`，active 才原 release；released 视幂等完成。404/传输未知不得假完成，审计保留并可控重试；只有已验证的显式终态可完成。不调用 durable_delete 误删共享资产；Upload 在最后引用 release 后按30天 retention 自己清理/归还 quota。

结果晚到：删除的是本地输出意图，不能取消 Task financial owner。删除事务留下 run tombstone；Task 后来给出原 reference 时同事务登记该 identity 并排 release outbox，不能让 worker重新选中输出或创建替代引用。已 release reference 不重新 retain；用户恢复删除不能复活，需正式独立新引用设计，不在首版暗加。

永久删工程与 in-flight run/跨 app handoff 必须区分：已有有效下游引用由下游 owner 负责 release，产品不撤别人的引用。旧 assets 无durable owner保留旧API；不得通过新reference推定旧文件合法。旧 ready 输入 adoption 须 Upload 完整 owner/实际字节/hash/size/状态CAS证明；已deleted旧 handledmetal保持deleted，root重新授权新输入版本。

## 9. 三模式供应商/价格/质量合同

| 模式 | 输入与供应商事实 | 付款与可交付语义 |
|---|---|---|
| text_generate | 首段仅 `modelxing-qwen-image-2.0-v1`、model qwen-image-2.0、image.generate、n1、1024*1024；params恰好model/prompt/n/size；不收参考图 | 正式普通图/概念图，保真 not_applicable，不能叫精确商品图 |
| background_plate_lock | 供应商生成新底板，原输入/mask/coverage在正式复合Task里冻结与确定性合成，最终组合输出须durable后才capture；当前首个provider不支持此协议 | 独立 capability/provider/SKU 与公开复合输出协议待公共 owner；不能用已capture的底板等同交付成品/把未入账本地合成当Task成功 |
| reference_edit | 正式 JSON images/edits，冻结具体认证 model/profile、单张输入授权reference、尺寸语义与原图版本；无公共异步任务恢复可假设 | 独立报价，未冻结SKU/公开sourceadapter默认禁用；creative不称保真，通过样本后仅对应已证范围 |

普通生成的 root 已冻结 QA SKU 为 customer25 CNY minor、pricing_version=`qa-20261001-qwen-image2-cny25-v1`，supplier20 minor另记。只在明确授权 QA scope 配置，平台价格仍从真实 Bill source_quote 返回；产品不默认25、不以supplier20替代用户价、不把该SKU套编辑。此稿不宣称曾真实调用/扣费。

工程尺寸与原图1024方图不混淆。首段 UI 只展示实际1024×1024，不把旧 entry800×800默认为已生成800图。后续电商尺寸派生另记转换版本、裁切保护区与费用规则，未实现时禁止输出不存在的尺寸宣称。

授权三主体覆盖最低矩阵：carton/包装文字Logo；glass/透明反光；handled-metal/细边把手结构。每样本冻结来源、授权scope/ref、输入asset+bytesSHA/dims、maskSHA、coverageSHA、产品数量、预期文字/规格/结构关键点、目标背景与验收者。现有授权插画不能称真实实体商品照片；新增实物证据须另获授权。

保真底板阈值：保护 mask 内所有 decoded RGBA 像素逐点相同（0差异）；原 logo/text/spec/structure 区域都被批准mask覆盖；product_count精确相同、声明颜色/关键纹理逐区无变化。背景非保护区域必须存在真实变化且满足明确背景指令，不能原图回传冒充换背景。透明材质被原像素锁住可能保留旧背景，须逐例人工边缘/透射/接触阴影评审，不能因为像素0差异自动通过材质质量。

参考编辑逐项记录 product_count/logo_text/shape/key_texture/declared_color、背景指令、边缘瑕疵与尺寸；任一关键项fail即不保真，缺证unknown，整体相似度/OCR置信度不能替代结论。没有批准的参考区域与人审证据不生成保真pass。普通文生图另验证解码/尺寸、提示意图、文字明显错误/安全内容等，质量pass只针对普通图，不置subject_protected。

质量报告冻结 input/output hashes、mode/method/model/profile/pricing、prompt摘要、Task/usage/charge、检查轴及machine/human evidence ref、验收范围与限制。旧 recorder 仍按旧语义；新增 `product-source-quality/v1` 报告不得接受浏览器 bool pass/supplier_authorized。root可执行工程视觉核验；真实人类采用单独Human NOT_RUN，不伪造成完成。

负例必须包括：错字Logo、缺把手/结构变形、数量多/少、透明材质保留旧背景、蒙版漏规格区域、mask与输入版本错配、输出同原图、背景未改、尺寸误标、模型/价格变更后复用旧确认。不能只测成功样本。

## 10. 验证与发布/回退边界

本地正式合同测试用真实 Go handlers+临时file DB+明确 fake upstream，按发布 SDK 做 wire conformance；不得把 fakeProvider/合法PNG当真实供应商验收。HTTP故障点逐项：usage落账丢响应、quote落账丢响应、confirm丢响应、Task落库丢响应、多worker重入、重启、provider unknown、result先持久后sink中断、capture丢响应、Task成功但本地mapping丢响应、ref release响应丢失及删除/完成竞态。每项断言原ID/原键/唯一Task/唯一charge/no extra dispatch与最终事实。

权限负例：另一user/tenant/payer/project/app、membership撤销、JWT伪org字段、缺delegation、组织付款失败不能personal fallback、任意body金额/alias/duplicate、已有source误走legacy、旧genericupdate/retry不能清claim、GET不写。整数MaxInt64/溢出与UI精度拒绝必须覆盖。

前端测试覆盖：获取报价0 Task；明确显示金额/付款人/到期与单独确认；双击同run；quote变化/过期需新确认；source disabled不显可用；刷新/退出重登同run结果费用；普通图/保真/creative标签隔离；delete后不可复现选择。正常端到端由root运行现有Web入口，不能API脚本代替浏览器验收。

工程跑 `GOWORK=off go test ./...`、针对source/store/platform/httpapi race、`go vet ./...`；web现有test、typecheck/build（构建不启动服务）；SDK正式版本 conformance及依赖审查。实现前另交逐文件TDD计划，root审批后才能写产品。

部署只有root通过既有manifest/CI/service发布：SDK版本已发布→公共runtime独立PASS/正式旧服务装配→本产品默认关闭发布→限定QA scope启用→正常Web明确付费确认→核对供应商费用和Bill唯一账→跨重启重登恢复→逐模式Gate。不得另起QA Billing常驻平行钱账。

回退先关闭新创建/确认入口，保留已存在source run的只读/恢复与公共Task结算。不能旧binary接管source rows或把source请求送旧直调。加表迁移保值，历史行/schema对象/索引/FK/rowcount保留、失败事务rollback、file DB reopen验证。数据库快照回滚不得抹掉真实已收费事实；若旧binary不认识source回收/恢复，禁止直接回滚到它，使用兼容制品只禁新创建。

验收记录六列：Engineering（本地已验证/具体矩阵）、Browser（root真实操作）、Service（精确部署commit/入口）、Billing（原usage/quote/hold/charge/release与suppliercost）、Production（授权scope与发布状态）、Human（实际采用未做即NOT_RUN）。本稿当前只有Go基线工程PASS；其余新链均NOT_RUN/未部署。不得关闭父票或声称edit/fidelity通过。

## 11. root 已裁定与依赖交接

D1 采用 A；真实 personal 先完整闭环，组织代码必须正式 Context+E7+Bill 校验，无合法委托关闭组织路径，组织 Gate 未完成。
D2 归档保留 refs、禁止新生成；明确删除同事务 tombstone/outbox；永久工程删除必须显式终结语义。
D3 三主体冻结输入 hash/mask/证据、逐轴质量核验；root 执行技术与视觉验收，真实客户采用保持 NOT_RUN；已 deleted 输入不复活。
公共owner须发布精确SDK版本、Task output reference_id/project_id以及部署manifest合同；产品不能提前猜实现ready。后续底板复合/参考edit的provider/capability/SKU/输入授权和paid质量批次，root分别冻结后才实施。
