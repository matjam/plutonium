# escape2 piece F: handoff (2026-10-24)

Agent F stops here at the coordinator's request. This note covers what is done, what is left, and the
commands that reproduce it.

## Where the work is

Branch `f-wip`, one commit on main at b6dc2107. It is not on main yet.

Checks that pass:
- go vet and golangci-lint on internal/escape2, 0 issues.
- Every internal/escape2 package's tests.
- The 30,000-case differential, and TestTranslationsUpToDate.

What blocks the push: the whole-repo suite (go test -p 4 ./...) fails only in tools/go2pu. testguard
stopped the binary at 6.4 GB against its 6.3 GB cap, in its TestDifferential (constdemo, O0 and O2). It is
not yet known whether main does the same; run `go test -count=1 ./tools/go2pu` alone on main and on the
branch. If main passes, the branch's translated escape2 (the new filter.p files) is what pushes it over.
After that, rebase, rerun the suite, and push to main.

## D51 type filters: built

The design and what changed while building it are in
[2026-10-24-type-filtered-summaries.md](2026-10-24-type-filtered-summaries.md). The solver rules are in
internal/escape2/solve/rules.go, under "Type filters". Section 18.3 gains the one new bullet. In short:

- Filters on keys and `In` tokens (domain/filter.go). There is one table per run (`Paths.Filters`), and
  `sched/filters.go` renumbers it canonically at the end of the run, so results do not depend on the
  workers.
- Filters come from three places: `OpAssert` to an interface or to a non-pointer concrete type, the
  receiver of each `CallIface` candidate, and the join members' receiver keys (Esc, Store sources, Res,
  Deferred Fn and Args).
- A call through an interface now has its own virtual functions. A value-receiver method of a non-direct
  `T` gets "is T" there.
- Admission (solve/filter.go `atomAdmitsObj`):
  - Exact roots of typed frame objects are checked with an underlying-type test, so safe pointer
    conversions pass (review2/k06).
  - "Is T" for a non-direct `T` passes an object that contains a `T` (fmt's walk calls a field's
    method).
  - A pointer into a box is checked by the type at its path; a place that is not a value of its own is
    admitted only by "implements".
  - A pointer into any other object is always admitted, for unsafe reinterpretation (review2/m20, m22,
    m24).
- Widening: same-key filters merge as unions. Cap cuts keep filters; only a third pass drops them.
- Cache format v7 encodes atoms as text. refsolve mirrors all of this, and its generator covers it.
  30,000 differential cases agree.
- Tests: internal/escape2/filter_test.go, and TestRoundTrip checks that filtered keys are present.

Counts with this commit, on main at b6dc2107:

| Target | Findings |
|---|---|
| selfhost/cmd/pluto | 7 |
| selfhost/demo/escape2demo | 34 |
| Corpus (deferral on) | 852 agree / 57 differ |

Before D51, at the earlier base, pluto had 89 findings and escape2demo 85. The one corpus "old rejects,
new accepts" (diag/counted_error_unwrap) is there without filters too. No E3010.

Pluto's solve steps grew from about 0.77 G to 1.24 G. Assertions to pointer types make no filter, on
purpose: they cost 0.6 G steps and saved no finding.

Known gaps:
- Two local types with the same text get canonical IDs in first-interned order.
- Past 16,383 filters, Intern gives none, which depends on order. Pluto uses about 7,000.
- Unsafe reinterpretation of a variable's root as another type is outside the rule (design doc).

## deriveKey / [fmt]: diagnosed, not fixed

On escape2demo, 27 of the 34 findings end in the `overflowed` literal in
selfhost/internal/escape/walkkey.p:260. The cause:

1. Some walk member calls a `func() bool` value it cannot resolve.
2. The per-signature candidates of that call include deriveKey's `star` literal (walkkey.p:272).
3. star's own Deferred call of `overflowed()` (origin star, site 0) then spreads to every fmt caller as
   `fn inN[...] ge`.
4. At the top, the dcall's function value holds Heap tokens and foreign frame objects (boxes, varargs),
   so `fallback` applies overflowed.
5. Its record is bound to that rest, and its record stores land on the frame objects.

Proposed fix, a rule of the same kind as D51 but for closure records: a function value points at a
closure record. So:

- In `fireDcall`, an Obj token of a frame object that cannot be a record (Var, Param, Temp, Box,
  SliceLit, Varargs, StmtTemp) should not trigger the fallback.
- It should also not be part of the record that `joinFallback` binds (`recOther`).
- The same applies to `fireFRest` and `restNode` (xNoRec).
- Keep ObjClosure, ObjMethodVal, regions, arena windows and box views.
- Mirror it in refsolve's `rest`, `resolve` and groupFallback (refsolve/defer.go, funccall.go).
- Add the rule to rules.go under "Deferred calls" and "Calls of function values".

With that, overflowed is applied with a heap record only, and the class should go. The other 7 findings
on escape2demo are small own-code classes (AtomicPointer.Store, moves.(*fn).block, pu.Box,
compat.grow).

## Repro commands

All heavy runs go through /home/matjam/src/.bigjob/bigjob.sh, with `unset GOROOT` first.

    # build the explain binary
    go test -c -o /tmp/sched-ex.test ./internal/escape2/sched
    # classes on the demo (about 40 s) or pluto (about 2 min)
    cd internal/escape2/sched && ESCAPE2_EXPLAIN=selfhost/demo/escape2demo ESCAPE2_EXPLAIN_CLASSIFY=1 \
      ESCAPE2_EXPLAIN_SITES=1 bigjob.sh /tmp/sched-ex.test -test.run 'TestExplain$' -test.v
    # chains of a class; members of a join entry; deferred origins
    ESCAPE2_EXPLAIN_CHAIN=deriveKey ESCAPE2_EXPLAIN_CHAIN_N=2 ...
    ESCAPE2_EXPLAIN_MEMBERS='pluto/fmt.(*printer).printArg#0' ...
    ESCAPE2_EXPLAIN_ORIGIN=<func id> ESCAPE2_EXPLAIN_IDS=<ids> ...
    # differential, 30,000 cases
    REFSOLVE_CASES=30000 go test -run 'TestDifferential$' ./internal/escape2/refsolve
    # corpus groups: tools/escape2diff with PLUTO_ESCAPE2_DEFER=1 -stats -j 1 -run '<regexp>'
    # translations after changing internal/escape2
    GO2PU_UPDATE=1 go test ./tools/go2pu -run 'TestTranslationsUpToDate$'

## Next

1. The record rule above, with a test: a frame box printed through fmt, where a String method calls a
   func field, is accepted.
2. Re-measure the demo and pluto.
3. Look at the solve-step cost of the join filters, if time matters: pass the filter id with the member
   instead of rewriting every key.
