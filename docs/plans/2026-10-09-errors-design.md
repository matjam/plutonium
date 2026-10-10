# Error Handling in Plutonium: Design

**Date:** 2026-10-09
**Status:** library conventions implemented; three language additions proposed (section 9)

## 1. The problem

`error` is an interface, and Go code builds error values in ways that Plutonium's rules do not allow:

- `errors.New(s)` and `fmt.Errorf(...)` allocate on the garbage-collected heap. Plutonium has no hidden
  allocation (spec section 6).
- `return &PathError{Op: "open", Path: name, Err: e}` returns the address of a composite literal. In
  Plutonium that literal is a temporary in the returning function's frame (spec section 6), so the
  pointer dangles as soon as the function returns.
- `return PathError{...}` as an `error` copies the struct into the converting function's frame (spec
  4.4), and returning such an interface is a compile error.
- Error messages are often built by concatenation (`e.Op + " " + e.Path + ": " + e.Err.Error()`), which
  Plutonium forbids at run time because it allocates.

Yet error handling has to stay Go-simple: `if err != nil { return err }`, `errors.Is`, `errors.As`, and
wrapping with context. A Go programmer should read Plutonium error handling without a manual.

## 2. Constraints

From the spec and the code as of this date:

1. An interface value is two words. A value that fits in a word (integer, pointer, `owned`/`shared`
   pointer) is stored in the data word; a larger value is boxed in the converting function's frame
   (4.4).
2. Returning or storing in a package variable an interface that holds a frame box is meant to be a compile
   error where the compiler can see it (4.4). This is not enforced yet; a separate memory-safety change
   owns the escape checks (section 8).
3. A package-level initializer runs once in the package's init function. `&T{...}` and slice literals
   there get static storage (`staticLits` in `internal/lower`). As of commit `f73a3e0` interface boxes
   made there are static too, so `var ErrX error = someStruct{...}` is valid (it used to point into the
   dead init frame).
4. Memory comes from an allocator: the default one (`alloc.Default()`), or one passed explicitly. The
   library's convention for "default unless the caller says otherwise" is a pair of functions, `F` and
   `FIn(a, ...)` (`alloc.New`/`alloc.NewIn`, `strings.NewBuilder`/`NewBuilderIn`), because default
   parameter values (spec 5.1) are not implemented yet.
5. `panic` aborts. There is no `recover`, so errors are the only recoverable failure path, and the error
   path must work in a kernel: before an allocator exists, in interrupt context, and without leaking
   memory per failure in a long-running system.

## 3. What the library did before

- `errors.New` allocated a `*errors.Text` from the default allocator. It cannot run in a freestanding
  build's package initialisation (no allocator yet) and leaks one object per call when used at run time.
- Sentinels were written as static literals: `var EOF = &errors.Text{Msg: "EOF"}`. They work, but their
  static type is `*errors.Text`, not `error`, and the spelling is not Go's.
- `errors.Is` was `==`. There was no `As`, `Unwrap` or `Join`.
- `syscall.Errno` knew twelve error numbers and printed "system call error" for the rest.
- `os.Open` returned a bare `Errno`, with no operation or path; `os` had a private copy of `io.EOF`.
- `fmt` printed errors with `%v`, `%s` and `%q`, and had no `Errorf`.
- `strconv` returned the `ErrSyntax` and `ErrRange` sentinels directly.
- Two programs returned `&T{...}` as an error: `examples/15-interfaces` and `testdata/run/ifaces.p`. They
  passed only because nothing overwrote the dead frame before the error was read.

## 4. The options

### 4.1 Sentinel errors

A sentinel is a package-level `var` compared by identity: `var ErrNotFound = errors.New("not found")`.
It needs storage that lasts for the whole program and has an identity of its own.

