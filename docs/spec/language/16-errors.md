## 16. Errors

### 16.1 The error interface

The predeclared type `error` is defined as

```go
type error interface {
	Error() string
}
```

It is the conventional interface for representing an error condition, with the `nil` value representing
no error. For instance, a function to read data from a file might be declared:

```go
func Read(f *File, b []byte) (n int, err error)
```

A function that can fail returns an `error` as its last result. The caller tests it against `nil`, and
passes it up with `if err != nil { return err }`, or with the or clause, its short form
(section 11.27, Or clauses): `v := f() or return`. There is no `try` operator and no exception. An or
clause names its exit on the line that has it, and the error remains a value that the program tests and
handles.

A panic cannot be caught (section 13.1, Built-ins Plutonium does not have). Errors are therefore the only
way to recover from a failure. A failure that a program must survive is an `error` result, and a failure
that is a bug in the program is a panic (chapter 17, Run-time panics).

`error` is an ordinary interface (section 7.9, Interface types). A value converted to `error` is either
held in the interface's data word, or the interface holds a pointer. An owner converted to an ordinary
interface is lent: the interface looks at the owner and does not own it (section 7.9). The rules that
follow say where the storage behind an `error` lives, since Plutonium never allocates without an owner
(section 18.10, No allocation without an owner or an arena).

### 16.2 Where an error lives

**An error returned from a function is either a value that fits in a word, or a pointer to storage that
outlives the function.** That storage is one of the following.

| Storage | Used for | Example |
|---|---|---|
| the interface's data word | error codes, structs wrapping one pointer (section 7.9), and `errors.New` results (a per-site type that holds a counter) | `syscall.Errno`, a kernel's own code type, `struct{ d *detail }`, `return errors.New("bad header")` |
| static storage | sentinel errors and fixed messages | `var ErrClosed = &errors.Text{Msg: "file already closed"}` |
| a field of a longer-lived object | errors with context that belong to an owner | a parser that returns `&p.err`, `fmt.ErrorBuf` |
| a counted object | errors with context that travel up the stack | `fmt.Errorf`, `errors.Join`, `*os.PathError`, `*os.NameError`, `*strconv.NumError` |

A counted object is a heap object that the `error` values referring to it count, and that is freed when
the last of them goes (section 16.7).

Returning a struct whose interface representation needs a copy in the returning function's frame,
or a pointer to a local variable or local composite literal, breaks the rule. These are compile errors
(E3001, sections 7.9 and 18.3). A single-pointer struct stored directly in the data word is permitted
when its referenced storage outlives the result. Larger frame-backed values such as
`return PathError{...}` and `return &PathError{...}` are caught by the frame rule.

```go
type pathError struct{ op, path string }

func (e pathError) Error() string { return e.op + " " + e.path }

func open() error {
	return pathError{"open", "x"} // error E3001: the copy would live in open's frame
}
```

An error value that a package-level initializer makes needs no such care. Its copy is static
(section 15.2, Package initialization), so `var ErrBadMagic error = formatError{want: 0x7f}` is valid.

### 16.3 Sentinels and `errors.New`

`errors.New(text)` works anywhere: in a package-level variable and inside a function.

```go
package main

import "pluto/errors"

var ErrNotFound = errors.New("not found")

func parseHeader(b []byte) error {
	if len(b) < 4 {
		return errors.New("short header")
	}
	return nil
}

func main() {
	println(parseHeader(nil) != nil, ErrNotFound.Error())
}
```

The argument `text` must be a constant. Any other argument is a compile error (E2008) that points to
`fmt.Errorf`. Each call site has its own error type, made when compiling, and its own counter in static
storage. Each evaluation of the call adds one to the counter and returns an error whose dynamic type is
the site's type and whose data word holds the new counter value. The type's `Error` method returns
`text`. So `errors.New` never allocates and never runs out of memory, and it works before an allocator
exists, in interrupt context and in every build mode.

- Each evaluation returns a new error, as in Go. Two results of one call site have the same type and
  different counter values, so they are not equal. A copy of a result is equal to it.
- Different call sites have different types, even with equal messages, so two packages' sentinels never
  compare equal.
- A package-level `var ErrX = errors.New("x")` is evaluated once, so it is one error for the whole
  program, as in Go.
- A counter that has reached its maximum (2^64 - 1 evaluations of one site) panics at the next
  evaluation (section 17.1, Panics). It never wraps to a value that would make two errors equal. Reaching it is
  not possible in practice: it takes about 584 years at one evaluation per nanosecond.
