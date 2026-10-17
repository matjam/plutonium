# Coordinated specification correction

**Status:** approved correction set applied and independently reviewed, 2026-10-15.
All listed audit conflicts are reconciled. This is not a compiler-conformance certificate. Focused checks below provide limited execution evidence; full compiler conformance remains unestablished.

**Baseline:** `e63a14977a296798fd79bb7617c180d3b26c2edb`.
The [audit](../plans/2026-10-14-spec-consistency-audit.md) describes that snapshot.
The [approved checklist](../plans/2026-10-15-spec-resolution-checklist.md) authorizes
these corrections. Existing D6 shutdown and D7 arena decisions remain in force.

## Resolution and witness register

The witnesses below define expected behavior for the review. They are test designs,
not claims that a compiler probe has passed. Section references are in the language
specification unless a companion document is named. Every row is resolved in the coordinated text and was checked against its approved
decision. This closes the listed audit findings; it is not a formal proof that a
natural-language specification has no remaining defect.

| Finding | Defining sections and dependent text | Small conformance witness |
|---|---|---|
| S1: kernel safety | 1.2–1.4, 18.3, 18.11–18.12, 20.3, 20.5–20.6, 21.5; ABI 1.3 | Save a vector-element view, grow the vector, then use the view. Every ordinary mode must reject the unsafe use or detect it before invalid access. Missing protection cannot silently yield an accepted safe program. Explicit unchecked execution is a separate opt-out. |
| C1: shutdown | 12.12, 14.5, 15.4; appendix A | Main starts a terminating worker and returns. The worker's cleanup finishes before package cleanup. A nonterminating worker prevents normal exit. |
| C2: shutdown ordering | 15.4, 19.1; memory model 3.2/3.9; ABI 12.10 | Use a construction separately accepted under section 19.3 to isolate worker publication before retirement and observation after drain. A low-level memory-order harness may test that boundary separately. An atomic observer alone does not isolate the shutdown edge; the clarification does not relax static race admission. |
| C3: promoted literal keys | 7.6, 11.3 | An unambiguous permitted promoted key initializes its field; ambiguous keys, overlapping initialization and prohibited embedded-pointer traversal are rejected. |
| C4: inline errors | 7.9, 16.2 | A word-inline error struct referring to static storage may return. The same struct referring to an expired local may not. Larger frame-backed errors retain their escape restriction. |
| C5: ErrorBuf | 16.5–16.6 | Format two `%w` operands through ErrorBuf: traversal follows only the first cause. The separately specified counted multi-cause API preserves both causes. |
| C6: composite views | 7.9, 8.7, 18.1 | Copy a composite containing a borrowed pointer and a counted string. Dropping each copy releases its string reference without freeing the borrowed pointee. An owning-interface copy releases payload references at its drop, without duplicating the containing frame temporary's cleanup. |
| C7: debug detection | 18.9; debug-allocator.md | A stale protected-page access faults while protection remains; reading unchanged poison bytes is not promised to fault. Corrupted canaries are detected at their specified inspection point. |
| C8: optimization | 21.3; debugging.md | Deterministic output remains exact at O0/O2. Valid map iteration orders may differ; neither optimization may remove observable cleanup or admit an invalid access. The same panic path retains logical source frames. |
| C9: local slice example | 7.4, 18.3 | Returning a local array by value is valid; returning a slice into that expired array is rejected. |
| C10: reclamation claims | 1.1, 18.7, 18.10 | A strong cycle survives release of external references and appears in tracked final leak accounting. Replacing a back reference with weak storage permits destruction at strong count zero. |
| C11: scheduler activation | 1.1, 19 introduction; ABI 12.9 | Sequential allocation alone needs no scheduler. A reachable scheduler-dependent timer API requests its support even without a source task statement; direct sole-thread sleep retains its documented alternative. |
| C12: error identity | appendix A | Two calls through the same errors.New call site compare equal by its static identity; calls from distinct sites remain distinct. |
| C13: interrupt spinning | 20.3, 20.10 | A handler may use a valid interrupt-disabled, preemption-safe spin protocol. A wait requiring its interrupted lock holder to resume is forbidden. All lock users must obey the protocol. |
| C14: interrupt ordering | memory model 6.1 | Plain shared accesses do not become safe solely because an interrupt occurred between them. Correct interrupt exclusion on one processor and cross-processor synchronization have distinct obligations. |
| C15: saved receiver cleanup | 11.9, 18.7; ABI 4.1 | Save a value-receiver method value whose saved receiver copy contains a map, channel and counted box. Each copied counted component survives release of the original receiver and releases when the saved receiver's containing storage is cleaned up, with no hidden heap promotion. Pointer-receiver method values retain their borrowing restriction. |
| C16: channel counts | 18.7, 19.8; memory model 3.3/3.7; ABI 12.6 | Hosted channel references transition to atomic counting before another thread runs. Freestanding references use atomic counting from the start. |
| C17: HandlerAddr | 20 hardware operations; ABI 10.5 | A compile-time-known valid handler yields its address without a memory barrier. An unsupported indirect handler value is a compile error, not a delayed runtime panic. |
| C18: task stack fallback | 19.1; ABI 12.7 | An unsized hosted task normally reserves at least 64 MiB virtually; a constrained address space may reduce that reservation but not below 64 KiB, preserving overflow protection. |
| C19: enum/union aliases | 7.12–7.13, 10.8, 11.7 | An alias of an existing named enum or union preserves type identity and variant selection. Declaration-only variant syntax cannot directly create an alias target. |
| C20: generic-method grammar | 11.6; appendix E | `factory().Pair[int, string](1, "x")` is grammatically derivable, subject to the normal method and type checks. |
| Owner-transfer aliases | 8.8, 19.3 | A borrow's last use before owner transfer is permitted. A conflicting use after transfer, including a borrow through a field/helper, is rejected unless non-overlap is proved. Generation checks alone do not satisfy this obligation. |
| Owner Index/Len receivers | 10.14, 11.11, 13.6 | Borrowing pointer receivers support owner indexing/length. Consuming owner value receivers cannot silently become nonconsuming overloads. |
| Allocation visibility | 1.1, 18.2–18.3 | Returning `&local` is rejected; returning an explicit `new(int)` owner moves ownership. A documented allocating API requires no per-call allocation annotation. |
| Iterator continue | 12.11 | Ordinary continue requests the next iterator yield; break terminates iteration. |
| Evaluation dependencies | 11.26 | An indexed expression supplying a function argument is evaluated before that invocation, while other evaluations obey the specified partial order. |
| Small type/arena summaries | 7.18, 17.6, 18.1 | Unnamed owner arrays and all four value kinds remain described; the final arena summary includes both Fixed and Chunked. |
| Stack accounting explanation | debug-allocator.md | Normal final accounting follows task retirement and reclaimable runtime cleanup; stack-report policy cannot rely on tasks still retiring at normal exit. |

