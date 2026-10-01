# HUI-2232 Personal Source Image Consumer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:executing-plans inline, task by task. Root prohibits spawning agents. Each task uses red/green evidence, explicit file commits, and root-owned independent review. This plan is not implementation approval.

**Goal:** 普通 Web 用户明确确认真实报价后，只通过原 source Task 取得真实普通文生图、durable Asset 与唯一 Bill 费用，并能重登恢复及明确删除引用。

**Architecture:** 产品同库持久原请求/原报价/Task映射与引用释放 outbox。独立 sourceimage 领域类型、store 持久化、platform 正式 SDK 适配、sourceflow 恢复协调、HTTP/BFF/UI 五个边界；旧 legacy job/updater 不接管新 owner。个人链先验收，组织实现正式验证但默认关闭，无 E7 关闭且组织 Gate 未完成。

**Tech Stack:** Go1.26、modernc SQLite、已发布 public-ai/sdk/go、Next15/React19、现有 Vitest；不增服务、端口、数据库、运行时依赖或供应商直调。

**Spec:** `docs/superpowers/specs/2026-10-01-hui-2232-source-quality-gate-design.md`，root已全文批准（本计划同时补入已批准的 Context read 收紧）；设计提交 `3d578ad24410a6a4d713bdb08c484bb8e3946e8b`，产品基线 `de9e7acda5d3203929bc2ff2c75de61dac9cbbd3`。

## Global Constraints / publication gate

- 限定个人普通 `text_generate`；provider=`modelxing-qwen-image-2.0-v1`、model=`qwen-image-2.0`、capability=`image.generate`、quantity/n=1、size=`1024*1024`；params只含model/prompt/n/size，prompt UTF-8非空≤16KiB。
- funding_version=`source_reservation_v1`，Upload=`durable_asset_v1`；新建默认关闭，组织另默认关闭；没有refs/mask/edit/底板/尺寸衍生/真实供应商密钥或服务。
- 本计划提交时，产品SDK仍v0.1.0。**Task2依赖升级之前，必须收到root给定的最终已发布SDK精确tag及对应公共runtime审查/发布事实。** 不编造tag，不把5a/source12源码当发布，不replace邻仓、不导internal、不新增假服务。Task1纯本地模型/迁移可在实现获批后独立执行；Task2及依赖其契约的后续步骤不能越过发布门。实施前将root提供的精确SDKtag与checksum补为本计划的发布记录；当前外部依赖未给定是显式执行门，不是实现中选择“latest”的占位。
- root授权QA pricing_version=`qa-20261001-qwen-image2-cny25-v1`仅显式QA scope；实际amount来自Bill，customer25与supplier20不能混同。无价格配置拒绝；无默认25或免费0。若真实Bill返回0，首段付费profile拒绝配置不一致而不是伪造金额；未来合法免费SKU另经明确配置。
- principal_user_id=payer_user_id=验证JWT sub；principal_account_id、storage_tenant、payer_account_id分开。新source不读org_payers伪claims、不接受body account/amount/identity。
- GET只读本地/授权/上游事实，不推进usage/quote/Task/outbox；write handler自身method guard。Task独占hold/capture/release，产品仅usage+quote与原Task恢复。
- 所有失败、未知、重启沿原key/tuple；Task output必须含原reference_id/project_id。任何失联不得让PNG、旧receipt或默认零冒充最新费用证明。
- archiving保留refs、禁止新生成；显式output删除才tombstone+outbox。此段没有新输入上传或永久删工程，旧输入删除不凭空release无durable proof的引用。
- 每项代码/测试提交都显式列路径。不得git add -A、push、merge、修改旧WT、启动服务/浏览器/私有QA/provider、派agent。真实验收仅root执行。

## File and interface map

新增 `server/internal/sourceimage/{types.go,validate.go,quality.go}`：版本化纯领域数据/校验/展示事实，不导入store/platform。新增 `server/internal/store/{source_runs.go,source_outbox.go}`：导sourceimage，仅此处写两新表；无泛化任意patch方法。新增 `server/internal/platform/{source_client.go,source_decode.go,source_context.go,source_download.go}`：仅SDK操作与严格decode/下载边界，导sourceimage，不导store。

新增 `server/internal/sourceflow/{authorize.go,quote.go,task.go,recover.go,output.go}`：使用sourceimage端口+现有store，恢复状态机。新增HTTP `source_image.go/source_image_view.go/source_image_content.go`：调用sourceflow，现有JWT guard。`main.go`只装配现有进程+有界恢复循环与退出取消，不新增daemon。

新增前端 `lib/source-image.ts`、`components/source-image/Panel.tsx`和显式source资源代理；既有TextImagePanel保持历史卡片，正常文生图新创建切到SourceImagePanel，不因source disabled回落旧begin。

所有后续代码共用以下领域签名（定义于Task1，测试fixture也用同一类型）：

