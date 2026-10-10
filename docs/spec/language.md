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
6. [Ownership](#6-ownership)
7. [Memory and allocation](#7-memory-and-allocation)
8. [Runtime faults and panic](#8-runtime-faults-and-panic)
9. [Directives](#9-directives)
10. [Assembly and C](#10-assembly-and-c)
11. [The unsafe package](#11-the-unsafe-package)
12. [Build modes and targets](#12-build-modes-and-targets)
13. [Standard library](#13-standard-library)
14. [Errors](#14-errors)

## 1. Goals

- Read like Go. A Go programmer should understand Plutonium code without a manual.
- Compile like C: no garbage collector, no hidden allocation, no runtime the program did not ask for. A
  program that never starts a puroutine or makes a channel contains no scheduler.
- Make the costs visible. Every allocation is a call you can see; every memory-mapped register access is
  marked in its type.
- Make ownership visible. Every piece of memory has one owner, named by its type, and is freed when that
  owner is dropped; looking, moving and sharing are written differently (section 6).
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
name suffixes such as `_amd64.p` or `_linux.p`. See [Build modes and targets](#12-build-modes-and-targets)
for the tags that are set.

```go
//pu:build amd64 && !freestanding
```

## 3. What Plutonium removes from Go

Each of these is a compile error that names the construct and the reason.

| Removed | Reason | Use instead |
|---|---|---|
| `map[K]V`, `delete` | needs a garbage-collected hash table, and a map value that secretly refers to memory nobody owns (section 6.1) | `pluto/hash.Map[K, V]`, an owner |
| `recover` | needs an unwinder; `panic` aborts (section 8) | return errors (section 14) |
| `import "reflect"` | needs runtime type metadata | type switches, generics |
| `complex64`, `complex128`, `complex`, `real`, `imag` | rarely useful in systems code; keeps the backend small | a struct of two floats |
| runtime string concatenation (`s + t` where either is not a constant) | allocates, and a `string` is a view that cannot own the result (section 6.3) | `pluto/strings.Buf`, an owner (section 6.6) |
| conversions between `string` and `[]byte` or `[]rune`, and `string(r)` for a non-constant rune | allocate a copy that nothing owns | `strings.Buf`, `unsafe.String`, `unsafe.StringData`, `pluto/strings` |
| `append` beyond capacity | a slice is a view and owns no array it could grow (section 6.3) | `pluto/vec.Vec[T]`, an owner (section 6.6) |

Constant string concatenation (`const greeting = "hello, " + name`) remains, since it happens at compile
time.

## 4. What Plutonium changes

### 4.1 `panic` aborts

`panic(v)` stops the program. It does not unwind the stack, deferred calls do not run, and owners are not
dropped (section 6.4). It calls the program's panic handler, which never returns. See
[section 8](#8-runtime-faults-and-panic).

### 4.2 `new` and `make` allocate from an allocator

`new(T)`, `new(v)` and `make([]T, len)` / `make([]T, len, cap)` allocate from the current allocator
(section 7). Each accepts an optional final allocator argument:

```go
n := new(Node)                  // current allocator
m := new(Node, arena)           // explicit allocator
k := new(Node{val: 1}, arena)   // a new variable holding a value
buf := make([]byte, 4096)       // current allocator
big := make([]byte, 0, 4096)    // length and capacity
tmp := make([]byte, 64, scratch) // length and allocator
all := make([]byte, 0, 64, scratch)
```

As in Go 1.26, `new(v)` allocates a variable of `v`'s type (an untyped constant takes its default type) and
stores `v` in it; `new(T{...})` is how a composite literal outlives its frame. Whether the first operand is a
type decides between `new(T, a)` and `new(v, a)`; `v` is evaluated before the allocator. The allocator is
any value assignable to `alloc.Allocator`, such as `&arena`, and the checker tells it from a capacity by
its type: a final argument of `make` is a capacity if it is an integer or untyped numeric constant and an
allocator otherwise (with four arguments the last is always the allocator). Memory from `new(T)` and
`make` is zeroed. With no allocator argument and no default allocator set, as in a freestanding program
before `alloc.SetDefault`, the allocation panics with "no allocator" (section 7.2); a nil allocator
argument panics too.

**What they return (proposal, needs sign-off).** Every allocation needs an owner or an arena (section
6.6), so `new` and `make` return owners:

- `new(T)` and `new(v)` return an `owned[T]` (section 5.6): the single owner of the new variable, which
  frees it when dropped. It dereferences like `*T`, so `n := new(Node); n.val = 1` reads as in Go.
- `make([]T, len)` and `make([]T, len, cap)` return a `vec.Vec[T]` of that length, which owns the array.
  `v.Slice()` lends it as a `[]T`, and `v[i]` and `len(v)` work on it directly through the indexing
  interface (section 6.7).
- `make(chan T)` and `make(chan T, n)` return a channel, which counts its references (section 4.8).

Memory that only an arena should own is allocated through the arena, which returns views: `a.New[T]()`
gives a `*T` and `a.Slice[T](n)` a `[]T` that live until the arena is reset or dropped. Today's compiler
still returns `*T` and `[]T` from `new` and `make`; that memory is freed only by an explicit `alloc.Free`
or an arena reset, which section 6.6 forbids outside the unchecked allocation layer.

### 4.3 `append` works within capacity

`append(s, x...)` stores into `s`'s spare capacity and returns the longer slice. If the result would exceed
`cap(s)`, the program panics. A slice is a view (section 6.3): the array behind it belongs to someone else,
so it cannot grow. Growable sequences are `pluto/vec.Vec[T]`, an owner that records its allocator.

If `T` is an owner type, `append(s, x)` moves `x` into the spare slot. `append(s, t...)` with another slice
of owners is a compile error, since it would duplicate the owners in `t` (section 6.9).

### 4.4 Interfaces hold a word or a frame pointer

Interfaces, including `any` and `error`, work as in Go: method sets, implicit satisfaction, type
assertions, type switches. An interface value is two words: a pointer to a static method table (or, for
`any`, a type descriptor) and a data word.

- A value that fits in a word (integers, floats, booleans, pointers, and `owned`/`shared`/`synced`/`weak`
  references) is stored in the data word.
- As in Go, a struct with exactly one field, or an array of length 1, whose field or element is a single
  pointer (a pointer, `unsafe.Pointer`, a function value, an `owned`/`shared`/`synced` reference, or
  recursively another such struct or array) is stored in the data word too, as that pointer. An error type
  that wraps one pointer, `type wrapErr struct{ d *detail }`, can therefore be returned as an `error`, and
  two such interface values are equal when they hold the same pointer. A struct with one integer field
  is not covered: it is boxed like any other struct.
- A larger value (a string, slice, array, struct or union) is copied into a temporary in the stack frame of the
  function that performs the conversion, and the data word points at that copy. The interface value is
  valid until that function returns, exactly like a pointer to a local variable.

An interface that holds such a frame copy is a view of the frame (section 6.3). Returning it, storing it in
a package-level variable or on the heap, or passing it to a function that keeps it, is a compile error. To
keep a large value in an interface beyond the current function, store a pointer to memory that lives long
enough instead, or move an owner into an owning interface (below).

The initializer of a package-level variable is the exception: it runs once, and its results must outlive
package initialization, so a conversion there copies the value into static storage instead of a frame.
`var ErrBad error = parseError{line: 1}` and `var names any = []string{"a", "b"}` are valid. The same
holds for `&T{...}` and slice literals in package-level initializers, which also get static storage.

**Owners in interfaces (proposal, needs sign-off).** An interface value has no static owner kind of its
own, so the kind comes from the interface type:

- An **ordinary interface** (`any`, `error`, `io.Reader`, any interface type not declared `owner`) never
  owns what it holds. Converting an owner, or a `shared`/`synced` reference, to an ordinary interface
  **lends** it: the interface is a view of the variable that was converted, valid while that variable
  holds the value, and the variable is not moved. This is what makes `fmt.Println(buf)` and
  `fmt.Printf("%v", m)` work on owners without giving them away. Such an interface follows the view rules
  of section 6.3: it cannot be returned or stored beyond the variable's life.
- An **owning interface** is declared with `owner` (section 6.4): `type Resource owner interface { Read(p
  []byte) (int, error) }`. It is itself an owner type: it moves, and dropping it drops the value inside.
  Converting an owner to it moves the owner in; converting a `shared` or `synced` reference copies it
  (count +1); converting a value or a view copies it, as for any interface, and dropping it then does
  nothing. Every owner type satisfies the implicit `Drop` of an owning interface, whether or not it
  declares a `Drop` method.
- A value larger than a word moved into an owning interface is boxed in the frame like any other, so the
  interface cannot leave the function. To return one, box the owner first: `new(f)` is an `owned[File]`,
  one word, and its methods are `*File`'s.
- A type assertion `x.(T)` from an owning interface to an owner type moves the value out of `x`; in the
  comma-ok form `v, ok := x.(T)`, `x` is moved only where `ok` is true, and it is still usable on the path
  where `ok` is false. A type switch with a binding (`switch v := x.(type)`) moves `x` into `v` in every
  clause. Asserting an ordinary interface to an owner type is a compile error, since the interface only
  lends the value and the result would be a second owner (section 6.9); assert it to an interface type
  instead and use the value's methods.

The predeclared `error` is an ordinary interface. How an error that allocates memory is freed exactly once
is the subject of the proposal in section 14.6.

Type descriptors exist only for types that are converted to an interface, and for the types those are
made of (their elements, fields and pointees). Besides identifying the type, a descriptor records its
size, alignment and kind, and its element type and length or its field offsets and types, which is what
generic printing, hashing and equality in the standard library use (abi.md section 5). Field names are
included unless the build strips them: `--fieldnames=false` leaves them out, and kernel mode leaves them
out unless `--fieldnames=true` is given. There is no reflection package; `pluto/rt.Type` reads
descriptors for the standard library.

Inside generic code, `switch any(x).(type)` on a value of a type parameter is decided at compile time for
each instantiation, so it costs nothing at run time.

### 4.5 Closures do not outlive their frame

Function literals may capture variables of the enclosing function, by reference as in Go. Captured
variables stay in the enclosing frame; nothing is moved to the heap. A closure that captures variables is
therefore valid only while the enclosing function is running:

- Calling it, passing it as an argument, assigning it to a local variable, and deferring it are allowed.
- Returning it, storing it in a package-level variable, or storing it through a pointer into memory that
  may outlive the frame is a compile error (section 6.3).

A function literal that captures nothing is an ordinary function value and may be used anywhere.

**Closure constructors.** A function whose body is a single `return` of a function literal that captures
the function's parameters (its receiver, parameters or named results), or of a call of another such
function, is a closure constructor. Go's iterator constructors are the common case:

```go
func (v *Vec[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i := 0; i < len(v.data); i++ {
			if !yield(i, v.data[i]) {
				return
			}
		}
	}
}
```

Returning the literal would make it outlive the constructor's frame, so each call of a closure
constructor is compiled where it is made instead: the arguments are stored in fresh variables of the
caller's frame, and the literal's closure is built in that frame and captures them there. The result is
an ordinary closure of the caller, valid while the caller runs, and `for i, x := range v.All()` costs no
allocation. Each call site has its own variables: a call in a loop reuses them on every iteration, as a
function literal in a loop does. A closure constructor can only be called directly; using it as a func
value or a method value, or calling it through an interface, is a compile error (E2543), since there is no
caller's frame to build the closure in.

A capturing closure is a view of the enclosing frame (section 6.3). Captured owners stay where they are
declared:

- The literal may use a captured owner, call its methods, take its address, and assign a new value to it
  (which drops the old one, section 6.4).
- Moving a captured owner inside the literal (passing it by value, returning it, sending it) is a compile
  error: the literal may run several times, or not at all, so the enclosing function could not tell
  whether the variable still holds a value. Pass the owner to the literal as an argument instead, or move
  it before or after the literal runs (section 6.9).
- The enclosing function still owns and drops the captured variable at the end of its scope. A deferred
  literal runs before the function's own variables are dropped, so it may use them; it may not use an
  owner declared in an inner block, which is dropped when that block ends (section 6.4).

```go
func process(items []Item) strings.Buf {
	var log strings.Buf
	each(items, func(it *Item) {
		fmt.Fprintf(&log, "%s\n", it.name) // uses the captured owner: allowed
	})
	return log                            // moves it to the caller after the literal is done
}
```

### 4.6 Integer behaviour

As in Go, signed and unsigned arithmetic wraps on overflow, shifts by at least the width of the operand
yield 0 (or -1 for a negative signed value shifted right), and integer division by zero panics.
`int`, `uint` and `uintptr` are 64 bits wide on all current targets.

### 4.7 `go` starts a puroutine

`go f(x, y)` starts a puroutine, a lightweight thread of execution scheduled by the program's
scheduler. As in Go, the function value and arguments are evaluated and copied when the `go` statement
runs. An argument of owner type is moved into the new puroutine, which drops it when `f` returns unless
`f` moves it on; a `synced` reference is copied (count +1). Arguments must be sendable (below), and a view
of the spawning frame cannot be passed at all (section 6.3), since the puroutine may outlive the frame.

- Each puroutine has a fixed-size stack: stacks cannot grow or move, because nothing knows where the
  pointers into them are. A guard page below the stack turns an overflow into a fault, reported as a
  stack overflow with the puroutine's trace. The default size is 64 KiB; `rt.Go(f, size)` starts a
  puroutine with an explicit stack size instead (at least `rt.MinStackSize`, 8 KiB). For a spawned
  function whose call graph has no recursion and no indirect calls, the compiler computes the stack it
  needs and uses that instead, without a guard page; otherwise it uses the default, and `-stackreport`
  lists the functions it could not size (planned, roadmap item 18; today every puroutine gets the
  default or `rt.Go`'s size).
- A function literal that captures variables cannot be started with `go`: its captures live in the
  spawning frame, which may be gone before the puroutine runs (section 4.5). Pass the values as
  arguments: `go func(n int) { work(n) }(n)`. The same holds for a method value `x.M`, whose closure
  record holds a copy of the receiver in the frame that made it. Wherever the compiler can tell that a
  `go` statement's function, or one of its func-typed arguments, is such a closure, it is a compile
  error: a capturing literal or a method value written in the `go` statement, or a local variable whose
  only assignment is its declaration from one of them (directly or through other such variables). Any
  other func value whose closure record lies in the starting puroutine's stack (one that reached the
  `go` statement through a parameter, a field or a variable assigned more than once) panics when the
  `go` statement runs, and so does `rt.Go` with one.
- Puroutines run in parallel and are preempted: one that runs for a full time slice (10 ms in hosted
  builds) without blocking is suspended at whatever instruction it has reached and another runs in its
  place, even in a loop that makes no calls. Since there is no garbage collector, every instruction is a
  safe place to stop; the full register state is saved. Code that must not be interrupted (scheduler and
  allocator locks, `rt.DisablePreemption`) disables preemption for its own duration; a preemption that
  arrives meanwhile happens when it is enabled again. Blocking on a channel, a lock or I/O, or calling
  `rt.Yield()`, gives up the processor early.

The scheduler is split in two (abi.md section 12). The runtime, `pluto/rt`, provides the mechanism: the
record of each puroutine (`rt.Task`), the context switch (`rt.Switch`), parking with a permit so that a
wakeup that arrives first is never lost (`rt.Park`, `rt.Ready`), the preemption entry (`rt.Preempt`),
and channels, `select` and `pluto/sync` built only on those. A **backend**, a type implementing
`rt.Scheduler`, provides the policy and the memory: it allocates task records and stacks (`NewTask`,
`FreeTask`), keeps run queues and decides what runs where (`Spawn`, `Park`, `Wake`, `Yield`), and drives
preemption from a timer.

The backend is bound when the program is linked: a package-level variable marked `//pu:scheduler`
whose type (not an interface) implements `rt.Scheduler`. The runtime calls its methods directly.
Hosted builds bind `pluto/rt`'s own backend unless the program binds one; a freestanding program, such
as a kernel, that starts puroutines, uses channels or waits in `pluto/sync` without binding one fails to
build, as does a program that binds two. A program that never uses `go` or channels contains none of
this.

```go
//pu:scheduler
var scheduler kernelScheduler // implements rt.Scheduler
```

The hosted backend runs puroutines on one worker thread per CPU (`$PLUTO_MAXPROCS` sets the number),
made with `clone(2)` without a C library. Each worker has its own run queue, and an idle worker steals
half the queue of a busy one, so load spreads across processors without any work by the program. A
monitor thread preempts a puroutine that has run for 10 ms by sending its thread a signal, and hands the
processor of a worker blocked in a system call (`rt.EnterSyscall`, which `pluto/os` file I/O uses) to
another worker. Waits for I/O go through one epoll poller: `os.Pipe`'s descriptors are non-blocking,
and a puroutine that reads an empty pipe or writes a full one parks there (`rt.PollWait`). When every
puroutine waits for another and nothing waits for I/O or a system call, the program stops with "fatal
error: all puroutines are asleep - deadlock!" and status 2, as in Go.

A kernel implements the backend over its own per-CPU run queues and calls `rt.Preempt()` from its timer
interrupt handler; the runtime keeps the current task in a per-CPU slot and masks interrupts while it
holds its own locks, so interrupt handlers may make non-blocking channel operations and call
`rt.Ready`. `testdata/kernel/sched` is such a kernel, booted under QEMU by the compiler's tests.

**The sendable rule.** Values that cross between puroutines, as `go` arguments or channel elements, must
be safe to use from several processors at once. A type is **sendable** unless it contains, directly or in
any field or element:

- a `shared[T]` or a `weak[shared[T]]`, whose counts are not atomic (section 6.5);
- a view of the sending function's frame, which section 6.3 already forbids passing on.

Values, owners, `synced[T]` and `weak[synced[T]]` (with a sendable `T`) and channels are sendable. Moving
an owner to another puroutine is safe because the sender loses it: only one puroutine can use it. A
`synced[T]` lets several puroutines reach the same `T`, so fields of `T` that change must be protected
with `pluto/sync` or `pluto/sync/atomic`; the count is atomic, the data is not. Views of heap and static
memory (`*T`, `[]T`, `string`) are sendable and, like raw pointers in C, their data is not checked for
races; their lifetime is checked at run time as for any heap view (section 6.3).

The checker applies the rule to the arguments and receiver of every `go` statement and to the element
type of every channel type, looking through struct fields, array elements and the referents of
`owned`, `synced` and the transparent wrappers (`ring`, `volatile`, `aligned`), but not through views,
interfaces or functions. Until the escape checks of section 6.3 land (roadmap item 5), the views of the
frame it rejects are the ones written out: the address of a local variable or of a field or element of
one (`&x`, `&t.f`, `&a[i]`), a slice of a local array, the address of a composite literal, and a
pointer method called on a local variable (`go wg.Wait()`). Passing a local `sync.WaitGroup` by address,
the usual Go idiom, is therefore an error: allocate it, or make it a package-level variable. In a generic
function the rule is checked for each instantiation.

Stacks come from the default allocator (the starting puroutine's, if it installed one with `alloc.Use`),
or from `rt.Go(f, size, a)` with an explicit one, and are freed when the puroutine returns. They belong
to the runtime: the debug allocator does not count a stack still in use at exit as a leak.

What a kernel's backend must implement, and what the runtime gives it, is specified in abi.md section 12;
the design is in [the scheduler contract](../plans/2026-10-09-scheduler-contract.md). The standard library
has no kernel scheduler. On PlutoniumOS a puroutine is a kernel task and there is one scheduler in the
system, the kernel's. That task model is proposed in PlutoniumOS: docs/architecture/lightweight-tasks.md
(github.com/matjam/plutoniumos, private).

### 4.8 Channels

`chan T`, `<-chan T`, `chan<- T`, the send and receive operators, `select`, `close`, `len`, `cap` and
`for v := range ch` work as in Go, on top of the scheduler (section 4.7). Sending on a closed channel and
closing a closed or nil channel panic; receiving from a closed channel yields the zero value and `false`.

`make(chan T)` and `make(chan T, n)` allocate the channel and its buffer from the current allocator, or
from an allocator given as the final argument: `make(chan Msg, 16, arena)`. A channel value is one word,
the address of the channel. Operations on a nil channel block forever, `select {}` blocks forever, and a
`select` with several ready cases picks one at random, as in Go.

A channel value is a reference counted like `synced[T]` (section 6.5), since a channel always has
several users: copying it counts a reference, and when the last reference goes away the channel and its
buffer are freed. Values still buffered at that point are dropped first, so owners in the buffer are
freed rather than leaked; their `Drop` runs on the puroutine that released the last reference.

**Today** the compiler does not count channel references yet (roadmap item 48, with `synced[T]`). Until
it does, a channel lives until the program frees it with `alloc.FreeChan(ch)`, which returns the channel
and its buffer to the allocator they came from and discards buffered values; no puroutine may use the
channel afterwards, and freeing one that puroutines wait on panics. A channel made in an arena needs no
`FreeChan`. Programs in the common subset with Go leak their channels until they exit, as they do the
memory of `new`. When counting lands, `FreeChan` goes away.

The operations that may wait (a send or receive, a `range` over a channel, a `select` without a
`default` case) call runtime functions marked `//pu:blocking`; a `select` with a `default` case and
`close`, `len` and `cap` never wait, so interrupt handlers may use them (section 12.6).

The element type must be sendable (section 4.7). Sending an owner moves it: after `ch <- b`, `b` holds
nothing, and the receiver of `v := <-ch` owns it. In a `select`, the value of a send case is evaluated on
entry as in Go, but an owner is moved only if that case proceeds: inside the other clauses the variable
still holds its value, and after the `select` it counts as possibly moved (section 6.4).

```go
func produce(out chan<- strings.Buf) {
	for i := 0; i < 3; i++ {
		b := fmt.Sprintf("item %d", i)
		out <- b              // moves b to whoever receives it
	}
	close(out)
}
```

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

### 4.10 Range over functions

`for k, v := range f`, where `f` is a function `func(yield func(K, V) bool)` with zero, one or two
parameters to `yield`, works as in Go 1.23. `pluto/iter` defines `Seq[V]` and `Seq2[K, V]`, and the
library's containers have iterator methods (section 13). The loop body becomes a yield function, a
closure built in the enclosing frame and passed to `f`, which is how Go compiles the loop too:

- `f` may call `yield` as often as it likes while it runs, but may not keep it: `yield` is a closure of
  the enclosing frame (section 4.5), and storing it where it outlives the call of `f` is an escape that
  section 6.3 forbids. (The escape checks do not catch it yet.)
- `break`, `continue`, `return`, `goto` and labelled `break` and `continue` out of the body work as in
  Go: the body returns false, and once `f` returns the loop goes on at the target.
- An iterator that calls `yield` when it may not panics with Go's message: after `yield` returned false
  ("range function continued iteration after function for loop body returned false"), after the loop
  ended ("... after whole loop exit"), or while the body is running ("... after loop body panic", which
  Go reports for that state). A panic in the body stops the program (section 4.1); no iterator can
  recover it.
- A `defer` in the body defers a call of the enclosing function, as in Go: it runs when that function
  returns. Plutonium gives each `defer` statement one slot in the function's frame, so a `defer` inside
  any loop is a compile error, and in the body of a range-over-func loop, which runs once per iteration,
  the `defer` must be followed on every path by leaving the loop: a `return`, a `panic`, or a `break`,
  `continue` or `goto` to a statement outside the loop (E2533). A function literal deferred there may not
  capture the body's own variables, the loop variables included, since the body's frame is gone when it
  runs (E2534); pass them as arguments.

```go
func find(items *vec.Vec[*Item], id int) int {
	for _, it := range items.All() {
		if it.id == id {
			it.mu.Lock()
			defer it.mu.Unlock() // runs when find returns
			return it.value
		}
	}
	return -1
}
```

Iterator constructors such as `items.All()` are closure constructors (section 4.5), so a loop over one
allocates nothing. The optimiser inlines the iterator and then the loop body through their closures, so a
loop over a simple iterator compiles to the loop the iterator runs: ranging over `v.All()` costs the same
as ranging over `v.Slice()`.
- Moving an owner is an ordinary write; the `go` statement or channel send that hands it to another
  puroutine is the synchronising operation. Every use of a `synced[T]` through any reference happens
  before its `Drop` runs and its memory is freed.

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

The rules in full:

- **Declaration.** A defaulted parameter is written `name T = expr` (or `T = expr` in a list of unnamed
  parameters), with a type of its own: `a, b int = 1` is an error. Once a parameter has a default, every
  later parameter must have one. Defaults are allowed only in function and method
  declarations: not on receivers or results, and not in function types, function literals or interface
  methods.
- **Scope.** A default is checked where the function is declared: it may use the names of that package,
  exported or not, the imports of that file, and the type parameters of the function and of its receiver
  type. It may not refer to the function's parameters or receiver; a parameter name inside a default is an
  error even where a package-level name of the same spelling exists. The default must be assignable to
  the parameter's type.
- **Evaluation.** Each call that omits a defaulted parameter evaluates its default after all explicit
  arguments, left to right, as if the expression were written at the call site (its temporaries live in
  the caller's frame) but with its names resolved in the callee's declaration. A call from another
  package may therefore run the callee's unexported helpers. For `defer`, the defaults are evaluated
  with the other arguments when the `defer` statement runs. The package-level variables a default uses
  are dependencies of the function for initialisation order, as those its body uses are.
- **Which calls.** Defaults apply to calls that name a declared function or method directly: `f(x)`,
  `pkg.F(x)`, `F[T](x)` and `v.M(x)`, including promoted methods and methods with type parameters.
  Function types do not record defaults, so a function value, method value or method expression has all
  of the parameters: after `g := pkg.F` or `m := v.M`, `g` and `m` are called with every argument, as in
  C++ and Kotlin. Interface methods cannot declare defaults, and a call through an interface passes every
  argument; a method with defaults implements an interface method with the same parameter types, since
  defaults are not part of the signature and signature identity ignores them.
- **Generics.** A default may use type parameters, as `Default()` in `New[T]` could use `T`; it is
  evaluated with the type arguments of the instance called. Omitted parameters take no part in type
  inference, so a type parameter that appears only in omitted parameters must be given explicitly.
- **Multiple values.** In `f(g())`, where `g` returns k values and `f` has more parameters, the k values
  fill the first k parameters and the rest take their defaults, provided they all have one.
- **Variadic functions** cannot have defaults: a defaulted parameter after `...T` could never be given
  explicitly, since an argument after the variadic ones cannot be told apart from them, and a defaulted
  parameter before it would have to be given whenever any variadic argument is. The variadic
  constructors of the standard library (`fmt.Errorf`, `errors.Join`) therefore allocate from
  `alloc.Default()`, which `alloc.Use` replaces for a region of code.

```go
func Grow(n int, factor int = 2, limit int = 1 << 20) int

Grow(10)        // factor = 2, limit = 1 << 20
Grow(10, 3)     // limit = 1 << 20
g := Grow       // g has type func(int, int, int) int
g(10, 2, 64)    // a function value takes every argument

func (v *Vec[T]) Map[U any](f func(T) U, a alloc.Allocator = alloc.Default()) Vec[U]
func Errorf(format string, args ...any, a alloc.Allocator = alloc.Default()) error // error: variadic
```

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

### 5.6 `owned`, `shared`, `synced` and `weak`

Raw pointers `*T` are views (section 6.3): they never free anything and may point at the stack, statics,
memory-mapped devices or the heap. Four built-in types hold heap objects whose lifetime the compiler
manages. Section 6 gives the rules they follow; this section describes the types.

| Type | Kind (section 6) | Meaning |
|---|---|---|
| `owned[T]` | owner | The single owner of a heap `T`, the box. Moves; when dropped, `T` is dropped and the memory freed. |
| `shared[T]` | counted | A reference-counted heap `T` used by one puroutine. The count is not atomic. |
| `synced[T]` | counted | Like `shared[T]`, but the count is atomic, so references may cross puroutines. |
| `weak[P]` | counted (weak) | A back reference to the object of `P`, a `shared[T]` or `synced[T]`, that does not keep it alive. |

`owned[T]` is one owner among many: `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Buf` and `os.File` are owners
too, declared in the library with `owner` (section 6.4). It is the one to use for a single object on the
heap, such as a tree node or a large struct that must keep its address.

**Representation.** Each is one word, the address of the object (abi.md section 1). The allocation begins
with a hidden header at a negative offset: for `owned[T]`, the allocator the object came from; for
`shared[T]` and `synced[T]`, the allocator, the strong count and the weak count. Freeing goes to the
recorded allocator even if the default has changed since; freeing into an arena does nothing (section
6.6). The zero value of each is nil; dropping a nil reference does nothing, and dereferencing one faults
like a nil pointer.

**Creation.** `alloc.Owned[T]()`, `alloc.Shared[T]()` and `alloc.Synced[T]()` allocate a zeroed `T`, each
with an optional allocator argument (section 7.1); under the proposal of section 4.2, `new(T)` and
`new(v)` also return an `owned[T]`. A `weak[P]` is made by converting a reference:
`weak[shared[Node]](p)`.

**Use.** `owned[T]`, `shared[T]` and `synced[T]` dereference like `*T`: `p.field`, `*p` and method calls
with `*T`'s methods. Each of these gives a view of the object, which may not outlive the reference it was
made through (section 6.3). `&*p` and the conversion `(*T)(p)` produce a `*T` view to pass to code that
does not take ownership. A `weak[P]` does not dereference; its one method is

```go
func (w weak[P]) Upgrade() (P, bool)
```

which returns a new strong reference and `true` while the object is alive, and the zero `P` and `false`
once the last strong reference has been dropped (always `false` for the zero `weak`).

**Own methods.** Besides `*T`'s methods, `owned[T]` has a `Drop` (drop the `T`, then free the memory),
used when it sits in an owning interface, and a `Clone()` when `T` is not an owner or has a `Clone`
method of its own. `shared[T]` and `synced[T]` have no methods of their own, so they never hide one of
`T`'s; their counts are changed only by copying and dropping.

```go
type Tree owner struct {
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

`Tree` is declared `owner` because it has owner fields (section 6.4). The moves, drops, counts and weak
references of these types are specified in sections 6.4 and 6.5.

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

### 5.9 Enums

An enum type is a closed set of named integer constants of its own type. `enum` is a keyword.

```
EnumType   = "enum" [ Type ] "{" { EnumMember ";" } "}" .
EnumMember = identifier [ "=" Expression ] .
```

```go
type Color enum {
	Red
	Green
	Blue
}

type Errno enum uint16 {
	OK     = 0
	EPERM               // 1
	ENOENT              // 2
	EIO    = 5
}
```

An enum type may only be the type of a type definition: not an alias, not generic, and not written
anywhere else. The design rationale is in
[2026-10-09-enums-design.md](../plans/2026-10-09-enums-design.md).

**Type and values.** An enum is a defined type whose underlying type is an integer type: the type after
`enum` (which must be an integer type), or else the smallest of `uint8` to `uint64` that holds every
member, or of `int8` to `int64` if a member is negative. The first member is 0 unless given a value; a
member without a value is one more than the previous member. A value is a constant integer expression,
evaluated in the scope enclosing the declaration; it must be representable in the underlying type and may
not refer to the enum being declared. Two members may not share a name or a value, `_` is not a member
name, and **one member must have the value 0**: it is the zero value of the type.

**Names.** A member is selected through its type, `Color.Red` (also `pkg.Color.Red`, or through an alias);
it is a typed constant of type `Color`. Members enter no scope. Unexported members are not visible in
other packages. On a type name a member takes precedence over a method of the same name.

**Switches.** In an expression switch whose tag has enum type `E`, a case expression that is a single
identifier naming a member of `E` denotes that member, whatever else the name denotes in scope:
`case Red, Green:`. Bare member names are not accepted anywhere else. A switch on an enum value that has no
`default` case must list every member among its constant case values; otherwise it is a compile error
naming the missing members.

**Operations.** Enum values compare with `==`, `!=`, `<`, `<=`, `>`, `>=`. Arithmetic, bitwise operators,
`++`, `--` and shifting an enum value are errors; an enum value may be a shift count or an index. An
untyped constant does not convert implicitly to an enum: `var c Color = 1` and `c == 1` are errors.

**Conversions.**

- `E(c)` for an integer constant `c` is valid if `c` is a member's value, and gives that member.
- For a non-constant integer `n`, `E(n)` is valid only in the comma-ok form `v, ok := E(n)` (or `=`, or
  `var v, ok = E(n)`): `ok` reports whether `n` equals a member's value, and `v` is that member, or the
  zero value if `ok` is false. Floats and other non-integer types do not convert to an enum.
- An enum converts to any integer or floating-point type as its underlying integer does.

**Generated operations.**

| Form | Meaning |
|---|---|
| `x.String()` | the member's name, or `"E(?)"` for a value that is not a member; generated unless `E` declares `String` |
| `len(E)` | the number of members, an untyped integer constant |
| `for v := range E` | each member in declaration order; one iteration variable |

**Other types.** An enum value is an integer everywhere else: it is stored like its underlying type,
held directly in an interface's data word, satisfies constraints by its underlying type (`~uint8`), and
may be `volatile` or `aligned`. A packed field of enum type (section 5.5) is a bit field as wide as the
members need (unsigned, or two's complement if a member is negative); `f bits[E, N]` gives a wider
field. Values that are not members can arise only through `unsafe`, packed values converted from raw
bits, and generic code operating on the underlying type; such a value matches no case of a switch.
`type F E` declares an ordinary integer type with `E`'s underlying type and no members. An enum is a value
(section 6.2): it cannot be declared `owner`; a handle that owns a resource is a struct declared `owner`
with an enum field.

Enums do not provide bit-flag sets; packed `bits` groups do that.

### 5.10 Unions

A union type (a tagged union, or sum type) holds exactly one of a closed set of named variants, together
with that variant's payload, stored inline: no allocation and no boxing, so a union can be returned,
stored in package variables and put in arrays like a struct. It is the sibling of the enum, whose members
have no payloads. The design and the alternatives considered are in the
[sum types proposal](../plans/2026-10-09-sum-types-proposal.md) (option A).

```
UnionType       = "union" "{" { VariantDecl ";" } "}" .
VariantDecl     = identifier { "," identifier } Type | identifier .
PackedUnionType = "packed" "union" [ "le" | "be" ] Type "{" TagField ";" { PackedVariant ";" } "}" .
TagField        = identifier PackedFieldType .
PackedVariant   = identifier "case" ExpressionList "{" { PackedFieldDecl ";" } "}" .
```

```go
type DeviceState union {
	Off                                  // no payload
	Probing struct{ attempt int }
	Ready   struct {
		irq  uint8
		base uintptr
	}
	Failed syscall.Errno
}

var st DeviceState                       // zero value: Off, the first variant
st = DeviceState.Probing{attempt: 1}
st = DeviceState.Failed(syscall.EIO)

switch s := st.(type) {
case Off:
	start()
case Probing:
	retry(s.attempt)                     // s is the payload, a struct{ attempt int }
case Ready:
	enableIRQ(s.irq)
case Failed:
	log("device failed:", s)             // s is a syscall.Errno
}                                        // a missing case is a compile error
```

**Declaration.** A union type may only be the type of a type definition, as an enum, since its variants
are reached through the type's name; it may have type parameters (a packed union may not). `union` is not
a keyword: it begins a union type only as the type of a type declaration followed by `{`, or after
`packed`, so Go code that uses `union` as an identifier still compiles. Variants are written like struct
fields: a name and a payload type, or a name alone for a variant without a payload; `A, B T` declares two
variants with the same payload type. A union has at least one variant; variant names are distinct, not
`_`, and carry no tags. A payload may be any type, including an enum, an interface, a type parameter or
another union; a union cannot contain itself except through a pointer. Unexported variants are not
visible in other packages.

**Construction.** For a variant `V` of union type `U`:

- without a payload, `U.V` is a value of type `U`;
- with a payload of type `P`, `U.V(x)` makes a `U` holding `x`, which must be assignable to `P` (as a
  function argument); `U.V{...}` is a composite literal of `P` making a `U`; and `U.V` alone is a function
  value of type `func(P) U`.

`U` may be qualified by a package or instantiated: `pkg.U.V`, `Result[int].Ok(5)`. On a type name a
variant takes precedence over a method of the same name. A variant is not a type: `var x U.V` is an error,
and a payload, once selected, has its own payload type. `U{...}` is an error, as is `U.V{}` for a variant
without a payload. There are no implicit conversions into a union: `var u U = x` and `u = nil` are errors,
and a value of another union type with identical variants converts only explicitly.

**Zero value.** The first variant, with its payload's zero value. Put the idle or empty state first.

**Type switches.** `switch v := u.(type)` on a union value selects the variant `u` holds. Each case names
variants: `U.V` (or `pkg.U.V`, `U[T].V`), or the bare name `V`, which in a case of such a switch denotes
the variant whatever else the name denotes in scope, as an enum member does in an expression switch.
Bare names are accepted nowhere else. In a clause naming one variant, `v` has that variant's payload type
(`struct{}` for a variant without a payload); in a clause naming several, and in the default clause, it
has type `U`. A union value is never nil, so there is no `nil` case; naming a variant twice is an error,
and `fallthrough` is not allowed. A switch without a `default` case must name every variant, or it is a
compile error that lists the missing ones:

```
dev.p:14:2: missing case in type switch on st (DeviceState): Failed (add it or a default case)
    A type switch on a union value that has no default case must name every
    variant. That way, adding a variant to the union shows every switch that
    needs to handle it.
    Add a case for each missing variant, or a default case if the remaining
    variants share one action. A switch that names every variant needs no
    default to end a function.
    Go cannot check this for interfaces, whose set of types is open. A union's
    variants are a closed set, so the compiler can tell when one is forgotten.
    (see "Unions" in the Plutonium spec, section 5.10)
    (run "pluto explain E2414" for a longer explanation)
```

The union errors have codes E2401 to E2415, explained by `pluto explain`.

Such an exhaustive switch is a terminating statement when every clause is, so a function may end with it.
A tag that names no variant can only come from `unsafe` code; the switch then panics.

**Assertions.** `u.(U.V)` yields the payload of `V`, and panics (section 8) if `u` holds another variant;
`p, ok := u.(U.V)` reports instead, with `p` the zero payload when `ok` is false. A bare variant name is
not accepted here.

**Operations.** A union is comparable if every payload type is; two values are equal when they hold the
same variant with equal payloads. A union type may have methods and implement interfaces. Converting a
union to an interface copies it into the frame like a struct (section 4.4); a type switch on the
interface sees the union type, and a second switch on the union selects the variant. `fmt` prints a union
that has no `String` method as its variant and payload: `Some(5)`, `Err(no such file or directory)`,
`None`.

**Generics.** Every instantiation of a generic union has the same variants, so a switch on a `Result[T]`
in generic code is checked once for every `T`, and payload types may coincide (`Result[syscall.Errno]`).
Variants are selected through the instance, `Result[int].Ok(5)`, or `Result[T].Ok(v)` in generic code. A
union may be a type argument; unions cannot appear in constraints, where `A | B` keeps Go's meaning.

**Layout.** A union is its tag, a `uint8` (a `uint16` for more than 256 variants) numbering the variants
from 0 in declaration order, followed by the payload of the variant it holds at the first offset that
satisfies the largest payload alignment; its size is rounded up to its alignment. `DeviceState` above is
24 bytes. A variant without a payload takes no space beyond the tag. The layout is not part of the C ABI:
unions are not allowed in `//pu:extern` or `//pu:export` signatures (abi.md sections 1 and 3).

**Packed unions.** A packed union gives a bit-exact layout for hardware words and records whose shape
depends on a tag bit:

```go
type PTE packed union uint64 {
	present bits[bool, 1]                // the tag: shared by every variant

	Swapped case false {
		slot bits[uint64, 63]
	}
	Mapped case true {
		writable bits[bool, 1]
		user     bits[bool, 1]
		_        bits[uint, 9]
		frame    bits[uint64, 40]
		_        bits[uint, 11]
		nx       bits[bool, 1]
	}
}

pte := PTE(table[i])                     // converting from the raw word never fails
switch e := pte.(type) {
case PTE.Mapped:
	return uintptr(e.frame) << 12
case PTE.Swapped:
	return swapIn(e.slot)
}
```

- The one field before the first variant is the tag: a `bool`, integer or enum field (section 5.5) that
  occupies the same bits in every variant. A packed union has a backing type, which fixes its size.
- Each variant lists the constant tag values that select it (`case false`, `case 1, 2`), each of the tag
  field's type and fitting its width; no value selects two variants. Its fields follow the tag, laid
  out by the packed rules of section 5.5.
- A variant's payload is a packed type with the same storage holding the tag and the variant's fields:
  in the switch above, `e.present` and `e.frame` are both fields of `e`. `PTE.Mapped(e)` converts a payload
  back, keeping its tag if it is one of the variant's values and otherwise setting the first;
  `PTE.Mapped{frame: f}` sets the tag to the variant's first value and may not set it itself.
- A packed union converts to and from its backing type or a byte array of its size, like any packed type,
  and is comparable as its bits. Its tag is readable as a field (`pte.present`); a variant's fields are not.
- A type switch without a `default` case must cover every value the tag field can hold, since hardware
  may hold any bit pattern; `PTE` above needs none, since its one-bit tag has two values.

**Ownership.** A union whose payloads are all values is a value (section 6.2). A union with a variant
whose payload is an owner must be declared `owner` like a struct (section 6.4): dropping it drops the
payload of the variant it holds, giving it a new variant drops the old payload first, and moving a
payload out through a type switch binding moves the whole union. The ownership checker does not exist
yet, and until it does, a payload that holds an `owned`, `shared` or `synced` pointer (directly, or in a
field, an array element or another union, but not behind a pointer) is a compile error, in a
declaration or through a type argument.

**Not yet.** Updating a variant's payload in place through a pointer (`(&u).(*U.V)`, proposal section 3)
waits for real use; assign a new variant instead. Debug information describes a union as its size only.

## 6. Ownership

Plutonium has no garbage collector, so every piece of memory a program uses has an owner that frees it,
and the owner is visible in the program's types. Every type is one of three kinds:

- **Values** are copied on assignment, exactly as in Go: integers, floats, booleans, enums, packed types,
  arrays `[N]T` of values, and structs made only of values.
- **Views** look at memory that something else owns: `string`, `[]T` and `*T`, built into the language. A
  view never owns or frees anything. Creating, copying and passing views is free, and a view may not
  outlive what it looks at.
- **Owners** hold memory or another resource and release it when they are dropped. They are types
  declared with `owner`, such as `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Buf`, `os.File` and the
  built-in box `owned[T]`. Assigning an owner **moves** it to the new place; copies are made only by
  calling `Clone`.

When several parts of a program must keep the same object alive, the program says so with a counted
reference, `shared[T]` or `synced[T]`, and breaks cycles with `weak[...]`.

| You want to | Use | Cost |
|---|---|---|
| let a function use it for a while | view | free |
| give it to exactly one new owner | move | free |
| let several places keep it | shared/synced | count update |

Editors (hover) and `pluto doc` show each type's kind next to its name: `value`, `view`, `owner (moves)`
or `shared (counted)`.

### 6.1 Why three kinds

Go's built-in map, slice and string look like values but are references that own memory through the
garbage collector. Go programmers learn this through surprises:

```go
func load() *map[string]string {
	x := map[string]string{}
	x["timeout"] = "30s"
	return &x
}
```

`x` is a small header that refers to a hash table. `&x` points at the header, which Go moves to the heap
without saying so. Copying `x` copies the reference, not the table. Nobody frees the table; the collector
does, eventually. Slices are the same: `t := s` shares the array, and `append(s, v)` sometimes writes into
that shared array and sometimes copies it. Most Go programmers cannot say, for code like the above, what
is copied, what is shared and where it lives.

Plutonium separates the three jobs so that each type does one:

- a view only looks: `[]T`, `string`, `*T`;
- an owner only owns: `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Buf`;
- a value is only bits.

```go
func load() hash.Map[string, string] {
	x := hash.NewMap[string, string](8)
	x.Set("timeout", "30s")
	return x // moves the map to the caller: the table is not copied, and is freed when the caller drops it
}
```

`return &x` would be a compile error that explains this (section 6.9): `x` lives in `load`'s frame, and
`&x` is a view of it there.

### 6.2 Values

Integers, floats, booleans, enums, packed types, `volatile[T]` and `aligned[T, N]` of values, function
values that capture nothing, and arrays, structs and unions whose elements, fields and payloads are all
values. They copy on
assignment, on passing and on return, and nothing happens when they go out of scope. This is Go's
behaviour.

A struct or array that contains a counted reference (`shared[T]`, `synced[T]`, `weak[P]`) or a channel,
and no owner, also copies; copying it updates each count, and dropping it releases each reference
(section 6.5). It needs no declaration, since assignment still copies.

### 6.3 Views

`string`, `[]T` and `*T` are views. So are `unsafe.Pointer`, and the frame-bound values of sections 4.4
and 4.5: a capturing closure, a method value, and an interface holding a frame copy or a lent owner. A
struct or array that contains a view, and no owner, is a view too: it copies freely and must not outlive
what any of its views look at.

1. **Views are free.** `&x`, `s[i:j]`, `b.String()`, `v.Slice()` and copying or passing any of them cost
   nothing beyond the words copied. A view never frees anything; dropping one does nothing.
2. **`&x` is a view of `x` where `x` lives.** Where a variable lives is decided by its declaration, never
   by how it is used later: a local variable or parameter lives in its function's frame, a package
   variable in static storage, a field in its struct, an element in its array. Plutonium never moves a
   variable to the heap because its address is taken, and `return &x` does not copy `x` to the caller. It
   is a compile error (section 6.9) whose message shows the two fixes: return `x` itself, which moves or
   copies it to the caller, or allocate it with `new`.
3. **Passing a view down is always allowed.** A callee's frame ends before its caller's, so `f(&x)`,
   `f(buf[:n])` and `f(b.String())` are valid whatever `x`, `buf` and `b` are. The callee may return a view
   derived from its parameters (`return s[4:]`), since the caller's memory outlives the call.
4. **A view may not outlive what it looks at.** How this is checked depends on where the memory is:
   - **Frame memory** is checked at compile time. A view of a frame (of a local, of a temporary, of a
     composite literal `&T{...}` or slice literal, or of a variadic call's argument array) may be used,
     copied into locals, stored in the same frame and passed to calls. It is a compile error to return it
     (directly or inside a struct, array, interface or closure), to store it in a package-level variable,
     in memory that may be on the heap, or through a pointer the function received from its caller, to
     send it on a channel or pass it to a `go` statement, or to pass it to a function that does one of
     these with that parameter. The compiler summarises what every function does with its parameters, so
     the last case needs no annotation; calls through interfaces and function values are checked against
     every possible callee.
   - **A local owner's memory** is treated like the frame. A view obtained from an owner held in a local
     variable (`b.String()`, `v.Slice()`, `&m`) follows the frame rules above, and in addition may not be
     used after that variable has been moved, dropped or given a new value in the same function. A view of
     a temporary owner (`fmt.Sprintf(...).String()`) may not outlive the statement (section 6.4).
   - **Heap memory** reached any other way is checked at run time by generation checks: a view whose
     allocation has since been freed, or reallocated by a growing owner, faults when it is used instead of
     reading freed memory. The design is in progress (roadmap item 20); until it lands such a view is
     unchecked, as in C, and the debug allocator (section 7.3) catches its use.
   - **Static memory, and memory nobody in the program owns** (string literals, package variables, device
     registers, memory made from a `uintptr`) is never freed, so views of it have no limit.

Package-level initialisers are the exception of section 4.4: their literals and interface boxes are
static. A function implemented outside Plutonium (assembly, `//pu:extern`) is assumed to keep every
pointer argument.

### 6.4 Owners

**Declaring an owner.** A type is an owner only if its declaration says so. `owner` precedes the type in a
type definition:

```go
type File owner struct {
	fd   int32
	open bool
}

func (f *File) Drop() {
	if f.open {
		syscall.Close(f.fd)
	}
}

type Frame owner uintptr            // a physical page frame, returned to the page allocator on Drop
type Pages owner [4]Frame
type Vec[T owner any] owner struct { ... }
type Resource owner interface { Read(p []byte) (int, error) }
```

The rules for where `owner` may appear:

- **Kinds.** `owner` may qualify a struct type, an array type, an integer type (including `uintptr`), a
  pointer type or `unsafe.Pointer`, an interface type (an owning interface, section 4.4) or a named type of
  one of these. It may not qualify a string, slice, map, channel or function type, a boolean, float, enum,
  packed or `volatile` type, or `owned`/`shared`/`synced`/`weak`, which have kinds of their own. A
  resource behind one of those is wrapped in a struct declared `owner`.
- **Only in a type definition**, `type T owner ...`, including generic ones. An alias `type A = T` has
  `T`'s kind and takes no `owner` of its own; a type literal elsewhere cannot be marked.
- **Never inferred.** A struct with a field of owner type, including an embedded field or an array of
  owners, must itself be declared `owner`, and so must an interface that embeds an owning interface. A type
  definition `type B A` where `A` is an owner must be written `type B owner A`. An unnamed struct type may
  not have owner fields. An unnamed array type `[N]T` and a ring `ring[T, N]` whose element is an owner
  are owners without a declaration, because their element type already says so; so is `aligned[T, N]` of
  an owner. A union with an owner variant must be declared `owner` like a struct (section 5.10).
- **`Drop` needs `owner`.** A method named `Drop` must have no parameters and no results, and may be
  declared only on a type declared `owner` (on `T` or `*T`). An owner without a `Drop` method is valid: its
  values still move, and dropping one drops its fields.
- **Generics.** A type parameter admits owner types only if its constraint is marked `owner`:
  `[T owner any]`, `[T owner io.Reader]`. Inside such code, values of `T` move: assignment, passing and
  returning move them, using one after a move is an error, and a variable of type `T` still holding a
  value at the end of its scope is dropped (which does nothing for an instantiation with a value type). A
  type parameter without `owner` cannot be instantiated with an owner type, so ordinary generic code such
  as `Max[T cmp.Ordered]` or `Fill[T any]` keeps copying freely. A generic type that stores values of an
  `owner` type parameter must be declared `owner`; all of its instantiations then move, including
  `vec.Vec[int]`.
- **Embedding.** The methods of an embedded owner are promoted as in Go, except `Drop`, which is never
  promoted: the outer type's own `Drop` (if any) runs, and then the embedded field is dropped like any
  other field.

The library's `vec.Vec[T]`, `hash.Map[K, V]`, `hash.Set[K]`, `strings.Buf`, `os.File` and arenas that own
their chunks are declared `owner`.

**Moves.** An owner is moved, not copied, when it is

- assigned to a variable (`n := m`, `n = m`);
- passed by value as an argument, including as the receiver of a method with a value receiver;
- returned;
- sent on a channel or passed to a `go` statement (sections 4.7, 4.8);
- stored in a field, an element or a composite literal (`c.buf = b`, `s[i] = b`, `Conn{buf: b}`);
- converted to an owning interface (section 4.4);
- dropped explicitly with `x.Drop()`.

A move copies the owner's bytes and nothing else; the source is not touched at run time. The compiler
tracks that the source no longer holds a value, and using it afterwards (reading it, calling a method,
taking its address) is a compile error. Assigning a new value to a moved-from variable makes it usable
again. The error message names the move and its line, and suggests a fix based on how the variable is
used afterwards: `Clone` when both copies are needed, a pointer when the later use only looks, reordering
when the later use could come first (section 6.9).

**Places.** An owner can be moved only out of a whole variable (a local or a parameter). Moving it out of
anything else is a compile error: a field (`t.left`), an element (`s[i]`), a value reached through a
pointer (`*p`, `p.buf`) or a package-level variable. Each of those places would still believe it holds the
value and would drop it again. To move out of such a place:

- `take(&place)` returns the value and leaves the zero value behind (**`take` is a new predeclared
  function; proposal, needs sign-off**);
- an assignment that permutes places, such as `a, b = b, a` or `s[i], s[j] = s[j], s[i]`, is allowed,
  since nothing is duplicated or lost;
- `place.Clone()` copies instead.

For the same reason a `for _, x := range s` loop over a slice or array of owners is a compile error (each
iteration would copy an element); range over the indexes and use `&s[i]`.

**Assigning to a place** that holds an owner drops the old value first, then moves the new one in:
`c.buf = b` frees the buffer `c.buf` held before. Through a view this works the same way: `*p = v` and
`s[i] = v` drop the old contents.

**Methods.** A method with a pointer receiver borrows its receiver: `b.WriteString("x")` takes `&b`, a
view, and `b` still owns its buffer afterwards. A method with a value receiver on an owner type consumes
the receiver: calling it moves the owner into the method, which drops it when it returns. Owner types
should declare their methods on pointer receivers, unless the method's purpose is to consume.

**The zero value** of an owner holds nothing, and dropping it does nothing. `Drop` methods must treat the
zero value as empty (as `File.Drop` above checks `open`), because memory from every allocator is zeroed,
`take` leaves the zero value, and early returns drop variables that were never filled.

**Clone.** Copies are explicit. By convention an owner that can be copied has a method
`Clone(a alloc.Allocator = ...) T` that returns an independent copy. `vec.Vec[T]` (for `T` that is not an
owner or has `Clone`), `hash.Map[K, V]`, `strings.Buf` and `owned[T]` have one; `os.File` has none, since
a file descriptor cannot be copied that way.

**Drops.** An owner held by a variable when the variable's scope ends is dropped automatically, on every
path out of the scope: falling off the end of a block, `return`, `break`, `continue` and `goto`, and at the
end of each iteration for a variable declared in a loop body. In detail:

- Variables are dropped in reverse order of declaration, so an owner that holds a view of an earlier
  variable (a `vec.Vec` allocating from a local arena) is dropped before it.
- Parameters are dropped when the function returns, after its locals. Results are moved to the caller and
  not dropped; a bare `return` moves the named results.
- A variable moved on some paths and not others is dropped only on the paths where it still holds a value
  (the compiler keeps a flag where it cannot tell statically). Using it after such a point is an error.
- Deferred calls run before the function's own variables are dropped, so a deferred call may use them. A
  deferred call may not use an owner declared in an inner block, which is dropped when the block ends.
- Dropping an owner runs its `Drop` method if it has one, then drops its fields in declaration order and
  array elements in index order. Fields that are views or values need nothing; counted fields release
  their references.
- An owner that an expression produces and nothing stores is a **temporary**. It is dropped at the end of
  the statement that made it, so a view of it may be passed to a call in that statement:
  `os.Stdout.WriteString(fmt.Sprintf("%d\n", n).String())` is valid. In the header of an `if`, `switch` or
  `for ... range`, a temporary lives until the whole statement ends; in the condition and post statement
  of a three-clause `for` it is dropped after each evaluation.
- `panic` aborts without unwinding (section 8), so nothing is dropped; neither does `os.Exit`.
- `x.Drop()` on an owner variable ends its life early: it counts as a move, and `x` is not dropped again.
  Calling `Drop` in any other way (through a pointer, a method value or an ordinary interface) is a
  compile error, since the real owner would drop the value a second time.

`Drop` cannot report failure. A type whose release can fail (a file whose last write is flushed on close)
provides a method such as `Close() error` for callers that care; `Drop` then has nothing left to do.

### 6.5 Shared ownership

Shared ownership is chosen by type, never by default:

- `shared[T]` counts its references with ordinary arithmetic. It may be used by one puroutine only:
  passing it to a `go` statement or sending it on a channel is a compile error (section 4.7).
- `synced[T]` counts atomically. It is the only counted kind that may cross puroutines, under the sendable
  rule of section 4.7. The count is safe to share; `T`'s fields are not, so a `T` that changes holds a
  lock (`sync.Mutex`) or uses atomics.
- `weak[P]`, where `P` is `shared[T]` or `synced[T]`, refers to the same object without keeping it alive.
  `w.Upgrade()` returns `(P, true)` with a new strong reference while the object is alive, and the zero `P`
  and `false` after the last strong reference is gone. A `weak[synced[T]]` is sendable; a
  `weak[shared[T]]` is not.

The rules:

1. Copying a strong reference (assignment, passing, returning while the source is still used, storing it,
   sending a `synced` reference) increments the strong count. Copying a weak reference increments the weak
   count.
2. A reference that is dropped (end of scope, overwritten, its containing object dropped) decrements its
   count.
3. When the strong count reaches zero, `T`'s `Drop` runs, then `T`'s fields are dropped. When the weak
   count is also zero, the memory is returned to the allocator recorded in the header. Until then the
   memory stays allocated so that `Upgrade` can see the count is zero.
4. The compiler may remove an increment and the decrement that matches it when nothing in between can
   observe the count: `return p` as `p`'s last use, and passing `p` as its last use, cost nothing.
5. A view obtained through a counted reference (`p.field`, `&*p`) is bounded by that reference like a view
   of a local owner (section 6.3).
6. Cycles of strong references are never freed. A structure with back pointers makes them `weak`
   (section 6.8).

**Why not count everything.** Views and moves cover the common cases, passing data down and handing it
on, for free. Counting costs something on every copy, and Plutonium makes that cost opt-in:

- **Cost.** Every copy and every drop of a counted reference writes to the object's header and every drop
  tests for zero. A pointer passed through a hot loop or a deep call chain pays for it at each step, and
  the header adds two words to every object.
- **Atomics across cores.** A `synced` count is an atomic read-modify-write, a locked instruction that
  costs tens of cycles uncontended. When several processors copy the same reference, the cache line
  holding the count moves between them on every change, and a read-only object becomes a point of
  contention.
- **Predictability for kernels.** With moves, the place where memory is freed is written in the source.
  With counts, it is wherever the last reference happens to be released: possibly an interrupt handler or a
  latency-sensitive path, possibly freeing a whole graph at once. A kernel needs to know when and where
  memory is released, and with views and moves it does.

### 6.6 No allocation without an owner or an arena

Every allocation belongs to an owner or to an arena. The library is written so that functions that create
memory return owner types:

- **Owners record their allocator.** `vec.Vec`, `hash.Map` and `strings.Buf` keep it in a field; `owned`,
  `shared` and `synced` keep it in the allocation's header. Dropping an owner frees to that allocator,
  whichever is the default at the time.
- **Arenas own what is allocated in them.** Freeing into an arena does nothing, and the arena reclaims
  everything at once when it is reset or dropped. The same code therefore works in an arena and on the
  general heap: an owner allocated in an arena is dropped as usual, and its free costs nothing. Memory that
  only an arena owns can also be handed out as plain views (`a.New[T]()` returns a `*T`), valid until the
  arena is reset or dropped.
- **Text.** `fmt.Sprintf` returns a `strings.Buf`, an owner of the formatted bytes, lent as a `string`
  view through `.String()`. `fmt.Bprintf(buf, ...)` formats into a caller's `[]byte` and allocates
  nothing; `fmt.Fprintf(&b, ...)` appends to a `strings.Buf`. A zero `strings.Buf` is empty and allocates
  from `alloc.Default()` on its first write; `strings.NewBuf(a)` chooses the allocator.
- **Growing.** A slice is a view and cannot grow beyond its capacity (section 4.3). Growing sequences are
  `vec.Vec[T]` or another owner; there are no arrays that nobody owns.
- **`new` and `make`** return owners under the proposal in section 4.2 (`owned[T]` and `vec.Vec[T]`).
- **The unchecked layer.** `alloc.Raw`, `alloc.New` and `alloc.Free` (section 7.1) allocate and free
  memory that has no owner until the code that called them gives it one. They are what owner types are
  written with, and, like `unsafe`, the compiler does not check them.
- **Errors.** The accepted error design (section 14.3) allocates `fmt.Errorf`, `errors.Join` and
  `*os.PathError` from the default allocator and frees them with `errors.Free` or an arena reset, which is
  an exception to this rule; section 14.6 proposes how such errors get an owner.

### 6.7 Indexing library containers

Go's maps and slices have index syntax built in. Plutonium's growable containers are library owners
(`vec.Vec[T]`, `hash.Map[K, V]`), so the syntax is lent to them through methods. On an operand `x` whose
type is not a string, array, pointer to array or slice:

| Form | Calls | Notes |
|---|---|---|
| `x[k]` | `x.Index(k)` | a value of type `V` |
| `x[k] = v` | `x.SetIndex(k, v)` | also as a target of `a, x[k] = ...` and `for x[k] = range ...` |
| `v, ok := x[k]` | `x.IndexOk(k)` | also with `=` and `var v, ok = x[k]` |
| `x[k] op= v`, `x[k]++` | `x.Index(k)`, then `x.SetIndex(k, result)` | `x` and `k` are evaluated once |
| `len(x)` | `x.Len()` | |

```go
counts := hash.NewMap[string, int](0)
defer counts.Free()
counts["a"] = 1       // counts.SetIndex("a", 1)
counts["a"] += 2      // counts.SetIndex("a", counts.Index("a")+2)
n := counts["zzz"]    // 0: hash.Map's Index returns the zero value for a missing key, as Go's map does
n, ok := counts["a"]  // 3, true: counts.IndexOk("a")

v := vec.New[int]()
v.Push(1)
v[0] *= 10            // bounds checked by Vec's methods: v[5] panics "index out of range [5] with length 1"
for i := 0; i < len(v); i++ { ... }
```

**The methods.** They are found in the method set of `x`'s type as for a call `x.Index(k)`, so methods
with pointer receivers need an addressable `x` or a pointer, and indexing through a pointer works:
`p := &counts; p["b"] = 2`. Their forms are fixed: `Index(k K) V`, `SetIndex(k K, v V)`,
`IndexOk(k K) (V, bool)` and `Len() int`, with no type parameters of their own and no variadic parameter.
A type may have any subset; each use needs its own method, and the methods a type has must agree on `K`
and `V`. The key is assigned to `K` as an argument is. A field named `Index` does not count.

**Built-in types are unaffected.** A string, array, pointer to array or slice is always indexed by the
language, even when its named type declares an `Index` method; so is `len` of those types and of rings.
In generic code the choice is made from the type parameter's constraint: a type parameter whose type set
is made of indexable types is indexed by the language, and one constrained only by methods calls them,
whatever its type argument.

**The result is a value.** `x[k]` is a copy returned by `Index`, never a variable, as a Go map element is:

- `&x[k]` is a compile error. A pointer into a container's storage would be invalidated by any call that
  grows it or rehashes it, which nothing in the syntax would show; `&s[i]` on a slice has no such
  hazard, since a slice never moves its elements. Containers return such pointers from methods named
  for what they do, such as `vec.Vec.Ptr(i)` and `hash.Map.Ptr(k)` and `Upsert(k)`, whose documentation
  says how long the pointer stays valid (until the container next grows or is modified). When the
  generation checks of section 6.3 land, such a pointer becomes a checked heap view; `&x[k]` stays an
  error, so that the cost and the lifetime stay visible at the call.
- Assigning to a field or element of `x[k]` (`v[i].count = 1`) is a compile error, as for Go maps;
  calling a pointer method on it is too. Read, change and store back, or go through `Ptr`.

**Ownership.** The receiver is borrowed for the call (a view, section 6.3), so indexing never moves the
container. `x[k] = v` moves `v` into the container when `V` is an owner, and `SetIndex` drops the value
it replaces. `Index` returns a copy, which for an owner `V` would be a second owner: reading `x[k]` from a
container of owners is therefore a compile error once the move checker lands (roadmap item 20), and such
code uses `Ptr` to look at an element or a method that takes it out (`Remove`, `Pop`).

**Deleting** stays a method: `m.Delete(k)`. The predeclared `delete` belongs to Go's built-in maps,
which Plutonium does not have (section 3).

**Range.** `for k, v := range x` over a container will call an iterator method once range over functions
lands (roadmap item 22); until then it is a compile error whose message points at the alternatives:
`for i := 0; i < len(v); i++`, `range v.Slice()` and `m.Iter()`.

**Generics.** `pluto/container` declares the constraints: `Indexable[K, V]` (`Index` and `SetIndex`),
`Reader[K, V]`, `Lookup[K, V]` (`IndexOk`) and `Sized` (`Len`). A value of a type parameter so
constrained supports the corresponding syntax. The library's containers declare their methods on pointer
receivers, so their pointers satisfy the constraints:

```go
func Count[M container.Indexable[K, int], K any](m M, keys []K) {
	for _, k := range keys {
		m[k] += 1
	}
}

Count(&counts, words)
```

### 6.8 Examples

**Views passed down.**

```go
func sum(xs []int) int {           // xs is a view: sum never frees it
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func count(words *hash.Map[string, int], w string) {
	n, _ := words.Get(w)
	words.Set(w, n+1)              // changes the caller's map through the view
}

func main() {
	nums := [4]int{1, 2, 3, 4}
	fmt.Println(sum(nums[:]))      // a view of a local array, passed down: free

	words := hash.NewMap[string, int](16)
	count(&words, "go")            // lends the map; main still owns it
	count(&words, "go")
}                                  // words is dropped here and its table freed
```

**Slicing a buffer.**

```go
func splitHeader(b []byte) (magic, rest []byte) {
	return b[:4], b[4:]            // views of the caller's buffer: allowed
}

func readHeader(f *os.File) error {
	var buf [512]byte              // lives in readHeader's frame
	n, err := f.Read(buf[:])       // a view of buf, passed down
	if err != nil {
		return err
	}
	magic, rest := splitHeader(buf[:n])
	return check(magic, rest)      // fine: buf is still alive
	// return buf[:n] here would be a compile error: buf dies with this frame
}
```

**A string view of a Buf.**

```go
func greet(name string) {
	msg := fmt.Sprintf("hello, %s\n", name) // msg is a strings.Buf: it owns the bytes
	s := msg.String()                       // s is a view of msg's bytes: nothing is copied
	os.Stdout.WriteString(s)
	audit(msg.String())                     // as many views as needed
}                                           // msg is dropped: its bytes are freed
```

**Returning an owner.**

```go
func render(items []Item) strings.Buf {
	var b strings.Buf                       // empty; allocates on its first write
	for i := range items {
		fmt.Fprintf(&b, "%s: %d\n", items[i].name, items[i].count)
	}
	return b                                // moves b to the caller: nothing copied, nothing freed
}

func main() {
	out := render(items)                    // main owns the text now
	os.Stdout.WriteString(out.String())
}                                           // and it is freed here
```

**Storing an owner in a struct.**

```go
type Conn owner struct {                    // owner: it has owner fields
	file os.File
	in   vec.Vec[byte]
	name strings.Buf
}

func dial(path string) (Conn, error) {
	f, err := os.Open(path)                 // f is an os.File, an owner
	if err != nil {
		return Conn{}, err                  // f is the zero File: dropping it does nothing
	}
	var c Conn
	c.file = f                              // moves f into the field
	c.name.WriteString(path)                // c owns its own copy of the path
	return c, nil                           // moves c, and with it the file, to the caller
}
```

**The use-after-move error, and Clone.**

```go
a := vec.New[int]()
a.Push(1)
b := a                    // moves: b owns the vector, a holds nothing
b.Push(2)
a.Push(3)                 // compile error: a was moved to b (section 6.9)

c := b.Clone()            // an explicit copy: c and b own separate arrays
c.Push(4)                 // b is unchanged
```

**Automatic drop at the end of a scope and on early return.**

```go
func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err                  // in is the zero File: nothing to close
	}
	out, err := os.Create(dst)
	if err != nil {
		return err                  // in is dropped here: its file is closed
	}
	var buf [4096]byte
	for {
		n, err := in.Read(buf[:])
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr         // out, then in, are dropped here
			}
		}
		if err == io.EOF {
			return out.Close()      // Close reports the error; out's Drop then does nothing
		}
		if err != nil {
			return err              // out, then in, are dropped here too
		}
	}
}
```

**A cache with `shared[T]`.**

```go
type Image owner struct {
	w, h   int
	pixels vec.Vec[uint32]
}

type Cache owner struct {
	images hash.Map[uint64, shared[Image]]
}

func (c *Cache) Get(id uint64) shared[Image] {
	if img, ok := c.images.Get(id); ok {
		return img                  // a copy out of the map: count +1, the cache keeps its own
	}
	img := alloc.Shared[Image]()    // count 1
	img.decode(id)                  // fills it in through a view (a *Image method)
	c.images.Set(id, img)           // a copy into the map: count 2
	return img                      // last use, so a move: the count stays 2
}

func (c *Cache) Evict(id uint64) {
	c.images.Delete(id)             // count -1; an image still on screen stays alive
}

func show(c *Cache, id uint64) {
	img := c.Get(id)
	draw(&*img)                     // lends a *Image
}                                   // count -1: freed here if it was evicted meanwhile
```

**A tree with weak parent pointers.**

```go
type Node owner struct {
	name     strings.Buf
	parent   weak[shared[Node]]     // does not keep the parent alive
	children vec.Vec[shared[Node]]  // keeps the children alive
}

func addChild(parent shared[Node], name string) shared[Node] {
	child := alloc.Shared[Node]()
	child.name.WriteString(name)
	child.parent = weak[shared[Node]](parent)
	parent.children.Push(child)     // a copy: the child's count is 2
	return child                    // moved to the caller
}

func depth(n shared[Node]) int {
	d := 0
	for {
		p, ok := n.parent.Upgrade() // (shared[Node], bool)
		if !ok {
			return d
		}
		n = p                       // releases the old reference and moves p in
		d++
	}
}
```

When the last reference to the root is dropped, the root's count reaches zero; dropping it drops its
`children`, whose counts reach zero in turn, down the tree. If `parent` were a `shared[Node]`, every child
would keep its parent alive and the tree would never be freed.

**`synced[T]` and a Mutex across puroutines.**

```go
type Stats struct {
	mu    sync.Mutex
	total int
	most  int
}

func worker(id int, s synced[Stats], done chan<- int) {
	n := work(id)
	s.mu.Lock()
	s.total += n
	if n > s.most {
		s.most = n
	}
	s.mu.Unlock()
	done <- id
}                                   // s is dropped: the count goes down atomically

func main() {
	s := alloc.Synced[Stats]()
	done := make(chan int)
	for id := 0; id < 8; id++ {
		go worker(id, s, done)      // each puroutine gets its own reference: count +1
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	s.mu.Lock()
	fmt.Println(s.total, s.most)
	s.mu.Unlock()
}                                   // whichever puroutine drops the last reference frees s
```

With `alloc.Shared[Stats]()` the `go` statement would be a compile error (section 6.9).

**An arena.**

```go
func handle(req *Request) error {
	var mem [16 << 10]byte
	a := arena.New(mem[:])              // a views mem; both live in handle's frame
	fields := vec.New[Field](&a)        // allocates in the arena; holds a view of a
	parseFields(req.body, &fields)
	reply := strings.NewBuf(&a)
	for i := 0; i < fields.Len(); i++ {
		f := fields.Ptr(i)
		fmt.Fprintf(&reply, "%s=%s\n", f.key, f.val)
	}
	return send(req, reply.String())
}   // reply, then fields, are dropped: their frees go to the arena and do nothing;
    // a and mem go with the frame. Returning fields would be a compile error: it views a.
```

Code that allocates through `alloc.Default()` without an allocator parameter, such as `fmt.Sprintf`, is
pointed at an arena with `alloc.Use` (section 7.1).

**A kernel MMIO view.**

```go
type LAPIC struct {
	_   [0xB0]byte
	eoi volatile[uint32]
}

// A view of device registers. Nothing in the program owns them and nothing frees them,
// so the view has no lifetime limit and can live in a package variable.
var lapic = (*LAPIC)(unsafe.Pointer(uintptr(0xFFFF8000FEE00000)))

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	tick()
	lapic.eoi = 0
}

// A slice view of the framebuffer the boot loader mapped; returning it is fine.
func framebuffer(info *BootInfo) []uint32 {
	return unsafe.Slice((*uint32)(unsafe.Pointer(info.fbAddr)), info.width*info.height)
}
```

### 6.9 Diagnostics

Ownership and escape errors are written for a reader who has not read this section. Each one says what
happened in plain words, explains the idea it relies on, names the related line, shows fixes as code, and
points here. The texts below are exact, with the names, types and positions of the example filled in; the
fixes shown depend on the case and are chosen from those listed with each message.

**Returning a pointer to a local variable.**

```
config.p:7:9: cannot return &x, a pointer to a local variable
    x is a local variable of load (declared at config.p:4). Its memory is
    reclaimed when load returns, so a pointer to it would point at memory that
    no longer holds x. Plutonium never moves a variable to the heap because its
    address is taken.
    To give the value to the caller, return it instead. This moves it, which is free:
        return x
    To keep it at a fixed address after load returns, allocate it:
        x := new(Config)        // an owned[Config]; change the result type to match
    (see "Ownership" in the language spec, section 6.3)
```

**Returning a view of a local array or a local owner.**

```
names.p:12:9: cannot return b.String(), which points into b, a local variable
    b.String() does not copy anything: it is a view, a string that looks at the
    bytes b owns. b is a local variable of join (declared at names.p:5), and when
    join returns b is dropped and its bytes are freed, so the string would look
    at freed memory.
    To give the caller the text, return the buffer itself. That moves it:
        func join(parts []string) strings.Buf {
            ...
            return b
        }
    The caller reads it as a string with .String() for as long as it keeps it.
    (see "Ownership" in the language spec, section 6.3)
```

The same message with `a slice of buf, a local array` and the fix `return a vec.Vec[byte]` (or "copy the
bytes into a buffer the caller passes in: copy(dst, buf[:n])") is given for `return buf[:n]`.

**Storing a view where it outlives its target.**

```
cache.p:21:12: cannot store &e in c.entries, which outlives e
    &e is a pointer to e, a local variable of add (declared at cache.p:18). e's
    memory is reclaimed when add returns, but c.entries is reached through c,
    which points to memory that add's caller owns and that lives longer. The
    pointer would be left pointing at reclaimed memory.
    Store the value instead of a pointer to it. This copies e, or moves it if it
    is an owner:
        c.entries.Push(e)
    Or allocate e so that it lives as long as the cache:
        e := new(Entry)
        c.entries.Push(e)       // entries holds owned[Entry]
    (see "Ownership" in the language spec, section 6.3)
```

Storing in a package-level variable reads `cannot store &e in package variable last, which outlives e`
with "package variables live for the whole program" in place of the second sentence.

**Passing a view to a function that keeps it.**

```
main.p:12:12: cannot pass buf[:n] to cache.Put, which keeps it after returning
    buf[:n] is a slice that looks at buf, a local array of main (declared at
    main.p:9). cache.Put stores its parameter p in c.entries (cache.p:31), which
    lives longer than this call, so the slice would outlive buf.
    Give Put something that owns its bytes, for example a buffer:
        var b strings.Buf
        b.Write(buf[:n])
        c.Put(b)                // moves b into the cache; Put takes a strings.Buf
    (see "Ownership" in the language spec, section 6.3)
```

**Passing a view of the frame to a puroutine or a channel.**

```
srv.p:30:13: cannot pass &req to a go statement: the new puroutine may outlive req
    &req points to req, a local variable of serve (declared at srv.p:24). The
    puroutine started here runs on its own and may still be running after serve
    returns and req's memory is reclaimed.
    Pass the value itself. This copies it, or moves it if it is an owner:
        go handle(req)
    (see "Ownership" in the language spec, section 6.3)
```

For a send the first line is `cannot send &req on ch: the receiver may outlive req`, with "the value sent
here may be received after serve returns" as the explanation.

**Returning a closure that captures variables.**

```
count.p:9:9: cannot return the function literal: it uses total, a local variable of counter
    A function literal that uses variables of the function around it refers to
    those variables where they live, in counter's frame (total is declared at
    count.p:4). That frame is reclaimed when counter returns.
    Return a value that carries its own state instead, such as a struct with a method:
        type Counter struct{ total int }
        func (c *Counter) Add(n int) int { c.total += n; return c.total }
    (see "Ownership" in the language spec, section 6.3)
```

**Returning an interface that holds a frame copy, or lends an owner.**

```
parse.p:40:10: cannot return err, an interface holding a copy of a ParseError in parse's frame
    Converting a value larger than one word to an interface puts the value in
    the frame of the function that converts it (here at parse.p:38), and the
    interface points at that copy. The copy is reclaimed when parse returns.
    Return a pointer to memory that outlives parse, such as a field of the parser:
        p.err = ParseError{...}
        return &p.err
    or a word-sized error code.
    (see "Ownership" in the language spec, section 6.3, and "Errors", section 14)
```

When the interface lends an owner, the first line is `cannot return v, an interface that lends b, a local
variable`, the explanation is "converting an owner to an ordinary interface lends it: the interface only
looks at b, and b is dropped when f returns", and the fixes are "return b itself" and "declare an owning
interface (type Resource owner interface {...}) to move b into it".

**Using a variable after it was moved.**

```
main.p:14:2: m was moved to n at line 12, so it can no longer be used here
    hash.Map is an owner type: a map value owns its table and frees it when it
    is dropped. Assigning an owner moves it instead of copying it, so that the
    table has exactly one owner. Since line 12, n owns the table and m holds
    nothing.
    If you need two independent maps, copy it explicitly at line 12:
        n := m.Clone()
    If n only needs to look at the map, give it a pointer instead:
        n := &m
    (see "Ownership" in the language spec, section 6.4)
```

The first line names how the value left: `moved to n`, `moved into consume (its parameter takes a
hash.Map by value)`, `moved into c.cache`, `moved by the send on ch`, `moved to the puroutine started`,
`moved into an owning interface`, `dropped by m.Drop()`, or `moved by the call m.Name() (Name has a value
receiver)`. The fixes follow from it and from the later use, which the compiler finds by last-use
analysis:

- for a call that takes the owner by value: "If consume only needs to read the map, change its parameter
  to *hash.Map and pass &m";
- for a value receiver: "Declare Name with a pointer receiver so that calling it does not consume m:
  func (m *Map) Name() string";
- when the later use only looks at `m` and nothing between the two lines changes it: "This is m's last
  use. Move it before line 12 and no copy is needed:" followed by the reordered lines.

**Using a variable that may have been moved.**

```
main.p:20:5: b may have been moved at line 15, so it cannot be used here
    b is moved into flush at line 15 only when verbose is true. On that path b
    holds nothing afterwards, and the compiler cannot let a value be used on a
    path where it may already belong to someone else.
    Use b before line 15, or give it a new value on that path:
        if verbose {
            flush(b)
            b = strings.Buf{}
        }
    (see "Ownership" in the language spec, section 6.4)
```

In a loop the first line is `b was moved to out at line 9 in the previous iteration of this loop`, and the
fixes are "declare b inside the loop so that each iteration has its own" and "b.Clone()".

**Moving out of a field, an element or through a pointer.**

```
tree.p:31:10: cannot move n.left out of n: n.left is a field, not a variable
    Tree is an owner type, so taking n.left would move it. But n would still
    believe it owns n.left and would free it again when n is dropped.
    To move it out and leave an empty Tree in its place:
        left := take(&n.left)
    To look at it without moving it:
        left := &n.left
    To copy it:
        left := n.left.Clone()
    (see "Ownership" in the language spec, section 6.4)
```

For a range loop the first line is `cannot copy the elements of bufs in a range loop: strings.Buf is an
owner type`, and the fix is

```
        for i := range bufs {
            b := &bufs[i]
            ...
        }
```

**Moving a captured variable inside a function literal.**

```
job.p:17:8: cannot move b inside a function literal: the literal may run more than once, or never
    b is a variable of run (declared at job.p:12) that the literal uses where it
    lives. Moving it here would leave b empty after the first call, and run
    could not tell whether b still needs to be dropped.
    Pass the value to the literal as an argument:
        do := func(b strings.Buf) { send(b) }
        do(b)
    (see "Ownership" in the language spec, section 6.4)
```

**Using a view after its owner is gone.**

```
main.p:16:14: s cannot be used here: it looks at b's bytes, and b was moved at line 15
    s := b.String() at line 14 made s a view: a string that looks at bytes b
    owns, without copying them. At line 15 b was moved into send, and whoever
    owns the bytes now may free or change them at any time.
    Use s before line 15, or keep a copy that does not depend on b:
        c := b.Clone()
        s := c.String()
    (see "Ownership" in the language spec, section 6.3)
```

For a temporary:

```
main.p:8:7: s would look at a temporary that is dropped at the end of this statement
    fmt.Sprintf returns a strings.Buf, which owns the formatted bytes. It is not
    stored anywhere, so it is dropped when this statement ends, and s would look
    at freed bytes.
    Keep the buffer in a variable and look at it while it lives:
        msg := fmt.Sprintf("%d items", n)
        s := msg.String()
    (see "Ownership" in the language spec, section 6.4)
```

**A deferred call that uses an owner from an inner block.**

```
main.p:11:9: the deferred call uses b, which is dropped at the end of its block (line 14)
    Deferred calls run when the function returns. b is declared in the block
    that ends at line 14 and is dropped there, before this call would run, so
    the call would use freed memory.
    Declare b at the top level of the function, or do the work at the end of
    the block instead of deferring it.
    (see "Ownership" in the language spec, section 6.4)
```

**A `Drop` method on a type not declared owner.**

```
file.p:9:1: File has a Drop method but is not declared owner
    Drop runs automatically when a value is destroyed. That only makes sense
    for a type whose values have exactly one owner: if File values were copied
    as ordinary values are, every copy would run Drop and close the same
    descriptor.
    Declare the type as an owner, so that its values move instead of being copied:
        type File owner struct {
            ...
        }
    (see "Ownership" in the language spec, section 6.4)
```

A `Drop` with parameters or results reads `Drop must have no parameters and no results`, explaining that
the compiler calls it with nothing and ignores any result; if the method is not meant as a destructor, the
fix is to rename it.

**An owner field in a struct not declared owner.**

```
conn.p:4:2: Conn must be declared owner: its field buf is a strings.Buf, which is an owner
    An owner owns memory and frees it when dropped, so it moves instead of
    being copied. A struct that contains one owns that memory too, and copying
    the struct would copy the owner, leaving two values that both free it.
    Plutonium does not make Conn an owner silently; say so in its declaration:
        type Conn owner struct {
            ...
        }
    Conn values then move, and buf is dropped when a Conn is.
    If Conn should only look at a buffer that something else owns, store a pointer:
        buf *strings.Buf
    (see "Ownership" in the language spec, section 6.4)
```

The same message covers an embedded owner (`its embedded field Buf`), an array of owners (`its field
pages is a [4]Frame, an array of owners`), an interface embedding an owning interface, and `type B A`
(`B is defined from A, which is an owner; write type B owner A`).

**`owner` where it is not allowed.**

```
kind.p:3:11: owner cannot qualify an enum type
    owner marks a type whose values own something and move instead of being
    copied. An enum is a set of integer constants, and its values are always
    copied. To attach a resource to an enum value, wrap both in an owner:
        type Handle owner struct {
            kind Kind
            fd   int32
        }
    (see "Ownership" in the language spec, section 6.4)
```

For other kinds the first line names them (`owner cannot qualify a slice type`), the explanation says a
slice is a view, and the fix is the same wrapper struct. `owner` outside a type definition reads `owner can
only be used in a type definition: type T owner struct {...}`.

**Calling `Drop` through a view.**

```
pool.p:22:2: cannot call p.Drop(): p is a *File, which only points at a File that something else owns
    Drop releases what an owner holds. The File p points to belongs to another
    variable, which will drop it when its scope ends, so dropping it here would
    release it twice.
    To end an owner's life early, call Drop on the owner itself; it cannot be
    used afterwards:
        f.Drop()
    To leave an empty File behind instead:
        old := take(p)
    (see "Ownership" in the language spec, section 6.4)
```

**An owner type for a type parameter that does not allow owners.**

```
main.p:10:12: cannot use strings.Buf as T in Fill: strings.Buf is an owner and T does not allow owners
    Inside Fill, values of T are copied (Fill assigns v to every element at
    fill.p:4). Copying an owner would leave several values that all free the
    same memory.
    If Fill can be written so that each value of T is moved at most once, allow
    owners in its declaration:
        func Fill[T owner any](s []T, v T)
    Otherwise fill with copies made explicitly:
        for i := range bufs { bufs[i] = v.Clone() }
    (see "Ownership" in the language spec, section 6.4)
```

**Duplicating owners with `copy` or `append`.**

```
main.p:7:2: cannot copy elements of type strings.Buf: each element is an owner
    copy duplicates elements, and duplicating an owner would leave two values
    that both free the same memory.
    Move the elements one at a time:
        for i := range src { dst[i] = take(&src[i]) }
    or copy each one explicitly:
        for i := range src { dst[i] = src[i].Clone() }
    (see "Ownership" in the language spec, section 6.4)
```

**Taking an owner out of an ordinary interface.**

```
main.p:19:7: cannot assert x to strings.Buf: x is an any, which only lends the value it holds
    Converting an owner to an ordinary interface such as any lends it: the
    interface looks at the owner, which still belongs to the variable that was
    converted. Taking a strings.Buf out of x would create a second owner.
    Call the value's methods through an interface type instead:
        if s, ok := x.(fmt.Stringer); ok { use(s.String()) }
    or move it into an owning interface in the first place:
        type Text owner interface{ String() string }
    (see "Ownership" in the language spec, section 6.4, and section 4.4)
```

**A `shared[T]` crossing puroutines.**

```
worker.p:22:12: cannot pass s to a go statement: shared[Stats] can only be used by one puroutine
    shared[T] counts its references with ordinary arithmetic, which is fast but
    breaks if two processors change the count at the same moment. The new
    puroutine may run on another processor while this one copies or drops s.
    Use synced[T], whose count is atomic, to share across puroutines:
        s := alloc.Synced[Stats]()
    and protect the fields of Stats that change with a sync.Mutex inside Stats.
    (see "Ownership" in the language spec, section 6.5, and section 4.7)
```

For a send the first line is `cannot send s on ch: shared[Stats] can only be used by one puroutine`; when
the reference is inside another value, it continues `(Job contains a shared[Cache] in its field cache)`.

## 7. Memory and allocation

Plutonium never allocates heap memory unless the program asks: through `new`, `make`, the `pluto/alloc`
functions, `owned`/`shared`/`synced` constructors, or library code that takes an allocator. Every
allocation belongs to an owner or an arena (section 6.6). Local variables, temporaries and interface frame
copies live on the stack. Package-level variables live in static storage (`.data` or `.bss`).

### 7.1 Allocators

```go
package alloc

type Allocator interface {
	Alloc(size, align uintptr) unsafe.Pointer      // zeroed memory, or nil
	Free(p unsafe.Pointer, size, align uintptr)
}

func Default() Allocator                // the current allocator
func SetDefault(a Allocator)            // replaces it for all later allocations
func Use(a Allocator) Restore           // replaces it until Restore is called
func (r Restore) Restore()

// Owners of a single heap object (sections 5.6 and 6).
func Owned[T any](a Allocator = Default()) owned[T]
func Shared[T any](a Allocator = Default()) shared[T]
func Synced[T any](a Allocator = Default()) synced[T]

// The unchecked layer that owner types are written with (section 6.6).
func Raw(size, align uintptr, a Allocator = Default()) unsafe.Pointer
func New[T any](a Allocator = Default()) *T
func Free[T any](p *T, a Allocator = Default())   // drops *p first if T is an owner

// Until channel references are counted (section 4.8).
func FreeChan[T any](ch chan T)
```

`Use` scopes an allocator to a region of code without changing the code inside it. The override belongs
to the puroutine that calls `Use`: `Default()` returns it there and the program-wide default everywhere
else, and a new puroutine starts with the program-wide default. `Use` returns a `Restore` value rather
than a closure, since a closure that captures the previous allocator could not be returned (section 4.5).

```go
func handle(req *Request) {
	defer alloc.Use(req.arena).Restore()
	parse(req) // every allocation through Default() lands in the arena
}
```

Owners allocated while the override is in force record the arena as their allocator, so dropping them
later frees into the arena, which does nothing (section 6.6).

### 7.2 The system allocator

In hosted builds the default allocator is `alloc.SyncSlab`, a slab allocator over `mmap` that is safe to
use from several puroutines at once: each processor of the hosted scheduler allocates from its own cache
of free slots, without locks, in front of shared free lists behind one runtime lock, and requests over
2 KiB go straight to `mmap`. `alloc.SetDefault` must likewise be given a thread-safe allocator once
puroutines run in parallel (`alloc.Slab`, which is not, suits a single processor or a lock of the
kernel's own); an arena installed with `alloc.Use` belongs to the puroutine that installed it. In freestanding builds there is
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

### 7.3 The debug allocator

Raw pointers and heap views are not yet checked at run time (section 6.3), and the unchecked allocation
layer can be misused, so overflows, use after free and double frees are possible. The debug
allocator, `pluto/alloc/debug`, catches them deterministically. `pluto build --alloc=debug` makes it the
default allocator of a hosted program without code changes (settings come from `PLUTO_DEBUGALLOC`), and
`alloc.SetDefault(debug.New(debug.DefaultOptions()))` installs it explicitly, in freestanding builds over
the kernel's page provider.

- Each allocation ends right before a guard page that is inaccessible, so an overrun faults at once
  (`underflow` puts the guard page before it instead). Bytes of its pages it does not use hold canaries,
  checked when it is freed. Past a cap on guard pages, or for small allocations if asked, allocations sit
  between canaries instead.
- Freed memory is poisoned (`0xDD`), made inaccessible where it has pages of its own, and held in a
  bounded first-in, first-out quarantine before reuse, so stale pointers fault or read poison.
- A side table of every allocation catches double frees, frees with the wrong size or alignment, and
  frees of pointers that were never allocated.
- At exit, and on demand with `alloc.ReportLeaks()`, it lists the allocations still live, with the call
  site that made each one.

Memory it returns is zeroed, like any allocator's. Every error stops the program as a panic does. It is
safe for concurrent use: its state sits behind one runtime lock, and the site of each allocation is
recorded per puroutine. [debug-allocator.md](debug-allocator.md) has the details.

## 8. Runtime faults and panic

The following stop the program through the same path as `panic`:

- an index or slice expression out of range,
- integer division or remainder by zero,
- a failed type assertion with a single result, on an interface or a union (section 5.10),
- a union whose tag names no variant, in a type switch without a default case,
- `append` beyond capacity,
- an allocation with no allocator set,
- sending on a closed channel, or closing a closed or nil channel,
- an iterator calling the yield function of a range-over-func loop when it may not (section 4.10),
- overflowing a puroutine's stack (the guard page faults),
- a `go` statement whose function value, or a func-typed argument, has its closure record in the starting
  puroutine's stack (section 4.7),
- an operation that would park in interrupt context, when the scheduler can tell (`InInterrupt`, abi.md
  section 12),
- `panic(v)`.

A program that needs a scheduler and binds none fails to build rather than panic (section 4.7). A panic
in a puroutine other than the one that runs `main` names it: the built-in handler prints `puroutine N:`
before the trace, and `rt.Panic.Puroutine` holds the number.

A panic does not unwind: deferred functions do not run and owners are not dropped. The runtime writes
the panic's message and calls the program's **panic handler** (section 8.1), which never returns. In
hosted builds without a handler of their own, the built-in one prints `panic: ` followed by the value
(and, for faults, what failed and where), then a stack trace with the function, file and line of each
frame, and the process exits with status 2.

Dereferencing a nil pointer is not checked by the compiler; it faults in hardware wherever address zero is
unmapped, which is true in hosted builds and should be arranged by kernels. In hosted builds the runtime
turns such faults, and stack overflows, into a panic, with a trace from the faulting instruction; a kernel
does the same from its exception handlers with `rt.PanicFault` (section 8.1). Heap misuse through raw
pointers is not checked either, except by the debug allocator (section 7.3), which reports it through the
same path: a fault in its heap is described by the allocator, and that description is the panic's
message.

Stack traces and DWARF debugging information (`-g`, on by default; `-s` strips it) are described in
[debugging.md](debugging.md); the debug allocator in [debug-allocator.md](debug-allocator.md).

Bounds checks can be removed from a function with `//pu:nobounds`, for code that has been proven correct
and must be as fast as possible. The optimiser removes most bounds checks by itself.

### 8.1 Panics in freestanding programs

A program says what happens when it panics by marking one function `//pu:panic`:

```go
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(serial)           // "panic: ...", the fault if any, the stack trace
	for {
		x86.Cli()
		x86.Hlt()
	}
}
```

- **One handler.** Freestanding and kernel builds must have exactly one; none, or more than one, is a
  build error, since such a program has no operating system to report a crash to. A hosted program may
  define one to replace the built-in handler, which prints the panic and exits with status 2.
- **Its declaration.** A plain function with a body, not a method, not generic, not also
  `//pu:interrupt`, declared `func name(p *rt.Panic)`.
- **It never returns.** A panic has nothing to return to. The compiler checks that every path through the
  handler ends in an endless loop or in a call of a function that never returns: one marked
  `//pu:noreturn` (`os.Exit`, an assembly halt routine), or one whose every path ends that way, which the
  compiler infers. A call that the compiler cannot see through (through a func value or interface, or
  into recursion) counts as returning.
- **A direct call.** The runtime's panic path calls the handler directly; nothing is registered at run
  time, so it works from the first instruction of the program, before any initialisation.
- **It must not wait.** The handler runs on top of whatever panicked, possibly an interrupt handler or
  code holding a lock, so the rules of section 12.6 apply to it as to an interrupt handler.

`rt.Panic` describes the panic:

```go
type Panic struct {
	Message string // "runtime error: index out of range [4] with length 4", or the value passed to panic
	Value   any    // the value passed to panic, nil for runtime errors and faults
	Fault   Fault  // the hardware fault, if the panic came from one; Fault.Kind is NoFault otherwise
	CPU     int    // the processor that panicked: in kernel mode its number from rt.SetCPU, otherwise 0
}

type Fault struct {
	Kind   FaultKind // NoFault, MemoryFault, DivideFault or IllegalInstruction
	Addr   uintptr   // the address accessed, for a MemoryFault, if known
	PC     uintptr   // the faulting instruction
	Signal int       // the signal that reported it in hosted builds, or 0
}

func (p *Panic) Print(w rt.Writer)      // the panic as a hosted program prints it, then the trace
func (p *Panic) PrintTrace(w rt.Writer) // the stack trace alone
```

`rt.Writer` has the method of `io.Writer`, so any writer will do; `rt.Console{}` writes where `print`
does (in freestanding builds, the function registered with `rt.SetConsole`). The message lives in a
buffer of the runtime (1 KiB, longer messages are cut), and printing the trace reads the trace table and
the stack (debugging.md section 5), so nothing allocates. Since a panic does not unwind, the stack the
trace describes is still there while the handler runs.

Hardware faults reach the handler too. In hosted builds the runtime's signal handler does it. A kernel's
exception handlers call

```go
rt.PanicFault(kind rt.FaultKind, addr, pc, sp, bp uintptr) // never returns
```

with the faulting state from the interrupt frame, and the trace starts at the faulting instruction:

```go
//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) {
	rt.PanicFault(rt.MemoryFault, uintptr(x86.ReadCR2()), uintptr(f.RIP), uintptr(f.RSP), 0)
}
```

Tools that describe an error at length, such as the debug allocator, start the panic with
`rt.BeginPanic()`, print the description with `print` (which then goes into the panic's message), and
raise it with `rt.Abort()` or, for a fault, `rt.PanicFault`.

The runtime guards the handler:

- **One panic at a time.** The first processor (or, hosted, thread) to panic takes an atomic flag and runs
  the handler. One that panics while another handles a panic stops: a kernel's processor disables
  interrupts and halts, a hosted thread blocks until the first ends the process. In kernel mode the
  runtime tells processors apart by the number each one gives itself with `rt.SetCPU(n)`, which a kernel
  calls on every processor once its GS base points at its per-CPU block (section 12.5); until the first
  call, every processor counts as CPU 0. Freestanding builds outside kernel mode count all processors as
  one.
- **A last resort.** A panic inside the handler, or while a panic's message is being written, does not
  call the handler again: in freestanding and kernel builds the processor disables interrupts and halts
  (`cli` then `hlt` in a loop), touching no memory or device; in hosted builds the runtime writes `fatal:
  panic while handling a panic` to standard error with a raw system call and exits with status 2.

Errors name the rule they break and say what to do. Their codes are E2580 (no handler), E2581 (a second
handler), E2582 (a declaration that cannot be the handler) and E2583 (a handler that can return); for
example, a kernel without a handler:

```
kernel: no panic handler is defined
    A kernel build has no operating system to report a crash to, so the program
    itself must say what happens when it panics (print to a serial port or
    screen, then halt).
    A kernel has no operating system to fall back on when it panics, so
    Plutonium makes the program choose what happens at compile time, and checks
    the choice: the runtime calls the handler directly, with nothing to
    register, so it works from the first instruction.
    Mark one function with //pu:panic:
        //pu:panic
        func onPanic(p *rt.Panic) { ... }
    (see "Panics in freestanding programs" in the Plutonium spec, section 8.1)
    (run "pluto explain E2580" for a longer explanation)
```

## 9. Directives

Directives are line comments starting with `//pu:` with no space, placed in the doc comment of the
declaration they apply to (or at the top of the file for `//pu:build`).

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:build expr` | file | include the file only when `expr` holds |
| `//pu:inline` | function | inline calls to this function whatever its size, as long as the caller stays within its growth limit (four times its own size, at least 5,000 and at most 20,000 IR values) |
| `//pu:noinline` | function | never inline it |
| `//pu:nobounds` | function | omit bounds checks in its body |
| `//pu:extern name` | body-less function | the function is the external symbol `name` (C ABI) |
| `//pu:export name` | function | also export the function under the symbol `name` |
| `//pu:section name` | function or package variable | place it in the named section |
| `//pu:interrupt` | function | an x86-64 interrupt handler: saves every register it may change, returns with `iretq` (section 12.4) |
| `//pu:noreturn` | function | the function never returns (lets the optimiser drop code after calls) |
| `//pu:panic` | function | the program's panic handler, `func(p *rt.Panic)`; exactly one in freestanding and kernel builds (section 8.1) |
| `//pu:blocking` | function (with or without a body) | calling it may wait (park the caller); interrupt handlers and the panic handler must not reach it (section 12.6) |
| `//pu:scheduler` | package variable | the program's scheduler backend, of a type implementing `rt.Scheduler`; at most one in a program, replacing the hosted one (section 4.7) |
| `//pu:nosimd` | function | the optimiser adds no vector instructions to it (implied by `//pu:interrupt`, and by kernel mode outside `//pu:fpu` functions) |
| `//pu:fpu` | function | may use floats in kernel mode; the caller has saved the FPU state (section 12.3) |
| `//pu:percpu` | package variable | one copy per processor, addressed through `GS`; kernel mode only (section 12.5) |
| `//pu:stack size` | `func main` | run the program on a stack of `size` bytes (`K`, `M` or `G` suffixes, 64K to 64G) that the hosted runtime reserves, maps as it is used and puts a 1 MiB guard region below; without it, main runs on the stack the system gave the process (usually 8 MiB). Hosted builds only |

A program that recurses deeply checks how much stack remains with `rt.StackLeft()`, which returns the bytes
between the caller's frame and the limit of its stack: in a puroutine, the guard page below the
puroutine's stack (section 4.7); on the main thread, the reserved stack's guard or the process stack's
resource limit. It returns `^uintptr(0)` where the limit is unknown. `rt.StackBounds` returns
the bounds, and a freestanding program that sets up its own stack records them with `rt.SetStackBounds`.

## 10. Assembly and C

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

## 11. The unsafe package

`unsafe` is Go's: `Pointer`, `Sizeof`, `Alignof`, `Offsetof`, `Add`, `Slice`, `SliceData`, `String`,
`StringData`. Converting between `unsafe.Pointer`, `uintptr` and pointer types follows Go's rules,
without Go's restrictions that exist only for the garbage collector. Every package that uses raw memory
imports `unsafe`, so such code is easy to find. An `unsafe.Pointer` is a view (section 6.3); one made from
a `uintptr` is assumed to point outside every frame and to memory nobody in the program frees.

## 12. Build modes and targets

### 12.1 Modes

The mode is chosen with `--mode`; by default it follows the target (`hosted` for `x86_64-linux`,
`freestanding` for `x86_64-none`).

| Mode | Meaning | Tags set |
|---|---|---|
| `hosted` | a Linux program: `_start` sets up arguments and calls `main` | `linux`, `hosted` |
| `freestanding` | no operating system; the program provides its entry point and linker script | `freestanding`, `none` |
| `kernel` | `freestanding`, plus code generation for ring 0 (section 12.2) | `freestanding`, `none`, `kernel` |

Targets are named by triples: `x86_64-linux` (default) and `x86_64-none`; `aarch64-linux` and
`aarch64-none` are planned. The architecture tag (`amd64`, `arm64`) is always set.

Output formats are ELF executables (default) and flat binaries (`--format binary`). PE32+ for UEFI
(`--format pe32+`) is planned; it needs position-independent code (abi.md section 7).

### 12.2 Kernel mode

`pluto build --target x86_64-none --mode kernel` builds code that can run in ring 0 with interrupts
enabled and no vector state saved on entry. It differs from `freestanding` in three ways, specified in
[abi.md section 10](abi.md#10-kernel-mode):

- **No red zone.** Plutonium code never stores below the stack pointer in any mode (the backend
  subtracts from RSP before using stack memory, and leaf functions without stack slots have no frame), so
  nothing changes for Plutonium code. Assembly and C linked into a kernel must not use the red zone
  either.
- **No floating point** (section 12.3).
- **Kernel code model.** The image is linked in the top 2 GiB of the address space. The compiler's
  addressing (RIP-relative symbol addresses, and absolute sign-extended 32-bit displacements for
  `sym+disp32(,index,scale)`) is correct there without change. Without `--ldscript`, the build links with
  a default kernel linker script (abi.md section 10.3) that places the image at 1 MiB physical and
  `0xFFFFFFFF80100000` virtual.

Kernel mode also enables per-CPU variables (section 12.5). `pluto/arch/x86` (section 12.4) and
`//pu:interrupt` handlers work in every mode; in kernel mode a handler without `//pu:fpu` saves no vector
registers. `pluto/os` and `pluto/syscall` are unavailable, as in every freestanding build. `pluto run`
refuses kernel builds. Like every freestanding program, a kernel must define its panic handler (section
8.1).

### 12.3 Floating point in kernel mode

An interrupt or system call that enters the kernel does not save the SSE and x87 registers, so kernel code
must not touch them. In kernel mode, any floating-point operation (arithmetic, comparison, conversion,
a float constant, passing or returning a float, converting a float to an interface) in a function the
program uses is a compile error. "Passing" counts at the level of the calling convention: a struct
parameter whose float fields would travel in XMM registers (one of at most four leaves, abi.md section 3)
is an error, while one passed by reference is not. Float types may still appear, and copying float-typed
values (whole structs, fields, array elements, zero values) is allowed, since it uses integer registers.
Because the compiler only generates the functions a program reaches, a float function in an imported
package that the kernel never calls costs nothing, and the standard library keeps its float code behind
`//pu:build !kernel` (in `pluto/fmt`, a float argument prints as its bits in kernel builds, and so does
a float passed to `print` or `println` from a `//pu:fpu` function). Package-level
arrays and structs initialised with constants, such as `pluto/math`'s coefficient tables, are written into
the image, so importing such a package adds no float code to package initialisation.

A function marked `//pu:fpu` may use floats in kernel mode, and so may the function literals inside it.
Whoever calls it must have saved the interrupted context's vector state and enabled the unit
(`x86.FPUBegin` and `x86.FPUEnd`); the compiler does not check this, but it never inlines a `//pu:fpu`
function into one without it. Soft-float was rejected: it would put an IEEE-754 library in every kernel
and hide a large cost behind ordinary arithmetic, for code that hardly ever wants floats.

### 12.4 `pluto/arch/x86`

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

### 12.5 Per-CPU variables

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

### 12.6 Interrupts

A function marked `//pu:interrupt` is an interrupt or exception handler, entered by the processor rather
than called; [abi.md section 10.4](abi.md#104-interrupt-handlers) gives its signature, entry and exit.

**Interrupt context cannot block.** A handler runs on top of whatever code the processor was executing.
If it waits, for a lock or for room in a channel, the interrupted code can never resume to release it, and
the processor stops. So nothing an interrupt handler can reach, and nothing the panic handler (section
8.1) can reach, may:

- send or receive on a channel, except as a case of a `select` with a `default` case; run a `select`
  without a `default` case; or range over a channel;
- call a function marked `//pu:blocking`: those that park the caller, such as `pluto/sync`'s
  `Mutex.Lock` and `WaitGroup.Wait` and the runtime's parking operations, and any function a program
  marks;
- start a puroutine (`go`), which allocates a stack and takes the scheduler's locks;
- allocate from an allocator that may wait (below).

Operations that do not wait are allowed: `select` with `default`, `TryLock`, readying a parked
puroutine, atomics, and spin locks that disable interrupts while held. To hand work to normal code, a
handler sends without waiting:

```go
select {
case work <- item:
default: // full: drop it, or count the loss
}
```

The compiler checks this over the whole program. From each interrupt handler the program uses and from
the panic handler it follows every call, through every package, and reports the first path it finds to
an operation above, naming each call on the way:

```
irq.p:5:3: keyboard is an interrupt handler, so it must not wait
    Interrupt handlers run on top of whatever code the CPU was executing. If a
    handler waits, for a lock or for room in a channel, the interrupted code can
    never resume to release it, and the CPU stops.
    keyboard calls readConfig (irq.p:5), which waits on cfgMutex (config.p:22).
    A kernel that waits in an interrupt handler hangs, often only under load and
    far from the cause, so Plutonium checks the whole program at compile time
    instead.
    To hand work to normal code, use a non-blocking send:
        select { case work <- item: default: }
    (see "Interrupts" in the Plutonium spec, section 12.6)
    (run "pluto explain E2584" for a longer explanation)
```

A go statement on the path is reported the same way, as E2585 ("so it must not start a puroutine").

- **What is followed.** Direct calls, method calls, deferred calls and the calls the compiler adds for
  `new`, `make`, interface conversions and panics. It does not enter a `//pu:blocking` function, and a
  path from an interrupt handler stops at the panic handler, which is checked on its own.
- **Calls through interfaces and func values** cannot be resolved exactly, so the compiler is
  conservative, using what it knows about the whole program. A call of method `m` through an interface
  type `I` may reach `m` of every type the program converts to `I`, whether directly, from another
  interface, or by a type assertion (the method tables the program contains). A call through a func value
  of type `F` may reach every function of type `F` whose value the program takes anywhere (as a func
  value, a method value or a method expression, or a function literal). The message says when a path goes
  through such a call. To make a handler's call exact, call the function directly.
- **Allocation.** An allocator is not marked as a whole: `new` and `make` call the allocator's `Alloc`
  through the `alloc.Allocator` interface, and the check follows that call into every allocator the
  program installs. An allocator whose `Alloc` or `Free` may wait, because it takes a `//pu:blocking`
  lock (a slab cache refilled under a `sync.Mutex`), is reported like any other function that may wait;
  one that uses a spin lock with interrupts disabled, or none, is allowed. So `//pu:blocking` belongs on
  the primitives that park, and no allocator needs a marking of its own. (The alternative, forbidding all
  allocation in interrupt context unless an allocator is marked safe, was rejected: it would forbid the
  common, safe case of a lock-free per-CPU cache, and the marking could be wrong without the compiler
  noticing.)
- **Assembly and C** are not looked into: a body-less function counts as not waiting unless its
  declaration is marked `//pu:blocking`.
- **Non-blocking channel operations** lower to runtime functions that are not marked `//pu:blocking`
  (`selectnb`, `closechan`), so that a `select` with `default` stays allowed; the waiting forms call
  ones that are (`chansend1`, `chanrecv1`, `chanrecv2`, `selectgo`, `spawnGo`).
- **The scheduler's `Wake`.** `rt.Ready` calls the bound scheduler's `Wake` (section 4.7), and interrupt
  handlers may call `rt.Ready`, so `Wake` is checked as a root too, with the message "is the
  scheduler's Wake, which interrupt handlers reach through rt.Ready".

## 13. Standard library

The library's containers, buffers and files are owners (section 6.4); their `Drop` methods free what they
hold, and most have a `Clone`.

| Package | Contents |
|---|---|
| `pluto/rt` | panic hook, program entry, memory copy and compare primitives the compiler calls, the scheduler contract (`Scheduler`, `Task`, `Switch`, `Park`, `Ready`, `Preempt`), `Go`, `Yield`, the hosted scheduler |
| `pluto/sync` | `Mutex`, `RWMutex`, `WaitGroup` (with `Go`), `Once`, `Cond` over the scheduler; the operations that park are `//pu:blocking` |
| `pluto/alloc` | allocators, the default allocator, `Use`/`Restore`, `Owned`/`Shared`/`Synced`, and the unchecked layer `Raw`/`New`/`Free` (section 7.1) |
| `pluto/alloc/debug` | the checking allocator (section 7.3) |
| `pluto/arena` | bump allocators; freeing into one does nothing, and `Reset` reclaims everything (section 6.6) |
| `pluto/fmt` | `Printf`, `Fprintf`, `Sprintf` returning a `strings.Buf`, `Bprintf` into a caller's buffer, `Println`; integer, float, string, pointer verbs with width, precision and flags; arrays, slices, structs, pointers, interfaces and enums with `%v`, `%+v` and `%#v` as Go prints them, read from type descriptors (section 4.4), and containers through `fmt.Mapper` (`hash.Map` prints as `map[k:v]` with sorted keys) and `fmt.Lister` (`vec.Vec` prints as a slice); all formatting goes through a fixed buffer and never allocates; `Errorf` and `ErrorBuf` (section 14) |
| `pluto/container` | the constraints of the indexing interface (section 6.7): `Indexable`, `Reader`, `Lookup`, `Sized` |
| `pluto/io` | `Reader`, `Writer`, buffered I/O |
| `pluto/os` | files (`os.File`, an owner that closes on drop), `Stat`, whole-file reads and writes, directories (`ReadDir` returns a `DirEntries` owner), removal, renaming, temporary directories, the working directory, pipes, processes, arguments, environment, exit (hosted) |
| `pluto/os/exec` | running commands: `Cmd` (an owner), pipes copied inside `Wait`, `ExitError` in the data word (hosted) |
| `pluto/path/filepath` | slash-separated paths: views (`Base`, `Dir`, `Ext`, `Split`) and `Append` forms (`AppendJoin`, `AppendClean`, `AppendRel`, `AppendAbs`), `Match`, `WalkDir` |
| `pluto/time` | `Duration`, `Time` in UTC with a monotonic reading, `Now`, `Since`, `Sleep` |
| `pluto/flag` | command-line flags; a `FlagSet` owns its flags and their variables |
| `pluto/syscall` | raw Linux system calls (hosted) |
| `pluto/strings`, `pluto/bytes` | `strings.Buf`, an owning string buffer (section 6.6); searching, comparison; Go 1.24's iterators `Lines`, `SplitSeq`, `SplitAfterSeq`, `FieldsSeq`, `FieldsFuncSeq` |
| `pluto/iter` | `Seq` and `Seq2` (section 4.10), and adaptors that allocate nothing: `Filter`, `Map`, `Limit`, `Concat` (each with a `2` form), `Enumerate`, `Keys`, `Values`, `Reduce`, `Count`, `Fill`. Go's `Pull` needs a coroutine per iterator and has no equivalent |
| `pluto/bits` | bit operations (section 5.8) |
| `pluto/math` | floating-point functions |
| `pluto/hash` | `Map[K, V]` and `Set[K]`, owners that record their allocator; `All`, `Keys` and `Values` iterate in table order, the same for the same changes and seed |
| `pluto/vec` | `Vec[T]`, a growable owner that records its allocator; `All`, `Values` and `Backward` iterate it, reading it as they go so the body may change it |
| `pluto/sort` | sorting without allocation |
| `pluto/sync/atomic` | atomic integers and pointers |
| `pluto/unicode/utf8` | UTF-8 encoding and decoding |
| `pluto/testing` | tests and benchmarks for `pluto test` (hosted) |
| `pluto/errors` | `New` (a static error per call site), `Is`, `As`, `AsType`, `Unwrap`, `Join`, `Free` (section 14) |

`pluto/os` and `pluto/syscall` are not available in freestanding builds.

## 14. Errors

Errors are values of the predeclared `error` interface, handled as in Go: a function returns an `error`
last, `nil` means success, and callers write `if err != nil { return err }`. `pluto/errors` provides
`Is`, `As`, `Unwrap` and `Join` with Go's semantics, and `fmt.Errorf` wraps with `%w`. Since `panic`
aborts (section 8), errors are the only way to recover from a failure.

What differs from Go is where an error lives. An `error` is an ordinary interface (section 4.4), so an
error value larger than a word would be boxed in the converting function's frame, an owner converted to
`error` is only lent, and Plutonium never allocates implicitly (section 7). The rule that follows is:

**An error returned from a function is either a value that fits in a word, or a pointer to storage that
outlives the function.** That storage is one of:

| Storage | Used for | Example |
|---|---|---|
| the interface's data word | error codes, and structs wrapping one pointer (section 4.4) | `syscall.Errno`, a kernel's own code type, `struct{ d *detail }` |
| static storage | sentinel errors and fixed messages | `var ErrNotFound = errors.New("not found")`; `return errors.New("bad header")` |
| a field of a longer-lived object | errors with context that belong to an owner | a parser that returns `&p.err`; `fmt.ErrorBuf` |
| memory from an allocator | errors with context that travel up the stack | `*os.PathError`, `fmt.Errorf`, `errors.Join` |

Returning a struct as an `error`, or a pointer to a local variable or composite literal, breaks the rule;
both are compile errors (sections 4.4 and 6.3).

### 14.1 Sentinels and `errors.New`

`errors.New(text)` works anywhere, as in Go: in a package-level variable and inside a function.

```go
var ErrNotFound = errors.New("not found")

func parseHeader(b []byte) error {
	if len(b) < 4 {
		return errors.New("short header")
	}
	...
}
```

`text` must be a constant; any other argument is a compile error that points to `fmt.Errorf`. The
compiler evaluates each call to the address of a static, read-only `errors.Text{Msg: text}` that belongs
to the call site, so `errors.New` never allocates and never runs out. The same call site always returns
the same error, however often it runs and in every instance of a generic function; different call sites
return different errors, even with equal messages, so two packages' sentinels never compare equal.
(Go returns a new error from every evaluation; no correct program can tell, except by comparing two
results of one call site.) Since its result belongs to a call site, `errors.New` must be called
directly: taking its value (`f := errors.New`) or deferring it is a compile error.

A sentinel may also be written as a static literal, `var ErrClosed = &errors.Text{Msg: "file already
closed"}`, and a package-level initializer may convert a struct to `error` directly, since its box is
static (section 4.4).

### 14.2 Error codes

A named integer type with an `Error() string` method fits in the data word, so it can be returned from
anywhere and costs nothing. `syscall.Errno` is the standard one: its `Error` returns Linux's message for
the code, `Name` its symbol (`"ENOENT"`), and its `Is` method makes `errors.Is(err, os.ErrNotExist)`,
`os.ErrExist`, `os.ErrPermission` and `errors.ErrUnsupported` match the corresponding codes.

### 14.3 Errors with context

An error that records context (a path, a line and column) needs storage for it. Its message is formatted
when the error is made, into that storage, because `Error()` must return a string and cannot concatenate
one at run time.

- **Allocated.** `fmt.Errorf(format, args...)` allocates the error and its message in one block from the
  default allocator, and so does `errors.Join`; both are variadic, so they cannot take a defaulted
  allocator (section 5.1), and `alloc.Use` installs another allocator around them.
  `os.NewPathError(op, path, err, a = alloc.Default())` allocates from its last argument, and so do
  `os.Open`, `os.Create` and `os.OpenFile` when they fail. Such an error has a `Free()` method. `errors.Free(err)` releases every allocated error
  in `err`'s tree and skips the others; a wrapper owns the errors it wraps, so free only the outermost
  one. Code that runs under an arena (`defer alloc.Use(&a).Restore()`) needs no `Free`. This is the
  accepted design; it is the one place where memory is allocated without an owner (section 6.6), and
  section 14.6 proposes replacing `errors.Free` with release that happens exactly once, automatically.
- **Owned.** An object that performs operations (a parser, a device, a connection) keeps its last error in
  a field and returns a pointer to it. The error is valid while the object lives and until its next
  error. `fmt.ErrorBuf` is such a field: `buf.Errorf(format, args...)` formats into its 200-byte buffer,
  wraps the first `%w` operand, and returns the buffer as the error.

### 14.4 Wrapping, `Is` and `As`

`fmt.Errorf` and `ErrorBuf.Errorf` accept `%w`, which formats an error operand like `%v` and wraps it.
With one `%w` the result has `Unwrap() error`; with several, `Unwrap() []error`. `%w` in any other
formatting function prints `%!w(error)`.

`errors.Is(err, target)` reports whether any error in `err`'s tree equals `target` or has an
`Is(error) bool` method that reports true; it compares with `==` only when `target`'s dynamic type is
comparable. `errors.As` is generic, `func As[E any](err error, target *E) bool`, so the Go spelling
`errors.As(err, &perr)` works without reflection, with `E` inferred from `target`.
`errors.AsType[E error](err) (E, bool)` is Go 1.26's form.

### 14.5 Differences in the standard library

`os.File.Read`, `Write` and `Close` report a bare `syscall.Errno` (and `io.EOF`), not a `*PathError`, so
that I/O does not allocate. `strconv` reports `ErrSyntax` and `ErrRange` without Go's `*NumError`. An
`Errno` the table does not know reports `"unknown error"`.

### 14.6 Allocated errors and ownership (proposal, needs sign-off)

Section 6.6 requires every allocation to have an owner or an arena. An allocated error as section 14.3
describes it has neither when the default allocator is the general heap: `error` is an ordinary interface,
so it only looks at the block, and the block is freed only if some code calls `errors.Free` exactly once,
on the outermost error. A missed call leaks; a second call, or a call on an inner error that a wrapper
also frees, is a double free. This proposal makes the release automatic and exactly once, without changing
how error handling reads.

**Proposal: allocated errors are counted.**

1. `fmt.Errorf`, `errors.Join` and `os.NewPathError` allocate their error as a `synced[T]` object (the
   header records the allocator and the count) and return it in an `error`.
2. An interface value of type `error`, or of an interface type that embeds `error`, counts a `shared` or
   `synced` reference it holds: copying the interface increments the count, and dropping or overwriting
   it decrements. At zero the error's `Drop` runs, which drops the errors it wraps, and the block is freed
   to its allocator, which does nothing in an arena. Converting an `error` to `any` or another interface
   still lends it (section 4.4).
3. Every other error (codes, sentinels, errors stored in an owner such as `fmt.ErrorBuf`) copies as in Go.
   The test is one bit in the method table, made only when the error is not nil, so code whose errors are
   codes, as in a kernel, pays one predictable branch on paths that copy or drop a non-nil error.
4. `%w` copies the wrapped error into the wrapper (count +1). A wrapper keeps what it wraps alive, which
   is what "a wrapper owns the errors it wraps" says today, and the caller's own copy is released when it
   goes out of scope.
5. `errors.Free` and the `Free()` methods of error types are removed. Arenas keep working: errors made
   under `alloc.Use(&a)` free into the arena, and `a.Reset()` reclaims them, as now.
6. A program's own error type that needs heap storage is allocated with `alloc.Synced[MyErr]()` and
   returned as an `error`; one that is an owner and is converted to `error` directly is only lent, and
   returning it is the compile error of section 6.9.
7. The count is atomic, so allocated errors may be sent between puroutines.

Code does not change: `if err != nil { return err }` moves at the last use with no count update, and
`errors.Is(err, os.ErrNotExist)` lends `err`.

**Alternatives considered.**

- **`error` as an owning interface** (`error` would move). No run-time cost, but `return io.EOF` would
  move out of a package variable, `Unwrap() error` would move out of a field, and passing an error to a
  function would give it away; too far from Go for the most common code in every program.
- **Allocated errors only in an arena** (`fmt.Errorf` panics when the current allocator is not an arena).
  No run-time cost and no counting, but every program needs an arena around every call that can fail, and
  a simple hosted tool could not report a path without one.

**Recommendation: adopt counted errors.** They keep Go's error handling unchanged, release each allocated
error exactly once whether or not an arena is in use, and cost nothing for the code and sentinel errors
that kernels use. The accepted design (section 14.3) remains in force until this is signed off.
