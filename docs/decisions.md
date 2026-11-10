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
`plans/2026-10-17-review-design-decisions.md`), in that order. D31 to D35 record later
review decisions of the same day, and D36 to D40 the owner's final decisions on the open
questions about operating-system names, per-CPU access, interrupt gates, waiting for tasks and
`time.AfterFunc`. D41 records the kernel lifetime protection decisions of 2026-10-28. D42 to D58
record the specification rulings of 2026-11-09 (`verification/2026-11-09-spec-rulings.md`); D42 to D46 are the
user's decisions on audit findings A1, C1, C2, B1 and B3. D59 to D68 record the rulings on audit D; D59 to D61 are
the user's decisions on findings D1, D2 and D3.

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

## D32. A deferred function literal that captures an owner counts as a use at function exit (18.5, 18.6, 12.19)

Question: A function literal in `defer func() { use(o) }()` captures the owner `o` by reference. If the
function later moves or drops `o` (`consume(o)`), the deferred call runs when the function returns and finds
`o` empty, which would be a nil-owner panic at run time. What is it?

Decision: A compile error, the existing use-after-move error (E3301, or E3302 where the move happens on some
paths only). The capture by a deferred literal counts as a use at the point the deferred call runs, that is
at every exit of the function after the `defer` statement. A move or drop of the owner on a path from the
`defer` statement to an exit is a conflict; an owner that is given a new value before the exit is fine, and
so is a move on a path that returns before the `defer` statement.

Why: No hidden failure and Rust-level memory safety without annotations: the program is wrong whatever the
input, so the compiler says so instead of leaving a panic that depends on the path taken. Rejecting is the
safe direction.

Lands with: the move checker (`internal/moves`) and the sentence in section 18.5 (commit 9561c0d0 for the
function literal in call position); the extension to views of an owner in the receiver or arguments of a
`defer` statement and to function literals held in local variables, in the same analysis and the paragraph
that follows that sentence in 18.5.

## D33. Exhausting an `errors.New` counter panics and is not reachable in practice (16.3)

Question: The per-site counter of `errors.New` has 2^64 - 1 values. Does the specification need more than
the panic it already states?

Decision: No. Section 16.3 states the panic and adds that it is not reachable in practice (about 584 years
at one evaluation per nanosecond).

Why: The rule stays simple (no wrap, so two errors never compare equal wrongly), and no mechanism is added
for a case that cannot occur.

Lands with: one sentence in section 16.3.

## D34. With a handler also using a per-CPU variable, any load or store through a `&v` pointer needs interrupts disabled, except a lone word-sized store (20.9, memory-model.md 6.4)

Question: Section 20.9 allowed a lone load or store of a word anywhere and said `&v` needs a region, but the
compiler is stricter for a pointer: what does it check when a handler also uses the variable?

Decision: The implemented rule stands. A load or store through a pointer from `&v` must be in an
interrupt-disabled region when a handler also uses the variable. The only exception is a store that is the
whole statement and writes the whole variable of at most a machine word. A load through the pointer has no
exception. The text says this and no looser wording for loads remains.

Why: A load through a pointer is not the single `GS`-relative instruction of a direct access: the pointer
was formed earlier, and the value read may be one that a handler on the same processor is changing in the
middle of a larger use. Matching the text to the stricter code keeps the checked rule and the written rule
the same.

Lands with: the edit of section 20.9 and memory-model.md section 6.4.

## D35. The first release ends a nested per-CPU region (20.9)

Question: The region analysis counts a region once. Does the first `rt.EnablePreemption` end a region that
was opened twice?

Decision: Yes. The first release of a kind ends the region even if its opening was nested; this errs toward
rejecting. No code change.

Why: A precise count would need path-sensitive nesting depth for a rare pattern; rejecting is the safe
direction and the program can restructure.

Lands with: nothing to build; section 20.9 already states it.

## D36. Operating-system names that are not valid UTF-8 are an error, with a lossy form named as such (7.3, A.3, F.9)

Question: A file name, an environment variable or an argument from the operating system may not be valid
UTF-8, and a string must be. The library returned a string with each invalid byte replaced by U+FFFD. That is
a hidden fourth difference from Go: `os.Getenv` and `e.Name()` look like Go and return something else. What
should they do?

Decision (final): the Go-named string forms return an error instead of a lossy string: `name, err := e.Name()`
and `v, err := os.Getenv(k)`. The error is `*os.NameError` (the name is not UTF-8; it holds the raw bytes). A
single-value use (`name := e.Name()`) is a compile error whose explanation names `RawName`. A display-only form
is named so that the loss shows in the source: `e.NameLossy()`. The byte forms stay (`RawName`, `OpenBytes`,
`GetenvBytes`). This is the split of `strings.FromUTF8` and `strings.FromUTF8Lossy` (A.2). PlutoniumOS refuses
non-UTF-8 names at creation. The Deferred entry on Linux file names is closed by this decision.

Why: no false friends. Code that looks like Go and compiles must do what Go does or make the difference
visible; a silent replacement of bytes did neither, and a lossy name could not open the file it came from. An
error is visible at the call, and the display form says what it does in its name.

Lands with: appendix A.3 and A.5, section 7.3, appendix F.9, section 16.2 and 16.7 (the error type), and the
library `lib/pluto/os` with its callers, `tools/go2pu` and the skill's references (roadmap item 122).

## D37. A per-CPU store that depends on an earlier load of the same variable needs a non-migrating region (20.9, 19.3, memory-model.md 6.4)

Question: Section 20.9 allowed a lone load and a lone store of a per-CPU word anywhere. But
`x := ticks; ticks = x + 1` is two lone accesses that together are `ticks++`, and the task may move to
another processor between them. Is that allowed?

Decision (final): No. A store whose value or controlling condition depends, by data or control flow, on an
earlier load of the same per-CPU variable is a read-modify-write and needs the same non-migrating region as
`ticks++` (E2022). Independent lone loads and stores stay allowed. `x := ticks; ticks = x + 1` and
`if current == idle { current = next }` are E2022 outside a region; inside `rt.DisablePreemption()` and
`rt.EnablePreemption()` they are fine.

Why: rust-level race safety without annotations. The rule for `ticks++` was a syntactic accident; the hazard
is the dependence, and writing it as two statements must not escape the check.

Lands with: sections 20.9 and 19.3, memory-model.md section 6.4, abi.md section 10.6 (roadmap item 123).

## D38. A handler is entered through an interrupt gate; the check is made through types (20.3, 20.8, 20.10, 20.11, 19.3, abi.md 10.4, memory-model.md 6.1)

Question: The compiler treated the body of an interrupt handler as an interrupt-disabled region, but nothing
made the processor disable interrupts on entry; the kernel could install the handler through a trap gate.
How is the assumption made true?

Decision (final): the contract is that a handler is entered through an interrupt gate, which disables
interrupts. The check is made through types: handler addresses are installed through a typed
`x86.InterruptGate` (type `x86.Gate`), and raw IDT words become unsafe code. A handler that can be re-entered
says so with the directive form `//pu:interrupt trap`, installed through `x86.TrapGate`, and its body is not an
interrupt-disabled region. A call of `x86.Sti` inside a handler ends its interrupt-disabled region.
Exceptions and NMIs remain a kernel contract. `x86.HandlerAddr` is replaced by the two gate constructors.
The directive list in 20.3, and every other list of directives, names the `trap` form.