```go
package sourceimage
const Owner = "product_source_v1"
type Scope struct { AppID, TenantID, ProjectID, UserID, PrincipalAccountID, PayerAccountID string }
type Intent struct {
 Scope Scope
 RequestKey, Mode, Provider, Model, Capability, Size, Prompt, PricingVersion string
 Quantity int64
}
type Quote struct {
 UsageID, UsageKey, QuoteID, PricingVersion, BusinessRef, Currency string
 Quantity, UnitPriceMinor, AmountMinor, QuotedAtUnix, ExpiresAtUnix int64
}
type Output struct {
 AssetID, ReferenceID, ProjectID, AppID, PrincipalType, PrincipalID string
 SHA256, ContentType, Status, ReferenceState string
 SizeBytes int64
}
type BillFact struct {
 Scope Scope // tenant/project由原run保留，不冒充Bill返回字段
 UsageID, UsageKey, Capability, PricingVersion, BusinessRef, Status string
 Quantity int64
 Quote *Quote
 HoldID, ChargeID, ReleaseID, RefundID string
 ChargedMinor *int64
}
type TaskFact struct {
 ID, IdempotencyKey, Phase, Status, Provider, Capability string
 Scope Scope
 Quote Quote
 Output *Output
}
type Run struct {
 ID, Owner, Fingerprint, UsageKey, TaskKey, BusinessRef string
 Intent Intent
 Quote *Quote
 ConfirmationHash string
 ConfirmedAt int64
 Phase, TaskID string
 Bill *BillFact
 Task *TaskFact
 Output *Output
 Selected, Deleted, CancelRequested, SubmitAttempted bool
 Revision, LeaseEpoch, LeaseUntil, RetryAt, Attempts, UpdatedAt int64
 LastError, QualityJSON, EventsJSON string
}
type ContextFact struct {
 Resolved, PayerKnown bool
 AppID, UserID, TenantID, Role, PayerAccountID, PayerSource, MemberRole, Reason string
}
type ContextQuery struct { AppID, UserID, TenantID, SourceRef string; ReadOnly bool }
type ContextPort interface { Resolve(context.Context, ContextQuery) (ContextFact,error) }
type BillPort interface {
 Lookup(context.Context, Run) (BillFact,bool,error)
 Get(context.Context, Run) (BillFact,bool,error)
 Usage(context.Context, Run) error
 Quote(context.Context, Run) error
}
type TaskPort interface {
 Lookup(context.Context, Run) (TaskFact,bool,error)
 Get(context.Context, Run) (TaskFact,bool,error)
 Submit(context.Context, Run) error
 Cancel(context.Context, Run) error
 Reconcile(context.Context, Run) error
}
type AssetPort interface {
 Verify(context.Context, Run) (Output,error)
 Reference(context.Context, Outbox) (string,error)
 Release(context.Context, Outbox) error
 Download(context.Context, Run) (io.ReadCloser,error) // returns only verified local temp bytes, Close removes temp
}
type Outbox struct {
 ID, RunID string
 Scope Scope
 Output Output
 State, Reason, LastError string
 Attempts, RetryAt, LeaseEpoch, LeaseUntil int64
}
```

`ErrInvalid/ErrConflict/ErrNotFound/ErrUnavailable/ErrInvariant/ErrUnconfigured/ErrForbidden` are `errors.New` sentinels in sourceimage/types.go; malformed upstream is invariant/unavailable, never authoritative404. SDK response decoding maps actual wire fields into these product views, not public internal types. Fields only validated from upstream may fill BillFact/TaskFact/Output; local tenant/project supplement is always compared to frozenrun rather than claimed as Bill fields.

## Task 1: Immutable run/quote persistence and cross-process CAS

**Files:** Create sourceimage/types.go, validate.go, types_test.go; store/migrations/0018_source_image.sql, source_runs.go, source_runs_test.go. Existing migration machinery store.go is reused unchanged unless a tested narrow hook for migration rollback is essential.

**Interfaces:** domain `Fingerprint(in Intent) (string,error)` and `ValidateQuote(run Run,q Quote) error`; Store methods (sourceimage types abbreviated by import alias `si` in this signature block):
```go
func (s *Store) CreateSourceRun(ctx context.Context, in si.Intent) (si.Run, bool, error)
func (s *Store) GetSourceRun(ctx context.Context, scope si.Scope, id string) (si.Run, error)
func (s *Store) GetSourceRunForRecovery(ctx context.Context, id string) (si.Run, error)
func (s *Store) FindSourceRun(ctx context.Context, scope si.Scope, key string) (si.Run, error)
func (s *Store) ListSourceRuns(ctx context.Context, scope si.Scope) ([]si.Run, error)
func (s *Store) SaveSourceQuote(ctx context.Context, scope si.Scope, id string, q si.Quote) (si.Run, error)
func (s *Store) ConfirmSourceRun(ctx context.Context, scope si.Scope, id, quoteHash string, now int64) (si.Run, error)
func (s *Store) ClaimSourceRun(ctx context.Context, scope si.Scope, id string, now, ttl int64) (si.Run, bool, error)
func (s *Store) SaveSourceObservation(ctx context.Context, run si.Run, expectedRevision, leaseEpoch int64) (si.Run, error)
```
Recovery loader is coordinator-only: read DB and reject wrong stored owner; never expose arbitrary-ID read in HTTP. Observation writer verifies immutable storedowner/tuple/quote/IDs and monotonic terminal flags; it does not blindly overwrite caller Run.

- [x] Add tests first using file DB and two separately opened Stores. Use existing `openTest(t)` for purecases, real Open(filepath.Join(t.TempDir(),"source.db")) for durability/concurrency. Add `sourceIntent()` exactfixture:
```go
func sourceIntent() sourceimage.Intent {
 return sourceimage.Intent{Scope:sourceimage.Scope{AppID:"product-image",TenantID:"acct_u",ProjectID:"prj_a",UserID:"usr_u",PrincipalAccountID:"acct_u",PayerAccountID:"acct_u"},RequestKey:"request-a",Mode:"text_generate",Provider:"modelxing-qwen-image-2.0-v1",Model:"qwen-image-2.0",Capability:"image.generate",Size:"1024*1024",Prompt:"一张蓝色纸盒概念图",PricingVersion:"test-price-v1",Quantity:1}
}
func TestSourceRunReplayAndScope(t *testing.T) {
 s:=openTest(t); in:=sourceIntent()
 a,dup,e:=s.CreateSourceRun(t.Context(),in); if e!=nil||dup { t.Fatal(a,dup,e) }
 b,dup,e:=s.CreateSourceRun(t.Context(),in); if e!=nil||!dup||a.ID!=b.ID { t.Fatal(b,dup,e) }
 in.Prompt="changed"; if _,_,e=s.CreateSourceRun(t.Context(),in); !errors.Is(e,sourceimage.ErrConflict) { t.Fatal(e) }
 wrong:=in.Scope; wrong.UserID="usr_other"
 if _,e=s.GetSourceRun(t.Context(),wrong,a.ID); !errors.Is(e,sourceimage.ErrNotFound) { t.Fatal(e) }
}
```
- [x] RED `GOWORK=off go test ./internal/store ./internal/sourceimage -run 'TestSource(Run|Quote|Migration|Claim)' -count=1`; record real missing API/assert failure.
- [x] Add SQL two tables. `product_source_runs`: id PK, owner CHECK exactversion, app/user/tenant/project/payer/principal_account columns, request_key, fingerprint, immutable intent_json CHECK json_valid, usage_key/task_key/business_ref UNIQUE, nullable usage_id/task_id UNIQUE, quote_json nullable CHECK json_valid, confirmation_hash/confirmed_at, phase, observation_json/events_json/quality_json CHECK json_valid, revision/lease_epoch/lease_until/retry_at/attempts, deleted/selected/cancel_requested/submit_attempted CHECK IN(0,1), created_at/updated_at. `product_source_reference_outbox`: id PK, run_id FK source_runs RESTRICT, original scope/output JSON, app/user/asset/project/reference/action, state/retry/lease/error; UNIQUE(app,user,asset,project,reference,action), action CHECK release. Add due indices. No FK to historical test projects required; source creation checks actualproject in flow. No old table/row rewrite.
- [x] Implement writes in one `BEGIN IMMEDIATE` connection; do not call s.db while holding that single connection. IDs/keys mint once after replay lookup; compare all immutable fields before returning duplicate. SQL triggers reject changing owner/scope/keys/intent/nonnull upstream IDs/frozenquote. Use checked revision increments and DB CAS:
```sql
UPDATE product_source_runs SET lease_epoch=lease_epoch+1,lease_until=?,revision=revision+1
WHERE id=? AND app_id=? AND user_id=? AND tenant_id=? AND project_id=? AND payer_account_id=?
  AND lease_until<=? AND deleted IN(0,1) AND lease_epoch<9223372036854775807;
```
Lease expiry only allows rereading/retrying idempotent *platform* operations; it never authorizes direct provider POST. Save requires epoch+revision. Claim/state/events commit atomically. quote compare `quantity==1`, version/usage/business match, positiveamount for thisconfiguredpaidprofile, `unit<=MaxInt64/quantity`, amount product equality, fixed five-minute interval fromformalBill, canonical integers; do not extend expiry.
- [x] Add rejection/reopen tests: mutatedscope/owner, twoStoresoneclaim, abandonedlease reread originalidentity, frozenquote differentbody409, exactreplaykeeps expiry, null/float/exponent/overflow input rejection, append migration rollback/read oldtext/plate/quote data counts and FK_check, selected/deleted CAS. GREEN same targetedcommand + `go test ./internal/store ./internal/sourceimage`; explicit commit only these six files.

