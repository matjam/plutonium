# The Plutonium Language

**Version:** 0.2 (draft)
**Date:** 2026-10-09

Plutonium is a systems programming language with Go's syntax and most of Go's semantics, minus the parts
that need a garbage collector or reflection. Puroutines and channels run on a scheduler that only programs
using them carry, and that a kernel supplies itself. It is meant for operating system
kernels, drivers, boot code, allocators and other code that runs without a managed runtime. It is the
language PlutoniumOS is written in.

This document describes Plutonium as differences from Go. **Where this document is silent, the
[Go language specification](https://go.dev/ref/spec) as of Go 1.27 applies.** Plutonium has one language
version: every Go feature up to 1.27 that is not removed or changed below is part of the language.

## Contents

1. [Goals](#1-goals)
2. [Source files and packages](#2-source-files-and-packages)
3. [What Plutonium removes from Go](#3-what-plutonium-removes-from-go)
4. [What Plutonium changes](#4-what-plutonium-changes)
5. [What Plutonium adds](#5-what-plutonium-adds)
6. [Memory and allocation](#6-memory-and-allocation)
7. [Runtime faults and panic](#7-runtime-faults-and-panic)
8. [Directives](#8-directives)
9. [Assembly and C](#9-assembly-and-c)
10. [The unsafe package](#10-the-unsafe-package)
11. [Build modes and targets](#11-build-modes-and-targets)
12. [Standard library](#12-standard-library)

## 1. Goals

- Read like Go. A Go programmer should understand Plutonium code without a manual.
- Compile like C: no garbage collector, no hidden allocation, no runtime the program did not ask for. A
  program that never starts a puroutine or makes a channel contains no scheduler.
- Make the costs visible. Every allocation is a call you can see; every memory-mapped register access is
  marked in its type.
- Give kernel code the tools it needs in the language: bit-exact layouts, volatile access, alignment
  control, ring buffers, and a fatal-error path that works before any allocator exists.
- Generate fast code. The compiler has its own optimising backend and is measured against Rust.

## 2. Source files and packages

Source files use the `.p` extension and are UTF-8. Packages, imports, visibility and initialization order
follow Go. Standard library packages live under the `pluto/` import path (`pluto/fmt`, `pluto/alloc`).

A program's entry point is `main` in package `main`. It may return nothing, as in Go, or an `int64`, which
becomes the process exit status in hosted builds:

```go
package main

import "pluto/fmt"

func main() int64 {
	fmt.Printf("hello, %s\n", "world")
	return 0
}
```

Files are selected for a build by `//pu:build` constraints, which use Go's boolean syntax, and by file
name suffixes such as `_amd64.p` or `_linux.p`. See [Build modes and targets](#11-build-modes-and-targets)
for the tags that are set.

```go
//pu:build amd64 && !freestanding
```

## 3. What Plutonium removes from Go

Each of these is a compile error that names the construct and the reason.

| Removed | Reason | Use instead |
|---|---|---|
| `map[K]V`, `delete` | needs a garbage-collected hash table | `pluto/hash` |
| `recover` | needs an unwinder; `panic` aborts (section 7) | return errors |
| `import "reflect"` | needs runtime type metadata | type switches, generics |
| `complex64`, `complex128`, `complex`, `real`, `imag` | rarely useful in systems code; keeps the backend small | a struct of two floats |
| runtime string concatenation (`s + t` where either is not a constant) | allocates | `pluto/strings.Builder` with an allocator |
| conversions between `string` and `[]byte` or `[]rune`, and `string(r)` for a non-constant rune | allocate a copy | `unsafe.String`, `unsafe.StringData`, `pluto/strings` |
| `append` beyond capacity | the slice does not know its allocator | `pluto/vec.Vec[T]` (section 6) |

Constant string concatenation (`const greeting = "hello, " + name`) remains, since it happens at compile
time.

## 4. What Plutonium changes

### 4.1 `panic` aborts

`panic(v)` stops the program. It does not unwind the stack and deferred calls do not run. See
[section 7](#7-runtime-faults-and-panic).

### 4.2 `new` and `make` allocate from an allocator

`new(T)` and `make([]T, len)` / `make([]T, len, cap)` allocate from the current allocator
(section 6). Each accepts an optional final allocator argument:

```go
n := new(Node)                  // current allocator
m := new(Node, arena)           // explicit allocator
buf := make([]byte, 4096)       // current allocator
tmp := make([]byte, 0, 64, scratch)
```

The checker tells an allocator from a capacity by its type. Memory from `new` and `make` is zeroed and is
not freed automatically; free it with `alloc.Free` or use `owned[T]` (section 5.6).

### 4.3 `append` works within capacity

`append(s, x...)` stores into `s`'s spare capacity and returns the longer slice. If the result would exceed
`cap(s)`, the program panics. Growable sequences are `pluto/vec.Vec[T]`, which records its allocator.

### 4.4 Interfaces hold a word or a frame pointer

Interfaces, including `any` and `error`, work as in Go: method sets, implicit satisfaction, type
assertions, type switches. An interface value is two words: a pointer to a static method table (or, for
`any`, a type descriptor) and a data word.

- A value that fits in a word (integers, floats, booleans, pointers, and `owned`/`shared`/`synced`
  pointers) is stored in the data word.
- A larger value (a string, slice, array or struct) is copied into a temporary in the stack frame of the
  function that performs the conversion, and the data word points at that copy. The interface value is
  valid until that function returns, exactly like a pointer to a local variable.

Returning an interface that holds such a frame copy, or storing it in a package-level variable, is a
compile error where the compiler can see it. To keep a large value in an interface beyond the current
function, store a pointer to it instead.

Type descriptors exist only for types that are converted to an interface. Besides identifying the type,
a descriptor records its size, alignment and kind, and its element type and length or its field offsets
and types, which is what generic printing, hashing and equality in the standard library use. Field names
are included unless the build strips them. There is no reflection package.

Inside generic code, `switch any(x).(type)` on a value of a type parameter is decided at compile time for
each instantiation, so it costs nothing at run time.

### 4.5 Closures do not outlive their frame

Function literals may capture variables of the enclosing function, by reference as in Go. Captured
variables stay in the enclosing frame; nothing is moved to the heap. A closure that captures variables is
therefore valid only while the enclosing function is running:

- Calling it, passing it as an argument, assigning it to a local variable, and deferring it are allowed.
- Returning it, storing it in a package-level variable, or storing it through a pointer is a compile
  error.

A function literal that captures nothing is an ordinary function value and may be used anywhere.

### 4.6 Integer behaviour

As in Go, signed and unsigned arithmetic wraps on overflow, shifts by at least the width of the operand
yield 0 (or -1 for a negative signed value shifted right), and integer division by zero panics.
`int`, `uint` and `uintptr` are 64 bits wide on all current targets.

### 4.7 `go` starts a puroutine

`go f(x, y)` starts a puroutine, a lightweight thread of execution scheduled by the program's
scheduler. As in Go, the function value and arguments are evaluated and copied when the `go` statement
runs.

- Each puroutine has a fixed-size stack: stacks cannot grow or move, because nothing knows where the
  pointers into them are. A guard page below the stack turns an overflow into a fault. The default size
  is 64 KiB; `rt.Go(f, size)` starts a puroutine with an explicit stack size instead. For a
  spawned function whose call graph has no recursion and no indirect calls, the compiler computes the
  stack it needs and uses that instead; otherwise it uses the default, and `-stackreport` lists the
  functions it could not size.
- A function literal that captures variables cannot be started with `go`: its captures live in the
  spawning frame, which may be gone before the puroutine runs (section 4.5). Pass the values as
  arguments: `go func(n int) { work(n) }(n)`.
- Scheduling is cooperative: a puroutine runs until it blocks on a channel, a lock or I/O, or calls
  `rt.Yield()`. Preempting a puroutine that never blocks is up to the platform (for example a kernel's
  timer interrupt).

The scheduler sits behind an interface in `pluto/rt` with four operations: start a puroutine, park the
current one, make a parked one runnable, and yield. Hosted builds get a scheduler that runs puroutines on
a pool of OS threads. A freestanding program provides its own with `rt.SetScheduler` before its first
`go` statement or channel operation; until then they panic with "no scheduler". A program that never uses
`go` or channels contains none of this.

Stacks come from the default allocator, or from `rt.Go(f, size, a)` with an explicit one, and are freed
when the puroutine returns.

### 4.8 Channels

`chan T`, `<-chan T`, `chan<- T`, the send and receive operators, `select`, `close`, `len`, `cap` and
`for v := range ch` work as in Go, on top of the scheduler (section 4.7). Sending on a closed channel and
closing a closed or nil channel panic; receiving from a closed channel yields the zero value and `false`.

`make(chan T)` and `make(chan T, n)` allocate the channel and its buffer from the current allocator, or
from an allocator given as the final argument: `make(chan Msg, 16, arena)`.

A channel value is a reference counted like `synced[T]` (section 5.6), since a channel always has
several users: copying it counts a reference, and when the last reference goes away the channel and its
buffer are freed. Values still buffered at that point are discarded without running `Drop`.

## 5. What Plutonium adds

### 5.1 Default parameter values

The trailing parameters of a function may have default values. A call may omit any suffix of the
defaulted parameters; the default expressions are evaluated at each call, in the callee's package scope,
after the explicit arguments.

```go
func New[T any](a Allocator = Default()) *T

n := alloc.New[Node]()          // a = alloc.Default(), evaluated now
m := alloc.New[Node](arena)
```

A default may refer to package-level names visible where the function is declared, but not to other
parameters. Variadic functions cannot have defaults. Function types do not record defaults: a function
value is called with all of its arguments.

### 5.2 `volatile[T]`

A variable of type `volatile[T]` is accessed with exactly one load or store per read or write, in program
order. The compiler never removes, merges or reorders volatile accesses, and never moves other memory
accesses across them. `T` must be an integer, boolean, pointer or integer-backed packed type.

```go
type UART struct {
	data   volatile[uint8]
	status volatile[uint8]
}

func put(u *UART, b uint8) {
	for u.status&0x20 == 0 {
	}
	u.data = b
}
```

Reading a `volatile[T]` yields a `T`, and any value assignable to `T` may be assigned to it. Taking the
address keeps the qualifier: `&u.data` has type `*volatile[uint8]`. A pointer to MMIO is made with a
conversion:

```go
lapic := (*volatile[uint32])(unsafe.Pointer(uintptr(0xFEE000B0)))
*lapic = 0 // end of interrupt
```

Volatile is not atomic and implies no barrier between processors. Use `pluto/sync/atomic` for that.

### 5.3 `aligned[T, N]`

`aligned[T, N]` stores a `T` on an `N`-byte boundary. `N` must be a constant power of two. The type is
`N`-aligned wherever it appears: in a package variable, a local variable, a struct field, an array element
or an allocation. Its size is `T`'s size rounded up to a multiple of `N`. As with `volatile`, it reads and
writes as `T`.

```go
var pml4 aligned[[512]uint64, 4096]
var dma  aligned[[2048]byte, 64]
```

### 5.4 `ring[T, N]`

`ring[T, N]` is a fixed-capacity ring buffer stored inline: two `uint64` indexes followed by `[N]T`.
`N` must be a constant power of two. The zero value is an empty ring.

| Method | Result |
|---|---|
| `Push(v T) bool` | appends `v`; `false` if full |
| `Pop() (T, bool)` | removes the oldest element; `false` if empty |
| `Peek() (T, bool)` | the oldest element without removing it |
| `Len() int` | number of elements |
| `Cap() int` | `N` |
| `Full() bool`, `Empty() bool` | |
| `Reset()` | empties the ring |

All methods have pointer receivers and are expanded inline. A ring is not safe for concurrent use; a
single-producer, single-consumer queue between an interrupt handler and a task can be built from
`volatile` indexes.

### 5.5 `packed` types

A packed type lays out fields at exact bit positions, for hardware registers, descriptor tables, on-disk
structures and protocol headers.

```go
packed [le|be] [Backing] { fields }
```

- **Backing** is an integer type, a byte array `[N]byte`, or `[]byte`; it may be omitted. With an integer
  or array backing the type is exactly that size and fields must fit. With `[]byte` or no backing the size
  is the total field width rounded up to whole bytes.
- **Layout** is LSB-first in declaration order: the first field starts at bit 0. There is no padding
  between fields except where a field's type is `aligned[T, N]`, which moves it to an `N`-byte boundary.
- **Fields** are one of:
  - `name bits[T, N]`: an `N`-bit field read as `T`. `T` is an integer type at least `N` bits wide, or
    `bool` with `N` equal to 1. Unsigned fields read zero-extended, signed fields sign-extended. Writes keep
    the low `N` bits of the value.
  - `name bits { A B C }`: a group of one-bit `bool` flags, accessed as `x.name.A`.
  - `name T` where `T` is an integer, boolean, float, packed type or array of those: a byte-level field
    of `T`'s full size, at the next bit position (it need not be byte-aligned).
  - `_` names reserve bits.
- **Byte order**: `le` and `be` say how the backing is stored in memory. Field bit positions always count
  from the least significant bit of the value after byte-swapping. With no annotation the target's native
  order applies, and accesses compile to plain loads, masks and shifts.
- **Conversions**: a packed type converts to and from an integer type of the same size, or a byte array
  of the same length. That is how a raw register value becomes a typed view.
- **Literals** name their fields: `ControlReg{mode: 5, enable: true}`.
- Packed values are comparable; comparison compares the storage, and reserved bits are always zero.

```go
type ControlReg packed uint32 {
	mode     bits[uint, 4]
	enable   bits[bool, 1]
	priority bits[uint, 3]
	_        bits[uint, 24]
}

type IPv4Header packed be [20]byte {
	ihl      bits[uint8, 4]
	version  bits[uint8, 4]
	tos      uint8
	length   uint16
	// ...
}

reg := ControlReg(mmio.Load())
reg.enable = true
mmio.Store(uint32(reg))
```

### 5.6 Owned and counted pointers

Raw pointers `*T` work as in C: they never free anything and may point at the stack, statics, memory-mapped
devices or the heap. For heap objects whose lifetime the compiler should manage there are three pointer
types:

| Type | Meaning |
|---|---|
| `owned[T]` | The only owner of a heap `T`. Freed when its owner goes out of scope. |
| `shared[T]` | A reference-counted heap `T` used on one processor. Freed when the last reference goes. |
| `synced[T]` | Like `shared[T]` but the count is atomic, so references may cross processors. |

All three are one word, dereference like `*T` (`p.field`, `*p`, method calls), and are created by
`alloc.Owned[T]()`, `alloc.Shared[T]()` and `alloc.Synced[T]()`, each with an optional allocator argument.
Each allocation records its allocator in a hidden header word, so it is freed to the right allocator even
if the default has changed since.

**owned[T]** has move semantics. Assigning it, passing it to an `owned[T]` parameter, returning it, or
storing it in an `owned[T]` field moves ownership and makes the source unusable; using a moved-from
variable is a compile error. When an `owned[T]` variable goes out of scope still holding its value, the
compiler calls `Drop()` on it (if `T` has a `Drop()` method) and frees it. Struct fields of type
`owned[T]` are dropped when the struct that holds them is dropped or freed.

```go
type Tree struct {
	left, right owned[Tree]
	key         int
}

func build(depth int) owned[Tree] {
	t := alloc.Owned[Tree]()
	if depth > 0 {
		t.left = build(depth - 1)   // moves into the field
		t.right = build(depth - 1)
	}
	return t                        // moves to the caller
}                                   // nothing freed: t was moved
```

An `owned[T]` converts to `*T` to lend the object to code that does not take ownership. Such a borrowed
pointer must not outlive the owner; the compiler does not check this.

**shared[T]** and **synced[T]** count references. Copying one increments the count; a reference going
out of scope or being overwritten decrements it; at zero `Drop()` runs and the object is freed. Cycles
are not collected.

### 5.7 Methods with type parameters

A method may declare type parameters of its own, in addition to those of its receiver's type:

```go
func (v *Vec[T]) Map[U any](f func(T) U, a alloc.Allocator = alloc.Default()) Vec[U]

lengths := names.Map[int](func(s string) int { return len(s) })
lengths := names.Map(func(s string) int { return len(s) }) // U inferred
```

Such methods are instantiated for each set of type arguments used, like generic functions. They are
called directly and never through an interface: a method with its own type parameters does not count
towards implementing an interface, and an interface cannot declare one. This is the restriction that lets
Plutonium allow them where Go does not, since every instantiation is visible to the compiler.

### 5.8 Standard bit operations

The `pluto/bits` package mirrors Go's `math/bits` (`OnesCount`, `LeadingZeros`, `TrailingZeros`,
`RotateLeft`, `ReverseBytes`, `Len`, `Add64`, `Mul64` and so on) and adds bit manipulation through a
pointer: `bits.Set(&flags, 3)`, `bits.Clear`, `bits.Toggle`, `bits.Test`, `bits.Extract(x, lo, width)`,
`bits.Insert`. The compiler expands them to single instructions where the target has one. These replace
the March draft's `bits[V]` lens type.

## 6. Memory and allocation

Plutonium never allocates heap memory unless the program asks: through `new`, `make`, the `pluto/alloc`
functions, `owned`/`shared`/`synced` constructors, or library code that takes an allocator. Local
variables, temporaries and interface frame copies live on the stack. Package-level variables live in
static storage (`.data` or `.bss`).

### 6.1 Allocators

```go
package alloc

type Allocator interface {
	Alloc(size, align uintptr) unsafe.Pointer      // zeroed memory, or nil
	Free(p unsafe.Pointer, size, align uintptr)
}

func Default() Allocator                // the current allocator
func SetDefault(a Allocator)            // replaces it for all later allocations
func Use(a Allocator) (restore func())  // replaces it until restore is called

func New[T any](a Allocator = Default()) *T
func Free[T any](p *T, a Allocator = Default())   // calls p.Drop() first if T has one
func Owned[T any](a Allocator = Default()) owned[T]
func Shared[T any](a Allocator = Default()) shared[T]
func Synced[T any](a Allocator = Default()) synced[T]
```

`Use` scopes an allocator to a region of code without changing the code inside it:

```go
func handle(req *Request) {
	defer alloc.Use(req.arena)()
	parse(req) // every allocation through Default() lands in the arena
}
```

### 6.2 The system allocator

In hosted builds the default allocator is a slab allocator over `mmap`. In freestanding builds there is
none: allocating before the program calls `alloc.SetDefault` panics with "no allocator". A kernel sets it
once its page allocator is ready:

```go
func kernelMain(info *BootInfo) {
	pages := initPageFrames(info.memoryMap)
	alloc.SetDefault(slab.New(pages))
	// new, make and the alloc functions work from here on
}
```

The slab allocator takes its pages from an `alloc.PageProvider`, so a kernel can reuse it on top of its own
physical page allocator.

## 7. Runtime faults and panic

The following stop the program through the same path as `panic`:

- an index or slice expression out of range,
- integer division or remainder by zero,
- a failed type assertion with a single result,
- `append` beyond capacity,
- an allocation with no allocator set,
- a `go` statement or channel operation with no scheduler set,
- sending on a closed channel, or closing a closed or nil channel,
- overflowing a puroutine's stack (the guard page faults),
- `panic(v)`.

Panicking prints `panic: ` followed by the value (and, for faults, what failed and where) and stops. It
does not unwind and deferred functions do not run. In hosted builds the process exits with status 2. In
freestanding builds the compiler calls the function registered with `rt.SetPanicHandler`; the default
handler disables interrupts and halts the processor.

Dereferencing a nil pointer is not checked by the compiler; it faults in hardware wherever address zero is
unmapped, which is true in hosted builds and should be arranged by kernels.

Bounds checks can be removed from a function with `//pu:nobounds`, for code that has been proven correct
and must be as fast as possible. The optimiser removes most bounds checks by itself.

## 8. Directives

Directives are line comments starting with `//pu:` with no space, placed in the doc comment of the
declaration they apply to (or at the top of the file for `//pu:build`).

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:build expr` | file | include the file only when `expr` holds |
| `//pu:inline` | function | always inline calls to this function |
| `//pu:noinline` | function | never inline it |
| `//pu:nobounds` | function | omit bounds checks in its body |
| `//pu:extern name` | body-less function | the function is the external symbol `name` (C ABI) |
| `//pu:export name` | function | also export the function under the symbol `name` |
| `//pu:section name` | function or package variable | place it in the named section |
| `//pu:interrupt` | function | an x86-64 interrupt handler: saves all registers, returns with `iretq` |
| `//pu:noreturn` | function | the function never returns (lets the optimiser drop code after calls) |

## 9. Assembly and C

A function declared without a body is implemented outside Plutonium.

- **Assembly**: a `.s` file in the same package, in GNU assembler syntax, assembled by the system
  assembler. The symbol of a Plutonium function is its import path and name, `"pluto/syscall.Syscall"`,
  quoted in assembly because of the slash. Methods are `"path.Type.Method"` (value receiver) and
  `"path.(*Type).Method"` (pointer receiver).
- **C**: with `//pu:extern name`, the function is the C symbol `name`. Plutonium uses the System V AMD64
  calling convention everywhere, so C code compiled separately, such as the reference Lua interpreter,
  links in directly. `//pu:export name` makes a Plutonium function callable from C under `name`.

```go
//pu:extern lua_pushinteger
func pushInteger(L *State, n int64)
```

The full calling convention and data layouts are in [abi.md](abi.md).

## 10. The unsafe package

`unsafe` is Go's: `Pointer`, `Sizeof`, `Alignof`, `Offsetof`, `Add`, `Slice`, `SliceData`, `String`,
`StringData`. Converting between `unsafe.Pointer`, `uintptr` and pointer types follows Go's rules,
without Go's restrictions that exist only for the garbage collector. Every package that uses raw memory
imports `unsafe`, so such code is easy to find.

## 11. Build modes and targets

| Mode | Meaning | Tags set |
|---|---|---|
| `hosted` (default) | a Linux program: `_start` sets up arguments and calls `main` | `linux`, `hosted` |
| `freestanding` | no operating system; the program provides its entry point and linker script | `freestanding`, `none` |

Targets are named by triples: `x86_64-linux` (default) and `x86_64-none`; `aarch64-linux` and
`aarch64-none` are planned. The architecture tag (`amd64`, `arm64`) is always set.

Output formats are ELF executables (default), flat binaries (`-format binary`) and PE32+ for UEFI
(`-format pe32+`).

## 12. Standard library

| Package | Contents |
|---|---|
| `pluto/rt` | panic hook, program entry, memory copy and compare primitives the compiler calls, the scheduler interface, `Go`, `Yield` |
| `pluto/sync` | `Mutex`, `RWMutex`, `WaitGroup`, `Once` over the scheduler |
| `pluto/alloc` | allocators, the default allocator, `New`/`Free`/`Owned`/`Shared`/`Synced` |
| `pluto/fmt` | `Printf`, `Fprintf`, `Sprintf` into a caller's buffer, `Println`; integer, float, string, pointer verbs with width, precision and flags |
| `pluto/io` | `Reader`, `Writer`, buffered I/O |
| `pluto/os` | files, arguments, environment, exit (hosted) |
| `pluto/syscall` | raw Linux system calls (hosted) |
| `pluto/strings`, `pluto/bytes` | searching, comparison, `Builder` with an allocator |
| `pluto/bits` | bit operations (section 5.8) |
| `pluto/math` | floating-point functions |
| `pluto/hash` | hash maps and sets with an allocator |
| `pluto/vec` | growable `Vec[T]` with an allocator |
| `pluto/sort` | sorting without allocation |
| `pluto/sync/atomic` | atomic integers and pointers |
| `pluto/unicode/utf8` | UTF-8 encoding and decoding |

`pluto/os` and `pluto/syscall` are not available in freestanding builds.
