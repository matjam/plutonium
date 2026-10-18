## 20. System considerations

This chapter covers what a program says to the system it runs on: raw memory (package `unsafe`), the
sizes of types, the directives that state facts the compiler cannot see, code written in assembly or C,
the build modes, and the extra rules of kernel mode. Most programs use none of it. Kernels and the
libraries below them use all of it.

### 20.1 Package `unsafe`

The built-in package `unsafe`, known to the compiler and imported by the path `"unsafe"`, gives access to
raw memory and to operations that bypass the type system and the ownership rules. A package that uses it
must be reviewed by hand for memory safety. The package has this interface:

```go
package unsafe

type ArbitraryType int // shorthand for an arbitrary type; it is not a real type
type Pointer *ArbitraryType

func Alignof(variable ArbitraryType) uintptr
func Offsetof(selector ArbitraryType) uintptr
func Sizeof(variable ArbitraryType) uintptr

type IntegerType int // shorthand for an integer type; it is not a real type
func Add(ptr Pointer, len IntegerType) Pointer
func Slice(ptr *ArbitraryType, len IntegerType) []ArbitraryType
func SliceData(slice []ArbitraryType) *ArbitraryType
func String(ptr *byte, len IntegerType) string
func StringData(str string) *byte

func Drop(p *ArbitraryType) // drops *p
```

**Pointer.** A `Pointer` is a pointer type, but a `Pointer` value may not be dereferenced. Any pointer
type, any value of underlying type `uintptr`, and any `owned[T]`, `shared[T]` or `synced[T]` can be
converted to `Pointer`, and a `Pointer` can be converted to any pointer type and to `uintptr`. If the
types involved are type parameters, all types in their type sets must have the same underlying type,
which must be `uintptr` and `Pointer` respectively. Converting a box to `Pointer` gives the address of
its object and moves nothing; the box keeps owning the object (section 7.19, `owned`, `shared`, `synced`
and `weak`).

Converting between `Pointer` and `uintptr` converts between an address and an integer holding it. There
is no garbage collector and memory never moves, so an address stays valid exactly as long as the object
it names, and the conversions have no further restriction. The program is responsible for the rest: the address must name an object of the type it is converted to,
suitably aligned, that is alive whenever the result is used.

```go
var f float64
bits := *(*uint64)(unsafe.Pointer(&f)) // the bits of f

lapic := (*volatile[uint32])(unsafe.Pointer(uintptr(0xFEE000B0))) // a device register
```

A `Pointer` is a view (section 18.3, Views). A `Pointer` made from a `uintptr` is assumed to point
outside every frame and to memory that nobody in the program frees, so the escape rules of section 18.3
do not follow it. A pointer converted to `uintptr` loses any generation tag it carries (section 18.11,
Generation checks), and a pointer made from an integer carries none, so accesses through it are not
checked.

**Sizes and offsets.** `Alignof` and `Sizeof` take an expression `x` of any type and return the
alignment or size of a hypothetical variable `v` declared as `var v = x`. The argument is not evaluated: it contributes its type only, so it moves
nothing, calls nothing and cannot cause a drop (the `Alignof(*p)` of the `unsafe.Drop` example is valid
for an owner `T`). `Offsetof` takes a selector
`s.f` that denotes a field `f` of the struct denoted by `s` or `*s`, and returns the field's offset in
bytes relative to the struct's address. If `f` is an embedded field, it must be reachable without
pointer indirections through fields of the struct. For a struct `s` with field `f`:

```go
uintptr(unsafe.Pointer(&s)) + unsafe.Offsetof(s.f) == uintptr(unsafe.Pointer(&s.f))
```

For a variable `x`, `uintptr(unsafe.Pointer(&x)) % unsafe.Alignof(x) == 0`.

A type `T` has **variable size** if it is a type parameter, or an array or struct type containing
elements or fields of variable size. Otherwise its size is **constant**. Calls of `Alignof`, `Offsetof`
and `Sizeof` are constant expressions of type `uintptr` if their arguments (or the struct `s` in the
selector `s.f` for `Offsetof`) are of constant size. The sizes and alignments themselves are the subject
of section 20.2.

**Add.** `Add(ptr, len)` adds `len` to `ptr` and returns the updated pointer
`unsafe.Pointer(uintptr(ptr) + uintptr(len))`. The `len` argument must be of integer type or an untyped
constant. A constant `len` must be representable by a value of type `int`; if it is an untyped constant
it is given type `int`.

**Slice.** `Slice(ptr, len)` returns a slice whose underlying array starts at `ptr` and whose length and
capacity are `len`. It is equivalent to `(*[len]ArbitraryType)(unsafe.Pointer(ptr))[:]`, except that if
`ptr` is `nil` and `len` is zero, the result is `nil`. The `len` argument must be of integer type or an
untyped constant. A constant `len` must be non-negative and representable by a value of type `int`; if
it is an untyped constant it is given type `int`. At run time, if `len` is negative, or if `ptr` is `nil`
and `len` is not zero, a run-time panic occurs (section 17.1, Panics). The slice is a view of the memory
at `ptr`; nothing checks that the memory holds `len` elements.

**SliceData.** `SliceData(slice)` returns a pointer to the underlying array of `slice`. If the slice's
capacity `cap(slice)` is not zero, that pointer is `&slice[:1][0]`. If `slice` is `nil`, the result is
`nil`. Otherwise it is a non-`nil` pointer to an unspecified memory address.

**String.** `String(ptr, len)` returns a `string` whose bytes start at `ptr` and whose length is `len`.
The requirements on `ptr` and `len` are those of `Slice`. If `len` is zero, the result is the empty
string. The string is a view: it is not counted (section 18.8, Strings), so it owns nothing and frees
nothing. The program must ensure that the bytes are valid UTF-8, because every string holds valid UTF-8
(section 7.3, String types), that nothing modifies them, and that they outlive every copy of the string.
If the bytes are not valid UTF-8, the results of later operations on the string are unspecified.

