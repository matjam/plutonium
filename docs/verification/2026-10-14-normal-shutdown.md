# Orderly normal shutdown

Status: native verified; final review complete. This
record describes the D6 implementation against sections 15.4, 18.6 and 19.1,
ABI section 12.10 and the amended debug allocator exit rules. It does not
certify the whole compiler or close unrelated audit findings.

## Behavior and implementation

Normal main return finishes local cleanup, waits for tasks and scheduled
application callbacks through retirement, and then destroys package resources
in reverse actual initialization order. The full type-checker ready queue
retains zero-initialized variables as well as explicit initializer groups.
Imported dependencies outlive their clients. Explicit process exit, panic and
main-task termination retain their distinct behavior.

Package destruction preserves static addresses. Mandatory traversal zeros
completed places, and typed program writes into completed resource storage
panic before ownership transfer. This includes alias writes, deferred
clear/copy, finite atomic write adapters and existing safe syscall buffer-output
wrappers. Ordinary POD storage remains writable. User Drop callbacks suspend
traversal progress; their ordinary field replacement does not finalize pending
siblings. Progress is bound to the shutdown task, published atomically, and
ignored by other runtime service contexts.

Static initializer aggregates, copied method receivers, owning interface
payloads and caller-provided closure regions each preserve their actual cleanup
obligations. Per-capture mandatory region cleanup prevents a later destructor
from reviving an earlier released capture. Heap-region refill behavior is
unchanged. Cleanup ExitTask is accumulated until mandatory cleanup completes.

Application callback accounting includes pending AfterFunc work until firing or
explicit program cancellation. Channel-delivery and scheduler timers do not
independently keep the application alive. Timer Reset/Stop bookkeeping is
serialized, and callback completion and captured-resource disposal precede
retirement. Reclaimable stack caches are flushed before package allocators they
may borrow; runtime facilities needed by cleanup remain available.

The public task-termination name is `rt.ExitTask`, with `rt.CatchExitTask` for
the existing catch boundary. The user approved replacing the previous names
without compatibility aliases before 1.0. Internal flag names and historical
records are retained.

## Focused verification

All commands use the default timeout and memory guards. Native cases run at
O0 and O2; the combined shutdown run enables SSA verification after each pass.

| Check | Result | Evidence |
|---|---|---|
| Combined shutdown and existing closure-string lifetime regressions | PASS, 12.632s | `/tmp/plutonium-shutdown-focused-region-final.log`, exit 0 |
| Finalized caller-region capture, exact original failure | PASS, 1.563s | `/tmp/plutonium-shutdown-region-fixed.log`, exit 0 |
| Combined shutdown, timer handoff, existing published handoff and trace regression bundle | PASS, 18.747s | `/tmp/plutonium-shutdown-handoff-final-bundle.log`, exit 0 |
| NewTask-before-copy, Spawn-after-publication and runner rollback timer contrasts | Eight SSA/native cases PASS, 7.455s | `/tmp/plutonium-shutdown-timer-handoff-second.log`, exit 0 |
| Existing concurrency, timer, poller, Scope and task-exit bundle | Behavior passes; initial rename scaffolding failures corrected | `/tmp/plutonium-shutdown-affected-first.log` |
| Exact four affected fixture groups after coherent injected-source rename | PASS, 5.657s | `/tmp/plutonium-shutdown-affected-rename-fixed2.log`, exit 0 |
| Existing timer suite (60 native cases), task retirement, AfterFunc and timer handoff after exact retirement fix | PASS with SSA, 9.107s | `/tmp/plutonium-shutdown-timer-retirement-fixed.log`, exit 0 |
| Full audit and diagnostic packages after reviewed golden refresh | PASS, 0.357s / 0.050s | `/tmp/plutonium-shutdown-full-audit-diag.log`, exit 0 |
| All 22 original full-gate regressions plus shutdown/atomic cases at the local-completion checkpoint, before the final timer fix | PASS with SSA, 8.826s | `/tmp/plutonium-shutdown-frozen-local-bundle.log`, exit 0 |
| All-package uninstrumented tests | PASS, exit 0; native build 239.604s, CLI 151.959s | `/tmp/plutonium-shutdown-full-plain.jsonl`, `.exit` |
| Exhaustive sharded tests with aggregate coverage | PASS; all 196 native tests exactly once, all other packages | `/tmp/plutonium-shutdown-coverage-{plan,receipt}.json`, raw shard profiles |
| Lint on final frozen source | PASS, 0 issues | `/tmp/plutonium-shutdown-lint-final.log`, exit 0 |
| Full SSA build suite | PASS, 379.211s, exit 0 | `/tmp/plutonium-shutdown-full-ssa.log`, `.exit` |
| Full unchecked build suite | PASS, 151.540s, exit 0 | `/tmp/plutonium-shutdown-full-genoff.log`, `.exit` |
| Tracked-source archive, compiler build and embedded-runtime hello | PASS; all-package archive tests, native build 243.201s; exact hello | `/tmp/plutonium-shutdown-archive-{receipt.json,tests.log,build.log,hello.log}` |

