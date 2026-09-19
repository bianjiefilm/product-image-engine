# HUI-1745【平台生态 I1】产品图跨应用续接与成果回流 — 实施计划

- 分支:`hui-1745-i1-source-binding`(基线 main=59af1a0,I0 底座)
- 契约输入(只读,经 `git -C D:/Projects/public-ai show origin/main:…`):
  - `docs/contracts/order-handoff/v1/README.md`(冻结 v1 线格式与语义)+ `handoff.schema.json` + `receipt.schema.json`(order-receipt/v1)+ `SOURCE-PROFILE.md` + `source-profile-vectors.json`(三来源+降级向量)
  - `docs/contracts/app-registry/v1/README.md` + `sample-manifest.json`(target 解析/URL 白名单)
  - `docs/order-ecosystem/2026-09-19-hui-1732-permission-matrix.md`(E2)、`2026-09-19-hui-1734-directed-events.md`(E4)
  - `examples/notify-directed-events/main.go`、`examples/identity-asset-adaptation/main.go`、`examples/order-handoff-receipt/main.go`
- 对端(只读):`git -C D:/Projects/guanlan-order show origin/main:backend/internal/ecoproduct/ecoproduct.go`、`backend/internal/handoffv1/frame.go`、`migrations/0034_tool_receipt_events.sql`
- 指挥者预答已采纳:回执直 POST 来源登记 receipt target + 本地 stub 验签/字段/幂等(真实共测 deferred);非收费交接允许测试用合法 PNG 经 upload 客户端登记;绑定/快照/回执状态机全在 server Go,web 只加展示与触发按钮;不解除 HUI-875 Hold、不进生产 RC。

## 1. 设计定稿

### 1.1 契约解析 `server/internal/handoff`(新包)

按冻结 v1 README + SOURCE-PROFILE 扩展实现解析器(本仓不 import public-ai 模块——无代理依赖路径;在本包内实现同语义解析,共享向量复制为测试夹具钉住):

- 严格 JSON:≤1 MiB、重复键拒绝(token 级)、未知字段拒绝、null 拒绝、尾随内容拒绝、嵌套 ≤16 层。
- 二分规则:顶层含 `source_profile` 键 → profiled 路径;否则纯 v1(order)。
  - 纯 v1:必须含 `order_ref`+`stage_ref`,kind=order。
  - profiled:`profile_version=source-profile/v1`(其他 → `unsupported_profile`);`source_kind∈{standalone,campaign}`(`order` → `source_kind_mismatch`);`order_ref`/`stage_ref` 必须缺席(存在即拒绝);campaign 必含 `campaign_ref`,standalone 必须缺席。
- 字符串纪律:1–256 code points、非全空白(固定 White_Space 集合 U+0009–000D/U+0020/U+0085/U+00A0/U+1680/U+2000–200A/U+2028/U+2029/U+202F/U+205F/U+3000,不用 `\s`);description ≤4096;数组 ≤100(scopes 1–4 封闭枚举去重)。
- 时间:`YYYY-MM-DDTHH:mm:ssZ` 严格;`issued_at < expires_at ≤ issued_at+900s`;垃圾 expires_at fail-closed(视为已过期)。
- sha256/proof_digest:64 小写 hex;assets 每项 asset_ref/sha256/size_bytes(正)/media_type;actor.app_id=source_app;binding/gating 段必填。
- `TenantScopeMatches`:精确比对,任一侧空 → false(fail-closed,不 trim)。
- 错误码机器可读:`unsupported_schema_version`/`invalid_document`/`unsupported_profile`/`source_kind_mismatch`/`handoff_expired`/`tenant_mismatch`。

### 1.2 持久模型(migration 0002,全部 additive 新表)

- `source_bindings`:**UNIQUE(tenant_scope, source_app, source_ref, target_app, purpose)**——票面唯一键逐字落实(source_ref 取 handoff `source_project_ref`,target_app=本应用 app_id,purpose=制作用途)。任何绑定查询都走五元组,不凭 source_ref 单字段或工程 ID 反查。
- `handoff_snapshots`:UNIQUE(tenant_scope, handoff_id),`content_fp`=接受内容规范序列化 SHA-256,不可变(profile_json/content_fp 只写一次)。语义:同 handoff_id 同内容 → SNAPSHOT_SAME;同 handoff_id 异内容 → SNAPSHOT_CONFLICT(保留原快照,409);新 handoff_id → SNAPSHOT_NEW。
- `handoff_redemptions`:UNIQUE(binding_ref)——应用内一次性兑换语义:同 (binding_ref,tenant,content_fp) 重放 → SAME_RESULT 返回原绑定;异 tuple → 409。(平台 identity 真兑换=HUI-656 域,deferred。)
- `project_outputs`:用户明确选择的输出版本登记(版本行不可变;产出文件字节→sha256→经 upload client 登记→asset_id 引用;status succeeded + asset 事实落行)。
- `source_receipts`:UNIQUE(tenant_scope, event_id),payload_bytes 不可变;status pending→delivered/failed;重传只重发既有 payload(同 event_id、刷新帧时间戳/签名)。

### 1.3 端点(server)