**StringData.** `StringData(str)` returns a pointer to the underlying bytes of `str`. For an empty string
the result is unspecified and may be `nil`. The bytes must not be modified. The pointer holds no
reference: the string must stay alive for as long as the pointer is used. Reading or writing the words of
a string through `unsafe` bypasses its count (section 18.8).

**Drop.** `Drop(p)` drops `*p` as the end of its owner's life would: it calls `T`'s `Drop` method if
there is one, then drops the fields of `*p` in the order of section 18.6 (Drops). It is the operation that
the unchecked allocation layer is written with (`alloc.Free`, a container dropping its elements). Nothing
stops `*p` from being used or dropped again afterwards, and nothing frees the memory: freeing is the
allocator's business.

```go
func release[T owner any](p *T, a alloc.Allocator) {
	unsafe.Drop(p)                                  // run the destructors
	a.Free(unsafe.Pointer(p), unsafe.Sizeof(*p), unsafe.Alignof(*p))
}
```

**What counts as unsafe code.** Everything in package `unsafe` is unsafe code. Section 18.12 (Safe and
unsafe code) lists the rest (the unchecked allocation layer, functions without a body, the directives that
make promises, `rt.Pinned`, `pluto/arch/x86` and memory made from an integer) and states what safe code is
guaranteed. Unsafe code needs no keyword: every use is an import of `unsafe` or `pluto/arch/x86`, a call
into the unchecked layer of `pluto/alloc`, an explicit manual arena operation listed in section 18.12,
a function declaration without a body, a use of `rt.Pinned`, or a `//pu:` directive, so it is found by
searching the imports, the calls, the bodiless declarations and the directives, and the implementation
provides a tool that lists it (section 21.5, Run-time checks and the unchecked mode).

### 20.2 Size and alignment guarantees

The sizes of the basic types are guaranteed:

| Type | Size in bytes |
|---|---|
| `bool`, `byte`, `uint8`, `int8` | 1 |
| `uint16`, `int16` | 2 |
| `uint32`, `int32`, `float32`, `rune` | 4 |
| `uint64`, `int64`, `float64` | 8 |
| `int`, `uint`, `uintptr` | 8 |

`int`, `uint` and `uintptr` are 64 bits wide on every target, by a rule of the language: a program does
not need to ask how wide they are. There are no complex types.

The following minimal alignment properties are guaranteed:

1. For a variable `x` of any type, `unsafe.Alignof(x)` is at least 1.
2. For a variable `x` of struct type, `unsafe.Alignof(x)` is the largest of the values
   `unsafe.Alignof(x.f)` for each field `f` of `x`, but at least 1.
3. For a variable `x` of array type, `unsafe.Alignof(x)` is the alignment of a variable of the array's
   element type.

The layout of a struct follows from these rules. Fields appear in declaration order. Each field is placed
at the lowest offset, not less than the end of the previous field, that is a multiple of the field's
alignment. The size of the struct is the end of the last field, rounded up to a multiple of the struct's
alignment. If the last field has size zero and starts at a non-zero offset, one byte of padding is
counted after that field before rounding, so that the address of the last field is never the address of
the next object.

A struct or array type has size zero if it contains no fields (or elements, respectively) that have a
size greater than zero. Two distinct zero-size variables may have the same address in memory.

The other types have these sizes and alignments:

| Type | Size | Alignment |
|---|---|---|
| pointer, `unsafe.Pointer`, func value, channel, map | 1 word | 1 word |
| `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]` | 1 word | 1 word |
| `string`, slice | 3 words | 1 word |
| interface | 2 words | 1 word |
| `[N]T` | `N` times the size of `T` | that of `T` |
| `volatile[T]` | that of `T` | that of `T` |
| `aligned[T, N]` | that of `T`, rounded up to a multiple of `N` | the larger of `N` and that of `T` |
| `ring[T, N]` | 16 bytes plus `N` times the size of `T`, rounded up to its alignment | the larger of 8 and that of `T` |
| packed type | its bit width rounded up to whole bytes | that of its backing type; 1 when the backing is `[]byte`, a byte array or omitted |
| enum type | that of its underlying integer type | that of its underlying integer type |
| union | the tag, then the largest payload at its alignment, rounded up to the alignment | the larger of the tag's and the largest payload's |
| an owner type | that of its underlying type | that of its underlying type |

A word is 8 bytes on the specified targets (section 20.5, Build modes and targets). A string and a slice
are the same size, three words, but a string's third word is not a capacity (section 7.3). The exact
layout of each type on x86-64, including the order of the words of a string, a slice and an interface
and the layout of unions and `ring`, is in the ABI document (abi.md section 1). Programs may depend on
the sizes and alignments in this section. They may not depend on the layout of a union, a string's control
word, or the contents of padding.

### 20.3 Directives

A **directive** is a line comment that starts with `//pu:` and no space. It sits in the doc comment of the
declaration it applies to, or, for `//pu:build`, before the package clause.

The syntax of a directive comment is given in section 4.1 (Comments).

A comment that is only a note starts with `// ` (a slash pair and a space), and is not a directive.

**The rule.** A directive exists only for a fact the compiler cannot derive from the program: linkage, a
body it cannot see, hardware state, a binding to the runtime, a promise it must take on trust, or an
explicit request from the programmer. Anything the compiler can work out from the program is inferred,
and has no directive. The compiler infers which functions never return, which may wait, which start
tasks with their func arguments, which pointers a function
keeps, which lock guards which data, what a closure may reference and what each owner moves. A language
that needed a directive for such a fact would require every programmer to know how the compiler works. The
set of directives below is complete: an implementation defines no others.

