# Specification resolution checklist

**Status:** all decisions agreed and the coordinated specification correction reviewed,
2026-10-15. The [resolution record](../verification/2026-10-15-spec-consistency.md)
records dispositions, witnesses and focused checks. The initial compiler inventory is
complete; full compiler conformance remains a separate, unfinished milestone.

**Workflow agreed with the user:** work through this list in order, record each decision
here, then correct the specification in one coordinated pass. Do not implement from
this checklist or change normative text while the decisions are being discussed.
Implementation work follows the corrected specification, cited by section.

**Evidence:** [Specification consistency audit](2026-10-14-spec-consistency-audit.md).
Its C1–C20 identifiers below connect decisions to findings. The audit describes the
reviewed snapshot; this file tracks subsequent decisions. Existing D6 shutdown and D7
arena decisions remain in force except where a later decision expressly supersedes them.

## Ordered status

| Item | Subject | Status |
|---|---|---|
| 1 | Safety guarantee and kernel freedom | Agreed direction; specification corrected and reviewed |
| 2 | Ownership, borrowing and receiver rules | Agreed; specification corrected and reviewed |
| 3 | Shutdown consistency | Agreed; specification corrected and reviewed |
| 4 | Kernel synchronization | Agreed; specification corrected and reviewed |
| 5 | Conflicting language constructs | Agreed; specification corrected and reviewed |
| 6 | Grammar and implementer contracts | Agreed; specification corrected and reviewed |
| 7 | Accurate guarantees and examples | All subpoints agreed; specification corrected and reviewed |
| 8 | Evidence that the language meets its goals | Agreed; evidence methodology recorded; execution pending |
| 9 | Coordinated spec correction and compiler audit | Specification correction reviewed; initial compiler inventory complete |

## 1. Safety guarantee and kernel freedom

**Agreed direction, 2026-10-15:** ordinary typed kernel and application code should
have the same memory and data-race safety contract. Kernel mode alone must not silently
disable necessary protection. Developers should not need function annotations or
knowledge of compiler internals to make ordinary code safe.

- Infer static safety wherever possible, avoiding runtime checks when proof suffices.
- Supply required runtime protection automatically when static proof is incomplete.
- If an environment cannot provide required protection and safety cannot be proved,
  reject the operation with a concrete explanation and a code-level remedy. Routine
  diagnostics must not direct developers to add a safety-disabling annotation.
- Express hardware and manual-memory responsibilities through explicit unsafe
  operations/APIs, such as physical-address pointer construction or manual arena
  backing. Small unsafe implementations may expose safe interfaces by upholding
  their contracts.
- Keep `//pu:nochecks` exceptional, for deliberate reviewed low-level use or measured
  optimization. It is not an ordinary ownership, borrowing or allocation requirement.
- Retain deliberate unchecked execution as an explicit opt-out from the full lifetime
  guarantee. It does not remove the static rules or make invalid accesses correct.
- Checks used during early boot or interrupt execution must respect those environments'
  allocation, waiting and failure constraints. Protection cannot introduce forbidden
  operations or an undeclared allocation contract.

**Implications accepted in the direction:** existing kernel code may need stronger
proof or automatic protection; where neither is available it cannot silently compile
as safe code. Unsafe implementations remain responsible for their stated invariants.
Memory/race safety does not promise freedom from deadlocks, exhaustion or hardware faults.

**Still implementation design, not chosen here:** kernel pointer representation,
metadata placement, check algorithms and optimization strategy. No new directive or
hardware API is selected by this agreement.

**Later text changes:** contents-page goals; sections 1.2–1.4, 18.3, 18.11–18.12,
20.3, 20.5–20.6 and 21.5; relevant ABI and memory-model descriptions. Reconcile the
previous unchecked kernel default explicitly. Preserve the approved safe/manual arena split.

## 2. Ownership, borrowing and receiver rules

**Status:** agreed by the user, 2026-10-15; specification corrected and reviewed. Audit C6 and
the two proof/admission questions.

**Decision:** distinguish a pure view from a composite containing both views and counted
fields. Copies and drops retain/release the counted fields. An owner transferred to
another task must not leave an outstanding alias that permits a conflicting access;
generation checks alone cannot establish this. Require nonconsuming receivers for owner
indexing and `Len` overloads rather than making an ordinary consuming method behave
differently when invoked through indexing syntax.

