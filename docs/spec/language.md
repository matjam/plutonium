# The Plutonium Language

**Version:** 0.2 (draft)
**Date:** 2026-10-10

Plutonium is a systems programming language for operating system kernels, drivers, boot code, allocators
and other code that runs without a managed runtime. It is the language PlutoniumOS is written in. It has
no garbage collector. Puroutines and channels run on a scheduler that only programs using them carry, and
that a kernel supplies itself.

Four rules shape the language:

- **Go-familiar, not Go-compatible.** Plutonium uses Go's syntax and idioms wherever they fit, so a Go
  programmer can read Plutonium code without a manual. Compiling Go source unchanged is not a goal. Where
  Go's meaning depends on a garbage collector, a growing stack or hidden allocation, Plutonium changes the
  language, and Go code may need edits before it compiles.
- **No false friends.** Where Plutonium behaves differently from Go in a way a Go programmer would not
  expect, the difference is visible in the source: a different name, a different syntax, a different
  type, or a compile error that explains it. Code that looks like Go and compiles does what Go would do.
  Where the language does not meet this rule yet, the gap and its planned fix are listed in
  [the false-friends audit](../plans/2026-10-10-go-false-friends.md).
- **No annotations for ordinary code.** Ordinary code needs no annotations and no knowledge of how the
  compiler works. What the compiler can work out for itself (what escapes, what moves, which functions
  never return or may wait, which lock guards which data) it works out, and there is no `//pu:` directive
  for such a fact. Directives state only what the compiler cannot see: linkage to assembly and C,
  hardware state, bindings to the runtime, and explicit requests such as `//pu:inline` (section 9).
- **Go as a test oracle.** Programs in the common subset of Go and Plutonium are kept as tests:
  `internal/build/testdata/run` takes its expected output from Go, and `tools/difftest` runs generated
  programs under both compilers and compares the results. The subset is how the compiler is checked
  against Go's behavior. It is not a design constraint: a change that moves a program out of the subset
  is judged on its own merits, and the program is rewritten or moved to the Plutonium-only tests.

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

- Read like Go, and never mislead a Go reader: code that differs from Go looks different (the rules
  above).
- Compile like C: no garbage collector, no hidden allocation, no runtime the program did not ask for. A
  program that never starts a puroutine or makes a channel contains no scheduler.
- Make the costs visible. Every allocation is a call you can see; every memory-mapped register access is
  marked in its type.
- Make ownership visible. Every piece of memory has one owner, named by its type, and is freed when that
  owner is dropped; looking, moving and sharing are written differently (section 6).
- Give kernel code the tools it needs in the language: bit-exact layouts, volatile access, alignment
  control, ring buffers, and a fatal-error path that works before any allocator exists.
- Generate fast code. The compiler has its own optimising backend and is measured against Rust.

### 1.1 Design principles

Four standing principles decide the cases the rules above leave open. They serve two audiences:
kernel code, where a run-time check may be unavailable or unaffordable, and application code, which
should get strong guarantees without writing annotations.

1. **Doubt resolves to a compile error.** Every static analysis (escape, moves, sendable, lock guarding,
   closure regions) rejects what it cannot prove safe, and never accepts on doubt. A false positive is
   fixed by improving the analysis, not by weakening the rule. Why: applications get Rust-level guarantees
   without annotations, and kernels get them at compile time.

2. **Run-time checks change what a bug does, never what correct code means.** Generation and arena-epoch
   checks (a use of freed memory is a panic) are on by default for hosted builds once their cost is
   acceptable, and off for kernel and freestanding builds until a kernel tag encoding exists. A build
   without them is a documented unchecked mode, not a dialect: correct programs behave identically under
   both. Why: a program that is correct must not depend on whether the checks are present, so each
   audience can choose its build without rewriting code.

3. **Mechanism in the language, policy in the kernel.** The runtime provides mechanisms with hooks, and the
   kernel supplies policy. The runtime owns the heap, and the kernel supplies `Now` and `ArmTimer` for
   timers. The scheduler contract and the panic handler are hooks. New tasks start with interrupts
   enabled, so the kernel installs its table first. Allocators are chosen per call. Application code
   never sees the hooks and needs no directives; kernel directives exist only for facts about hardware the
   compiler cannot see. Why: a kernel must decide for itself how time, scheduling and failure work, while
   an application should not have to know that these decisions exist.

4. **Bounded checks fail loudly, never silently.** Where a check has a finite range (slab generations and
   arena epochs, which use one program-wide counter so that a reused chunk cannot match a stale pointer),
   reaching the end of the range retires the memory or widens the field. It never lets a stale pointer
   pass. The hosted 16-bit encoding accepts a wrap after 32,768 epochs, as documented in the ABI
   (abi.md section 1.3). The kernel encoding must retire on wrap. Why: a check that can start passing
   stale pointers by accident gives a false assurance, which is worse for a kernel than no check.

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
| `recover` | needs an unwinder that resumes a frame; a panic never returns to the code that panicked (section 8) | return errors (section 14) |
| `import "reflect"` | needs runtime type metadata | type switches, generics |
| `complex64`, `complex128`, `complex`, `real`, `imag` | rarely useful in systems code; keeps the backend small | a struct of two floats |
| `append` | Go's `append` grows a slice by allocating a larger array behind the program's back, and the collector frees the old one; a slice is a view and owns no array it could grow (sections 4.3, 6.3) | `pluto/vec.Vec[T]`, an owner that grows (`Push`, `Append`); `vec.Fixed[T]` to fill a caller's array (section 13.2); slicing and `copy` within a slice's capacity |
| `make([]T, n)`, `make([]T, n, c)` | the array would belong to no one: a slice is a view, and without a collector nothing would free it (sections 4.2, 6.6) | `vec.Make[T](n)` and `vec.WithCapacity[T](c)`, which return an owning `vec.Vec[T]`; an array for a constant size; `a.Slice[T](n)` from an arena |
| string literals that are not valid UTF-8 (`"\xff"`) | every string holds valid UTF-8 (section 6.10) | `[]byte{0xff}` for binary data |
| `string(i)` for an integer `i` of a type other than `rune`, such as `int` or `byte` (E2017; an untyped constant such as `string(65)` stays) | Go makes the character with code `i`, so `string(65)` is `"A"` where programs expect `"65"`; `go vet` reports it. Plutonium makes the choice visible in the source (section 6.10) | `string(rune(i))` for the character; `strconv.Itoa(i)`, `FormatInt` or `FormatUint` for the digits |
| `[]byte(s)` and `[]rune(s)` for a string `s` (E2016) | Go allocates the copy and the collector frees it; here a slice is a view and the copy would belong to no one (sections 4.2, 6.6) | `strings.Bytes(s)` and `strings.Runes(s)`, which return an owning `vec.Vec`; `copy(buf[:], s)` into a buffer of your own; `for i, r := range s`; `utf8.RuneCountInString(s)` to count |
| `string(b)` for a byte slice `b` (E2015) | Go keeps the bytes; a Plutonium string holds only valid UTF-8, so the conversion would replace each invalid byte with U+FFFD without saying so, and `string([]byte{0xfe}) == string([]byte{0xff})` (section 6.10) | `strings.FromUTF8(b)`, which returns an error for bytes that are not text; `strings.FromUTF8Lossy(b)`, which replaces them by name; a byte array or slice for binary data |

Strings work as in Go otherwise. Runtime concatenation (`s + t`, `s += t`) and the conversions
`string(runes)` and `string(r)` allocate from the current
allocator, and the strings they make are counted and freed when their last copy goes (section 6.10).
In kernel mode they follow the rules of any allocation (section 12.7).

## 4. What Plutonium changes

### 4.1 `panic` aborts

