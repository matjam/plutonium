# Plutonium Compiler Design

> **Historical:** superseded by the specification; see chapter 1 (Introduction), chapter 20 (System considerations) and chapter 21 (Implementation requirements) of `docs/spec/language.md`. This is the design of the March 2026 prototype: a freestanding subset of Go, with ownership inferred by the compiler and the library as it was then. The language described in it is not the language the specification defines (for example, memory is owned by types that say so, tasks replace goroutine-style `go` statements, and the standard library is Plutonium source under `lib/pluto/`). The specification is the single source of truth: where this document and the specification differ, the specification wins. This document is kept for the history of the March 2026 prototype and is not maintained.

**Date:** 2026-03-14  
**Status:** Approved

---

## Overview

Plutonium is a freestanding, kernel-safe subset-of-Go compiler. It preserves Go's core syntax and developer ergonomics while rejecting all features that require a managed runtime. The compiler produces predictable low-level code suitable for operating system kernels, drivers, boot code, allocators, schedulers, and other systems software.

The goal is not to compile all Go. The goal is to define and implement a kernel-safe subset of Go. Ordinary Go is treated as inspiration; the language enforces a strict freestanding subset with explicit, predictable, low-level semantics.

The compiler is called **Plutonium**. The command is `pluto`. Source files use the `.pu` extension. All directives use the `//pu:` prefix.

---

## Long-Term Vision

1. **Phase 1:** Write Plutonium in idiomatic Go. Define the language, build the pipeline, compile real x86_64 ELF binaries on Linux. The compiler output is kernel-safe; the compiler itself uses whatever Go stdlib it needs.
2. **Phase 2:** Grow the `pluto` standard library to cover what a compiler needs. Port or transpile the compiler source into `.pu`. Compile it with itself — self-hosted.
3. **Phase 3:** Use the self-hosted compiler to build a kernel and OS. The OS runs its own compiler natively.

Phase 1 imposes only architectural discipline toward self-hosting: clean internal structure, no deep coupling to Go runtime semantics in data structure design, stdlib usage isolated at boundaries.

---

## Language Specification

### What Plutonium Supports

**Primitive types:**
- Integer: `int8`, `int16`, `int32`, `int64`, `int`, `uint8`, `uint16`, `uint32`, `uint64`, `uint`, `uintptr`
- Float: `float32`, `float64`
- `bool`, `byte`, `rune`

**Composite types:**
- Pointers: `*T`, `**T`, unsafe pointer arithmetic via `unsafe.Pointer` + `uintptr`
- Arrays: `[N]T` — fixed size, stack-allocated
- Slices: `[]T` — fat pointer struct `{ ptr, len, cap }` only; no implicit heap allocation; backing array must come from the caller
- Structs: `struct { ... }` with field access and embedding
- Named types and type aliases
- Function types and first-class functions
- Closures: only where captured variables do not escape to the heap
- Interfaces: compile-time dispatch only; no runtime type assertions; no `interface{}` / `any`
- Generics: type parameters on functions and structs, monomorphized at compile time
- `unsafe` package: `Pointer`, `Sizeof`, `Alignof`, `Offsetof`

**Statements:**
- `var`, `:=`, `const`, `iota`
- `if / else if / else`
- `for` (C-style, range over array/slice, infinite)
- `switch` (expression switch; type switch forbidden for interface types)
- `return`, `break`, `continue`, `goto`, `fallthrough`
- `defer` — stack-managed only; no hidden closure heap allocation

**Functions:**
- Regular functions, methods on named types
- Variadic functions
- Multiple return values
- Named return values
- `//pu:nosplit`, `//pu:noescape`, `//pu:noinline`, `//pu:inline` pragmas
- Assembly stubs: function declared in `.pu` with no body, implemented in a `.s` file

**Memory:**
- Stack allocation is the default and only implicit allocation
- Static storage: package-level `var` of non-pointer types, `const`
- `//pu:static` to force static placement
- All dynamic allocation is explicit via user-supplied allocator functions or arenas

**Build system:**
- `//pu:build` tags (same boolean syntax as Go 1.17+)
- File suffix conventions: `foo_amd64.pu`, `foo_amd64.s`
- `package` declarations, `import` paths

### What Plutonium Rejects (compile-time errors)