| Option | Verdict |
|---|---|
| Static literal: `var ErrX = &errors.Text{Msg: "x"}`. `staticLits` already makes it static. | Works today with no compiler change, but its type is `*errors.Text` and it is not how Go spells it. Kept as an alternative form. |
| `errors.New` allocates from the default allocator. | Panics in freestanding package initialisation, where kernels declare their sentinels. Rejected. |
| `errors.New` returns a `Text` value and the caller's package-level `var` boxes it statically. | `New` itself makes the interface, inside its own frame, so the box would be in `New`'s frame. Does not work without inlining at lowering time. Rejected. |
| `errors.New` deduplicates by message (returns the same pointer for the same text). | Two packages that both define `New("not found")` would compare equal, which breaks the point of a sentinel. Rejected. |
| `errors.New` hands out slots from a fixed static table in package `errors`, and panics with an explanation when the table is full. | Works in freestanding builds before any allocator, needs no compiler support, keeps Go's spelling and identity semantics. Each call takes a slot, so calling it at run time in a loop runs out; the panic message says what to do instead. **Chosen** (512 slots, 8 KiB of `.bss`). |
| The compiler evaluates `errors.New("constant")` to a static `errors.Text` per call site. | The best result: Go's spelling works everywhere, including `return errors.New("bad header")` inside a function, at zero cost, with no table. It makes a library call special to the compiler. **Proposed for sign-off** (section 9.1). |

### 4.2 Word-sized error values

A value that fits in the data word is copied into the interface, so it can be returned from anywhere and
never refers to a frame.

- **An integer code type with an `Error` method**: `syscall.Errno` is the model. `Error()` returns a
  static string from a table; `Is` maps codes onto portable sentinels (`ENOENT` is `os.ErrNotExist`). A
  kernel's own error codes, a driver's status codes and protocol errors fit this shape. Cost: none.
- **A pointer to a static struct**: a sentinel is one. A static table of richer descriptors (code, name,
  message, severity) indexed by a code also works: return `&table[i]`.
- **A pointer to caller-owned storage**: see 4.3.
- **A small struct inside the data word**: a struct of at most 8 bytes, such as `struct{ line, col int32 }`,
  could be stored directly instead of boxed. It would need a language change (section 9.3) and buys
  little, because most errors worth carrying context in also carry a string.

### 4.3 Errors that carry context

Context (a path, a line and column, a device name) needs storage that outlives the function that found
the error. There are three places it can live, and each is right for some code:

1. **In the object that owns the operation.** A parser keeps its last error in a field and returns a
   pointer to that field; a device driver, a connection or a file system mount does the same. The error
   lives as long as its owner and is overwritten by the owner's next error, which matches how such errors
   are used. Nothing is allocated. `fmt.ErrorBuf` packages this: 200 bytes of message plus the wrapped
   error, filled by `buf.Errorf(...)`.
2. **In memory from an allocator.** `fmt.Errorf`, `errors.Join` and `os.PathError` allocate the error and
   its message in one block, from the default allocator or one the caller passes (`fmt.ErrorfIn(a, ...)`).
   This is the general case: the error can travel any distance up the stack. It is meant to be paired with
   an arena that covers a request, a system call or a command, so that errors die with everything else the
   request allocated; for code without an arena, `errors.Free` releases a whole chain.
3. **In static storage.** Only for sentinels and code tables, which carry no per-failure context.

Rejected alternatives:

- **A per-thread "last error" slot** (errno style, but holding a context struct). Two errors alive at once,
  for example one being wrapped while a cleanup fails, overwrite each other. Unsound.
- **A global ring of error records.** Same failure, merely rarer.
- **A fixed buffer inside every context error type**, so `Error()` formats lazily into it. It makes every
  `PathError` hundreds of bytes and still needs the error itself to live somewhere.
- **Lazy messages through a second method** (`FormatError(w)`) so that `Error()` need not build a string.
  `error` requires `Error() string`, so a string has to exist anyway. Eager formatting into the error's own
  allocation is simpler and gives the error a stable message even when the strings it was built from (a
  path in a stack buffer) are gone.

### 4.4 Wrapping

Wrapping is a context error (4.3) whose context is "the error I was given". Both storage classes work:

- `fmt.Errorf("load config: %w", err)` allocates; its message is formatted when the error is made.
- `p.errBuf.Errorf("line %d: %w", n, err)` wraps into owner storage, with no allocation.

`Unwrap() error` and `Unwrap() []error` work as in Go, and so do `errors.Is` and `errors.As` over the
tree. Wrapping hands ownership of the inner error to the wrapper: free only the outermost error, or reset
the arena.

Wrapping without any storage (a chain of static wrappers) is possible only when the inner error is known
statically, which is the sentinel case and needs no wrapping.

### 4.5 Language support considered

