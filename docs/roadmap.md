# Plutonium roadmap

**Updated:** 2026-10-10

Every known gap between the specification and the compiler and standard library, in priority order. The
specification ([language.md](spec/language.md), version 0.3, with the binary interface in
[abi.md](spec/abi.md), the [memory model](spec/memory-model.md), [debugging.md](spec/debugging.md) and the
[debug allocator](spec/debug-allocator.md)) is the single source of truth; this document tracks status and
order only. Each item names the section of the specification that states the rule it implements. Where code
and specification differ, the code is fixed; a change to the specification is a user decision recorded
before the code moves (AGENTS.md).

Statuses:

- **done**: implemented and tested on `main`, with the commit;
- **in progress**: being worked on now;
- **planned**: specified, not started;
- **decided**: the specification settles it and nothing is left to build.

The order puts wrong code first (the compiler must never produce it, AGENTS.md rule 4), then the gaps the
integrated specification defines (complete-spec section 6.5, the implementation brief), then what a kernel
needs, then concurrency, the rest of the language, tooling, platforms and libraries. Item numbers are stable
because other documents cite them; an item added later takes the next free number and is listed in the
section where it belongs. Words: the specification calls a unit of concurrent execution a *task*; what the
compiler and the runtime print still says "puroutine" in places until item 62 is finished.

## Gaps of the integrated specification

The 24 implementation gaps of
[complete-spec section 6.5](plans/2026-10-10-complete-spec.md), and the item that tracks each:

| Gap | Subject | Item | Section |
|---|---|---|---|
| 1 | Order of evaluation | 73 | 11.26 |
| 2 | Pointer-receiver method on an owner temporary | 78 | 11.14, 12.4 |
| 3 | Composite literal keys that name promoted fields | 79 | 11.3 |
| 4 | Package initialization order (done) | 80 | 15.3 |
| 5 | `len` of a `ring[T, N]` | 81 | 13.6, 7.17 |
| 6 | `defer` of built-ins that may not be deferred | 82 | 12.19 |
| 7 | Exhaustive enum switch is a terminating statement | 75 | 12.1, 12.9, 7.12 |
| 8 | `append` and the others are not predeclared | 83 | 10.3, 13.1 |
| 9 | Every nil dereference panics | 84 | 17.5 |
| 10 | No untagged heap or arena memory in checked builds | 85 | 18.11 |
| 11 | Sendability looks through pointers and slices | 86 | 8.8, 19.2 |
| 12 | An owner's sendability is decided by what it holds | 87 | 8.8 |
| 13 | A channel or map in an ordinary interface is lent | 88 | 7.9 |
| 14 | Operators other than comparison do not apply to an owner | 89 | 11.18, 7.18 |
| 15 | A `//go:` line comment is an error | 75 | 4.1, 20.3 |
| 16 | `portable` (done) | 70 | 8.9 |
| 17 | Task stacks from the program-wide default allocator | 90 | 19.1 |
| 18 | Stack overflow message and stack probes | 91 | 19.1, 17.5 |
| 19 | `NewTimer`, `NewTicker`, `AfterFunc` return owners | 67 | 19.6, F.7 |
| 20 | The `task` statement and the rename sweep | 62 | 12.12, 4.5, 4.6, 17.1 |
| 21 | The built-in `map[K]V` (done) | 60 | 7.10, 12.10, 13.5, 13.7 |
| 22 | Counted `error` values and library error types | 11, 69 | 16.7, 16.5 |
| 23 | Package initializer that allocates, in a freestanding build | 75 | 15.2 |
| 24 | Embedding `shared[T]` and `synced[T]` | 92 | 7.6, 7.19 |

The pass that aligned the documents with the chapters found six more, items 93 to 98; they are rows 25 to
30 of complete-spec section 6.5.

## Wrong code and silent failures

1. **Volatile loads order plain loads** (done, `a74ffc3`; section 7.15, memory-model.md section 5.2): each
   volatile load starts a new memory state, so plain loads on either side of a polling loop stay separate.
2. **Interface boxes in package initializers** (done, `f73a3e0`; sections 7.9 and 18.2): a box made by a
   package-level initializer lives in static storage.
