## 17. Run-time panics

### 17.1 Panics

Execution errors such as attempting to index an array out of bounds trigger a **run-time panic**,
equivalent to a call of the built-in function `panic` (section 13.11, Handling panics). So does a call of
`panic` itself.

A panic stops the program. It does not unwind the stack: control never returns to a function that
panicked, so there is no `recover`, and owners are not dropped (section 18.6, Drops). In a hosted build the
panicking task first runs its pending deferred calls, innermost frame first, and other tasks are not
affected (section 17.2, Deferred calls on a panic). Then the runtime calls the program's **panic handler**,
which never returns (section 17.4, Panics in freestanding programs). In a hosted build without a handler of
the program's own, the built-in handler prints `panic: ` followed by the message, then a stack trace with
the function, file and line of each frame, and the process exits with status 2.

A panic carries a **message** and, for a call of `panic(v)`, the **value** `v`. A run-time panic has no
value. Its message says which operation failed and with which values, such as the index and the length of
a failed index expression. The wording of the messages is not part of the language. There is no value of
an error type that a program could receive, since a panic cannot be caught.

A panic in a task other than the one that runs `main` names the task: the built-in handler prints
`task N:` before the trace, and `rt.Panic.Task` holds the number.

**What panics.** The following cause a run-time panic.

*Indexing and slicing.*

- An index expression out of range on an array, a slice or a string (section 11.10, Index expressions).
- A slice expression whose bounds are out of range (section 11.12, Slice expressions).
- A slice expression on a string with a bound that is not at the start of a rune. A string holds valid
  UTF-8 and can be sliced only between runes. This is the one documented exception to the no-false-friends
  rule (section 1.2, Design rules; appendix A, section A.5). The standard library provides
  `strings.Truncate` and `utf8.RuneStartBefore` for cutting at a rune boundary, and the message names them.
- A conversion of a slice to an array type with a different length (section 11.24, Conversions).

*Arithmetic.*

- Integer division or remainder by zero (section 11.19, Arithmetic operators).
- A shift by a negative count.

Integer overflow does not panic: the result wraps. Floating-point division by zero gives an infinity or a
NaN. Converting a floating-point value that is out of range to an integer type gives an implementation-
dependent value and neither panics nor faults (section 11.24).

*Types and values.*

- A failed type assertion with a single result, on an interface or on a union (section 11.13, Type
  assertions).
- A type switch on a union value whose tag names no variant, and an expression switch on an enum value that
  is not a member, when the switch has no `default` case (section 12.9, Switch statements). Only unsafe
  code, raw bits converted to a packed type, or generic code working on the underlying integer can make
  such a value.
- A comparison of two interface values that have identical dynamic types that are not comparable, and
  the use of such a value as a map key (section 8.10, Comparable types).
- An assignment to an entry of a nil map.
- A dereference of a nil pointer, of a nil `owned`, `shared` or `synced` value, and a call of a nil function
  value (section 17.5, Faults).

*Channels.*

- Sending on a closed channel, and closing a closed channel or the nil channel (section 13.4, Close).
- A `make` of a channel or map with a negative size (section 13.7, Making channels and maps).

*Allocation.*

- An allocation when no default allocator has been set, as in a freestanding program before
  `alloc.SetDefault`; an allocation with a nil allocator argument; an allocator that returns no memory
  ("out of memory"). These are `new`, `make`, the operations that make strings, and the allocating
  functions of the standard library (sections 13.7, 13.9 and 18.9).

*Control.*

- An iterator that calls the `yield` function of a range-over-func loop when it may not (section 12.11,
  Range over functions).
- A `task` statement whose function value, or one of whose func-typed arguments, has its closure record in
  the starting task's stack, when the compiler cannot see it (section 12.12, Task statements; the cases it
  can see are E2590 and E2594).
- An operation that would park the task, in interrupt context, when the scheduler can tell (section
  20.10, Interrupts).
- `rt.ExitTask` called in a function that C code called (section 17.3).

*Memory.*

- The use of freed heap memory, and a stale use of fixed or Chunked arena memory that was not
  rejected by the static lifetime rules (sections 17.6 and 18.11).
- A stack overflow (section 17.5).

