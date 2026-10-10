# Plutonium roadmap

**Updated:** 2026-10-10

Every known gap between the specification ([language.md](spec/language.md),
[memory-model.md](spec/memory-model.md), [abi.md](spec/abi.md)) and the compiler and standard library,
in priority order. Statuses:

- **done**: implemented and tested on `main`;
- **in progress**: being worked on now (design or implementation);
- **planned**: specified, not started;
- **proposed**: written up, awaiting the user's decision before it becomes part of the language.

The order puts wrong code first (the compiler must never produce it, AGENTS.md rule 4), then what a
kernel needs, then the concurrency features the spec already promises, then the rest of the language,
tooling, platforms and libraries. Item numbers are stable because other documents cite them; an item added
later takes the next free number and is listed in the section where it belongs.

## Wrong code and silent failures

1. **Volatile loads order plain loads** (done): each volatile load now starts a new memory state, so
   plain loads on either side of a volatile polling loop stay separate (memory-model.md section 5.2).
2. **Interface boxes in package initialisers** (done): `var x any = "s"` boxed the string in the init
   function's frame and left a dangling pointer; such boxes now get static storage as `&T{}` does
   (language spec section 4.4, `testdata/run/initboxes.p`).
3. **`//pu:interrupt` compiles as an ordinary function** (done): handlers now get the entry and exit
   sequence of abi.md section 10.4 (item 9).
4. **`//pu:extern` and `//pu:export` with aggregates** (done): calls and entry points now follow System V
   (item 12), and signatures with no C equivalent are compile errors.
5. **Escape errors for frame views** (planned): returning `&x`, a slice of a local array, an interface
   holding a frame copy or a closure with captures, or storing one in a package variable, on the heap or
   through a caller's pointer, or passing it to a function that keeps it, must be a compile error
   (language spec sections 4.4, 4.5 and 6.3), with the explanatory messages of section 6.9; today nothing
   checks it. This is the compile-time half of "a view may not outlive what it looks at" (item 20).

## Kernel enablement

6. **Memory model** (done, this document set): [memory-model.md](spec/memory-model.md), Go's model with
   packed locations, volatile, interrupt and scheduler rules.
7. **Kernel mode** (done): `--mode kernel` sets the `kernel` tag and links with a default top-2-GiB
   linker script; floating point is a compile error outside `//pu:fpu` functions, enforced through
   `ssa.Func.NoSSE` in lowering, the inliner and the backend (language spec section 12.2, abi.md section
   10). `pluto/fmt` formats floats only outside kernel mode. Tested by booting `testdata/kernel/smoke`
   under QEMU with SSE disabled.
8. **`pluto/arch/x86` intrinsics** (done): every function of abi.md section 10.5 expands inline to an
   `ssa.OpIntrinsic`, a compiler barrier; encodings are checked against GNU as.
9. **`//pu:interrupt`** (done): interrupt and exception handlers with register saves, `cld`, stack
   alignment, the error-code variant and `iretq` (abi.md section 10.4), with `x86.InterruptFrame` and
   `x86.HandlerAddr`. Tested by booting a kernel under QEMU (`testdata/kernel/irq`). In kernel mode a
   handler without `//pu:fpu` saves no XMM registers.
10. **Per-CPU variables, `//pu:percpu`** (done): a `.percpu` section linked at 0, accessed through `GS`
    (abi.md section 10.6). Tested as a Linux process that switches GS with `arch_prctl`, and in the QEMU
    smoke kernel.
11. **Error design** (in progress): how errors are made, wrapped and given context without hidden
    allocation, given that frame-boxed interfaces cannot be returned; owned by a dedicated design note.
    The library conventions and `errors.New` as a static error per call site (design 9.1) are done; the
    escape checks of item 5 remain. Reconciling allocated errors with the ownership model is
    **proposed** (language spec section 14.6): they become `synced` objects that `error` values count, so
    each is released exactly once and `errors.Free` goes away. With counted strings (item 57) an error's
    text is a string it keeps: `fmt.Errorf`'s message, `*os.PathError`'s path.
