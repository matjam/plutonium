## A. Differences from Go

This appendix lists every behavior of Plutonium that differs from Go's, for readers who know Go. It
defines nothing: each difference is defined by the chapter named in its row. A reader who knows Go can
read the table and then the chapters it points to. A reader who does not know Go can skip the appendix.

### A.1 The no-false-friends rule

Plutonium is Go-familiar, not Go-compatible. It uses Go's syntax and idioms wherever they fit. Where Go's
meaning depends on a garbage collector, a growing stack or hidden allocation, the language changes and Go
code may need edits before it compiles.

A **false friend** is code that looks like Go, compiles, and does something other than what a Go
programmer expects. The language is designed to have none:

> Where Plutonium behaves differently from Go in a way a Go programmer would not expect, the difference
> is visible in the source: a different name, a different syntax, a different type, or a compile error
> that explains it. Code that looks like Go and compiles does what Go would do.

So a difference falls in one of these classes, which the tables below use:

- **Removed.** The construct is a compile error. The error names the construct and the reason and shows
  what to write instead. The reader learns of the difference at the line that has it.
- **Changed.** The construct exists and compiles, but the difference shows in the source, in a name, a
  type, a syntax, or in a compile error that appears when the program relies on Go's meaning. A function
  that Go does not have, or a type with a different name, is not a false friend, because its
  documentation is the first thing a reader looks up.
- **Added.** The construct has no counterpart in Go.
- **The exception.** One difference in the language itself is neither a compile error nor visible in the
  source (section A.5).

Programs in the common subset of Go and Plutonium are kept as tests: the expected output of the programs
in that subset comes from running them with Go. The subset is how an implementation is checked against
Go. It does not limit the language.

### A.2 Removed

| In Go | In Plutonium | Write instead | Section |
|---|---|---|---|
| `append(s, x...)` | compile error (E2013): a slice is a view and never grows | `v.Push(x)` and `v.Append(xs...)` on a `vec.Vec[T]`; `slices.Clone`, `slices.Concat`, `slices.Delete`; `vec.NewFixed(buf[:])` over an array; `s = s[:len(s)+1]` within a capacity | 13.1, 7.5 |
| `make([]T, n)`, `make([]T, n, c)` | compile error (E2014): the array would belong to no one | `vec.Make[T](n)`, `vec.WithCapacity[T](c)`; an array for a constant size; `a.Slice[T](n)` from an arena | 13.7, 7.5 |
| `string(b)` for a `[]byte` | compile error: Go keeps the bytes, a Plutonium string holds only valid UTF-8 | `strings.FromUTF8(b)` for bytes that should be text; `strings.FromUTF8Lossy(b)` where replacing invalid bytes is acceptable; an array key for binary data | 11.24, 7.3 |
| `[]byte(s)`, `[]rune(s)` | compile error: the slice would belong to no one | `strings.Bytes(s)`; `copy(buf[:], s)` into the caller's buffer; `for i, r := range s` or `strings.Runes(s)` | 11.24 |
| `string(i)` for an integer `i` | compile error unless `i` is a `rune` or an untyped constant | `string(rune(i))`, `strconv.Itoa(i)` | 11.24 |
| `recover` | not a built-in function: a panic never returns to the code that panicked | return errors | 13.1, 17.1 |
| `complex64`, `complex128`, `complex`, `real`, `imag`, imaginary literals | removed | a struct of two floats | 7.2, 13.1, 4.9 |
| `import "reflect"` | compile error: there is no run-time type metadata for programs | type switches, generics | 14.3 |
| `go f(x)` | compile error that names `task` | `task f(x)` | 12.12, 4.6 |
| `//go:build`, `//go:noinline` and every other `//go:` comment | compile error that names the `//pu:` equivalent where there is one | `//pu:build`, `//pu:noinline` | 20.3 |
| `defer` in a loop | compile error (E2530): each `defer` statement has one slot in the frame | call a function that defers, once per iteration | 12.19 |
| `defer` in the body of a range-over-func loop, unless every path leaves the loop after it; a deferred literal there that captures the body's own variables | compile error (E2533, E2534) | defer in the enclosing function, or pass the variables as arguments | 12.11, 12.19 |
| string literals that are not valid UTF-8 (`"\xff"`) | compile error | `[]byte{0xff}` for binary data | 4.11 |
| `iter.Pull` | not provided: it needs a coroutine for each iterator | a loop with `range` over the function | 12.11 |
| `copy(dst, src)` of owner elements | compile error (E3306): it would make a second owner | move the elements one at a time with `take` | 13.2, 18.5 |
| a package variable written by code that more than one task can reach, without a lock or an atomic type | compile error that names both writes (E3502) | `pluto/sync/atomic` types, or hold the same `sync.Mutex` at every access | 19.3 |

