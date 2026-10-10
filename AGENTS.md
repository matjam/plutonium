# Plutonium Development Guidelines

## Branch Strategy
- **Main branch:** `main` (no worktrees, develop directly on main)
- Do NOT use git worktrees for this project
- All development happens directly on the `main` branch

## Project Overview
Plutonium is a systems programming language: Go's syntax and most of its
semantics, without a garbage collector or scheduler (see `docs/spec/language.md`).
The compiler is written in Go. Pipeline:

| Stage | Package |
|---|---|
| scanner, parser, AST (forked from Go) | `internal/syntax/...` |
| type checker (forked go/types + Plutonium types) | `internal/types2` |
| package loading, build tags, `pluto.mod` | `internal/load` |
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
- Format Plutonium source (gofmt's rules plus the Plutonium syntax):
  `pluto fmt -l <paths>` lists files that differ, `-w` rewrites them, `-d`
  shows a diff. The formatter is `internal/syntax/printer` and
  `internal/syntax/format`, forks of go/printer and go/format; plain Go must
  format exactly as gofmt formats it (`TestSameAsGoFormat`).
- Run Plutonium tests: `PLUTOROOT=$PWD/lib /tmp/pluto test pluto/strings pluto/sort`,
  or a directory (`pluto test ./lib/pluto/strings`, which uses that tree as
  the standard library), or `./...`. Flags: `-run`, `-bench`, `-v`, `-count`,
  `-O`. Tests are `func TestXxx(t *testing.T)` in `*_test.p` files of the
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
  `$FILE` for the program's path and `PC` for code addresses. Each runs at -O0 and -O2 and must print the
  same trace at both. DWARF output is checked by `internal/build/dwarf_test.go` (see
  `docs/spec/debugging.md`).
- `examples/*/main.p` are checked against `examples/*/expected` (first line
  `exit N` for a non-zero exit status).

## Verification Checklist (before considering work complete)

Every change must pass ALL of these before committing:

1. **Tests**: `go test -count=1 ./...` — all packages must pass. This builds and
   runs every example and every `testdata/run` program.
2. **Lint**: `golangci-lint run ./...` — must report 0 issues.
   Fix all errcheck, staticcheck, and unused warnings before committing.
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

   New code paths need tests: a `testdata/run` program for language features,
   a unit test for passes and backend details.
4. **No silent failures**: unsupported constructs must produce a compile error,
   never wrong code. Grep new code for `TODO` and placeholder values.