12. **`//pu:extern` C struct classification** (done): System V eightbyte classification for aggregate
    parameters and results, hidden result pointers, several results as a struct, export wrappers and
    func-value thunks (abi.md section 9.1); `.c` and `.o` files in packages are linked (section 9.2).
    Tested against GCC and Clang. Strings, slices, interfaces, func values and C varargs are rejected.
13. **Debugging** (done): DWARF line tables and CFI, symbolised stack traces on panic and on faults,
    and the debug allocator, which reports through the same fault path and names its sites from the
    same table; specified in [debugging.md](spec/debugging.md) and
    [debug-allocator.md](spec/debug-allocator.md). Implementation items 41 to 43; what remains is listed
    there and in debugging.md section 9.
54. **Compile-time panic handler, `//pu:panic`** (done): one `func(p *rt.Panic)` per program, required in
    freestanding and kernel builds, linked as a direct call from the runtime's panic path, checked never
    to return (with inferred no-return functions); `rt.Panic` carries the message, value, fault and CPU
    and prints its trace without allocating; one panic at a time with a last resort for nested panics
    (language spec section 8.1). Replaces `rt.SetPanicHandler`. Tested under QEMU
    (`testdata/kernel/panic`: explicit panic, bounds error, page fault in an interrupt handler, nested
    panic) and hosted. Hosted panics report CPU 0 and name the puroutine that panicked instead (item 14).
55. **Interrupt context cannot block** (done): a whole-program check from every `//pu:interrupt` handler
    and the panic handler against blocking channel operations, `go`, `//pu:blocking` functions and
    allocators that may wait, conservative through interfaces and func values (language spec section
    12.6). Channel operations and `go` are found in the source, and their lowering calls `//pu:blocking`
    runtime entries, so the walk sees them through library code too; `pluto/sync` marks its parking
    operations `//pu:blocking`, and the bound scheduler's `Wake` is checked as interrupt context.

## Concurrency

14. **Puroutines and the scheduler** (done): `go`, `rt.Go`, fixed stacks with guard pages, the
    runtime's task mechanism (`rt.Switch`, `rt.Park`/`rt.Ready` with a permit, `rt.Preempt`) and the
    hosted work-stealing scheduler on Go's M/P/G design: per-processor run queues with stealing,
    worker threads made with `clone`, preemption by `SIGURG` from a monitor thread every 10 ms, hand-off
    of a processor blocked in a system call (`os` file I/O, `Process.Wait` and the waits of
    `os/exec`), an epoll poller for pipes from `os.Pipe` and non-blocking descriptors given to
    `os.NewFile` (sockets), with a read waiter and a write waiter per descriptor as in Go's `pollDesc`,
    and `Close` waking the waiters with `os.ErrClosed` (`File.Fd` makes one blocking again, as in Go,
    so `os/exec` can hand it to a child), `SIGPIPE` handling as in Go (a write to a closed pipe
    returns `EPIPE`; on standard output or error it ends the program with the signal), timers
    (per-processor heaps run by the workers and the poller, `rt.Now` through the vDSO,
    `rt.ParkUntil`), and deadlock detection that waits for armed timers (language spec section 4.7;
    abi.md section 12). `rt.StackLeft` measures a puroutine's own stack. The sendable rules are checked
    (`shared[T]` never crosses, frame views are rejected syntactically, and closures in the current
    frame are a compile error, E2594, where the compiler can tell, and panic at the `go` otherwise).
    Follow-ups: traces of parked puroutines (`rt.TraceTask`); a program that binds its own hosted
    backend runs on one thread; spawning costs about twice Go's.
15. **Channels and `select`** (done): `chan T`, send, receive, `close`, `range`, `select` with Go's
    random choice, `len`, `cap` and `make(chan T, n, a)`, on Go's design (a lock per channel, waiters
    queued in their frames, values copied directly to a waiting receiver). Channels are freed with
    `alloc.FreeChan` until item 48 counts them. Buffered channels run at about a third of Go's speed.
