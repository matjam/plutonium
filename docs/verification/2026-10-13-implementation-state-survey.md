# Implementation-state survey, 2026-10-13

Status: source-backed audit checkpoint. Implementation is paused. The user has
approved normal-exit resource cleanup (D6), but its specification and code are
not complete. This report preserves the unfinished work and distinguishes
shipped verification from source presence and focused evidence.

Subsequent cleanup archived and removed the old item 96 implementation from the
active checkout. The snapshot observations below remain historical evidence;
see [cleanup ledger](2026-10-13-item96-cleanup.md). D7 arena work proceeds separately.

## Shipped and verified

HEAD and locally recorded origin/main are
`deeadf80738a31e3c579b14d63aa7f492ed2b346`. The installed compiler is the
verified core cleanup landing, not the dirty item96 candidate. No audit fetch
or installation occurred.

The landed code completes required destruction after Goexit from user Drop or
allocator callbacks, stages produced owner temporaries until actual transfer,
cleans unreceived named results on Goexit, and distinguishes provisional task
arguments from scheduler-owned records using the internal handoff signal.
Reference-release metadata excludes unrelated status effects. The landed
optimizer/backend changes remove measured redundant work without changing
their safety predicates or move-state transitions.

These claims have native O0/O2 and SSA evidence, plus full tests, lint, covered
tests with nondecrease and all package floors, full SSA, generation-off and a
tracked archive gate. Aggregate coverage is 48,458/55,010, **88.08943828%**.
See `2026-10-13-goexit-cleanup.md` for the exact accepted scopes and failed-run
evidence. This full gate does not cover the uncommitted root reporting work.

## Reproduced safety boundary

A typed application importing only `pluto/arena` allocates an int from a fixed
Arena, saves the pointer in a heap Holder, resets the arena, reallocates the
same location and writes 99. Reading the retained pointer prints **99** and
exits successfully. There is no unsafe import or unchecked directive in the
application. The corresponding Chunked program panics with “use of memory
from an arena that was reset” and exits 2.

The exact fixed-Arena fixture is:

```go
package main

import "pluto/arena"

var memory [4096]byte
var a = arena.New(memory[:])

type Holder struct { p *int }
var held = new(Holder)

func retain() {
    p := a.New[int]()
    *p = 7
    held.p = p
}

func main() {
    retain()
    a.Reset()
    p := a.New[int]()
    *p = 99
    println(*held.p)
}
```

The Chunked counterpart replaces the two fixed-buffer declarations with
`var a = arena.NewChunked()`. The compiler was built from the current dirty
checkout with `go build -o /tmp/plutonium-safety-survey-pluto ./cmd/pluto`
(exit0), SHA-256
`c7d0091f44cdc117e1d7895ea80a202d85ec699d8a8053f55dcc6ef4765fff26`.
Both runs used its embedded standard library and the default checked mode.

| Command | Exit | Complete output |
|---|---:|---|
| `/tmp/plutonium-safety-survey-pluto run -O0 /tmp/plutonium-safety-fixed` | 0 | `99\n` |
| `/tmp/plutonium-safety-survey-pluto run -O0 /tmp/plutonium-safety-chunked` | 2 | `panic: runtime error: use of memory from an arena that was reset at main.main (./main.p:18)\n    main.main (./main.p:21)\n` |

Only O0 was probed; no epoch-wrap or O2 campaign was run. An earlier ordinary
`new(int, &a)` owner-to-view storage control was correctly rejected with
E3002 because its local owner would die. The accepted route is the typed
arena API, not absence of the general local-owner escape check.

The fixture is `/tmp/plutonium-safety-fixed/main.p`; retained outputs are
`/tmp/plutonium-safety-fixed-typed.log` and
`/tmp/plutonium-safety-chunked-typed.log`. The safety reviewer owns the exact
command and checked-mode receipt. This demonstrates safe-looking API
reachability of the documented fixed-arena exception. It does not claim
arbitrary memory corruption or that the implementation violates the explicit
exception in current section18.11. It does prevent treating that exception
as compatible with an unqualified safe-language lifetime guarantee.

No repair was made. The specification must choose enforceable safe lifetime
handling or a visible unsafe boundary, as described in the specification audit.

## Normal shutdown is not implemented

Current source establishes the following path:

| Operation | Source and behavior | Assessment |
|---|---|---|
| Compiler entry | `internal/lower/global.go`, lowerEntry: dependency-ordered package initializers, then main, then status return | No package-owner shutdown glue is generated here. |
| Hosted return | `lib/pluto/rt/rt_linux.p`, start/mainOnStack: programMain, mainReturned, Exit | Main return reaches runtime exit; this is not D6 resource cleanup. |
| Task handling | `lib/pluto/rt/sched.p`, mainReturned: invoke exitWaitFn if bound | Waits for begun drops through the selected backend; it does not join or destroy every surviving task. |
| Exit | `lib/pluto/rt/rt_linux.p`, Exit: flush stack cache, clear/call exitHook, exit syscall | The single hook is shared by normal main return and explicit Exit. No package destruction sequence exists. |
| Debug hook | `lib/pluto/alloc/debug/debug_linux.p`, atExit: SettleTasks, CheckHeap, ReportLeaks, prior hook | Debug reporting does not replace ordinary resource cleanup. |
| Settlement | `lib/pluto/rt/sched_linux.p`, SettleTasks: bounded wait until other tasks return or wait | Quiescence of blocked tasks does not mean their resources have been destroyed or are safe to destroy underneath them. |

The direction approved in D6 therefore remains a design and implementation gap.
Task termination/join policy, dependency ordering, counted releases, cleanup
failure propagation and abrupt exit semantics must be specified first. A
kernel that does not terminate requires no artificial exit phase.

## Old item96 work is present, incomplete and preserved

The pre-audit dirty snapshot contained 23 tracked modifications and 227
untracked path entries. New untracked source totaled 226 files and 29,842 text
lines, including 125 production files and 19,938 lines. These are file text
counts, not proof coverage. D6 and the audit reports add documentation after
that snapshot.

The candidate contains compiler root descriptors, owner-backing recipes,
source/global/foreign effect analyses, iterative runtime tracing, immutable
snapshot report rows, private metadata allocation and scheduler snapshot
coordination. Source presence is not acceptance.

The basic `TestDebugReportAfterCompleteAllocation` native oracle passed at
O0/O2. `TestDebugRootSymbolizerAfterSnapshot` still rejects its Report before
the callback. In `internal/lower/strscope.go`, startBody opens a body token
when debugOwnRootMutation is true. The last checked source query propagates
unknown/rooted PageProvider effects from `Slab.Free`/genFreeLarge into fill
and main. The token then remains open across Report. The statement token
listing is balanced; the selected whole-function span causes rejection.

`internal/lower/debugsnapshot.go` supplies statement/call transfers and polls,
excluding runtime/debug package bodies. `lib/pluto/rt/debugsnapshot.p`
rejects a snapshot while lock/drop/store/runtime/report depth is nonzero.
The remaining defect cannot be solved by forcing runtime admission. No
additional proof rules or runtime patches were made during this audit.

Focused owner-recipe, E3601, origin/publication and geometry tests exist, but
the full supported reporting matrix and required full gates have not passed.
E3601 rejects some valid opaque custom raw owners instead of producing false
classification. That was a precision/conformance gap under the old policy;
it did not prove arbitrary custom-owner support complete. A Backing proposal
is documentation only and has no approved public API implementation.

D6 changes the premise of exit-time package-held classification. Preserve and
review this candidate before retiring or adapting it. Review separately any
demonstrated independent safety repairs useful under either exit policy.
Do not make arbitrary opaque classification a completion requirement merely
because the old candidate attempted it.

## Front-end and library implementation inventory

The second bounded survey inspected the actual syntax/type/lowering entrypoints
and named existing tests. The current compiler built successfully for the
probes above. These mechanisms are implemented, but that build and source
inventory do not certify every path in the dirty checkout.

| Area | Current mechanism and evidence | Assessment |
|---|---|---|
| Syntax and formatting | `internal/syntax/parser`, AST/scanner, printer/format; parser/format or-clause tests, TestSameAsGoFormat and repository-format tests | Implemented, existing regression inventory. Not rerun during the survey. |
| Enum and tagged union | `internal/types2/enum.go`, taggedunion.go, checker statements and union lowering; enum/union/native tests | Implemented. Alias wording remains a specification conflict. |
| Packed/volatile/aligned/ring | types2 Plutonium types and lower builtin operations | Implemented. Packed reserved-bit conversion rule remains unsettled; owner-ring nonconsuming results require review. |
| Generics/defaults/promotion | types2 defaults, instantiation/method lookup, plutonium_litfield and indexing; default/generic/promoted fixtures | Implemented. Promoted literal behavior follows 11.3, contrary to 7.6. |
| Boxes/counts/errors/strings | distinct ownership kinds, lower retain/release, fmt/errors/os/strconv library paths | Prior native records cover counted errors, assertions, As, wrapped trees and releases. A generic assertion precision limit remains documented. |
| Initialization | load/build ordering and whole-program task-start reachability, inittasks tests | No-task-during-init rule has a native verification record. Normal shutdown is a separate missing design and implementation. |
| Runtime/platform/tools | scheduler/allocator runtime, generation directory, doc/vet diagnostics and source graph | Named native verification records establish their frozen scopes. They do not establish near-Rust performance or current dirty-tree acceptance. |