3. **`//pu:interrupt` handlers** (done, `a6788e5`; section 20.10, abi.md section 10.4): handlers get the
   entry and exit sequence, not an ordinary function's.
4. **`//pu:extern` and `//pu:export` with aggregates** (done, `46aef18`; section 20.4, abi.md section 9.1):
   System V classification; signatures with no C equivalent are compile errors.
5. **Escape errors for frame views** (done, `70f2401`, `052c091`, `2a27f12`, `af28ec6`; sections 18.3, 18.4
   and 11.5): returning, storing or sending a view of a frame is a compile error (E3001 to E3008, closures
   in regions E3100 to E3102), by a whole-program analysis (`internal/escape`) with explanatory messages.
   The run-time half is item 71.

## Gaps the integrated specification defines (complete-spec section 6.5)

73. **Order of evaluation** (planned, gap 1; section 11.26): operands other than calls, method calls and
    receives are read after the calls, so `[]int{a, f()}` with an `f` that changes `a` gives `[2 2]`.
    Lowering gives `[1 2]` (verified). Survey `internal/lower`; add `testdata/run` and difftest cases
    (composite literals, assignments, `x[i] = f()`, operands of operators); every difference from the
    specified order is a false friend and is fixed.
78. **A pointer-receiver method on an owner temporary** (planned, gap 2; sections 11.14 and 12.4):
    `readAll(f).Slice()`, with views of the temporary limited to the statement.
79. **Composite literal keys that name promoted fields** (planned, gap 3; section 11.3): the compiler
    crashes; it must accept or report an error.
80. **Package initialization order** (done, gap 4, `4aa24f2`; section 15.3): `pluto/rt` and `pluto/alloc`
    first, then the remaining packages by import path, each after its imports.
81. **`len` of a `ring[T, N]`** (planned, gap 5; sections 13.6 and 7.17): verified to be rejected today.
82. **`defer` of built-ins that may not be deferred** (planned, gap 6; section 12.19, chapter 13): E2532,
    with the fix to wrap the call in a function literal.
83. **`append`, `recover`, `complex`, `real`, `imag`, `complex64` and `complex128` are not predeclared**
    (planned, gap 8; sections 10.3 and 13.1): a program may declare them, and a use without a declaration
    is the error that names the replacement. `append` stays predeclared today so that E2013 can name it.
84. **Every nil dereference panics** (planned, gap 9; section 17.5): an explicit check where the offset of
    an access from the pointer can exceed the unmapped region below address zero.
85. **No untagged heap or arena memory in checked builds** (planned, gap 10; section 18.11, abi.md section
    1.3): slab allocations over 2 KiB, arena chunks over 56 KiB and the buffer of a fixed `arena.Arena` get
    generations or epochs.
86. **Sendability looks through pointers and slices** (planned, gap 11; sections 8.8 and 19.2): a `*S`
    whose `S` holds a `shared[T]` is not sendable.
87. **An owner's sendability is decided by what it holds** (planned, gap 12; section 8.8): a view field that
    holds only memory the owner allocated moves with it; any other view field is checked where the owner
    crosses. Today owners of packages that import `unsafe` are exempt (`internal/race`).
88. **A channel or map in an ordinary interface is lent** (planned, gap 13; section 7.9): as a `shared[T]`
    is, so `func f() any { return make(chan int) }` is E3001 (verified: it compiles today).
89. **Operators other than comparison do not apply to an owner** (planned, gap 14; sections 11.18 and
    7.18): `*f + 1` for `type Fd owner int32` is an error (verified: accepted today).
90. **Task stacks from the program-wide default allocator** (planned, gap 17; section 19.1, abi.md section
    12.1): never from the starting task's `alloc.Use` override (`rt.spawnGo` takes the override today).
91. **Stack overflow message and probes** (planned, gap 18; sections 19.1 and 17.5): the panic names the
    size of the stack and how to ask for more (`rt.Go(f, size)`), and a function whose frame is larger
    than the guard region probes its stack so that it cannot step over the guard.
92. **Embedding `shared[T]` and `synced[T]`** (planned, gap 24; sections 7.6 and 7.19): embedding promotes
    `T`'s methods, as `owned[T]` does. To verify.
94. **Any two functions exported under one symbol are E2512** (planned; section 20.4): `internal/lower`
    reports it only for wrapped exports.
