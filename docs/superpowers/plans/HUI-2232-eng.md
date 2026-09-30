# HUI-2232 工程走查 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让受支持的 `text_image` 模式对着 HTTP 提供者走完任务、资产、摘要、导出、保真和记账六层，且不把合同图或回环地址写成商业真实出图。

**Architecture:** `engwalk.Gate.Submit` 对调用方给出的端点 POST 一次。200 且 PNG 签名与解码都通过时保存原字节并计算 SHA-256。四个轴从像素读出，再交给 `fidelity.Evaluate`；破损案例只在内存里清掉标记像素。账本按指纹保存第一份结果。真实供应商只在凭证存在时尝试一次，而 `config.Config` 没有成对 URL，所以不拨号。

**Tech Stack:** Go 1.26，标准库 `net/http`、`image/png`、`crypto/sha256`，以及本模块已有的 `fidelity`、`textimage`、`config`。

**Spec:** `docs/superpowers/specs/HUI-2232-eng.md`

## Global Constraints

- 只新增 `server/internal/engwalk` 与本规格、本计划。不改 `qualitygate`、`textimage`、`bgreplace`、`fidelity`、`lightscene`、`config`、`go.mod`。
- 合同图必须由测试用 `image/png` 编码，经端口 0 的 `httptest` 返回，再由本包 POST 取回。包内写死且不经网络的 PNG 不算通过。
- 状态在 `LivePassed` 为 false 时逐字是 `真实出图未完成`。`production_generation_passed` 与 `billing_passed` 保持 false。
- 供应商夹具成本 label 是 `fixture`、amount 是 `1.00`。用户价与补贴是另外两个字段，amount 为空。
- 同一指纹的第二次提交不发第二次 HTTP，记账只有一条。503、空正文、未知都不记账，且 `generation=false`、资产为空。
- 三个模型凭证都空时，真实尝试的错误类是 `配置缺失`。有凭证但没有成对 URL 时错误类是 `端点缺失`，且不拨号、不打印凭证。
- `127.0.0.1` 不得让 `LivePassed` 为 true。`ContractPassed` 在资产为空时不得为 true。相似度不得把 Logo 失败写成通过。
- 六层顺序：`task`、`asset`、`hash`、`export`、`fidelity`、`charge`。
- 不绑定 8080、18084、18101–18106。不新增 SDK。
- 测试命令：`cd server && GOWORK=off go test -count=1 ./internal/engwalk/`。不跑整个模块。
- 总指挥已指定在本会话内联执行。不要另开子代理。全绿后一次提交并开 PR，不合并。

## Review Focus

回环地址、空资产、相似度翻案、重复记账、凭证泄漏、无 URL 仍拨号。这些都写在下面的测试里。

---

### Task 1: 合同 POST 存下 PNG、摘要、导出和六层

**Files:**
- Create: `server/internal/engwalk/engwalk_test.go`
- Create: `server/internal/engwalk/engwalk.go`
- Test: `server/internal/engwalk/engwalk_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `NewGate() *Gate`；`(*Gate).Submit(context.Context, Submit) (Result, error)`；`ErrSubmitIncomplete` 文本 `提交不完整`；`ModeTextImage = "text_image"`；`StatusIncomplete = "真实出图未完成"`。`Result` 含 `Asset`、`SHA256`、`ByteLen`、`Export`、`Layers`、`ContractPassed`、`LivePassed`、`Status`、`Generation`、`ProviderCalls`。

- [x] **Step 1: Write the failing test**

测试用 `image/png` 画 4×1 的不透明图。标记像素 `(0,0)` 是 `0x11,0x22,0x33,0xFF`，`(1,0)` 是 `0x44,0x55,0x66,0xFF`，`(2,0)` 是 `0x10,0x20,0x30,0xFF`，`(3,0)` 是 `0x01,0x02,0x03,0xFF`。`httptest` 只在 POST JSON 的 `mode` 为 `text_image` 时写回这批原字节。

断言：存下的字节与服务器字节相同；测试自己 `png.Decode` 这些字节；SHA-256 与 `ByteLen` 等于服务器字节；`Export["sha256"]`、`Export["byte_len"]`、`Export["openable"]` 相同；`ProviderCalls==1`；第二次提交打到另一个会返回 503 的服务器时，那个服务器调用数为 0，第一台仍为 1；调用方改 `Asset[0]` 后再取同一指纹，库内字节不变。六个 `Layers` 名称按顺序为 `task`、`asset`、`hash`、`export`、`fidelity`、`charge`，且这次都为 true。`Status` 为 `真实出图未完成`，`LivePassed`、`ProductionGenerationPassed`、`BillingPassed` 为 false。提交前把三枚模型凭证环境变量设为空。

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test -count=1 ./internal/engwalk/ -run TestContractStoresServerPNG -count=1`

