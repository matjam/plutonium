# Safe arena lifetimes, native Linux amd64

Status: native verified, reviewed, landed on main and installed.
The user approved D7's lifetime policy and concrete API split. Sections 17.6,
18.9, 18.11, 18.12, 20.1 and 21.5, appendix F, ABI section 1.3 and memory-model
section 3.8 define the behavior. The implementation landed as
`218008141373138b78f3d2224850eaf4afbd2a19` on 2026-10-14. This verifies the
arena feature's tested scope, not complete compiler/specification conformance.

## Behavior

`arena.NewFixed(capacity, parent)` explicitly allocates private fixed-capacity
backing and returns a movable `arena.Fixed` owner. It preserves payload addresses
and takes allocation bookkeeping from that same parent. The existing caller-buffer
representation is available through `arena.NewUnsafe(buffer)`, with explicit manual
lifetime and aliasing obligations. Exact API identity, including `Arena` receiver
origin, determines its unsafe audit classification; a name prefix does not.

Fixed and Chunked allocations use exact-range registrations. Reclaiming a suffix
invalidates only reclaimed allocations; earlier byte-adjacent siblings remain
valid. Registered child memory also depends on its backing's saved incarnation.
Heap and arena tags use persistent shared address history and disjoint tag ranges,
and exhaustion fails before an old view can match newly reused storage. The old
hosted modulo epoch permission is removed. Explicit unchecked builds and
`//pu:nochecks` retain their separate contract.

Mandatory teardown first detaches and invalidates the selected allocation records,
then releases their saved next-linked records through the typed internal
`rt.CatchAllocFree` hook. Callback exits are captured and cleared between releases.
Fixed backing and Chunked chunk/spare drains complete, cursor/mark state is finalized,
and only then does the public operation propagate Goexit. The helper retains the
ordinary allocator Free effect graph; it adds no generic callback or wait exemption.

## Focused passing evidence

The combined frozen source command used `PLUTO_SSACHECK=1` and selected:

```text
TestSafeArenaMinimal
TestChunkedIncarnationExhaustion
TestFixedParentAllocationCost
TestSafeArenaFree
TestSafeArenaDropCallback
TestDeferredAllocatorRestore
TestArenaEpochs
TestGoexitCleanupBalance
TestSchedulerHookGoexit
TestAllocatorCreationHookGoexit
TestSourceLateHookGoexit
TestBoundLateHookGoexit
```

All selected tests pass in **11.680 seconds**, exit 0. Full command output and
exact status are preserved in `/tmp/plutonium-arena-bounded-ssa-third.log` and
`/tmp/plutonium-arena-bounded-ssa-third.exit`. This includes 28 new O0/O2 arena
cases, 22 legacy epoch cases, the retained core cleanup matrix and four hook
regressions. The exact scope is bounded; it is not the entire build suite.

| Scope | Required observed behavior |
|---|---|
| Fixed retained view after Reset/reallocation | Checked panic before reading the new value through the old heap-retained pointer. |
| Prefix across Release and fresh allocations across Reset | Exact output `7 8`, `7 9`, `11`; earlier prefix remains valid. |
| Byte-adjacent siblings and zero capacity/length | Live byte survives suffix release/reuse; empty slice performs no access. |
| Chunked incarnation exhaustion | Loud exhaustion or checked stale-view panic before printing a stale match after 32768 reuse attempts. |
| Parent cost and cleanup accounting | One payload plus two metadata records; suffix release, Reset and final Drop reduce live allocations to zero, with exactly three matching frees. |
| Latest Free, double/stale Free after reuse | Normal latest Free succeeds; an old pointer cannot reclaim the current same-address allocation. |
| Free callback Goexit | Fixed and Chunked finish all three releases; callback tails are skipped; one task-exit request propagates after cleanup. Normal callbacks execute all three tails. |
| Deferred allocator override | Direct and deferred installation both remain active until Restore runs; default is restored afterwards at O0/O2. |
| Legacy heap-retained pointers/strings/boxes/errors | Existing epoch and counted cleanup rejection controls pass with the new registration model. |

The initial source/API checkpoint passed the checks below. Final source positions,
lint and generated artifacts are recorded separately below.

