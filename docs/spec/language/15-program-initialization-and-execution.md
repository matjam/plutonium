## 15. Program initialization and execution

### 15.1 The zero value

When storage is allocated for a variable, either through a declaration or a call of `new`, or when a new
value is created, either through a composite literal or a call of `make`, and no explicit initialization
is provided, the variable or value is given a default value. Each element of such a variable or value is
set to the zero value for its type:

| Type | Zero value |
|---|---|
| boolean | `false` |
| numeric (integer and floating-point) | `0` |
| string | `""` |
| pointer, function, interface, slice, channel, map | `nil` |
| `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]` | `nil` |
| enum | the member whose value is 0 |
| union | its first variant, holding the zero value of that variant's payload |
| owner type | the zero value of its underlying type; it holds nothing |
| `packed` type | all bits zero |
| `volatile[T]`, `aligned[T, N]` | the zero value of `T` |
| `ring[T, N]` | an empty ring |
| array, struct | each element or field set to the zero value of its type |

This initialization is done recursively, so for instance each element of an array of structs has its
fields zeroed if no value is specified.

Every enum has a member whose value is 0 (E2307), so the zero value of an enum is always a member
(section 7.12, Enums). A union starts in its first variant, so the first variant is the idle or empty
state (section 7.13, Unions).

The zero value of an owner type holds nothing, and dropping it does nothing (section 18.6, Drops). This
is what lets an owner be moved out of a variable: the move leaves the zero value behind, which serves as
the drop flag (section 18.5, Moves). Memory from every allocator is zeroed, so the memory of a new
`owned[T]` holds the zero value of `T` (section 13.9, Allocation). A `Drop` method must treat the zero
value as empty (section 10.14, Method declarations).

The zero value of a map is the nil map. It has length 0, reading from it yields the zero value of the
element type, and writing to it is a run-time panic (section 17.1, Panics). The nil channel blocks every
send and receive forever (section 7.11, Channel types).

These two simple declarations are equivalent:

```go
var i int
var i int = 0
```

After

```go
type T struct {
	i    int
	f    float64
	next *T
}

var t T
```

the following holds:

```go
t.i == 0
t.f == 0.0
t.next == nil
```

The same would be true after `p := new(T)` with `p.i == 0`, `p.f == 0.0` and `p.next == nil`, where `p` is
an `owned[T]`.

### 15.2 Package initialization

Within a package, package-level variable initialization proceeds stepwise, with each step selecting the
variable earliest in declaration order which has no dependencies on uninitialized variables.

More precisely, a package-level variable is considered ready for initialization if it is not yet
initialized and either has no initialization expression or its initialization expression has no
dependencies on uninitialized variables. Initialization proceeds by repeatedly initializing the next
package-level variable that is earliest in declaration order and ready for initialization, until there are
no variables ready for initialization.

If any variables are still uninitialized when this process ends, those variables are part of one or more
initialization cycles, and the program is not valid (E0009).

Multiple variables on the left-hand side of a variable declaration initialized by a single (multi-valued)
expression on the right-hand side are initialized together: if any of the variables on the left-hand side
is initialized, all those variables are initialized in the same step.

```go
var x = a
var a, b = f() // a and b are initialized together, before x is initialized
```

For the purpose of package initialization, blank variables are treated like any other variables in
declarations.

The declaration order of variables declared in multiple files is determined by the order of the files'
names: files are presented in lexical order of file name, variables declared in the first file are
declared before any of the variables declared in the second file, and so on.

Dependency analysis does not rely on the actual values of the variables, only on lexical references to
them in the source, analyzed transitively. For instance, if a variable `x`'s initialization expression
refers to a function whose body refers to variable `y` then `x` depends on `y`. Specifically:

- A reference to a variable or function is an identifier denoting that variable or function.
- A reference to a method `m` is a method value or method expression of the form `t.m`, where the
  (static) type of `t` is not an interface type, and the method `m` is in the method set of `t`. It is
  immaterial whether the resulting function value `t.m` is invoked.
- A variable, function, or method `x` depends on a variable `y` if `x`'s initialization expression or body
  (for functions and methods) contains a reference to `y` or to a function or method that depends on `y`.

For example, given the declarations

```go
var (
	a = c + b // == 9
	b = f()   // == 4
	c = f()   // == 5
	d = 3     // == 5 after initialization has finished
)

func f() int {
	d++
	return d
}
```

