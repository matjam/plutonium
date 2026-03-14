# Plutonium Language Specification

**Version:** 0.1 (draft)  
**Date:** 2026-03-14

## Overview

Plutonium is a kernel-safe subset of Go designed for systems software: operating system kernels, drivers, boot code, allocators, schedulers, and other code that cannot depend on a managed runtime.

Plutonium source files use the `.pu` extension. All compiler directives use the `//pu:` prefix.

## Goals

- No garbage collector
- No hidden heap allocation  
- No goroutines or channels
- No reflection or runtime type metadata
- No implicit scheduler
- Explicit, predictable, low-level semantics
- Compatible with Go syntax where practical

## File Structure

A Plutonium source file begins with a package declaration, followed by optional import declarations, followed by top-level declarations.

```
package main

import "pluto/os"

func main() {
    os.Exit(0)
}
```

## Directives

All Plutonium compiler directives use the `//pu:` prefix:

| Directive | Meaning |
|---|---|
| `//pu:build <expr>` | Build constraint |
| `//pu:nosplit` | Do not insert stack split check |
| `//pu:noescape` | Pointer arguments do not escape to heap |
| `//pu:noinline` | Do not inline this function |
| `//pu:inline` | Force inline this function |
| `//pu:static` | Force static placement of variable |

## Types

### Primitive Types

| Type | Size | Description |
|---|---|---|
| `bool` | 1 | Boolean |
| `int8` | 1 | Signed 8-bit integer |
| `int16` | 2 | Signed 16-bit integer |
| `int32` | 4 | Signed 32-bit integer |
| `int64` | 8 | Signed 64-bit integer |
| `uint8`, `byte` | 1 | Unsigned 8-bit integer |
| `uint16` | 2 | Unsigned 16-bit integer |
| `uint32` | 4 | Unsigned 32-bit integer |
| `uint64` | 8 | Unsigned 64-bit integer |
| `int` | 8 | Platform-width signed integer |
| `uint` | 8 | Platform-width unsigned integer |
| `uintptr` | 8 | Unsigned integer large enough to hold a pointer |
| `float32` | 4 | IEEE 754 32-bit float |
| `float64` | 8 | IEEE 754 64-bit float |
| `rune` | 4 | Alias for int32; represents a Unicode code point |

### Composite Types

- **Pointers:** `*T` — pointer to T
- **Arrays:** `[N]T` — fixed-size array, stack-allocated
- **Slices:** `[]T` — fat pointer `{ptr *T, len int64, cap int64}`. No implicit heap allocation; backing array must be supplied by the caller.
- **Structs:** `struct { ... }` with field access and embedding
- **Interfaces:** Compile-time dispatch only. No `interface{}` or `any`.

### Forbidden Types

| Type | Reason |
|---|---|
| `chan T` | Requires runtime scheduler |
| `map[K]V` | Requires GC-managed hash table; use `pluto/hash` |
| `interface{}` / `any` | Requires runtime type metadata |

## Memory Model

Stack allocation is the only implicit form of allocation. All other allocation is explicit:

- **Stack:** Default for local variables and function calls
- **Static:** Package-level `var` of non-pointer types, `const`
- **Arena:** Via `pluto/mem.Arena` backed by a user-supplied buffer
- **Custom allocators:** Functions that accept `unsafe.Pointer` parameters

`new(T)` and `make([]T, n)` are compile errors.

## Generics

Type parameters are supported on functions and struct types. They are expanded via compile-time monomorphization — no runtime type information is generated.

```go
func Max[T int32 | int64](a, b T) T {
    if a > b { return a }
    return b
}
```

## Assembly Integration

Functions declared without a body in a `.pu` file are assembly stubs, implemented in a `.s` file in the same package:

```go
// math.pu
func addU64(a, b uint64) uint64
```

```asm
// math_amd64.s
//pu:build amd64

TEXT ·addU64(SB), NOSPLIT, $0-24
    MOVQ a+0(FP), AX
    ADDQ b+8(FP), AX
    MOVQ AX, ret+16(FP)
    RET
```

## Forbidden Constructs

The following constructs are compile errors in Plutonium:

| Construct | Error message |
|---|---|
| `go f()` | goroutines are not permitted |
| `chan T` | channel types are not permitted |
| `select { }` | select is not permitted |
| `map[K]V` | use pluto/hash instead |
| `new(T)` | no implicit heap allocation |
| `make([]T, n)` | no implicit heap allocation |
| `interface{}` / `any` | no runtime type metadata |
| `panic(...)` | no runtime unwinder |
| `recover()` | no runtime unwinder |
| `import "reflect"` | requires runtime metadata |

## Build Tags

Build tags use the `//pu:build` directive with Go 1.17+ boolean syntax:

```
//pu:build amd64
//pu:build amd64 && linux
//pu:build amd64 || arm64
//pu:build !freestanding
```

File name suffixes are also honored: `foo_amd64.pu`, `foo_linux.pu`, `foo_amd64.s`.