95. **Channel reference counts are always atomic** (planned; sections 18.7 and 19.8): `lib/pluto/rt/chan.p`
    switches from plain to atomic counts like a `synced[T]`.
96. **The debug allocator reports package-variable memory apart from leaks** (planned; sections 15.4 and
    18.6, debug-allocator.md).
97. **`pluto vet -unsafe`** (planned; sections 18.12 and 21.1): lists, for each package, the functions that
    use unsafe code and the safe functions that call them. The compiler has no `vet` command yet.
98. **Hosted task stacks under `RLIMIT_AS`** (planned, needs a decision; sections 19.1 and 21.4): the
    runtime shrinks stacks below 64 MiB when the address-space limit is small, and the specification says
    "at least 64 MiB" without a rule for the case where that is impossible.

## Kernel enablement

6. **Memory model** (done, `9723c79`; section 19.7, [memory-model.md](spec/memory-model.md)): packed
   locations, volatile, interrupt and scheduler rules.
7. **Kernel mode** (done, `a9b0b60`, `ac6e21c`; sections 20.5 to 20.7, abi.md section 10): `--mode kernel`
   sets the `kernel` tag and links with the default top-2-GiB script; floating point is an error outside
   `//pu:fpu` functions. Tested by booting `testdata/kernel/smoke` under QEMU.
8. **`pluto/arch/x86` intrinsics** (done, `ed974cb`; section 20.8, abi.md section 10.5): expanded inline
   as compiler barriers; encodings checked against GNU as.
9. **`//pu:interrupt`** (done, `a6788e5`, `eebd382`; section 20.10, abi.md section 10.4): register saves,
   stack alignment, the error-code variant and `iretq`, tested under QEMU (`testdata/kernel/irq`).
10. **Per-CPU variables, `//pu:percpu`** (done, `0c1fad1`; section 20.9, abi.md section 10.6): a
    `.percpu` section accessed through `GS`.
11. **Errors** (in progress; chapter 16): the conventions of sections 16.1 to 16.6 and `errors.New` as a
    static error per call site (section 16.3) are done (`72841e9`, `3f18f71`). Open: allocated errors are
    counted objects that `error` values count, so each is released exactly once and no `errors.Free`
    exists (section 16.7, gap 22): `fmt.Errorf` allocates its message as a counted string, and the library
    error types are counted (item 69). The escape checks of item 5 cover the rest.
12. **`//pu:extern` C struct classification** (done, `46aef18`; section 20.4, abi.md sections 9.1 and 9.2):
    eightbyte classification, hidden result pointers, export wrappers and func-value thunks; `.c` and `.o`
    files in packages are linked. Tested against GCC and Clang.
13. **Debugging** (done, `002e065`, `48d130a`, `8d57684`, `170d539`, `697ad2b`; sections 17.1, 17.5,
    21.7; [debugging.md](spec/debugging.md), [debug-allocator.md](spec/debug-allocator.md)): DWARF line
    tables and CFI, symbolized traces on panics and faults, reproducible paths, and the debug allocator.
    Open: DWARF variable and type information (item 42).
54. **Compile-time panic handler, `//pu:panic`** (done, `1fd3e85`; section 17.4): one handler per
    freestanding or kernel program, linked as a direct call from the runtime and checked never to return.
55. **Interrupt context cannot wait** (done, `1fd3e85`, `662497a`; section 20.10): a whole-program check
    from every interrupt handler and the panic handler against blocking operations, conservative through
    interfaces and func values.

## Concurrency

14. **Tasks and the hosted scheduler** (done, `0ebe75d`, `1976415`, `d40ae60`, `ec9d6dc`, `add8fb6`;
    sections 19.1, 19.5 and 12.12, abi.md section 12): the task mechanism, fixed stacks with guard pages,
    a work-stealing scheduler with preemption and system-call hand-off, an epoll poller, timers, and
    deadlock detection that lists every task. The sendable rules are checked (section 19.2). Follow-ups: a
    program that binds its own hosted backend runs on one thread; spawning costs about twice a mature
    scheduler's.
15. **Channels and `select`** (done, `0ebe75d`; sections 7.11, 12.13 and 19.8): counted references
    (item 67), `make(chan T, n, a)`.