| Forbidden construct | Reason |
|---|---|
| `go` keyword (goroutines) | requires scheduler |
| `chan`, `<-`, `select` | requires runtime |
| `make(chan ...)` | requires runtime |
| `map[K]V` built-in | requires GC-managed hash table; use `pluto/hash` instead |
| `new(T)` / `make([]T, n)` | implicit heap allocation |
| Closures capturing heap-escaped vars | hidden allocation |
| `interface{}` / `any` | requires runtime type metadata |
| Type assertions `x.(T)` on interfaces | requires runtime |
| `reflect` package import | requires runtime metadata |
| `panic` / `recover` | requires runtime unwinder |
| String concatenation `+` on non-const strings | may allocate |
| `append` without explicit backing | may allocate |

**Note on maps:** `map[K]V` is not a language builtin in Plutonium. Hash maps are provided as explicit library types in `pluto/hash`, backed by user-supplied allocators. Similarly, other data structures (sets, trees, etc.) live in the `pluto` standard library rather than the language.

### Directives

All Plutonium directives use the `//pu:` prefix in both `.pu` and `.s` files:

| Directive | Meaning |
|---|---|
| `//pu:build <expr>` | build tag constraint |
| `//pu:nosplit` | do not insert stack split check |
| `//pu:noescape` | pointer args do not escape |
| `//pu:noinline` | do not inline this function |
| `//pu:inline` | force inline this function |
| `//pu:static` | force static placement of variable |

---

## Compiler Architecture

### Approach

Plutonium uses Approach A: a clean from-scratch pipeline in Go, modeled on Go's `go/ast` architecture but stripped of runtime-dependent constructs. The backend emits x86_64 machine code via a pure-Go instruction encoder and writes ELF relocatable object files directly, with no dependency on LLVM or any external library beyond the Go standard library and `cobra`.

Go standard library code is reused or adapted where appropriate (BSD licensed):
- `go/token` → basis for token types and position tracking
- `go/scanner` → basis for the lexer
- `debug/elf` → ELF structure definitions and constants
- `encoding/binary` → binary encoding for object file emission
- `golang.org/x/arch/x86/x86asm` → x86_64 instruction definitions as encoding reference

### Pipeline

```
Source (.pu files + .s files)
         │
         ▼
    [ Scanner / Lexer ]
    Tokenizes .pu source. Token stream with file/line/col.
    Adapted from go/scanner. Handles //pu: directives.
         │
         ▼
    [ Parser ]
    Recursive-descent. Produces a typed AST.
    Rejects forbidden syntax at parse time where possible
    (go, chan, select keywords → immediate error).
    Handles generics syntax (type parameters, constraints).
         │
         ▼
    [ Semantic Analysis ]
    - Name resolution (scopes, imports, package-level decls)
    - Type checking (bidirectional, handles generics)
    - Kernel-safety validation:
        * Reject new/make(heap forms)
        * Reject interface{}/any
        * Reject closures with heap-escaped captures
        * Reject reflect imports
        * Reject panic/recover
        * Reject forbidden builtins
    - Constant folding
         │
         ▼
    [ Monomorphization Pass ]
    Instantiates generic functions/types for each concrete
    type argument set found in the program. Produces a
    fully-concrete AST with no remaining type parameters.
         │
         ▼
    [ IR Lowering ]
    Walks the concrete AST, emits Plutonium IR.
    - Stack allocations → alloca instructions
    - Slice fat pointers → { ptr, len, cap } struct
    - Struct layout follows Go ABI
    - defer lowered to inline cleanup sequences
    - Assembly stubs → external symbol references
         │
         ▼
    [ x86_64 Code Generator ]
    Converts Plutonium IR → x86_64 instructions.
    Pure-Go instruction encoder (x86asm tables as reference).
    Linear scan register allocator (v0.1).
         │
         ▼
    [ ELF Object Writer ]
    Pure-Go ELF .o writer using debug/elf layout.
    Emits relocatable object files.
    .s assembly files passed to system assembler (as/gas).
         │
         ▼
    [ Linker Invocation ]
    pluto invokes ld/lld with user-supplied linker script.
    Output format: elf (default), binary, pe32+
    Raw binary output (e.g. kernel8.img) via objcopy.
```

### IR Design

