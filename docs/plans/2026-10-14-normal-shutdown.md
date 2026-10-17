# Normal program shutdown

Status: normative amendment reviewed and implementation native verified, 2026-10-14. D6 approves
normal-exit resource cleanup and waiting for all tasks to finish naturally.

**Superseded in part, 2026-10-17 (decision D10).** The wait for tasks is removed: when `main` returns
the program ends as in Go, and package cleanup and the leak report run only when no other application
task is running (sections 12.12, 14.5, 15.4, 19.1 and 19.7; abi.md 12.10; memory-model.md 3.9;
debug-allocator.md, Leaks). The "Approved task policy" below and every step of the finite sequence that
drains running tasks no longer apply; the plan of
[2026-10-17-no-false-friends-exceptions.md](2026-10-17-no-false-friends-exceptions.md) part 2 replaces
them. Still in force: package cleanup in reverse initialization order, in-place drops, protection of
completed static places, mandatory cleanup across `ExitTask`, the retirement of ended tasks, and the
distinction from `os.Exit`.

Sections 15.4, 18.6 and 19.1, ABI section 12.10 and the debug allocator exit rules
are amended together. Native source gates and the tracked-source archive pass.
See the [verification record](../verification/2026-10-14-normal-shutdown.md).
This plan records technical choices for that approved direction and is not
itself normative. Those sections supersede this plan wherever they differ.
Baseline: `408dd2ab`.

## Approved task policy

Normal return from `main` completes its ordinary local cleanup, then waits for
every other task to finish naturally, including descendants created while
draining. There is no forced cancellation. An infinite task keeps the process
alive; a blocked task remains pending while progress is possible. A global
deadlock remains a fatal error under section 19.1. Task completion includes frame cleanup and
backend retirement of its stack and task record, not just a Scope count
reaching zero. No package resource destruction begins before this boundary.

This applies to ordinary hosted termination. Explicit `os.Exit`, an unhandled
panic and the main task's `ExitTask` are separate termination paths whose existing
behavior is not implicitly converted into normal return. A kernel that never
terminates acquires no shutdown requirement.

## Recommended finite shutdown sequence

1. Finish `main`'s locals and defers and retain its exit status.
2. Drain tasks and in-flight application callbacks through actual retirement.
   The zero-task observation and closing
   task creation form one scheduler transition, so no task can appear between
   that observation and package cleanup.
3. Destroy package storage in reverse actual initialization order, with the
   lifetime and static-object rules below. The shutdown task and runtime
   services required by destruction remain usable.
4. Retire remaining runtime infrastructure in its explicit internal order,
   keeping allocator dependencies alive until their clients are released.
   Reclaimable task-stack caches are already flushed at the drain boundary,
   before package allocators they may borrow are destroyed; this step also
   flushes any cache entries required synchronous cleanup created afterward.
5. Run final debug accounting after application ownership and reclaimable
   runtime storage have been released, then perform the final system exit.

No ownership graph is inferred, no destructor ordering is chosen from method
bodies, and no repeated scan tries to reach a resource-free state.

## Package order and dependency lifetime

Record the actual initialization sequence defined by sections 15.2 and 15.3.
Each named variable has one destruction position, including a zero-initialized
variable. Its current value at shutdown is destroyed there. For simultaneous
stores in one initializer, use reverse store order. Imported packages therefore
outlive their importing clients. Initialization functions are not rerun or
inverted; resources they assign are owned by their destination variables.

Preserve the full ready-variable sequence in section 15.2, including its
zero-initialized variables. Existing `internal/types2/initorder.go` computes
that queue but omits no-expression variables from `Info.InitOrder`; retain a
separate complete lifetime sequence rather than reconstructing their positions
from only emitted initialization stores. Reassignment does not move a slot's
cleanup position.

Destruction follows the ordinary lifetime requirement used by local owners:
an allocator or borrowed resource must outlive every client that uses it.
Programmers establish that order through initialization, or explicitly remove
and destroy clients before returning from `main`.

```go
var heap = arena.NewFixed(4096)
var values = vec.New[int](&heap)
// Shutdown destroys values, then heap.
```

Late setup does not change a slot's destruction position:

```go
var values vec.Vec[int]
var heap arena.Fixed
func main() {
    heap = arena.NewFixed(4096)
    values = vec.New[int](&heap)
    // This order does not give heap a longer lifetime than values.
    // Destroy/take values here, or change the declaration/initialization order.
}
```

A lifetime violation must fail before unsafe memory access in a checked build;
shutdown does not silently reorder it or waive generation checks. This is a
program error, not a promise to find an optimal cleanup order. Unchecked builds
retain their documented lifetime obligations.

## Finalized static contents and revival

Static storage keeps its program lifetime under section 18.3. Cleanup runs in
place, preserving the receiver's address and lifetime, then zeroes the completed
place. A compiler stack temporary would introduce a new escaping-frame risk if
a user Drop publishes its receiver; it is not used. There is no pinned or
nonmovable ordinary type category to exempt: section 18.5 defines byte-copy and
zero moves, section 13.10 permits take for every type, and unsafe `rt.Pinned`
hides pointer provenance rather than forbidding moves. A live Scope's address
restriction ends when its joined tasks finish.