16. **`pluto/sync`** (done): `Mutex`, `RWMutex`, `WaitGroup` (with `Go`), `Once` and `Cond` over the
    scheduler, with the guarantees of memory-model.md section 3.4. Parking operations are
    `//pu:blocking`; `TryLock` is not.
17. **Thread-safe default allocator** (done): `alloc.SyncSlab`, per-processor caches in front of shared
    slabs, is the hosted default; the debug allocator's lock is real; allocation sites are per
    puroutine (memory-model.md section 3.8).
18. **Stack size analysis and `-stackreport`** (planned): compute the stack a spawned function needs when
    its call graph has no recursion or indirect calls. Stack requests already carry a `Proven` bound,
    always 0 today.
53. **Scheduler contract for kernels** (done; [scheduler contract](plans/2026-10-09-scheduler-contract.md),
    abi.md section 12): the interface and rules any kernel implements to run puroutines, with no kernel
    scheduler in the standard library: a permit-based `Park`/`Ready`, interrupt-safe `Ready`,
    backend-allocated task records embedding `rt.Task`, a current-task slot (`%gs:8` in kernels), a
    preemption count, `rt.Preempt` on the interrupted task's stack, stack requests carrying the proven
    bound, and link-time binding with `//pu:scheduler`. Timers are optional: a backend that provides a
    clock (`Now`) and a timer (`ArmTimer`, serviced with `rt.RunTimers`) runs the runtime's
    allocation-free timer heap, and a program that uses timers without them fails to build (E2595). A
    single-processor test kernel with timer preemption, and `time.Sleep`, `time.After` and tickers
    on the PIT, runs under QEMU. Its section 9 lists later requirements from PlutoniumOS (a userland
    target, `//pu:nonblocking`, a `portable` constraint). The PlutoniumOS task model they serve is in
    PlutoniumOS: docs/architecture/lightweight-tasks.md (github.com/matjam/plutoniumos, private).

## Language

19. **Methods with type parameters** (done): merged on 2026-10-09; method values must be instantiated.
20. **Ownership: owners, moves and views** (planned; language spec section 6). Today `owned`, `shared`
    and `synced` exist in the checker and nothing else does. The work:
    - `owner` in type definitions and in type parameter constraints, with the placement rules of section
      6.4 (structs with owner fields must be declared `owner`; `Drop` only on owners; never inferred);
    - the move checker: moves on assignment, passing, return, send, `go`, field stores and owning
      interfaces; use-after-move and maybe-moved errors with last-use hints; moving out of places only
      through `take` or permuting assignments; value receivers that consume;
    - automatic drops on every path, in reverse declaration order, with drop flags for conditional moves,
      temporaries dropped at the end of their statement, and fields dropped after the type's `Drop`;
    - views of local owners: not escaping, and not used after the owner is moved or dropped;
    - generation checks for heap views (design in progress), the run-time half of "a view may not
      outlive what it looks at";
    - the explanatory diagnostics of section 6.9, and the kind of each type (`owner (moves)`, `shared
      (counted)`, `view`, `value`) in editor hover and `pluto doc`.
    Item 5 is the compile-time check for frame views. Items 48 to 51 are the counted references, the
    library and the proposals that go with this item.
21. **Default parameter values** (done): language spec section 5.1, with the open questions settled
    there (variadic functions, function values, interface methods, generics, `f(g())`). The library's
    `F`/`FIn` pairs are single functions with a trailing allocator defaulting to `alloc.Default()`;
    `new` and `make` take an optional allocator (section 4.2), and so does `make(chan T, n, a)`.
22. **Range over func and iterators** (done): language spec section 4.10. Loop bodies are yield
    closures in the enclosing frame, with Go's exit codes and state checks; iterator constructors are
    closure constructors (section 4.5), expanded in the caller. `pluto/iter` has `Seq`, `Seq2` and
    allocation-free adaptors; `vec.Vec`, `hash.Map`, `hash.Set`, `strings` and `bytes` have iterators.
    The inliner inlines calls through known closures and SimplifyCFG threads branches on phis of
    constants, so a loop over `v.All()` compiles like one over `v.Slice()`. Follow-ups: the escape
    checks (item 5) should reject an iterator that keeps `yield`; `defer` in a loop body runs at most
    once per call (item 28); `ring[T, N]` has `All` and `Values` (section 5.4); no `iter.Pull` without
    coroutines. A constructor's closure is named after the constructor (`main.Count.func1`) in
    symbols, DWARF and traces.
