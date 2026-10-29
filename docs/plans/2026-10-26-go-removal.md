# Removing the Go implementation

Status: done (2026-10-27): phase 3 is `15e6515d`, phase 4 is `9ea1540f`. Supersedes the "stage 0 stays" paragraph of
AGENTS.md "Self-hosted compiler" once phase 4 lands.

The compiler is self-hosted since the `go-cutover` tag. The Go implementation
(`cmd/pluto`, `internal/`, about 160k lines with tests) is frozen but still
provides the commands the self-hosted driver lacks, the test harness, and stage 0
of the bootstrap. This plan removes it and puts the self-hosted compiler at the
centre of the repository.

## Decisions

- **Bootstrap.** A machine without a `pluto` builds stage 0 from the `go-cutover`
  tag (`tools/bootstrap` checks the tag out into a temporary worktree and runs
  `go build` there), then builds `compiler/` with it. When the tagged compiler can
  no longer build the current source, a seed binary is published as a release and
  the bootstrap starts from that instead. Go is then only needed for the test
  harness and tools, never for the compiler.
- **Layout.** `selfhost/` becomes `compiler/` with module `compiler` (imports
  `compiler/internal/...`); the driver is `compiler/cmd/pluto`. It stays in a
  subdirectory, not the root, because the loader takes the nearest `pluto.mod`
  upward and a root module would capture `examples/` and the test programs. The
  rename is the last phase, so that ports in flight do not conflict with it.
- **Tests.** Tests that give a program to the compiler and check its output,
  diagnostics or exit status are kept and run against the self-hosted binary. They
  move to a Go harness under `tests/` that only execs `pluto` (Go is fine for a
  harness; it is not the compiler). Program-in, diagnostic-out unit tests of the Go
  packages (race, escape review corpora, types2 `mustFail`, lower `lowerError`,
  `buildError`) become corpus files. API-level unit tests of Go internals with no
  program form (opt, arch, escape2 domain/eir) go with the code they test; their
  behaviour stays covered by the corpus and the self-build fixpoint.
- **Commands.** Every Go driver command is ported to `selfhost/cmd/pluto` before
  its Go package is deleted: `explain`, `doc`, `vet -unsafe`, `test`, `fmt`, `lsp`.
  `tools/go2pu` translates each Go package first, as it did the compiler; the
  result is then edited by hand.

## Phases

1. Ports (in parallel): explain + doc + vet; test; fmt (printer, format, diff);
   lsp. Each lands with its tests running against the self-hosted binary.
2. Harness: `tests/` drives an external `pluto` for every corpus and CLI test
   (`PLUTO_TEST_COMPILER` everywhere, default the self-hosted build); in-process
   `build.Build` helpers become CLI calls; inline-program error tests become
   corpus entries.
3. (done, `15e6515d`) Delete: `cmd/pluto` (Go), `internal/`, `stdlib.go`, go2pu, the escape
   comparison tools, `internal/pu`, `selfhost/demo` (only go2pu tests use it).
   Rework `tools/bootstrap` (stage 0 from the tag), `tools/difftest`,
   `bench/run.go`, `skill/` generators and `editors/vscode` tests to exec a binary.
   The tools in full: delete `tools/go2pu`, `tools/escapeoracle`,
   `tools/escape2diff`, `tools/escapedigest`, `tools/escapescc` (they read the
   Go compiler's escape analysis) and `tools/pkgcov` (coverage of `internal/`;
   `tools/escapedigest` was later restored as a driver of the `pluto` binary);
   keep `tools/mkunicode` (pure Go, reads the Go toolchain's tables). Done
   ahead of the deletion: `tools/bootstrap` builds stage 0 at `go-cutover` or
   takes `-pluto0`; `tools/difftest` defaults to `pluto` on `$PATH`.
4. (done, `9ea1540f`) Rename `selfhost/` to `compiler/`; update AGENTS.md, README, skill docs,
   .gitignore, .golangci.yml.

## Phase 2 status (2026-10-26)

Phase 2 is done except for `tools/skillcheck`, which is reworked separately.
Everything below runs through an external `pluto` (`PLUTO_TEST_COMPILER`,
default `./pluto`) and passes against both the Go-built and the self-hosted
compiler, except where noted.

Corpus cases in `tests/<group>/`, run by TestCorpus. Each was checked against
its original Go assertion:

| Group  | Cases | Source |
|--------|-------|--------|
| race   | 291   | `internal/race` check, checkKernel (kernel cases add a `//pu:panic` loop) |
| types2 | 369   | `internal/types2` checkSrc, checkWithRT, TestUnionErrors, TestOrValid, TestOrErrors, enum/union/owner two-package tests (`pluto vet`) |
| escape | 603   | `internal/escape` check, review2/3 (80 set `PLUTO_ESCAPE=old`) |
| moves  | 106   | `internal/moves` check (one knownfail fixed, below) |
| lower  | 161   | `internal/lower` lowerError, lowerModeError |
| diag   | 156   | `cmd/pluto/testdata/diag` |

Other tests moved to `tests/`:
- `cli_test.go`, `vetdoc_test.go`, `fmt_test.go`, `plutotest_test.go`: the
  command-line tests of `cmd/pluto` (build, run, test, vet, doc, fmt, explain),
  with their fixtures in `tests/testdata`.
- `programs_test.go`, `goprog_test.go`: the program runners of `internal/build`
  (testdata/run, lib, plutonium, trace, cabi, kernel, examples).
- `escapereview_test.go`: the escape review programs (review2/3/4) with their
  gap and control lists, built with `PLUTO_ESCAPE=new`. Twelve review3
  programs are rejected by the lowering's closure and scope rules (E2594,
  E3100, E3101, E3401) before escape2 runs, so its own verdict on them is not
  visible through pluto; the test requires those codes instead
  (`rejectedBeforeEscape`).