The Plutonium IR is a typed, flat, register-transfer language. It is not SSA in v0.1 (SSA can be added later as an optimization pass).

**Structure:**
```
Program → []Package → []Function → []BasicBlock → []Instruction
```

Each basic block ends with exactly one terminator instruction.

**Types:**
```
i8, i16, i32, i64
u8, u16, u32, u64
f32, f64
bool
ptr<T>
array<T, N>
slice<T>          ← { ptr<T>, i64, i64 }
struct<...fields>
func<...params, ret>
void
```

**Instructions (representative):**
```
%slot = alloca <type>
%v    = load <type> <ptr>
        store <type> <val> → <ptr>
%v    = add|sub|mul|div|rem|and|or|xor|shl|shr <type> <a> <b>
%v    = cmp <eq|ne|lt|le|gt|ge> <type> <a> <b>
%v    = extend|truncate|bitcast <from> <to> <val>
%v    = ptrtoint <ptr> → u64
%v    = inttoptr <u64> → ptr<T>
%v    = gep <type> <base> <index...>
%v    = extractfield|insertfield <struct> <field> [<val>]
%v    = call <func> <args...>
        asm "<template>" <constraints> <args...> → <type>
; Terminators:
        br <label>
        condbr <cond> <true> <false>
        ret [<type> <val>]
        unreachable
```

### Backend Interface

Defined in `internal/codegen/codegen.go`:

```go
type Backend interface {
    Name()       string
    Triple()     string
    Compile(fn *ir.Function) (*obj.ObjectFile, error)
    WordSize()   int
    Endianness() binary.ByteOrder
}
```

Each architecture implements this interface in its own package. The frontend never imports a backend package directly; the CLI selects the backend by target triple.

---

## Object File and Output Format

Plutonium always emits relocatable ELF `.o` files from the compiler. The final output format is determined at link time:

| Target | Output format | Method |
|---|---|---|
| x86_64 Linux kernel/binary | ELF | ld/lld + linker script |
| aarch64 bare metal (Raspberry Pi) | flat binary image | ld/lld + linker script + objcopy |
| UEFI | PE32+ | lld with PE target |

The user supplies the linker script. Plutonium invokes the linker with the appropriate flags.

---

## Assembly File Integration

Assembly files follow Go's Plan 9 assembly model. A `.pu` file declares a function with no body; the implementation lives in a `.s` file in the same package directory:

```
// math.pu
func addU64(a, b uint64) uint64   // no body = assembly stub

// math_amd64.s
//pu:build amd64

TEXT ·addU64(SB), NOSPLIT, $0-24
    MOVQ a+0(FP), AX
    ADDQ b+8(FP), AX
    MOVQ AX, ret+16(FP)
    RET
```

Build tag evaluation happens before assembly. Both `.pu` and `.s` files use `//pu:build` tags. File suffix conventions (`_amd64.s`, `_arm64.s`) are also honored.

---

## CLI Design

Built with Cobra, mirroring `go` command structure.

### Commands

```
pluto build     compile + link a package or file
pluto compile   compile only, emit .o files
pluto assemble  assemble .s files to .o
pluto link      link .o files to final binary/image
pluto run       build + execute (hosted mode only)
pluto fmt       format .pu source files
pluto version   print compiler version + target info
```

### Key Flags (`pluto build`)

```
-o <file>          output file
-target <triple>   e.g. x86_64-linux, aarch64-none
-mode <mode>       hosted (default) | freestanding
-opt <level>       0, 1, 2, s
-tags <list>       build tags, comma-separated
-ldscript <file>   linker script (freestanding mode)
-format <fmt>      elf (default) | binary | pe32+
-v                 verbose
-S                 emit assembly instead of object files
```

### Example Invocations

```bash
# Hosted Linux binary
pluto build -o myapp ./cmd/myapp

# Freestanding x86_64 kernel ELF
pluto build -target x86_64-linux -mode freestanding \
            -ldscript kernel.ld -format elf -o kernel.elf ./kernel

# Raspberry Pi flat binary
pluto build -target aarch64-none -mode freestanding \
            -ldscript rpi.ld -format binary -o kernel8.img ./kernel

# Compile only
pluto compile -o foo.o ./foo.pu
```

---

## Repository Layout