23. **Richer type descriptors** (done; abi.md section 5): element types, lengths, field offsets, types and
    names, the `*T` descriptor for types with pointer methods, and Go's `reflect` type names, emitted for
    converted types and the types they are made of, read through `rt.Type`. Field names follow
    `--fieldnames` (off by default in kernel mode). Generic hashing in `hash` can now be built on them.
24. **Compile-time type-switch folding** (planned): `switch any(x).(type)` on a type parameter decided per
    instantiation, as language spec section 4.4 promises.
25. **Enums and sum types** (done): enums (language spec section 5.9,
    [design](plans/2026-10-09-enums-design.md)) and unions (section 5.10,
    [proposal](plans/2026-10-09-sum-types-proposal.md), option A): named variants, type switches with
    bare case names and exhaustiveness, assertions, comparison, generic unions such as `Result[T]`,
    packed unions for hardware formats, and `fmt` printing. Follow-ups: DWARF enumeration types and
    variant parts; dropping bounds checks when a dense enum indexes an array of length `len(E)`; union
    payloads that are owners or counted references, which wait for item 20 and are compile errors until
    then; updating a payload in place, `(&u).(*U.V)` (proposal section 3), when real use asks for it;
    a jump table for type switches over many variants; inferring a generic union's type arguments in
    `Result.Ok(5)`.
26. **`alloc.Use` signature** (done): the spec now gives the library's `Use(a) Restore` and
    `defer alloc.Use(a).Restore()` (language spec section 7.1); `Use(a) (restore func())` could not be
    written under section 4.5.
27. **Per-puroutine `alloc.Use`** (done): the override belongs to the puroutine that installs it, and a
    puroutine spawned under it starts with the program-wide default.
28. **`defer` in loops** (planned, possibly never): rejected today because it needs unbounded stack.
    In the body of a range-over-func loop a `defer` is accepted when every path after it leaves the
    loop, so it runs at most once (language spec section 4.10).
29. **`min`/`max` of strings and comparison of large arrays** (planned): rejected with "not supported
    yet".
48. **Counted references and `weak[P]`** (planned; language spec sections 5.6 and 6.5): the allocation
    header (allocator, strong and weak counts), increments on copy and decrements on drop with matched
    pairs removed at last use, `Drop` at zero strong count and the free at zero weak count, the atomic
    forms for `synced` with the ordering of memory-model.md section 3.7, `weak[P]` conversion and
    `Upgrade`, and the sendable rule of section 4.7 (no `shared` or `weak[shared]` across puroutines).
49. **Indexing interface** (done; language spec section 6.7): `x[k]`, `x[k] = v`, `v, ok := x[k]`,
    `x[k] op= v` and `len(x)` on types that are not built-in indexable call `Index`, `SetIndex`,
    `IndexOk` and `Len`; `vec.Vec` and `hash.Map` have them, and `pluto/container` declares the
    constraints for generic code. `&x[k]` and assigning to a part of `x[k]` are explained errors
    (containers return pointers from `Ptr`). `for k, v := range x` on such a type ranges over `x.All()`
    (item 22), so `vec.Vec`, `hash.Map` and `hash.Set` range like Go's slices and maps; `container.Iterable`
    is its constraint.
50. **Ownership proposals awaiting sign-off** (proposed):
    - `new` returns `owned[T]` and `make([]T, ...)` returns `vec.Vec[T]`, with `a.New[T]()` and
      `a.Slice[T](n)` for arena-owned views (language spec section 4.2);
    - the predeclared `take(&place)`, which moves out of a field, element or pointer and leaves the zero
      value (section 6.4);
    - owning interfaces (`type R owner interface {...}`) that move owners in, and ordinary interfaces that
      lend them (section 4.4);
    - counted allocated errors (section 14.6; item 11).
