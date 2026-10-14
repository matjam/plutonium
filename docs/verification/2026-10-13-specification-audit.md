# Specification and project-state audit

Status: audit checkpoint; implementation is paused. This report changes no
language rule, public API or compiler behavior. The compiler is not certified
complete. The supplied gap table's completion labels do not establish that the
specification is internally consistent or that every implementation path meets it.

## Current project state

HEAD and locally recorded `origin/main` both equal
`deeadf80738a31e3c579b14d63aa7f492ed2b346`. No fetch was performed for this audit.
The installed compiler remains the separately verified core cleanup landing.
Its recorded aggregate coverage is 48,458/55,010, **88.08943828%**, with every
required package floor. This does not verify the uncommitted item96 work.

The pre-audit snapshot, before this report and D6 were added, has **23 tracked
modified files and 227 untracked path entries**.
The tracked diff is 565 insertions and 124 deletions. The untracked source
inventory contains 226 `.go`, `.p` and `.s` files, 29,842 text lines; 125 of
those are production source files, 19,938 lines. Test-embedded source is counted
once as its containing file's text.

That snapshot has **no working diff in `docs/spec/` or `docs/decisions.md`**. Existing
tracked documentation edits are item96 status changes in the roadmap and
complete-spec plan. The two untracked documents are the custom-owner proposal
and package-memory verification record. The proposal expressly changes no
specification and grants no implementation authority.

Item96 has a passing basic native Report oracle at O0/O2. Its symbolizer and
snapshot integration has not passed, and its full gates have not run. The last
checked source query rejects `fill()` at `Slab.Free`'s unknown/rooted PageProvider
argument to `genFreeLarge`; the propagated whole-function guard surrounds a
later Report. This is an implementation architecture/verification blocker,
not a reason to change the language. E3601 avoids false reports for opaque
custom backing, but leaves valid custom-owner reporting unsupported under the
old exit policy. The user has since approved normal-exit resource cleanup
(D6 in `docs/decisions.md`). Its task and destruction ordering is not yet
specified or implemented. This changes the premise of exit-time package-held
classification; it does not retroactively verify or remove that dirty work.

## Prioritized findings

These are textual findings and unresolved design boundaries. They are not
claims of reproduced compiler exploits. The subsequent implementation survey
did reproduce a safe-looking fixed-arena stale read; its exact evidence is in
the separate implementation report. “Safe” guarantees and limits of a
diagnostic detector require separate treatment.

