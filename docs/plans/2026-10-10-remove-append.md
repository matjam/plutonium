# Removing `append`

**Date:** 2026-10-10
**Status:** decided 2026-10-10 (all of section 8 accepted) and implemented (roadmap item 77, which had the number 58 until the roadmap resolved its duplicate). The open
question at the end of section 8 was settled the same day: `bytes.Buffer` grows as Go's does, and bounded
writes use `vec.Fixed[byte]`. Two details differ from the text below: `vec.NewFixed` takes its room from
`len(storage)`, not the capacity, and the `os` and `exec` functions read a path into their own buffer and
append it whole, so they never store part of one.

> **Status (2026-10-10): decided and implemented.** The decisions of section 8 are rules of the
> specification and are in the compiler and library. This document keeps the survey and the reasoning.
> Where the specification's text differs from the text below, the specification wins; remaining
> follow-ups are in docs/roadmap.md.
>
> **Defined now by:** section 13.1 (Built-ins Plutonium does not have), section 7.5 (Slice types),
> section 13.7 (Making channels and maps, for `make` of a slice), section 13.2 (Copying slices),
> section 11.24 (Conversions), section 18.10 (No allocation without an owner or an arena), section 20.11
> (Allocation in kernel mode), appendix A.2 (Removed), appendix F.3 (Iterators and containers: `vec.Vec`,
> `vec.Fixed`, `vec.Appender`, `vec.AppendString`), appendix F.6 (Strings).
>
> **The specification supersedes these statements in this document:**
> - "`append` stays a predeclared built-in, as `recover` does, so a call finds it and gets this error
>   instead of 'undefined: append'" and decision 1 ("`append` stays predeclared so the error can name
>   it") (section 5.1 and section 8): `append` and `recover` are not predeclared. A use of the name in a
>   program that declares no `append` of its own is E2013 (E2004 for `recover`); a program may declare its
>   own `append`, which is then an ordinary identifier. Sections 13.1, 7.5 and 10.3.
> - "`make([]T, n, cap)` still gives a slice with spare capacity, which a program fills by slicing"
>   (section 7), and the `rangefuncgo.p` row, "collects results in `make([]int, 0, 64)`" (sections 2.1,
>   class (a), and 6.2): `make` of a slice type is a compile error (E2014). The fix is `vec.Make[T](n)`,
>   `vec.WithCapacity[T](c)`, an array for a constant size, or `a.Slice[T](n)` from an arena. Sections 13.7
>   and 7.5.
> - "`return string(b.Slice())`" in `FormatInt` (section 3.2) and "`vec.Fixed[byte]` and
>   `string(f.Slice())`" (section 3.2, "What stays as it is"): `string([]byte)` is a compile error. A string
>   is valid UTF-8, so the conversion is `strings.FromUTF8(b)` for text or `strings.FromUTF8Lossy(b)`
>   where replacing invalid bytes is acceptable. Sections 11.24 and F.6.
> - "`defer b.Free()`" and "`defer v.Free()`" (section 3.2, the growing example; section 5.1, the catalog
>   `Fix` and `Fixed` texts): an owner such as `vec.Vec` is dropped at the end of its variable's scope and
>   needs no `Free` call. Sections 7.18, 18.6 and 13.1.
> - "as `map[K]V` is in section 3" (section 1): the built-in `map[K]V` is part of the language, a counted
>   reference with Go's semantics. Section 7.10.
> - "Today's kernel check looks only at calls of functions marked as allocating" (section 3.2, Cost): no
>   function needs a marking. The compiler follows every allocation into the `Alloc` method of each
>   allocator the program installs, and judges a generic function once for each set of type arguments.
>   Section 20.11.
> - "Section 3's row becomes `append`; section 4.3 keeps its number ... and becomes 'Slices do not grow'"
>   (section 6.1, step 6), and "language spec section 4.3", "section 6.3", "6.1 and 6.6", "6.9", "section
>   8's fault list" and "12.7" (sections 1, 2.4, 3.2 and 6.1): these are the numbers of the earlier
>   language.md. The rule is section 13.1 (Built-ins Plutonium does not have) and section 7.5 (Slice
>   types); no section is titled "Slices do not grow". The views rules are section 18.3, duplicating
>   owners with `copy` is section 13.2 (E3306), the fault list is section 17.1, and allocation in kernel
>   mode is section 20.11.

## 1. Summary

