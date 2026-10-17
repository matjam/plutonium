# Specification consistency and goal coverage audit

**Status:** review findings, not language decisions or implementation instructions. No normative rule
is changed by this report. The specification remains authoritative; conflicts below need resolution
before it can serve as an unambiguous conformance target. Existing approved decisions, particularly
D6 normal shutdown and D7 arena safety, are preserved.

**Snapshot:** `e63a14977a296798fd79bb7617c180d3b26c2edb`, reviewed 2026-10-14.

**Coverage:** complete reading of all 21 chapters, appendices A–G, and the ABI, memory-model,
debugging and debug-allocator companion specifications. Three review lanes covered language/grammar,
memory/types and systems contracts; the coordinating review covered goals and appendix A and reconciled
their findings. Witnesses below are reasoning examples, not executed compiler probes. No normative or
implementation file was changed. The previously passing section-reference test checks numbering and
catalog citations, not semantic consistency.

## Conclusion

The language has a coherent systems-programming premise: familiar syntax, explicit ownership and
borrowing, no garbage collector, counted sharing where needed, and hardware/runtime controls for
kernels. Its specification does not yet state that premise consistently enough to certify it as
self-consistent. Concrete chapter conflicts remain, and the strongest safety claims exceed the
guarantees of its documented unchecked build profile.

This is a specification audit. Compiler acceptance, generated code, coverage percentages and passing
tests cannot resolve contradictory normative rules. No implementation conformance claim is made.

## Goals and their coverage

| Goal in section 1.1 | Supporting specification | Assessment |
|---|---|---|
| Familiar, unsurprising source | Chapters 2–14; appendix A | Broadly supported. Conflicting expression rules and inaccurate exception descriptions need correction. |
| No GC; predictable storage and costs | Sections 18.2–18.11 | Mechanisms are specified. Headlines must include counted sharing, static storage, runtime metadata and explicitly specified allocating operations. Ownership responsibility does not prove eventual reclamation. |
| Safe applications without compiler annotations | Sections 8.7–8.8, 18.3–18.6, 18.12, 19.2–19.4 | Substantial coverage. Composite views and owner-transfer alias obligations need clearer rules. Checked and unchecked guarantees must be distinguished. |
| Kernel safety and hardware control | Sections 7.14–7.17, 17.4, 19.5, chapter 20, ABI and memory model | Facilities are present. Default unchecked lifetime behavior conflicts with the compile-time safety promise; interrupt locking and ordering need consistent definitions. |
| Runtime only when needed | Chapters 18–20, appendix F | Supported by capability-based mechanisms, but the scheduler-free headline omits timers and waiting operations. |
| Performance competitive with Rust | Section 1.1, implementation requirements | A design goal, not yet an assessable acceptance criterion. No workload, configuration or comparison methodology is specified. A semantic audit cannot establish performance. |

## Decisions that must be explicit

### S1. Safety when lifetime checks are disabled

Sections 1.2 and 18.12 promise compile-time safety for kernels and memory safety for ordinary source.
Section 1.3 says only unsafe code can reach unchecked behavior. But section 18.3 permits views through
heap fields to rely on runtime generation checks, and section 21.5 disables those checks by default
in kernel/freestanding builds. The example in section 18.11 saves a pointer into a vector, grows the
vector and dereferences the old pointer. Its caller contains no unsafe operation, yet the text gives
undefined behavior without checks.

Evidence: `01-introduction.md:92–103,136–137`; `18-ownership-and-memory.md:120–123,715–719,774–778`;
`21-implementation-requirements.md:127–136` (all chapter paths under `docs/spec/language/`).

The existence of unchecked mode is already approved. The unresolved conflict is the advertised
guarantee and default profile. Two coherent resolutions are possible:

1. Keep the strong kernel safety goal: ordinary code requires static proof, runtime protection or
   rejection. Disabling necessary protection is an explicitly unsafe build choice.