The alias rule includes borrows obtained through fields and helpers. A borrow whose
last use precedes transfer remains allowed; diagnostics identify the borrow, transfer
and conflicting use. Borrowing receivers use existing pointer-receiver syntax, not
new annotations. A lifetime-generation check alone cannot establish race freedom.

**Why:** preserve visible ownership, predictable cleanup and race safety without new
annotations. The user accepted all three recommendations after their code examples
were explained. This decision does not claim current implementation conformance.

## 3. Shutdown consistency

**Status:** D6 policy and synchronization clarification agreed by the user, 2026-10-15;
specification corrected and reviewed. Audit C1–C2 and the stale stack-report explanation.

**Decision:** preserve main cleanup, natural task/callback drain and reverse package
cleanup. State that task/callback completion and retirement happen before package
cleanup, including visibility across processors. Update stale summaries, the sieve
example and compatibility descriptions. Keep abrupt termination separate.

Completion includes mandatory cleanup and retirement before package cleanup. The
runtime supplies cross-processor visibility without programmer annotations or fences.
Reclaimable runtime resources are released before final debug allocation accounting.
Nonterminating workers can keep normal shutdown waiting; there is no forced cancellation.
Nonreturning kernels do not undergo this normal-exit sequence.

**Why:** deterministic resource cleanup requires both completion and synchronization.
This does not reopen the approved wait-for-tasks policy.

## 4. Kernel synchronization

**Status:** agreed by the user, 2026-10-15; specification corrected and reviewed. Audit C13–C14
and the ordering aspect of C17.

**Decision:** define interrupt accesses consistently with volatile/atomic requirements;
remove universal ordering language that accidentally permits plain shared accesses.
Distinguish permitted interrupt-safe spinning from waits requiring interrupted code to
resume. State interrupt-disable, preemption, nesting and progress obligations.
`HandlerAddr` is an address query and orders no memory accesses.

Permitted spin-lock protocols disable relevant local interrupts before acquisition,
exclude inappropriate preemption while held, release the lock, then restore the prior
state. Every user follows the protocol. Nested exceptions and nonmaskable interrupts
need separate treatment; ordinary interrupt disabling does not exclude them.
Handlers cannot park or depend on interrupted execution resuming to release a lock.

Same-processor interrupt sharing uses the specified volatile/atomic operations or a
correctly protected interrupt-disabled region. Cross-processor sharing requires atomics
or an appropriate lock. Volatile alone provides neither compound-operation atomicity
nor mutual exclusion. Infer/check supported operations without adding routine safety
annotations; opaque assembly implementations retain explicit contracts.

**Why:** kernel authors need precise hardware contracts, not annotations whose meaning
depends on conflicting definitions of waiting. The user accepted the explanation of
interrupted lock holders, shared accesses and HandlerAddr ordering. The coordinated text correction is recorded in the resolution record; implementation
verification remains separate.

## 5. Conflicting language constructs

**Status:** all four resolutions agreed by the user, 2026-10-15, after their examples
and consequences were explained. Specification edits are recorded in the resolution record.

| Finding | Agreed resolution | Reason |
|---|---|---|
| C3: promoted literal fields | Keep section 11.3's detailed allowance and restrictions; reconcile section 7.6. | One predictable construction rule without removing a described facility. |
| C19: enum/union aliases | Allow aliases of existing named types; reserve enum/union declaration syntax for definitions. | An alias preserves identity rather than declaring a new set of variants. |
| C4: inline error structs | Permit inline representations whose referenced storage satisfies lifetime rules. | A struct name alone should not imply a frame allocation or escape. |
| C5: ErrorBuf wrapping | Keep first-`%w` wrapping for ErrorBuf; multiple wrapping belongs to fmt.Errorf. | Preserve a bounded buffer contract without adding unspecified storage. |