`panic(v)` stops the program. It does not unwind the stack: control never returns to a function that
panicked, so there is no `recover`, and owners are not dropped (section 6.4). In hosted builds the
panicking task first runs its pending deferred calls, innermost frame first, as Go runs them for a panic
that nothing recovers; other tasks are not affected (section 8.2). Then the runtime calls the program's
panic handler, which never returns. Freestanding and kernel builds run no deferred calls: their panic
handler must work from whatever state the panic left. See [section 8](#8-runtime-faults-and-panic).

`rt.Goexit` is the controlled way to leave a task early: it runs the deferred calls of every frame and
drops what each holds (section 8.3).

### 4.2 `new` and `make` allocate from an allocator

`new(T)`, `new(v)` and `make(chan T, n)` allocate from the current allocator (section 7). Each accepts an
optional final allocator argument:

```go
n := new(Node)                  // current allocator
m := new(Node, arena)           // explicit allocator
k := new(Node{val: 1}, arena)   // a new variable holding a value
c := make(chan Msg, 16, arena)  // a channel and its buffer
```

`make` of a slice type is a compile error (E2014): the array it allocates would belong to no one, since a
slice is a view and nothing collects what no slice refers to. `vec.Make[T](n)` returns a `vec.Vec[T]` of
`n` zero elements that owns its array, `vec.WithCapacity[T](c)` an empty one with room for `c`, each with
the same optional allocator, and `v.Slice()` lends the elements as a slice:

```go
buf := vec.Make[byte](4096)          // Go's make([]byte, 4096)
defer buf.Free()
big := vec.WithCapacity[byte](4096)  // Go's make([]byte, 0, 4096)
tmp := vec.Make[byte](64, scratch)   // from an allocator
var small [64]byte                   // a constant size needs no allocation
```

As in Go 1.26, `new(v)` allocates a variable of `v`'s type (an untyped constant takes its default type) and
stores `v` in it; `new(T{...})` is how a composite literal outlives its frame. Whether the first operand is a
type decides between `new(T, a)` and `new(v, a)`; `v` is evaluated before the allocator. The allocator is
any value assignable to `alloc.Allocator`, such as `&arena`, and the checker tells it from a capacity by
its type: a final argument of `make` is a size if it is an integer or untyped numeric constant and an
allocator otherwise. Memory from `new(T)` and `make` is zeroed. With no allocator argument and no default allocator set, as in a freestanding program
before `alloc.SetDefault`, the allocation panics with "no allocator" (section 7.2); a nil allocator
argument panics too.

**What they return.** Every allocation needs an owner or an arena (section 6.6), so `new` and `make`
return owners:

- `new(T)` and `new(v)` return an `owned[T]` (section 5.6): the single owner of the new variable, which
  frees it when dropped. It dereferences like `*T`, so `n := new(Node); n.val = 1` reads as in Go, but it
  is not a `*T`: storing it in a `*T` variable, returning it as a `*T` or passing it where a `*T` is
  expected is a compile error, so Go code that keeps the pointer of a `new` shows where the owner must
  go. `(*T)(n)` and `&*n` lend a `*T` view of the variable (section 6.5, rule 5). `new(v)` with an owner
  `v` moves it into the new variable.
- slices come from `vec.Make` and `vec.WithCapacity` (above), which return the owner, a `vec.Vec[T]`;
  `v[i]` and `len(v)` work on it directly through the indexing interface (section 6.7).
- `make(chan T)` and `make(chan T, n)` return a channel, which counts its references (section 4.8).

Memory that only an arena should own is allocated through the arena, which returns views: `a.New[T]()`
gives a `*T` and `a.Slice[T](n)` a `[]T` that live until the arena is reset or dropped. `alloc.New[T]()`
returns a `*T` that nothing owns; it belongs to the unchecked layer that owner types are written with
(section 6.6).

### 4.3 Slices do not grow

A slice is a view (section 6.3): the array behind it belongs to someone else, so a slice cannot grow, and
Plutonium has no `append`. Every call of `append` is a compile error (E2013) whose fix is the form of the
idiom at hand:

| Go | Plutonium | Allocates |
|---|---|---|
| `v = append(v, x)`, `v = append(v, xs...)` | `v.Push(x)`, `v.Append(xs...)` on a `vec.Vec[T]` | when it grows |
| `b = append(b, s...)` with a string `s` | `vec.AppendString(&b, s)` | when `b` is a `vec.Vec` that grows |
| `append([]T(nil), s...)` | `slices.Clone(s)`, a `vec.Vec[T]` | yes, an owner |
| `c := append(a, b...)` as a new sequence | `slices.Concat(a, b)`, a `vec.Vec[T]` | yes, an owner |
| `s = append(s[:i], s[j:]...)` | `s = slices.Delete(s, i, j)` | no |
| `slices.Insert`, `slices.Replace`, `slices.AppendSeq` | `v.Insert`, `v.Replace`, `v.AppendSeq` on a `vec.Vec` | when it grows |
| `append(buf[:0], ...)` into an array the code owns | `vec.NewFixed(buf[:])`, then `Push` and `Append` | no |
| `s = append(s, x)` within a capacity the code chose | `s = s[:len(s)+1]; s[len(s)-1] = x` | no |

Everything else slices do in Go they do here: indexing, slicing (`s[:len(s)+1]` within the capacity),
`copy`, `clear`, `range`, and the `slices` functions that shrink or reorder in place. `append` stays a
predeclared name, so that a call names it in the error, and a program may declare its own. The library's
`Append` functions (`strconv.AppendInt`, `fmt.Appendf` and the rest) keep their names and append to a
`vec.Vec[byte]` or a `vec.Fixed[byte]` (section 13.2).

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
  valid until that function returns, exactly like a pointer to a local variable. A copy that holds
  strings holds references of its own (section 6.10), released when the function returns or when the
  same conversion runs again and refills it; in a package-level initializer the copy is static and keeps
  its references for ever.

An interface that holds such a frame copy is a view of the frame (section 6.3). Returning it, storing it in
a package-level variable or on the heap, or passing it to a function that keeps it, is a compile error. To
keep a large value in an interface beyond the current function, store a pointer to memory that lives long
enough instead, or move an owner into an owning interface (below).

The initializer of a package-level variable is the exception: it runs once, and its results must outlive
package initialization, so a conversion there copies the value into static storage instead of a frame.
`var ErrBad error = parseError{line: 1}` and `var names any = []string{"a", "b"}` are valid. The same
holds for `&T{...}`, slice literals and the array behind a variadic call's arguments in package-level
initializers, which also get static storage.

**Owners in interfaces.** An interface value has no static owner kind of its own, so the kind comes from
the interface type:

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
- An owning interface drops what it holds through its method table: an owning interface's itab has one
  more slot after the methods, the dynamic type's drop code (abi.md section 5), so dropping the interface
  runs the right `Drop` whatever the type. An owning interface with no methods, `type R owner
  interface{}`, has no itab, so the slot is a word of the dynamic type's type descriptor instead; it
  holds and drops any owner, `shared`, `synced` or `weak` reference, or plain value, as `any` holds any value.
- A method of the dynamic type with a value receiver never satisfies an interface method when the type is
  an owner (section 6.4): calling it would consume the value, and the interface, ordinary or owning,
  cannot give it up. The conversion is a compile error that shows the fix, a pointer receiver. A
  pointer-receiver method of an owner is reached through `&b` (an ordinary interface) or through a box
  (`new(b)` in an owning interface), as in Go: `fmt.Println(b)` prints the fields and
  `fmt.Println(&b)` calls `b`'s `String` method.

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
variables stay in a frame; nothing is moved to the heap. A capturing closure, like a method value, is a
**closure record**: the code to run, with where each captured variable is (or a copy of the receiver),
built in the frame of the function that makes it. A closure is therefore valid while that frame is:

- Calling it, passing it as an argument, assigning it to a local variable, and deferring it are allowed.
- Returning it is allowed: the closure is then built in a region its caller provides, below.
- Storing it in a package-level variable, or through a pointer into memory that may outlive the frame, is
  a compile error (section 6.3).

A function literal that captures nothing is an ordinary function value and may be used anywhere.

**Loop variables.** Go gives each iteration of a loop its own copies of the variables the loop declares,
so a closure made in one iteration keeps seeing that iteration's values. Plutonium cannot: the copies would
need memory for every iteration, and closures refer to variables where they live, in the frame, which has
room for one copy of each. Every iteration of a loop reuses the same memory for the variables declared in
its header (`for i := ...`, `for k, v := range ...`) and its body, and for the temporaries its body makes.
A view of that memory (a closure that uses one of those variables, `&x`, a slice of a local array) is
therefore valid for one iteration:

- Using it within the iteration (calling the closure, passing the pointer down, storing it in a variable
  declared in the body) is allowed, and behaves exactly as in Go.
- Keeping it where the next iteration can see it is a compile error (section 6.9): in a variable, array
  element or field declared outside the loop body or in the loop's header, or anywhere a view of the frame
  may not go at all (returned, sent, stored on the heap). A local variable whose address is never taken is
  checked where the next iteration starts, so keeping the view and then leaving the loop with `break` or
  `return` is allowed.
- A closure that uses only variables declared outside the loop holds the same addresses in every
  iteration and may be kept.
- Copying the variable into one declared in the body (`i := i`) does not help, since that copy is reused
  too. Call the closure within the iteration, store values instead of closures or pointers, or pass the
  value to the closure as a parameter when it is called.

```go
var first func() int
for i := 0; i < 3; i++ {
	f := func() int { return i * 10 }
	println(f())   // allowed: used within the iteration
	if i == 0 {
		first = f  // compile error: the next iteration reuses i
	}
}
```

**Returned closures.** A function whose results can hold a closure (a func result, an interface, or a
struct or array holding one) builds the closures it may return in a **region** of memory its caller
provides: their records, the variables they capture, and the regions of the calls whose closures it
returns in turn. The caller reserves the region in its own frame, like a local variable, so the closure
lives as long as the caller's frame, captures by reference exactly as in Go, and costs no allocation. A
caller that returns the closure in turn passes part of its own region down instead, so a chain of such
calls reserves the space once, in the frame where the closure is last used. Go's iterators are the common
case, written as in Go, with whatever checks and early returns they need:

```go
func Chunk[Slice ~[]E, E any](s Slice, n int) iter.Seq[Slice] {
	if n < 1 {
		panic("cannot be less than 1")
	}
	return func(yield func(Slice) bool) {
		for i := 0; i < len(s); i += n {
			end := min(n, len(s[i:]))
			if !yield(s[i : i+end : i+end]) {
				return
			}
		}
	}
}
```

The region holds only those. A returned closure that points to another variable of the function, through
a pointer it captures (`p := &x` where the closure uses `p` but not `x`), would point into a frame that is
gone, and is a compile error (section 6.3). What calling the closure returns, or passes to `yield`, may be
in the region too (a closure built there, or the address of a variable it captures), and is valid as long
as the caller's frame, like the closure itself.

The size of a region is fixed when compiling. A call through a func value or an interface reserves the
largest region of any function of its signature. Such a function is an ordinary function: it can be used
as a func value, a method value or an interface method, and is inlined when it is small, so that
`for i, x := range v.All()` compiles to the loop `All` runs. The closure's code is compiled once, and a stack
trace names its frame after the function that returns it (`main.Count.func1`,
`pluto/vec.(*Vec).All[int].func1`) wherever it was called. The region's place in the calling convention
is in abi.md section 4.1. Strings the closures capture are counted as in any variable (section 6.10): the
region holds references to them, released when the call runs again and refills the region, and when
the caller returns.

The compiler follows closures through variables, fields, elements, interfaces, pointers and calls, without
regard to order, and reports what a region cannot hold:

- A function that returns a closure made by a function literal nested in it, or in the body of a
  range-over-func loop, which is a function of its own whose frame is gone when it returns (E3100); and
  a function whose results hold a closure only through a pointer, for which its callers provide no region
  (E3100). Make the closure in the function that returns it, and return it by value.
- A closure that a call in a loop returns, kept past the iteration in a variable declared outside the loop,
  in a variable of the `for` statement (which passes its value on to the next iteration), or in memory
  reached through a pointer (E3101): every iteration's call reuses the call's region, so the next
  iteration would overwrite it. A loop made by a `goto` back to a label before the call counts too. This
  rules out composing closures in a loop, `it = Filter(it, f)`: each call's closure would hold the one
  the same call made before, in the region the call is about to refill, a chain as long as the loop runs
  that no region of fixed size can hold. Compose a fixed number of them, or keep what each step needs in a
  slice and return one closure that applies every step. A function literal in a loop is built the same
  way, one record per literal, reused on every iteration. Composing closures within the iteration, and
  passing them to functions that call or range over them, is allowed: the compiler follows what each
  function does with the closures it is given, and what those closures do when called. A closure is kept
  if any of them stores it, or what it holds, in a package-level variable or through a pointer, or passes
  it to a func value or an interface.
- A function that returns the closures of a call that can reach the same function again, directly, through
  other functions, or through a func value or interface whose implementations do, when any of them builds
  a closure: its region would have to hold itself, and no fixed size fits (E3102). Build the closures in a
  loop, or keep their state in a value the caller owns.

A call through a func value or an interface is assumed to return and keep anything passed to it, so the
check may reject a function that would never return or keep the record at run time. Storing a record in
a package-level variable or through a pointer is not checked yet (roadmap item 5).

Since no closure a call builds in its region reaches the call's next run, the region can be released and
refilled when the call runs again: the arguments of that run cannot refer to it. The address of a
variable in a region (a variable that a returned closure captures, whose address the function also
returns or stores) is a view of the caller's frame at that call, like a loop variable's: it must not be
kept where the call's next run can see it, which is checked with the other frame views (roadmap item 5).

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
func process(items []Item) vec.Vec[int] {
	sizes := vec.New[int]()
	each(items, func(it *Item) {
		sizes.Push(it.size) // uses the captured owner: allowed
	})
	return sizes           // moves it to the caller after the literal is done
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

- **A method of a box.** `go p.Serve()`, where `Serve` has a pointer receiver and `p` is an `owned[T]` or a
  `synced[T]`, hands the box itself to the new puroutine, so the object lives as long as the puroutine needs
  it and no view of the spawning frame is involved. An `owned[T]` moves into the puroutine, as an owner
  argument does: `p` holds nothing afterwards (using it is the use-after-move error of section 6.4), and the
  puroutine drops the box when `Serve` returns, or when `Serve` ends it with `rt.Goexit`. The receiver must be
  a whole variable or a temporary, since an owner moves only out of a whole variable (`go newServer().Serve()`
  is fine, `go s.box.Serve()` is not). A `synced[T]` is copied as an argument is: the new puroutine holds a
  reference of its own, taken before it starts and released when the method returns, so the object is dropped
  by whichever puroutine releases the last one and a field or element may be the receiver. A `shared[T]` is
  not sendable (below): `go z.Serve()` is a compile error, as is a box whose `T` contains one. The
  method must be one of `T`, not one promoted from a field, and the box type must not be a named type;
  other receivers are pointers into the object, which the escape analysis (section 6.3) treats as views of the
  frame. A method that keeps its receiver (stores it somewhere that outlives the puroutine) is rejected as
  before. Starting a method value, `go f()` with `f := p.Serve`, remains an error, since the closure record
  would live in the starting frame (below); so does passing it to `rt.Go`, `sync.WaitGroup.Go` or
  `time.AfterFunc`.

- Each puroutine has a fixed-size stack: stacks cannot grow or move, because nothing knows where the
  pointers into them are. A guard region below the stack turns an overflow into a fault, reported as a
  stack overflow with the puroutine's trace. In hosted builds a puroutine started with `go` has at least
  64 MiB of stack (section 12.1): address space reserved for it, committed only as the stack grows into
  it, so a puroutine that stays shallow uses a page or two of memory and one that recurses deeply works
  as a goroutine would. Where the scheduler cannot reserve address space (kernels, and programs that
  bind their own scheduler) the default is 64 KiB (`rt.DefaultStackSize`). `rt.Go(f, size)` starts a
  puroutine with an explicit stack size instead (at least `rt.MinStackSize`, 8 KiB). For a spawned
  function whose call graph has no recursion and no indirect calls, the compiler is to compute the stack
  it needs and use that instead, and `-stackreport` to list the functions it could not size (planned,
  roadmap item 18; today every puroutine gets the default or `rt.Go`'s size).
- A function literal that captures variables cannot be started with `go`: its captures live in the
  spawning frame, which may be gone before the puroutine runs (section 4.5). Pass the values as
  arguments: `go func(n int) { work(n) }(n)`. The same holds for a method value `x.M`, whose closure
  record holds a copy of the receiver in the frame that made it. Wherever the compiler can tell that a
  `go` statement's function, or one of its func-typed arguments, is such a closure, it is a compile
  error: a capturing literal or a method value written in the `go` statement, or a local variable whose
  only assignment is its declaration from one of them (directly or through other such variables). Any
  other func value whose closure record lies in the starting puroutine's stack (one that reached the
  `go` statement through a parameter, a field or a variable assigned more than once) panics when the
  `go` statement runs, and so does `rt.Go` with one. A call of a function that starts a puroutine with
  one of its func arguments (`sync.WaitGroup.Go`, `time.AfterFunc`, `rt.Go`, or a function of the
  program's own that passes a func parameter on to a `go` statement or to one of these) is checked the
  same way: the compiler rejects an argument it can tell is such a closure, and the function panics for
  the rest. The compiler finds these functions itself by following each func parameter through the
  function's body.
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
another worker. Waits for I/O go through one epoll poller, on Go's design: a non-blocking descriptor
(`os.Pipe`'s, or one given to `os.NewFile`, such as a socket made with `SOCK_NONBLOCK`) has a poller
record (`rt.PollDesc`) with its own queue of puroutines waiting to read and another of those waiting to
write, so a read and a write can wait on one socket at once, and an event wakes everything waiting for
it. A puroutine that reads an empty pipe or writes a full one parks there. `Close` on the `os.File`
wakes every waiter, whose `Read` or `Write` then reports `os.ErrClosed`, and so does any later use of the
file; the descriptor itself is closed when the last operation in progress on it returns, so that the
number is not reused under a running call. When every puroutine waits for another and nothing waits
for I/O, a system call or an armed timer, the program stops with "fatal error: all puroutines are
asleep - deadlock!" and status 2, as in Go, and lists each puroutine with what it waits for and its
stack (`puroutine 3 [chan send]:` and the frames, debugging.md section 5; `rt.TraceTask`).

A hosted program catches `SIGPIPE` at startup, with a handler that does nothing (so a program it starts
with `os/exec` gets the default action back), and follows Go's rule: a write to a pipe or socket with no
reader fails with `EPIPE`, except that a write to standard output or standard error (descriptors 1 and 2)
that fails with `EPIPE` ends the program as if by the signal, so that `prog | head` stops `prog` as it
would a C program (`rt.DieFromSigpipe`).

A kernel implements the backend over its own per-CPU run queues and calls `rt.Preempt()` from its timer
interrupt handler; the runtime keeps the current task in a per-CPU slot and masks interrupts while it
holds its own locks, so interrupt handlers may make non-blocking channel operations and call
`rt.Ready`. `testdata/kernel/sched` is such a kernel, booted under QEMU by the compiler's tests.

**Timers.** `time.Sleep` parks the puroutine until a deadline, so its processor runs other puroutines
and a program whose puroutines all sleep uses no processor time. A program that never starts the
scheduler (no `go` statement, no channel operation or `pluto/sync` wait that can block, no timer,
`time.After`, `AfterFunc` or `rt.ParkUntil`) has no other puroutine to run, so there `Sleep` waits in
the kernel on the program's only thread (`clock_nanosleep`), and the scheduler, monitor and poller
threads are neither started nor linked. The compiler decides this once it has lowered the whole
program, from whether the runtime's `startTasks` is reachable (`rt.Sleep`, a body-less function the
compiler knows by name, takes the direct body or the parking one then); the default hosted backend is the
only one it applies to, so a program that binds its own scheduler, a kernel included, always parks.
`time.Timer`, `time.Ticker`,
`time.After` and `time.Tick` deliver the time on a channel, so a `select` waits for a timer like any
other case (section 4.8), and `time.AfterFunc` starts a function on a puroutine of its own when its
timer expires. Underneath is the runtime's mechanism: a monotonic clock, `rt.Now()`; timers,
`rt.Timer`, which call a function when their deadline passes and never allocate; and `rt.ParkUntil`,
`rt.Park` with a deadline (abi.md section 12.9). The hosted backend keeps the timers of each processor
in a heap of its own, run by the workers between puroutines and, while they sleep, by the poller. A
kernel's backend provides the clock and the hardware: a `Now` method, and an `ArmTimer` method that
arranges for `rt.RunTimers()` to be called, typically from the timer interrupt, once a deadline has
passed. Both are optional, so a kernel that never sleeps needs neither; a program that uses timers with
a backend that lacks them fails to build. Arming and stopping a timer, and `rt.RunTimers`, never block
and may be used in interrupt handlers; sleeping and `rt.ParkUntil` park the caller, so interrupt handlers
must not reach them (section 12.6).

The `time` API follows Go's where the language allows. A timer's channel owns its timer: the channel
of `time.After`, `time.Tick`, a `Timer` or a `Ticker` stops and frees its timer when the last reference
to the channel goes (section 4.8), where Go 1.23 collects a timer nothing refers to. So
`time.After` in a `select` loop frees each timer when its `select` ends, and a timer keeps firing while
anything holds its channel. `Timer` and `Ticker` objects come from an allocator (`time.NewTimer(d, a)`,
with the default allocator if none is given) and are freed with `alloc.Free`, which releases their
reference to the channel and stops an `AfterFunc` timer. As in Go 1.23, a timer's channel holds no stale
value once `Stop` or `Reset` has returned. `time.AfterFunc(d, f)` cannot run `f` where timers fire (an
interrupt handler, in a kernel), so expired timers are queued for one puroutine of `pluto/time`'s,
which starts each function with a `go` statement; like a function passed to `go`, `f` must not capture
variables, and `AfterFunc` panics if its closure record lies in the caller's stack.

**The sendable rule.** Values that cross between puroutines, as `go` arguments or channel elements, must
be safe to use from several processors at once. A type is **sendable** unless it contains, directly or in
any field or element:

- a `shared[T]` or a `weak[shared[T]]`, whose counts are not atomic (section 6.5);
- a view of the sending function's frame, which section 6.3 already forbids passing on.

- a view (`*T`, `[]T`) of heap or static memory that may change while both puroutines can use it
  (section 6.11).

Values, owners, `synced[T]` and `weak[synced[T]]` (with a sendable `T`) and channels are sendable. Moving
an owner to another puroutine is safe because the sender loses it: only one puroutine can use it, and a
library owner that manages its memory (`vec.Vec`, `hash.Map`, `strings.Builder`) takes that memory with
it, so only its elements are checked. A `synced[T]` lets several puroutines reach the same `T`, so fields
of `T` that change are guarded by a lock in the same object or used atomically (section 6.11); the count
is atomic, the data is not. Strings are sendable: their bytes never change and their counts are atomic
once a program has several threads (section 6.10).

A view may cross only when what it points to cannot change while both puroutines may use it, or every
change is synchronised: memory of a `pluto/sync` or `pluto/sync/atomic` type (or a channel, or a struct
of only those), a package variable that no puroutine writes once the program's puroutines run, the
object of a `synced[T]`, or a struct type whose objects are safe by the type rule of section 6.11 (no
field changes once an object is shared, or each field that changes is used only under one lock of the
same object or atomically). Anything else, such as a slice of a `[]int` that `main` still fills, is
error E3501, which names a write that could change it. The fixes are to pass a copy, to move an owner,
or to share through a `synced[T]` or a package variable guarded by a lock.

The type checker applies the type part of the rule (`shared[T]`) to the arguments and receiver of every
`go` statement and to the element type of every channel type, looking through struct fields, array
elements and the referents of `owned`, `synced` and the transparent wrappers (`ring`, `volatile`,
`aligned`). The escape analysis rejects views of the frame (E3004, E3005, section 6.3). The race
analysis (section 6.11) checks views of other memory where they cross: the arguments and receiver of a
`go` statement, values sent on a channel, and, through interface values, every type the program converts
to that interface. Packed variadic arguments of a `go` statement are copied into the new puroutine, so
each one crosses as a value. Passing a local `sync.WaitGroup` by address, the usual Go idiom, is an error
(E3004): use a package-level variable, or the `sync.Scope` of scoped puroutines once it lands. In a
generic function the rule is checked for each instantiation.

The large stacks of hosted puroutines are reserved from the operating system, not an allocator. Other
stacks come from the default allocator (the starting puroutine's, if it installed one with
`alloc.Use`), or from `rt.Go(f, size, a)` with an explicit one. Stacks are freed when the puroutine
returns. They belong to the runtime: the debug allocator does not count a stack still in use at exit as a
leak.

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
`select` with several ready cases picks one at random, as in Go. A puroutine blocked on a channel is
reported in a deadlock as `chan receive`, `chan send`, `select`, `chan receive (nil chan)`, `chan send
(nil chan)` or `select (no cases)` (section 4.7).

A channel value is a reference counted like `synced[T]` (section 6.5), since a channel always has
several users: copying it counts a reference, and when the last reference goes away the channel and its
buffer are freed to the allocator they came from (nothing, for an arena). Values still buffered at that
point are dropped first, so owners in the buffer are dropped and strings and counted references in it
released rather than leaked; their `Drop` runs on the puroutine that released the last reference. The
counts follow the rules of counted strings (section 6.10): a variable, field, element or buffer slot
holding a channel holds one reference, parameters are borrowed and results owned, and the compiler
leaves out the counting where no reference can go away meanwhile. They are plain until the program
starts its first puroutine and atomic after. A `go` statement gives the new puroutine its own reference
to each channel among its arguments, and sending a channel on a channel gives the receiver the
sender's. A puroutine waiting on a channel holds a reference to it, so a channel is never freed under a
waiter; a puroutine blocked for good on a channel nothing else refers to keeps it, as a goroutine does in
Go. Nothing frees a channel explicitly; `close` only marks it closed.

The operations that may wait (a send or receive, a `range` over a channel, a `select` without a
`default` case) call runtime functions that park the caller; a `select` with a `default` case and
`close`, `len` and `cap` never wait, so interrupt handlers may use them (section 12.6).

Timeouts are channels too: `time.After(d)` and a `time.Timer`'s `C` receive the time once the timer
expires, so a `select` that waits for a value or a deadline has a case for each (section 4.7).

```go
for {
	select {
	case v := <-results:
		use(v)
	case <-time.After(100 * time.Millisecond):
		println("timed out")
	}
}
```

A timer's channel owns the timer: the channel `time.After` returns is freed, and its timer stopped, when
the `select` that waited on it ends, so the loop above holds one timer at a time.

The element type must be sendable, and a view sent on a channel must point to memory that cannot change
while both puroutines may use it (section 4.7). Sending an owner moves it: after `ch <- b`, `b` holds
nothing, and the receiver of `v := <-ch` owns it. In a `select`, the value of a send case is evaluated on
entry as in Go, but an owner is moved only if that case proceeds: inside the other clauses the variable
still holds its value, and after the `select` it counts as possibly moved (section 6.4).

```go
func produce(out chan<- vec.Vec[int]) {
	for i := 0; i < 3; i++ {
		v := vec.New[int]()
		v.Push(i)
		out <- v              // moves v to whoever receives it
	}
	close(out)
}
```

### 4.9 Memory model

The memory model is Go's, specified with Plutonium's changes in [memory-model.md](memory-model.md). In
brief:

- `go` statements, channel operations, `pluto/sync` locks, `WaitGroup` and `Once`, and
  `pluto/sync/atomic` operations synchronise exactly as in Go. Atomics are sequentially consistent.
- Safe code cannot race: the compiler rejects data races (section 6.11). In unsafe code a data race is
  a bug but not undefined behaviour. A racy read of a naturally aligned value of at most a
  word observes some value that was written to it; larger values (strings, slices, interfaces, structs)
  may tear. The compiler may not assume that a program has no races.
- A packed value is a single memory location: writing one field rewrites its neighbours, so two
  puroutines writing different fields of one packed value race.
- `volatile[T]` accesses are never removed, merged or reordered with one another, and atomic operations
  are compiler barriers. Volatile accesses do not synchronise between processors, but they are enough
  for an interrupt handler and the code it interrupts on the same processor to share data.
- Any scheduler, including a kernel's own (section 4.7), must make starting a puroutine and readying a
  parked one synchronising operations, so that these rules hold however puroutines are scheduled.
- Moving an owner is an ordinary write; the `go` statement or channel send that hands it to another
  puroutine is the synchronising operation. Every use of a `synced[T]` through any reference happens
  before its `Drop` runs and its memory is freed.

### 4.10 Range over functions

`for k, v := range f`, where `f` is a function `func(yield func(K, V) bool)` with zero, one or two
parameters to `yield`, works as in Go 1.23. `pluto/iter` defines `Seq[V]` and `Seq2[K, V]`, and the
library's containers have iterator methods (section 13); a range over a container itself ranges over its
`All` method (section 6.7). The loop body becomes a yield function, a
closure built in the enclosing frame and passed to `f`, which is how Go compiles the loop too:

- `f` may call `yield` as often as it likes while it runs, but may not keep it: `yield` is a closure of
  the enclosing frame (section 4.5), and storing it where it outlives the call of `f` is an escape that
  section 6.3 forbids. (The escape checks do not catch it yet.)
- `break`, `continue`, `return`, `goto` and labelled `break` and `continue` out of the body work as in
  Go: the body returns false, and once `f` returns the loop goes on at the target.
- An iterator that calls `yield` when it may not panics with Go's message: after `yield` returned false
  ("range function continued iteration after function for loop body returned false"), after the loop
  ended ("... after whole loop exit"), or while the body is running ("... after loop body panic", which
  Go reports for that state). A panic in the body stops the program (section 4.1), after the deferred
  calls of the iterator and of the function the loop is in have run (hosted builds); no iterator can
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

An iterator such as `items.All()` builds its closure in a region of the caller's frame (section 4.5), so a
loop over one allocates nothing. The optimiser inlines the iterator and then the loop body through their closures, so a
loop over a simple iterator compiles to the loop the iterator runs: ranging over `v.All()` costs the same
as ranging over `v.Slice()`.

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
| `All() iter.Seq2[int, T]` | an iterator over the index and value of each element, oldest first |
| `Values() iter.Seq[T]` | an iterator over the values, oldest first |

All methods have pointer receivers and are expanded inline. A ring is not safe for concurrent use; a
single-producer, single-consumer queue between an interrupt handler and a task can be built from
`volatile` indexes.

`All` and `Values` are iterators for `for` loops over functions (section 4.10), and work like those of
`vec.Vec` (section 13). A call of one builds its closure, which holds the address of the ring, in the
caller's frame; used as a method value or a method expression, or called through an interface, it builds
it in the region its caller provides, as any function that returns a closure does (section 4.5). Their
results have the type `func(yield func(int, T) bool)` and `func(yield func(T) bool)`, which are assignable
to `iter.Seq2[int, T]` and `iter.Seq[T]`.

```go
var r ring[int, 4]
r.Push(10)
r.Push(20)
for i, x := range r.All() { // 0 10, then 1 20
	fmt.Println(i, x)
}
for i, x := range r { ... } // the same: a ring ranges over its All method (section 6.7)
```

Since `All` has a pointer receiver, `range r` needs an addressable ring or a pointer to one.

The loop reads the ring as it goes, so the body may call `Push`, `Pop` and `Reset`. The loop keeps a position
among the elements ever pushed (the ring counts them, from the last `Reset`), starting at the oldest element
when the loop starts. On each step:

- If the body popped the element at the position, the loop moves on to the oldest element left.
- If the position has reached the end, nothing more has been pushed and the loop ends. Elements the body
  pushed are visited, as long as it finds room for them.
- Otherwise the loop yields the element, and the position moves on.

The index `All` yields is the element's place in the ring when it is yielded, so 0 is the oldest element
now: a body that pops each element as it is visited sees index 0 every time. A body that calls `Reset` should leave the loop: the position
then counts the pushes since the reset, so the loop ends, or goes on from where it was among the elements
pushed after the reset.

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

`owned[T]` is one owner among many: `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Builder` and `os.File` are owners
too, declared in the library with `owner` (section 6.4). It is the one to use for a single object on the
heap, such as a tree node or a large struct that must keep its address.

**Representation.** Each is one word, the address of the object (abi.md section 1.2). The allocation begins
with a hidden header in front of the object: for `owned[T]`, the allocator the object came from; for
`shared[T]` and `synced[T]`, the strong count, the weak count and the allocator. Freeing goes to the
recorded allocator even if the default has changed since; freeing into an arena does nothing (section
6.6). The zero value of each is nil; dropping a nil reference does nothing, and dereferencing one faults
like a nil pointer. They compare by address, with each other and with `nil`.

**Creation.** `new(T)` and `new(v)` return an `owned[T]` (section 4.2). `alloc.Owned[T]()`,
`alloc.Shared[T]()` and `alloc.Synced[T]()` allocate a zeroed `T`, each with an optional allocator
argument (section 7.1). A `weak[P]` is made by converting a reference: `weak[shared[Node]](p)`. A box
holds a concrete type: `owned[io.Reader]` is a compile error (E3210) whose fix is an owning interface
(section 4.4), which already owns a value behind an interface. A `*T` view never becomes a box; the
conversions out of a box are `(*T)(p)`, `&*p` and, as unsafe code, `unsafe.Pointer(p)`.

**Use.** `owned[T]`, `shared[T]` and `synced[T]` dereference like `*T`: `p.field`, `*p`, `p[i]`, `len(p)`
and `range p` when `T` is an array, and method calls with `*T`'s methods. Each of these gives a view of
the object, which may not outlive the reference it was made through (section 6.3): a view made through a
box held in a local variable is bounded by that variable, and one made through a temporary box by its
statement. A struct may embed one, `struct { owned[Style] }`, as Go embeds `*Style`, promoting `Style`'s
methods; a struct that embeds an `owned[T]` is an owner and must be declared `owner`. `&*p` and the conversion `(*T)(p)` produce a `*T` view to pass to code that
does not take ownership. A `weak[P]` does not dereference; its one method is

```go
func (w weak[P]) Upgrade() (P, bool)
```

which returns a new strong reference and `true` while the object is alive, and the zero `P` and `false`
once the last strong reference has been dropped (always `false` for the zero `weak`).

**Own methods.** Besides `*T`'s methods, `owned[T]` has a `Drop` (drop the `T`, then free the memory,
ending the variable's life as `x.Drop()` does for any owner) and, when `T` is not an owner, a `Clone()`
that returns a new box from the current allocator holding a copy of the object. For an owner `T` with
a `Clone` method of its own, write `new(p.Clone())`. These methods act on the box, never satisfy an
interface method, and are never promoted from an embedded box. `shared[T]` and `synced[T]` have no
methods of their own, so they never hide one of `T`'s; their counts are changed only by copying and
dropping.

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
whose payload is an owner (a type declared `owner`, or `owned[T]`) must be declared `owner union`, like
a struct with an owner field (section 6.4; E2406 otherwise):

```go
type Slot owner union {
	Empty
	Open File            // File is an owner
	Boxed owned[Node]
	Code int
}
```

An owner union moves instead of being copied. Dropping it drops the payload of the variant it holds,
and giving it a new variant drops the old payload first. A type switch with a binding on an owner union
variable (`switch v := s.(type)`) moves the payload into the binding of the clause that runs, which drops
it at the end of the clause unless it moves on; the variable is empty afterwards. The comma-ok assertion
`f, ok := s.(Slot.Open)` moves the payload out only where `ok` is true; where it is false `s` keeps it. A
union whose payloads hold counted references (`shared`, `synced`, `weak`, strings) and no owner copies,
counting each copy, as a struct of them does (section 6.2).

**Not yet.** Updating a variant's payload in place through a pointer (`(&u).(*U.V)`, proposal section 3)
waits for real use; assign a new variant instead. Debug information describes a union as its size only.

## 6. Ownership

Plutonium has no garbage collector, so every piece of memory a program uses has an owner that frees it,
and the owner is visible in the program's types. Every type is one of three kinds:

- **Values** are copied on assignment, exactly as in Go: integers, floats, booleans, enums, packed types,
  strings, arrays `[N]T` of values, and structs made only of values. A string is the one built-in value
  that holds memory: its copies share an immutable buffer and count it, and the last one to go frees it
  (section 6.10). The counting is automatic, so strings copy as freely as integers.
- **Views** look at memory that something else owns: `[]T` and `*T`, built into the language. A
  view never owns or frees anything. Creating, copying and passing views is free, and a view may not
  outlive what it looks at.
- **Owners** hold memory or another resource and release it when they are dropped. They are types
  declared with `owner`, such as `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Builder`, `os.File` and the
  built-in box `owned[T]`. Assigning an owner **moves** it to the new place; copies are made only by
  calling `Clone`.

When several parts of a program must keep the same object alive, the program says so with a counted
reference, `shared[T]` or `synced[T]`, and breaks cycles with `weak[...]`.

| You want to | Use | Cost |
|---|---|---|
| let a function use it for a while | view | free |
| give it to exactly one new owner | move | free |
| let several places keep it | shared/synced | count update |
| keep text | string | count update when a heap string is stored; none for literals |

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
does, eventually. Slices are the same: `t := s` shares the array, and Go's `append(s, v)` sometimes writes
into that shared array and sometimes copies it. Most Go programmers cannot say, for code like the above, what
is copied, what is shared and where it lives.

Plutonium separates the three jobs so that each type does one:

- a view only looks: `[]T`, `*T`;
- an owner only owns: `vec.Vec[T]`, `hash.Map[K, V]`, `strings.Builder`;
- a value is only bits, except a string, which is text that can be copied anywhere and is freed
  when nothing holds it (section 6.10).

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

A struct or array that contains a counted reference (`shared[T]`, `synced[T]`, `weak[P]`), a string or a
channel, and no owner, also copies; copying it updates each count, and dropping it releases each reference
(sections 6.5 and 6.10). It needs no declaration, since assignment still copies.

A `string` is a value of this kind. Its bytes never change, and they count their references: a string
literal is static and uncounted, a string made at run time (by `+`, a conversion from `[]byte`, or
`fmt.Sprintf`) owns its bytes with a count, copying a string increments it, a substring shares the
bytes, and dropping a string decrements it. A struct holding strings copies as any value does; the
compiler releases its strings where the struct is destroyed. Strings are not owners and never move, and
they may cross puroutines. The one exception is `unsafe.String(p, n)`, which makes an uncounted string
over memory something else owns: it is a view, and follows the rules of section 6.3.

### 6.3 Views

`[]T` and `*T` are views. So are `unsafe.Pointer`, and the frame-bound values of sections 4.4
and 4.5: a capturing closure, a method value, and an interface holding a frame copy or a lent owner. A
struct or array that contains a view, and no owner, is a view too: it copies freely and must not outlive
what any of its views look at. A string made with `unsafe.String` is a view of the memory it was made
from (section 6.10). A closure or method value that a function returns is built in a region its caller
provides (section 4.5), so it is a view of the caller's frame, not of the function's: returning it is
allowed, and the caller may not let it outlive its own frame.

1. **Views are free.** `&x`, `b[i:j]`, `v.Slice()` and copying or passing any of them cost
   nothing beyond the words copied. A view never frees anything; dropping one does nothing.
2. **`&x` is a view of `x` where `x` lives.** Where a variable lives is decided by its declaration, never
   by how it is used later: a local variable or parameter lives in its function's frame, a package
   variable in static storage, a field in its struct, an element in its array. Plutonium never moves a
   variable to the heap because its address is taken, and `return &x` does not copy `x` to the caller. It
   is a compile error (section 6.9) whose message shows the two fixes: return `x` itself, which moves or
   copies it to the caller, or allocate it with `new`.
3. **Passing a view down is always allowed.** A callee's frame ends before its caller's, so `f(&x)`,
   `f(buf[:n])` and `f(v.Slice())` are valid whatever `x`, `buf` and `v` are. The callee may return a view
   derived from its parameters (`return b[4:]`), since the caller's memory outlives the call.
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
     variable (`v.Slice()`, `b.Bytes()`, `&m`) follows the frame rules above, and in addition may not be
     used after that variable has been moved, dropped or given a new value in the same function. A view of
     a temporary owner (`makeVec().Slice()`) may not outlive the statement (section 6.4).
   - **Heap memory** reached any other way is checked at run time by generation checks: a view whose
     allocation has since been freed, or reallocated by a growing owner, faults when it is used instead of
     reading freed memory. So is arena memory (section 7.1): a view, string or box into an
     `arena.Chunked` faults when it is used, or dropped, after the arena's `Reset`, `Drop` or a `Release`
     past it, with a panic that names that call. A prototype is built with `pluto build --gencheck`
     (hosted builds only; abi.md section 1.3); the design and its measured cost are in
     docs/plans/2026-10-09-memory-safety-design.md. Without it such a use is unchecked, as in C: the
     documented unchecked mode, which the debug allocator (section 7.3) narrows by catching uses of
     memory returned to it.
   - **Static memory, and memory nobody in the program owns** (string literals, package variables, device
     registers, memory made from a `uintptr`) is never freed, so views of it have no limit.

Package-level initialisers are the exception of section 4.4: their literals, interface boxes and method
value records are static. A function implemented outside Plutonium (assembly, `//pu:extern`) is assumed
to keep every pointer argument; `//pu:noescape` on its declaration says it keeps none past the call,
except by storing one argument into memory another points to (as `atomic.StorePointer` does), which is
still checked. An `unsafe.Pointer` carries the same facts as the pointer it came from; a pointer made from
a `uintptr` is assumed to point outside every frame, and a pointer converted to a `uintptr` is no longer
followed. `//pu:resultholds p` on a function that returns an owner says it keeps `p` only for as long as
that owner lives, and stores `p` in it: the call then counts as returning `p` rather than keeping it, so
the result is a view of what `p` points to. `alloc.Use` is declared this way, so a local arena may be
installed as the default allocator while the `Restore` it returns lives (section 7.1). The directive is
for the standard library: it states a promise the compiler cannot check, and a program's own code does not
need it (E3008).

How precise the frame check is, so that programs know what it accepts:

- It is whole-program and needs no annotations. Each function's summary records which parameters it
  keeps, which it stores into memory another parameter points to, and which it returns, field by field;
  a call is checked against the callee's summary, and a call through an interface or a function value
  against every method or function that could be there (narrowed by type assertions on the way, and by
  the type of the value in the interface where the caller knows it). Summaries follow pointers up to
  three loads deep; beyond that, everything reachable is assumed.
- Local variables whose address is never taken are followed along the control flow, so reusing a
  variable (`err` above all) for a view of the frame and later for heap memory is fine. Other storage is
  followed field by field, two levels of struct fields deep, without regard to the order of statements.
- A generic function is checked once for all its instantiations: a conversion of a type parameter to an
  interface counts as a frame copy if any instantiation is larger than a word, and makes the methods of
  each type it is instantiated with possible callees of interface calls.
- A struct field is *heap-only* when, in the whole program, its address (or that of anything inside it)
  is never taken and everything stored in it is heap memory, or a parameter that every call passes heap
  memory for. Loading a heap-only field yields heap memory, so a list's `next` or an arena's current
  chunk does not make the function that follows it keep its caller's pointers. Writes through memory
  reinterpreted with `unsafe.Pointer` are not seen (section 11).
- A pointer-shaped value (a struct with one pointer-shaped field, an array of one, section 4.4) in an
  interface is the pointer it holds, not a copy in the frame.
- Each iteration of a loop reuses the frame memory of the loop's variables, so views of them are checked
  as section 4.5 describes.

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

The library's `vec.Vec[T]`, `hash.Map[K, V]`, `hash.Set[K]`, `strings.Builder`, `os.File` and arenas that own
their chunks are declared `owner`.

**Moves.** An owner is moved, not copied, when it is

- assigned to a variable (`n := m`, `n = m`);
- passed by value as an argument, including as the receiver of a method with a value receiver;
- returned;
- sent on a channel or passed to a `go` statement (sections 4.7, 4.8);
- stored in a field, an element or a composite literal (`c.buf = b`, `s[i] = b`, `Conn{buf: b}`);
- converted to an owning interface (section 4.4);
- dropped explicitly with `x.Drop()`.

A move copies the owner's bytes and clears the source variable to the zero value, which serves as its drop
flag: dropping the zero value does nothing (the optimizer removes the clearing store where nothing reads
it). The compiler tracks that the source no longer holds a value, and using it afterwards (reading it, calling a method,
taking its address) is a compile error. Assigning a new value to a moved-from variable makes it usable
again. The error message names the move and its line, and suggests a fix based on how the variable is
used afterwards: `Clone` when both copies are needed, a pointer when the later use only looks, reordering
when the later use could come first (section 6.9).

**Places.** An owner can be moved only out of a whole variable (a local or a parameter). Moving it out of
anything else is a compile error: a field (`t.left`), an element (`s[i]`), a value reached through a
pointer (`*p`, `p.buf`) or a package-level variable. Each of those places would still believe it holds the
value and would drop it again. To move out of such a place:

- `take(&place)` returns the value and leaves the zero value behind. `take` is a predeclared function,
  `func take[T any](p *T) T`, for values of any type; the move checker treats it as a use of the
  place, which still holds a value (the zero one) afterwards;
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
owner or has `Clone`), `hash.Map[K, V]`, `strings.Builder` and `owned[T]` have one; `os.File` has none, since
a file descriptor cannot be copied that way.

**Drops.** An owner held by a variable when the variable's scope ends is dropped automatically, on every
path out of the scope: falling off the end of a block, `return`, `break`, `continue` and `goto`, and at the
end of each iteration for a variable declared in a loop body. In detail:

- Variables are dropped in reverse order of declaration, so an owner that holds a view of an earlier
  variable (a `vec.Vec` allocating from a local arena) is dropped before it.
- Parameters are dropped when the function returns, after its locals. Results are moved to the caller and
  not dropped; a bare `return` moves the named results.
- A variable moved on some paths and not others is dropped only on the paths where it still holds a value
  (the move cleared it, so dropping it at the end of its scope does nothing). Using it after such a
  point is an error.
- Leaving a scope by `break`, `continue`, `return` or `goto` drops what the scopes it leaves hold. A
  `goto` back to a label before a variable's declaration drops the variable first, since its declaration
  runs again.
- `defer x.Drop()` on an owner variable drops `x` (its `Drop`, then its fields) when the function
  returns; `x` is not dropped again.
- Deferred calls run before the function's own variables are dropped, so a deferred call may use them. A
  deferred call may not use an owner declared in an inner block, which is dropped when the block ends.
- Dropping an owner runs its `Drop` method if it has one, then drops its fields in declaration order and
  array elements in index order. Fields that are views or plain values need nothing; counted fields,
  strings among them, release their references.
- An owner that an expression produces and nothing stores is a **temporary**. It is dropped at the end of
  the statement that made it, so a view of it may be passed to a call in that statement:
  `os.Stdout.Write(readAll(f).Slice())` is valid. In the header of an `if`, `switch` or
  `for ... range`, a temporary lives until the whole statement ends; in the condition and post statement
  of a three-clause `for` it is dropped after each evaluation.
- `panic` aborts without unwinding (section 8), so nothing is dropped; neither does `os.Exit`.
- `x.Drop()` on an owner variable ends its life early: it drops `x` completely (its `Drop` method, then
  its fields), counts as a move, and `x` is not dropped again. Calling `Drop` in any other way (through a
  pointer, a method value or an ordinary interface) is a compile error, since the real owner would drop
  the value a second time. Code that manages memory itself (an allocator, a container dropping its
  elements, an arena's variables) drops a value through a pointer with `unsafe.Drop(p)` (section 11).
- Values in memory from `alloc.New` or an arena are not dropped by the compiler: that memory has no
  owner until the code that allocated it gives it one, and `alloc.Free` drops the value before freeing.
- An `owned[T]` is dropped by dropping its `T` and freeing the memory to the allocator recorded in its
  header; an owning interface by the drop slot of its method table (section 4.4); an owner union by
  dropping the payload of the variant it holds (section 5.10). An owning interface with no methods finds
  the drop code in the type descriptor of what it holds (abi.md section 5), as one with methods finds it in the itab.

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
   (section 6.8); the debug allocator (section 7.3) reports what a cycle leaks.

How the compiler keeps these rules: a counted reference is counted exactly as a string is (section
6.10). A store into a place takes a reference for the new value and releases the old one; a call's
parameters are borrowed, so passing a reference down costs nothing unless the callee keeps it; a result
hands its reference to the caller; a reference read from memory a call could change is held for the
statement, unless no call made while it is held can release a reference (as for strings, section
6.10.1), so passing `h.p` to a function that only reads it costs no count either. The strong references together hold one weak reference, as in Rust's `Arc`, so the memory is
freed when the weak count reaches zero after `Drop` has run. A `synced` count is atomic once the program
runs more than one task (`rt.SetThreaded`, as for strings) and plain before; a `shared` count is always
plain, which is why it may not cross tasks (section 4.7). `weak[P].Upgrade` on a `synced` object is a
compare-and-swap that fails once the strong count is zero.

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

Strings are the exception, and counted by default (section 6.10): text is copied, stored and returned
far more than it is changed, a string's buffer refers to nothing else (so releasing one never frees a
graph), literals cost nothing to count, and the compiler removes most counts of strings passed down. The
kernel rules of section 12.7 keep string allocation out of code that must not allocate.

### 6.6 No allocation without an owner or an arena

Every allocation belongs to an owner or to an arena. The library is written so that functions that create
memory return owner types:

- **Owners record their allocator.** `vec.Vec`, `hash.Map` and `strings.Builder` keep it in a field;
  `owned`, `shared` and `synced` keep it in the allocation's header, and so does a string's buffer.
  Dropping an owner frees to that allocator, whichever is the default at the time.
- **Arenas own what is allocated in them.** Freeing into an arena does nothing, and the arena reclaims
  everything at once when it is reset or dropped. The same code therefore works in an arena and on the
  general heap: an owner allocated in an arena is dropped as usual, and its free costs nothing. Memory that
  only an arena owns can also be handed out as plain views (`a.New[T]()` returns a `*T`), valid until the
  arena is reset or dropped.
- **Text.** Strings own their buffers by counting (section 6.10): `s + t`, `strings.FromUTF8Lossy(b)` and
  `fmt.Sprintf` return a string, freed when its last copy goes. `strings.Builder` builds one with fewer
  copies: its `String()` shares the builder's buffer instead of copying it. `fmt.Appendf(&f, ...)` into a
  `vec.Fixed[byte]` over a caller's array formats without allocating (section 13.2).
- **Growing.** A slice is a view and cannot grow (section 4.3). Growing sequences are `vec.Vec[T]` or
  another owner; there are no arrays that nobody owns.
- **`new`** returns an owner under the proposal in section 4.2 (`owned[T]`); slices are allocated as
  `vec.Vec[T]` owners (`vec.Make`), since `make` of a slice is not allowed.
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

**Range.** `for k, v := range x`, where `x`'s type is not one the language ranges over (a string,
integer, array, pointer to array, slice, map, channel or function), ranges over `x.All()`: the iterator
`All` returns is ranged over as section 4.10 describes, so `x` is evaluated once and `All` is called once,
before the loop. `All` is found in the method set of `x`'s type as `Index` is, so a pointer receiver needs
an addressable `x` or a pointer (E2459); its form is `All() F`, with no parameters, type parameters or
variadic parameter, where `F` is a function type that range accepts, such as `iter.Seq[V]` or
`iter.Seq2[K, V]` (E2458). The iteration variables follow `F`, so `vec.Vec[T]`, whose `All` returns
`iter.Seq2[int, T]`, ranges like a slice, `hash.Map[K, V]` like a map (in its table's order, section 13),
and `hash.Set[K]` over its elements. As for a slice in Go, a `vec.Vec` loop reads the length once, when it
starts: elements the body pushes are not visited (a worklist that should see them is an index loop,
`for i := 0; i < v.Len(); i++`), each element is read when the loop reaches it, and the loop stops early
if the body shrinks the vector below the next index. A `ring[T, N]` ranges over its built-in `All` (section 5.4). A struct,
or a type with an `Index` method, that has no `All` is a compile error (E2456).

```go
for i, x := range v { ... }  // v.All(): index and element
for k, n := range counts { ... }
for k := range counts { ... } // keys only, as for a Go map
```

In generic code the choice is made from the constraint, as for indexing: a type parameter whose type set
is made of types the language ranges over is ranged over by the language, and one constrained only by
methods ranges over its `All` method.

**Generics.** `pluto/container` declares the constraints: `Indexable[K, V]` (`Index` and `SetIndex`),
`Reader[K, V]`, `Lookup[K, V]` (`IndexOk`), `Sized` (`Len`) and `Iterable[K, V]` (`All`, for range). A value of a type parameter so
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

**Strings.**

```go
func greet(name string) string {
	msg := fmt.Sprintf("hello, %s\n", name) // a new string: one reference, msg's
	os.Stdout.WriteString(msg)              // passed down: borrowed, nothing counted
	last = msg                              // a package variable keeps a copy: count 2
	return msg[7:]                          // a substring shares the buffer: count 3 for the caller
}                                           // msg goes out of scope: count 2
```

**Returning an owner.**

```go
func render(items []Item) vec.Vec[byte] {
	out := vec.New[byte]()                  // empty; allocates on its first write
	for i := range items {
		out.Append(items[i].line...)
	}
	return out                              // moves out to the caller: nothing copied, nothing freed
}

func main() {
	out := render(items)                    // main owns the bytes now
	os.Stdout.Write(out.Slice())
}                                           // and they are freed here
```

**Storing an owner in a struct.**

```go
type Conn owner struct {                    // owner: it has owner fields
	file os.File
	in   vec.Vec[byte]
	name string                             // a string field: copied with the struct, counted
}

func dial(path string) (Conn, error) {
	f, err := os.Open(path)                 // f is an os.File, an owner
	if err != nil {
		return Conn{}, err                  // f is the zero File: dropping it does nothing
	}
	var c Conn
	c.file = f                              // moves f into the field
	c.name = path                           // c keeps a reference to the path's bytes
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
	name     string
	parent   weak[shared[Node]]     // does not keep the parent alive
	children vec.Vec[shared[Node]]  // keeps the children alive
}

func addChild(parent shared[Node], name string) shared[Node] {
	child := alloc.Shared[Node]()
	child.name = name
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
	var reply strings.Builder
	reply.Init(&a)                      // the builder grows in the arena
	for i := 0; i < fields.Len(); i++ {
		f := fields.Ptr(i)
		fmt.Fprintf(&reply, "%s=%s\n", f.key, f.val)
	}
	return send(req, reply.Bytes())
}   // reply, then fields, are dropped: their frees go to the arena and do nothing;
    // a and mem go with the frame. Returning fields would be a compile error: it views a.
```

A string built while an arena is the current allocator lives in the arena, and must not be used after the
arena is reset or dropped (section 6.10); for a local arena, returning or storing such a string beyond the
arena is a compile error (section 7.1).

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
points here. Every message has a code from E3000 to E3999, and `pluto explain` prints the long form.

The compiler's exact output for each implemented message is pinned by the programs and expected
output in `cmd/pluto/testdata/diag/escape`, `owners`, `moves` and `moves_more`; the first example below
is copied from there. The others show what each message says and which fixes it offers, in the same
form; their wording in the compiler can differ in detail. Messages marked *(planned)* belong to rules
that are not enforced yet (sections 6.3 and 6.5).

**Returning a pointer to a local variable** (E3001).

```
main.p:25:9: cannot return &c, a pointer to a local variable
    &c is a pointer to c. c is a local variable of load (declared at main.p:23).
    Its memory is reclaimed when load returns, so &c would point at memory that
    no longer holds c.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    To give the value to the caller, return it instead. This copies it, or moves
    it if it is an owner:
        return c
    To keep it at a fixed address after load returns, allocate it:
        c := new(Config)
    (see "Views" in the Plutonium spec, section 6.3)
    (run "pluto explain E3001" for a longer explanation)
```

**Returning a view of a local owner** *(planned; a slice of a local array is E3001 today)*.

```
names.p:12:9: cannot return v.Slice(), which points into v, a local variable
    v.Slice() does not copy anything: it is a view, a slice that looks at the
    elements v owns. v is a local variable of collect (declared at names.p:5),
    and when collect returns v is dropped and its elements are freed, so the
    slice would look at freed memory.
    To give the caller the elements, return the vector itself. That moves it:
        func collect(parts []string) vec.Vec[int] {
            ...
            return v
        }
    The caller reads it as a slice with .Slice() for as long as it keeps it.
    (see "Ownership" in the language spec, section 6.3)
```

The same message with `a slice of buf, a local array` and the fix `return a vec.Vec[byte]` (or "copy the
bytes into a buffer the caller passes in: copy(dst, buf[:n])") is given for `return buf[:n]`.

**Storing a view where it outlives its target** (E3002).

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

**Passing a view to a function that keeps it** (E3003).

```
main.p:12:12: cannot pass buf[:n] to cache.Put, which keeps it after returning
    buf[:n] is a slice that looks at buf, a local array of main (declared at
    main.p:9). cache.Put stores its parameter p in c.entries (cache.p:31), which
    lives longer than this call, so the slice would outlive buf.
    Give Put something that keeps its bytes alive, for example a string:
        c.Put(strings.FromUTF8Lossy(buf[:n]))  // a copy the cache can keep; Put takes a string
    (see "Ownership" in the language spec, section 6.3)
```

**Passing a view of the frame to a puroutine or a channel** (E3004, E3005).

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

**Returning a closure that captures variables** (E3100; keeping one past a loop iteration is E3006,
section 4.5).

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

Returning a closure is allowed when the function makes it and its results hold it by value: the closure
is built in the region the caller provides (section 4.5). The message above is for the closures a region
cannot hold (E3100). When the closure is returned inside something else, the first line names what is
returned and where the literal is: `cannot return h: it holds the function literal at count.p:12, which
uses total, a local variable of counter`. The explanation then says why the region cannot hold it: "counter's
results can hold the closure only through a pointer, so its callers provide no region to build it in",
or "counter builds the closures it returns in a region its caller provides, but this one is made in the
function literal at count.p:10, which is a function of its own". A method value reads `cannot return the
method value c.get: its closure record is in f's frame`, with "a method value is a closure record, holding
the method to call and a copy of the receiver" as the explanation and a method expression
(`(*Counter).Get`) as the fix. The closures of a call read `cannot return h: it holds a closure that
counter() (count.p:20) builds in f's frame`.

**Keeping the closure of a call in a loop past the iteration (E3101).**

```
main.p:17:10: cannot keep the closure counterFrom(n) returns past this iteration of the loop: it is stored in last, declared outside the loop (main.p:15)
    A function that returns a closure builds it in a region its caller provides
    (spec section 4.5). The region of a call in a loop is part of this frame,
    and the call uses the same region on every iteration, so the closure
    counterFrom(n) returns on one iteration is overwritten on the next. Keeping it
    past the iteration, in last, declared outside the loop (main.p:15), would
    leave it changing under you.
    Use the closure within the iteration, or declare the variable that holds it
    inside the loop.
```

Stored through a pointer, the message ends `it is stored in memory reached through a pointer`.

**A region that holds itself (E3102).**

```
main.p:49:10: chain cannot return the closures of this call: the call reaches chain again, so its region would have no fixed size
    A function that returns a closure builds it in a region its caller provides,
    and the region holds the regions of the calls whose closures it returns in
    turn (spec section 4.5). chain returns closures that hold those of this
    call, and the call can reach chain again, directly, through other
    functions, or through a func value or an interface, so its region would
    have to hold itself.
    Build the closures in a loop instead of by recursion, or keep their state in
    a value the caller owns, such as a struct with a method.
```

**Returning an interface that holds a frame copy** (E3001)**, or lends an owner** *(planned)*.

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

**Using a variable after it was moved** (E3301).

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

**Using a variable that may have been moved** (E3302).

```
main.p:20:5: b may have been moved at line 15, so it cannot be used here
    b is moved into flush at line 15 only when verbose is true. On that path b
    holds nothing afterwards, and the compiler cannot let a value be used on a
    path where it may already belong to someone else.
    Use b before line 15, or give it a new value on that path:
        if verbose {
            flush(b)
            b = vec.New[byte]()
        }
    (see "Ownership" in the language spec, section 6.4)
```

In a loop the first line is `b was moved to out at line 9 in the previous iteration of this loop`, and the
fixes are "declare b inside the loop so that each iteration has its own" and "b.Clone()".

**Moving out of a field, an element or through a pointer** (E3303; the range loop is E3304).

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

For a range loop the first line is `cannot copy the elements of bufs in a range loop: vec.Vec[byte] is an
owner type`, and the fix is

```
        for i := range bufs {
            b := &bufs[i]
            ...
        }
```

**Moving a captured variable inside a function literal** (E3305).

```
job.p:17:8: cannot move b inside a function literal: the literal may run more than once, or never
    b is a variable of run (declared at job.p:12) that the literal uses where it
    lives. Moving it here would leave b empty after the first call, and run
    could not tell whether b still needs to be dropped.
    Pass the value to the literal as an argument:
        do := func(b vec.Vec[byte]) { send(b) }
        do(b)
    (see "Ownership" in the language spec, section 6.4)
```

**Using a view after its owner is gone** *(planned)*.

```
main.p:16:14: s cannot be used here: it looks at b's bytes, and b was moved at line 15
    s := b.Slice() at line 14 made s a view: a slice that looks at bytes b
    owns, without copying them. At line 15 b was moved into send, and whoever
    owns the bytes now may free or change them at any time.
    Use s before line 15, or keep a copy that does not depend on b:
        c := b.Clone()
        s := c.Slice()
    (see "Ownership" in the language spec, section 6.3)
```

For a temporary:

```
main.p:8:7: s would look at a temporary that is dropped at the end of this statement
    readAll returns a vec.Vec[byte], which owns the bytes. It is not stored
    anywhere, so it is dropped when this statement ends, and s would look at
    freed bytes.
    Keep the vector in a variable and look at it while it lives:
        data := readAll(f)
        s := data.Slice()
    (see "Ownership" in the language spec, section 6.4)
```

**A deferred call that uses an owner from an inner block** (E3311).

```
main.p:11:9: the deferred call uses b, which is dropped at the end of its block (line 14)
    Deferred calls run when the function returns. b is declared in the block
    that ends at line 14 and is dropped there, before this call would run, so
    the call would use freed memory.
    Declare b at the top level of the function, or do the work at the end of
    the block instead of deferring it.
    (see "Ownership" in the language spec, section 6.4)
```

**A `Drop` method on a type not declared owner** (E3201; a `Drop` with parameters or results is E3206).

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

**An owner field in a struct not declared owner** (E3202; in a struct type without a name, E3203).

```
conn.p:4:2: Conn must be declared owner: its field buf is a strings.Builder, which is an owner
    An owner owns memory and frees it when dropped, so it moves instead of
    being copied. A struct that contains one owns that memory too, and copying
    the struct would copy the owner, leaving two values that both free it.
    Plutonium does not make Conn an owner silently; say so in its declaration:
        type Conn owner struct {
            ...
        }
    Conn values then move, and buf is dropped when a Conn is.
    If Conn should only look at a buffer that something else owns, store a pointer:
        buf *strings.Builder
    (see "Ownership" in the language spec, section 6.4)
```

The same message covers an embedded owner (`its embedded field Buf`), an array of owners (`its field
pages is a [4]Frame, an array of owners`), an interface embedding an owning interface, and `type B A`
(`B is defined from A, which is an owner; write type B owner A`).

**`owner` where it is not allowed** (E3204; on an alias, E3205).

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

**Calling `Drop` through a view** (E3307).

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

**An owner type for a type parameter that does not allow owners** (E3207).

```
main.p:10:12: cannot use strings.Builder as T in Fill: strings.Builder is an owner and T does not allow owners
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

**Duplicating owners with `copy`** (E3306; owners as variadic arguments are E3310, in a slice literal
E3312, and `x[k]` on a container of owners E3309).

```
main.p:7:2: cannot copy elements of type strings.Builder: each element is an owner
    copy duplicates elements, and duplicating an owner would leave two values
    that both free the same memory.
    Move the elements one at a time:
        for i := range src { dst[i] = take(&src[i]) }
    or copy each one explicitly:
        for i := range src { dst[i] = src[i].Clone() }
    (see "Ownership" in the language spec, section 6.4)
```

**Taking an owner out of an ordinary interface** (E3308).

```
main.p:19:7: cannot assert x to strings.Builder: x is an any, which only lends the value it holds
    Converting an owner to an ordinary interface such as any lends it: the
    interface looks at the owner, which still belongs to the variable that was
    converted. Taking a strings.Builder out of x would create a second owner.
    Call the value's methods through an interface type instead:
        if s, ok := x.(fmt.Stringer); ok { use(s.String()) }
    or move it into an owning interface in the first place:
        type Text owner interface{ String() string }
    (see "Ownership" in the language spec, section 6.4, and section 4.4)
```

**A `shared[T]` crossing puroutines** *(planned)*.

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

### 6.10 Strings

A `string` is an immutable, reference-counted value. String code reads and behaves as in Go: `s + t`,
`s += t`, `fmt.Sprintf`, substrings, comparison, `len`, indexing and `range`
all work, strings are stored in structs, slices and maps and passed and returned freely, and nothing is
ever freed while a copy can still reach it. The compiler counts the copies, as Swift does, and frees a
string's bytes when the last copy goes. This is the one place where Plutonium counts without being asked
(section 6.5 says why everything else does not).

**Values, buffers and counts.**

- A string made at run time (by `+`, a conversion, `fmt.Sprintf`, `strconv.Itoa`, `strings.Builder`)
  has a **buffer**: its bytes, allocated from the current allocator (section 7.1) behind a header that
  records the allocator, the size and a count of the strings that use it. The buffer never changes once
  a string uses it.
- Copying a string (assigning it, storing it in a field, an element, an interface, a result, a captured
  variable) copies its three words and adds one to the count. Dropping a copy (its variable's scope ends,
  it is overwritten, the struct or container holding it is dropped) subtracts one, and the last drop
  frees the buffer to the allocator recorded in its header.
- A substring `s[i:j]` shares `s`'s buffer and keeps it alive.
- String literals, constants and every other static string (package-level constant initializers,
  compile-time concatenations, type names, enum names) live in read-only data and are **never counted**:
  their control word has no buffer, so copying or dropping one costs nothing. The zero string is static.
- Counts are atomic once a program can run Plutonium code on several threads (memory-model.md section
  3.7). A hosted program counts with plain arithmetic until it starts its first puroutine, when the
  runtime switches to atomic counts (`rt.SetThreaded`); a freestanding program counts atomically from
  the start, since its kernel starts processors on its own. Strings are therefore sendable (section
  4.7). A `go` statement gives the new puroutine its own references to the strings in its arguments,
  and a send gives the channel one, which the receiver takes over; a channel freed with values still
  buffered releases their strings (section 4.8).

**Strings and ownership.** A string is a value, not an owner: it never moves, a moved-from string does
not exist, and copying is always allowed. A struct or array with string fields is a value too: it copies
freely, each copy counting its strings, and needs no `owner` declaration. When such a value is destroyed,
the compiler releases its strings: this is **drop glue**, the same release that an owner's fields get
(section 6.4), but implicit. Values are destroyed:

- at the end of a variable's scope, on every path out of it (falling off the end of the block, `return`,
  `break`, `continue`, `goto`), and at the end of each iteration for a variable declared in a loop body;
- when a place is overwritten: the new value takes its references first, then the old one is released,
  so `s = s[1:]` and `a, b = b, a` are safe;
- for a **temporary** (a call's result, a concatenation, a conversion or a composite literal that nothing
  stores), at the end of the statement that made it; in the condition of a three-clause `for`, after each
  evaluation;
- for storage that outlives a statement but belongs to the frame (an interface's box, the object of
  `&T{...}`, a slice literal's array, a method value's receiver, a deferred call's saved arguments, the
  region a call builds its closures in, section 4.5), when the function returns, after its deferred calls
  have run, or when the same expression runs again and refills it.

`panic` and `os.Exit` release nothing, as for owners. A `[]string` slice is a view: its elements belong
to the array it looks at, and whoever owns the array releases them. The library's containers do:
`vec.Vec[string]` and `hash.Map[string, V]` release their elements when they are cleared, truncated,
freed or dropped, through `rt.ReleaseValues`, the drop glue the compiler makes for any type. Memory from
`new`, `make` or `alloc.New` that nobody frees keeps the strings in it alive too: `alloc.Free(p)` releases
the strings in `*p` before freeing it.

**Passing and returning.** A string parameter is **borrowed**: the caller keeps the string alive for the
call, so passing one costs nothing, and the callee counts only the copies it keeps. A function that
assigns to a string parameter, or takes its address, takes a reference of its own on entry. A result is
**owned**: the callee hands the caller one reference, and `return x` for a local `x` hands over `x`'s
own instead of adding one.

**Representation** (abi.md section 1). A string is three words: the address of its first byte, its length
in bytes, and a **control word**: the address of its buffer's header, or'ed with flag bits, or only the
flags for a string nobody counts. The header is 8-aligned, so the low three bits are free. Bit 0 is set
when the string may contain a byte at or above `0x80`; when it is clear, every byte is ASCII, which gives
the library O(1) rune counting and indexing and ASCII-only paths for case conversion and comparison.
Constructors compute it as they copy the bytes, and concatenation ors its operands' bits without
scanning again. A substring keeps its parent's bit, which may then say "maybe" for an ASCII substring.

The third word was chosen over two-word forms because substrings must find their buffer: a count found
from the data pointer (at a fixed offset before it) does not work once a substring points into the
middle of the buffer, and packing a count pointer or an offset into the length word limits strings' length
and costs a mask on every `len`. The cost of the third word is one more register for each string passed
or returned (a function returning a string and an error returns them in memory), and eight more bytes for
each string stored; the roadmap (item 55) records what it measured.

**Valid UTF-8.** Every string holds valid UTF-8, as Rust's `str` does.

- A string literal or constant that is not valid UTF-8 (`"\xff"`, `"\xc0\x80"`) is a compile error.
  Binary data belongs in `[]byte`, `[N]byte` or `vec.Vec[byte]`.
- `string(b)` for a byte slice is a compile error (E2015). Go keeps the bytes, so a string is any byte
  sequence there; here the conversion would have to replace each byte that does not begin a valid UTF-8
  sequence with U+FFFD, and so `string([]byte{0xfe}) == string([]byte{0xff})`, a silent change of data.
  `strings.FromUTF8(b)` returns the string and an error, `ErrInvalidUTF8`, for bytes that are not valid
  UTF-8. `strings.FromUTF8Lossy(b)` returns a copy with each such byte replaced by U+FFFD, as `range`
  over the bytes would decode them; it never fails, and the name says that two different slices can give
  equal strings. Binary data (a digest used as a key, a protocol field) belongs in a byte array or slice.
  The library's other paths from bytes to strings (`strings.Builder.String`, `bytes.Buffer.String`,
  `fmt.Sprintf` of a `[]byte`, names from the operating system, section 13.1) are text-oriented and keep
  replacing. `string(runes)` and `string(r)` replace invalid code points (surrogate halves, values above
  U+10FFFF, negative values) with U+FFFD, as in Go.
- `s[i:j]` must start and end at the start of a rune (or at `len(s)`); otherwise it panics. The check
  reads the bytes at `i` and `j`, and the compiler leaves it out for an ASCII string and where it can
  prove the bound is a rune boundary: 0 and `len(s)`; a bound of an earlier slice of `s` that was
  checked; an offset that advances by the width of the rune decoded there
  (`_, w := utf8.DecodeRuneInString(s[i:]); i += w`, or the index of a `range` over `s`) or retreats by
  the width of the rune before it (`utf8.DecodeLastRuneInString(s[:i])`); the index of a match that
  `strings.Index`, `IndexRune`, `IndexFunc` and their relatives return, or `IndexByte` of an ASCII byte;
  and, where the index is known not to be -1, the end of that match (`s[i+len(sep):]`). The library's
  byte-level searching, which compares slices at offsets that need not be boundaries, slices with
  `rt.SliceBytewise(s, i, j)`, which checks the bounds against `len(s)` only.
- `len(s)` counts bytes, `s[i]` is a byte, `range` yields runes and comparison is bytewise, as in Go.
- `unsafe.String(p, n)` is the escape hatch: it makes a string of `n` bytes at `p` without copying or
  checking them. Such a string is not counted (its control word has no buffer, and its flags say the
  bytes may be non-ASCII); it is a view of that memory, which must outlive every copy of it, as for any
  view (section 6.3). If the bytes are not valid UTF-8, the behaviour of the string functions and of
  `range` on it is undefined. `unsafe.StringData(s)` returns the address of the first byte. The library
  never uses it on bytes from the operating system (section 13.1).

**Conversions.** `[]byte(s)` and `[]rune(s)` are compile errors (E2016): a slice is a view, so the copy
would belong to no one, and without a collector nothing would free it. `strings.Bytes(s)` returns the
bytes in a `vec.Vec[byte]` and `strings.Runes(s)` the runes in a `vec.Vec[rune]`, which own the copy,
allocate from the allocator they are given (by default the current one) and free it when dropped;
`v.Slice()` lends it as a slice. `copy(buf[:], s)` copies the bytes of a string into a buffer of the
program's own and allocates nothing; `for i, r := range s` visits the runes without copying, and
`utf8.RuneCountInString(s)` counts them. `string(runes)`
and `string(r)` for a non-constant rune make a counted string; `string(r)` for an ASCII rune returns a
static one-byte string and does not allocate, and a rune out of range gives U+FFFD as in Go. `string(i)`
for an integer of any other type is a compile error (E2017): Go makes the character with code `i`, so
`string(65)` is `"A"` where a program usually wants `"65"`, a trap that `go vet` reports. A conversion to
string is accepted from a `rune` (`int32`), from an untyped constant (`string(65)`, `string('A')`) and from
a string; `string(rune(i))` says that `i` is a character, and `strconv.Itoa(i)`, `strconv.FormatInt` and
`strconv.FormatUint` make the digits. A named type whose underlying type is `int32` is
converted to `rune` first.

**Interface boxes.** A string converted to an interface is copied into a box in the converting
function's frame (section 4.4), which holds a reference until the function returns or the same
conversion runs again. A loop that prints a string therefore keeps the last one alive until the
function returns; code that must see the memory freed earlier converts in a function of its own.

**Arenas.** A string built while an arena is the current allocator (`defer alloc.Use(&a).Restore()`) has
its buffer in the arena. Releasing it writes its header, so it must not be used, or dropped, after the
arena is reset or dropped. For an arena in a local variable the compiler checks this: such a string is a
view of the arena, which may not be returned or stored beyond it (section 7.1). Declare the arena before
the strings that use it, as for any owner that views it (section 6.4).

**Races.** Reading a string variable while another puroutine writes it is a data race, as for any value
larger than a word (memory-model.md section 4.3): the reader may see a torn value, or a buffer the writer
has just released. Strings that are not written while shared need no locks.

**What it costs.** Copying a static string costs nothing but the words copied, and so does passing any
string down. Storing a heap string costs an increment, and dropping one a decrement and a test, both
plain arithmetic in a single-threaded program. The compiler removes the counts it can prove unneeded
(section 6.10.1); the rest are the price of strings that read like Go's.

#### 6.10.1 Count elision

The compiler leaves out the counting that cannot be observed:

- **Parameters** are borrowed (above), so passing a string costs nothing in the common case. When the
  argument is read from memory a call could change (a field reached through a pointer, a slice element, a
  package variable), the caller holds a reference for the rest of the statement, unless no call made
  while it is held can release a string: the optimiser (at `-O1` and above) summarises every function of
  the program, and a function may release one if it drops a string, `shared`, `synced` or `weak`
  reference, makes a call through a func value or an interface, or calls a function that may. Counted
  boxes (section 6.5) are held and released by the same rule. Calls that never return (panics) do not
  count. Searching, comparing, hashing and other code that only reads strings therefore costs no count.
- **Static strings** never touch a count, and the compiler skips the call altogether when it knows a
  value is static.
- **Local copies**: a temporary stored into a variable hands over its reference instead of being copied
  and released; `return x` hands over `x`'s; a local whose address is not taken and that no function
  literal uses is read without a reference.

In kernel mode, the operations that make new strings allocate under the same rules as `new` and `make`
(section 12.7).

### 6.11 Data races

Safe code has no data races: two puroutines never use the same memory at once with one of them writing,
unless both use atomic operations. Go leaves races to a run-time detector; Plutonium checks the whole
program when it compiles, with no annotations (memory model, section 4). Three rules do it, all inferred
from the program. The rest of this section says what each rule accepts, what it rejects with which error,
and what to write instead.

**Puroutines.** The compiler finds every puroutine the program may start: each `go` statement, and each
function value passed to a function that starts a puroutine with it (`rt.Go`, `time.AfterFunc`,
`sync.WaitGroup.Go`, or a function of the program's own that passes a func parameter on to one of these;
found by following func parameters, section 4.7). The **main puroutine** runs package initialisation
and `main`. A `go` statement that may run more than once (in a loop, or in a function other than `main`)
may start several puroutines that run the same code at once. Package initialisation, and the part of
`main` before the first statement that may start a puroutine, run while no other puroutine exists.

**Accesses and locks.** For each puroutine the compiler collects what it reads and writes, in its own
code and in every function it calls (calls through interfaces and func values reach every function that
could be called there), and which locks are held at each access. A lock is a `sync.Mutex` or
`sync.RWMutex`, held from `Lock` (or `RLock`, or a `TryLock` that succeeded, in the branch where it did)
to `Unlock`, or to the end of the function with `defer mu.Unlock()`. The analysis follows the control
flow of each function: a lock counts as held at a statement only if it is held on every path there. A
function that returns with a lock held (a helper such as `func (s *State) lock() { s.mu.Lock() }`) holds
it for its caller, and a lock held around a call counts inside the callee. A lock reached through a
pointer is named by where the pointer came from, so `s.mu` and `s.items` in one method are the lock and
the data of the same object. Unlocking a mutex the compiler cannot name counts as unlocking every lock.
The order that channel operations and `WaitGroup.Wait` impose between puroutines is not used: reading a
counter after `wg.Wait()` still holds the counter's lock. A method value (`f := s.add`) may be called
later, anywhere, so what the method does counts as done with no lock held.

**Guards.** For data that several puroutines may use, the compiler infers the lock that guards it: a lock
that every write of the data holds for writing (a `sync.RWMutex`'s `Lock`, not `RLock`). The data is then
safe when every access, read or write, holds its guard (a read may hold it with `RLock`). Data that every
access changes or reads with `pluto/sync/atomic` operations (the `atomic.Int64` types' methods, or
`atomic.AddInt64(&x, 1)` and the other functions) needs no lock. A field of a struct, and all the
elements of an array or slice together, are one piece of data each; writing a whole struct writes all its
fields. Values of `pluto/sync` and `pluto/sync/atomic` types synchronise themselves through their methods;
assigning a whole new value to one is a write like any other.

**Rule 1: package variables** (gap G32 of the complete-spec plan, adopted). A package variable (or a
field or element of one) that two puroutines may use, or that one `go` statement's puroutines may use
while several run, with at least one of them writing it once puroutines run, must be guarded or atomic:

- E3502: it is written with no lock that every write holds. The message names the write, the puroutine
  that makes it (by the `go` statement that starts it), and another puroutine's access it may meet.
- E3503: it is read without its guard. The message names the guard, the writes that hold it, and the
  read.

```go
var (
	mu   sync.Mutex
	hits int
)

func handle(done chan bool) {
	mu.Lock()
	hits++ // every write holds mu: mu guards hits
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		go handle(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	mu.Lock()
	println(hits) // without mu.Lock(): E3503, hits is read without holding mu
	mu.Unlock()
}
```

A package variable written only during initialisation, or only by `main` before it starts its first
puroutine, may be read by every puroutine without a lock. Variables of the runtime itself (`pluto/rt`),
which implements puroutines with its own locks and with preemption disabled, are outside the rule, as is
memory that unsafe code reaches through `unsafe.Pointer`.

**Rule 2: views that cross** (the sendable rule of section 4.7; memory safety decision D5(b)). A pointer
or slice given to another puroutine, as a `go` argument or receiver, a value sent on a channel, or inside
an interface, struct or array that crosses, must point to memory that is safe for both:

- memory of a `pluto/sync` or `pluto/sync/atomic` type, of the runtime's own records (`rt.Task`,
  `rt.Timer`), a channel, or a struct of only those
  (`go worker(&wg)` with a package-level `wg`);
- a package variable, or part of one, that no puroutine writes once puroutines run;
- the object of a `synced[T]` (rule 3);
- an object of a struct type that satisfies the **type rule**: no code overwrites a whole object of the
  type through a pointer, and each field either never changes once an object is shared, or is used only
  atomically or only under one lock of the same object (or one package-level lock), inferred as above.
  Writes that initialise an object do not count: writes through a pointer to memory the function
  allocated (`new`, `make`, `alloc.New`, or a function that returns memory it allocated and kept to
  itself, such as a constructor), before the pointer is stored, passed to a function that keeps it, sent
  or returned, including writes made by a function it is passed to, such as an `init` method. So an
  object built by its constructor and then only read, and an object whose methods all hold its own
  `sync.Mutex`, may both be shared; `os.File`, whose state is atomic, is one of the second kind.

A pointer to, or slice of, anything other than a struct is accepted only in the first three cases.
Anything else is E3501, which names a write that could change the memory while the other puroutine uses
it, where it knows one. The fixes: pass a copy
of the value; move an owner (a `vec.Vec` instead of a slice) so that only the receiver has it; or keep
the shared data where the guards of rules 1 and 3 apply.

```go
type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *Counter) Peek() int { return c.n } // E3501 at the go statement below: n is changed under
                                            // c.mu in Add and read here without it

var counter Counter

go work(&counter, done) // work calls Add, and main calls Peek
```

An owner, such as the `owned[T]` that `new` returns, moves to the task instead (`go c.bump()` on an
`owned[Counter]` hands over the box), so only one task has it and none of this applies.

**Rule 3: synced objects.** Every puroutine that holds a `synced[T]` reference may use its object, so
every field of `T` that some code changes must be guarded by a lock of the same object, or used only
atomically, with the same errors E3502 and E3503 as package variables. Writes that initialise the object,
before the reference is copied anywhere, do not count. A pointer method called on the box
(`go s.Serve()`, `s.Add(1)`) uses the object the same way.

Maps, once they return as counted reference types (gap G16), are sendable like `synced[T]`, and their
contents are guarded by the same rule: writes from several puroutines hold one lock.

**What is not checked.** Unsafe code (section 11 and memory safety design section 2) is the programmer's
responsibility: memory reached through `unsafe.Pointer` or `rt.Pinned`, functions without a body
(assembly and C), and the runtime's own variables. A data race in unsafe code has the limited
consequences of the memory model (section 4 there), never undefined behaviour.

**Scoped puroutines.** The sharing check of `sync.Scope` (docs/plans/2026-10-10-scoped-puroutines.md,
section 4.5) uses the same lock inference for locals of the frame that scoped puroutines share; its
errors are E3401 to E3406 once it lands.

## 7. Memory and allocation

Plutonium never allocates heap memory unless the program asks: through `new`, `make`, the `pluto/alloc`
functions, `owned`/`shared`/`synced` constructors, the operations that make strings (`+` on strings that
are not constants, `strings.FromUTF8Lossy(b)`, `string(runes)`, `string(r)` for a rune that is not a constant;
section 6.10), or library code that takes an allocator or uses the default
one. Every allocation belongs to an owner, an arena or, for a string's buffer, the strings that count it
(section 6.6). Local variables, temporaries and interface frame copies live on the stack. Package-level
variables live in static storage (`.data` or `.bss`).

### 7.1 Allocators

```go
package alloc

type Allocator interface {
	Alloc(size, align uintptr) unsafe.Pointer      // zeroed memory, or nil
	Free(p unsafe.Pointer, size, align uintptr)
}

func Default() Allocator                // the current allocator
func SetDefault(a Allocator)            // replaces it for all later allocations
func Use(a Allocator) Restore           // replaces it while the Restore lives
func (r Restore) Restore()              // drops r now, putting the previous default back

// Owners of a single heap object (sections 5.6 and 6).
func Owned[T any](a Allocator = Default()) owned[T]
func Shared[T any](a Allocator = Default()) shared[T]
func Synced[T any](a Allocator = Default()) synced[T]

// The unchecked layer that owner types are written with (section 6.6).
func Raw(size, align uintptr, a Allocator = Default()) unsafe.Pointer
func New[T any](a Allocator = Default()) *T
func Free[T any](p *T, a Allocator = Default())   // drops *p first if T is an owner
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

`Restore` is an owner: dropping it, or calling its `Restore` method, which consumes it, puts the previous
default back. `Use` is declared `//pu:resultholds a` (section 6.3), so the `Restore` counts as a view of
`a`: an arena in a local variable may be installed, and the compiler rejects returning the `Restore` or
storing it anywhere that outlives the arena.

```go
func parseAll(files []string) {
	var a arena.Arena = arena.New(buf[:])
	r := alloc.Use(&a)  // r holds &a, so it may not leave this function
	...
	r.Restore()         // or let r be dropped at the end of the function
}
```

Owners allocated while the override is in force record the arena as their allocator, so dropping them
later frees into the arena, which does nothing (section 6.6). So that nothing allocated in a local arena
outlives it, the compiler treats what is allocated through `Default()` while a `Restore` that views a
local arena is live as a view of that arena (section 6.3): returning it, storing it in a package
variable, or handing it to a task is a compile error that names the arena. The window runs from the
call of `alloc.Use` to the end of its block, or to the end of the function for `defer
alloc.Use(&a).Restore()`, and covers `new` and `make` without an allocator argument, string
concatenation and conversions, calls that leave out a defaulted `alloc.Allocator` parameter (such as
`vec.New[T]()` or `fmt.Sprintf`, whose results come from `Default()`), and calls whose results keep their
allocator out of sight (a string, an `owned`/`shared`/`synced`/`weak` reference, an interface such as an
error):

```go
func name(n int) string {
	var mem [256]byte
	a := arena.New(mem[:])
	defer alloc.Use(&a).Restore()
	return fmt.Sprintf("item %d", n) // error: the string is in a, which is gone when name returns
}
```

To keep something past the arena, allocate it from an allocator that outlives it, passed explicitly
(`heap := alloc.Default()` before `Use`, then `vec.New[T](heap)`), or install an arena the caller
provides. Memory from the arena stored into other heap memory, by the function or a callee, is not
reported (it is most often in the same arena); per-arena epochs (memory-safety design D4) catch a use of
it after the arena is reset, released past it or dropped, at run time, in builds with generation checks
(section 6.3, abi.md section 1.3):

```go
func parse(a *arena.Chunked, errs *vec.Vec[error], tok string) {
	defer alloc.Use(a).Restore()
	errs.Push(fmt.Errorf("bad token %q", tok)) // the error is in a, the vector is not
}
...
parse(&a, &errs, "}")
a.Reset()
println(errs.At(0).Error()) // with --gencheck: panic: use of memory from an arena that was reset at main.run (main.p:31)
```

Dropping a string or counted box whose memory an arena has taken back is such a use too, since the
drop updates its count: a string made in an arena between `m := a.Mark()` and `a.Release(m)` must go out
of scope before the `Release`. Arena epochs cover `arena.Chunked`; a fixed `arena.Arena` works in its
caller's buffer, which has no epochs, and its views are checked only as that buffer is.

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
- slicing a string at a bound that is not the start of a rune (section 6.10),
- integer division or remainder by zero,
- a failed type assertion with a single result, on an interface or a union (section 5.10),
- a union whose tag names no variant, in a type switch without a default case,
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

A panic does not unwind: control never returns to a frame that panicked, and owners are not dropped. In
hosted builds the panicking task first runs its pending deferred calls (section 8.2). The runtime then
calls the program's **panic handler** (section 8.1), which never returns. In hosted builds without a
handler of their own, the built-in one prints `panic: ` followed by the value (and, for faults, what
failed and where), then a stack trace with the function, file and line of each frame, and the process
exits with status 2.

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
  handler ends in an endless loop or in a call of a function that never returns (`os.Exit`, `rt.Abort`,
  a function of the program's own whose every path ends that way), which the compiler works out from
  the functions' bodies (section 13). A call that the compiler cannot see through (through a func value
  or interface, or into recursion) counts as returning.
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

func (p *Panic) Print[W rt.Writer](w W)      // the panic as a hosted program prints it, then the trace
func (p *Panic) PrintTrace[W rt.Writer](w W) // the stack trace alone
```

`rt.Writer` has the method of `io.Writer`, so any writer will do; `rt.Console{}` writes where `print`
does (in freestanding builds, the function registered with `rt.SetConsole`; in hosted builds, standard
error, with a system call that never waits). The methods are generic so that a writer of a known type is
called directly: a call through the `rt.Writer` interface counts, for the check of section 12.6, as
calling every writer the program has. For the same reason a hosted handler should write to `rt.Console{}`
rather than `os.Stderr`: once a program makes a pipe, writing to an `os.File` may wait for the poller,
and the check cannot tell which file is written. The message lives in a
buffer of the runtime (1 KiB, longer messages are cut), and printing the trace reads the trace table and
the stack (debugging.md section 5), so nothing allocates. Since a panic does not unwind, the stack the
trace describes is still there while the handler runs, deferred calls having run below it (section 8.2).

A `print` or `println` statement is atomic, as in Go: statements that run at the same time, on other
puroutines or processors, never mix their output within a line. The statement's pieces go into a buffer
of 512 bytes on the runtime, held under a runtime lock, and are written with one call when the statement
ends (a longer statement is written in several calls, still under the lock). Nothing allocates. In
kernel mode the lock also masks interrupts on its processor, so an interrupt handler may print, and a
console function registered with `rt.SetConsole` runs with them masked and must not wait for another
puroutine. A statement that prints a single value writes it at once, without the lock. The messages of a
panic bypass the lock and the buffer, so that a panic can always report itself.

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

### 8.2 Deferred calls on a panic

In hosted builds a panic runs the panicking task's pending deferred calls before the panic handler is
called, as Go runs the deferred calls of a goroutine whose panic nothing recovers:

```go
w := bufio.NewWriter(os.Stdout)
defer w.Flush()            // runs: the output written so far is not lost
defer os.RemoveAll(tmp)    // runs
process(items)             // panics on bad input
```

- **Which calls.** Every deferred call armed in a frame of the task that panicked, innermost frame
  first and, within a frame, the last deferred first, with the arguments evaluated at its `defer`
  statement. A `defer` in the body of a range-over-func loop belongs to the function the loop is in
  (section 4.10). Deferred calls of other tasks do not run; those tasks keep running meanwhile, so a
  deferred `Unlock` lets a task waiting for the lock go on. If one of them panics in the meantime, it may
  end the program first, as in Go.
- **Nothing else changes.** Control never returns to a function that panicked: there is no `recover`.
  Owners are not dropped (section 6.4), and a function's result variables are not returned anywhere.
- **Which panics.** `panic(v)`, the runtime errors of section 8, and the faults the runtime turns into
  panics (a nil pointer dereference, an integer division fault). For a fault, the runtime leaves the
  signal handler and runs the deferred calls on the faulting task's own stack. A panic runs no deferred
  calls after a stack overflow, which leaves no stack to run them on (Go also reports one without
  them); after a heap error the debug allocator reports (section 7.3) or a stop by `rt.Abort` or
  `rt.PanicFault`, after which the program's state is not to be trusted; while the task has preemption
  disabled, which means it holds a runtime lock or is inside the scheduler, where a deferred call could
  wait for good; and outside any task (a scheduler's own context).
- **A panic in a deferred call** that runs for a panic goes straight to the panic handler: the deferred
  calls after it do not run. The new panic's `rt.Panic.During` points to the first, and the built-in
  handler prints both messages as Go does, then the trace from the second panic through the deferred
  call into the frames where the first one happened:

  ```
  panic: runtime error: integer divide by zero
  	panic: close failed
      main.(*conn).Close (./main.p:13)
      main.work (./main.p:20)
      main.main (./main.p:26)
  ```

  A deferred call that calls `rt.Goexit` while deferred calls run for a panic stops them too, and the
  program ends with the panic.
- **What the handler sees.** The panic as it happened: `Message` (copied out of the runtime's buffer
  before the deferred calls run), `Value`, `Fault`, and a trace that starts where the panic happened,
  not in the deferred calls, since those ran below the frames it describes and left them as they were.
  A program's own `//pu:panic` handler is called in the same way, after the deferred calls; it does not
  run them itself.

Freestanding and kernel builds keep the panic as it was: no deferred calls run, since the handler must
work from any state, with interrupts disabled or a lock held.

**How.** A function with `defer` statements keeps, in hosted builds, the bits that say which of its
deferred calls are armed and the values each `defer` statement saved in one slot of its frame, the defer
record, which the compiler never moves into registers. For each such function it also generates a small
function that runs the armed calls of a record, last first. The trace table lists, for each such
function, where its record lies in the frame and that function (debugging.md section 5); the runtime walks
the panicking task's frames as it does for a stack trace and runs each frame's armed calls. A deferred
call clears its bit before it runs, on a normal return too, so a deferred call that panics on the way out
of a return is not run again. Code that does not panic pays a store when each `defer` statement runs and
another when each deferred call runs, and functions with `defer` statements are not inlined (Go does not
inline them either); a function without `defer` pays nothing.

### 8.3 Ending a task with `rt.Goexit`

`rt.Goexit()` ends the calling task, as Go's `runtime.Goexit` does: every function on the task's stack,
innermost first, runs its deferred calls and drops what it holds, as if it returned at the call it is in,
and then the task ends. It is not a panic: no handler is called, and other tasks are not affected.

- **In a deferred call.** A deferred call that calls `Goexit` ends its own frames; the deferred calls
  after it in the frame that deferred it still run, and the task ends afterwards, as in Go.
- **In the task that runs `main`.** `main` does not return: its deferred calls run, the other tasks go on,
  and once none of them is left (or all wait for good) the program stops with "fatal error: no
  puroutines (main called rt.Goexit) - deadlock!" and status 2, as Go's does.
- **Tests.** `testing`'s `FailNow`, and `Fatal`, `Fatalf`, `SkipNow`, `Skip` and `Skipf`, call
  `Goexit`, so a test's deferred calls run when it stops early. The test runner stops the exit with
  `rt.CatchGoexit(f func()) bool`, which calls `f` and reports whether it ended with `Goexit`; the next
  test then runs on the same task.
- **C.** `Goexit` cannot leave a function that C code called (`//pu:export`): the call panics at the
  boundary with "rt.Goexit cannot leave a function called from C".

`Goexit` is not an unwinder. It sets a flag in the task and returns; after every call that may end
with `Goexit`, the compiler checks the flag and, if it is set, leaves the function through its ordinary
return path, which runs its deferred calls and drops its variables exactly as a `return` there would, and
sets the flag again for its caller. The check is made only after calls that may reach `Goexit`, which
the compiler works out from the whole program: a call of a function that calls `Goexit` or a function
that may, and a call through a func value or an interface that may reach one: a function literal or a
function whose value the program takes, of an identical type, or a method of the same name and type, with
type parameters matching any type. A program that never calls `Goexit` has no checks; in one that does,
code that cannot reach it has none. The match is conservative where generic code is involved, so a test
binary checks after some calls in the library (in `pluto/fmt`, through generic iterators) that never
reach `Goexit`; a check costs a call and a branch.

Since every frame leaves through its return path, a frame that holds a `sync.Scope` (scoped tasks, the
scoped-puroutines design note) joins its tasks there, as on any return: `Goexit` needs no list of scopes
to wait for. That list (`rt.WaitScopes`) remains for kernels that abandon a task's stack without
returning.

## 9. Directives

Directives are line comments starting with `//pu:` with no space, placed in the doc comment of the
declaration they apply to (or at the top of the file for `//pu:build`).

A directive states something the compiler cannot work out from the program: linkage, hardware state, a
binding to the runtime, a body it cannot see, or an explicit request. Anything it can work out it infers
instead (which functions never return, which may wait, which start puroutines with their func
arguments, which string slices fall on rune boundaries), so using the language does not require
knowing how the compiler works. Ordinary hosted code needs only the requests: `//pu:build`,
`//pu:inline`, `//pu:noinline`, `//pu:stack`, and `//pu:extern` and `//pu:export` for C. The hardware and
binding directives (`//pu:interrupt`, `//pu:panic`, `//pu:scheduler`, `//pu:percpu`, `//pu:fpu`,
`//pu:blocking`) are for kernels.

A `//pu:` comment that the compiler would not act on is an error, so that a misspelt directive does not
silently change nothing: an unknown name (E2600, with the closest known name suggested), a directive that
was removed because the compiler now works it out (E2601: `alloc`, `bytewise`, `gofunc`, `schedhook`,
`schedlate`, and `noreturn` on a function with a body), a directive in a place it does not apply to (E2602:
not in the doc comment of the right kind of declaration, or `//pu:build` after the package clause), and an
argument a directive does not take or lacks (E2603). A note that is not a directive starts with `// `.

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:build expr` | file | include the file only when `expr` holds |
| `//pu:inline` | function | at `-O2`, inline calls to this function whatever its size, as long as the caller stays within its growth limit (four times its own size, at least 5,000 and at most 20,000 IR values); recursive calls, calls on paths that panic, calls through func values and calls of a function that never returns stay out of line. `pluto build --inlinenotes` prints `file:line:col: note: call to F marked //pu:inline was not inlined: reason` for each call in the program's own packages that stays out of line |
| `//pu:noinline` | function | never inline it |
| `//pu:nobounds` | function | omit bounds checks in its body |
| `//pu:extern name` | body-less function | the function is the external symbol `name` (C ABI) |
| `//pu:export name` | function | also export the function under the symbol `name` |
| `//pu:section name` | function or package variable | place it in the named section |
| `//pu:interrupt` | function | an x86-64 interrupt handler: saves every register it may change, returns with `iretq` (section 12.4) |
| `//pu:noreturn` | body-less function | the assembly or C function never returns (lets the optimiser drop code after calls). A function with a body needs no directive: the compiler finds that it never returns when it has no return statement and its body ends in a terminating statement (Go's definition), counting a call of a function that never returns as one, so a panic, an endless loop and a call of `os.Exit` all count, through any number of calls |
| `//pu:noescape` | body-less function | keeps no pointer argument past the call, except by storing it where another argument points (section 6.3) |
| `//pu:resultholds p` | function returning an owner, in the standard library only (E3008 elsewhere) | keeps parameter `p` only while the result lives, and stores `p` in it; the call counts as returning `p` (section 6.3). The compiler checks that the result holds `p`; that its `Drop` gives `p` back is the author's promise |
| `//pu:panic` | function | the program's panic handler, `func(p *rt.Panic)`; exactly one in freestanding and kernel builds (section 8.1) |
| `//pu:blocking` | function (with or without a body) | kernel code and assembly: calling it may wait in a way the compiler cannot see (a kernel's spinning lock, a routine that halts until an interrupt); interrupt handlers and the panic handler must not reach it. Waits the compiler can see need no directive: it follows calls into the runtime's `park` (section 12.6) |
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
`unsafe.String` makes an uncounted string that views its bytes, which must be valid UTF-8 and outlive
every copy of the string (section 6.10). Reading or writing a string's words through `unsafe` bypasses
its count.

Plutonium adds `unsafe.Drop(p *T)`, which destroys `*p` as the end of its owner's life would: `T`'s `Drop`
method, then its fields (section 6.4). It is what the unchecked layer (`alloc.Free`, a container dropping
its elements) is written with; nothing stops `*p` from being used or dropped again afterwards.

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

**Stacks of hosted puroutines.** A puroutine that a `go` statement starts in a hosted build gets 64 MiB
of address space for its stack (`rt.HostedStackSize`), with a guard region of 1 MiB below it, mapped
without reserving memory: a page of the stack takes memory only once the stack has touched it. About two
million such stacks fit in the 47-bit address space of x86-64 Linux, but each stack is two mappings, so
Linux's limit on the number of mappings (`vm.max_map_count`: 65530 by the kernel's default, often raised
by distributions) allows at most half as many puroutines at once. Under an address-space limit (`RLIMIT_AS`, `ulimit -v`), the stacks may take half of it at full size;
after that each new one is halved until it fits what is left of that half, down to 64 KiB, so many
puroutines still fit, each with a smaller stack. Stacks are reused for later puroutines; the memory a
puroutine committed deep in its stack goes back to the system before the stack is reused if the
runtime saw the puroutine there (when it parked or was preempted), and in any case once every 16 uses.

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
- call a function that may park the caller, such as `pluto/sync`'s `Mutex.Lock` and `WaitGroup.Wait`,
  `time.Sleep` or `rt.Park`, or a function a program marks `//pu:blocking` (below);
- start a puroutine (`go`, `rt.Go`, `sync.WaitGroup.Go`), which allocates a stack and takes the
  scheduler's locks;
- allocate from an allocator that may wait (below).

Operations that do not wait are allowed: `select` with `default`, `TryLock`, readying a parked
puroutine, arming, stopping and running the runtime's timers (`rt.Timer`, `rt.RunTimers`), atomics, and
spin locks that disable interrupts while held. To hand work to normal code, a
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
  `new`, `make`, interface conversions and panics, into the standard library as into the program's own
  code. A path ends at the runtime's `park`, which every wait in the library ends in, at the runtime's
  `spawn`, which starts every puroutine, and at a function marked `//pu:blocking`; a path from an
  interrupt handler stops at the panic handler, which is checked on its own. The message names the path
  as far as the call that enters the standard library (`keyboard calls readConfig (irq.p:5), which waits
  on cfgMutex (config.p:22)`), not how the library goes on to wait.
- **`//pu:blocking`** is for what the compiler cannot see: a kernel's own primitive that waits for
  another processor (a lock that spins until it is released, a routine that halts until an interrupt),
  and assembly or C that waits. The standard library marks only `park`.
- **Calls through interfaces and func values** cannot be resolved exactly, so the compiler is
  conservative, using what it knows about the whole program. A call of method `m` through an interface
  type `I` may reach `m` of every type the program converts to `I`, whether directly, from another
  interface, or by a type assertion (the method tables the program contains). A call through a func value
  of type `F` may reach every function of type `F` whose value the program takes anywhere (as a func
  value, a method value or a method expression, or a function literal). The message says when a path goes
  through such a call. To make a handler's call exact, call the function directly.
- **Allocation.** An allocator is not marked as a whole: `new` and `make` call the allocator's `Alloc`
  through the `alloc.Allocator` interface, and the check follows that call into every allocator the
  program installs. An allocator whose `Alloc` or `Free` may wait, because it takes a lock that parks
  (a slab cache refilled under a `sync.Mutex`), is reported like any other function that may wait; one
  that uses a spin lock with interrupts disabled, or none, is allowed. So no allocator needs a marking of
  its own. (The alternative, forbidding all
  allocation in interrupt context unless an allocator is marked safe, was rejected: it would forbid the
  common, safe case of a lock-free per-CPU cache, and the marking could be wrong without the compiler
  noticing.)
- **Assembly and C** are not looked into: a body-less function counts as not waiting unless its
  declaration is marked `//pu:blocking`.
- **Non-blocking channel operations** lower to runtime functions that never reach `park` (`selectnb`,
  `closechan`), so that a `select` with `default` stays allowed; the waiting forms call ones that do
  (`chansend1`, `chanrecv1`, `chanrecv2`, `selectgo`).
- **The scheduler's `Wake`.** `rt.Ready` calls the bound scheduler's `Wake` (section 4.7), and interrupt
  handlers may call `rt.Ready`, so `Wake` is checked as a root too, with the message "is the
  scheduler's Wake, which interrupt handlers reach through rt.Ready".

### 12.7 Allocation in kernel mode

Kernel code allocates as other code does: `new`, `make`, the `pluto/alloc` functions and the operations
that make strings (`+` and `+=` on strings that are not constants, `strings.FromUTF8Lossy(b)`, `string(runes)`,
`string(r)` for a rune that is not a constant, and library functions that
return new strings, such as `fmt.Sprintf` and `strconv.Itoa`) all take memory from the current
allocator, and no function needs a marking to use them. Where allocating is not allowed is decided by
what the allocator does, not by which operation calls it:

- **In interrupt context.** Whatever an interrupt handler, the panic handler or the scheduler's `Wake`
  can reach must not wait (section 12.6), and the compiler follows every allocation, a string
  concatenation as much as a `new`, into the `Alloc` method of every allocator the program installs.
  An allocator that may wait (one that takes a `sync.Mutex`) is reported on the path that allocates; one
  that does not (a per-CPU cache, a spin lock with interrupts disabled) may be used there.
- **Before an allocator is set.** A kernel starts without a default allocator. Allocating before
  `alloc.SetDefault` panics when it runs, for strings ("no allocator: call alloc.SetDefault before
  building strings") as for `new` and `make`.
- **When memory runs out.** The allocator returns nil and the operation panics with "out of memory".

Everything about strings that does not make a new one never allocates: literals and constants,
copying, comparison, slicing, indexing, `len`, `range`, passing and returning, `unsafe.String`, and the
`Append` functions with a `vec.Fixed[byte]` destination over a caller's array (`fmt.Appendf`,
`strconv.AppendInt`, section 13.2). Copying and dropping a counted string made elsewhere is allowed, and a
drop may free a buffer: a kernel that cannot free in some context keeps the strings it uses there static or
made in an arena.

A generic function is judged for each set of type arguments it is compiled with, since each is a function
of its own: `strconv.AppendInt` into a `*vec.Fixed[byte]` never allocates, so an interrupt handler may
call it while other code appends to a `*vec.Vec[byte]`, which grows from an allocator that may wait.
`fmt.Appendf` reaches a destination other than a `vec.Fixed` through an `io.Writer`, as `Fprintf` does,
so a handler that formats with it counts as calling the `Write` method of every `io.Writer` the program
makes, a growing `vec.Vec` destination's among them; in a handler of such a program, format with the
`strconv` functions into a `vec.Fixed`.

An earlier design required kernel functions that make strings to be marked `//pu:alloc`, to make the
allocation visible. It was retired once the check of section 12.6 followed allocation into the
allocator, which tells the places where allocating is wrong from the places where it is fine, while the
marking only said that a function allocates.

## 13. Standard library

The library's containers, buffers and files are owners (section 6.4); their `Drop` methods free what they
hold, and most have a `Clone`.

| Package | Contents |
|---|---|
| `pluto/rt` | panic hook, program entry, memory copy and compare primitives the compiler calls, the scheduler contract (`Scheduler`, `Task`, `Switch`, `Park`, `Ready`, `Preempt`), timers (`Now`, `Timer`, `ParkUntil`, `RunTimers`), `Go`, `Yield`, the hosted scheduler |
| `pluto/sync` | `Mutex`, `RWMutex`, `WaitGroup` (with `Go`), `Once`, `Cond` over the scheduler; interrupt handlers must not reach the operations that park |
| `pluto/alloc` | allocators, the default allocator, `Use`/`Restore`, `Owned`/`Shared`/`Synced`, and the unchecked layer `Raw`/`New`/`Free` (section 7.1) |
| `pluto/alloc/debug` | the checking allocator (section 7.3) |
| `pluto/arena` | bump allocators; freeing into one does nothing, and `Reset` reclaims everything (section 6.6) |
| `pluto/fmt` | `Printf`, `Fprintf`, `Sprintf`, `Sprint` and `Sprintln` returning a string, `Appendf`, `Append` and `Appendln` appending to a `vec.Vec[byte]` or `vec.Fixed[byte]` (section 13.2), `Println`; integer, float, string, pointer verbs with width, precision and flags; arrays, slices, structs, pointers, interfaces and enums with `%v`, `%+v` and `%#v` as Go prints them, read from type descriptors (section 4.4), and containers through `fmt.Mapper` (`hash.Map` prints as `map[k:v]` with sorted keys) and `fmt.Lister` (`vec.Vec` prints as a slice); formatting goes through a fixed buffer, and only the `S` functions allocate, for their result, and the `Append` functions to grow a `vec.Vec`; `Errorf` and `ErrorBuf` (section 14) |
| `pluto/container` | the constraints of the indexing interface (section 6.7): `Indexable`, `Reader`, `Lookup`, `Sized` |
| `pluto/container/heap` | Go's `container/heap` (`Init`, `Push`, `Pop`, `Remove`, `Fix` over a `heap.Interface`), and `Heap[T]`, a min-heap in a `vec.Vec` ordered by a less function, without boxing elements in `any` |
| `pluto/io` | `Reader`, `Writer`, buffered I/O |
| `pluto/os` | files (`os.File`, an owner that closes on drop), `Stat`, whole-file reads and writes, directories (`ReadDir` returns a `DirEntries` owner), removal, renaming, `Readlink`, temporary directories, the working directory, pipes, processes, arguments, environment, exit (hosted). Names and paths come back as strings with invalid bytes replaced, next to exact bytes (`DirEntry.RawName`, `ArgBytes`, `AppendWd`), and each core operation has a `Bytes` form that takes a `[]byte` path (`OpenBytes`, `StatBytes`, `RemoveBytes`, ...); see "Names from the operating system" below |
| `pluto/os/exec` | running commands: `Cmd` (an owner), pipes copied inside `Wait`, `ExitError` in the data word (hosted) |
| `pluto/path/filepath` | slash-separated paths: substrings (`Base`, `Ext`, `Split`), `Join`, `Clean`, `Dir`, `Rel` and `Abs` returning strings, with `Append` forms (section 13.2), `Match`, `WalkDir` (paths as strings with invalid bytes replaced) and `WalkDirBytes` (the exact bytes) |
| `pluto/time` | `Duration`, `Time` in UTC with a monotonic reading, `Now`, `Since`; `Sleep`, `Timer`, `Ticker`, `After`, `Tick` and `AfterFunc` over the scheduler's timers (section 4.7) |
| `pluto/flag` | command-line flags; a `FlagSet` owns its flags and their variables; `flag.Arg` is a string with invalid bytes replaced, `flag.ArgBytes` the exact bytes |
| `pluto/syscall` | raw Linux system calls (hosted) |
| `pluto/strconv` | `Itoa`, `FormatInt`, `FormatFloat`, `Quote` returning strings, with `Append` forms (section 13.2); `ParseUint`, `ParseInt`, `Atoi`, `ParseFloat` (decimal and hexadecimal, any rounding case, exact as Go's), `PutUint`, `IsPrint`, `CanBackquote`; a failed parse returns `ErrSyntax` or `ErrRange` itself, and `NewNumError` makes Go's `*NumError` from an allocator when a caller wants one |
| `pluto/strings`, `pluto/bytes` | `strings.Builder`, which builds a string in a buffer it shares with the result (section 6.10); `bytes.Buffer`, Go's growing buffer, an owner over a `vec.Vec[byte]`; searching, comparison, case conversion, `Join`, `Repeat`, `Replace`, `FromUTF8`, with `Append` forms of the functions that make strings (section 13.2); Go 1.24's iterators `Lines`, `SplitSeq`, `SplitAfterSeq`, `FieldsSeq`, `FieldsFuncSeq` |
| `pluto/iter` | `Seq` and `Seq2` (section 4.10), and adaptors that allocate nothing: `Filter`, `Map`, `Limit`, `Concat` (each with a `2` form), `Enumerate`, `Keys`, `Values`, `Reduce`, `Count`, `Fill`. Go's `Pull` needs a coroutine per iterator and has no equivalent |
| `pluto/bits` | bit operations (section 5.8) |
| `pluto/math` | floating-point functions |
| `pluto/hash` | `Map[K, V]` and `Set[K]`, owners that record their allocator; `All`, `Keys` and `Values` iterate in table order, the same for the same changes and seed |
| `pluto/vec` | `Vec[T]`, a growable owner that records its allocator, with `Push`, `Append`, `Insert`, `Replace` and `AppendSeq`; `All`, `Values` and `Backward` iterate it, reading its length once as range over a slice does (section 6.7); `Fixed[T]`, a bounded vector over a caller's array, the `Appender[T]` constraint both satisfy, and `AppendString` (section 13.2) |
| `pluto/sort` | sorting without allocation |
| `pluto/cmp` | Go's `cmp`: `Ordered`, `Less`, `Compare`, `Or` |
| `pluto/slices` | Go's `slices`: `Sort`, `SortFunc`, `SortStableFunc` (pdqsort and in-place merge sort, three-way comparison functions), `BinarySearch`, `Index`, `Contains`, `Equal`, `Compare`, `Max`, `Min`, `IsSorted`, `Reverse`, `Delete`, `DeleteFunc`, `Compact`, `Clip`, and the iterators `All`, `Values`, `Backward`, `Chunk`; `Insert`, `Replace` and `AppendSeq`, which grow, are `vec.Vec`'s methods, and `Clone`, `Concat`, `Repeat`, `Collect`, `Sorted`, `SortedFunc`, `SortedStableFunc` (each with an `In` form taking an allocator) return a `vec.Vec` |
| `pluto/sync/atomic` | atomic integers and pointers |
| `pluto/unicode` | Go's `unicode` with Go 1.27's Unicode 17.0.0 tables (generated by `tools/mkunicode`): `IsLetter`, `IsDigit`, `IsSpace`, `IsUpper`, `IsLower`, `IsPunct`, `IsPrint`, `IsGraphic`, `ToUpper`, `ToLower`, `ToTitle`, `SimpleFold`, `Is` and `In` over `RangeTable`s; a variable for each category, script and property table, and no maps from names to tables |
| `pluto/unicode/utf8` | UTF-8 encoding and decoding; `AppendRune` (section 13.2) |
| `pluto/testing` | tests and benchmarks for `pluto test` (hosted); `FailNow`, `Fatal`, `SkipNow` and their relatives end the test function with `rt.Goexit`, so its deferred calls run (section 8.3) |
| `pluto/errors` | `New` (a static error per call site), `Is`, `As`, `AsType`, `Unwrap`, `Join`, `Free` (section 14) |

`pluto/os` and `pluto/syscall` are not available in freestanding builds.

### 13.1 Names from the operating system

A string holds only valid UTF-8 (section 6.10), but the system does not promise it: a Linux file name is
any bytes except NUL and `/`, and so is a symbolic link's target, the working directory, a command-line
argument and an environment variable. The library therefore follows one rule:

**Bytes that the operating system hands back become a `string` only through validation, which replaces each
byte that does not begin a valid UTF-8 sequence with U+FFFD, as `strings.FromUTF8Lossy(b)` does.** A name that is valid
UTF-8 comes back as it is; no other string is made from external bytes without being checked. The
conversion is lossy, as Rust's `to_string_lossy`. Two different files can give the same string, and a lossy
name does not name the file it came from, so it must not be used to open, rename or remove that file.

The exact bytes stay available next to every string that can be lossy:

| String | Exact bytes |
|---|---|
| `DirEntry.Name`, `FileInfo.Name` of an entry | `DirEntry.RawName() []byte` |
| `os.Readlink(name)` | `os.AppendReadlink(&b, name)`, `os.ReadlinkBytes(&b, name)` |
| `os.Getwd()`, `filepath.Abs` | `os.AppendWd(&b)`, `filepath.AppendAbs(&b, path)` |
| `os.Arg(i)`, `os.Args()`, `flag.Arg(i)` | `os.ArgBytes(i)`, `os.ArgsBytes()`, `flag.ArgBytes(i)` |
| `os.Getenv`, `os.LookupEnv`, `os.Environ()`, `os.TempDir()` | `os.GetenvBytes`, `os.LookupEnvBytes`, `os.EnvironBytes()`, `os.TempDirBytes()` |
| the `path` of `filepath.WalkDir` | `filepath.WalkDirBytes`, whose function takes `path []byte` |
| `os.MkdirTemp`, `exec.LookPath` | `os.AppendMkdirTemp`, `exec.AppendLookPath` |
| `PathError.Path`, `File.Name` | none: the caller has the bytes it passed |

The views (`RawName`, `ArgBytes`, `GetenvBytes`) are valid as long as what they view: the `DirEntries`
that holds the entry, or the life of the program for the process's arguments and environment.

The core operations on a path have a form that takes the path as a `[]byte`, named by the suffix `Bytes`:
`OpenBytes`, `OpenFileBytes`, `CreateBytes`, `StatBytes`, `LstatBytes`, `ReadDirBytes`, `ReadlinkBytes`,
`ReadFileBytes`, `WriteFileBytes`, `MkdirBytes`, `MkdirAllBytes`, `RemoveBytes`, `RemoveAllBytes`,
`RenameBytes`, `ChmodBytes` and `ChdirBytes`. A program that must handle any file writes `os.OpenBytes(e.RawName())`
(joined to the directory) where it would write `os.Open(e.Name())`. The string forms remain the normal API.
`ReadDir` sorts by the exact bytes of the names, as Go's does; `os/exec` passes the process's environment
to a child unchanged. The runtime's own reads of the environment (`PLUTO_MAXPROCS`, `PLUTO_DEBUGALLOC`)
ignore a value that is not valid UTF-8, and the trace table the compiler writes has file names made valid.

`unsafe.String` stays in the library only where the bytes are valid by construction (literals, ASCII
formatting output, text the library validated or built from valid strings) or are used inside a package as
bytes and not returned as a string; each use has a comment that says which.

### 13.2 Append functions

The library's `Append` functions write text into a destination the caller declares, whose type says
whether it grows. They keep Go's names and take the destination by pointer, generic over
`vec.Appender[byte]`:

```go
type Appender[T any] interface {
	Push(x T)
	Append(xs ...T)
	Len() int
	Slice() []T
	Truncate(n int)
}
```

- A `*vec.Vec[byte]` grows from its allocator, as Go's `[]byte` destination does.
- A `*vec.Fixed[byte]` is a bounded vector over the caller's array (`vec.NewFixed(mem[:])`, whose length
  is the room). It never allocates and never panics when full: `Push` and `Append` store what fits, drop
  the rest and record it, and `Truncated()` reports it; `TryPush` refuses instead, for containers that must
  not drop elements, and `Reset` empties it and clears the record. A `Fixed` holds a view of the array and
  owns nothing, so it copies freely and must not outlive the array (section 6.3).

```go
v := vec.New[byte]()            // grows: Go's behavior
defer v.Free()
strconv.AppendInt(&v, n, 10)
v.Push(' ')
strconv.AppendQuote(&v, name)

var mem [64]byte
f := vec.NewFixed(mem[:])       // bounded: no allocation, kernel safe
fmt.Appendf(&f, "%d %q", n, name)
console.Write(f.Slice())
```

The functions return nothing, or only their error: `strconv.AppendInt`, `AppendUint`, `AppendBool`,
`AppendFloat`, `AppendQuote`; `utf8.AppendRune`; `strings.AppendUpper`, `AppendLower`, `AppendRepeat`,
`AppendJoin`, `AppendReplace`; `fmt.Appendf`, `Append`, `Appendln`; `time.Duration.AppendString`,
`time.Time.AppendRFC3339`, `os.FileMode.AppendString`; `filepath.AppendClean`, `AppendJoin`, `AppendDir`,
and `AppendAbs` and `AppendRel`, which return an error; and `vec.AppendString`, which appends a string's
bytes (`append(b, s...)` in Go). The functions that must not store a partial path, `os.AppendWd`,
`AppendReadlink`, `ReadlinkBytes`, `AppendMkdirTemp` and `exec.AppendLookPath`, leave the destination as
it was and return `ENAMETOOLONG` (`ERANGE` for `AppendWd`) when the path does not fit in a `vec.Fixed`.
Each function is compiled once for each destination type a program uses with it, so its calls are direct.

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

An error that records context (a path, a line and column) needs storage for it. Its text is a string
(section 6.10): `fmt.Errorf` formats its message into a string when the error is made, as `fmt.Sprintf`
does, and `*os.PathError` keeps the operation and the path as strings and builds its message when
`Error()` is called, as in Go. The error object holding those strings is what needs a home.

- **Allocated.** `fmt.Errorf(format, args...)` allocates the error from the default allocator, and so does
  `errors.Join`; both are variadic, so they cannot take a defaulted allocator (section 5.1), and
  `alloc.Use` installs another allocator around them. Freeing the error releases its strings.
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

`os.File.Read`, `Write` and `Close` report a bare `syscall.Errno` (and `io.EOF`, and `os.ErrClosed` for a
closed file), not a `*PathError`, so that I/O does not allocate. `strconv` reports `ErrSyntax` and `ErrRange` without Go's `*NumError`. An
`Errno` the table does not know reports `"unknown error"`.

### 14.6 Allocated errors and ownership (approved; not implemented yet)

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
8. The text in an error is a string (section 6.10): a message, an operation, a path. An error object is
   then a small struct of strings and wrapped errors (`struct{ msg string; err error }`), and its `Drop`
   needs no code of its own for the text: the drop glue releases the strings, and a message that is a
   substring of another, or a path the caller still holds, is shared rather than copied. Only the object
   itself, and the errors it wraps, need the counting of points 1 to 4. Before strings were counted, the
   message had to be formatted into the error's own block, and every error type freed its text by hand.

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
