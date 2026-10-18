# Implementation decisions

Decisions D1 to D5 record choices the implementation made under the authority granted on 2026-10-11:
the pending items in HANDOVER.md are resolved against the project goals (AGENTS.md),
recorded here, and reviewed by the user afterwards. Each decision names the sections of
the specification it changes and the work that lands them.

D6 and D7 record directions approved directly by the user beginning on 2026-10-13.
D8 records the coordinated specification resolutions approved on 2026-10-15. These
are not part of the earlier delegated implementation choices. D9 to D11 record the
owner's decisions of 2026-10-17 that remove the three exceptions to the no-false-friends
rule. Historical rationale below is not authority to change the language to match code.

The current goals, stated precisely in section 1.1 and D8: Go-familiar, not
Go-compatible, with differences visible or expressly documented; the same ordinary
memory/race-safety contract for applications and kernels, without programmer-written
borrow annotations or compiler knowledge; no GC or silent heap promotion, with
specified allocating-operation contracts and explicit lifetime responsibility; kernel
suitability; and performance near Rust, assessed separately from semantic conformance.
Since D9 to D11 the no-false-friends rule has no exceptions.
D12 to D30 record the owner's design decisions DD1 to DD19 of 2026-10-17 (the plan
`plans/2026-10-17-review-design-decisions.md`), in that order.

## D1. `errors.As` matches a counted error against a `*T` target (16.6, 16.7)