Expected: FAIL。包不存在，或 `Submit` 还没有取回服务器字节。

- [x] **Step 3: Write minimal implementation**

`postTask` 使用独立的 `contractClient` POST JSON。`openPNG` 先对签名再 `png.Decode`。成功时保存原字节。`liveAttempt` 在凭证全空时返回错误类 `配置缺失` 且 `Passed=false`。`commercialStatus` 在未真实通过时返回 `真实出图未完成`。`productFlags` 调用 `textimage.RealGenerationCompleted` 后仍返回两个 false。

- [x] **Step 4: Run test to verify it passes**

同一命令。Expected: PASS。

- [x] **Step 5: Commit**

不要单独提交红测试。全部任务绿了再提交一次。

---

### Task 2: 四个轴与破损 Logo

**Files:**
- Modify: `server/internal/engwalk/engwalk_test.go`
- Modify: `server/internal/engwalk/engwalk.go`
- Test: `server/internal/engwalk/engwalk_test.go`

**Interfaces:**
- Consumes: `Submit.Similarity`、存下的 `Asset`
- Produces: `Intact`、`Broken`（`fidelity.Report`）、`IntactAxes`、`BrokenAxes`。轴字段 `Logo`、`PackagingText`、`Spec`、`Structure`。

- [x] **Step 1: Write the failing test**

`TestContractStoresServerPNG` 传入 `Similarity: 0.99`。测试自己解码存下的字节，用规格里的四个颜色算出期望轴，再比对 `IntactAxes`。`logo_text` 只有 logo 与 packaging 都通过才是 `pass`；`shape` 等于 structure；`declared_color` 等于 spec。

破损案例的 `Logo` 为 `fail`，另外三轴与完好案例相同，资产里的 `(0,0)` 仍是标记色。`Broken` 的 `logo_text` 为 `fail`，`verdict` 不是 `pass`，`exact_product` 与 `deliverable` 为 false。`fidelity.BlocksVerifiedReceipt` 对这一份报告返回阻止，代码 `fidelity_failed`。完好报告的 `exact_product` 为 false。

`TestMissingMarkerIsLogoFailure` 提供没有标记像素的 PNG 和相似度 `0.99`。完好案例的 logo 轴与 `logo_text` 仍是 `fail`，不得变成 `pass`。

失败文案使用 `similarity flipped a logo failure into a pass`。

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test -count=1 ./internal/engwalk/ -run 'TestContractStoresServerPNG|TestMissingMarkerIsLogoFailure'`

Expected: FAIL。轴还没从像素读出，或相似度把失败写成通过。

- [x] **Step 3: Write minimal implementation**

`observe` 按规格颜色读四个像素。`stripMarker` 用 `image/draw` 复制后把 `(0,0)` 设为透明黑，再 `observe`。`evalCase` 把测量结果填进 `fidelity.Evaluate`，`SupplierAuthorized=false`，`Similarity>0` 时 `SimilarityOnly=true`，但不改轴。`ContractPassed` 还要求破损报告被 `BlocksVerifiedReceipt` 以 `fidelity_failed` 拒绝。

- [x] **Step 4: Run test to verify it passes**

同一命令。Expected: PASS。

- [x] **Step 5: Commit**

仍不单独提交。

---

### Task 3: 失败、未知和重复都不记第二次账

**Files:**
- Modify: `server/internal/engwalk/engwalk_test.go`
- Modify: `server/internal/engwalk/engwalk.go`
- Test: `server/internal/engwalk/engwalk_test.go`

**Interfaces:**
- Consumes: `Gate` 的指纹账本
- Produces: `ChargeLine`（`SupplierCost`、`UserPrice`、`Subsidy`）；`ChargeCount`；`Outcome` 为 `stored`、`failed` 或 `unknown`；`FailureClass`。

- [x] **Step 1: Write the failing test**

`TestFailedOrUnknownDoesNotStoreOrCharge` 覆盖 503、200 空正文、200 正文 `not-a-png`。每种都提交两次。服务器调用数为 1。资产长度为 0，`generation=false`，`ChargeCount=0`，`ContractPassed=false`。空资产却通过时失败文案为 `ContractPassed is true when the stored bytes are empty`。503 的 `outcome` 是 `failed`、`failure_class` 是 `http_503`。空正文的 class 是 `empty_body`。垃圾正文的 `outcome` 是 `unknown`，且不是成功。`charge` 层为 true，前五层为 false。

成功路径断言一条记账：供应商 `fixture` / `1.00`，用户价 label `user_price` 且 amount 不等于 `1.00`，补贴 label `subsidy` 且 amount 不等于 `1.00`。

`TestUnsupportedModeDoesNotCall` 使用模式 `nope`，服务器调用数为 0，`outcome=unknown`。

`TestIncompleteSubmitDoesNotStore` 对空 `Submit` 得到 `提交不完整`。

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test -count=1 ./internal/engwalk/ -run 'TestFailedOrUnknown|TestUnsupportedMode|TestIncompleteSubmit'`