## Task 2: Published SDK gate and real scoped platform adapters

**Files:** Modify server/go.mod, go.sum, internal/publicconsumer/contract_test.go. Create platform/source_client.go, source_decode.go, source_context.go, source_client_test.go, source_decode_test.go. Domain port signatures are the interface map above.

- [x] Add operation-catalog and wiretests first. Required IDs: identity.context.resolve; billing.usage/usage_lookup/usage_get/source_quote; task.source_submit/source_get/source_lookup/source_cancel/source_reconcile; upload.durable_asset_get/durable_download/durable_reference_get/durable_release. Tests verify exactscope/header/body/query, distincttokens per service, principalheaders Upload only, nooperator/legacyfloat. RED `GOWORK=off go test ./internal/publicconsumer ./internal/platform -run 'TestSource(SDK|Wire|Decode|Context)' -count=1` on old SDK.
- [x] Publication gate: obtain exactrootprovided releasedtag; resolve/verify its module checksum, update onlySDKrequire then `GOWORK=off go mod tidy` and `GOWORK=off go mod verify`. Record exacttag, module SHA/checksum, catalogue requiredoperations and optionalTask outputreference fields in report. Do not perform thisstep with a guessedversion or sourceWT; if notpublished stop dependenttask and tellroot.
- [x] Implement `platform.NewSourceClients(config.Config,*http.Client)(*SourceClients,error)` with separate SDK clients for Bill/Task/Identity and per-request Upload Caller (no mutating sharedCaller). SDK `Call` exactly once, Attempt=0; resolveunknown from coordinator, notadapterautomaticretry. Stub externalwire only in httptest. Readhelpers return `(found=false,nil)` solely on valid upstream404; 401/403/5xx/malformed remains explicit error. No log body/rawsignedURL/secrets.
```go
q:=map[string]string{"app_id":r.Intent.Scope.AppID,"payer_account_id":r.Intent.Scope.PayerAccountID,"payer_user_id":r.Intent.Scope.UserID}
res,e:=client.Call(ctx,platformconsumer.Call{Operation:"billing.usage_get",PathParams:map[string]string{"id":r.Quote.UsageID},Query:q})
// Check transport+status; decode exact canonical int64s; bind usage/app/payer/user/key/capability/quantity/version/business BEFORE returning facts.
```
- [x] `DecodeSourceJSON(raw,dst)` recursively rejects duplicate keys, case aliases and trailingJSON; UseNumber+canonicaldecimal→ParseInt. For response compatibility allow irrelevant documented metadata only, never case-fold identity or money. Parse actualsourceTask receipt (publishedcontract) and result_json fields; require output reference/project for thisprovider. Task/Billscope/originalIDs mismatch ErrInvariant, no partialfieldfallback. `Resolve(ReadOnly:true)` sends no tenant/source selector, compares returned exactcurrentcontext; writeauthorization can include targettenant/source. 实际 Context selector 会保存 LastScope，SDK mutating=false 不等于实现无写；此处避免 product GET 隐式更新。任意组织纯 read selector 仍是未完成公共依赖。
- [x] Test lostresponse returnsunknownandnofurtherPOST, wrongscope/quote/output, source-freelegacy receipt denied, missingreference denied, serviceheader/tokenfamily, integer9007199254740993 roundtrip withoutfloat, malformed/casealiases; `GOWORK=off go test ./internal/publicconsumer ./internal/platform -run 'TestSource' -count=1`; go mod verify; commit exact sevennew/modifiedSDKfiles (include any source_decode helper tests listed).

## Task 3: Personal authorization and default-closed organization validation

**Files:** Create sourceflow/authorize.go, authorize_test.go; modify config/config.go + config_test.go; create sourceflow/types.go. No productroutes yet.

**Interfaces:** `Actor{UserID,AccountID string}` fromverifiedJWT; `Authorization{Scope sourceimage.Scope; PayerSource,Role string}`. `Authorizer{Store *store.Store; Context sourceimage.ContextPort; AppID string}` with `ForProject(ctx context.Context,actor Actor,projectID string,write bool)(Authorization,error)`. `Service{Store *store.Store; Auth *Authorizer; Bill BillPort; Tasks TaskPort; Assets AssetPort; Profile Profile; Now func()time.Time}`; `Profile{Enabled,OrgEnabled bool; Provider,Model,Capability,Size,PricingVersion string; Quantity int64}`; `Profile.CanCreate(payerSource string) bool` requires Enabled and, for delegation, OrgEnabled. Creation flags gate Create/Confirm/new submit only; authorized historical read/delete/cancel/recovery do not require new-creation flags. Service never takes browserScope. `LoadRun(ctx context.Context,actor Actor,projectID,runID string,write bool)(sourceimage.Run,error)` authorizes then loads and compares originalUser; newpayer mismatch cannot retargetexistingrun.