### A.3 Changed

The rows in this table are behaviors that differ from Go's. Each says how the difference shows.

**Memory.**

| In Go | In Plutonium | How the difference shows | Section |
|---|---|---|---|
| `new(T)` returns `*T` | returns `owned[T]`, the single owner of the new variable; it dereferences like `*T` | a different type: storing it in a `*T` variable is a compile error | 13.9, 7.19 |
| a variable whose address escapes moves to the heap | a variable lives where its declaration puts it, never on the heap by itself | returning, storing or sending a view of a frame is a compile error (E3001 to E3006) | 6, 18.2, 18.3 |
| each iteration of a `for` loop has its own copy of the loop variables | every iteration reuses the same storage | keeping a closure, pointer or interface box of a loop variable where the next iteration can see it is a compile error (E3006) | 12.10, 18.4 |
| a function literal can be stored anywhere | a closure that captures variables is valid while its frame is; a returned closure is built in a region its caller provides | storing one in a package variable, or returning it in a way no region can hold, is a compile error (E3100 to E3102) | 11.4, 11.5 |
| an interface holding a large value boxes it on the heap | the box is in the frame of the function that converts | returning or storing such an interface where it outlives the frame is a compile error | 7.9 |
| converting a value to an interface copies it | converting an owner or a counted reference to an ordinary interface lends it | asserting an owner back out of an ordinary interface is a compile error (E3308) | 7.9, 11.13 |
| a struct is always copyable | a struct with an owner field must be declared `owner`, and copying it is a move | a use after the move is a compile error (E3301, E3302) | 7.6, 7.18, 18.5 |
| `os.File` is used through `*os.File` | `os.File` is an owner that closes when its variable goes out of scope | a different type: `os.Open` returns an `os.File`, not a pointer | 18.6 |
| the garbage collector frees memory | an allocation has an owner, an arena, or a count; the last copy of a counted string or reference frees it | allocation functions take an optional allocator argument, and the names say what owns | 18.1, 18.9, 18.10 |
| `slices.Clone`, `slices.Concat` and the like return slices | they return a `vec.Vec`, an owner | a different type | 13.1 |

**Strings.**

| In Go | In Plutonium | How the difference shows | Section |
|---|---|---|---|
| a string is any sequence of bytes | a string is valid UTF-8, always: three words, immutable, counted | no conversion from bytes keeps arbitrary bytes (see A.2) | 7.3, 18.8 |
| `s[i:j]` slices bytes | `s[i:j]` panics when `i` or `j` is not the start of a rune | see A.5 | 11.12 |
| `os.Args`, `os.Getenv`, directory entries return the bytes the system gave | they return a string with each invalid byte replaced by U+FFFD; the exact bytes are next to every such string (`RawName`, `ArgBytes`, `GetenvBytes`, the `Bytes` forms of the file functions) | the library documents it, and a lossy name does not open the file it came from | 7.3 |

**Types, maps and channels.**

| In Go | In Plutonium | How the difference shows | Section |
|---|---|---|---|
| `map[K]V` is a reference to a table the collector frees | `map[K]V` is a counted reference; the table is freed with its last reference; iteration follows Go's guarantees | `make(map[K]V, n, a)` takes an optional allocator; keys and values cannot be owners | 7.10, 13.7 |
| a map can be passed to any goroutine | a map is sendable, and its contents fall under the race rules | writes from two tasks need a lock the compiler infers, a partition in a scope, or one task | 7.10, 19.3 |
| `make(chan T, n)` and the channel is collected | `make(chan T, n, a)` takes an optional allocator; the channel is a counted reference, freed with its last reference, and owns its timer | sending an owner moves it | 7.11, 12.5, 19.6 |
| `errors.New` returns a new error at every call | the same error at every run of one call site, a different error for each call site | observable only by comparing two results of one call site; `errors.New` must be called directly with a constant text | 16.3 |
| an error value with context lives on the heap | an error returned from a function is a value that fits in a word, or a pointer to storage that outlives the function; allocated errors are counted | the rule of section 16.2; returning a struct or a pointer to a local as `error` is a compile error | 16.2, 16.7 |
| an exhaustive `switch` does not end a function | an exhaustive `switch` on an enum or a union does | a function can omit the final `return` | 12.1 |

