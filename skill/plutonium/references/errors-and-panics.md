# Errors and panics

Recoverable failures are `error` results. Bugs are panics, and a panic **cannot be caught**: there is no
`recover` (E2004). There is no `try`, no exception and no stack unwinding. Spec chapters 16 and 17.

## 1. Error values (chapter 16)

`error` is the usual interface (`Error() string`). A function that can fail returns `error` as its last result, and
the caller tests `err != nil` or uses an **or clause** (section 3 below). Where the storage of an error lives is part
of the language, because nothing is allocated behind your back (section 16.2):

| Kind of error | How to write it | Storage |
|---|---|---|
| fixed message | `var ErrX = errors.New("text")` or `return errors.New("text")` | none: a counter in static memory |
| code | `type Status int32` with `Error() string` | the interface's data word |
| one pointer | `type E struct{ d *detail }` with `Error()` (the pointee must outlive the call: a package variable, a field of a longer-lived object) | the data word |
| message with context | `fmt.Errorf("open %s: %w", name, err)`, `errors.Join(a, b)` | a counted `synced` object, freed with the last copy |
| library errors | `*os.PathError`, `*strconv.NumError` | counted objects, made only when an operation fails |
| in a longer-lived object | a field `fmt.ErrorBuf` returned as `error` | the field |

- `errors.New(text)`: `text` must be a **constant** (E2008: use `fmt.Errorf("%s", s)`), and it must be called, not
  taken as a value or deferred (E2542). **Each evaluation is a new error**, as in Go: two results of one call site
  are unequal, a copy of one is equal to it, and a package-level `var ErrX = errors.New(...)` is evaluated once. It
  allocates nothing and works before any allocator exists.
- Returning a struct that is copied into the returning function's frame, or a pointer to a local, as `error`
  is E3001: `return pathError{op, path}` where `pathError` has two strings fails. Use a counted error
  (`fmt.Errorf`), a code type, or a single-pointer struct.
```pluto-error E3001
package main

type pathError struct{ op, path string }

func (e pathError) Error() string { return e.op + " " + e.path }

func open() error {
	return pathError{"open", "x"} // the interface would hold a copy in open's frame
}

func main() { _ = open() }
```

- A type that holds a `shared[T]` cannot become an `error` (E2020: every error may cross tasks). Hold a
  `synced[T]`. Your own error type that needs heap storage is `alloc.Synced[MyErr]()` returned as `error`.
- No code frees errors; there is no `errors.Free`.
- Converting an `error` to `any` lends it: it cannot outlive the `error` it came from.

## 2. Wrapping and matching (section 16.6)

- `%w` in `fmt.Errorf` wraps an error (several `%w` wrap several). `errors.Unwrap`, `errors.Is(err, target)`,
  `errors.As(err, &target)` and `errors.AsType[E](err)` walk the tree. `errors.Join(a, b)` joins non-nil errors.
- `errors.As` matches a counted library error against a **pointer target**: `var pe *os.PathError;
  errors.As(err, &pe)` works though the dynamic value is a counted box. The pointer borrows the error; keep the
  `err` alive while you use it.
- `errors.Is(err, os.ErrNotExist)` and `strconv.ErrSyntax` work through the wrapping. `syscall.Errno` values match
  `os.ErrNotExist`, `os.ErrExist`, `os.ErrPermission`.

## 3. The or clause (section 11.27)

`x := f() or return` is the short form of "call, test the error, leave". `err` is declared inside the clause.

| Form | Meaning |
|---|---|
| `f() or return` | in a function whose last result is `error`: return `err` as that result (other results: zero values if unnamed, current values if named) |
| `f() or return E` | several results, last is `error`: return the single expression `E` as the error (`err` is in scope in `E`) |
| `f() or return 0, err` | an ordinary return statement |
| `f() or continue` / `break` / `goto L` | leave the statement that way |
| `f() or panic(err)` | panic |
| `f() or { ...; return err }` | a block that must end in a statement that leaves (return, break, continue, goto, panic, or an `if/else` whose branches leave) |

- The operand is a **call** written directly before `or`, on the same line, whose **last result is exactly `error`**
  (not a type that implements it). A conversion or built-in is E2461; a call with no error result E2462; a
  clause that can complete E2463; a bare `return` clause in a function whose last result is not `error` E2464;
  an or statement as a `for` post statement E2465; a non-call operand E1013.
- The values are the results except the last: `n := strconv.Atoi(s) or return` (one value), `a, b := f() or return`
  for a three-result call. A call whose results besides the error matter (for example a count of bytes read before
  the failure) cannot use `or`; use `if`.
- It is a statement: an ordinary statement, or the init of `if`, `switch` or `for`. Not a `var`, `defer`, `task`,
  `select` case, nor an expression: `foo(bar() or return)` is not valid.
- `err != nil` plus `return err` keeps working, and means what it does in Go.

