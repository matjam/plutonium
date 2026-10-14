# Debugging Plutonium programs

**Version:** 0.1 (draft)

This document describes what the compiler and runtime provide for debugging: DWARF debugging information
for debuggers and profilers, call frame information, and the stack traces a program prints when it panics.
The debug allocator, which finds memory errors at run time, is described in
[debug-allocator.md](debug-allocator.md).

The language specification ([language.md](language.md)) is the source of truth. It defines what a panic is
and what happens when one occurs (chapter 17), the checks and the unchecked mode (sections 18.11 and
21.5), the deadlock report (section 19.1), the diagnostics (section 21.1) and the rules for file names and
reproducible builds (section 21.7). Where this document disagrees with it, the specification wins. This
document adds the details the specification leaves to the implementation: the format of the report, the
trace table, and the debugging information.

## 1. Summary

| Feature | Where | Cost |
|---|---|---|
| DWARF: units, functions, inlined calls, line table | `.debug_info`, `.debug_line`, `.debug_abbrev`, `.debug_ranges`, `.debug_aranges` | file size only; not loaded |
| Call frame information | `.debug_frame` | file size only; not loaded |
| Stack traces on panic and faults | `pluto.tracetab` in `.rodata` | loaded, read only on a panic |
| Frame pointers | none (see section 6) | none |

| Flag | Meaning |
|---|---|
| `-g` (default) | emit DWARF |
| `-g=false` | omit DWARF; keep the symbol table |
| `-s` | omit DWARF and the symbol table (`ld -s`) |
| `--trace=false` | leave the trace table empty: panics print no stack trace (a hosted program keeps what a panic needs to run deferred calls, section 7) |
| `--trimpath=false` | record absolute source paths in DWARF and the trace table (section 2.1) |

Panics print symbolized traces with `-g=false` and `-s` too: the trace table does not depend on DWARF or
on the symbol table.

`-g` is on by default. DWARF sections are not part of any loaded segment, so they cost nothing at run time:
the code is the same with and without them, and the loaded image is the same size. They only make the file
larger (section 7 has the numbers), which matters mostly for freestanding images, where the linker script
or `--format binary` decides what is kept anyway. Having debuggers and profilers work on every build
without a rebuild is worth more than the file size; `-s` or `strip` removes it when it matters.

## 2. Source positions

Every instruction carries the source position it was generated from:

1. Lowering gives each SSA value the position of the statement or expression it comes from
   (`ssa.Func.SetPos`). Bounds checks and other panics take the position of the expression that failed.
2. The inliner copies the callee's positions into the caller and records the call as an *inline site*
   (`ssa.Func.Inlines`: the callee's name, the call's position, and the enclosing site). Each copied value
   carries the index of its site (`ssa.Value.Inl`), so a position in an inlined body knows its call chain.
