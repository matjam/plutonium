# Package-variable memory reporting, native Linux amd64

Status: historical, abandoned implementation direction. D6 superseded the old
exit-time package-held classification premise on 2026-10-13. Its source, tests,
fixtures and optional independent candidates are archived at
`/home/matjam/.codex/recovery/plutonium-item96-2026-10-13/` and removed from the
active checkout; see [cleanup ledger](2026-10-13-item96-cleanup.md). The scoped
results and outstanding failures below describe the archived snapshot, not
current implementation status. Independent core Goexit cleanup remains landed.
Sections 15.4 and 18.6 still define the current behavior until D6's shutdown
specification is complete; D7's separate arena amendment is approved.

## Current verified scope

Fresh prerequisite verification passes sixteen hostile PageProvider cases at
O0/O2 with generation checks on and off. Bootstrap, Slab and SyncSlab clear
dirty and reused payload, and bootstrap honors alignment above PageSize.
PageProvider promises zeroed pages (`lib/pluto/alloc/alloc.p`); these dirty-page
fixtures violate that provider contract and measure optional defensive robustness,
not a conformance defect. Strong alignment is a separate recovery candidate.
The native command exits 0 in 4.612 seconds; its log is
`/tmp/item96-provider-native-refresh.log`.

Fresh allocator geometry verification exits 0 in 2.801 seconds. It covers
large backing ownership, exact nonzero request and cleanup counts, pointer
tags, invalid bounds and alignments, high canonical addresses, page trimming
and nested Arena backing after ancestor reset. Its log is
`/tmp/item96-geometry-native-refresh-second.log`. An earlier command failed
to compile during concurrent file additions and is preserved separately.

The frozen E3601 native matrix passes sixty cases in 27.086 seconds: twenty
diagnostic cases and forty accepted controls. Full CLI diagnostic goldens,
catalog checks and four-package vet pass. These are scoped checks, not final
acceptance. Subsequent source-effect repairs still require the final combined
verification.

Replacing the transient sorted record-pointer index with record indices
preserves the live filter, pointer ordering, containing-record lookup and
epoch/sequence checks. The actual `TestDebugReportAfterCompleteAllocation`
oracle now passes at O0 and O2. The two-oracle command
`go test ./internal/build -run
'^(TestDebugReportAfterCompleteAllocation|TestDebugRootSymbolizerAfterSnapshot)$'
-count=1 -v` still exits 1: the latest baseline passes in 7.10 seconds, while the
symbolizer case fails at O0 before its callback with critical-operation
admission (20.91 seconds, `/tmp/item96-output-native.log`). Generated
SSA shows balanced statement tokens but an intentional whole-function token
across Report. The selected output, identity and terminal-path contracts now
pass their focused controls. The remaining source witness loses the known
independent `tracker` value when entering zero-argument `fill`: the call frame
omits globals, so its fallback treats an assigned global as unknown. The
first-loss probe passes in 18.246 seconds
(`/tmp/item96-exact-tracker-origin-transfer.log`). At-call global transport
now passes twenty controls in 0.999 seconds
(`/tmp/item96-global-callpoint-fifth.log`). They cover direct and forwarded
calls, opaque calls, rebindings, conditional and loop state, old pointer
aliases, helper field writes through saved aliases, early returns and future
deferred/task global reads. The repaired origin probe passes in 18.597 seconds.
The exact checked main classifier and native callback oracle are pending.
Runtime snapshot admission has not been bypassed.

No full modified-suite, lint, coverage, SSA, generation-off or archive
acceptance is claimed for item 96.

## Source-proof boundaries under review

Physical destination storage is distinct from addresses contained in its
fields. An exact independent mapping can contain a borrowed root address;
writing through that address retains the root's provenance. Allocation
zeroing and extent proofs do not establish physical separation from a
receiver or package-owned allocation.

Call and write snapshots retain actual values, callable targets, numeric
facts, generic arguments and publication state at their execution point.
Later stores, callbacks and publications cannot rewrite an earlier snapshot.
Reachable writes and address exposure invalidate scalar facts; branch and
backedge joins remain conservative. Deferred and task effects do not become
immediate definite poststates.