57. **Counted strings** (done; language spec section 6.10, abi.md section 1.1, memory-model.md section
    3.7): `string` is an immutable, reference-counted value of three words, `{ptr, len, ctl}`, whose
    control word points at the buffer's header (count, allocator, size) or holds only flags for a static
    or `unsafe.String` string, which is never counted. Runtime `+` and `+=` (one allocation for a chain),
    `string(b)`, `string(runes)`, `string(r)`, `[]byte(s)` and `[]rune(s)` work again; every string is
    valid UTF-8 (invalid literals are errors, conversions replace with U+FFFD, `s[i:j]` panics inside a
    rune, `strings.FromUTF8` is strict), and bit 0 of the control word marks strings that may hold
    non-ASCII bytes. Lowering counts with drop glue kept apart from the ownership work
    (`internal/lower/strcount.go`, `strscope.go`, `strops.go`, `strelide.go`): stores retain then
    release, parameters are borrowed and results owned, temporaries are released at the end of their
    statement, variables on every path out of their scope, frame slots when the function returns.
    `rt.ReleaseValues`, `RetainValues`, `MoveValues` and `HoldsStrings` give containers the drop glue;
    `vec.Vec`, `hash.Map`, `hash.Set`, `alloc.Free`, `flag.FlagSet` and the error types release their
    strings. `fmt.Sprintf`, `strconv.Itoa`, `strings.Join` and the rest return strings again, as do
    `os.Getwd`, `filepath.Join` and `time.Duration.String`, with `Append` forms kept. In kernel mode the
    allocating operations need `//pu:alloc` (E2544, E2545). Measured (2026-10-10, `bench`, Pu/Rust
    geomean): 1.03 before, 1.00 after, within the noise of a busy machine, since no benchmark builds
    strings in its loop. On a loop that passes struct fields to a search function, removing holds that
    no call can release (`opt.ElideStringHolds`) saves about 30%; atomic counts make a loop that stores
    a heap string five times slower than plain ones, which is why counts stay plain until
    `rt.SetThreaded`, which the runtime calls when the first puroutine starts (a puroutine may preempt
    another in the middle of a plain increment) and before it starts a thread. `go`
    statements give the new puroutine references to the strings in their arguments, a send hands the
    channel one, `alloc.FreeChan` releases the strings still buffered, and the bodies of range-over-func
    loops and of ranges over containers count like the function they are in; a ring's iterator holds
    the element it passes, which the body may pop (`testdata/lib/strconc.p` and `strcontainers.p`,
    leak-checked). The storage that
    a defer statement's arguments keep in such a body (interface boxes, literals, variadic arrays) is in
    the enclosing function's frame, where the deferred call runs. Remaining:
    - interface boxes hold their string until the function returns; a box used only as a call's argument
      could be a statement temporary;
    - `(string, error)` has five leaves and returns through memory; a sixth result register would keep
      it in registers;
    - elision through calls whose targets are known only as a set (interfaces, func values), and biased
      counting for strings confined to one puroutine once there are several;
    - Linux file names are bytes, not necessarily UTF-8: `os.ReadDir` still returns them as views made
      with `unsafe.String` (arguments and environment are checked and replaced). Whether such names
      become `[]byte`, a separate path type, or are refused needs a decision;
    - `tools/go2pu` and `selfhost/compat` can drop the helpers for what is native now (`compat.Concat`,
      `String`, `Bytes`, `RuneString`, `Join`, `Sprintf`, `selfhost/pu.Sprintf` and `internal/pu.Sprintf`
      returning a `Buf`), and translate `fmt.Sprintf` to `pluto/fmt`.

## Tooling

30. **`pluto fmt`** (in progress): the formatter, from the forked Go printer.
31. **`pluto test`** (in progress): test discovery and running, `testing`-style package.
32. **Differential fuzzing** (in progress): generate programs in the common subset of Go and Plutonium and
    compare their output.
33. **Language server** (planned): diagnostics, go to definition, hover and completion over
    `internal/types2`.