the initialization order is `d`, `b`, `c`, `a`. Note that the order of subexpressions in initialization
expressions is irrelevant: `a = c + b` and `a = b + c` result in the same initialization order in this
example.

Dependency analysis is performed per package; only references referring to variables, functions, and
(non-interface) methods declared in the current package are considered. If other, hidden, data
dependencies exist between variables, the initialization order between those variables is unspecified.
For instance, given the declarations

```go
var x = I(T{}).ab()  // x has an undetected, hidden dependency on a and b
var _ = sideEffect() // unrelated to x, a, or b
var a = b
var b = 42

type I interface{ ab() [2]int }
type T struct{}

func (T) ab() [2]int { return [2]int{a, b} }
```

the variable `a` will be initialized after `b`, but whether `x` is initialized before `b`, between `b`
and `a`, or after `a`, and thus also the moment at which `sideEffect()` is called (before or after `x` is
initialized) is not specified.

**`init` functions.** Variables may also be initialized using functions named `init` declared in the
package block, with no arguments and no result parameters (an `init` that has any is E0039, and one
without a body is E0037).

```go
func init() { ... }
```

Multiple such functions may be declared per package, even within a single source file. In the package
block, the `init` identifier can be used only to declare `init` functions, yet the identifier itself is not
declared. Thus `init` functions cannot be referred to from anywhere in a program.

The entire package is initialized by assigning initial values to all its package-level variables followed
by calling all `init` functions in the order they appear in the source, possibly in multiple files, in the
lexical order of the file names.

**Where the variables live.** Package-level variables live in static storage, zeroed before any
initialization runs (section 18.2, Where variables live). The literals in package-level initializers are
static too: a composite literal whose address is taken (`&T{...}`), a slice literal, the copy that
converting a value to an interface makes, a method value's record, and a function literal that
captures nothing are placed in static storage and last as long as the program (sections 11.3, 7.9 and
11.9). A view of one of them has no lifetime to check (section 18.3, Views). The initializer of a
variable such as

```go
var ErrBadMagic error = formatError{want: 0x7f}
var table = []entry{{"a", 1}, {"b", 2}}
var root = &node{name: "root"}
```

therefore allocates nothing.

**Allocation during initialization.** An initializer allocates when evaluating it can reach `new`, `make`,
a map literal, an operation that makes a string at run time (section 18.8, Strings), or any call that can
reach one. The compiler works this out from the whole program; no annotation says so.

- In a hosted build, package initializers may allocate. The default allocator is installed before any
  other package is initialized (section 15.3), so `var cache = make(map[string]int)` and
  `var names = vec.New[string]()` are valid. The objects they create belong to package variables and live as long as the
  program (section 15.4).
- In a freestanding build, which includes every kernel (section 20.5, Build modes and targets), an
  initializer that allocates is a compile error (E2586). A freestanding program has no allocator when package
  initialization runs, and no console to report the failure on. The error names the allocation and the
  package variable or `init` function that reaches it. The fix is to initialize the variable in the
  program's entry function, after the program has called `alloc.SetDefault`. The check does not follow
  what an earlier `init` function installs: doubt resolves to an error (section 1.2, Design rules).

```go
var cache = make(map[string]int) // error in a freestanding build: allocates before an allocator exists

var table map[string]int

func kernelMain(info *BootInfo) {
	alloc.SetDefault(newSlab(info))
	table = make(map[string]int) // the allocator exists now
}
```

`errors.New` and the other forms of section 16.3 (Sentinels and `errors.New`) are static and allocate
nothing, so sentinel errors are declared at package level in every build.

**Panics and tasks.** A panic during initialization is a panic like any other (chapter 17, Run-time
panics). Initialization starts no task. A `task` statement, a call of `s.Task` on a `sync.Scope` or of
`wg.Task` on a `sync.WaitGroup`, and a call of any other library function that starts a task with a func
argument (`rt.Go`, `time.AfterFunc`; section 19.1, Tasks) is a compile error when a package-level
initializer or an `init` function can reach it, directly or through the functions it calls. The compiler
works this out from the whole program, as it does for allocation, and the error names the start and the
initializer or `init` function that reaches it. The fix is to start the task from `main`, or from a
function that `main` calls. The runtime's own threads are not tasks. The task that runs initialization is
therefore the only task that exists while it runs, which is what lets a package variable that is written
only during initialization be read by every task without a lock (section 19.3, Data race freedom).

