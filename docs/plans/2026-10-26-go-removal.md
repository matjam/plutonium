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
4. Rename `selfhost/` to `compiler/`; update AGENTS.md, README, skill docs,
   .gitignore, .golangci.yml.