| Priority | Finding and evidence | Required resolution |
|---|---|---|
| 1: safety boundary | Sections 18.9:620–622 and 18.11:700–705 leave heap-retained fixed-arena memory without static tracking or an epoch, while 18.12:734–738 promises safe views never outlive memory. Hosted epochs may match after 32,768 resets (18.11, 21.5, ABI1.3). | Establish the domain of safe calls. Prefer enforceable lifetime rejection or complete checked lifetime handling; a documented detector exception alone is not proof of safety. No exploit was reproduced. |
| 1: ownership API | Owner ring `Peek() (T,bool)`, `All` and `Values` in 7.17:1466–1472 leave elements in place, but 7.17 permits owner elements and 18.5 forbids copying them. AppendixF iterator parameters also do not accept owner instantiations as written. | Decide explicit borrowed/noncopying specializations or method restrictions. Do not infer an API from contradictory signatures. |
| 1: invariant boundary | Assignment rules 8.4:195–207 omit the package restriction imposed on non-owner-to-owner conversions by 8.11:615 and 18.5:285. Written rules permit `var h pkg.Handle = 7` where an explicit conversion is forbidden. | Decide whether nonzero assignment follows the same construction boundary; distinguish zero/nil creation. No accepted-program exploit was tested. |
| 1: borrowed error lifetime | Section 16.5 suggests wrapping an ErrorBuf/owner-field error to keep it longer, while `%w` retains that borrowed cause through Unwrap (16.6/16.7). Counting a wrapper does not count a borrowed holder. | Distinguish copying only the message from retaining a counted cause. Preserve the original borrowed lifetime unless an explicit operation changes it. |
| 1: representation | Packed raw conversion and storage equality coexist with “reserved bits always zero” (7.14, 8.10, 11.24), without a masking, rejection or preservation rule. | Decide the raw conversion behavior and its round-trip/equality semantics for hardware data. |
| 2: exit semantics | Chapter15 says `os.Exit` ends immediately and waits for no task (15.4:302–304); debug-allocator:192–195 says it settles tasks for about a second. | Keep the chapter's immediate behavior or explicitly decide a debug-mode exception. Do not silently change the chapter to match instrumentation. |
| 2: examples | The 7.4:208–213 example returns a slice of a local array without an error marker; 18.3 forbids it. The 15.4:290–299 Log example prints a destructor for an untouched zero owner; 15.1/18.6 say it is not dropped. | Correct examples to demonstrate the existing lifetime and zero-owner rules. |
| 2: contradictory summaries | 7.11:1010–1013 omits “and empty” for zero/false receives from closed buffered channels. 10.8:321–322 appears to forbid aliases of enums, while 7.12 permits selecting members through an alias. | Align summaries with detailed receive semantics and distinguish aliasing an existing type from an inline enum/union declaration. |
| 2: generic kinds | 8.7:400 calls an unmarked type parameter a value, although legal instantiations include counted strings/maps and borrowed slices. | Separate generic move classification from the instantiated copy/drop/lifetime kind. This is not evidence of missing compiler drop glue. |
| 2: literal acceptance | Section 7.6 forbids promoted fields as struct-literal keys; 11.3 permits them along nonpointer paths and demonstrates that permission. | Choose the acceptance rule and align both sections and examples. |
| 2: optimization failure order | ABI1.3:283–290 permits a hoisted stale check to precede a different runtime panic; 21.3 requires the same panic message and stack at all optimization levels. | Preserve first observable failure or record a deliberate exception in the language before changing the ABI. |
| 2: count semantics | ABI12.6:1336–1340 says channel counts are always atomic; canonical 18.7 says plain before the first hosted task/thread, atomic afterward. | Make supporting documents cite the canonical switch. Stronger atomics alone are not evidence of a race. |
| 2: documentation claims | Debug-allocator says retained package memory “cannot” be dropped, despite replacement dropping old contents; it also promises immediate detection while its own poison/quarantine table permits later detection or undetected reads. | Say there is no automatic program-exit drop and state detector limits. Direct `global.Drop()` needs its own move-rule clarification; replacement is already specified. |
| 2: constructor/race boundary | 8.8's owner-private-memory rule “assumes” no surviving allocation alias, while 19.3 relies on exclusivity. | Specify compiler certification for safe constructors and trusted invariants of unsafe implementations. Existing alias rejection is evidence against treating the wording alone as a waived race guarantee; its completeness was not tested. |

Additional clarification items: scheduler-free sufficient conditions differ
between 1.1 and 19.1; 19.4 contains the obsolete `go s.Task(f)` spelling;
21.3's identical-output optimization rule needs to accommodate permitted
nondeterministic task interleavings and select choices while preserving
deterministic semantics.

Further wording needing alignment: the owner-operator summary in 11.18 must
permit the box dereference guaranteed by 7.19/13.9; ownership conversion
alternatives in 11.24 need their underlying-type compatibility condition;
ABI task thunk `.go<N>` names disagree with debugging's `.task<N>`; Scope
parameter joins must explicitly precede destruction of locals its tasks use,
despite the general parameters-after-locals rule.

Map grouping by type (19.3), scoped-task restrictions on nonlocal/helper
invocations (19.4), and unsafe integer-pointer responsibilities (18.12/20.1)
are explicit conservative or unsafe boundaries. They are not newly discovered
soundness defects. The E3601 custom-owner boundary remains unfinished under
the old policy. Its conservative rejection was never completion evidence.
D6 requires reviewing which reporting mechanisms remain useful after normal
shutdown, rather than solving arbitrary opaque-owner classification.

## History and authorization

The history starts with `15f6d5ba` (March14 v0.1 kernel-safe Go subset), followed
by the October9 v0.2 rewrite (`eca6cdaa`), task/channel changes (`865535ec`) and
ownership model (`856ed766`). The October10 chapter writers integrated v0.3 at
`b05a0b6d`. These are substantial design evolution, not a specification that
has always said the same thing. The decision record contains user choices for
channels, pointer owners, take, owning interfaces and later safety rules.