Ordinary hosted code needs only the requests: `//pu:build`, `//pu:inline`, `//pu:noinline`,
`//pu:stack`, and `//pu:extern` and `//pu:export` when it binds to C. The hardware and binding directives
are for kernels, where code must be exact about machine state.

**Errors.** A `//pu:` comment that the implementation would not act on is a compile error, so that a
misspelled directive never silently does nothing:

| Code | Error |
|---|---|
| E2600 | an unknown name (the error suggests the closest known name) |
| E2601 | a retired directive: one removed because the compiler now works the fact out, or replaced |
| E2602 | a directive in a place it does not apply to: not in the doc comment of the right kind of declaration, or `//pu:build` after the package clause |
| E2603 | an argument the directive does not take, or lacks |
| E2604 | a line comment that starts with `//go:` and no space |

The retired directives are `alloc`, `bytewise`, `gofunc`, `schedhook` and `schedlate`, which are no
longer needed; `nobounds`, which `nochecks` replaces; and `noreturn` and `noescape` on a function that has
a body, where the compiler infers the fact.

**`//go:` comments.** A line comment that starts with `//go:` and no space is a compile error, reported
with the directive errors above, so that such a comment never sits in a file looking as if it had an
effect. The error names the `//pu:` directive that does the same job where there is one: `//go:build` is `//pu:build`,
`//go:noinline` is `//pu:noinline`, and `//go:noescape` is `//pu:noescape` (for functions without a
body). For the rest (`//go:generate`, `//go:embed`, `//go:linkname`, `//go:nosplit`) the error says that there
is no equivalent.

The directives, in groups by the reason each exists:

**Requests and linkage.** These ask for something the compiler cannot guess.

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:build expr` | file | include the file only when the constraint `expr` holds (section 14.4, Build constraints) |
| `//pu:inline` | function | at the highest optimization level, inline calls of this function whatever its size, as long as the caller stays within its growth limit |
| `//pu:noinline` | function | never inline the function |
| `//pu:stack size` | `func main` | run the program on a stack of `size` bytes; hosted builds only |
| `//pu:section name` | function or package variable | place the object in the section named `name` |
| `//pu:extern name` | function without a body | the function is the external symbol `name`, called with the C convention (section 20.4) |
| `//pu:export [name]` | function | also export the function under the symbol `name`; without a name, only keep the function even if no Plutonium code calls it (section 20.4) |

`//pu:inline` is a request. Recursive calls, calls on paths that panic, calls through func values and
calls of a function that never returns stay out of line, and an implementation can report each call of an
`//pu:inline` function that it did not inline, with the reason. The growth limit keeps a caller from
becoming unreasonably large: an implementation documents it.

`//pu:stack` takes a decimal number with an optional `K`, `M` or `G` suffix (powers of 1024), from 64K to
64G. The hosted runtime reserves that much address space, maps it as the program touches it and places a
guard region of 1 MiB below it, so that an overflow is a fault reported as a panic (section 17.5, Faults).
Without the directive `main` runs on the stack the system gave the process. The directive is an error
when its size is missing or malformed (E2603), on any function other than `main`
of package `main` (E2602), when the size is outside 64K to 64G, and in a freestanding build,
where the program sets up its own stack (E2560). A program that
recurses deeply asks how much stack remains with `rt.StackLeft()`, which returns the bytes between the
caller's frame and the limit of its stack, or `^uintptr(0)` where the limit is unknown.

**Bodies the compiler cannot see.** A function written in assembly or C has no body to read, so the
programmer states what the body would have shown.

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:noreturn` | function without a body | the function never returns, which lets the optimizer drop the code after calls of it |
| `//pu:noescape` | function without a body | the function keeps no pointer argument past the call, except by storing it where another argument points (section 18.3) |

A function with a body needs neither directive, and either one on it is retired (E2601). The compiler
finds that a function never returns when it has no return statement and its body ends in a terminating
statement (section 12.1, Terminating statements), counting a call of a function that never returns as
one, however many calls away; a panic, an endless loop and a call of `os.Exit` all count. This inference
is used for the panic handler check (section 17.4, Panics in freestanding programs) and by the optimizer.
It is not used for the missing-return rule: a function whose last statement is a call of a function that
never returns still needs a `return` or a terminating statement of its own.

**Kernel and binding.** These state facts about hardware or bind the program to the runtime, and are for
kernels.

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:interrupt` | function | an x86-64 interrupt or exception handler, entered through an interrupt gate that disables interrupts: saves every register it may change and returns with `iretq` (section 20.10) |
| `//pu:interrupt trap` | function | the same handler, entered through a trap gate that leaves interrupts as they were, so it can be re-entered; its body is not an interrupt-disabled region (section 20.10) |
| `//pu:panic` | function | the program's panic handler, `func(p *rt.Panic)`; exactly one in a freestanding or kernel build (section 17.4) |
| `//pu:scheduler` | package variable | the program's scheduler backend, of a type that implements `rt.Scheduler`; at most one in a program, replacing the hosted one (section 19.5, The scheduler contract) |
| `//pu:percpu` | package variable | one copy per processor, addressed through `GS`; kernel mode only (section 20.9) |
| `//pu:fpu` | function | may use floats in kernel mode; the caller has saved the FPU state (section 20.7) |
| `//pu:blocking` | function, with or without a body | calling it may park or wait in a way the compiler cannot see (for example, halting until an interrupt, or on a function with a body, spinning until an interrupt handler or another processor sets a flag); interrupt handlers and the panic handler must not reach it (section 20.10). It only adds this restriction and never makes a program accepted |
| `//pu:nosimd` | function | the compiler adds no vector instructions to it |