- `audit_test.go`, `directive_test.go`, `parse_test.go`: `pluto vet -unsafe`
  goldens, directive diagnostics and the spec 20.3 audit, parser error cases
  and the repository parse check.
- `dwarf_test.go` (restored after phase 3): `internal/build/dwarf_test.go`,
  the DWARF, call frame information and gdb checks of `docs/spec/debugging.md`,
  on programs built by the binary.

Programs that failed an earlier pass through the CLI were made whole with a
small edit that keeps the check under test (package variables for values
tasks keep, assembly bodies for bodyless functions, a lock-free variant of a
shared counter). Left in Go, with the reason:
- lower goframeclosure (6 cases: address_taken, assigned_again, field,
  method_on_var, range_assigns, var_without_value): the escape check rejects
  the task statement by design, and moving the captured variable out of the
  frame would remove what the test checks.
- lower closurestoredinfreshallocation (accepted half): its helpers return
  closures kept in fresh allocations, which escape rejects in a whole program.
- race percpureviewrejected_task_literal_started_in_a_region: the capture rule
  rejects the original first. Its argument form is now in the corpus as
  `tests/race/percpudeprejected_a_task_literal_that_stores_its_argument`
  (the race checker gap below, now fixed).
- escape `//pu:resultholds` programs (9): the directive is allowed only in the
  standard library, so they have no CLI form.
- API tests with no program form: types2 TestNestingLimit config knobs,
  PointerShaped, IndexesByMethod, RangesByMethod, the TestOrValid Implicits
  info; directive TestTree; the parser AST tests; the audit failed-writer
  test; `internal/build` unit tests; escape2 unit tests; lower SSA tests;
  moves TestEmptyFacts; `cmd/pluto` TestBuildModes and TestGenCheckFlag.
  They go with their packages in phase 3.