- Since each call site has its own type, `errors.New` must be called directly. Taking its value
  (`f := errors.New`) or deferring it is a compile error (E2542).

```go
e1, e2 := parseHeader(nil), parseHeader(nil)
println(e1 == e2) // false, as in Go
e3 := e1
println(e1 == e3) // true, as in Go
```

A sentinel may also be written as a static literal, `var ErrClosed = &errors.Text{Msg: "file already
closed"}`.

### 16.4 Error codes

A named integer type with an `Error() string` method fits in the data word of an interface, so values of
it can be returned from anywhere and cost nothing. The standard library's `syscall.Errno` is the model:
its `Error` returns the system's message for the code and its `Name` returns the symbol (`"ENOENT"`). Its
`Is` method makes `errors.Is(err, os.ErrNotExist)`, `os.ErrExist`, `os.ErrPermission` and
`errors.ErrUnsupported` match the corresponding codes (section 16.6). A kernel's own error codes, a
driver's status codes and protocol errors take this shape.

```go
type Status int32

const (
	StatusOK Status = iota
	StatusBusy
	StatusFault
)

func (s Status) Error() string {
	switch s {
	case StatusBusy:
		return "device busy"
	case StatusFault:
		return "device fault"
	}
	return "unknown status"
}

func start(busy bool) error {
	if busy {
		return StatusBusy // in the data word: no allocation
	}
	return nil
}
```

### 16.5 Errors with context

An error that records context (a path, a line and column, the operation that failed) needs storage for
it. Its text is a string (section 7.3, String types), formatted when the error is made: the error object
holds the strings, and the drop of the object releases them (section 18.8, Strings). Two places hold such
an error.

- **A counted object**, for errors that travel. `fmt.Errorf(format, args...)`, `errors.Join` and the
  error types of the standard library make one, and return it in an `error`. Section 16.7 gives the rules.
- **A field of a longer-lived object**, for errors that belong to an owner. An object that performs
  operations (a parser, a device, a connection) keeps its last error in a field and returns a pointer to
  it. The error is valid while the object lives and until its next error. `fmt.ErrorBuf` is such a
  field: `buf.Errorf(format, args...)` formats into a buffer of its own, wraps the first `%w` operand, and
  returns the buffer as the error. Code that keeps such an error longer wraps it with `fmt.Errorf`, which
  copies the message.

**Library error types.** The functions of the standard library report failures with error types that
record the context. A failed file operation returns a `*os.PathError`, with the operation, the path and
the cause. A failed parse in `strconv` returns a `*strconv.NumError`. `errors.Is(err, strconv.ErrSyntax)`
and `errors.Is(err, os.ErrNotExist)` hold for them. These errors are counted objects, allocated only when an operation fails. The sentinels `io.EOF`
and `os.ErrClosed` stay sentinels. A successful operation allocates nothing.

```go
var perr *os.PathError
if errors.As(err, &perr) {
	println("missing file:", perr.Path)
}
```

### 16.6 Wrapping, `Is` and `As`

An error wraps another error by holding it. `fmt.Errorf` accepts `%w`, which formats an error operand
like `%v` and wraps it. With one `%w` its result has `Unwrap() error`; with several it has
`Unwrap() []error` and wraps all those operands. `ErrorBuf.Errorf` instead keeps only the first `%w`
operand as its cause and has `Unwrap() error`; additional `%w` operands format like `%v` but add no
causes. `errors.Is` and `errors.As` follow only that first cause. This preserves ErrorBuf's bounded
buffer contract (section 16.5). The verb `%w` in any other formatting function prints as a bad verb.

`errors.Unwrap(err)` returns the result of calling `Unwrap() error` on `err`, or `nil` if `err` has no such
method. `errors.Join(errs ...error)` returns an error that wraps the non-nil errors it is given, with
`Unwrap() []error`, or `nil` if there are none.

The **tree** of an error `err` is `err` itself, followed by the trees of the errors that it wraps, found
with `Unwrap() error` or `Unwrap() []error`, in a preorder walk.

- `errors.Is(err, target)` reports whether any error in the tree of `err` is equal to `target`, or has a
  method `Is(error) bool` that reports `true` for `target`. It compares with `==` only when the dynamic
  type of `target` is comparable, so it never panics.