| 端点 | 语义 |
|---|---|
| `POST /api/v1/handoffs/accept` | 解析校验→租户比对→兑换去重→快照(SAME/CONFLICT/NEW)→五元组 find-or-create 绑定+工程。只创建/恢复工程,绝不调用 TaskClient(测试断言零调用)。返回来源上下文(来源/素材/用途尺寸/需求版本/付款主体 principal_id/授权范围 scopes+capabilities+constraints)。NEW 且绑定已存在→返回 pending_snapshot+diff,不自动采用 |
| `POST /api/v1/bindings/{id}/adopt` | 用户确认差异后采用:仅移动 current_snapshot_id + 追加缺失输入;不改人工编辑字段、不动已选输出/版本 |
| `POST /api/v1/projects/{id}/outputs` | 用户明确选择输出版本:接收合法 PNG(测试用,E2E fixture 同源)→sha256→upload client 登记→落 project_outputs(不触发生成/扣费) |
| `POST /api/v1/projects/{id}/outputs/{outputId}/receipt` | 构造 order-receipt/v1(字段见 §1.4)→存 payload→POST 来源登记 receipt target(§8 HMAC 帧)→delivered/failed;同 output 重复调用→返回既有回执不重发新事件 |
| `GET /api/v1/projects/{id}/receipts` | 查询恢复:只读状态 |
| `POST /api/v1/receipts/{id}/resend` | 只重传既有 payload(同 event_id);回执失败绝不追加生成任务或扣费 |
| `GET /api/v1/bindings/{id}` | 绑定+工程+当前/待采用快照聚合(重登/重启恢复入口) |

### 1.4 回执线格式(order-receipt/v1 + §8 HMAC 帧,对端 guanlan-order 字段对照)

对端消费面:`handoffv1.FrameVerify`(X-Handoff-Key-Id/X-Handoff-Timestamp/X-Handoff-Signature,`HMAC-SHA256(ts\n source\n target\n event_id\n raw_body)`,±300s)+ `tool_receipt_events`(event_id UNIQUE 幂等、handoff_id、run_id、result_version、status、assets_json、payload_sha256)。故:

- `source_app`=本应用 app_id,`target_app`=绑定 source_app,`handoff_id`=快照 handoff_id,`principal_id`=快照 principal_id,`project_ref`=source_project_ref,`project_revision`=source_revision,`brief_version`=brief_version(来源版本),`run_id`=platform_task_id(占位生成链路时 `manual-<outputId>`),`sequence` 每 (绑定,run) 严格递增,`status=succeeded`,`assets=[{asset_ref=登记 asset_id, sha256, size_bytes, media_type}]`,`billing_fact_refs`=计费开启时从 ledger 匹配 task 的既有事实引用(否则空数组,绝不触发扣费)。
- 投递地址:本仓 `internal/appregistry` 加载 app-registry/v1 manifest(`PRODUCT_REGISTRY_MANIFEST` env,缺省内嵌样例;URL 白名单 https-or-loopback-http/无 query/fragment/userinfo),`ResolveTarget(target_app, receipt_target_id, "receipt")`。HMAC secret 经 `PRODUCT_RECEIPT_KEYS`(app_id:secret;…,专用密钥纪律,不入库)。
- E4(directed-event/v1 work.result_ready 经 platform-notify)为平台总线可选路径,本票不接(deferred);payload_ref 语义(引用+sha256,不带内部路径/长期下载密钥)在 order-receipt/v1 assets 中同型落实。

### 1.5 web/BFF

- BFF 新路由(只转发,不新增权限判断——服务端单一门控):`/api/handoffs/accept`、`/api/bindings/[id]`、`/api/bindings/[id]/adopt`、`/api/projects/[id]/outputs`、`/api/projects/[id]/outputs/[outputId]/receipt`、`/api/projects/[id]/receipts`、`/api/receipts/[id]/resend`。
- 页面:`/handoff`(来源上下文确认+接受并恢复/创建)、工程详情页加:来源面板(来源/素材/用途尺寸/需求版本/付款主体/授权范围)、待采用新快照 diff+确认采用按钮、输出登记+逐版本「回传来源」按钮、回执状态+重传、「返回来源」链接(注册表 ResolveTarget(source_app, return_target_id, "launch")+`?h=<handoff_id>`;来源不可用不阻塞保存)。

### 1.6 负例与回归矩阵(全部测试)

契约层:三来源向量(order/campaign/standalone)+降级(profile/v2 显式拒绝、纯 v1 原样接受)、错租户 fail-closed、过期 fail-closed、垃圾时间 fail-closed。应用层:错主体/错租户(403 同型)、过期交接(410)、旧需求版本回执(晚到 sequence 不推进/stale 拒绝)、重复提交(幂等 SAME)、同 handoff 异内容(409 保原快照)、standalone 伪造 order(order_ref/stage_ref 出现在 standalone → 拒;campaign 无 campaign_ref → 拒)、不支持 profile 明确拒绝、未绑定工程回执(409)、回执失败(不追加任务/扣费;重传只重发)。独立入口回归:重登/重启后绑定与工程仍在(GET bindings)。

## 2. 实施顺序(TDD)

1. migration 0002 + store 绑定/快照/兑换/回执仓储(唯一键/去重测试先行)。
2. `internal/handoff` 解析器 + 向量夹具测试。
3. httpapi accept/adopt/outputs/receipt 端点 + 无自动生成断言。
4. `internal/appregistry` + upload client RegisterAsset(PROVISIONAL 常量记录)。
5. web BFF + 页面 + vitest。
6. E2E(身份桩复用 I0 + 回执/上传 stub 新建,仅 _reports)。
7. 全量测试 + build + 提交(不 push)。

## 3. 不做(拍板记录)

- 不解除 HUI-875 Hold;产品图不进生产 RC/订单默认推荐。
- 真实生成质量归 FEAT-0198/0199;平台 identity 真兑换/真 upload 端点形状、E4 notify 总线、与 guanlan-order 真实共测 → deferred(报告列明)。
