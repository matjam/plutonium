# Escape2 port to the self-hosted compiler: handoff (agent G, 2026-10-24)

Status: working notes for whoever continues, not normative. The rules are sections 18.3 and 18.4
of the specification.

## On main

- go2pu translates `internal/escape2` (all packages but refsolve) into `selfhost/internal/escape2`.
  `TestTranslationsUpToDate` checks it, and so does `TestTranslationsLoad`. Generated files with no
  Go source left now fail the test. The hand-written files are `escape2/guard.p` and
  `escape2/sched/workers.p` (see `untranslated` in `tools/go2pu/translate.go`).
- Stage 4 call facts: `escape2.CallFacts`, with `eir.Call.Expr` and `eir.Program.NoCalls`.
  `PLUTO_ESCAPE=new` runs escape2 alone (`CheckCalls`); internal/escape does not run.
- Source restructurings that work around escape2 precision on the translated compiler:
  - `fff3d301`: argEval methods.
  - `8004a236`: capResolver.
  - `b6dc2107`: internalf.
- Since D's `00662d27` and `d865ecc8`, `escape2diff -moves` agrees on 743 of 743 programs, and
  pluto has 80 findings (D's count).

## Closure-capture rule: on branch `g-wip`, not on main

Commit `a146c68b` on `g-wip` implements "reading a captured variable is not a load the limits
count":
- `eir.Instr.Cap` marks the load (set only in `fnb.captured`).
- `domain.StepCap` is a load for everything except MaxLoads; `domain.LimitLoads` counts only
  StepLoad.
- Both solvers handle it, as do the sched overlay, refsolve's NaivePaths and summarytest.
- The section 18.3 sentence is updated.
- Tests: `TestCaptureLoadsUncounted` (refsolve, both solvers) and `TestStepCap` (domain).
  The escape2 package tests pass.

**Why it is not on main:** after rebasing on D's `00662d27` and `d865ecc8`, the 30,000-seed
differential fails at seed 12444. The failing lines are `solve only: res 0: in0.* at .+` and
`res 0: in0.** at .+`. Before the rebase, the same commit passed all 30,000 seeds. So the failure
comes from D's counted-error and lend changes combined with StepCap. A likely suspect is a
`StepLoad`-only branch that D's code added and that should use `domain.IsLoad`.

Repro:
```
REFSOLVE_FROM=12444 REFSOLVE_CASES=1 go test -count=1 -run 'TestDifferential$' ./internal/escape2/refsolve
```

There is also a separate, older disagreement (it reproduces with no StepCap at all):
- Seed 1859 of the generator, when it drew StepCap from the main random source. The minimized
  case: a callee whose store pair has a Deep destination three loads deep (`in1.***` Deep), bound
  to a value that loads its own closure record. solve reports `res 0: in0 at .+` and refsolve does
  not.
- `g-wip` draws StepCap from `g.extra`, so the original seeds stay as they were and this case does
  not come up. It is worth a hand-built refsolve test.

Counts with the rule, before the rebase:
- They did not change: escape2demo 71, pluto 78.
- With `fff3d301` and `8004a236` reverted, escape2demo has 74 findings:
  - The 11 sites from `fff3d301` stay gone, so that restructuring is now redundant.
  - The 3 sites from `8004a236` come back. A recursive closure that calls itself through its own
    variable re-roots its summary on its record at each call, a path that grows by three steps per
    call, and the cut at MaxSteps then covers the variable. So keep `8004a236`.

## Findings left on the translated compiler

Counts from before D's last two commits:
- escape2demo: 71 findings. 61 + 2 are fmt and deriveKey func-literal joins, and 8 are typeWriter.
  All are F's (D51).
- `selfhost/cmd/pluto`: 78 findings. 63 are fmt and deriveKey, 10 are typeWriter (F). 5 have no
  owner yet:
  - 4 from the function literal in `lower.(*fnState).collectCleanupGoexit`, for example
    `lower/deferrec.p:99`.
  - 1 from `token.(*File).SetLines`, at `token/position.p:568` (in `FileSet.Iterate`).
- The coordinator reports 2 findings in `internal/build` that come from an `errors.As` target in
  heap memory. Proposed go2pu fix: make the As target a local (declare a local, call As with its
  address, then assign it back to the heap location), only if behaviour stays identical, with a
  go2pu test. Not started.

## Wiring escape2 into the driver and stage 1

1. Get the translated compiler accepted by `PLUTO_ESCAPE=new`. The check is:
   ```
   PLUTO_ESCAPE=new pluto build -O 0 --brief -o /tmp/p1 selfhost/cmd/pluto
   ```
   It must report no errors. It takes about 4.4 minutes and 1.6 GB.
2. Delete `selfhost/internal/build/escapemode.p`, so that go2pu translates `escapemode.go` and the
   driver links escape2. Regenerate with:
   ```
   GO2PU_UPDATE=1 go test -run TestTranslationsUpToDate ./tools/go2pu
   ```
   Then check that `selfhost/demo/escape2demo` builds under `PLUTO_ESCAPE=new` (escape2's own
   findings are counted in the 71 above).
3. Drop `unbuilt` from the escape2demo entry in `tools/go2pu/go2pu_test.go` and the gate in
   `TestEscape2Demo`. Then run, with the bigjob script in front:
   ```
   GO2PU_ESCAPE2DEMO=1 PLUTO_ESCAPE=new go test -run TestEscape2Demo ./tools/go2pu
   ```
   That is the Go-versus-translated differential of the analysis.
4. Build stage 1 with `PLUTO_ESCAPE=new go run ./tools/bootstrap -stages 1`, and measure the
   translated analysis's time and memory in pluto1 (nothing containing it has been built yet).

## Commands

All heavy commands go through `/home/matjam/src/.bigjob/bigjob.sh`.
- Findings, classified: in `internal/escape2/sched`, build the test binary, then run it with
  `-test.run 'TestExplain$' -test.v` and these settings:
  - `ESCAPE2_EXPLAIN=<abs>/selfhost/demo/escape2demo` (or `selfhost/cmd/pluto`)
  - `ESCAPE2_EXPLAIN_CLASSIFY=1`
  - `ESCAPE2_EXPLAIN_SITES=1`

  Add `ESCAPE2_EXPLAIN_CHAIN=<base> ESCAPE2_EXPLAIN_CHAIN_N=n` for reason chains, and
  `ESCAPE2_EXPLAIN_SUMS='f|g'` or `ESCAPE2_EXPLAIN_FOLLOW='f#e'` for summaries.
- Move-checker parity over the corpus: `go run ./tools/escape2diff -moves -j 4`.
- Verdicts over the corpus: `go run ./tools/escape2diff -j 4`.
- Differential: `REFSOLVE_CASES=30000 go test -run 'TestDifferential$' ./internal/escape2/refsolve`.
