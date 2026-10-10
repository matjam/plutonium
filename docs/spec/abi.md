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
declarations work without any translation. Where the two differ, the table in
[section 9](#9-compatibility-with-system-v) says so.

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
| `*T`, `unsafe.Pointer`, `owned[T]`, `shared[T]`, `synced[T]` | 8 | 8 | address |
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
| `chan T` (planned) | 8 | 8 | address of the channel object |

`owned[T]`, `shared[T]` and `synced[T]` point at the object. The allocation behind them begins with a
hidden header word recording its allocator (and, for the counted kinds, the count) at a negative offset
from the object (planned with the move checker).

Package-level variables are in `.data` (constant initialiser, written into the image), `.bss` (no
initialiser, or one computed by the package initialiser) or the section named by `//pu:section`. Read-only data (string bytes, type descriptors,
itabs, closure records) is in `.rodata`.

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
floats live in XMM registers and use scalar SSE instructions. Memory copies and clears never use XMM
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
  pointers, func values, `owned`/`shared`/`synced`, packed values of up to 8 bytes): zero-extended
  integers and booleans, float bits, or the address. For every other type `data` points at a copy of the
  value in the frame of the function that made the conversion (language spec section 4.4).

Type descriptor, 40 bytes, read-only, named `type:<type>` (mirrored by `pluto/rt.typeDesc`):

| Offset | Field | |
|---|---|---|
| 0 | `size uintptr` | 0 for interface types |
| 8 | `hash uint32` | FNV-1a of the type string |
| 12 | `kind uint8` | Go's reflect numbering, plus 27 packed, 28 ring, 29 owned |
| 13 | `flags uint8` | 1 = direct |
| 16 | `name string` | the type as written, package-qualified by name |
| 32 | `equal func(a, b unsafe.Pointer) bool` | closure record, or nil if not comparable or direct |

Itab, named `itab:<type>,<interface>`: the type descriptor, the interface's descriptor, then one code
pointer per interface method in method order. Each code pointer is an adapter that takes the data word as
its first argument (in place of the receiver) followed by the method's arguments, and is called with RAX
set to 0. `pluto.itabs` lists every itab for the run-time lookup that type assertions to non-empty
interfaces use.

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

There are no unwind tables and no frame-pointer chain today, so a debugger or a stack trace cannot walk
Plutonium frames. Unwind information is planned with the debugging work (see the roadmap).

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
programs (no floats, section 10.2) have none.

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
| string data | `.str.N` (local) |
| static object (a `&T{}` in a package initialiser) | `.static.N` (local) |
| float constant | `pluto.const.<hex>` |

Symbols contain `/`, `*`, `(`, `[` and `·`, so assembly quotes them: `"pluto/syscall.Syscall"`.
A `//pu:extern name` function refers to the symbol `name`; a body-less function without it refers to its
own Plutonium symbol, implemented in a `.s` file of the same package. `//pu:export name` adds `name` as
a second global symbol for the same code; `//pu:export` with no name only keeps the function even if
nothing in Plutonium calls it.

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

`//pu:extern` and `//pu:export` functions currently use the internal ABI too. That is correct for every
signature made of scalars with at most two results, which covers system calls, `lua_*`-style C APIs and
most kernel interfaces. Signatures with aggregate parameters or results, or more than two results, need
System V classification, which the backend supports (`ssa.ABIC`, `CallInfo.OnStack`) but lowering does
not yet select; until it does, such `//pu:extern` and `//pu:export` declarations must be rejected (see the
roadmap). Variadic C calls set AL to the number of vector registers used.

## 10. Kernel mode

Kernel mode (`--mode kernel`, language spec section 11.2) is a freestanding build with these differences
in generated code. **Status: proposed.**

### 10.1 No red zone

Plutonium code never uses the red zone (section 6), so kernel mode changes nothing here. Assembly files
and C objects linked into a kernel must also not use it (`-mno-red-zone` for C), because an interrupt
taken without a stack switch pushes its frame directly below the interrupted RSP.

