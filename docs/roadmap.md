# Plutonium roadmap

**Updated:** 2026-11-09 (cleanup after the implementation survey: stale statuses fixed, open remainders of done
items moved to items 177 to 197, order of work added)

Every known gap between the specification and the compiler and standard library, in priority order. The
specification ([language.md](spec/language.md), version 0.4 (draft), with the binary interface in
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

## Order of work

The [completion plan](plans/2026-11-09-completion-plan.md) sets the order. Its phases, with the items each
one covers:

- **Phase 0, the specification.** User and coordinator rulings on the audits (done: decisions D42 onward,
  each with the audit finding it settles, in [decisions.md](decisions.md)), audits E to J and their
  rulings, a test that compiles the spec's examples (item 185), this roadmap cleanup and the known-gaps
  list, then spec version 0.4. Lazy initialization through `sync.Once` (item 218) and `sync.Cond.Wait`
  in the guard inference (item 219) are decided (D105, D106), and so is the rule for releasing a lock
  (D108, item 220); their compiler work is in phase 1. The race rules of 19.2 to 19.4 were rewritten as
  one model (D109 to D115, the "Race rules rewrite" section of the
  [rulings](verification/2026-11-09-spec-rulings.md)).
- **Phase 1, soundness in the compiler.** The rulings' compiler work (items 146 to 176, first A1, C1, C2,
  B1 and B10: items 146, 147 (done), 148, 152, 159; and item 177); a consuming method value (item 178); the
  view-tracking cases left by item 138 (item 179); the escape contract of `rt.ReleaseValues` (item 180);
  the false escape errors in `types2` unit-test builds (item 181, done); the lowering backstops (item 182); the
  order of evaluation in the rest of lowering (item 183); the allocators the race check does not follow
  (item 196). From audits E to J: arena counts, drop entries and frees (items 198, 202, 209, 212 and
  217); one-word owners lent to an interface (item 199); owner conversions through a defined type (item
  200, done); deferred calls in a scoped interval (item 201); generic conversions and the strictest rule for
  type arguments (items 204 and 208); frame storage in loops (item 206); loads of views from shared memory, guarded owned memory, static objects and handler loads
  (item 214). From D105 and D106: lazy initialization through `sync.Once` in the race check and the
  `Once.Do` release on `rt.ExitTask` (item 218); `sync.Cond.Wait(l)` in the guard inference (item 219),
  with the library's value `Cond` (item 197). From D108: race-check soundness, the rule that a task
  releases only a lock it holds (item 220). From D109 to D115, the race rules rewrite: the guard set
  of every shared write (item 221), the may-release and returned-view summaries (item 222), range bodies
  on another party (item 223), interrupts enabling as the start of sharing (item 224), copying a lock
  (item 225), exact precision limits (item 226), views in scoped sharing (item 227) and the owned memory
  of a per-CPU variable (item 228).
- **Phase 2, test closure.** Tests for untested diagnostic codes and rules (item 184); codes for a
  `//pu:stack` size out of range and for implementation limits (item 207); the deadlock report names a
  panic in progress (item 205); CI gaps (item 186); close item 20 when nothing is left open under it.
- **Phase 3, checks, kernel and performance evidence.** Item 101 phases 3 and 4; the wait check in
  non-migrating regions and its run-time tests (items 210, 213, 215 and 216); proven task stacks in
  checked, kernel and freestanding builds (item 188); generation-check tuning (item 195); measurements
  per the [evidence plan](plans/2026-10-15-language-goal-evidence.md).
- **Phase 4, compiler speed and tools.** The [speed plan](plans/2026-10-28-compiler-speed.md); `sync.Scope`
  phase 2 (item 193), then parallel compiler phases and incremental builds (item 34); DWARF variables and
  types (item 189); alternate signal stacks (item 190); optimizations and precision limits (items 24, 191,
  192, 194, 203 and the limit recorded in item 5).
- **Phase 5, library.** `io`, `bufio`, scanning in `fmt`; time zones and layouts (item 45); `net` (item 46).
  `sync.NewCond` (item 197) moved to phase 1 with item 219.
- **Phase 6, platforms.** Items 38, 37, 36 and 39, in that order.

Small cleanups that fit anywhere: item 187.

## Current conformance follow-up, 2026-10-15

The approved consistency corrections were applied and reviewed in the
[resolution record](verification/2026-10-15-spec-consistency.md). Earlier completed
rows remain evidence for their stated scope; they do not certify the whole compiler
against the corrected specification. The immediate implementation order is:

101. **Ordinary kernel lifetime protection** (in progress; sections 1.2, 18.11, 21.5; decision D41).
     Phase 0 is done: a freestanding or kernel build that does not pass `--gencheck=false` is now
     an error that says to pass it for a deliberate unchecked build, instead of a silent unchecked
     build, and `--gencheck=true` there is still rejected. Every in-repo kernel and freestanding
     build passes the flag. Phase 1 is done: tags use the canonical XOR encoding of abi.md
     section 1.3 (untag is sign extension from bit 47), with hosted results unchanged; the
     checked compiler is 0.05% larger and about 5% slower (plan section 7 has the numbers).
     Phase 2 is done (58fcd043): freestanding and kernel builds are checked by default. A checked one
     exports its metadata source as `pluto_gen_metadata` (abi.md section 1.3), or the build
     fails with E2588, and it refuses to start under 5-level paging. A checked kernel boots
     in QEMU and catches section 18.11's stale view through its panic handler.
     Remaining: the early-boot, interrupt and arena work of phases 3 and 4.
     Design: [kernel lifetime protection](plans/2026-10-28-kernel-lifetime-protection.md).
102. **Outstanding aliases during owner transfer** (done; sections 8.8, 19.3). Probes in `tests/race/transfer*`
     (11 accepted, 30 rejected) borrow an owner's memory through a field or a helper, then move the
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
     stale, and a node reached through another (`l.head.next`) was not a view of `l`. An owner passed by
     pointer to a helper that hands a view of its memory to a call that keeps it (`lend(&l)` with
     `func lend(l *List) { stash(l.head) }`) was accepted: the escape analysis says only whether a callee
     keeps the pointer it is given, not memory loaded through it. `moves/keeps.p` now records, from those
     facts, which inputs of each function may have memory loaded through them kept, through any number of
     helpers and methods, and such a call pins the owner's views as `heapPin` does, so a later task or
     send is E3314. A call through an interface method or a func value follows every function the escape
     analysis takes it may reach: for an interface method, the methods of that name on the types the
     program converts to an interface; for a func value, the functions whose values reach the call, and
     the worst case when those are not known. A callee that only returns memory of its receiver, as an
     allocator's `Alloc` does, does not count as keeping it, so containers that call their allocator are
     still accepted. The seven cases of a borrow returned by a method of a local owner pass since
     item 138.
103. **Nonconsuming owner indexing and length receivers** (done; sections 10.14, 11.11,
     13.6). `x[k]`, `x[k] = v`, `v, ok := x[k]` and `len(x)` on an owner type whose Index,
     SetIndex, IndexOk or Len has a value receiver were accepted before, and ran wrong: each call
     dropped the container, the next use went on with it, and it was dropped again at the end of
     its scope (three drops for two uses, at -O0 and -O2). The type checker now
     rejects them with E2452 (`BadIndexMethod`), with an explanation and a pointer-receiver fix:
     `consumingContainerMethod` in `compiler/internal/types2/indexing.p`, called from `indexSig`
     for every indexing method the type has and from `containerLen`. Generic code was already
     covered: a consuming method never satisfies an interface method, so `container.Sized` and
     the other constraints reject such a type argument. Tests: `tests/diag/container_owner_value_receiver`
     (the four rejections) and `tests/types2/container_owner_receivers` (owner pointer receivers
     and nonowner value receivers, run).
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
     that owns nothing. This restriction is intended: section 10.9 lets a type parameter without `owner` take
     no owner (E3207), and section 7.10 limits only the built-in map. An owner given to such a parameter of the
     library gets E3207 with a fix that names `vec.Vec` and `hash.Map` (`tests/diag/library_owner_arg`).
     E3306 from a copy method is reported at the call in the program that reached it, found by following
     the first caller of each library function (`tests/diag/container_owner_copy`). Generic code over
     `[T owner any]` that ranges over a `vec.Vec[T]` sees `*T`, and since item 145 an instance with a
     value type (`Bump[int]`) does too (`tests/lower/generic_owner_range`). Tests: `testdata/plutonium/containerowners.p`
     and `takecounted.p` (O0, O2, debug allocator leak report), `internal/build/container_owners_test.go`.
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
137. **The address of a variable in a region** (done; sections 11.5, 18.3, 18.4; found by the second
     adversarial review of the escape analysis, program c31). A function that returns a closure may return
     the address of a variable the closure captures, or store it in the region (in another captured
     variable): `func mk() (func() int, *int) { x := 1; g := func() int { return x }; return g, &x }`
     compiles, the variable lives in the caller's region, and the closure and the pointer see the same
     variable. The address is a view of the caller's frame at that call. A function that takes no region
     (its results hold no func) and returns it is E3001 (`func h() *int { _, p := mk(); return p }` used to compile); one that
     returns a closure passes its region down and may return it. Kept past an iteration of a loop around
     the call it is E3006 with its own message ("it points into the region of `mk()`"), where a closure
     kept so stays E3101. Storing it in a package variable or through a pointer the caller passed is E3002:
     "stores" in 11.5 is read as a store into region memory only. Tests: `tests/diag/closure_region_address`
     (E3001, E3006 in a `for` loop, a field and a `goto` loop, E3002 twice),
     `tests/diag/closure_region_address_run` (runs), and
     `tests/escape/accepted_address_of_a_variable_a_returned_closure_captures`, which was a `knownfail`.
     Known holes: a `goto` loop whose kept pointer is dead where the next iteration starts is accepted, as
     for any frame view (18.4 checks a kept view there; nothing can read the stale value); a store whose
     right side is a tuple keeps the E3101 wording for a pointer.
138. **Views of local owners, counted references and arenas bounded at compile time** (done; sections
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
     holds views of local vectors in locals, must be checked for each step. Done: the move checker's part.
     A call result that loads, through a heap-only field, a pointer or slice of a type the owner holds is a
     view of the local owner, so `v.Slice()`, `v.Ptr(i)`, `m.Ptr(k)`, a user owner's methods and a local
     arena's `Alloc` used after the owner moved, was dropped or got a new value are E3313 (tests
     `diag/stale_owner_views`, `diag/stale_owner_views_map`, `diag/stale_arena_view`). The escape part:
     each addressed frame variable that holds an owner has an object for the memory it owns, and a static
     call that passes its address gets, in each result that may point into that memory (by type), a view
     of it (`eir/ownerview.p`); a local arena's `New`, `Slice` and `Alloc` give views of the arena's
     memory. Returning such a view is E3001 and storing it in the heap or a package variable E3002 (o03,
     c19, c20, o26, ar01, ar04); a view stored into the memory of the same owner (a directory listing's
     entries pointing at its names) is accepted. A function literal holds the views of the variables it
     uses, so calling it after the owner moved is E3313 (o24). A view taken through a `shared` or `synced`
     reference in a local variable ends when the variable gets a new value (o25). `Reset` of a local
     checked arena ends every view of its allocations, and `Release(m)` those made after `m :=
     a.Mark()` when that is the only assignment to `m` (otherwise all of them), E3313 (ar02, ar03, ar07;
     section 18.9 now says so). Tests `diag/owner_view_escapes`, `diag/stale_closure_views`,
     `diag/stale_counted_views`, `diag/stale_arena_reclaim`, and the seven item 102 race probes that waited
     on this item. A local owner loaded whole is moved and its memory goes with it, so a view of it kept
     in the value moved is a view of the new owner. A map's walk (`rt.MapIter`) owns no memory: what it
     returns is a view of the map. Four cases are still accepted; item 179 tracks them.
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
     shared variable that holds such a view only after the join is still rejected. The old escape analysis,
     since removed (item 142), was not changed. Tests: `tests/escape/gotoloop_*` (five rejected, among them a loop
     entered in the middle and a nested one, and five accepted), `scopeblock_*` (seven rejected, six
     accepted) and the review programs l05, b16 and b23.
