# HUI-2596 Selective Revision Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让产品图在锁定主体的前提下做局部修改、并排、回退，并用一个整数分函数和明确版本引用进入下游。

**Architecture:** 纯函数包 `server/internal/revision` 产生同一个 `RevisionIntent`、费用、计划和版本账本；像素路径只调用已有 `bgreplace.LockSubject`。SQLite 迁移 `0022` 保存不可变版本和采用指针。HTTP 由 `FEATURE_SELECTIVE_REVISION` 开关注册。Web 只转发，不第二套加费，不新做视觉组件。

**Tech Stack:** Go 1.26、SQLite、现有 `httpapi` fixture、Next BFF、Vitest。

## Global Constraints

- 只做锁定、局部修改、并排、回退、整数分费用和版本语义。
- 不另起视觉组件，不引入 shadcn。
- 自然语言和可点击动作生成同一个 RevisionIntent。
- `preserve_subject=true` 时不得静默整图重画；复用 `bgreplace.LockSubject`，不重写生成引擎。
- provider 不能局部编辑时，执行前说明影响范围。
- failed/unknown 不覆盖已采用版本；新 Brief 只显示差异。
- 导出和下游引用绑定明确版本，不要求重新上传。
- `IncrementalCostCents` 是唯一整数分函数；背景 40、光影 20、回退/采用/下游 0。
- 至少一次局部 revision 不执行无关昂贵步骤。
- 不打真模型。真实供应商 UNKNOWN。不宣称 HUI-2232 完成。
- 基线若已失败，只记录，不改无关代码。

---

### Task 1: RevisionIntent、费用、计划与像素执行

**Files:**
- Create: `server/internal/revision/intent.go`
- Create: `server/internal/revision/cost.go`
- Create: `server/internal/revision/plan.go`
- Create: `server/internal/revision/execute.go`
- Test: `server/internal/revision/revision_test.go`

**Interfaces:**
- Consumes: `bgreplace.LockSubject`, `bgreplace.SubjectPixelChecks`, `bgreplace.OriginSubjectLock`
- Produces:
  - `ParseNaturalLanguage(text string) (Intent, error)`
  - `FromClick(action, targetVersion, downstream string) (Intent, error)`
  - `SameIntent(a, b Intent) bool`
  - `IncrementalCostCents(intent Intent) (int64, error)`
  - `PlanRevision(intent Intent, provider Provider) (Plan, error)`
  - `Execute(req ExecuteRequest) (Result, error)`
  - 昂贵步骤名：`supplier_full_redraw`、`subject_resample`、`logo_regen`、`packaging_text_regen`、`video_transcode`、`matrix_publish`、`new_upload`

- [x] **Step 1: Write failing tests** for same intent, recomputable fen, impact notice before work, subject pixels held, expensive steps not entered, full-redraw hook calls stay 0.
- [x] **Step 2: Run** `go test ./internal/revision/ -count=1` and confirm failure is missing package.
- [x] **Step 3: Implement** the minimum to pass.
- [x] **Step 4: Re-run** the same test until it passes.

### Task 2: 版本账本

**Files:**
- Create: `server/internal/revision/ledger.go`
- Test: `server/internal/revision/ledger_test.go`

**Interfaces:**
- Produces: `(*Ledger).PutCandidate`, `RecordUnresolved`, `Adopt`, `Rollback`, `SideBySide`, `ProposeBrief`, `Export`, `Downstream`, `Snapshot`, `Restore`

- [x] **Step 1: Write failing tests** for failed/unknown not moving the adopted hash, brief diff not moving it, side-by-side, rollback, export/downstream explicit version and `RequiresReupload=false`.
- [x] **Step 2: Run** `go test ./internal/revision/ -count=1 -run 'Ledger|Brief|Export|Downstream'` and confirm failure.
- [x] **Step 3: Implement.**
- [x] **Step 4: Re-run** until pass.

### Task 3: 持久化与刷新

**Files:**
- Create: `server/internal/store/migrations/0022_selective_revision.sql`
- Create: `server/internal/store/revision.go`
- Test: `server/internal/store/revision_test.go`

- [x] **Step 1: Write a failing reopen test.**
- [x] **Step 2: Run** `go test ./internal/store/ -count=1 -run Revision` and confirm failure.
- [x] **Step 3: Implement** insert-only versions plus a head row.
- [x] **Step 4: Re-run** until pass.

### Task 4: HTTP 与开关

**Files:**
- Modify: `server/internal/config/config.go`
- Modify: `server/internal/config/config_test.go`
- Modify: `server/internal/httpapi/server.go`
- Create: `server/internal/httpapi/revision.go`
- Create: `server/internal/httpapi/revision_test.go`
- Modify: `README.md`
- Modify: `deploy/product-image.env.example`

- [x] **Step 1: Write failing HTTP tests** for flag-off 404, intent parity, 409 before ack, subject bytes, adopted hash stable after failed outcome and new brief, export/downstream without upload, cross-tenant 404.
- [x] **Step 2: Run** `go test ./internal/httpapi/ -count=1 -run Revision` and confirm failure.
- [x] **Step 3: Implement.**
- [x] **Step 4: Re-run** until pass.

### Task 5: Web 只转发整数分

**Files:**
- Create: `web/src/lib/revision.ts`
- Create: `web/src/app/api/projects/[id]/revisions/route.ts`
- Create: `web/src/app/api/projects/[id]/revisions/intent/route.ts`
- Create: `web/src/app/api/projects/[id]/revisions/[label]/export/route.ts`
- Create: `web/src/app/api/projects/[id]/revisions/[label]/downstream/route.ts`
- Test: `web/tests/revision.test.ts`
- Test: `web/tests/revision-bff.test.ts`

- [x] **Step 1: Write failing tests** that `displayCostCents` returns the server integer unchanged and the BFF forwards the body without adding a fee.
- [x] **Step 2: Run** `npm test -- tests/revision.test.ts tests/revision-bff.test.ts` and confirm failure.
- [x] **Step 3: Implement** with no visual component.
- [x] **Step 4: Re-run** until pass.

### Task 6: 验证后提交并开 PR

- [x] **Step 1: Run** revision、store、config、httpapi revision、bgreplace 与 web revision 测试。
- [x] **Step 2: Commit** on `ext/hui-2596-selective-revision` only. Do not push `6219124`.
- [x] **Step 3: Open a non-draft PR** whose title contains `HUI-2596` and whose body says the real supplier is UNKNOWN.