Six small frontend probes ran in three feature groups using the same current
compiler hash above, with command
`env -u PLUTOROOT /tmp/plutonium-safety-survey-pluto vet <case-directory>`.
Vet checks source and unsafe paths; it does not lower, link or run the program.
Complete source/log/exit receipts remain under
`/tmp/plutonium-foundation-probes/`.

| Contrast | Current result | What it establishes |
|---|---|---|
| owner ring Peek and All, plus int control | All three frontend probes exit0 | Owner nonconsuming operations are accepted. Inspected signatures return the owner element T, and lower operations do not specialize an owner-view result. Native double-drop/wrong-code behavior was not proved. Settle the contradictory ownership API before a conformance fix. |
| promoted struct literal `Outer{a:7}` through embedded value Inner | Exit0 | Current checker uses the promoted field path, agreeing with 11.3. This is not an implementation omission. |
| local ErrorBuf cause returned through `%w`, versus static error | Local case exit1/E3003; static case exit0 | Compiler rejects the escaping borrowed holder. The broad spec keep-alive wording is wrong; counting the wrapper does not own a borrowed cause. |

For the ErrorBuf case, the exact first message is
`cannot pass &b to fmt.Errorf, which keeps it after returning` at main.p:7:9.
`lib/pluto/fmt/errorf.p` stores the original wrapped cause and exposes borrowed
ErrorBuf text. The counted wrapper owns its message and counted children, not
the stack holder. No accepted unsafe lifetime behavior was found in that pair.

The principal-feature source matrix and detailed entrypoints are preserved in
`/tmp/plutonium-implementation-foundations-survey.md`. Old roadmap prose still
contains Open labels for some already implemented features; those labels alone
cannot establish missing code or contradict current verified snapshots.

## Evidence limits and next gates

The bounded safety source survey confirms substantial existing mechanisms:

| Area | Source-backed implementation | Limits of this assessment |
|---|---|---|
| Frame and local-owner views | `internal/escape` whole-program actual parameter/result summaries and local flow; `internal/moves/views.go` stale-view tracking on move/drop/reassignment | Existing tests cover return/interface/closure/helper/temporary holders. Escape paths are bounded (three loads/eight steps, two field levels); this survey did not rerun those suites. |
| Owner moves and emptiness | `internal/moves` live/moved/maybe-moved joins, loop fixed points and Empty/Fresh facts supplied to lowering | Source presence and test inventory establish implementation, not a proof of every native cleanup exit. |
| Sendability | `internal/race/ownedview.go` records field stores, address exposure, freshness, retention and crossing; owning-field inference is used by sendable.go | Direct alias controls exist. Custom unsafe allocator freshness and owner invariants remain trusted contracts; no universal unsafe uniqueness theorem was proved. |
| Race checks | Whole-program access/lock summaries and task expansion in `internal/race` | Maps shared across tasks merge by type under the specification; this is conservative precision. |
| Scoped tasks | `internal/scoped` shared task/capture/partition inventory consumed by moves/escape/race | Direct local Scope plus literal recognition is implemented; pointer/helper/nonliteral calls remain unscoped by the stated rule. Same-type slice and moving-container partition limits remain. |

The fixed-arena result is a reproduced boundary. Other listed bounds are
conservative or trusted preconditions, not newly demonstrated accepted races
or memory corruption. Source references and test inventories are retained in
`/tmp/plutonium-implementation-safety-survey.md`.

The source survey is not a complete proof of the compiler. The shipped gate
provides broad regression evidence; known conservative analysis boundaries
and untested source shapes remain distinct from demonstrated unsafe acceptance.
The only structural audit test was the passing SpecReferences/CatalogComplete/
CatalogStyle selection. No new full gates were run during the audit.

The bounded front-end and safety inventories are complete. They do not inspect
every compiler/lower/backend/library path. Native owner-ring ownership behavior,
epoch-wrap campaigns, arbitrary unsafe owner uniqueness, all race soundness
paths, performance equivalence, full self-hosting and an exhaustive
differential campaign remain unverified by this survey. None is inferred
complete from roadmap labels or existing test names.

Settle the specification contradictions and D6 lifetime/order rules, and only
then resume implementation against those rules. A stable candidate must pass
exact native safety and shutdown oracles and all AGENTS verification gates.
Differential testing and reverse compiler-to-spec review remain subsequent
conformance checks, rather than substitutes for unresolved specification rules.
