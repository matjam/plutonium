# Plutonium

Plutonium is a kernel-safe subset of Go designed for systems software. It compiles `.pu` source files to relocatable ELF object files, targeting code that cannot depend on a managed runtime: OS kernels, drivers, allocators, schedulers, and bare-metal firmware.

## Status

**v0.1 — in active development.** The full compilation pipeline (parse → type-check → sema → monomorphize → IR → x86_64 codegen → ELF .o) is functional. Linking to a runnable binary requires the `pluto/os` package to be self-hosted, which is the v0.2 milestone.

## What Plutonium Rejects

The compiler enforces kernel safety at compile time:

| Forbidden construct | Reason |
|---|---|
| `go f()` | No scheduler |
| `chan T` | No scheduler |
| `select {}` | No scheduler |
| `map[K]V` | Requires GC; use `pluto/hash` |
| `new(T)` | No implicit heap allocation |
| `make([]T, n)` | No implicit heap allocation |
| `interface{}` / `any` | No runtime type metadata |
| `panic(...)` | No runtime unwinder |
| `recover()` | No runtime unwinder |
| `import "reflect"` | Requires runtime metadata |

## Building the Compiler

Requires Go 1.21 or later.

```bash
go build -o pluto ./cmd/pluto/
```

## Usage

```bash
# Compile a single source file to an object file
pluto compile -o main.o main.pu

# Compile with build tags
pluto compile --tags freestanding -o kernel.o kernel.pu

# Compile a package directory
pluto build -o output.o ./path/to/package

# Print version
pluto version
```

## Example Program

```go
package main

func main() {
    msg := "hello, plutonium\n"
    writeString(1, msg)
    exit(0)
}

func writeString(fd int64, s string) {
    _ = fd
    _ = s
}

func exit(code int64) {
    _ = code
}
```

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
  bytes/            # Byte slice utilities
  fmt/              # Formatted output
  io/               # I/O primitives
  mem/              # Arena allocator
  os/               # OS interface (write, exit)
  strings/          # String utilities
  syscall/          # Raw Linux syscall wrappers
docs/
  spec/
    language.md     # Language specification
    abi.md          # x86_64 ABI and calling convention
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