140. **Memory allocated from a shared allocator, for the race check** (done; sections 19.4 "Memory reached
     through views", 18.9; found by the race-fix agent, probe `a13fixed`). A scoped task that called `Reset`
     on an `arena.Fixed` held in the starting function's frame, while the starting function read through a
     pointer into an earlier allocation, compiled:

     ```go
     a := arena.NewFixed(1024)
     v := vec.New[int](&a)
     v.Push(1)
     p := v.Ptr(0)
     var s sync.Scope
     s.Task(func() { mu.Lock(); a.Reset(); mu.Unlock() })
     println(*p) // races with Reset, after which the arena may hand the memory out again
     s.Wait()
     ```

     The check saw the task's writes to `a`'s fields under `mu`, and nothing tied `p` to `a`. Now the
     memory an allocator handed out is a place of its own, past a load from the allocator
     (`compiler/internal/race/allocmem.p`), so the allocator's fields do not hold it. A method that takes
     memory back in bulk (`Reset`, `Release`, `Drop`) of an allocator type outside the runtime and
     `pluto/alloc` records in its summary an atomic write of its receiver's memory; in the check of scoped
     tasks, a use past a load through a variable that is the allocator, or that holds its address in a
     field of allocator type (a `Vec` or `Box` built on `&a`, through `alloc.Allocator` too), is also a
     read of that memory. A bulk call made through a field of a value holding `&a` counts for `a`. So the
     probe is E3402 at `println(*p)`, and the same read under `mu` is accepted. `Alloc` and `Free` are not
     writes of the allocator's memory, which the roadmap's first design had: by 18.9 `Alloc` never hands
     out again memory it handed out and `Free` takes back only what it is given, so a task allocating from
     the frame's arena (and freeing its own vector) does not meet a live view of another party. The
     default allocator needs nothing. What the check does not cover is item 196. Tests:
     `tests/race/viewsrejected_task_{resets,releases}_the_arena_a_view_points_into`,
     `viewsrejected_task_resets_a_chunked_arena_a_view_points_into`,
     `viewsrejected_task_resets_the_arena_through_a_{helper,field}`, and accepted
     `viewsaccepted_view_of_an_arena_read_under_the_task_lock`,
     `viewsaccepted_task_allocates_from_{an_arena_of_its_own,the_arena_a_view_points_into}` and
     `viewsaccepted_task_resets_another_arena`.
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
142. **One escape analysis** (done; sections 11.5, 16.5 to 16.7, 18.3, 20.3). The old analysis
     (`internal/escape`) and `PLUTO_ESCAPE` are gone; `internal/escape2` is the only one. Before the
     removal, `tools/escapedigest -compare` over the corpus listed the cases where the two disagreed.
     Fixed in escape2 with the removal: a function without a body now keeps every pointer argument
     unless it has `//pu:noescape`, which keeps none but still stores one argument where another points
     (it used to keep nothing at all); a box held in a temporary that is a field or element of a call's
     result is a view of that temporary; a task statement's packed variadic arguments are checked as
     the task's; `//pu:resultholds` outside the standard library and on a name that is not a parameter
     is reported (E3007, E3008). A closure region result followed back through a function literal's
     frame is E3001 (item 137); its cases now cite section 11.5. A counted error loaded from memory
     the analysis does not follow (a field, an element, a pointer's target, a channel, a package
     variable) or returned by a function without a body holds counted heap references, and a counted
     error converted to an interface that does not count is a view its holder bounds, so the `Unwrap`
     borrow check sees them. A box passed by value to a parameter of generic type (`save[T Getter](g T)`
     called with `sharedHolder().p`) is lent to the callee: the generic body is analysed once, so what
     it keeps through the type parameter's methods is bounded by what holds the box, as for a counted
     error (`temporaryboxviews` cases). The fmt walk calls a pointer method of `*V` on the address
     of a `V` the walked value holds in place, such as a struct field inside a box
     (`rejected_fmt_calls_a_pointer_method_of_a_struct_field_inside_a_box`). A method called after a
     type assertion is chosen only among the receivers the assertion admits, and a value method's
     receiver is the value one load past the data word, so a pointer the value holds is not counted
     as the box (`accepted_type_assertion_and_a_method_that_keeps_its_receiver`,
     `staysexact_fmt_and_a_value_that_holds_a_type_with_a_method_that_keeps_nil`). A `copy` that
     stores a view is reported at the call, as a store in the backing array of its destination
     (`rejected_copy_to_heap_slice`, `review3_programs_d13` to `d15`), and the error an `or` clause
     declares carries the views of its call's arguments, so the view is named in place of a format
     string (`review2_programs_f26`, `f27`). A pointer method that keeps a pointer it loads from its
     receiver keeps what the pointer points at, not the receiver
     (`accepted_printing_a_frame_value_while_an_allocator_whose_pointer_method_keeps_a_loaded_pointer_is_current`).
     The corpus-sized unit tests of escape2 and its EIR (eir TestBuildCorpus, escape2 TestAdversarial
     and TestGoldenMessages, which replaced TestMessageDiff's comparison with internal/escape) load
     each program in a `scratch.NewRegion()` released after it. Without generation checks that bounds
     them (TestBuildCorpus: the corpus in 200 MB). With them a Region was a custom allocator whose
     allocations were each registered (`rt.GenCustomAlloc`) and never ended, so TestBuildCorpus
     passed 20 GB 240 programs in. An address's tags only go up, so the region's addresses also ran
     out of tags and its Alloc returned nil. Now a Region, a home and the scratch levels are
     `pluto/arena` Chunked in a checked build: Release ends each allocation, a pointer kept past
     it panics where it is used, and memory stays flat over many regions
     (`compiler/internal/scratch` tests, run by TestCompilerUnits). The tests' knownfail skips are
     gone.
143. **An exit through a function that never returns** (done; section 17.3). A function whose body ends
     in a panic or a call that never returns counts as never returning, but `rt.ExitTask` before that
     point still ends the task through its return path. The caller skipped the exit check after such
     a call and ended the block in an illegal instruction, so the task crashed with SIGILL in place of
     running its caller's deferred calls and drops. The call now gets the check when the callee may
     exit, and the code after the check stays unreachable, as escape2's EIR has it. The panic handler
     is left out of the exit analysis, as its lowering already left it out. Cases
     `lower/goexitnoreturncallee` and `_o2`.
144. **One exit cleanup path per frame** (done; sections 17.3, 18.6). Every call that may exit got its
     own copy of the frame's exit cleanup: deferred calls, the scopes open there and the frame slots,
     so a function holding N owners and making N such calls had code quadratic in N. Now each such
     call records the releases its exit needs, in order, with what the move checker knows there, and
     jumps to landings built once the body is lowered (`exitpath.p`). A landing is one release followed
     by the landing of the rest, and exits whose remaining releases are the same share it, so the
     landings form a tree towards one return and the code is linear. A variable known to hold nothing
     still gets no release (section 18.6). The deferred calls still run first: in a function with
     defer statements every exit stores the number of its landing and jumps to one block that runs
     the armed deferred calls and then dispatches on that number. This needs no unsealed blocks, as
     everything is built at the end of the body. `big` in the generated probe (N owners, N calls
     that may exit) is now:

     | N   | -O0 before | -O0 after | -O2 before | -O2 after |
     |-----|-----------:|----------:|-----------:|----------:|
     | 25  | 8,066      | 3,416     | 14,864     | 7,756     |
     | 50  | 24,366     | 6,941     | 37,964     | 15,506    |
     | 100 | 81,341     | 13,991    | 78,133     | 31,006    |
     | 200 | 292,799    | 28,099    | 286,391    | 62,014    |

     The compiler itself has no calls that may exit: the old and the new compiler build the same
     compiler source into the same binary, byte for byte, in the same time (992 and 1,046 s of user
     time with the old, 1,219 and 983 s with the new, alternating in one job). The compiler is now
     56,141,904 bytes, from 56,042,744 at `d13d9cef` (the new lowering and three other commits).
     Exits from a range-over-func loop body and from generated code with fewer results keep their
     own copies, and an ordinary `return` still copies its deferred calls and scope releases. Cases
     `lower/goexitmanysites` and `_o2`.
145. **Instances of owner generic code keep their views** (done, `d301cd2d`; sections 8.7, 10.9, 12.10;
     found with item 132). In generic code over `[T owner any]`, a range over a `vec.Vec[T]` or the values
     of a `hash.Map[K, T]` yields views, `*T`, because `T` is an owner there (8.7, 12.10), and an instance
     keeps the meaning of the generic code (10.9). The library's iterators decide through `rt.View`, which
     collapses to the value for a value type. Lowering now makes an instance of generic code outside the
     standard library with each value type argument of an owner type parameter marked as a view argument
     (`types2.NewViewArg`): an alias of the type that is identical to it, except that `rt.View` of it
     stays a pointer. The mark goes with the substitution into the library code the instance calls, so
     `Vec[int].All` made for `Bump[int]` yields `*int`. Symbol names and type hashes add `·view` to a
     marked argument, so these instances are separate from the ordinary ones. The E2500 rejection and
     `types2.OwnerViewTypeParam` are gone. Cases `lower/generic_owner_range`, `_views`, `_index` and
     `lower/generic_owner_map_views` (map values, a nested generic call and a closure). The related gap
     is closed by the same mark: generic code over an owner `T` that calls a copy method such as `v.At`
     is E3306 for `F[int]` too (`diag/generic_owner_copy`). Symbol names of alias types now print the
     aliased type, which fixes a type assertion of a value of an alias type held in an interface: it
     failed because the type descriptor carried the alias name (`lower/alias_type_assert`).
     Follow-up (`2595613f`): itabs and their adapters were keyed by the plain type name, so
     `*vec.Vec[int·view]` and `*vec.Vec[int]` shared the adapter of `Values`. Generic code that called it
     through an interface got the plain instance's ints as pointers (a crash or a wrong sum, depending on
     which instance was lowered first). Assertions to interfaces failed or matched the wrong case: the
     two types share one type descriptor, so the run time cannot tell which itab a value needs. Itab and
     adapter names now carry `·view` (`viewTypeString`), and only plain itabs are listed in the run
     time's itab table. An assertion or interface conversion in code with view type arguments asks with
     a descriptor of its own for the interface (`assertTarget`), keyed by the interface and those
     arguments, and `finish` lists under it the itabs of each concrete type as that code sees it, with
     each type argument identical to a view argument marked (`withViews`). A value does not record which
     code made it, so such code sees a `vec.Vec[int]` made by plain code with views too, which is what
     its own instances do. Type switch cases on concrete types compare descriptors, since one type can
     now have several itabs for an interface. Method values and method expressions already used the
     marked instance names. Cases `lower/generic_owner_iface_views`, `_order`,
     `lower/generic_owner_iface_assert`, `_order`, `lower/generic_owner_iface_concrete` and
     `lower/generic_owner_method_value_views` (a method value, a method expression and a type switch).


The next work is in the order of work at the top of this document. Item 18's step A is done: with
`--gencheck=false` tasks get proven stacks. What is left of it (checked, kernel, freestanding and
custom-scheduler builds, and the unverified witness pairs of the resolution record) is item 188.
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
| 34 | Ordinary kernel lifetime protection (in progress) | 101 | 1.2, 18.11, 21.5 |
| 35 | Outstanding aliases across owner transfer (done) | 102 | 8.8, 19.3 |
| 36 | Nonconsuming owner indexing/length receivers (done) | 103 | 11.11, 13.6 |
| 37 | Bounded task-stack inference (done for `--gencheck=false`; the rest is item 188) | 18, 188 | 19.1 |
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
   in regions E3100 to E3102), by a whole-program analysis with explanatory messages. That analysis is now
   `internal/escape2`; item 142 records the change.
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

