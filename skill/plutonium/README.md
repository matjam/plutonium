# The Plutonium skill

An installable skill that gives an LLM coding agent what it needs to write and change code in the **Plutonium**
language with this compiler suite: the differences from Go, the ownership model, the toolchain, the diagnostics
and the standard library, with working examples. It is written for an agent that knows Go well and has never seen
Plutonium.

Every program and every error example in it is compiled by the real compiler as part of `go test ./...`
(`tools/skillcheck`), so the text cannot drift from the language without a test failing.

## What is in it

```
skill/plutonium/
  SKILL.md                    entry point: frontmatter, the five rules, the Go differences, ownership on one page,
                              toolchain, diagnostics, workflow (under 400 lines)
  references/                 loaded on demand
    language-tour.md          types, declarations, statements, generics, enums, unions, defer, built-ins
    ownership-and-memory.md   owners, moves, drops, views, owned/shared/synced/weak, arenas, E3xxx errors
    concurrency.md            tasks, channels, select, sync.Scope, locks, sendability, race errors
    errors-and-panics.md      error values, the or clause, panics
    idioms.md                 programs: CLI, files, config parser, formatting, sorting, iterators, tests
    idioms-types-memory.md    programs: lists, trees, enums, unions, Drop, generics, constructors, multimaps
    idioms-memory.md          programs: arenas, packed headers, counted boxes, weak parents
    idioms-concurrency.md     programs: pools, pipelines, timers, locks, partitioned work
    go-to-plutonium.md        translating Go
    kernel-mode.md            freestanding builds, directives, volatile, packed, ring, per-CPU, interrupts
    known-gaps.md             where the compiler or library differs from the specification today
    diagnostics.md            every error code (generated from compiler/internal/diag)
    library-cheatsheet.md     the calls programs use most, with the shapes that differ from Go
    stdlib*.md                every exported library name, one line each (generated from lib/pluto)
  tools/
    genstdlib/                generator for references/stdlib*.md
    gendiag/                  generator for references/diagnostics.md
    regenerate.sh             runs both generators
    build-single-file.sh      builds one plutonium-guide.md for harnesses without skill support
```

## Install for Claude Code

Copy (or symlink) the directory so that `SKILL.md` sits directly inside a folder named `plutonium`:

```sh
# for all your projects
mkdir -p ~/.claude/skills && cp -r skill/plutonium ~/.claude/skills/plutonium

# or for one project (commit it so the team shares it)
mkdir -p .claude/skills && cp -r /path/to/plutonium/skill/plutonium .claude/skills/plutonium
```

`tools/` is only needed to regenerate the references; leave it out of an installed copy if you like. The skill
loads when a task mentions Plutonium, `.p` files, `pluto.mod` or the `pluto` commands (see the `description`
in `SKILL.md`), and reads a reference file when `SKILL.md` points at it.

## Install for opencode and other harnesses

Harnesses that read `AGENTS.md` or a rules file but have no skills can use the single-file build:

```sh
skill/plutonium/tools/build-single-file.sh plutonium-guide.md      # about 7000 lines (roughly 90k tokens)
skill/plutonium/tools/build-single-file.sh -only language-tour,ownership-and-memory,concurrency,errors-and-panics \
    plutonium-core.md                                                # a smaller guide
```

Then give the file to the tool:

- **opencode, Codex, Zed and others that read `AGENTS.md`**: put the file (or its content) in the project as
  `AGENTS.md`, or add it to the instructions the tool loads. For opencode, list it under `instructions` in
  `opencode.json` or reference it from `AGENTS.md`.
- **Cursor**: `.cursor/rules/plutonium.mdc` with `alwaysApply: false` and a description, or a glob for `**/*.p`.
- **Aider**: `aider --read plutonium-guide.md` (or set `read:` in `.aider.conf.yml`).
- **GitHub Copilot, Continue and tools with a size limit**: use the `-only` build with the references you need
  (`language-tour,ownership-and-memory` is about 1200 lines) and keep `SKILL.md`'s five rules in the always-on file.

The guide drops the frontmatter, turns each reference into a section named `Reference: ...`, and rewrites the
checked fences to plain `go` fences.

## Keeping it correct

```sh
go test ./tools/skillcheck                # compiles and runs every marked example (about a minute)
                                          # with $PLUTO_TEST_COMPILER, else pluto on PATH; skips without one
skill/plutonium/tools/regenerate.sh       # after the library or the diagnostics catalog changes
SKILLCHECK_FRESH=1 go test ./tools/skillcheck -run TestGeneratedFresh   # are the generated files up to date?
```

The test reads every `*.md` file under `skill/plutonium/` and checks:

| Fence | Meaning |
|---|---|
| ` ```pluto-run [exit=N] ` | a complete program: must build and run, exit with status N (default 0); a following ` ```output ` fence is the exact standard output |
| ` ```pluto-compile [mode=kernel] ` | a complete file that must build (a missing `func main` is added; `mode=kernel` or `freestanding` compiles to an object file in that mode) |
| ` ```pluto-error E3301 [mode=kernel] ` | a complete file that must fail with that error code |
| ` ```pluto-test ` | a package with `_test.p` files, given as several files each starting with a `//file: name.p` line; run like `pluto test` |
| ` ```pluto-names ` / ` ```pluto-absent ` | library names (`strings.Cut`, `vec.Vec.Push`) that must exist / must not exist |
| any other fence | illustrative, not checked |

It also checks that every `Exxxx` code named in the text is in the catalog, that every `section N.M` cited exists in
the specification, that links resolve, that `SKILL.md` has valid frontmatter and fewer than 400 lines, and that the
single-file build works. When a compiler change breaks an example, fix the text (or, if the compiler now disagrees
with the specification, fix the compiler); `known-gaps.md` lists the places where it does today, each guarded by a
block that fails when the gap closes.

The specification (`docs/spec/language.md` and `docs/spec/language/`) is the source of truth. This skill distils it
and cites section numbers; it never overrides it.