3. Values made by optimization passes have no position of their own unless the pass inserts them next to
   a value that has one. A position always travels with its inline site: a pass that copies or replaces
   a value or block (the loop unroller, the vectorizer, the inliner, edge splitting) copies both
   (`ssa.Value.CopyPos`, `ssa.Block.CopyPos`), since a position from an inlined body without its site
   would be reported as a line of the function it was inlined into. Under `PLUTO_SSACHECK`, after every
   pass, `ssa.Func.VerifyPos` checks that each position lies in the source of the function its site
   names (`ssa.Func.Pos` to `ssa.Func.End`, or the inlined function's extent recorded in its site).
4. Instruction selection gives each machine instruction the position of the value it is selected for.
   Constants and addresses folded into an instruction take the position of the instruction that uses
   them. The prologue has the function's declaration; an epilogue has its `return` statement.
5. Instructions with no position (spill code, register copies) take the position of the next instruction
   that has one in emission order, or the previous one at the end of a function.
6. The encoder records the offset of every instruction. Branch relaxation, which shortens branches after
   encoding and so moves the code after them, moves these offsets with the code.

### 2.1 File names

DWARF and the trace table name source files relative to two fixed roots, so that the same source gives
the same binary whichever directory, machine or user builds it (language spec section 21.7):

| File | Recorded as | Example |
|---|---|---|
| in the program's module | `./` and its path from the module root (the directory holding `pluto.mod`) | `./cmd/tool/main.p` |
| in a main package outside any module | `./` and its name (the package directory is the root) | `./main.p` |
| in the standard library | its import path and name, whether the library is embedded or read from `$PLUTOROOT` | `pluto/fmt/print.p` |
| generated by the compiler | its name | `_testmain.p` (`pluto test`) |

A Plutonium program has one module, so the module path would name nothing that the leading `./` does
not, and the form relative to the module root is a path a debugger and an editor can open from there. The
`./` keeps the program's files apart from the standard library's.

`--trimpath=false` (on `pluto build`, `pluto run` and `pluto test`) records absolute paths instead: the
program's files by their real path, the standard library's under `$PLUTOROOT` when it is set and as
`$PLUTOROOT/pluto/...` when the library is embedded. The build is then reproducible only in the same
directory (language spec section 21.7).

Compiler error messages are not affected.

## 3. DWARF

The compiler writes DWARF version 4 into its object file; the linker concatenates and relocates it.

- **Compile units**: one per package, named by the import path, with `DW_AT_language` `DW_LANG_Go` (DWARF
  has no code of its own for Plutonium), no
  `DW_AT_comp_dir` (the build directory with `--trimpath=false`), and the package's functions as
  `DW_AT_ranges`. Generated functions with
  no package (`pluto.main`, method wrappers) are in a unit named `pluto`.
- **Subprograms**: one per function, named by its symbol (`main.f`, `main.(*T).M`, `pluto/fmt.Printf`),
  with `DW_AT_low_pc`, `DW_AT_high_pc`, `DW_AT_decl_file` and `DW_AT_decl_line`, and the CFA as
  `DW_AT_frame_base`.
- **Inlined calls**: a `DW_TAG_inlined_subroutine` for each inlined call, nested as the calls were, with
  the code that came from it (`DW_AT_low_pc` and `DW_AT_high_pc` if it is contiguous, otherwise
  `DW_AT_ranges`) and the call's `DW_AT_call_file`, `DW_AT_call_line` and `DW_AT_call_column`. Its
  `DW_AT_abstract_origin` is an abstract `DW_TAG_subprogram` for the inlined function in the same unit,
  with `DW_AT_inline` and the function's `DW_AT_decl_file` and `DW_AT_decl_line`. A call with no source
  position, such as `pluto.main`'s call of `main.main` once `main.main` is inlined into it, has call line
  and column 0 and the inlined function's own file as its call file: gdb leaves an inlined call without
  `DW_AT_call_file` out of its backtraces, and llvm-dwarfdump rejects DWARF 4's file 0 ("no file"). gdb
  then shows such a caller with its address and no line (`#3 0x... in pluto.main ()`).
  The line table attributes the inlined code to the inlined function's own lines.
- **Line table**: one sequence per function, a row wherever the file or line changes. The row where the
  body starts is marked `prologue_end`: the first line after the function's declaration line once the
  frame is set up, or the end of the frame setup if the whole function is on one line. Debuggers put a
  function's breakpoint there.
- **Call frame information**: `.debug_frame`, one CIE and one FDE per function (section 4).
- **Not emitted yet**: types, variables and parameters (`info locals`, `print x`), so neither subprograms
  nor inlined calls have `DW_TAG_formal_parameter` or `DW_TAG_variable` children, and column numbers in
  the line table.

File names are relative (section 2.1), and with no compilation directory a debugger resolves them against
its own working directory. The line table splits each name into a directory (`.` for `./main.p`) and a
base name, so gdb, `ld` and `addr2line` print the names the trace table prints. A compilation directory
of `.` would add nothing for a debugger, and `ld` and the other binutils tools would prepend it to the
line table's directory and report `././main.p`. Run the debugger from the module root (for a program outside a module,
its directory) and it finds the program's source; from anywhere else, or for the standard library, add the
roots to its source path:

```
(gdb) directory /home/me/prog       # the module root
(gdb) directory /home/me/pluto/lib  # $PLUTOROOT, which holds pluto/
```