| Command/scope | Result | Evidence |
|---|---|---|
| `go test ./internal/audit -count=1` | PASS, 0.281s | `/tmp/plutonium-arena-audit-controls-final.log`, `.exit` |
| `go test ./internal/escape -run '^(TestAccepted\|TestRejected)$' -count=1` | PASS, 14.806s; existing acceptance/rejection expectations retained | `/tmp/plutonium-arena-migration-escape.log`, `.exit` |
| Two CLI diagnostic fixtures, default and brief | PASS, 2.369s; inspected goldens change source positions only | `/tmp/plutonium-arena-migration-diag-final.log`, `.exit` |
| `TestArenaCleanupPreservesWaitEffect` | PASS, 0.264s; a real may-wait Free remains rejected on a panic path | `/tmp/plutonium-arena-wait-effect.log`, `.exit` |
| Repository formatting check | PASS, 0.109s | `/tmp/plutonium-arena-format-check.log`, `.exit` |
| `golangci-lint run ./...` | 0 issues, exit 0 | `/tmp/plutonium-arena-lint.log`, `.exit` |
| Initial selfhost translation freshness | PASS, 8.729s, exit 0; before the later range/optimizer changes | `/tmp/plutonium-arena-translations.log`, `.exit` |

The manual-arena audit golden distinguishes direct/indirect manual calls
from NewFixed, Fixed methods, queries and unrelated lookalike names. The existing
counted-cleanup audit golden initially changed only the runtime source call
position `rt.p:839` to `rt.p:842`; no effect path or assertion was removed.

The first covered run reported a stale manual-arena golden after final cleanup
changes added the intentional Reset-to-Goexit and panic/runtime paths. Exact API
labels and all assertions were unchanged. The full 76-line golden was regenerated
and read, preserving every new reachable path; the focused non-update audit check
passes. Evidence is `/tmp/plutonium-arena-audit-cleanup-update.log` and
`/tmp/plutonium-arena-audit-cleanup-final.log`, with their exact exit files. The
first covered output/profile remains failed-run evidence and cannot supply the
required final passing all-package gate.

The final `panicStaleRange` insertion shifted nine existing audit locations in
`rt.p` by 21 lines. Both affected goldens were regenerated through their existing
update mechanism and their full diffs read: eight manual-arena locations and one
counted-cleanup location changed, with all labels, paths and assertions preserved.
The **full non-update audit suite passes in 0.269s**, exit 0
(`/tmp/plutonium-arena-audit-final-preflight.log`, `.exit`). The position-only
update itself passes in 0.231s (`/tmp/plutonium-arena-audit-position-update.log`,
`.exit`).

Ordinary escape/default-window, slices, mapalloc, genlib, Chunked typed-allocation
and NumError fixtures migrate to NewFixed. Vector cleanup precedes Reset; counted
NumError references are released before Reset and the global Fixed is explicitly
dropped. Caller-buffer task-stack location, dirty-buffer zeroing and raw alignment
tests intentionally use NewUnsafe. The measured Chunked/NumError native scope is
reported below; the remaining migrated library cases also pass in the final full suite.

The subsequent fixture repair has now passed all six selected normal/debug
library cases under `PLUTO_SSACHECK=1` in **49.684 seconds**, exit 0
(`/tmp/plutonium-arena-fixture-repair.log`, `.exit`): Chunked and strconvfloat
at O0/O2 plus both debug-allocator counterparts. Payload and metadata costs are
reported separately: the original seven payload chunks, one returned large
chunk and successful spare reuse remain asserted, alongside exactly 100 node
records, 51 records returned by Release, 50 records for reused scratch and one
record after Trim/reallocation. Scratch, vectors and map are dropped before
reclamation. After counted errors are cleared, the global NumError Fixed is
transferred with `take` into a local owner and then dropped; a direct global
Drop was rejected by the existing checker. These are valid lifetime repairs,
not removal of stale-access safety assertions.

## Failures preserved and repaired

- Manual Arena operations were not directly labeled unsafe. The first audit run
  fails in 0.226s (`/tmp/plutonium-arena-audit-first.log`, exit 1). Exact loaded
  constructor and receiver-origin classification repairs it without labeling
  ordinary Fixed or lookalikes unsafe.
- Stale latest-allocation Free could reclaim a newer allocation. The paired first
  run fails in 0.508s (`/tmp/plutonium-arena-free-first.log`, exit 1); normal cases
  pass. Incoming pointer generation/extent validation now occurs before reclaim.
- Parent Free Goexit skipped remaining metadata/backing release. Normal callbacks
  passed while exiting callbacks failed in 1.031s
  (`/tmp/plutonium-arena-cleanup-first.log`, exit 1). The completed typed drain
  captures each callback exit and propagates only after all required work.
- A produced owner value receiver lost its temporary identity while saving a defer.
  Deferred `alloc.Use(...).Restore()` therefore restored the override at the defer
  statement rather than function exit. First O0/O2 output was `direct true`,
  `defer false`, `restored true`, failing in 0.817s
  (`/tmp/plutonium-arena-defer-first.log`, exit 1). Returning the whole materialized
  value preserves its owner temporary until the deferred record takes it. Pointer
  and promoted receiver handling is unchanged. The earlier legacy counted failures
  came from this premature override restoration, not from lost arena tags.