`trap` is the only argument `//pu:interrupt` takes; any other argument is E2603. `//pu:nosimd` is implied by `//pu:interrupt` (either form) and, in kernel mode, by every function that is not
`//pu:fpu`. `//pu:blocking` is for what the compiler cannot see; waits it can see, which all end in the
runtime's `park`, need no directive. On a function with a body it states a wait on progress that only
another execution context makes, such as a spin until an interrupt handler or another processor sets a
flag. It only adds the restriction of section 20.10. It never makes a program accepted, so it is neither
unsafe code nor a promise taken on trust.

**Standard library only.**

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:resultholds p` | function that returns an owner | the result keeps parameter `p` while it lives and stores `p` in it, so the call counts as returning `p` (section 18.3) |

The compiler checks that the result holds `p` (E3007). That the result's `Drop` gives `p` back is the
author's promise. Outside the standard library the directive is an error (E3008).

**Checks off.**

| Directive | Applies to | Meaning |
|---|---|---|
| `//pu:nochecks` | function | remove the bounds checks and the generation checks from its body |

`//pu:nochecks` is an exceptional request for reviewed low-level use or measured optimization in one
function. It is not a routine remedy for an ownership, borrowing or allocation error. It
states no fact the compiler could derive, which is why it is a directive. It makes the function unsafe
code (section 20.1), so that the audit lists it. Function literals inside the function are part of its
body. The compile-time checks (moves, escapes, races) cannot be turned off: they cost nothing at run
time.

### 20.4 Assembly and C

A function declared without a body is implemented outside Plutonium.

**Assembly.** The implementation is a `.s` file in the same package, in GNU assembler syntax. The symbol of
a Plutonium function is its import path and name, such as `"pluto/syscall.Syscall"`, quoted in assembly
because of the slash. The symbol of a method is `"path.Type.Method"` for a value receiver and
`"path.(*Type).Method"` for a pointer receiver. A function without a body that has no `//pu:extern` is
implemented this way. If no `.s`, `.c` or `.o` file of the program defines the symbol of a function without a body, the link fails (E5001). A `.s` file that the assembler rejects is E5004. Which registers carry the arguments and results is the internal calling convention
(abi.md sections 3 and 8).

**C.** With `//pu:extern name`, the function is the C symbol `name`, called with the System V AMD64
calling convention, so C code compiled separately links in directly. `//pu:export name` makes a
Plutonium function callable from C under `name`. Calls between Plutonium functions use an internal
convention that passes strings, slices, interfaces and small structs in registers; the implementation
translates at the C boundary, classifying structs the System V way and returning several results as a
struct of the results (abi.md section 9.1).

```go
//pu:extern lua_pushinteger
func pushInteger(L *State, n int64)

//pu:export plu_add
func add(a, b int32) int32 { return a + b }
```

These rules apply to the signature of every `//pu:extern` and `//pu:export` function:

- A call of `rt.ExitTask` that would leave a `//pu:export` function panics at the boundary (section 17.3),
  and a panic ends the program as everywhere else (section 17.4).
- The types that may appear in parameters and results are integers, enum types (as their underlying
  integer), `uintptr`, `bool`, `float32` and `float64`, pointers and `unsafe.Pointer`, `owned`, `shared`,
  `synced` and `weak` references (which cross as the address of the object), and structs of these, with
  arrays as fields. `volatile[T]` and `aligned[T, N]` with `N` at most 8 cross
  as `T`. A packed type crosses as its storage.
- Strings, slices, interfaces, func values, maps, channels, rings and unions have no C type and are errors in a
  by-value parameter or result, including inside structs. Pass `unsafe.StringData(s)` and `len(s)`
  instead of a string, and `unsafe.SliceData(b)` and `len(b)` instead of a slice. Behind a pointer, a
  struct that contains slices may be shared with C, with the layout of section 20.2; one that contains
  strings may be read by C but not copied or stored by it, because a copy would hold a reference that the
  string's count does not know about.
- Arrays as a whole parameter or result, zero-size types, types aligned to more than 8 bytes and variadic
  functions are errors; so are generic functions.
- All of these are reported at the declaration even if nothing calls the function (E2511). `//pu:extern`
  on a function with a body is an error (E2510), as are two functions exported under one symbol (E2512)
  and a generic function with `//pu:export` (E2513).
- A method with `//pu:extern` or `//pu:export` takes its receiver as the first C parameter.

**C sources.** `.c` files in a package are compiled with the system C compiler (a file it rejects is E5005) and linked in, and `.o`
files are linked as they are (abi.md section 9.2). Both follow the file-name rules for build tags that
`.p` files follow (section 14.4). There is no C library: C code must not call one, and must provide
`memcpy` and `memset` itself if the C compiler emits calls to them. The full calling convention and data
layouts are in the ABI document, [abi.md](../abi.md).

### 20.5 Build modes and targets

A build has a **mode**, chosen when the program is built. By default the mode follows the target.

| Mode | Meaning | Tags set |
|---|---|---|
| `hosted` | a program for an operating system: the entry code sets up the arguments and calls `main` | `linux`, `hosted` |
| `freestanding` | no operating system: the program provides its entry point and its linker script | `freestanding`, `none` |
| `kernel` | `freestanding`, plus code generation for ring 0 (section 20.6) | `freestanding`, `none`, `kernel` |

A **target** is named by a triple. The specified targets are `x86_64-linux`, whose default mode is
`hosted`, and `x86_64-none`, whose default mode is `freestanding`. The tag `amd64` is set for both, and
`int`, `uint`, `uintptr` and pointers are 64 bits wide on both (section 20.2). The build tags select
files and the contents of files (section 14.4).

The output is an ELF executable, or a flat binary on request.

