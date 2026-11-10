# Spec audit H: version 0.4 at a7ae2571

Scope: the audit G edits (e0f1c988..a7ae2571, docs/spec), then chapters 18 and 19 in full, plus 7.9, 7.18, 7.19, 8.7, 8.8, 11.4, 11.5, 11.13, 11.24, 12.9, 12.10 and 12.19. Read-only.

## 1. Verdict per G ruling

| Ruling | Verdict | Notes |
|---|---|---|
| G1 | Resolved, with a cost | The bullets in 18.3 agree with 7.9, 8.7 and appendix A row 81. "Checked once" is consistent: the result is the same for every instance, taken over the instantiation set of the whole program. Per-instance checks in 19.2 (sendable) and 20.11 (E2584) are for other rules and say so. The cost to ordinary code is in A81. One cost is not listed there (H6). The bullets name only "a value of a type parameter", and the lead sentence covers `any(Box[T]{x})` (H7). |
| G2 | Resolved, but opens a gap | "Every arena where the compiler cannot tell which" can be computed by whole-program points-to analysis. `alloc.Use` does not matter, because drop entries come only from `New` and `Slice` calls on an arena. The narrowed wording leaves out reclaiming values without drop glue (H1). The precision has no stated limit (H8). |
| G3 | Resolved | abi 1.3, 17.1, 20.5 and 21.5 all say "panics before `main`". No compile-error clause is left. |
| G4 | Resolved | `pluto_gen_metadata` and E2588 are in 20.5, 21.5, F and abi 1.3 and 8. The 21.5 citation of 20.4 for `//pu:export` is right. |
| G5 | Resolved for the rule, with problems in the backstop and the cost | The "every path" condition is consistent with how locks are found. The backstop does not cover Cli regions (H3). The 20.10 precision makes the check reject common kernel patterns (H2). The runtime's own regions are not exempted (H4). |
| G6 | Resolved | 21.5 line 205 and abi 1.3 say "no other task or interrupt handler". |
| G7 | Resolved | 17.1, 18.9 and abi 1.3 have "arena: out of memory". |
| G8 | Resolved | 18.9 and abi 1.3 agree. |
| G9 | Resolved | 19.4, 17.1 and abi 12.10 agree. |
| G10 | Resolved | 20.9 names E5007, as abi 10.6 does. |
| G11 | Resolved | All four documents say "0.4 (draft)". |
| G12 | Resolved | F.1 has `rt.DefaultStackSize`. 19.1, 21.4 and abi agree on 64 KiB. |
| G13 | Resolved | 18.9 and abi 1.3 agree. |
| Stale 1 to 3 | Resolved | Checked in 18.11, 7.19 and 20.10. |
| Stale 4 to 7, Ambiguous 1 to 3 | Not checked | These are skill reference files outside docs/spec. |
| Cross-checks 1 to 4 | Accepted | Only 4 was rechecked (19.1 now names the `rt.Go` allocator). 1 to 3 were taken as stated. |

## 2. Findings

### H1. Resetting an arena does not count as freeing values without drop glue (high)

**Kind:** safety hole that depends on the reading; contradicts memory-model 3.8 and 4.1.

**Text:**
- 19.3, Frees are writes, the G2 sentence: a Reset, Release or Drop "counts as dropping every value that `New` or `Slice` allocated in that arena with a type that has drop glue".
- 7.19 (line 1732): "freeing into an `arena.Fixed` or `arena.Chunked` takes no memory back".
- 18.9: an arena "reclaims it all at once when it is reset". So "free" and "reclaim" are different words.
- Rule 2's type rule: "no code ... frees, drops or moves one ... once tasks have started". Initializing writes do not count.
- memory-model 3.8: "A free that races with a use is a data race ... safe code cannot make one".
- Competing reading, 21.5 line 206: "a reset could free the bytes of a string".

**Example** (`P` is a struct of ints; `ar` is a package-level `arena.Chunked`):

```go
p := ar.New[P]()
p.x = 1
task worker(p) // P meets the type rule: nothing frees, drops or moves a P
ar.Reset()     // counts only as dropping values with drop glue; P has none
q := ar.New[P]()
q.x = 5        // an initializing write to the same memory; worker reads p.x
```

**Why the runtime does not catch it:**
- The live count (3.8) keeps the arena from reclaiming strings and owners, but plain values are not counted.
- The epoch check in `worker` is not atomic with the read (3.8; 18.11).

