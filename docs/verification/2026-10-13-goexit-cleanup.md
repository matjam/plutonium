# Task-exit cleanup verification, native Linux amd64

Status: native verification passed; final review complete. This independent follow-up implements sections 17.3,
18.6, and 15.4. Package-variable debug allocator reporting (item 96) remains unfinished.

## Behavior and exact oracles

A `Drop` method or allocator callback can call `rt.Goexit`. Generated destruction now completes
the remaining fields, elements, backing release, and drop bookkeeping before propagating that
exit. Its shared cleanup collector captures and clears each callback's exit before the next
callback. Mandatory frame, scope, and deferred cleanup use the same collector rather than
entering the current frame's cleanup again.

`finishReturn` collects pending exits after frame and deferred cleanup, before destroying
unreceived results and transferring normal results through the ABI. An exiting caller receives
no owned results. Deferred writes to named results are therefore destroyed here; counted results
held in the caller's region keep that region's reference and take a separate result reference
only on a normal return. A further exit during result destruction still completes the other slots.

Evaluated owner results stay in registered statement temporaries until a store or invocation
takes them. Replacement destroys and clears the old owner before checking for an exit, so an
abandoned replacement still destroys its evaluated right-hand side without dropping the old
owner twice. Counted replacement preserves its existing install-before-release ordering and
checks the exit before later assignment targets.

Task argument records keep provisional references while creation can still exit. The runtime
sets a handoff flag after initializing the new task and copying its arguments, immediately before
entering the backend's `Spawn`. Lowering retires the caller's provisional cells on that signal.
Creation before handoff releases them in the caller; the backend owns published records. Local,
literal, and produced owners use the same staging, including boxed method receivers and counted
string arguments.

Reference-hold elision identifies the actual generated reference-release call at emission.
Task-exit status calls and other effects stay outside the elidable releases, including calls
that return no value. This preserves their results and control flow when a temporary reference
is unnecessary.

Branch threading's safety predicate now returns on its first unsafe use. The dominance and
phi-edge checks are unchanged. The predicate has no mutations or observable effects, and once
one use rejects a branch, later uses cannot make it safe. This avoids repeated whole-function
scans exposed by the additional cleanup control flow.

Register allocation now tests whether a node has participating moves without constructing a
filtered slice. Enabling moves visits the existing node move list and moves only active entries
to the worklist, preserving their order and state transitions. The freeze eligibility check uses
the same predicate. Coloring, coalescing, and move selection are unchanged. Unit controls cover
active, worklist, overlapping, and completed states, repeated entries and shared nodes, and
neighbor eligibility after freezing.

The normal-allocator cleanup matrix preserves 29 exact event, order, tail, defer, right-hand-side,
and later-store assertions at O0 and O2. A test-only accessor to the existing `current().drops`
checks balance after `CatchGoexit` resumes on the same task. `stringFreeCallback` and
`allocatedStringStoreCallback` verify deferred `dropEnd` after an allocator exits. The following
field's `After.Drop`, through a normally returning `idle`, reaches `record(100)` after the previous
callback's exit has been collected. The named-result matrix has 22 cases, including
`cleanupExit`, `cleanupCountedExit`, and `cleanupFreeExit` for second exits during result cleanup.
Task handoff covers six creation-exit cases and four normal/published cases at both levels.

The combined SSA-checked gate also includes the four existing bound-hook regressions,
`TestBuildTestFailNow`, `TestBoxHoldElision`, and the existing boxed-task processor matrix.
It passes in 6.091 seconds. These receipts cover exits raised by the tested callbacks; they do
not claim the private item-96 token tests or manually pre-set internal flags in unrelated callees.

## Acceptance record

Linux x86-64, Go 1.27.1, golangci-lint 2.14.0. The default timeout, parallelism, and memory guards
remain unchanged. The repaired cleanup checkpoint has 24 source/test SHA-256 receipts. The
final frozen candidate has 29 source/test/generated-file receipts at
`/tmp/pluto-core-goexit-freeze-regalloc.sha256`, including both allocation optimizations and
their required translations.

The fresh tracked `83a014f2` baseline covered suite passed. Its profile checksum is
`fa5da8551e45007d8aea5fa1b70a969f8b42cf5eefd0407129969a1b69fc32b1`.
Merging duplicate blocks by logical OR covers 48300/54851 statements, 88.05673552%.