Promoted literal keys retain the existing ambiguity, overlapping initialization and
embedded-pointer restrictions. Aliases preserve type identity and variant sets.
Inline error permission does not permit a pointer to expired local storage or waive
escape restrictions for larger frame-backed values. ErrorBuf's first-cause rule must
be explicit for errors.Is/As traversal; multiple-cause APIs retain their counted
storage and lifetime contracts. These decisions do not assert compiler conformance.

## 6. Grammar and implementer contracts

**Status:** all five recommendations agreed by the user, 2026-10-15; specification
corrected and reviewed. Audit C15–C18 and C20; coordinate C17 with item 4.

**Decision:** add generic-method instantiation after general receiver expressions to
both grammar locations. Describe region cleanup for every counted component. Refer
channel reference counting to one rule. Align HandlerAddr invalid-use behavior and
carry the task-stack address-space fallback into the ABI.

Saved method values retain their saved receiver's counted components and release them
when their containing storage is cleaned up. This includes maps, channels and counted
boxes as well as strings; it introduces no hidden heap allocation and preserves the
existing frame/caller-region lifetime rules and pointer-receiver borrowing rules.

Hosted channel counts follow the established plain-before-threading rule, transitioning
to atomic before another thread can run; freestanding counts remain atomic from start.
HandlerAddr requires a compile-time-known valid interrupt handler; unsupported indirect
uses are compile errors rather than runtime panics. For hosted unsized task stacks,
carry the normal at-least-64-MiB virtual reservation and constrained-address-space
fallback, never below 64 KiB, into the ABI. Preserve overflow protection and explicit
stack sizing; virtual reservation is distinct from physical-memory commitment.

**Why:** an independent implementer should derive the same behavior from the language
chapters, grammar and ABI. No compiler behavior selects the winning rule.

## 7. Accurate guarantees and examples

**Status:** reviewed one subpoint at a time, with examples, at the user's request.
All six subpoints agreed on 2026-10-15. Audit C7–C12 and smaller editorial findings.

| Subpoint | Subject | Status |
|---|---|---|
| 7.1 | Allocation visibility | Agreed; specification corrected and reviewed |
| 7.2 | Cleanup responsibility versus guaranteed reclamation | Agreed; specification corrected and reviewed |
| 7.3 | Debug-allocator detection limits | Agreed; specification corrected and reviewed |
| 7.4 | Scheduler capability activation | Agreed; specification corrected and reviewed |
| 7.5 | Optimization equivalence | Agreed; specification corrected and reviewed |
| 7.6 | Observable differences and example corrections | Agreed; specification corrected and reviewed |

**7.1 decision:** “No hidden allocation” means explicit ownership and documented
allocating-operation/API behavior, without an allocation annotation at every call site.
The compiler does not silently promote escaping locals to the heap. `return &local`
remains an error; explicit `new(int)` returns `owned[int]`, whose ownership moves to
the caller on return. A `*int` is a borrowed view. Strings, maps, growing containers
and library calls retain their specified allocation behavior; automatic safety machinery
must honor its specified allocation contract. The user accepted this after the `new`
and owner-return example. Later subpoints are not approved by this acceptance.

**7.2 decision:** promise defined lifetime-management responsibility, not universal
reclamation. Owners release resources when dropped; counted objects are destroyed when
their last strong reference is released, with storage reclamation also respecting the
remaining weak references. Programs avoid or explicitly break strong-reference cycles;
weak back references and checked Upgrade provide the existing non-owning relationship.
Normal shutdown does not collect cycles. Surviving tracked cycle allocations are leaks
for the debug allocator to report. The user agreed after the strong-cycle and weak-parent
examples. Preserve no GC and memory safety without claiming leaks are impossible.

**7.3 decision:** describe debug allocator detection conditions and timing accurately.
Protected-page accesses fault while protection remains; poison and canary inspection
detect qualifying corruption when inspected, not every stale read or write. Finite
quarantine extends the detection window before reuse without permanent raw-pointer
identity. Report tracked surviving allocations after normal cleanup. Ordinary safe
code retains the static/runtime protection contract independently of selecting the
debug allocator; debugging mechanisms supplement it. User agreed after protected-page,
poison-read/write and boundary-canary examples.