Unknown calls and publication invalidate the affected origin, shape or
callable facts. Raw-root words keep their provenance through conversions,
fields, returned values and transport helpers. A bodyless or mixed target
cannot establish a complete read-only footprint.

Field shapes are bounded to two levels. Exhaustion widens to unknown and
preserves effects. Recursive proof frames compare actual values, callable
targets, scalars and types. The mutation-closure analysis currently has an
eight-function conservative boundary. It must retain guards for unproved
effects; it supplies no ownership or lifetime authority. Repeated closure,
alias and global-writer scans have produced governing-source runs above
thirty seconds. Final verification must measure this cost and establish
finite termination under the unchanged testguard limits.

Remaining recognizers prove source ranges, zeroing, overflow conditions,
normal-return paths, or exact selected runtime dependencies. Runtime receipts
compare the selected source declarations, build constraints, globals and
assembly against immutable audited inputs. A package, function or field name
alone grants no authority.

Sections 15.4 and 18.6 require correct classification, but do not require
automatic whole-body proof of every private reporting helper. That breadth is
an implementation choice. The current closure approach uses the existing
named compiler/runtime contract for a finite set of original internal
operations, checking the actual selected dependencies where the contract
requires them. Custom mark, enter, allocator, console and panic callbacks
retain their own effects. The original Linux SYSwrite read-buffer contract
passes six selected-number/source/assembly/evaluation controls; it grants no
allocation origin, extent, liveness or ownership facts.

Three demonstrated publication omissions now pass paired controls: expanded
tuple arguments, omitted default arguments and copied aggregate fields.
All twenty-five mapping-publication cases pass in 2.396 seconds; eight nearby
source families pass in 4.810 seconds, including opaque allocation results,
Free callbacks, interior addresses, loaded words, scheduler carriers and long
alias graphs. Logs are `/tmp/item96-publication-expanded-fixed.log` and
`/tmp/item96-publication-expanded-controls.log`. These repairs change exposure
inventory only.

Compiler transactions establish an exact interval in which a report cannot
acknowledge the caller. They do not establish address independence, ownership
or absence of effects. Any completion receipt must match the emitted call
site and its cleanup and exit paths; it cannot transfer to an unwrapped
runtime/debug caller or sibling call.

## Goexit cleanup follow-up

An activated same-task `CatchGoexit` probe first failed in 59.747 seconds:
compiler-generated store boundaries balanced, but generated destruction could
leave before `dropEnd`. The first repaired matrix balanced both depths across
sixteen O0/O2 cases and preserved deferred execution and skipped later source
work, but failed cleanup-count assertions. Its log is
`/tmp/item96-store-goexit-native-third.log` (exit 1, 162.187 seconds).

The replacement RHS exposed an unregistered owner temporary: the old value's
Drop ended the task before the evaluated replacement was transferred. Holding
the replacement until its actual store now passes at O0/O2, with Drop identities
1 then 100. Aggregate totals 121 versus 111 came from a fixture Drop that added
10 even for an empty aggregate, not evidence of a duplicate resource release.
That fixture now treats zero as empty and prints nonempty Drop identity/order.
The compiler also retains the exact fact that a whole variable has been cleared
after its completed explicit Drop on the implicit-exit branch. The numeric
failure log is `/tmp/item96-store-goexit-native-fourth.log` (exit 1).

Bare-literal staging probes initially used the wrong oracle: section 11.26
evaluates calls before those literal constructions. Corrected earlier producer
calls exposed three genuine missing held-result lifetimes (later RHS, call
argument and return operand). Their first failing log is
`/tmp/item96-store-goexit-staging-producer-first.log` (29.993 seconds, exit 1).
Actual produced owners now remain registered until their real store, deferred
record transfer or callee invocation, including C ABI argument handoff. Borrowed
uses keep the producer until statement cleanup; a blank owner result keeps one
cleanup entry.

