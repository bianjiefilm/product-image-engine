# HUI-1739【平台生态 I0】产品图独立应用底座 — 实施计划

- 日期:2026-09-19
- 仓库:`D:/Projects/product-image-engine`(新立项,main=be2f55a)
- 分支:`hui-1739-i0-foundation`(本地提交,不 push)
- 性质:只做可运行应用底座 + 平台接入;生成能力(上传/保真/背景替换)由后续 FEAT 票实施。

## 1. 事实输入(契约,只读)

- `public-ai@origin/main:docs/integration-guide.md`:标准路径 = 产品 BFF-only;七服务 loopback 18101–18107;内部头 `X-PilotSeaView-Internal-Token` + `X-App-ID`;`*_APP_TOKENS` 每 app×每服务一把;legacy 共享 token 禁用。
- `examples/bff-identity-billing/`:identity 端点 `/internal/v1/identity/{login,refresh,session/resolve,revocations}`;billing `/internal/v1/billing/{accounts/ensure,ledger,hold,spend,refund}`;`Refresh 必带 app_id`。
- `internal/identity/token.go`(读源码核实):JWT = **EdDSA(Ed25519)**,claims `iss/sub/aud/iat/exp/jti/typ="access"/tenant_id/account_id/roles/scopes`,header `kid`;JWKS 为 OKP(Ed25519) `/.well-known/jwks.json`;校验 `aud=app_id` + `iss` + exp 必填。
- `docs/adr/0001`(HUI-1723):产品图应用私有"商品与生成工程"域;standalone 是一等来源(无 order_id 必须可用);登录身份≠租户成员资格≠资源授权≠付费权限;禁止跨应用读写他人 SQLite;新产品禁止 legacy token 路径。
- `docs/public-services-checklist-v3.1.md`:前端只打产品 BFF;BFF 走 loopback;产品 env 键 = `PLATFORM_*_BASE_URL` + `PLATFORM_*_TOKEN`;task/upload/notify 永不公网。
- `docs/order-ecosystem/2026-09-19-hui-1723-repos-sha-inventory.md` §4:平台服务配置键名清单(只键名,值不入库)。

## 2. 架构拍板(按工单)

```
浏览器 ──只打 /api/*──▶ Next.js BFF(18320, src/app/api/* route handlers)
                            │  服务端仅 loopback,带 PRODUCT_INTERNAL_TOKEN
                            ▼
                     Go server(127.0.0.1:18220, net/http + modernc.org/sqlite)
                            │  平台密钥只在 server env
                            ├─▶ platform-identity :18101(登录/刷新/JWKS 校验,真实实现)
                            ├─▶ platform-upload :18104(client+配置+fail-closed)
                            ├─▶ platform-task   :18103(client+配置+fail-closed)
                            └─▶ platform-billing:18102(client+配置+fail-closed)
```

- BFF 持有的是产品内部凭据(PRODUCT_INTERNAL_TOKEN)与会话 Cookie(httpOnly);平台密钥(`PLATFORM_*_BASE_URL`/`*_TOKEN`)全部收敛在 Go server env,浏览器永不接触。web 的服务端只调 Go loopback,满足"页面只打 /api/*"。
- 端口:Go `127.0.0.1:18220`,Next `18320`(避开 18081/18101–18107/18200)。

## 3. 领域模型(原生 SQLite,每行带租户作用域)

- `projects`:id, tenant_id, name, status(draft/active/archived), usage_kind, width_px/height_px, source_type∈{standalone,order,campaign}(可空,无 order_id 必填), source_ref, created_by(principal), created_at, updated_at
- `project_inputs`:引用平台 asset_id + 本地快照元数据(name/size/content_type)
- `project_versions`:version_no, platform_task_id, platform_asset_id, status
- principal/tenant 来自平台身份(JWT `sub`/`account_id`/`tenant_id`),本库不建用户表。tenant = `tenant_id` claim,空则回退 `account_id`。

## 4. 配置门(核心不变式)

- env 缺失或服务不可达 → HTTP 503 + 中文原因;禁止回退共享 SQLite/假余额/假生成。
- `ECO_BILLING_ENABLED`(默认 off)、`FEATURE_GENERATION_ENABLED`(默认 off)分别控制收费与生成。
- 错误响应统一 `{"error":{"code","message"}}`;鉴权失败 401;开关 off / 服务未配置 / 不可达 503(中文 message 区分)。

## 5. 任务分解(TDD 先行)

1. `.gitignore` + 计划文档(本文件)。
2. `server/` Go 模块:`go.mod`(modernc.org/sqlite + golang-jwt/jwt/v5)+ `internal/config`(env 解析、门控矩阵,先写测试)。
3. `internal/store`:embed 迁移 `0001_init.sql`;projects/inputs/versions 仓储;测试含跨租户不可见断言。
4. `internal/platform`:identity client(login/refresh/revocations + JWKS/EdDSA 校验器,缓存 kid,未知 kid 重取一次);upload/task/billing client 骨架;测试用 httptest 桩。
5. `internal/httpapi`:mux + 鉴权中间件 + auth/projects/inputs/versions/billing/readyz;测试覆盖 鉴权失败 401 / 服务不可达 503 / 开关 off 503 三态。
6. `cmd/server/main.go` 装配。
7. `web/` Next.js 15:app router;BFF 路由(auth login/logout/session/refresh、projects CRUD、inputs、versions、billing balance);页面(登录→项目列表→新建/打开→详情:用途/尺寸/输入/任务费用事实/返回来源入口);vitest BFF 三态测试;`npm run build`。
8. `deploy/` 样例(全占位值)+ 根 README(运行说明+架构图+键名清单+开关矩阵)。
9. E2E(本机):最小身份桩仅放 `_reports/hui-1739-i0/stub/`(不入生产代码路径,只补登录链路,不伪造 upload/task/billing 成功);步骤证据 tee 入 `_reports/hui-1739-i0/e2e/`。
10. 自查 code-review:grep 全仓无真实 token;`go test ./...` 全绿;`npm run build` 通过;提交(带 HUI-1739,不 push)。

## 6. 验收对照(工单 E2E)

- 干净 DB 启动→登录→创建无订单工程(standalone)→保存→退出重登恢复 ✔ 计划 §9
- 第二账号看不到别人工程 ✔(store 租户隔离测试 + E2E)
- 平台服务关闭/配置指空 → 受限动作显式报错,不伪造成功 ✔(三态测试 + E2E)
- `go test ./...` 全绿;BFF 三态路由测试;`npm run build` 成功 ✔