A deadlock is not a panic. When every task waits for another and nothing can wake them, the program ends
with a fatal error and status 2 (section 19.1, Tasks). A stop by `rt.Abort` or `rt.PanicFault` ends the
program through the same panic handler (section 17.4).

**Omitting checks.** The directive `//pu:nochecks` removes the bounds checks and the generation checks
from one function (section 20.3, Directives), for code that has been proved correct and must be as fast
as possible. It makes the function unsafe code (section 18.12, Safe and unsafe code). An index out of
range in such a function does not panic, and what it does is not defined. This is the only place where a panic
of this list depends on the program's own marking, and it is the program's promise, not a feature of the
language, that the bounds hold.

```go
package main

func main() {
	var a [4]int
	i := 4
	a[i%5] = 1 // panic: runtime error: index out of range [4] with length 4
}
```

### 17.2 Deferred calls on a panic

In a hosted build, a panic runs the panicking task's pending deferred calls before the panic handler is
called. Deferred calls do not stop the panic.

```go
w := newWriter(os.Stdout)
defer w.Flush()         // runs: the output written so far is not lost
defer os.RemoveAll(tmp) // runs
process(items)          // panics on bad input
```

- **Which calls.** Every deferred call that was armed in a frame of the task that panicked, innermost
  frame first. Within a frame, the last one deferred runs first. The arguments are those evaluated at the
  `defer` statement (section 12.19, Defer statements). A `defer` in the body of a range-over-func loop
  belongs to the function that contains the loop (section 12.11). Deferred calls of other tasks do not
  run. Those tasks keep running meanwhile, so a deferred `Unlock` lets a task waiting for the lock go on.
  If one of them panics in the meantime, it may end the program first.
- **Nothing else changes.** Control never returns to a function that panicked. Owners are not dropped
  (section 18.6, Drops), and a function's result variables are not returned anywhere. A deferred call may
  use the owners of its frame, which still hold their values.