```
plutonium/
  cmd/
    pluto/
      main.go                   ← cobra root, subcommand registration

  internal/
    token/                      ← token types, positions (adapted from go/token)
    scanner/                    ← lexer (adapted from go/scanner)
    ast/                        ← AST node types
    parser/                     ← recursive descent parser
    types/                      ← type system, type checker
    sema/                       ← semantic analysis, kernel-safety validation
    mono/                       ← monomorphization pass
    ir/                         ← Plutonium IR types and builder
    codegen/
      codegen.go                ← Backend interface definition
      x86_64/                   ← x86_64 backend
        encoder/                ← x86_64 instruction encoder
        regalloc/               ← linear scan register allocator
    obj/
      elf/                      ← ELF .o writer
    link/                       ← linker invocation, output format handling
    asm/                        ← .s file handling, build tag evaluation
    buildtag/                   ← build tag parser and evaluator
    diag/                       ← diagnostics, error reporting, source locations
    config/                     ← build configuration (target, mode, tags)

  pluto/                        ← freestanding standard library
    syscall/
      syscall.pu
      syscall_amd64.pu          ← x86_64 syscall numbers + wrappers
      syscall_amd64.s           ← raw SYSCALL instruction stubs
    os/
      os.pu                     ← open, read, write, close, exit, args
    io/
      io.pu                     ← Reader, Writer interface types
    fmt/
      fmt.pu                    ← formatting (integer, string, hex; no heap Sprintf)
    mem/
      mem.pu                    ← arena allocator, slab allocator
    strings/
      strings.pu                ← string operations, no hidden alloc
    bytes/
      bytes.pu                  ← byte slice utilities

  docs/
    plans/                      ← design documents
    spec/
      language.md               ← language specification
      ir.md                     ← IR specification
      abi.md                    ← ABI specification

  testdata/
    programs/                   ← end-to-end test programs
      hello/
      freestanding/
    invalid/                    ← programs that must be rejected with specific errors

  go.mod
  go.sum
  README.md
```

---

## Standard Library Phasing

### Phase 1a — enough to compile and run useful programs

```
pluto/syscall     raw Linux x86_64 syscalls
pluto/os          open, read, write, close, stdout, stderr, exit, args
pluto/io          Reader, Writer types
pluto/fmt         integer + string formatting to a Writer, no alloc
pluto/mem         arena allocator
pluto/strings     basic string operations
pluto/bytes       byte slice operations
```

### Phase 1b — enough to write the compiler in Plutonium (self-hosting)

```
pluto/os          + stat, mkdir, glob, env
pluto/io          + buffered reader/writer
pluto/fmt         + full formatting
pluto/mem         + slab allocator, general allocator interface
pluto/hash        + hash map backed by explicit allocator
pluto/path        + path manipulation
pluto/sort        + sort (no alloc)
pluto/unicode     + UTF-8 utilities
```

### Freestanding mode

Under `//pu:build freestanding`, `pluto/os` and `pluto/syscall` are compile errors. Only the following are available:

```
pluto/mem
pluto/fmt         (requires user-supplied Writer)
pluto/strings
pluto/bytes
pluto/hash
pluto/sort
pluto/unicode
```

---

## Key Design Decisions Summary

| Decision | Choice | Rationale |
|---|---|---|
| Source extension | `.pu` | Unambiguous, no conflict with Go tooling |
| Directive prefix | `//pu:` | Clean namespace, consistent across `.pu` and `.s` files |
| Backend | Pure-Go x86_64 encoder + ELF writer | Zero runtime dependencies, single static binary |
| LLVM | Not used | Removes large dependency, compiler is fully self-contained |
| IR type | Flat register-transfer, not SSA | Simpler for v0.1; SSA added later as optimization pass |
| Generics | Monomorphization at compile time | No runtime type metadata required |
| Maps | Library type in `pluto/hash` | Not a language builtin; explicit allocator-backed |
| Assembly | Plan 9 `.s` model with `//pu:build` tags | Familiar to Go systems programmers |
| Object format | ELF `.o` (always) | Final image format is a linker concern |
| Stdlib | Freestanding/hosted split via build tags | Same source tree, different syscall layer |
| Self-hosting | Future phase, designed toward | Phase 1 compiler written in idiomatic Go |
