# Plutonium

Plutonium is a kernel-safe subset of Go designed for systems software. It compiles `.pu` source files to relocatable ELF object files, targeting code that cannot depend on a managed runtime: OS kernels, drivers, allocators, schedulers, and bare-metal firmware.

This project was mostly coded using Claude Code's Opus model.

## Why?

Go is one of the most ergonomic systems languages ever designed. Clear syntax, fast compilation, great tooling. But it makes a hard trade-off: the runtime. Goroutines need a scheduler. The GC needs stop-the-world pauses. Strings and slices allocate behind your back. That runtime is the reason you can't write a page fault handler, a network driver, or a boot loader in Go.

Rust solves this problem, but at a cost: borrow checkers, lifetime annotations, trait bounds, turbofish syntax. It's a powerful language, but it's not a *readable* language -- not in the way Go is. If you've spent years writing Go, switching to Rust feels like trading clarity for control.

Plutonium asks: what if you didn't have to choose?

Take Go's syntax and strip out everything that needs a runtime. No GC, no scheduler, no implicit heap allocation. Then add the things systems programmers actually need: packed bitfield types for hardware registers, volatile loads and stores for MMIO, built-in ring buffers, explicit allocation with automatic reference counting, and direct syscall access. The result reads like Go but compiles like C -- a static binary with no libc, no runtime, just your code and the kernel.

The goal is a language suitable for:

- **Kernel development.** Address hardware directly -- packed types with bit-level field layouts, endianness control, volatile I/O. No wrangling required to avoid the runtime because there is no runtime.
- **Predictable real-time code.** No GC pauses, no scheduler preemption, no hidden allocations. If you need a tight timing loop -- game engines, audio processing, control systems -- the compiler won't surprise you.
- **Firmware and bare-metal.** Freestanding build mode, `_start` entry point, static ELF binaries with no dependencies. Just your code on the metal.
- **Userspace systems programs.** Not everything needs a runtime. CLI tools, daemons, embedded databases -- if you want small, predictable binaries that don't carry a GC, Plutonium compiles those too.

Is the compiler optimizing? No, not yet -- it produces correct but unoptimized code. Will it eventually? Probably. Right now the focus is on getting the semantics right. Performance comes after correctness.

Honestly, this started as an experiment -- kicking ideas around with Claude to see if an AI-assisted compiler for a Go-without-the-runtime language could actually work. Surprisingly, it does.

## Status

**v0.1 -- in active development.** The full compilation pipeline (parse -> type-check -> sema -> monomorphize -> IR -> x86_64 codegen -> ELF .o) is functional. Programs compile and run. 18 working examples cover arithmetic, control flow, pointers, generics, printf, packed bitfields, heap allocation, ring buffers, RAII auto-free, and reference counting.

## What Plutonium Keeps From Go

- Package system, imports, visibility rules (`Exported` vs `unexported`)
- Functions, methods (value and pointer receivers), closures
- Structs, embedding, arrays, slices, pointers
- `for`, `if/else`, `switch/case`, `defer`, `:=` short declarations
- Generics with type constraints (monomorphized at compile time)
- Familiar `fmt.Printf` with `%d`, `%x`, `%s`, `%c`, `%p`

Interfaces are partially supported today (compile-time dispatch for non-empty interfaces). Full structural interface support -- Go's implicit satisfaction model where types implement interfaces by convention, not declaration -- is a major goal. The `any` type and related semantics will likely evolve as that work progresses.

## What Plutonium Adds

| Feature | Description |
|---|---|
| `packed` types | Bit-level field layout for hardware registers with endianness control |
| `volatile[T]` | Every read/write is exactly one load/store, never elided or reordered |
| `aligned[T, N]` | Guarantees N-byte alignment for DMA buffers, SIMD, page tables |
| `ring[T, N]` | Built-in lock-free ring buffer with compiler-inlined Push/Pop/Len |
| `bits[V]` lens | Zero-cost bit-addressing over existing variables (Set/Clear/Test/Toggle) |
| `alloc.New[T]()` | Explicit heap allocation via slab allocator with auto reference counting |
| RAII auto-free | Compiler inserts `decRef` at scope exit; no manual free required |
| `any` tagged union | 16-byte `{typeID, data}` struct for type-safe variadic arguments |
| `unsafe` package | `Sizeof`, `Alignof`, pointer casts -- typed, not opaque |

## What Plutonium Rejects

The compiler enforces kernel safety at compile time:

| Forbidden construct | Reason |
|---|---|
| `go f()` | No scheduler |
| `chan T` | No scheduler |
| `select {}` | No scheduler |
| `map[K]V` | Requires GC; use `pluto/hash` |
| `new(T)` | No implicit heap allocation; use `alloc.New[T]()` |
| `make([]T, n)` | No implicit heap allocation; use explicit allocators |
| `interface{}` | No runtime type metadata yet; full structural interfaces are planned |
| `panic(...)` | No runtime unwinder |
| `recover()` | No runtime unwinder |
| `import "reflect"` | Requires runtime metadata |

## Example

```go
package main

import "pluto/fmt"

type ControlReg packed uint32 {
    mode     bits[uint, 4]
    enable   bits[bool, 1]
    priority bits[uint, 3]
}

func fib(n int64) int64 {
    a := int64(0)
    b := int64(1)
    for i := int64(2); i <= n; i++ {
        next := a + b
        a = b
        b = next
    }
    return b
}

func main() int64 {
    // Hardware register with bit-level field access
    var reg ControlReg
    reg.mode = 5
    reg.enable = true
    reg.priority = 7
    fmt.Printf("mode=%d enable=%d priority=%d\n", reg.mode, reg.enable, reg.priority)

    // Familiar Go-style code
    for i := int64(1); i <= 5; i++ {
        fmt.Printf("fib(%d) = %d\n", i, fib(i))
    }
    return 0
}
```