146. **Views of a local with cleanup end at its scope** (done, `db734f44`; section 18.3, D42, finding A1 of the
    [rulings](verification/2026-11-09-spec-rulings.md)): a view of a local whose type has drop glue (a
    string, box, map, channel, allocated error, or a struct, array or union holding one), the variable
    itself included, used after the end of the block that declares it is E3313, also when a `break`,
    `continue` or `goto` leaves the block; the catalog text of E3313 covers every such local, not only
    owners. A local of the function's top block, and one with no drop glue, is bounded by the frame alone.
    A function that calls a func value it was given (D49) keeps of an argument what the function its
    caller passes there keeps: the move checker's call facts read the summary of a declared function or a
    literal without captures passed for it, so `run(f, i, &out)` from Go's range test keeps nothing of
    `out` when `f` keeps nothing. Tests: `tests/diag/stale_scope_views` and
    `tests/moves/accepted_views_within_their_scope`.
147. **Owner conversions and literals stay in the defining package** (done, `ee470311`; sections 7.13, 7.18, 8.11,
    11.3, 11.24, D43, finding C1): E3212 for conversions to an owner type, a pointer to one or an owner
    union outside its package, for pointer conversions from `*Owner`, and for literals of an owner struct
    with an unexported field; widen the catalog text. `ownerConversion` in `types2/plutonium_owner.p`
    applies rules 1 and 3 of 11.24 (rule 3 also in the package of an operand type defined from the
    target) and the pointer rule (both directions), and `ownerLiteral` checks keyed and unkeyed literals;
    an empty literal `T{}` stays allowed, as it is the zero value. Converting a value to an owning
    interface is not a conversion to an owner type and stays allowed. Tests: `tests/types2/ownerconversion`
    (accepted forms), `ownerconversion_forms` and `ownerconversion_literal`.
200. **Owner conversions through a type defined from another owner** (done, `5c00ed4c`; sections 11.24, 18.5, 8.11,
    7.18, D72, finding E2 of audit E; extends item 147): for `type T owner A`, `T(x)` and the pointer and
    union forms are E3212 unless converting `x` to `A` is allowed too; `T(a)` with `a` of type `A` stays
    allowed. Tests: `tests/types2/ownerconversion_defined` and `ownerconversion_defined_ok`; the 11.24
    `package q` spec example builds, with `together`.
152. **Arenas count their live counted objects** (done, `e14194a6`, with item 198; sections 18.8, 18.9, 17.1, F.2, abi.md 1.3,
    D45, finding B1): one atomic counter per Fixed and Chunked arena, raised at each counted allocation
    and lowered at its last release; `Reset`, `Release` and `Drop` panic with "arena has live counted
    objects" while it is not zero. Item 198 widens it to every allocation and renames the message.
    Tests: see item 198.
198. **Arenas count every live allocation** (done, `e14194a6`; sections 18.8, 18.9, 13.7, 13.9, 16.7, 17.1, 17.6,
    21.5, F.2, abi.md 1.3, memory-model.md 3.8, D70, finding E3 and E9 of audit E; extends item 152): the
    counter of item 152 rises at every `Alloc` of a Fixed or Chunked arena, owners included, and falls at
    every `Free`; only `New` and `Slice` views stay out of it. The panic message is "arena has live
    allocations". The compiler's own arenas must then free everything before they reset:
    `compiler/internal/scratch/mem_on.p` calls `Chunked.Reset` on a Region and a home that may still
    hold vectors, maps and strings, so check each `Reset` and `Release` there and in the library before
    the count lands. The compiler's scratch memory never frees, so it uses the new
    `arena.NewChunkedUnsafe`, a Chunked with no count, which the audit lists as unsafe; `pluto/flag`
    allocates its variables as views. Tests: `TestArenaLiveAllocations` (`tests/build_arena_live_test.go`).
202. **Arenas run drop entries before they check the count** (done, `78e261c7`; sections 18.9, 18.6, 18.8, 18.10,
    13.7, 13.9, 16.7, 17.1, 21.5, F.2, abi.md 1.3, memory-model.md 3.8, D74, finding F1 of audit F;
    extends item 198): `New[T]()` and `Slice[T](n)` of a Fixed or Chunked arena record a drop entry
    (address, element count, drop-glue function) when `T` has drop glue, in the Fixed bookkeeping table
    or in blocks taken from Chunked chunks; a full table fails the allocation. `Reset`, `Drop`, and
    `Release` for the entries after the mark, run the entries in reverse order, then check the count, then
    zero the granules and end the epoch. A panicking entry leaves the rest recorded. `flag.FlagSet` no
    longer releases its strings by hand. Tests: `TestArenaDropEntries` (`tests/build_arena_entries_test.go`).
203. **Plain arena counter where no other task can reach** (planned, optimization; sections 21.5, 18.9,
    abi.md 1.3, D77, findings F4 of audit F and G6 of audit G; extends item 198): use plain adds for the
    live-allocation count where the compiler proves that no other task or interrupt handler can reach the
    arena or anything allocated from it.
209. **The checks follow arena drop entries** (planned; sections 18.9, 18.3, 19.3, 20.10, 17.1, abi.md 1.3,
    D82, findings G2, G8 and G13 of audit G; extends item 202): E2584, the race check (frees are writes,
    E3501, E3404) and the view rules (E3313) follow a `Reset`, `Release` or `Drop` of a Fixed or Chunked
    arena into the drop glue of every type passed to `New` or `Slice` of that arena, or of every arena
    where the compiler cannot tell which. A `Slice` of length zero records no entry. The count check after
    the entries panics only for an allocation still live then, or an entry recorded while they ran.
    Item 212 widens the race check and E3313 to every object the arena takes back, of every type.
212. **Arena frees of every type** (in progress, `208b5343`: generic allocations for E3501 only; sections 19.3 (Frees are writes), 19.4, 18.3, 18.9, 12.12, memory-model.md 3.8
    and 4.1, D86 and D93, findings H1 and H8 of audit H; extends items 168 and 209): a `Reset`, `Release`
    or `Drop` of an arena is a free, and so a write, of every object it takes back, of every type, for
    E3501, E3404 and the E3313 events, not only of objects whose type has drop glue. `Release` counts
    every object of the arena, since the compiler cannot order objects against marks. An arena is named
    by its variable or field, as a lock is: `workers[i].arena` counts as every arena it may be, and an
    arena behind a parameter or a heap pointer stays item 196.
214. **A load of a view from shared memory is a crossing** (in progress, `d2a9aee9`, `HASH214`: not a use of a view after a call that may release its lock, the 19.3 peekWait example; phase 1, wrong acceptance; sections
    19.2, 19.3, 8.8, 20.9, 20.10, A, memory-model.md 4.1, D96, D102 to D104, finding I1 of audit I and
    findings J1 to J3 of audit J; extends items 168 and 212): a
    task's or interrupt handler's load of a pointer, slice, interface, func value or method value (or an
    aggregate that holds one) from a package variable, a per-CPU variable, the object of a `synced[T]` or a
    map, or from memory reached through them, crosses to that party, in its own code or a callee, atomic
    loads included (19.3, Crossings); rule 2 (what may cross) then checks the memory (E3501). Rule 1
    (shared data) and rule 2 count the memory a datum owns as part of it (19.3, Memory and data): of a
    package variable, and of a field of a static object, a `synced[T]` object or an object whose type
    satisfies rule 3 (the type rule), to any depth (D110, finding K2 of audit K). A package variable that holds a `shared[T]` used by two tasks is E2010. So
    `p = &*b; task reader(); b = new(Node)` is E3501. Audit J adds three parts (D102 to D104, findings J1
    to J3). A view of memory that a package variable or a `synced[T]` object owns crosses when every access
    to that memory holds its inferred guard and the view is used only where a guard is held; a use past
    the `Unlock` (returned after the callee unlocks, stored outside memory the guard guards, sent, passed
    to a task, captured, held in a method value, or deferred past the release) is E3501, so a package-level
    `vec.Vec` under a mutex compiles. A static object (a composite literal whose address a package-level
    initializer takes, never moved, dropped or overwritten after tasks start, with `take(p)` and `*p = v`
    named by where `p` came from) meets rule 2 case 3 with the field conditions and without the free
    condition, so `fmt.Println` from any task compiles while other `File`s are dropped; an atomic field
    makes it case 3, not case 2 (finding K11). A handler's load crosses from the start of sharing (19.3,
    The start of sharing). Spec examples: the `type Node struct{ n int }`, "first lets a pointer" and
    `type Logger owner struct {` blocks of 19.3.
217. **Arena names beyond the precision limits and through a move** (planned, phase 1; section 19.3 (Frees are writes; How precise the race check is), D100,
    finding I7 of audit I; extends item 212): a call on an arena that the 18.3 limits cannot name counts as
    that call on every arena of the program, and an arena's name follows a move (`b := take(a)`).
158. **Task exit mid-statement and inside `Drop`** (done, `6fc4a533`, tests only; section 17.3, abi.md 12.10, D51, finding B8):
    `rt.ExitTask` drops operands already evaluated, and a `Drop` method it leaves still finishes the
    object's field drops and free. Tests: `TestTaskExitDrops` (`tests/build_task_exit_drops_test.go`),
    on `sync.Scope` tasks, with a twin that returns.
159. **Count before release in `copy` and borrowed parameters** (done, `6d2ae5f5`, tests only; sections 13.2, 18.8, abi.md 3.3,
    findings B10 and B18): check that `copy` counts the source before it releases the destination, and
    that a first assignment to a borrowed string parameter releases nothing. Tests: `TestCountBeforeRelease`
    (`tests/build_count_order_test.go`).
160. **Variadic arrays release their counted elements** (done, `d725d296`, tests only; sections 18.8, D76, finding C7): the array that
    holds a call's variadic arguments is frame storage whose counted elements are released at the end of
    the statement. Built to 18.8 and D76: the elements are released when the function returns, after its
    deferred calls, or when the same expression refills the array. Tests: `TestVariadicArrayRelease`
    (`tests/build_variadic_release_test.go`).
206. **Frame storage in loops** (done; sections 18.4, 11.3, 11.15, A.3, D76, finding F3 of audit F;
    extends item 160): E3006 for a view of an `&T{...}` object, a slice literal's array, a variadic array
    or a method value's receiver kept where the next iteration can see it. The message for an `&T{...}`
    gives `new(T{...})` as a fix, and for a slice literal or a variadic array an owner such as a `vec.Vec`
    (`loopDiag` in `compiler/internal/escape2/report/text.p`). A deferred call's saved arguments cannot be
    kept past an iteration, since `defer` in a loop body is already E2530 (D18). Tests:
    `tests/escape/rejected_loop_objects_kept_past_the_iteration` and
    `accepted_loop_objects_made_with_new_or_copied`.
207. **Codes for a `//pu:stack` size out of range and for implementation limits** (done, `2876a166`; sections
    20.3, 21.4, 21.1, D78, findings F12 and F15 of audit F): assign a code to each and add catalog entries;
    the spec names them only as compile errors until then.
