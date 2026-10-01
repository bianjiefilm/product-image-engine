# HUI-2232 Task5 original Task recovery

Parent is independently approved Task4 c8bb218. This increment adds Task binding,
original-key reads, cancellation intent and bounded recovery. It does not register
usable media, mutate Bill funds, call a provider or deploy any service. Normal
personal IAM/credit/SKU/bridge gates remain separate and disabled by default.

Advance reloads the durable run, requires original confirmation and claims a
30-second revision/epoch-fenced lease. It reads original scoped Task ID or key
first. Unknown reads never submit. All unlinked Task POSTs require creation flags,
fresh project authorization, unchanged payer, unexpired original quote and fresh
pre-funding Bill facts. The typed MarkSourceSubmitAttempt then rechecks the full
run, revision/epoch, current project tenant, personal owner, archive/delete,
tombstone, cancellation and quote expiry in one transaction before a single POST.
No new key, Usage, quote or provider request is created during recovery.

Migration 0019 stores the first submit marker's Unix time and its fixed 20-second
local request budget. It freezes the pair on retries and rejects partial,
non-integer and overflowing pairs. Historical attempted rows retain zero/zero:
they can read original facts but cannot fabricate a clock and retry POST. A retry
needs formal original-key absence, original local budget elapsed, current creation
flags, valid original quote/payer and the same durable intent. Elapsed time is not
proof that remote execution ended; published Task same-key idempotency and original
Usage ownership are the duplicate Task/funds authority. Actual Submit receives a
context whose deadline is at most 20 seconds; the whole Advance has that budget.

Known Task facts bind unique original Task ID, quote, provider, capability,
scope/key and original observed financial IDs. Terminal status and known output
cannot regress or change. Their output metadata is only an observation; local
Run.Output stays absent and success is not granted by Task success alone. Task6
must verify current Bill, original financial IDs, Upload and content before use.
Unknown errors preserve original facts and lease, record a short generic code and
bounded retry, and never persist raw upstream messages or secrets.

Cancel commits its intent in the same SQLite write transaction as the submit
marker's competing state checks. Cancel also rechecks current project owner,
tenant and archive/deletion in that transaction. Unattempted cancellation becomes
not_dispatched. Attempted unknown cancellation stays cancel_pending; absence is
not released funds. Later recovery finds and cancels the original Task, then reads
proof. Known tasks continue original GET without new-creation flags or fresh org
membership; user-origin cancellation/reconciliation still requires current write
authority. Platform-only cancellation is observed for review without inventing a
user cancellation flag.

Root-approved confirmation compatibility is included here. Only first confirmation
enters confirmed. Repeated original hash still refreshes authorization/frozen
payer, claims the lease and checks current project state in the typed transaction;
it retains original phase/events/ConfirmedAt/Task/Bill. It does not require held or
charged Bill to return to pre-funding. First confirmation still reads scoped Bill
proof. Repeated confirmation cannot create quote, Task or financial writes.

The recovery loop is joined and context cancellable, with one coordinator per
Service, a 20-second whole batch, at most ten due rows, 30-second leases and no
detached goroutines. Separate processes/stores arbitrate through SQLite CAS.
Backoff is 1,2,4,8,16,32,60 seconds plus deterministic jitter of at most ten percent.
One normal Task read advances backoff once. Canceled or terminal local runs are
not polled. New-creation flags never bypass original linked Task reads.

Actual test-first checkpoints:

- Missing Advance/Cancel/due-row APIs and typed first-budget marker: RED exit1
  before implementation, using preserved external Task5 preparation.
- Platform-only canceled fact rejected, normal poll doubled attempts, archived
  Cancel mutated run: three actual behavioral REDs then targeted GREEN.
- Reconfirm queued/asset_pending rewound phase: actual RED then typed SQL GREEN.
  Reconfirm with held/charged Bill incorrectly required pre-funding: actual RED
  then flow GREEN; original Task/Bill/phase/events/time conserved, no wire writes.
- Recovery API missing: RED then whole-batch limit, deadline, cancellation join,
  backoff and overlap GREEN. No real waiting for the full twenty-second budget.

