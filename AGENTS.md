# Plutonium Development Guidelines

## Branch Strategy
- `main` is the only long-lived branch.
- Agents may work in git worktrees under `.claude/worktrees/` (gitignored)
  and push verified commits to `main` (`git push origin HEAD:main`) after
  rebasing on `origin/main`.
- Humans can commit on `main` directly.

## Project Overview
Plutonium is a systems programming language: Go-familiar, not Go-compatible,
with no garbage collector, ownership visible in the types, and tasks and
channels on a scheduler that only programs using them carry
(`docs/spec/language.md`, chapter 1). The compiler is written in Plutonium and
builds itself: its source is `compiler/` (module `compiler`, imports
`compiler/...`), and the driver is `compiler/cmd/pluto`. The Go implementation it
was translated from was deleted in `15e6515d`; the `go-cutover` tag keeps it. See
"Self-hosted compiler" below. The pipeline:

| Stage | Package |
|---|---|
| scanner, parser, AST (forked from Go) | `compiler/internal/syntax/...` |
| type checker (forked go/types + Plutonium types) | `compiler/internal/types2` |
| package loading, build tags, `pluto.mod` | `compiler/internal/load` |
| scoped tasks of `sync.Scope`, for the checks and lowering (spec 19.4) | `compiler/internal/scoped` |
| move checker for owners (spec 18.5, 18.6) | `compiler/internal/moves` |
| stack escape analysis (spec 18.3, 18.4, 11.5) | `compiler/internal/escape2` (the default, `PLUTO_ESCAPE=new`); `compiler/internal/escape` (`PLUTO_ESCAPE=old`) |
| data race check (spec 19.2, 19.3) | `compiler/internal/race` |
| typed AST to SSA | `compiler/internal/lower` |
| SSA IR, verifier, basic passes | `compiler/internal/ssa` |
| optimiser pipeline | `compiler/internal/opt` |
| x86-64 backend (isel, regalloc, encoder) | `compiler/internal/arch/amd64` |
| ELF object writer | `compiler/internal/objfile` |
| driver (assemble, link) | `compiler/internal/build`, `compiler/cmd/pluto` |
| diagnostics and their catalog | `compiler/internal/diag` |
| `pluto lsp`, `pluto doc` | `compiler/internal/lsp`, `compiler/internal/doc` |

`compiler/compat` and `compiler/pu` are helpers the translation from Go left
behind. The standard library is Plutonium source under `lib/pluto/`. The compiler
does not embed it: it reads `$PLUTOROOT`, or else a `lib` directory beside its
executable or one level above it. A `pluto` at the repository root finds `lib/`
beside it; elsewhere set `PLUTOROOT=$PWD/lib`.

## The specification

The complete language specification is `docs/spec/language.md` (the contents
page and version) and the 21 chapters and 7 appendices in
`docs/spec/language/`. It is the single source of truth for the language:
every other document, comment, diagnostic and test agrees with it or says
plainly that it is historical.

- Section numbers are unique across the specification. Cite them as
  "section 18.7" (comments, catalog entries, plans). The catalog's `Spec`
  field is written with `pspec(section, title)`, and `TestSpecReferences`
  (`tests/spec_test.go`, no compiler needed) guards the specification: every
  reference in the catalog names a section that exists, under the title given;
  every section number mentioned in `compiler/internal/diag` and
  `compiler/internal/types2/errors/plutonium.p` exists; each section is
  numbered once, in the file of its chapter; no `> **Open` block remains.
- `docs/spec/abi.md` (binary interface), `memory-model.md`, `debugging.md`
  and `debug-allocator.md` refine the specification for implementers. They
  keep their section numbers, which the catalog, comments and tests cite, and
  they never contradict a chapter.
- The specification wins over the code. Where the compiler or the library
  differs from the text, the code is fixed; the gap is listed in
  `docs/roadmap.md` and in section 6.5 of
  `docs/plans/2026-10-10-complete-spec.md` until it is closed. A change to the
  specification is a user decision, recorded before any code moves. Do not
  change a chapter to match the code.
- Implementation agents work from the specification's text, by section number
  (the task statement is section 12.12, ownership and escape analysis are
  chapter 18, data race freedom is section 19.3, directives are section
  20.3), not from plans or decision notes. The plans in `docs/plans/` are the
  reasoning behind decisions; each starts with a Status block that names the
  sections now defining its subject and the statements the specification
  supersedes. `docs/plans/archive/` is historical.
