# Removing the Go implementation

Status: in progress (2026-10-26). Supersedes the "stage 0 stays" paragraph of
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
3. Delete: `cmd/pluto` (Go), `internal/`, `stdlib.go`, go2pu, the escape
   comparison tools, `internal/pu`, `selfhost/demo` (only go2pu tests use it).
   Rework `tools/bootstrap` (stage 0 from the tag), `tools/difftest`,
   `bench/run.go`, `skill/` generators and `editors/vscode` tests to exec a binary.
   The tools in full: delete `tools/go2pu`, `tools/escapeoracle`,
   `tools/escape2diff`, `tools/escapedigest`, `tools/escapescc` (they read the
   Go compiler's escape analysis) and `tools/pkgcov` (coverage of `internal/`);
   keep `tools/mkunicode` (pure Go, reads the Go toolchain's tables). Done
   ahead of the deletion: `tools/bootstrap` builds stage 0 at `go-cutover` or
   takes `-pluto0`; `tools/difftest` defaults to `pluto` on `$PATH`.
4. Rename `selfhost/` to `compiler/`; update AGENTS.md, README, skill docs,
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
| moves  | 103   | `internal/moves` check (1 knownfail, below) |
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
  rejects the original first; passing x as an argument makes it accepted with
  no E2022 (a race checker gap, below).
- escape `//pu:resultholds` programs (9): the directive is allowed only in the
  standard library, so they have no CLI form.
- API tests with no program form: types2 TestNestingLimit config knobs,
  PointerShaped, IndexesByMethod, RangesByMethod, the TestOrValid Implicits
  info; directive TestTree; the parser AST tests; the audit failed-writer
  test; `internal/build` unit tests; escape2 unit tests; lower SSA tests;
  moves TestEmptyFacts; `cmd/pluto` TestBuildModes and TestGenCheckFlag.
  They go with their packages in phase 3.

Known differences and bugs found:
- `pluto fmt --harness` exists only in the self-hosted compiler; its goldens
  fail on pluto0.
- moves misreads an explicit type argument that is itself an instantiation,
  `vec.New[vec.Vec[int]]()`, as an index expression (E3309), on both
  compilers: `tests/moves/type_argument_of_owner_vec` (knownfail).
- race: a task literal that takes its operand as an argument inside a
  per-CPU region, `task func(x uint64) { ... }(x)`, is accepted with no E2022,
  while the named-function form `task set(x + 1)` is reported.
- The self-hosted compiler never set `types2.DeclPrinter`, so its E3202 fix
  showed `...` in place of the owner type declaration
  (`tests/diag/owners`). Fixed by `selfhost/internal/load/printhook.p`.
