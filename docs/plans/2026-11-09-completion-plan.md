# Completion plan

Status: proposed, 2026-11-09. Based on a survey of `main` at `f8d399d4`.

This plan says what is left to finish Plutonium: first the holes and mistakes in
the specification, then the implementation. It follows the project rule that the
specification comes first: a spec question is settled in the text before code
moves.

## Inputs

Four reports, all read-only (nothing was built or run):

- [Spec audit A](../verification/2026-11-09-spec-audit-a.md): chapters 1 to 12 and the appendices. 8 findings.
- [Spec audit C](../verification/2026-11-09-spec-audit-c.md): the parts of chapters 8, 10, 11, 12 and appendix E that audit A skipped. 14 findings.
- [Spec audit B](../verification/2026-11-09-spec-audit-b.md): chapters 13 to 21, appendix F, abi.md, memory-model.md, debugging.md, debug-allocator.md. 22 findings.
- [Implementation survey](../verification/2026-11-09-implementation-survey.md): the roadmap, known gaps, untested diagnostics, test coverage by section, the library and the tools.

## Where things stand

- The compiler is self-hosted, and CI is green. 21 roadmap items are open: 7 large, 10 medium, 4 small.
- The spec audits found 44 problems: 3 high, 14 medium, 27 low. The three high ones are safety holes in safe code.
- The audits did not read all of the spec closely. Still to read: most of chapter 7, 8.6 to 8.10, 10.6 onward, most of chapter 12, 18.1 to 18.4, 18.7, parts of 18.9 to 18.12, chapter 19 line by line, chapter 5, 11.5, and the layout sections of abi.md.
- The compiler accepts some programs the spec rejects: a consuming method used as a method value, the four view-tracking cases of item 138, and one escape case (`review3/o14`).
- 133 of 358 diagnostic codes have no test that expects them (30 are Plutonium's own). The spec's code examples are not compiled by any test.
- "Performance near Rust" has no measurement on the current compiler. Checked reads cost about 20 times unchecked ones in the one recorded microbenchmark.

## Phase 0: close the specification

Nothing in later phases starts on a rule that is still open here.

### 0.1 Decisions that need the user

Five findings change a promise the spec makes to programs today. Each has a recommendation.

| Id | Problem | Recommendation |
|---|---|---|
| A1 | A pointer to a string (or other counted local) declared in a block can be used after the block ends and the string is released. | A view of any local that has cleanup may not be used after the local's scope ends: the rule E3313 already applies to owners. |
| C1 | Any package can make another package's owner by converting from a look-alike owner type, a pointer to one, or an identical union. Dropping it runs the other package's `Drop` on a resource it never issued. | Give those conversions the package limit that plain conversions to an owner already have (E3212). |
| C2 | `switch v := x.(type)` on an owning interface moves `x` into `v` in every clause. In a clause that names an ordinary interface, `v` cannot own it, so the owner leaks or `v` views a cleared variable. | In such a clause nothing moves: `v` lends from `x`. The same for the assertion `x.(I)` with `I` ordinary. |
| B1 | A string, box, map or error made in an arena that is not a local variable can be sent to another task. The arena's `Reset` takes the memory back while the other task still reads it. | Each arena counts its live counted objects; `Reset`, `Release` and `Drop` panic while the count is not zero. |
| B3 | Appendix F.2 and abi 1.3 say an arena may take back its latest allocation. Four other places say arenas take memory back only in bulk. | Bulk only. Fix F.2 and abi 1.3. |

### 0.2 Rulings the coordinator makes

The other 39 findings fill gaps or fix text without breaking a promise. Each ruling is recorded in the decision log with the goal it serves, then the spec text is changed. The medium ones:

- A2: `owner` in a type parameter list is the keyword only after the names.
- A3: say when a moving conversion moves its operand (11.26).
- C3: the comma-ok move is followed only through direct tests of `ok`; on any other path the operand counts as possibly moved.
- C4: an enum is not an integer type; make the conversion rules of 11.24 agree.
- C5: define `type V U` for union and packed types the way 7.12 does for enums.
- B2: a Fixed arena sizes its bookkeeping from its own backing at construction, with no record per allocation from the parent.
- B4: state where the cleaned-place write check runs and what it costs.
- B5: decided as option (b), D69: `%w` keeps the field error it wraps, as in Go, and the wrapper is bounded by the object (16.5, 16.7 rule 4). The copy first proposed here was a false friend.
- B6: the handler data rule becomes a consequence of race rules 1 to 3 (written data must be atomic or locked); the init exemption ends when a handler is installed or interrupts are enabled.
- B7: counted box types are refused at the C boundary, like strings.
- B8: state what `rt.ExitTask` drops mid-expression and inside a `Drop` method.
- B9: state what a deferred call may assume after a panic raised while a lock is held.

The 27 low findings are wording, wrong references and small contradictions; they go in with the same pass.

### 0.3 Second audit pass

Audit the sections listed above as unread, with the same method (each finding checked against the whole text). Chapters 18 and 19 get an attack-style review: try to build a use after free, double drop or data race from accepted safe code. Repeat until a pass finds no high or medium finding.

### 0.4 Keep the spec checked

- Add a test that compiles every code example in the spec: examples marked as errors must fail with the stated code, and the rest must compile.
- Clean up the roadmap: the survey lists 14 stale or contradictory places (section 1.3). Refresh `skill/plutonium/references/known-gaps.md`.
- Bump the spec to version 0.4 when phase 0 lands.

### 0.5 What the later audits created

The second pass ran as audits E to J. Their rulings are decisions D70 to D107 in
[decisions.md](../decisions.md), with records under `docs/verification/`. The roadmap items they created,
and the phase each goes in:

- Phase 1, soundness: items 198, 202, 209, 212 and 217 (arena counts, drop entries, arena frees of every
  type, arena names); 199 (one-word owners lent to an interface); 200 (owner conversions through a defined
  type); 201 (deferred calls in a scoped interval); 204 and 208 (generic code); 206 (frame storage in
  loops); 211 (a use before a move in one call); 214 (loads of views from shared memory, with guarded owned
  memory, static objects and handler loads from audit J).
- Phase 2, tests and diagnostics: items 205 (the deadlock report names a panic in progress) and 207 (codes
  for a stack size out of range and for implementation limits).
- Phase 3, checks and kernel: items 210, 213, 215 and 216 (the wait check in non-migrating regions and its
  run-time tests).
- Phase 4, optimizations: item 203 (a plain arena counter where no other task can reach).
- Open decisions after version 0.4: items 218 (lazy initialization through `sync.Once`) and 219
  (`sync.Cond.Wait` in the guard inference), both from the audit J walk-through.

## Phase 1: soundness in the compiler

Wrong acceptance and wrong code come before everything else (AGENTS.md rule 4).

1. Implement the phase 0 safety rulings: A1, C1, C2, B1, and B10 (count the source before releasing the destination on copy).
2. Item 20(a): a consuming method used as a method value is a compile error (11.9, 18.5).
3. Item 138: the four view-tracking cases 18.3 rejects.
4. `review3/o14`: give `rt.ReleaseValues` and `ZeroValues` their effects in the escape contract table.
5. The false escape errors in heavy unit-test builds of `types2` (`conversions.p:89`, `:97`, `unify.p:735`). The agent on this was stopped on 2026-10-31 with nothing landed. Add the cheap gate check it planned.
6. Audit the `unsupportedf` (E2500) and packed-field backstops in `compiler/internal/lower`: each is either unreachable after the checker, or a missing feature.
7. Item 73: check the rest of lowering against the order of evaluation (11.26), with run and difftest cases.
8. Arena soundness from audits E to I: count every live allocation (item 198), run drop entries before the
   count check (item 202), follow drop entries in the checks (item 209), count an arena free as a free of
   every object of every type (item 212), and name arenas beyond the precision limits and through a move
   (item 217).
9. View and move soundness from audits E to I: items 199, 200, 201, 204, 206, 208 and 211.
10. Item 214: a load of a view from shared memory is a crossing (audit I), with audit J's three parts:
    views of guarded owned memory (D102), static objects (D103) and handler loads from the first install
    (D104).

## Phase 2: test closure

1. A test for each of the 30 untested Plutonium diagnostic codes, and for the sub-forms the item 20 audit lists (E3202, E3204, E3210, E3212, and the seven rules with no test).
2. The 103 untested Go-derived codes: check which are tested by message, which are unreachable in Plutonium (remove them), and add tests for the rest.
3. CI: install gdb, run the heavy unit tests and difftest weekly, restore coverage measurement and the test memory guard that left with the Go implementation.
4. Close items 20 and 75 when phases 1 and 2 leave nothing open under them.

## Phase 3: checks, kernel and performance evidence

1. Item 101 phases 3 and 4: arenas and custom allocators in checked kernels; rejection where no check can run; static elision.
2. Rewrite `genLive` without recursion, so checked builds get proven task stacks (item 18). Then bounds for kernel, freestanding and custom-scheduler builds.
3. Item 85 tuning: snapshots, same-granule checks, extent reuse, lookup hints.
4. Measure against Rust and Go on the benchmark suite, per the [evidence plan](2026-10-15-language-goal-evidence.md). Record the result; set targets from it.

**Stability point.** When phases 0 to 3 are done, the language is specified, sound against its own spec, and tested. That is the point to decide on spec 1.0 and a first release. The decision is the user's.

## Phase 4: compiler speed and tools

1. The remaining single-thread steps of the [speed plan](2026-10-28-compiler-speed.md).
2. Item 63, `sync.Scope` phase 2. The parallel compiler phases need it.
3. Parallel compiler phases, then item 34 (incremental builds).
4. Item 42: DWARF variables, parameters and types. Item 41: alternate signal stacks for worker threads.
5. Items 24, 57, 60, 14 and 5: optimizations and precision limits.

## Phase 5: library

1. `io` and `bufio`; scanning in `fmt`.
2. Item 45: time zones, `Format` and `Parse`.
3. Item 46: `net` over the epoll poller.
4. Decide `sync.NewCond` (it returns a raw pointer today).

## Phase 6: platforms

In dependency order: item 38 (position-independent code), item 37 (PE32+ for UEFI), item 36 (arm64), item 39 (C header generator).

## How the work runs

- Two agents at a time; heavy builds on kraken, one slot each.
- Every agent is briefed from the spec text by section number, with the full gate (tests, skillcheck, three-stage bootstrap, `pluto fmt`).
- Spec work and soundness-sensitive analyses (moves, escape, races, generation checks, backend) go to the most capable model; tests, docs and library ports to a cheaper one.
- CI is checked after each landing.
