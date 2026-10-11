# Plutonium Development Guidelines

## Branch Strategy
- `main` is the only long-lived branch.
- Agents may work in git worktrees under `.claude/worktrees/` (gitignored)
  and push verified commits to `main` (`git push origin HEAD:main`) after
  rebasing on `origin/main`.
- Humans can commit on `main` directly.

## Project Overview
Plutonium is a systems programming language: Go's syntax and most of its
semantics, without a garbage collector or scheduler (see `docs/spec/language.md`).
The compiler is written in Go. Pipeline:

| Stage | Package |
|---|---|
| scanner, parser, AST (forked from Go) | `internal/syntax/...` |
| type checker (forked go/types + Plutonium types) | `internal/types2` |
| package loading, build tags, `pluto.mod` | `internal/load` |
| scoped tasks of `sync.Scope`, for the checks and lowering (language spec 19.4) | `internal/scoped` |
| move checker for owners (language spec 6.4) | `internal/moves` |
| stack escape analysis (language spec 6.3) | `internal/escape` |
| data race check (language spec 6.11) | `internal/race` |
| typed AST to SSA | `internal/lower` |
| SSA IR, verifier, basic passes | `internal/ssa` |
| optimiser pipeline | `internal/opt` |
| x86-64 backend (isel, regalloc, encoder) | `internal/arch/amd64` |
| ELF object writer | `internal/objfile` |
| driver (assemble, link) | `internal/build`, `cmd/pluto` |

The standard library is Plutonium source under `lib/pluto/`, embedded in the
compiler binary. Set `PLUTOROOT=$PWD/lib` to use the working tree instead.

## Build & Test
- Build compiler: `go build -o /tmp/pluto ./cmd/pluto/`
- Run all tests: `go test -count=1 ./...`
- Build and run a program: `PLUTOROOT=$PWD/lib /tmp/pluto run ./examples/07-hello/`
- Listing / SSA dumps: `pluto build -S - ./dir`, `pluto build --ssa=main.main ./dir`
- Check SSA invariants after every pass: `PLUTO_SSACHECK=1 go test ./internal/build/`
- Hosted builds have generation checks by default (`docs/spec/abi.md` section 1.3);
  `pluto build --gencheck=false` (and `pluto test -gencheck=false`) is the unchecked mode, and
  `PLUTO_GENCHECK=0 go test ./internal/build/` runs the suite in it. Tests of code shapes the checks
  add to build with `build.Config{NoGenCheck: true}` (`listFuncUnchecked`).