The consolidated activated oracle passes 21 labelled scenarios at O0 and O2:
depth balance, exact nonempty Drop identity/order, skipped later operands/stores,
deferred execution, replacement temporaries, nested and dynamic interfaces,
normal tuple/aggregate/defer/blank handoff, callee ownership transfer, borrowed
use, and shared/synced/counted-error producer release. Its command is
`go test ./internal/build -run '^TestDebugStoreGoexitBalance$' -count=1 -v`;
it exits 0 in 24.591 seconds. The log and exit file are
`/tmp/item96-store-goexit-native-combined-first.log` and its `.exit` counterpart.
Runtime activation and O0 generated `DebugStoreBegin`/`DebugStoreEnd` listing
assertions prevent a vacuous result.

Existing hook regressions also pass: `go test ./internal/build -run
'^(TestSchedulerHookGoexit|TestAllocatorCreationHookGoexit|TestSourceLateHookGoexit|TestBoundLateHookGoexit)$'
-count=1 -v` exits 0 in 0.850 seconds. Its verbose log
`/tmp/item96-goexit-hooks-after-depth.log` confirms all four named tests ran.
The same four tests plus `TestDebugStoreGoexitBalance` pass with
`PLUTO_SSACHECK=1` (25.284 seconds, exit 0,
`/tmp/item96-goexit-ssacheck.log`). Full-suite acceptance and source call-span
acceptance remain pending; these native checks establish the tested cleanup
and lifetime repairs under sections 17.3 and 18.6.

That 21-case acceptance predates custom allocator `Free` callbacks that call
Goexit. The expanded native oracle exposed further required-cleanup defects:
weak/channel/string release could pass an inherited flag into the next cleanup,
string release could skip `dropEnd`, and timer release could skip channel
backing release. Allocated string/shared/error replacements did release both
old and new values once; the timer first failure printed 10 then 100 instead
of 10, 10, 100. These results are in
`/tmp/item96-goexit-token-release-ssa-second.log`; its O2 compilation was
interrupted diagnostically at 150.884 seconds (exit 1).

Saved private state tokens now close each compiler-generated store span without
a fresh task-state lookup. Required cleanup uses the existing exit accumulator
rather than recursively generating the same frame's return cleanup. A conditional
implicit cleanup propagates only its own accumulated exit, preserving an inherited
flag on an untaken branch. Strings defer `dropEnd`; channels defer their saved
backing and timer releases in timer-before-backing order. The complete expanded
release oracle now passes 29 labelled scenarios at O0 and O2 with
`PLUTO_SSACHECK=1` (42.170 seconds, exit 0,
`/tmp/item96-goexit-full29-ssa-first.log`). The command is
`PLUTO_SSACHECK=1 go test -o /tmp/item96-goexit-full29-profile.test
./internal/build -run '^TestDebugStoreGoexitBalance$' -count=1 -v
-cpuprofile=/tmp/item96-goexit-full29.cpu`. It checks exact Drop/Free order,
store/drop depth balance and skipped later work, including custom weak/channel/
string Free callbacks, timer then channel backing release, and allocated new
string/shared/error replacements. Timer cleanup prints 10, 10, 100; replacement
cleanup releases each old and new value once.

The reduced saved-token oracle and all four named hooks pass with
`PLUTO_SSACHECK=1` at O0/O2 (36.182 seconds, exit 0,
`/tmp/item96-goexit-token-release-ssa-fourth.log`). It verifies fresh lookup exits
before user mutation, fresh lookup exits during normal and deferred cleanup with
exact Drop totals 1, 3 and 5, and an inherited exit across private End. O0 listings
assert the emitted Begin/check/End order and no End task-state lookup. This scoped
acceptance does not establish the complete release matrix or item 96.

Measured compilation costs remain open. The interrupted O2 stack was in amd64
register coalescing. A separate completed compiler-rejection profile sampled
180.93 CPU seconds: repeated `debugGlobalAliasFlow` global seeding consumed
106.49 seconds, including 100.29 seconds at its pure retention-type classifier.
The smaller passing saved-token profile sampled 53.81 CPU seconds, including
16.48 seconds in that alias flow and 16.99 seconds in inlining. Immutable type
classification now caches completed immutable results; actual origin/publication results
must retain their complete context. Neither timeouts nor testguard caps changed.