Plutonium's `append` works only within a slice's capacity and panics past it (language spec section
4.3), because a slice is a view and owns no array it could grow (section 6.3). Go programmers read
`append` as "grow", so the keyword promises what the language cannot do. The decision of 2026-10-10 is to
remove it: `append(...)` becomes a compile error that says slices cannot grow and shows the `vec.Vec`
equivalent, as `map[K]V` is in section 3.

The survey (section 2) finds 247 calls of the built-in in Plutonium source. None of the library's own
code uses `append` to grow anything. Almost half of all calls (117) are the zero-allocation idiom
`dst = append(dst, ...)` inside the 27 library functions that write into a caller's `[]byte`
(`strconv.AppendInt`, `filepath.AppendJoin`, `fmt.Bprintf` and the rest). Those functions have the same
problem as the keyword: their Go names promise growth, and today they panic or silently truncate.

**Recommendation.** Remove the keyword, and give the `Append` functions one generic destination that is
either a growing owner or a bounded buffer, chosen by the caller's declaration:

- `vec.Vec[byte]`, which grows from its allocator, for code that wants Go's behavior;
- a new `vec.Fixed[T]`, a bounded vector over the caller's array, for zero allocation and kernel code. It
  never allocates and never panics: what does not fit is dropped and recorded, and `Truncated()` reports
  it.

```go
v := vec.New[byte]()            // grows: Go's behavior
strconv.AppendInt(&v, n, 10)

var mem [64]byte
f := vec.NewFixed(mem[:])       // bounded: no allocation, kernel safe
strconv.AppendInt(&f, n, 10)
```

The call is the same in both cases; only the declaration differs. Slices keep everything that does not
grow them: indexing, slicing (`s[:len(s)+1]` within capacity), `copy`, `clear`, and the `slices`
functions that shrink or reorder in place.

## 2. Survey

Counted with a regular expression over `*.p` files (comments excluded) and with a small Go program for
Plutonium source embedded in Go tests. Counts are calls of the built-in, not lines of `compat.Append`
or of methods named `Append`.

### 2.1 Classes