- Format Plutonium source (gofmt's rules plus the Plutonium syntax):
  `pluto fmt -l <paths>` lists files that differ, `-w` rewrites them, `-d`
  shows a diff. The formatter is `internal/syntax/printer` and
  `internal/syntax/format`, forks of go/printer and go/format; plain Go must
  format exactly as gofmt formats it (`TestSameAsGoFormat`).
- Run Plutonium tests: `PLUTOROOT=$PWD/lib /tmp/pluto test pluto/strings pluto/sort`,
  or a directory (`pluto test ./lib/pluto/strings`, which uses that tree as
  the standard library), or `./...`. Flags: `-run`, `-bench`, `-v`, `-count`,
  `-O`, `-trimpath`. Tests are `func TestXxx(t *testing.T)` in `*_test.p` files of the
  package itself, using `pluto/testing`; the loader leaves `_test.p` files out
  of every other build. `internal/build/test.go` generates the test main;
  `cmd/pluto/test.go` runs it and prints go test's format.

End-to-end tests live in `internal/build`:
- `testdata/run/*.p` are programs in the common subset of Go and Plutonium. Their
  expected output (`*.out`) comes from running them with Go:
  `go test ./internal/build -run TestRun -update`. Each runs at -O0 and -O2.
- `testdata/lib/*.p` exercise the standard library (they import `pluto/...`, so
  Go cannot regenerate their output). Check a new or changed `.out` against Go
  running the same program with its imports pointed at Go's packages, or by
  hand for Plutonium-only APIs.
- `testdata/trace/*.p` panic; their `.out` files hold the expected message and stack trace, with
  `PC` for code addresses. Traces name files relative to the module root (`./main.p` for these
  programs, which have no `pluto.mod`) and the standard library (`pluto/fmt/print.p`), so builds are
  reproducible in any directory (`docs/spec/debugging.md` section 2.1). Each runs at -O0 and -O2 and must print the
  same trace at both. DWARF output is checked by `internal/build/dwarf_test.go` (see
  `docs/spec/debugging.md`).
- `examples/*/main.p` are checked against `examples/*/expected` (first line
  `exit N` for a non-zero exit status).
- The tests in `internal/build` and their subtests call
  `testguard.Parallel(t)` (`internal/testguard`, which calls `t.Parallel()`
  and records the test as running); a new one should too. The exceptions are the scheduler, timer and poller
  tests in `conc_test.go`, `timer_test.go` and `poll_test.go` whose
  programs depend on timing: they build in parallel (`buildConcAll`) and
  then run their programs one at a time, before the parallel tests start.
- Memory guard (`internal/testguard`, used by `TestMain` in `internal/build`
  and `tools/go2pu`): unless `-parallel` is given, it lowers the parallelism
  so that slots at `PLUTO_TEST_SLOT_MB` each (default 256; 0 turns this
  off) fit in `MemAvailable`, and says so on stderr. A watchdog samples the
  test binary's resident memory every second and, past `PLUTO_TEST_RSS_MB`
  (default: 80% of the memory available at start, at most 12288; 0 turns it
  off), prints the tests in flight and exits with status 2. A run killed
  this way means a test, or a compiler change it exercises, uses far more
  memory than before: fix that rather than raising the cap. Linux only
  (`/proc`); elsewhere both do nothing.

## Directives

Using the language should not need an understanding of how the compiler
works. Ordinary hosted code needs no `//pu:` directives except explicit
requests: build constraints (`//pu:build`), inlining (`//pu:inline`,
`//pu:noinline`), a stack size (`//pu:stack`), and C linkage
(`//pu:extern`, `//pu:export`). Hardware and binding directives
(`//pu:interrupt`, `//pu:panic`, `//pu:scheduler`, `//pu:percpu`,
`//pu:fpu`, `//pu:blocking`) are for kernels, where code must be precise
about machine state.

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
- Update the table in `docs/spec/language.md` section 9 and the hover text
  in `internal/lsp/hover.go` with any change.

## Diagnostics

Every compile error is an `*diag.Diagnostic` (`internal/diag`): position,
code, one-line message, and optionally an explanation of its own. `pluto`
prints the message line unchanged (`file:line:col: message`, all that
`--brief` or `PLUTO_BRIEF=1` prints), then an indented explanation, a fix,
the spec section, and `run "pluto explain E1234"`. Codes and their ranges
are listed in `internal/diag/diag.go`.

To add an error:
1. Give it a code. Type checker errors: a constant at the end of a group in
   `internal/types2/errors/plutonium.go` (E2000-E2499), then `go generate`
   in that directory. Anything else: a constant in `internal/diag/codes.go`
   (ownership and escape errors use E3000-E3999).
2. Add a catalog entry (`internal/diag/catalog_pluto.go`, or
   `catalog_build.go`): `Text` in plain words, `Why` when Plutonium differs
   from Go (no hidden allocation, no GC, kernel safety), `Spec`, and a
   `Fix` or `Example`/`Fixed`. Short sentences, no jargon without a
   definition, no em dashes, and American spelling, as in the go/types
   text the catalog builds on. `TestCatalogComplete` and `TestCatalogStyle`
   fail without it.
3. Report it: `check.errorf(at, Code, ...)` in the checker;
   `l.errorf(pos, diag.Code, ...)` in `internal/lower`, or `l.report(pos, d)`
   with `d.Text`/`d.Fix` filled in from what is known at the error (names,
   lines); `diag.New` elsewhere. Type checker errors are specialised by
   code in `internal/diag/typecheck.go`.
4. Test the full output, not a substring: add a program under
   `cmd/pluto/testdata/diag/` and run
   `go test ./cmd/pluto -run TestDiagnostics -update`, then read the diff of
   `default.out` and `brief.out`. Tests that match only the first line keep
   passing, and that line must not change without reason.

Go's codes (E0001-E0999) take their text from go/types' documentation
(`catalog_go.go`, `go generate` in `internal/diag`) unless `goPlain` in
`catalog_go_plain.go` has a hand-written entry.

## Verification Checklist (before considering work complete)

Every change must pass ALL of these before committing:

1. **Tests**: `go test -count=1 ./...` — all packages must pass. This builds and
   runs every example and every `testdata/run` program. It also checks that
   every `.p` file under `lib`, `examples`, `testdata` and `bench` is
   `pluto fmt` clean (`TestRepoFormatted` in `internal/syntax/format`); fix a
   failure with `pluto fmt -w lib testdata examples bench`. A file that must
   stay unformatted goes in that test's `unformattedAllowed` list with a reason.
2. **Lint**: `golangci-lint run ./...` — must report 0 issues.
   Fix all errcheck, staticcheck, and unused warnings before committing.
   Lint includes the `gofmt` formatter (`.golangci.yml`), so unformatted Go
   files fail it; fix them with `gofmt -w <file>`.
3. **Coverage**: coverage must not decrease. Measure across packages, since the
   end-to-end tests exercise most of the compiler:
   ```
   go test -count=1 -coverpkg=./internal/... -coverprofile=/tmp/cover.out ./...
   go run ./tools/pkgcov /tmp/cover.out
   ```
   Current minimum coverage targets:
   - `internal/arch/amd64`: ≥86%
   - `internal/lower`: ≥85%
   - `internal/opt`: ≥90%
   - `internal/ssa`: ≥79%
   - `internal/load`: ≥71%
   - `internal/objfile`: ≥95%
   - `internal/syntax/parser`: ≥70%
   - `internal/syntax/printer`: ≥94%
   - `internal/syntax/format`: ≥91%
   - `internal/diff`: ≥98%
   - `internal/types2`: ≥56%
   - `internal/escape`: ≥90%
   - `internal/moves`: ≥80%
   - `internal/race`: ≥90%
   - `internal/lsp`: ≥85%

   New code paths need tests: a `testdata/run` program for language features,
   a unit test for passes and backend details.
4. **No silent failures**: unsupported constructs must produce a compile error,
   never wrong code. Grep new code for `TODO` and placeholder values.