**Hosted.** The entry code sets up the arguments and the environment, starts the scheduler if the
program or its dependencies request scheduler capabilities (section 19.5), runs package initialization and calls `main` (section 15.4, Program
execution). A return value of `main` becomes the exit status. The default panic handler prints the panic
and exits with status 2. Run-time checks that cost memory or time, such as generation checks, are on by
default (section 21.5).

**Freestanding.** There is no operating system and so no `pluto/os` or `pluto/syscall`. The program
provides its entry point, which must call the program's initialization and `main` on a 16-byte aligned
stack with the direction flag clear (abi.md section 8). The program must define its panic handler
(section 17.4). There is no default allocator until the program calls `alloc.SetDefault`; allocating
before then panics (section 20.11). A program that starts tasks, performs a channel operation or `select` that may wait, or waits in
`pluto/sync` must bind a scheduler with `//pu:scheduler` (section 19.5), and fails to build otherwise. Scheduler-dependent
timers, callbacks and poller operations also require the backend through their API contracts. A program
requiring none of these capabilities contains no scheduler. Required safety protection remains enabled
by default: unavailable runtime protection requires static proof or rejection (section 21.5).

**Kernel.** A kernel build is a freestanding build with the additions of section 20.6.

### 20.6 Kernel mode

Kernel mode builds code that can run in ring 0 with interrupts enabled and no vector state saved on
entry. It differs from `freestanding` in these ways:

- **No red zone.** Plutonium code never stores below the stack pointer in any mode, so nothing changes
  for Plutonium code. Assembly and C linked into a kernel must not use the red zone either, because an
  interrupt taken without a stack switch pushes its frame directly below the interrupted stack pointer.
- **No floating point.** Kernel code does not touch the vector registers (section 20.7).
- **Kernel code model.** The image is linked in the top 2 GiB of the address space. Without a linker
  script of the program's own, the build links with a default kernel script (abi.md section 10.3).
- **Per-CPU variables** are available (section 20.9).
- **Type descriptors omit field names** unless the build asks to keep them, since a kernel image rarely
  prints `%+v`. Formatting then prints the fields without their names.

`pluto/arch/x86` (section 20.8) and `//pu:interrupt` handlers (section 20.10) are available in every
mode; in kernel mode a handler without `//pu:fpu` saves no vector registers. `pluto/os` and
`pluto/syscall` are unavailable, as in every freestanding build. Like every freestanding program, a
kernel must define its panic handler (section 17.4). Kernel mode preserves the ordinary safety
contract; it does not implicitly select unchecked execution (section 21.5).

Three rules apply to the whole of kernel code and are stated where they belong: floating point appears
only in `//pu:fpu` functions (section 20.7); nothing an interrupt handler or the panic handler can reach
may wait (section 20.10); and allocation depends on what the allocator does, not on which operation calls
it (section 20.11).

### 20.7 Floating point in kernel mode

An interrupt or system call that enters the kernel does not save the SSE and x87 registers, so kernel
code must not touch them. In kernel mode, any floating-point operation in a function the program uses is
a compile error (E2520), unless the function is marked `//pu:fpu`. The operations are arithmetic,
comparison, conversion between floats or between floats and integers, a float constant, passing or
returning a float, and converting a float to an interface. "Passing" is judged by the calling convention:
a struct parameter whose float fields would travel in vector registers is an error, while one passed by
reference is not (abi.md section 3). Float types may still appear in declarations, and copying float-typed
values (whole structs, fields, array elements, zero values) is allowed, since a copy uses integer
registers.

The rule applies to the functions the program uses. A float function in an imported package that the
kernel never calls costs nothing, so the standard library keeps its float code behind
`//pu:build !kernel`: in `pluto/fmt`, a float argument prints as its bits in a kernel build, and so does
a float passed to `print` or `println` from a `//pu:fpu` function. Package-level arrays and structs
initialized with constants, such as `pluto/math`'s coefficient tables, are written into the image, so
importing such a package adds no float code to package initialization.

A function marked `//pu:fpu` may use floats in kernel mode, and so may the function literals inside it.
Whoever calls it must have saved the interrupted context's vector state and enabled the unit
(`x86.FPUBegin` and `x86.FPUEnd`, section 20.8); the compiler does not check this, but it never inlines a
`//pu:fpu` function into one that lacks the directive. The optimizer adds no vector instructions to a
function that may not use floats (section 20.3, `//pu:nosimd`), block copies and clears included.

The alternative of emulating floats in software was rejected: it would put an IEEE 754 library in every
kernel and hide a large cost behind ordinary arithmetic, for code that hardly ever wants floats.

### 20.8 `pluto/arch/x86`

The package `pluto/arch/x86` gives access to the instructions a kernel needs. Except for the
gate constructors `InterruptGate` and `TrapGate` and the constructor `NewDescriptorTablePointer`, which are
ordinary functions with no side effect, each function is expanded inline to the instruction or short
sequence it names, is never removed even if its result is unused, and is a compiler barrier: no memory access is moved across it (section 19.7, Memory model). The
expansion of each function is in the ABI document (abi.md section 10.5). Privileged instructions fault
outside ring 0. The package is available on amd64 in every build mode, though only `Pause`, `Cpuid`,
`Rdtsc` and `Rdtscp` are useful outside ring 0.

