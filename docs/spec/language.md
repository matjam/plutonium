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
13. [Errors](#13-errors)

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
| `recover` | needs an unwinder; `panic` aborts (section 7) | return errors (section 13) |
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

`new(T)`, `new(v)` and `make([]T, len)` / `make([]T, len, cap)` allocate from the current allocator
(section 6). Each accepts an optional final allocator argument:

```go
n := new(Node)                  // current allocator
m := new(Node, arena)           // explicit allocator
k := new(Node{val: 1}, arena)   // a new variable holding a value
buf := make([]byte, 4096)       // current allocator
tmp := make([]byte, 0, 64, scratch)
```

As in Go 1.26, `new(v)` allocates a variable of `v`'s type (an untyped constant takes its default type) and
stores `v` in it; `new(T{...})` is how a composite literal outlives its frame. Whether the first operand is a
type decides between `new(T, a)` and `new(v, a)`; `v` is evaluated before the allocator. The allocator is
any value assignable to `alloc.Allocator`, and a final argument of `make` is a capacity if it is an integer
or untyped numeric constant and an allocator otherwise. Memory from `new(T)` and `make` is zeroed; none of
it is freed automatically; free it with `alloc.Free` or use `owned[T]` (section 5.6). A nil allocator panics.

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

The initializer of a package-level variable is the exception: it runs once, and its results must outlive
package initialization, so a conversion there copies the value into static storage instead of a frame.
`var ErrBad error = parseError{line: 1}` and `var names any = []string{"a", "b"}` are valid. The same
holds for `&T{...}` and slice literals in package-level initializers, which also get static storage.

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
- Puroutines run in parallel and are preempted: one that runs for a full time slice without blocking is
  suspended at whatever instruction it has reached and another runs in its place. Since there is no
  garbage collector, every instruction is a safe place to stop; the full register state is saved. Code in
  the runtime that must not be interrupted (scheduler and allocator locks) disables preemption for its
  own duration. Blocking on a channel, a lock or I/O, or calling `rt.Yield()`, gives up the processor
  early.

The scheduler sits behind an interface in `pluto/rt` with four operations: start a puroutine, park the
current one, make a parked one runnable, and yield. Hosted builds get a scheduler that runs puroutines on
one worker thread per CPU. Each worker has its own run queue, and an idle worker steals half the queue of
a busy one, so load spreads across processors without any work by the program. A worker that enters a
blocking system call hands its queue to another worker, and waits for I/O go through one epoll poller.
Preemption uses a timer signal. A freestanding program provides its own scheduler with `rt.SetScheduler`
before its first `go` statement or channel operation (a kernel uses per-CPU run queues and its timer
interrupt the same way); until then they panic with "no scheduler". A program that never uses `go` or
channels contains none of this.

Values that cross between puroutines must be safe to use from several processors at once. A
`shared[T]`, or any value containing one, cannot be passed to a `go` statement or sent on a channel, since
its reference count is not atomic: use `synced[T]` (section 5.6). Raw pointers are not checked, as in C;
data they reach must be protected with `pluto/sync` or `pluto/sync/atomic`.

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

### 4.9 Memory model

The memory model is Go's, specified with Plutonium's changes in [memory-model.md](memory-model.md). In
brief:

- `go` statements, channel operations, `pluto/sync` locks, `WaitGroup` and `Once`, and
  `pluto/sync/atomic` operations synchronise exactly as in Go. Atomics are sequentially consistent.
- A data race is a bug but not undefined behaviour. A racy read of a naturally aligned value of at most a
  word observes some value that was written to it; larger values (strings, slices, interfaces, structs)
  may tear. The compiler may not assume that a program has no races.
- A packed value is a single memory location: writing one field rewrites its neighbours, so two
  puroutines writing different fields of one packed value race.
- `volatile[T]` accesses are never removed, merged or reordered with one another, and atomic operations
  are compiler barriers. Volatile accesses do not synchronise between processors, but they are enough
  for an interrupt handler and the code it interrupts on the same processor to share data.
- Any scheduler, including a kernel's own (section 4.7), must make starting a puroutine and readying a
  parked one synchronising operations, so that these rules hold however puroutines are scheduled.

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
towards implementing an interface, and an interface cannot declare one. Plutonium follows Go 1.27 here:
the rules are Go's, and since every instantiation is visible to the compiler, each is an ordinary
function in the output.

As with a generic function, a method value `x.M` or method expression `T.M` of such a method must be
instantiated, either explicitly (`names.Map[int]`) or from the function type it is assigned to.

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

`Use` scopes an allocator to a region of code without changing the code inside it. The override belongs
to the puroutine that calls `Use`: `Default()` returns it there and the program-wide default everywhere
else, and a new puroutine starts with the program-wide default.

```go
func handle(req *Request) {
	defer alloc.Use(req.arena)()
	parse(req) // every allocation through Default() lands in the arena
}
```

### 6.2 The system allocator

In hosted builds the default allocator is a slab allocator over `mmap`, safe to use from several
puroutines at once: each worker thread allocates from its own cache in front of the shared slabs.
`alloc.SetDefault` must likewise be given a thread-safe allocator once puroutines run in parallel; an
arena installed with `alloc.Use` belongs to the puroutine that installed it. In freestanding builds there is
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
| `//pu:interrupt` | function | an x86-64 interrupt handler: saves every register it may change, returns with `iretq` (section 11.4) |
| `//pu:noreturn` | function | the function never returns (lets the optimiser drop code after calls) |
| `//pu:fpu` | function | may use floats in kernel mode; the caller has saved the FPU state (section 11.3) |
| `//pu:percpu` | package variable | one copy per processor, addressed through `GS`; kernel mode only (section 11.5) |

## 9. Assembly and C

A function declared without a body is implemented outside Plutonium.

- **Assembly**: a `.s` file in the same package, in GNU assembler syntax, assembled by the system
  assembler. The symbol of a Plutonium function is its import path and name, `"pluto/syscall.Syscall"`,
  quoted in assembly because of the slash. Methods are `"path.Type.Method"` (value receiver) and
  `"path.(*Type).Method"` (pointer receiver).
- **C**: with `//pu:extern name`, the function is the C symbol `name`, called with the System V AMD64
  calling convention, so C code compiled separately, such as the reference Lua interpreter, links in
  directly. `//pu:export name` makes a Plutonium function callable from C under `name`. Calls between
  Plutonium functions use an internal convention that passes strings, slices, interfaces and small
  structs in registers; the compiler translates at the C boundary, classifying structs the System V way
  and returning several results as a struct of the results (abi.md section 9.1). Strings, slices,
  interfaces and func values have no C equivalent and are compile errors in `//pu:extern` and
  `//pu:export` signatures, as are variadic functions.
- **C sources**: `.c` files in a package are compiled with the system C compiler and linked in, and `.o`
  files are linked as they are (abi.md section 9.2). There is no C library.

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

### 11.1 Modes

The mode is chosen with `--mode`; by default it follows the target (`hosted` for `x86_64-linux`,
`freestanding` for `x86_64-none`).

| Mode | Meaning | Tags set |
|---|---|---|
| `hosted` | a Linux program: `_start` sets up arguments and calls `main` | `linux`, `hosted` |
| `freestanding` | no operating system; the program provides its entry point and linker script | `freestanding`, `none` |
| `kernel` | `freestanding`, plus code generation for ring 0 (section 11.2) | `freestanding`, `none`, `kernel` |

Targets are named by triples: `x86_64-linux` (default) and `x86_64-none`; `aarch64-linux` and
`aarch64-none` are planned. The architecture tag (`amd64`, `arm64`) is always set.

Output formats are ELF executables (default) and flat binaries (`--format binary`). PE32+ for UEFI
(`--format pe32+`) is planned; it needs position-independent code (abi.md section 7).

### 11.2 Kernel mode

`pluto build --target x86_64-none --mode kernel` builds code that can run in ring 0 with interrupts
enabled and no vector state saved on entry. It differs from `freestanding` in three ways, specified in
[abi.md section 10](abi.md#10-kernel-mode):

- **No red zone.** Plutonium code never stores below the stack pointer in any mode (the backend
  subtracts from RSP before using stack memory, and leaf functions without stack slots have no frame), so
  nothing changes for Plutonium code. Assembly and C linked into a kernel must not use the red zone
  either.
- **No floating point** (section 11.3).
- **Kernel code model.** The image is linked in the top 2 GiB of the address space. The compiler's
  addressing (RIP-relative symbol addresses, and absolute sign-extended 32-bit displacements for
  `sym+disp32(,index,scale)`) is correct there without change. Without `--ldscript`, the build links with
  a default kernel linker script (abi.md section 10.3) that places the image at 1 MiB physical and
  `0xFFFFFFFF80100000` virtual.

Kernel mode also enables per-CPU variables (section 11.5). `pluto/arch/x86` (section 11.4) and
`//pu:interrupt` handlers work in every mode; in kernel mode a handler without `//pu:fpu` saves no vector
registers. `pluto/os` and `pluto/syscall` are unavailable, as in every freestanding build. `pluto run`
refuses kernel builds.

### 11.3 Floating point in kernel mode

An interrupt or system call that enters the kernel does not save the SSE and x87 registers, so kernel code
must not touch them. In kernel mode, any floating-point operation (arithmetic, comparison, conversion,
a float constant, passing or returning a float, converting a float to an interface) in a function the
program uses is a compile error. "Passing" counts at the level of the calling convention: a struct
parameter whose float fields would travel in XMM registers (one of at most four leaves, abi.md section 3)
is an error, while one passed by reference is not. Float types may still appear, and copying float-typed
values (whole structs, fields, array elements, zero values) is allowed, since it uses integer registers.
Because the compiler only generates the functions a program reaches, a float function in an imported
package that the kernel never calls costs nothing, and the standard library keeps its float code behind
`//pu:build !kernel` (in `pluto/fmt`, a float argument prints as its bits in kernel builds). Package-level
arrays and structs initialised with constants, such as `pluto/math`'s coefficient tables, are written into
the image, so importing such a package adds no float code to package initialisation.

A function marked `//pu:fpu` may use floats in kernel mode, and so may the function literals inside it.
Whoever calls it must have saved the interrupted context's vector state and enabled the unit
(`x86.FPUBegin` and `x86.FPUEnd`); the compiler does not check this, but it never inlines a `//pu:fpu`
function into one without it. Soft-float was rejected: it would put an IEEE-754 library in every kernel
and hide a large cost behind ordinary arithmetic, for code that hardly ever wants floats.

### 11.4 `pluto/arch/x86`

The package `pluto/arch/x86` gives access to the instructions a kernel needs. Every
function in it is expanded inline to the instruction or short sequence it names, is never removed even if
its result is unused, and is a compiler barrier: no memory access is moved across it (see the memory
model). The full table, with expansions, is in [abi.md section 10.5](abi.md#105-plutoarchx86-intrinsics).

```go
package x86

func Cli()
func Sti()
func StiHlt()                     // sti; hlt with no gap for an interrupt
func Hlt()
func Pause()
func SaveFlagsCli() uint64
func RestoreFlags(f uint64)

func Inb(port uint16) uint8
func Inw(port uint16) uint16
func Inl(port uint16) uint32
func Outb(port uint16, v uint8)
func Outw(port uint16, v uint16)
func Outl(port uint16, v uint32)

func Rdmsr(msr uint32) uint64
func Wrmsr(msr uint32, v uint64)

func ReadCR0() uint64
func ReadCR2() uint64
func ReadCR3() uint64
func ReadCR4() uint64
func WriteCR0(v uint64)
func WriteCR3(v uint64)
func WriteCR4(v uint64)
func Invlpg(addr uintptr)

func Lgdt(p *DescriptorTablePointer)
func Lidt(p *DescriptorTablePointer)
func Ltr(sel uint16)

func Cpuid(leaf, subleaf uint32) (eax, ebx, ecx, edx uint32)
func Rdtsc() uint64
func Rdtscp() (tsc uint64, aux uint32)

func Swapgs()
func ReadGSBase() uintptr
func WriteGSBase(v uintptr)
func GSLoad64(off uintptr) uint64
func GSLoad32(off uintptr) uint32
func GSStore64(off uintptr, v uint64)
func GSStore32(off uintptr, v uint32)

func FPUBegin(area *FPUState)
func FPUEnd(area *FPUState)

func HandlerAddr(h any) uintptr   // h must name a //pu:interrupt function; for building the IDT

func NewDescriptorTablePointer(base uintptr, size int) DescriptorTablePointer

const GSBaseMSR = 0xC0000101       // IA32_GS_BASE
const KernelGSBaseMSR = 0xC0000102 // IA32_KERNEL_GS_BASE, exchanged by Swapgs

type DescriptorTablePointer packed [10]byte { limit uint16; base uint64 }
type FPUState aligned[[512]byte, 16] // the FXSAVE area
type InterruptFrame struct { RIP, CS, RFLAGS, RSP, SS uint64 }
```

`//pu:interrupt` handlers take a `*x86.InterruptFrame`, and an error code for the exceptions that push
one; abi.md section 10.4 gives the rules and the entry and exit sequence.

### 11.5 Per-CPU variables

A package-level variable marked `//pu:percpu` has one copy per processor:

```go
//pu:percpu
var current *Thread
```

The compiler places it in the `.percpu` section, which the kernel links at address 0 so that each
variable's address is its offset in a per-CPU block, and accesses it relative to the `GS` base, which the
kernel points at the current processor's block. A word-sized access is a single instruction. `&v` is a
pointer into the current processor's copy at the time it is taken, and keeps pointing there if GS
changes. Only constant initialisers are allowed, and `//pu:percpu` is a compile error outside kernel
mode. Code that reads and then writes a per-CPU variable must not migrate to another processor in
between, so it runs with preemption or interrupts disabled. Details, including how a kernel sets up each
processor's block, are in [abi.md section 10.6](abi.md#106-per-cpu-variables).

## 12. Standard library

| Package | Contents |
|---|---|
| `pluto/rt` | panic hook, program entry, memory copy and compare primitives the compiler calls, the scheduler interface, `Go`, `Yield` |
| `pluto/sync` | `Mutex`, `RWMutex`, `WaitGroup`, `Once` over the scheduler |
| `pluto/alloc` | allocators, the default allocator, `New`/`Free`/`Owned`/`Shared`/`Synced` |
| `pluto/fmt` | `Printf`, `Fprintf`, `Sprintf` into a caller's buffer, `Println`; integer, float, string, pointer verbs with width, precision and flags; `Errorf` with an allocator and `ErrorBuf` (section 13) |
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
| `pluto/testing` | tests and benchmarks for `pluto test` (hosted) |
| `pluto/errors` | `New` for sentinels, `Is`, `As`, `AsType`, `Unwrap`, `Join`, `Free` (section 13) |

`pluto/os` and `pluto/syscall` are not available in freestanding builds.

## 13. Errors

Errors are values of the predeclared `error` interface, handled as in Go: a function returns an `error`
last, `nil` means success, and callers write `if err != nil { return err }`. `pluto/errors` provides
`Is`, `As`, `Unwrap` and `Join` with Go's semantics, and `fmt.Errorf` wraps with `%w`. Since `panic`
aborts (section 7), errors are the only way to recover from a failure.

What differs from Go is where an error lives. An `error` is an interface, so an error value larger than a
word would be boxed in the converting function's frame (section 4.4), and Plutonium never allocates
implicitly (section 6). The rule that follows is:

**An error returned from a function is either a value that fits in a word, or a pointer to storage that
outlives the function.** That storage is one of:

| Storage | Used for | Example |
|---|---|---|
| the interface's data word | error codes | `syscall.Errno`, a kernel's own code type |
| static storage | sentinel errors | `var ErrNotFound = errors.New("not found")` in a package-level variable |
| a field of a longer-lived object | errors with context that belong to an owner | a parser that returns `&p.err`; `fmt.ErrorBuf` |
| memory from an allocator | errors with context that travel up the stack | `*os.PathError`, `fmt.Errorf`, `errors.Join` |

Returning a struct as an `error`, or a pointer to a local variable or composite literal, breaks the rule:
the first is a compile error (section 4.4), and the second leaves a dangling pointer.

### 13.1 Sentinels

`errors.New(text)` returns a distinct error each time it is called, held in static storage that package
`errors` reserves (512 errors). It is meant for package-level variables, whose initializers run once:

```go
var ErrNotFound = errors.New("not found")
```

`text` must be a constant or other static string. `errors.New` panics when its storage runs out, which
happens only if a program calls it repeatedly at run time; such code should declare a package-level
sentinel or use `fmt.Errorf`. A sentinel may also be written as a static literal,
`var ErrClosed = &errors.Text{Msg: "file already closed"}`, and a package-level initializer may convert
a struct to `error` directly, since its box is static (section 4.4).

### 13.2 Error codes

A named integer type with an `Error() string` method fits in the data word, so it can be returned from
anywhere and costs nothing. `syscall.Errno` is the standard one: its `Error` returns Linux's message for
the code, `Name` its symbol (`"ENOENT"`), and its `Is` method makes `errors.Is(err, os.ErrNotExist)`,
`os.ErrExist`, `os.ErrPermission` and `errors.ErrUnsupported` match the corresponding codes.

### 13.3 Errors with context

An error that records context (a path, a line and column) needs storage for it. Its message is formatted
when the error is made, into that storage, because `Error()` must return a string and cannot concatenate
one at run time.

- **Allocated.** `fmt.Errorf(format, args...)` allocates the error and its message in one block from the
  default allocator; `fmt.ErrorfIn(a, format, args...)` uses `a`. `errors.Join`/`JoinIn` and
  `os.NewPathError`/`NewPathErrorIn` work the same way, and so do `os.Open`, `os.Create` and `os.OpenFile`
  when they fail. Such an error has a `Free()` method. `errors.Free(err)` releases every allocated error
  in `err`'s tree and skips the others; a wrapper owns the errors it wraps, so free only the outermost
  one. Code that runs under an arena (`defer alloc.Use(&a).Restore()`) needs no `Free`.
- **Owned.** An object that performs operations (a parser, a device, a connection) keeps its last error in
  a field and returns a pointer to it. The error is valid while the object lives and until its next
  error. `fmt.ErrorBuf` is such a field: `buf.Errorf(format, args...)` formats into its 200-byte buffer,
  wraps the first `%w` operand, and returns the buffer as the error.

### 13.4 Wrapping, `Is` and `As`

`fmt.Errorf` and `ErrorBuf.Errorf` accept `%w`, which formats an error operand like `%v` and wraps it.
With one `%w` the result has `Unwrap() error`; with several, `Unwrap() []error`. `%w` in any other
formatting function prints `%!w(error)`.

`errors.Is(err, target)` reports whether any error in `err`'s tree equals `target` or has an
`Is(error) bool` method that reports true; it compares with `==` only when `target`'s dynamic type is
comparable. `errors.As` is generic, `func As[E any](err error, target *E) bool`, so the Go spelling
`errors.As(err, &perr)` works without reflection, with `E` inferred from `target`.
`errors.AsType[E error](err) (E, bool)` is Go 1.26's form.

### 13.5 Differences in the standard library

`os.File.Read`, `Write` and `Close` report a bare `syscall.Errno` (and `io.EOF`), not a `*PathError`, so
that I/O does not allocate. `strconv` reports `ErrSyntax` and `ErrRange` without Go's `*NumError`. An
`Errno` the table does not know reports `"unknown error"`.