### 15.3 Program initialization

The packages of a complete program are initialized stepwise, one package at a time. If a package has
imports, the imported packages are initialized before initializing the package itself. If multiple
packages import a package, the imported package is initialized only once. The importing of packages, by
construction, guarantees that there can be no cyclic initialization dependencies (E4009).

More precisely, the runtime packages come first: `pluto/rt` and `pluto/alloc`, with the packages they
import, are initialized before every other package, so that the default allocator and the panic path
exist when any other initialization runs. Then, given the list of all remaining packages, sorted by
import path, in each step the first uninitialized package in the list for which all imported packages (if
any) are already initialized is initialized. This step is repeated until all packages are initialized.

Package initialization (variable initialization and the invocation of `init` functions) happens in a
single task, the task that goes on to run `main` (section 19.1, Tasks), sequentially, one package at a
time. No other task exists while it runs, since initialization may not start one (section 15.2, Panics
and tasks). It invokes the `init` functions in sequence: it does not invoke the next one until the
previous one has returned. All initialization is complete before `main` begins.

### 15.4 Program execution

A complete program is created by linking a single, unimported package called the main package with all the
packages it imports, transitively. The main package must have package name `main` and declare a function
`main` that takes no arguments and returns nothing or a single `int64` (section 10.12, Function
declarations). A `main` with another signature is E2009, and a main package without `main` is E4016.

```go
func main() { ... }
func main() int64 { ... }
```

Program execution begins by initializing the program (section 15.3) and then invoking the function `main`
in package `main`.

**Exit status.** In a hosted build the process exits with status 0 when `main` returns nothing, and with
the value `main` returns when it returns an `int64`. The operating system keeps the low eight bits of the
status, as it does for any process. A program that ends with a panic or with a fatal error (a deadlock, for
example) exits with status 2 (section 17.1).

**When `main` returns.** When the function invocation returns, the program exits. First `main` returns as
every function does: its deferred calls run, and the owners its variables hold are dropped (sections
12.19, Defer statements, and 18.6, Drops). Then:

- The program does not wait for other tasks to complete, and does not join them. A task that is running,
  blocked or not yet started when `main` returns is abandoned: its pending deferred calls do not run and
  the owners in its frames are not dropped. A program that needs a task to finish waits for it, with a
  channel, a `sync.WaitGroup`, or a `sync.Scope` that joins at the end of its block (section 19.4,
  Scoped tasks).
- A drop that another task has begun when `main` returns completes before the program exits: a `Drop`
  method that is running, or the release of the last reference to a counted object. Such a drop is not cut
  in half. No other work of that task is finished. The task runtime recognizes a drop in progress
  (section 19.1, Tasks).
- Package-level variables are not dropped. A package-level owner is never dropped, in any order, when the
  program ends; the process exit reclaims its memory and descriptors. A program that must flush or close
  what a package variable holds does it in `main`, with a deferred call. The debug
  allocator reports memory that package variables still hold apart from leaks (`debug-allocator.md`).

```go
package main

type Log owner struct {
	fd int32
}

func (l *Log) Drop() {
	println("closing a log")
}

var global Log // a package-level owner: its Drop never runs

func main() {
	var local Log
	_ = local
} // local is dropped here: "closing a log" is printed once
```

**`os.Exit`.** The function `os.Exit(code)` ends the program at once with the given status. It runs no
deferred calls, drops no owner and does not wait for any task. The compiler works out that a function never
returns from its body, so `os.Exit` needs no annotation.

**`rt.Goexit` in `main`.** If the task that runs `main` calls `rt.Goexit` (section 17.3, Ending a task with
`rt.Goexit`), `main` does not return: its deferred calls run, the other tasks go on, and once none of them
is left the program ends with a fatal error and status 2.

**Stacks.** The task that runs `main` runs on the stack the operating system gave the process, or on a
stack of the size that a `//pu:stack` directive on `main` asks for (section 20.3, Directives). The stacks
of other tasks, and what happens when a task overflows its stack, are defined in section 19.1 (Tasks).

**Freestanding builds.** A freestanding program provides its own entry point (section 20.5). The entry
point runs package initialization and calls `main`, or runs package initialization and then calls the
program's own functions. A kernel does not return from its entry point, so the rules about `main`
returning apply to hosted programs and to freestanding programs that call `main` and let it return.
Every freestanding program defines its panic handler (section 17.4, Panics in freestanding programs).

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
