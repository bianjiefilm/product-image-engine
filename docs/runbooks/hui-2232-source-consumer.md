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

后续由 Root 按阶段门单独执行普通 Web/full services 与真验收。Task9 本身未执行 Task10；后续 Task10 的工程回归见下表。SDK 发布、consumer/deploy/生产门和父票关闭仍需独立真实证据。


## Task10 故障矩阵与已发布 wire 回归

所有以下行都是工程测试，不是远程 Billing/Task/Upload 或供应商验收。
新 `sourceRecoveryWire` 使用实际产品 Router、JWT、文件 SQLite、已发布 v0.1.1
SDK 和本地 httptest 上游。上游自己记录接受的原 key/quote/task，在提交 fixture
事实后实际断开 HTTP 连接；重开只关闭/重开产品文件数据库，上游事实继续存在。
该上游是独立脚本事实边界，不是公共服务实现。未把内部包接进消费端。
`source_contract_test.go` 独立冻结14个已发布 operation 的 method/path/service/
auth/idempotency/retry/query/payer/tenant/timeout/mutating/app字段，不能以目录存在
替代合同核对。SDK tag、checksum 和真实外部验收边界保持上文原值。

|故障项|实际测试锚点|测试层与限制|
|---|---|---|
|usage 已接受但回复丢失，再重开|`TestSourceRecoveryPublishedWireLostCommitReopen/billing.usage`|真实 Router/JWT/fileDB/SDK wire；原 key、commit1、未确认 Task0|
|quote 已接受但回复丢失|`TestSourceRecoveryPublishedWireLostCommitReopen/billing.source_quote`、`TestSourceQuoteFormalSDKUnquotedUsageRecovery`|真实 SDK wire；原 quoteID/25 minor/300秒窗口保留，不重新报价|
|confirm 回复丢失/重复确认|`TestSourceRecoveryConfirmResponseLostReopenOriginalReceipt`、`TestSourceRecoveryPublishedWireLostCommitReopen/task.source_submit`|实际产品HTTP执行确认后断连、产品fileDB重开并重复确认；原confirmation/hash/quote/Task不变、dispatch1；正式浏览器实网仍未验证|
|Task 接受但回复丢失|`TestSourceRecoveryPublishedWireLostCommitReopen/task.source_submit`、`TestSourceTaskFormalSDKLostHTTPResponseReopensOriginalTask`|真实 SDK wire；重开查回原 TaskID，submit/commit各1|
|双 Store claim/cancel 交错|`TestSourceTaskTwoStoresHaveOneCommit`、`TestSourceTaskMarkerAndCancelHaveOneWinner`、`TestSourceCancelWinsBeforeSubmitMarker`|真实 SQLite 事务/CAS；外部端口 fixture，未直接杀远程进程|
|lease 到期/旧 writer|`TestSourceClaimTwoStoresAndStaleWriter`、`TestSourceRunLeaseExpiryAndProtectedObservation`|真实 SQLite/CAS/时钟 fixture，原身份复用、旧 epoch 拒绝|
|provider_unknown|`TestSourceRecoveryPublishedWireLostCommitReopen/task.source_submit`|真实 SDK wire 返回未知状态，不能新 quote/Task/output|
|asset_pending/capture_unknown|`TestSourceRecoveryPublishedWirePendingOutputDoesNotUpgrade` 两分支|真实 SDK wire/重开；result保留，未生成可用 Output/charged receipt，select/export409|
|成功 Task 但 Bill 读取超时/unknown|`TestSourceOutputUnknownBillNeverBecomesSuccess`、`TestSourceOutputWrongOriginalProofAndFreshTimeoutPreserveReceipt`|产品 service/store + 外部端口故障 fixture；不能把未知资金升级成功|
|Upload 的 app/user/project/hash 不符|`TestSourceRecoveryWrongPublishedUploadFactsNoSelection` 四分支|实际 Upload SDK wire，其他端口 fixture；select/export503、原映射/金额守恒、GET无写。显式 select允许记录失败证明与 lease/retry元数据|
|已持久映射重开/重复观察|`TestSourceRecoveryMappingReopenArchiveRoundTrip`|实际 Router/JWT/fileDB+外部端口 fixture；原 asset/reference/quote/Task 不变，没有第二映射|
|删除先于输出/释放回复丢失|`TestSourceDeleteLateRefundedOutputReleaseLostReplyReopen`、`TestSourceExplicitDeleteBeforeLateOutputNeverRevivesHTTP`、`TestSourceDeleteBeforeLateOutputQueuesOriginalReferenceWithoutResurrection`|真实产品 store/service/Router；上游释放端口 fixture；tombstone和原 outbox 不复活|
|archive/unarchive|`TestSourceRecoveryMappingReopenArchiveRoundTrip`、`TestSourceOutputArchiveRetainsOriginalReference`|Router/JWT/fileDB；archive只读、select403；恢复 active保持原引用，无资金/Task变更|
|重新登录后的 GET 历史|`TestSourceRecoveryPublishedWireLostCommitReopen` 三分支、`TestSourceFileDBReopenJWTSessionKeepsOriginalFacts`|新实际 JWT/Router/文件重开；原 key/quote/Task保留、完整 Run与所有远端变更计数守恒；真实用户登出界面尚未验证|
|org撤销/无E7/错误当前Context|`TestSourceAuthOrgMissingRevokedOrForeignFactsAreHidden`、`TestSourceConfirmRevokedOrganizationMakesNoBillWrite`、`TestSourceReadCapabilitiesCannotGrantViewerWritePermission`|实际产品授权层+Context事实 fixture；无个人 fallback，未证明正式组织资金链已完成|
|GET与别名/body/token族攻击|`TestSourceStrictRequestsRejectBeforeMutation`、`TestSourceCookieOnlyDoesNotGrantGoAuth`、`TestSourceWireScopeAndOneAttempt`|实际 Router/JWT 与 SDK wire；拒绝前不调用资金/Task变更|
|DNS/peer/TLS/redirect/超大/挂起/MIME/hash/取消|`TestSourceDownloadRejectsUnsafeOriginsAndAllDNSAnswers`、`TestSourceDownloadRejectsChangedPeerOrUntrustedTLS`、`TestSourceDownloadRejectsContentFaultsAndCleansErrors`、`TestSourceDownloadHeadersHaveFiveSecondDeadline`、`TestSourceDownloadCancelRemovesReturnedTemp`|实际 downloader/临时文件、受控 DNS/TLS/HTTP故障边界；有界 deadline/清理，不是公网 OSS验收|
|关闭新建但保留旧记录|`TestSourceRecoveryPublishedWireLostCommitReopen` 三分支、`TestSourceActionsRemainOriginalAndNewFlagsDoNotBlockRecovery`、`TestSourceRecoveryObservesOriginalOutputWithCreationDisabled`|原数据库/JWT/恢复服务；GET守恒、无新增付费工作；原迁移/legacy测试继续执行|

首次已有绿色用例按新增回归记录，不捏造生产 RED。新增测试一度使用不存在的
helper 导致编译失败，随后误要求 select POST 不得写产品 bookkeeping，四分支
断言失败；均为测试 harness问题，原实际失败日志/receipt保留，未据此修改生产代码。
最终分别核对显式 POST 的原身份/输出/quote/charge守恒和随后 GET 的完整 Run无写。

完整工程门：`server/` 下 `GOWORK=off GOPROXY=off go test -p 1 -count=1 ./...`、
`go test -race -p 1 -count=1 ./internal/sourceimage ./internal/sourceflow ./internal/store ./internal/platform ./internal/httpapi ./cmd/server`、
`go vet ./...`、`go mod verify`；`web/` 下 `npm test`、本地锁定 tsc `--noEmit`、
`npm run build`。实际 exits/耗时/完整日志与精确源 SHA 在共享 Root作者交接，
不以此 runbook 文字代替命令 receipt。Web201测试、类型检查与build均actual0。
构建保留现有多lockfile workspace-root提示，没有为消除提示顺手改配置。

Task10 工程结果等待另一作者独立审查；Browser/Service/Billing/Production/
Human adoption仍为NOT_RUN，org/背景锁定/参考编辑合同仍未完成。
