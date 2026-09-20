# product-image-engine — 产品图独立应用底座(HUI-1739 · 平台生态 I0)

产品图生成独立应用(平台生态 I0):Next.js + Go/SQLite,复用 public-ai 身份/资产/任务/计费,
无订单可用,订单/活动来源可选续接。

产品图是 public-ai 平台的**平等消费方**(standalone 无订单语境是一等来源):登录身份来自
platform-identity,钱包与资金事实在 platform-billing,素材与任务事实在平台侧留痕;
本应用只私有自己的领域数据(制作工程/输入引用/输出版本)。本仓库是 **I0 底座**:
可运行应用 + 平台接入;生成能力(上传/保真/背景替换)由后续 FEAT 票实施,
所有受限路径 fail-closed,绝不伪造成功。

## 架构

```
浏览器 ──只打 /api/*──▶ Next.js BFF(web, :18320)
                            │  服务端仅 loopback + X-Product-Internal-Token
                            ▼
                     Go server(server, 127.0.0.1:18220)
                      net/http + modernc.org/sqlite(纯 Go,嵌入迁移,env 配置)
                      本地 SQLite = 产品私有域:projects / project_inputs / project_versions
                      (每行带 tenant 作用域;principal 来自平台身份,本库不建用户表)
                            │  平台密钥只在本进程 env
                            ├─▶ platform-identity  :18101  登录/刷新/吊销 + JWT(EdDSA/JWKS)本地校验  [必需,真实实现]
                            ├─▶ platform-upload    :18104  素材会话 client                    [client+配置+fail-closed]
                            ├─▶ platform-task      :18103  生成任务 client                    [client+配置+fail-closed]
                            └─▶ platform-billing   :18102  账本只读 client                    [client+配置+fail-closed]
```

- BFF(web)只持 `PRODUCT_SERVER_TOKEN`(web→Go 内部凭据)与会话 Cookie;
  `PLATFORM_*` 全部收敛在 Go server env,浏览器永不接触。
- 平台调用一律带 `X-App-ID` + `X-PilotSeaView-Internal-Token`(本 app 每服务专用 token);
  禁止 legacy 共享 token 路径。
- 领域来源建模:`source_type ∈ {standalone, order, campaign}` 可空 + `source_ref`;
  无 order_id 必须可用;详情页有“返回来源”入口,无来源不影响任何功能。

## 目录

```
server/   Go 模块(cmd/server + internal/{config,store,platform,httpapi})
web/      Next.js 15 app router(src/app/api/* 即 BFF;页面只打 /api/*)
deploy/   env 样例(全占位符)
docs/superpowers/plans/  实施计划
```

## 本地运行

### 1) Go server(:18220)

```bash
cd server
export PRODUCT_INTERNAL_TOKEN=dev-internal-token-please-rotate \
       PLATFORM_IDENTITY_BASE_URL=http://127.0.0.1:18101 \
       PLATFORM_IDENTITY_APP_ID=product-image \
       PLATFORM_IDENTITY_TOKEN=<IDENTITY_APP_TOKENS 专用 token>
go run ./cmd/server        # 默认 127.0.0.1:18220,库文件 ./data/product-image.db
```

配置不完整不会崩溃:`GET /readyz` 报 503+中文原因,受保护端点 fail-closed。

### 2) Web BFF(:18320)

```bash
cd web
export PRODUCT_SERVER_BASE_URL=http://127.0.0.1:18220 \
       PRODUCT_SERVER_TOKEN=dev-internal-token-please-rotate
npm install && npm run dev   # 开发:next dev -p 18320
# 生产:npm run build && npm start(同为 :18320)
```

浏览器访问 `http://localhost:18320` → 登录 → 工程列表 → 新建/打开工程 → 详情
(用途/尺寸/已选输入/任务与费用事实/返回来源入口)。

### 测试

```bash
cd server && go test ./...        # 单测:配置门控/存储租户隔离/平台客户端/HTTP 三态
cd web && npm test                # vitest:BFF 三态(鉴权失败/服务不可达/开关 off)
```

## 配置键清单(键名 + 占位值)

服务端(`server`,样例 `deploy/product-image.env.example`):