```go
package x86

func Cli()
func Sti()
func StiHlt() // sti; hlt with no gap for an interrupt
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

func InterruptGate(h any, selector uint16, ist uint8) Gate // h must name a //pu:interrupt function; for building the IDT
func TrapGate(h any, selector uint16, ist uint8) Gate      // h must name a //pu:interrupt trap function

func NewDescriptorTablePointer(base uintptr, size int) DescriptorTablePointer

const GSBaseMSR = 0xC0000101       // IA32_GS_BASE
const KernelGSBaseMSR = 0xC0000102 // IA32_KERNEL_GS_BASE, exchanged by Swapgs

type DescriptorTablePointer packed [10]byte { limit uint16; base uint64 }
type Gate struct{ /* one 16-byte IDT entry; the fields are not exported */ }
type FPUState aligned[[512]byte, 16] // the FXSAVE area
type InterruptFrame struct{ RIP, CS, RFLAGS, RSP, SS uint64 }
```

`FPUBegin` leaves the FPU in the interrupted context's state; it does not enable SSE, which the kernel
does once. A `//pu:fpu` function called from a function without the directive must run between `FPUBegin`
and `FPUEnd` on the same area. A handler is installed through a typed gate. `InterruptGate` takes the name
of a `//pu:interrupt` function and returns the `Gate`, the 16-byte entry of an interrupt descriptor table,
that enters it through an interrupt gate (the processor clears the interrupt flag on entry). `TrapGate` takes
the name of a `//pu:interrupt trap` function and returns a trap gate (the flag is left as it was). A table is
an array of `Gate`, and `Lidt` loads its address. Neither constructor orders memory accesses or is a compiler
barrier. The argument must be a compile-time-known handler of the matching form; a function value, an
indirect use, a handler of the other form or any other argument is a compile error (E2572), not a run-time
panic. A `Gate` has no exported fields. Putting a handler's address into the table as a raw word, for
example by storing a `uintptr` through `unsafe.Pointer` into the table's memory, is unsafe code (section
20.1): the typed gate is the safe way to install a handler, and it is what lets the compiler know that
every installed handler is entered with interrupts disabled (section 20.10).

### 20.9 Per-CPU variables

A package-level variable marked `//pu:percpu` has one copy per processor:

```go
//pu:percpu
var current *Thread
```

The variable lives in the `.percpu` section, which the kernel links at address 0 so that each variable's
address is its offset in a per-CPU block, and it is accessed relative to the `GS` base, which the kernel
points at the current processor's block. A word-sized access is a single instruction, so an interrupt on
the same processor cannot split it. `&v` is a pointer into the current processor's copy at the time it is
taken, and keeps pointing there if `GS` changes later.

The declaration above compiles: `current = t` and `t := current` are lone accesses of a word. A compound
access, or a store that depends on a load of the same variable, needs a region:

```go
//pu:percpu
var ticks uint64

func count() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	ticks++ // inside a non-migrating region
}
```

These rules apply:

- Only constant initializers are allowed (constants, and arrays and structs of them), because package
  initialization runs once, before any processor has its own block (E2550).
- `//pu:percpu` is a compile error outside kernel mode (E2551), and together with `//pu:section` (E2552).
- Rule 1 of section 19.3 (Data race freedom) does not apply to a per-CPU variable: each processor has its
  own copy, so the copies are not shared. A per-CPU variable follows the rules of this section instead.
- A lone load or a lone store of a whole per-CPU variable of at most a machine word, named directly
  (`current = t`, `t := current`), is allowed anywhere, provided the store does not depend on an earlier
  load of the same variable (the next bullet). It is one instruction, atomic with respect to the same
  processor (memory-model.md section 6.4).
- A store to a per-CPU variable whose value, or whose controlling condition, depends on an earlier load of
  the same variable is a read-modify-write, whether the dependence is by data or by control flow, and it
  needs the same non-migrating region as `ticks++`. The task may move to another processor between the load
  and the store, and the store would write that processor's copy with a value read from this one's. Outside
  a region these are compile errors (E2022):

  ```go
  x := ticks
  ticks = x + 1 // E2022: the value depends on a load of ticks

  if current == idle {
  	current = next // E2022: the store is controlled by a load of current
  }
  ```

  Inside `rt.DisablePreemption()` and `rt.EnablePreemption()` (or another non-migrating region) both are
  allowed. Loads and stores that do not depend on each other stay allowed anywhere: `t := current` in one
  statement and `current = u` in another, where `u` was not computed from `current`, are two independent
  lone accesses.
- Every other access must be inside a non-migrating region (section 19.3): a compound assignment or `++`,
  a store that depends on an earlier load of the same variable, a read and a write in one expression, any access to a variable larger than a machine word or to part of
  one, a call of a method with a pointer receiver, and `&v`. If an interrupt handler may also access the
  variable, the access must be inside an interrupt-disabled region (section 19.3). An access outside the
  region it needs is a compile error (E2022). The error names the access and says whether a handler makes
  disabled interrupts necessary.
- When an interrupt handler also uses the variable, every load or store through a pointer from `&v` must
  be inside an interrupt-disabled region as well, whatever its size and even though the pointer is bounded
  by a region (the next bullet): preemption disabled is not enough. The one exception is a store through
  the pointer that is the whole statement and writes the whole variable, when the variable is at most a
  machine word. A load through the pointer has no exception.
- A pointer from `&v` is bounded by the region it was taken in, which is one run of that region: the same
  code run again opens another region, whether by the next iteration of a loop, a jump back with `goto`, a
  recursive call or a second call of a function literal. It may be kept only in local variables, and not
  in a variable that lives across such a run (one declared before the loop or the label, or a variable of
  the enclosing function that a function literal captures). It may not be used after the region ends or in
  a later region, even one opened at the same place in the source, sent on a channel, passed to a task,
  stored anywhere but in a local variable, or returned. A function that receives the pointer may use it
  only inside a region that its caller held at the call and the function has not released; a region the
  function opens itself is another region. The analysis counts a region once, so the first release of a
  kind (the first `rt.EnablePreemption`, for example) ends it even if its opening was nested: this errs
  toward rejecting. A use outside the region is a compile error (E2023).