### 10.2 No SSE, x87 or other vector state

Kernel-mode code uses only general-purpose registers, so an interrupt or system call entry need not save
vector state.

- Any operation on `float32` or `float64` values (arithmetic, conversion to or from integers,
  comparisons, float constants, float parameters or results) in a function compiled in kernel mode is a
  compile error: `float64 arithmetic is not allowed in kernel mode (no FPU state is saved)`. Floats may
  still appear in types, and copying a float-typed value as bits is allowed, since it uses integer
  registers.
- The check applies to functions the program uses. Lowering only compiles functions that are reachable
  from the program's roots, so an unused float function in an imported package costs nothing; standard
  library packages put their float code under `//pu:build !kernel`, so `fmt`, `strconv` and `math` remain
  usable for integers and strings.
- Block copies and clears already avoid XMM registers (section 2); kernel mode keeps it so. There is no
  constant pool (section 7). Float values passed to an interface in kernel mode are a compile error like
  any other float operation.
- A function marked `//pu:fpu` may use floats in kernel mode. Its callers must have saved the vector
  state and enabled the FPU (with `x86.FPUBegin`/`x86.FPUEnd`, section 10.5); the compiler does not check
  this.

Soft-float was considered and rejected: it would add an IEEE-754 library to every kernel, hide a large
cost behind ordinary arithmetic, and still be the wrong answer for the rare kernel code that wants floats,
which is better served by `//pu:fpu` regions that use the real unit.

### 10.3 Kernel code model

The image is linked in the top 2 GiB (`0xFFFFFFFF80000000` and up) by the kernel's linker script.
Relocations are as in section 7: RIP-relative for symbol addresses, `R_X86_64_32S` for indexed globals,
`R_X86_64_64` for pointers in data. The compiler emits nothing that assumes the low 2 GiB. With
`--emit-relocs` a boot loader can slide the image within the top 2 GiB (KASLR); sliding outside it needs
position-independent code (section 7).

### 10.4 Interrupt handlers

`//pu:interrupt` (planned; today the directive is accepted but compiles an ordinary function ending in
`ret`, which must become a compile error until this is implemented) makes a function an x86-64 interrupt
or exception handler:

```go
type InterruptFrame struct {
	RIP, CS, RFLAGS, RSP, SS uint64
}

//pu:interrupt
func timer(f *x86.InterruptFrame)

//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) // vectors that push an error code
```

The compiler generates the entry and exit:

1. On entry RSP points at the error code (if the signature has one) or at the saved RIP. The prologue
   pushes every general-purpose register the handler, or anything it calls, may change: all caller-saved
   registers (RAX, RCX, RDX, RSI, RDI, R8 to R11) plus the callee-saved registers it uses.
2. It executes `cld`, aligns RSP to 16 bytes, and passes the address of the `InterruptFrame` in RDI and
   the error code in RSI.
3. The epilogue restores the registers and RSP, discards the error code (`add $8, %rsp`), and returns
   with `iretq`.
4. In kernel mode no vector registers are saved; a `//pu:interrupt` function in a mode that allows floats
   must not use them, and the compiler reports an error if it does.

Handlers cannot be called directly or used as func values; their address is taken with
`x86.HandlerAddr(timer)` for the IDT.

### 10.5 `pluto/arch/x86` intrinsics

Each function in `pluto/arch/x86` is expanded inline to the instructions shown. Every one of them is a
compiler barrier (memory model section 5.2) and is never removed, even when its result is unused. Each
also has an assembly body for when its address is taken. Privileged instructions fault outside ring 0.