- Go is named in the specification only in chapter 1, appendix A and appendix
  G. Elsewhere it states the Plutonium rule alone, and the word for a unit of
  concurrent execution is "task", never "goroutine" or "puroutine".

## Self-hosted compiler
Since the `go-cutover` tag (2026-10-26, self-hosting plan section 6.4) the compiler's
source is `compiler/`, edited by hand. It was translated from the Go implementation by
go2pu, and the first line of each translated file names the Go file it came from. The
Go implementation and go2pu were deleted in `15e6515d`
(`docs/plans/2026-10-26-go-removal.md`); the `go-cutover` tag keeps them. The driver has
every command (`build`, `run`, `version`, `explain`, `vet`, `test`, `fmt`, `doc`, `lsp`).

- Build the compiler with an existing `pluto`:
  `PLUTOROOT=$PWD/lib pluto build -O2 --gencheck=false -o pluto ./compiler/cmd/pluto`
  (about 15 minutes, about 7 GB).
- Without one: `go run ./tools/bootstrap -stages 1 -work DIR` checks the `go-cutover` tag
  out into a temporary worktree, builds its Go compiler there (stage 0), and builds
  `DIR/pluto1` with it. `-pluto0 FILE` starts from a given compiler instead.
- Self builds need `PLUTO_ESCAPE=new` in the environment. It is the default; an
  inherited `PLUTO_ESCAPE=old` or `both` makes a self build need far more memory.
- A change to `compiler/` is checked by the fixpoint:
  `go run ./tools/bootstrap -pluto0 ./pluto -stages 3` builds pluto1 with the given
  compiler, pluto2 with pluto1 and pluto3 with pluto2, and pluto2 and pluto3 must be the
  same byte for byte. Every stage builds with `-flags` (default `-O2 --gencheck=false`,
  unchecked); `-pluto1` starts from a stage 1 compiler built before. `-corpus` compares
  pluto1 with stage 0 over the test corpus; that comparison holds only until a change
  deliberately changes the compiler's output.

## Build & Test
- Run all tests: `PLUTO_TEST_COMPILER=$PWD/pluto go test -count=1 ./...`. The Go code
  left is the test harness `tests/`, `tools/bootstrap`, `tools/difftest`,
  `tools/mkunicode`, `tools/skillcheck`, the tests of `editors/vscode`, the skill's
  generators (`skill/plutonium/tools/{gendiag,genstdlib}`) and `bench/run.go` (a module
  of its own). None of it is a compiler: it execs one. `tests/` takes
  `PLUTO_TEST_COMPILER`, else `./pluto`; `tools/skillcheck` and `editors/vscode` take
  `PLUTO_TEST_COMPILER`, else `pluto` on `PATH`. Without a compiler they skip, so a run
  that finds none passes without testing anything.
- Build and run a program: `./pluto run ./examples/07-hello/`
- Listing / SSA dumps: `pluto build -S - ./dir`, `pluto build --ssa=main.main ./dir`
- Check SSA invariants after every pass: `PLUTO_SSACHECK=1` in the compiler's
  environment (`PLUTO_SSACHECK=1 go test ./tests`; the harness passes its environment on).
- Hosted builds have generation checks by default (`docs/spec/abi.md` section 1.3);
  `pluto build --gencheck=false` (and `pluto test -gencheck=false`) is the unchecked mode, and
  `PLUTO_GENCHECK=0 go test ./tests` runs the suite in it.