- [x] RED tests personal noorgmembership succeeds, forgedOrgclaims irrelevant, Profile.CanCreate("delegation") is false when org flag is off; no Bill call, missingE7/orgpayerpersonal fails0Bill, wrongtenant404, forbiddenrole, ordinaryGET0mutations. Build ContextPort fake counting readonlyselector; fixture actualScope stable; no fabricated JWTorgfields. Command `GOWORK=off go test ./internal/sourceflow ./internal/config -run 'TestSource(Auth|Config)' -count=1`.
- [x] Add `FEATURE_SOURCE_IMAGES=false`, `FEATURE_SOURCE_ORG_IMAGES=false`, `PRODUCT_SOURCE_PRICING_VERSION=""`, `PRODUCT_SOURCE_DOWNLOAD_HOSTS=""` (exact OSS originlist, no wildcard). Existing BillingEnabled and GenerationEnabled also required fornewcreation. AppID must equal IdentityAppID. Existing service tokens remainexistingkeys; no newprovider/operator credentials. Disabled mode stillconstructs read/recovery clients forstoredruns; missingcredentials means unavailable rather thanlegacyfallback. SourceProfiles fixedmodel/provider/size in code; pricingversion explicitlyconfigured.
- [x] Personal project: only existing owner-scoped acct/personal_default key plus matching CreatedBy (or existingdirectpersonalownership proof), frozenpayer=verifiedJWTaccount, noContext call requiringorganization. Organization authorization uses fresh Context targettenant on POST; requireResolved, exactapp/user/tenant, accepted productrole, payerKnown, payerSource=delegation, nonemptypayerref/memberrole. Membership roles use explicit owner/admin/editor whitelist forwrite, member/viewer can onlyread ownruns ifexistingprojectpolicyallows; unknown role denied. All identities storedserver-side. Billsourcequote/Taskhold remain authoritativeprice/member/entitlement check; noaccounts ensure/mint.
- [x] ForGET org callonlyreadonlynoselectorContext, strictcurrenttenantcompare; selection_required or differentcurrenttenant closesread withoutswitch. Forprojectarchive denywritebut permitread. Forhistoricalrun do notreplace frozenpayer onnewContext changes: read may validateprojectmembershipthen useoriginaluser+scope; newconfirm requirescurrentpayer==frozenpayer or409 newexplicitquote. Add testrevokedmembershipdenial while workerdoesnotinvoke newpaid Task; alreadyTask-ownedsettlement unaffected.
- [x] GREEN targeted plus allconfig/sourceflow; commit files explicitly. Report orgpath implementedbutdefaultoff/realE7notverified; noorganizationGatePASS.

## Task 4: Durable usage/quote producer with explicit confirmation

**Files:** Create sourceflow/quote.go, quote_test.go, testfixture_test.go. Extend store/source_runs.go tests only with requiredCAS. No hold/capture methods in productports.

**Interfaces:** `NewRunRequest{RequestKey,Mode,Prompt,Size string}`, Service `Create(ctx context.Context,actor Actor,projectID string,in NewRunRequest)(sourceimage.Run,error)`, `RecoverQuote(ctx context.Context,run sourceimage.Run)(sourceimage.Run,error)`, `Confirm(ctx context.Context,actor Actor,projectID,runID,quoteID,quoteHash string)(sourceimage.Run,error)`. `Confirm` recordsconfirmationonly then Task5 drivesoriginalsubmit. `sourceimage.QuoteHash(Quote) string` hashes canonical exacttuple, notdisplaytext.

Testfixture defined here: `flowFixture(t)` returns realfileStore+Service+recorded fake Context/Bill/Task/Assets and safeclock; fakeBill map keyed fullscope+usagekey, fakeTaskmap keyedfullscope+taskkey, fakes mutateonlyonportcalls and countattempts/commits separately. `billFault.AfterCommit` returnsnetworkerror aftersaving remote fact. FakeTask has dispatch counter increment only on first new key; fakeAssets represent the published ready reference. Fixture methods `reopen(t *testing.T)` reopen the same file and rebuild Service while retaining fake remote stores; `confirmed(t *testing.T) sourceimage.Run` calls Create with request-a then Confirm with its exact QuoteHash, returning the persisted confirmed Run; no test injects confirmation by raw SQL. These are testports, never runtimefallback/service.

- [ ] Write failuretests beforecode:
```go
func TestSourceUsageLostResponseKeepsOriginalRun(t *testing.T) {
 f:=flowFixture(t); f.bill.AfterCommit="usage"
 a,e:=f.svc.Create(t.Context(),f.actor,f.project.ID,NewRunRequest{RequestKey:"request-a",Mode:"text_generate",Prompt:"蓝纸盒",Size:"1024*1024"})
 if e==nil||a.ID=="" { t.Fatal(a,e) }
 f.reopen(t) // closes original Store, same file and fake remote facts; rebuilds Service
 b,e:=f.svc.Create(t.Context(),f.actor,f.project.ID,NewRunRequest{RequestKey:"request-a",Mode:"text_generate",Prompt:"蓝纸盒",Size:"1024*1024"})
 if e!=nil||a.ID!=b.ID||a.UsageKey!=b.UsageKey||f.bill.UsageCommits!=1||f.task.Submits!=0 { t.Fatal(b,e) }
}
```
- [ ] RED `GOWORK=off go test ./internal/sourceflow -run 'TestSource(Usage|Quote|Confirm)' -count=1`. Saveintent beforeanyremote call. Underlease GETscopedlookup; absence→Usageonecall→GETproof; quoteabsent→source_quoteonecall→GETproof; persistquoteonlyafterfullmatch. Never derivequoteID/price/minorlocally. AlwaysreturndurableRun alongwith error toHTTP so lostresponsecanrecoverhistory.
- [ ] Confirm freshauthorization andGEToriginalfacts; quotehash/ID exact, notexpired, noalternatehold/charge/cancel/deletion; transactionCAS writesconfirmation. Do notcallquoteagain onconfirm, changeexpiry, silentlynewusage, orreprice. Ifnetworkunknown storelast_error/retry; publishamount string onlywhenquotevalidated. Repeatconfirm identicalreturnsoriginal; differenthash409. Maxintoverflow/null/negative/noncanonicalquote rejects beforeTask.
- [ ] Add quotePOSTloss+reopen, five-minuteexpiryimmutable, explicitfreshrequestafterexpiredknownnothold, modifiedprompt/newpricingconfig originalrun replayconflict ratherthanupgrade, rejectedquote leaves0Task, membershiprevokedbeforeconfirm, andconfirm+delete/archiverace tests. GREEN same command; commit quote.go/testfixture/test and any exactstore/sourceimagechanges.