| Gate | Result |
|---|---|
| Combined focused SSA cleanup, results, handoff, hold elision, boxed tasks, and hooks | Exit 0; 6.091 seconds |
| Same focused SSA gate with final early-return optimization | Exit 0; 5.218 seconds |
| Existing branch-threading safety regressions | Exit 0; 0.001 seconds |
| Plain all-package suite | Exit 0; internal/build 281.406 seconds; cmd/pluto 211.853 seconds |
| Lint on final source | Exit 0; 0 issues |
| Backend, SSA, and optimizer unit packages after move-list repair | Exit 0; 0.006, 0.001, and 0.029 seconds |
| Optimized filepath build with SSA checks and CPU profile | Exit 0; 64.895 seconds |
| Full covered suite and exact baseline comparison | Exit 0; internal/build 513.567 seconds; cmd/pluto 220.780 seconds; aggregate 88.05673552% to 88.08943828% |
| Full SSA build checks | Exit 0; 204.785 seconds |
| Full generation-off build suite | Exit 0; 113.991 seconds |
| Explicit tracked/approved archive full suite and embedded hello | Exit 0; internal/build 155.554 seconds; cmd/pluto 141.335 seconds; embedded-runtime hello matches its complete oracle |

The archive inventory contains 1897 baseline tracked paths and five explicitly approved additions:
three native test files, the release-metadata unit test, and this verification record. It contains
no ignored worktree inputs or unfinished package-reporting source.

The successful complete covered profile covers 48458/55010 statements. Every required floor
passes: amd64 91.2689%, lower 94.9461%, opt 94.8071%, SSA 88.2101%, load 89.2655%,
objfile 95.1220%, parser 85.5133%, printer 94.6872%, format 91.5966%, diff 98.4000%,
types2 76.7319%, escape 95.0083%, moves 93.2692%, race 91.9829%, and LSP 89.2905%.
Packages with unchanged statement counts have no covered-statement variation from the baseline.
The complete command, profile, comparison, and package report are retained under
`/tmp/pluto-core-goexit-coverage-regalloc.log`, `/tmp/pluto-core-goexit-cover-regalloc.out`,
`/tmp/pluto-core-goexit-coverage-comparison-final.log`, and `/tmp/pluto-core-goexit-pkgcov-final.log`.
The full mode checks are retained in `/tmp/pluto-core-goexit-full-ssa.log` and
`/tmp/pluto-core-goexit-genoff.log`. The archive suite and exact hello receipts are
`/tmp/pluto-core-goexit-archive-full-tests.log` and `/tmp/pluto-core-goexit-archive-hello.out`.

## Failed-run evidence

The first extracted full gate exited 1, with the complete dump retained in
`/tmp/pluto-core-goexit-full-tests.log`. `TestBuildTestFailNow` with SSA checks first rejected
`fmt.compareValues` after `strelide`: exit collection had been recorded as a reference release,
so elision replaced a boolean result by memory. The metadata regression failed before the
emission-identity repair; the original native fixture subsequently passed.

The same full gate exposed a task receiver temporary being dropped before the child used it.
Narrow task controls then demonstrated missing counted cleanup before publication and missing
staging for moved local and literal owner arguments. The handoff signal and provisional staging
repair these paths without changing scheduler selection or task execution rules.

The first covered candidate run passed the CLI package in 428.915 seconds, then the native build
package reached the unchanged ten-minute timeout. The stack dump repeatedly shows
`phiUsesThreadable` scanning uses during inline CFG cleanup, after an unsafe use could already
have rejected that branch. The complete failed log and partial profile are preserved in
`/tmp/pluto-core-goexit-coverage.log` and `/tmp/pluto-core-goexit-cover-timeout.out`; neither is an
acceptance profile. The verification limits remain unchanged.
After the reviewed early return, the covered CLI package passed in 229.199 seconds with the
same command and limits; the complete native coverage gate remained pending at that checkpoint.

That run's native compiler package passed in 570.732 seconds, but the complete command exited 1
because `TestTranslationsUpToDate` found the optimizer's self-hosting copy stale. Regeneration
changed only `selfhost/internal/opt/thread.p`. Its formatting check passed, and the full covered
translation package passed in 46.483 seconds. The failed all-package profile is retained as
failed-run evidence, and a fresh exact all-package covered command is required for acceptance.

The clean rerun passed the refreshed translation tests and all other packages but reached the
same 600-second timeout with only `TestBuildTest/pluto/path/filepath/O2` remaining. Its runnable
compiler was in register allocation (`nodeMoves`, `enableMoves`, `decrementDegree`, `simplify`).
The complete log and partial profile remain preserved separately in
`/tmp/pluto-core-goexit-coverage-complete.log` and `/tmp/pluto-core-goexit-cover-last-timeout.out`.
No timeout or memory limit was raised, and this failed run is not an acceptance result.
The targeted move-state control then reproduced one allocation per positive predicate call.
The reviewed direct iteration removes that allocation and passes the complete backend, SSA,
and optimizer unit packages. Regeneration changed only the register allocator's required
self-hosting copy; its formatting check passed.

The candidate was extracted into an isolated worktree at `83a014f2`. It includes no package-root
descriptors, saved transaction tokens, source-effect proofs, diagnostic precision-limit feature,
late-hook inference addition. Its cost repairs are the pure early return and move-list traversal
changes described above.
The primary item-96 checkout remains
separate. No language specification, allocator retention policy, test oracle, or verification cap
was weakened.
