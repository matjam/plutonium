# escape2 piece D (solver, refsolve, domain, summaries): handoff

Status as of 2026-10-24, paused for quota. This note picks up from
docs/plans/2026-10-20-escape-redesign.md and its known-gaps list. It does
not change that plan or docs/decisions.md.

## Done (on main)

- **Arena windows, arena lifetimes and resultholds (D52 item 1)** landed in
  2fbd6cf9:
  - eir/arena.go, OpArenaView and ObjArenaWindow.
  - SinkBoxCopy E3313.
  - The rt.mapMakeWith contract.
  - TestArenas and TestResultHolds.

  In the corpus, lib/chunked, lib/newalloc, lib/mapalloc, lib/vec and
  plutonium/scope agree with the old analysis. diag/arena_default and
  diag/arena_boxed are still rejected. Nothing is left to do here unless a
  new disagreement shows up.
- **Counted-error views (D52 item 2)** landed in 00662d27:
  - The ops: OpBorrow, OpLend and OpRecount.
  - The key flags: Borrowed and Counts, with lending ranks in domain.Covers
    and the antichains.
  - report/counted.go.

  diag/counted_error_unwrap is fixed.
- **Stage-B heap-only (plan section (h))** landed in d865ecc8.
  `go run ./tools/escape2diff -moves` agrees with the old move facts on
  743 of 743 programs. Keep it in the acceptance checks
  (scratchpad/D/moves.sh).

Counts at d865ecc8:

| Check | Result |
|---|---|
| pluto | 80 findings, all E3003, 0 E3010 |
| corpus, deferral on | 850 agree, 56 differ |
| corpus, deferral off | 845 agree, 60 differ |
| differential, 30k cases | both solvers agree |

No corpus group has the new analysis failing.

## The 56 corpus differences (deferral on)

All 56 are old-accepts, new-rejects.

- **52 review4 programs:** the old analysis is wrong. REVIEW4 lists each one
  as dangling.
- **4 are stricter and wrong.** These are the stage-7 gate:
  - lib/errors
  - lib/fmtstringerkeeps
  - examples/24-errors
  - diag/rangefunc

  The first three depend on D51 (fmt keeps every operand), which is with
  piece F. diag/rangefunc is the same cause as a review4 case.

## Pluto's three small classes

| Count | Base |
|---|---|
| 4 | collectCleanupGoexit's closure (lower/deferrec.p:99) |
| 2 | errors.As in build.internal |
| 1 | token.(*File).SetLines |

The errors.As findings are a target in heap memory. G (go2pu) is giving it
a local As target. The big classes are fmt via deriveKey's closure (63) and
typeWriter.typ (10).

## Counted-error gaps (solve/counted_old_test.go countedGaps, nine)

Each gap is a case the old analysis rejects (E3003) that escape2 accepts,
unless noted otherwise. The old rule (internal/escape expr.go `value`,
call.go) is countedHeap: a counted error loaded through a selector, an
index, a star, a receive or a global becomes counted heap. Parameter tokens
keep their provenance. A conversion from a counted box to a counted error
counts the value, and so does a bodyless result of counted-error type.

1. **fresh unwrap counted field**, `h.err` of a fresh synced box: closable
   by counting counted-error loads (see WIP).
2. **counted map**, `m[0]`: same fix.
3. **counted channel**, `<-c`: same fix.
4. **temporary strong field**, `h.p` (synced[E]) converted to error:
   count at the box-to-counted-error conversion.
5. **unknown unwrap counted result**, an extern child(): the default of a
   bodyless function should give CountedHeap for a counted-error result.
   This was started and not applied. The helper would be
   eir.CountedResult(sig, r), used in solve/call.go applyDefault and in
   refsolve/call.go bodylessSummary(cf, sig). Every bodylessSummary call
   site needs the call's Sig: call.go:74 and :92, defer.go:201, :386 and
   :388.
6. **pointer field factory**, box(h *holder) returning `h.p`.
7. **slice pointer field factory**: the same as 6, through a slice.

   Gaps 6 and 7 need a key flag like the old directCount, so that a
   Counts key whose value was counted at a conversion makes the caller's
   Heap tokens counted. The existing Counts flag can't do this. An attempt
   that made every OpRecount turn Heap into CHeap closed 6 of the gaps but
   rejected the static-sentinel cases (`return sentinel` is a recount too).
   It was reverted.
8. **counted target**, As[synced[E]]: strict, not unsound. Low priority.
9. **borrowed any return**: lendView leaves out the holder because of
   D51. It waits on D51.

Suggested shape for 1 to 4: a new EIR op (for example OpCountHeap) that
turns Heap into CHeap and leaves In tokens alone. Emit it where the old
analysis calls countedHeap, and handle it in both solvers and in stage B's
origin as a copy. Draw it in the refsolve generator from gen.extra so that
existing seeds keep their cases.

## WIP (branch d-arena-wip, not on main)

Uncommitted at the pause, now pushed to d-arena-wip:

- In both solvers, a load or reach through a CHeap token gives CHeap
  instead of Heap (solve.go cLoad and cReach, refsolve ops.go load). The
  differential and the solve tests pass with it. On its own it closes no
  gap, because a box local's payload is an ObjBoxView whose loads give
  Heap.
- sched/heaponly.go holdsCounted also excludes counted-error interface
  fields, so that a load of `w.err` keeps its In provenance. The moves
  check, the corpus and pluto have not been rerun with it.

## Standing rules and scripts

Standing rules:
- Run `unset GOROOT` before builds.
- Heavy runs go through /home/matjam/src/.bigjob/bigjob.sh.
- Before a push that touches escape2, regenerate the translation:
  `GO2PU_UPDATE=1 go test -run TestTranslationsUpToDate ./tools/go2pu/`.
  Then run the go2pu suite less TestMixedPipeline in one slot. Don't use
  GO2PU_SLOW.
- Commit as 578676+matjam@users.noreply.github.com.

These scripts are in the session scratchpad, which won't survive the
session; recreate them from the descriptions:

- **D/explain.sh OUT PROGDIR [env]:** TestExplain under bigjob. It takes
  the ESCAPE2_EXPLAIN_* variables, including HEAPONLY=pkg.Type.field. A
  single .p file must be copied into a directory as main.p.
- **D/moves.sh OUT:** `escape2diff -moves -j 1` with PLUTO_ESCAPE2_DEFER=1.
- **D/go2pu.sh:** `go test -count=1 -skip TestMixedPipeline ./tools/go2pu/`
  under bigjob.
- **corpusD2.sh:** escape2diff -stats per corpus group, with deferral on
  and off. review4 comes from worktree agent-aa824fc654870155a's
  review4-programs.
- **diff30k.sh:** `REFSOLVE_CASES=30000`, refsolve TestDifferential.
- **pluto:** `D/explain.sh pluto.txt selfhost/cmd/pluto
  ESCAPE2_EXPLAIN_CLASSIFY=1`.