## Final source gates

| Gate | Final result | Evidence |
|---|---|---|
| All-package covered tests | PASS, exit 0; build package 571.043s | `/tmp/plutonium-arena-covered-final.log`, `.exit` |
| Aggregate coverage and all 15 floors | PASS; 88.08943828% to 88.10920783% | `/tmp/plutonium-arena-coverage-comparison.json`, `/tmp/plutonium-arena-pkgcov.log` |
| Lint | PASS, 0 issues, exit 0 | `/tmp/plutonium-arena-lint-final.log`, `.exit` |
| Full build-suite SSA | PASS, 269.300s, exit 0 | `/tmp/plutonium-arena-full-ssa-final.log`, `.exit` |
| Full generation-off build suite | PASS, 115.971s, exit 0 | `/tmp/plutonium-arena-full-genoff.log`, `.exit` |
| Final audit/spec/format/translation preflight | PASS; source positions refreshed, all labels retained | `/tmp/plutonium-arena-audit-final-preflight.*`, `/tmp/plutonium-arena-preflight-{diag,format,translations}.*` |

The final all-package covered command passed:

```text
go test -count=1 -coverpkg=./internal/... -coverprofile=/tmp/plutonium-arena-cover.out ./...
```

The first command **exited 1**. Its complete log is
`/tmp/plutonium-arena-covered.log`, exact status is
`/tmp/plutonium-arena-covered.exit`, and its failed profile is preserved at
`/tmp/plutonium-arena-cover-first.out`. It cannot supply final all-package acceptance
or the required passing coverage comparison. It exposed:

- the stale audit golden described above;
- Chunked's newly explicit metadata allocation costs and owners still alive at
  reclaim, plus the forbidden direct global NumError Fixed Drop, repaired and
  verified by the six library cases above;
- six accepted stale partial-tail accesses at O0/O2: indexed access, a crossing
  scalar load and bulk copy. The repaired exact-bound checks and range-panic
  argument path pass all six plus the live-prefix O0/O2 controls in **1.910s**
  (`/tmp/plutonium-arena-tail-fixed-third.log`, `.exit`). The atomic `hi` snapshot
  is published under the record version; runtime range diagnostics receive the
  pointer and byte count and locate the first stale address. The old success
  outputs remain preserved as first-failure evidence;
- the unchanged 600-second timeout, with filepath O2 still compiling. A measured
  first profile took 34.873s with 48.22% in `phiUsesThreadable`; the initial pure
  local-use counting trial took 39.565s with 52.44% there. Passing SSA tests did
  not establish a performance improvement. The timeout log/profile and both
  measurements are retained. The accepted replacement and its measured improvement
  are described below; the final covered rerun passed.

The range-panic and initial threading changes required `internal/ssa/func.p` and
`internal/opt/gencheck.p`/`thread.p` for their corresponding Go sources.
The range update/freshness command passes in 3.073s and the thread
update/freshness command in 2.651s; logs are
`/tmp/plutonium-arena-range-translation-update.log` and
`/tmp/plutonium-arena-thread-translation-update.log`. The unsuccessful local-use
counting trial was replaced with an ephemeral use inventory owned by one immutable
threading scan. Each argument/control occurrence and phi-edge predecessor is
recorded; the inventory is discarded immediately after the first CFG mutation,
before the next scan. The unchanged safety predicate passes opt/SSA controls.
The same filepath O2 compile now passes in **17.173s**, exit 0, with 26.06 seconds
of sampled CPU time; the inventory accounts for 2.80 seconds (10.74%). The original
profile contained 42.58 sampled CPU seconds, including 20.53 seconds (48.22%) in
the repeated phi-use scan. This measures the concrete compile regression, not
application performance. Evidence is `/tmp/plutonium-arena-filepath-inventory.log`,
`.exit` and `.cpu`.

The final translation update/freshness check passes in **2.647s**, exit 0
(`/tmp/plutonium-arena-final-translation-update.log`, `.exit`). Exactly four
corresponding selfhost artifacts are required: `internal/opt/cfg.p`,
`internal/opt/thread.p`, `internal/opt/gencheck.p` and `internal/ssa/func.p`.
Final lint reports **0 issues**, exit 0 (`/tmp/plutonium-arena-lint-final.log`,
`.exit`). The final source freeze is `/tmp/plutonium-arena-source-final-freeze.json`,
containing 1850 non-document paths, including the new manual-arena audit golden.
No production source changes are pending in this candidate.