2. Keep present unchecked defaults and acceptance: explicitly state that those builds do not provide
   the complete lifetime-safety guarantee, even for otherwise ordinary typed source.

Recommendation: preserve the strong guarantee as the normal language contract and retain unchecked
execution as an explicit escape hatch. Whether kernel defaults change requires a recorded user
decision; it must not be slipped into an editorial repair. Do not describe missing protection as
compile-time safety merely because correctly written programs work in both modes.

### S2. Conflicting construct rules

Where two operative clauses disagree, selecting the compiler's current behavior would be circular.
The expression conflicts listed below require choosing the intended rule from the language's goals
and existing decisions before implementation work. They do not justify reopening unrelated features.

## Confirmed corrections

Each entry identifies the narrow correction, not a request to redesign its subject. File/line references
are to the audited snapshot and may move after corrections.

| ID | Conflict and evidence | Recommended resolution |
|---|---|---|
| C1 | Normal shutdown waits for tasks in section 15.4, but section 12.12 says it does not (`12-statements.md:785`), the sieve example abandons blocked tasks (`14-packages.md:203–204`), and appendix A says exit is unchanged (`a-differences-from-go.md:119`). | Propagate approved D6; replace the nonterminating example with one that terminates or explicitly demonstrates abrupt exit. |
| C2 | Section 19.1 says task completion synchronizes only with a scoped join (`19-concurrency.md:46–48`); the memory model lacks the normal-shutdown completion edge required before package destructors run. | State that completed tasks/callback retirement happen before package cleanup. Preserve the absence of an ordinary implicit join during program execution. |
| C3 | Section 7.6 forbids promoted-field literal keys (`07-types.md:353–355`); section 11.3 permits them and gives an example (`11-expressions.md:77–80,102`). | Choose one rule and make prose, grammar/examples and appendix agree. This is an unresolved language choice unless an existing recorded decision settles it. |
| C4 | Section 16.2 permits a word-inline pointer-containing struct as an error (`16-errors.md:38–46`) but then rejects every returned struct (`:51–54`). | Qualify rejection by actual representation and lifetime, consistent with section 7.9; do not reject a safe inline value merely because it is a struct. |
| C5 | Section 16.5 says `ErrorBuf.Errorf` wraps the first `%w` (`16-errors.md:155`); section 16.6 says it and `fmt.Errorf` wrap several (`:174–176`). | Select and document the bounded-buffer behavior explicitly; preserve the separately specified counted `fmt.Errorf` behavior. |
| C6 | Section 8.7 says dropping a view does nothing, but the same section requires counted cleanup for a composite view (`08-properties-of-types-and-values.md:362–407`). Section 18.1 and owning-interface prose repeat the overstatement. | Distinguish pure views from composites containing views and counted fields. Keep mandatory retain/release of counted components. |
| C7 | `debug-allocator.md:16–20` promises immediate deterministic stops for every listed misuse; its canary/quarantine mechanisms explicitly permit undetected reads and delayed detection (`:66–69,136–149`). Section 18.9 repeats the broad claim. | State detection and retention limits accurately. Universal checked-pointer protection belongs to generation/epoch checks, not poison bytes. |
| C8 | Section 21.3 demands identical output at all optimization levels (`21-implementation-requirements.md:93–95`); map order and concurrent scheduling permit different outputs. | Require preservation of permitted observable behavior, with resource-cost qualifications. Retain logical source traces for the same failure path; byte-exact oracles apply to deterministic programs. |
| C9 | Section 7.4 returns a slice of a local array without marking an error (`07-types.md:207–214`), contrary to section 18.3 and the example convention in section 1.3. | Correct the example or mark E3001. |
| C10 | Section 1.1 says every allocation has one owner or arena; sections 18.7/18.10 include counted lifetimes, and strong cycles are explicitly never freed (`18-ownership-and-memory.md:415,676–707`). | Describe explicit lifetime/cleanup responsibility, including counts, rather than guaranteed reclamation of every allocation. Preserve leak reporting and the requirement to break strong cycles. |
| C11 | Section 1.1 promises no scheduler without task starts or channels (`01-introduction.md:33`); chapter 19 also requires absence of waits and timers (`19-concurrency.md:11–13`). | Describe scheduler inclusion by the full set of required capabilities. Timers and waiting library operations are requests too. |
| C12 | Appendix A admits observable `errors.New` call-site identity (`a-differences-from-go.md:93`), then says correct programs cannot distinguish it (`:197–200`). | Keep the specified static identity if intended, but describe the library exception truthfully. Comparing repeated results from one call site is a valid observation. |
| C13 | Interrupt rules allow spin locks with interrupts disabled (`20-system-considerations.md:588–589,620–621`), yet describe spinning locks as `//pu:blocking` and forbidden (`:284,608–610`). | Distinguish permitted spin protocols from waits requiring the interrupted context to resume. Specify the required progress, preemption and nesting assumptions, not merely the name “spin lock”. |
| C14 | The memory model calls plain accesses shared with an interrupt races (`memory-model.md:420–424`), then gives every before/after operation a synchronization edge around the interrupt (`:429–431`), defeating its own unordered-access definition. | Define interrupt agents and permitted visibility/order precisely; do not accidentally bless plain shared accesses through universal happens-before edges. |
| C15 | The ABI's region-drop recipe only releases strings and nested regions (`abi.md:534–544`), although saved receivers can contain other counted references (sections 11.9 and 18.7). | Describe typed cleanup for every counted component, including static regions at normal shutdown. |
| C16 | Sections 18.7/19.8 and memory-model section 3.7 use plain channel counts before threading; ABI section 12.6 requires atomic `lock xadd` in every build (`abi.md:1349–1351`), and memory-model section 3.3 calls them unconditionally atomic. | Refer to the single counting rule, distinguishing an implementation's optional extra atomic cost if allowed. |
| C17 | Chapter 20 and ABI section 10.5 call every x86 function a compiler barrier; the `HandlerAddr` ABI row says it orders nothing (`abi.md:1086`). Its func-value panic also disagrees with compile-time-only restrictions (`20-system-considerations.md:521–522`). | State the address-query exception and invalid-use behavior consistently. |
| C18 | ABI section 12.7 requires at least 64 MiB for unsized task stacks (`abi.md:1384`); section 19.1 permits a fallback down to 64 KiB under address-space limits. | Carry the chapter's fallback into the ABI. |
| C19 | Section 10.8 forbids an enum/union type as an alias target (`10-declarations-and-scope.md:321–322`); sections 7.12/11.7 expressly permit variant selection through an alias (`07-types.md:1071`; `11-expressions.md:348`). | Distinguish declaration-only enum/union syntax from aliasing an existing named type, if that is intended. Confirm the alias policy before editing. |
| C20 | Generic methods are defined, but the primary-expression grammar cannot derive `factory().Pair[int, string](1, "x")` (`11-expressions.md:290–305`; `e-grammar.md:291–300,326–346`). Index accepts one expression; OperandName does not cover that receiver. | Add the explicit-instantiation production for the already-defined generic method feature in both grammar locations. |