16. **`pluto/sync`** (done, `8fd524c`; section F.5, memory-model.md section 3.4): `Mutex`, `RWMutex`,
    `WaitGroup`, `Once`, `Cond` over the scheduler.
17. **Thread-safe default allocator** (done, `0ebe75d`; sections 18.9 and 19.7): `alloc.SyncSlab` with
    per-processor caches is the hosted default.
18. **Stack size analysis and `-stackreport`** (planned; section 19.1 item 1): compute the stack a task
    needs when its call graph is bounded. Stack requests carry a `Proven` bound, always 0 today. A sound
    bound covers what lands on a task's stack outside the call graph (the preemption frame, the fault
    path, a kernel's interrupt frames, the panic path).
53. **Scheduler contract for kernels** (done, `7d4cf29`, `d81508d`, `5001093`; section 19.5, abi.md
    section 12, [plan](plans/2026-10-09-scheduler-contract.md)): permit-based park and ready,
    interrupt-safe wake, backend task records, a current-task slot, `rt.Preempt`, link-time binding with
    `//pu:scheduler`, optional timers (E2595). A test kernel with timer preemption runs under QEMU.
63. **`sync.Scope`** (done, `a7948f6`, `f23c29c`, `8444dae`; sections 19.4 and 15.4, abi.md section 12.10,
    [plan](plans/2026-10-10-scoped-puroutines.md)): an owner whose drop waits, with `Task`, `Wait` and
    `Drop`; tasks started on a local `Scope` use the starting function's variables, checked for races
    (`internal/scoped`, `internal/race`). Follow-ups (phase 2 of the plan): a Scope reached through a
    pointer, helpers that start tasks for their caller's Scope, an error-collecting scope; `Index` and
    `SetIndex` of library containers are not partitioned between tasks.
66. **Large task stacks** (done except the overflow message, `173c2f6`; section 19.1, abi.md sections 12.1
    and 12.7): 64 MiB lazily mapped hosted stacks, cached. Open: items 91 and 98.
67. **Counted channels, and a channel owns its timer** (done except the timer owners, `bdb7683`; sections
    7.11, 19.6, 19.8, abi.md section 12.6): a channel is a counted reference freed with its last reference.
    Open (gap 19): `time.NewTimer`, `NewTicker` and `AfterFunc` return `*Timer` and `*Ticker` freed with
    `alloc.Free`; the specification has them return `owned[Timer]` and `owned[Ticker]`, which stop and free
    the timer when dropped (section F.7).

## Language

19. **Methods with type parameters** (done, `4f98246`; section 10.15): method values must be instantiated.
20. **Ownership: owners, moves and views** (in progress; chapter 18, sections 7.18, 7.19 and 8.7,
    [design](plans/2026-10-09-memory-safety-design.md)): done are `owner` types, the move checker
    (`internal/moves`, E3301 to E3312), automatic drops on every path, `take`, `unsafe.Drop`, `owned[T]`,
    `shared`, `synced`, `weak`, owning interfaces, owner unions, the local-arena rule (section 18.9), and
    generation checks and arena epochs (items 71 and 72). Commits: `ca7e064`, `dc808a9`, `01daa4b`,
    `1b25e7d`, `052c091`. Open: items 78, 85 to 89 and 92, and the library owners of item 51.
24. **Compile-time type-switch folding** (planned; an optimization, sections 12.9 and 21.3): decide
    `switch any(x).(type)` on a type parameter per instantiation; acceptance never depends on it.
28. **`defer` in loops** (decided; sections 12.19 and 12.11): a `defer` in a loop is E2530; a range-over-func
    body allows one only when every path after it leaves the loop. Nothing is left to build.
29. **`min` and `max` of strings, comparison of large arrays** (done, `82642d4`, `65addf9`; sections 13.8
    and 11.20).
58. **Deferred calls on a panic, and `rt.Goexit`** (done, `71d0441`; sections 17.2 and 17.3): a hosted panic
    runs the task's deferred calls before the handler; `rt.Goexit` ends a task through its frames' return
    paths; `testing`'s `FailNow` uses it. Freestanding and kernel builds are unchanged.
