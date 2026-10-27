# Plutonium roadmap

**Updated:** 2026-10-27 (paths; the compiler is self-hosted and the Go implementation removed)

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
section where it belongs.

## Current conformance follow-up, 2026-10-15

The approved consistency corrections were applied and reviewed in the
[resolution record](verification/2026-10-15-spec-consistency.md). Earlier completed
rows remain evidence for their stated scope; they do not certify the whole compiler
against the corrected specification. The immediate implementation order is:

101. **Ordinary kernel lifetime protection** (planned; sections 1.2, 18.11, 21.5).
     `compiler/internal/build/build.p` currently forces generation checks off for kernel and
     freestanding builds and rejects enabling them. Replace that blanket policy with
     static proof, available automatic protection, or rejection of unsupported unproved
     operations. Keep deliberate unchecked execution distinct. A kernel checking
     representation has not been selected; hosted pointer tags cannot simply be assumed
     suitable. This is a confirmed source-policy gap.
102. **Outstanding aliases during owner transfer** (audited; sections 8.8, 19.3; the cases that need a
     method's result to be a view of a local owner wait on item 138). Probes in `tests/race/transfer*`
     (nine accepted, 24 rejected) borrow an owner's memory through a field or a helper, then move the
     owner by `take`, a task or a channel send, then use the borrow. A borrow kept in a local variable, a
     copy of it or a field of a local struct is E3313 at the conflicting use, and its last use before the
     transfer is accepted. The audit found and fixed four gaps. The race checker missed `alloc.New[T]()`
     written with its package name as a fresh allocation, so an owned view field such as a list's head did
     not own its nodes and any write through the list before a transfer was E3501 (a false positive). Memory
     an owned view field holds moved with the owner even when an alias of it was kept in a package variable,
     a heap object, a map, a channel or another task: `race/leak.p` follows each load of such a field and
     the locals it reaches, and a field whose memory leaks no longer owns it, so the transfer is E3501. A
     view of what a local owner holds, passed to a call that may keep it, is E3314 when the owner then
     crosses to another task (`moves/views.p`, `heapPin`). `take(&h.l)` did not make the views of `h.l`
     stale, and a node reached through another (`l.head.next`) was not a view of `l`. Known failures, each
     marked `knownfail`: a borrow returned by a method of a local owner (`List.First`, `vec.Vec.Ptr`,
     `vec.Vec.Slice`; seven cases, item 138), and an owner passed by pointer to a helper that hands a view
     of its memory to a call that keeps it (one case: neither checker follows a view through calls there).
103. **Nonconsuming owner indexing and length receivers** (planned; sections 11.11,
     13.6). Visible Index/IndexOk/SetIndex and Len validators lack the owner
     value-receiver prohibition. Confirm whole-pipeline rejection and add the missing
     validation where needed, preserving owner pointer receivers and ordinary nonowner
     value receivers. Source inspection establishes a missing local guard, not a
     demonstrated native wrong-code result.
104. **`strings.Slice`, `strings.SliceFrom` and E2019** (done, `13c2a00f`; decision D9; sections 7.3, 11.12,
     17.1, F.6). A slice expression on a string is the compile error E2019
     (`StringSliceNotPermitted`, `pspec("11.12", "Slice expressions")`) with a fix that spells the
     `strings.Slice` or `strings.SliceFrom` call. `lib/pluto/strings` declares `Slice[S ~string]` and
     `SliceFrom`; `internal/lower` lowers both inline from the string-slice code of `strops.go`
     (bounds check, boundary check behind the `nonASCII` flag, shared buffer and count);
     `internal/opt/runebounds.go` applies to the intrinsic. Rewrite every `s[i:j]` on a string in `lib/`,
     the examples and the Plutonium test programs; update the panic message to name
     `strings.Truncate` and `utf8.RuneStartBefore`; add the `tools/go2pu` rewrite table. Tests: E2019 for
     typed, untyped, constant and named string operands, panics for both bounds, no panic for
     `Index`-derived cuts, no call in the SSA of a simple use. Includes item 74.
105. **Normal termination follows Go** (done, `3bb04e01`; review fixes `8b3b99bc`, decision D31; decision D10; sections 12.12, 14.5, 15.4, 19.1, 19.7;
     abi.md 12.10; memory-model.md 3.9; debug-allocator.md Leaks). After `main` returns, one scheduler
     transition closes admission and counts running application tasks and in-flight callbacks (`lib/pluto/rt`
     `sched.p`, `join.p`, `rt_linux.p` `mainOnStack`). With none: cancel pending application timer
     callbacks, retire ended tasks, run package cleanup and leak accounting. Otherwise exit with `main`'s
     status, with no package cleanup; `lib/pluto/alloc/debug` prints the running-task count and skips the
     report. Remove the drain loop. Freestanding entry returns to the program's entry point on the abandon
     path; custom scheduler backends implement the count-and-close transition. Tests: the "forever task
     keeps the program alive" case becomes "program exits with `main`'s status, no package cleanup"; the
     joined-task cases keep their cleanup and leak assertions; Go-oracle programs that start a task and
     return from `main`.
106. **`errors.New` has a fresh identity** (done, `ff1eb3b2`; decision D11; sections 15.2, 16.2, 16.3, 17.1, F.4).
     `internal/lower/errorsnew.go` emits a type descriptor with a constant `Error` method and a static
     `uint64` counter for each call site, and lowers each call to an atomic fetch-add, an exhaustion
     panic and an interface value (site type, count). `fmt` `%T` prints `*errors.errorString`. Update the
     catalog `Why` texts of E2008 and E2542; find library code and tests that assert `*errors.Text` on an
     `errors.New` result. Tests: two results of one site compare unequal, copies equal, a Go-oracle program.
107. **Non-sendable type converted to `error` (E2020)** (done, `1af396e4`, comparisons `074077c9`; decision D14; sections 7.9, 8.8, 16.7,
     19.2). Add E2020 in `internal/types2/errors/plutonium.go` and the catalog (`Spec` 8.8, with a fix
     showing `alloc.Synced`): a conversion of a value whose type is not sendable to `error` or an interface
     embedding it. Make the whole-program interface sendability analysis skip conversions that section 18.3
     proves are views of the frame, and name one offending conversion (package and line) in the crossing
     error. Tests: a `shared[T]` returned as `error`, `fmt.Println(sh)` not counting, a `chan any` crossing
     error naming the conversion.
108. **Func values that may be closures of a frame at a task start** (done, `3d0a8a7c`; review test `bf23da1e`; decision D13; sections
     11.5, 12.12, 17.1, 18.3, 19.4; abi.md 12.6). Report E3004 where a func value that section 18.3 may place
     in a frame reaches a `task` start, `wg.Task`, `rt.Go` or `time.AfterFunc` argument, and E3003 at a call
     that passes such a closure to a function whose summary starts a task with it (path in the message). Keep
     E2590 and E2594 for the forms the checker sees directly. Delete `rt.checkGoFunc` and its call, the run-
     time panic and its message, and the abi.md 12.6 sentence. Tests: func values from a parameter, field,
     element, channel and a twice-assigned variable; a func loaded from heap never rejected.
109. **`time.AfterFunc` returns `synced[Timer]`** (done, `e6063931`; decision D16; sections 19.6, F.7, A.3;
     abi.md 12.9). Change `lib/pluto/time` `AfterFunc` to return a counted `synced[Timer]`; the runtime holds
     a reference while the timer is armed, released when `f` starts or `Stop` succeeds; dropping handles
     never stops the timer; `NewTimer` and `NewTicker` keep `owned[...]`. Update `Stop` and `Reset` and the
     leak accounting of the debug allocator. Tests: the statement `time.AfterFunc(d, f)` fires `f`; `Stop`
     through a copy cancels it; no leak after firing.
110. **Elementwise loop over two slices** (done, `59242b98`; decision D17; sections 19.4, 17.1). In
     `internal/scoped` and `internal/race`, accept two possibly overlapping views used only in the same
     partition form with the same loop variable and parameters; lower a check before each `s.Task` call that
     the views start at the same element or do not overlap, panicking otherwise, in every build mode (not
     removed by `--gencheck=false` or kernel builds). Keep E3402 for `in[i+1]` with `out[i]`. Tests: the
     accepted loop, the overlap panic, the rejected shifted access, unchecked mode.
111. **`defer` in a loop: one path rule; E2533 retired** (done, `ba0535ec`; decision D18; sections 12.11, 12.19,
     A.2). Replace the loop `defer` checks by the rule of 12.19: every path from the `defer` leaves every
     containing loop (`return`, `panic` or never-returning call, `goto` out, `break` of the outermost loop,
     labeled `continue` outward) before reaching it again, over `for`, range-over-func bodies and backward
     `goto` loops; otherwise E2530. Retire E2533 from the catalog and codes (keep E2534). Tests: lock-and-
     return loop, unlabeled `continue`, labeled `break` and `continue`, backward `goto`.
112. **`defer` evaluates at the statement; `Drop` has a pointer receiver** (done, `58c4369a`; decisions D15, D21;
     sections 7.18, 10.14, 12.19, 18.5, 18.6). In `internal/moves`, move an owner passed by value or as a
     value receiver of a deferred call at the `defer` statement; save `&x` for a pointer-receiver call; make
     `defer x.Drop()` leave `x` usable, drop what it holds when the call runs and not again at scope end.
     Report E3201 for `Drop` with a value receiver, widen its catalog text, and show `func (f *File) Drop()`.
     Update library and test code that declares a value-receiver `Drop`. Tests: the 12.19 example, `defer
     x.Drop()` after a move, E3201.
113. **Drop of `type B owner A` and `x.Drop()` on every owner** (done, `fdb055f7`; decision D20; sections 7.18,
     8.7, 10.8, 18.6). Generate drop glue for a type defined from an owner: its own `Drop`, then the drop of
     `A` (its `Drop`, then its fields); `B` gets none of `A`'s methods. Accept `x.Drop()` on a whole variable
     of any owner type, declared `Drop` or not, as the early-drop operation (not a method value, not in the
     method set). Tests: `type Log owner File` closes the file on drop, `x.Drop()` on a type with no `Drop`,
     E3307 and E3303 unchanged.
114. **`print` and `println` argument types and float format** (done, `28c8e545`; decision D19; sections 13.12,
     A.2, A.4). Accept a value whose underlying type is boolean, integer (enums included), floating-point or
     string; report the unprintable-argument error (E2521) for an owner type, even over an integer, and for
     other types (catalog fix names `fmt` and `int32(fd)`). The float format is the shortest form
     of Go 1.27.2 (D19 withdrew the older fixed form), which the compiler already prints. Kernel-mode floats keep the bit-pattern form. Tests: defined integer
     types and enums, an owner over `int32` rejected.
115. **`string(x)` accepts only `rune`** (done, `a8a5a048`; decision D23; sections 8.11, 11.24, A.2). In
     `internal/types2`, accept the single-value conversion only for type `rune` (identical to `int32`) or an
     untyped integer or rune constant, and the slice conversion only for a slice whose element type is `rune`
     (defined slice types included); report an error for `type myRune rune`, `type Fd int32` and `[]myRune`,
     with a fix naming `string(rune(x))`. Rewrite uses in `lib/`, examples and tests. Tests: each accepted
     and rejected form.
116. **Packed types keep reserved bits** (done, `177e61c5`; decision D29; sections 7.14, 8.10). Conversions
     between an integer or byte array and a packed type keep every bit both ways; field writes change only
     their bits; reserved bits (bits of `_` fields and uncovered backing bits) are zero in literals and zero
     values. Equality and map hashing of packed values ignore reserved bits and compare the rest bit by bit
     (floating-point fields by their bits). Tests: round-trip of a register value with reserved bits,
     equality, map key.
117. **Packed bit layout with an array backing; E2021** (done, `74ccb87b`; decision D30; section 7.14). Lay out
     `[N]byte`, `[]byte` and unbacked packed types by byte: position `p` is bit `p mod 8` of byte `p / 8`, a
     bit field in consecutive positions, a byte-level field in the stated byte order whatever the target. Add
     E2021 and its catalog entry: in a big-endian packed type with a byte-array, slice or no backing, a bit
     field crosses a byte boundary or a byte-level field is not byte-aligned; the fix shows a nested `packed
     be uint16` field. Check the `IPv4Header` example and add the `Frag` field. Tests: IPv4 header bytes on
     both endiannesses, E2021.
118. **Per-CPU access regions; E2022 and E2023** (done, `046f09c3`; review fixes `0fd6f59d`; decision D24; sections 19.3, 20.9; memory-
     model.md 6.4). Infer non-migrating regions (preemption disabled, maskable interrupts disabled, `rt.Lock`
     held; on every path, across calls, deferred release counting) and interrupt-disabled regions (also the
     body of a `//pu:interrupt` handler up to a call that may switch tasks), in `internal/race`. Exempt
     `//pu:percpu` variables from rule 1; allow lone word-sized loads and stores anywhere; report E2022 for
     any other access outside the region it needs (naming the access and whether a handler makes interrupts
     necessary); report E2023 for a pointer from `&v` used outside its region, sent, or passed to a task.
     Treat `x86.Cli`, `Sti`, `SaveFlagsCli` and `RestoreFlags` as safe calls the analysis follows. Tests:
     `current`, `v++` under `rt.DisablePreemption`, pointer escape, handler sharing.
119. **Interrupt handlers as parties in the race rules** (done, `046f09c3`; decision D25; sections 18.12, 19.3,
     20.10, 20.11, F.5; memory-model.md 4.1, 6.1). Treat each installed `//pu:interrupt` handler as a party
     that may run concurrently with itself and all tasks; apply rules 1 to 3 with handlers as parties, accept
     atomics and `rt.Lock` guards (recognized like `sync.Mutex`), report E3502 and E3503 naming the handler,
     and accept `volatile[T]` and interrupt-disabled regions only for per-CPU data. Remove the allocator
     exemption ("or no lock"). Remove `Cli`, `Sti`, `SaveFlagsCli` and `RestoreFlags` from the unsafe listing
     of `pluto vet -unsafe`. Audit the kernel allocators in `lib` for state shared with handlers.
120. **Interrupt-context wait check: inserted drops, `//pu:blocking`, bodyless functions** (done, `dd72a5f3`;
     decisions D26, D27, D28; sections 1.2, 18.12, 20.3, 20.10, 20.11, 21). Make the E2584 analysis follow
     inserted drops (scope end, overwrite, temporaries, counted release at zero) into `Drop`, drop glue and
     the `Free` of every installed allocator; take a bodyless declaration as not waiting unless marked
     `//pu:blocking`; keep `//pu:blocking` on functions with a body as a restriction only. Tests: a drop that
     frees through a waiting allocator in a handler, a bodyless function with and without the directive.
121. **E3202 explanation shows `owner` on the declaration** (done, `48c2c9ce`; decision D12; sections 7.18, 18).
     Update the catalog text of E3202 and `tests/diag` so the explanation shows the declaration
     with `owner` inserted and says why the keyword is written (a change to a field's type elsewhere is
     reported at the declaration). Update `pluto explain` and the hover text if they repeat the old wording.
122. **Operating-system names: `Name()` returns an error, `NameLossy`, `*os.NameError`** (done, `04d4a1c8` library, `fdf508f1` compile error, `8821701e` go2pu, `0ea27d1e` skill; decision D36;
     sections 7.3, A.3, F.9, 16.7). In `lib/pluto/os`: `DirEntry.Name() (string, error)`, `NameLossy()`,
     `Getenv(key) (string, error)` and the other string forms that return a system name (`Getwd`, `Readlink`,
     `LookupEnv`, `Arg`, `FileInfo.Name`, `File.Name`, `TempDir`) get the same split; add the counted
     `*os.NameError{Op, Bytes}`; the `GetenvBytes`, `RawName` and `Bytes` forms stay. Make the single-value use a
     compile error whose explanation names `RawName`. Update every caller in `lib/` (`os/exec`, `flag`,
     the tools), `tools/go2pu` (it maps Go's `Name()` and `Getenv` to the two-value forms) and the skill's
     references in `skill/plutonium/`. Decide the `PathError.Path` field with it. PlutoniumOS refuses
     non-UTF-8 names at creation. Tests: a directory with a non-UTF-8 entry, a non-UTF-8 variable, the
     compile error, `NameLossy`. Closes the open part of item 57. Built as specified, `LookupEnv` returning
     `(string, bool, error)`, plus `FileInfo.RawName` and `File.RawName`; `PathError.Path` stays a string (lossy,
     for the message). `go2pu` maps Go's `Getenv`, `LookupEnv`, `TempDir` and `Name()` to the Lossy forms
     (`compat.Getenv`, `x.NameLossy()`), since an expression cannot take the two-value form. Still lossy strings
     with a documented Bytes form, outside `pluto/os`: `flag.Arg`/`flag.Args`, the `path` argument of
     `filepath.WalkDir`, `exec.LookPath`; the owner decides whether they follow D36 too.
123. **Dependent per-CPU store is E2022** (done, `e56a5d81`; decision D37; sections 20.9, 19.3, memory-model.md 6.4).
     In `internal/race`, treat a store to a `//pu:percpu` variable whose value or controlling condition
     depends, by data or control flow, on an earlier load of the same variable as a read-modify-write that
     needs a non-migrating region. Tests: `x := ticks; ticks = x + 1` and
     `if current == idle { current = next }` are E2022 outside a region and accepted inside
     `rt.DisablePreemption`/`EnablePreemption`; independent lone loads and stores stay accepted. Update the
     E2022 catalog text. Built in `internal/race/dep.go`: a taint analysis over each body (locals, struct and
     array copies, arithmetic, conditions, exits from branches, loops, calls with the callees' loads and the
     parameters that reach a store) and, in the walker, the regions held at each load the dependent store
     depends on, so the load and the store must be in one region and two regions in a row do not satisfy it.
     Calls it cannot see (func values, functions without a body) are assumed to depend on all their
     arguments, and the message says so; a call through a func value is taken to store a variable from all its
     arguments when a function of its type that the program uses as a value stores it.
124. **Typed interrupt gates** (done, `56a8c928`; decision D38; sections 20.8, 20.10, abi.md 10.4 and 10.5). Add
     `x86.Gate`, `x86.InterruptGate` and `x86.TrapGate` to `pluto/arch/x86`; remove `x86.HandlerAddr` and
     move its callers (kernel tests in `testdata/kernel`, the examples) to the gates; E2572 now covers a
     handler of the wrong form for the constructor. Raw IDT words are unsafe code: check `pluto vet -unsafe`.
125. **`//pu:interrupt trap`** (done, `56a8c928`; decision D38; sections 20.3, 20.10). Accept `trap` as the only
     argument of the directive (any other is E2603); a trap handler's body is not an interrupt-disabled
     region. Update the directive tables, the catalog and `pluto explain`.
126. **`x86.Sti` ends a handler's interrupt-disabled region** (done, `bb1fa4c0`; decision D38; sections 19.3, 20.10,
     memory-model.md 6.1). In `internal/race`, the region of a non-trap handler body ends at a call of
     `x86.Sti` as well as at a call that may switch tasks. Tests: a per-CPU access after `Sti` in a handler
     is E2022 without a region of its own. The region analysis already ended the body at `Sti`; the commit
     adds the tests.
127. **Hover text and directive audit** (done, `fe941a3e`; decision D38). The hover text in `internal/lsp/hover.go` for
     `//pu:interrupt` must change (interrupt gate, the `trap` form). Audit every list of directives against
     section 20.3: the compiler's directive table, `internal/diag/catalog_own.go`, the skill's
     `references/kernel-mode.md` and `references/diagnostics.md`, `pluto explain` and the hover text.

128. **Pointer-receiver calls on a field reached through a box** (done, `725937b1`; sections 11.14 and 7.10). A box
     (`owned`, `shared`, `synced`) is a pointer, so `get().v.Push(2)`, `m["a"].v.Push(2)` and `s[0].v.Push(2)`
     act on the shared object. The lowering treated a field of a box-valued expression that is not a
     variable as a value and called the method on a copy, silently losing the effect; `isAddressable`
     (`internal/lower/expr.go`) now follows a box like a pointer, as the checker already did. A struct held
     directly by a map or returned by a call stays an E0125 error. Tests: `testdata/plutonium/boxtemprecv.p`,
     `tests/diag/temp_receiver_field`.

129. **Arena text converted to an interface** (done, `8188dfb8`; sections 18.3, 18.8, 18.9, decision D7). A string
     made in a local arena is a view of the arena, but only strings made in an `alloc.Use` window were
     followed, and converting one to an interface (`fmt.Println(s)`) built and then panicked as the function
     returned: the interface's copy in the frame is released after the arena was dropped. The analysis now
     also follows a string returned by a call that was given a local arena as its allocator, or whose receiver
     holds one (`strings.Builder.Init(&a)` then `b.String()`; `internal/escape/arena.go`), and converting such a
     string, or a value holding one, to an interface in the arena's function is E3313 with the rule and the
     fixes in its text (the holder is the interface's copy, the spec's "declaration of the holder"). Returning
     it is E3001. Other uses of memory from an explicit arena stay with the run-time checks of section 18.11.
     Tests: `tests/diag/arena_boxed`, `testdata/plutonium/arenatext.p`, `internal/escape` tables.

130. **E0021 fix text in a return statement** (done, `c8672374`; sections 8.6, 13.9, 18.3). A value whose method has a
     pointer receiver, returned as an interface, got the fix `&T{...}`, which is E3001 in a constructor.
     Where the value is returned the fix is now `new(T{...})`, with the note that the interface must be
     declared `owner` to take the box (`explainImplements`, `internal/diag/typecheck.go`); other contexts
     keep `&T{...}`. Tests: `tests/diag/ptr_recv_return`, `testdata/plutonium/newiface.p`.

131. **Tests that join tasks without a `sync.Scope` no longer depend on the exit grace** (done, `40fc2eda`; decision D31,
     section 15.4). `TestBoxTasks`, `TestMapTasks` and every test that runs a program under the debug
     allocator with a `WaitGroup`, channel or counter join failed now and then with "1 task still running
     at exit" when the machine was loaded: only a scope join guarantees that the tasks have retired when
     `main` returns. `runDebugAlloc` runs again a program whose run took the abandon path, and the
     run that accounts is the one checked, for the output, the status and the leak report
     (`internal/build/joined_test.go`, `untilRetired`; 30 abandoned runs in a row fail the test);
     `TestNormalTerminationJoinedTasks` (WaitGroup and channel cases), `TestNormalShutdownTaskRetirement`
     and `TestConcDebugAlloc` do the same, and the scope case still requires the cleanup of every run.
     `TestNormalTerminationDebugAllocator`, which expects the abandon line, uses `runDebugAllocRaw`.
132. **Containers hold owners** (done, `a2991a32`; sections 7.18, 10.9, 11.11, 12.10, 13.3, 18.6, F.3; found
     while writing the agent skill, `skill/plutonium/references/known-gaps.md`). `vec.Vec[T owner any]` and
     `hash.Map[K, V owner any]` (over `rt.MapTable`) accept owner elements and own them: `Push`, `Set`,
     `SetIndex` and `m[k] = v` move one in and drop the one they replace, `Pop`, `Remove`, `SwapRemove`,
     `hash.Map.Remove(k)` and `hash.Map.Pop()` move one out, `Delete`, `Truncate`, `Clear`, `Free` and `Drop`
     drop the elements in order (index order for a vector, table order for a map, and the same during a walk),
     `Ptr` lends one, and the iterators yield pointers (`rt.View[T]`, an alias the checker resolves to `*T` for
     an owner `T` and `T` otherwise, section 12.10). The methods that copy an element (`At`, `Index`, `Last`,
     `Clone`, `Map`, `Get`, `IndexOk`) do not compile for owners (E3306, from the intrinsics `rt.CopyValue`,
     `rt.CopyValues` and `rt.MustCopy`). Receiver type parameters of a method inherit `owner` from the type's
     declaration, so the move checker now checks the bodies of generic owner methods. Moving a counted value
     (a string) out of a place with `take`, or out of a variable of a generic function over an `owner` type
     parameter, transfers its references instead of leaking them. `hash.Set` keys and `vec.Fixed` elements
     stay values: a key is looked up by value, which would consume an owner, and a `Fixed` is a copyable view
     that owns nothing. Tests: `testdata/plutonium/containerowners.p` and `takecounted.p` (O0, O2, debug
     allocator leak report), `internal/build/container_owners_test.go`.
133. **A `vec.Vec` crosses in a program that imports `pluto/fmt`** (done, `10f54d3e`; sections 8.8, 12.5, 19.3, 19.8,
     F.2; found with item 132). The race analysis recorded the accesses to the default allocator
     (`alloc.SyncSlab`) through package variables only once a program reached the library's string and
     formatting code, and its per-processor caches (touched in pinned regions, not under a lock) then failed
     the type rule, so E3501 named the Vec's allocator field. Section 19.3 makes the parts of an allocator
     written in unsafe code the responsibility of that code, and appendix F.2 says an arena is not safe for
     simultaneous use: `race.selfSynchronized` treats an allocator that implements `Allocator`, is written in
     unsafe code and holds a lock of its own (`rt.Lock` or `sync.Mutex`) as synchronized by its own methods,
     so its view may cross. An allocator without a lock (an arena, a bump allocator) is still E3501. Tests:
     `internal/race/cases_test.go` (a vec and a map cross in a program that formats text; a vec on a lockless
     allocator does not).
134. **`os.File` is an owner** (done, `45998860`; sections 7.18, 18.6, A.3; found with item 132). `os.Open`,
     `os.Create`, `os.OpenFile`, their `Bytes` forms and `os.NewFile` return an `os.File` by value, and
     `os.Pipe` returns two; `File` is `owner struct` with a `Drop` that closes the descriptor if it is open.
     The zero File is not open (its open bit is clear, so every operation reports `ErrClosed` and `Fd`
     returns -1), `Close` is idempotent and leaves `Drop` nothing to do, and `os.Stdin`, `Stdout` and `Stderr`
     stay `*os.File` pointers to files that are never dropped. `alloc.Free` of a File is gone from `lib`, and
     the tests share a File among tasks through a `sync.Scope` (a view of an owner cannot outlive it) or move it
     into a task. Tests: `testdata/plutonium/osfile.p` (drop at scope end, early return, close then drop,
     zero File, a File in an owner struct, `clear` of an array of files, pipes, files on disk; debug allocator).
135. **Two error codes of the text** (done, `5a7e6732`; sections 7.12, 8.4, 12.9, 14.3; found with item 132).
     An untyped constant used as an enum value (`var c Color = 1`, `c == 1`, `d = 2`, an argument, `case 1:`)
     is E2315 (`UntypedEnumConstant`), where the assignment forms reported E0023 and the comparison and case
     forms E0097; other assignment mismatches stay E0023. The code E2315 belonged to `NotEnumType` (`len(E)`
     or `range E` of a type that is not an enum), which the specification does not number; it moves to the end
     of the group (E2318). `import "reflect"` is E2005 whether or not a `pluto.mod` exists: the loader, which
     resolves paths first, asks `types2.ForbiddenImport` and reports the checker's code instead of E4007 or
     E4008. Tests: `tests/diag/enum_untyped` and `import_reflect` with their full output.
136. **`pluto/arena` needs no scheduler** (done, `8a8246d5`; sections 19.5, 18.9; found with item 132).
     A freestanding or kernel program that imported `pluto/arena` failed with E2593, because the arenas'
     drop paths call `rt.ExitTask`, so `pluto.main` had to be ready for a main that ended with it, and
     `rt.mainGoexit` parked the main task (`schedPark`, a required scheduler hook). `mainGoexit` now ends in
     the late hook `rt.mainWait`, whose body the compiler chooses once the program is lowered, as it does for
     `rt.Sleep`: a program that starts no scheduler stops (`mainWaitDirect`, the existing "no tasks left"
     message), any other parks (`mainWaitParked`). A program that sleeps and ends main with `ExitTask` thus
     no longer starts a scheduler thread either. Tests: `TestKernelArenaNeedsNoScheduler` (arenas build in
     kernel mode at -O0 and -O2; a task still needs a scheduler), and `TestBoundLateHookGoexit` runs its
     program with and without a task.
137. **The address of a variable in a region** (planned; sections 11.5, 18.3; found by the second adversarial
     review of the escape analysis, program c31). Section 11.5 allows a function that returns a closure to
     return or store the address of a variable the closure captures, which lives in the caller's region and
     is a view of the caller's frame at that call. The compiler rejects it as a pointer to a local variable
     (E3001): `func mk() (func() int, *int) { x := 1; g := func() int { return x }; return g, &x }`. The
     analysis must treat such an address as a view of the caller's frame at that call, kept from the next
     run of the call (section 11.5, "Refilling"), and the variable must live in the region.
138. **Views of local owners, counted references and arenas bounded at compile time** (planned; sections
     18.3 "A local owner's memory" with E3001, E3002, E3313 and E3314, 18.7 rule 5, 18.9; found by the third
     adversarial review of the escape analysis, defect 7). A view obtained from an owner held in a local
     variable is bounded by that variable and follows the frame rules, but the analysis takes what a local
     owner's methods return (`v.Slice()`, `v.Ptr(i)` of a local `vec.Vec`, `a.New[int]()` of a local
     `arena.Fixed`) for heap memory, so returning or storing it compiles: the default build catches the use
     by a generation check ("use of freed memory", "use of memory from an arena that was reset"), and
     `--gencheck=false` reads garbage, which 18.9 forbids for arena views. Programs (third review,
     `review3-programs/`): o03, c19 (`return v.Slice()` / `return v.Ptr(0)` of a local vector), c20
     (`keep = v.Slice()`), o26 (a view returned through a method of a local owner struct), o24 (a view
     captured by a closure, then the owner moved into a callee), o25 (`p := &s.n` through a `shared`
     reference that is then replaced), ar01 and ar04 (a pointer from a local `arena.Fixed` returned or
     stored in a package variable), ar02, ar03 and ar07 (use after `Reset`/`Release`, which 18.9 lets a
     checked build catch at run time but not an unchecked one). Controls that are rejected today: o17 to
     o23, ar05, ar06. The work: memory reached through the fields of an owner held in a frame location is a
     view of that location, as the object of an owned box already is (`lkObject`), for every owner type, and
     a local arena's allocations are views of the arena (as `lkArena` is for `alloc.Use` windows); the move
     checker's view tracking (E3313, E3314) then follows them, and `Reset`/`Release` of a local arena end
     its views. This changes what every call on a local owner returns, so the translated compiler, which
     holds views of local vectors in locals, must be checked for each step.