34. **Incremental builds and caching** (planned): per-package object caching keyed by source and flags;
    every build compiles the whole program today.
35. **`-race`-style tooling** (planned): a race detector for hosted builds, after puroutines exist.

## Platforms and formats

36. **arm64** (planned): `aarch64-linux` and `aarch64-none` targets, a second backend under
    `internal/arch`, with the memory-model instruction mapping of memory-model.md section 5.3.
37. **PE32+ for UEFI** (planned): `--format pe32+`, which needs a position-independent code mode without
    `R_X86_64_32S` (abi.md section 7). The flag is documented but rejected by the driver today.
38. **Position-independent code** (planned): for PE32+, PIE and KASLR outside the top 2 GiB.
39. **C header bindings generator** (planned): turn C headers into `//pu:extern` declarations and
    matching struct layouts.

## Standard library

40. **`fmt` `%v` for composite types** (done): arrays, slices, structs, pointers, interfaces, enums and
    `[]byte` print as Go prints them with `%v`, `%+v`, `%#v`, `Print` and `Println`, including Go's
    `%!verb(type=value)`; `hash.Map` prints as a map with sorted keys and `vec.Vec` as a slice, through
    `fmt.Mapper` and `fmt.Lister`. Checked against Go by `testdata/run/fmtvalues.p`. Not yet Go's: `%+q`
    and `%#q` quoting, width on quoted strings, and union values (item 25).
41. **Symbolised stack traces on panic** (done): the compiler's trace table, walked by the panic path
    and the hosted fault handler, usable in kernels (`rt.Traceback`); inlined calls are frames of their
    own. Files are named relative to the module root and the standard library, so builds are
    reproducible in any directory (`--trimpath=false` for absolute paths; debugging.md section 2.1). A
    per-thread alternate signal stack for puroutine worker threads remains.
42. **DWARF line tables and CFI** (done): compile units, subprograms, inlined calls, line table and
    `.debug_frame` for `gdb` and `perf` (`-g`, default on; `-s` strips). Variable and type information
    remains, and with it DWARF enumeration types (`DW_TAG_enumeration_type`) for enums: no type is
    emitted yet for anything to refer to.
43. **Debug allocator** (done): `pluto/alloc/debug` and `--alloc=debug` ([debug-allocator.md](spec/debug-allocator.md)):
    guard pages, canaries, poisoning and a quarantine for freed memory, a side table that catches double,
    wrong-size and invalid frees, and leak reports at exit, with allocation and free sites named by
    function and line. Heap faults go through the runtime's single fault handler, which prints the
    allocator's diagnosis and then the stack trace. Not yet safe for concurrent use (one lock, a no-op
    until puroutines run in parallel).
44. **`println` of floats matching Go** (done): `print` and `println` write the shortest form that reads
    back to the same value, in strconv's `g` format, as Go's runtime does (`1.5`, `1e+21`, `-0`), for
    float32 at float32 precision. The digits come from Go's unrounded scaling, with integer arithmetic
    and a fixed buffer. Kernel builds print the bits, as `pluto/fmt` does.