162. **Explicit nil checks in freestanding builds outside kernel mode** (done, `a57c70c6`; section 17.5, D56,
    finding B21): one compare and branch before each dereference not proved non-nil.
165. **Library invariants at panicking checks** (done, `96d2496b`; sections 17.2, 18.12, D53, finding B9):
    review `vec`, `hash`, `strings` and `bytes` so each type restores its invariants before every check that
    can panic.

168. **Frees count as writes in the race check** (done, `70b8df4b`; sections 19.3 (Frees are writes), 19.4, D59, finding D1): dropping,
    freeing or moving an object, or writing an owner that holds it, is a write of the whole object for E3501
    and E3404.
201. **Deferred calls in a scoped interval** (done; section 19.4, A.3, D73, finding E4 of audit E; extends
    item 168): a deferred call armed on a path that then reaches a point of a scoped task's interval, or a
    return before its join, counts as a statement of that interval for E3402 and E3404, in every build
    mode. A deferred `s.Wait()` joins the tasks started after its `defer` statement for the calls deferred
    before it. Done: the race check already ran deferred calls at each exit; a deferred `x.Drop()` is now
    a drop of `x` there (`compiler/internal/race/call.p`), and the callee's own reads of `x` are part of
    that drop, not separate accesses (`dropCalls` in `scope.p`). Tests:
    `tests/race/scoperejected_a_deferred_drop_before_the_join` and `scopeaccepted_a_deferred_drop_after_the_join`.
205. **The deadlock report names a panic in progress** (done, `584c36a9`; sections 19.1, 19.4, debugging.md 5,
    D79, finding F9 of audit F; extends item 201): a task running deferred calls for a panic when the
    program deadlocks, as a deferred `s.Wait()` can, is listed with a `panicking:` line, the panic's
    message and its trace.
169. **Closure regions own captured variables** (planned; sections 11.5, 18.8, abi.md 4.1, D60, finding D2):
    the region's drop function runs owner drop glue for every captured variable with cleanup, and copies
    parameters into the region on entry.
170. **E3313 at calls that may free** (done, `7acbe902`; sections 18.3, 7.17, 7.18, 7.19, D61, finding D3 and
    candidates 7.17 to 7.19): the move checker's summaries (`compiler/internal/moves/frees.p`, over every
    package, to a fixed point like `keeps.p`) record through which inputs a function may free memory or
    move an owner out: an assignment that replaces an owner or counted box through a pointer, `take`,
    `clear` and `delete` of such elements, an allocator's `Free`, an owner's `Drop`, a ring's `Pop`,
    `Reset` and `Drop`, `rt.ReleaseValues` and `MoveValues` (the source, unless a `RetainValues` of the
    same destination and count follows it: the pair copies, as `vec.Append` does), and calls that pass
    the input on. A result that cannot hold a view, such as the `bool` of `p, ok := m.Upsert(k)`, gets
    none of the call's views. A call that
    passes a local owner, or memory it owns, to such an input ends the views obtained from it (E3313),
    all but `&v` itself; a call through a counted
    reference ends the views obtained through every other local reference of the same box type; `(*T)(h)`
    for an owner defined from a pointer type, and for a box, is a view bounded by the variable, as are
    `&*b` and a box passed where a `*T` is wanted, a pointer receiver among them. A call
    through the address of a local owner ends that owner's views, not those of a variable the owner holds
    a view of, such as an allocator lent to it (the example of section 18.13). A method called through an
    ordinary interface loaded through an input frees nothing of that input's memory (section 7.9: the
    interface lends), so an arena's parent allocator does not end the arena's views, and the views of a
    checked arena's allocations end only at its `Reset`, `Release` and drop (section 18.9). The result of a
    function given an owner as an ordinary interface is a view of the owner only when a result of one of
    the interface's methods may hold what the owner holds, so `ReadAll(&f)` through an `io.Reader` is not
    ended by `f.Close()`. `pluto/flag` keeps
    its flags and string variables in lists in its arena, so defining flags and parsing free nothing and
    the variables `Int`, `String` and the like return stay usable. The limits are item 237's.
    Tests:
    `tests/moves/staleviews_call_that_may_free` and `accepted_views_across_calls_that_free_nothing`.
    Closed later: a free through a call's result (`42de5711`); a call of an unknown func value or of a function
    without a body or contract entry is the worst case, with the runtime's scheduler hooks other than
    `schedFreeTask` known to free nothing (`630edb01`); an arena reset or released through a pointer or a
    function given it (`bf013a88`); a call through an owner's address ends the views of other locals that its
    fields hold (`c009a291`); a map or channel lent to an interface ends at a new value of its variable
    (`05157463`); a view asserted out of an interface parameter under C2 (`1186999b`); a free while a view of
    the owner's heap memory is stored where the walk cannot follow is E3314 (`19e3a8ec`). Tests:
    `tests/moves/staleviews_free_through_*`, `staleviews_arena_reclaimed_through_a_call`,
    `staleviews_map_lent_to_an_interface`, `staleviews_view_asserted_out_of_an_interface_parameter`,
    `rejected_free_while_a_view_is_kept_in_the_heap` and their `accepted_` twins.
199. **One-word owners lent to an interface end at the D3 events** (done, `2343726a`; sections 18.3, 18.5, 7.9,
    11.13, 12.9, A.3, D71, findings E1, E5 and E7 of audit E; extends items 170, 171 and 148): an ordinary
    interface holding a box, a map, a channel, a `weak` reference, an owner defined from a pointer or a
    one-field struct of one is a view obtained from the variable, so a move, drop or new value, a call that
    may free or move out, or an owner field assignment on its path ends it (E3313). The result of an assertion from an owning interface to an ordinary interface is such a
    view of the asserted variable, for every payload size; a type-switch clause naming an ordinary
    interface makes the same view, and is tested once item 148 stops it moving. Function literals that hold
    such views follow the same events. An owner larger than one word lent to an interface stays a view of
    the variable itself. A map or channel lent to an interface is not followed yet: the move checker bounds
    views by owners and counted boxes only (item 237). Tests: `tests/moves/staleviews_one_word_owner_lent_to_an_interface` and
    `accepted_large_owner_lent_to_an_interface_across_a_call`.
237. **The freeing events items 170 and 199 leave** (done; sections 18.3, 7.9, 7.19; moved from items 170
    and 199): a call through a copy of a counted reference held in a field or parameter now ends the views
    obtained through a local reference of the same type (`countedReached` in `frees.p`); a method called
    through an ordinary interface is a callee the summaries do not know, so it may free what the input
    holds; a view of what a local owner holds kept where the walk cannot follow is E3314 at the owner's
    drop, at the end of its block and when it is given a new value, as at a free; and memory loaded
    through such a view counts as the view when the loaded value may itself free memory (an element that
    is an owner, a box, a map or a channel: `loadFrees`, `lowns`). A view a callee only returns is
    followed in its result rather than pinned. Tests: `tests/moves/rejected_free_through_a_copy_of_a_counted_reference`,
    `rejected_free_through_an_interface_in_a_field`, `rejected_block_end_while_a_view_is_kept_in_the_heap`,
    `rejected_free_after_an_owned_element_is_stored` and `accepted_frees_twins_of_item_237`.
204. **Type-parameter conversions to an interface in generic code** (done; sections 18.3, A.3, D75,
    finding F2 of audit F; extends item 199): a conversion of a value of a type parameter to an interface
    is a frame copy for every instantiation, and for `[T owner any]` also a view that the D3 events end
    (E3313, E3314), checked once for the generic body. Both halves were in place; this adds the tests:
    `tests/moves/rejected_owner_type_parameter_converted_to_an_interface` (a new value, a move and the
    end of the block) and `accepted_owner_type_parameter_converted_to_an_interface`.
208. **Generic code takes the strictest rule any type argument needs** (done; sections 18.3, 8.7, A,
    D81, finding G1 of audit G; extends item 204): the view half of item 204 applies to every type
    parameter, `[T any]` and `[T ~int]` included, so `x := any(t); t = z; use(x)` is E3313 in a generic body; a `T` local
    counts as having drop glue unless no type in its type set has it (the scope rule of E3313, the
    declaration order rule and the loop rules); a view obtained from a `T` value is bounded by its holder
    unless every type in the set is a value or a view (E3313, E3314, E3009); and a method call on a type
    parameter is checked against that method of every instantiating type. Done: `tpBounds`, `tpMay` and
    `tpTerms` in `compiler/internal/moves/views.p` read the type set from the constraint's terms, and a
    type parameter whose set is not limited to values and views is bounded and has drop glue. The result
    of a method called on a value of such a type parameter is a view of its variable (`tpRecvViews`), as a
    counted box's `Slice` would be. An element assignment in a generic body whose place has a type
    parameter's type frees through the slice when a type argument the program gives that parameter
    holds an owner or a counted box, followed through generic code that passes its own type parameter
    on (`freeingStore` and `tpFrees` in `frees.p`); an assignment that only exchanges places
    (`a[i], a[j] = a[j], a[i]`, `permutes` in `frees.p`) frees nothing, so a sort keeps a view usable.
    `inspectList` in `compiler/internal/syntax/ast/walk.p` indexes its list instead of converting the
    range variable, which the rule now rejects. Tests: `tests/moves/rejected_view_of_a_type_parameter_value`
    and `accepted_view_of_a_type_parameter_of_values`, `rejected_generic_store_may_free`,
    `accepted_generic_store_of_values_frees_nothing` and `accepted_generic_swap_frees_nothing`; `tests/escape/review2_programs_u12_generic_union_switch` gets the
    E3314 its `[T any]` binding stored in a package variable now needs.
173. **Range loops hold a count** (done, `47a958a4`; section 12.10, D64, finding D7): the range temporary over a counted
    value holds its own count until the loop ends. Maps, arrays and channels already did; a range over a
    string now holds an owned count (`rangeString` in `compiler/internal/lower/stmt.p`). Tests:
    `TestRangeHoldsCount` (`tests/build_range_hold_test.go`).
174. **Owners in for clauses** (done; sections 12.10, 18.6, D65, finding D8): an owner declared in a for
    clause moves between iterations and is dropped once. The checker and lowering already did this; the
    item adds the tests: `tests/moves/accepted_owner_in_a_for_clause_moves_between_iterations` (a run test
    of the drops) and `rejected_owner_in_a_for_clause_moved_and_not_replaced`.
177. **A `%w` wrapper of a field error is bounded by the object** (planned, check only; sections 16.5, 16.7
    rule 4, 18.3, D69, finding B5): `fmt.Errorf("...: %w", err)` of an error in a field of an owner keeps a
    pointer into the owner. The analysis already bounds `Errorf`'s result by its `%w` operand for a frame
    `ErrorBuf` (`tests/testdata/escape/review4/e03_errorbuf_wrapped_by_errorf.p`, `e04_errorbuf_joined.p`).
    Add tests for a wrapper of a field error of a local owner: used after the owner is moved or dropped is
    E3313, returned past the owner is E3001, and stored in the heap is E3002. Fix the analysis if one is
    accepted.
178. **A consuming method used as a method value is an error** (done, `4fd02203`; sections 11.9, 18.5, G26
    decision (a); moved from items 20 and 75): `done := c.Close` for a method with an owner value
    receiver is error E3315 (ConsumingMethodValue), promoted methods included. The method expression
    `Conn.Close` and the call `c.Close()` still work. Tests: `tests/moves/rejected_method_value_of_a_consuming_method`
    and `accepted_consuming_method_called_or_named_by_type`.