Each completed resource-bearing place is protected from subsequent typed
writes, including scalar fields, zero assignment, take and clear, through the
actual destination address and extent. Aliases through helpers, interfaces and
closures cannot bypass the check. Reads remain valid; an unrelated static POD
object remains writable. A still-pending slot can change and its eventual
cleanup consumes its current value. User Drop runs before its fields; as each
field/element completes it becomes protected, so a later sibling's callback
cannot revive an already-cleaned field. No slot reopens or repeats its cleanup.
Unsafe raw memory operations retain their obligations.

A fixed compiler-emitted table of static bounds and cleanup progress can enforce
the rule without application heap allocation or source-origin inference.
Completed-prefix progress may implement ascending field/element cleanup where
layout permits; wrappers/unions must preserve the semantic subobject rule.
The check must precede old-value release or ownership handoff. `storeLV` in
`internal/lower/value.go` exposes typed destinations and `dropAt` in `drop.go`
supplies typed cleanup, but neither alone covers every mutation/transfer path.
The implementation must audit their common boundaries. No static read needs a
new lifetime tag, and existing released-backing generation checks remain active.

## Tasks and callbacks during package destruction

Recommendation: after the drain transition, starting a language task is a
shutdown error. Enforce this at the runtime handoff before a task can own its
arguments; existing provisional argument cleanup remains responsible if the
start fails. The task expression's ordinary operand evaluation still occurs.
The prohibition also covers starts reached through helpers or callbacks.

Timers, poller services, scheduler hooks and allocator callbacks needed by
synchronous destruction remain available. Their internal retirement contract
must prevent previously queued application work from executing after the drain
boundary. They are not exempt from storage lifetime checks. A destructor may
block, so normal shutdown is not guaranteed to terminate. There is no new
directive or arbitrary runtime-namespace exemption.

## Hidden static ownership and counted resources

Every compiler-created static object that owns fields needs exactly one
cleanup obligation, even when reached only through a borrowed pointer or
ordinary interface. Examples include static composite-literal backing and
static closure storage permitted by initialization rules. Register such an
object before the initializer slots that borrow it; reverse cleanup then
destroys those slots before the backing object. Reuse of one static object
does not duplicate its owning obligation.

Counted values follow reference semantics: each stored owning reference is
released once, including references in arrays, aggregates and hidden objects.
An owner moved into a named slot is cleaned there, not again at its source.
Borrowed interface copies do not become owners. Strong reference cycles and
explicitly unreleased raw storage remain reportable leaks; this is not tracing
garbage collection. Static literal bytes that own no resources need no Drop.

## Exits during cleanup and accounting

An `ExitTask` raised by required package cleanup is accumulated
while the remaining mandatory fields, backing releases and package obligations
complete, using the existing cleanup-exit protocol. No later ordinary user
statement in the exiting destructor resumes. It does not start a second
shutdown pass. Preserve `main`'s saved status for the final normal system exit.
Panic and explicit `Exit` remain abrupt and may prevent subsequent cleanup.

Debug reporting follows completed application cleanup. It reports genuinely
outstanding allocations, counted cycles and abandoned raw storage. Private
permanent generation-directory metadata must have an explicit runtime lifetime
and accounting boundary; it must not be mislabeled as an application leak or
hidden by treating arbitrary allocator allocations as permanent. Allocator and
reporting infrastructure must remain alive through this final accounting.

## Implementation boundary and acceptance

Before this change at `408dd2ab`, entry lowering in `internal/lower/global.go`
(`lowerEntry`) emitted initializers and main without teardown. Typed cleanup is in `drop.go`.
Hosted entry is `lib/pluto/rt/rt_linux.p` (`mainOnStack`); `sched.p` and `join.p`
previously waited only for begun drops. Task retirement passes through
`afterSwitch` and the selected `FreeTask` backend. `alloc/debug/debug_linux.p`
previously settled and reported rather than joining tasks. The new compiler entry
and runtime share the phase boundary; a custom backend must implement the same named
runtime contract rather than being assumed safe from hosted behavior.

The reviewed order, finalized-storage rule, task-start prohibition, hidden-object
ownership and cleanup-exit behavior are now stated by the cited normative
sections. Implementation follows those sections. Minimum native O0/O2 acceptance is:

| Requirement | Exact contrast |
|---|---|
| Task draining | descendant finishes and stack allocator is released before globals; forever task keeps program alive |
| Ordering | main defers before packages; dependency allocator initialized first is destroyed last |
| Lifetime failure | late dependency cannot access freed backing; finalized static storage itself remains readable |
| Revival | writes to pending slots are cleaned later; alias/zero/scalar writes to finalized resource-bearing places fail |
| Hidden objects | static composite/interface/closure owning fields release once; counted copies balance |
| Task boundary | helper/callback start during cleanup is rejected without losing argument ownership |
| Cleanup exit | ExitTask completes remaining mandatory releases and package obligations once |
| Abrupt exit | explicit Exit/panic retain the chosen abrupt contract |
| Accounting | normal owned resources reach zero; raw leaks/cycles remain visible; runtime metadata is accounted separately |

Existing initialization, task, Scope, ExitTask, allocator and exit-status tests
remain controls. Full tests, lint, aggregate coverage and floors, SSA checks,
generation-off tests and tracked-only archive verification remain landing gates.