## Task 5: Original Task binding, cancellation, observations and bounded recovery

**Files:** Create sourceflow/task.go, recover.go, task_test.go, recover_test.go; extend store/source_runs.go/source_runs_test.go for cancel/due selection. No Provider interface in product.

**Interfaces:** Service `Advance(ctx context.Context,runID string)(sourceimage.Run,error)` takesIDonly, loadsDBowner/intent; `Reconcile(ctx context.Context,actor Actor,projectID,runID string)(sourceimage.Run,error)`; `Cancel(ctx context.Context,actor Actor,projectID,runID string)(sourceimage.Run,error)`; `RunRecovery(ctx context.Context) error`. Store `ListDueSourceRuns(ctx context.Context,now int64,limit int)([]sourceimage.Run,error)`, `CancelSourceRun(ctx context.Context,scope sourceimage.Scope,id string)(sourceimage.Run,error)`; alltaskbinding through CAS observation preserves originalusage/task uniqueness.

- [ ] RED lostTaskresponse+reopen & cancelrace:
```go
func TestSourceTaskLostResponseNeverCreatesSecondTask(t *testing.T) {
 f:=flowFixture(t); r:=f.confirmed(t); f.task.AfterCommit="submit"
 _,_ = f.svc.Advance(t.Context(),r.ID)
 f.reopen(t)
 for i:=0;i<3;i++ { _,_ = f.svc.Advance(t.Context(),r.ID) }
 got,e:=f.store.GetSourceRun(t.Context(),r.Intent.Scope,r.ID)
 if e!=nil||got.TaskID==""||f.task.Commits!=1||f.task.Dispatches!=1 { t.Fatal(got,e) }
}
```
Command `GOWORK=off go test ./internal/sourceflow -run 'TestSource(Task|Cancel|Recovery)' -count=1`.
- [ ] Advance reloadDB; requireOwnerexact; no confirmed→noTask; lease then originalsource_lookup. Foundexactquote/scope→bind; authoritative404 withconfirmed/notdeleted/noncancel→persistSubmitAttempted thensamekeySubmit; errored→unknown; afterwardGETlookup. CrashafterclaimbeforeHTTP is safe toresume *sameTaskkey* because Task isidempotent; no directproviderPOSTexists. Bindingoneusage/Task tooneRun DBunique. Rejectstoredscope/hash/owner changes evenifcallerpreviouslyhadlegitRun.
- [ ] Beforeinitialsubmit recheckcurrentauthorization, archive/deletion, confirmationexpiry. IfSubmitAttempted=true, recoveroriginalTaskevenifquoteexpired; aTaskalreadyheld mayhonor frozenquote. Unknownlookupnevernewquote. OnceTasklinked onlyGet/Reconcile; provider_unknown remainsnonterminal. Verifyreturnedscope/quote/provider/model/resultfully. Donotmark localSUCCEEDEDuntilTaskSucceeded+freshBillcharged+verifiedOutput; Taskstatusnetworkerror cannotbackwrite priorcomplete facts.
- [ ] Cancel under sameDBtransaction flipsCancelRequested before submitclaim canwin; confirmedunattempted→not_dispatched noBillrelease needed; attempted/unknown→lookuporiginalTask+Cancelifknown; never claimreleasedbecause404whilepreviousPOSTpending. A laterlookupmustkeepcancelintent andcanceloriginalTask beforeanyfurther submit; ifpriorTaskcreation provenabsent throughterminal platformcontract only thennot_dispatched. Noresubmit afterCancelRequested.
- [ ] Recovery polling bounded: onepass≤20s, lease30s, atmost10dueRows, nooverlapperprocess, contextcancellable; retry delays1,2,4,8,16,32,60s + ≤10%jitter; no busyloop/leaseepochoverflow. NewcreationflagoffdoesnotstopknownTask/query/output/outboxrecovery; *unattempted* confirmedruncannotbeginpaidworkwhileflagoff. Recordsunknown/last_error with no rawupstreamsecrets. WorkerdoesnotrepeatedlysubmitonunknownGET.
- [ ] GREEN withmultiStore concurrentAdvance (oneTaskcommit), cancellationbeforeclaim/afterclaim/unknown, staleleaseCASrejected, genericlegacyupdate cannotfindsourceID, projectarchive preservesexistingrunningTask, clockadvancedbackoff count, contextcancel exits; `go test -race ./internal/sourceflow ./internal/store -run TestSource -count=1`; commit exactfiles.

## Task 6: Verified output, selection, release outbox and bounded content

**Files:** Create sourceflow/output.go, output_test.go; store/source_outbox.go, source_outbox_test.go; platform/source_download.go, source_download_test.go; sourceimage/quality.go, quality_test.go.

**Interfaces:** Service `ObserveOutput(ctx context.Context,run sourceimage.Run)(sourceimage.Run,error)`, `Select(ctx context.Context,actor Actor,projectID,runID string)(sourceimage.Run,error)`, `DeleteOutput(ctx context.Context,actor Actor,projectID,runID string)(sourceimage.Run,error)`, `DrainReferenceOutbox(ctx context.Context) error`; Store `DeleteSourceOutput(ctx context.Context,scope sourceimage.Scope,id string)(sourceimage.Run,error)`, `AttachSourceOutput(ctx context.Context,run sourceimage.Run,output sourceimage.Output,revision,epoch int64)(sourceimage.Run,error)`, `ListDueSourceOutbox(ctx context.Context,now int64,limit int)([]sourceimage.Outbox,error)`, `FinishSourceRelease(ctx context.Context,item sourceimage.Outbox,epoch int64) error`. AssetPort signatures frommap; `sourceimage.QualityReport(Run) ([]byte,error)` is versioned ordinary-image evidence (fidelity not_applicable, visualquality unknown until root evidence), no user booleanpass.