179. **The view-tracking cases item 138 left** (planned; section 18.3; moved from item 138): heap or caller
    memory a method of a local owner returns is taken to be the owner's, so a view of the owner stored
    into unrelated memory reached that way is not caught, and a pointer to caller memory so treated loses
    its tie to the caller when the owner moves. A view kept in a local owner that is moved and then
    stored in a package variable is not caught. A view of a table a map walk retired may outlive the walk
    while the map lives.
180. **The escape contract of `rt.ReleaseValues` and `ZeroValues`** (done, `10086fdc`; sections 18.3, 18.6;
    found by the third adversarial review, program `review3/o14_drop_in_vec_of_owners_local`): the
    escape analysis adds, after `rt.ReleaseValues(p, n)`, the drop of a `T` at `p`, so a `Drop` that keeps
    its receiver is E3003 for elements in the frame. `ZeroValues` writes zeros and drops nothing, so it
    keeps no effect. o14 stays in `knownEscapeGaps`, with a new reason: `vec` now drops its elements with
    `clear`, the elements are in the heap, where a kept receiver is the generation checks' case, and the
    one escape error the program gets is in `vec.p` (`AppendSeq`'s local), not in `main.p`. Tests:
    `tests/escape/release_values_drops_frame_elements` and `accepted_release_values_keeping_no_frame_address`.
    Closed later (`36e91cc0`): a `Drop` that keeps its receiver is E3002 at the `Drop` itself, for every owner
    (section 18.6), so o14 left `knownEscapeGaps`. Tests: `tests/escape/rejected_drop_keeps_its_receiver` and
    `accepted_drop_keeps_a_copy`.
196. **Allocators the race check does not follow** (planned; sections 19.4, 18.9; moved from item 140): an
    allocator reached other than through the variable itself or a field holding its address (one behind
    a parameter or a heap pointer, `alloc.Use` windows), and bulk calls seen by the race checks outside
    scoped tasks.
211. **A use before a move in one call** (done, `584c7ed5`; sections 11.14, 11.26, 18.5, E3301; extends item
    75): a use of a variable in the same expression or simple statement as its move is E3301 in either
    order: call arguments, receiver and argument, operands, composite literal fields, index and slice
    expressions, a select send case and the two sides of an assignment, and a use inside a function
    literal. `v = grow(v)` and `a, b = b, a` stay allowed (`compiler/internal/moves/oneexpr.p`). Tests:
    `tests/moves/rejected_move_and_use_in_one_expression` and
    `accepted_move_and_assign_back_in_one_statement`.

## Gaps the integrated specification defines (complete-spec section 6.5)

73. **Order of evaluation** (done, gap 1, `f74c4a4`; section 11.26): calls, method calls, receives and
    logical operations are evaluated first, left to right, then the other operands, so `[]int{a, f()}` with
    an `f` that changes `a` gives `[2 2]`. The survey of the rest of lowering is item 183.
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
    Tuning is item 195. Dense reads cost 16.498 ns versus prior checked 0.809 ns; consumed 1024-byte
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

153. **Fixed arena bookkeeping from its own backing** (done, `b9f4672f`; section 18.9, F.2, abi.md 1.3, D54, finding
    B2 of the [rulings](verification/2026-11-09-spec-rulings.md)): the table is sized from the arena's backing at creation; allocation fails when it is
    full, and the parent is never asked again. The table follows the payload in one parent request, with
    one record and one drop entry per granule. Tests: `TestArenaFixedTable`
    (`tests/build_arena_fixed_table_test.go`).
154. **Arenas free in bulk only** (done, `436842a9`, audit goldens `fb97db78`; section 18.9, F.2, abi.md 1.3, D46, finding B3): `Free` through
    an arena reclaims nothing; remove the "arena allocation freed" panic and its epoch check.
    `rt.GenTrimArena` is gone, and the manual `Arena.Free` does nothing. Tests: `TestArenaBulkFree`
    (`tests/build_arena_bulk_free_test.go`), and the partial tail cases of
    `TestGenerationDirectoryComposition`.
155. **Gate the cleanup write check on a flag** (done, `675daa0a`; sections 15.4, 21.5, abi.md 12.10, D55, finding
    B4): the store check runs only once package cleanup has started. The flag is `rt.cleanupStarted`, and
    unchecked mode keeps the check. Tests: `TestCleanupStartedGate` (`tests/build_cleanup_started_test.go`).

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
    DWARF variable and type information is item 189.
54. **Compile-time panic handler, `//pu:panic`** (done, `1fd3e85`; section 17.4): one handler per
    freestanding or kernel program, linked as a direct call from the runtime and checked never to return.
55. **Interrupt context cannot wait** (done, `1fd3e85`, `662497a`; section 20.10): a whole-program check
    from every interrupt handler and the panic handler against blocking operations, conservative through
    interfaces and func values.

161. **Safe x86 intrinsics** (done, `7bdc1b25`; section 18.12, D56, finding B14 of the
    [rulings](verification/2026-11-09-spec-rulings.md)): `pluto vet -unsafe` stops listing `x86.Pause`,
    `Cpuid`, `Rdtsc` and `Rdtscp`, and lists `//pu:export` functions (D50).
163. **A handler's print does not wait on its own processor** (done, `9d288d6a`; sections 13.12, 20.10, D58,
    finding B15): the console lock records its holder's processor, and an exception or NMI handler that finds
    its own processor holding it drops its output.

## Concurrency

14. **Tasks and the hosted scheduler** (done, `0ebe75d`, `1976415`, `d40ae60`, `ec9d6dc`, `add8fb6`;
    sections 19.1, 19.5 and 12.12, abi.md section 12): the task mechanism, fixed stacks with guard pages,
    a work-stealing scheduler with preemption and system-call hand-off, an epoll poller, timers, and
    deadlock detection that lists every task. The sendable rules are checked (section 19.2). The
    follow-ups are item 194.
15. **Channels and `select`** (done, `0ebe75d`; sections 7.11, 12.13 and 19.8): counted references
    (item 67), `make(chan T, n, a)`.
16. **`pluto/sync`** (done, `8fd524c`; section F.5, memory-model.md section 3.4): `Mutex`, `RWMutex`,
    `WaitGroup`, `Once`, `Cond` over the scheduler.
17. **Thread-safe default allocator** (done, `0ebe75d`; sections 18.9 and 19.7): `alloc.SyncSlab` with
    per-processor caches is the hosted default.
