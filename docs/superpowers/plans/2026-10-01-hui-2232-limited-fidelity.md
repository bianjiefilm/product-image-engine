# HUI-2232 Limited Fidelity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. No subagents; root owns independent review and branch finishing.

**Goal:** Repair the limited model-plate consumption path without inventing provider, payment, or quality evidence.

**Architecture:** Existing platform Task owns provider invocation and funds. Product-image owns immutable inputs, authorization, subject coverage, composition and evidence presentation. Unknown effects stay attached to the same durable execution.

**Tech Stack:** Go 1.26, public-ai sdk/go v0.1.0, SQLite, Next.js 15, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-01-hui-2232-limited-fidelity.md`

## Global Constraints

- Scope: model_plate_lock only; baseline 42c2abfabf8b56e8dbbbf2525bc53d65bc5ae4e1.
- No service start, provider call, other worktree mutation, secrets output, push or production release.
- Existing platform owns hold/spend/refund; no product wallet and no direct-model replacement bypass.
- Tests with httptest are fixtures; real Provider/Billing/Browser remain separate.

### Task 1: Formal Task consumption and scoped polling

**Files:** add `server/internal/platform/imagetask.go`, `server/internal/platform/imagetask_test.go`; update shared Task status decoding in `clients.go` only where backward compatible.

**Interfaces:** `TaskClient.SubmitImage(ctx, ImageTaskRequest) (TaskStatusResult,error)` builds SDK task.submit; `TaskClient.Status(ctx,taskID)` reads result_json and app scope. No new capability names.

- [x] Red test httptest rejects old `/tasks/submit`/kind/payload; accepts canonical JSON including validated AccountID and image.generate. Assert token never enters body.
- [x] Red test GET must include app_id; response `task_id=other` errors, `status=SUCCEEDED,result_json={...}` decodes to `succeeded` and Result.
- [x] Implement exact SDK calls, bounded decoding, scrubbed error status; keep Task ID, Hold ID and Provider Result distinct.
- [x] Run `GOWORK=off go test ./internal/platform ./internal/publicconsumer`.

### Task 2: Immutable execution and concurrent claim

**Files:** add `server/internal/store/plateexecution.go`, `plateexecution_test.go`, migration `0017_bg_plate_execution.sql`.

**Interfaces:** `PreparePlateExecution`, `GetPlateExecution`, `ConfirmPlateExecution`, `ClaimPlateExecution`, `UpdatePlateTask` operate with tenant/project/job scope and immutable fingerprint; state transition confirmed→submitting occurs once.

- [x] Red test two concurrent claims: sum of winners equals 1; repeated prepare returns same snapshot, changed mask/payer/model after confirmation conflicts; different tenant cannot read.
- [x] Persist original/mask/evidence hashes, immutable params, quote ID/amount/payer, task idempotency key, task/hold IDs and recovery state before network effects.
- [x] UNKNOWN keeps same fingerprint; no expired-lease regeneration. TaskID lookup remains read-only.
- [x] Run `GOWORK=off go test ./internal/store`.

### Task 3: Server-frozen subject coverage

**Files:** add `server/internal/bgreplace/coverage.go`, `coverage_test.go`. Runtime manifest loading belongs to the deferred HTTP integration.

**Interfaces:** `VerifyCoverage(original,mask []byte,sample CoverageSample) (CoverageEvidence,error)` binds hashes, authorization provenance and all four axes to protected pixels.

- [x] Red tests missing source/license/approval, wrong original hash, mask mismatch, omitted axis, out-of-bounds/empty region, uncovered text region, fully protected canvas all reject.
- [x] Positive authorized synthetic test establishes only deterministic contract success; no Provider PASS label.
- [x] Keep coverage evidence tied to the same sample and canvas; pixel-equality checks cannot promote uncovered text. Runtime composite verification belongs to the deferred HTTP integration.
- [x] Run `GOWORK=off go test ./internal/bgreplace`.

## Deferred HTTP/UI work: separate contract prerequisite

This plan executes only Tasks 1–3. Task 4 is not executable until HUI-2294 supplies its approved interface design. No endpoint or result schema is inferred here. The implementation draft was removed before this checkpoint; the existing HTTP model-plate route still has the reported direct-provider/concurrency limitation and must not be treated as fixed.

The platform owner must supply: exact app-scoped idempotency lookup operation/path/response; verified Catalog image.generate input schema and provider binding; authoritative Task result/Asset linkage; app/account/project/quote/hold/settlement facts and retry semantics. Product-image requires a confirmed mapping from model result to an authorized, downloadable Asset.

After those prerequisites are available, root resumes this plan by adding a separately reviewed concrete Task 4. Intended file boundaries are `server/internal/httpapi/modelplate.go`, `modelplate_test.go`, existing `bgreplace.go` and `server.go`, `web/src/components/background-replace/Panel.tsx`, and `web/src/app/api/projects/[id]/background-replacements/[jobId]/[action]/route.ts`. No changes to these files are part of the current checkpoint.

## Exact execution details for Tasks 1–3

These supplement the steps above with the actual types and red/green assertions used. The named source test files contain the complete fixtures. Previously green tests are evidence, not a reason to rerun them without changes.

### Task 1 types and code

```go
type ImageTaskRequest struct {
    AccountID, ProjectID, IdempotencyKey, Provider string
    Params json.RawMessage
    AmountMinor int64
}
func (c *TaskClient) SubmitImage(ctx context.Context, in ImageTaskRequest) (TaskStatusResult, error)
func decodeTaskStatus(body []byte, expectedID string) (TaskStatusResult, error)
```

The request body is built with the published SDK operation and integer-minor to decimal-CNY formatting:

```go
body, err := json.Marshal(map[string]any{
    "app_id": c.AppID, "account_id": in.AccountID, "project_id": in.ProjectID,
    "capability": "image.generate", "idempotency_key": in.IdempotencyKey,
    "provider": in.Provider, "params": in.Params,
    "billing": map[string]any{
        "mode": "hold",
        "amount_cny": json.Number(fmt.Sprintf("%d.%02d", in.AmountMinor/100, in.AmountMinor%100)),
        "reason": "product-image:model_plate_lock",
    },
})
```

Red/green assertion from `TestImageTaskUsesCanonicalPlatformContract`:

```go
in := ImageTaskRequest{AccountID: "acct_verified", ProjectID: "prj_one",
    IdempotencyKey: "frozen-key", Provider: "xingmo",
    Params: json.RawMessage(`{"prompt":"empty studio","model":"verified-model","size":"1024x1024"}`), AmountMinor: 123}