59. **Data race freedom** (done, `6e74756`, `23e57de`; section 19.3, memory-model.md section 4.1, error
    codes E3401 to E3407 and E3501 to E3503): `internal/race` infers which lock guards which data and
    rejects racing package variables and views that cross tasks; maps have a rule of their own (item 60).
48. **Counted references and `weak[P]`** (done, `1b25e7d`; sections 7.19 and 18.7, abi.md section 1.2):
    allocation header, borrowed parameters and owned results, atomic `synced` counts once the program
    starts a task, `weak[P]` upgrade. Leak-checked under the debug allocator.
49. **Indexing interface** (done, `217cfd0`, `8cc7c27`; sections 11.11 and 12.10): `Index`, `SetIndex`,
    `IndexOk` and `Len` for library containers, and range over a container through `All`.
50. **Ownership surface** (done, `1b25e7d`, `01daa4b`; sections 13.9, 13.10 and 7.9): `new` returns
    `owned[T]`, `take(&place)`, owning interfaces. Counted allocated errors are item 11.
57. **Counted strings** (done, `a0e0987`, `70a2ebf`, `a433e5e`; sections 7.3 and 18.8, abi.md section 1.1,
    memory-model.md section 3.7): an immutable, valid-UTF-8, reference-counted value with elided counts
    (`opt.ElideStringHolds`). Open: interface boxes hold their string until the function returns;
    `(string, error)` returns through memory; Linux file names that are not UTF-8 need a decision (bytes, a
    path type, or refusal); `tools/go2pu` and `selfhost/compat` can drop helpers for what is native.
60. **Counted `map[K]V`** (done, gap 21, `7867af2`, `8fd4ab7`, `0f8bc1b`, `c491a67`, `3c094e3`; sections 7.10,
    12.10, 13.3, 13.5, 13.7 and 19.3, abi.md section 1.4): one word, a counted reference to a
    `pluto/rt.MapTable`; `make(map[K]V, n, a)` takes an allocator and a literal allocates from the current
    allocator; owner keys and elements are E3213; a map is sendable, and the contents of the maps of a type
    that crosses to another task fall under the guard rule of item 59; go2pu translates Go's maps as they
    are. Left: the owner error suggests counted references, since `hash.Map` does not take owner values yet
    (`Remove` and `Pop` for owners); the race rule groups maps by type, so two maps of one type are guarded
    together when either crosses.
61. **Removed conversions** (done, `080e49b`, `d611757`, `271e57f`; sections 11.24 and 18.8): `string(b)`
    is E2015, `[]byte(s)` and `[]rune(s)` are E2016, `string(i)` of a non-rune integer is E2017; the
    messages show the replacement (`strings.FromUTF8`, `strings.Bytes`, `strconv.Itoa`).
62. **The `task` statement and the rename sweep** (in progress, gap 20; sections 12.12, 4.5, 4.6, 17.1,
    19.4): `task f(x)` starts a task; `task` is a contextual keyword that begins a statement when the next
    token is an identifier or `func`; `go f()` is E1012 and names `task`. Done: the statement in the parser,
    checker, formatter, language server and go2pu (`4cb685c`), programs, tests and catalog examples
    converted (`3967e27`), `(*sync.WaitGroup).Task` and `rt.Panic.Task` (`523a21f`). Left: the word "task"
    in what the program prints ("task N" in traces and "all tasks are asleep" and the `main called
    rt.Goexit` fatal error in deadlock reports, still "puroutine" in `lib/pluto/rt`), thunk names
    (`<function>.go<N>`), the texts of E-codes and the diagnostics goldens that show them.
64. **Map iteration guarantees and per-map seeds** (done, `7867af2`; sections 12.10 and 7.10): a deletion
    never moves another entry, every entry present when a loop starts and not deleted before it is reached
    is visited exactly once, and a table rebuilt during a walk is kept until the last walk ends. Hosted maps
    are seeded from `AT_RANDOM`; kernels call `rt.SetMapSeed`; `hash.Map` keeps its fixed seed unless given
    one with `InitSeed`. Applies to `hash.Map`, `hash.Set` and the built-in map.
70. **`portable`** (done, gap 16, `3146e99`; section 8.9, E2018,
    [scheduler contract](plans/2026-10-09-scheduler-contract.md) section 9): a predeclared structural
    constraint; no owner is portable. Needed by the PlutoniumOS IPC design. The transfer of handles and
    page-owning owners between address spaces waits for the PlutoniumOS IPC design.