| Class | Meaning |
|---|---|
| (a) growth | The code would grow in Go. In Plutonium it fits only because a capacity was chosen in advance (`make([]T, 0, 64)`). |
| (o) owner internals | An owner (`vec.Vec`, `strings.Builder`, `fmt`'s string sink) or an exactly sized new string fills room it has just reserved. |
| (b) caller's buffer | A bounded write into a buffer the caller provides: the `Append*` functions and `bytes.Buffer`. |
| (c) fixed local buffer | Formatting into a `[N]byte` on the stack or a static array: the kernel idiom. |
| (d) copy, concat, shift | `append(s[:i], s[j:]...)`, building a concatenation into a buffer. |
| (e) exercises append | Test or example code whose subject is `append` itself. |

### 2.2 Totals

| Where | Calls | (a) | (o) | (b) | (c) | (d) | (e) |
|---|---:|---:|---:|---:|---:|---:|---:|
| `lib/pluto`, library code (21 files) | 153 | 0 | 12 | 113 | 25 | 3 | 0 |
| `lib/pluto`, `_test.p` files (4) | 15 | 6 | 0 | 0 | 3 | 6 | 0 |
| `testdata/run` (9 of 150 programs) | 48 | 42 | 0 | 0 | 0 | 0 | 6 |
| `testdata/lib` (8 of 36 programs) | 24 | 4 | 0 | 4 | 16 | 0 | 0 |
| `testdata/plutonium`, `trace`, `kernel` | 0 | | | | | | |
| `examples` (`11-alloc`) | 1 | 0 | 0 | 0 | 0 | 0 | 1 |
| Go tests with embedded programs (`internal/build/strconv_diff_test.go`) | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `selfhost/compat` (the implementation of `compat.Append`) | 3 | 0 | 3 | 0 | 0 | 0 | 0 |
| `bench`, `cmd/pluto/testdata` | 0 | | | | | | |
| **Total** | **247** | **52** | **15** | **117** | **47** | **9** | **7** |

Outside the built-in:

- `testdata/kernel` formats with `fmt.Bprintf` into stack buffers (3 calls), class (c).
- `selfhost` calls `compat.Append` and `compat.AppendString` 246 times (amd64 108, opt 74, ssa 41,
  objfile 13, demos 10). These are go2pu's translation of Go's `append` (section 5.2). They are library
  calls, so removing the keyword does not touch them; only `compat`'s own 3 calls change.
- The compiler's Go source has 1,072 `append` calls and 6 calls of `strconv`/`utf8`/`fmt` `Append`
  functions, all with a `nil` or growing destination (`strconv.AppendFloat(nil, x, 'g', -1, 64)` in
  `internal/constant`). They are Go and compile with Go; they matter only through go2pu.

### 2.3 The library by package

| Package | Calls | Class | What |
|---|---:|---|---|
| `strconv` | 56 | (b) 50, (c) 6 | `AppendInt`, `AppendUint`, `AppendBool`, `AppendFloat` (34 in `ftoa.p`), `AppendQuote`; `QuoteRune` into a `[16]byte` |
| `path/filepath` | 23 | (b) 20, (c) 3 | `AppendClean`, `AppendJoin`, `AppendDir`, `AppendAbs`, `AppendRel`; `WalkDir` builds child paths in a `[4096]byte` |
| `strings` | 14 | (b) 9, (o) 5 | `AppendUpper`, `AppendLower`, `AppendRepeat`, `AppendJoin`, `AppendReplace`; `Builder.Write*` after `grow`; `Clone`, `Repeat` into an exactly sized buffer |
| `time` | 10 | (b) 10 | `Duration.AppendString`, `Time.AppendRFC3339` |
| `os` | 9 | (b) 9 | `FileMode.AppendString`, `AppendMkdirTemp` (also `AppendWd`, `AppendReadlink`, which write through `dst[len(dst):cap(dst)]` without `append`) |
| `os/exec` | 9 | (b) 4, (c) 5 | `AppendLookPath`; the NUL-terminated program path; exit texts in a static array |
| `flag` | 6 | (c) 6 | `PrintDefaults` builds each line in a `[256]byte`, with no length check: a long flag name panics |
| `unicode/utf8` | 5 | (b) 5 | `AppendRune` |
| `bytes` | 4 | (b) 4 | `Buffer` writes, already bounded with `ErrFull` |
| `slices` | 4 | (b) 1, (d) 3 | `AppendSeq`; `Insert` and `Replace` (within capacity, panic past it), `Delete` |
| `testing` | 4 | (c) 4 | log lines in `[msgSize]byte`, truncated on purpose |
| `vec` | 4 | (o) 4 | `Push`, `Append`, `Clone`, `Map` after `grow` or `WithCapacity` |
| `fmt` | 3 | (b) 1, (c) 1, (o) 1 | `Bprintf` flush (silently drops what does not fit); `%w` operands in a fixed array; `Sprintf`'s sink after it grows |
| `errors` | 2 | (o) 2 | `Join`'s message in an exactly sized string buffer |

The 27 functions that take a destination `[]byte`: `strconv.AppendInt`, `AppendUint`, `AppendBool`,
`AppendFloat`, `AppendQuote`; `strings.AppendUpper`, `AppendLower`, `AppendRepeat`, `AppendJoin`,
`AppendReplace`; `filepath.AppendClean`, `AppendJoin`, `AppendDir`, `AppendAbs`, `AppendRel`;
`os.FileMode.AppendString`, `os.AppendWd`, `os.AppendReadlink`, `os.ReadlinkBytes`, `os.AppendMkdirTemp`;
`exec.AppendLookPath`; `time.Duration.AppendString`, `time.Time.AppendRFC3339`; `utf8.AppendRune`;
`slices.AppendSeq`; `fmt.Bprintf`, `fmt.Bprint`.

Every caller inside the library knows its bound in advance. Either it formats into a stack array sized
for the worst case (`FormatInt` into `[65]byte`, `FormatFloat` into `[800]byte`, `Getwd` into
`[maxPath]byte`), or it measures first and formats into a new string's buffer of exactly that size
(`Quote`, `strings.Join`, `strings.Replace`, `filepath.Join` for long paths). Overflow there is a library
bug, not a condition to report.

### 2.4 The compiler

- `internal/types2/builtins.go`: `case _Append` checks the call as Go does (`InvalidAppend`, E0083).
- `internal/lower/builtin.go`: `appendCall` and `appendCheck` (about 60 lines) store into the spare
  capacity, retain counted elements, and call `rt.panicAppend` past capacity.
- `internal/lower/closureflow.go`: `append` passes every argument's flow through to its result.
- `lib/pluto/rt/rt.p`: `panicAppend`.
- `internal/diag`: `goPlain["InvalidAppend"]` explains the capacity rule and points at spec 4.3; the texts
  of the kernel string errors (E2544, E2545) recommend `fmt.Bprintf` and `strconv.AppendInt`.
- Spec: section 3's table ("`append` beyond capacity"), 4.3, 6.1, 6.6 ("Growing"), 6.9 ("Duplicating
  owners with `copy` or `append`"), 8 (the runtime fault list), 12.7 and 13; `debugging.md` section 2.
- `editors/vscode/syntaxes/plutonium.tmLanguage.json` highlights `append` as a built-in.

## 3. The `Append` functions (classes (b) and (c))

This is the main design question: what a zero-allocation formatting function takes when a slice cannot
grow. The candidates were judged on Go readability and go2pu translation, zero allocation and kernel use,
honesty (no silent truncation, no hidden panic in code that looks like it grows), ownership, the
principle that a user should not need to know how the library works to use it, and the rule of no
annotations outside kernel precision.

### 3.1 Candidates

**(i) Take `*vec.Vec[byte]` and grow it.** `strconv.AppendInt(&v, n, 10)`. Honest and Go-like for
growth. It fails for the cases that use these functions most:

- The library's own callers format into stack arrays and into new strings' counted buffers. A `Vec` owns
  storage from an allocator and frees it on drop; it cannot wrap either.
- Kernel code could use a `Vec` over a fixed arena (`arena.New(buf[:])`), but then every `Append` call
  may allocate, so it is not usable where allocation is not allowed, and an exhausted arena panics
  ("vec: out of memory") in what is usually a logging path.

**(ii) A fixed buffer type only.** `strconv.AppendInt(&f, n, 10)` with `f` bounded. Right for kernels and
for the library's internals. Wrong for ordinary code: Go programs write `strconv.AppendFloat(nil, ...)`
and expect growth, and choosing a size up front is exactly the knowledge of the implementation the user
should not need.

**(iii) Keep `Append*(dst []byte, ...) []byte` over a library helper.** No migration, and the signatures
stay Go's. It keeps the defect being removed: `b = strconv.AppendInt(b, n, 10)` with a `nil` `b` compiles
and panics at run time, and the same is true of every go2pu translation of Go code that passes `nil`
(the compiler's own `internal/constant` does). Renaming `append` to `slices.AppendBounded` hides the same
panic behind a longer name. Rejected.

**(iv) One generic destination, either kind (recommended).** The functions are generic over a small
constraint that both `*vec.Vec[T]` and a new `*vec.Fixed[T]` satisfy. Growth and bounds are chosen where
the destination is declared, and each type's name says which it is.

| | (i) Vec | (ii) Fixed | (iii) `[]byte` | (iv) either |
|---|---|---|---|---|
| Go readability | `AppendInt(&v, ...)` | `AppendInt(&f, ...)` | unchanged | `AppendInt(&v, ...)` |
| go2pu | mechanical | needs a size the Go code does not have | compiles, then panics on `nil` | mechanical (Vec) |
| zero allocation | reserve first, or an arena | yes | yes | yes (Fixed) |
| kernel | may allocate; panics when the arena is full | yes | panics past capacity | yes (Fixed) |
| library internals | cannot wrap stack or string buffers | yes | yes | yes (Fixed) |
| honesty | yes | yes, with a truncation flag | no | yes |
| ownership | `&v` borrows an owner | `Fixed` is a view of the caller's array | view | both |

### 3.2 The recommended design

In `pluto/vec`:

```go
// An Appender is a sequence the library's Append functions write into: a
// *Vec[T], which grows from its allocator, or a *Fixed[T], which holds what
// fits in the caller's array and records the rest as truncated.
type Appender[T any] interface {
	Push(x T)
	Append(xs ...T)
	Len() int
	Slice() []T
	Truncate(n int)
}

// A Fixed is a vector over storage the caller provides. It never allocates
// and never panics when full: Push and Append store what fits, drop the
// rest, and record that they did; Truncated reports it.
type Fixed[T any] struct {
	data      []T
	truncated bool
}

func NewFixed[T any](storage []T) Fixed[T]
func (f *Fixed[T]) Push(x T)
func (f *Fixed[T]) TryPush(x T) bool      // for containers: false when full, nothing recorded
func (f *Fixed[T]) Append(xs ...T)
func (f *Fixed[T]) Len() int
func (f *Fixed[T]) Cap() int
func (f *Fixed[T]) Slice() []T
func (f *Fixed[T]) Truncate(n int)
func (f *Fixed[T]) Reset()                 // empty, and clears Truncated
func (f *Fixed[T]) Truncated() bool

// AppendString appends the bytes of s.
func AppendString[D Appender[byte]](dst D, s string)
```

`AppendString` replaces `append(b, s...)`, which no method can express: a `...byte` parameter cannot
accept a string without exposing the string's immutable bytes to the callee, and `Vec[T]` cannot have a
method that exists only for `T = byte`.

The `Append` functions keep their Go names and change their destination:

```go
func AppendInt[D vec.Appender[byte]](dst D, i int64, base int)
func AppendFloat[D vec.Appender[byte]](dst D, f float64, fmt byte, prec, bitSize int)
func AppendRune[D vec.Appender[byte]](dst D, r rune)
func Appendf[D vec.Appender[byte]](dst D, format string, args ...any) // fmt; replaces Bprintf
func AppendWd[D vec.Appender[byte]](dst D) error                       // os
```

They return nothing (the destination is updated in place) or only their error. A function that must not
store a partial result, such as `AppendWd` with a name longer than the room left, checks `dst.Len()` after
writing, restores it with `Truncate`, and returns `ENAMETOOLONG` or `ERANGE` as today.

Go and Plutonium side by side:

```go
// Go
b := make([]byte, 0, 64)
b = strconv.AppendInt(b, n, 10)
b = append(b, ' ')
b = strconv.AppendQuote(b, name)
os.Stdout.Write(b)

// Plutonium, growing
b := vec.WithCapacity[byte](64)
defer b.Free()
strconv.AppendInt(&b, n, 10)
b.Push(' ')
strconv.AppendQuote(&b, name)
os.Stdout.Write(b.Slice())

// Plutonium, no allocation (kernel, interrupt handlers, hot paths)
var mem [64]byte
b := vec.NewFixed(mem[:])
strconv.AppendInt(&b, n, 10)
b.Push(' ')
strconv.AppendQuote(&b, name)
console.Write(b.Slice())
```

Library internals use `Fixed` over their stack arrays and new string buffers:

```go
func FormatInt(i int64, base int) string {
	var buf [65]byte
	b := vec.NewFixed(buf[:])
	AppendInt(&b, i, base)
	return string(b.Slice())
}
```

**Why truncate rather than panic when a `Fixed` is full.** Bounded formatting is mostly logs and messages,
where a cut line is better than a stopped kernel; `snprintf`, `fmt.Bprintf` today and `testing`'s log lines
already behave this way. It is not silent: the type is named for its bound, `Truncated()` reports the loss,
and the functions that must not lose bytes (paths) check and return an error. A container that must not
drop elements uses `TryPush`.

**Ownership.** `&v` lends the owner to the call (a view, section 6.3), and the call may grow it. A `Fixed`
holds a view of the caller's array and no owner, so it copies freely, and the frame rules stop it from
outliving that array: returning a `Fixed` over a local array is a compile error, as returning `mem[:]`
is.

**Cost.** Generic functions are instantiated for each type argument, so the calls are direct and the
per-byte work is the same as `append`'s (a length check and a store). Each `Append` function is compiled
once for each destination type a program uses with it. Kernel mode must judge allocation per instantiation: `AppendInt` with a
`*Fixed` allocates nothing, with a `*Vec` it may. Today's kernel check looks only at calls of functions
marked as allocating, and `vec`'s methods are not, so nothing conflicts now; when allocation is inferred
instead of marked, the inference must look at instantiations, not generic bodies.

**What stays as it is.**

- `bytes.Buffer` already has bounded writes that return `ErrFull`. It changes only inside (`copy` instead
  of `append`). Whether it should become Go's growing `bytes.Buffer` is a separate question.
- `strings.Builder` keeps growing from its allocator. Its fixed mode (`NewBuilder(buf)`, which panics past
  the buffer "as append does") goes: `vec.Fixed[byte]` and `string(f.Slice())` do the same with an honest
  bound. Three uses, all tests.
- `strconv.PutUint` (formats at the end of a buffer and returns the count) stays.

## 4. Copy and concatenation (class (d))

No new functions are needed. Each Go idiom has an existing replacement that says whether it allocates:

| Go | Plutonium | Allocates |
|---|---|---|
| `append([]T(nil), s...)` | `slices.Clone(s)` (a `vec.Vec[T]`) | yes, an owner |
| `append(a, b...)` as a new sequence | `slices.Concat(a, b)` (a `vec.Vec[T]`) | yes, an owner |
| `v = append(v, x)`, `v = append(v, xs...)` | `v.Push(x)`, `v.Append(xs...)` on a `vec.Vec` | when it grows |
| `append(s[:i], s[j:]...)` | `slices.Delete(s, i, j)` | no |
| `slices.Insert(s, i, xs...)`, `slices.Replace(s, i, j, xs...)` | `v.Insert(i, xs...)`, `v.Replace(i, j, xs...)` on a `vec.Vec` | when it grows |
| `append(dst[:0], src...)` into a buffer the code owns | `n := copy(dst[:cap(dst)], src)`, or a `vec.Fixed` | no |
| `s = append(s, x)` within known capacity | `s = s[:len(s)+1]; s[len(s)-1] = x` | no |

`slices.Insert`, `slices.Replace` and `slices.AppendSeq` work within capacity today and panic past it, the
same defect as `append` under Go's names. They leave `pluto/slices`; `vec.Vec.Insert` becomes variadic,
`vec.Vec.Replace` is added, and `vec.Vec.AppendSeq` already exists. `Delete`, `DeleteFunc`, `Compact`,
`CompactFunc` and `Clip` stay: they only shrink. No `slices.Copy` is added; the built-in `copy` covers it.

## 5. The compiler error and go2pu

### 5.1 The error

A new code at the end of the first group of `internal/types2/errors/plutonium.go`:
`AppendNotPermitted`, E2013. `append` stays a predeclared built-in, as `recover` does, so a call finds it
and gets this error instead of "undefined: append"; a program may still declare its own `append`.
`case _Append` becomes:

```go
check.forbidden(call.Fun, AppendNotPermitted, "append is", "a slice cannot grow; use pluto/vec")
```

The message line, in the style of E2001:

```
main.p:6:7: append is not permitted in Plutonium (a slice cannot grow; use pluto/vec)
```

The catalog entry (`internal/diag/catalog_pluto.go`):

```go
&Entry{
	Code: CheckerCode(typerr.AppendNotPermitted), Name: "AppendNotPermitted",
	Text: "Plutonium has no append. In Go, append grows a slice by allocating a larger array behind " +
		"the program's back, and the garbage collector frees the old one. A Plutonium slice is a view " +
		"of an array that something else owns, so it cannot grow.\n" +
		"Use a vec.Vec from pluto/vec, which owns its array and grows it from an allocator. " +
		"To fill a slice's spare capacity without growing it, slice it longer and assign, or use copy.",
	Why:     whyNoGC,
	Fix:     &Fix{Intro: "To build a sequence that grows", Code: "import \"pluto/vec\"\n\nv := vec.New[int]()\ndefer v.Free()\nv.Push(1)\nv.Append(2, 3)"},
	Spec:    pspec("3", "What Plutonium removes from Go"),
	Example: "var s []int\ns = append(s, 1)\ns = append(s, 2, 3)",
	Fixed:   "s := vec.New[int]()\ndefer s.Free()\ns.Push(1)\ns.Append(2, 3)",
},
```

An entry in `specialisers` (`internal/diag/typecheck.go`) replaces the Fix with the one for the shape of the call, so
the common idioms get their exact replacement:

| Call | Fix shown |
|---|---|
| `x = append(x, v)` / `append(x, vs...)` | `x.Push(v)` / `x.Append(vs...)`, with `x` a `vec.Vec[T]` |
| `append(b, s...)` with a string `s` | `vec.AppendString(&b, s)` |
| `append([]T(nil), s...)`, `append([]T{}, s...)` | `slices.Clone(s)` |
| `c := append(a, b...)` | `slices.Concat(a, b)` |
| `append(s[:i], s[j:]...)` | `slices.Delete(s, i, j)` |
| `append(buf[:0], ...)` of a local array | `vec.NewFixed(buf[:])`, then `Push`/`Append` |

The rest of the compiler:

- `goPlain["InvalidAppend"]` can no longer be reached from a call (E2013 comes first). Its text drops the
  capacity rule and points at section 3.
- The texts of E2544 and E2545 recommend `fmt.Appendf` and `strconv.AppendInt` into a `vec.Fixed` in place
  of `fmt.Bprintf`.
- `appendCall`, `appendCheck`, the `append` case in `closureflow.go` and `rt.panicAppend` are deleted.
- A diagnostics program `cmd/pluto/testdata/diag/append/main.p` covers each shape in the table, checked
  with `go test ./cmd/pluto -run TestDiagnostics -update` (both `default.out` and `brief.out`).

### 5.2 go2pu

Today (`tools/go2pu/rewrite.go`, `builtin`): `append(b, s...)` with a string becomes
`compat.AppendString(b, s)`, and every other `append` becomes `compat.Append(...)`, which grows from
`alloc.Default()` and never frees the old array (it may be shared). Go's `strconv.Append*`,
`utf8.AppendRune` and `fmt.Appendf` are passed through unchanged, so a `nil` destination translates
cleanly and panics at run time.

After the change:

| Go | Translation (compat path) | Translation (`pu` path, self-hosting plan section 11) |
|---|---|---|
| `append(s, x...)` | `compat.Append(s, x...)`, unchanged | `v.Push(x)` / `v.Append(xs...)` on `pu.Vec`, so `vec.Vec` |
| `append(b, s...)`, string `s` | `compat.AppendString(b, s)`, unchanged | `vec.AppendString(&v, s)` |
| `append([]T(nil), s...)` | `compat.Append([]T(nil), s...)`, unchanged | `slices.Clone(s)` |
| `strconv.AppendInt(b, ...)` and the other Append functions on `[]byte` | `compat.AppendInt(b, ...)`, a wrapper that formats into a `vec.Fixed` on its stack and grows `b` as `compat.Append` does | `strconv.AppendInt(&v, ...)` |

`compat.Append`, `compat.AppendString` and `compat.Buffer` are rewritten with `copy` and slicing (their 3
uses of the built-in). The wrappers cover the 4 functions the compiler's Go source uses (`AppendInt`,
`AppendFloat`, `AppendRune`, `Appendf`); any other gets a go2pu note. The compat path stays what the
self-hosting plan says it is: right for arenas, wrong for freeing allocators, and transitional.

## 6. Migration

### 6.1 Order

Each step leaves `go test ./...`, lint and coverage passing. The compiler error comes last, when nothing
in the tree calls `append`.

1. **`pluto/vec` groundwork.** `Fixed`, `Appender`, `AppendString`; `Vec.Insert` variadic, `Vec.Replace`;
   `vec` stops using `append` internally (`copy` into the room it reserved). Unit tests, and a
   `testdata/lib` program for `Fixed` and truncation.
2. **The `Append` functions, by package, in import order:** `unicode/utf8`, `strconv` (the differential
   test against Go's `AppendFloat` and `ParseFloat` must stay exact), `strings` (and `Builder` internals,
   removing `NewBuilder`), `time`, `os`, `os/exec`, `path/filepath`, `fmt` (`Append`, `Appendf`,
   `Appendln` replace `Bprint` and `Bprintf`; the sink's internals), `testing`, `flag` (which gains a length
   check it lacks today), `errors`, `bytes` (internals only), `slices` (remove `Insert`, `Replace`,
   `AppendSeq`; `Delete` with `copy`). Each package converts its callers in the same change. The spec's
   section 13 rows change with their packages.
3. **Tests and programs.** `testdata/lib` (24 calls), `testdata/kernel` (`Bprintf` to `Appendf` into a
   `Fixed`), `examples/11-alloc` (shows a `vec.Vec` growing from the arena, with the same output), the
   program in `strconv_diff_test.go`, the library's own `_test.p` files, and the 9 `testdata/run` programs
   (section 6.2).
4. **selfhost and go2pu.** Rewrite `compat`'s 3 calls; add the compat wrappers and go2pu's mapping for the
   `Append` functions; regenerate the translations (only those calls should change) and run the demos.
5. **The compiler.** E2013, its catalog entry and its `specialisers` entry; delete the lowering and
   `rt.panicAppend`; update `InvalidAppend`, E2544 and E2545; the diagnostics program; the VS Code
   grammar.
6. **The spec.** Section 3's row becomes `append`; section 4.3 keeps its number (so later references do
   not move) and becomes "Slices do not grow", saying what a slice can still do and pointing at `vec.Vec`
   and `vec.Fixed`; 6.1 and 6.6 lose their `append` sentences; 6.9's "Duplicating owners with `copy` or
   `append`" becomes `copy` only; section 8's fault list loses "`append` beyond capacity"; 12.7 names
   `fmt.Appendf` and `vec.Fixed`; `debugging.md` section 2 drops `append`. The roadmap gets an item.