- [ ] RED `GOWORK=off go test ./internal/sourceflow ./internal/store ./internal/platform ./internal/sourceimage -run 'TestSource(Output|Release|Delete|Download|Quality)' -count=1`. Tests outputmissingreference, wronguser/project, BilltimeoutafterTasksuccess, immutablecharge mismatch, delete-after-ready, delete-before-lateoutput, lostrelease+reopen withsameOutboxID, archivekeepsref, outputneverre-RegisterAsset.
- [ ] Reconcile originalTask receipt andBillGET; validateBillcharge originalscope/usage/quote/amount andno contradictoryrelease; refundifobserved mustdisplay refunded factualstate, never“stillcharged”or createautomaticrefund. Asset Verify usesdurable_asset_get+reference_get exactowner/user/project/reference, ready/hash/MIME/size matchesTaskreceipt; onlythenstoredOutput. Billunknownstoresoutputpending, notsuccess. Preserve lastverifiedreceipt andrefresherror separately.
- [ ] DeleteOutput transactionsetsdeleted andselectedfalse, queuesoriginalrefifknown. AttachSourceOutput seesdeletedwithin sameconnection andqueueslateoriginalref withoutresurrection. SQLuniqueoutbox meansrepeatdeleteoneaction. Drain GETreference first: active→oneRelease→GET; released→done;404/error/manualunknownkeepspending withbackoffandreason, no deleteobject/noretainresurrection. Storeclaimhasepochguard. WorkeroriginalscopecomesonlyDB; no browserowner/projectoverride.
- [ ] Download obtainsSDKdurable_download onlyafterasset/refverify. Production fetcher onlyexact configuredOSSHTTPS hostlist/443/no userinfo/IP literal/fragment/redirect; resolveallIPs, rejectprivate/loopback/linklocal/etc, pinapprovedIPwithoriginalHost/TLSServerName. DNSrebind/tamperedescapedauthority rejects. HTTPtotal15s, headers5s, max16MiB+1; rejectlengthmismatch/chunkedoverlimit/hanging/cancel, actualSHA/MIME/decodeConfigPNG1024² match. Use0600temporaryfile with limit andsha streaming, rewindonlyafterverification; Close/cancel/errorremoves. NeverbodytoLog orsignedURLDB. Test transportinjectableonlyconstructor, no configdisablessecurity. Allocationbounded buffers≤64KiB +PNGdecodeconfig, nofullassetduplicateinBFFmemory.
- [ ] Select marksoriginalrun output; dedicatedsourceexport carriesversionedfacts/limitations withsameasset. NolegacyRegisterOutputBytes orreceipt-qualitypass. Guardexisting genericoutputregistration ifsame sourceasset referenced: itcannotpromote/modifynewowner; no sourcebytes injected oldfixturepath. Restrictnewordinaryoutputdownstreamreceipt toconceptual/unverified-fidelity; no “exactproduct” label. GREEN targeted+racesourceflow/store/platform; commit explicitfiles.

## Task 7: Real authenticated HTTP routes and old-owner isolation

**Files:** Create httpapi/source_image.go, source_image_view.go, source_image_content.go, source_image_test.go; modify server.go (Source *sourceflow.Service + routes). Modify narrow inputs to existingprojects.go onlyifneeded toserialize archive against sourceCreate/confirm; store Create/Confirm must readprojectstatus in same transaction. Do notexpandoldtext/bg updater.

**Interfaces:** handlers `handleSourceCapabilities/Create/List/Find/Get/Confirm/Reconcile/Cancel/Select/Delete/Content/Export`. `sourceActor` usesprincipalFrom; server configapp notbody. Route pathlist exactlyspec§6, project `{id}`, run `{runId}`. `sourceView(Run) map[string]any`: stringamounts, payment/source/payerlabel, quoteid/hash/expiry, Taskphase, lastverifiedcharge/ref, freshobservationstatus, mode/fidelity and readiness; no tokens/urls/privateSupplierresponse.

- [ ] Testreal Router + existingJWTfixture + realStore + testports; nofakeHTTPsuccessproxy. AddGETmethodwrong+JSON, aliases, bodypayer, wrongscope, duplicatequery, malformedjson, unknownsourceID→404, crossuser sameorg denied, archivedGETallowed/writeblocked. TestGETsnapshots leave all Bill/Task/Asset mutationcounterszero. RED `GOWORK=off go test ./internal/httpapi -run TestSource -count=1`.
- [ ] Register explicitroutesindependentofnewcreationflag soexistingread/recovery available. Eachhandlerrequiresactualmethod; allread no-store; bodylimit32KiB (prompt16KiBplusenvelope), recursiveexactkeys, overflow/nullrejected. `Source==nil` returns503newsource, neverlegacyfallback. 404nondisclosing; conflict409; invalid400; unauthorized401; knownforbidden403; upstreamunknown503 withdurablerunidifauthorized. ReadListlimit100/pagination stablecreated_at+id, no crossuser metadata.
- [ ] Confirm exactquote/bodyhash andoriginstate; callConfirmthenAdvance boundedrequestctx. GET neverAdvance/Drain. Content/Export GET mayfreshreadSDKasset/Billauthorizationfacts butnocapture/reconcile/writeRun; return currentavailability withsnapshotfacts, no fundsprogress. DELETEonlytombstonesoutput, no refund. Reconcile POSTonly boundedAdvance. MutatingcookieshandledTask8.
- [ ] Add testoldpaths/sourceID never dispatch, directprovider/TextPaint panictrapsuntouched, sourceflagoffdoesnotrenderoldfallback, JSONintstringsnevercoercedmoney, lateoutputafterdelete, originalrunreopenview. Fullhttpapi regression +racefocused; commit exactfiles.

## Task 8: Normal Web quote/confirm/result/history and safe BFF proxy