The region failure before correction exited successfully after printing
`drop 1`, `revive`, and `unexpected committed`. The fixed program panics before
the replacement commits at both optimization levels. The first-failure log is
`/tmp/plutonium-shutdown-region-first.log`. Other preserved failures distinguish
real cleanup/progress/timer defects from renamed test-source scaffolding.

The initial covered all-package command was deliberately stopped with exit
143 when `TestTraceTask` retained an intentionally sleeping one-hour worker
after main returned. It is not a completed gate. The trace fixture now uses an
explicit wakeable park and main readies the observed sleeper; its four native
configurations preserve the exact trace output (3.498s). The interrupted run
also identified CLI panic expectations and unsafe-audit reports requiring only
the approved task API rename and changed source positions. Their expected
operations and classifications remain unchanged.

Read-only runtime review found two real timer dispatch defects under supported
scheduler hooks that end their caller with ExitTask: an accepted callback could
leave dispatch bookkeeping unfinished, and failed runner-start rollback could
leave a published runner parked. The exact internal dispatch helper reuses the
existing task handoff slot: retry only an unpublished callback, never duplicate
a published one. Rollback wakes an adopted runner while its identity is locked.
The first-failure and fixed logs retain both pre- and post-handoff cases.

The next complete covered command reported 22 regressions and timed out at
600.185s. Hooks had been emitted before the compiler knew whether the program
had any static resource obligations, inflating code and inlining cost even for
resource-free code. The final implementation defers the static-cleanup helper
jobs until the complete typed inventory is known. An empty protected inventory
elides provisional hooks before optimization, preserving incoming memory and
false/zero query results. ABI-only write adapters remain: their original call
lowering strips generation tags before an external implementation, including
when the adapter is called through a function value. The exact runtime `faultPanic` record remains
fault-reporting infrastructure under the documented named contract; it does not
create a fictitious application obligation. Named zero owners and hidden
resource-bearing objects still require protection.

With a real global channel, the overflow fixture still requires protection of
that channel, but its bounds-checked local array writes do not overlap it.
Compiler-known local inline array elements now retain their physical local
identity. This is not applied to arbitrary pointer arithmetic, loaded backing
pointers or unchecked dynamic indices. All 22 original assertions and the
shutdown safety matrix pass unchanged after these corrections.

The same full run exposed a kernel-mode test executing `cli` at normal return
without any tasks. Preserved binary/GDB evidence identifies `irqSave+2` through
the new shutdown lock, after all expected program output. Closing admission in
the original single startup context now avoids scheduler locking; the admitted
task path retains its locked close. Per-CPU, basic, task retirement and callback
controls pass with SSA at O0/O2 (6.882s).

The subsequent covered run timed out at 600.599s with one named functional
failure in `TestRaces/synced_object`. GDB showed valid allocated data but a
generation-tagged, noncanonical pointer reaching the atomic operation. The
first applicability prototype had redirected an internal adapter to a raw
external symbol after argument lowering, bypassing the external ABI's pointer
untagging. Removing that symbol rewrite and retaining the thin ABI adapter
fixes the exact one-, two- and 24-processor case (0.880s).
The permanent counterpart tests tagged heap operands through direct atomic
calls and saved/global function values at O0/O2 with SSA checks (0.514s), rather
than relying only on untagged static-address tests.

A CPU profile of the exact remaining debug-allocator `strconvfloat` case
measured 27.865s, including 7.42s in SimplifyCFG, 6.52s in inlining and 9.36s
in register allocation (cumulative categories overlap). Existing `noinline`
requests on the two internal table-scan helpers, Check and Done, keep those
scans out of every guarded write/drop. The identical semantic oracle passes in
22.226s; sampled Build CPU decreases from 23.68s to 16.69s and allocator CPU
from 9.36s to 4.81s. This is a scoped compiler-cost measurement, not an
application runtime benchmark. No optimizer algorithm or limit was changed.

The next covered run had no named functional failures but still reached the
600.201s package timeout, with examples and counted-path-error cases
in flight. The accepted baseline's covered build suite already took 571.043s,
leaving little margin for the added shutdown cases. A bounded same-case baseline
profile passes in 15.874s, with 13.22s sampled compiler CPU. The current case
initially used 16.69s compiler CPU. Keeping Pause/Resume out of each Drop frame
and omitting completion queries for the same proven inline local storage already
excluded from write checks reduces this to 13.48s compiler CPU and 18.923s wall
time. The local query necessarily returns false because local storage cannot
overlap the static table; unknown pointers, static objects and transferred
payloads retain their completion checks. The optimizer and allocator samples
decrease from 7.59s/4.81s to 5.89s/3.78s. These profiles isolate compiler cost;
they do not authorize raising a test limit or treating a timed-out gate as a pass.