71. **Generation checks by default for hosted builds** (done, `766837b`, `b7a42d8`; sections 18.11 and
    21.5, abi.md section 1.3): a use of freed heap memory is a run-time panic; `--gencheck=false` is the
    unchecked mode; kernel and freestanding builds stay unchecked until a kernel tag encoding exists. Cost
    1.02x over `bench/`. Open: item 85.
72. **Arena epochs** (done, `3410025`; section 18.11, abi.md section 1.3): `arena.Chunked` carries a
    per-arena epoch; `alloc.SyncSlab` tags its allocations.
75. **Language checks the integration left open** (planned, gaps 7, 15 and 23; each is checked against the
    compiler when the work starts, and those already enforced are struck): a `//go:` line comment is an
    error that names the `//pu:` equivalent (sections 4.1 and 20.3); `iota` in an enum is an error
    (7.12, 10.7); a package initializer that allocates in a freestanding build is an error (15.2); an
    expression that moves a variable and uses it elsewhere is an error whatever the order (11.26, 18.5); a
    consuming method used as a method value is an error (11.9); an exhaustive enum switch is a terminating
    statement and panics for a value that is not a member, as a union switch does (12.1, 12.9, 7.12);
    `clear` on a slice of owners drops each element (13.3); the directive groups of section 20.3
    (`section`, `noreturn`, `noescape`, `nosimd`, `nochecks`, with `nobounds` retired into `nochecks`, E2601)
    with AGENTS.md and `internal/lsp/hover.go` changing together.
77. **No `append`** (done, `38f6777`, `fa4bdcc`, `57e4f05`; sections 13.1, 7.5 and 18.10,
    [plan](plans/2026-10-10-remove-append.md)): every call is E2013 with the fix for its idiom; `vec.Vec`
    grows, `vec.Fixed` is bounded; `fmt.Appendf` replaces `Bprintf`; `make([]T, ...)` is E2014 (fix
    `vec.Make`). Item 83 removes the name from the predeclared scope.

## Tooling

30. **`pluto fmt`** (done, `9015b55`): the formatter, from the forked printer; `TestRepoFormatted` keeps the
    repository clean.
31. **`pluto test`** (done, `9015b55`): `Test` and `Benchmark` functions in `*_test.p` files, in `go test`'s
    output format.
32. **Differential testing** (in progress, `89b29d9`, `c9390db`; section 1.2 rule 4): generated programs in
    the common subset of Go and Plutonium run under both compilers and compared.
33. **Language server** (done, `c408034`): `pluto lsp` and the VS Code extension: diagnostics with codes,
    hover with the kind of each type (section 18.1), definition, references, completion, rename.
34. **Incremental builds and caching** (planned): per-package object caching keyed by source and flags;
    every build compiles the whole program today.
76. **The complete specification** (done, `131ac9e` to `b05a0b6`, `68b7efb`;
    [plan](plans/2026-10-10-complete-spec.md)): 21 chapters and 7 appendices in `docs/spec/language/`,
    guarded by `TestSpecReferences`; the companion documents, plans and README agree with it. Open
    questions stay in the plan's section 6.5, which is the implementation brief.
93. **`pluto doc`** (planned; sections 1.1, 18.1 and F): shows a package's documentation and the kind of
    each type (`value`, `view`, `owner (moves)`, `counted`). The specification points to it for everything
    the standard library defines; the compiler has no `doc` command yet.

## Platforms and formats

36. **arm64** (planned; section 20.5): `aarch64-linux` and `aarch64-none` targets, a second backend under
    `internal/arch`, with the instruction mapping of memory-model.md section 5.3.
37. **PE32+ for UEFI** (planned; section 20.5, abi.md section 7): `--format pe32+`, which needs a
    position-independent code mode without `R_X86_64_32S`. The flag is documented and rejected today.
38. **Position-independent code** (planned; abi.md section 7): for PE32+, PIE and KASLR outside the top
    2 GiB.
39. **C header bindings generator** (planned; section 20.4): turn C headers into `//pu:extern`
    declarations and matching struct layouts.

## Standard library