139. **Loops made with `goto` and the blocks a scoped task outlives** (done; sections 18.4, 11.5, 12.17 and
     19.4; found by the third adversarial review, defect 8). A loop made with `goto` (l05: `top: v := n;
     ps[i] = &v; ...; goto top`) shares one variable across its iterations. Sections 11.5 and 12.17 count a
     backward `goto` as a loop, and 18.4 rejects a view kept past an iteration with E3006 rather than giving
     each iteration its own variables, so the escape analysis applies E3006 and lowering is unchanged. The
     loops of `eir` were natural loops only; `findLoops` now also finds a loop that a `goto` enters in the
     middle (no block dominates it), and the back-edge checks run at each block where it is entered. The
     report names the loop from the label to the last `goto` that jumps back to it, where it printed "line 0".
     A scoped task given a view of a variable whose block ends before the scope waits (b16, b23: `{ y := 42;
     p := &y; s.Task(func() { ... *p ... }) }` with the join after the block) is E3401 by 19.4. `scoped`
     records the innermost block where the task is joined (`Task.JoinBlock`); the task's values also go to a
     holder object whose `Ends` lists the variables declared in any block other than that one and those around it, and
     the solver reports a view of one of them stored there. The analysis is flow-insensitive for memory, so a
     shared variable that holds such a view only after the join is still rejected. The old escape analysis
     (`PLUTO_ESCAPE=old`) is unchanged. Tests: `tests/escape/gotoloop_*` (five rejected, among them a loop
     entered in the middle and a nested one, and five accepted), `scopeblock_*` (seven rejected, six
     accepted) and the review programs l05, b16 and b23.