| 键 | 必需 | 说明 |
|---|---|---|
| `PRODUCT_SERVER_ADDR` | 默认 127.0.0.1:18220 | 监听地址 |
| `PRODUCT_DB_PATH` | 默认 ./data/product-image.db | 本产品私有 SQLite |
| `PRODUCT_INTERNAL_TOKEN` | 必需 | web BFF→Go 内部凭据 |
| `PLATFORM_IDENTITY_BASE_URL` | 必需 | 如 `http://127.0.0.1:18101` |
| `PLATFORM_IDENTITY_APP_ID` | 必需 | `product-image`(JWT aud) |
| `PLATFORM_IDENTITY_TOKEN` | 必需 | identity `IDENTITY_APP_TOKENS` 本 app 专用 |
| `PLATFORM_IDENTITY_ISSUER` | 可选 | 配置后本地校验 JWT iss |
| `PLATFORM_UPLOAD_BASE_URL` / `PLATFORM_UPLOAD_TOKEN` | 生成开启时必需 | `:18104` |
| `PLATFORM_TASK_BASE_URL` / `PLATFORM_TASK_TOKEN` | 生成开启时必需 | `:18103` |
| `PLATFORM_BILLING_BASE_URL` / `PLATFORM_BILLING_TOKEN` | 计费开启时必需 | `:18102` |
| `ECO_BILLING_ENABLED` | 默认 0 | 收费开关 |
| `FEATURE_GENERATION_ENABLED` | 默认 0 | 生成能力开关 |
| `FEATURE_PHOTO_UPLOAD` | 默认 0 | 产品照片上传开关(HUI-1697;off 时上传路由不注册=404 不可见) |
| `PRODUCT_PHOTO_MAX_BYTES` | 默认 20971520 | 照片上传服务端单点大小上限(20MiB) |
| `FEATURE_TEMPLATES` | 默认 0 | 产品图模板库开关(HUI-1705;off 时模板路由不注册=404 不可见;模板=确定性预设+参数引用集,零触发生成) |

Web(`web`,样例 `deploy/web.env.example`):

| 键 | 说明 |
|---|---|
| `PRODUCT_SERVER_BASE_URL` | Go server loopback 地址 |
| `PRODUCT_SERVER_TOKEN` | 同 `PRODUCT_INTERNAL_TOKEN` |
| `PRODUCT_COOKIE_SECURE` | HTTPS 部署置 1 |

## 开关矩阵(fail-closed)

| 状态 | 行为 |
|---|---|
| 登录/会话,JWT 无效或过期 | **401** `登录状态无效或已过期,请重新登录` |
| identity 不可达(JWKS 拉不到) | **503** `平台身份服务不可达,暂时无法校验登录状态…` |
| `FEATURE_GENERATION_ENABLED=0` | **503** `生成能力开关未开启(FEATURE_GENERATION_ENABLED=0)…` |
| 生成 on 但 task/upload 配置缺失 | **503** `…服务未配置(点名缺失键)` |
| 生成 on 但 platform-task 不可达 | **503** `生成任务服务不可达…(不伪造成功)`,且不落版本行 |
| `ECO_BILLING_ENABLED=0` | **503** `计费开关未开启(ECO_BILLING_ENABLED=0)…` |
| 计费 on 但 platform-billing 不可达 | **503** `计费服务不可达…(不伪造成功)` |
| `FEATURE_PHOTO_UPLOAD=0` | **404** 上传路由不注册(不可见,沿 size_adapt 语义) |
| 照片上传 on 但 upload 配置缺失 | **503** `素材上传服务未配置(点名缺失键)` |
| 照片格式伪造(扩展名/Content-Type 不作数) | **415** `unsupported_media_type` / 坏字节 **422** `undecodable_image` |
| 照片超限 | **413** `photo_too_large`(上限 `PRODUCT_PHOTO_MAX_BYTES`,默认 20MiB) |
| 同内容重复上传(同租户) | **200** 幂等返回同一资产引用,不重复登记不重复计费 |
| 照片平台登记失败/不可达 | **502** `upstream_rejected` / **503** `upstream_unavailable`,本地零落库 |
| `FEATURE_TEMPLATES=0` | **404** 模板路由不注册(不可见,沿 size_adapt 语义) |
| 模板含生成参数字段(prompt/model/…) | **422** `forbidden_field`(模板库只收确定性预设参数) |
| 改/删内置模板 | **403** `builtin_readonly`(只读;可派生为自定义) |
| 同工程重复套用同模板同版本 | **200** 幂等返回既有留痕,不重复写(首次 **201**) |
| 模板跨租户读写 | **404**(不可见,无串数据) |
| 内部凭据缺失/不正确 | **401** `产品内部凭据缺失或不正确` |

## 红线对照(public-ai checklist v3.1 / ADR 0001)

- 前端只打产品 BFF(`/api/*`);BFF→Go→平台,全部 loopback;task/upload/notify 不出公网。
- 每 app × 每服务专用 token;请求带 `X-App-ID` + `X-PilotSeaView-Internal-Token`。
- 钱包主账在 billing.db,本产品零资金账本(只读 ledger 展示)。
- principal/tenant 来自 identity 派生体系(usr_/acct_);本库不建用户表。
- 租户隔离:所有领域读写按 tenant 过滤,跨租户读取与“不存在”同型(404)。