Why: the safety claim (per-CPU data shared with a handler is safe in the handler's body) held only if the
gate was right, and an unchecked claim is a promise taken on trust. Types make the common case checked
without annotations for ordinary kernel code, and the rare re-entrant handler says so at its declaration.

Lands with: the sections named above (roadmap items 124 to 127).

## D39. There is no explicit wait-for-all function (15.4, 19.4)

Question: Should `rt.WaitTasks` or `sync.WaitAll` wait for every task of the program?

Decision (final): No. A program that wants to wait for its tasks uses `sync.Scope` (guaranteed join and
package cleanup) or a `sync.WaitGroup` or a channel as in Go.

Why: such a function would wait for library tasks that never end, which brings back the hang of D6, and it
would be an unscoped second way to do what `sync.Scope` does. The text next to D31 says so.

Lands with: one paragraph each in sections 15.4 and 19.4. Nothing to build.

## D40. `time.AfterFunc` keeps returning `synced[Timer]` (19.6)

Question: `NewTimer` returns `owned[Timer]`, stopped on drop. Should `AfterFunc` do the same?

Decision (final): No. `AfterFunc` keeps returning `synced[Timer]`, a counted reference whose drop releases a
count and does not stop the timer, in contrast with the `owned[Timer]` of `NewTimer`, which is stopped on drop.

Why: the call `time.AfterFunc(d, f)` with the result discarded is common in Go and must keep working as it
does there; the handle is for `Stop` and `Reset`.

Lands with: one sentence in section 19.6. Nothing to build.

## D41. Kernel lifetime protection uses canonical XOR tags; kernel builds need `--gencheck=false` until it lands (18.11, 21.5, abi.md 1.3)

Question: the seven questions of the kernel lifetime protection plan
(`docs/plans/2026-10-28-kernel-lifetime-protection.md`, section 6), roadmap item 101.

Decision (final, 2026-10-28):
1. Checked kernels require 4-level paging. Under LA57 a checked kernel refuses to start, with a message.
2. The kernel supplies the checks' metadata pages through a hook bound at link time, as `//pu:scheduler` is,
   not through a run-time setter.
3. A checked kernel build with no such hook is a link or build error.
4. `--gencheck=false` and `//pu:nochecks` stay the only opt-outs. There is no named unchecked region.
5. A checked kernel heap lives in a remappable virtual window. About 25% metadata overhead is accepted for
   now and is measured in phase 2.
6. abi.md section 1.3's untag rule becomes sign extension from bit 47 on every target. Lower-half pointers keep
   their bits.
7. Until phase 2, a kernel or freestanding build that does not pass `--gencheck=false` is an error that says so
   and names section 18.11 and item 101.

Why: section 21.5 forbids building a kernel unchecked because the mode was selected, and forbids accepting an
unprotected access because the hosted encoding cannot represent kernel addresses. Rejecting the build until
the checks exist keeps unchecked execution an explicit choice. One encoding for every target keeps kernels and
hosted programs on the same checks (suitable for the PlutoniumOS kernel), and a tagged pointer that skips its
untag is noncanonical, so it faults instead of reading memory.

Lands with: phase 0 (the rejection) and phase 1 (the encoding in abi.md section 1.3) of item 101. Phases 2 to
4 follow the plan.

## D42. A view of a local with cleanup ends at the local's scope (18.3, A.3)

Question: audit A1. A view `&s` of a counted local could be used after the block that declares `s` ended and
released it. E3313 named owners only.

Decision (final, user, 2026-11-09): E3313 covers every local whose type has drop glue (an owner, a counted
value, or a type with a counted or owner field). A view of such a local, or of memory it owns, may not be used
after the local's scope ends, as after a move, drop or new value.

Why: Rust-level safety without annotations. The compiler already tracks block ends to run drops, so the rule
needs no annotation and adds no run-time cost.

Lands with: section 18.3 and appendix A.3; roadmap item 146.

## D43. Owner conversions and owner literals are limited to the defining package (7.13, 7.18, 8.11, 11.3, 11.24)

Question: audit C1. A conversion to an owner type, or a literal of one, could forge an owner in another package.

Decision (final, user, 2026-11-09): a conversion whose result is an owner type, a pointer to one, or an owner
union is valid only in the package that defines the result type (E3212). A conversion between owner types
defined from each other is valid only in the package of the defined type. A pointer conversion from `*Owner`
to a non-owner pointer type is limited the same way. A struct literal of an owner type in another package is
valid only if every field is exported (keyed or not); exported variants of an owner union are public in the
same way.

Why: an owner's invariant (one live owner, one drop) is the defining package's promise. The audit's broader
exception for operands defined from the result type would let any package forge an owner in two steps.

Lands with: the sections named above; roadmap item 147.

## D44. A type-switch clause or assertion to an ordinary interface lends (7.9, 11.13, 12.9, 18.5)

Question: audit C2. Whether a type switch on an owning interface moves the operand in a clause that names an
ordinary interface.

Decision (final, user, 2026-11-09): a clause that names a single ordinary interface type binds a view lent from
the operand and does not move it. An assertion from an owning interface to an ordinary interface lends the
same way. After the switch the operand is possibly moved if any clause moved it (E3302 on later use).

Why: an ordinary interface is a view kind; moving into it would lose the drop obligation. Lending keeps the
common "does it implement Stringer" test free of moves.

Lands with: the sections named above; roadmap item 148.

## D45. An arena counts its live counted objects (18.8, 18.9, 13.7, 16.7, 17.1, 17.6, 21.5, F.2, abi.md 1.3, memory-model.md 3.8)

Question: audit B1. A string, box, map, channel or error allocated in an arena could outlive a `Reset`,
`Release` or `Drop` of the arena through a count held elsewhere.

Decision (final, user, option a, 2026-11-09): each Fixed and Chunked arena keeps a live count of the counted
objects allocated in it. `Reset`, `Release` and `Drop` panic with "arena has live counted objects" while the
count is not zero. `Release` checks the whole count. The manual `arena.NewUnsafe` arena keeps no count.

Why: safety without a borrow checker. The cost is one atomic counter per arena and one atomic increment and
decrement per counted object allocated in an arena; plain values pay nothing. The check stays in unchecked mode
because it guards a free, not an access.

Lands with: the sections named above; roadmap item 152.

Changed by: D70 counts every live allocation and replaces the panic message "arena has live counted objects"
with "arena has live allocations" (see Superseded decisions).

## D46. An arena takes memory back in bulk only (18.9, F.2, abi.md 1.3)

Question: audit B3. `Free` of an arena's latest allocation reclaimed it, which needed a per-allocation epoch
check and a panic.

Decision (final, user, 2026-11-09): an arena reclaims memory only through `Reset`, `Release` and `Drop`.
`Free` through an arena allocator reclaims nothing, and the "arena allocation freed" panic is removed.

Why: one reclaim path keeps the epoch rule simple and the check cheap; a stack-like `Free` saved little.

Lands with: the sections named above; roadmap item 154.

## D47. The comma-ok move is followed through direct tests of `ok` (11.13, 18.5, 7.13)

Question: audit C3. After `v, ok := x.(T)` from an owning interface, whether `x` is moved on the paths where
`ok` is false.

Decision: the compiler follows `ok` only through direct tests: an `if` condition that is `ok`, `!ok`, or a
chain of `&&` or `||` with one of these as an operand, with no assignment to `ok` in between. On a branch where
the condition fixes `ok`, `x` is moved when `ok` is true and still holds its value when it is false. On any
other path `x` is possibly moved (E3302).

Why: ordinary code needs no annotation, and the rule stays local and predictable.

Lands with: the sections named above; roadmap item 149.

## D48. An enum type is not an integer type (7.12, 11.24)

Question: audit C4. Whether the integer conversion rules apply between enum types.

Decision: an enum type is not an integer type for conversions. A conversion between two enum types, or a
constant that is not a member in the comma-ok form, is E2313. An enum converts to and from its integer type
as section 7.12 says.

Why: no false friends. In Go an enum is an integer, so the difference is listed in appendix A.

Lands with: sections 7.12 and 11.24; roadmap item 150.

## D49. Defined union and packed types (7.13, 7.14, 8.2, 10.8)

Question: audits C5 and C6. What `type V U` means when `U` is a union or packed type, and the underlying type of
a type constructor instance.

Decision: `type V U` declares a new union or packed type with `U`'s variants or fields and layout and none of
its methods. `V` and `U` convert explicitly. A defined union or packed type and each instance of a type
constructor is its own underlying type.

Why: Go-familiar. It is how a defined struct type behaves.

Lands with: the sections named above; roadmap item 151.

## D50. The C boundary refuses boxes by value (20.4, 18.12, abi.md 9.1)

Question: audit B7. C could copy or keep a counted box passed by value, which breaks its count.

Decision: `owned[T]`, `shared[T]`, `synced[T]` and `weak[T]` are errors as by-value parameters and results of
`//pu:extern` and `//pu:export` functions (E2511). A program passes `unsafe.Pointer(b)` and keeps the box alive.
Behind a pointer, C may read a box but not copy or store it. Functions marked `//pu:export` are unsafe code
for `pluto vet -unsafe`.

Why: C cannot keep a count or run a drop. `owned` is included because C cannot drop it either.

Lands with: the sections named above; roadmap item 157.

## D51. A task exit mid-statement or inside `Drop` (17.3, abi.md 12.10)

Question: audit B8. What `rt.ExitTask` does to operands already evaluated, and to a `Drop` method it passes
through.

Decision: operands already evaluated are dropped as on a return from the enclosing statement; an owner moved
into a call in progress belongs to the callee. A `Drop` method that `rt.ExitTask` leaves still finishes its
object's field drops and free, so no task ends inside a drop.

Why: every owner is dropped exactly once on every exit path.

Lands with: section 17.3 and abi.md 12.10; roadmap item 158.

## D52. Interrupt handlers follow Rules 1 to 3; the init exemption ends at the first handler (19.3, memory-model.md 3.1, 4.1, 6.1)

Question: audit B6. The handler sentence required atomics or locks for all shared data, which is stricter than
Rules 1 to 3, and the init exemption did not say when handlers start.

Decision: data a handler accesses while other code may write it, or writes while other code may access it,
must be atomic or guarded by an `rt.Lock` (unless per-CPU). Data not written while a handler may run needs
neither. The package initialization exemption ends at the first statement that may install a handler or enable
interrupts (`x86.Sti`, `x86.RestoreFlags`).

Why: one race rule for tasks and handlers; read-only tables in kernels need no lock.

Lands with: the sections named above; roadmap item 156.

## D53. Library state at a panic (17.2, 18.12)

Question: audit B9. What state deferred calls see when a check panics inside a library operation, and whether
an `rt.Lock` held at a panic is released.

Decision: no deferred calls run while an `rt.Lock` is held, because a runtime lock disables preemption
(abi.md 12.4, 12.5); 17.2 already decided this and now says so by name. The standard library's types, and every
type that wraps unsafe code, restore their invariants before each check that can panic, so deferred calls see
consistent state. A held `sync.Mutex` stays locked until a deferred `Unlock` runs.

Why: safe code must not observe a broken invariant through a deferred call.

Lands with: sections 17.2 and 18.12; roadmap item 165 (library review).

## D54. A Fixed arena's bookkeeping lives in its own backing (18.9, F.2, abi.md 1.3)

Question: audit B2. Whether a Fixed arena may ask its parent for bookkeeping memory.

Decision: a Fixed arena sizes its bookkeeping table from its own backing and never allocates from the parent
after creation. Allocation fails (nil, or the allocation-failure panic of the caller) when the table is full.

Why: a Fixed arena is the kernel's bounded allocator; it must not allocate behind the program's back.

Lands with: the sections named above; roadmap item 153.

## D55. The cleanup write check is gated on a flag (15.4, 21.5, abi.md 12.10)

Question: audit B4. The write check for static objects with cleanup obligations ran on every store for the
whole program.

Decision: the check runs only after package cleanup has started, gated on a program-wide flag. Its cost, one
load and branch per store that may reach such an object, is listed in 21.5, and unchecked mode keeps it.

Why: performance near Rust; the check matters only during cleanup.

Lands with: the sections named above; roadmap item 155.

## D56. Safe x86 intrinsics and explicit nil checks for freestanding builds (18.12, 17.5)

Question: audits B14 and B21. Whether every x86 intrinsic is unsafe, and how a freestanding build outside
kernel mode catches a nil dereference with no guard page.

Decision: `x86.Pause`, `Cpuid`, `Rdtsc` and `Rdtscp` are safe. A freestanding build outside kernel mode checks
nil explicitly before each dereference it cannot prove non-nil (one compare and branch).

Why: these intrinsics touch no memory or processor state a program relies on; the nil panic is a promise
every build keeps.

Lands with: sections 18.12 and 17.5; roadmap items 161 and 162.

## D57. `owner` as a type parameter name (4.6)

Question: audit A2. Whether `[owner any]` declares a parameter named `owner`.

Decision: in a type parameter declaration `owner` is the keyword only after the identifier list and before a
constraint. `[owner any]` declares a parameter named `owner`.

Why: a contextual keyword must not take a name a Go user would write.

Lands with: section 4.6; roadmap item 164.

## D58. A handler's print may drop its output; the panic buffer is 1024 bytes (13.12, 20.10, 17.4)

Question: audits B15 and B16. A print in an exception or NMI handler could wait forever on the console lock its
own processor holds, and the panic message limit was not stated.

Decision: a `print` in an exception or NMI handler that interrupts a `print` on its own processor drops its
output. A panic message is cut after 1024 bytes with no marker, possibly inside a UTF-8 sequence.

Why: suitable for the kernel. A handler must not deadlock on output. The limit matches the runtime.

Lands with: the sections named above; roadmap item 163.

## D59. Frees are writes (19.3, 19.4, 12.12, 8.8, memory-model.md 3.7 and 4.1)

Question: audit D1. Whether a task may keep a view of an object that another task frees by replacing or
dropping the owner that holds it.

Decision: dropping, freeing or moving an object, or assigning to or dropping an owner that holds it, is a write
of the whole object for the race rules. A view another task can reach while that happens is E3501 for an
unscoped task and E3404 for a scoped one. Such objects are shared through `synced[T]` or by moving the owner.

Why: a free is the strongest change to memory. Treating it as anything less lets a task read freed memory.

Lands with: the sections named above; roadmap item 168.

## D60. A closure region owns its captured variables (11.4, 11.5, 18.6, 18.8, abi.md 4.1)

Question: audit D2. Who drops a captured owner of a closure that a function returns into a region.

Decision: the region owns every captured variable that has cleanup. It drops them when the region is refilled
or when the caller's frame returns. Parameters a returned closure captures are copied into the region on entry.

Why: the closure outlives the function's frame, so its captured owners cannot be dropped at scope end.

Lands with: the sections named above; roadmap item 169.

## D61. Views end at calls that may free (18.3, 18.10, 18.11, 7.9, 7.17, 11.24, abi.md 5)

Question: audit D3 and candidates 7.17 to 7.19. Whether a view of a local owner's memory survives a call such as
`v.Push(1)` that may free the memory it looks at.

Decision: each function summary records through which pointer parameters the function may free memory or move
an owner out. A view of a local owner's memory ends (E3313) at such a call, at an assignment to an owner field
on its path, and, for a counted reference, at such a call through another copy that may point to the same
object. A conversion of an owner to a non-owner pointer gives a view bounded by the owner. A large owner lent
to an ordinary interface is a view of the variable, not a copy.

Why: this closes the use-after-free without annotations, using the summaries the compiler already builds.

Lands with: the sections named above; roadmap items 170 and 171.

## D62. `defer x.Drop()` does not copy `x` (18.5)

Question: audit D4. Sections 12.19 and 18.6 say a deferred call's arguments are evaluated at the defer, which
would make `defer f.Drop()` then `send(take(&f))` a double drop.

Decision: `defer x.Drop()` on a variable does not copy it. A later move of `x` cancels the deferred drop.

Why: the common cleanup pattern must stay correct without new syntax.

Lands with: section 18.5; roadmap item 175.

## D63. A moving assertion on a place is E3303 (11.13, 12.9, 7.13)

Question: audit D5. What a moving type assertion or bound type switch on a field, element or `*p` does.

Decision: it is E3303, moving out of a place. The fix is to take the value first: `take(&d.state).(T)`.

Why: a place cannot be left moved. The existing code already names this error.

Lands with: the sections named above; roadmap item 172.

## D64. A range loop holds its own count (12.10)

Question: audit D7. Whether the body of a range loop over a counted value can release the value it ranges over.

Decision: the loop ranges over a hidden temporary that holds its own count until the loop ends.

Why: the loop must not read released memory, and one count per loop is cheap.

Lands with: section 12.10; roadmap item 173.

## D65. An owner in a for clause is dropped once (12.10, 18.6)

Question: audit D8. When an owner variable declared in a for clause is dropped.

Decision: it moves to the next iteration and is dropped once, when the loop ends.

Why: per-iteration copies of an owner would drop it many times.

Lands with: sections 12.10 and 18.6; roadmap item 174.

## D66. A race gives torn or stale values (19.3, 19.7, 18.12, memory-model.md 4.2)

Question: audit D10. The documents disagreed on whether a race is undefined behavior.

Decision: a race gives torn or stale values, never undefined behavior in the compiler's sense. A torn pointer
or length can break memory safety.

Why: one statement in every place. The compiler does not assume race freedom for optimization.

Lands with: the sections named above.

## D67. Any non-frame error may be sent (16.7)

Question: audit D9. Section 16.7 limited sending to some error kinds.

Decision: every `error` value that is not a view of a frame may be sent. A frame view is E3005 or E2011.

Why: the sendable rule of 8.8 already decides this; 16.7 now matches it.

Lands with: section 16.7.

## D68. Where a region closure may go (11.5)

Question: audits D6 and D14. Which uses of a closure that captures frame variables are allowed.

Decision: a closure passed through a func value or an interface counts as kept for the region errors, without
summaries. A closure may be stored in other memory of the same frame and written as the argument of `s.Task`
on a local Scope.

Why: one simple rule, and the two uses ordinary code needs.

Lands with: section 11.5; roadmap item 176.

## D69. A `%w` wrapper of a field error is bounded by the object (16.5, 16.7)

Question: audit B5. Section 16.5 said that wrapping an error held in a field of a longer-lived object with
`fmt.Errorf` makes it independent of the object. The audit proposed that `%w` copy such an error into a
counted error.

Decision: as in Go, `%w` keeps the error it wraps. For an error in a field (`&p.err`, a `fmt.ErrorBuf`) the
wrapper holds a pointer into the object, so `errors.Unwrap(e) == err` holds and `errors.As` sees the field.
Only the message text is copied. The wrapper is bounded by the object like the error it wraps, and the view
rules of 18.3 apply to it. Code that needs an independent error formats it with `%v`, or copies what it needs
into an error allocated with `alloc.Synced`.

Why: in Go a wrapped pointer error is the same pointer and sees later changes. A copy would make `Unwrap` and
`As` differ from Go, a false friend. The escape analysis already bounds `Errorf`'s result by its `%w` operand.

Lands with: sections 16.5 and 16.7 rule 4; roadmap item 177 (tests).

## D70. An arena counts every live allocation (18.8, 18.9, 13.7, 13.9, 16.7, 17.1, 17.6, 21.5, F.2, abi.md 1.3, memory-model.md 3.8)

Question: audit E3 and E9. D45 counted only counted objects and left owners uncounted. An owner allocated
in a Fixed or Chunked arena (a box from `new(T, &a)`, a vector's array, a map table, a channel) can be moved
to another task, and a `Reset` while that task still uses it frees its memory.

Decision: this extends D45. Each Fixed and Chunked arena counts every allocation made through its `Alloc`
that is not freed yet, owners and counted objects alike. Only the plain views of `New[T]()` and
`Slice[T](n)` stay uncounted. `Reset`, `Release` and `Drop` panic with "arena has live allocations" while
the count is not zero. `Release` still checks the whole count, so an owner that must outlive a `Release` is
allocated from the parent or from another arena. Freeing into an arena still takes no memory back; it only
lowers the count. For a local arena the compile-time window rule already proves that its allocations are
freed before it is dropped, so the count matters there only for an earlier `Reset` or `Release`.

Why: the D1 promise (no use after free) without tracking which owners cross tasks. The cost stays one atomic
add at each end of an allocation's life. The other fix, forbidding an arena-backed owner from moving to
another task, needs annotations the compiler does not have. Goal: Safe, with a small Perf cost.

Lands with: the sections named above; roadmap item 198, which extends item 152.

## D71. A one-word owner lent to an interface is a copy, ended by the D3 events (18.3, 18.5, 7.9, 11.13, 12.9, A.3)

Question: audit E1, E5, E7 and the C2 and D3 verdicts. 18.3 exempted "an ordinary interface that lends"
the variable from the events that end a view of an owner's memory. For a one-word owner the interface holds
a copy of the word, not the variable's address, so `x := any(b); reset(&b); use(x)` read freed memory.

Decision: only two views are views of the variable itself, exempt from calls and field assignments: `&v`,
and an owner larger than one word lent to an ordinary interface, whose data word points at the variable. An
ordinary interface holding a one-word owner (an `owned`, `shared` or `synced` box, a map, a channel, a
`weak` reference, an owner defined from a pointer, a one-field struct or array of one) is a view obtained
from the variable, and each D3 event ends it (E3313). The result of an assertion or type-switch clause from
an owning interface to an ordinary interface is such a view of the asserted variable for every payload size,
since a large payload is a frame copy, not the variable. The function-literal sentence and the E3314 list of
18.3 name every D3 event. 7.9 and appendix A row 80 say which values the interface views in place and which
it copies. abi.md 5 is unchanged.

Why: the layout already copies one-word values; the rule now follows the layout. Extending the C2 case to
large payloads was settled here: the frame copy is dropped when the owning interface is given a new value,
so the result cannot be a view of the variable itself. Goal: Safe, Plain.

Lands with: the sections named above; roadmap item 199, which extends items 170, 171 and 148.

## D72. A conversion through a type defined from another owner type (11.24, 18.5, 8.11, 7.18)

Question: audit E2 and the C1 verdict. Rule 3 of 11.24 let a package convert to a type it declares, so
`type MyFrame owner p.Frame` and `MyFrame(Fake(0x1000))` forged a `p.Frame` outside `p`.

Decision: when the target type `T` is defined from another owner type `A`, `T(x)` (and the pointer and union
forms) is allowed only where converting `x` to `A` is allowed too; an operand of type `A` needs no
conversion. 8.11, the 7.18 summary and 18.5 cite 11.24 for the full limit instead of restating part of it.

Why: a new `T` holds an `A`, so making one makes an `A`. This keeps C1's promise that only `p` makes a
`p.Frame`, with no new code. Goal: Safe.

Lands with: the sections named above; roadmap item 200, which extends item 147.

## D73. Deferred calls belong to a scoped task's interval (19.4, A.3)

Question: audit E4. In a function's outermost block a Scope is dropped after the deferred calls run, and a
panic runs the armed deferred calls while scoped tasks keep running. No rule checked a deferred call that
drops or writes what a running task uses.

Decision: a deferred call counts as a statement of a scoped task's interval when a path runs its `defer`
statement and then reaches a point of that interval, or a return before the task is joined. The Sharing
rules (E3402) and E3404 apply to it, with the accesses of its callee or literal. The rule is the same in every
build mode. A deferred `s.Wait()` joins the tasks started on `s` after its `defer` statement before any call
deferred earlier runs. The fix is to arm the deferred call after `s.Wait()`, or to arm a deferred `s.Wait()`
after it and before the first `s.Task`; an owner may also be left to the end of its block.

Why: keeps the D1 promise with no run-time cost. The ruling named "declare the Scope before the deferred
call" as a fix, but the drop order of 19.4 runs every deferred call before any Scope of the outermost block is
dropped, wherever the Scope is declared, so that fix does not work; the deferred `s.Wait()` does what it
meant. A plain `s.Wait()` before the return covers the return path but not a panic between the `s.Task` and
the `Wait`, so the rule counts panic paths too. Counting them in freestanding and kernel builds, where a
panic runs no deferred calls, keeps one result for every build. Goal: Safe, Plain.

Lands with: section 19.4 and appendix A; roadmap item 201, which extends item 168.

## D74. Arenas drop the values of their plain views (18.9, 18.6, 18.8, 18.10, 13.7, 13.9, 16.7, 17.1, 21.5, F.2, abi.md 1.3, memory-model.md 3.8)

Question: audit F1. An owner or counted value stored in a plain view from `New` or `Slice` was never
dropped, so it kept the arena's count up and `Reset` panicked. 18.9 claimed that a program that compiles
drops such values before a local arena is dropped, which was false.

Decision: for a type with drop glue, `New` and `Slice` of a Fixed or Chunked arena record a drop entry
(address, element count, drop function). `Reset` and `Drop` run every entry, and `Release(m)` the entries
recorded after the mark, in reverse order of allocation; each entry drops its values in place and leaves
the zero value. Then the call checks the count. A type without drop glue records nothing. These details
were settled here:
- A place already emptied (by `take`, or by assigning the zero value) holds the zero value, whose drop
  does nothing. Unsafe code that uses `unsafe.Drop` on arena memory must leave the zero value there.
- The arena takes an entry off its table before running it. A drop function that panics leaves the call:
  the other entries stay recorded, the count is not checked, the epoch does not end and nothing is taken
  back. A deferred `Reset` for that panic starts from the entries left.
- The order is: run the entries, check the count (and that no entry was recorded meanwhile), then zero
  the granules, end the epoch and move the cursor back. Program views are dead from the start of the call
  by the existing rules (E3313 for a local arena, the epoch check otherwise); the entries run while the
  epoch is current.
- A Fixed keeps its entries in its own bookkeeping table, growing down toward the allocation records; a
  `New` or `Slice` that finds no room fails without advancing the cursor and panics, so payload capacity
  holds in full only for allocations without entries. A Chunked keeps entries in blocks from its chunks.
  The mark records the entry position.
- The manual `Arena` of `arena.NewUnsafe` records no entries.
- A deferred `Reset` of an arena that a frame variable still references (audit F7) still panics, since
  deferred calls run before the frame's variables are dropped and the entries cover only plain views.
  18.9 shows the case and the fix (a helper function or a nested block). No new compile error, since a
  deferred `Reset` is correct when the window allocated nothing that survives.

Why: the count exists so a reset never frees memory still in use. Without entries, the natural use of
`a.Slice[string](n)` made every reset panic, and the only fix was to clear each place by hand. Recording
an entry only for types with drop glue keeps plain arenas as cheap as before. Goal: Safe, Plain, NoGC.

Lands with: the sections named above; roadmap item 202, which extends item 198.

## D75. Type-parameter conversions to an interface (18.3, A.3)

Question: audit F2 (verdict E1 incomplete). 18.3 decided a conversion of a type-parameter value to an
interface by "if any instantiation is larger than a word", which a checker that checks a generic body once
cannot know, and it named only the frame-copy half of E1.

Decision: the conversion counts as a frame copy for every instantiation, whatever the size of the type
argument. For a `[T owner any]` parameter it is also a view obtained from the variable that the D3 events
end (E3313, E3314); the checker applies the stricter rule of each kind. Applying the frame-copy rule to a
plain `[T any]` parameter regardless of its type argument was settled here: it is the rule a once-checked
body can state, and it rejects only programs that keep such an interface past the frame.

Why: one check per generic body must give one answer for every instantiation. Goal: Safe, Plain.

Lands with: sections 18.3 and appendix A; roadmap item 204, which extends item 199.

## D76. Frame storage in loops (18.4, 11.3, 11.15, A.3)

Question: audit F3. 18.4 listed the loop storage before ruling C7 and missed the object of `&T{...}`, the
array of a slice literal, the variadic array, a method value's receiver and a deferred call's saved
arguments.

Decision: 18.4 lists all five. Keeping a view of one across iterations is E3006; the fix is `new(T{...})`
or an owner such as `vec.Vec`. 11.3 and 11.15 say the same, and appendix A has a row, since in Go each
evaluation makes a new variable.

Why: the storage is refilled when the same expression runs again (C7), so a kept view would see the next
iteration's value. Goal: Safe, Go.

Lands with: the sections named above; roadmap item 206, which extends item 160.

## D77. The atomic arena counter is accepted (21.5, 18.9, abi.md 1.3)

Question: audit F4. The cost of the atomic counter was stated but not decided.

Decision: the cost is accepted. An implementation may use a plain counter where it proves that no other
task can reach the arena or anything allocated from it.

Why: an owner can be dropped, and a string released, in another task; the counter must see it. The proof
removes the cost where no other task can be involved. Goal: Safe, Perf.

Lands with: the sections named above; roadmap item 203, which extends item 198.

## D78. Codes for a `//pu:stack` size out of range and for implementation limits (20.3, 21.4)

Question: audits F12 and F15. Neither error had a code, and 21.1 requires one.

Decision: both are compile errors. The spec does not invent codes here; it says "a compile error" and the
rulings record lists both as needing codes.

Why: the code catalog is assigned in phase 1 with the other new codes. Goal: Plain.

Lands with: sections 20.3 and 21.4; roadmap item 207.

## D79. A deferred `s.Wait()` during a panic (19.4, 19.1, debugging.md 5)

Question: audit F9. A deferred `s.Wait()` that runs for a panic can wait for good when a scoped task waits
on the panicking task, and the panic's report is replaced by a deadlock report.

Decision: the deferred `s.Wait()` waits for the scoped tasks during a panic as on a return. The deadlock
report names the panic in progress, with its message and where it happened. 19.4 gives the fix: close the
task's channel, or cancel it, in a deferred call armed after the deferred `s.Wait()`. The `panicking:` line
of the hosted report was settled here.

Why: the join keeps D73's promise; naming the panic keeps its cause from being lost. Goal: Safe, Plain.

Lands with: the sections named above; roadmap item 205, which extends item 201.

## D80. Default task stack in freestanding and kernel builds (21.4, 19.1, 19.5)

Question: audit F14. 19.1 pointed to a default stack size in 21.4 that 21.4 did not give, and the overflow
behavior depended on the backend.

Decision: the default is `rt.DefaultStackSize`, 64 KiB, the value the library already uses. 19.5 already
required a backend to put a guard region of at least one page below each stack; it now says this applies
to every stack whose size the compiler did not prove, as abi.md 12.1 says, and 19.1 says freestanding builds
too. Overflow is then a stack overflow panic in every build mode.

Why: a stated floor lets a program know when it must ask for more with `rt.Go`. Goal: Kernel, Safe.

Lands with: sections 21.4, 19.1 and 19.5. No roadmap item: the library already uses this size.

## D81. Generic code takes the strictest rule any type argument needs (18.3, 8.7, A)

Question: audit G1. D75 limited the D3 view events to `[T owner any]`. A `[T any]` parameter admits maps,
channels and counted boxes, which 7.9 lends uncounted as one-word views, so `x := any(t); t = z; use(x)`
in a generic body was a use after free that a once-checked body did not reject.

Decision: the D3 events (E3313, E3314) apply to every conversion of a type-parameter value to an ordinary
interface, for every type parameter, `[T ~int]` included. The other kind rules that depend on the type
argument take the strictest form any type in the parameter's type set needs. These details were settled
here:
- A `T` local counts as having drop glue unless no type in its type set has it. This decides the scope rule
  of E3313, the declaration order rule of 18.4 and the loop rules for generic code.
- A view obtained from a `T` value is bounded by its holder unless every type in the set is a value or a
  view (E3313, E3314, E3009).
- A method call on a value of a type parameter is checked against that method of every instantiating type,
  as its summary says.
- 7.9 is not changed: it states the conversion for a known type, and 18.3 states the generic case.

A sweep of the other places generic code meets a kind rule (assignment, `&x`, loops, closures, sending,
composite literals, comparison, temporaries) found them decided by 8.7, 8.10, 11.5, 18.5 and 19.2 or by the
bullets above.

Why: one check per generic body must give one answer that is safe for every instantiation, and the
strictest answer is the only one that is. Goal: Safe, Plain.

Lands with: sections 18.3 and 8.7, appendix A; roadmap item 208, which extends item 204.

## D82. The checks follow arena drop entries (18.9, 18.3, 19.3, 20.10, 17.1, abi.md 1.3)

Question: audit G2, with G8 and G13. D74's drop entries run drop glue at `Reset`, `Release` and `Drop`,
but the Fixed paragraph still promised such calls may run where the parent may not, and E2584 did not
follow them. Separately, the chapter and the ABI disagreed on an allocation made during the entries, and a
`Slice` of length zero took a table entry.

Decision: E2584, the race check (frees are writes) and the view rules treat a `Reset`, `Release` or
`Drop` of a Fixed or Chunked arena as dropping, in place, a value of every type with drop glue passed to
`New` or `Slice` of that arena, or of every arena where the compiler cannot tell which. A Fixed arena never
calls its parent for bookkeeping; the drops its entries run are ordinary drops, checked by 20.10, 19.3 and
18.3, and an arena that holds only types without drop glue keeps the old promise in full. These details
were settled here:
- The view rules treat `Reset` and `Release` as freeing through every pointer into the arena.
- An allocation still live when the entries finish, or an entry recorded while they run, makes the call
  panic with "arena has live allocations". One made and freed within the entries does not.
- A `Slice` of length zero records no entry.
- A failed `New` or `Slice` panics with "arena: out of memory", listed in 17.1.

Why: the entries are drops the program asked for by storing values with drop glue, so they must meet
every rule a drop meets, in a handler as anywhere else. Goal: Safe, Kernel.

Lands with: the sections named above; roadmap item 209, which extends item 202.

## D83. 5-level paging in a checked kernel or freestanding build (21.5, 21.2, 17.1, 20.5, abi.md)

Question: audit G3. abi.md said a checked build on a machine with 5-level paging was a compile error, but
the compiler cannot see the machine; the runtime panics at startup.

Decision: a checked kernel or freestanding build needs 4-level paging. On a machine with 5-level paging
enabled it panics before `main` with "generation checks need 4-level paging". 21.2 states the general
rule: a requirement on the machine that only run time can see is checked at startup and panics before
`main`. The 21.5 rule that a target which cannot provide a requirement is a compile error stays, since it
concerns targets the compiler knows.

Why: the check sits where the fact is known, and the program never runs unchecked. Goal: Kernel, Safe.

Lands with: the sections named above. No roadmap item: the runtime already panics this way.

## D84. The generation metadata hook (20.5, 21.5, F)

Question: audit G4. The `pluto_gen_metadata` hook and E2588 were stated only in abi.md.

Decision: a checked freestanding or kernel build exports `pluto_gen_metadata` (`//pu:export`), which takes
a byte count and returns page-aligned memory that is never taken back, or 0. It must not allocate. A
checked build without it fails with E2588. Hosted and unchecked builds do not need it. 20.5, 21.5 and F
state this.

Why: an obligation a program must meet belongs in the language specification. Goal: Kernel, Plain.

Lands with: sections 20.5 and 21.5, appendix F. No roadmap item: E2588 is implemented.

## D85. No waiting in a preemption-disabled region (19.3, 19.1, 17.1, abi.md 12.4)

Question: audit G5. `rt.Park` with preemption disabled panics with "blocking with preemption disabled",
and no chapter said so.

Decision: an operation that may park the task, reachable inside a non-migrating region (between
`rt.DisablePreemption` and `rt.EnablePreemption`, while an `rt.Lock` is held, or while maskable interrupts
are disabled), is a compile error. The operations are the waiting operations of 20.10, plus the `Drop` of a
`sync.Scope`. The compiler follows calls as the E2584 analysis does, including inserted drops and arena
entries, and reports a path only where every path to the operation is inside a region. The run-time panic
stays as the backstop and is listed in 17.1 and 19.1. The error has no code yet: E2584 is defined for
interrupt handlers.

Why: a task that parks there holds a processor or a spun-on lock while it waits for something that may
need it; catching this at compile time is the Rust-level answer, and the panic covers what the analysis
cannot see. Goal: Safe, Kernel.

Lands with: the sections named above; roadmap item 210, which extends items 120 and 156.

Changed by: D87 narrows "the compiler follows calls as the E2584 analysis does" to exact paths, and D97 then
defines the calls followed by target set (see Superseded decisions).

## D86. An arena takes back objects of every type (19.3, 19.4, 18.3, 18.9, 12.12, memory-model.md 3.8, 4.1)

Question: audit H1. D82 made a `Reset`, `Release` or `Drop` of an arena a free only through the drop glue
of the types passed to `New` or `Slice`. A plain struct with no drop glue, allocated in an arena and used by
a task while another task resets the arena, was then a race no rule named.

Decision: a `Reset`, `Release` or `Drop` of an arena, the implicit drop at the end of the arena's scope
included, counts as freeing, and so as writing, every object that `New` or `Slice` allocated in that arena,
of every type. A `Release` counts objects of every type in the arena, since the compiler cannot order
objects against marks. The drops the drop entries run count on top. A live `Alloc` allocation makes the call
panic, so it is not freed. This holds for rules 1 to 3 and the type rule of 19.3, for scoped tasks (19.4)
and for the E3313 events of 18.3. 12.12 counts it as a change of a shared object, and memory-model.md 3.8
and 4.1 say the same.

Why: an arena reset reuses the memory of every object at once; whether the type has drop glue does not
change that another task may still read it. The live count and the generation epochs catch some misuse at
run time but do not stand in for the race rules. Goal: Safe.

Lands with: the sections named above; roadmap item 212, which extends items 168 and 209.

## D87. Waits in a non-migrating region: exact paths only (19.3, 19.1, 17.1, 20.10)

Question: audit H2. D85 made a wait reachable inside a non-migrating region a compile error, following
calls as E2584 does. E2584 is conservative through interfaces, func values and allocators, so a kernel
that calls an operations table or allocates while a lock is held would be rejected wherever one possible
implementation waits.

Decision: the compile error reports only exact paths: the wait is written in the region, or every call on
the way must reach a wait whichever function runs there. Direct calls, calls of methods of known types,
deferred calls, hooks and function literals called where they are written are exact. A call through an
interface or a func value where only some candidates wait, and an allocation or drop where only some
installed allocators wait, compile; the run-time panic "blocking with preemption disabled" catches them.
E2584 stays conservative: a wait in a handler has no safe run-time outcome, since the processor stops,
while a wait in a task's region fails loudly at the panic.

Why: rejecting ordinary kernel code for a wait that only some implementations make would force
annotations or restructuring on code that is correct. The panic keeps every such wait loud. Goal: Kernel,
Plain.

Lands with: the sections named above; roadmap item 210, whose text changes.

## D88. `rt.Park` tests the interrupt flag in kernel mode (19.3, 19.1, 17.1, abi.md 12.4)

Question: audit H3. The backstop panic tested only the task's preemption count. A wait with interrupts
disabled by `x86.Cli`, and no lock held, would park with interrupts off and could hang the processor in
silence.

Decision: in kernel mode `park` reads RFLAGS on entry, before it takes any runtime lock, and panics with
"blocking with preemption disabled" when IF is clear. The switch inside the scheduler's `Park` happens after
that test, with the runtime's own locks held, and is not tested. `rt.Yield`, `rt.Preempt` and `preemptPark`
switch a task without parking and are not tested, so a handler may still yield through them. Hosted builds
cannot run `x86.Cli` and do not test the flag; other freestanding builds do not test it either, since their
privilege level is not known to the runtime.

Why: with D87, every wait in a non-migrating region of a kernel must fail to compile or panic; this closes
the interrupt-disabled case. The test is one flag read on a path that is about to switch tasks. Goal:
Kernel, Safe.

Lands with: the sections named above; roadmap item 213, which extends item 210.

## D89. The runtime's regions are not checked for waits (18.12, 19.3)

Question: audit H4. The runtime takes its own locks and disables preemption while it parks a task, so the
D85 check would reject `pluto/rt` itself.

Decision: the code of package `pluto/rt` is unsafe code (18.12): its variables and records are outside the
race rules, and its non-migrating regions are outside the check for waits. The check starts only from
regions in other code. A path from a program's region into the runtime still ends at `park` and is
reported.

Why: the runtime implements waiting and must hold its locks across the switch; it is audited code, as
other unsafe code is. Goal: Kernel.

Lands with: 18.12 and 19.3; part of roadmap item 210.

## D90. Owner transfer speaks of views (19.3, 8.8)

Question: audit H5. 19.3 and 8.8 said an owner moves to another task only when no "borrow" is live. The
spec defines views, not borrows, and named no error code.

Decision: the text says "view" (18.3). A view of a local owner used after the move is E3313; one stored
where the compiler cannot follow it is E3314; any other view that may be used while the receiving task can
use the memory is E3501. 11.14 counts a call of a pointer-receiver method as taking a view.

Why: one term for one idea, defined once, and a named error for each case. Goal: Plain.

Lands with: 19.3, 8.8 and 11.14; no compiler change.

## D91. The cost of the generic frame-copy rule (A row 81)

Question: audit H6. Appendix A row 81 listed the type-parameter rule of 18.3 without its cost.

Decision: the row says what the bound means for a program: `return x.Bytes()` for a parameter `x T` is a
compile error (E3001), even when every type argument the program uses returns heap memory, and the fix is
to take `*T` or the interface type.

Why: appendix A must list what a Go programmer would not expect, with its cost. Goal: Go, Plain.

Lands with: appendix A.

## D92. "A value whose type contains a type parameter" (18.3, A, known gaps)

Question: audit H7. The G1 bullets said "a value of a type parameter", which reads as excluding `Box[T]{x}`.

Decision: the wording is "a value whose type contains a type parameter (`x` of type `T`, or `Box[T]{x}`)" in
18.3, appendix A and the skill's known-gaps list.

Why: the rule must cover every generic type, or a wrapper slips past it. Goal: Safe.

Lands with: the sections named above.

## D93. Arenas are named as locks are (19.3, 20.10)

Question: audit H8. D82 said "every arena where the compiler cannot tell which", without saying how the
compiler tells.

Decision: the compiler names an arena by the variable or field it is reached through, and for a pointer by
where the pointer came from, as it names locks. A call on an arena it cannot name, such as
`workers[i].arena` with a variable index, counts as that call on every arena it may be, within the limits
of 18.3 (How precise the frame check is). An arena behind a parameter or a heap pointer is item 196.

Why: one precision rule for locks and arenas, which a programmer can predict. Goal: Safe, Plain.

Lands with: 19.3 and 20.10; part of roadmap item 212.

## D94. Three terms for "region" (1, 11.5, 19.3, 20.9, 20.10, abi.md, memory-model.md, skill)

Question: audit H9. "Region" meant the memory a caller reserves for returned closures, a stretch of code
with preemption or interrupts disabled, and the "region annotations" other languages need.

Decision: the first is always a **closure region** (11.5, abi.md 4.1, the "closure region pointer"). The
second is always a **non-migrating region** or an **interrupt-disabled region**; the 19.3 paragraph is
"Non-migrating and interrupt-disabled regions" and the wait paragraph is "Waiting in a non-migrating
region". "Region annotations" keeps the wording D12 chose, and the glossary of chapter 1
defines all three. Other memory that was called a region (address zero, page-table memory types, `//pu:fpu`
code) is now called memory, a range or a function; "guard region" stays, as it is qualified.

Why: a reader should never have to guess which meaning applies. Goal: Plain.

Lands with: the sections named above; no compiler change.

## D95. Spec examples fixed with audit H (7.9, 7.18, 10.10, 11.6, 11.7, 11.18, 11.24, 11.25, 11.27, 12.17, 15.1)

Question: the spec-example test (item 185) found 12 examples that are wrong by the spec's own rules, and one
(7.9) whose error mark is on a line the rule does not name.

Decision: each example is fixed to obey the rules and to say what its text says, with every error line
marked and its code stated where the spec gives one. No rule changed. The 7.9 duplicate-method example
marks both lines of the clash with a heading comment, and the text says an implementation may report it at
either method. The 11.24 conversion-parsing block is skipped by the test, since no declarations fit every
line.

Why: the spec's examples are part of its contract; a wrong one teaches a wrong rule. Goal: Plain.

Lands with: the sections named above and `tests/testdata/specexamples/examples.txt`.

## D96. A load of a view from shared memory is a crossing (19.2, 19.3, 20.9, A, memory-model.md 4.1, skill)

Question: audit I1. A task could reach heap memory through a pointer it loaded from a package variable,
with no crossing in the 19.2 list. Rule 2 never saw the pointer, rule 1 covered only the variable, so
`p = &*b; task reader(); b = new(Node)` compiled while `reader` used the freed `Node`.

Decision: a pointer, or any value that holds a view (a slice, an interface, a func value, a method value, or
a struct, array or union that holds one), crosses to the task or interrupt handler that loads it from a
package variable (a per-CPU variable included), from the object of a `synced[T]` or from the contents of a
map, or from memory reached through one of those. The load counts in the task's own code and in every
function it calls, atomic loads included. Rule 2 then decides whether the memory is safe. A package
variable includes, for rules 1 and 2, the memory it owns: the object of an `owned[T]` it holds and the
memory of the owners it holds. A package variable whose type holds a `shared[T]` may be used by one task
only (E2010). The sweep of other ways a task could hold a view with no visible crossing:

- a package func value or closure: a closure that captures variables, and a method value, is a view of a
  frame and cannot be stored in a package variable (11.5); a plain function holds no view. A func value
  loaded from one is covered by the new sentence anyway;
- a package interface: covered by the new sentence, since an interface holds a view;
- a per-CPU variable: covered, and said in 20.9; `&v` itself stays bounded by its non-migrating region (E2023);
- a value reached through a `synced` box: covered (the object of a `synced[T]`); a `shared` box cannot
  cross (E2010), and the new E2010 case covers one in a package variable;
- a method value stored in a global: not possible (a frame view, 11.5); a method value received another
  way is covered as a value that holds a view;
- init-time registration (a handler table): the table itself is rule 1 (written only during
  initialization); the pointers in it cross when loaded, so the records they point to are safe only by
  rule 2. Said in rule 1 and under Interrupt handlers.

Why: the race-freedom claim of 19.3 and memory-model.md 4.1 needs every way a task reaches memory to be
checked. A load is how a task gets a pointer it was not given; treating it as a crossing reuses rule 2
with no new error. Goal: Safe.

Lands with: item 214, extending items 168 and 212.

Changed by: audit J. D102 lets a view of memory a package variable or a `synced[T]` object owns cross while
its guard is held, D103 makes static objects safe without the type rule's free condition, and D104 starts a
handler's load crossing at the first install or interrupt enable, as D52 does.

## D97. The wait check follows calls with a single known target (19.3, 19.1, 17.1, 20.10, skill)

Question: audit I2. "Exact path" had two definitions in 19.3: every call on the way must reach a wait
whichever function runs there, and a list of exact calls that included hooks. They disagreed for an
interface call where every candidate waits, and for hooks.

Decision: one definition, by the call's set of targets. The check of waits in a non-migrating region
follows only calls with a single known target, through any number of them: direct calls of a named
function, method calls on a concrete type, deferred calls of those, a function literal called where it is
written, and the drops the compiler inserts for a value of a known type, with the drop entries of an arena
the compiler can name. It reports a wait reached that way when every path to the wait lies inside the
non-migrating region; a wait on only some paths of a followed callee is still reported. It does not follow
a call with more than one possible target (an interface, a func value, a method value, a hook, an arena it
cannot name), nor allocation or frees into the installed allocators. Those rely on the run-time test. A
function literal called where it is written has one target, so it is followed. A hook may be assigned more
than one function, so it is not, unlike E2584, which keeps hooks exact. 19.3 has one worked example of
each kind. This replaces the definition of an exact path in D87; the rest of D87 stands.

Why: a definition by target set has no list to drift out of date, and its result does not depend on what
other code the program converts to an interface. Goal: Plain, Kernel.

Lands with: item 215, extending items 210 and 213.

## D98. `//pu:blocking` functions make the park test at entry (19.3, 19.1, 17.1, 20.3, 20.10, abi.md 12.4, skill)

Question: audit I3. 19.3 claimed that no wait in a non-migrating region hangs the processor in silence,
but a `//pu:blocking` function that spins or halts never reaches `park`, so nothing tested it.

Decision: the compiler inserts the test that `rt.Park` makes (preemption disabled; in kernel mode also
interrupts disabled) at the entry of every function marked `//pu:blocking`, after its prologue. A function
without a body cannot take an inserted test, so the test goes before each call of it, in the caller. The
claim is narrowed to waiting operations: a loop that spins until other code acts, written without
`//pu:blocking` (`for {}`, a poll of a flag under an `rt.Lock`), is not a wait the language can see, and if
it never ends the program has a bug of its own. Code outside Plutonium that waits without the marking
breaks the contract of a function without a body. An interrupt-safe spin lock is not a wait. The
standard library marks only `park`, which already tests, so the inserted test is a repeat there.

Why: every waiting operation the language names then ends at one test, so the claim holds as written for
them. Testing at the call site for body-less functions keeps the test in code the compiler emits. Goal:
Kernel, Safe.

Lands with: item 216, extending items 210 and 213.

## D99. The one check that accepts on doubt (1.2, 21)

Question: audit I4. Principle 1 said every static analysis never accepts on doubt, and chapter 21's
introduction said an implementation never accepts a program because it could not decide. D87 made the
wait check of non-migrating regions accept calls it cannot resolve.

Decision: "never accepts on doubt" holds for every safety analysis. The wait check of non-migrating regions
is named in both places as the one check that accepts on doubt, because its failure is a loud panic at run
time, not a memory error or a data race, and a conservative rule would reject the operations tables and
locked allocation that kernels need. E2584 for interrupt handlers stays conservative.

Why: the principle must be true as stated. Goal: Plain.

Lands with: 1.2 and chapter 21; no compiler change.

## D100. Naming an arena beyond the precision limits and through a move (19.3)

Question: audit I7. Frees are writes named an arena "within the limits of section 18.3" but said nothing
beyond them, nor what happens to the name when the arena moves.

Decision: beyond the limits of 18.3, a call on an arena the compiler cannot name counts as that call on
every arena of the program. The name follows a move: after `b := take(a)`, the arena is named by `b`, and
a call on `b` frees the objects allocated through `a` before the move.

Why: a free that no name covers would let a task keep using memory an arena took back. Goal: Safe.

Lands with: item 217, extending item 212.

## D101. Precision limits are exact (18.3, 21.3)

Question: audit I8. 18.3 said "the limits may be raised; they are never lowered", which let an
implementation accept programs another rejects.

Decision: an implementation uses the stated limits exactly and may not raise them. A later edition of the
language may raise them. 18.3 cites 21.3, since acceptance may not depend on the implementation or its
optimizer.

Why: the same program must be accepted or rejected everywhere. Goal: Plain.

Lands with: 18.3; no compiler change beyond checking the compiler does not exceed the limits (item 184).

## D102. Views of guarded owned memory may cross (19.3, 8.8, A, memory-model.md 4.1, skill)

Question: audit J1. D96 made a load of a view from a package variable a crossing. A package-level
`vec.Vec`, `hash.Map` or `strings.Builder` under one mutex then failed rule 2: `Push` loads the backing
slice from memory the variable owns, the slice is not of a struct, and case 2 needs memory no task writes.
Every access held the lock, and the program was still E3501.

Decision: rule 2 gets a case for memory that a package variable owns, or that the object of a `synced[T]`
owns through owners that hold one another. A view of it may cross when every access to that memory, by
every task and through every view, holds the guard inferred for it (rule 1 for a variable, the lock of the
same object for rule 3; a write holds it for writing), and the view is used only where a guard of that
memory is held, on every path. A use where no guard is held is E3501. Settled with it:

- a lock held around a call counts inside the callee, so library methods called under the lock pass;
- any lock that every write holds is a guard, so a view loaded under one guard may be used under another;
- a view returned to a caller is judged at the caller's uses, so a function that unlocks before it returns
  a view makes the caller's use E3501, and one that returns with the lock held does not;
- a copy whose uses the compiler cannot follow counts as used with no lock held: one stored anywhere but in
  memory the same guard guards, sent on a channel, passed to a task, captured by a function literal not
  called where it is written, or held in a method value;
- an argument of a deferred call is used at the end of the function, so it holds the guard only if the
  guard is still held there;
- a write through the view under `RLock` fails the case.

A view stored in memory under the same guard stays a lifetime question: a later free under the guard is
ordered, and 18.3 and 18.11 decide whether the stored view may still be used.

Why: the most common Go shape for shared state is a container under a mutex. The case reuses the guard
inference of rules 1 and 3 and needs no annotation, and no party can touch the memory while the guard is
held. Exempting library containers by name would be a false friend and would miss user containers built
the same way. Goal: Go, Plain, Safe.

Lands with: item 214, which extends items 168 and 212.

## D103. Static objects are safe to reach without a lock (19.3, 8.8, 20.10, memory-model.md 4.1, skill)

Question: audit J2. `os.Stdout` points to a `File` that the package initializer of `pluto/os` created.
Rule 2 case 2 covered only package variables, and the type rule fails for `File` as soon as any code drops a
`File` once tasks run. So `fmt.Println` from a task was E3501 in any program that closes a file.

Decision: rule 2 case 2 covers a **static object**: an object that a package-level initializer creates as
a composite literal whose address it takes (`var Stdout = &File{fd: 1}`), which lives in static storage
(18.2), and which no code moves, drops or overwrites as a whole once tasks have started. The type rule's
free condition does not apply to it, since nothing frees static storage and its cleanup runs after the
tasks end (15.4): a drop, free, move or overwrite of another object of the type does not count. The field
conditions stay: each field or element never changes once tasks start, or is used only atomically, or
only under one lock of the same object or one package-level lock. Settled with it:

- how the compiler knows an object is static: it is made by a composite literal in a package-level
  initializer, and the compiler names the target of each `take(p)` and `*p = v` by where `p` came from, as
  it names locks; one it cannot name counts as a move of every static object of the type, which makes
  them not static;
- `take` of a static owner, or a whole assignment through a pointer to one, after tasks start makes it not
  static; it then follows the type rule, and a task's load of it is E3501 if the type rule fails;
- assigning a new pointer to the package variable leaves the old object static and usable by a task that
  loaded the old pointer. The assignment is a rule 1 write of the variable, so after tasks start it needs
  the variable's guard or an atomic store. A load after it gets the new object, which needs its own case;
- objects that `new` or an allocator makes in an initializer are not static: an owner holds them, and
  replacing that owner frees them (case 2 and Frees are writes).

Why: printing from a task is the first thing a Go programmer writes. The free condition of the type rule
exists to stop a use after free, and a static object cannot be freed. Making `os.Stdout` a `synced[File]`
would change a Go API and leave every user global of the same shape failing. Goal: Go, Plain.

Lands with: item 214.

## D104. A handler's load of a view crosses from the first install (19.2, 19.3, 20.10, skill)

Question: audit J3. The 19.2 load bullet counted a load "once a task other than the main task may run". 19.3
"The tasks", D52 and memory-model.md 3.1 make a handler a party from the first statement that may install
one or enable interrupts. A handler that ran before the first task could load a pointer to a `Node` that
`main` then freed, and rule 2 never checked it.

Decision: the load counts once a task other than the main task, or an interrupt handler, may run, with the
start of 19.3 "The tasks". 19.3 and 20.10 say that a handler's load crosses whether or not a task has
started.

Why: the race-freedom promise must hold for kernels that install handlers before they start tasks.
Goal: Safe, Kernel.

Lands with: item 214, with item 156.

## D105. Lazy initialization through `sync.Once` (19.3, 19.7, F.5, A, memory-model.md 3.4 and 4.1, skill)

Question: the audit J walk-through. Go code writes a package variable inside `once.Do(f)` and reads it
after `Do` returns with no lock. `Once.Do` calls `f` while it holds the Once's own mutex, so that mutex is
inferred as the variable's guard, and the read after `Do` is E3503, naming a lock the program cannot see.
For version 0.4 the rules stood, with the proposal below recorded under Deferred (history in Superseded).

Decision (2026-11-09, coordinator ruling after an audit of its own, the "Once and Cond" section of the
[rulings](verification/2026-11-09-spec-rulings.md)): data that a Once initializes needs no lock. A package
variable, with the memory it owns, is initialized by a package-level Once `o` when, once tasks have
started, every write of it is made during a call of a function passed to `o.Do`, and every read outside
such a call is made where a call of `o.Do` has returned on every path. A field of an object is initialized
in the same way by a Once field of the same object, named as locks are named. The details:

- A write is inside when it is in `f` or a callee of `f` on the path from the call `Do` makes. The same
  function reached any other way writes outside. A write through a pointer is a write of the data named as
  its target; one with no name is a write of all data of its type. A task `f` starts writes outside. A
  method value's method writes outside.
- A `Do` counts for a read when it is in the reading function, or in a direct callee that calls it on
  every path to its return. A `Do` in another task, or through an interface, func value or method value,
  does not count. A handler cannot call `Do` (E2584).
- Otherwise the ordinary rules apply: the writes inside hold the Once's lock, so a write outside with no
  lock is E3502 and a read with no returned `Do` is E3503, naming the Once.
- `Do` returns only after some `f` has returned. A panic in `f` stops the program. An `f` that ends its
  task with `rt.ExitTask` leaves the Once not done, and the next `Do` runs its own function: the library
  releases the lock in a deferred call and sets `done` only after `f` returns (item 218).
- Different functions may be passed to one Once; only one returns, and all write inside.
- A Once assigned as a whole, moved, dropped or freed once tasks have started initializes nothing. A copy
  is another Once.
- Views loaded from the data cross: memory the data owns is rule 2 case 2 for a package variable and case
  3 for a `synced[T]` object; rule 3 and the type rule accept a field a Once of the same object
  initializes. Dropping or replacing an owner the data holds, outside `f`, is a write outside.

Why: the Once edge of memory-model.md 3.4 orders the return of `f` before the return of every `Do`, and
the rule uses that edge only where a `Do` has returned in the reading task itself, so it needs no
annotation and no run-time cost. Requiring every write inside a `Do` call of one Once keeps the data
unwritten after the first `Do` returns. A Go-style `ExitTask` that marked the Once done would let a read
follow a `Do` whose `f` left its writes half made, so the Once is left not done instead. Goal: Go, Safe.

Lands with: item 218.

## D106. `sync.Cond.Wait` in the guard inference, and `Cond` as a value (19.3, 19.7, F.5, A, memory-model.md 3.4, skill)

Question: the audit J walk-through, with item 197. `c.Wait()` unlocks `c.L` and locks it again through the
`Locker` interface. The compiler cannot name a lock reached through an interface, so the `Unlock` counts
as unlocking every lock and the `Lock` holds none it can name. Shared data read after `Wait` in the usual
`for cond(...) { c.Wait() }` loop is then read without its guard (E3503). Separately, `sync.NewCond`
returns a raw `*Cond` from `alloc.New` that the caller frees with `alloc.Free`. For version 0.4 the rules
stood, with the proposal below recorded under Deferred (history in Superseded).

Decision (2026-11-09, coordinator ruling after an audit of its own, the "Once and Cond" section of the
rulings):

- `sync.Cond` is a value whose zero value is ready, with no `L` field and no `NewCond`. `c.Wait(l Locker)`
  unlocks `l`, parks, and locks `l` again; `Signal` and `Broadcast` keep their forms and need no lock
  (F.5, item 197).
- The guard inference counts `c.Wait(l)` as `l.Unlock()` then `l.Lock()` when it names the lock: `&mu`,
  `&s.mu`, or a pointer named by where it came from. Otherwise, as for a `Locker` in a variable,
  `rw.RLocker()`, or a `Wait` through an interface or func value, it counts as unlocking every lock and
  leaves none held, and E3503 for a later read names the `Wait`.
- Rule 2 case 4 needs the guard held with no release from a view's load to its use. A view kept across
  `Wait`, or across `Unlock` and `Lock`, is E3501.

Why: the proposal named the lock through `c.L` as `NewCond(&mu)` set it, with `c.L` never assigned again.
That form keeps a view of the mutex inside the `Cond`. For a `Cond` beside its mutex in a struct, the view
points into the object that holds it, and it would have to be followed through every move and free of
that object; a `Cond` from `alloc.New` also needs a manual free. Passing the lock to `Wait` names it at the
call, stores no view, and leaves nothing to free, so every form is the usual one. The tightened case 4
closes a hole the Cond attacks found: a view loaded under a lock, kept across a release and a new `Lock`,
passed the old wording ("used only where a guard of that memory is held"), though another task could free
its memory in between. Goal: Go, Safe, Plain.

Lands with: item 219, with item 197. (D108 replaced the fallback for a lock the compiler cannot name:
such a `Wait` is now a compile error.)

## D107. Placement of the audit items and the record fixes (roadmap, decisions)

Question: audit J, records R2 to R4. Roadmap items 198 to 210, 212, 213, 215 and 216 had no phase in "Order
of work"; phase 0 named decisions "D42 to D69". D87 narrowed D85, and D70 replaced D45's panic message,
with no note in the older entries.

Decision: arena and view soundness (198 to 202, 204, 206, 208, 209, 211, 212, 214, 217) go in phase 1. The
wait check (210, 213, 215, 216) goes in phase 3 with the kernel checks: a wait in a non-migrating region is
a liveness failure that the run-time test already makes loud, not a memory error. The deadlock report (205)
and the missing codes (207) go in phase 2 with the diagnostics. The plain arena counter (203) goes in phase
4 with the optimizations. The open decisions (218, 219) go in phase 0. Phase 0 names decisions "D42
onward". D45 and D85 get notes, and Superseded records both.

Why: every planned item needs a place in the order, and every older decision must point to what changed
it. Goal: Plain.

Lands with: roadmap.md, decisions.md and the completion plan; no compiler change.

## D108. A task releases only a lock it holds (19.3, 19.7, 18.12, 17.1, 20.10, F.5, A, memory-model.md 4.1, skill)

Question: the open finding of the "Once and Cond" section of the rulings. An `Unlock`, `RUnlock` or
`c.Wait(l)` of a lock the task does not hold releases another task's lock with no error. `Mutex.Unlock`
checks only that the mutex is locked, not by whom, and the guard inference counts locks per task, so it
does not see the release. A third task then enters the critical section while the first is still in it:
a data race in safe code. The old text also said that unlocking a lock the compiler cannot name counts as
unlocking every lock, which makes the guard inference stricter but does not stop the release.

Decision (2026-11-09, coordinator ruling):

- Every `Unlock`, `RUnlock` and `c.Wait(l)` must release a lock that the guard inference shows the calling
  task holds, on every path to the call, for writing (`Unlock`, `Wait`) or for reading (`RUnlock`).
  Otherwise it is a compile error, with no code yet. This covers `sync.Mutex`, `sync.RWMutex` and
  `rt.Lock`, in tasks and in interrupt handlers.
- A lock the compiler cannot name cannot be shown held, so releasing it is a compile error. This replaces
  "unlocking a lock the compiler cannot name counts as unlocking every lock", and D106's fallback for a
  `Wait` whose lock it cannot name.
- Names for this rule are stable: a package variable that is not a pointer, or a local or parameter whose
  address is not taken and that no function literal assigns, then fields and constant indexes. Copies
  carry names; assignments and moves end them. Tasks and handlers start with nothing held, and calls
  through interfaces, func values and method values carry nothing held. Generic code is checked per
  instantiation. The full list is in 19.3, Releasing a lock.
- Assigning a new value to a lock that another task may also use is E3502, naming the assignment. A
  guard held at each method call would not do: the other task's critical section lasts from its `Lock`
  to its release, past the calls.
- The bodies of the methods of `sync.Mutex`, `sync.RWMutex` and the `RLocker` result, and of
  `sync.Cond.Wait`, are trusted code (18.12); the rule is checked at their calls. `Cond.Wait` makes its
  checks that may panic before it unlocks.
- The run-time panic for unlocking an unlocked mutex stays, listed in 17.1; only unsafe code reaches it.

Why: a static rule costs nothing at run time, needs no annotation, and works for a read lock, which has no
single owner to record. It is what Rust's lock guards enforce, with the inference standing in for the guard
value. An owner check in `Unlock` would cost a store on every `Lock` and a compare on every `Unlock`, would
not cover read locks, and would turn a race into a run-time stop instead of a compile error. Handing a
locked mutex to another task to unlock is rejected; a channel, a buffered channel used as a semaphore, or a
`sync.Scope` does that job. The idiom list in the rulings shows that the usual Go forms compile, and the
ones that do not have a short form that does. Goal: Safe, Go.

Lands with: item 220.

## D109. The race rules as one model (19.2, 19.3, 19.4, 8.8, 12.11, 20.9, 20.10, F.5, A, memory-model.md 3 and 4, skill)

Question: audit K (docs/verification/2026-11-09-spec-audit-k.md). Sections 19.2 and 19.3 had grown by
amendment: D96 and D100 to D108 each added a paragraph, and the terms (party, crossing, guard, the point
where sharing starts, part of a function) were defined in several places with different words. Audit K
found fourteen places where two of those definitions disagree or leave a case open.

Decision (2026-11-10, coordinator ruling on audit K):

- 19.3 is written in three parts: the model, each term defined once (parties, the start of sharing, part
  of the enclosing function, memory and data with the memory an object owns, crossings, accesses, frees
  as writes, locks and names, held sets with summaries, the guard set, views loaded under a lock,
  initialization and lazy initialization, the orders used); the rules, each with its error (rule 1,
  shared data; rule 2, what may cross, with four cases; rule 3, the type rule; releasing a lock; waiting
  in a non-migrating region; interrupt handlers; owner transfer; copying a lock); and a conformance table
  of 72 programs with their results. 19.2 keeps the sendable rule and points to 19.3 for crossings.
- K4: the start of sharing is the first statement that may start a task, install a handler, or enable
  interrupts, and the text says "before the first other party (task or interrupt handler) may run".
- K10: 8.8 says a view is used only while its lock stays held with no release between the load and use.
- K11: a static object with an atomic field meets rule 2 case 3, not case 2; memory-model.md 4.1 lists
  static objects with the other shared objects instead of with unchanged memory.
- K12: `c.Wait(l)` needs `l` held in write mode; F.5 lists `WaitGroup` alone in the bullet that named
  `Once` and `Cond` twice.
- The race check's precision limits are stated as 18.3 states the frame check's: two loads and eight
  steps per place, sixteen origins per value, func values followed four calls deep, recursion to a fixed
  point. They are part of the language, and an implementation uses them exactly (21.3). Today's
  `widenPass` and the silent drop past `maxGroups` break that, so they are compiler work (item 226).
- Where audit K and the checker disagree, the spec takes the coherent model and the roadmap lists the
  difference as compiler work (items 221 to 228). Each difference is listed in the "Race rules rewrite"
  section of the rulings.

Why: an implementer must get one answer for every program, and that needs each term defined once. The
table makes the answers testable. Goal: Plain, Safe.

Lands with: items 221 to 228; the spec text needs no compiler change.

## D110. The memory an object owns (19.3, 20.9, memory-model.md 4.1)

Question: audit K2 and K7. Rule 1 counted the memory a package variable owns as part of it, and D102 let
views of memory a `synced[T]` object owns cross under its guard. A static object or an object whose type
satisfies the type rule could own a `vec.Vec` too, and nothing said whether a view of that storage was
guarded. A per-CPU variable that holds a `vec.Vec` had the same gap.

Decision: the memory an object owns is the object of each `owned[T]` it holds and the memory managed by
each owner value it holds, through owners that hold one another, to any depth; not what pointers, counted
references, maps or channels refer to. A read or write of that memory is a read or write of the datum that
owns it. This holds for package variables, fields of static, `synced[T]` and type-rule objects, and map
contents. For a per-CPU variable, the owned memory is part of each processor's copy and follows 20.9.

Why: one definition covers every kind of object, so the guarded case of rule 2 covers storage owned by
anything that may be shared. Goal: Safe, Plain.

Lands with: items 214 and 228.

## D111. The guard set and valid views (19.3, 19.4, 8.8, memory-model.md 4.1)

Question: audit K5 and K6. The old text inferred "the lock held at every write" per pair of conflicting
accesses. One party that writes `x` under `L` at one write and under `M` at another, with a reader that
holds `L`, passed: each write met only the read, and the read pass found no lock common to both writes and
skipped it. The scoped Sharing form 2 had no rule for a view kept across an `Unlock` and a new `Lock`.

Decision: the guard set G(D) is the set of write-mode locks held at every shared write of D, with "atomic"
as a lock of its own. A shared write with an empty G(D) is E3502. A read needs a lock of G(D) when another
party, or another instance of its own party, may write D. A view loaded under locks of G(D) is valid while
one of them stays held on every path to the use with no ending event: an `Unlock`, `RUnlock` or `Wait` of
it under any name, a call that may release it, or the end of its name. Scoped form 2 uses the same
validity, with E3402.

Why: a per-datum guard set is what "every access holds one lock" means; the pairwise form misses
programs with one writer. Goal: Safe.

Lands with: items 221 and 227.

## D112. Lazy initialization at any depth and in code that is part of the function (19.3, F.5, skill)

Question: audit K9 and K13. D105 counted a `Do` only in the reading function. A helper `cfg()` that calls
`once.Do` on every path was not enough, and a range body or a deferred literal after the `Do` had no
stated answer.

Decision: a `Do` counts for a read when it is made on the reading party, in the reading function or in
code that is part of it (a literal called in place, a deferred literal, a range body whose iterator calls
`yield` on the caller's task), or in a callee whose summary shows a `Do` on every path to its return, at
any depth through calls with a known target. A `Do` through an interface, a func value or a method value
does not count.

Why: summaries already carry facts at any depth; a helper that calls `Do` is the usual Go shape. Goal: Go,
Plain.

Lands with: item 218.

## D113. Summaries record "may release" (19.3)

Question: audit K3. A view kept across a call of a helper that waits on a `Cond` and returns with the lock
held was not ended: the summary showed the lock held before and after.

Decision: a summary records each lock the function may release on some path, to any depth, `c.Wait(l)`
included, even if it takes it again. A view live across such a call is ended, and a use after it is E3501.
A view a call returns counts as loaded at the call under the locks its summary records for it.

Why: another party may run while the lock is released, so the view may point to freed memory. Goal: Safe.

Lands with: item 222.

## D114. A range body is part of its function only on the caller's task (19.3, 12.11)

Question: audit K1. D108 counted every range-over-func body as part of the enclosing function. An
iterator that calls `yield` in a scoped task runs the body on that task, so the body could release the
starter's lock, which D108 forbids for scoped tasks.

Decision: a range body is part of the enclosing function only when the iterator's summary shows that it
calls `yield` on the caller's task. The summary records each func-valued parameter called on another
party (inside a function given to a task starter, or through a callee that does); a copy that cannot be
followed, and an unknown candidate of a func-value or interface iterator, count as such a call. Otherwise
the body is code of that party: it starts with nothing held, a `Do` before the loop does not count in it,
and a `defer` in it is a compile error.

Why: the summary already follows func parameters; one bit more keeps the release rule sound with no
annotation. Ordinary iterators such as `vec.Vec.All` call `yield` in their own loop and keep today's
meaning. Goal: Safe, Go.

Lands with: item 223.

## D115. Locks may not be copied (19.3, F.5, A)

Question: audit K14. Go forbids copying a `sync.Mutex`, `RWMutex`, `Once`, `WaitGroup` or `Cond` after
first use, and `go vet` warns. A copy of a locked mutex is a second lock that is born locked, and a copy of
a done Once initializes nothing. The spec said nothing.

Decision: a copy from a place of a value whose type holds one of these types (and `rt.Lock` outside
`pluto/rt`), directly or through fields and array elements, is a compile error: assignments, arguments,
results, sends, range values, value receivers and conversions. Composite literals, zero values, call
results and owner moves are not copies. A `sync.Scope` is an owner, so the type system already makes a
copy a move, and E3405 forbids moving one that has started tasks.

Why: the compiler cannot tell the first use, so it forbids every copy, as Rust does by not making the
types `Copy`. A false friend becomes a compile error. Goal: Go, Safe.

Lands with: item 225.

## D116. No task switch inside a non-migrating region (19.1, 19.3, 20.9, 20.10, 17.1, abi.md 12.4)

Question: audit L1. `rt.Yield` inside `rt.DisablePreemption` let a task resume on another processor
while it still held a view of the first processor's per-CPU data. Only parking was checked.

Decision: a call that may switch tasks (`rt.Yield`, `rt.Preempt`, or a function whose summary may reach
them) is a compile error inside a non-migrating region, followed through calls with a single known
target as the wait check is. In a region opened only by `x86.Cli` or `x86.SaveFlagsCli`, it is also
followed through interfaces, func values, method values, hooks and allocators, as the handler check is.
`rt.Yield` with a non-zero preemption count panics with "blocking with preemption disabled". The
runtime's own switch path is exempt, and neither call tests the interrupt flag, so a handler can still
yield.

Why: a count-raised region is caught at run time by the count; a Cli-only region raises no count and looks
like a handler body, which may yield, so only the compiler can catch it there. Goal: Safe.

Lands with: item 229.

## D117. A function started by `time.AfterFunc` is a repeated party (19.3, 19.6, F)

Question: audit L2. `Reset` through any handle starts the function again, and a run may overlap one still
going, but the function counted as started once.

Decision: an `AfterFunc` function is repeated wherever the call is. No other library entry starts a
function more than once from one call: tickers send on a channel, and `rt.Go`, `wg.Task` and `s.Task`
start once per call. The function of an `rt.Timer` is unsafe code (it takes an `unsafe.Pointer`), so it is
not a party; What is not checked says so (audit L8).

Why: two runs of one function race on what it writes. Goal: Safe.

Lands with: item 230.

## D118. Calls of `pluto/sync` methods and channel operations are atomic accesses (19.3)

Question: audit L3. Nothing said whether `mu.Lock()` is an access of the memory that holds `mu`, so a
free of that memory while another task locks it was accepted under one reading.

Decision: a call of a method of a `pluto/sync` value, and a send, receive or `close` on a channel, is an
atomic write of that value's memory. It does not conflict with another such call or an atomic access. It
conflicts with a plain write of the value's own memory: a new value, or a free, move or overwrite of what
holds it. A write of other memory of the same datum does not conflict with it. Rule 2 case 3 gets the
condition its struct objects have: the memory is not freed, moved or overwritten while shared.

Why: it makes the E3502 bullet for locks and the case 3 crossing follow from one definition, and keeps
locking a shared mutex legal. Goal: Safe.

Lands with: item 231.

## D119. A range body holds the locks its iterator holds at `yield` (19.3, 12.11)

Question: audit L4. A locked iterator (`mu.Lock(); defer mu.Unlock(); ... yield(p)`) was accepted when
the body was written as a literal, and rejected as a range loop.

Decision: a range body that is part of the enclosing function also holds the locks the iterator's
summary shows held at every call of `yield`, in the same mode, under the caller's names; a lock with no
name in the caller is not added. They count for accesses and views, not for releases. A view passed to
`yield` counts as loaded there under those locks. Summaries gain "the locks held at every call of each
func parameter". A returned `Do` now counts inside a callee, as a held lock does, when every call of it on
the same party follows a returned `Do`, so a literal passed to an iterator gets the Onces a range body
gets. A body still differs from a literal where it can do more: release a lock held at the loop, `defer`
to the end of the function, `break` and `return`.

Why: the two forms are the same code; Go 1.23 iterators are written with the lock around `yield`. The
Once rule is sound by program order and the Once edge. Goal: Go, Plain.

Lands with: item 232.

## D120. Maps kept in place belong to their place (19.3)

Question: audit L5. The contents of all maps of one type were one datum, so two objects each with a mutex
and a map could not guard their maps with their own mutexes.

Decision: a package variable or a struct field of map type is kept in place when every value stored in it
is new (`make`, a map literal or `nil`, written right there) and every other use is an index, `len`,
`delete`, `clear`, `range`, a comparison with `nil`, or an argument for a parameter or receiver that keeps
it in place. Everything else copies the map out, including a copy of a struct value that holds it. The
contents of a map kept in place are part of the datum of its place, per object, so the lock that guards
the place guards them. Other maps stay in the datum of their type, which only a package-level lock can
guard. A summary records, per map parameter and receiver, whether it keeps it in place.

Why: a struct with a mutex and a map is the commonest concurrent Go type. The test is syntactic, so two
implementers agree, and a struct with a mutex cannot be copied anyway (D115). Goal: Go, Safe.

Lands with: item 192.

## D121. The only writer may keep views without a lock (19.3)

Question: audit L6. The guard set let the only writer read without a lock, but rule 2 case 4 asked every
access to hold one, and a view was valid only when loaded under a lock.

Decision: case 4 asks a lock of every access that the guard set requires to hold one. A view loaded by the
only writer of D, not a repeated party, from D or memory D owns, is valid at each use with no lock; it
still counts as used with no lock held if it crosses.

Why: no other party writes D, so only the writer's own code can end the view, and chapter 18 checks that.
Goal: Safe, Plain.

Lands with: item 233.

## D122. Smaller fixes of audit L (19.3)

Question: audit L7, L9 to L13.

Decision: a method value's method runs with the caller's held set, like any func value; it still counts as
run outside every `Do` (L7). Locks inside `owned` children cannot guard the root datum, and a row says so
(L9). Rows 5 and 71 are fixed (L10). The map trigger counts memory that the party's own code, or a function
it called on its own party, allocated (L11). Frees are writes promises that no party frees memory another
may use at the same time (L12). `p := &mu` makes `*p` a name of `mu` (L13).

Why: each makes the text say one thing. Goal: Plain.

Lands with: item 234.

## D123. A handler body is a non-migrating region (19.3, 20.9, 20.10)

Question: audit M1. 19.3 said a handler body was interrupt-disabled but not non-migrating, so 20.9 rejected
`ticks++`, `&v` and `runq.Push(t)` in a timer handler, while row 61 accepted them.

Decision: the body of a `//pu:interrupt` handler is a non-migrating region up to the end of its
interrupt-disabled region. A call that may switch tasks ends it, as `x86.Sti` does, and is not an error
there; a call through an interface, func value, method value or hook ends it when any candidate may
switch. The switch check does not start from that region; it does start from regions handler code opens
itself. The wait check does not start from regions in code a handler may reach, since E2584 reports those
waits through every call.

Why: interrupts are off and nothing switches before that call, so the task stays on one processor. It is
the commonest kernel shape. Requiring `rt.DisablePreemption` in handlers was rejected: a cost on every
interrupt, and it would defer the handler's own `rt.Preempt`. Goal: Kernel, Safe.

Lands with: item 235.

## D124. A returned closure carries its captured parameters' names (19.3, 12.11)

Question: audit M2. A method that returns a locking iterator closure, `for _, p := range sc.All()`, gave
the body no locks, since the closure's lock `s.mu` has a capture as its base.

Decision: a captured parameter or receiver that nothing assigns and whose address is not taken is the
same name as the parameter inside the literal. When the result of `x.M(args)` is ranged over or called
directly, the literal's names map to the caller's through `x` and `args`. A result stored first is a func
value and maps nothing. Several returned literals count as candidates.

Why: it is the Go 1.23 shape for a guarded collection, and row 109 promised it. Treating every capture as a
name was rejected as unsound when the capture is assigned. Goal: Go, Safe.

Lands with: item 232.

## D125. Party entries get no returned-`Do` credit (19.3)

Question: audit M3. "Every call of it follows a returned `Do`" held vacuously for `main`, handlers, task
functions and others with no visible call.

Decision: each entry of a party counts as a call with no returned `Do`: `main`, `init`, the functions of
task statements, `s.Task`, `wg.Task` and `rt.Go`, `AfterFunc` functions, interrupt handlers, the panic
handler, `//pu:export` functions, scheduler methods the runtime calls, functions whose value reaches
unsafe code, and each candidate of a func value whose calls are not all known.

Why: the credit is sound only when every real entry passes a `Do` first. Goal: Safe.

Lands with: item 232.

## D126. The map fallback error names the copy (19.3, 21.1)

Question: audit M4. One copy of a map out of its place anywhere sends its contents to the type-wide datum,
and the error at the visibly locked write named nothing that pointed at the copy.

Decision: an E3502 or E3503 about map contents reached through a place that is not kept in place names the
first occurrence of that place (by package path, file, position) that copies it out or stores a value that
is not new, and says that it takes the map out of its place so the place's lock no longer guards it.

Why: ordinary code should not need the rule to find the cause. Making the copy itself the error was rejected:
it breaks maps that a package lock guards, which the rule allows. Goal: Plain.

Lands with: item 192.

## D127. Smaller fixes of audit M (19.3)

Question: audit M low notes and guesses G2, G3, G5 and G7, and row 25.

Decision: when a range body may release a lock, every access of the iterator counts as made without it,
so summaries need no "after `yield`" item (G3). An `owned[T]` box in a variable is no pointer load for
names and guards in place (G2, rows 80 and 81). The Cli-only conservative follow applies where the count
may be zero on some path to the call (G5). The summary records locks held at calls of copies of a func
parameter, and a copy it cannot follow counts as called with nothing held (G7). Row 25 cites Releasing a
lock.

Why: each makes the text say one thing; G3 picks the simpler sound form. Goal: Plain, Safe.

Lands with: items 232 and 235.

## Deferred

None open. D105 (lazy initialization through `sync.Once`) and D106 (`sync.Cond.Wait`) were deferred for
version 0.4 and decided on 2026-11-09.

The entry on Linux file names that are not valid UTF-8 is closed by D36.

## Superseded decisions

### D3: lookup at each payload address's 64 KiB base

The 2026-10-11 D3 wording assumed the existing check could reach every large allocation's
table by masking each data address to its 64 KiB base. The user replaced that assumption
on 2026-10-12 with a shared metadata directory. Contiguous objects larger than one chunk
need the later boundaries for data, so a table at every boundary cannot represent them.
Large allocation tables remain at their own metadata bases; directory lookup replaces
the address-mask lookup. That 2026-10-12 revision retained the fixed-arena exemption and
generation/epoch values. D7 later supersedes the exemption and permits no stale-matching wrap.

### D45: the panic message "arena has live counted objects"

D45 counted only counted objects and named its panic "arena has live counted objects". D70 (audit E3 and
E9) counts every live allocation made through `Alloc` and renames the panic "arena has live allocations".
The rest of D45 stands.

### D85: following calls as the E2584 analysis does

D85 had the wait check follow calls as E2584 does, which is conservative through interfaces, func values
and allocators. D87 (audit H2) narrowed it to exact paths, and D97 (audit I2) replaced D87's definition with
calls that have a single known target. D85's rule that a wait in a non-migrating region is a compile error,
with the run-time panic as backstop, stands.

### D87: the definition of an exact path

D87 called a path exact when the wait was written in the region or every call on the way must reach a wait
whichever function runs there, and listed hooks among the exact calls. D97 (audit I2) replaced that with
one definition by target set: only calls with a single known target are followed, hooks are not, and a
wait on only some paths of a followed callee is reported. D87's choice that the check accepts what it
cannot resolve, and leaves it to the run-time panic, stands.

### D105: lazy initialization through `sync.Once` stays unresolved

For version 0.4, D105 kept the rules: the read after `Do` was E3503. The safe forms were a package-level
initializer (a static object, D103) or a `synced[T]` under a mutex copied out under it. It recorded the
proposal that data written only inside functions passed to `Do` of one Once, and read only where a `Do`
on it has returned on every path, counts as written during that Once's initialization, and deferred it
because a new guard form needed its own audit. The 2026-11-09 ruling adopted it after that audit.

### D106: `sync.Cond.Wait` in the guard inference stays unresolved

For version 0.4, D106 kept the rules: data read after `c.Wait()` was E3503. The safe form of a work queue
was a buffered channel, or a `vec.Vec` under a mutex with a buffered `chan struct{}`. It recorded the
proposal to treat `c.Wait()` as `Unlock` then `Lock` of the lock `c.L` was made from (`sync.NewCond(&mu)`
with a named lock, and `c.L` never assigned again), and deferred it with item 197. The 2026-11-09 ruling
adopted the rule with the lock passed to `Wait` instead of stored in `c.L`.

### D106: a `Wait` whose lock the compiler cannot name

The 2026-11-09 D106 counted such a `Wait` as unlocking every lock, leaving none held, with E3503 for a later
read naming the `Wait`. D108 made it a compile error, since a lock the compiler cannot name cannot be shown
held by the task that releases it.

### D102: a view used only where a guard is held

D102 let a view of guarded owned memory cross when it is used only where a guard is held. D111 (audit K6)
requires a lock of the guard set held at the load to stay held to the use with no ending event, so a view
kept across an `Unlock` and a new `Lock` is ended. D113 adds a call that may release the lock.

### D105: a `Do` in the reading function

The 2026-11-09 D105 counted a call of `o.Do` only in the reading function. D112 (audit K9 and K13) counts
a `Do` in code that is part of that function and in callees that call it on every path, at any depth.

### D108: every range body is part of its function

D108 treated every range-over-func body as part of the enclosing function. D114 (audit K1) does so only
when the iterator calls `yield` on the caller's task.