18. **Stack size analysis and `--stackreport`** (done, `d11cb89e`, `17e3bc1c`; section 19.1
    item 1): each task statement loads its `Proven` bound from a read-only word, `<thunk>.stack`, that
    the backend fills after it has laid out every frame. The bound is the deepest call path from the
    task's function, plus the frames that land on the task's stack outside the call graph: the
    preemption frame and `preemptPark`, the fault frame and `raiseFault`, and 256 bytes of slack. It
    is rounded up to 16 and is at least 8 KiB. The walk counts each function's final frame and its
    direct calls, inlined bodies, defer runners and the indirect calls that lowering resolved to a
    closed set of callees; assembly functions are scanned for pushes and frame adjustments, and the
    vDSO is assumed to need 4 KiB. A task is not computed, and keeps the hosted 64 MiB stack, when its
    graph has a cycle (the reason names it), reaches a body whose stack use is unknown, or makes an
    indirect call with an open callee set; kernel, freestanding and custom-scheduler builds compute
    no bounds. `pluto build --stackreport` prints one line per task statement. The spec said
    `-stackreport`; the flag is `--stackreport`, and the spec now says so.
    With `--gencheck=false` the runtime's call graph has no cycles, so a task that calls nothing
    unbounded is proven at every optimization level (step A). The cycles it broke, each without
    changing what the runtime does: the panic printers' bounds checks, which panicked again (the
    printers and formatters are `//pu:nochecks`, and `decodeRune`, `hasPrefix` and `hasSuffix`
    read through pointers checked by hand); `printMessages`, recursive on `During`, now a loop; the
    counted strings on the panic path, whose release can free and so fail (the message is written
    from its words, a signal's name by `writeSignal`, and the trace code holds the table's strings
    as `traceStr`, not `string`); `ifaceEqual` through the allocator's own equality (the default
    allocator's cache compares an `Allocator`'s words); the hosted scheduler's switches, whose
    finishing could free stacks and wake tasks under locks that may yield (`switchToSched` runs only
    the scheduler's half). Lowering makes a constant condition a plain jump and registers no frame
    slot in unreachable code, so a panic under a false build constant leaves no edge. Checked builds
    stay not computed: `genLive` follows a record's chain of parents recursively, as deep as
    allocators nest. The compiler itself starts no tasks. Checked, kernel, freestanding and
    custom-scheduler builds are item 188.
53. **Scheduler contract for kernels** (done, `7d4cf29`, `d81508d`, `5001093`; section 19.5, abi.md
    section 12, [plan](plans/2026-10-09-scheduler-contract.md)): permit-based park and ready,
    interrupt-safe wake, backend task records, a current-task slot, `rt.Preempt`, link-time binding with
    `//pu:scheduler`, optional timers (E2595). A test kernel with timer preemption runs under QEMU.
63. **`sync.Scope`** (done, `a7948f6`, `f23c29c`, `8444dae`; sections 19.4 and 15.4, abi.md section 12.10,
    [plan](plans/2026-10-10-scoped-puroutines.md)): an owner whose drop waits, with `Task`, `Wait` and
    `Drop`; tasks started on a local `Scope` use the starting function's variables, checked for races
    (`internal/scoped`, `internal/race`). Phase 2 of the plan is item 193.
66. **Large task stacks** (done, `173c2f6`; section 19.1, abi.md sections 12.1 and 12.7): 64 MiB lazily
    mapped hosted stacks, cached.
67. **Counted channels, and a channel owns its timer** (done, `bdb7683`; sections 7.11, 19.6, 19.8, abi.md
    section 12.6): a channel is a counted reference freed with its last reference. `time.NewTimer`,
    `NewTicker` and `AfterFunc` return `owned[Timer]` and `owned[Ticker]`, which stop and free the timer
    when dropped (section F.7, gap 19); nothing is freed by hand.

156. **Interrupt handlers as race parties** (planned; section 19.3, memory-model.md 3.1, 4.1, 6.1, D52,
    finding B6 of the [rulings](verification/2026-11-09-spec-rulings.md)): the race check treats handlers as parties from the first statement that may
    install one or enable interrupts, and does not require locks for data no one writes while a handler
    may run.
210. **No waiting in a preemption-disabled region** (planned; sections 19.3, 19.1, 17.1, 20.10, abi.md 12.4,
    D85, finding G5 of audit G; extends items 120 and 156): from each non-migrating region (between
    `rt.DisablePreemption` and `rt.EnablePreemption`, while an `rt.Lock` is held, or with interrupts
    disabled), follow every call with a single known target (item 215) and reject a path to an operation that
    may park, naming each call.
    An interface or func-value call where only some candidates wait, and an allocation or drop where only
    some installed allocators wait, compile; the run-time panic "blocking with preemption disabled"
    catches them (D87, finding H2 of audit H). Regions in the code of `pluto/rt` are not checked (D89,
    finding H4). The diagnostic needs a code (E2584 is defined for interrupt handlers). Item 213 adds
    the kernel-mode interrupt test to the backstop.
213. **`rt.Park` tests the interrupt flag in kernel mode** (planned; sections 19.3, 19.1, 17.1, abi.md
    12.4, D88, finding H3 of audit H; extends item 210): in kernel mode, `park` reads RFLAGS on entry,
    before it takes any runtime lock, and panics with "blocking with preemption disabled" when IF is
    clear. The switch inside the scheduler's `Park`, and `Yield`, `Preempt` and `preemptPark`, do not
    call `park` and are not tested. Hosted builds have no such regions; other freestanding builds do not
    test the flag.
215. **The wait check follows calls with a single known target** (planned; sections 19.3 (Waiting in a non-migrating region), 19.1, 17.1,
    20.10, D97, finding I2 of audit I; extends items 210 and 213): from a non-migrating region, follow
    direct calls, method calls on a concrete type, deferred calls of those, function literals called where
    written, and inserted drops of known types with the drop entries of a named arena, through any depth.
    Report a wait every path to which lies inside the region, including one on only some paths of a
    followed callee. Do not follow interface, func-value, method-value or hook calls, an arena that cannot
    be named, or allocation and frees into installed allocators.
216. **`//pu:blocking` functions test at entry** (planned; sections 19.3 (Waiting in a non-migrating region), 19.1, 17.1, 20.3, 20.10, abi.md
    12.4, D98, finding I3 of audit I; extends items 210 and 213): insert the test `park` makes
    (preemption count; in kernel mode RFLAGS.IF) after the prologue of every function marked
    `//pu:blocking`, and before each call of one without a body, panicking with "blocking with preemption
    disabled".
218. **Lazy initialization through `sync.Once`** (done, `5681d379`, except views carried by an exported function's parameters; phase 1; sections 19.3 (Lazy initialization), 19.7, F.5,
    memory-model.md 3.4 and 4.1, D105, the "Once and Cond" section of the
    [rulings](verification/2026-11-09-spec-rulings.md)): data written only during a call of a function
    passed to `Do` of one Once (a package-level Once for package variables, a Once field for the fields of
    the same object), and read only where a `Do` on that Once has returned on every path, needs no lock.
    The race check must follow the call of `f` that `Do` makes, count writes in `f` and its callees on that
    path as inside, find a returned `Do` on every path to each read, and fall back to rule 1 (E3502,
    E3503) otherwise. A `Do` counts in code that is part of the enclosing function (a literal called in
    place, a deferred literal, a range body on the caller's task) and through a helper whose summary
    shows a `Do` on every path, at any depth (D112, findings K9 and K13 of audit K). Rule 2 cases 2 and 3
    and rule 3 (the type rule) accept Once-initialized memory. Today the race check has no rule for
    `sync.Once`: it follows `Once.Do` as ordinary code, so the writes inside `f` hold the Once's own
    mutex, a read after `Do` is E3503, and a write outside `f` is an error only when it holds no lock
    that the writes inside hold. It does not yet reject writes outside the Once's function as such.
    Note for spec examples: the race check follows tasks only from package initialization and `main`,
    and the test adds an empty `main` to a block that has none, so a block whose tasks start in an
    uncalled function is never race-checked. Library:
    `Once.doSlow` releases its lock when `f` ends its task with `rt.ExitTask` and sets `done` only after
    `f` returns, so such an `f` leaves the Once not done instead of locked for good. A panic in `f` must
    keep the lock while the panicking task's deferred calls run (D128, finding N2 of audit N), so the
    release cannot be a plain deferred `Unlock`: catch the task's end (`rt.CatchExitTask`) or test for a
    panic in progress. A deferred read counts at its `defer` statement and every point after it (item
    236). Spec
    example: the `type Config struct{ port int }` and `type Limits struct{ max int }` blocks of 19.3,
    which now start their tasks from `main`.
219. **`sync.Cond.Wait` in the guard inference** (done, `14dd6d35`; sections 19.3 (Held sets; Views loaded under a lock), 19.7, F.5,
    memory-model.md 3.4, D106, the "Once and Cond" section of the rulings; with item 197): `c.Wait(l)`
    counts as `l.Unlock()` then `l.Lock()` when the compiler names the lock (`&mu`, `&s.mu`, or a pointer
    named by where it came from); a `Wait` whose lock it cannot name is the compile error of item 220
    (D108 replaced the earlier fallback, which counted it as unlocking every lock). Rule 2 case 4 now needs a
    view to be valid (19.3, Views loaded under a lock): a lock of the guard set held at the load stays held
    with no ending event to the use, so a view kept across `Wait`, or across `Unlock` and `Lock`, is E3501. Spec
    examples: the `type Job struct{ id int }` and "second keeps a pointer" blocks of 19.3.
220. **A release of a lock the task does not hold** (done, `bf49bf83`; sections
    19.3 (Locks and names; Held sets; Releasing a lock), 19.7, 18.12, 17.1, 20.10, F.5, A, memory-model.md 4.1, D108, the "Releasing a lock" section of
    the [rulings](verification/2026-11-09-spec-rulings.md)): every `Unlock`, `RUnlock` and `c.Wait(l)`
    must release a lock the calling task holds on every path, for writing (`Unlock`, `Wait`) or reading
    (`RUnlock`), for `sync.Mutex`, `sync.RWMutex` and `rt.Lock`, in tasks and interrupt handlers;
    otherwise a compile error, which needs a code. The guard inference must track held locks by stable
    names (a non-pointer package variable, or a local or parameter whose address is not taken and that no
    function literal assigns, then fields and constant indexes), carry names through copies, end them at
    assignments and moves (in callees too), map them through parameters at each call and back on return,
    solve recursion over every chain of calls, treat a range-over-func body and literals called where
    written as part of their function, start tasks and handlers with nothing held, give calls through
    interfaces, func values and method values nothing held, check generic code per instantiation, and
    check a deferred release at its `defer` and every point after it. Assigning a new value to a lock
    that another task may also use is E3502 naming the assignment. The bodies of the lock methods and `Cond.Wait` are exempt (18.12). Library, with item 197:
    `Cond.Wait(l)` makes its checks that may panic before it unlocks `l`. Spec example: the "finish
    releases a lock that another task took" block of 19.3.
221. **The guard set of every shared write** (done, `8272a1b9`; section 19.3 (The guard
    set; Rule 1), D111, finding K6 of audit K): the guard set of a datum is the intersection of the
    write-mode locks over all its shared writes, with "atomic" as a lock of its own, not the intersection
    over the writes that conflict with each other. A shared write with an empty guard set is E3502. Today
    `checkPlaces` intersects only over conflicting writes, so a single writer that writes under `L` at one
    write and `M` at another, with a reader that holds `L`, compiles; and the read pass skips a read whose
    conflicting writes share no lock. Only a lock that guards the datum in place (same base, no pointer
    load between, or package-level) counts, and a `Lock` of a lock with no name holds nothing. Conformance
    rows 59, 60 and 69 of 19.3.
222. **May-release and returned-view summaries** (done, `688f64f6`, code in `d2a9aee9`; phase 1, wrong acceptance; section 19.3 (Held
    sets; Views loaded under a lock), D113, finding K3 of audit K; with items 219 and 220): a summary
    records the locks a function may release on some path, to any depth, `c.Wait(l)` included, even if it
    takes them again; a view live across a call that may release its lock is ended (E3501 at the use). A
    summary also records, for each view it returns, the locks it was loaded under that stay held to the
    return. Spec example: the "peekWait keeps a view" block of 19.3.
223. **Range bodies on another party** (planned, phase 1, wrong acceptance; sections 19.3 (Part of the
    enclosing function), 12.11, D114, findings K1 and K9 of audit K): a summary records each func-valued
    parameter that the function calls on another party (inside a function given to `s.Task`, `wg.Task`, a
    task statement or a task starter, or through a callee that does); a copy that cannot be followed, and
    an unknown iterator candidate, count as such a call. A range-over-func body whose iterator may call
    `yield` on another party starts with nothing held, is not part of the enclosing function for `Do`,
    and a `defer` in it is a compile error, which needs a code. A body on the caller's task starts with the
    loop's held set less the iterator's may-release set. Spec example: the "par runs the loop body" block
    of 19.3.
224. **Enabling interrupts starts sharing** (planned, phase 1; section 19.3 (The start of sharing), 20.10,
    finding K4 of audit K; with items 156 and 214): the start of sharing is the first statement that may
    start a task, install a handler, or enable interrupts (`x86.Sti`, `x86.StiHlt`, `x86.RestoreFlags`),
    itself or through calls. Accesses before it do not count.
225. **Copying a lock** (done, `58c8137c`; sections 19.3 (Copying a lock), F.5, A, D115, finding K14 of
    audit K): a copy from a place of a value whose type holds a `sync.Mutex`, `sync.RWMutex`, `sync.Once`,
    `sync.WaitGroup` or `sync.Cond` (and `rt.Lock` outside `pluto/rt`), directly or through fields and
    array elements, is a compile error, which needs a code: assignments, arguments, results, sends, range
    values, value receivers and conversions. Composite literals, zero values, call results and owner
    moves are not copies.
226. **The race check's precision limits are exact** (planned, phase 1; section 19.3 (How precise the race
    check is), 21.3, 21.4, D109): remove `widenPass`, which lowers the load limit for groups slow to
    settle and so makes acceptance depend on the order of the fixed point; make a program past
    `maxGroups` an implementation-limit error instead of dropping its tasks, and likewise a func value
    followed past `maxDepth`.
227. **Views in scoped sharing** (planned; section 19.4 (Sharing, form 2), D111, finding K5 of audit K): a
    view loaded from a place guarded by a lock in the scoped form is valid only while the lock stays held
    from the load to the use, with no ending event; a use after `Unlock` and a new `Lock` is E3402.
228. **The owned memory of a per-CPU variable** (planned; section 20.9, D110, finding K7 of audit K): the
    memory a per-CPU variable owns is part of each processor's copy, reached only through pointer methods
    or `&v`, so its accesses need the variable's region (E2022), and views of it are bounded by that
    region (E2023).
229. **No task switch in a non-migrating region** (planned, phase 1; sections 19.3 (Switching tasks in a
    non-migrating region), 17.1, 20.9, abi.md 12.4, D116, finding L1 of audit L): a call of `rt.Yield` or
    `rt.Preempt`, or of a function whose summary may reach them, inside a non-migrating region is a
    compile error, which needs a code; follow single-target calls as the wait check does, and in a region
    opened only by `x86.Cli` or `x86.SaveFlagsCli`, also every candidate of interface, func-value,
    method-value, hook and allocator calls. In the runtime, `rt.Yield` with a non-zero preemption count
    panics with "blocking with preemption disabled"; the pending yield, `preemptPark` and the backend's
    switch are exempt. Spec rows 73 to 76.
230. **`AfterFunc` functions are repeated parties** (planned, phase 1; sections 19.3 (Parties), 19.6, D117,
    finding L2 of audit L): the race check counts a function started by `time.AfterFunc` as a repeated
    party at every call site. Spec rows 77 and 78.
231. **`pluto/sync` calls and channel operations are atomic accesses** (done, `2a85915e`, `HASH231`; phase 1; section 19.3
    (Accesses; rule 2 case 3), D118, finding L3 of audit L): collect each such call as an atomic write of
    the value's memory that conflicts only with a plain write of that memory (a new value, or a free, move
    or overwrite of what holds it); add the case 3 condition that crossing sync memory is not freed, moved
    or overwritten while shared. Spec rows 79 to 81. A send, a receive, `close` and a `range` over a channel
    are atomic accesses of the memory that holds the channel; tests for rows 102 and 105.
232. **A range body holds the iterator's locks at `yield`** (planned, phase 1; sections 19.3 (Part of the
    enclosing function; Lazy initialization), 12.11, D119, finding L4 of audit L): summaries record the
    locks held at every call of each func parameter, with modes; a range body on the caller's task adds
    them under the caller's names for accesses and views, and a view passed to `yield` counts as loaded
    there. A returned `Do` counts in a callee whose every call on the same party follows one. Spec rows 82
    to 85. Audit M (D124, D125, D127): name a returned closure's captured, never-assigned receiver and
    parameters by the call's arguments when `x.M(args)` is ranged over or called directly (M2, rows 109,
    116, 117); count every party entry (`main`, `init`, task functions, `AfterFunc` functions, handlers, the
    panic handler, `//pu:export` functions, scheduler methods, functions whose value reaches unsafe code,
    func values with unknown callers) as a call with no returned `Do` (M3, rows 118, 119); when the body may
    release a lock, count every access of the iterator without it (G3, row 121); record locks held at calls
    of copies of a func parameter (G7).