45. **`time`** (in progress): `pluto/time` has `Duration` (with `String`, `AppendString` and `ParseDuration`),
    `Time` in UTC with a monotonic reading, the calendar, `Now`, `Since`, `Until`, and over the
    scheduler's timers (items 14 and 53) `Sleep`, which parks the puroutine, `Timer`, `Ticker`,
    `After`, `Tick` and `AfterFunc` (started on a puroutine by one of the package's), with Go 1.23's
    channel semantics and freed with `alloc.Free`. Time zones and layout-based formatting remain.
46. **`net`** (planned, hosted): sockets over the epoll poller of item 14.
47. **`fmt.Sprintf` returns a string** (done, with item 57): `Sprintf`, `Sprint` and `Sprintln` return
    counted strings, built in a counted buffer of `pluto/fmt`'s own, so `fmt` still need not import
    `pluto/strings`; `Bprintf` keeps formatting into a caller's buffer. `selfhost/pu.Sprintf`, which
    returns a `strings.Builder` for translated code (item 52), can go (item 57).
51. **Owner types in the library** (planned; language spec sections 6.4 and 6.6): declare `vec.Vec`,
    `hash.Map`, `hash.Set`, `strings.Builder` and growable arenas `owner`, and turn their `Free` methods
    into `Drop`; make `os.File` an owner returned by value from `Open`, `Create` and `OpenFile` that
    closes on drop; add `Clone` to `Vec` and `Map`. (`strings.Buf`, an owning string buffer, is not
    needed now that strings are counted, item 57: `strings.Builder`'s `String` shares its buffer.) Done ahead of the keyword (item 52): `Drop` on `vec.Vec`,
    `hash.Map`, `hash.Set` and `strings.Builder` (beside `Free`), `Clone` on `Vec`, `Map` and `Set`,
    `Map.At`, zero `Vec`s and `Map`s that take the default allocator when they first grow, and the
    arenas' `New[T]()` and `Slice[T](n)` of section 4.2. `arena.Chunked` (item 56) already releases through `Drop`;
    declaring it `owner` is a one-line change to its type declaration, as it is for `os.DirEntries`,
    `exec.Cmd` and `flag.FlagSet` (item 56). Converting the OS layer: `Open`, `Create`, `OpenFile` and
    `Pipe` return `os.File` by value. (`Getwd`, `MkdirTemp`, `exec.LookPath` and the `filepath`
    functions return strings since item 57.)
52. **The `pu` mirror package for self-hosting** (done; migrating the compiler to it is not started): a
    Go package `pu` with `Map`, `Vec`, `Buf` and the other library owners, mirroring the Plutonium API,
    which the compiler's Go sources migrate to so that go2pu translates them one to one. Specified as an
    amendment in [the self-hosting plan](plans/2026-10-09-self-hosting-plan.md), section 11.
    `internal/pu` has `Vec`, `Map`, `Set`, their iterators, `Buf`, `Sprintf` and `New`, each with a
    `noCopy` marker so that go vet reports copies (Go's use after move). go2pu maps each name to
    `vec.Vec`, `hash.Map`, `hash.Set`, `hash.Iter`, `hash.SetIter` and `strings.Builder` (`strings.Buf` once
    it exists), `Sprintf` to `selfhost/pu.Sprintf` (which `fmt.Sprintf` can replace, item 57), and `New` to `selfhost/pu.New`, which allocates in the phase arena (the
    default allocator the driver installs): under the accepted `new` proposal `new(T)` returns
    `owned[T]`, which a `*T` field cannot hold, so arena allocation is written `pu.New` in Go.
    `TestPuMirrorsLibrary` checks every name and method against the library; the `pudemo` demo prints
    the same output from Go and from its translation at `-O0` and `-O2`. go2pu's translation of escaping
    `&T{...}` and of variables Go moves to the heap emits `pu.New` too, instead of `new`.
56. **Self-hosting prerequisites** (in progress): the compiler and library work of phases 0 and 1 of
    [the self-hosting plan](plans/2026-10-09-self-hosting-plan.md) (section 8).
    - Single-pointer structs in an interface's data word (done): a struct with one field, or an array of
      length 1, that is a single pointer is direct, as in Go (language spec section 4.4,
      `types2.PointerShaped`), so an error wrapping one pointer can be returned. Tested by
      `testdata/run/directstruct.p` and unit tests in `types2` and `lower`.
    - Itab lookup (done): `rt.findItab`, behind conversions between non-empty interfaces and assertions
      to interface types, probes a hash table the compiler lays out (abi.md section 5) instead of
      scanning every itab. Tested by `testdata/run/itablookup.p` and `lower.TestItabTable`.
    - Large stack for the compiler (done): `//pu:stack 256M` on `func main` makes the hosted runtime
      reserve a lazily mapped stack with a 1 MiB guard region and run the program on it (language spec
      section 9); `rt.StackLeft`, `rt.StackBounds` and `rt.SetStackBounds` report what remains. The type
      checker stops at `Config.MaxNesting` levels of nested expressions and types (default 100,000, the
      parser's limit) and calls `Config.StackCheck`, the hook the self-hosted compiler points at
      `rt.StackLeft`, every 16 levels. Tested by `testdata/lib/mainstack.p`, `build.TestMainStack*` and
      `types2.TestNestingLimit`.
    - Growable arenas (done): `arena.Chunked` takes chunks from a parent allocator, keeps standard chunks
      for reuse across `Reset` and `Release(Mark())`, returns oversized ones at once, frees everything
      on `Drop` (spares alone on `Trim`), can poison released memory, and is an `alloc.Allocator`. It is
      an owner by convention until the `owner` keyword lands (item 51). Tested by
      `testdata/lib/chunked.p` and `build.TestChunkedMisuse`.
    - OS and process layer (done, Linux): `pluto/os` gains `Stat`/`Lstat`/`File.Stat` (a `FileInfo`
      struct with Go's method names), `ReadFile` (a `vec.Vec[byte]`), `WriteFile`, `ReadDir` (a
      `DirEntries` owner, sorted), `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod`,
      `Getwd` and `MkdirTemp` (strings, with `Append` forms), `Chdir`, `TempDir`, `Pipe`, `LookupEnv`,
      `Environ`, `Args`, `Process` and `ProcessState`; files open with `O_CLOEXEC`. `pluto/os/exec`
      runs commands with fork and execve, connecting each stream to `/dev/null`, a file, or a pipe
      that `Wait` copies with `poll`; `ExitError` is a word. `pluto/path/filepath`, `pluto/time`
      (item 45), `pluto/flag` and `io.ReadAll` complete it. Errors are `syscall.Errno` and
      `*os.PathError`. Tested by `testdata/lib/osfs.p`, `exec.p` and `time.p` and the `filepath` and
      `flag` package tests. What is left for item 51: `os.File` returned by value as an owner (today
      `*File` from an allocator, freed with `alloc.Free`). Text results are strings since item 57. When
      allocated errors become counted (section 14.6,
      accepted), the `errors.Free` calls in `os.ReadDir` and `filepath.WalkDir` go away.
    - Constant package (done): `internal/constant`, written in Go for go2pu to translate, has
      go/constant's API with a value-typed `Value` (zero value Unknown) and its own arbitrary-precision
      integers, exact fractions and 512-bit floats with math/big's rounding, so its results are
      go/constant's bit for bit; complex constants are dropped. `TestDifferential` compares 10,000
      random constant expressions against go/constant, and the literal tables of go/constant's tests
      are checked the same way. `pluto/bits.Div64` was added for its translation, which go2pu
      produces with three notes left (`strconv.Quote`, `Unquote` and `UnquoteChar`, the strconv
      quoting work of phase 1). Switching `types2` and `lower` from go/constant is phase 7.
    - Phase 1 libraries, second part (done): `pluto/unicode` (Go 1.27's Unicode 17.0.0 tables,
      generated by `tools/mkunicode`; ranges are arrays of constants because the compiler writes
      those into the data section but builds a slice literal at package level with code at start-up,
      which would cost about 14 bytes of code per range), `pluto/cmp`, `pluto/slices` (Go's code with
      the sorts and iterators; `Insert` and `Replace` within capacity; `Clone`, `Concat`, `Repeat`,
      `Collect` and `Sorted*` return a `vec.Vec`), `pluto/container/heap` (Go's
      functions over a `heap.Interface`, and a generic `Heap[T]` in a `vec.Vec`) and
      `strconv.ParseFloat` (Go 1.27's algorithm: exact one-operation cases, fast unrounded scaling
      with the power-of-ten table of `pluto/rt`, the multiprecision slow path, hexadecimal floats and
      underscores), with `NewNumError` for Go's `*NumError`. `internal/build/strconv_diff_test.go`
      compares `ParseFloat` and `AppendFloat` with Go's over a million random and hard inputs, and
      the golden test maps `pluto/unicode`, `slices`, `cmp` and `container/heap` to Go's packages so
      that `testdata/run` programs are checked by running them with Go.
