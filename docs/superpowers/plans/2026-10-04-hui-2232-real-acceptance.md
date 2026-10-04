# HUI-2232 真实栈验收计划（M1–M3）——Root 执行轮 2026-10-04

BASE: `0c5ac0f`（origin/main，PR #45 合并点）。分支 `root/accept-20261004`。
本计划只做**真实验收与证据**；不重做已合入的工程（PR #33–#41 已合并并通过独评）。

## 设计裁决（Root 代答）

1. **运行架构**：本机 worktree 构建产品 Go 服务（buildinfo 显式注入，`vcs_modified=false`）+ Next 生产构建；平台 Identity/Billing/Task/Upload 使用远端已部署 QA 栈（120.26.69.202，源运行时 `TASK_SOURCE_ENABLED=1`、真实 ModelXing、OSS 真桶），SSH 本地转发 18101–18104。产品侧与 Task 侧必须看到**同一 billing**（runbook §1：同一 Bill 报价/hold/charge）。
2. **付款人**：新建本轮专属 QA 个人用户（`qa-source-<ts>@qa.pilotseaview.invalid`，口令仅存 run 目录 0600 config），billing `kind=personal` 账户 + entitlement + 600 分 scoped credit（25 分/张，24h 过期，幂等键带本轮 namespace）。不动生产用户行；QA 测试额度不是现金结算。
3. **登录态**：Root 以 QA 凭据走 `POST /api/auth/login`（产品 Web BFF → platform-identity）取得 `pia_access/pia_refresh`，落 Playwright storage state 文件（0600）。登录记 `ROOT_ASSISTED`；验收代理不键入任何密码。
4. **预算护栏**：`E2E_BUDGET_MINOR` 注入实际余额；driver 预算不足自动 skip 不硬花。`charge-ledger.jsonl` 每笔付费前 intent、付费后 observed；供应商成本只记声明值（`supplier_expense_ledger_verified=false`）。
5. **顺序**：先 `REAL_DRY_RUN_FIXTURE=1` 对 fixture 栈空跑 driver（免费，不算证据）→ 真跑 M1×3（第 2 笔经故障代理丢首个 Task submit 响应，验证沿原键恢复、charge=1）→ M2 文生图 1 笔 → M3 零新增费用恢复探针 → `verdict.mjs`。
6. **独评**：真跑后派**全新上下文**独立评审代理对照原图/合成图/冻结关键点写 `review.simulated_human.v1.json`（只作参考，人判层仍 NOT_RUN）。
7. **失败语义**：前置缺失 → `BLOCKED.json` 如实记录，不用 fixture 补数；基线红灯先区分既有/本次；不以"基线失败"免除新缺陷。

## 前置清单（缺一即 BLOCKED）

- [ ] 远端四服务 healthz 200
- [ ] SSH 隧道 18101–18104 通
- [ ] 产品 app token 四件（identity/billing/task/upload，从远端 /etc 注册表提取，只进进程 env）
- [ ] 定价版本注册（今日 namespace，25 分）
- [ ] QA 个人付款人 + 额度 ≥ M1+M2+裕量
- [ ] 冻结样本 `PRODUCT_FIDELITY_SAMPLES_DIR`（仓库 `fixtures/frozen-samples/v1`）
- [ ] `E2E_STORAGE_STATE`、`E2E_BUDGET_MINOR`、Chrome、playwright-core
- [ ] `/readyz` → `gates.plate_lock.usable=true` 且 build 身份一致、树干净

## 验收步骤

1. M1 三笔付费限定保真：`carton-1024x1024`、`handled_metal-1024x1280`、`glass_bottle-768x1024`，背景方向互异；全程正常 UI（上传→写背景→报价→确认→生成→质量结果→选定→导出），桌面+移动截图，导出 zip 校验。
2. M2 一笔付费 `text_generate`（不宣称保真）。
3. M3 恢复探针：原报价再确认、reconcile、刷新、新浏览器上下文复用同一登录态；usage/quote/Task/charge、run 数、余额不变。
4. 证据落 `/Volumes/MobileHD/Work/派诺生态/.codex-orchestration/2026-10-04-product-image/evidence/HUI-2232/`（run-record.json、charge-ledger.jsonl、capabilities-snapshot.json、exports/*.zip、截图、gates 日志）；仓库内写 runbook 结论文档（含 SHA256 索引）；关键制品作为 Linear 附件。
5. 四值结论汇总（通过/未通过/待核实/不适用），N/A 与 UNKNOWN 不混同。

## 本票之外、同栈搭车验收（独立票周期）

- HUI-2596：在真实 run 产物上走 selective revision（锁定商品/局部修订/并排/回退），证据独立落盘；代码缺陷走独立 worktree/红绿/独评/PR。
- HUI-2627：工作台 UI 真实走查 + r2 遗留 Minor N1 修复（计划成功句复述蒙版外替换，不把「供应商」放回主视图）。
- HUI-2746：资格未成立（HUI-2732 Todo、HUI-2737 In Progress）；只修 r1 Minor N1（多条引用共用同一对 price/color state），保持引用不变量，票面记录上游 ID 与未满足项。

## 不做

- 不动生产业务服务、不部署、不新增仓库/app_id/端口/数据库。
- 不改 public-ai 真源；不用桩接口关闭真实验收；不把 QA 额度说成现金结算。
- 不打印任何密钥；凭据只经环境变量/0600 文件传递。