| Change | Classification and authorization evidence |
|---|---|
| `f2949ce5`: or clauses, 11.27 | Semantic addition; Gandalf records the user's October10 “agreed.” |
| `b461793a`: counts, stacks, scoped owners, initialization and seeds | Mixed contradiction repair and semantic choices. First-task atomic counting reconciles conflicting earlier rules. Address-space fallback below the earlier unconditional 64MiB minimum narrows a resource promise; distinct fallback approval was not found in this bounded record search. Missing evidence is not proof of unauthorized action. |
| `721100db`: D4 sentences | Nil-array slicing clarifies the existing nil-panic rule. No-alias owner filling adds an explicit premise; `44e0ac24` and the safety-gap session show alias-rejection checks were added. Delegated D4 is recorded. |
| `e365a43f`: counted errors.As | D1 resolves the prior example contradiction with borrowed *T targets. Text includes shared and synced; D1 specifically discusses synced under its counted-reference decision. |
| `b01bae59`: shared generation directory/fixed arenas | Heap/custom-allocator coverage is strengthened, but the fixed-arena generation promise is genuinely narrowed. See below. |
| `cc5104d6`: G44 removed | Recorded D2 resolves the open initialization decision; prohibition was already stated. |
| `220bb94b`: finite epoch summaries | Makes an already documented hosted 32,768-epoch limit explicit in previously absolute summaries. This commit did not first introduce that limit. |
| `09d78ca3`: unsafe query list | Adds Sizeof/Alignof/Offsetof to the unsafe-tool list, consistent with the unsafe-import classification. No lifetime exemption. |

**D3 is a recorded reversal of an earlier safety requirement.** Original 18.11
and ABI1.3 require epochs and stale-use panics for fixed Arena too. The October10
Gandalf writer decision7 explicitly required tags/epochs for fixed arenas,
“not documented as a hole.” The October11 D3 decision (`2ab671c3`) instead
chooses an exemption under the recorded authority to resolve five HANDOVER
decisions against the goals, for later user review. October12's explicit
shared-directory choice records retention of the approved exemption.
`b01bae59` then makes fixed Arena tag0 with undefined stale use.

This establishes semantic weakening with recorded delegated authority. It does
not establish a stealth unauthorized edit. The examined records are decision
notes, not the original human-message transcript. The earlier cost rationale
assumed an address-mask ABI that could not reach arbitrary fixed-buffer tables;
the later shared directory changes that premise. Reassess the exemption and
safe-call boundary on current grounds, without claiming either impossibility
or an already reproduced exploit.

**PageProvider correction:** absence of a PageProvider rule from chapter18
does not establish absence of an API precondition. The unchanged public
`lib/pluto/alloc/alloc.p:156` says it supplies zeroed pages, dating to
`6fbb5ae8b`. The dirty roadmap/verification claim that it has no zeroing
contract conflicts with that library documentation. Dirty-provider tests
establish robustness, but do not alone prove that a valid provider violated
a normative dependency contract. Reconcile the authoritative contract before
labeling this a specification-required bug or changing the API.

## Verification and audit coverage

The only test run in this audit was:

```
go test ./internal/diag -run 'TestSpecReferences|TestCatalogComplete|TestCatalogStyle' -count=1
```

It passes (exit0, 0.005s). These checks establish numbering, references and
catalog structure/style, not semantic consistency or compiler conformance.
No implementation test, full suite, lint or new coverage campaign ran.

Completed full read across the two bounded reviewers: contents/version page,
all 21 chapters and appendices A–G, plus the complete ABI, memory-model,
debugging and debug-allocator documents. Previously truncated ranges were
reread in smaller chunks. The foundations lane read 9,575 lines; the other
lane completed chapter15, chapters17–21 and all four supporting documents.
Historical review enumerated the
specification commits from creation and inspected the significant diffs from
the integrated baseline; it did not read every historical line or reconstruct
all original user transcripts.

Source findings are preserved in `/tmp/plutonium-spec-foundations-audit.md`,
`/tmp/plutonium-spec-memory-audit.md` and
`/tmp/plutonium-spec-history-audit.md`. No chapter or implementation edits were
made to resolve them during the audit. D6 records the subsequent user-approved
direction; the implementation-state survey separately distinguishes source
inspection and reproduced behavior.

## Ordered completion gates

1. Review the completed findings and the safety/API decisions above.
   Record user decisions before any semantic chapter or public API change.
2. Resolve textual contradictions against the agreed rules, then recheck
   examples, grammar, catalog references and structural consistency.
3. Specify D6's normal shutdown dependency and lifetime design: task policy,
   resource destruction order, counted releases and cleanup failures, with
   abrupt termination kept distinct. Record the rules before code moves.
4. Preserve and review the old item96 work. Separate reusable demonstrated
   safety fixes from the exit-time classification approach before retiring or
   adapting that approach. Do not require opaque classification solely to
   complete a policy the user has replaced.
5. Verify the agreed native shutdown/drop/task oracles, then all required
   tests, lint, coverage nondecrease and package floors, SSA checks,
   generation-off and tracked-archive checks on one stable chosen snapshot.
6. Complete the reverse compiler-to-spec review and differential campaign.
   Passing current tests alone must not be described as complete conformance.