- Two lone accesses in separate statements may reach the copies of two different processors, because the
  task can migrate between them. That is defined behavior, not a data race. Where the second depends on the
  first, it is the dependent store above and not a lone access.
- The kernel gives each processor its block and points `GS` at it before that processor touches a per-CPU
  variable. How it does this is in the ABI document (abi.md section 10.6). The runtime has two per-CPU
  variables of its own, the processor's number recorded by `rt.SetCPU` and the current-task slot, so every
  kernel image has a `.percpu` section.

### 20.10 Interrupts

A function marked `//pu:interrupt` is an interrupt or exception handler, entered by the processor rather
than called. The contract is that a handler is entered through an interrupt gate, which disables maskable
interrupts on entry, so the handler is not interrupted by another maskable interrupt on its processor until
it returns or enables them with `x86.Sti`. The compiler checks this through types: a handler's address is
installed only through `x86.InterruptGate` (section 20.8), which returns a `Gate`, and a raw word written
into the table is unsafe code. The body of such a handler is an interrupt-disabled region (section 19.3)
up to a call of `x86.Sti`, which ends it, or a call that may switch tasks.

A handler that can be re-entered says so with the directive form `//pu:interrupt trap`. It is installed
only through `x86.TrapGate`, which makes a trap gate that leaves interrupts as they were, and its body is
not an interrupt-disabled region. Per-CPU data that such a handler shares with the code it interrupts, or
with itself, is protected by a region that the handler opens itself. Any other argument to the directive is
E2603.

Exceptions and nonmaskable interrupts are not held off by an interrupt gate, and no region excludes them.
That remains a contract of the kernel: a handler for one of them must not use data that the code it
interrupts may be changing. It is a plain function with a body (not a method, not generic) and no results, with one
parameter, the address of the interrupt frame, or two, the frame and the error code. The frame parameter
may be any pointer type, `unsafe.Pointer` or `uintptr`, and the error code is `uint64` or `uintptr`.
Handlers for the exceptions that push an error code take two parameters, and all others take one; the
signature decides the stack layout, so the program installs each handler only on a vector that matches.
Anything else is an error (E2570 for a declaration that cannot be a handler, E2571 for wrong parameters or
results). The compiler generates the entry and exit, saving every register the handler may change and
returning with `iretq`, so changes the handler makes to the frame, such as advancing `RIP` past a faulting
instruction, take effect when it returns (abi.md section 10.4).

```go
//pu:interrupt
func timer(f *x86.InterruptFrame) { x86.Outb(0x20, 0x20) }

//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) { rt.PanicFault(rt.MemoryFault, uintptr(x86.ReadCR2()), uintptr(f.RIP), uintptr(f.RSP), 0) } // a vector that pushes an error code
```

A handler is never inlined (`//pu:interrupt` on a function that is also `//pu:inline` is E2570), cannot be called, deferred or used as a func value (E2573), and its address
is installed with `x86.InterruptGate` or `x86.TrapGate`, according to its form.

**Interrupt context cannot block.** A handler runs on top of whatever code the processor was executing.
If the handler waits, for a lock or for room in a channel, the interrupted code can never resume to
release it, and the processor stops. So nothing an interrupt handler can reach, and nothing the panic
handler (section 17.4) can reach, may:

- send or receive on a channel, except as a case of a `select` with a `default` case; run a `select`
  without a `default` case; or range over a channel;
- call a function that may park the caller, such as `Lock` of a `sync.Mutex`, `Wait` of a
  `sync.WaitGroup`, `time.Sleep` or `rt.Park`, or a function marked `//pu:blocking`;
- start a task (a `task` statement, `rt.Go`, or a library function that starts one);
- allocate from an allocator that may wait.

Operations that do not wait are allowed: `select` with `default`, `TryLock`, readying a parked task,
arming, stopping and running the runtime's timers, atomic operations, and correctly protected
interrupt-safe spinning. Such a spin-lock protocol disables the relevant local interrupts **before
acquisition**, excludes preemption that could prevent the holder from running, releases the lock, then
restores the prior interrupt/preemption state. Every user follows that protocol. Contention may depend
on another processor's progress, never on the interrupted context resuming to release the lock.
Nested exceptions and nonmaskable interrupts require a separate exclusion/progress protocol; ordinary
interrupt disabling does not exclude them. These permitted spins do not park and are not the forbidden
waiting operations above.

An interrupt handler is a party to the data race rules (section 19.3). Data that a handler accesses, other
than a per-CPU variable, must be atomic or guarded by an `rt.Lock`, whether the other party is a task, a
handler on another processor or the same handler running on several processors at once. A `sync.Mutex`
cannot be taken in a handler (E2584). `volatile[T]` alone, and an interrupt-disabled region alone, exclude
only the same processor, so they satisfy the rules only for a per-CPU variable, accessed as section 20.9
describes (memory-model.md section 6.1). Volatile supplies neither mutual exclusion nor compound-operation
atomicity. The race errors (E3502, E3503) name the handler as the party. To hand work to normal code, a
handler sends without waiting:

```go
select {
case work <- item:
default: // full: drop it, or count the loss
}
```

The compiler checks this over the whole program. From each interrupt handler the program uses, and from
the panic handler, it follows every call, through every package, and reports the first path it finds to
an operation above, naming each call on the way (E2584). A path to a `task` statement is reported the same
way (E2585).

- **What is followed.** Direct calls, method calls, deferred calls, and the calls the compiler adds for
  `new`, `make`, interface conversions and panics, into the standard library as into the program's own
  code. The check also follows the drops the compiler inserts (section 18.6), like any call: at the end of
  a scope, on assignment over an owner, for temporaries, and the release of a counted value whose count
  reaches zero, into the type's `Drop` and drop glue and into the `Free` of every allocator the program
  installs. It is an error (E2584) only where one of these may wait, so the result does not depend on
  whether the cleanup is written or inserted. A path ends at the runtime's `park`, in which every wait in the library ends, at the runtime's
  `spawn`, in which every task start ends, and at a function marked `//pu:blocking`. A path from an
  interrupt handler stops at the panic handler, which is checked on its own.