Small related corrections: section 7.18 says all owner types are defined types despite unnamed owner
arrays (`07-types.md:1551,1567–1569`); section 18.1 says “three kinds” after defining four; the debug
allocator's stack-report explanation still suggests normal shutdown can leave tasks retiring
(`debug-allocator.md:307–310`); section 17.6's final summary omits Fixed despite including it earlier.
These need precise wording, not new runtime mechanisms.

Two further example/explanation repairs: section 12.11 groups ordinary `continue` with iterator
transfers returning false (`12-statements.md:738–743`), contrary to continuing iteration; section 11.26's
evaluation example says indexing occurs after calls even where that index must supply a call's argument
(`11-expressions.md:1608–1612`). Preserve the controlling iteration and argument-dependency rules.

D6 also needs an explicit compatibility explanation alongside section 1.2's exhaustive
“one exception” claim. The different `task` keyword and `pluto/` imports make many concurrent programs
visibly different, but they do not justify appendix A calling normal termination unchanged. Keep the
approved shutdown behavior and disclose its observable consequences.

## Proof obligations and goal qualifications

**Owner transfer and outstanding aliases.** Section 8.8 assumes owner construction retains no aliases;
section 19.3 relies on exclusivity when an owner crosses tasks. The rule must also cover a later view
borrowed through a heap field before `take` transfers that field to another task. Generation checks
detect stale storage, not simultaneous accesses while storage is still live. The general race rule
already requires rejecting an unproved overlap. This is a missing concrete explanation of that proof
obligation, not evidence that the current compiler accepts an exploit.

