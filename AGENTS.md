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