**Scoped variant:** the same code inside a Scope with `ar` as a parameter is probably caught. The type-based conflict rule in 19.4 ("Memory reached through views") sees the write to `q.x` against the task's read of a `P`.

**Resolution:** in 19.3 and 18.9, state that a Reset, Release or Drop counts as a free (a write of the whole object) of every allocation it reclaims, of every type: everything in the arena, or everything after the mark for Release. The drops through drop entries come on top of that. For an arena the compiler cannot tell apart, apply this to every such arena.

**Rejected alternatives:**
- Count plain allocations at run time: extra cost, against "performance near Rust".
- Leave it to epochs: they cannot make a race safe (3.8).

**Changes a promise:** no. It narrows what compiles, toward the promise already stated in 4.1.

### H2. The region check rejects common kernel code, because of how calls are followed (medium)

**Kind:** goal conflict (Kernel, Plain).

**Text:**
- 19.3, Waiting in a region: "from each region it follows every call by the rules of section 20.10 (What is followed)".
- 20.10: a call through an interface reaches "`m` of every type the program converts to `I`".
- 20.10, Allocation: "the check follows that call into every allocator the program installs".
- 20.10, Strings: a dropped string is an error "only if the `Free` of an installed allocator may wait".

**Example:**

```go
mu.Lock()          // rt.Lock: a non-migrating region
dev.ops.Poll()     // an error if any type converted to the ops interface may wait
buf := make([]byte, n) // an error if any installed allocator's Alloc may wait,
mu.Unlock()            // even with alloc.Use of a spin-locked pool
```

**Why this matters:**
- For interrupt handlers this cost was accepted because handlers are small.
- Regions are everywhere in a kernel: every `rt.Lock`, every Cli and every per-CPU update.
- A kernel with one page allocator that may sleep could then never allocate, and never drop a counted value, under any lock.
- 19.3 itself calls a wrong pairing "a liveness failure, not a safety one".

**Resolution:**
- Report exact paths only: direct calls, hooks, a literal called in place, and concrete receivers.
- For an allocation, follow the allocator installed at that point when the compiler knows its type (an `alloc.Use` in the same function), and every installed allocator otherwise.
- Leave paths that go through imprecise calls to the run-time backstop. Extend the backstop as H3 says.

**Rejected alternatives:**
- An annotation such as "may not wait": violates "no annotations for ordinary code".
- The current text: ops tables and allocation under locks are ordinary in a kernel.

**Changes a promise:** yes. Item 210 promises a compile error. It would become "a compile error on exact paths; a panic otherwise".

### H3. The backstop does not cover interrupt-disabled regions (medium)

**Kind:** two texts give different behavior.

**Text:**
- 19.3: a wait the check cannot see "still fails at run time: `rt.Park` with preemption disabled panics".
- abi 12.4: the preemption count is raised by `DisablePreemption`, runtime locks, `rt.ProcPin` and task switching. `x86.Cli` does not raise it.
- 17.1 lists the panic only for `rt.DisablePreemption` and `rt.Lock`.
- 19.3 counts a Cli region as non-migrating.

**Example:** `x86.Cli()`, then a call through a func value that parks. The check sees no exact path, and `rt.Park` does not panic. The task switches away with interrupts off, and the processor may hang.

**Conflict:** this breaks 21.2 ("no silent failures") and the claim in 19.3.

**Resolution:** in kernel mode, `rt.Park` also panics when the saved flags show interrupts disabled, with the same message. State this in abi 12.4, 17.1 and 19.1. It is one flag test on a path that is already slow.

**Rejected alternative:** narrowing the claim in 19.3. That leaves a silent hang.

**Changes a promise:** no. It makes the existing promise true.

### H4. The runtime's own locked code is not exempted from the region check (low)

**Text:**
- 20.10: "A path ends at the runtime's `park`".
- abi 12.5: channel and Scope code takes a runtime lock and then parks.
- 19.3 "What is not checked" exempts runtime variables only from the race rules.

**Problem:** read literally, `pluto/rt` fails its own check.

**Resolution:** state that the check starts from regions in code outside `pluto/rt`, or that `park` and its lock handoff are the runtime's contract (abi 12.4).

**Changes a promise:** no.

### H5. "Borrow" is used as a term but never defined (low)

**Kind:** wording.