**Owner receiver overloads.** Section 10.14 makes an owner value-receiver method consume its receiver;
section 11.11 admits indexing methods as ordinary calls but promises indexing never moves the
container (`10-declarations-and-scope.md:877–882`; `11-expressions.md:625,634`). For an owner with
`func (c C) Index(i int) int`, the overload needs an explicit nonconsuming-receiver requirement or a
specified special invocation rule. Recommend the former to preserve the borrowing contract without
inventing a second way to invoke consuming methods. Apply the same clarification to `Len` overloads.

**Allocation visibility.** “No hidden allocation” should mean no undeclared lifetime promotion or
allocation outside specified operation contracts. String construction, maps, channels, containers,
arena bookkeeping and runtime metadata already have stated allocating behavior. If the intended goal
instead requires a visible allocation expression at every allocating call site, that is a materially
different language decision. This audit does not propose that redesign.

**Performance acceptance.** Keep semantic conformance separate from a performance acceptance record.
Define representative application and kernel workloads, checked/unchecked configurations, hardware,
resource measurements and comparison methodology before claiming the performance goal is met. Do not
invent a numerical requirement or change lifetime guarantees to satisfy a benchmark during this audit.

## Bounded completion plan

1. Record the safety-profile choice and resolve the conflicting construct rules.
2. Apply one coordinated specification correction set, retaining D6/D7 and the other settled decisions.
3. Review every changed rule against its dependent chapters, appendices and implementer documents.
4. Give each resolved rule a small witness of accepted, rejected or runtime-failing behavior. Only then
   use those witnesses to audit the compiler, without changing the rule to accommodate its output.

This avoids another open-ended implementation cycle. A successful correction pass means the listed
conflicts are closed and their dependent statements agree; it does not constitute a formal proof that
no defect remains anywhere in a natural-language specification.

## Findings deliberately not claimed

- Generation/epoch wrap is already forbidden. Fixed/Chunked safe APIs and manual `NewUnsafe` use are
  explicitly separated, with parent lifetime and bookkeeping costs described. Those are not new gaps.
- Strong-reference cycles, abrupt termination and unsafe owner implementations can leak; the debug
  allocator documents these. No garbage collector or guaranteed collection of cycles is proposed.
- Main's local cleanup preceding task drain is the approved policy. Existing task lifetime rules still
  require any storage/allocator used by tasks to outlive them.
- A callback such as scheduler `Wake` can have stricter rules than a general interrupt handler. Its
  allocation prohibition is not itself a contradiction.
- Static storage can remain addressable after its resources are released. Finalized-write protection
  is a resource-state rule, not reclamation of that storage.
- The owner-alias and receiver-overload items above are explicit proof/admission questions. No compiler
  exploit, rejection or acceptance was demonstrated by this specification-only review.
- No throughput, annotation-free expressiveness or complete compiler conformance claim follows from
  the reading pass. Those need separate, rule-based implementation evidence after text conflicts close.