Section 16.7 says `fmt.Errorf`, `errors.Join` and the standard library error types
(`*os.PathError`, `*strconv.NumError`) are allocated as `synced[T]` objects that are
returned in an `error` and released exactly once by the count ("no code releases it by
hand", "there is no `errors.Free`"). Section 7.19 says a box is not a `*T`. Section 16.6's
matching rule ("the first error in the tree whose dynamic type is assignable to `E`")
would then make the 16.5 example, with `var perr *os.PathError` and
`errors.As(err, &perr)`, report `false`, so the chapter contradicts itself.

Decision: `errors.As` matches a counted reference in the tree against a pointer target.
When the dynamic type of an error in the tree is a counted reference `synced[T]`, `As`
reports a match for `E = *T` and stores a copy of the pointer in `*target`. Section 16.6
gets one sentence saying this, cross-referenced from 16.7. The 16.5 example works as
written. `errors.Is` needs no rule: it matches sentinels by identity and `Unwrap`,
neither of which depends on the box.

Why this and not the alternative (standard library errors as plain `*T` with a manual
`Free`, which is what the implementation does today): the counted object is the design of
16.7, and a manual free is what that section explicitly forbids, because a kernel error
path that forgets one leaks and one that frees twice corrupts. A Go programmer writing
`errors.As(err, &perr)` against a `*os.PathError` target is the familiar shape this
language promises.

Lands with: counted errors (roadmap items 11, 69; gap 22).

## D2. No task may be started during package initialization (15.2, 15.3, 19.1, 19.3; resolves G44)

Decision: a statement that starts a task (a `task` statement, `Scope.Task`,
`WaitGroup.Task`, or a library function that takes a func argument and starts a task),
reached from a package-level variable initializer or an `init` function directly or
through the functions it calls, over the whole program, is a compile error that names
the start and the initializer. The `Open (G44)` block in the specification is replaced by
this rule.

Why: initialization runs single-threaded, before any task exists. The rule that a package
variable written only during initialization needs no lock (19.3) is sound only if no task
can observe it while initialization is writing it. The alternative (allow tasks in
initialization and treat such variables as shared in the race analysis) weakens the
guarantee for every program, and a freestanding or kernel initializer has no scheduler to
run a task on anyway.

Lands with: roadmap item 100 (gap 32).

## D3. Shared metadata checks large contiguous blocks; fixed arenas stay unchecked (18.9, 18.11, abi.md 1.3)

Status: the shared-directory decision remains in force. D7 supersedes the
fixed-arena exemption below and the hosted arena-epoch wrap exception. The
following rationale records the earlier choice, not the current safe-API rule.

Decision: slab allocations over 2 KiB and `arena.Chunked` chunks over 56 KiB are
allocated with their own 64 KiB-aligned metadata base and granule table before contiguous
payload. On 2026-10-12 the user explicitly chose a shared metadata directory for every
checked pointer lookup. The directory reaches the correct registered table for any
interior address without placing tables inside large payloads. The existing 16-bit tag
values and address mask are preserved. The buffer of a fixed
`arena.Arena` is exempt from the generation check, and the specification says so
(sentences in 18.11 and abi.md 1.3). Metadata readers must remain safe during concurrent
registration and reclamation; address generation history survives allocator and mapping
reuse, retires before heap generations wrap, and preserves nested parent/child validity.
Ordinary allocations through custom allocators meet the same checked contract (18.9).

Why: 18.11 requires no untagged heap or arena memory in a checked build. A fixed arena is
one user-owned buffer, and per-allocation tags for its allocations would mean the arena
suballocates through the allocator (defeating the point) or pays a tag lookup and a tag
write on every allocation. As written, abi.md 1.3 cannot be implemented for a fixed
buffer, so the honest move is a documented exception. Kernels use fixed arenas for
exactly this reason, and the exception names the cost instead of hiding it. The shared
directory avoids a second hole: an object spanning 64 KiB boundaries cannot contain
tag tables at each boundary without corrupting its contiguous payload. The user accepts
additional lookup cost, to be measured. The bit-47 flag with direct small-object checks
and permanently segregated address reservations was considered and not chosen.

Lands with: roadmap item 85 (gap 10).

## D4. Two implementation readings confirmed (17.5, 8.8)

Status: historical delegated readings. D8 strengthens (b) to cover borrows obtained
after construction and every later transfer; construction non-aliasing alone is not
a complete race-safety proof. The historical code-based rationale below is not the
authority for the current normative rule.

(a) Slicing through a nil array pointer is always checked. `p[lo:hi]` computes an
address, and 17.5 requires every nil dereference to panic; the implementation checks even
where the bounds would not use the value. A sentence in 17.5 states that.

(b) An owner's sendability is decided by what it holds (8.8) under the assumption that
the filling function does not alias the allocation: no pointer to the owner's memory
outlives the fill except through the owner itself. The race analysis already assumes
this; a sentence in 8.8 states it.

Why: the code is already correct as implemented. Where the specification is silent, it
should say what the code does, so a later reader does not have to re-derive it. No code
change is expected.

Lands with: the spec fix pass.

## D5. `defer` accepts every built-in that may stand as an expression statement (12.19)

Decision: `defer` accepts `clear`, `close`, `copy`, `delete`, `panic`, `print` and
`println`, and reports E2532 for the rest. Section 12.19 already says that calls of
built-in functions are "restricted as for expression statements", and 12.4 defines
exactly that set. The implementation was narrower (only `print`, `println`, `close` and
`panic`); the implementation is fixed to match the specification. No specification change.

Why: the specification's sentence already states the rule, and 12.4's set is the line a
Go programmer expects. Forcing a function literal for `clear`, `copy` or `delete` would
be a difference from Go that the specification does not state.

Lands with: roadmap item 82 (gap 6).

## D6. Normal program exit releases owned resources (15.4, 18.6, 19.1)

Decision, approved by the user on 2026-10-13: normal hosted program termination
will clean up the owned resources the program still holds, including package
owners. This supersedes the former rule leaving package owners undropped at
normal exit.
This direction replaces the premise of item 96's exit-time separation of
package-retained memory from leaks. Existing uncommitted reporting work is
archived under the cleanup ledger; it is not authority for the new exit behavior.

Follow-up decision, approved by the user on 2026-10-14: after `main` returns
normally, normal termination waits for all remaining tasks to finish naturally,
including tasks they start. It does not cancel them. A task that runs forever
keeps the program alive; blocked tasks remain pending while progress is possible,
and global deadlock remains fatal. Completion includes its required
cleanup and runtime retirement; package resource destruction must not race a
task's final stack release. This implicit wait is a deliberate language rule.

**Superseded by D10 (2026-10-17):** the wait for other tasks is gone. When `main`
returns, the program ends as in Go; package cleanup and the exact leak report run only
when no other task is running. The first paragraph (package owners are cleaned up at
normal exit), the reverse-initialization order, the in-place drops and the protection of
completed places stay in force, as does the distinction from `os.Exit`.

The [shutdown design](plans/2026-10-14-normal-shutdown.md) records the technical
elaboration of that direction: deterministic reverse initialization cleanup,
in-place address-preserving drops, protection of completed static places from
typed writes, task/callback admission closing at the drain boundary, and
mandatory cleanup completing across `ExitTask`. These are engineering choices
for the approved resource-cleanup policy, not additional statements attributed
to the user. The normative amendment has passed consistency review, and its
implementation passes the required native gates and tracked-source archive. The
[verification record](verification/2026-10-14-normal-shutdown.md) records the exact
coverage partition and shipping evidence. Resource dependencies must outlive
their clients, as for local ownership; shutdown does not infer an arbitrary
destructor dependency graph.

Abrupt termination, including `os.Exit`, is distinct from normal cleanup. Its
relationship to exit hooks and debug reporting is stated in the amended specification.
A kernel that never terminates does not acquire a shutdown requirement merely
by using the language.

Why: explicit ownership should account for resources still held when normal
execution finishes, rather than treating those resources as permanently
retained. The implementation must preserve the language's task and lifetime
guarantees while releasing them.

Lands with: a reviewed normal-termination specification and its implementation
and native cleanup tests. These pass native verification, including the
tracked-source archive.

## D7. Ordinary arena APIs enforce view lifetimes (18.9, 18.11, 18.12, 21.5; ABI 1.3)

Decision, approved by the user on 2026-10-13: ordinary fixed and Chunked arena
APIs must prevent stale accesses through compiler lifetime checks or runtime
checks. Safe application code must not acquire an unchecked lifetime obligation
merely by calling an arena's typed allocation or ordinary reset/release methods.
This supersedes D3's fixed-arena exemption. Hosted epochs must not wrap so that
a stale view can become valid again; exhaustion retires affected storage,
widens the identity or fails loudly without making a stale view match.

Manual lifetime operations remain explicitly unsafe through the existing
unsafe allocation, pointer and unchecked-function mechanisms. This decision
adds no directive. The subsequently approved API split below makes the manual
constructor explicit. Disabling generation checks
is still the documented unchecked build mode; it does not disable static checks
or make incorrect programs correct.

The existing caller-provided buffer form must not silently replace that
buffer with an allocated one or insert metadata into caller payload. A
distinct ordinary fixed-capacity constructor may explicitly allocate private
backing. The user subsequently approved the concrete split: `NewFixed(capacity uintptr,
parent alloc.Allocator = alloc.Default()) Fixed`, a movable owner with private fixed-capacity
backing, and `NewUnsafe(buf []byte) Arena`, the explicit manual borrowed-buffer route.
Constructor and manual-method unsafe classification uses actual API identity, not a naming prefix.
Exact metadata storage and exhaustion handling
must be reviewed before implementation, with their costs accounted for under
the existing allocation rules.

Why: the audit reproduced a plain typed pointer retained in a heap Holder,
used after fixed Arena Reset, reading the next allocation's value in a checked
application without an unsafe import. A documented unchecked exception in an
ordinary API conflicts with the intended safe-language guarantee.

Lands with: the focused specification amendment and native acceptance matrix
in `docs/plans/2026-10-13-safe-arena-lifetimes.md`, then the implementation and
full verification. The arena implementation subsequently completed the scoped native
verification recorded in `docs/verification/2026-10-13-safe-arena-lifetimes.md`.
This is historical implementation evidence for that snapshot, not certification of
D8's stronger all-mode safety contract. Conservative rejection alone does not
complete supported valid arena usage or its positive acceptance cases.

## D8. Coordinated specification consistency resolutions (2026-10-15)

Decision: the user approved items 1–9 of the
[resolution checklist](plans/2026-10-15-spec-resolution-checklist.md), after
reviewing the examples and tradeoffs individually. The corrected normative
sections, not this record or the current implementation, define the language.
The [audit](plans/2026-10-14-spec-consistency-audit.md) retains the reviewed
pre-correction findings C1–C20. This decision preserves D6 and D7.

- **Safety in every ordinary build** (sections 1.2–1.4, 18.3, 18.11–18.12,
  20.3, 20.5–20.6 and 21.5; ABI 1.3): applications and kernels have the same
  memory/race-safety contract. Prove safety, supply suitable automatic protection,
  or reject an unproved operation if that environment cannot protect it. This
  explicitly supersedes the former unchecked kernel/freestanding default. An
  unchecked build is an explicit opt-out; static rules remain. Ordinary code
  needs no annotations. Early-boot/interrupt protection respects its allocation,
  waiting and failure constraints. No new directive or pointer encoding is chosen.
- **Ownership and borrowing** (sections 7.9, 8.7–8.8, 10.14, 11.9, 11.11,
  13.6 and 18.1/18.6/19.3; ABI 4.1): composite views retain/release counted
  components. Owner transfer must account for outstanding aliases through fields
  and helpers, not merely a non-aliasing constructor. A borrow ending before
  transfer is allowed. Indexing and Len overloads on owners require nonconsuming
  pointer receivers; no syntax-dependent invocation of a consuming method.
- **Shutdown** (sections 12.12, 14.5, 15.4, 19.1/19.7; memory-model 3.9;
  ABI 12.10; the draining part is superseded by D10): preserve natural draining and reverse package cleanup. Task/callback
  operations, mandatory cleanup and retirement happen before package cleanup,
  with cross-processor visibility. No automatic cancellation or runtime cycle
  collection is added. Abrupt exit and nonreturning kernels stay distinct.
- **Kernel synchronization** (section 20.10; memory-model 6.1): asynchronous
  interrupt sharing needs volatile/atomic access or correct interrupt exclusion;
  no universal plain-access synchronization is implied. An interrupt-safe spin
  protocol disables relevant interrupts before acquisition, excludes inappropriate
  preemption, releases, then restores prior state. It cannot depend on interrupted
  execution resuming; nested/nonmaskable exceptions need separate handling.
  HandlerAddr is an address query ordering no memory, with invalid indirect uses
  rejected at compile time (section 20.8; ABI 10.5).
- **Construct conflicts** (sections 7.6/11.3, 7.12–7.13/10.8, 7.9/16.2,
  16.5–16.6): preserve promoted literal keys and their existing restrictions;
  allow aliases of existing named enums/unions, reserving declaration syntax for
  definitions; allow inline pointer error structs with valid referenced storage;
  ErrorBuf wraps only its first percent-w cause, while fmt.Errorf can wrap several.
- **Grammar and ABI** (sections 10.15/11.6/11.16 and appendix E; ABI 4.1,
  12.6–12.7): explicit generic methods work after general receiver expressions;
  regions clean up every counted component. Channel counts follow section 18.7's
  single hosted plain-before-threading/atomic-after rule and are atomic from
  freestanding startup. Unsized hosted stacks normally reserve at least 64 MiB
  virtually, with constrained-address-space fallback never below 64 KiB and
  overflow protection preserved.
- **Accurate promises** (sections 1.1–1.4, 18.7/18.10, 21.3; appendix A;
  debug-allocator): allocation visibility means specified allocating operations
  and explicit lifetime responsibility, with no silent heap promotion, rather
  than per-call annotations. Last strong release destroys; weak references can
  delay reclamation; programs avoid/break strong cycles. Debug protection,
  poison/canary inspection and finite quarantine have stated detection windows,
  not universal stale-access detection. Scheduler inclusion follows required
  capabilities, including library dependencies, not task syntax alone.
- **Equivalence and examples** (section 21.3, appendix A, sections 7.4,
  11.26 and 12.11): optimization preserves permitted observable behavior,
  ordering, safety and logical source panic frames. Permitted nondeterminism and
  resource-use variation remain. Static errors.New identity and D6 waiting were
  disclosed observable differences; D10 and D11 remove both. Correct local-array, iterator-continue,
  evaluation-dependency and type-kind summaries without changing their mechanisms.
- **Evidence and conformance:** use a small fixed application/kernel witness set
  and a separate repeated-run performance methodology. Source acceptance and
  safety remain mandatory; no numeric benchmark target or exemption is selected.
  Implementation is audited against the corrected specification after review.

Status: this records approved language decisions and the coordinated specification
amendment. It does not claim compiler/library conformance to every corrected rule;
the roadmap and witness register track that separate work. Existing D6/D7 native
receipts remain evidence for their tested snapshots, not proof of the new findings.

## D9. A string is cut with a library call, not with `s[i:j]` (1.2, 7.3, 11.12, 17.1, 18.8, A.2, F.6)

Recorded in the owner's brief of 2026-10-17 as "D8"; numbered D9 here because D8 above is
taken. Reasoning: [the plan](plans/2026-10-17-no-false-friends-exceptions.md), part 1.

Question: every Plutonium string holds valid UTF-8, so `s[i:j]` cannot cut at any byte offset
as Go does. The specification made a cut inside a rune panic, which made
`title[:40]` a false friend: valid Go that compiles, works on ASCII and panics on the first
accented letter. Can the exception go?

Decision (final): a slice expression on a string of any kind, constant or not, is a compile
error (E2019, `StringSliceNotPermitted`) that names `strings.Slice(s, i, j)` and
`strings.SliceFrom(s, i)`. Both are generic over the string type, share the buffer, panic
unless each bound is a rune boundary, and the compiler lowers them inline. Arrays and slices
keep `a[i:j]`; `s[i]`, `len(s)`, `range s` and comparison are unchanged. `strings.Truncate`
and `utf8.RuneStartBefore` round to a boundary by name. A string stays valid UTF-8.

Why: Go-familiar, no false friends (the check moves behind a name Go does not have, and the
compile error names the fix); strings stay valid UTF-8 as the owner wants; the same code as
before, so no cost to performance; no annotations. The cost is one mechanical edit at each
string slice, which the error message and `go2pu` provide. Rounding silently, rune-counting
slices, an offset type and strings of arbitrary bytes were rejected in the plan.

Lands with: the specification amendment of 2026-10-17 (sections 1.2, 7.3, 11.12, 17.1, 18.8,
20.3, A.1 to A.3, A.5, F.6) and roadmap items 104 and 74.

## D10. Normal termination follows Go; cleanup runs when no task is left (12.12, 14.5, 15.4, 19.1, 19.7)

Recorded in the owner's brief as "D9"; numbered D10 here. Reasoning: the plan, part 2.

Question: D6 made `main` wait for every task, so `task heartbeat(); fmt.Println("done")`
printed and never exited, which differs from Go with nothing in the source to show it.

Decision (final): when `main` returns, its own cleanup completes (which joins its scopes).
One scheduler transition then closes task and callback admission and checks whether any
other application task is running or any callback is in flight. If none is, pending timer
callbacks are canceled, ended tasks finish retiring, package cleanup runs in reverse
initialization order, the debug allocator accounts, and the program exits with `main`'s
status. Otherwise the program ends at once with `main`'s status, as `os.Exit` ends it:
tasks stop where they are, no owner is dropped, no package cleanup runs, and the debug
allocator prints the number of running tasks and skips the leak report. `os.Exit` and
`rt.ExitTask` in `main` are unchanged. Supersedes the wait-for-tasks rule of D6 and the
"Approved task policy" of the shutdown plan; D6's package cleanup, ordering and in-place
drops stay.

Why: Go-familiar and no false friends (a Go program that returns from `main` with workers
running exits); the memory model still holds (package cleanup runs only when no task can
touch a package variable); kernel `main` does not return, and freestanding backends need
only the count-and-close transition. A program that wants cleanup joins its tasks, which is
visible in its source.

Lands with: the specification amendment of 2026-10-17 and roadmap item 105.

## D11. Each `errors.New` evaluation is a new error, without allocating (7.9, 15.2, 16.2, 16.3, 17.1, F.4)

Recorded in the owner's brief as "D10"; numbered D11 here. Reasoning: the plan, part 3.

Question: Go returns a new pointer from every `errors.New`. Plutonium returned one static
error per call site, so two results of one site compared equal (`e1 == e2` was true).

Decision (final): each call site has its own compiler-made error type and a 64-bit counter
in static storage. Each evaluation adds one atomically and returns an error of the site's
type whose data word holds the new count. Two results of one site are unequal; a copy is
equal; different sites differ by type. The text must still be a constant (E2008) and the
call direct (E2542). A package-level sentinel is evaluated once. Exhausting a counter
panics and never wraps. `errors.New` allocates nothing and needs no allocator. Supersedes
decision F19 of the false-friends plan.

Why: Go-familiar and no false friends (equality, `errors.Is` and map keys match Go); no
hidden allocation and works before an allocator exists (kernel boot, interrupt context).

Lands with: the specification amendment of 2026-10-17 and roadmap item 106.

## D12. `owner` stays on struct and generic container declarations; the "no annotations" claims are made exact (1.1, 1.2, 7.18, 18)

Question: Does a struct with an owner field, or a generic type that stores an `owner` type parameter, have to be declared `owner` (E3202), or should the compiler infer it?

Decision: Keep the keyword. The claims now read: no lifetime, borrow or region annotations, and no `//pu:` directive for a fact the compiler can infer. The kind of a type is part of its declaration.

Why: A type whose assignment moves instead of copies is the largest behavioral difference from Go, and the keyword shows it where the type is declared. With inference, changing a field's type in another package would turn copies into moves in every user, reported as E3301 far from the cause; with the keyword the error is E3202 at the one declaration. Code that uses an owner type writes nothing extra.

Lands with: roadmap item 121.

## D13. A func value that may be a closure of a frame is a compile error at a task start (11.5, 12.12, 18.3, 19.4, 17.1)

Question: What happens when a func value from a parameter, field or variable that may be a closure of a frame is given to `task`, `wg.Task`, `rt.Go` or `time.AfterFunc`?

Decision: A compile error, through the existing frame-view analysis, and no run-time check. E3004 where the value reaches the task start; E3003 at a call that passes such a closure to a function that starts a task with it. E2590 and E2594 stay for the forms the type checker sees directly. The run-time panic and `rt.checkGoFunc` are deleted.

Why: Principle 1: doubt resolves to a compile error. A kernel may have no use for a panic here, and the run-time check looked only at the starting task's own stack, so it was incomplete. A func value loaded from heap or package memory is never a frame closure, since storing one there is E3002.

Lands with: roadmap item 108.

## D14. `error` is always sendable; other interfaces keep the whole-program rule with a precision fix (7.9, 8.8, 16.7, 19.2)

Question: How does an interface value that holds a `shared[T]` cross tasks?

Decision: `error` and every interface that embeds it are always sendable. Converting a value of a non-sendable type to such an interface is a compile error at the conversion (E2020), which names the `shared[T]` and shows `alloc.Synced`. Other interfaces keep the whole-program rule, except that a conversion whose result section 18.3 proves is a view of the frame (such as `fmt.Println(sh)`) does not count, and the crossing error names a conversion that made the interface non-sendable, with package and line.

Why: Errors cross tasks in ordinary code, and a `chan error` must not break because an unrelated package converted a `shared` value. Race safety is unchanged, since `synced` counts are atomic.

Lands with: roadmap item 107.

## D15. Deferred calls evaluate at the `defer`; `defer x.Drop()` saves `&x` (12.19, 18.5, 18.6)

Question: What does `defer` do with an owner?

Decision: One rule with no special case. The function value, arguments and receiver are evaluated at the `defer` statement. An owner passed by value, or as the receiver of a value-receiver method, moves there. A pointer-receiver method saves `&x` and `x` stays usable. `defer x.Drop()` is the pointer-receiver case: the deferred call drops whatever `x` holds when it runs (nothing, if `x` was moved), and `x` is not dropped again at scope end.

Why: `defer f.Drop()` right after the open is the idiom Go programmers write, and it works. Moves happen where Go evaluates arguments, so nothing is hidden.

Lands with: roadmap item 112.

## D16. `time.AfterFunc` returns a counted `synced[Timer]` handle; dropping it never stops the timer (19.6, F.7, A.3, abi.md 12.9)

Question: What does the statement `time.AfterFunc(d, f)`, which discards the result, do?

Decision: `AfterFunc` returns `synced[Timer]` instead of `owned[Timer]`. While the timer is armed the runtime holds a reference of its own, released when `f` is started or the timer is stopped. Dropping every handle never stops the timer; `Stop` through any handle does. `NewTimer` and `NewTicker` keep `owned[...]`, stopped when dropped. No new error and no new function.

Why: The statement then does what it does in Go, and `t.Stop()` and `t.Reset(d)` read as in Go. The allocation is at the visible call and its lifetime is counted. Releasing a count never waits.

Lands with: roadmap item 109.

## D17. The elementwise loop over two slices is accepted with one overlap check per task start (19.4, 17.1)

Question: Is `s.Task(func() { out[i] = f(in[i]) })` with `in, out []int` an error (E3402) because the caller could pass overlapping slices?

Decision: Accepted. When every task of an `s.Task` loop accesses two views that may overlap only in the same partition form with the same loop variable and parameters, the program checks before each `s.Task` call that the views start at the same element or do not overlap, and panics otherwise. Every other access stays E3402 (`in[i+1]` with `out[i]`). The check is always performed, in unchecked and kernel builds too, since it is a race check and section 21.5's unchecked mode removes only generation and epoch checks.

Why: The commonest parallel pattern compiles as written for one comparison per task start. Section 19.4 already panics on a non-positive step.

Lands with: roadmap item 110.

## D18. One rule for `defer` in every kind of loop; E2533 folds into E2530 (12.11, 12.19, A.2)

Question: When is a `defer` inside a loop allowed?

Decision: If every path from it leaves every loop that contains it before it can reach the `defer` again. Leaving means `return`; a call of `panic` or of a function that never returns; a `goto` to a label outside the outermost loop; a `break` that ends the outermost loop; a labeled `continue` naming a loop outside it. An unlabeled `continue` does not leave. Otherwise E2530. E2533 is retired. E2534 stays, because a range-over-func body is a separate frame.

Why: The lock-and-return loop Go programmers write compiles, and changing a slice to a `vec.Vec` no longer changes whether a loop compiles. The one-slot reason is kept exactly.

Lands with: roadmap item 111.

## D19. `print` and `println` format floats as Go does; printable types follow underlying types (13.12, A.2, A.4)

Question: How do `print` and `println` write a floating-point value, and which types can they print?

Decision: `print` and `println` write a floating-point value exactly as Go's runtime does, which in Go 1.27 is the shortest decimal form that reads back as the same value, in the `g` format of `strconv` (`println(1.5)` prints `1.5`, `println(1e21)` prints `1e+21`). A value whose underlying type is boolean, integer (enums included), floating-point or string can be printed; an owner type cannot, even over an integer. Kernel mode keeps the bit-pattern form because the console code cannot use the floating-point unit. The specification already states this (13.12). The earlier recommendation that quoted the fixed form `+1.500000e+000` was based on the older runtime and is withdrawn: the goal it served, agreement with Go, is met by the shortest form.

Why: `println(x)` is common in small programs kept as Go-oracle tests, and code that looks like Go and compiles must print what Go prints. Readable output belongs to `fmt`.

Lands with: roadmap item 114.

## D20. A type defined from an owner drops as that owner; `x.Drop()` works on every owner (7.18, 8.7, 10.8, 18.6)

Question: What does dropping a value of `type B owner A` do when `A` has a `Drop`?

Decision: The value of `B` holds an `A`. Dropping it runs `B`'s own `Drop` if declared, then `A`'s full drop (its `Drop`, then its fields). This is drop glue, not method inheritance: `B` gets none of `A`'s methods. `x.Drop()` on a whole owner variable is the early-drop operation for every owner type, whether or not it declares `Drop`; it is not a method value and does not put `Drop` in the method set.

Why: The compiler works out freeing and the program is correct with nothing written. A compile error would leave no clean way to write `B`'s `Drop`, since calling `A`'s `Drop` through a pointer is E3307 and a conversion out of a place is E3303. It matches the embedded-owner rule and `type B owned[T]`.

Lands with: roadmap item 113.

## D21. `Drop` takes a pointer receiver only (7.18, 10.14, 18.6)

Question: May `Drop` have a value receiver?

Decision: No. `func (f File) Drop()` is E3201, as is `Drop` on a type not declared `owner`.

Why: A value receiver on an owner consumes the receiver, which would then be dropped a second time. Every example already uses `*T`. It also makes D15 uniform.

Lands with: roadmap item 112.

## D22. `defer` accepts exactly the built-ins allowed as statements (12.19, 13)

Question: Which built-in calls may be deferred?

Decision: Exactly those that may stand as an expression statement: `clear`, `close`, `copy`, `delete`, `panic`, `print`, `println`. The rest, and the functions of package `unsafe`, are E2532. This confirms D5; section 13 now states the list instead of referring to 12.19.

Why: As in Go, and the circular reference between chapter 13 and 12.19 disappears.

Lands with: roadmap item 82 (done); no new work.

## D23. Only `rune` itself converts to a string (8.11, 11.24, A.2)

Question: Does `string(x)` accept a defined type over `int32`?

Decision: No. The single-value conversion accepts a value of type `rune` (identical to `int32`) or an untyped integer or rune constant; the slice conversion accepts a slice whose element type is `rune`, including `type runes []rune`. `type myRune rune` and `type Fd int32` are errors in both forms.

Why: `string(Fd(3))` is the false friend appendix A guards against, and a defined type over `int32` cannot be told from one meant as a rune.

Lands with: roadmap item 115.

## D24. Per-CPU variables leave the package-variable rule; compound access needs a non-migrating region (19.3, 20.9, memory-model.md 6.4)

Question: How are `//pu:percpu` variables checked?

Decision: Rule 1 of 19.3 does not apply to them. A lone load or store of a whole per-CPU variable of at most a machine word is allowed anywhere. Every other access (compound assignment, `++`, read and write in one expression, a variable larger than a word or part of one, a pointer-receiver call, `&v`) must be inside a non-migrating region (preemption disabled, maskable interrupts disabled, or an `rt.Lock` held, inferred like held locks); if a handler may also access the variable, inside an interrupt-disabled region. A pointer from `&v` may not be used after its region. Two new errors: E2022, an access outside the region it needs; E2023, a pointer to a per-CPU variable used outside its region.

Why: The kernel's most common idiom (`current`) stays safe code and is checked. A lone load and a later lone store may act on two processors' copies after a migration; that is defined behavior, not a race.

Lands with: roadmap item 118.

## D25. Interrupt handlers are parties in the race rules; allocators get no exemption (18.12, 19.3, 20.10, 20.11, F.5)

Question: How do data shared with an interrupt handler meet the race rules?

Decision: Each installed handler is a party that may run anywhere interrupts are enabled, on any processor, and on several at once. Rules 1 to 3 apply with handlers as parties. Data a handler accesses (other than per-CPU variables) must be atomic or guarded by an `rt.Lock`. `volatile[T]` and interrupt-disabled regions exclude only the same processor, so they suffice only for per-CPU data. Errors are E3502 and E3503 naming the handler. An allocator's state is data like any other: "or no lock" is deleted from 20.10. `x86.Cli`, `x86.Sti`, `x86.SaveFlagsCli` and `x86.RestoreFlags` stop being unsafe code.

Why: Safe code has no races (18.12), and kernels are written in safe code. An allocator in safe code is checked by the same rule; one with unsafe code is already outside the check.

Lands with: roadmap item 119.

## D26. The interrupt-context wait check follows inserted drops (20.10, 20.11)

Question: Does the check that interrupt context never waits follow drops the compiler inserts?

Decision: Yes, like any call: at scope end, on assignment over an owner, for temporaries, and the release of a counted value at count zero, into `Drop`, drop glue and the `Free` of every installed allocator. An error (E2584) only where one of these may wait.

Why: The result no longer depends on whether cleanup is written or inserted.

Lands with: roadmap item 120.

## D27. `//pu:blocking` stays on functions with a body (1.2, 20.3, 20.10)

Question: Is `//pu:blocking` on a function with a body a trusted promise?

Decision: Kept. It states a wait the compiler cannot see, such as a spin until another context sets a flag. It only adds the restriction of 20.10, never makes a program accepted, so it is not unsafe code.

Why: Directives state what the compiler cannot see; who will set a flag is such a fact.

Lands with: roadmap item 120.

## D28. Bodyless functions are trusted not to wait; the wait check guards against deadlock (1.2, 18.12, 20.10, 21)

Question: Is the interrupt-context wait check a safety analysis that must cover assembly and C?

Decision: No. It is a check against deadlock, which principle 1 does not promise to prevent. It follows all Plutonium source and takes the declaration of a bodyless function as its contract: not waiting unless marked `//pu:blocking`. Such functions are already unsafe code, listed by `pluto vet -unsafe`.

Why: No new directive for ordinary kernel assembly, and the contradiction between 1.2 and chapter 21 goes away.

Lands with: roadmap item 120.

## D29. Conversions between packed types keep reserved bits; comparison ignores them (7.14, 8.10)

Question: Are the bits of `_` fields and uncovered backing bits kept?

Decision: Yes. Converting an integer or byte array to a packed type keeps every bit, and converting back returns the same value; writing a field changes only its bits. Reserved bits are zero in a literal and the zero value. Two packed values are equal when their bits outside the reserved bits are equal, compared bit by bit (a floating-point field by its bits); map hashing ignores reserved bits too.

Why: Register read-modify-write must round-trip bits the hardware defines, and ignoring reserved bits matches Go's rule for blank fields.

Lands with: roadmap item 116.

## D30. Array-backed packed types number bits by byte; a big-endian bit field may not cross a byte (7.14)

Question: How are bits laid out when the backing is `[N]byte`, `[]byte` or absent?

Decision: Bit position `p` is bit `p mod 8` (0 the least significant) of byte `p / 8`, and a bit field occupies consecutive positions, least significant bit lowest. A byte-level field of `k` bytes is stored in the stated order (`be` or `le`), not the target's. With `be` (or no order on a big-endian target) a bit field may not cross a byte boundary and a byte-level field must start on one (E2021); the fix is a nested packed field with an integer backing. Integer backings keep the existing rule.

Why: Protocol headers and on-disk formats are a stated purpose of packed types. The crossing rule keeps big-endian layouts unambiguous.

Lands with: roadmap item 117.

## D31. Only a `sync.Scope` join guarantees package cleanup; a bounded wall-clock grace before the abandon path (15.4, 19.1)

Question: D10 says cleanup and the exact leak report run when no task is left. A task
that wakes `main` through `defer wg.Done()` or a closed channel can still be returning
(running its deferred calls and drops) when `main` observes the count, so a program that
joins with a `sync.WaitGroup` or a channel took the abandon path now and then, and the
runtime papered over it by yielding up to 200 times, which on an oversubscribed machine
delayed the exit of a program with spinning workers by seconds. What does 15.4 promise?

Decision (final): package cleanup and the exact leak report are guaranteed only when every
task has retired when `main` returns, which a `sync.Scope` join provides (its join follows
the end of the scoped task's function and drops). A `sync.WaitGroup` or a channel join
makes cleanup likely but not guaranteed, because `defer wg.Done()` releases `wg.Wait()`
while the task is still returning. Code that needs a resource released on every exit uses
`defer` in `main`. Before the abandon path the runtime waits a short wall-clock time
(about a millisecond) for tasks that are finishing, not a count of yields, so the latency
of the exit does not depend on the load (a scheduler without a Now method, which runs no
timers, bounds it by a count). The specification says this in section 15.4.

Why: no false friends and Go-familiar (a Go program that returns from `main` with workers
running exits at once, and a `WaitGroup` there promises nothing about cleanup); the
guarantee that remains is the one the compiler can state exactly, a scope join, and the
alternative (a per-task epilogue mark that makes `Wait` wait for the return of the
function) would add cost to every task to keep a promise Go does not make.

Lands with: the specification edit of section 15.4 and the review fixes of roadmap item 105.

## Deferred

- Linux file names that are not valid UTF-8 (open part of roadmap item 57): byte paths,
  a dedicated path type, or refusing non-UTF-8 names. This needs a design of its own, not
  a one-line decision. The `os` package keeps treating paths as strings until it is made.

## Superseded decisions

### D3: lookup at each payload address's 64 KiB base

The 2026-10-11 D3 wording assumed the existing check could reach every large allocation's
table by masking each data address to its 64 KiB base. The user replaced that assumption
on 2026-10-12 with a shared metadata directory. Contiguous objects larger than one chunk
need the later boundaries for data, so a table at every boundary cannot represent them.
Large allocation tables remain at their own metadata bases; directory lookup replaces
the address-mask lookup. That 2026-10-12 revision retained the fixed-arena exemption and
generation/epoch values. D7 later supersedes the exemption and permits no stale-matching wrap.