233. **The only writer's views** (done, `7d33940b`; section 19.3 (Views loaded under a lock; rule 2 case
    4), D121, finding L6 of audit L): a view loaded by the only writer of a datum, not repeated, is valid
    with no lock. Check what the race check does today and add a test for spec row 90.
234. **Smaller race-check fixes of audit L** (planned, phase 1; section 19.3, D122, findings L7, L11 and L13
    of audit L): a method value's method runs with the caller's held set (and outside every `Do`); the
    map trigger counts memory a callee on the same party allocated; `p := &mu` names `mu`. Check what the
    race check does today for each, and add tests for spec rows 91, 94 and 96.
235. **A handler body is a non-migrating region** (planned, phase 1; sections 19.3 (Non-migrating and
    interrupt-disabled regions; Waiting in a non-migrating region; Switching tasks in a non-migrating
    region), 20.9, 20.10, D123, finding M1 of audit M): the body of a `//pu:interrupt` handler is
    non-migrating up to `x86.Sti` or a call that may switch tasks, which ends it (through every candidate
    of interface, func-value, method-value, hook and allocator calls) and is not an error; the switch
    check does not start from that region, and the wait check does not start from regions in code a
    handler may reach (E2584 reports those waits once). `compiler/internal/race/region.p` already puts a
    handler body in `regIRQ`; check the rest and add tests for spec rows 110 to 115.
236. **A deferred call counts at every point after its `defer`** (done, `887b5d74`; sections 19.3 (Held
    sets; Views loaded under a lock; Lazy initialization; The start of sharing), 17.2, 17.3, D128, finding
    N2 of audit N): a deferred call runs where its function leaves, which may be a callee that ends the
    task with `rt.ExitTask` or a panic, so its accesses, views, regions and returned `Do`s use what holds
    at its `defer` statement and at every point after it, less what deferred calls that run before it
    release. Today `walker.finish` (`compiler/internal/race/walk.p`) runs the deferred calls with the join
    of the states at the returns only. Add tests for spec rows 122 to 126.
188. **Proven task stacks in every build** (planned; section 19.1; the rest of item 18 and gap 37): checked
    builds stay not computed because `genLive` follows a record's chain of parents recursively; rewrite
    it without recursion. Then compute bounds for kernel, freestanding and custom-scheduler builds, and
    fill the small unverified witness pairs in the
    [resolution record](verification/2026-10-15-spec-consistency.md).
193. **`sync.Scope` phase 2** (planned; section 19.4, [plan](plans/2026-10-10-scoped-puroutines.md); moved
    from item 63): a Scope reached through a pointer, helpers that start tasks for their caller's Scope,
    an error-collecting scope; `Index` and `SetIndex` of library containers are not partitioned between
    tasks. The parallel compiler phases need it.
194. **Hosted scheduler follow-ups** (planned; sections 19.1, 19.5; moved from item 14): a program that binds
    its own hosted backend runs on one thread; spawning a task costs about twice a mature scheduler's.

## Language

19. **Methods with type parameters** (done, `4f98246`; section 10.15): method values must be instantiated.
20. **Ownership: owners, moves and views** (in progress; chapter 18, sections 7.18, 7.19 and 8.7,
    [design](plans/2026-10-09-memory-safety-design.md)): done are `owner` types, the move checker
    (`internal/moves`, E3301 to E3314, including views of local owners used after a move, section 18.3),
    automatic drops on every path, `take`, `unsafe.Drop`, `owned[T]`,
    `shared`, `synced`, `weak`, owning interfaces, owner unions, the local-arena rule (section 18.9), and
    generation checks and arena epochs (items 71 and 72). Commits: `ca7e064`, `dc808a9`, `01daa4b`,
    `1b25e7d`, `052c091`. Audited on 2026-10-31 ([record](verification/2026-10-31-item20-audit.md)): items
    78, 86 to 89 and 92 are done, the generation checks of item 85 work (what is left in item 85 is speed
    tuning), and every library owner that item 51 names is an owner with `Drop`. Open, each part
    tracked by its own item: a consuming method used as a method value is accepted, not an error (item
    178, sections 11.9 and 18.5); generation checks for early boot, interrupts and arenas in kernel
    builds (item 101 phases 3 and 4, section 18.11); the four view-tracking cases left by item 138 (item
    179, section 18.3); `rt.ReleaseValues` and `ZeroValues` have no effects in the escape contract
    table (item 180); and cases for rules that have none (item 184: E3205, E3208, E3209, E3007, the
    embedded, array and interface forms of E3202, most kinds of E3204, a view converted into a box, a
    map lent to an ordinary interface, a send in a `select`; the audit lists them). The 2026-11-09
    rulings add ownership work: moves and views in items 146, 147, 148, 149, 170, 171, 172, 174 and 175;
    drops and counts in items 152, 158, 159, 160, 169 and 173; closure regions in item 176. Close this
    item when those are done.
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
    (`opt.ElideStringHolds`). Linux file names that are not UTF-8 are decided (D36, item 122). The
    efficiency follow-ups are item 191.
60. **Counted `map[K]V`** (done, gap 21, `7867af2`, `8fd4ab7`, `0f8bc1b`, `c491a67`, `3c094e3`; sections 7.10,
    12.10, 13.3, 13.5, 13.7 and 19.3, abi.md section 1.4): one word, a counted reference to a
    `pluto/rt.MapTable`; `make(map[K]V, n, a)` takes an allocator and a literal allocates from the current
    allocator; owner keys and elements are E3213; a map is sendable, and the contents of the maps of a type
    that crosses to another task fall under the guard rule of item 59; go2pu translates Go's maps as they
    are. `hash.Map` takes owner values and has `Remove` and `Pop` (item 132). The race rule groups maps
    by type, except maps kept in place; item 192 tracks that.
61. **Removed conversions** (done, `080e49b`, `d611757`, `271e57f`; sections 11.24 and 18.8): `string(b)`
    is E2015, `[]byte(s)` and `[]rune(s)` are E2016, `string(i)` of a non-rune integer is E2017; the
    messages show the replacement (`strings.FromUTF8`, `strings.Bytes`, `strconv.Itoa`).
62. **The `task` statement and the rename sweep** (done, gap 20, `4cb685c`, `3967e27`, `523a21f`, `6798a0c`;
    sections 12.12, 4.5, 4.6, 17.1, 19.4): `task f(x)` starts a task; `task` is a contextual keyword that
    begins a statement when the next token is an identifier or `func`; `go f()` is E1012 and names `task`.
    The word "task" replaced "puroutine" in the runtime's messages and traces, `(*sync.WaitGroup).Task`,
    `rt.Panic.Task`, the diagnostics and their goldens. The generated thunks are named
    `<function>.task<N>` (`compiler/internal/lower/conc.p:601`, `scope_task.p:127`), as traces show
    (`main.main.task1` in `tests/build_deadlock_test.go`, `tests/dwarf_test.go`); abi.md sections 8 and
    12.6 now say so.
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
    unchecked mode. Kernel and freestanding builds were left unchecked at first; item 101 phase 2
    (`58fcd043`) made them checked by default too.
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
    step; `clear` on a slice of owners drops each element (13.3; done, row 33, `2f0b88da`: each owner is dropped in index order, its
    `Drop` method before its place becomes the zero value). One sub-item is not done and moved to item 178:
    a consuming method used as a method value is an error (11.9; G26, decision (a)); the compiler accepts
    it today.
77. **No `append`** (done, `38f6777`, `fa4bdcc`, `57e4f05`; sections 13.1, 7.5 and 18.10,
    [plan](plans/2026-10-10-remove-append.md)): every call is E2013 with the fix for its idiom; `vec.Vec`
    grows, `vec.Fixed` is bounded; `fmt.Appendf` replaces `Bprintf`; `make([]T, ...)` is E2014 (fix
    `vec.Make`). Item 83 removes the name from the predeclared scope.

148. **Moves through type switches, assertions and conversions** (done; sections 7.9, 11.13, 11.26, 12.9,
    18.5, D44, findings C2 and A3 of the [rulings](verification/2026-11-09-spec-rulings.md)): a clause or assertion naming an ordinary interface lends
    instead of moving; the operand is possibly moved after a switch where another clause moved it; a
    moving conversion moves at the enclosing call or assignment. Done: the move checker records the
    lending clauses (`SwitchLends`); a lending clause's binding is a view of the operand, and the operand
    is possibly moved (E3302) after the switch when only some clauses move it. Lowering clears the operand
    only in a moving clause, and an unmatched value stays in it to be dropped with it
    (`compiler/internal/lower/stmt.p`, `compiler/internal/escape2/eir/stmt.p`). Tests:
    `tests/moves/rejected_type_switch_moves_only_in_some_clauses`, `accepted_type_switch_moves_only_in_some_clauses`,
    `rejected_assertion_lends_and_conversion_moves` and `accepted_assertion_lends_and_conversion_moves`.
149. **Follow comma-ok `ok` through direct tests** (done; sections 11.13, 18.5, D47, finding C3): the move
    analysis knows `ok` on the branches of a direct test, and the E3302 message suggests `if !ok`. Done: a
    moving comma-ok assertion records an `okFact` on its `ok`; `if ok`, `if !ok` and their `else` branches
    see the operand moved or as it was, while the move that emptied it still stands. Tests:
    `tests/moves/rejected_ok_of_a_moving_assertion_not_tested_directly`,
    `accepted_ok_of_a_moving_assertion_tested_directly` and the hint in `tests/diag/owning_iface`.
150. **An enum is not an integer type** (done, `f4200cfa`; sections 7.12, 11.24, D48, finding C4): `Shade(color)` and
    the other enum-to-enum conversions are E2313; update the catalog text. Constant, comma-ok and plain
    forms are all E2313, and the message shows the conversion through the integer type. Tests:
    `tests/types2/enumerrors_enum_conversion` and `enumconversion_through_integer`.
151. **Defined union and packed types** (done, `4ae3caaf`; sections 7.13, 7.14, 8.2, 10.8, D49, findings C5 and C6):
    `type V U` for a union or packed `U` gets the variants or fields and layout, no methods, and explicit
    conversion. The checker and lowering already did this; the item adds the tests:
    `testdata/plutonium/uniondefined` (unions, a generic instance, packed types and packed unions, an
    owner union defined from another, conversions both ways and layout) and
    `tests/types2/uniondefined_errors` (no methods, no implicit conversion, exhaustive switches, the owner
    mark).
157. **The C boundary refuses boxes** (done, `53a5fbc1`; sections 20.4, 18.12, abi.md 9.1, D50, finding B7):
    E2511 for `owned`, `shared`, `synced` and `weak` by value in `//pu:extern` and `//pu:export` signatures.
164. **`owner` as a type parameter name** (done, `37da753d`; section 4.6, D57, finding A2): the parser takes
    `owner` as the keyword only after the identifier list, before a constraint.
166. **Conversion checks** (done, `3f0783f6`; sections 11.12, 11.24, findings C8, C10, C11, C12): E2019 for
    slicing a type parameter with a string in its type set; packed types to and from byte arrays; `string(c)`
    only for untyped integer or rune constants and `rune` constants; owner conversions need a valid conversion
    of the underlying types.

171. **Lending a large owner to an ordinary interface** (planned; sections 7.9, abi.md 5, D61, finding D3): the
    data word points at the variable, and the interface value is a view bounded by it.