### 6.2 The `testdata/run` programs

They are in the common subset of Go and Plutonium, and their `.out` files come from Go. Every one of
their `append` calls stays within a capacity the program chose, so each can be written with slicing,
which is valid Go with the same result, and all 9 stay in the common subset. Running
`go test ./internal/build -run TestRun -update` afterwards must leave every `.out` unchanged.

| Program | Calls | Use | Rewrite |
|---|---:|---|---|
| `rangefuncgo.p` | 34 | collects results in `make([]int, 0, 64)` (Go's range-over-func tests) | a local `push(&result, x)` that slices one longer and assigns |
| `containerheap.p` | 3 | `heap.Interface.Push` into `make([]int, 0, 1000)` | `n := len(*h); *h = (*h)[:n+1]; (*h)[n] = x.(int)` |
| `strcount.p` | 3 | counted strings stored into a `[8]string` through `append` | the same stores by slicing and assignment, which retain and release the same way |
| `slicespkg.p` | 2 | fills a slice for `slices.Insert` and `Replace` | fill by slicing; the `Insert`/`Replace`/`AppendSeq` section moves to a `testdata/lib` program on `vec.Vec` |
| `slices.p` | 2 | prints `append`'s length and capacity | write the elements by slicing; the printed line keeps its values |
| `directstruct.p` | 1 | interfaces into `make([]error, 0, 3)` | slicing |
| `genbuiltins.p` | 1 | `Grow[S ~[]E]` tests `append` through a core type | `Grow` fills `make(S, n)` by index; the comment drops `append` |
| `libidioms.p` | 1 | a stack's `push` after checking it is not full | slicing |
| `tp_issue48645b.p` | 1 | `append(acc, e)` in a `Reduce` callback that never runs | slicing (the callback still never runs) |

### 6.3 Size and risk

| Part | Scope | Risk |
|---|---|---|
| Compiler | one error code, a catalog entry, a `specialisers` entry, about 60 lines of lowering and runtime deleted | low: the error is checked before lowering ever sees a call |
| `pluto/vec` additions | one type, one constraint, one function, two methods | low |
| Library conversion | 153 call sites in 21 files, the 27 functions' signatures, about 45 internal callers, 15 test call sites | medium: `strconv` formatting must stay bit-exact (the differential test covers it); path functions must keep their error behavior on long names; call sites get a line longer where a stack array becomes a `Fixed` |
| Tests, examples, kernel tests | 76 call sites in 19 files, plus 3 `Bprintf` in kernel tests | low: outputs must not change |
| selfhost and go2pu | 3 calls, 4 wrappers, one mapping, regeneration | low |
| Spec and docs | about 12 places | low |

Binary size grows slightly where a program uses an `Append` function with both destinations.

## 7. What this does not change

Slices still index, slice, `copy`, `clear` and range as in Go. `make([]T, n, cap)` still gives a slice with
spare capacity, which a program fills by slicing. Strings still concatenate with `+`. `strings.Builder` is
still the way to build a string that grows.

## 8. Decisions

1. Remove the built-in `append`: every call is E2013 `AppendNotPermitted`, with the message, catalog text
   and shape-specific fixes of section 5.1. `append` stays predeclared so the error can name it.
2. The library's `Append` functions keep their Go names and become generic over `vec.Appender[byte]`,
   writing into a `*vec.Vec[byte]` (grows) or a `*vec.Fixed[byte]` (bounded). They return nothing, or only
   their error.
3. Add `vec.Fixed[T]`: storage from the caller, never allocates, never panics when full; `Push` and
   `Append` store what fits and record the rest, `Truncated()` reports it, `TryPush` refuses instead. Add
   `vec.AppendString` for `append(b, s...)`.
4. `fmt.Append`, `fmt.Appendf` and `fmt.Appendln` (Go 1.19's names) replace `fmt.Bprint` and
   `fmt.Bprintf`.
5. `slices.Insert`, `slices.Replace` and `slices.AppendSeq` leave `pluto/slices` for `vec.Vec` methods;
   the shrinking functions stay. No `slices.Copy`.
6. `strings.NewBuilder(buf)` (the fixed mode that panics "as append does") is removed.
7. The 9 `testdata/run` programs stay in the common subset, rewritten with slicing; their `.out` files
   must not change.
8. go2pu keeps `compat.Append` for the compat path and adds compat wrappers for the `Append` functions
   the compiler uses; the `pu` path translates to `vec.Vec` and the generic `Append` functions.
9. Kernel mode judges allocation per instantiation of a generic function, so `AppendInt` into a `Fixed`
   is allowed where allocation is not.

Open, not part of this change: whether Plutonium's `bytes.Buffer` (bounded, `ErrFull`) should become Go's
growing `bytes.Buffer` and the bounded one be expressed with `vec.Fixed[byte]`.