```pluto-run exit=0
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/os"
	"pluto/strconv"
)

var ErrEmpty = errors.New("empty input") // one error for the whole program

func mk() error { return errors.New("fresh") } // a new error at every call

type Status int32

const (
	StatusOK Status = iota
	StatusBusy
)

func (s Status) Error() string {
	if s == StatusBusy {
		return "device busy"
	}
	return "unknown"
}

type detail struct {
	line int
	msg  string
}

type syntaxErr struct{ d *detail } // one pointer: fits the interface word

func (e syntaxErr) Error() string { return fmt.Sprintf("line %d: %s", e.d.line, e.d.msg) }

var lastDetail = detail{3, "unexpected token"} // outlives every call

func parse() error { return syntaxErr{&lastDetail} }

func parseAll(fields []string) (parsed int, err error) {
	for _, f := range fields {
		strconv.Atoi(f) or return // parsed keeps the count so far; err is set
		parsed++
	}
	return
}

func atoi(s string) (int, error) {
	n := strconv.Atoi(s) or return 0, fmt.Errorf("atoi %q: %w", s, err)
	return n, nil
}

func main() int64 {
	a, b := mk(), mk()
	fmt.Println(a == b, a == a, a.Error() == b.Error())
	fmt.Println(errors.Is(fmt.Errorf("ctx: %w", ErrEmpty), ErrEmpty))
	var e error = StatusBusy
	fmt.Println(e, e == StatusBusy)
	fmt.Println(parse())
	var se syntaxErr
	fmt.Println(errors.As(fmt.Errorf("wrap: %w", parse()), &se), se.d.line)

	n, err := parseAll([]string{"1", "2", "x", "4"})
	fmt.Println(n, err != nil)
	_, err = atoi("zz")
	fmt.Println(err)
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		fmt.Println(ne.Func, ne.Num, errors.Is(err, strconv.ErrSyntax))
	}
	j := errors.Join(ErrEmpty, StatusBusy)
	fmt.Println(errors.Is(j, StatusBusy), errors.Is(j, ErrEmpty))
	_, err = os.Open("/no/such")
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		fmt.Println(pe.Op, pe.Path, errors.Is(err, os.ErrNotExist))
	}

	if v := strconv.Atoi("12") or return 1; v > 10 { // or as the init of an if
		fmt.Println("big", v)
	}
	for _, s := range []string{"1", "q", "3"} {
		x := strconv.Atoi(s) or { // a block clause must leave
			fmt.Println("skip", s)
			continue
		}
		fmt.Println("x", x)
	}
	return 0
}
```

```output
false true true
true
device busy true
line 3: unexpected token
true 3
2 true
atoi "zz": strconv.Atoi: parsing "zz": invalid syntax
Atoi zz true
true true
open /no/such true
big 12
x 1
skip q
x 3
```

```pluto-error E2463
package main

import "pluto/strconv"

func f(s string) int {
	n := strconv.Atoi(s) or println("bad") // the clause can complete: it must leave
	return n
}

func main() { _ = f("1") }
```

```pluto-error E2464
package main

import "pluto/strconv"

func main() {
	strconv.Atoi("1") or return // main has no error result to carry err
}
```

```pluto-error E2008
package main

import "pluto/errors"

func fail(msg string) error { return errors.New(msg) } // use fmt.Errorf

func main() { _ = fail("x") }
```

```pluto-error E2004
package main

func main() {
	defer func() { recover() }() // there is no recover
}
```

## 4. Panics (chapter 17)

A panic stops the program: control never returns to the function that panicked, **owners are not dropped**, and
`recover` does not exist. In a hosted build the panicking task first runs its pending deferred calls (innermost
frame first, unable to stop the panic), then the handler prints `panic: <message>` and a trace to standard error
and the process exits with **status 2**. Other tasks keep running meanwhile. Freestanding and kernel builds run no
deferred calls; the program defines its handler with `//pu:panic` (`kernel-mode.md`).

Run-time panics: index or slice bounds, `strings.Slice` off a rune boundary, integer division by zero, a negative
shift, failed type assertion (single-result form) or union assertion, nil map write, nil pointer or nil box
dereference, calling a nil function, send on or close of a closed channel, `make` with a negative size, out of
memory or no allocator, use of freed memory (`pluto build --gencheck=false` removes the check), stack overflow in
a task, an iterator calling `yield` after it returned false, and `panic(v)`. Integer overflow wraps, floating
point never panics, a float-to-int conversion out of range gives an unspecified value. A deadlock is a fatal error
(status 2), not a panic.

`panic(v)` takes any value; strings, errors, booleans and integers become the message. Use panic for bugs
(unreachable states, violated invariants) and `error` for anything a caller can handle.

```pluto-run exit=2
package main

import "pluto/fmt"

func f(i int) int {
	var a [4]int
	return a[i]
}

func main() {
	defer fmt.Println("deferred runs before the panic message")
	fmt.Println("start")
	fmt.Println(f(7))
	fmt.Println("never")
}
```

```output
start
deferred runs before the panic message
```

## 5. Ending a program or a task

| Call | Effect |
|---|---|
| `return n` from `func main() int64` | exit status `n`; package-level owners are dropped if no other task runs |
| `os.Exit(n)` | ends at once from any task; drops nothing, runs no deferred call |
| `panic(...)`, a run-time panic | status 2 after deferred calls (hosted) |
| `rt.ExitTask()` | ends the calling task after running its deferred calls and drops; in `main` the program waits for the other tasks and then fails with status 2 |

`pluto/testing` ends a failing test with `FailNow`/`Fatal`, which call `rt.ExitTask`, so deferred calls and drops
run when a test stops early.

## 6. Quick answers

| Question | Answer |
|---|---|
| How do I catch a panic? | You do not. Return an `error` where failure is expected |
| `errors.New` with a computed message? | `fmt.Errorf("%s", s)` |
| Compare to a sentinel? | `errors.Is(err, ErrX)` or `err == ErrX` for a package-level sentinel |
| Get the underlying type? | `errors.As(err, &target)` with `target` of the pointer or value type |
| Stop on the first error in a loop? | `x := f(i) or return` (or `break`) |
| Ignore an error knowingly? | assign it: `_ = f()`; an unused result of `error` type is not an error in Go either |
