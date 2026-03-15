# `any` Type, Variadic Arguments, and `fmt.Printf` Design

**Date:** 2026-03-15
**Status:** Approved

---

## Overview

This design adds three tightly coupled features to Plutonium:

1. **`any` type** — a tagged fat pointer for type-safe value erasure without reflection
2. **Variadic arguments** — `...any` passed as a stack-allocated slice
3. **`fmt.Printf`** — full POSIX + Go-extended format string processing

These features enable `fmt.Printf("x=%d y=%v\n", x, y)` to work in a kernel-safe language with no GC, no heap allocation, and no runtime reflection.

---

## `any` Type

### Representation

```
any = struct { typeID uint64; data uint64 }
```

16 bytes on 64-bit platforms. No heap allocation, no pointer indirection for small types.

### Type IDs

Compile-time-assigned constants. Not configurable. Stable across compilations.

| ID | Type | data field |
|---|---|---|
| 0 | nil | 0 |
| 1 | bool | 0 or 1 |
| 2 | int8 | sign-extended to int64 |
| 3 | int16 | sign-extended to int64 |
| 4 | int32 | sign-extended to int64 |
| 5 | int64 / int | value |
| 6 | uint8 / byte | zero-extended to uint64 |
| 7 | uint16 | zero-extended to uint64 |
| 8 | uint32 | zero-extended to uint64 |
| 9 | uint64 / uint / uintptr | value |
| 10 | float32 | IEEE 754 bits as uint64 |
| 11 | float64 | IEEE 754 bits as uint64 |
| 12 | pointer (*T) | pointer value as uint64 |
| 13 | string | pointer to {ptr *byte, len int64} on stack |

Small values (bool, integers, floats, pointers) are stored inline in the `data` field — no indirection. Strings store a pointer to a stack-allocated string header.

### Conversion to `any`

When the compiler encounters a value being passed to an `any` parameter (or `...any` variadic), it generates a monomorphized pack at the call site:

```go
// User writes:
fmt.Printf("%d", x)   // x is int64

// Compiler generates:
var _arg0 any
_arg0.typeID = 5       // TYPE_INT64
_arg0.data = uint64(x)
// pass &_arg0 as part of the variadic slice
```

No runtime type lookup. No allocation. The compiler knows the source type.

### Type switch on `any`

```go
switch v.typeID {
case 5:  // int64
    n := int64(v.data)
    // ...
case 1:  // bool
    b := v.data != 0
    // ...
}
```

Compiles to integer comparisons. No reflection.

### What `any` is NOT

- Not an interface with method dispatch — `any` has no methods
- Not reflectable — no `reflect.TypeOf`, no `reflect.ValueOf`
- Not extensible at runtime — the type ID set is fixed at compile time
- Not a replacement for named interfaces — `io.Reader` is still compile-time dispatch

---

## Variadic Arguments

### Syntax

```go
func Printf(format string, args ...any)
```

Same syntax as Go. `...any` means "zero or more values of any type".

### Calling convention

At the call site, the compiler:
1. Allocates `[N]any` on the stack (N = number of variadic args)
2. Packs each argument into its `any` slot (type ID + data)
3. Constructs a slice header `{ptr: &array[0], len: N, cap: N}`
4. Passes the slice as the last argument to the function

The function receives `args` as `[]any` — a 24-byte fat pointer (ptr, len, cap).

### Stack layout at call site

```
          ┌─────────────┐
  RSP →   │ arg0.typeID │  any[0]
          │ arg0.data   │
          │ arg1.typeID │  any[1]
          │ arg1.data   │
          │ arg2.typeID │  any[2]
          │ arg2.data   │
          └─────────────┘
```

The slice header points to `RSP` with `len=3, cap=3`.

### Non-variadic call with explicit slice

```go
var args [2]any
args[0] = any{typeID: 5, data: uint64(42)}
args[1] = any{typeID: 5, data: uint64(17)}
fmt.Printf("%d %d\n", args[:]...)
```

Both forms compile to the same code.

---

## `fmt.Printf` Format Specification

### Format string syntax

```
%[flags][width][.precision]verb
```

### Flags

| Flag | Meaning |
|---|---|
| `-` | Left-justify within field width |
| `0` | Pad with zeros instead of spaces (integers only) |
| `+` | Always print sign for numeric values |
| ` ` | Space before positive numbers (ignored if `+` present) |
| `#` | Alternate form: `0x`/`0X` for hex, `0o` for octal, `0b` for binary, force decimal point for floats |

### Width

- Numeric literal: `%10d` — minimum field width 10
- `*` — width from next `any` argument (must be integer type)

### Precision

- `.N` — precision N
- `.*` — precision from next `any` argument (must be integer type)
- For integers: minimum number of digits (zero-padded)
- For floats: digits after decimal point (default 6 for `%f`, shortest for `%g`)
- For strings: maximum bytes to output
- For `%g`/`%G`: maximum significant digits

### Verbs

**Integers:**

| Verb | Output | Example |
|---|---|---|
| `%d` | Decimal | `42`, `-17` |
| `%x` | Hex lowercase | `2a` |
| `%X` | Hex uppercase | `2A` |
| `%o` | Octal | `52` |
| `%b` | Binary | `101010` |
| `%c` | Character (byte value) | `*` |

**Floats:**

| Verb | Output | Example |
|---|---|---|
| `%f` | Decimal notation | `3.141593` |
| `%e` | Scientific lowercase | `3.141593e+00` |
| `%E` | Scientific uppercase | `3.141593E+00` |
| `%g` | Shortest of `%e` or `%f` | `3.14159` |
| `%G` | Shortest of `%E` or `%f` | `3.14159` |

