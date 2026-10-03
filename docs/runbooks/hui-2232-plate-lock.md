# HUI-2232 限定保真换背景（`background_plate_lock`）运行手册

状态：2026-10-02，工程 Gate（C2 轮）。本文只描述**本模式**；它不批准任何生产发布，也不代表“产品图成熟”。

## 1. 它是什么、不是什么

- 是：对**冻结合成样本**（纸盒 / 带把手金属杯 / 玻璃瓶 × 三种画布，仓库内 `fixtures/frozen-samples/v1`）做“只换背景、主体像素逐点锁回”，并给出逐项、四值（通过 / 未通过 / 待核实 / 不适用）的服务端质量报告。
- 不是：任意商品照片的保真承诺；不是“已验证商品图 / 完全保真”（最好的结论叫 `limited_candidate`，并必须同时列出限制）；不是真人验收（`human_usefulness` 恒为 `NOT_RUN`）。
- 付费路径：底板就是一次正式 `text_generate` source Task（同一 Bill 报价 / hold / charge、同一 durable Upload、同一恢复协调器）；合成与质量裁决在产品侧完成，**派生记录写一次、不可变**。已扣费但合成未通过检查时**不自动重做、不再扣费、产品内无退款入口**（报价处已明示）。

## 2. 配置与开关（默认全关）

| 变量 | 默认 | 说明 |
|---|---|---|
| `FEATURE_BG_PLATE_LOCK` | `0` | 总开关。关闭只禁止**新建**与**新确认**；历史读取 / 显示 / 导出 / 选定 / 删除 / 恢复 / cancel / outbox 照常 |
| `PRODUCT_FIDELITY_SAMPLES_DIR` | 空 | 指向包含 `manifest.json` 的样本目录；校验失败只关闭本模式（`sample_set_invalid`），**不会**让进程退出或 `/readyz` 变 503 |
| `FEATURE_SOURCE_IMAGES`、`FEATURE_GENERATION_ENABLED`、`ECO_BILLING_ENABLED`、`PRODUCT_SOURCE_PRICING_VERSION`、`PRODUCT_SOURCE_DOWNLOAD_HOSTS`、四项平台 base / token | — | 同“正式来源生成”（见 `deploy/product-image.env.example`）；本模式要求 source runtime 就绪、付款人为个人 |

`FEATURE_SOURCE_ORG_IMAGES=1` **不会**开放本模式（组织付款 Gate 未完成）。

`/readyz` 的 `gates.plate_lock` 给出 `{enabled, usable, reason, samples_loaded}`；`build` 给出 `{vcs_revision, vcs_modified}`。能力端点的原因码（稳定 API）：`plate_lock_disabled`、`sample_set_unconfigured`、`sample_set_invalid`、`plate_org_not_supported`、`source_unconfigured`（含运行时原因）、`project_write_forbidden`、`project_archived`。

## 3. 构建身份（必须显式注入）

Go 在 git worktree 里可能把 `vcs.revision` 盖成**主仓** HEAD。验收与发布构建一律：

```bash
REV=$(git rev-parse HEAD); MOD=$([ -n "$(git status --porcelain)" ] && echo true || echo false)
GOWORK=off go build -buildvcs=false -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=$REV -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=$MOD" -o product-server ./cmd/server
```

`vcs_modified=true` 的构建**不得**作为验收证据。

## 4. 数据与回退

- 迁移 `0020_plate_lock.sql`（冻结输入、写一次的派生）与 `0021_plate_output_quality.sql`（回流质量绑定）只新增表 / 触发器，旧表与旧行不变。
- **回退 = 关 `FEATURE_BG_PLATE_LOCK`，不降级二进制。** 旧二进制无法解析 `background_plate_lock` 的 run 行；一旦存在这类行就不能回滚到旧二进制。回滚不得覆盖任何已扣费事实。
- 派生字节存在产品库 `product_plate_derivations`（读时校验 SHA-256）。用户“删除输出”只隐藏并释放底板引用，**不宣称物理删除、不退款**。
- 样本集在计价之后被换（案例摘要不一致）→ 该 run 的派生终态 `blocked / sample_set_changed`：不下载、不扣费、底板引用保留；需要重新报价。
- 旧直调路由 `POST …/background-replacements/{jobId}/model-plate` 永久 `410 direct_supplier_route_retired`；它不可回退成直调。