## Evidence for the language goals

The [evidence plan](../plans/2026-10-15-language-goal-evidence.md) fixes representative
application, kernel, lifetime and runtime cases and a separate performance methodology.
No performance threshold or weaker safety contract was introduced. Combined application
and kernel fixtures and performance measurements remain pending implementation audit.

## Implementation disposition

The bounded source inventories distinguish the following:

| Disposition | Evidence and next action |
|---|---|
| Confirmed policy mismatch | `internal/build/build.go:124` forces lifetime checks off in kernel/freestanding modes and rejects enabling them. Roadmap item 101 tracks proof/protection/rejection and an explicit unchecked opt-out. |
| Confirmed missing capability | `internal/lower/conc.go:635` supplies zero for every proven task-stack bound. Roadmap item 18 remains open; reservation fallback is already implemented. |
| Missing visible validator | `internal/types2/indexing.go:107` and `:232` validate overload shapes without rejecting consuming owner receivers. Whole-pipeline acceptance is not yet demonstrated. Roadmap item 103 requires the positive/negative pair before a bounded fix. |
| Unverified proof boundary | `internal/moves/views.go` distinguishes local-owner views from heap-backed aliases; `internal/race/ownedview.go` proves backing provenance. Their composition for outstanding aliases through a taken owner field remains to test under roadmap item 102. This is not a demonstrated accepted exploit. |
| Implementation support, exact cases partly unverified | General counted-component cleanup exists in `internal/lower/strcount.go`, `regions.go` and `call.go`. Enum aliases use `Unalias`; promoted literals have path/overlap checks. Exact mixed-view native, named union-alias and promoted-literal witness coverage remains to confirm. |
| Existing native receipts inspected | The D6 full plain receipt has terminal passes for retirement/callback cleanup, static saved receivers/regions, channel release, stack fallback and direct sleep. These historical results support the existing mechanisms, not complete conformance to every clarified sentence. |
| Source support with remaining qualification | Shutdown drains through a shared lock before package cleanup; HandlerAddr lowers as a pure address; channel counts switch before threading. Source inspection is not a new memory-order, optimization-motion or before/after counting instruction oracle. |

Existing verification receipts establish only their recorded revision, configuration
and tests. In particular, the retirement oracle uses atomic result fields and does
not independently isolate plain-data visibility from the normal-shutdown edge.
Custom spin-protocol correctness is not established by existing blocking-call tests.

## Focused execution evidence

- Existing type/move tests passed on 2026-10-15:
  `go test ./internal/types2 ./internal/moves -run '^(TestOwnershipKind|TestIndexing|TestIndexingErrors|TestEnumImported|TestUnionImported|TestStaleViews|TestViewsAccepted)$' -count=1`.
  This verifies those existing cases, not the new receiver-negative or owner-field-transfer witnesses.
- Extracted the complete revised section 14.5 sieve into a temporary `main.p` and
  ran the installed baseline compiler with the working-tree library using `pluto run
  -O0` and `-O2`. Both returned status 0 and exactly `2, 3, 5, 7, 11, 13, 17, 19,
  23, 29`, one prime per line. This directly checks the revised stop protocol.
- `go test ./internal/diag -run 'TestSpecReferences|TestCatalogComplete|TestCatalogStyle' -count=1`
  passed (exit 0, 0.005s); log `/tmp/plutonium-spec-resolution-diag.log`.
- Independent memory/type and systems reviewers re-read their raised clauses after
  correction and reported no remaining blockers in that reviewed scope. Corrections
  included owning-interface cleanup timing, dependent panic evaluation, enum/union
  alias wording and cross-processor interrupt participants.
- `git diff --check` passed. Relative links in the new checklist, evidence plan and
  this record resolve. Section numbers and titles were retained.

No compiler/library source was changed in this pass. Full tests, lint and coverage
have not been rerun for this documentation-only correction; no commit or push is
claimed. A structural specification-reference test is not a proof of semantic consistency.