- **`//pu:blocking`** is for an opaque primitive that may park, halt pending an event or wait for
  progress that is unsafe in interrupt context, including a lock requiring interrupted execution to
  resume. On a function with a body it states a wait that the compiler cannot see, such as a spin until an
  interrupt handler or another processor sets a flag; it only adds this restriction and never makes a
  program accepted, so it is not unsafe code and not a trusted promise. It is not required merely for the
  permitted interrupt-safe spin protocol above. Source-visible
  operations are inferred; opaque assembly/C contracts must describe their waiting and exclusion
  obligations. The standard library marks only `park`.
- **Calls through interfaces and func values** cannot be resolved exactly, so the check is conservative.
  A call of method `m` through an interface type `I` may reach `m` of every type the program converts to
  `I`, whether directly, from another interface, or by a type assertion. A call through a func value of
  type `F` may reach every function of type `F` whose value the program takes anywhere: as a func value,
  a method value or a method expression, or as a function literal. The error says when a path goes
  through such a call. To make a handler's call exact, call the function directly.
- **Allocation.** An allocator is not marked as a whole. `new`, `make` and every operation that makes a
  string call the allocator's `Alloc` through the `alloc.Allocator` interface, and the check follows
  that call into every allocator the program installs. An allocator whose `Alloc` or `Free` may wait,
  because it takes a lock that parks, is reported like any other function that may wait; one that uses a
  spin lock with interrupts disabled is allowed. No allocator needs a marking of its own. An allocator
  has no exemption from the data race rules: its state is data like any other. If an interrupt handler
  can reach its `Alloc` or `Free`, the state they share with other code must meet section 19.3, for
  example per-CPU caches touched only in interrupt-disabled regions (section 20.9). Parts of an allocator
  written in unsafe code are its own responsibility (section 18.12).
  Forbidding all allocation in interrupt context unless an allocator is marked safe was rejected: it
  would forbid the common, safe case of a lock-free per-CPU cache, and a marking could be wrong without
  the compiler noticing.
- **Assembly and C** are not looked into: a function without a body counts as not waiting unless its
  declaration is marked `//pu:blocking`. This is a contract taken on trust, because a function without a
  body is unsafe code (section 18.12). The check of this section guards against deadlock, not against
  memory or data race errors, and it follows all Plutonium source and takes the declaration of a function
  without a body as that function's contract.
- **Non-blocking channel operations** never reach `park`, so a `select` with `default`, `close`, `len` and
  `cap` stay allowed; the waiting forms reach it.
- **The scheduler's `Wake`.** `rt.Ready` calls the bound scheduler's `Wake`, and interrupt handlers may
  call `rt.Ready`, so `Wake` is a root of the check too.

A panic inside a handler, or a fault that an exception handler reports with `rt.PanicFault`, goes to the
program's panic handler with a stack trace that starts at the faulting instruction and ends at the
handler's frame (section 17.4).

### 20.11 Allocation in kernel mode

Kernel code allocates as other code does. `new`, `make`, the `pluto/alloc` functions and the operations
that make strings all take memory from the current allocator, and no function needs a marking to use
them. The operations that make strings are `+` and `+=` on strings that are not constants, `string(r)` for
a rune that is not a constant, `string(runes)`, and library functions that return new strings, such as
`fmt.Sprintf`, `strconv.Itoa` and `strings.FromUTF8`. Where allocating is not allowed is decided by what
the allocator does, not by which operation calls it:

- **In interrupt context.** Whatever an interrupt handler, the panic handler or the scheduler's `Wake`
  can reach must not wait (section 20.10). The compiler follows every allocation, a string concatenation
  as much as a `new`, into the `Alloc` method of every allocator the program installs. An allocator that
  may wait is reported on the path that allocates; one that does not may be used there. The shared state
  of an allocator that an interrupt handler can reach follows section 19.3.
- **Before an allocator is set.** A kernel starts without a default allocator. Allocating before
  `alloc.SetDefault` panics when it runs, for strings as for `new` and `make`.
- **At package initialization.** A package-level initializer that allocates is a compile error in a
  freestanding build, because no allocator exists while packages initialize. The fix is to initialize the
  variable in the entry function after `alloc.SetDefault`. Hosted builds allocate from the default
  allocator (section 15.2, Package initialization).
- **When memory runs out.** The allocator returns nil and the operation panics with an out-of-memory
  message (section 17.1).

Everything about strings that does not make a new one never allocates: literals and constants, copying,
comparison, slicing, indexing, `len`, `range`, passing and returning, `unsafe.String`, and the `Append`
functions with a `vec.Fixed[byte]` destination over a caller's array. Copying and dropping a counted
string never waits. Dropping one may free its buffer, and the drop is followed like a call (section
20.10): it is an error only if the `Free` of an installed allocator may wait. A kernel whose allocator
may wait keeps the strings it drops there static or in an arena.

A generic function is judged for each set of type arguments it is compiled with, since each instance is a
function of its own. `strconv.AppendInt` into a `*vec.Fixed[byte]` never allocates, so an interrupt
handler may call it while other code appends to a `*vec.Vec[byte]`, which grows from an allocator that
may wait. A function that reaches its destination through an `io.Writer` counts, for the check of
section 20.10, as calling the `Write` method of every `io.Writer` the program makes. In a handler,
format with the `strconv` functions into a `vec.Fixed`.

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