**Expressions and statements.**

| In Go | In Plutonium | How the difference shows | Section |
|---|---|---|---|
| the order of evaluation of operands other than calls, method calls, receives and logical operators is not specified | it is specified: calls, method calls and receives first, left to right, then the other operands (the order Go's gc compiler uses) | none: it matches what every Go program has seen | 11.26 |
| `clear(s)` zeroes elements | the same; assigning to an owner element drops it first, so every file in `clear(files[:])` is closed. `clear(m)` deletes every entry and releases counted keys and values | the elements are owners | 13.3 |
| iterators over a container yield its elements | iterators over owners yield views (`*T`) | the loop variable is a pointer | 12.11 |
| `defer` runs when the function returns | the same; each `defer` statement has one slot, a function has at most 64, and a deferred call runs before the function's own variables are dropped | none for programs that compile | 12.19 |
| `panic` unwinds the stack running deferred calls, and `recover` stops it | `panic` never returns to the frame that panicked; in a hosted build the panicking task first runs its pending deferred calls, then the panic handler runs; owners are not dropped | there is no `recover` | 17.1, 17.2 |
| `runtime.Goexit` runs deferred calls | `rt.Goexit` runs the deferred calls and drops the owners of every frame of the task | owners are dropped as well | 17.3 |

**Tasks and concurrency.**

| In Go | In Plutonium | How the difference shows | Section |
|---|---|---|---|
| `go f(x)` starts a goroutine | `task f(x)` starts a task | a different keyword | 12.12, 19.1 |
| a goroutine's stack grows | a task has a fixed stack, with a guard region below it; overflowing it is a panic with a trace | a program that recurses deeply in a task fails at the task's limit | 19.1, 21.4 |
| a function literal that captures variables can be started with `go` | a `task` statement of a capturing literal is a compile error (E2590) | pass the values as arguments | 12.12 |
| a local `sync.WaitGroup` is passed by address to goroutines | passing a view of the frame to a task is a compile error (E3004); `sync.Scope` starts tasks that may use the frame and joins them before its scope ends | a different type, `sync.Scope`; `sync.WaitGroup` behaves as in Go | 19.4 |
| sending a value shares it | a value sent on a channel or passed to a `task` statement must be sendable; sending an owner moves it | a view of changing memory is a compile error (E3501) | 19.2 |
| a data race is a bug with limited consequences | safe code is free of data races, because the compiler rejects them; in unsafe code a race is still a bug with limited consequences | compile error in safe code | 19.3, 19.7 |
| stack traces and the deadlock report say `goroutine` | they say `task` | the word | 17.1, 19.1 |
| the program exits when `main` returns | the same; `main` may return nothing or an `int64`, which becomes the exit status in hosted builds | the signature | 15.4, 10.12 |

**Programs and packages.**

| In Go | In Plutonium | How the difference shows | Section |
|---|---|---|---|
| source files end in `.go` and are described by `go.mod` | source files end in `.p` and the module is described by `pluto.mod`; standard packages live under `pluto/` | the names | 3.3, 14.3 |
| build constraints are `//go:build` and file name suffixes | `//pu:build` and file name suffixes; the tags of the build mode are set (`hosted`, `freestanding`, `kernel`) | `//go:build` is an error | 14.4, 20.5 |
| the standard library is Go's | the standard library is Plutonium's, in `pluto/`; packages that keep Go's names keep Go's behavior except where their documentation says | the import path | Appendix F |
| `unsafe` conversions follow rules written for the collector | the conversions are unrestricted, and the program upholds validity; `unsafe.Drop` is added | the import of `unsafe` | 20.1 |

### A.4 Added

These have no counterpart in Go.

| Addition | What it is | Section |
|---|---|---|
| owner types | types declared with `owner`: they move instead of being copied, and run `Drop` when they end | 7.18, 18.5, 18.6 |
| `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]` | a single heap owner; counted references for one task and for several tasks; a back reference | 7.19, 18.7 |
| `take(&place)` | moves the value out of a place and leaves the zero value | 13.10 |
| `Drop` methods | run when an owner's life ends | 10.14, 18.6 |
| enums | `enum` types: a closed set of named constants, with exhaustive switches | 7.12 |
| unions | `union` types with variants, and type switches over them | 7.13 |
| `packed` types | bit-exact layouts of fields | 7.14 |
| `volatile[T]` | memory-mapped access that the compiler never removes or reorders | 7.15 |
| `aligned[T, N]` | storage aligned to `N` bytes | 7.16 |
| `ring[T, N]` | a fixed-capacity ring buffer stored inline | 7.17 |
| owning interfaces | `type R owner interface{...}`, an interface that owns what it holds | 7.9 |
| default parameter values | `func f(a int, b int = 2)` | 10.13, 11.14 |
| the indexing interface | `x[k]` on a library container calls its `Index`, `SetIndex` and `IndexOk` methods | 11.11 |
| optional allocator arguments | `new(T, a)`, `make(chan T, n, a)`, `make(map[K]V, n, a)` | 13.7, 13.9 |
| the `task` statement | replaces `go` | 12.12 |
| `sync.Scope` | tasks that may use the starting frame and are joined before its scope ends | 19.4 |
| `portable` | a constraint for values that may cross between address spaces | 8.9 |
| `main` returning `int64` | the exit status | 10.12, 15.4 |
| directives | `//pu:` comments for facts the compiler cannot derive | 20.3 |
| kernel facilities | `pluto/arch/x86`, `//pu:interrupt`, `//pu:percpu`, `//pu:fpu`, the panic handler, the scheduler contract | 20.5 to 20.11, 19.5 |
| assembly and C | `.s` and `.c` files in a package, and `//pu:extern` and `//pu:export` | 20.4 |
| `unsafe.Drop` | destroys what a pointer points to | 20.1 |
| the standard library of `pluto/` | `vec`, `hash`, `arena`, `alloc`, `container`, `rt` and the rest | Appendix F |

### A.5 The one documented exception

Slicing a string at a position that is not the start of a rune panics:

```go
if len(title) > 40 {
	title = title[:40] + "..." // panics if byte 40 is in the middle of a rune
}
```

Go slices bytes and may leave half a rune at the end. A Plutonium string holds valid UTF-8, and the
language depends on that: it is what makes `range`, comparison and the other operations on strings safe
without a check on every use. A slice that cut a rune in two would break the property, so the operation
that would do so panics. A slice bound at the start or the end of the string is always at a boundary.
Text that is only ASCII never panics. The first title that has an accented letter in the wrong place
does, and the panic names the rule and the helpers: `strings.Truncate(s, n)` returns the longest prefix of
at most `n` bytes that ends on a rune boundary, and `utf8.RuneStartBefore(s, i)` rounds a byte offset down
to the start of a rune.

```go
title = strings.Truncate(title, 40) + "..." // never splits a rune
```

This is the one difference in the language that the rule of section A.1 does not make visible in the
source. The alternatives were worse:

- a compile error wherever the compiler cannot prove that a bound is a boundary would reject most code
  that parses ASCII text (`line[:4]`, `s[i+2:]`), and the proof would depend on the optimizer, so whether
  a program compiled would depend on the optimization level, which section 21.3 (Acceptance does not
  depend on optimization) forbids;
- rounding the bound down to a boundary would be silent, which is what the rule forbids;
- slicing bytes as Go does would require strings that may hold invalid UTF-8, which removes the property
  the rest of the language relies on.

The panic is of the same kind as Go's own panic for a bound out of range: an index that does not fit the
data, reported at the line that has it, loudly. Two library behaviors are also invisible in the source,
and are kept because no correct program can tell them apart from Go's, or because they fail loudly and
come with the exact bytes: `errors.New` returns one error for each call site (section 16.3), and names
that the operating system returns are checked and made valid UTF-8 (section A.3, Strings). They belong
to the documentation of their packages.