140. **Memory allocated from a shared allocator, for the race check** (planned; sections 19.4 "Memory reached
     through views", 18.9; depends on item 138; found by the race-fix agent, probe `a13fixed`). A scoped task
     that calls `Reset` on an `arena.Fixed` held in the starting function's frame, then allocates from it,
     while the starting function reads through a pointer into an earlier allocation, compiles:

     ```go
     a := arena.NewFixed(1024)
     v := vec.New[int](&a)
     v.Push(1)
     p := v.Ptr(0)
     var s sync.Scope
     s.Task(func() { mu.Lock(); a.Reset(); w := vec.New[int](&a); w.Push(7); mu.Unlock() })
     println(*p) // races with Reset and with w.Push, which may write the same memory
     s.Wait()
     ```

     The check sees the task's writes to `a` under `mu`, and its writes through `w` as writes to memory the
     task allocated itself, which no other party can reach; it does not see that `p` and `w` may point to the
     same memory, since an allocator may hand out again what it took back. The read of `*p` is not under
     `mu`, so the program is E3402 by 19.4 (a view of memory in the frame's arena, written by one party
     while another reads it, with no common lock); a checked build stops it at run time only when `*p`
     happens to read after the reset ("use of memory from an arena that was reset", 18.9), and an unchecked
     build reads whatever the task wrote.

     Design (allocator provenance). Memory allocated from an allocator is, for the race check, part of the
     memory of that allocator: a view of an allocation from `&a` (a vector or box built on it, `v.Ptr(i)`,
     `a.New[T]()`) is a view reaching `a`'s memory, wherever `a` is held. Every call that may recycle what
     the allocator handed out (`Reset`, `Release`, `Free`, and `Alloc`, which may return memory freed
     before) is a write to all of that memory; reading or writing through a view of an allocation is an
     access to it. Two parties then conflict when one recycles or writes allocator memory and another
     accesses a view of the same allocator, unless they hold a common lock (E3402). A task's fresh
     allocation stays its own only when it comes from an allocator no other party can reach (a local of the
     task, the task's default allocator not shared with the starting function). The provenance is the one
     item 138 gives the escape analysis (a local arena's allocations are views of the arena, as `lkArena`
     already is for `alloc.Use` windows), carried into the race summaries as a path "the memory of the
     allocator at parameter path P"; the default allocator, which is per task (section 18.9), needs nothing.
     Tests: the probe (E3402 at `println(*p)`), the same with the read under `mu` (accepted), and with the
     task allocating from an arena of its own (accepted).
141. **A task's counted arguments belong to its parameters** (done, `456d5b46` and `89870257`; sections 12.12,
     18.7 rule 2; found by the CI failure of `embedshared` at -O0). A task statement copies each argument
     into the new task as an argument is assigned to a parameter, so the parameter holds the task's one
     reference, and overwriting it releases that reference. The thunk used to keep the argument block's
     reference until the call returned, while the callee took a reference of its own: `s = SWidget{}` in the
     task dropped nothing, and a `Drop` that the spec places before the task goes on ran after it returned,
     or never. For static functions, concrete methods and literals that capture nothing, the task calls a
     variant of the function (`f·task`) whose counted parameters take over the block's references
     (`taskEntry` in lower.p). A task started through an interface method or a func value calls a
     dispatcher (`taskdispatch.p`), one per interface method or func signature used that way with a counted
     argument: it compares the code pointer with each function that can be in that slot or of that signature
     and owns a counted parameter, and calls that function's task variant; any other code borrows, and the
     dispatcher releases the references when it returns. Tests: `taskcounted`, `taskdynamic`. A task that calls an
     allocator's method through an interface (`AllocMethod`) is not dispatched and still borrows.


Next, finish **item 18**, bounded task-stack inference: lowering still supplies a
literal zero bound. Existing virtual-reservation fallback and overflow handling are
implemented, but do not satisfy inference. Then fill the small unverified witness
pairs in the resolution record; reuse completed shutdown, counting and arena tests.
Performance work follows the separate [evidence plan](plans/2026-10-15-language-goal-evidence.md).

## Gaps of the integrated specification

The implementation gaps of
[complete-spec section 6.5](plans/2026-10-10-complete-spec.md), and the item that tracks each:

| Gap | Subject | Item | Section |
|---|---|---|---|
| 1 | Order of evaluation (done) | 73 | 11.26 |
| 2 | Pointer-receiver method on an owner temporary (done) | 78 | 11.14, 12.4 |
| 3 | Composite literal keys that name promoted fields (done) | 79 | 11.3 |
| 4 | Package initialization order (done) | 80 | 15.3 |
| 5 | `len` of a `ring[T, N]` (done) | 81 | 13.6, 7.17 |
| 6 | `defer` of built-ins that may not be deferred (done) | 82 | 12.19 |
| 7 | Exhaustive enum switch is a terminating statement (done) | 75 | 12.1, 12.9, 7.12 |
| 8 | `append` and the others are not predeclared (done) | 83 | 10.3, 13.1 |
| 9 | Every nil dereference panics (done) | 84 | 17.5 |
| 10 | Checked heap and arena memory (D7 follow-up native verified) | 85 | 18.11 |
| 11 | Sendability looks through pointers and slices (done) | 86 | 8.8, 19.2 |
| 12 | An owner's sendability is decided by what it holds (done) | 87 | 8.8 |
| 13 | A channel or map in an ordinary interface is lent (done) | 88 | 7.9 |
| 14 | Operators other than comparison do not apply to an owner (done) | 89 | 11.18, 7.18 |
| 15 | A `//go:` line comment is an error (done) | 75 | 4.1, 20.3 |
| 16 | `portable` (done) | 70 | 8.9 |
| 17 | Task stacks from the program-wide default allocator (done) | 90 | 19.1 |
| 18 | Stack overflow message and stack probes (done) | 91 | 19.1, 17.5 |
| 19 | `NewTimer`, `NewTicker`, `AfterFunc` return owners (done) | 67 | 19.6, F.7 |
| 20 | The `task` statement and the rename sweep (done) | 62 | 12.12, 4.5, 4.6, 17.1 |
| 21 | The built-in `map[K]V` (done) | 60 | 7.10, 12.10, 13.5, 13.7 |
| 22 | Counted `error` values and library error types (done) | 11, 69 | 16.7, 16.5 |
| 23 | Package initializer that allocates, in a freestanding build (done) | 75 | 15.2 |
| 24 | Embedding `shared[T]` and `synced[T]` (done) | 92 | 7.6, 7.19 |
| 31 | The `or` clause (done) | 99 | 11.27, 4.6 |
| 32 | Package initialization starts no task (done, D2) | 100 | 15.2, 15.3, 19.1, 19.3 |
| 33 | `clear` of a slice of owners drops each element (done) | 75 | 13.3 |
| 34 | Ordinary kernel lifetime protection (planned) | 101 | 1.2, 18.11, 21.5 |
| 35 | Outstanding aliases across owner transfer (audited; parts wait on item 138) | 102 | 8.8, 19.3 |
| 36 | Nonconsuming owner indexing/length receivers (planned) | 103 | 11.11, 13.6 |
| 37 | Bounded task-stack inference (planned) | 18 | 19.1 |
| 38 | A string is cut with `strings.Slice`, E2019 (done, D9) | 104, 74 | 7.3, 11.12, F.6 |
| 39 | Normal termination follows Go (done, D10) | 105 | 15.4, 19.1 |
| 40 | `errors.New` has a fresh identity (done, D11) | 106 | 16.3 |
| 43 | A task's counted arguments belong to its parameters (done) | 141 | 12.12, 18.7 |

The pass that aligned the documents with the chapters found six more, items 93 to 98; they are rows 25 to
30 of complete-spec section 6.5. Row 31, item 99, is the `or` clause, decided later the same day. Row 32,
item 100, comes from the pass that removed the specification's internal contradictions. Row 33, item 75, is
the `clear` of a slice of owners, found when the spec fix pass checked item 75's sub-items against the
compiler (2026-10-11).

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

   The two known escape bugs are fixed and verified on native amd64 (2026-10-12). Closure result
   provenance is separate by result and field, so an unrelated captured region does not make an
   independent counted string borrow that region (difftest seed 362192). Views through temporary
   owners and counted boxes receive E3009 when stored beyond their statement (section 18.3),
   including projected aggregates, arrays, generic helpers, interfaces and indirect calls. Counted
   transfers keep independent references; immediate calls and full statement-header borrows pass.
   See [the verification record](verification/2026-10-12-escape-bugs.md).

   Known precision limit (section 16.6): a generic factory that returns either a counted assertion or
   its ordinary parameter retains both branches in its summary. The static instantiation below is
   conservatively rejected when `errors.As` borrows the returned child, although that child is static.
   Heap provenance does not retain the exact dynamic type needed to rule out the assertion branch.
   Preserve this case for the later compiler/specification review; normal static unwraps and root-held
   children pass.

   ```plutonium
   package main
   import "pluto/errors"
   type E struct { n int }
   func (*E) Error() string { return "error" }
   var sentinel = &E{n: 42}
   func box[T interface{ Error() string }](p T) error {
       if s, ok := any(p).(synced[E]); ok { return s }
       return p
   }
   type root int
   func (root) Error() string { return "root" }
   func (root) Unwrap() error { return box(sentinel) }
   func main() { var p *E; errors.As(root(0), &p); println(p.n) }
   ```

## Gaps the integrated specification defines (complete-spec section 6.5)

73. **Order of evaluation** (done, gap 1, `f74c4a4`; section 11.26): calls, method calls, receives and
    logical operations are evaluated first, left to right, then the other operands, so `[]int{a, f()}` with
    an `f` that changes `a` gives `[2 2]`. Open: survey the rest of `compiler/internal/lower` and add `testdata/run`
    and difftest cases (assignments, `x[i] = f()`, operands of operators); every difference from the
    specified order is a false friend and is fixed.
78. **A pointer-receiver method on an owner temporary** (done, gap 2, `04d7cf95`; sections 11.14 and 12.4):
    `readAll(f).Slice()`, with views of the temporary limited to the statement.
79. **Composite literal keys that name promoted fields** (done, gap 3, `42dc6640`; section 11.3): the key
    selects the promoted field; lowering used to crash on it.
80. **Package initialization order** (done, gap 4, `4aa24f2`; section 15.3): `pluto/rt` and `pluto/alloc`
    first, then the remaining packages by import path, each after its imports.
81. **`len` of a `ring[T, N]`** (done, gap 5, `36160adc`; sections 13.6 and 7.17): `len(r)` calls `r.Len()`,
    as for a library container; `cap(r)` stays an error.
82. **`defer` of built-ins that may not be deferred** (done, gap 6, `e92486fc`, `6a5d201c`; section 12.19, chapter 13):
    `clear`, `copy` and `delete` run at the function's return with their arguments evaluated when the defer
    ran; every other builtin is E2532, with the fix to wrap the call in a function literal.
83. **`append`, `recover`, `complex`, `real`, `imag`, `complex64` and `complex128` are not predeclared**
    (done, gap 8, `4e0cb95c`; sections 10.3 and 13.1): a program may declare them, and a use without a
    declaration is the error that names the replacement.
84. **Every nil dereference panics** (done, gap 9, `64e41b2`; section 17.5): an access whose offset from the
    pointer can reach past the unmapped page compares the pointer with nil first.
85. **Generation checks for heap and arena memory** (previous scope native verified 2026-10-12;
    D7 safe/manual API and nonwrapping lifetime follow-up native verified
    2026-10-14, `2180081`, gap10; tracked archive, final review and installed smoke passed;
    section 18.11, abi.md section 1.3): the approved shared directory covers every checked pointer,
    small and arbitrary contiguous large Slab/Chunked blocks and inferred custom allocators.
    Persistent address histories, stable reader metadata and saved backing incarnations prevent
    reuse resurrection while preserving unaffected partial-release views. Fixed `arena.Arena`
    buffers were explicitly unchecked in that verified scope. D7 removes the ordinary safe API
    exemption and hosted epoch-wrap allowance. Approved `NewFixed(capacity,parent) Fixed` owns
    private fixed-capacity backing; `NewUnsafe(buf) Arena` is the explicit manual route. The isolated
    implementation enforces exact allocation invalidation, backing lifetime and noncolliding
    incarnations, with live-prefix and ordinary-use acceptance controls. All-package tests,
    aggregate coverage nondecrease and every floor, zero lint, full SSA and generation-off suites
    pass with unchanged caps. See the
    [current verification record](verification/2026-10-13-safe-arena-lifetimes.md) and
    [implementation and acceptance plan](plans/2026-10-13-safe-arena-lifetimes.md).
    Prior full native gates pass with unchanged caps; see the
    [verification and measurement record](verification/2026-10-12-generation-directory.md).
    Tuning remains ordered after the gap list: coherent whole-range/ancestor snapshots,
    same-granule scalar checks, proven extent reuse and interval lookup hints without cached
    liveness. Dense reads cost 16.498 ns versus prior checked 0.809 ns; consumed 1024-byte
    copy/clear costs 1503.776 ns versus 49.247 ns. These scoped measurements are not application
    ratios, and sparse mapping-layout differences are documented.
86. **Sendability looks through pointers and slices** (done, gap 11, `29b06f73`; sections 8.8 and 19.2): a
    `*S` whose `S` holds a `shared[T]` is not sendable.
87. **An owner's sendability is decided by what it holds** (done, gap 12, `44e0ac24`; section 8.8): a view
    field that holds only memory the owner allocated moves with it; any other view field is checked where
    the owner crosses.
88. **A channel or map in an ordinary interface is lent** (done, gap 13, `2c5ad38f`; section 7.9): as a
    `shared[T]` is, so `func f() any { return make(chan int) }` is E3001.
89. **Operators other than comparison do not apply to an owner** (done, gap 14, `1ddde0ac`; sections 11.18
    and 7.18): `*f + 1` for `type Fd owner int32` is E3214, naming the conversion that reads the value.
90. **Task stacks from the program-wide default allocator** (done, gap 17, `cb9e3ef`; section 19.1, abi.md
    section 12.1): never from the starting task's `alloc.Use` override.
91. **Stack overflow message and probes** (done, gap 18; sections 19.1 and 17.5, abi.md section 6): the
    panic names the size of the stack and how to ask for more (`rt.Go(f, size)` for a task, `//pu:stack`
    for `main`), and a function whose frame leaves its first call more than a page below its pushes probes
    each page of the frame in its prologue, so that it cannot step over the guard.
92. **Embedding `shared[T]` and `synced[T]`** (done, gap 24, `2a0c6a21`; sections 7.6 and 7.19): embedding
    promotes `T`'s methods, as `owned[T]` does; verified by `embedshared.p`.
94. **Any two functions exported under one symbol are E2512** (done, `5d25c7c6`; section 20.4):
    `internal/lower` reports it for any two exports of one symbol, not only for wrapped exports.
95. **Channel reference counts** (decided, gap 26; sections 18.7 and 19.8): hosted counts
    are plain before threading and switch to atomic before another thread can run, except
    `shared` remains plain. Freestanding counts are atomic from the start. Channels follow
    this approved rule; inspected runtime counting agrees with it.
96. **Package-variable memory reporting** (old implementation direction superseded by D6 on
    2026-10-13; sections 15.4 and 18.6, debug-allocator.md).
    D6 approves normal program termination releasing owned resources, including package owners.
    The shutdown policy and normative amendment are reviewed; the replacement implementation
    passes all-package tests, exhaustive sharded coverage, lint, full SSA and unchecked-mode
    gates and the tracked-source archive. See its
    [verification record](verification/2026-10-14-normal-shutdown.md). The abandoned
    package-held classification, E3601, source proofs and snapshot machinery were archived and removed
    from the active checkout; that abandoned design is not claimed complete. Its D6 replacement
    is landed at `e63a1497`. Independent core task-exit cleanup remains landed.
    Optional allocator and scheduler candidates are archived for separate evidence-based review.
    The hostile dirty-page tests demonstrated defensive robustness, not a PageProvider contract failure:
    providers promise zeroed pages. See the [cleanup ledger](verification/2026-10-13-item96-cleanup.md)
    and [historical verification record](verification/2026-10-13-package-memory.md).
97. **`pluto vet -unsafe`** (done; sections 18.12, 21.1 and 21.5): audits every source function and
    initializer, with typed unsafe sites and safe caller paths, including possible indirect calls and
    cleanup. Findings exit 0; source errors retain normal diagnostics. Native full verification passes;
    [record](verification/2026-10-12-vet-unsafe.md).
98. **Hosted task stacks under `RLIMIT_AS`** (decided, gap 29; sections 19.1 and 21.4): the specification
    states what `173c2f6` does: at least 64 MiB where the address space allows, otherwise smaller
    reservations, never below 64 KiB, and `rt.Go` asks for a size. Nothing is left to build.
99. **The `or` clause** (done, gap 31; section 11.27, decided 2026-10-10): `v := f() or return`, `or return E`,
    `or continue`, `or panic(err)` and `or { ... }` after a call whose last result is exactly `error`. The
    parser (`OrStmt`, E1013), the type checker (E2461 to E2465, the `err` block, the leaving rule, the short
    forms of an unbraced `return`), lowering and the move, escape and race analyses, `pluto fmt`, the language
    server and the VS Code grammar implement it; go2pu writes plain propagations as `or return`, and the
    library uses it in `os`, `os/exec`, `path/filepath` and `flag`. Tests: `testdata/plutonium/or*.p` at `-O0`
    and `-O2` (`orowner.p` also under the debug allocator), `testdata/trace/orpanic.p`, a kernel-mode compile,
    and `tests/diag/or_rules` and `syntax_invalid_or`. A counted `err` is released when the
    clause is left, including return and discard paths; the counted-error runtime tests verify this
    under the debug allocator at both optimization levels (items 11 and 69, gap 22).
100. **Package initialization starts no task** (done, gap 32, decision D2; sections 15.2, 15.3,
    19.1 and 19.3): E2587 names the task start and the package-variable initializer or explicit
    `init`, with the fix to start it from `main`. Whole-program reachability runs before optimization
    for every loaded package and every build mode. It follows function values, interfaces, generated
    adapters, deferred calls and implicit drops, and continues past waits. Merely storing a callback
    is legal. Blank initializer results are destroyed after evaluation and named stores, including
    fresh owners from receives and assertions; counted temporaries are released before the next
    initializer. Full default and brief diagnostics, O0/O2 regressions, library initializer coverage,
    exact destruction counts and native verification pass. The obsolete G44 block and allowlist
    entry are removed. See [verification](verification/2026-10-12-initialization-tasks.md).

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
11. **Errors** (done, `e365a43f`; chapter 16, native amd64 verified 2026-10-12): the conventions of
    sections 16.1 to 16.6 and static `errors.New` calls (`72841e9`, `3f18f71`), plus counted allocated
    errors (section 16.7, gap 22). Copying or dropping an `error` counts its shared/synced dynamic value;
    there is no `errors.Free`. `fmt.Errorf`, `errors.Join` and the library error types are counted
    (item 69). Borrowed `errors.As` targets remain bounded by the caller's held error, including
    wrapped fields, trees and helpers; freshly allocated unwrap children require a counted target.
    Exact shared/synced assertions and counted As targets retain independent references. Runtime
    allocation/drop tests pass at O0/O2, including `or` releases, and full diagnostics cover unsafe
    unwrap borrowing. The escape checks of item 5 cover the rest, with its recorded precision limit.
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
66. **Large task stacks** (done, `173c2f6`; section 19.1, abi.md sections 12.1 and 12.7): 64 MiB lazily
    mapped hosted stacks, cached.
67. **Counted channels, and a channel owns its timer** (done, `bdb7683`; sections 7.11, 19.6, 19.8, abi.md
    section 12.6): a channel is a counted reference freed with its last reference. `time.NewTimer`,
    `NewTicker` and `AfterFunc` return `owned[Timer]` and `owned[Ticker]`, which stop and free the timer
    when dropped (section F.7, gap 19); nothing is freed by hand.

## Language

19. **Methods with type parameters** (done, `4f98246`; section 10.15): method values must be instantiated.
20. **Ownership: owners, moves and views** (in progress; chapter 18, sections 7.18, 7.19 and 8.7,
    [design](plans/2026-10-09-memory-safety-design.md)): done are `owner` types, the move checker
    (`internal/moves`, E3301 to E3314, including views of local owners used after a move, section 18.3),
    automatic drops on every path, `take`, `unsafe.Drop`, `owned[T]`,
    `shared`, `synced`, `weak`, owning interfaces, owner unions, the local-arena rule (section 18.9), and
    generation checks and arena epochs (items 71 and 72). Commits: `ca7e064`, `dc808a9`, `01daa4b`,
    `1b25e7d`, `052c091`. Open: items 78, 85 to 89 and 92, and the library owners of item 51.
24. **Compile-time type-switch folding** (planned; an optimization, sections 12.9 and 21.3): decide
    `switch any(x).(type)` on a type parameter per instantiation; acceptance never depends on it.
28. **`defer` in loops** (decided; sections 12.19 and 12.11): a `defer` in a loop is E2530; a range-over-func
    body allows one only when every path after it leaves the loop. Nothing is left to build.
29. **`min` and `max` of strings, comparison of large arrays** (done, `82642d4`, `65addf9`; sections 13.8
    and 11.20).
58. **Deferred calls on a panic, and `rt.ExitTask`** (done, `71d0441`; sections 17.2 and 17.3): a hosted panic
    runs the task's deferred calls before the handler; `rt.ExitTask` ends a task through its frames' return
    paths; `testing`'s `FailNow` uses it. Freestanding and kernel builds are unchanged.
    Native-verified follow-up: callers propagate exits through bound scheduler methods and late
    runtime hooks, including custom stack allocators and saved/deferred calls. Real source bodies
    retain their own effects. See [verification](verification/2026-10-12-goexit-hooks.md).
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
    `(string, error)` returns through memory; Linux file names that are not UTF-8 are decided (D36, item 122); `compiler/compat` can drop helpers for what is native.
60. **Counted `map[K]V`** (done, gap 21, `7867af2`, `8fd4ab7`, `0f8bc1b`, `c491a67`, `3c094e3`; sections 7.10,
    12.10, 13.3, 13.5, 13.7 and 19.3, abi.md section 1.4): one word, a counted reference to a
    `pluto/rt.MapTable`; `make(map[K]V, n, a)` takes an allocator and a literal allocates from the current
    allocator; owner keys and elements are E3213; a map is sendable, and the contents of the maps of a type
    that crosses to another task fall under the guard rule of item 59; go2pu translates Go's maps as they
    are. `hash.Map` takes owner values and has `Remove` and `Pop` (item 132). Left: the race rule groups maps
    by type, so two maps of one type are guarded together when either crosses.
61. **Removed conversions** (done, `080e49b`, `d611757`, `271e57f`; sections 11.24 and 18.8): `string(b)`
    is E2015, `[]byte(s)` and `[]rune(s)` are E2016, `string(i)` of a non-rune integer is E2017; the
    messages show the replacement (`strings.FromUTF8`, `strings.Bytes`, `strconv.Itoa`).
62. **The `task` statement and the rename sweep** (done, gap 20, `4cb685c`, `3967e27`, `523a21f`, `6798a0c`;
    sections 12.12, 4.5, 4.6, 17.1, 19.4): `task f(x)` starts a task; `task` is a contextual keyword that
    begins a statement when the next token is an identifier or `func`; `go f()` is E1012 and names `task`.
    The word "task" replaced "puroutine" in the runtime's messages and traces, `(*sync.WaitGroup).Task`,
    `rt.Panic.Task`, the diagnostics and their goldens. The generated thunks keep the name
    `<function>.go<N>` (abi.md section 12.6), which the specification does not fix.
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
    unchecked mode; kernel and freestanding builds stay unchecked until a kernel tag encoding exists.
    Historical prefix-only measurements reported 1.02x over `bench/`. That result does not describe
    the shared-directory implementation; item 85 records its current measured costs and tuning work.
72. **Arena epochs** (original `3410025` implementation; D7 nonwrapping follow-up under85,
    native verified 2026-10-14, `2180081`; tracked archive, final review and installed smoke passed;
    section 18.11, abi.md section 1.3): the isolated Fixed/Chunked implementation uses persistent
    noncolliding incarnations and fails before exhaustion can revive an old view. Native stale-view,
    reuse-exhaustion and live-prefix controls pass; see item85's verification record.
75. **Language checks the integration left open** (done, gaps 7, 15 and 23 and the directive group; each
    sub-item was checked against the compiler before it was struck): a `//go:` line comment is an error
    that names the `//pu:` equivalent (sections 4.1 and 20.3, E2604, `57a292e4`); `iota` in an enum is an
    error (7.12, 10.7, E0036); a package initializer that allocates in a freestanding build is an error
    (15.2, E2586, `15290dd6`); an expression that moves a variable and uses it elsewhere is an error
    whatever the order (11.26, 18.5, E3301); an exhaustive enum switch is a terminating statement and
    panics for a value that is not a member, as a union switch does (12.1, 12.9, 7.12, `b87fbac2`); the
    directive groups of section 20.3 (`section`, `noreturn`, `noescape`, `nosimd`, `nochecks`, with
    `nobounds` retired into `nochecks`, E2601, `45f1758`), with AGENTS.md and `internal/lsp/hover.go` in
    step. Open: a consuming method used as a method value is an error (11.9; G26, decision (a): the method
    expression and the direct call still work; the compiler accepts it today); `clear` on a slice of
    owners drops each element (13.3; done, row 33, `2f0b88da`: each owner is dropped in index order, its
    `Drop` method before its place becomes the zero value).
77. **No `append`** (done, `38f6777`, `fa4bdcc`, `57e4f05`; sections 13.1, 7.5 and 18.10,
    [plan](plans/2026-10-10-remove-append.md)): every call is E2013 with the fix for its idiom; `vec.Vec`
    grows, `vec.Fixed` is bounded; `fmt.Appendf` replaces `Bprintf`; `make([]T, ...)` is E2014 (fix
    `vec.Make`). Item 83 removes the name from the predeclared scope.

## Tooling

30. **`pluto fmt`** (done, `9015b55`): the formatter, from the forked printer; `TestFmtRepoFormatted`
    (`tests/fmt_test.go`) keeps the repository clean.
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
    guarded by `TestSpecReferences` (`tests/spec_test.go`); the companion documents, plans and README agree with it. Open
    questions stay in the plan's section 6.5, which is the implementation brief.
93. **`pluto doc`** (done; sections 1.1, 18.1 and F): documents source packages without running or linking
    them, with exact type labels (`value`, `view`, `owner (moves)`, `counted`). Optional symbols and
    methods follow aliases, promotions and generic instantiations; defaults and source comments remain
    visible. The shared classifier also corrects editor labels. Exact CLI goldens, native full tests,
    zero lint, aggregate coverage nondecrease and all floors, SSA checks, generation-off tests and a
    tracked-only archive pass. See the [verification record](verification/2026-10-12-pluto-doc.md).

## Platforms and formats

36. **arm64** (planned; section 20.5): `aarch64-linux` and `aarch64-none` targets, a second backend under
    `compiler/internal/arch`, with the instruction mapping of memory-model.md section 5.3.
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
    `exec.Cmd`, `arena.Chunked`, `sync.Scope` and `os.File` (item 134) are owners with `Drop`.
52. **The `pu` mirror package for self-hosting** (done, `de6aa50`;
    [plan](plans/2026-10-09-self-hosting-plan.md)): a Go package `pu` with the library owners,
    which go2pu mapped one to one. The Go package went with the Go implementation; the translated
    compiler uses `compiler/pu`. A Go `map` translates to the built-in map unchanged (item 60).
56. **Self-hosting prerequisites** (done: the compiler builds itself since the `go-cutover` tag; [plan](plans/2026-10-09-self-hosting-plan.md)): done are
    single-pointer structs in an interface's data word (`bcfe96f`, section 7.9), itab lookup by hash
    (`79a4326`, abi.md section 5), `//pu:stack` for `main` (`b951dd2`, section 20.3), growable arenas
    (`56d1d39`), the OS and process layer (`e5128d3`), `internal/constant` (`dee6b7d`), and the phase 1
    libraries (`unicode`, `slices`, `cmp`, `container/heap`, `strconv.ParseFloat`). Complete
    `strconv` quoting is native amd64 verified on 2026-10-12: all string/rune plain, ASCII and
    graphic forms and their bounded/growable Appender forms match exact Go oracles at O0/O2.
    Counted string buffers balance and bounded appends allocate nothing. See the
    [verification record](verification/2026-10-12-strconv-quoting.md). Streaming hashing is also
    native verified: zero-ready `hash.FNV32a`/`FNV64a` match Go, and explicit-key `hash.SipHash24`
    matches official vectors and an independent one-shot Go oracle at O0/O2. Copies and sums
    preserve independent state; public methods allocate nothing. Full native gates pass. See the
    [verification record](verification/2026-10-12-streaming-hash.md). Compiler hashing migration,
    explicit comparable-value encoding and key acquisition remain in the later port.
65. **Range over a vector reads its length once** (done, `dad7c36`; sections 12.10 and 11.11): `All`,
    `Values` and `Backward` of `vec.Vec` read the length when the loop starts.
68. **`bytes.Buffer` grows** (done, `9c6cdcc`; sections 7.18 and F.3): `NewBuffer` and `NewBufferString`; a
    bounded write into a caller's array uses `vec.Fixed[byte]`.
69. **Error types in `os` and `strconv`** (done, `e365a43f`, native amd64 verified 2026-10-12; gap 22,
    sections 16.5 and 16.7): failed file operations return counted `os.PathError` objects and failed
    parsers return counted `strconv.NumError` objects. Successful operations do not allocate an error.
    `io.EOF` and `os.ErrClosed` keep their sentinel identity. `flag` uses `errors.Is` to classify wrapped
    numeric range failures; file and parse context, counted lifetimes and allocation counts are tested.
74. **`strings.Truncate` and `utf8.RuneStartBefore`** (done, `13c2a00f`; sections
    7.3 and F.6): `Truncate(s, n)` returns the longest prefix of at most `n` bytes that ends on a rune
    boundary, and `RuneStartBefore(s, i)` rounds a byte offset down to the start of a rune. The panic of
    `strings.Slice` off a rune boundary names them (item 104).