| Idea | Verdict |
|---|---|
| Small structs in the data word (4.2) | Little benefit. Not proposed. |
| `return &T{...}` moves the struct "somewhere safe" | Either a hidden allocation, which the language forbids, or a caller-provided slot (like a struct return buffer). The slot fails for wrapping: the wrapper's box would point at the inner error's slot in a frame that has returned. Rejected. |
| A `try` shorthand (`x := try f()`) | Go rejected `try`; `if err != nil { return err }` is what Go programmers read. It would also have to know whether to wrap, and with which allocator. Rejected. |
| `errors.New` with a constant evaluated at compile time | Proposed for sign-off (9.1). |
| `return &local` and `return &T{...}` are compile errors | Needed so that the Go habit `return &PathError{...}` fails at compile time instead of dangling. Proposed for sign-off (9.2). |
| Package-level initializer boxes are static | Implemented in `f73a3e0` as a wrong-code fix; it changes spec 4.4, which said storing a box in a package variable is an error. Listed for confirmation (9.3). |

## 5. The design

Error handling is Go's, with one rule added: **an error that leaves a function is a word-sized value or a
pointer to storage that outlives the function.** The library gives each kind of error a home:

| Kind | Example | Storage | Cost |
|---|---|---|---|
| Sentinel | `io.EOF`, `os.ErrNotExist` | static (`errors.New` in a package-level `var`) | none |
| Code | `syscall.Errno` | the interface's data word | none |
| Owned context | a parser's `*ParseError`, `fmt.ErrorBuf` | a field of a long-lived object | none |
| Allocated context | `*os.PathError`, `fmt.Errorf`, `errors.Join` | allocator: default, or explicit with `...In(a, ...)` | one allocation, freed by `errors.Free` or an arena reset |

Everything else is Go:

- `if err != nil { return err }` returns the same interface value up the stack. It never copies a box,
  because no error that follows the rule holds one.
- `errors.Is(err, target)` walks `Unwrap() error` and `Unwrap() []error`, uses `==` when the target's type
  is comparable, and calls `Is(error) bool` methods. `syscall.Errno.Is` maps codes onto `os.ErrNotExist`,
  `os.ErrExist`, `os.ErrPermission` and `errors.ErrUnsupported`.
- `errors.As(err, &target)` is generic, `As[E any](err error, target *E) bool`, so the call reads exactly
  as in Go and needs no reflection; `E` is inferred from `&target`. `errors.AsType[E error]` matches Go
  1.26.
- `errors.Unwrap`, `errors.Join`, `errors.ErrUnsupported` are Go's.
- `fmt.Errorf` supports `%w` (one or several operands), and `%v`, `%s`, `%q`, `%x` and `%X` print an
  error's message.

Additions that Go does not have:

- `errors.Free(err)` releases every allocator-owned error in a tree. An error owns memory if it has a
  `Free()` method. Sentinels, codes and owner-stored errors have none and are skipped.
- `fmt.ErrorfIn(a, ...)`, `errors.JoinIn(a, ...)`, `os.NewPathErrorIn(a, ...)`, `os.OpenFileIn(a, ...)`
  take an explicit allocator.
- `fmt.ErrorBuf` is caller-owned storage for one formatted error.
- `syscall.Errno.Name()` returns the symbolic name ("ENOENT"), which kernel logs want.

### 5.1 Where Plutonium's library differs from Go's on purpose

| Go | Plutonium | Why |
|---|---|---|
| `errors.New` anywhere | in package-level variables; at run time it uses up a static table | no hidden allocation (sign-off item 9.1 removes the difference) |
| `os.File.Read`/`Write`/`Close` return `*PathError` | they return the bare `syscall.Errno` (and `io.EOF`) | the I/O path must not allocate; the caller knows which file it was using |
| `strconv` returns `*NumError` | returns `ErrSyntax`/`ErrRange` | parsing must not allocate; `errors.Is(err, strconv.ErrSyntax)` works in both |
| `PathError.Path` is the caller's string | `Path` views a copy inside the error's allocation | the caller's string may be a stack buffer that is gone by the time the error is printed |
| `Errno(999).Error()` is "errno 999" | "unknown error" | the message is a static string; building "errno 999" needs a buffer |
| `fmt.Printf("%w", err)` prints `%!w(*errors.errorString=&{...})` | prints `%!w(error)` | the existing bad-verb format; there is no reflection to print the value |

## 6. Examples