Unit tests restored (2026-10-27). The Go packages' API-level tests at
`go-cutover` are `_test.p` files in the matching `compiler/` packages, run by
`pluto test` and by TestCompilerUnits (`tests/compilerunits_test.go`; the heavy
packages need `PLUTO_UNIT_HEAVY=1`). The heavy packages build with
generation checks on since 2026-10-28, sched too since 2026-10-29, when
its test fakes stopped appending to package slices from scratch memory.
Their times are the checked build plus the run on kraken, with another job
in the other slot, and the memory is the peak of either. On 2026-10-29 the
escape analysis rejects the checked test builds of sched, summary, solve
and escape2 in types2 (`conversions.p:89`: "cannot pass &cause to
(*operand).convertibleTo, which keeps it after returning", and `:97`);
escape2's is rejected without the checks too. Their rows are the last
runs that built; the rejected builds peaked at 4.9, 2.8, 3.0 and 5.7 GB:

| Package | Tests | pluto test |
|---------|-------|------------|
| ssa | schedule, disjoint, verify (4) | 3.5 s, 370 MB |
| opt | all 21 | 10 s, 570 MB |
| arch/amd64 | all 27 (3 run GNU `as`) | 16 s, 650 MB |
| escape2/domain | all 37 | 163 s, 1.45 GB |
| escape2/summary | all 8 | 720 s, 5.2 GB (checked build rejected) |
| escape2/solve | 68 (internal package files) | 694 s, 5.5 GB (checked build rejected) |
| escape2/sched | 60 of 61 | 192 s, 5.8 GB (unchecked; checked build rejected) |
| escape2/eir | build, desugar, corpus | 662 s, 5.5 GB |
| escape2 | cache, calls, report, scale and the rest | 931 s, 12 GB (unchecked; its tests do not build now) |
| lsp | all 32 | 410 s, 3.2 GB |
| build | fieldnames, ssatext | 41 min, 9.1 GB |

The `internal/build` program tests (about 190) are Go tests in
`tests/build_*_test.go`, built through the CLI (Config fields map to `pluto
build` flags; library overlays copy `lib` and set `PLUTOROOT`), with shared
helpers in `tests/build_support_test.go`. New test helpers:
`compiler/internal/testrand` (Go's PCG), `compiler/internal/testpanic`,
`compiler/internal/testenv`, `compat.SetenvForTest`.

Left out, with the reason:
- `escape2/solve` external `solve_test` files (arena, counted, counted_old,
  libfalse, measure, review): they import escape2, which imports solve, and
  `pluto test` has no external test packages.
- escape2 oracle_test (needs `lower.LowerFacts`, gone) and sample_test (needs
  `runtime.Stack`); refsolve and its tests (no Plutonium refsolve).
- sched explain_test (a debug aid with no assertions, behind `ESCAPE2_EXPLAIN`).
- lsp TestDefinitionExtractedStdlib (the library is no longer embedded).
- arch/amd64 `testing.AllocsPerRun` checks (no counter in `pluto/testing`).
- `internal/build` main_test.go (Go GC tuning only).

The corpus-sized tests (eir TestBuildCorpus, escape2 TestAdversarial and
TestGoldenMessages, run with `PLUTO_LONG_TESTS=1`) load each program in a
`scratch.NewRegion()` released when its subtest ends: with no GC nothing
else frees a program's load, and without the regions they passed kraken's
24 GB cap (TestBuildCorpus about 250 programs in, TestAdversarial eight).
Built with `-gencheck=false` the regions bound them: TestBuildCorpus runs
the corpus in 200 MB (2026-10-29). Built with generation checks, as
TestCompilerUnits builds them, they are knownfail: each allocation of a
custom allocator is registered (`rt.GenCustomAlloc`) and
`scratch.Region.Release` unregisters none, so TestBuildCorpus passed 20 GB
240 programs in. The fix belongs in `compiler/internal/scratch` (a Region
that ends its allocations as `pluto/arena`'s Chunked does). The escape2
two are not run yet either way: the escape2 test build is rejected (below).
TestGoldenMessages was TestMessageDiff, which also compared the messages
with internal/escape's; that analysis is gone.

Bugs found by the ported tests:
- Fixed: escape2 cache summaries were damaged by UTF-8 replacement in
  `compat.String` (80dfe057); `lower` crashed indexing an array element of a
  value with no address, and panicked comparing interfaces of two types
  (2e700afa, `testdata/run/elemarrayindex.p`, `ifaceeqmixed.p`).
- `pluto/testing` printed test names with fmt, which the escape analysis
  rejects (E3003) in any program with a formatting method that keeps
  receiver data, such as types2; it now writes them directly.
- Open: the test builds of escape2, lsp and build run out of memory with
  generation checks on (first opt group, around copyelim).

Known differences and bugs found:
- `pluto fmt --harness` exists only in the self-hosted compiler; its goldens
  fail on pluto0.
- moves misread an explicit type argument that is itself an instantiation,
  `vec.New[vec.Vec[int]]()`, as an index expression (E3309), on both
  compilers. Fixed in the self-hosted compiler: the moves pass no longer
  walks type expressions, as a type is not evaluated (spec 11.16; E3309 is
  for reads `x[k]`, 11.11). `tests/moves/type_argument_of_owner_vec` passes,
  with `accepted_owner_type_arguments` and
  `rejected_index_a_generic_container_of_owners` beside it.
- With `vv` a `vec.Vec[vec.Vec[int]]`, `w := vv[0]` showed only E3306 at
  the library line, as lowering errors were reported before move errors.
  Fixed in the self-hosted compiler: move errors come first, and lowering's
  E3306 is left out when there are move errors. The user sees E3309 at
  `vv[0]` (`tests/moves/rejected_index_a_vec_of_vecs`).
  `tests/diag/review_q06_fresh_alias_before_store` now also shows the move
  of `p` that E3101 hid.
- race: a task literal that takes its operand as an argument inside a
  per-CPU region, `task func(x uint64) { ... }(x)`, was accepted with no
  E2022, while the named-function form `task set(x + 1)` was reported. Fixed:
  the dependence analysis now reads a literal started as a task with the
  arguments as its parameters, and a store in it that depends on the
  starter's load is a store of the task statement, as for a named function.
  Tests: `tests/race/percpudep*task*`. A deferred literal had the same gap,
  `defer func(y uint64) { ticks = y }(x + 1)` with the region ended before
  the return, and a deferred literal that captured the load was rejected even
  when the region was still held where it ran. Fixed: a deferred literal is
  read at its defer statement with the arguments as its parameters, and its
  store is a store of the deferred call, checked where the function returns,
  as for a named function. Tests: `tests/race/percpudep*deferred*`. The E2022
  of `percpureviewrejected_deferred_literal` is now at the defer statement, not
  at the store in the literal. A task of
  a `sync.Scope` and a literal given to `time.AfterFunc` take no arguments,
  so a load reaches them only by capture, which the checks already catch:
  `tests/race/percpudep*scope_task*` and `tests/race/percpudep*afterfunc*`.
- The self-hosted compiler never set `types2.DeclPrinter`, so its E3202 fix
  showed `...` in place of the owner type declaration
  (`tests/diag/owners`). Fixed by `selfhost/internal/load/printhook.p`.
