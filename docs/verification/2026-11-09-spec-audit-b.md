# Spec audit B: chapters 13 to 21, appendix F, abi, memory model, debugging, debug allocator

Source: /home/matjam/src/plutonium/.claude/worktrees/coord (origin/main), docs/spec/.
Read-only. Each finding was checked with grep across the whole spec. A1 to A8 are not repeated.

Counts: high 1, medium 8, low 13.

---

## B1. Counted values made in a non-local arena can reach another task and outlive the arena

- Kind: safety hole (use after free, data race in safe code). Contradiction.
- Severity: high.
- Changes a promise: yes. It adds a compile error or a run-time panic to programs that are accepted today.

Sections and quotes:

- 18.9, the local-arena window rule covers arenas in local variables only. For other arenas: "A view stored in heap memory does not lose its arena lifetime. Where the compiler cannot follow such retention, a checked build uses the runtime lifetime checks of section 18.11".
- 18.8: "Dropping the string writes its header, so the string must not be used, or dropped, after the arena is reset or dropped. For an arena in a local variable the compiler checks this". For other arenas this is an obligation on the program with no rule behind it.
- 18.9 and memory-model 3.8: "An arena takes its memory back only in bulk, through `Reset`, `Release` or `Drop`. Strings and counted boxes made in an arena are dropped before the arena takes back their memory". Nothing makes that true for a string that another task still holds.
- 13.7 (line 295): "A map made in an arena needs no release: the arena takes its memory back when it is reset or dropped."
- 19.2 and 16.7 rule 7: strings and every `error` value are sendable. 19 mentions arenas only at lines 69 and 532 and puts no limit on sending arena-made values.
- memory-model 3.8: "A free that races with a use is a data race ... safe code cannot make one (section 4.1)".
- abi 1.3 (lines 277 to 289): a check is valid "until a may-free call or a synchronizing op", and "a free that races with an access, unsynchronized, is a data race the check could never have caught".

Example:

```go
type Server struct {
	arena alloc.Chunked
	out   chan string
}

func (s *Server) step(n int) {
	r := alloc.Use(&s.arena)
	msg := fmt.Sprintf("item %d", n) // counted string in s.arena
	r.Restore()
	s.out <- msg                     // sendable: strings may cross tasks
	s.arena.Reset()                  // bulk reclaim; count is ignored
}

// another task:
//   m := <-s.out
//   println(m)   // the epoch check passes, then the owner task resets
//                // and reuses the chunk, then the bytes are read
```

The receiver's check and its read are not atomic with the reset in the other task. That is the race abi 1.3 says no check can catch. A single-task form also exists: keep `msg` in a field, call `Reset`, and the later drop of `msg` writes the count into reused memory. A checked build catches the single-task form at the drop only if the drop path checks the epoch, which 18.11 does not state for drop glue.

Resolution options, by the goals:

- (a) Each arena counts its live counted objects (strings, boxes, maps, channels, allocated errors). `Reset`, `Release` and `Drop` panic while the count is not zero. This is cheap (one counter per arena), keeps Go-style code, and turns the hole into a loud failure.
- (b) A value made while any arena is installed is not sendable and may not be stored outside the Use window unless copied. The compiler then treats every Use window like a local one. This is static and fits "Rust-level safety without annotations", but rejects more code.
- (c) Counted types are never allocated from an arena; they go to the arena's parent. This is simple but makes arenas less useful and hides an allocation choice.

Option (a) fits the goals best. It needs a new sentence in 18.9, memory-model 3.8 and 13.7, and a panic in chapter 17.

---

## B2. A Fixed arena allocates a bookkeeping record from its parent for every allocation

- Kind: goal conflict. Contradiction.
- Severity: medium.
- Changes a promise: no (it changes cost, not meaning).

Sections and quotes:

- 18.9: "Each successful allocation additionally obtains one bookkeeping record from the same parent, in checked and unchecked modes; reclaiming that allocation returns its record".
- 18.9: "Freeing into an arena does nothing ... its free costs nothing". 18.9 line 601: "an arena takes memory back in bulk only".
- abi 1.3 lines 344 to 350: Fixed arenas "storage and ownership must follow the explicit allocation contract".

Problems:

- Every arena allocation becomes a parent allocation, which is the cost arenas exist to avoid.
- `Reset` must return one record per live allocation, so it is O(n), not O(1).
- Arena allocation takes on the parent's wait and interrupt properties. A Fixed arena in a handler then depends on its parent being usable there (E2584).
- It is a hidden allocation in the sense of the goals.

Resolution: size a bounded side table from the Fixed backing at construction (one record per granule or per slot), and say allocation fails when the table is full. This keeps "free costs nothing" and keeps the arena usable in interrupt context.

---