A `--trimpath=false` build names every file absolutely, so the debugger finds the sources from anywhere
on the machine that built it (and the standard library's only when `PLUTOROOT` was set).

Absolute paths are not the `-g` default, although they need no `directory`: DWARF is on by default, and a
default build that recorded the build directory would make every binary depend on where it was built,
which is what self-hosting (stage 2 and stage 3 must be byte-identical) and reproducible builds cannot
have (language spec section 21.7).

### What works

With `gdb` (tested with gdb 17 by `internal/build/dwarf_test.go`, at -O0 and -O2, run from the module
root, and with `--trimpath=false` from elsewhere):

- `break main.p:12` and `break main.f` (after the prologue, and at each copy of `main.f` inlined
  elsewhere), `run`, `next`, `step`, `finish`;
- `bt` through every frame, with or without a frame pointer, with inlined calls as frames of their own,
  including `main.main` where it is inlined into `pluto.main`;
- `info line`, `list`, `disassemble /s`, `info frame`, `up` and `down`;
- in a task, `bt` through its frames to the task statement's thunk (`main.main.task1`, at the line of
  the task statement) and `pluto/rt.taskMain`, ending at the runtime's entry `goStart`, whose call frame
  information marks it as the outermost frame. Each worker thread is a gdb thread (`info threads`);
  the backtrace of a parked task is not reachable from gdb yet.

The hosted scheduler preempts with `SIGURG`, which gdb passes to the program without stopping.

gdb guesses Pascal from the `.p` extension until the program stops in a frame (whose unit says
`DW_LANG_Go`), and
Pascal reads `main.f` as a field access, so before `run` quote such names (`disassemble 'main.f'`) or
`set language go`.

`step` into a function stops first on its declaration line: gdb trusts `prologue_end` markers for
`break` but, for producers it does not recognize, analyzes the prologue itself when stepping, and finds no
`push rbp; mov rbp, rsp`. One more `next` reaches the body.

`perf record --call-graph dwarf` unwinds with the same `.debug_frame` information, and `perf report` and
`perf annotate` use the line table; lldb reads the same sections. Neither was available where this was
written, so these are untested.

Printing variables does not work yet (there is no type or location information).

## 4. Frames and call frame information