### 6.1 `open()` failing with a path and an errno

```go
// pluto/os
func OpenFileIn(a alloc.Allocator, name string, flags int, perm uint32) (*File, error) {
	var path [4096]byte
	if len(name) >= len(path) {
		return nil, NewPathErrorIn(a, "open", name, syscall.ENAMETOOLONG)
	}
	copy(path[:], name)
	fd, err := syscall.Open(path[:len(name)+1], flags, perm) // err is an Errno
	if err != nil {
		return nil, NewPathErrorIn(a, "open", name, err)
	}
	...
}
```

`NewPathErrorIn` allocates the `PathError` and its message, "open /etc/app.conf: no such file or
directory", in one block; `Op` and `Path` are views of the message.

### 6.2 A parser error with a line and column

The parser owns its error. No allocation, and the error lives as long as the parser:

```go
type ParseError struct {
	Line, Col int
	Err       error // a sentinel: ErrUnexpected, ErrTooDeep, ...
	text      [64]byte
	n         int
}

func (e *ParseError) Error() string { return unsafe.String(&e.text[0], e.n) }
func (e *ParseError) Unwrap() error { return e.Err }

type Parser struct {
	line, col int
	err       ParseError
}

func (p *Parser) fail(err error) error {
	p.err.Line, p.err.Col, p.err.Err = p.line, p.col, err
	p.err.n = len(fmt.Bprintf(p.err.text[:0], "%d:%d: %v", p.line, p.col, err))
	return &p.err
}
```

When the message needs no structured fields, `fmt.ErrorBuf` does the same in one line:
`return p.errBuf.Errorf("%d:%d: unexpected %q", p.line, p.col, tok)`.

### 6.3 A system call errno

```go
fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
if err == syscall.EINTR { ... }                 // direct comparison
if errors.Is(err, os.ErrNotExist) { ... }        // portable check, through Errno.Is
fmt.Println(err, err.(syscall.Errno).Name())    // "no such file or directory ENOENT"
```

### 6.4 Wrapping across three layers, and `errors.Is`/`As` at the top

```go
// layer 1: syscall returns syscall.ENOENT
// layer 2: os wraps it with the operation and path
// layer 3: the config loader adds what it was doing
func loadConfig(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defer alloc.Free(f)
	defer f.Close()
	...
}

// top: the program decides
func main() int64 {
	var mem [16 << 10]byte
	a := arena.New(mem[:])
	defer alloc.Use(&a).Restore() // every error below comes from the arena

	if err := loadConfig("/etc/app.conf"); err != nil {
		fmt.Println(err) // load config: open /etc/app.conf: no such file or directory
		var pe *os.PathError
		if errors.As(err, &pe) {
			fmt.Println("missing file:", pe.Path)
		}
		if errors.Is(err, os.ErrNotExist) {
			return 2
		}
		return 1
	}
	return 0
}
```

Without the arena, the program calls `errors.Free(err)` when it has finished with the error; that frees
the `fmt.Errorf` wrapper and the `PathError`, and skips the `Errno`.

### 6.5 A kernel system call

A kernel turns errors into an errno at the system call boundary. Inside the kernel, the VFS can wrap with
context from a per-call arena; the boundary uses `errors.As` to find the code:

```go
func sysOpen(path *byte, flags int) int64 {
	defer alloc.Use(&curTask().scratch).Restore()
	defer curTask().scratch.Reset()
	fd, err := vfs.Open(cstring(path), flags)
	if err != nil {
		if errno, ok := errors.AsType[syscall.Errno](err); ok {
			return -int64(errno)
		}
		return -int64(syscall.EIO)
	}
	return int64(fd)
}
```

## 7. Ownership rules

1. An error returned from a function is a word-sized value or points to static, owner or allocator
   storage. Never to the returning function's frame.
2. An allocator-backed error owns its message and the errors it wraps. Free only the outermost error, once,
   with `errors.Free`, or let the arena it came from be reset.