| Function | Expansion |
|---|---|
| `Cli()`, `Sti()`, `Hlt()`, `Pause()` | `cli`, `sti`, `hlt`, `pause` |
| `StiHlt()` | `sti; hlt` adjacent, so an interrupt cannot arrive between them |
| `SaveFlagsCli() uint64` | `pushfq; pop %rax; cli` |
| `RestoreFlags(f uint64)` | `push %rdi; popfq` |
| `Inb(port uint16) uint8`, `Inw(port uint16) uint16`, `Inl(port uint16) uint32` | `in %dx, %al/%ax/%eax` |
| `Outb(port uint16, v uint8)`, `Outw(port uint16, v uint16)`, `Outl(port uint16, v uint32)` | `out %al/%ax/%eax, %dx` |
| `Rdmsr(msr uint32) uint64` | `rdmsr`, EDX:EAX combined |
| `Wrmsr(msr uint32, v uint64)` | `wrmsr` |
| `ReadCR0() uint64`, `ReadCR2() uint64`, `ReadCR3() uint64`, `ReadCR4() uint64` | `mov %crN, %rax` |
| `WriteCR0(v uint64)`, `WriteCR3(v uint64)`, `WriteCR4(v uint64)` | `mov %rdi, %crN` |
| `Invlpg(addr uintptr)` | `invlpg (%rdi)` |
| `Lgdt(p *DescriptorTablePointer)`, `Lidt(p *DescriptorTablePointer)` | `lgdt (%rdi)`, `lidt (%rdi)` |
| `Ltr(sel uint16)` | `ltr %di` |
| `Cpuid(leaf, subleaf uint32) (eax, ebx, ecx, edx uint32)` | `cpuid` (results in RAX, RDX, RCX, RSI per section 3.3) |
| `Rdtsc() uint64`, `Rdtscp() (tsc uint64, aux uint32)` | `rdtsc`, `rdtscp` |
| `Swapgs()` | `swapgs` |
| `ReadGSBase() uintptr`, `WriteGSBase(v uintptr)` | `rdmsr`/`wrmsr` of `IA32_GS_BASE` (or `rdgsbase`/`wrgsbase` with `--cpu` support for FSGSBASE) |
| `GSLoad64(off uintptr) uint64`, `GSLoad32`, `GSStore64(off uintptr, v uint64)`, `GSStore32` | `mov %gs:(%rdi), ...` |
| `FPUBegin(area *FPUState)`, `FPUEnd(area *FPUState)` | `fxsave`/`fxrstor` (or `xsave`/`xrstor`), with `clts` |
| `HandlerAddr(h any) uintptr` | `lea handler(%rip), %rax`; `h` must name a `//pu:interrupt` function, checked at compile time |

```go
type DescriptorTablePointer packed [10]byte {
	limit uint16
	base  uint64
}
```

`pluto/arch/x86` is available on amd64 in every build mode; only `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`
are useful outside ring 0.

### 10.6 Per-CPU variables (proposed)

A package-level variable marked `//pu:percpu` has one copy per processor:

```go
//pu:percpu
var current *Thread

//pu:percpu
var runQueue RunQueue
```

- The compiler places such variables in the section `.percpu`, which the kernel's linker script links
  at virtual address 0 (with a load address elsewhere), so each symbol's address is its offset in the
  per-CPU block. The kernel allocates one block per processor, copies the section's initial contents
  into it, and points `GS` base at it.
- The first word of every block holds the block's own address (`pluto.percpu.self`, reserved by the
  compiler at offset 0).
- A read or write of a per-CPU variable of at most 8 bytes compiles to one `GS`-relative `MOV` using the
  symbol's offset as `disp32` (`R_X86_64_32S`), and so cannot be split by an interrupt on the same
  processor. Larger values are accessed through their address.
- `&v` yields `%gs:0` (the self pointer) plus the offset: an ordinary pointer into the current
  processor's copy, which stays valid after a migration but then refers to the old processor's copy.
- Per-CPU variables may not be initialised by code that runs in the package initialiser (they have no
  processor yet); only constant initialisers are allowed.
- In hosted builds `//pu:percpu` is a compile error.

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