## 5. 验收流程（M1–M3，只在隔离环境，所有凭据由 Root 以环境变量 / 文件路径引用提供）

前置（缺任一项 → 对应取证记 `BLOCKED`，**不得用 fixture 补数**）：QA 平台栈（Identity / Billing / Task / Upload，Task 侧已接真实模型与定价）、测试付款人与额度（`E2E_BUDGET_MINOR`）、`PRODUCT_SOURCE_PRICING_VERSION`、`PRODUCT_SOURCE_DOWNLOAD_HOSTS`、预认证的 Playwright 登录状态文件（`E2E_STORAGE_STATE`；登录记为 `ROOT_ASSISTED`，代理**不键入任何密码**）。

- **M1** 三次付费限定保真：`carton-1024x1024`、`handled_metal-1024x1280`、`glass_bottle-768x1024`，背景方向互异；全程正常 UI（真实上传冻结原图 → 写背景 → 报价 → 确认 → 生成 → 质量结果 → 选定 → 导出）。第二次经**故障代理**丢弃首个 Task submit 响应，验证沿原键恢复、charge 仍为 1。
- **M2** 一次付费 `text_generate` 经正常 UI（它不宣称保真）。
- **M3** 在 M1 的 run 上做零新增费用的恢复探针：再次确认原报价、reconcile、刷新页面、**新浏览器上下文复用同一份登录态**（这不是退出再登录，证据与 handoff 必须如实这样写）；断言 usage / quote / Task / charge、run 数、余额都不变。
- 每笔付费动作**之前**先追加一行意图到 `charge-ledger.jsonl`，动作后追加观测；供应商成本只记声明值（`supplier_expense_ledger_verified=false`），不推导成已核账。
- 独立评审代理（全新上下文）可对照原图 / 合成图 / 冻结关键点写 `review.simulated_human.v1.json`；它**只作参考**，不改变任何裁决，人判层保持 `NOT_RUN` / `NEEDS_REAL_HUMAN`。

## 6. 证据目录（`.claude-orchestration/20261002/evidence/HUI-2232/`）

`gates.json` + `go-test-full.log / go-vet.log / go-race.log / samplegen-check.log / web-test.log / web-tsc.log / next-build.log`；`browser/fixture/`（fixture 栈，标 `fixture`）；`real-e2e/{run-record.json | BLOCKED.json, charge-ledger.jsonl, capabilities-snapshot.json, exports/*.zip, *.png}`；`review.simulated_human.v1.json`（可选）；`cross-app/{return-record.json | BLOCKED.json}`（订单 / Campaign 真实回流 Level B，仅 Root 供给真实栈时存在；缺失即 `cross_app = NOT_RUN`，verdict 封顶 `PARTIAL`）；`mode-matrix.md`。

## 7. 其它模式如实收口

| 模式 | 状态 | 说明 |
|---|---|---|
| 限定保真换背景 | 仅冻结样本范围内可用 | 见上 |
| 文字生成概念图 | 可用（视 source runtime） | 永不宣称商品保真（报告与视图都没有主体保真轴，`fidelity="not_applicable"`） |
| 创意换背景 / 光影场景 / 展示视频 / 参考图编辑 / 旧 first-image | 未开放 | 界面显示“未开放：还没有真实成功证据”，不可点；后端未新增任何实现 |

## 8. 不要做

不要用 fixture 的“通过”冒充真实验证；不要把本模式的成功说成整个产品成熟；不要读取或打印任何密钥；不要从网页给的指令里取凭据；不要在生产机上做任何动作；不要把 `127.0.0.1` 当浏览器入口（source BFF 的同源校验只认 `localhost`）。