Raw published v0.1.1 SDK HTTP integration uses real file SQLite and an independent
public-wire fixture. The server saves the original Task and closes the connection
without a response; original scoped reads are temporarily unavailable. Reopen
with creation disabled binds the same Task by original key, with one POST and one
fixture commit. A later changed Usage ID is rejected without overriding binding.
This is HTTP fault evidence, not a real provider or funds transaction. An initial
fixture accidentally used the wrong absence code and failed with zero POSTs; it
was corrected to published task_not_found. Initial race found unsynchronized
fixture counters after connection close; a mutex corrected the test authority and
the raw EOF race test passed. Production code was unchanged by that race repair.

Migration tests build an actual prior-0018 file DB with attempted row, interrupt
0019 after ALTERs before schema registration, prove no partial columns, retry,
then verify intent/key/time and historical zero pair unchanged. Additional tests
cover malformed/overflow SQL pairs, immutable first budget across reopen/retry,
old attempted rows refusing invented start, actual Submit deadline, same-key retry
only after budget/lease, multi-store concurrent single commit, cancellation races,
expiry, closed flags, archive recovery, unique Task and stale observations.

All-server normal suite passed. Scoped source flow/image/store/platform normal,
vet, module verification and diff checks passed. Final scoped race results are
recorded at the exact commit checkpoint; original failed race remains documented.
HUI-2232 is not deployed or closed, and structured fidelity is not claimed.

Final checkpoint: fresh scoped race PASS flow29.332s/image1.877s/store18.846s/
platform1.561s. Final all-server normal PASS (flow6.657s; unchanged packages cached),
all-server vet exit0, module verification all modules verified, diff check exit0.

## Independent Task5 terminal-cancellation correction

The independent review of e620095 returned CHANGES for P2 terminal cancellation.
Its original six-case RED (flow0.640s) and expanded nine-case RED (flow0.903s)
remain in the orchestration archive. This narrow correction does not advance
Task6 or change financial/output authority. The archived independent HTTP cases
are now permanent author-tree regression tests, renamed to the Source test suite;
the reviewer worktree was read only and subsequently cleaned by its owner.

Author actual pre-fix RED reproduced all nine cases (flow0.981s): already known
succeeded/failed/canceled Task could be canceled locally into false pending; a
terminal result arriving after cancellation intent was hidden; and a failed
original GET reprojected historical cancellation intent over verified terminal
facts. Additional actual RED tests exposed the same projection on local terminal
rows with SQL NULL Task and terminal cancellation mutating local terminal rows.
These are preserved failures, not superseded by the earlier normal/race checks.

Cancel now checks complete original scope and current project owner/archive/delete
first, in its existing transaction. An already stored terminal Task or local
terminal phase returns original run with stable conflict and changes no flag,
revision, event or other fact. Task observation prioritizes succeeded as
asset_pending and failed/canceled as review_required, ahead of cancellation
intent. Existing local terminal phase stays unchanged. Error recording follows
the same priority and cannot project known terminal facts into cancel_pending.
Historical cancel_requested is never cleared. Task6 must verify original Task,
Bill and output facts before local finalization; cancellation intent is not a
release/refund or failure authority.

The nine published-SDK HTTP cases pass with no terminal Task POST/Cancel, no Bill
GET/write, unchanged original Bill facts, no usable local output and unchanged
known terminal run on a rejected Cancel. Additional regression tests cover four
local terminal phases with SQL NULL Task, preservation under unknown errors,
foreign full scope, archive/deletion and changed owner before the terminal guard.
Initial targeted GREEN: sourceflow0.726s/store0.995s. Final fresh full-server normal,
sourceflow/store race, vet and diff checks are recorded at the fix checkpoint.

Fix final checks: fresh full-server `go test ./... -count=1` PASS
(flow5.447s/store5.294s/httpapi8.502s/platform4.117s). Fresh Source race PASS
flow28.933s/store18.011s; all-server vet exit0; diff check exit0. No SDK pin,
configuration, server/provider, money operation, migration or Task6 changes.