## Building the Compiler

Requires Go 1.21 or later.

```bash
go build -o pluto ./cmd/pluto/
```

## Usage

```bash
# Build a package to a linked binary
pluto build -o output ./path/to/package/

# Write the object file instead of linking, with build tags
pluto build -c --tags freestanding -o kernel.o ./kernel/

# Build a hosted program and run it
pluto run ./path/to/package/ -- args...

# Format source files the way gofmt formats Go
pluto fmt -l .            # list files whose formatting differs
pluto fmt -w ./lib/       # rewrite them in place
pluto fmt -d main.p       # show the changes as a diff

# Run tests and benchmarks
pluto test ./mypkg/
pluto test -v -run 'TestParse/empty' ./mypkg/
pluto test -run '^$' -bench . -count 3 ./...

# Print version
pluto version
```

### pluto fmt

`pluto fmt [-l] [-w] [-d] [path ...]` formats Plutonium source exactly as
gofmt formats Go, including the Plutonium-only syntax (packed types, bits
groups, type constructors such as `volatile[T]` and `ring[T, N]`), and
normalises `//pu:build` lines like `//go:build`. A directory is walked for
`.p` files; with no paths, or `-`, it formats standard input. `-l` lists files
that differ, `-w` rewrites them, `-d` prints a unified diff (and exits with
status 1 if there was one). Syntax errors exit with status 2.

### pluto test

Tests live in `*_test.p` files in the package they test, and use
`pluto/testing`:

```go
package strings

import "pluto/testing"

func TestIndex(t *testing.T) {
	if got := Index("chicken", "ken"); got != 4 {
		t.Errorf("Index = %d, want 4", got)
	}
	t.Run("empty", func(t *testing.T) {
		if Index("", "") != 0 {
			t.Fatal("empty string not found in itself")
		}
	})
}

func BenchmarkIndex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Index("chicken", "ken")
	}
}
```

`pluto test [-run regexp] [-bench regexp] [-v] [-count n] [-O level] [packages]`
builds one test binary per package (the package's files plus its `_test.p`
files, and a generated `main`), runs it, and prints the results as `go test`
does: `--- FAIL: TestX`, `ok  pkg 0.01s`, `FAIL`. The exit status is 1 if a
package fails to build or a test fails. Packages are directories, standard
library paths (`pluto/strings`), or either followed by `/...`. Benchmarks run
only with `-bench`. Below the top level, `-run` patterns are literal text with
optional `^` and `$` anchors.

`testing.T` has `Error`, `Errorf`, `Fatal`, `Fatalf`, `Log`, `Logf`, `Fail`,
`FailNow`, `Failed`, `Skip`, `Skipf`, `SkipNow`, `Helper`, `Name` and `Run`;
`testing.B` adds `N`, `ResetTimer`, `StartTimer`, `StopTimer` and
`ReportAllocs`. Messages are formatted with `pluto/fmt` into fixed buffers,
so testing allocates nothing; they carry no file:line prefix. Since `panic`
aborts, a test that panics ends the binary: each test is announced before it
runs, and `pluto test` reports the panic as a failure of that test. `FailNow`
returns to the test runner directly, so the test function's deferred calls do
not run. Tests must be in the package itself; external `_test` packages are
not supported, and a package that `pluto/testing` depends on (such as
`pluto/fmt`) cannot be tested this way.

## Project Structure

```
cmd/pluto/          # Compiler CLI (pluto build, pluto compile, pluto version)
internal/
  ast/              # Abstract syntax tree node types
  buildtag/         # //pu:build constraint parsing and evaluation
  codegen/          # Codegen interface and x86_64 backend
    x86_64/
      encoder/      # x86_64 instruction encoding
      regalloc/     # Linear scan register allocator
  config/           # Compiler configuration (target, tags)
  diag/             # Diagnostic message formatting
  ir/               # Plutonium IR: types, instructions, basic blocks
  mono/             # Generic monomorphization pass
  obj/elf/          # ELF64 relocatable object file writer
  parser/           # .pu source parser (produces AST)
  scanner/          # Lexical scanner (produces tokens)
  sema/             # Kernel-safety validation pass
  token/            # Token types and file position tracking
  types/            # Type checker and type system
pluto/              # Standard library (.pu source)
  alloc/            # Slab allocator with reference counting
  bytes/            # Byte slice utilities
  fmt/              # Formatted output (Printf, PrintInt, Println)
  io/               # I/O primitives (Reader, Writer interfaces)
  mem/              # Arena bump-pointer allocator
  os/               # OS interface (write, exit)
  strings/          # String utilities (Len, Equal, Contains, TrimSpace)
  syscall/          # Raw Linux syscall wrappers
docs/
  spec/
    language.md     # Language specification
    abi.md          # x86_64 ABI and calling convention
examples/           # 18 working examples (exit through strings)
testdata/
  programs/hello/   # End-to-end pipeline test
  invalid/          # Programs that must be rejected
```

## Long-Term Vision

Plutonium is intended to be self-hosting: the compiler itself will be written in Plutonium and compiled by a prior version. The roadmap:

- **v0.1:** Full compilation pipeline to ELF .o; all forbidden constructs rejected at compile time
- **v0.2:** Linker integration; `pluto/os` fully compiled; hello world binary runs on Linux x86_64
- **v0.3:** Self-hosting bootstrap; compiler compiled by itself
- **v1.0:** Production-ready for kernel and firmware development

## Language Specification

See [`docs/spec/language.md`](docs/spec/language.md) for the full language specification.

See [`docs/spec/abi.md`](docs/spec/abi.md) for the x86_64 ABI and calling convention.