Expected: FAIL。失败响应仍被当成存下的图，或第二次又发出了请求。

- [x] **Step 3: Write minimal implementation**

非 200 与空正文不保存字节、不记账，`generation=false`。非 PNG 的 200 为 `unknown`。指纹已存在时直接返回副本。不受支持的模式不调用 `postTask`。只有 `outcome=stored` 才追加那一条夹具记账。`charge` 层在“失败为零条、成功为符合规格的一条”时为 true。

- [x] **Step 4: Run test to verify it passes**

同一命令。Expected: PASS。

- [x] **Step 5: Commit**

仍不单独提交。

---

### Task 4: 真实尝试不把回环当通过，没有 URL 不拨号

**Files:**
- Create: `server/internal/engwalk/live.go`
- Modify: `server/internal/engwalk/engwalk_test.go`
- Test: `server/internal/engwalk/engwalk_test.go`

**Interfaces:**
- Consumes: `config.Load`；`liveClient`
- Produces: `liveAttempt(config.Config) LiveFact`；`performLive(context.Context, string) LiveFact`；`judgeLive(host string, status int, body []byte) LiveFact`。`LiveFact` 只有 `Passed`、`StatusCode`、`ByteLen`、`SHA256`、`ErrorClass`。

- [x] **Step 1: Write the failing test**

`TestLoopbackResponseIsNotLivePass`：`performLive` 对 `httptest`（GET 返回测试编码的 PNG）的 `Passed` 必须为 false，但状态码、长度、摘要仍记录服务器字节。`judgeLive("127.0.0.1", 200, png)` 与 `judgeLive("127.0.0.1:9", 200, png)` 的 `Passed` 也必须为 false。失败文案为 `LivePassed is true for a 127.0.0.1 host`。

`TestCredentialWithoutEndpointDoesNotDial`：把 `PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL` 设为 `cred-do-not-print`，另外两枚设为空。把 `liveClient.Transport` 换成一旦被调用就记失败的 RoundTripper。`liveAttempt(config.Load())` 必须 `Passed=false`、状态码 0、长度 0、摘要空、错误类 `端点缺失`，且 JSON 与错误类都不含该凭证。失败文案为 `secret leaked` 或 `dialed without a model URL`，不要把凭证打进日志。

合同测试已经要求三枚变量为空时 `Live.ErrorClass` 为 `配置缺失`，且合同结果的 `LivePassed` 为 false。

- [x] **Step 2: Run test to verify it fails**

Run: `cd server && GOWORK=off go test -count=1 ./internal/engwalk/ -run 'TestLoopbackResponse|TestCredentialWithoutEndpoint'`

Expected: FAIL。回环被当成真实通过，或有凭证时发生了拨号。

- [x] **Step 3: Write minimal implementation**

`pairedModelEndpoint` 恒返回空字符串：`Config` 上没有与三枚凭证成对的 URL。`liveAttempt` 因此不调用 `performLive`。`performLive` 只 GET 一次。`judgeLive` 在主机是 `127.0.0.1`、`localhost` 或 `::1` 时强制 `Passed=false`、错误类 `loopback`。可解码指 PNG 签名加上 `png.Decode`。

- [x] **Step 4: Run test to verify it passes**

Run: `cd server && GOWORK=off go test -count=1 ./internal/engwalk/`

Expected: PASS。不得使用缓存结果；`-count=1` 必须留在命令里。

- [x] **Step 5: Commit**

全绿后只 `git add` 本计划、规格和 `server/internal/engwalk`。提交说明只含 HUI-2232。然后推送并开 PR，不合并。

PR 标题逐字：`HUI-2232: walk the image gate against an HTTP provider`

PR 正文只提到 HUI-2232、测试命令和六层，并写明 `LivePassed` 为 false 时没有调用商业模型。