The passing 29-case profile samples 50.26 CPU seconds. Completed top-level
retention classification now caches immutable instantiated type identity, with
generic, cyclic, alias and repeated-hit checks; the previously measured seeding
line accounts for 0.68 seconds in this profile. This comparison spans runtime
cleanup repairs as well as caching and is not a general speed ratio. Remaining
alias/result work accounts for 16.04 seconds. Optimizer phi-use rejection now
stops at the first unrepairable use, preserving its pure all-use predicate;
the complete opt/ssa suites pass with SSA checks (0.029/0.001 seconds,
`/tmp/item96-thread-inline-ssa-tests.log`). Actual Report and full-suite acceptance
remain pending.

Named results filled before Goexit initially leaked at O0 and O2 in eight
owner, shared, counted-error and string cases (1.413 seconds, exit 1,
`/tmp/item96-goexit-named-results-first.log`). The corrected return boundary
destroys ordinary result slots that were not handed to the caller, after
deferred calls finish. Counted result cells in a caller-owned closure region
keep their region's reference and gain no caller reference on Goexit.
The 19-case owner/counting/normal-transfer/region oracle passes at O0 and O2
(1.589 seconds, `/tmp/item96-goexit-named-results-second.log`). Three further
cases exercise Goexit raised during owner, counted-error and allocator result
destruction; remaining result slots still release exactly once.

The isolated core extraction passes its normal-allocator 29-case cleanup
oracle, the expanded 22-case named-result oracle and all four hook tests with
SSA checks (3.122 seconds, `/tmp/pluto-core-goexit-focused.log`). All required
full gates subsequently passed, and commit `deeadf80738a` landed independently.
The installed compiler contains that verified core scope. See the
[core verification record](2026-10-13-goexit-cleanup.md). This landing excludes
item 96 descriptors, transactions, source proofs and diagnostics.

Required generated cleanup callbacks share an exit accumulator. Each callback's
exit is collected and cleared before the next callback; `finishReturn` merges
pending exits after frame cleanup and before ABI transfer. The string Free
callback cases check mandatory `dropEnd` completion. The aggregate `After.Drop`
case checks that a later normal helper finishes after an earlier callback exits.
Named-result destruction cases check a second exit through remaining slots.
The private token inherited-End pair belongs to the reporting implementation.
A manually inherited pending flag entering an ordinary result-producing callee
is a distinct scope without a dedicated named-result oracle.

## Remaining conformance gap

E3601 prevents an unsupported owner from being silently reported as a leak.
It is a compiler precision diagnostic for valid ownership patterns whose
backing or dynamic payload cannot yet be established. It does not implement
every custom owner allowed by the specification. Such patterns remain an
explicit gap in roadmap item 96 and section 6.5 of the complete-spec plan.
No conservative diagnostic or passing verification gate closes that gap.

## Later specification consistency review

No chapter text has been edited. Section 15.4's Log example declares an
unfilled local owner but says its Drop prints once. Section 18.6 says that
never-filled and known-zero places are not dropped, and Drop must treat zero
as empty. A proposed correction is to state that the unfilled local is not
dropped, or to initialize a live nonzero field and make Drop explicitly
ignore zero before demonstrating one local drop. This requires the user's
specification decision and does not block the reporting implementation.

The later review also retains the explanatory debug-allocator wording about
package-held memory that a program cannot drop versus section 18.6's explicit
whole-variable Drop, and the fresh-provider zeroing assumption versus section
18.9's PageProvider contract. These are recorded consistency questions, not
changes to the normative ownership or allocator behavior.

## Coverage baseline

The accepted baseline is the passing covered suite for landed core commit
`deeadf80738a`: 48458/55010 statements, 88.08943828%, with all package floors
passing and default testguard limits unchanged. Duplicate profile blocks are
merged by logical OR, as `tools/pkgcov` does. Its profile is
`/tmp/pluto-core-goexit-cover-regalloc.out`; the exact all-package command and
comparison are in the core verification record. The earlier tracked-only
`83a014f2` profile remains preserved as historical evidence. Item 96 has not
yet run its acceptance coverage command.