## B3. Whether an arena can reclaim its latest allocation is stated both ways

- Kind: contradiction.
- Severity: medium.
- Changes a promise: yes, whichever way it is settled, for code that relies on Mark/Release or on memory reuse.

Sections and quotes:

- Appendix F.2 (line 122): "arena ordinarily does nothing, except that the latest allocation may be reclaimed; `Reset` reclaims everything."
- abi 1.3 line 335: "`Reset`, `Release`, `Drop` and a `Free` of the latest allocation zero the granules they take back".
- 18.9 line 601: "an arena takes memory back in bulk only". 18.9 line 686: "Freeing into an arena does nothing".
- memory-model 3.8: "An arena takes its memory back only in bulk".
- 07-types.md about line 1686 repeats the bulk-only rule.

Why it matters: if the latest allocation can be reclaimed, a stale view of it must fail its epoch check, so `Free` must advance the epoch for that range. 18.11 describes epochs per reset, not per free. Also, optimizers and programs that assume arena memory stays valid until `Reset` are wrong under F.2.

Resolution: choose bulk only (simpler, matches four texts) and fix F.2 and abi 1.3. Or keep the latest-allocation rule and state it in 18.9 and memory-model 3.8, with the epoch rule for it in 18.11.

---

## B4. The write protection for cleaned places has an unstated cost on every store

- Kind: goal conflict (performance near Rust). Underspecified.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 15 (package cleanup, lines 313 to 322): "Once cleanup of a resource-bearing place has finished, any typed write there, directly or through an alias, is a run-time panic, including a zero assignment, `take` or `clear`."
- abi 12.10 lines 1557 to 1566: "Compiler-emitted static-object bounds and cleanup progress must reject typed writes overlapping a completed resource-bearing place, including aliases, scalar fields and zero writes, with a run-time panic ... no application heap allocation or general ownership tracing".

Problem: "through an alias" means any typed store that may point into a static object must compare its address with static bounds and cleanup progress. That code is shared with normal execution. The spec states no cost and no limit on where the check runs.

Resolution: say the check runs only in code reachable from cleanup glue (the compiler clones such functions), or that a store needs the check only when cleanup has started (one global flag test), and state the cost. Either way 21.5 should list it next to the other run-time checks.

---

## B5. `%w` on a field error keeps a view of the field, but 16.5 says the message is copied

- Kind: contradiction. Behavior depends on reading.
- Severity: medium.
- Changes a promise: maybe. It changes what `errors.Is`, `As` and `Unwrap` see after the source object's next error.

Sections and quotes:

- 16 storage table (line 45): "a field of a longer-lived object | ... | a parser that returns `&p.err`, `fmt.ErrorBuf`".
- 16.5 lines 168 to 172: "The error is valid while the object lives and until its next error ... Code that keeps such an error longer wraps it with `fmt.Errorf`, which copies the message."
- 16.7 rule 4: "`%w` copies the wrapped error into the wrapper (count plus one)". For `&p.err` the copied value is the pointer, so the wrapper still points at `p.err`.

Example:

```go
err := p.Parse(a)                          // &p.err
e2 := fmt.Errorf("first: %w", err)         // message text copied; cause is &p.err
p.Parse(b)                                 // p.err changes
errors.Is(e2, ErrEOF)                      // follows the new p.err
```

Races on `p.err` are decided by Rule 2 (19.3), so this is not a safety hole. But a reader of 16.5 expects `e2` to be independent, and it is not. It is a false friend for Go users, where a wrapped error never changes.

Resolution: say `%w` of an error held in a field of an owner copies it into a counted error, as 16.5 promises. Or say in 16.5 that only `%v` copies, and `%w` keeps a view bounded by the object.

---

## B6. The handler data rule is absolute and conflicts with the general race rules and the init exemption

- Kind: contradiction. Underspecified.
- Severity: medium.
- Changes a promise: yes, if read literally it rejects read-only tables in handlers.

Sections and quotes:

- 19 lines 309 to 317: "Rules 1 to 3 apply with handlers as parties ... Data that a handler accesses, other than per-CPU variables (section 20.9), must be atomic or guarded by an `rt.Lock`".
- memory-model 6.1: "Data that a handler shares with other code is accessed with atomic operations or under an `rt.Lock`".
- memory-model 4.1 counts "memory that does not change while shared, such as a package variable written only during package initialization" as safe shared memory. 3.7 says string bytes never change.
- 19 Rule 1 exemption (about line 160): package initialization runs "while no other task exists".

Problems:

- Read-only data (lookup tables, string bytes, init-time configuration) is neither atomic nor locked, yet Rule 1 allows it. The handler sentence forbids it.
- A handler installed during package initialization, with interrupts enabled, is a party while "no other task exists". The exemption then lets init write data the handler reads with no synchronization.

