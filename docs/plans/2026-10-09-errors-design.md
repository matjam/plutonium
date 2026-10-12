# Error Handling in Plutonium: Design

**Date:** 2026-10-09
**Status:** implemented and verified on native amd64, 2026-10-12. The historical design below is
superseded where the Status block names the specification's current rules.

> **Status (2026-10-12): implemented.** Static sentinels, contextual counted errors, retain/release of shared and synced dynamic error values, independent counted assertions and As targets, and borrowed As lifetime checks implement sections 16.1 to 16.7. There is no `errors.Free`. Native full tests, lint, coverage, SSA checks and generation-check-disabled tests pass; debug-allocator regressions verify drops at O0 and O2. Roadmap gap 22 is closed. The generic assertion precision limitation is recorded under roadmap item 5.
>
> **Defined now by:** section 16.1 (The error interface), section 16.2 (Where an error lives), section 16.3 (Sentinels and `errors.New`), section 16.4 (Error codes), section 16.5 (Errors with context), section 16.6 (Wrapping, `Is` and `As`), section 16.7 (Allocated errors and ownership), section 7.9 (Interface types), section 15.2 (Package initialization), section 18.3 (Views), section 18.7 (Shared ownership), section 18.10 (No allocation without an owner or an arena), section 10.13 (Default parameter values), appendix F.4 (Errors).
>
> **The specification supersedes these statements in this document:**
> - Every "spec section N" citation uses the old single-file numbering (this document's own section numbers are unchanged): 4.4 is section 7.9 (and section 18.3 for returning a frame copy); 5.1 is section 10.13; 6.3 is section 18.3; "spec section 6" (the ownership model) is chapter 18; "spec section 7" is sections 18.9 and 18.10; 14.6 is section 16.7.
> - Constraint 2 of section 2, section 8 and section 9.2, "This is not enforced yet", "Not implemented: 9.2 and the escape checks in section 8": returning a struct as an `error`, or a pointer to a local variable or composite literal, is a compile error (E3001), and so is storing a frame pointer or frame copy in a package variable (E3002), section 16.2 and section 18.3. Only package-level initializers box statically, section 16.2.
> - Section 4.1 (chosen option, "512 slots"), the first row of the table in section 5, the first row of the table in section 5.1, rule 4 of section 7 as written, and the `errors.New` line of section 10: `errors.New` takes a constant (E2008), is a static error per call site, never allocates, and may be called anywhere, section 16.3. Sections 9.1 and 10.1 already say this.
> - Section 4.3 item 2, the "Allocated context" row of section 5, the `errors.Free`, `fmt.ErrorfIn`, `errors.JoinIn`, `os.NewPathErrorIn` and `os.OpenFileIn` entries of section 5, the example of section 6.1, the last paragraph of section 6.4 ("Without the arena, the program calls `errors.Free(err)`") and rule 2 of section 7 ("Free only the outermost error, once, with `errors.Free`"): an allocated error is a counted `synced[T]` object, released exactly once when the last `error` value that refers to it goes. There is no `errors.Free`, and nothing releases an error by hand. A different allocator is installed with `alloc.Use` or passed to a function with a defaulted allocator parameter, section 16.7 (items 1, 2 and 5), section 18.7 and section 10.13.
> - Section 4.4, "Wrapping hands ownership of the inner error to the wrapper: free only the outermost error": `%w` copies the wrapped error into the wrapper, which adds one to its count, and the caller's own copy is released when it goes out of scope, section 16.7 (item 4).
> - Section 4.3 item 2, section 5.1 (`PathError.Path` "views a copy inside the error's allocation") and section 6.1 ("one block", "`Op` and `Path` are views of the message"): the text of an error is a string. The error object holds its strings, and the drop of the object releases them, section 16.5, section 16.7 (item 8) and section 18.8.
> - The `os.File.Read`/`Write`/`Close` and `strconv` rows of section 5.1 (bare `syscall.Errno`, and `ErrSyntax`/`ErrRange` returned directly): the specification says that a failed file operation returns a `*os.PathError` and a failed parse in `strconv` returns a `*strconv.NumError`, both counted objects allocated only when an operation fails, and that `errors.Is(err, strconv.ErrSyntax)` holds, section 16.5.
> - Section 11, "Reconciliation with the ownership model (proposal, needs sign-off)": decided. Allocated errors are counted objects, copying or dropping an `error` counts them, and an error's `Drop` drops the errors it wraps. `error` is an ordinary interface, so an owner converted to it is only lent and returning it is E3001. Section 16.7 and section 7.9. "The ownership model (language spec section 6)" is chapter 18, and its allocation rule is section 18.10.

## 1. The problem

`error` is an interface, and Go code builds error values in ways that Plutonium's rules do not allow:

- `errors.New(s)` and `fmt.Errorf(...)` allocate on the garbage-collected heap. Plutonium has no hidden
  allocation (spec section 7; section 6 before the ownership section was added).
- `return &PathError{Op: "open", Path: name, Err: e}` returns the address of a composite literal. In
  Plutonium that literal is a temporary in the returning function's frame (spec section 6.3), so the
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
   library's convention for "default unless the caller says otherwise" was a pair of functions, `F` and
   `FIn(a, ...)` (`alloc.New`/`alloc.NewIn`, `strings.NewBuilder`/`NewBuilderIn`), because default
   parameter values (spec 5.1) were not implemented yet. They now are, and each pair is one function
   (section 10.1).
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

**Status: approved and implemented.** Lowering turns each call into the address of a read-only
`errors.Text` in `.rodata` keyed by the call site; the checker rejects a non-constant message, and
lowering rejects taking `errors.New`'s value or deferring it. The table is gone.

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

Not implemented: 9.2 and the escape checks in section 8, which belong to the memory-safety work.

### 10.1 After default parameter values (2026-10-09)

Default parameter values (spec 5.1) and 9.1 changed the library described above:

- `errors.New` is a static error per call site (9.1); the 512-entry table is gone, and rule 4 of
  section 7 now reads: `errors.New` takes a constant and may be called anywhere.
- Each `F`/`FIn` pair is one function whose allocator is a trailing parameter defaulting to
  `alloc.Default()`: `alloc.New(a = ...)`, `alloc.Free(p, a = ...)`, `alloc.Raw(size, align, a = ...)`,
  `os.Open(name, a = ...)`, `os.Create`, `os.OpenFile(name, flags, perm, a = ...)` and
  `os.NewPathError(op, path, err, a = ...)`. The example in 6.1 is now `OpenFile(..., a)` calling
  `NewPathError("open", name, err, a)`.
- `fmt.Errorf` and `errors.Join` are variadic, and a variadic function cannot have defaults (spec 5.1),
  so `ErrorfIn` and `JoinIn` are gone: both allocate from `alloc.Default()`, and code that wants another
  allocator installs it with `alloc.Use`, the arena pattern of 6.4 and 6.5.
- Tests: `testdata/lib/errorsnew.p` (identity per call site, distinct sites, package variables, returns,
  generics), `testdata/lib/allocdefaults.p` (which allocator each library function used), and
  `internal/build/errors_test.go` (a non-constant message, `errors.New` as a value or deferred).

## 11. Reconciliation with the ownership model (proposal, needs sign-off)

The ownership model (language spec section 6) requires every allocation to have an owner or an arena.
The allocated errors of this design (section 4.3, item 2) have neither on the general heap: the `error`
interface only looks at the block, and `errors.Free` must be called exactly once on the outermost error.
Owners and views also change two details of this note: `error` is an ordinary interface, so an owner
converted to `error` is only lent and cannot be returned, and `Free()` methods become `Drop()` methods
on owner types.

This design stays in force. Language spec section 14.6 proposes the reconciliation: allocated errors become
`synced` objects, and copying or dropping an `error` counts them, so each is released exactly once,
automatically, whether or not an arena is in use, and `errors.Free` goes away. It also records the two
alternatives (an owning `error`, and allocated errors only in arenas) and why they are not recommended.
