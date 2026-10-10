# Plutonium x86-64 ABI

**Version:** 0.2 (draft)
**Date:** 2026-10-09

This document describes how Plutonium code is laid out and called on x86-64, as implemented by the
compiler in `internal/lower` (signature lowering, data layout, symbols) and `internal/arch/amd64`
(registers, frames, encoding). Sections marked **planned** or **proposed** describe behaviour the
compiler does not have yet.

There are two calling conventions:

- the **internal ABI**, used for every call between Plutonium functions, including calls through func
  values and interface method tables;
- the **C ABI** (System V AMD64), for `//pu:extern` and `//pu:export` functions.

The internal ABI is a superset of System V for signatures made only of scalars, so most `//pu:extern`
declarations need no translation. Where the two differ, the table in
[section 9](#9-compatibility-with-system-v) says so, and section 9.1 gives the classification lowering
applies to C calls and entry points.

## Contents

1. [Data layout](#1-data-layout)
2. [Registers](#2-registers)
3. [Internal calling convention](#3-internal-calling-convention)
4. [Func values and closures](#4-func-values-and-closures)
5. [Interfaces](#5-interfaces)
6. [Stack frames](#6-stack-frames)
7. [Code model and relocations](#7-code-model-and-relocations)
8. [Symbols and sections](#8-symbols-and-sections)
9. [Compatibility with System V](#9-compatibility-with-system-v)
10. [Kernel mode](#10-kernel-mode)
11. [Linux system calls](#11-linux-system-calls)

## 1. Data layout

Sizes and alignments follow Go's gc compiler on amd64. `int`, `uint`, `uintptr` and pointers are 8 bytes.

| Type | Size | Align | Layout |
|---|---|---|---|
| `bool`, `int8`, `uint8` | 1 | 1 | |
| `int16`, `uint16` | 2 | 2 | |
| `int32`, `uint32`, `float32`, `rune` | 4 | 4 | |
| `int64`, `uint64`, `int`, `uint`, `uintptr`, `float64` | 8 | 8 | |
| `*T`, `unsafe.Pointer`, `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]` | 8 | 8 | address |
| func value | 8 | 8 | address of a closure record (section 4) |
| `string` | 16 | 8 | `{ptr *byte, len int}` |
| `[]T` | 24 | 8 | `{ptr *T, len int, cap int}` |
| interface | 16 | 8 | `{tab, data}` (section 5) |
| `[N]T` | N × size(T) | align(T) | |
| struct | Go's rules | max field align | fields in order, padded to alignment |
| `volatile[T]` | size(T) | align(T) | as `T` |
| `aligned[T, N]` | size(T) rounded up to N | N | as `T` |
| `ring[T, N]` | 16 + N × size(T) | max(8, align(T)) | `{head, tail uint64; buf [N]T}` |
| packed type | its backing, or its field width rounded up to bytes | backing's | bit-exact, language spec section 5.5 |
| enum type | its underlying integer's | its underlying integer's | the member's value, language spec section 5.9 |
| union | tag, then the largest payload at its alignment, rounded up to the alignment | max(tag size, largest payload alignment) | `{tag uint8 (uint16 above 256 variants); payload}`, below |
| packed union | its backing | backing's | a packed type whose tag field selects the variant, language spec section 5.10 |
| `chan T` (planned) | 8 | 8 | address of the channel object |

`owned[T]`, `shared[T]`, `synced[T]` and `weak[P]` point at the object. The allocation behind them begins
with a hidden header at a negative offset from the object: one word recording its allocator for
`owned[T]`, and for the counted kinds the allocator, the strong count and the weak count (language spec
section 5.6; planned with the move checker). Owner types declared with `owner` (language spec section
6.4) have the layout of their underlying type; ownership adds no bytes.

A union (language spec section 5.10) is its tag at offset 0, numbering the variants from 0 in declaration
order, and the payload of the variant it holds at the first offset after the tag that satisfies the
largest payload alignment. The bytes after a shorter payload, and between the tag and the payload, are
padding: their contents are unspecified, and equality compares the tag and then the payload. For example
`union { Off; Probing struct{ attempt int }; Ready struct{ irq uint8; base uintptr } }` is 24 bytes, with
the tag at 0 and the payload at 8, and `union { None; Some uint8 }` is 2 bytes with the payload at 1. The
layout of ordinary unions is internal: the compiler may later store an empty variant in a payload's
unused values instead of a tag, and unions are not allowed in `//pu:extern` and `//pu:export`
signatures (section 9.1). Packed unions are exactly their backing.

Package-level variables are in `.data` (constant initialiser, written into the image; this includes
arrays and structs whose elements are constants), `.bss` (no initialiser, or one computed by the package
initialiser), `.percpu` (`//pu:percpu`, section 10.6) or the section named by `//pu:section`. Read-only
data (string bytes, type descriptors, itabs, closure records) is in `.rodata`.

## 2. Registers

| Register | Internal ABI | System V |
|---|---|---|
| RDI, RSI, RDX, RCX, R8, R9 | integer arguments 1 to 6 | same |
| XMM0 to XMM7 | float arguments 1 to 8 | same |
| RAX | 1st integer result; closure context on entry to a func-value call | 1st result; AL = vector register count for variadic calls |
| RDX | 2nd integer result | 2nd result |
| RCX, RSI | 3rd and 4th integer results | not result registers |
| XMM0 to XMM3 | float results 1 to 4 | XMM0, XMM1 only |
| RBX, RBP, R12 to R15 | callee-saved | same |
| RAX, RCX, RDX, RSI, RDI, R8 to R11, all XMM | caller-saved | same |
| RSP | 16-byte aligned at every `call` | same |

The direction flag is clear on entry to and exit from every function, as in System V. The compiler uses
`rep movsb` and `rep stosb` for block copies and clears, which rely on it.

The backend targets baseline x86-64 (SSE2). Integers and pointers live in general-purpose registers;
floats live in XMM registers and use scalar SSE instructions, except in loops the optimiser vectorises,
which compute two `float64` lanes at once with SSE2 packed doubles (each lane exactly as the scalar code
would). Packed values never cross a call or a function boundary. Memory copies and clears never use XMM
registers: they are sequences of 1 to 8 byte `MOV`s through a general-purpose register up to 64 bytes, and
`rep movsb` / `rep stosb` above that.

## 3. Internal calling convention

### 3.1 Flattening

Lowering turns every Plutonium parameter and result into a list of **scalar leaves**: integers,
booleans, pointers and floats, each with its offset in the value.

| Type | Leaves |
|---|---|
| integer, boolean, pointer, func value, `owned`/`shared`/`synced` | 1 |
| `float32`, `float64` | 1 (float class) |
| `string` | 2: pointer, length |
| slice | 3: pointer, length, capacity |
| interface | 2: table, data |
| struct | the leaves of its fields, in order |
| array of at most 4 elements | the leaves of each element |
| array of more than 4 elements | none: held in memory |
| `volatile[T]`, `aligned[T, N]` | those of `T` |
| packed type of 1, 2, 4 or 8 bytes | 1 integer |
| union | integers whatever the payload holds: the bytes before the payload (the tag and padding), then the payload's bytes, each as 8-byte words and then a 4-, 2- and 1-byte piece for what remains; none (held in memory) for more than 16. `union { Ok int; Err int }` is two leaves, the tag word and the payload |
| `ring[T, N]`, other packed types | none: held in memory |

A value **fits in registers** when it has between 1 and 4 leaves (`maxRegLeaves` in
`internal/lower/layout.go`).

### 3.2 Parameters

Parameters are assigned in this order: the hidden result pointer, if any (section 3.3), then the
receiver, then the declared parameters.

- A parameter that fits in registers is passed as its leaves, each one as a separate scalar argument.
- Any other parameter is passed **by reference**: the caller makes a copy in its own frame and passes the
  copy's address as one pointer argument. The callee may modify the copy.

The resulting scalar list is assigned to registers by class, in order: integer-class scalars to RDI, RSI,
RDX, RCX, R8, R9, float scalars to XMM0 to XMM7. A scalar that finds no register of its class goes to the
stack, in an 8-byte slot, in argument order: the first stack scalar is at `8(%rsp)` on entry (just above
the return address), the next at `16(%rsp)`, and so on. Narrow scalars occupy a whole slot; the upper bits
are unspecified. The leaves of one aggregate may be split between registers and the stack. The caller
reserves the outgoing area in its frame (no pushes) and owns it after the call.

### 3.3 Results

If the results together have at most 4 leaves, and none of them is held in memory, they are returned in
registers: integer-class leaves in RAX, RDX, RCX, RSI and float leaves in XMM0 to XMM3, each class in
order.

Otherwise the function returns through a hidden **result pointer** (sret), passed as the first scalar
argument (RDI). It points at a caller-owned buffer laid out as a struct whose fields are the results in
order. The callee writes the results there and returns nothing in registers; unlike System V, RAX does
not hold the buffer address on return.

### 3.4 Examples

```go
func f(s string, n int) (int, bool)    // RDI=s.ptr RSI=s.len RDX=n      -> RAX=int, RDX=bool
func g(x, y float64, p *T) float64     // XMM0=x XMM1=y RDI=p             -> XMM0
func h(b []byte, v Vec3) error         // RDI,RSI,RDX=b; v={x,y,z float64}: XMM0,XMM1,XMM2
                                       //                                  -> RAX=tab, RDX=data
func k(m [8]int) [8]int                // RDI=sret, RSI=&copy of m          -> nothing
func (r *Reader) Read(p []byte) (int, error)
                                       // RDI=r RSI,RDX,RCX=p               -> RAX=n RDX=tab RCX=data
```

## 4. Func values and closures

A func value is the address of a **closure record**. Word 0 of the record is the code pointer.

- A declared function used as a value, or a function literal that captures nothing, uses a static,
  read-only one-word record named `sym·f`.
- A function literal that captures variables gets a record in the creating function's frame: word 0 is
  the code pointer, word *i*+1 is the address of captured variable *i* (captures are by reference). This
  is why such closures cannot outlive their frame (language spec section 4.5).
- A method value `x.M` gets a record in the frame holding a wrapper's code pointer (`sym-fm`) followed by
  a copy of the receiver; an interface method value holds the interface value.

A call through a func value loads the code pointer from word 0 and calls it with the **record address in
RAX**, in addition to the ordinary arguments. Functions that do not need it ignore RAX, so a declared
function can be called directly or through its record with the same code. Calls to C functions never
pass a context.

## 5. Interfaces

An interface value is `{tab, data}`.

- For the empty interface, `tab` is the dynamic type's **type descriptor**. For other interfaces it is
  an **itab**, whose first word is the type descriptor. A nil interface has `tab == nil`.
- `data` holds the value's bits when the dynamic type is **direct** (integers, floats, booleans,
  pointers, func values, `owned`/`shared`/`synced`, packed values of up to 8 bytes, and structs with one
  field or arrays of length 1 that hold a single pointer): zero-extended integers and booleans, float
  bits, or the address. For every other type `data` points at a copy of the
  value in the frame of the function that made the conversion (language spec section 4.4).

Type descriptor, 40 bytes, read-only, named `type:<type>` (mirrored by `pluto/rt.typeDesc`):

| Offset | Field | |
|---|---|---|
| 0 | `size uintptr` | 0 for interface types |
| 8 | `hash uint32` | FNV-1a of the type string |
| 12 | `kind uint8` | Go's reflect numbering, plus 27 packed, 28 ring, 29 owned, 30 union |
| 13 | `flags uint8` | 1 = direct |
| 16 | `name string` | the type as written, package-qualified by name |
| 32 | `equal func(a, b unsafe.Pointer) bool` | closure record, or nil if not comparable or direct |

The descriptor of a union (kind 30) is 64 bytes, continuing with the payload's offset (`uintptr` at 40), a
pointer to a table of the variants in tag order (at 48) and their number (`int` at 56). Each table entry is
32 bytes: the variant's name (`string`), its payload's type descriptor (nil for a variant without a
payload) and a word that is 1 when the payload is an empty interface and 2 when it is another interface.
`pluto/rt.UnionInfo` reads them, and `fmt` prints a union with them.

Itab, named `itab:<type>,<interface>`: the type descriptor, the interface's descriptor, then one code
pointer per interface method in method order. Each code pointer is an adapter that takes the data word as
its first argument (in place of the receiver) followed by the method's arguments, and is called with RAX
set to 0. `pluto.itabs` holds every itab for the run-time lookup that conversions between non-empty
interfaces and type assertions to them use. It is an open-addressing hash table: a word holding the
number of slots `n`, a power of two at least twice the number of itabs, then `n` slots, each an itab
address or 0. With `t` and `i` the `hash` words of the concrete type's and the interface's descriptors,
the lookup computes, in 32-bit arithmetic,

```
h = t*0x9E3779B1 ^ i;  h ^= h >> 15;  h *= 0x85EBCA6B;  h ^= h >> 13
```

and probes slots `h & (n-1)`, `h+1 & (n-1)`, ... until it finds the itab or a 0 slot.

Richer descriptors (element types, field offsets and names, used by `fmt` for composite values) are
planned; see the roadmap.

## 6. Stack frames

The backend does not keep a frame pointer unless it needs one.

**Without a frame pointer** (the usual case):

```
higher addresses
  incoming stack arguments           8(%rsp_entry), 16(%rsp_entry), ...
  return address                     0(%rsp_entry)
  saved callee-saved registers       pushed in the prologue
  stack slots                        locals, temporaries, spills, interface boxes, closure records
  outgoing stack arguments           0(%rsp), 8(%rsp), ...
lower addresses                      <- %rsp
```

The prologue pushes the callee-saved registers the function uses, then subtracts the frame size; the
epilogue adds it back, pops and returns. Incoming stack argument *k* is at
`frameSize + 8*nSaved + 8 + 8k` from RSP. The frame size is chosen so that RSP is 16-byte aligned at
every call.

**Slots** are sorted by alignment. A slot of 16 bytes or more is placed 16-aligned whatever its type's
alignment, so block copies and clears of it are aligned.

**Leaf functions** that make no calls and have no stack slots get no frame at all: no `sub`, no `add`,
only pushes of any callee-saved registers they use.

**With a frame pointer**: a function with a slot aligned to more than 16 bytes (`aligned[T, 64]` local)
uses RBP:

```
push %rbp; mov %rsp, %rbp; push <saved>; and $-align, %rsp; sub $frameSize, %rsp
```

Incoming stack arguments are then at `16(%rbp)`, `24(%rbp)`, and so on, and the epilogue restores RSP from
RBP.

**No red zone.** Generated code never reads or writes below RSP: a function that needs stack memory
subtracts from RSP first, and a leaf without a frame uses only registers. This holds in every build
mode, which is why kernel mode needs no change to frame layout (section 10.1).

The prologue pushes the saved registers in the order RBX, RBP, R12, R13, R14, R15. There is no
frame-pointer chain: debuggers walk these frames with the DWARF call frame information in
`.debug_frame`, and the runtime's stack traces with the compiler's trace table, both of which describe
every prologue and epilogue (see [debugging.md](debugging.md)).

## 7. Code model and relocations

Plutonium generates non-position-independent code. Every program is linked statically (`ld -static
-nostdlib`) and code and data must lie within ±2 GiB of each other.

| Use | Encoding | Relocation |
|---|---|---|
| address of a symbol, `sym+disp` | RIP-relative `disp32(%rip)` | `R_X86_64_PC32` |
| direct call | `call rel32` | `R_X86_64_PLT32` |
| symbol plus scaled index, `sym+disp32(,%idx,scale)` | absolute 32-bit displacement, sign-extended | `R_X86_64_32S` |
| pointer stored in data | 64-bit absolute | `R_X86_64_64` |

The `R_X86_64_32S` form folds a global array's address into the instruction's addressing mode and is
correct only when every symbol's absolute address fits in a sign-extended 32-bit value: the low 2 GiB or
the top 2 GiB of the address space. That gives two supported code models:

- **Small** (hosted builds, and freestanding builds linked low): the image is in the low 2 GiB. The
  default `ld` layout for a static executable (0x400000) satisfies it.
- **Kernel** (kernel mode, section 10.3): the image is linked in the top 2 GiB, at or above
  `0xFFFFFFFF80000000`. RIP-relative addressing works unchanged and `R_X86_64_32S` sign-extends to the
  right address.

Position-independent output (PIE, KASLR beyond the top 2 GiB, and PE32+ images that UEFI may load
anywhere) is not supported; it needs a mode that never emits `R_X86_64_32S` and loads addresses with
`lea` instead (planned with PE32+).

**Constant pool.** Float constants and the sign masks used for float negation and absolute value are
16-byte, 16-aligned read-only objects in `.rodata`, addressed RIP-relative. Each is named
`pluto.const.<hi><lo>` after its bits in hexadecimal, so identical constants from different functions
are emitted once. Integer constants are immediates. The pool exists only for float code, so kernel-mode
functions without `//pu:fpu` (no floats, section 10.2) never use it.

## 8. Symbols and sections

| Entity | Symbol |
|---|---|
| function | `path.Name`, e.g. `pluto/fmt.Fprintf` |
| method, value receiver | `path.Type.Method` |
| method, pointer receiver | `path.(*Type).Method` |
| generic instance | `path.Name[T1,T2]`, type arguments package-qualified by import path |
| method of a generic type instance | `pluto/vec.(*Vec).Push[int]`: the receiver's type arguments, then the method's own, in the brackets |
| function literal | `<enclosing symbol>.func<N>` |
| bound method wrapper | `<method symbol>-fm` |
| static closure record | `<function symbol>·f` |
| package-level variable | `path.Name` |
| package initialiser | `path.init·pkg`; user `init` functions are `path.init.0`, `path.init.1`, ... |
| program entry (runs every initialiser, then `main.main`) | `pluto.main` |
| type descriptor | `type:<type>` |
| itab | `itab:<type>,<interface>` |
| itab table | `pluto.itabs`, `pluto.itabtable` |
| main's stack size from `//pu:stack` (0 if none), a function returning a `uintptr` | `pluto.mainstack` |
| string data | `.str.N` (local) |
| static object (a `&T{}` in a package initialiser) | `.static.N` (local) |
| float constant | `pluto.const.<hex>` |
| per-CPU block's self word (offset 0 of `.percpu`) | `pluto.percpu.self` |

Symbols contain `/`, `*`, `(`, `[` and `·`, so assembly quotes them: `"pluto/syscall.Syscall"`.
A `//pu:extern name` function refers to the symbol `name`; a body-less function without it refers to its
own Plutonium symbol, implemented in a `.s` file of the same package. `//pu:export name` makes `name` a
C entry point: a second global symbol for the same code, or a wrapper when the signature needs System V
translation (section 9.1); `//pu:export` with no name only keeps the function even if nothing in
Plutonium calls it.

Hosted programs start at `_start` (`lib/pluto/rt/sys_linux_amd64.s`), which aligns the stack and calls
`pluto/rt.start`; that calls `pluto.main` and exits with its result. Freestanding programs provide their
own entry point, which must call `pluto.main` (or the package initialisers and `main.main`) on a 16-byte
aligned stack with the direction flag clear.

## 9. Compatibility with System V

| Aspect | Internal ABI | System V | Compatible? |
|---|---|---|---|
| scalar arguments, up to 6 integer and 8 float | registers as in section 3.2 | same | yes |
| further scalar arguments | 8-byte stack slots in order | same | yes |
| one or two scalar results | RAX, RDX / XMM0, XMM1 | same | yes |
| three or four scalar results | RCX, RSI / XMM2, XMM3 | memory | **internal only** |
| strings, slices, interfaces, small structs as arguments | flattened to leaves | classified by eightbyte | **internal only** (a string happens to match, a slice does not) |
| aggregates with more than 4 leaves | pointer to caller copy | copied onto the stack | **internal only** |
| aggregate results | leaves in up to 4 registers, or sret | eightbyte classes, or sret returned in RAX | **internal only** |
| closure context | RAX | none | **internal only** |
| callee-saved registers, stack alignment, direction flag | as System V | | yes |
| red zone | never used | may be used | callers of Plutonium are unaffected; Plutonium never relies on one |

### 9.1 The C convention for `//pu:extern` and `//pu:export`

Calls to a `//pu:extern` function and entries through a `//pu:export name` symbol follow System V. When
the internal ABI already passes a signature exactly as System V does (every parameter a scalar of 4 or 8
bytes, a float or a pointer, and at most one such result), the call is an ordinary internal-ABI call and
an export is a second symbol on the same code, as before. Otherwise lowering classifies the signature:

- **Type mapping.** Integers, `uintptr`, pointers, `unsafe.Pointer` and `owned`/`shared`/`synced` map to
  the C integer or pointer type of the same size; `bool` is C `_Bool` (1 byte); `float32` and `float64` are
  `float` and `double`; structs and arrays inside structs are laid out as in section 1, which matches C
  for these members. `volatile[T]` and `aligned[T, N]` (N at most 8) are `T`. A packed type crosses as its
  storage: `packed uint32 {...}` is a `uint32_t`, and a byte-backed `packed {...}` of n bytes is a
  `uint8_t[n]` (as a parameter, a struct holding that array). It is not a C `__attribute__((packed))`
  struct with misaligned members, which System V passes in memory.
- **Scalars** are passed in registers or 8-byte stack slots as in section 3.2. Integers and booleans
  narrower than 32 bits are zero- or sign-extended to 32 bits by the caller for arguments and by the
  callee for results, as GCC and Clang do; values Plutonium receives are not assumed to be extended.
- **Structs of up to 16 bytes** are split into eightbytes, each classed INTEGER if any scalar in it is an
  integer, pointer or boolean, otherwise SSE. Each eightbyte travels as one 8-byte value in the next free
  register of its class (RDI to R9, XMM0 to XMM7). If the registers left cannot hold every eightbyte, the
  whole struct goes on the stack and later scalars still take the registers that remain.
- **Larger structs** are MEMORY: an argument is copied onto the stack, in argument order, in 8-byte
  slots; a result is written through a hidden pointer the caller passes in RDI (shifting the other
  integer arguments by one), and the callee returns that pointer in RAX.
- **Results.** A scalar is returned in RAX or XMM0. A struct of up to 16 bytes is returned by eightbyte
  class in RAX then RDX for INTEGER eightbytes and XMM0 then XMM1 for SSE ones, so a mixed struct uses
  RAX and XMM0 in either order. **Several results** are returned as a C struct whose members are the
  results in order: `(q, r int64)` is `struct { int64_t q, r; }` in RAX:RDX, `(a, b int32)` is one
  eightbyte in RAX, and three `int64` results are returned through a hidden pointer.
- **Receivers.** A method with `//pu:extern` or `//pu:export` takes its receiver as the first C
  parameter.
- **Rejected,** at the declaration, even if nothing calls it: `string` and slices (no C type; pass
  `unsafe.StringData(s)` or `unsafe.SliceData(b)` and the length), interfaces, func values (a func value
  points at a closure record, not at code), maps, channels and unions (their layout is not part of the
  ABI; pass a struct of a tag and the payload), anywhere in a by-value parameter or result,
  including inside structs; arrays as a whole parameter or result (C cannot pass them by value; wrap them
  in a struct or pass a pointer); zero-size types; types aligned to more than 8 bytes; variadic functions
  (C varargs are not supported); generic functions; `//pu:extern` on a function with a body; and two
  wrapped exports of the same name.

Strings and slices were rejected rather than passed as their `{ptr, len[, cap]}` structs because no C
declaration of a function taking `const char *, size_t` would match: a string would happen to arrive in
the same two registers, but a 24-byte slice is a MEMORY struct copied to the stack, and either would make
a mistake in the C prototype silent. Behind a pointer, a struct containing strings or slices may be
shared with C, with the layout of section 1.

An export whose signature needs translating gets a **wrapper** named by the export: an `ABIC` function
that takes System V arguments, calls the Plutonium function with the internal ABI and returns System V
results. The Plutonium function keeps its own symbol for Plutonium callers. A `//pu:extern` function used
as a func value whose signature needs translating is reached through a **thunk**, an internal-ABI
function named by the declaration's Plutonium symbol that makes the C call. `//pu:export` with no name and
`//pu:export` on a `//pu:interrupt` handler only add a symbol; they never wrap.

C calls never pass a closure context. The backend supports variadic C calls (AL holds the number of
vector registers used), but lowering does not generate them.

### 9.2 Linking C

A package may contain `.c` files, compiled with the system C compiler (`$CC`, default `cc`) as
`cc -c -O2 -ffreestanding -fno-stack-protector`, plus `-mno-red-zone` in freestanding builds, with the
package directory on the include path, and `.o` files, linked as they are. Both follow the file-name
tag rules of `.p` files (`name_linux.c`, `name_amd64.o`), and `.c` files may start with a `//pu:build`
line. Programs are linked with `ld -static -nostdlib`, so there is no C library: C code must not call
libc, and must provide `memcpy` and `memset` itself if the C compiler emits calls to them for large
copies.

## 10. Kernel mode

Kernel mode (`--mode kernel`, language spec section 12.2) is a freestanding build with these differences
in generated code. It sets the build tags `freestanding`, `none` and `kernel`.

### 10.1 No red zone

Plutonium code never uses the red zone (section 6), so kernel mode changes nothing here. Assembly files
and C objects linked into a kernel must also not use it (`-mno-red-zone` for C), because an interrupt
taken without a stack switch pushes its frame directly below the interrupted RSP.

### 10.2 No SSE, x87 or other vector state

Kernel-mode code uses only general-purpose registers, so an interrupt or system call entry need not save
vector state.

- Any operation on `float32` or `float64` values (arithmetic, comparisons, conversion to or from
  integers or between float sizes, float constants, float parameters, results and call arguments, and
  converting a float to an interface) in a function compiled in kernel mode without `//pu:fpu` is a
  compile error, reported once per function at the first such use, for example `float64 arithmetic is not
  allowed in kernel mode (no FPU state is saved; mark the function //pu:fpu to use floats)`. Parameters,
  results and arguments are judged after flattening (section 3.1): a float leaf passed in an XMM register
  is an error, a struct passed by reference is not. Floats may still appear in types, and copying a
  float-typed value as bits is allowed: the backend keeps such values (loads, stores, phis, copies and the
  zero constants that scalar replacement makes) in general-purpose registers.
- The check applies to functions the program uses. Lowering only compiles functions that are reachable
  from the program's roots, so an unused float function in an imported package costs nothing. Package
  variables initialised with constants, including arrays and structs of them, are written into the image
  rather than by the package initialiser, so importing `pluto/math` adds no float code. `pluto/fmt`
  passes floats as bits and keeps its float formatting under `//pu:build !kernel`; a kernel build prints a
  float argument as `%!v(float64=0x...)`.
- Block copies and clears avoid XMM registers in every mode (section 2: `MOV` through a general-purpose
  register up to 64 bytes, `rep movsb`/`rep stosb` above), so kernel mode needs no change there. There is
  no constant pool (section 7).
- A function marked `//pu:fpu` may use floats in kernel mode, as may the function literals inside it. Its
  callers must have saved the vector state and enabled the FPU (with `x86.FPUBegin`/`x86.FPUEnd`, section
  10.5); the compiler does not check this.

**The `NoSSE` property.** Lowering marks every function compiled in kernel mode without `//pu:fpu` with
`ssa.Func.NoSSE`. It is the contract between the passes:

- lowering rejects float operations in such a function, as above;
- no optimisation pass may introduce float or vector operations into a `NoSSE` function. A vectoriser
  must skip `NoSSE` functions entirely (block copies, clears and fills included). The loop vectoriser
  (`opt.Vectorize`) asks `ssa.Func.MayVectorize`, which is false for `NoSSE` functions and for those
  marked `//pu:nosimd` (`ssa.PragmaNoSIMD`) or `//pu:interrupt`. `//pu:fpu` functions may be vectorised:
  `FPUBegin` saves the whole vector state;
- the inliner never inlines a function without `NoSSE` (a `//pu:fpu` function) into one with it, so FPU
  code stays in functions the caller has to bracket with `FPUBegin`/`FPUEnd`. It may inline in the other
  direction;
- the backend (`internal/arch/amd64`) gives float-typed values general-purpose registers, refuses any
  operation that needs SSE with an error naming the function, and after instruction selection rejects any
  instruction that is an SSE instruction or names an XMM register. A `NoSSE` function therefore cannot be
  emitted with vector instructions, whatever the earlier passes did.

Soft-float was considered and rejected: it would add an IEEE-754 library to every kernel, hide a large
cost behind ordinary arithmetic, and still be the wrong answer for the rare kernel code that wants floats,
which is better served by `//pu:fpu` regions that use the real unit.

### 10.3 Kernel code model

The image is linked in the top 2 GiB (`0xFFFFFFFF80000000` and up) by the kernel's linker script.
Relocations are as in section 7: RIP-relative for symbol addresses, `R_X86_64_32S` for indexed globals and
per-CPU variables (section 10.6), `R_X86_64_64` for pointers in data. The compiler emits nothing that
assumes the low 2 GiB. With `--emit-relocs` a boot loader can slide the image within the top 2 GiB
(KASLR); sliding outside it needs position-independent code (section 7).

`--ldscript` names the kernel's own script. Without it, kernel builds link with a default script
(`build.KernelLinkerScript` in `internal/build/kernel.go`, with `-z max-page-size=0x1000`):

| Output section | Virtual address | Load (physical) address | Holds |
|---|---|---|---|
| `.boot` | `0x100000` | `0x100000` | input sections `.multiboot` (first), `.boot`, `.boot.*`: a multiboot header and the code that runs before paging maps the kernel high |
| `.text`, `.rodata`, `.data` | physical + `0xFFFFFFFF80000000`, from the next 4 KiB boundary | after `.boot` | the kernel |
| `.percpu` | `0` | after `.data`, 64-aligned | the per-CPU image (section 10.6) |
| `.bss` | physical + `0xFFFFFFFF80000000` | after `.percpu` (not loaded) | zeroed data |

It defines `__kernel_phys_start`, `__kernel_load_end` (the physical end of the loaded image),
`__kernel_phys_end` (after `.bss`), `__bss_start`, `__bss_end`, `__percpu_image` (the virtual address of
the per-CPU image), `__percpu_load` (its physical address) and `__percpu_size`; the entry symbol is
`_start`, which the kernel provides. The ELF output has a `PT_LOAD` segment at virtual address 0 for
`.percpu`, as Linux's `vmlinux` once did, so it is meant for loaders that load by physical address
(multiboot, or `--format binary`). `testdata/kernel/smoke` boots a kernel linked this way.

### 10.4 Interrupt handlers

`//pu:interrupt` makes a function an x86-64 interrupt or exception handler. It is available in every
build mode (a hosted program can compile one but has no way to install it).

```go
package x86

type InterruptFrame struct {
	RIP, CS, RFLAGS, RSP, SS uint64
}
```

```go
//pu:interrupt
func timer(f *x86.InterruptFrame)

//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) // vectors that push an error code
```

A handler is a plain function (not a method, not generic) with a body, no results, and either one
parameter, the address of the interrupt frame, or two, the frame and the error code. The frame parameter
may be any pointer type, `unsafe.Pointer` or `uintptr`; the error code is `uint64` or `uintptr`. The
signature decides the stack layout, so a handler with an error code must be installed only on the
vectors whose exceptions push one (8, 10 to 14, 17, 21, 29 and 30), and a handler without one on every
other vector, including software `int $n`. This follows GCC's and Clang's
`__attribute__((interrupt))`. Anything else is a compile error, as is `//pu:interrupt` combined with
`//pu:inline`.

The compiler generates the entry and exit:

1. On entry RSP points at the error code, if the signature has one, or at the saved RIP. In 64-bit mode
   the CPU aligns RSP to 16 bytes before pushing the frame, so RSP is 8 modulo 16 on entry without an
   error code (five words pushed, the same as after a `call`) and 0 with one (six words). The compiler
   uses this to size the frame; it does not realign at run time.
2. The prologue pushes every general-purpose register the handler changes. A handler that makes a call
   also saves every caller-saved register (RAX, RCX, RDX, RSI, RDI, R8 to R11), since the callee may
   change them; callees preserve the callee-saved ones. It then allocates the frame, padded so that RSP
   is 16-byte aligned at every call, and executes `cld`.
3. Outside kernel mode, the prologue saves, with `movups` into the frame, the XMM registers the handler
   changes: the ones its own code uses, and all of XMM0 to XMM15 if it makes a call, because any
   Plutonium, C or assembly callee may use them. A handler that neither calls nor uses floats touches no
   vector register. Saving XMM registers needs SSE enabled (CR4.OSFXSR). x87, AVX upper halves and MXCSR
   are not saved: Plutonium code does not change them, and legacy SSE instructions leave the upper halves
   of the YMM registers alone.

   In kernel mode a handler without `//pu:fpu` saves no XMM registers, even when it makes calls. It is
   `NoSSE` (section 10.2), and so is every function it can reach without `//pu:fpu`; a `//pu:fpu`
   function may only be called between `x86.FPUBegin` and `x86.FPUEnd`, which save and restore the
   interrupted state themselves. So nothing the handler runs changes a vector register except inside such
   a bracket. Assembly and C code called from a kernel-mode handler must likewise leave the vector
   registers alone (`-mgeneral-regs-only` for C) or bracket its use. A `//pu:fpu` handler saves the XMM
   registers it uses, and all sixteen if it calls, as outside kernel mode; it needs SSE enabled.
4. The handler's parameters are the frame's address (the saved RIP, `8(%rsp)` on entry when there is an
   error code, `(%rsp)` otherwise) and the error code (`(%rsp)` on entry). They are not passed in
   registers.
5. The epilogue restores the XMM registers, frees the frame, pops the saved registers in reverse order,
   discards the error code (`add $8, %rsp`) if there is one, and returns with `iretq`. Changes the handler
   makes to `*f` (for example advancing `RIP` past a faulting instruction) take effect then.

Handlers are never inlined, cannot be called directly (including with `defer`) and cannot be used as
func values, all compile errors; their address is taken with `x86.HandlerAddr(timer)`, which yields a
`uintptr` for the IDT gate. `x86.HandlerAddr` with anything other than the name of a `//pu:interrupt`
function is a compile error. `//pu:export name` on a handler gives its code a second global symbol, for
assembly stubs that refer to it.

The handler may call ordinary functions: they run on a 16-byte aligned stack with the direction flag
clear, as section 2 requires, and every register they may change is restored before `iretq`.

### 10.5 `pluto/arch/x86` intrinsics

Each function in `pluto/arch/x86` is expanded inline to the instructions shown. Lowering turns a call
into one `ssa.OpIntrinsic` (named, for example, `x86.Cli`), which takes the current memory state and
produces the next one, as a call does: no load or store is moved, merged or removed across it, so every
intrinsic is a compiler barrier (memory model section 5.2). It has side effects, so it is never removed,
even when its result is unused, and never merged with another. The backend expands it with the fixed
registers the instructions need. Each function except `HandlerAddr` also has an assembly body
(`x86_amd64.s`) for when it is called through a func value. Privileged instructions fault outside ring 0.
Encodings are tested against GNU as.

| Function | Expansion |
|---|---|
| `Cli()`, `Sti()`, `Hlt()`, `Pause()` | `cli`, `sti`, `hlt`, `pause` |
| `StiHlt()` | `sti; hlt` as one machine instruction of the backend, so nothing is scheduled between them and an interrupt cannot arrive in between |
| `SaveFlagsCli() uint64` | `pushfq; pop r; cli` |
| `RestoreFlags(f uint64)` | `push r; popfq` |
| `Inb(port uint16) uint8`, `Inw(port uint16) uint16`, `Inl(port uint16) uint32` | `in %dx, %al/%ax/%eax`, or `in $port` for a constant port below 256 |
| `Outb(port uint16, v uint8)`, `Outw(port uint16, v uint16)`, `Outl(port uint16, v uint32)` | `out %al/%ax/%eax, %dx`, or `out ..., $port` for a constant port below 256 |
| `Rdmsr(msr uint32) uint64` | `rdmsr`, EDX:EAX combined |
| `Wrmsr(msr uint32, v uint64)` | `wrmsr`, with `v` split into EDX:EAX |
| `ReadCR0() uint64`, `ReadCR2() uint64`, `ReadCR3() uint64`, `ReadCR4() uint64` | `mov %crN, r` |
| `WriteCR0(v uint64)`, `WriteCR3(v uint64)`, `WriteCR4(v uint64)` | `mov r, %crN` |
| `Invlpg(addr uintptr)` | `invlpg (r)` |
| `Lgdt(p *DescriptorTablePointer)`, `Lidt(p *DescriptorTablePointer)` | `lgdt`, `lidt` of `*p`, with `p`'s address folded into the operand |
| `Ltr(sel uint16)` | `ltr r16` |
| `Cpuid(leaf, subleaf uint32) (eax, ebx, ecx, edx uint32)` | `cpuid` (the function saves RBX, which it changes) |
| `Rdtsc() uint64`, `Rdtscp() (tsc uint64, aux uint32)` | `rdtsc`, `rdtscp` |
| `Swapgs()` | `swapgs` |
| `ReadGSBase() uintptr`, `WriteGSBase(v uintptr)` | `rdmsr`/`wrmsr` of `IA32_GS_BASE` (`GSBaseMSR`) |
| `GSLoad64(off uintptr) uint64`, `GSLoad32`, `GSStore64(off uintptr, v uint64)`, `GSStore32` | `mov %gs:(r), ...`, or `%gs:off` for a constant offset |
| `FPUBegin(area *FPUState)` | `clts; fxsave64 (area)` |
| `FPUEnd(area *FPUState)` | `fxrstor64 (area)` |
| `HandlerAddr(h any) uintptr` | `lea handler(%rip), r`; `h` must name a `//pu:interrupt` function, checked at compile time. A plain address, so it orders nothing; through a func value it panics |

```go
type DescriptorTablePointer packed [10]byte {
	limit uint16
	base  uint64
}

func NewDescriptorTablePointer(base uintptr, size int) DescriptorTablePointer

type FPUState aligned[[512]byte, 16]
```

`FPUBegin` leaves the FPU in the interrupted context's state (its MXCSR and x87 control word included);
it does not enable SSE, which the kernel does once by setting CR4.OSFXSR and CR4.OSXMMEXCPT. A
`//pu:fpu` function called from a function without it must run between `FPUBegin` and `FPUEnd` on the
same area. `rdgsbase`/`wrgsbase` (FSGSBASE) are not used; there is no `--cpu` option yet.

`pluto/arch/x86` is available on amd64 in every build mode; only `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`
are useful outside ring 0.

### 10.6 Per-CPU variables

A package-level variable marked `//pu:percpu` has one copy per processor:

```go
//pu:percpu
var current *Thread

//pu:percpu
var runQueue RunQueue
```

- The compiler places such variables in the section `.percpu` (`SHT_PROGBITS`, writable, holding their
  initial values), which the kernel's linker script links at virtual address 0 with a load address
  elsewhere, so each symbol's address is its offset in the per-CPU block. The default kernel script does
  this (section 10.3).
- The first word of every block holds the block's own address. The compiler reserves it as
  `pluto.percpu.self`, emitted first in `.percpu` of the Plutonium object; `pluto build` links that
  object first, so a script with `*(.percpu)` puts it at offset 0. After linking an ELF kernel, the build
  checks that `pluto.percpu.self` is at address 0 and fails otherwise.
- A load or store of a per-CPU variable, a field or an element of one, compiles to one `GS`-relative
  instruction with the symbol's offset as an absolute `disp32` (`mov %gs:sym+off, r`, or
  `%gs:sym(,%idx,scale)` for a variable index, relocated with `R_X86_64_32S`), and so cannot be split by
  an interrupt on the same processor. A load used once by an arithmetic instruction is folded into it
  (`add %gs:sym, r`), which reads it once; a read-modify-write such as `v++` is a separate load and store.
  Copies and clears above 64 bytes take the address as below.
- Taking the address (`&v`, slicing an array variable, calling a pointer method) reads the self word at
  that point and adds the offset (`mov %gs:pluto.percpu.self, r; lea sym(r), r`): an ordinary pointer into
  the current processor's copy, which keeps referring to that copy after GS changes (a migration, or
  `Swapgs`).
- Only constant initialisers are allowed (constants, and arrays and structs of them): the package
  initialiser runs once, before any processor has its own block, so it could only initialise one copy.
  Anything else is a compile error, as is `//pu:percpu` together with `//pu:section`.
- Outside kernel mode `//pu:percpu` is a compile error.

**Setting up GS.** The kernel gives each processor its block and points GS at it, before that processor
touches a per-CPU variable:

1. allocate a block of `__percpu_size` bytes (rounded up to the alignment the kernel wants) per
   processor;
2. copy `__percpu_size` bytes from `__percpu_image` (the initial image, mapped with the rest of the
   kernel) into it;
3. store the block's own address in its first word (`pluto.percpu.self`);
4. load the block's address into the GS base: `x86.WriteGSBase(block)` (`wrmsr` of `IA32_GS_BASE`) on
   that processor. A kernel that enters from user space with `swapgs` keeps the block's address in
   `IA32_KERNEL_GS_BASE` (`x86.Wrmsr(x86.KernelGSBaseMSR, block)`) while user code runs, and swaps it in
   on entry.

`testdata/kernel/smoke` does this for two blocks under QEMU, and `testdata/kernel/gsuser` does the same as
a Linux process, setting the GS base with `arch_prctl(ARCH_SET_GS)`.

Code that reads and then writes a per-CPU variable, or uses its address, must not migrate in between:
it runs with preemption disabled (memory model section 6.4).

## 11. Linux system calls

Linux system calls use their own convention, implemented in assembly in `pluto/syscall` and `pluto/rt`:

| Purpose | Register |
|---|---|
| system call number | RAX |
| arguments 1 to 6 | RDI, RSI, RDX, R10, R8, R9 |
| result | RAX; a value in [-4095, -1] is a negated errno |

`syscall` clobbers RCX and R11.