**7.4 decision:** scheduler inclusion follows capabilities required by the program,
including its library dependencies, without routine annotations. Sequential allocation
and cleanup alone do not require a scheduler; task starts and scheduler-dependent timer,
callback, waiting and poller operations request support through their APIs. Document
these dependencies and preserve direct alternatives such as sole-thread hosted sleep
when no scheduler is otherwise required. Kernel scheduler bindings remain separate
from ordinary application use. User agreed after sequential work, explicit task,
timer callback and direct-sleep examples.

**7.5 decision:** preserve specified observable behavior, ordering, safety and logical
panic information at every optimization level, allowing variation only where the language
permits it. Deterministic oracles remain exact; nondeterministic programs are checked
against permitted outcomes and invariants. Preserve logical source frames for the same
failure path despite inlining, without requiring equal machine addresses. Document
permitted resource-use differences such as stack consumption/exhaustion without licensing
removed observable cleanup or side effects. Source acceptance remains independent of
optimization level. User agreed after calculation, output-order, map-order, panic-chain
and stack-resource examples.

**7.6 decision:** retain errors.New static call-site identity and disclose its observable
comparison behavior: repeated calls at one site share identity; different sites remain
distinct. Describe approved normal-shutdown task waiting accurately, and give terminating
examples explicit stopping protocols where needed. Mark local-slice escapes invalid or
return arrays by value. Correct ordinary iterator continue to request another iteration,
and evaluation-order examples to respect argument dependencies. Preserve the chosen
semantics and explicitly document intentional compatibility differences. User agreed
after identity comparisons, task completion, array-return and control-flow examples.

**Agreed correction scope:** state debug-allocator detection limits; describe scheduler activation by
all required capabilities; distinguish cleanup responsibility from guaranteed reclamation;
disclose observable errors.New identity and shutdown differences accurately. Define
optimization equivalence in terms of permitted behavior while retaining source-trace
requirements. Fix the invalid local-slice return example, iterator-continue wording,
evaluation dependencies and type-kind summaries.

**Why:** preserve the language's mechanisms while making its promises truthful. The
allocation headline follows the explicit decision in 7.1.

## 8. Evidence that the language meets its goals

**Status:** agreed by the user, 2026-10-15. Audit goal-coverage and performance recommendations.

**Decision:** select representative application and kernel programs demonstrating
annotation-free ordinary code, ownership, allocation control, concurrency and runtime
independence. Define a separate performance comparison methodology with workloads,
hardware, compiler settings, safety configurations and resource measurements.

Keep the fixture set small and fixed; reuse suitable existing tests and benchmarks.
Application examples combine file parsing, owned containers, scoped tasks, errors and
normal cleanup. Kernel-oriented fixtures exercise caller-selected memory, hardware
layouts, interrupt work handoff and safe APIs over necessary unsafe implementations.
Pair valid and invalid rule examples, with runtime-failure examples where appropriate.
These are evidence, not a proof of complete soundness or a requirement to build a full OS.

Performance comparisons verify equivalent work/results, disclose relevant implementation
differences, and record repeated-run variability, allocations and memory as well as time.
Use representative container, string, allocation and concurrency workloads. Define the
method with the coordinated correction; run it after the specification and implementation
audit. Semantic conformance remains mandatory and separate from performance claims.

**Why:** a coherent specification alone does not establish usability or performance.
No numeric target, benchmark exemption or weaker safety mode is approved here.

## 9. Coordinated correction, then implementation conformance

**Status:** execution agreed by the user, 2026-10-15. Coordinated specification edits
and review are complete for this checklist. The bounded initial compiler inventory
is recorded, with gaps in roadmap items 101–103 and existing item 18. Full compiler
conformance is not yet established.

1. Apply all agreed specification changes together, including dependent chapters,
   appendices, ABI, memory model and supporting documents.
2. Review the resulting text against this checklist and the audit. Mark each finding
   resolved with its defining section; keep unresolved questions explicit.
3. Define small accepted, rejected and runtime-failing witnesses for resolved rules.
4. Audit the compiler against that stable text. Record implementation gaps and fix
   code to meet the specification, without revising rules to fit current output.

**Completion criterion:** every checklist decision has a recorded disposition, each
audit finding is reconciled, and the corrected text gives one answer for its witnesses.
Compiler conformance is a separate later result requiring implementation evidence.