**Strings/Bytes:**

| Verb | Output | Example |
|---|---|---|
| `%s` | Plain string | `hello` |
| `%q` | Go-quoted string | `"hello\n"` |

**Pointers:**

| Verb | Output | Example |
|---|---|---|
| `%p` | Hex with 0x prefix | `0x7ffd5e8c3a40` |

**General:**

| Verb | Output |
|---|---|
| `%v` | Default format based on type ID |
| `%T` | Type name string |
| `%%` | Literal `%` |

### `%v` defaults

| Type | `%v` equivalent |
|---|---|
| bool | `true` / `false` |
| int8..int64 | `%d` |
| uint8..uint64 | `%d` |
| float32/64 | `%g` |
| string | `%s` |
| pointer | `%p` |

### `%T` output

Produces the type name: `bool`, `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`, `uint64`, `float32`, `float64`, `string`, `*<type>`.

---

## Float Formatting: Ryu Algorithm

Float-to-string conversion uses the Ryu algorithm (Ulf Adams, 2018). Ryu produces the shortest decimal representation that round-trips correctly for both float32 and float64.

### Why Ryu

- Fastest known algorithm (no bignum arithmetic)
- Produces shortest accurate output
- Deterministic — no allocation, fixed-size lookup tables
- Used by Go's stdlib (`strconv.FormatFloat`) and Rust's stdlib
- Well-documented with reference C implementation

### Implementation plan

Port the Ryu reference implementation to Plutonium. The algorithm uses:
- Pre-computed power-of-5 tables (~1KB for float64)
- 128-bit multiplication (via two 64-bit multiplies)
- No division (only multiplies and shifts)
- No heap allocation

The tables are stored in `.rodata` as package-level constants.

### Ryu output modes

- **Shortest**: for `%v`, `%g` — produces shortest representation that round-trips
- **Fixed**: for `%f` — fixed number of decimal places
- **Scientific**: for `%e`/`%E` — scientific notation

---

## Implementation in Plutonium Source

### Package structure

```
pluto/fmt/
  fmt.pu                    ← Printf, Sprintf, Fprintf, Print, Println
  format.pu                 ← format string parser and dispatcher
  integer.pu                ← integer formatting (%d, %x, %o, %b)
  float.pu                  ← float formatting (%f, %e, %g) — Ryu
  float_tables.pu           ← Ryu lookup tables
  string.pu                 ← string formatting (%s, %q)
  write_linux_amd64.s       ← raw write(2) syscall stub
```

### Public API

```go
package fmt

// Printf formats according to a format string and writes to stdout.
func Printf(format string, args ...any)

// Fprintf formats and writes to a file descriptor.
func Fprintf(fd int64, format string, args ...any)

// Sprintf formats into a caller-supplied byte buffer.
// Returns the number of bytes written.
func Sprintf(buf []byte, format string, args ...any) int64

// Print writes the default format of each arg to stdout, separated by spaces.
func Print(args ...any)

// Println writes the default format of each arg to stdout, separated by spaces,
// followed by a newline.
func Println(args ...any)
```

### Internal design

The format engine is a state machine that walks the format string byte-by-byte:

```
State: LITERAL → saw '%' → FLAGS → WIDTH → PRECISION → VERB → emit
```

Each verb dispatches to a type-specific formatter based on the `any` value's type ID. The formatter writes into a stack-allocated scratch buffer, then flushes to the output.

### Memory model

- **No heap allocation** — all scratch buffers are stack-allocated
- **Maximum field width** — 256 bytes (stack buffer limit; wider fields are truncated)
- **Ryu tables** — static `.rodata` (~1KB)
- **Output** — direct `write(2)` syscall; Sprintf writes to caller's buffer

---

## Compiler Changes Required

### 1. `any` as a built-in type

- Add `TyAny` to `internal/types/types.go` — resolves to a 16-byte struct
- Add `any` to the universe scope in `universe.go` as a type (remove the current rejection)
- In the type checker, allow `any` as a parameter type and in variadic positions

### 2. Implicit conversion to `any`

- When assigning or passing a typed value to an `any` parameter, the compiler inserts a pack operation
- In `internal/ir/lower.go`, detect `any` target type and emit:
  - `store typeID` to `any.typeID`
  - `store data` to `any.data` (with appropriate conversion for the source type)

### 3. Variadic lowering

- In `internal/ir/lower.go`, when calling a variadic function:
  - Allocate `[N]any` on the stack
  - Pack each variadic argument
  - Construct a slice header `{ptr, len, cap}`
  - Pass the slice as the last parameter

### 4. Slice support in codegen

- `[]any` is `{ptr *any, len int64, cap int64}` = 24 bytes
- Need: load slice length, index into slice, iterate

### 5. String support

- `string` is `{ptr *byte, len int64}` = 16 bytes
- Need: load string length, index into string bytes
- Format string parsing iterates byte-by-byte

---

## Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Keyword | `any` (not `interface{}`) | Modern Go convention, cleaner syntax |
| Representation | `{typeID uint64, data uint64}` | 16 bytes, no indirection for small types |
| Type IDs | Fixed compile-time integers | Deterministic, no runtime tables |
| Variadic | Stack-allocated `[]any` slice | Same as Go, predictable, no heap |
| Float algorithm | Ryu | Fastest, shortest output, no allocation |
| Output buffer | Stack-allocated (256 bytes max) | No heap, bounded stack usage |
| Printf implementation | In Plutonium (`.pu` source) | Self-hosting path, not compiler intrinsic |