Frames have one of two fixed shapes ([abi.md](abi.md#stack-frame-layout)):

- **Without a frame pointer** (the usual case): `push` each callee-saved register the function uses, in the
  order RBX, RBP, R12, R13, R14, R15; `sub rsp, N`. The body never moves RSP. A leaf with nothing on the
  stack has no prologue at all.
- **With RBP as a frame pointer**, for functions with a stack slot aligned to more than 16 bytes:
  `push rbp; mov rbp, rsp`, the other pushes, `and rsp, -align; sub rsp, N`.

An epilogue undoes this (`add rsp, N` or `lea rsp, [rbp - 8k]`, the pops, `ret`). A function can have
several epilogues, with body code after them.

The backend follows these instructions and records the frame state wherever it changes: how to compute the
canonical frame address (CFA, the value of RSP before the call: RSP + n, or RBP + 16 once RBP is set) and
where each saved register is (at CFA - 16 - 8k for the k-th push). After each `ret` the state returns to the
body's. The FDEs encode exactly these states with `DW_CFA_def_cfa*`, `DW_CFA_offset` and
`DW_CFA_restore`; the CIE gives the state at entry (CFA = RSP + 8, return address at CFA - 8).

## 5. Stack traces

A panic, a runtime fault (an index or slice out of range, division by zero, a failed type assertion
and the rest in [language spec chapter 17](language/17-run-time-panics.md))
or, in hosted builds, a hardware fault goes to the panic handler (language spec section 17.4), after the
panicking task has run its deferred calls (language spec section 17.2; freestanding and kernel builds run
none). A panic does not unwind the stack, so the trace is that of the place where the panic happened. The
built-in handler of hosted programs, and `rt.Panic.Print`, print `panic: ` and the message and then the
stack, innermost frame first; the built-in handler then ends the process with status 2:

```
panic: runtime error: index out of range [4] with length 4
    main.level3 (./main.p:4)
    main.level2 (./main.p:8)
    main.level1 (./main.p:12)
    main.main (./main.p:18)
```

In a module, files are named from the module root, and standard library files by import path
(section 2.1):

```
panic: negative width
    example.com/repro/geom.Area (./geom/geom.p:5)
    example.com/repro/cmd/repro.main (./cmd/repro/main.p:7)
```

- Each frame is the function and the file and line of the call it is making (or, innermost, of the
  instruction that failed).
- A call that was inlined is reported as a frame of its own, as if it had not been inlined, so a trace is
  the same at every optimization level.
- A default argument ([language spec section 10.13](language/10-declarations-and-scope.md)) is evaluated by
  the caller, and its code has the position of the call, so a panic in it is reported at the call's line.
- A function literal is `<enclosing function>.func<N>`, and the body of a range-over-func loop is
  `<enclosing function>-range<N>`. A closure a function returns (an iterator such as `v.All()`,
  language spec section 11.5) is named after that function:
  `pluto/vec.(*Vec).All[int].func1`, not `main.main.func1`, whichever function called it and provided
  the region it is built in, and also when the function was inlined into its caller.
- The panic's own frames in package `pluto/rt` are left out, and so is generated code with no source
  position, such as the wrapper that calls a method through an interface.
- The walk stops at `pluto.main` (which runs initialization and `main.main`; where `main.main` is
  inlined into it, `main.main` and its inlined calls are still printed), at the bottom of a task
  (`pluto/rt.taskMain`), at code that is not Plutonium (assembly, C, a kernel's entry point), or after
  64 frames, which it reports with `...additional frames elided...`. A fault in an assembly leaf that
  the table does not describe (an atomic operation) is stepped over, since such a function has no
  frame: the walk continues from the return address at the stack pointer.

A panic in any task but the one that runs `main` names it before the trace (`rt.Panic.Task`).
The last frame is the task statement that started it, as its generated thunk, at the task statement's line:

```
panic: runtime error: index out of range [3] with length 3
task 3:
    main.level2 (./main.p:7)
    main.worker (./main.p:11)
    main.main.task2 (./main.p:20)
```

One panic is handled at a time: a task that panics on another thread while one is reported waits
for the process to end.

A panic in a deferred call that runs for a panic goes straight to the panic handler, and `rt.Panic.During`
points to the first panic (language spec section 17.2). The built-in handler prints both messages, then the
trace of the second panic through the deferred call into the frames where the first one happened. The
handler of a panic sees the panic as it happened: its trace starts where the panic happened, not in the
deferred calls.

A use of freed memory is a panic (language spec section 17.6) and is reported like any other. The panic
for fixed or Chunked arena memory names the call that ended the allocation's incarnation, such as
`Reset` or `Release`. Static lifetime errors prevent the use before execution; a remaining stale
access is reported by this panic path in a checked build. Whether a build makes these
checks is the checked or unchecked mode of language spec sections 18.11 and 21.5.

### Parked tasks

`rt.TraceTask(t *rt.Task, w)` writes the trace of a task that is not running, in the same format and
with the same rules, from the state its last switch saved in `t.Context()`: the stack pointer points at
RFLAGS and the callee-saved registers, then the return address into the caller of `swapcontext`
(`ctx_amd64.s`), and that return address, the stack pointer after it and the saved RBP start the walk.
Like `Panic.PrintTrace` it is generic in the writer, reads the trace table and the stack, and allocates
nothing. The frames of package `pluto/rt` that park the task, including calls of it inlined into the
program's functions, are left out, so the first frame is the program's (or the library's) call that
waits: `main.receive`, `pluto/sync.(*Mutex).lockSlow`. The task must be switched out and stay so;
`Task.Parked()` says it is waiting in `Park`, and `Task.WaitReason()` says what for (`rt.WaitReason`,
set by `rt.ParkReason`; plain `Park` gives "waiting"). A task preempted in the middle of a function has
the preemption stub on top, where the walk stops, and one that has not run yet has no frames.

The hosted scheduler uses it when the program deadlocks. It keeps every task on a list in the order they
were started, and after "fatal error: all tasks are asleep - deadlock!" prints each one's number,
what it waits for and its trace (language spec section 19.1). The task that runs `main` comes first.
Helpers of the runtime that wait for work (the runner of `time.AfterFunc` functions) are left out. The
program then exits with status 2:

```
fatal error: all tasks are asleep - deadlock!

task 1 [chan receive]:
    main.main (./main.p:53)

task 2 [chan receive]:
    main.receive (./main.p:8)
    main.middle (./main.p:12)
    main.main.task1 (./main.p:47)

task 3 [chan send]:
    main.send (./main.p:16)
    main.main.task2 (./main.p:48)
```

The reasons are `chan receive`, `chan send`, their `(nil chan)` forms, `select`, `select (no cases)`,
`sleep`, `IO wait` (a task waiting in the poller; it keeps the program from being reported as
deadlocked), and for `pluto/sync` `sync.Mutex.Lock`, `sync.RWMutex.RLock`, `sync.RWMutex.Lock`,
`sync.WaitGroup.Wait`, `sync.Cond.Wait` and `sync.Scope.Wait`. A program built with `--trace=false`
prints the numbers and reasons without frames. Tests: `testdata/trace/deadlockchan.p`, and
`internal/build/deadlock_test.go` for tasks parked in the library.

When the task that runs `main` ended with `rt.Goexit` and no task is left, or all wait for good, the
program also ends with a fatal error, one that says `main` called `rt.Goexit`, and status 2 (language spec
sections 15.4 and 17.3). A deadlock is not a panic (language spec section 17.1).

### Hardware faults (hosted)

At startup the runtime installs a handler for SIGSEGV, SIGBUS, SIGFPE and SIGILL that runs on a 16 KiB
alternate signal stack, so that it can report a stack overflow. It prints

```
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation addr=0x8 pc=0x401213]
    main.(*node).second (./main.p:9)
    ...
```

and the trace from the faulting instruction, then exits with status 2 without running the exit hook
(`rt.SetExitHook`). A fault within 4 KiB of address zero is reported as a nil dereference; one just below
the stack pointer, in the guard region below the running task's stack or below the main thread's,
as `stack overflow`; any other as `unexpected fault address`. A stack overflow's message names the size
of the stack and how to ask for a larger one (language spec section 19.1):

```
panic: runtime error: stack overflow (the task's stack is 64 MiB; start the task with rt.Go(f, size) for a larger one)
panic: runtime error: stack overflow (main's stack is 8 MiB; ask for a larger one with //pu:stack on func main)
```

A function whose frame is larger than a page probes it in its prologue (abi.md section 6), so the fault
is in a guard region however far below the stack pointer the frame reaches.

The signal handler does not print this itself: it raises a panic, with the fault in `rt.Panic.Fault`
(kind, address, faulting instruction, signal) and a trace that starts at the faulting instruction, and
calls the panic handler (language spec section 17.4). The output above is the built-in handler's; a
program with a `//pu:panic` handler of its own gets the same information. When the panic will run
deferred calls (language spec section 17.2), the handler first describes the fault, then rewrites the
signal's saved context so that, once it returns, the task resumes on its own stack, below the 128-byte red
zone, in a runtime function that raises the panic there: deferred calls never run on the signal stack or
with the fault signals blocked. A stack overflow raises the panic in the handler, without deferred calls.

There is one handler. For `SIGSEGV` and `SIGBUS` it first calls the function registered with
`rt.SetFaultHandler`, if any, with the fault address, the faulting instruction and whether the access was
a write. The debug allocator registers one ([debug-allocator.md](debug-allocator.md#faults-and-the-runtime)):
if the address is in its heap its diagnosis (`debug allocator: use after free: ...`, with where the block
was allocated and freed) becomes the panic's message in place of the runtime's, and the signal line and
the trace follow as usual. No deferred calls run for such a panic (language spec section 17.2). A fault in a function's prologue, before its frame is complete, is unwound
correctly too. Every thread has an alternate signal stack: the main thread's 16 KiB one, and 32 KiB for
each worker thread of the hosted scheduler, so a task that overflows its stack on any worker is
reported with its trace. The scheduler's preemption signal (`SIGURG`) runs on the same stacks.

### Freestanding builds

Walking the stack allocates nothing and needs no operating system: it reads the trace table and the stack.
A freestanding or kernel program has exactly one panic handler, marked `//pu:panic`, which never returns
and cannot wait or start a task (language spec section 17.4). It prints the trace where it likes with
`p.PrintTrace(w)`, or the whole report, message, fault and trace, with `p.Print(w)`, to any `rt.Writer`;
`rt.Console{}` writes through `rt.SetConsole`'s writer (in a hosted program, to standard error with a
system call that never waits, which makes it the writer for a hosted panic handler too). Since a panic does not unwind, the frames the
trace describes are still on the stack while the handler runs. (In hosted builds the deferred calls run
before the handler, below those frames, which they leave as they were: language spec section 17.2.) A kernel's exception handlers turn a fault
into a panic whose trace starts at the faulting instruction with

```go
rt.PanicFault(kind rt.FaultKind, addr, pc, sp, bp uintptr) // the fault, and the faulting RIP, RSP and RBP
```

or print the trace of the interrupted code to the console without panicking with
`rt.Traceback(pc, sp, bp uintptr)`.

A frame of a `//pu:interrupt` function ends the walk, since the code it interrupted is not its caller.

### Naming one address

`rt.PrintPC(pc)` prints the function and line of a single address from the same table, as a trace would
name its innermost frame (`main.makeNode (./main.p:12)`), or `pc 0x...` if the table does not
describe it. The debug allocator prints allocation and free sites, recorded with `rt.CallerPC`, this way
([debug-allocator.md](debug-allocator.md#allocation-sites)). `rt.SetSymbolizer` replaces the lookup, for
code the table does not cover.

### The trace table

The compiler emits one read-only table, `pluto.tracetab`, in `.rodata`; `lib/pluto/rt/trace.p` reads it.
All integers are little-endian; offsets are from the start of the table.

| Part | Contents |
|---|---|
| header (24 bytes) | `u32` number of functions, `u32` offset of the strings, `u32` offset of the line programs, `u32` offset of the inline sites, `u32` number of defer records, `u32` offset of the defer records |
| functions (32 bytes each) | `u64` start address, `u32` size, `u32` name (string offset), `u32` frame size (the `sub rsp`), `u16` end of the prologue, `u8` saved-register mask (bit 0 RBX, 1 RBP, 2-5 R12-R15), `u8` flags (1 frame pointer, 2 interrupt handler), `u32` offset of its line program, `u32` index of its first inline site |
| defer records (16 bytes each, by function) | `u32` index of a function with `defer` statements, `u32` offset of its defer record from RSP in its body, `u64` address of the function that runs the record's armed deferred calls (hosted builds; language spec section 17.2) |
| inline sites (16 bytes each) | `u32` parent site (1-based within the function, 0 for none), `u32` inlined function's name, `u32` call's file, `u32` call's line |
| strings | `u16` length, then the bytes |
| line programs | per function, rows covering its code from offset 0: `uvarint(zigzag(line delta) << 2 \| file changed << 1 \| site changed)`, then the new file (string offset) and the new inline site (1-based within the function, 0 for none) if they changed, then `uvarint` length of the row's code |

To unwind a frame at `pc` (a return address, looked up as `pc - 1` so that it falls within the call):

- without a frame pointer, CFA = RSP + frame size + 8 × (pushes) + 8;
- with one, CFA = RBP + 16, and the caller's RBP is at [RBP];
- the return address is at CFA - 8, and the caller's RSP is CFA;
- if RBP was pushed as an ordinary callee-saved register, the caller's RBP is at CFA - 16, or CFA - 24
  when RBX was pushed first; otherwise RBP still holds the caller's value.

For the innermost frame of a fault inside a prologue, the runtime counts the pushes already done from the
mask (pushes of RBX and RBP are one byte, of R12 to R15 two) and the frame-pointer setup
(`push rbp` is one byte, `mov rbp, rsp` three).

Functions are found by a linear search, which is fast enough for printing a trace once. A sorted index
would be needed for sampling profilers in the runtime; none exists yet.

A hosted panic walks the same frames to run their deferred calls (language spec section 17.2): for each
frame whose function has a defer record, the record lies at the frame's RSP plus the listed offset, its
first word holds the armed bits, and the runtime calls the listed function with the record's address if
any is set. Such a frame's runner is left out of traces, and a trace of a panic in a deferred call goes
on, at the runtime's call of the deferred calls, with the frames of the panic they ran for.

## 6. Frame pointers or a table

The compiler does not keep a frame pointer except where it realigns the stack, and the runtime unwinds with
the trace table. Measured on the benchmark suite (`bench/`, three runs, median), giving every function an
RBP frame pointer, which takes RBP away from the register allocator and adds `push rbp; mov rbp, rsp` and
`pop rbp` to every function, made the geometric mean 4% slower relative to Rust (1.05 to 1.09), with the
call-heavy benchmarks hit hardest: `fib` 19% slower, `structs` 34%, `binarytrees` 12%. (One run of each
on a shared machine, with Rust's times within a few percent of each other in the two runs; treat the figures as
indicative.) The table costs
nothing at run time and about 140 bytes per function in all (section 7). Frame pointers would only have
saved the table's frame descriptions, since the table is needed anyway for names and lines, and with
frames of a fixed shape those descriptions are 8 bytes per function.

## 7. Size

Sizes in bytes of some of the benchmark programs (`bench/`), built at -O2, before this work ("before") and
with each flag. "Code and read-only data" is the `text` column of `size`: code, `.rodata` and the trace
table, which is what a freestanding image keeps.

| Program | File before | `-g` (default) | `-g=false` | `-s` | Code and read-only data before | after |
|---|---:|---:|---:|---:|---:|---:|
| fib | 76872 | 149040 | 101784 | 78848 | 48221 | 71065 |
| nbody | 87912 | 162472 | 114152 | 86456 | 54797 | 78081 |
| spectralnorm | 83904 | 157280 | 109088 | 83648 | 52285 | 75401 |
| binarytrees | 98024 | 223328 | 133264 | 109768 | 67661 | 104929 |

For `fib`, which has 133 functions (most of them from `pluto/fmt`, `pluto/strconv` and the runtime), the
23 KB of growth in loaded code and data is the trace table (18.7 KB: 4.3 KB of function entries, 4.1 KB
for 258 inline sites, 3.9 KB of names and file names, 6.4 KB of line programs) and 4 KB of runtime code
that walks the stack and handles faults. The DWARF sections add 47 KB to the file. Hosted programs also
reserve a 16 KB alternate signal stack in `.bss`, which takes no space in the file and no memory until a
fault uses it.

`--trace=false` leaves the trace table empty, for images that cannot spare it: `fib` is then 52369 bytes of
code and read-only data, and a panic prints its message without a trace. A hosted program with `defer`
statements keeps the frame shapes and defer records (without names but those of the bottom frames, lines
or inline sites), which a panic needs to run its deferred calls; freestanding builds never need them.

Debugging information never changes code generation: the machine code of every benchmark's own functions
is byte for byte what it was before this work, with or without `-g`. The only new work at run time is
installing the fault handlers at startup (five system calls), so speed is unaffected; the benchmark suite
shows no change beyond its noise.

## 8. Reproducibility

The same source, compiler and flags give a byte-identical executable in any directory, on any machine and
for any user (language spec section 21.7): file names are
relative (section 2.1); object files carry no timestamps; symbols, sections and relocations are emitted in
an order fixed by the program (never by map iteration); the assembler and C compiler run in the work
directory on relative names, so no temporary path reaches their objects; and the linker is told to add no
build ID (`--build-id=none`), which some linkers add by default depending on how they were configured.
`internal/build/determinism_test.go` checks this: object files compiled repeatedly in fresh directories,
and executables (a module with packages in two directories, and a program with C files) built in two
directories at -O0 and -O2, with and without DWARF. The system assembler, linker and C compiler must be
the same versions for executables to match across machines. Debugging information describes the program and
changes nothing in the code that is generated (language spec section 21.3).

## 9. Not done yet

- Variable and type information (`DW_TAG_variable`, `DW_TAG_formal_parameter`, `DW_TAG_*_type`) and
  locations, first for parameters at -O0, where they live in stack slots.
- Column numbers in the line table.
- `.eh_frame`, so that a stripped binary can still be unwound by tools.
- Traces of the other tasks in a panic report (a panic handler can call `rt.TraceTask`, but the
  runtime keeps no list of tasks for it to walk outside the hosted deadlock report), and of parked
  tasks in gdb.
- A sorted function index in the trace table, for in-process sampling profilers.
- Call frame information for `//pu:interrupt` handlers describes the interrupted RIP (and the error
  code's slot) but not the interrupted RSP, which the CPU saved in its frame; a debugger attached to a
  kernel stops its backtrace at the handler.