**Text:**
- 19.3, Owner transfer requires exclusivity: "outstanding alias", "borrows made through fields, helpers and aliases".
- 8.8 says the same.
- Neither names an error code. Elsewhere the word only appears as "no ... borrow ... annotations".

**Resolution:** define the term as a view still live at the move (the events of 18.3 end views of the owner). Name the code: E3501, or "a compile error" until one is assigned.

**Changes a promise:** no.

### H6. Generic code that keeps an interface made from `T` is rejected for every `T`, and A81 does not list it all (low)

**Text:**
- 18.3, first G1 bullet: "the interface may not leave the frame".
- 18.3, third bullet: a method result of `T` "is bounded by the variable".
- A81 lists the conversion "even for a pointer-sized type argument".

**Examples:**
- `func Wrap[E error](e E) error { return e }`.
- `list = append(list, any(x))` into a heap slice (E3002).
- `return x.Bytes()` from a `T` parameter (E3001). This one is not in A81.

**Workaround:** use the interface type instead of a type parameter, or take `*T`.

**Resolution:** add the method-result bound to A81.

**Rejected alternative:** checking each instance, as 20.11 does for E2584. That costs compile time and gives instance-dependent errors in libraries.

**Changes a promise:** no.

### H7. The G1 bullets name only "a value of a type parameter" (low)

**Text:** 18.3, first bullet: "A conversion of a value of a type parameter to an interface".

**Example:** `any(Wrapper[T]{x})` with a one-field struct is lent in one word when `T` is a map.

**Status:** the lead sentence ("the strictest form that any type argument ... needs") decides this case, so it is not a hole.

**Resolution:** reword as "a value whose type contains a type parameter".

### H8. G2 precision has no stated limit (low)

**Text:** 19.3 and 20.10: "any arena where the compiler cannot tell which".

**Problem:** the points-to summaries in 18.3 state their depth, but this does not. Two arenas such as `workers[i].arena` cannot be told apart, so E3404 or E2584 can fire for types never allocated in the arena actually reset.

**Status:** the general precision rules of 18.3 arguably decide this.

**Resolution:** cite the 18.3 summary limits.

### H9. "Region" has three meanings (low)

**Text:**
- the closure region (11.5, 18.3 line 71, defined in bold);
- the non-migrating and interrupt-disabled regions (19.3, in bold);
- "region annotations" (1, 18.3, 19.3).

**Resolution:** call the 19.3 ones "non-migrating stretch" or keep "region" and qualify the 11.5 one as "closure region" everywhere. Most uses already do.

## 3. Checked and dropped

- D85 "every path to the operation" against 19.3 "a region on every path": 19.3 is clear, and D85 is the decision record.
- Deferred calls that run inside a region (`defer mu.Unlock(); defer ch.Send()`): deferred calls are followed (20.10), and deferred releases count at the end of the function.
- The wait lists in 19.3, 19.5 and 20.10 differ, but all are examples that end at `park`.
- `fmt.Println(x)` and `...any` with `x T`: the frame view is not kept. This is fine.
- `[T owner any]` converted to an owning interface: a frame copy, as 7.9 says for owners larger than one word.
- A scoped-task variant of H1 is caught by the type-based conflict rule of 19.4.
- 12.9 type-switch owners, 12.10 iteration storage, 12.19 defer owners and 11.24 owner conversions are consistent with 18 and 7.18.
- The 18.13 arena example: the drop order (reply, fields, a) and the E3001 note are right.
- The G4 wording "never returns" against "never takes back or uses for anything else" has the same meaning.
- The 21.5 citation "section 20.4" for `//pu:export` is right.
- The E3313 event lists in 18.3, 12.9, 18.9 and the skill reference (Ambiguous 1) agree with D61.

## 4. Sections not read closely

- 11.24: only the ownership and type-parameter parts were read.
- 8.8 after line 482.
- 18.13 between lines 1228 and 1380: skimmed for error claims only.
- The skill reference files (Stale 4 to 7, Ambiguous 1 to 3).
- abi.md outside 1.3, 12.4, 12.5 and 12.10.
- Cross-checks 1 to 3 were not rechecked.

## 5. Judgment

- Not closed. There is one high finding (H1) and two medium ones (H2, H3).
- H1 is a small text fix that does not change any promise.
- H2 is the main design question: G5's compile error, with handler-level precision, is too strict for regions in a kernel.
- H3 is a one-line runtime change.
- The rest is wording.
- G1, G3, G4 and G6 to G13 hold.
