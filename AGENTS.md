# Plutonium Development Guidelines

## Branch Strategy
- **Main branch:** `main` (no worktrees, develop directly on main)
- Do NOT use git worktrees for this project
- All development happens directly on the `main` branch

## Project Overview
Plutonium is a systems programming language compiler targeting x86_64 Linux.
The compiler is written in Go with the CLI at `cmd/pluto/`, internal packages
under `internal/`, and the stdlib as `.pu` source files under `pluto/`.

## Build & Test
- Build compiler: `go build -o /tmp/pluto ./cmd/pluto/`
- Run all tests: `go test ./...`
- Build an example: `/tmp/pluto build -o /tmp/example ./examples/07-hello/`
- Run: `/tmp/example`

## Verification Checklist (before considering work complete)

Every change must pass ALL of these before committing:

1. **Unit tests**: `go test -count=1 ./...` — all packages must pass
2. **Examples**: Build and run ALL examples — every one must produce correct output:
   ```
   go build -o /tmp/pluto ./cmd/pluto/
   for ex in examples/*/; do /tmp/pluto build -o /tmp/ex_test ./$ex && /tmp/ex_test; done
   ```
3. **Lint**: `golangci-lint run ./...` — must report 0 issues.
   Fix all errcheck, staticcheck, and unused warnings before committing.

4. **Coverage check**: `go test -cover ./...` — coverage must not decrease.
   Current minimum coverage targets:
   - `internal/ir`: ≥73%
   - `internal/codegen/x86_64`: ≥88%
   - `internal/parser`: ≥63%
   - `internal/types`: ≥52%
   - `internal/mono`: ≥62%
   
   If your change adds new code paths, add tests to maintain or improve
   coverage. Never merge code that drops a package below its current level.

5. **No silent failures**: Grep for `ConstInt(I32, 0)` fallbacks and
   `// TODO` comments in new code. New features must not silently produce
   wrong code.
