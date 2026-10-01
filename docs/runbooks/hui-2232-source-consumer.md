# HUI-2232 正式来源生成运行手册

## 冻结合同与证据边界

本消费端仍固定 `github.com/bianjiefilm/public-ai/sdk/go v0.1.1`，已发布 tag
commit `a2ea6f76be305f494cc1bdc4e6d2e1ae4f81ca36`。
模块 checksum `h1:mgfXyVXxX7txo03fLaQB+fWWUNaEvRDas4krdnlpXlQ=`；
go.mod checksum `h1:uHyMORODJVufViOTtxU/bCxRHGGlQmizefLgRjN7xiM=`。
Root 在本阶段观察到的 Task 服务源码为
`909458537dc4c2f7ee8688c2a82fcef28a503dcd`。这是已观察运行身份记录，
不是新 SDK tag、此任务的远程部署或本作者实网验收证明。不可捏造新发布版本。

## 配置与能力

使用原来的单一服务进程、监听地址和产品专用 Store。四个既有平台 app token
必须非空、彼此不同；AppID 必须与 Identity AppID 一致。仅使用既有 Identity、
Billing、Task、Upload 配置，不引入供应商凭据、跨产品资金权限或浏览器 principal。
内部 base 必须为绝对 http(s)、合法 host/port，无 userinfo/query/fragment。
合法前缀 path 保留。再由已发布 SDK Prepare 验证 loopback 约束；装配不发送探测。
严格 URL 条件是 Root 本次批准的启动配置限制，不能归称为 SDK 原有全部要求。

`FEATURE_SOURCE_IMAGES=0`、`FEATURE_SOURCE_ORG_IMAGES=0` 为默认值。
新建另需 `FEATURE_GENERATION_ENABLED=1`、`ECO_BILLING_ENABLED=1`、明确的
`PRODUCT_SOURCE_PRICING_VERSION` 和 `PRODUCT_SOURCE_DOWNLOAD_HOSTS`。
样例价格版本、下载主机均留空。禁止 QA 默认价格、补余额、mint、seed 或自动开户。
价格来自原 Billing quote；人民币金额均按 minor unit 的精确整数事实展示。
不得把产品报价、客户扣费、供应商成本混为一个事实或把 unknown 显示为零。

目前唯一可报价模式为 `text_generate`，provider
`modelxing-qwen-image-2.0-v1`、model `qwen-image-2.0`、capability
`image.generate`、数量 1、尺寸 `1024*1024`。背景锁定和参考编辑仍为正式合同未配置。
普通概念图不证明主体保真、视觉合格或人类采用。

有效 read credentials 即装配 read/recovery clients，新建开关关闭不阻断旧记录恢复。
缺少价格版本只禁用新建；缺少下载主机仍恢复原 Task/Bill 事实和既有引用回收队列，
但 Assets Verify/下载失败关闭，`automatic_output_recovery_ready=false`，原因
`source_download_hosts_unconfigured`。非法下载主机拒绝整个 source 装配，不能隐式回退。
完整输出配置且单一协调器 CAS 启动握手成功，才在 Router/监听器可见前一次固定
输出恢复 ready。该元数据通过 capabilities 和 HTTP Run view 只读投影，不持久化。
`can_quote`/`can_confirm` 还依赖当前实际主体写权限、项目 active 和新建配置。
ready 不证明远程服务可用或真实产品验收。Source 未装配返回 503；既有健康策略保留。

## 恢复、删除与关闭

现有 single coordinator/CAS 每秒一 pass：整批最多 20 秒、至多 10 个 due Run。
复用原身份 Advance，只有确认过且明确 terminal/asset_pending 的原 Task 快照进入
ObserveOutput；不把未知供应商或未确认请求猜成成功。相同 pass 处理最多 10 项
既有 outbox。取消立即停止；原 lease、CAS、有限重试和原 key 保护继续生效。
GET 不调用 Advance、ObserveOutput、Drain，不写 Run、Bill、Task、Asset。
删除 tombstone 保留原支付事实；迟到输出仅按原引用释放，不退款、不重建或重发任务。
内容/导出继续 fresh Billing、原 Upload 归属和真实字节校验，不暴露签名 URL/token。

SIGINT/SIGTERM、HTTP 退出或协调器异常退出会取消服务请求 BaseContext 和恢复 worker，
关闭请求准入，执行有界 HTTP Shutdown、worker join 与 handler join。整个关闭阶段
最多 10 秒；仅全部已 join 才 CloseStore。HTTP Shutdown 超时会关闭连接，仍须证明
handler 和 worker 退出。任何未完成 join 将返回不确定关闭结果，main fatal 退出，
不执行隐式 defer CloseStore。此时 OS 回收进程资源，下次在原身份恢复；不能宣称
已 graceful/joined，也不能先关仍被使用的数据库。没有 detached recovery loop。

关闭新建的回滚使用上述 source flags off，不删已付费 Run、原 quote、Task 关联或 outbox。
保留有效 read credentials 与必要下载配置，允许原记录完成事实核对/引用释放。
旧 binary 不支持新 owner rows/outbox，不能据此宣称兼容接管，更不能恢复 SQL 快照
覆盖后来支付事实。需要停用 worker 时停整个原进程，并遵守上面的取消/join 顺序。

## 本地验证与真实验收

所有本地命令在 `server/` 下使用 `GOWORK=off GOPROXY=off`，保持依赖锁定：

- `go test ./cmd/server ./internal/config ./internal/sourceflow ./internal/httpapi -run TestSource -count=1`
- `go test ./... -count=1`（包含原 HTTP 回归）
- `go test -race ./cmd/server ./internal/config ./internal/sourceflow ./internal/httpapi -run TestSource -count=1`
- `go vet ./...`

真实 exit、精确源码 hash、完整日志和原 RED/setup 失败都在共享 Task9 作者交接记录中。
测试仅使用产品临时 SQLite、内存 HTTP RoundTripper 和既有 HTTP/JWT test fixtures。
不启动服务、连接真实供应商、写远程 env、开启端口或宣称普通用户端到端验收。

|验收列|本 Task9 状态|进一步事实要求|
|---|---|---|
|Engineering|本地自动测试，详见实际 receipt|精确提交独立审查|
|Browser|NOT_RUN|Root 授权后实际浏览器登录/新项目/quote/明确 confirm/结果/历史|
|Service|NOT_RUN|记录原进程/服务版本、启动/取消/join 和真实关联恢复|
|Billing|NOT_RUN|原 quote/hold/charge/release/refund 实际身份及精确金额|
|Production|NOT_RUN|部署身份与普通用户实际产物、授权内容/导出验证|
|Human adoption|NOT_RUN|真实人类视觉/主体要求评审，禁止 fixture 推为 PASS|

后续由 Root 按阶段门单独执行普通 Web/full services 与真验收。本任务不提前执行
Task10 故障矩阵、SDK 发布、consumer/deploy/生产门或关闭父票。
