# HUI-2232 Task4 durable quote and explicit confirmation

Implementation parent: final independently approved Task3 16554a1. Root approved
Task4-only typed CAS methods and the real SDK unquoted Usage GET adjustment.
Task5 dispatch, Task6 assets, Web/HTTP wiring, provider calls and deployment are
separate steps. Flags still default off; no live SKU/member credit/bridge claim.

Create authorizes verified Actor/project, validates the configured fixed profile,
and commits immutable intent/keys before remote calls. Under a 30-second fenced
lease and 20-second request budget it reads original scoped usage key/ID first.
Only authoritative absence permits one original-key Usage attempt. It then reads
proof, persists full verified Usage before quote, and only issues source_quote if
formal facts have no quote. Quote proof is read back from the original Usage ID.
A lost response leaves the durable run ID and original keys visible with bounded
error/retry; reopen resolves the original facts, with no fresh key or reprice.

Three typed local writes enforce full stored run/revision/lease equality:
SaveSourceBillObservation binds unique original Usage and validated pre-dispatch
facts; SaveSourceQuoteUnderLease freezes immutable quote with five-minute expiry;
ConfirmSourceRunForProject rechecks original project/state/owner in its transaction.
Pre-dispatch facts cannot contain hold/charge/release/refund or charged amount.
An observed quote cannot be removed/repriced. Existing Task1 APIs retain their
original contracts; the new typed methods do not grant money or dispatch tasks.
Scan accepts an unquoted Usage only with complete self-consistent Bill metadata,
and rejects a changed persisted observation tuple even after local quote freeze.

Confirm refreshes real authorization and frozen payer, validates exact quote ID
and hash, source creation flags, current expiry and scoped Bill proof; no quote
POST or Task call occurs. The transaction checks archive/deletion and personal
account+creator ownership. Authorized organization collaborators need fresh
formal Context membership/payer and matching project tenant, not creator equality.
Same confirmation retains original confirmation time. Success clears the lease
and obsolete retry metadata. Unknown/error preserves lease/keys/known facts; its
bounded local metadata recording uses a one-second cleanup context and no network.

SDK pin remains v0.1.1 exact a2ea6f7 and prior verified checksum. The adapter now
uses the persisted validated Bill UsageID when local Quote is nil; known Quote
and Bill disagreement fails before network, and response IDs/observed quote cannot
change. Existing frozen-quote reads remain available. Private credentials and
signed URLs are absent from this report and test fixtures.

Actual test-first evidence retained:

- Missing Create/Confirm/NewRunRequest and the three typed store APIs: expected
  RED exit1 before implementation, rerun after Task3 final approval.
- Real SDK HTTP unquoted original GET: RED platform 1.766s, with zero recovery wire;
  mismatched Quote/Bill IDs also incorrectly sent wire. Corrected GREEN 1.676s.
- Independent-of-fake-flow real SDK + raw public HTTP fixture + real file DB:
  quote commit/503, reopen, server different UsageID rejection/no overwrite, then
  original ID GET recovery with one Usage and one Quote POST. RED 2.810s; GREEN
  0.997s. No Task request, no paid provider or fake runtime adapter.
- Real SDK known/observed quote disappearing from the same Usage: RED platform
  2.521s, then the decoder rejects removing frozen amount/quote facts.
- Erasing observed quote: RED store 1.529s; foreign persisted Bill tuple after
  quote: RED 1.768s; archive conflict losing durable Run ID: RED flow 1.081s;
  obsolete retry after successful quote: RED flow 1.887s. Each narrow fix followed
  its failure and subsequent targeted GREEN.

15 flow tests (one raw SDK wire integration) and eight typed store tests cover
lost Usage/Quote responses, reopen, unchanged key/expiry, explicit confirmation,
idempotence/hash mismatch, expiry/no reprice, explicit new request, prompt/pricing
conflict, malformed quote tuple, hold/unknown denial, profile closed, foreign run,
organization revocation/collaborator, archive/deletion/cancel/stale CAS, global
unique pre-quote Usage binding, frozen quote and persisted Bill corruption.

Initial complete server suite passed (flow9.318/store9.383/platform8.811/http12.485).
After final fixes scoped normal passed flow4.913/store3.550/platform2.066;
fresh scoped race passed flow13.495/store10.936/platform2.381/sourceimage2.755;
scoped vet exited0. A final all-server suite passed flow1.965/store2.890,
module verify reported all modules verified, and diff check exited0. Final
post-decoder-freeze checks are attached at the exact commit checkpoint. One intermediate parallel test setup transiently could not
find installed Go regexp/syntax; read-only check confirmed installed go1.26.3 and
existing standard-library directory, then serial retry passed flow2.243/store1.100.
No toolchain changes or discarded business failures.

This is engineering evidence. Normal personal bridge, deployed Web/provider/
Billing/Upload end-to-end acceptance and structured fidelity remain incomplete.

Final post-decoder check: targeted SDK wire GREEN 5.287s; fresh race flow27.730s,
store23.509s/platform7.037s/sourceimage6.200s; vet exit0; all-server PASS
flow1.174s/platform1.253s with unchanged packages cached; module verify/diff exit0.