Resolution: make the handler sentence a consequence of Rules 1 to 3 ("written data must be atomic or locked"). State that the init exemption ends when the first handler is installed or interrupts are enabled, whichever comes first.

---

## B7. Box types across the C boundary: count and ownership transfer are unstated

- Kind: hole. Underspecified.
- Severity: medium.
- Changes a promise: no (it fills a gap), but the chosen rule binds C code.

Sections and quotes:

- 20.4 lines 346 to 366: `owned`, `shared`, `synced` and `weak` "cross as the address of the object".
- abi 9.1: they "map to the C integer or pointer type". Strings are refused because C could keep an uncounted reference.
- 18.3 line 144: an extern function is assumed to keep every pointer argument.
- 18.12 lists unsafe code: x86 intrinsics, functions without a body. `//pu:export` is not listed.

Unstated:

- Whether passing an `owned` value to a `//pu:extern` function moves it, and who drops it.
- Whether a `//pu:export` function that receives an `owned` value drops it at return.
- Whether an extern that returns an `owned` or `shared` value gives a count to the caller.
- Whether a `shared` or `synced` reference kept by C is counted. The reason abi 9.1 gives for strings applies equally.
- Whether a `//pu:export` function, which trusts addresses from C, is unsafe code for `pluto vet -unsafe` (21.5).

Resolution: refuse counted box types at the boundary like strings (pass `unsafe.Pointer` and use explicit functions), or state each transfer rule in 20.4 and abi 9.1. List `//pu:export` with box parameters as unsafe code in 18.12. Refusing is simpler and matches the string rule.

---

## B8. ExitTask: what "as if it returned at the call it is in" means mid-expression and inside a drop

- Kind: underspecified run-time behavior. Possible double drop or leak.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 17.3: `rt.ExitTask` "drops what it holds, as if it returned at the call it is in". "`ExitTask` does not unwind the stack by any mechanism of its own. Each frame leaves through its ordinary return path".
- abi 12.10, drop bookkeeping: "`rt.taskMain` uncounts a task that leaves one with `rt.ExitTask`". So a task can leave in the middle of a drop.

Unstated:

- Temporaries already evaluated in the expression that contains the call (`f(make1(), g())` where `g` calls `ExitTask`): are they dropped? There is no ordinary return path for them.
- `ExitTask` inside a `Drop` method: do the remaining field drops of that object run? If they do, the method ran partway and the object is half dropped. If they do not, the fields leak.
- An owner moved into the call in progress: who drops it?

Resolution: say the call's evaluated operands are dropped as on return from the enclosing statement, and that `ExitTask` in a `Drop` method finishes the field drops of that object first (or is a panic, like the `//pu:export` boundary in 20.4).

---

## B9. Deferred calls on a panic can see broken internal invariants

- Kind: underspecified.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 17.2: a hosted panic runs the task's armed deferred calls before the handler. A panic does not unwind.
- debugging.md 5: deferred calls run "below those frames, which they leave as they were".

Problem: a panic raised in the middle of an operation (a bounds check inside a library function that holds an `rt.Lock`, or that has half-updated a vec) leaves that state. A deferred call in an outer frame can then touch the same object. Go has the same exposure but Go unwinds and runs inner defers first, which restore invariants. Here inner frames with no defer do nothing.

Resolution: state that a panic while an `rt.Lock` is held stops the program without deferred calls (as for faults found by the debug allocator), or that library types must restore state before a panicking check. Say which.

---

## B10. Self copy of a counted value releases before it counts

- Kind: mistake (possible use after free).
- Severity: low (one sentence fix; implementations will likely do it right).
- Changes a promise: no.

13.2 describes a copy as releasing the destination and then counting the source. For `s = s`, or a copy where source and destination share the last reference, the release frees the object before the count. Resolution: count the source first, then release the old destination.

## B11. 16.6 mentions `shared[T]` errors, which E2020 forbids

- Kind: contradiction. Severity: low. Promise: no.

16.6 talks about "shared[T] errors"; E2020 rejects `shared[T]` as an error type. Resolution: delete the phrase or name the form that is allowed.

## B12. Appendix F omits names that the chapters use

- Kind: contradiction (appendix vs chapters). Severity: low. Promise: no.

F.3 and F.6 omit `vec` `Ptr`, `Clone`, `Pop`, `Remove` and `strings.Builder`, which chapters 13 and 18 use. Resolution: add them.

## B13. F.2 `Default()` "or nil" vs the panic when there is no allocator

- Kind: contradiction. Severity: low. Promise: no.

F.2 says `Default()` returns the allocator "or nil". Chapter 18 says allocating with no allocator is a panic and every task starts with the program-wide default (18.9). Resolution: say when nil can be seen (before `rt.SetDefaultAllocator` in freestanding builds) or remove "or nil".