172. **E3303 for assertions and switches on places** (done, `1c3dd4f4`; sections 11.13, 12.9, 7.13, D63, finding
    D5): a moving assertion or bound type switch on a field, element or `*p` is E3303, and the fix shows
    `take(&x.f)`. Tests: `tests/moves/rejected_moving_assertion_out_of_a_place` and
    `accepted_assertion_on_a_place_after_take`.
175. **`defer x.Drop()` in the move checker** (done, `4bcd8118`; section 18.5, D62, finding D4): the deferred
    drop does not copy `x` and is not a use at the exit, so a later move is allowed and the drop then
    finds nothing to drop. Tests: `tests/moves/accepted_deferred_drop_then_moved` and the run test in
    `testdata/lib/drops.p`.
176. **Region rules for closures** (done; section 11.5, D68, findings D6 and D14): check that a closure passed
    through a func value or an interface counts as kept, and that storing it in the same frame or passing it
    to `s.Task` is allowed. The analysis already did this; the item adds the tests:
    `tests/escape/rejected_closure_kept_through_a_func_value_or_an_interface` (a func value, an interface,
    and a func value given to `s.Task`, which D68 does not allow: only a literal written as its argument) and
    `accepted_closure_stored_in_the_frame_or_given_to_a_scoped_task`.
181. **False escape errors in heavy unit-test builds of `types2`** (done, `de6b2994`, `9cd50379`, `6b4acfd4`;
    section 18.3): the checked test builds of `sched`, `summary`, `solve`, `escape2` and `eir` were rejected
    at `compiler/internal/types2/conversions.p:89` and `:97` and `unify.p:735`. The conversions errors came
    from a cut of a deep path that stood for the pointer loaded from it; `pluto/testing`'s `sprintln` errors
    from a join of virtual calls that lost one of a key's two type filters; the `unify.p` error from a solve
    over `MaxFuncArena` whose E3010 a later pass overwrote. `TestCompilerUnits` builds
    `compiler/internal/escape2/typesprobe` up to the end of the checks, which catches the first two in
    about 7 minutes.
182. **Audit the lowering backstops** (done, `ad67f688`; section 21.2): each `unsupportedf` (E2500) site and
    packed field backstop in `compiler/internal/lower` is either unreachable after the type checker (say so in
    a comment or remove it) or a missing feature (add an item): `builtin.p:149`, `:219`; `expr.p:117`, `:686`,
    `:1013`, `:1310`; `stmt.p:126`, `:721`; `packed.p:124`, `:136`, `:192`, `:238`.
238. **Packed fields lowering cannot access yet** (planned; section 7.14; moved from item 182): these fields
    are valid and stop the build with E2500: an array field that does not start on a byte, that has elements
    of several bytes in a `be` type laid out by byte, that is in a `be` integer-backed type, or that is in a
    `volatile` packed value; a field whose bytes span more than eight bytes (a `uint64` at bit 4); and a
    nested `be` integer-backed packed field that does not start on a byte. `tests/diag/packed_not_yet` has one
    of each; each becomes a run test when it works.
183. **Order of evaluation in the rest of lowering** (done, `ce9c5520`; section 11.26; moved from item 73):
    survey the rest of `compiler/internal/lower` and add `testdata/run` and difftest cases (assignments, `x[i]
    = f()`, operands of operators); every difference from the specified order is a false friend and is fixed.
191. **Counted string efficiency** (planned; sections 7.3, 18.8; moved from item 57): interface boxes hold
    their string until the function returns; `(string, error)` returns through memory;
    `compiler/compat` can drop helpers for what is native.
192. **Maps kept in place** (planned, phase 1; sections 7.10, 19.3 (Maps kept in place), D120, finding L5 of
    audit L; moved from item 60): the race check groups maps by type, so two objects that each guard a map
    with their own mutex are E3502. Find the places (package variables and struct fields of map type, per
    instantiation) that are kept in place: every stored value is a `make`, a map literal or `nil` written
    there, and every other use is an index, `len`, `delete`, `clear`, `range`, a comparison with `nil`, or
    an argument for a parameter or receiver whose summary keeps it in place. Make the contents of such a
    map part of the datum of its place, per object, guarded in place; leave other maps in the datum of
    their type, which only a package-level lock guards. Summaries record, per map parameter and receiver,
    whether it keeps it in place. Spec rows 86 to 89 and 101. Audit M (D126): an E3502 or E3503 about map
    contents reached through a place that is not kept in place names the first occurrence of that place
    (by package path, file, position) that copies it out or stores a value that is not new, and says that
    it takes the map out of its place so the place's lock no longer guards it (row 120; chapter 21.1).
195. **Generation check tuning** (planned; section 18.11; moved from item 85): coherent whole-range and
    ancestor snapshots, same-granule scalar checks, proven extent reuse and interval lookup hints without
    cached liveness. Item 85 records the current costs.

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
184. **Tests for untested diagnostic codes** (planned; section 21.1; the survey's section 3 and the
    [item 20 audit](verification/2026-10-31-item20-audit.md)): 30 Plutonium codes have no test that
    expects them, among them E1005 to E1009, E1011, E2009, E2101, E2104, E2106, E2109, E2405, E2500,
    E3007, E3205, E3208, E3209, E4002 to E4004, E4017, E4018, E5002 to E5008 and E9001; the sub-forms
    of E3202, E3204, E3210 and E3212 and the rules the audit lists also have none. Of the 103 untested
    Go-derived codes, first E0036, E0083, E0148, E0085, E0088, E0086, E0091, E0092, E0112, E0114, E0117,
    E0119, E0120, E0124, E0100, E0128, E0129, E0130, E0150 and E0133 to E0146: check which are tested by
    message, remove those Plutonium cannot reach, and test the rest.
185. **Compile the specification's examples** (done, `5280a707`; section 1.3, chapter 2):
    `tests/specexamples_test.go` (`TestSpecExamples`) compiles the go blocks of `docs/spec/language.md`,
    `docs/spec/language/`, `abi.md`, `memory-model.md`, `debugging.md` and `debug-allocator.md`. A line
    marked `error`, `illegal` or `invalid` must fail on that line with the stated code, and other lines
    and unmarked blocks must build cleanly. Per-block context and known failures are in
    `tests/testdata/specexamples/examples.txt`; a known failure that starts passing fails the test. Of 350
    blocks, 322 are checked in 368 builds, 27 are skipped by rule (elided code, non-source fragments) and
    1 by its entry (the section 11.24 parse examples, which no one set of declarations fits).
    The race check finds parties only from package initialization and `main`, so a block with no `main`
    that starts a task or declares an interrupt handler gets a `main` that starts them: it installs each
    handler and calls each function nothing else in the block uses, and an entry's `main:` lines call one
    that takes parameters. Such a block builds as a program of its own; 19 blocks are built this way.
    Known failures: 4, waiting on items 214, 215 and 223, and 1 spec example that
    misses an error (the 19.3 `type Config struct{ n int }` block). A block whose marked lines are errors
    of one program, not alternatives,
    has `together` in its entry and builds once, whole: every marked line must report its code and no
    other line any error (section 1.3; the 19.3 `type Counter struct {` block, D129). The 12 wrong spec examples were fixed with audit H
    (D95), and the section 7.9 example now marks both lines of the clash, since the rule names no single
    line.
186. **CI gaps** (planned): CI installs only `binutils` and `qemu-system-x86`, so the gdb tests in
    `tests/dwarf_test.go` skip; the heavy compiler unit tests need `PLUTO_UNIT_HEAVY=1`, which CI does not
    set; nothing runs difftest (item 32), which needs a Go toolchain. Coverage measurement of
    `compiler/` and the test memory guard left with the Go implementation (AGENTS.md, "Known gaps").
    Install gdb, run the heavy tests and difftest weekly, and restore the coverage measurement and the
    memory guard.
187. **Stale `TODO(A)` in the escape report** (planned): `compiler/internal/escape2/report/resolve.p:130`
    says `diag.EscapeLimit` is not in `internal/diag` yet, but `compiler/internal/diag/codes.p:136` has
    it. Use `diag.EscapeLimit` and remove the local `codeEscapeLimit`.
189. **DWARF variables, parameters and types** (planned; section 21.7, debugging.md; moved from items 13 and
    42): `DW_TAG_variable`, `DW_TAG_formal_parameter`, the type tags, locations, and enumeration types
    for enums. `compiler/internal/debuginfo/dwarf.p` writes only compile unit, subprogram and inlined
    subroutine tags.

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
    inlined calls are frames. A per-thread alternate signal stack for worker threads is item 190.
42. **DWARF line tables and CFI** (done, `002e065`, `8d57684`; debugging.md): for `gdb` and `perf`.
    Variable and type information and enumeration types are item 189.
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
51. **Owner types in the library** (done; sections 7.18 and 18.10): `vec.Vec`, `hash.Map`,
    `hash.Set`, `strings.Builder`, `bytes.Buffer`, `heap.Heap`, `flag.FlagSet`, `os.DirEntries`,
    `exec.Cmd`, `arena.Chunked`, `sync.Scope` and `os.File` (item 134) are owners with `Drop`; the
    [item 20 audit](verification/2026-10-31-item20-audit.md) found none missing. `sync.NewCond` still
    returns a `*Cond` from `alloc.New` that the caller frees with `alloc.Free`
    (`lib/pluto/sync/waitgroup.p:108`); D106 decides it and item 197 makes the change.
52. **The `pu` mirror package for self-hosting** (done, `de6aa50`, history;
    [plan](plans/2026-10-09-self-hosting-plan.md)): a Go package `pu` with the library owners,
    which go2pu mapped one to one. It was deleted with the Go implementation, so this item is history
    only. The translated compiler uses `compiler/pu`. A Go `map` translates to the built-in map
    unchanged (item 60).
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
    [verification record](verification/2026-10-12-streaming-hash.md). The port finished without moving
    the compiler to these hashers: no compiler code imports `pluto/hash` or uses `maphash`, and
    `lower/iface.p` (`descHash`) and `lsp/diagnostics.p` (`hashBytes`) keep small FNV-1a functions of
    their own. Nothing is left for self-hosting.
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
167. **Conformance checks for the 2026-11-09 rulings** (planned; sections 18.9, F.2, F.3, F.6, 17.4,
    debug-allocator.md, findings B12, B13, B16, B17 and B19 of the [rulings](verification/2026-11-09-spec-rulings.md)): tests that `alloc.Default()` is
    nil only before `SetDefault` in a freestanding build, that the arena window follows the `Restore`'s
    extent, that the panic buffer is 1024 bytes, that the library names in F.3 and F.6 exist, and that
    the debug allocator registers generation tags.
190. **Alternate signal stacks for worker threads** (planned; sections 17.1, 21.7; moved from item 41): a
    per-thread alternate signal stack, so a fault on a worker thread's stack can still be reported.
197. **`sync.Cond` as a value** (done, `7f743567`; sections 19.3, F.5, A, D106; from item 51):
    `NewCond` returns a `*Cond` from `alloc.New` that the caller frees with `alloc.Free`
    (`lib/pluto/sync/waitgroup.p:108`). Decided (D106): `Cond` becomes a value whose zero value is ready,
    with no `L` field and no `NewCond`, and `Wait(l Locker)` unlocks `l`, parks, and locks `l` again.
    `Signal` and `Broadcast` keep their forms. A `Cond` beside its mutex in a package variable or a struct
    then holds no view and needs no free. Change `lib/pluto/sync/waitgroup.p`, its tests (among them
    `tests/testdata/escape/review4/cd01_newcond_local_mutex.p`) and
    `skill/plutonium/references/stdlib-memory-concurrency.md`.
