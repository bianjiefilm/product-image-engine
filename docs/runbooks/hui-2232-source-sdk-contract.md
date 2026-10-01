# HUI-2232 source SDK consumer boundary

Task2 engineering implementation; independent root review pending. This is a
published-wire adapter, not a provider, second wallet or deployed service.

SDK `github.com/bianjiefilm/public-ai/sdk/go v0.1.1` pins tag
`sdk/go/v0.1.1` to `a2ea6f76be305f494cc1bdc4e6d2e1ae4f81ca36`.
Downloaded module sum: `h1:mgfXyVXxX7txo03fLaQB+fWWUNaEvRDas4krdnlpXlQ=`.
GoMod sum: `h1:uHyMORODJVufViOTtxU/bCxRHGGlQmizefLgRjN7xiM=`.
Only `GOPRIVATE=github.com/bianjiefilm/*` is private; SDK resolved directly.
Public dependencies use the ordinary proxy. An initial all-direct tidy failed
on unreachable go.googlesource.com; scoped-private/default proxy tidy passed.
No neighboring replace, public internal imports or server implementation copy.

The source fields below were read at that exact tag to resolve gaps in DTO docs.
They are local response views, not imported public domain implementation:

- Task `internal/task/source_http.go:sourceSummary`: bare object, full
  app/payer user/payer account/project/idempotency/provider/capability and frozen
  usage/quote/quantity/unit/amount/pricing/business, status/phase, IDs and flags.
  `result_json` is a string; successful ordinary image contains exactly
  asset_id/content_type/sha256/size_bytes/reference_id/project_id. `output_ready`
  may be true while result_json is empty during capture recovery: this preserves
  a pending fact, not success. Public result has no supplier locator.
- Bill `internal/billing/unified_http.go`: ingest `{ok,fact,replayed}`, quote
  `{ok,quote}`, scoped GET/lookup `{ok,facts}`. UsageFact from unified_usage.go
  binds funding/version/app/payer/user/key/capability/quantity/pricing/business.
  Settlement facts contain usage, quote, hold, charge, release, refund. Null
  optional facts are normal. Quote lacks currency/business/key, so those local
  frozen CNY profile facts are explicitly supplemented, never claimed received.
  Hold/release/refund IDs and charge amount/status/source split are checked.
- Identity `internal/contextcontract/http.go` and `contract.go`: resolve POST
  `{app_id,principal_id}` on read, with explicit tenant_id only on selected
  writes; response `{ok,resolved,reason,context}`. Context binds app/principal,
  display_org and payer_summary known/ref/source/member/account. A known payer
  does not itself grant product permission. Authorizer is Task3.
- Upload published `platformconsumer.UploadDurable*` shapes plus actual durable
  HTTP envelopes: `{ok,asset}`, `{ok,reference}`, download
  `{ok,download_url,expires_at,asset}`. Principal is always original user,
  not payer account. Exact active reference/project and ready bytes metadata
  must match original Task. Download grant is private; no exported Download or
  browser URL exists yet. Task6 implements verified bounded fetch before bytes
  can be exposed. No asset recreation/retain/delete call is in this consumer.

Four service credentials are distinct app tokens. Identity/Bill/Task requests
never attach principal/service/session credential families. Upload constructs a
fresh client caller per original user; no shared mutable caller. All SDK Call
operations are one attempt, no automatic adapter retry; unknown stays unknown.
Only a valid operation-specific not-found error can mean absent (the distinct
billing_legacy_bridge_missing error never means absent). Money remains int64
canonical JSON; recursive duplicate/case aliases, trailing JSON, float/exponent,
null numeric, negative/overflow and tuple changes are rejected without fallback.
SourceRef deep-link is explicitly rejected before wire on both reads and writes,
per root: the present ContextQuery has no separate source tenant. Ordinary org
selector uses a verified explicit tenant; GET carries no selector.

Red evidence: v0.1.0 missing 13 source/durable catalog operations; missing strict
JSON decoder/clients/asset methods/download-grant APIs; wrong capture-pending
interpretation; readonly SourceRef causing a transport call; malformed hold_id
object treated as empty; charge allocation/status/spent mismatch accepted.
Each was observed before its implementation/fix. Fake upstream is only httptest
wire recording; lost response test reads a committed actual HTTP response then
returns a controlled transport error and proves exactly one upstream POST.

Verification (Task2): `GOWORK=off go test ./...` passed (HTTP 12.118s,
platform 7.356s; unchanged packages may use cache). Fresh platform/publicconsumer/
sourceimage `-count=1` passed (0.840s/1.829s/1.311s). Fresh same-package
`-race -count=1` passed (4.129s/4.708s/5.398s). Scoped `go vet` exited 0,
`go mod verify` reported all modules verified, diff check passed. Twenty source
platform tests plus the required SDK catalog test are included; final exact
commit is handed to root. Baseline store/sourceimage/platform/publicconsumer passed first. Bill,
Browser, Production, Human and new complete Service chain remain NOT_RUN here.
HUI-2232 stays open: text generation does not prove product subject fidelity.