## B14. Harmless x86 intrinsics count as unsafe code

- Kind: goal conflict. Severity: low. Promise: no.

18.12 makes every x86 intrinsic except `Cli`, `Sti`, `SaveFlagsCli` and `RestoreFlags` unsafe. abi 10.5: "only `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp` are useful outside ring 0". These four cannot break memory safety, so listing them as unsafe adds noise to `pluto vet -unsafe`. Resolution: make `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp` safe.

## B15. `print` in an NMI handler

- Kind: underspecified. Severity: low. Promise: no.

13.12 allows `print` in handlers through the console writer. An NMI can arrive while the console lock is held, and abi 12.5 rule 6 covers maskable interrupts only. Resolution: say `print` in an NMI handler may drop output, or forbid it there.

## B16. Panic message buffer size is not stated

- Kind: underspecified. Severity: low. Promise: no.

17.4 says the panic message is formatted without allocation but gives no buffer size or truncation rule. Resolution: state the size, or that the message is truncated with a marker.

## B17. The Use window is described by its call site, not by its extent

- Kind: wording. Severity: low. Promise: no.

18.9 describes the arena window in terms of the call to `alloc.Use`. The rule needs the extent (from the call to `Restore` or to the end of the block). Resolution: define the window as that extent.

## B18. Reassigning a string parameter releases a borrowed value

- Kind: underspecified. Severity: low. Promise: no.

abi 3.3: "A string argument is borrowed". Reassigning the parameter in the callee would release the old value under 13.2, but the callee holds no count for it. Resolution: say the first reassignment of a borrowed parameter does not release the old value.

## B19. The debug allocator does not say how it meets the generation-tag contract

- Kind: hole (between documents). Severity: low. Promise: no.

abi 1.3: "The contract applies to custom allocators" (tags in the shared metadata directory). debug-allocator.md describes guard pages and quarantine but never says it registers tags, and its page-fault "use after free" example is not marked as unchecked-only. In a checked build `panicStale` fires first. Resolution: state that the debug allocator registers tags, and mark the fault example as the `--gencheck=false` case (which line 13 already discusses).

## B20. A moving `sync.Scope` stops the program at run time

- Kind: goal conflict. Severity: low. Promise: no.

abi 12.10: "A `sync.Scope` that moves while tasks it started may run stops the program (`rt.joinMoved`)". A compile-time rule (Scope not movable while it has started tasks, like a local owner with views) would fit the goal of static safety better. Resolution: make it a compile error where the move is visible, and keep the run-time stop for the rest.

## B21. Nil dereference in freestanding non-kernel builds

- Kind: underspecified. Severity: low. Promise: no.

Chapter 15 and 17 describe nil dereference faults for hosted builds (signal handler) and kernel builds (exception handler that calls `rt.PanicFault`). A freestanding non-kernel build may map page zero. Resolution: say the compiler emits explicit nil checks where the target gives no fault, or that the target must leave page zero unmapped.

## B22. debugging.md names an API that does not exist

- Kind: wording. Severity: low. Promise: no.

debugging.md 5 (Profiling, line 440): "Allocations through an allocator a task chose with `UseAllocator` are not seen." The only name for this in the spec is `alloc.Use` (18.9). Resolution: write `alloc.Use`.

---

## Checked and dropped (decided elsewhere)

- 13.4 `close` in an interrupt handler: abi 12.5 rule 6 says runtime locks mask interrupts in kernel mode.
- 18.3 views in variables used by literals: E3314 covers them.
- Scoped tasks on a panic: 19.4 "Non-local exits" decides it.
- Races on `fmt.ErrorBuf`: Rule 2 (19.3) decides them.
- debug-allocator line 81 "deferred calls do not run" matches 17.2 line 132 and debugging.md line 355.
- debugging.md 5 "freestanding and kernel builds run none" (deferred calls) matches 17.2.
- debugging.md 5 checked-build stale access text matches 18.11 and 21.5.
- 21.5 unchecked mode, "fail loudly" and epoch exhaustion match abi 1.3.
- 21.6 layout facts match abi.md.
- 20.4 ExitTask across `//pu:export` (panics at the boundary) matches abi 12.10.

## Sections not read closely

- Chapter 18, lines 1 to 300 (18.1 to 18.4): read before a context break, checked again only by grep.
- Chapter 14 and chapter 16 outside 16.5 to 16.7: read once, not cross-checked line by line.
- abi.md sections 2, 5 to 8 and 11 (layouts, frames, symbols): skimmed. They do not bear on safety.
- debugging.md sections 3, 4, 6 to 8 (DWARF, frame tables, sizes, reproducibility): skimmed.
- The `debug-allocator.md` implementation notes on quarantine sizing.
