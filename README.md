# Plutonium

Plutonium is a systems programming language for operating system kernels, drivers, boot code and
allocators, and for the ordinary programs that run on top of them. It is the language PlutoniumOS is
written in. It reads like Go and compiles like C: no garbage collector, no hidden allocation, and a static
binary with no libc and no runtime the program did not ask for. It gives Rust-level memory and race safety
without a borrow checker and without annotations, and it makes every allocation and every owner visible in
the source.

Plutonium is Go-familiar, not Go-compatible. It uses Go's syntax and idioms where they fit, and where it
behaves differently from Go in a way a Go programmer would not expect, the difference is visible in the
source: a different name, a different syntax, a different type, or a compile error that explains it.

This project was mostly coded using Claude Code's Opus model.

## Goals

These are the goals of chapter 1 of the specification:

- **Read like Go, and never mislead a Go reader.** Code that differs from Go looks different.
- **Compile like C.** No garbage collector, no hidden allocation, no runtime the program did not ask for. A
  program that never starts a task or makes a channel contains no scheduler.
- **Make costs visible.** Every allocation has an owner or an arena, and the source says which. Every
  memory-mapped register access is marked in its type.
- **Make ownership visible.** Every piece of memory has one owner, named by its type, and is freed when that
  owner is dropped. Looking, moving and sharing are written differently.
- **Give kernel code the tools it needs in the language:** bit-exact layouts (`packed`), volatile access,
  alignment control, ring buffers, and a fatal-error path that works before any allocator exists.
- **Generate fast code.** An optimizing compiler can match the performance of Rust, and the compiler is
  measured against it (`bench/`).

## The language in short

**Values, views, owners and counted values.** Every type is one of four kinds, and the kind decides what
assignment does (chapter 18).

| Kind | Examples | Assignment | Freed |
|---|---|---|---|
| value | numbers, booleans, enums, packed types, arrays and structs of values | copies | nothing to free |
| view | `*T`, `[]T`, closures | copies the view | never; it looks at memory something else owns |
| owner | `vec.Vec[T]`, `owned[T]`, `os.File`, a type declared `owner` | moves | when dropped, at the end of its scope |
| counted value | `string`, `map[K]V`, `chan T`, `shared[T]`, `synced[T]` | copies and counts | when the last copy is dropped |

A view may not outlive what it looks at: the compiler rejects returning, storing or sending a view of a
frame, and checks the use of a view of freed heap memory at run time. Doubt resolves to a compile error.
The errors explain the rule in plain words and show a fix (`pluto explain E3001`).

**Tasks, not goroutines.** `task f(x)` starts a task. Tasks run in parallel and are preempted, and
communicate through channels and `select`. Safe code has no data races: the compiler rejects sharing that
it cannot prove safe (chapter 19), and a `sync.Scope` lets tasks use the variables of the function that
starts them and joins them when its scope ends. A kernel supplies its own scheduler.

**Strings and collections.** A `string` is immutable, holds valid UTF-8 and is reference counted. A slice
is a view and never grows, so there is no `append`: a sequence that grows is a `vec.Vec[T]`, an owner. The
built-in `map[K]V` is a counted reference; `hash.Map` holds owners.

**Checks.** A use of freed heap or arena memory is a panic in hosted builds (generation checks, on by
default). Kernel and freestanding builds run without them. Correct programs behave the same either way.

**For systems code.** `packed` types with bit-level layouts and endianness, `volatile[T]`, `aligned[T, N]`,
`ring[T, N]`, enums, unions (including packed unions for page-table entries), default parameter values,
allocators chosen per call, `//pu:` directives for linkage, interrupts and per-CPU data, assembly and C
interop, and a kernel build mode.

Appendix A of the specification lists every difference from Go.

## Example

```go
package main

import (
	"pluto/fmt"
	"pluto/vec"
)

// Words owns its vector: it moves instead of copying, and the vector is
// freed when a Words goes out of scope.
type Words owner struct {
	list vec.Vec[string]
}

// longest looks at the words and owns nothing.
func longest(words []string) string {
	best := ""
	for _, w := range words {
		if len(w) > len(best) {
			best = w
		}
	}
	return best
}

func main() {
	var w Words
	for _, s := range []string{"owners", "move", "views", "look"} {
		w.list.Push(s)
	}

	// A map is a counted reference: copies share one table.
	counts := make(map[string]int)
	for _, s := range w.list.Slice() {
		counts[s[:1]]++
	}
	fmt.Println(longest(w.list.Slice()), w.list.Len(), len(counts))
}
```

More programs are in `examples/`.

## Building and running

The compiler is written in Go (1.27 or later). The standard library is Plutonium source under `lib/pluto/`,
embedded in the compiler binary.

```bash
go build -o pluto ./cmd/pluto/

pluto run ./examples/07-hello/              # build a hosted program and run it
pluto build -o hello ./examples/07-hello/   # build a linked binary
pluto build -S - ./examples/07-hello/       # print the listing instead
pluto explain E3001                         # explain a compile error
```

Set `PLUTOROOT=$PWD/lib` to use the library in the working tree instead of the embedded one. `pluto build
--help` lists the flags: target and mode (`hosted`, `freestanding`, `kernel`), build tags, the allocator,
optimization level and debug information.

Other commands:

- `pluto fmt [-l] [-w] [-d] [path ...]` formats source files. Plain Go formats exactly as gofmt formats it.
- `pluto test [packages]` runs the `Test` and `Benchmark` functions of `*_test.p` files with
  `pluto/testing` and prints the results in the format of `go test`.
- `pluto lsp` runs the language server on standard input and output, for the extension in `editors/vscode`.

## Where things are

| Path | Contents |
|---|---|
| `docs/spec/language.md` | The language specification: the contents page, and 21 chapters and 7 appendices in `docs/spec/language/` |
| `docs/spec/abi.md`, `memory-model.md`, `debugging.md`, `debug-allocator.md` | The x86-64 binary interface, the memory model, debugging information, and the debug allocator, which refine the specification |
| `docs/roadmap.md` | Every known gap between the specification and the compiler, in priority order |
| `docs/plans/` | Working documents with a Status block that says what the specification supersedes; `archive/` holds the superseded March prototype plans |
| `AGENTS.md` | Contributor guide: pipeline, build and test commands, verification checklist |
| `cmd/pluto/`, `internal/` | The compiler: syntax, type checker, ownership and escape analyses, SSA, optimizer, x86-64 backend, ELF writer, driver, language server |
| `lib/pluto/` | The standard library |
| `examples/`, `bench/`, `testdata/` | Example programs, benchmarks against C, Rust and Go, end-to-end tests |
| `selfhost/`, `tools/` | The compiler's sources translated to Plutonium (toward self-hosting), and development tools |

## Status

The specification (version 0.3) is the single source of truth: it defines the whole language and does not
fall back on Go's specification. The compiler implements most of it and runs on `x86_64-linux` (hosted) and
`x86_64-none` (freestanding and kernel). The places where the compiler still differs from the specification
are listed in [`docs/roadmap.md`](docs/roadmap.md); the largest is the `task` statement, which the compiler
still spells `go`.
Long-term, the compiler is meant to compile itself.

The specification adapts text from The Go Programming Language Specification under CC BY 4.0; appendix G of
the specification and `docs/spec/LICENSE-go` say what the licenses require.
