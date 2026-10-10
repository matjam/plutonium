# Plutonium roadmap

**Updated:** 2026-10-09

Every known gap between the specification ([language.md](spec/language.md),
[memory-model.md](spec/memory-model.md), [abi.md](spec/abi.md)) and the compiler and standard library,
in priority order. Statuses:

- **done**: implemented and tested on `main`;
- **in progress**: being worked on now (design or implementation);
- **planned**: specified, not started;
- **proposed**: written up, awaiting the user's decision before it becomes part of the language.

The order puts wrong code first (the compiler must never produce it, AGENTS.md rule 4), then what a
kernel needs, then the concurrency features the spec already promises, then the rest of the language,
tooling, platforms and libraries.

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
5. **Escape errors for frame-boxed interfaces and capturing closures** (planned): returning, or storing in
   a package variable or through a pointer, an interface holding a frame copy or a closure with captures
   must be a compile error (language spec sections 4.4 and 4.5); today nothing checks it.

## Kernel enablement

6. **Memory model** (done, this document set): [memory-model.md](spec/memory-model.md), Go's model with
   packed locations, volatile, interrupt and scheduler rules.
7. **Kernel mode** (done): `--mode kernel` sets the `kernel` tag and links with a default top-2-GiB
   linker script; floating point is a compile error outside `//pu:fpu` functions, enforced through
   `ssa.Func.NoSSE` in lowering, the inliner and the backend (language spec section 11.2, abi.md section
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
    escape checks of item 5 remain.
12. **`//pu:extern` C struct classification** (done): System V eightbyte classification for aggregate
    parameters and results, hidden result pointers, several results as a struct, export wrappers and
    func-value thunks (abi.md section 9.1); `.c` and `.o` files in packages are linked (section 9.2).
    Tested against GCC and Clang. Strings, slices, interfaces, func values and C varargs are rejected.
13. **Debugging** (done): DWARF line tables and CFI, symbolised stack traces on panic and on faults,
    and the debug allocator, which reports through the same fault path and names its sites from the
    same table; specified in [debugging.md](spec/debugging.md) and
    [debug-allocator.md](spec/debug-allocator.md). Implementation items 41 to 43; what remains is listed
    there and in debugging.md section 8.

## Concurrency

14. **Puroutines and the scheduler** (planned): `go`, `rt.Go`, fixed stacks with guard pages, the
    four-operation scheduler interface, the hosted work-stealing scheduler with signal preemption, and
    `rt.SetScheduler` for kernels. The type checker rejects `go` today.
15. **Channels and `select`** (planned): `chan T`, send, receive, `close`, `range`, `select`, reference
    counted channel objects. The type checker rejects channel types today.
16. **`pluto/sync`** (planned): `Mutex`, `RWMutex`, `WaitGroup`, `Once` over the scheduler, with the
    guarantees of memory-model.md section 3.4. Only `pluto/sync/atomic` exists.
17. **Thread-safe default allocator** (planned): per-worker caches in front of shared slabs, and `Free`
    synchronised before reuse (memory-model.md section 3.8). Today's `alloc.Slab` is single-threaded.
18. **Stack size analysis and `-stackreport`** (planned): compute the stack a spawned function needs when
    its call graph has no recursion or indirect calls.

## Language

19. **Methods with type parameters** (done): merged on 2026-10-09; method values must be instantiated.
20. **owned, shared, synced and the move checker** (planned): the types exist in the checker; moves,
    use-after-move errors, `Drop` calls, reference counting and the allocator header word do not.
21. **Default parameter values** (done): language spec section 5.1, with the open questions settled
    there (variadic functions, function values, interface methods, generics, `f(g())`). The library's
    `F`/`FIn` pairs are single functions with a trailing allocator defaulting to `alloc.Default()`;
    `new` and `make` take an optional allocator (section 4.2). `make(chan T, n, a)` parses the allocator
    the same way and waits for item 15.
22. **Range over func and iterators** (planned): `for x := range f` is rejected with "range over
    functions is not supported yet".
23. **Richer type descriptors** (planned): element types, lengths, field offsets, types and names, so `fmt`
    can print composite values and `hash` can hash them generically.
24. **Compile-time type-switch folding** (planned): `switch any(x).(type)` on a type parameter decided per
    instantiation, as language spec section 4.4 promises.
25. **Enums and sum types**: enums are done (language spec section 5.9,
    [design](plans/2026-10-09-enums-design.md)); tagged unions are proposed
    ([proposal](plans/2026-10-09-sum-types-proposal.md), option A accepted) and will reuse the enum's
    member selection, bare case names and exhaustiveness check. Follow-ups: DWARF enumeration types,
    and dropping bounds checks when a dense enum indexes an array of length `len(E)`.
26. **`alloc.Use` signature** (planned): the spec's `Use(a) (restore func())` cannot be written under
    section 4.5 (the closure captures the previous allocator and is returned); align the spec with the
    library's `Use(a) Restore` and `defer alloc.Use(a).Restore()`.
27. **Per-puroutine `alloc.Use`** (planned): the override belongs to the puroutine that installs it; today
    it replaces the program-wide default.
28. **`defer` in loops** (planned, possibly never): rejected today because it needs unbounded stack.
29. **`min`/`max` of strings and comparison of large arrays** (planned): rejected with "not supported
    yet".

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

40. **`fmt` `%v` for composite types** (planned): structs, arrays, slices and pointers to them print as
    `{TypeName}` today; needs item 23.
41. **Symbolised stack traces on panic** (done): the compiler's trace table, walked by the panic path
    and the hosted fault handler, usable in kernels (`rt.Traceback`); inlined calls are frames of their
    own. A per-thread alternate signal stack for puroutine worker threads remains.
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
44. **`println` of floats matching Go** (planned): `rt.printFloat` prints six fixed decimals; Go prints
    `+1.500000e+000` style.
45. **`time`** (planned): monotonic and wall clocks, durations, timers over the scheduler.
46. **`net`** (planned, hosted): sockets over the epoll poller of item 14.
47. **`fmt.Sprintf` naming** (planned): the spec lists `Sprintf` into a caller's buffer; the library calls
    it `Bprintf`. Pick one name and update the other.