- **Which panics.** A call of `panic`, the run-time panics of section 17.1, and the faults that the runtime
  turns into panics (section 17.5). For a fault, the runtime leaves the signal handler and runs the
  deferred calls on the faulting task's own stack. No deferred calls run after a stack overflow, which
  leaves no stack to run them on, after a heap error that the debug allocator reports or a stop by
  `rt.Abort` or `rt.PanicFault` (after which the program's state is not to be trusted), while the task has
  preemption disabled (it holds a runtime lock or is inside the scheduler, where a deferred call could
  wait for good), or outside any task (a scheduler's own context).
- **A panic in a deferred call** that runs for a panic goes straight to the panic handler: the deferred
  calls after it do not run. The new panic's `rt.Panic.During` points to the first. The built-in handler
  prints both messages, then the trace of the second panic through the deferred call into the frames where
  the first one happened.

  ```
  panic: runtime error: integer divide by zero
  	panic: close failed
      main.(*conn).Close (./main.p:13)
      main.work (./main.p:20)
      main.main (./main.p:26)
  ```

  A deferred call that calls `rt.ExitTask` while deferred calls run for a panic stops them too, and the
  program ends with the panic.
- **What the handler sees.** The panic as it happened: `Message`, `Value` and `Fault`, and a trace that
  starts where the panic happened, not in the deferred calls. A program's own `//pu:panic` handler is
  called in the same way, after the deferred calls; it does not run them itself.

Freestanding and kernel builds run no deferred calls on a panic. The handler must work from any state,
with interrupts disabled or a lock held.

### 17.3 Ending a task with `rt.ExitTask`

`rt.ExitTask()` ends the calling task. Every function on the task's stack, innermost first, runs its
deferred calls and drops what it holds, as if it returned at the call it is in. Then the task ends. It is
not a panic: no handler is called, and other tasks are not affected.

- **In a deferred call.** A deferred call that calls `ExitTask` ends its own frames. The deferred calls
  after it in the frame that deferred it still run, and the task ends afterwards.
- **In the task that runs `main`.** `main` does not return. Its deferred calls run, the other tasks go on,
  and once none of them is left, or all of them wait for good, the program ends with a fatal error ("main
  called rt.ExitTask") and status 2 (section 15.4, Program execution).
- **Scopes.** A frame that holds a `sync.Scope` joins its tasks as it leaves, as on any return
  (section 19.4, Scoped tasks), so `ExitTask` needs no extra step for them.
- **Tests.** The testing package ends a test that fails with `FailNow`, `Fatal` and `Fatalf`, and one that
  is skipped with `SkipNow`, `Skip` and `Skipf`, by calling `ExitTask`, so a test's deferred calls run and
  its owners are dropped when it stops early. `rt.CatchExitTask(f func()) bool` calls `f` and reports whether
  it ended with `ExitTask`, which is how the runner of tests goes on to the next test on the same task.
- **C.** `ExitTask` cannot leave a function that C code called (`//pu:export`): the call panics at the
  boundary (section 20.4, Assembly and C).

`ExitTask` does not unwind the stack by any mechanism of its own. Each frame leaves through its ordinary
return path, which runs its deferred calls and drops its variables exactly as a `return` there would.

### 17.4 Panics in freestanding programs

A program says what happens when it panics by marking one function `//pu:panic`:

```go
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(serial) // "panic: ...", the fault if any, the stack trace
	for {
		x86.Cli()
		x86.Hlt()
	}
}
```

- **One handler.** A freestanding or kernel build must have exactly one. None is E2580 and more than one
  is E2581, since such a program has no operating system to report a crash to. A hosted program may
  define one to replace the built-in handler.
- **Its declaration.** A plain function with a body, not a method, not generic, not also `//pu:interrupt`,
  declared `func name(p *rt.Panic)`. Anything else is E2582.
- **It never returns.** A panic has nothing to return to. Every path through the handler must end in an
  endless loop or in a call of a function that never returns (`os.Exit`, `rt.Abort`, or a function of the
  program's own whose every path ends that way). The compiler works out which functions never return from
  their bodies. A call that it cannot see through (through a func value or an interface, or into
  recursion) counts as returning. A handler that can return is E2583.
- **A direct call.** The runtime's panic path calls the handler directly. Nothing is registered at run
  time, so it works from the first instruction of the program, before any initialization.
- **It must not wait or start tasks.** The handler runs on top of whatever panicked, possibly an interrupt
  handler or code holding a lock. The rules of section 20.10 (Interrupts) apply to it as to an interrupt
  handler: it reaches no code that can wait (E2584) and no `task` statement (E2585).

`rt.Panic` describes the panic:

```go
type Panic struct {
	Message string // "runtime error: index out of range [4] with length 4", or the text of the value passed to panic
	Value   any    // the value passed to panic, nil for run-time errors and faults
	Fault   Fault  // the hardware fault, if the panic came from one; Fault.Kind is NoFault otherwise
	CPU     int    // the processor that panicked: in kernel mode its number from rt.SetCPU, otherwise 0
	Task    uint64 // the task that panicked: 1 for the one that runs main, 0 outside any task
	During  *Panic // the panic whose deferred calls were running when this one happened, or nil
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

`rt.Writer` has the method of `io.Writer`, so any writer will do. `rt.Console{}` writes where `print` does
(section 13.12, Bootstrapping). The methods are generic so that a writer of a known type is called
directly: a call through the `rt.Writer` interface counts, for the check of section 20.10, as calling
every writer the program has. For the same reason a hosted handler writes to `rt.Console{}` rather than to
`os.Stderr`. The message lives in a buffer of the runtime and is cut when it is longer than that buffer
(section 21.4). Printing the trace reads the trace table and the stack, so nothing allocates. Since a panic
does not unwind, the stack the trace describes is still there while the handler runs, with the deferred
calls having run below it (section 17.2).

**Faults reach the handler too.** In hosted builds the runtime's signal handler does it. A kernel's
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
raise it with `rt.Abort()` or, for a fault, `rt.PanicFault`. The statements of the description bypass the
lock of section 13.12 and its buffer, so a panic can always report itself.

**The runtime guards the handler.**

- **One panic at a time.** The first processor (in a hosted build, thread) to panic takes an atomic flag
  and runs the handler. One that panics while another handles a panic stops: a kernel's processor disables
  interrupts and halts, and a hosted thread blocks until the first ends the process. In kernel mode the
  runtime tells processors apart by the number each one gives itself with `rt.SetCPU(n)`, which a kernel
  calls on every processor once its GS base points at its per-CPU block (section 20.9, Per-CPU
  variables). Until the first call, every processor counts as CPU 0. Freestanding builds outside kernel
  mode count all processors as one.
- **A last resort.** A panic inside the handler, or while a panic's message is being written, does not
  call the handler again. In freestanding and kernel builds the processor disables interrupts and halts
  (`cli` then `hlt` in a loop), touching no memory or device. In hosted builds the runtime writes a short
  message to standard error with a raw system call and exits with status 2.

### 17.5 Faults

A **fault** is a panic that the hardware reports: an access to memory that is not mapped, an integer
divide error, an illegal instruction. The fault becomes a panic with a `rt.Fault` that records its kind,
the address, the faulting instruction and, in hosted builds, the signal. The panic's trace starts at the
faulting instruction.

**Nil dereference.** Every dereference of a nil pointer panics: a load or store through it, a selector of
a field through it, and an index or slice of a nil pointer to an array. The same holds for a nil `owned`,
`shared` or `synced` value, and for a call of a nil function value. The implementation relies on the
hardware where it can: address zero and the region above it are unmapped (in hosted builds, and in a
kernel, which must arrange it), so an access at a small offset from a nil pointer faults, and the runtime
turns the fault into this panic. Where the offset of an access from the pointer can exceed the size of
that unmapped region (a field at a large offset, or an element of a large array reached through a pointer
to it), the compiler checks the pointer explicitly before the access. A slice of an array through a
pointer is checked explicitly as well, however small the array: forming the slice computes its base
address but reads no memory, so no fault would report the nil. Ordinary accesses therefore cost no check,
and no nil dereference reads or writes memory.

**Divide errors.** An integer division by zero is checked before the hardware sees it and panics as
section 17.1 says. A divide error that the hardware reports anyway (from assembly or unsafe code) becomes a
fault of kind `DivideFault`. The division of the most negative integer by -1 gives the most negative
integer and does not fault (section 11.19).

**Stack overflow.** The stack of a task has a fixed size and a guard region below it (section 19.1,
Tasks). Running into the guard region is a fault, which the runtime reports as a stack overflow with the
task's trace, the size of the stack it had and how to ask for more. A program that recurses deeply asks
`rt.StackLeft()` for the bytes that remain. A stack overflow runs no deferred calls (section 17.2).

**Illegal instruction.** A fault of kind `IllegalInstruction` is reported the same way. Only unsafe code
and corrupt memory produce one.

**Raw pointers.** Heap misuse through raw pointers (`unsafe.Pointer`, memory from `alloc.Raw`) is not
checked by the language (section 18.12, Safe and unsafe code). The debug allocator, when the program uses
it, reports such misuse through this same path: a fault in its heap is described by the allocator, and
that description is the panic's message.

### 17.6 Use of freed memory

A **stale view** is a view of heap memory whose allocation has been freed, or moved by a growing owner,
since the view was made; or a view, string or box into an arena that has been reset, released past the
view, or dropped. The static lifetime rules reject uses they can prove stale. In a checked build,
a stale use of heap or arena memory that remains possible is a run-time panic. This includes fixed
`arena.Fixed` and `arena.Chunked` memory. It is neither a read of whatever the memory holds now nor a
fault. The documented unchecked mode omits the run-time checks (section 18.11).

```go
first := &ix.words.Slice()[0] // a view of the vector's array
ix.Add("x")                   // may grow words and free the old array
println(*first)               // panic: use of freed memory
```

A use is a read, a write, or a drop that updates a count. Dropping a string or a counted box whose memory
an arena has taken back is a use, since the drop changes its count. A string made in an arena between
`m := a.Mark()` and `a.Release(m)` must go out of scope before the `Release`.

The panic for arena memory names the call that ended the arena's epoch, such as `Reset`.

The compiler rejects most stale views before the program runs. A view of a frame is checked at compile
time (section 18.3, Views), and so is a view of a local owner after the owner moved, was dropped or was
assigned. The run-time check covers the views that the compiler cannot follow: heap memory reached
through pointers, and `arena.Chunked` memory.

**Checked and unchecked builds.** The checks are made in a checked build. Section 18.11 (Generation checks)
defines how they work, which builds are checked by default, and the unchecked mode, in which a use of a
stale view has no defined effect.

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