3. An owner-stored error (`fmt.ErrorBuf`, a parser's `ParseError`) is valid until its owner is gone or makes
   its next error. Code that keeps such an error longer wraps it with `fmt.Errorf`, which copies the
   message.
4. `errors.New` takes a constant or other static string, and is called in package-level variable
   initializers.

## 8. What the design needs from the compiler

The library cannot enforce rule 1; the compiler can, for the cases it can see. The memory-safety work owns
these checks:

- **Spec 4.4: returning an interface that holds a frame box is an error.** This catches
  `return PathError{...}` and `return 0, someStruct` as an `error`. The design relies on it.
- **Returning the address of a local variable or composite literal is an error** (proposed, 9.2). This
  catches the most common Go habit, `return &PathError{...}`, which today compiles and dangles. Without it,
  rule 1 is a convention only. `testdata/run/ifaces.p` returns `&myErr{42}` and needs updating when the
  check lands; `examples/15-interfaces` has been updated in this change.
- **Storing a frame pointer or frame box in a package-level variable is an error** (4.4, and the same for
  pointers). Package-level initializers are exempt because their boxes are static (2, item 3).

## 9. Decisions needing sign-off

### 9.1 `errors.New` with a constant message is evaluated at compile time

**Proposal.** A call `errors.New(c)` where `c` is a constant string lowers to the address of a static
`errors.Text{Msg: c}` owned by that call site. A non-constant argument is a compile error: "errors.New
needs a constant message; use fmt.Errorf". This removes the 512-entry table and makes
`return errors.New("bad header")` legal anywhere, as in Go. Each call site has one identity (Go gives each
evaluation its own, which no correct program can observe except by comparing two results of the same call
site).

**Recommendation: adopt.** It makes the most common Go idiom free and correct everywhere, and the
compiler change is small (a special case in call lowering, like the `pluto/bits` intrinsics).

### 9.2 `return &x` and `return &T{...}` for frame storage are compile errors

**Proposal.** Returning a pointer to a local variable or to a composite literal that lives in the frame,
directly or inside a returned interface or struct, is a compile error that suggests the alternatives
(owner storage, an allocator, or a package-level variable). Raw pointers otherwise stay as in C.

**Recommendation: adopt.** It is the Go habit most likely to produce dangling errors, and the case is
syntactically obvious. It belongs with the memory-safety agent's escape checks.

### 9.3 Interface boxes in package-level initializers are static

**Status.** Implemented in `f73a3e0` to fix wrong code (`var x any = "s"` pointed into the dead init
frame, as did `alloc.Pages`). The spec text in 4.4 said storing a frame box in a package variable is a
compile error; it now says initializers box statically, matching `&T{...}`.

**Recommendation: confirm.** It allows `var ErrBadMagic error = formatError{...}` and makes
package-level `any` values behave as in Go.

## 10. Implementation status

Implemented (pure library, no language rules):

- `pluto/errors`: `Text`, `New` (static table), `Is`, `As`, `AsType`, `Unwrap`, `Join`, `JoinIn`, `Free`,
  `ErrUnsupported`.
- `pluto/rt`: `Comparable(any) bool`, used by `errors.Is` to avoid comparing uncomparable targets.
- `pluto/alloc`: `Raw(a, size, align)`, for variable-sized error blocks.
- `pluto/internal/oserror`: the portable sentinels, shared by `syscall` and `os`.
- `pluto/syscall`: `Errno` with names and messages for 78 Linux error numbers, `Name`, `Is`, `Temporary`,
  `Timeout`.
- `pluto/os`: `ErrInvalid`, `ErrPermission`, `ErrExist`, `ErrNotExist`, `ErrClosed`, `PathError`,
  `NewPathError`, `NewPathErrorIn`, `OpenFileIn`; `Open`, `Create` and `OpenFile` return `*PathError`;
  `Read` returns `io.EOF`.
- `pluto/fmt`: `%w`, `%x`/`%X` on errors, `Errorf`, `ErrorfIn`, `ErrorBuf`.
- `pluto/io`: `EOF`, `ErrUnexpectedEOF`, `ErrShortWrite`, `ErrShortBuffer` as `error` sentinels;
  `ReadAtLeast`; `ReadFull` returns `ErrUnexpectedEOF` for a partial read, as in Go.
- `pluto/strconv`, `pluto/bytes`: sentinels made with `errors.New`.
- Tests: `testdata/lib/errors.p` (checked against Go through shims), `testdata/run/errpatterns.p` (Go
  common subset), `testdata/run/initboxes.p`, and `internal/build/errors_test.go` (compile errors and the
  `errors.New` limit).

Not implemented: the three proposals in section 9 (9.3 is in, pending confirmation), and the escape checks
in section 8.