The covered retry still timed out at 600.215s and exposed a real intermittent
timer failure in the unchanged sleep-order oracle at O2 with 24 processors.
The preserved binary maps the faulting PC to the newly added read of
`Timer.system` after publishing `Timer.heap = nil`. A stopped stack-local timer
may already have returned to its caller at that publication point. Retirement
must snapshot the flag before the callback and make the nil publication its
last timer-object access. That correction passes all 60 existing native timer
cases plus 16 shutdown/callback cases under SSA checks in 9.107s. That attempted
full covered gate remains failed. Its timeout also left queued tests, so its
active set alone cannot establish how close the entire suite was to completion.
The subsequent all-package run records uninstrumented JSON test events.

The subsequent `go test -json -count=1 ./...` passes all packages, including all
196 top-level native compiler tests and 70 new shutdown leaf cases. Its JSON
events preserve individual outcomes and timings. The new leaf durations
overlap and are not additive suite wall time. To finish coverage without
weakening the default per-process limit, coverage used the exact discovered
native inventory in three disjoint anchored sets (64, 67 and 65 tests), plus
every other package with its default test selection. Each subtest remains
selected by its parent. The receipt checks terminal outcomes against the full
inventory, records skips and source hashes, and merges the raw `set` profiles
by identical source block and statement count using maximum counters. This
replaces only the coverage measurement's single process; the all-package
correctness gate passes separately. No single full covered command is claimed
to have passed.

Three common-subset generator fixtures also relied on process termination
abandoning live tasks. They now stop explicitly: select signals its producer,
the sieve closes a bounded source/filter chain, and the lazy power-series
network cancels its request/data handshakes. Independent Go execution matches
their unchanged 47-, 72- and 307-byte output oracles. Native SSA verification
passes at O0/O2, including adjacent unchanged select variants (3.766s,
`/tmp/plutonium-shutdown-fixture-native.log`). No expected output was weakened.

First-failure logs, corrected focused results and the frozen source manifest
are preserved outside temporary storage at
`/home/matjam/.codex/recovery/plutonium-shutdown-2026-10-14/`, with SHA256 hashes.

## Coverage comparison

The accepted baseline is commit `408dd2ab`, whose arena verification record
documents **48505/55051 statements (88.10920783%)** with all 15 required package
floors. Its old raw profile is unavailable in the current temporary directory;
the committed verified counters in
[the arena record](2026-10-13-safe-arena-lifetimes.md) are the comparison basis.
The passing current merged profile covers **48735/55301 statements
(88.12679698%)**. All 15 package floors pass. The three native shards finish
in 202.585s, 250.894s and 290.985s; the other-package run finishes in 101.103s.
All exit 0 with the default timeout and memory guards. The native inventory
has no skips. Other packages report three packages without test files and the
existing VSCode protocol test skips because its npm dependencies are absent;
no test selection or skip condition was changed.

The plan, raw profiles, JSON events, outcomes, source manifest, comparison and
merged profile are retained in the recovery directory above. The merged profile
SHA256 is `eb7fb153fbf13c71a175d90a0093742e4133513a279cb28519406c0fe6064485`. The exact block
merge validates statement-count agreement, and each of the 196 discovered
top-level native tests has exactly one terminal result in the planned shard.
The other 36 package paths receive default test selection. No single-process
covered command is represented as passing.


| Required package | Current coverage | Floor |
|---|---:|---:|
| arch/amd64 | 91.26892% | 86% |
| lower | 94.98386% | 85% |
| opt | 94.51437% | 90% |
| ssa | 88.21008% | 79% |
| load | 89.26554% | 71% |
| objfile | 95.12195% | 95% |
| syntax/parser | 85.51329% | 70% |
| syntax/printer | 94.68719% | 94% |
| syntax/format | 91.59664% | 91% |
| diff | 98.40000% | 98% |
| types2 | 76.76448% | 56% |
| escape | 95.03605% | 90% |
| moves | 93.26923% | 80% |
| race | 92.05408% | 90% |
| lsp | 89.29050% | 85% |

## Archive and shipping receipt

The staged-source tree `47df9063a8bddb682ef613014e610242114fcaf6` passes
`go test -count=1 ./...` from `/tmp/plutonium-shutdown-archive.t5vdhyj3`,
exit 0. All 1,862 non-document source hashes match the frozen candidate.
The archive compiler builds and its embedded-runtime hello matches
`examples/07-hello/expected` byte-for-byte with `PLUTOROOT` unset. Only
documentation status/receipt edits follow that source archive. Final review
accepted the scoped implementation, tests, preserved historical evidence and
coverage qualification. No timeout, memory cap, negative oracle or coverage
floor was weakened.

Shipping uses a fresh `origin/main` comparison, the project noreply author,
`git push origin HEAD:main` and `GOBIN=$HOME/go/bin go install ./cmd/pluto`.
The durable shipping receipt records the actual commit, installed binary hash
and build revision, exact installed hello and task-exit/global-drop smoke,
and primary-checkout reconciliation. It is written after shipping alongside
the evidence above; this source-verification record does not anticipate a
commit hash or represent an unperformed installation as passing.