**Files:** Create `web/src/lib/source-image.ts`, `web/src/components/source-image/Panel.tsx`, `web/tests/source-image.test.ts`, `source-image-bff.test.ts`, `source-image-screen.test.tsx`; add routes under `web/src/app/api/projects/[id]/source-image-capabilities/route.ts`, `source-image-runs/route.ts`, `source-image-runs/by-request/route.ts`, `source-image-runs/[runId]/route.ts`, `source-image-runs/[runId]/[action]/route.ts`; shared `source-image-runs/proxy.ts`. Modify `web/src/app/start/ui.tsx` and `web/src/app/projects/[id]/page.tsx` at their TextImagePanel mounts to show new normal creation and separate legacy history; keep existingtextcomponent/proxytests forlegacyreading.

**Interfaces:** `SourceRunView` fields matchHTTP; `SourceImageAPI{create,confirm,get,list,find,reconcile,cancel,select,deleteOutput}` fetchonlysameorigin. Purehelpers `formatMinor(string):string`, `canConfirm(view,nowUnix):boolean`, `requestState(view)`; `SourceImageController` acceptsAPI port andUUIDgenerator, `quote(input)` callscreate only, `confirm()` calls confirm only after visible quote; `restore(project,runID)` get/list neverpost. Panel usescontroller, noautomaticconfirmfromeffect.

- [ ] RED realcontrollerrequestcounters andstaticrender:
```ts
it("报价点击不确认、不提交", async () => {
 const calls: string[]=[];
 const api=sourceAPIFixture(calls); // test helper returns exact wire quotedview; each method pushes its own name
 const c=new SourceImageController(api,()=>"request-a");
 await c.quote({projectID:"p",prompt:"蓝纸盒",size:"1024*1024"});
 expect(calls).toEqual(["create"]);
 expect(canConfirm(c.view!,1700000001)).toBe(true);
 await c.confirm(); expect(calls).toEqual(["create","confirm"]);
});
```
`sourceAPIFixture` definedin source-image.test.ts withfixedquoteid/hash/amount_minor="25"/expiry validnow, all API methodsrecordcalls+resolveview; readonlyrestore mustnotconfirm. Command `npm test -- tests/source-image.test.ts tests/source-image-bff.test.ts tests/source-image-screen.test.tsx` inweb.
- [ ] UI twoactions“获取报价”与“确认支付并生成”，后者展示actualpayer+amount+expiry+model/count/size+普通图不保真提示；unknownquote无付款按钮。请求键在点击前生成、retrylookup原key；pendingkey可sessionStorage保存用户范围提示但非权威，恢复先BFFverify；历史runURL `?source_run=<id>`与serverlist，重登恢复原费用/图。新建个人entry使用1024²；绑定已有非1024project明确显示本次原输出1024²而非改工程尺寸。
- [ ] UI展示“等待结果/结果已收到/素材保存中/费用待核实/已结算”分phase；临时unknown不显示失败免费。成功ordinary可预览/选用/export；不显示保真pass，不把rootpaidproof变“真人采用”。原actiondoubleclick同run；newgeneration另明确按钮新key。删除二次明确动作后显示ref释放进度而非“退款”。sourceflagoff显示配置状态+historicalread，不能调用旧begin。
- [ ] BFF dynamic `[action]` exactallowlist confirm/reconcile/cancel/select/content/export/output withexplicitGET/POST/DELETE maps, rejectsunknown/method beforeproxy. RequireHttpOnlyaccess cookie; POST/DELETE verify Origin exacttrustedrequestorigin, rejectcrosssite Sec-Fetch-Site, missingOrigin nonbrowser privilegedcalls needexistinginternalserverpathratherthanallowarbitrarypublicfallback; encodedsegments useencodeURIComponent and rejectslashes/control; rawbodystrictpass no`catch(()=>({}))`; Contentproxy boundedstream fromGo no redirect. No platformtokens inbrowser code. TestsOriginnegative0calls/cookieabsent0calls/pathinject/methodmismatch/quoteIDtamper/stringmoney andrawsourceURLnonleak.
- [ ] GREEN targeted then `npm test`, `npx tsc --noEmit`, `npm run build`; testcontroller+SSRqualitylabels honest limitations (no browserE2E here). No testdependency added. Explicitcommit routes/components/lib/tests+actualmountfiles only.

## Task 9: Existing process assembly and joined recovery shutdown

**Files:** Create `server/cmd/server/source_runtime.go`, source_runtime_test.go; modify main.go forcontext/cancel/workerjoin andsourcewiring; modify deploy/product-image.env.example; add docs/runbooks/hui-2232-source-consumer.md. No remoteconfigwrite.

**Interfaces:** `newSourceRuntime(cfg config.Config,st *store.Store)(*sourceflow.Service,error)` usesplatform.NewSourceClients; `runSourceRecovery(ctx,svc) error` delegatesbounded source/outbox loop, joinedwhenexistingHTTPserverstops. Sourceconfiguration validation returnsunavailablewhilelegacyhealthbehavior remainsdocumented; no fatal secretprinting.

- [ ] RED assemblytestdefaultflags0calls, completeconfig wiresformalports evenwithlegacyImageHTTPpanicfake; missingSDKops/configpreventnewcreate; read/recoveryclients survivecreationoff; canceljoinsworker≤bound withoutclosedDBrace. `GOWORK=off go test ./cmd/server -run TestSource -count=1`.
- [ ] main shutdown uses signal.NotifyContext, server.Shutdown withboundedtimeout, cancelsandjoins sourceworker beforest.Close; maintainexistingportlistener andsingleStore. Workeronlyactivatedfororiginalstoredrecordsandvalidreadcredentials, no syntheticseed/automaticmint/QAdefault. NoinitSQLfundsupdates. Readiness explicitlyshows source disabled/unconfigured separately, notglobalfalsereadyforsource.
- [ ] envexample containsonlyemptyversion/hostlist andfeature=false; commentsrootfillsappscopedtokens alreadyexistingkeys. Runbook recordsrequiredreleasedSDKtag/rootruntimeSHA, allowedprofile/price/scope, realWebmanualsteps, evidence6columns, sourceoff compatible rollback andwhyoldbinary mustnotownnewrows/outbox. Needexactreleaseversioninrunbook atimplementation, not“latest”.
- [ ] GREEN cmd/server + config + sourceflow; commit onlytheseactualfiles. No start/runservices.

## Task 10: Full fault matrix, published-wire conformance and review handoff