The attempted covered restart was explicitly interrupted after the audit-position
mismatch was identified. Its exit 1 at
`/tmp/plutonium-arena-covered-position-interrupted.log` and `.exit` is an
interrupted run, not a completed full-suite failure or acceptance. The goldens
above were refreshed before the final passing covered run.

The final covered all-package suite **exited 0**; `internal/build` completed in
**571.043s** with the default timeout and memory caps. This command supplies both
the all-package test gate and coverage measurement. Its evidence is
`/tmp/plutonium-arena-covered-final.log`, `.exit`, and the passing profile
`/tmp/plutonium-arena-cover.out` (SHA256
`5169166438917127d8e390b63d772781bd4f3bb996db5987ffc683b8696845d7`).
Aggregate statement coverage increases from landed `deeadf80738a`'s
**48458/55010 (88.08943828%)** to **48505/55051 (88.10920783%)**. All 15 required
package floors pass. The comparison is preserved in
`/tmp/plutonium-arena-coverage-comparison.json` and `/tmp/plutonium-arena-pkgcov.log`.

| Required package | Final coverage | Floor |
|---|---:|---:|
| arch/amd64 | 91.26892% | 86% |
| lower | 94.95679% | 85% |
| opt | 94.87058% | 90% |
| ssa | 88.21008% | 79% |
| load | 89.26554% | 71% |
| objfile | 95.12195% | 95% |
| syntax/parser | 85.51329% | 70% |
| syntax/printer | 94.68719% | 94% |
| syntax/format | 91.59664% | 91% |
| diff | 98.40000% | 98% |
| types2 | 76.74699% | 56% |
| escape | 95.00832% | 90% |
| moves | 93.26923% | 80% |
| race | 91.98292% | 90% |
| lsp | 89.29050% | 85% |

The **full build-suite SSA check passes in 269.300s**, exit 0
(`/tmp/plutonium-arena-full-ssa-final.log`, `.exit`). The **full generation-off
build suite passes in 115.971s**, exit 0 (`/tmp/plutonium-arena-full-genoff.log`,
`.exit`). Both use the final production candidate and default caps. The subsequent
audit refresh changed expected source locations only.

The cheap preflight on that final snapshot also passes: full audit 0.269s,
diagnostic spec/catalog checks 0.005s, repository formatting 0.111s and translation
freshness 2.411s. Their logs and exit files are
`/tmp/plutonium-arena-audit-final-preflight.*` and
`/tmp/plutonium-arena-preflight-{diag,format,translations}.*`.

The exact staged-tree archive `8ba34fe7224432077dba9c145bd40393d310f81a`
passes `go test -count=1 ./...`, exit 0 (`internal/build` **182.046s**), from
`/tmp/plutonium-arena-archive.C0qk07`. Its source hashes match all 1850 frozen
non-document paths. Only documentation status updates followed that snapshot;
all production, tests, fixtures and generated artifacts are byte-identical.
The archive compiler builds successfully and its embedded-runtime hello output
matches `examples/07-hello/expected` byte-for-byte with `PLUTOROOT` unset.
Evidence is `/tmp/plutonium-arena-archive-{tests,build,hello}.*`.

Final review approved the scoped implementation, tests, generated artifacts and
preserved audit/cleanup history after archive acceptance. Fetch confirms the
unchanged `deeadf80738a` origin/main basis; author email is
`578676+matjam@users.noreply.github.com`. The verified implementation was pushed
to main and installed with `GOBIN=$HOME/go/bin go install ./cmd/pluto`.
Installed embedded-runtime hello matches its exact oracle with `PLUTOROOT` unset.
The installed safe/manual API smoke prints `7\n9\n` and `vet -unsafe` labels
the Fixed function safe and the manual Arena function unsafe. Evidence is
`/tmp/plutonium-arena-{commit,rebase,push,install,installed-hello}.*` and
`/tmp/plutonium-arena-installed-api-{combined,cmp,vet}.*`.
Changed
lower/audit packages are not in the checked selfhost translation package list;
translation freshness is still part of full verification. No timeout, memory,
coverage target, negative test or specification rule has been weakened.

## Separate D6 and old item 96 work

D6 approves normal-exit resource cleanup but its shutdown semantics and
implementation remain pending. It is separate from this arena lifetime change.
Old item96 package-held classification, E3601 and source-proof/snapshot code was
discarded from active source after a verified durable archive. Independent landed
core Goexit fixes remain. The archive, exact 21-file restore/229-file removal
manifest and focused restoration results are recorded in
[item96 cleanup](2026-10-13-item96-cleanup.md). Optional old allocator/scheduler
candidates remain archive-only; none is imported into this feature.