got, err := c.SubmitImage(context.Background(), in)
if err != nil || got.TaskID != "tsk_one" || got.Status != "queued" || got.HoldID != "hold_one" {
    t.Fatalf("result=%+v err=%v", got, err)
}
in.AccountID = ""
if _, err = c.SubmitImage(context.Background(), in); err == nil || calls != 1 {
    t.Fatal("invalid payer reached platform")
}
```

This payload is explicitly a transport fixture; it is not evidence of a production Catalog model schema. Red command: `cd server && GOWORK=off go test ./internal/platform -run TestImageTask`; observed undefined ImageTaskRequest/SubmitImage/HoldID. Green command: `GOWORK=off go test ./internal/platform ./internal/publicconsumer`; expected and observed both packages `ok`. Shared Status adds app_id, parses result_json, and rejects mismatched task identity.

### Task 2 types, transitions and tests

```go
type PlateRequest struct {
    PayerID, QuoteID string
    AmountMinor int64
    OriginalHash, MaskHash, CoverageHash string
    Model, Provider, Size, ParamsJSON string
    SampleID string
    Mask []byte
}
type PlateExecution struct {
    TenantID, ProjectID, JobID, Fingerprint string
    Request PlateRequest
    State, TaskID, HoldID string
}
func PlateFingerprint(in PlateExecution) string
func (s *Store) PreparePlateExecution(ctx context.Context, in PlateExecution) (PlateExecution, error)
func (s *Store) GetPlateExecution(ctx context.Context, tenant, project, job string) (PlateExecution, error)
func (s *Store) ConfirmPlateExecution(ctx context.Context, tenant, project, job, fingerprint string) error
func (s *Store) ClaimPlateExecution(ctx context.Context, tenant, project, job string) (bool, error)
func (s *Store) UpdatePlateTask(ctx context.Context, tenant, project, job, state, task, hold string) error
```

Fingerprint is SHA256 of the canonical JSON tuple `(tenant, project, job, entire PlateRequest)`. The store calculates it and rejects a supplied mismatched digest. Scope must resolve to an existing BgJob. The exact claim mutation is:

```sql
UPDATE bg_plate_executions SET state='submitting',updated_at=?
WHERE tenant_id=? AND project_id=? AND job_id=?
AND state='confirmed' AND task_id=''
```

Red/green concurrency assertion from `TestPlateExecutionClaimsOnceAndFreezesQuote`:

```go
var winners atomic.Int32
var wg sync.WaitGroup
for i := 0; i < 12; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        won, err := s.ClaimPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID)
        if err != nil { t.Error(err) }
        if won { winners.Add(1) }
    }()
}
wg.Wait()
if winners.Load() != 1 { t.Fatalf("winners %d", winners.Load()) }
if err = s.UpdatePlateTask(ctx, in.TenantID, in.ProjectID, in.JobID, "unknown", "", ""); err != nil { t.Fatal(err) }
if won, err := s.ClaimPlateExecution(ctx, in.TenantID, in.ProjectID, in.JobID); err != nil || won {
    t.Fatal("unknown execution regenerated")
}
```

Red command: `GOWORK=off go test ./internal/store -run TestPlateExecution`; observed undefined types/methods. Green command same, expected/observed `ok`. After store validation changes run that focused command; full `GOWORK=off go test ./internal/store` validates migration compatibility.

### Task 3 types and tests

```go
type CoverageRegion [4]int // exclusive right/bottom coordinates
type CoverageSample struct {
    ID, Source, License, ApprovalRef, Scope string
    OriginalSHA256, MaskSHA256 string
    Width, Height int
    Regions map[string][]CoverageRegion
}
type CoverageEvidence struct {
    SampleID, OriginalSHA256, MaskSHA256, CoverageSHA256, Scope string
}
func ImageSHA(raw []byte) string
func VerifyCoverage(original, mask []byte, sample CoverageSample) (CoverageEvidence, error)
```

Server-controlled annotations bind the full approved silhouette hash as well as each required region. Before decoding full images, PNG header dimensions must equal the frozen canvas (maximum 4096 each side). The critical assertion is:

```go
if _, err := SubjectPixelChecks(original, original, mask); err != nil { t.Fatal(err) }
if _, err := VerifyCoverage(original, mask, sample); err == nil {
    t.Fatal("equal protected pixels cannot prove unprotected text")
}
```

Here the frozen packaging-text region is `[1,0,2,1]` while only `[0,0,1,1]` is protected. Equal pixels in the left area do not certify the unprotected text. Other fixtures cover missing license, wrong original/mask hash, omitted axis, uncovered region, and invalid region bounds.

Red command: `GOWORK=off go test ./internal/bgreplace -run TestCoverage`; observed undefined CoverageSample/VerifyCoverage. Green command: `GOWORK=off go test ./internal/bgreplace ./internal/store`; expected/observed `ok`.

## Checkpoint verification and integration

Run `cd server && GOWORK=off go test ./...` after final source changes; expected all 25 tested packages `ok`. Frontend is unchanged; its refreshed baseline is 20 files / 128 tests PASS, so a second identical run is unnecessary. Run `git diff --check` before explicit-file staging. Record actual SHA and incomplete HTTP/UI/true-service gates in the root report. Root owns spec/quality review and final branch decision; preserve both branches and the worktree.