- `errors.As` is generic: `func As[E any](err error, target *E) bool`. It finds the first error in the
  tree of `err` whose dynamic type is assignable to `E`, stores it in `*target`, and reports `true`; it
  reports `false` if there is none. The call `errors.As(err, &perr)` works without reflection, with `E`
  inferred from the type of `target`.
  A counted `shared[T]` or `synced[T]` error also matches a `*T` target; the pointer stored in
  the target borrows the object and remains valid while the counted error is held (section 16.7).
- `errors.AsType[E error](err error) (E, bool)` returns the first error in the tree that `errors.As` would store in a target of type `E`, and whether there was one. A counted `shared[T]` or `synced[T]` error therefore matches `E = *T`, and the result borrows the object as for `As` (section 16.7).

```go
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/os"
)

func loadConfig(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defer f.Close()
	return nil
}

func main() int64 {
	err := loadConfig("/nonexistent/app.conf")
	if err == nil {
		return 0
	}
	fmt.Println(err) // load config: open /nonexistent/app.conf: no such file or directory
	var perr *os.PathError
	if errors.As(err, &perr) {
		fmt.Println("missing file:", perr.Path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return 2
	}
	return 1
}
```

### 16.7 Allocated errors and ownership

Every ordinary allocation has an owner, arena or counted-reference lifetime responsibility
(section 18.10). An error that `fmt.Errorf` returns is an
allocation, but the `error` that holds it is an ordinary interface, which only looks at what it holds.
So allocated errors are **counted**: the object is released exactly once, when the last `error` value
that refers to it goes, and no code releases it by hand.

1. `fmt.Errorf`, `errors.Join` and the error types of the standard library (`*os.PathError`,
   `*os.NameError`, `*strconv.NumError`) allocate their error as a `synced[T]` object (section 7.19, `owned`, `shared`,
   `synced` and `weak`), and return it in an `error`. The object's header records the allocator and the
   count.
2. An interface value of type `error`, or of an interface type that embeds `error`, counts a
   `synced` reference that it holds. Copying the interface increments the count. Dropping it, or
   overwriting it, decrements the count. At zero the error's `Drop` runs, which drops the errors it wraps,
   and the memory is freed to the allocator recorded in the object (section 18.7, Shared ownership). In an
   arena, freeing does nothing and the arena reclaims the memory when it is reset.
3. Every other error value copies as a value: codes, sentinels, and errors that live in a field of an owner.
   Whether an `error` value is counted is a property of its dynamic type, so a program whose errors are
   only codes, as a kernel's are, pays nothing beyond one predictable test when it copies or drops a
   non-nil error.
4. `%w` copies the wrapped error into the wrapper (count plus one). A wrapper keeps what it wraps alive,
   and the caller's own copy is released when it goes out of scope.
5. There is no `errors.Free`. Errors made under an arena (`defer alloc.Use(&a).Restore()`) are freed
   into the arena, and `a.Reset()` reclaims them.
6. A program's own error type that needs heap storage is allocated with `alloc.Synced[MyErr]()` and
   returned as an `error`. An owner converted to `error` directly is only lent (section 7.9), so returning
   it is the compile error of section 18.3 (E3001). A value whose type is not sendable (section 8.8,
   Sendable types), in practice one that holds a `shared[T]`, cannot be converted to `error` at all
   (E2020); the message names the `shared[T]` and shows `alloc.Synced`.
7. The count is that of a `synced` object (section 18.7, Shared ownership, When counts are atomic): atomic
   once the program has started a task, so every `error` value may be sent between tasks (section 19.2,
   The sendable rule).
8. The text in an error is a string (section 7.3). An error object is then a small struct of strings and
   wrapped errors, such as `struct{ msg string; err error }`, and its `Drop` needs no code of its own for
   the text: the drop of the object releases the strings, and a message that is a substring of another is
   shared and not copied. Only the object itself, and the errors it wraps, need the counting above.

Converting an `error` to `any`, or to an interface that does not embed `error`, lends the error (section
7.9): the new interface does not count it, and may not outlive the `error` it came from.

Code that handles errors does not change: `if err != nil { return err }` hands the error to the caller,
`v := f() or return` does the same with the error unchanged (section 11.27, Or clauses), and
`errors.Is(err, os.ErrNotExist)` looks at `err` without taking it.

```go
func run(name string) error {
	if err := loadConfig(name); err != nil {
		return fmt.Errorf("run %s: %w", name, err) // the wrapper counts err; run's copy is released
	}
	return nil
}
```

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