40. **`fmt` `%v` for composite types** (done, `b5ed7be`, `b359bbe`): arrays, slices, structs, pointers,
    interfaces, enums, unions and `[]byte`, with the usual `%!verb(type=value)` forms; checked by
    `testdata/run/fmtvalues.p`.
41. **Symbolized stack traces on panic** (done, `48d130a`; sections 17.1 and 21.7,
    [debugging.md](spec/debugging.md)): the trace table, walked by the panic path and the fault handler;
    inlined calls are frames. Open: a per-thread alternate signal stack for worker threads.
42. **DWARF line tables and CFI** (done, `002e065`, `8d57684`; debugging.md): for `gdb` and `perf`. Open:
    variable and type information, and DWARF enumeration types for enums.
43. **Debug allocator** (done, `170d539`, `8bf78ed`; section 18.12, [debug-allocator.md](spec/debug-allocator.md)):
    guard pages, canaries, poisoning, a quarantine, a side table for invalid frees, leak reports with
    allocation sites. See item 96.
44. **`print` and `println` of floats** (done, `9df4459`): the shortest form that reads back to the same
    value.
45. **`time`** (in progress; sections 19.6 and F.7): `Duration`, `Time` in UTC with a monotonic reading,
    `Now`, `Since`, `Sleep`, `Timer`, `Ticker`, `After`, `Tick`, `AfterFunc`; the timer owners are item 67.
    Open: time zones and layout-based formatting.
46. **`net`** (planned, hosted): sockets over the epoll poller of item 14.
47. **`fmt.Sprintf` returns a string** (done, `a0e0987`; section 18.8): counted strings built in a
    buffer of `pluto/fmt`'s own; `Appendf` formats into a `vec.Vec` or `vec.Fixed`.
51. **Owner types in the library** (in progress; sections 7.18 and 18.10): `vec.Vec`, `hash.Map`,
    `hash.Set`, `strings.Builder`, `bytes.Buffer`, `heap.Heap`, `flag.FlagSet`, `os.DirEntries`,
    `exec.Cmd`, `arena.Chunked` and `sync.Scope` are owners with `Drop`. Open: `os.File` is a `*File` from
    an allocator, freed with `alloc.Free`; it becomes an owner returned by value from `Open`, `Create`,
    `OpenFile` and `Pipe`, closing on drop.
52. **The `pu` mirror package for self-hosting** (done, `de6aa50`; migrating the compiler to it is not
    started; [plan](plans/2026-10-09-self-hosting-plan.md)): a Go package `pu` with the library owners,
    which go2pu maps one to one. A Go `map` translates to the built-in map unchanged (item 60).
56. **Self-hosting prerequisites** (in progress; [plan](plans/2026-10-09-self-hosting-plan.md)): done are
    single-pointer structs in an interface's data word (`bcfe96f`, section 7.9), itab lookup by hash
    (`79a4326`, abi.md section 5), `//pu:stack` for `main` (`b951dd2`, section 20.3), growable arenas
    (`56d1d39`), the OS and process layer (`e5128d3`), `internal/constant` (`dee6b7d`), and the phase 1
    libraries (`unicode`, `slices`, `cmp`, `container/heap`, `strconv.ParseFloat`).
65. **Range over a vector reads its length once** (done, `dad7c36`; sections 12.10 and 11.11): `All`,
    `Values` and `Backward` of `vec.Vec` read the length when the loop starts.
68. **`bytes.Buffer` grows** (done, `9c6cdcc`; sections 7.18 and F.3): `NewBuffer` and `NewBufferString`; a
    bounded write into a caller's array uses `vec.Fixed[byte]`.
69. **Error types in `os` and `strconv`** (planned, after item 11, gap 22; sections 16.5 and 16.7): file I/O
    returns `*os.PathError` and the parsers return `*strconv.NumError`, allocated only on failure as
    counted objects. `io.EOF` and `os.ErrClosed` stay sentinels.
74. **`strings.Truncate` and `utf8.RuneStartBefore`** (planned; section 18.8): `Truncate(s, n)` returns the
    longest prefix of at most `n` bytes that ends on a rune boundary, and `RuneStartBefore(s, i)` rounds a
    byte offset down to the start of a rune. The panic for `s[i:j]` off a rune boundary names them.