- Format Plutonium source (gofmt's rules plus the Plutonium syntax):
  `pluto fmt -l <paths>` lists files that differ, `-w` rewrites them, `-d`
  shows a diff. The formatter is `compiler/internal/syntax/printer` and
  `compiler/internal/syntax/format`, forks of go/printer and go/format; plain Go
  must format exactly as gofmt formats it (`TestSameAsGoFormat` in
  `tests/gofmt_test.go`, over Go's go/... sources and gofmt test data and the
  repository's Go files).
- Run Plutonium tests: `pluto test pluto/strings pluto/sort`,
  or a directory (`pluto test ./lib/pluto/strings`, which uses that tree as
  the standard library), or `./...`. Flags: `-run`, `-bench`, `-v`, `-count`,
  `-O`, `-trimpath`. Tests are `func TestXxx(t *testing.T)` in `*_test.p` files of the
  package itself, using `pluto/testing`; the loader leaves `_test.p` files out
  of every other build. `compiler/internal/build/test.p` generates the test main;
  `compiler/cmd/pluto/test.p` runs it and prints go test's format.

The compiler's tests are in `tests/`, a Go package that only execs the compiler
(`harness_test.go`). `go test ./tests -update` rewrites the expected output of the cases
and goldens that fail; read the diff before committing it.
- Corpus (`corpus_test.go`, `TestCorpus`): each case is a directory under
  `tests/<group>/` with a program and the output it must produce (format in
  `tests/corpus_test.go`). The groups are `diag` (a program for every diagnostic, with
  its full output) and `race`, `types2`, `escape`, `moves`, `lower` (the programs of the
  Go packages' unit tests).
- Programs (`programs_test.go`): `testdata/run`, `testdata/lib`, `testdata/plutonium`,
  `testdata/trace`, `testdata/cabi`, `testdata/kernel` (under QEMU) and the examples.
- Commands: `cli_test.go`, `vetdoc_test.go`, `fmt_test.go`, `plutotest_test.go` (build,
  run, test, vet, doc, fmt, explain), with their fixtures in `tests/testdata`.
- `escapereview_test.go` (the escape review programs, rejected under
  `PLUTO_ESCAPE=new`), `audit_test.go` (`pluto vet -unsafe` goldens),
  `directive_test.go` (directive diagnostics and the section 20.3 audit),
  `parse_test.go` (parser errors and a parse of every source file of the repository).

About the programs:
- `testdata/run/*.p` are programs in the common subset of Go and Plutonium. Their
  expected output (`*.out`) comes from running them with Go:
  `go test ./tests -run TestRun -update`. Each runs at -O0 and -O2.
- `testdata/lib/*.p` exercise the standard library (they import `pluto/...`, so
  Go cannot regenerate their output). Check a new or changed `.out` against Go
  running the same program with its imports pointed at Go's packages, or by
  hand for Plutonium-only APIs.
- `testdata/trace/*.p` panic; their `.out` files hold the expected message and stack trace, with
  `PC` for code addresses. Traces name files relative to the module root (`./main.p` for these
  programs, which have no `pluto.mod`) and the standard library (`pluto/fmt/print.p`), so builds are
  reproducible in any directory (`docs/spec/debugging.md` section 2.1). Each runs at -O0 and -O2 and must print the
  same trace at both.
- `examples/*/main.p` are checked against `examples/*/expected` (first line
  `exit N` for a non-zero exit status), and run again under `--alloc=debug`.
- Profile a build with `PLUTO_CPUPROFILE=file` and `PLUTO_MEMPROFILE=file` (pprof, written when
  the compiler finishes; the allocation profile needs `--gencheck=false`). `PLUTO_PHASES=1` names
  each phase on standard error as it starts, and `PLUTO_PROFILE_PHASE=name` writes the profiles
  and stops the build as that phase starts (`compiler/cmd/pluto/profile.p`).
  `PLUTO_ESCAPE_CHECK=1` makes the old escape analysis (`compiler/internal/escape`) check its fast
  ways against the general ones as it goes.

Known gaps: these guards went with the Go implementation and have no replacement yet.
- `internal/build/dwarf_test.go`: the DWARF output (`docs/spec/debugging.md`).
- The Go packages' API-level unit tests with no program form (opt, arch, ssa, escape2,
  lsp, the `internal/build` units; list in the go-removal plan, "Phase 2 status"). Their
  behaviour is covered by the corpus and the self-build fixpoint only.
- Coverage: `tools/pkgcov` and the per-package floors measured the Go packages. Nothing
  measures the coverage of `compiler/`.
- The test memory guard (`internal/testguard`), `tools/escapedigest` (a digest of the
  compiler's output over the corpus, for changes that must not change it) and the scale
  test of the escape analysis (`PLUTO_SCALE_TEST`).

## The agent skill

`skill/plutonium/` is an installable skill that teaches an LLM coding agent to write Plutonium (see its
`README.md` for installation). Its examples are compiled and run by `go test ./tools/skillcheck` (with the compiler
named by `PLUTO_TEST_COMPILER`, or `pluto` on the `PATH`; without either it skips), so a compiler or
library change that breaks an example fails that test: fix the text, or the compiler if it now disagrees with the
specification. `references/stdlib*.md` and `references/diagnostics.md` are generated; after the library or the
diagnostics catalog changes run `skill/plutonium/tools/regenerate.sh` (`SKILLCHECK_FRESH=1 go test ./tools/skillcheck
-run TestGeneratedFresh` tells whether they are stale). `references/known-gaps.md` lists where the code differs from
the specification today; each item is guarded by a block that fails when the gap closes, so delete the item then.

## Directives

Using the language should not need an understanding of how the compiler
works. Ordinary hosted code needs no `//pu:` directives except explicit
requests: build constraints (`//pu:build`), inlining (`//pu:inline`,
`//pu:noinline`), a stack size (`//pu:stack`), and C linkage
(`//pu:extern`, `//pu:export`). Hardware and binding directives
(`//pu:interrupt` and its form `//pu:interrupt trap`, `//pu:panic`, `//pu:scheduler`, `//pu:percpu`,
`//pu:fpu`, `//pu:blocking`) are for kernels, where code must be precise
about machine state. Section 20.3 of the specification is the complete set;
it also defines `//pu:section`, `//pu:nosimd`, `//pu:nochecks`,
`//pu:noescape`, `//pu:noreturn` and `//pu:resultholds`, and an implementation
defines no others.

- A new directive is allowed only for a fact the compiler cannot derive:
  linkage, hardware state, ABI bindings, a body it cannot see (assembly,
  C), or an explicit request from the programmer.
- Anything the compiler can work out from the program is inferred, not
  annotated: which functions never return, which may wait, which start
  tasks with their arguments, which string slices are on rune
  boundaries. If inference is not yet good enough, improve the compiler
  (or the language) rather than adding a directive.
- The runtime's contract with the compiler (scheduler hooks, the panic
  hook, intrinsics) is by name in a compiler table, not by directive.
- Diagnostics explain what the program did and how to fix it in its own
  terms, without requiring knowledge of the implementation.
- Update the list of directives in section 20.3 of the specification
  (`docs/spec/language/20-system-considerations.md`; a user decision comes
  first, as for any specification change) and the hover text in
  `compiler/internal/lsp/hover.p` with any change.

## Diagnostics

Every compile error is an `*diag.Diagnostic` (`compiler/internal/diag`): position,
code, one-line message, and optionally an explanation of its own. `pluto`
prints the message line unchanged (`file:line:col: message`, all that
`--brief` or `PLUTO_BRIEF=1` prints), then an indented explanation, a fix,
the spec section, and `run "pluto explain E1234"`. Codes and their ranges
are listed in `compiler/internal/diag/diag.p`.

To add an error:
1. Give it a code. Type checker errors: a constant at the end of a group in
   `compiler/internal/types2/errors/plutonium.p` (E2000-E2499). Then add its
   name to `code_string.p` in that directory by hand: append the name to
   `_Code_name` and add a `_Code_map` entry for the new value (there is no
   `go generate` now; only `Code.String` reads these). Anything else: a
   constant in `compiler/internal/diag/codes.p` (ownership and escape errors
   use E3000-E3999).
2. Add a catalog entry (`compiler/internal/diag/catalog_pluto.p`, or
   `catalog_build.p`): `Text` in plain words, `Why` when Plutonium differs
   from Go (no hidden allocation, no GC, kernel safety), `Spec` (the section
   of the language specification that states the rule, `pspec("18.3",
   "Views")`), and a
   `Fix` or `Example`/`Fixed`. Short sentences, no jargon without a
   definition, no em dashes, and American spelling, as in the go/types
   text the catalog builds on. `TestCatalogComplete` and `TestCatalogStyle`
   (`tests/catalog_test.go`) fail without it; they read the catalog from the
   sources, so they need no compiler. Run `pluto explain` on the new code.
3. Report it: `check.errorf(at, Code, ...)` in the checker;
   `l.errorf(pos, diag.Code, ...)` in `compiler/internal/lower`, or `l.report(pos, d)`
   with `d.Text`/`d.Fix` filled in from what is known at the error (names,
   lines); `diag.New` elsewhere. Type checker errors are specialised by
   code in `compiler/internal/diag/typecheck.p`.
4. Test the full output, not a substring: add a program under
   `tests/diag/` and run
   `PLUTO_TEST_COMPILER=$PWD/pluto go test ./tests -run TestCorpus/diag -update`
   with the compiler that reports it, then read the diff of
   `default.out` and `brief.out`. Tests that match only the first line keep
   passing, and that line must not change without reason.

Go's codes (E0001-E0999) take their text from go/types' documentation
(`catalog_go.p`, generated once and now edited by hand) unless `goPlain` in
`catalog_go_plain.p` has a hand-written entry.

## Verification Checklist (before considering work complete)

Every change must pass ALL of these before committing:

1. **Tests**: `PLUTO_TEST_COMPILER=$PWD/pluto go test -count=1 ./...` with a compiler
   built from the change; all packages must pass, and a run that skips for want of a
   compiler does not count. This builds and runs every example and every `testdata/run`
   program, and runs the corpus. It also checks that every `.p` file under `lib`,
   `examples`, `testdata` and `bench` is `pluto fmt` clean (`TestFmtRepoFormatted` in
   `tests/fmt_test.go`); fix a failure with `pluto fmt -w lib testdata examples bench`.
2. **Fixpoint**: a change to `compiler/` must reach the fixpoint
   (`go run ./tools/bootstrap -pluto0 ./pluto -stages 3`, "Self-hosted compiler").
3. **Lint**: `golangci-lint run ./...`: must report 0 issues.
   Fix all errcheck, staticcheck, and unused warnings before committing.
   Lint includes the `gofmt` formatter (`.golangci.yml`), so unformatted Go
   files fail it; fix them with `gofmt -w <file>`.
4. **New tests**: new code paths need tests: a `testdata/run` program for language
   features, a corpus case under `tests/<group>/` for a diagnostic or a pass. Nothing
   measures coverage now (see "Known gaps").
5. **No silent failures**: unsupported constructs must produce a compile error,
   never wrong code. Grep new code for `TODO` and placeholder values.

## Status and next steps

**Checkpoint 2026-10-17.** The specification went through a full consistency review (seven independent
reviews, 145 findings, 93 verified defects corrected, 19 design questions decided) and the compiler and
library were brought to it. The decisions are D9 to D40 in `docs/decisions.md`: string slicing is a library
call and `s[i:j]` on a string is E2019 (D9); normal termination follows Go and package cleanup is guaranteed
only after a `sync.Scope` join (D10, D31); `errors.New` has a fresh identity without allocating (D11);
D12 to D30 settle the 19 review questions (owner keyword, frame closures at task starts, `error`
sendability E2020, `defer` evaluation, `AfterFunc`, per-CPU regions E2022 and E2023, interrupt handlers as
race parties, packed types E2021, `print` E2521); D32 to D35 extend the use-after-move rule to deferred
literals, views and stored literals; D36 to D40 answer the last open questions (OS names, dependent per-CPU
stores, interrupt gates, no wait-for-all function, `AfterFunc` handle). Roadmap items 104 to 121, 123 to 127 and 128 to
136 are implemented and pushed. An adversarial review of the soundness-sensitive work found five defects,
all fixed. `skill/plutonium/` is an installable skill for LLM agents; `tools/skillcheck` compiles every
example in it on each `go test ./...`.

Work that remains, in order:
1. Item 122 (decision D36), the `os` name API with `*os.NameError`, is done. Items 123 to 127 (D37, D38: the
   dependent-store check in `compiler/internal/race`, typed `x86.Gate`, `InterruptGate` and `TrapGate`, the
   `//pu:interrupt trap` form, `x86.Sti` ending a handler's region, the hover text and the directive audit) are built.
2. The self-hosted port is done. The compiler builds itself since the `go-cutover` tag, and the Go
   implementation is gone (`15e6515d` deleted it, `9ea1540f` renamed `selfhost/` to `compiler/`;
   `docs/plans/2026-10-26-go-removal.md`). What remains is the list of known gaps under "Build & Test": the
   guards that went with the Go code.
3. Loose ends from item 132: `hash.Set` takes no owner keys (spec 7.10) and `vec.Fixed` owns nothing; a
   copy-method error on a container of owners (E3306) points at the library line, not the call; E3309 is
   masked by E3306; generic ranges over owner containers see `*T` in the generic form.
4. Known: the debug-allocator tests that join with a `WaitGroup` or channel retry a run that took the abandon
   path (D31).

Environment: run `unset GOROOT` first (a stale exported `GOROOT` from an older mise Go breaks builds); put
`ulimit -v 6000000` on `go test` commands only, never on an agent process or a self build (which needs about
7 GB).

Current checkpoint, 2026-10-14: the approved safe-arena split is landed at
`408dd2ab`. D6 orderly normal shutdown passes native source gates (all-package
tests, exhaustive sharded coverage, lint, full SSA, unchecked mode and the
tracked-source archive). Its reviewed specification replaces the old
package-held reporting direction. See
[the verification record](docs/verification/2026-10-14-normal-shutdown.md).
The public task-termination API is `rt.ExitTask`, with `rt.CatchExitTask`.
The dated 2026-10-12 status below is historical, including its fixed-arena
exemption, item 96 plan and whole-spec completion claims. Consult the current
roadmap, decisions and verification records for remaining work.

Updated 2026-10-12. `docs/roadmap.md` tracks every item; `docs/decisions.md` holds the user's
implementation decisions D1 to D5. This section says where the work stands and what comes next, in
order.

### Where things stand

- The specification (version 0.3) is complete and consistent, and every language feature in the roadmap's gap table is
  implemented and verified. Every row of that table is done. Of the items 93 to 98 that the document pass added, 93, 94 and 97 are done, 95 and 98
  are decided with nothing to build, and 96 is planned (step 5).
- Landed on 2026-10-11: `//pu:nochecks` (replacing `//pu:nobounds`), `clear` of a slice of owners
  drops each element (gap 33), E2512 for any two exports of one symbol (item 94), `defer` of
  `clear`, `copy` and `delete` (gap 6), `go test ./internal/build -run TestRun -update` translating
  task statements for Go, and `strconv.Unquote`, `UnquoteChar` and `QuotedPrefix`.
- Counted errors (gap 22, items 11 and 69) and counted `or` releases (item 99) completed native amd64
  verification on 2026-10-12. Generation-directory checks (gap 10, item 85) also pass the full native
  checklist. Gap 32, the two known escape bugs and complete `strconv` quoting also pass native
  verification. Allocation-free streaming FNV, explicit-key SipHash and `pluto vet -unsafe` are native
  verified; `pluto doc` is now native verified, and package-variable reporting is next.
  A follow-up to item 58 preserves `rt.ExitTask` cleanup through bound scheduler and late runtime
  hooks, including custom stack allocation. Full native gates pass; see the
  [verification record](docs/verification/2026-10-12-goexit-hooks.md). Item 96 remains unfinished.
- `~/go/bin/pluto` was the installed compiler, reinstalled after each landing with
  `go install ./cmd/pluto` (historical: the Go driver is gone; build `pluto` as in
  "Self-hosted compiler").

### Next steps

#### Counted-error handoff completed on native amd64, 2026-10-12

The counted-error implementation (`e365a43f`) and native verification fixes implement sections 16.5
to 16.7. **Gap 22 is done.** Gap 32 subsequently completed native verification below.

- `types2.IsCountedError` recognizes ordinary interfaces including `Error() string`. Lowering
  retains/releases their shared or synced dynamic values, using descriptor flag bits 8 and 16 and
  the dynamic drop adapter. Ordinary interfaces remain borrowed. Counted type-switch bindings and
  counted interface assertions take their own references.
- `errors.As` matches a shared/synced box against a borrowed `*T` target (D1; section 16.6). Escape
  checks bound pointer and lending-interface results to the caller's error, including helper and
  indirect calls; counted targets keep independent references. `errors.As` now recursively walks
  single-error unwraps so its borrowed parameter provenance remains visible.
- Native review removed an unsafe recursive-As exception. Whole-program summaries distinguish static
  values from counted heap references, preserve definite and generic conversion effects through
  helper calls, and retain nested receiver/parameter field provenance within the existing field bound.
  Fresh unwrap children cannot supply borrowed targets; static children and children held by the root
  pass. Strong-box fields also retain their root provenance. Exact shared/synced assertions take
  independent references. Regressions include indirect calls, generic forwarding, temporary holders,
  nested fields beyond the bound, maps, slices, dereferences, channels and bodyless results.
- `fmt.Errorf`, `errors.Join`, `os.PathError` and `strconv.NumError` use counted boxes. There is no
  `errors.Free`, `PathError.Free` or `NumError.Free`. ParseInt, ParseUint, Atoi and ParseFloat wrap
  failed conversions in NumError; successful conversions do not allocate. File Read, Write and Close
  wrap failed system calls in PathError, preserving EOF and ErrClosed sentinel identity.
- New regressions are `internal/build/counted_errors_test.go`,
  `internal/build/counted_patherrors_test.go`, `internal/escape/counted_error_test.go` and
  `internal/types2/counted_error_test.go`. They cover
  copies, overwrites, error-embedding interfaces, exact assertions versus As, shared and synced
  payloads, arrays, maps, channels, wrapped trees, borrowed-pointer escape and counted targets.
  The runtime tests include debug-allocator leak checks at O0 and O2. The `or` return and discard
  paths release counted errors (item 99).
- Counted variadic argument arrays intentionally live until their caller returns. Allocation
  assertions in `testdata/lib/errors.p` and `allocdefaults.p` therefore run after helper frames
  return. Join now allocates a separate Vec for its error list; the golden allocation counts increase
  by one allocation and one matching free. NumError golden messages now include function and input.
- The full run exposed recursive cleanup generation when an implicit error drop emitted a Goexit
  check. Its indirect drop call now completes cleanup before propagating Goexit, as a deferred call
  does. The focused Goexit and kernel-or checks pass. Kernel-or's call-shape expectation permits
  the guarded counting machinery introduced by section 16.7.

Native Linux amd64 verification passes with Go 1.27.1 and golangci-lint 2.14.0: full tests, zero lint
issues, aggregate coverage nondecrease with every package floor, `PLUTO_SSACHECK=1` and
`PLUTO_GENCHECK=0` full build suites. Native timing and resident-memory checks pass with the default
testguard caps; the earlier QEMU timing failures are absent. Coverage compared the complete native
`e365a43f` baseline profile (with its known pre-fix oracle/import failures) with the passing current
suite: aggregate 87.76000% to 87.78009%, escape 94.65352% to 94.69588%. Unchanged-package stochastic
counter variations are documented in the verification record; deterministic EOF and out-of-order
file-set contract tests also pass. No caps, tests or specification rules were weakened.

The native pass also corrected flag range classification to use `errors.Is` for wrapped NumError,
updated PathError extraction and intentional error-message oracles, and preserved the errpatterns
oracle with recursive traversal while its root stays held. Default and brief diagnostics explain an
unsafe user Unwrap and suggest a matching counted target or storing the child in the root. One
conservative generic assertion precision limit is recorded under roadmap item 5 for the later
compiler/specification review. The remaining unfinished numbered work below has not been started.

#### Generation-directory handoff completed on native amd64, 2026-10-12

**Gap 10 is done.** Sections 18.9 and 18.11, ABI sections 1.2 and 1.3, and memory-model
section 3.8 define the approved shared metadata directory. All checked pointers use it, including
contiguous large Slab/Chunked blocks and inferred custom allocators. Address histories persist across
reuse; generations retire before 0x8000. Stable reader metadata and saved ancestor incarnations
preserve partial-release validity. Fixed `arena.Arena` buffers remain explicitly unchecked.

Native verification passes full tests from a tracked-only archive, zero lint issues, all package
coverage floors and aggregate nondecrease (87.78009% to 87.86287%), full SSA checks and the full
generation-off build suite. Default timeout and memory caps remain unchanged. Embedded-runtime
hello and the installed `~/go/bin/pluto` match their exact oracle. The
[verification record](docs/verification/2026-10-12-generation-directory.md) includes failed-run
evidence and complete coverage comparison.

Checked microbenchmarks expose substantial costs: dense uint64 reads 16.498 ns versus prior checked
0.809 ns; 1024-byte consumed copy/clear 1503.776 ns versus 49.247 ns. These are scoped probes,
not application-wide ratios or a near-Rust performance claim. Their sources, layout limits and
output-equivalence proofs are recorded for the ordered tuning pass.

#### Ordered work

1. **Gap 22, counted errors: done** (roadmap items 11 and 69; sections 16.5 and 16.7; decision D1:
   `errors.As` matches a `synced[T]` error against a `*T` target). The type checker, lowering, library,
   `os` and `strconv` error types and counted `or` releases (item 99) are verified on native amd64.
2. **Gap 10, generation tags for large blocks: done** (item 85; section 18.11, abi.md section 1.3;
   approved shared-directory decision of 2026-10-12). Universal directory checks cover small and
   arbitrary contiguous large heap/Chunked blocks, nested backing and custom allocators. Fixed
   `arena.Arena` buffers stay unchecked. Native verification and measured costs are recorded above.
3. **Gap 32, package initialization starts no task: done** (item 100; sections 15.2, 15.3, 19.1,
   19.3; decision D2). E2587 follows whole-program reachability before optimization, including
   indirect calls, deferred and implicit drops, all imported initializers and every build mode.
   It names the initializer and task start and suggests moving the start to `main`. Native full
   tests, zero lint, coverage nondecrease and all floors, SSA checks and generation-off tests pass.
   Blank initializer values are destroyed after named stores; counted temporaries are released
   before the next initializer. The obsolete G44 block and allowlist entry are removed.
   See the [verification record](docs/verification/2026-10-12-initialization-tasks.md).
4. **Two known escape bugs: done** (sections 11.5 and 18.3). Closure results track their actual
   outer dependencies separately by result and field, so counted strings remain independent of an
   unrelated closure capture (difftest seed 362192, present since `3265b8c`). Stored views through
   temporary owners and counted boxes now receive E3009 at compile time. Projected aggregate,
   array, generic, interface and indirect paths preserve the holder's lifetime; counted copies keep
   independent references. Immediate calls and full `if`/`switch` header borrows remain valid.
   Native full tests, zero lint, aggregate coverage nondecrease and all floors, SSA checks and
   generation-off tests pass. See the [verification record](docs/verification/2026-10-12-escape-bugs.md).
5. **Smaller items**, each with an exact oracle:
   - **`strconv` quoting: done.** All string/rune plain, ASCII and graphic forms and their
     `Append` forms share the rune-aware emitter in `lib/pluto/strconv/quote_format.p`.
     Exact Go hex oracles cover unsafe malformed-byte views, Unicode classes, rune boundaries,
     prefixed appends and bounded byte-prefix truncation at O0/O2. Strings use one counted buffer;
     bounded appends allocate nothing. Full native gates pass; phase 1's quoting prerequisite is
     complete. See the [verification record](docs/verification/2026-10-12-strconv-quoting.md).
   - **Streaming hash: done.** `hash.FNV32a` and
     `hash.FNV64a` are zero-ready allocation-free streams matching Go; `hash.SipHash24` uses an
     explicit copied 16-byte key and matches the official vectors and an independent Go oracle.
     Sums preserve state, resets preserve the SipHash key, and uninitialized SipHash operations
     fail explicitly. Full native gates and zero-allocation checks pass. Compiler hashing
     migration remains part of the later port. See the
     [verification record](docs/verification/2026-10-12-streaming-hash.md).
   - **`pluto vet -unsafe`: done.** Item 97 (sections 18.12, 21.1 and 21.5) audits every source
     function and initializer without running or linking packages. Typed unsafe sites include
     `//pu:nochecks`; safe callers retain their labels and show direct or possible call paths.
     Exact CLI and source-graph oracles, full native gates and coverage nondecrease pass. See the
     [verification record](docs/verification/2026-10-12-vet-unsafe.md).
   - **`pluto doc`: done.** Item 93 documents packages, symbols and methods without running or linking
     source. Exact labels and editor hovers share the section 8.7 classifier; comments, defaults,
     aliases, promotions and generic instances are covered by exact goldens. Native full gates and
     a tracked-only archive pass. See the [verification record](docs/verification/2026-10-12-pluto-doc.md).
   - The debug allocator reports package-variable memory apart from leaks (item 96; sections 15.4
     and 18.6).
6. **After the gap list is empty:** a tuning pass on a quiet machine (task spawn, about 2x Go;
   buffered channels; the global task-list lock added for deadlock traces; string-key map hashing;
   `strings.ReplaceAll`; shared-directory range checks using one coherent registration/ancestor
   snapshot, same-granule scalar checks, proven extent reuse and interval-node hints that never cache
   liveness); a full difftest campaign with every feature enabled; one pass reading the
   compiler against the specification in reverse, looking for code that does something the
   specification does not say.
7. **Self-hosting** (`docs/plans/2026-10-09-self-hosting-plan.md`): the rest of phase 1's library,
   moving the compiler onto the `pu` package API, then the port. `types2` is the hard part.

Known limits are recorded in the roadmap: `sync.Scope` phase 2 (a scope through a pointer, helpers
that start tasks, same-type slice partitions, `vec.Vec` element partitions); the race rule groups
maps by type; generated thunks are named `.task1`.