**Files:** Create `server/internal/httpapi/source_image_recovery_test.go`, `server/internal/publicconsumer/source_contract_test.go`; update docs/runbooks/hui-2232-source-consumer.md withactualtestcommands/results; externalcontrollerreport appendseparateimplementationsection. No adjacentfixeswithoutrootdecision.

FormalHTTPtests useactualproductRouter+fileDB andpublishedSDK adapter againsthttptest upstream scriptedrecordinghandlers; eachscriptvalidatestheactualSDKwire andstoresitsown upstreamfacts. This is explicitlyfakeBill/Task/Upload atnetworkboundary, nottheirrealserverimplementation (publicinternalsforbidden). Publicowner suppliesseparate cross-handler proof; consumerclaims onlyitscontract/duplicatebehavior. Nolocalfixturepackage reachesmain.

- [ ] Implement belowcase tablefirst andrunRED foranyuncoveredfailure; casesalreadygreen are regression, don'tinventRED. Recordeach initialcommand+observedfailure, subsequentfixfile+greencommand. Fixturefault API `FailAfterCommit(operation string, count int)`, `Calls(operation string) int`, `Committed(operation string) int`, `ReopenProduct(t)` closes/reopensproductDBonly, upstreamstatepreserved.

| case | exactexpectedproof |
|---|---|
| usage accepted/lostresponse thenreopen | originalrun/key, usagecommit1, Task0 untilconfirm |
| quote accepted/lostresponse | quoteID/amount/expiry unchanged, no silentrenew |
| confirm lostresponse/doubleclick | sameconfirmationhash, sameTaskkey |
| Task submit accepted/lostresponse | GETlookupfirst, sameTaskID, upstreamdispatch1 |
| twoStores claim+cancel interleaving | nofreshkey; canceledpreclaim0submit; attemptedunknownnotfakecanceled |
| leaseexpires/processdies | originalidentity reused; staleepochwritefails |
| Task provider_unknown | no extraSubmit/newquote/release/refund fromBFF |
| Task asset_pending/capture_unknown | retainsresultfacts; onlyoriginalTask reconcile |
| succeededTask+BillGETtimeout | sameasset pendingfunds, nofree/successupgrade |
| readyreference wrongapp/user/project/hash | invariant, noselect/export/pass |
| Task succeeded+localmappingcrash | sameasset/reference mappingonce |
| deletebeforeoutput+lostrelease | tombstone wins, oneoutbox/originalrelease, noretainresurrection |
| archive/unarchive | retainsrefs; no newcreationwhilearchive; originalTask untouched |
| logout/login+GEThistory | originalrun/quote/Task/charge/asset; allmutations0 |
| revokedorg/noE7/Contextdifferentcurrent | no personalfallback, GETnocontextselectorwrite |
| GET+body/alias/duplicate/tokenfamilyattack | reject0Bill/Taskmutations, nootherpayerfacts |
| downloadDNSrebind/redirect/oversize/hang/hash/MIMEfailure | boundedcancel+tempcleanup, noTask/funds mutation |
| disablednewsource witholdrows | availablehistory/recovery,0newpaidwork,oldlegacyfixturesunchanged |

- [ ] Full GREEN once code settled: server `GOWORK=off go test ./...`; `GOWORK=off go test -race ./internal/sourceimage ./internal/sourceflow ./internal/store ./internal/platform ./internal/httpapi ./cmd/server`; `GOWORK=off go vet ./...`; `GOWORK=off go mod verify`. web `npm test`, `npx tsc --noEmit`, `npm run build`. `git diff --check` and scope/statusaudit. No repeatedbroadtestswithoutchangedcode/newconcern.
- [ ] Publish exactBASE→HEAD, cleanstatus, SDKexacttag+checksum, allredgreencommands, faultmatrixcount/results, changedfiles, actualknownlimitations, migrationrollbackboundary. Independentreviewbyroot, no selfPASSsubstituteforrootgate. Browser/Service/Billing/Production/Human allremainNOT_RUNuntilrootactualevidence; org andtwofidelitymodesremainuncompletedGate. Commitonlytest/runbookfilesthenstopforreview.

## Self-review coverage / execution handoff

Spec1–3 constraints+publicationgate → Global/Tasks2,9,10. Spec4 identity → Tasks2,3,7. Spec5 DB → Task1. Spec6API → Tasks2,7,8. Spec7quote/unknown → Tasks4,5,10. Spec8references → Task6/10. Spec9ordinaryprofile/qualitylabels → Tasks1,6,8; bottom-plate/editcontractsandtheirpaidquality are explicitlyoutsideapprovedfirstsegment. Spec10validation/rollback → Tasks9,10. Allsignaturesabovearedomain-localandimplementedinthedesignatedtask; no SDKversion invented. No subagent choicequestion: root alreadyselectedinlineexecution andwillreviewfullplanbeforeproductedits.

## Task2 published SDK record (2026-10-01)

Pinned `sdk/go/v0.1.1`, verified Origin `refs/tags/sdk/go/v0.1.1` at `a2ea6f76be305f494cc1bdc4e6d2e1ae4f81ca36`; module sum `h1:mgfXyVXxX7txo03fLaQB+fWWUNaEvRDas4krdnlpXlQ=`. Scoped `GOPRIVATE=github.com/bianjiefilm/*`; private tag resolved directly, no public sumDB claim. No replace/internal imports. Detail in `docs/runbooks/hui-2232-source-sdk-contract.md`. SourceRef selection remains denied before transport for this segment per root; its future contract needs a separate source tenant.

## Task3 scope clarification approved by root (2026-10-01)

`Store.GetProjectForSourceAuthorization(ctx,id)` may read only project metadata to discover the tenant for formal Context authorization. It is coordinator-internal, never exposed by HTTP; failed owner/tenant/Context/app verification returns the same nondisclosing not-found. Personal first checks exact actor account or `personal/default:<account>` plus CreatedBy. Organization metadata only supplies a selector for explicit authorized writes; GET sends no selector. No SQL identity/membership/grant synthesis, no org fallback, no LastScope write in this store lookup. Add negative tests for foreign personal owner, missing/revoked/foreign app Context, personal payer fallback and forbidden write/member roles. This clarification supplements Task3 file list with a narrow store helper; existing HTTP projectFor and its legacy claims remain separate.
