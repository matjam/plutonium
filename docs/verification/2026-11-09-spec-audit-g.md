# Spec audit G (Plutonium 0.4, commit e0f1c988)

Scope: the audit F edits (2ce1fa44..e0f1c988), stale statements of D42 to D80, and abi.md sections 1 to
12 against the chapters. Paths are relative to the repository root. "18.9" means
docs/spec/language/18-ownership-and-memory.md section 18.9, and so on.

## 1. Rulings F1 to F15

| Ruling | Result | Notes |
|---|---|---|
| F1 drop entries | Holds, with gaps | Every listed case is decided by stated rules (see part 4), except interrupt context, where the unchanged Fixed paragraph and the E2584 check do not account for the entries (G2). Two wording gaps: G8, G13. |
| F2 generic interface conversion | Does not hold | Stated in 18.3 and appendix A, but `[T any]` admits maps, channels and counted boxes, which 7.9 lends uncounted. Checking them as frame copies only leaves a use after free (G1). |
| F3 frame storage in loops | Holds | 18.4 list, the `ps`/`bs` example and section 11 agree. Deferred-call arguments in loops are also bounded by E2530. |
| F4 atomic counter, plain allowed | Holds, with gap | The plain-counter permission names tasks, not interrupt handlers (G6). |
| F5 count rises only on success | Holds | abi.md 1.3 "only when it succeeds"; 18.9 "rises when `Alloc` hands out memory". |
| F6 18.13 example | Holds | |
| F7 deferred `Reset` example | Holds | 18.9 `handle`/`work` examples obey the order of 18.6 and F1. |
| F8 18.11 opening | Holds | The opening is fixed. The "or freed it" in "The rule." is a stale D46 place (part 2, item 1). |
| F9 deadlock report names the panic | Holds | debugging.md 5. |
| F10 "starting task" | Holds | 19.1, memory-model 3.8, abi 12.1 use it. |
| F11 kernel row in appendix A | Holds | |
| F12 `//pu:stack` out of range | Holds | 20.3 and 21.4: "a compile error whose code is not assigned yet". A deferred code is acceptable under item 207. |
| F13 21.4 defer rule | Holds | 21.4 matches 12.19 (E2530, E2531). |
| F14 `rt.DefaultStackSize` and guard | Holds, with gap | 19.1, 21.4 and abi 12.1 agree. F.2 lists `rt.MinStackSize` but not `rt.DefaultStackSize` (G12). |
| F15 limit errors | Holds | 21.4: "Like every compile error it has a code (section 21.1); the code is not assigned yet." |

Counts: 11 hold, 3 hold with gaps, 1 does not hold.

## 2. Stale statements (Task 2)

Seven places. Items 1, 2, 3 and 6 I confirmed by reading; the rest were found by a search pass and
checked against the decision text.

1. 18.11 "The rule.", lines 1031 to 1032: "after the arena has taken it back with `Reset`, `Release` or
   `Drop`, or freed it" (D46: arena memory is taken back only in bulk). Delete "or freed it".
2. 7.19 Representation, line 1732: "freeing into an arena does nothing" (D70). Say instead that it takes
   no memory back and lowers the arena's count of live allocations.
3. 20.10, lines 709 to 711: "Data that a handler accesses, other than a per-CPU variable, must be atomic
   or guarded by an `rt.Lock`" (D52). 19.3 Interrupt handlers now only requires this for data another
   party may write; data not written while a handler may run needs nothing. Restate as 19.3 does.
4. skill/plutonium/references/kernel-mode.md lines 204 to 205: the same handler rule (D52).
5. skill/plutonium/references/concurrency.md line 538: the same handler rule (D52).
6. skill/plutonium/references/kernel-mode.md lines 172 to 173: "The package is unsafe code except `Cli`,
   `Sti`, `SaveFlagsCli`, `RestoreFlags`" (D56). It omits `Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`, which
   20.8 makes safe.
7. skill/plutonium/references/language-tour.md lines 424 to 426: "lends it: the interface is a view of the
   variable", without the one-word copy rule of D71 (7.9: a one-word value is copied into the data word
   and is a view obtained from the variable).

Undecided, not counted: skill ownership-and-memory.md lines 287 to 293 list fewer D61 events
(incomplete rather than wrong); kernel-mode.md line 491 "Handlers: atomics and `rt.Lock` only" (a
summary line, ambiguous under D52); skill ownership-and-memory.md line 554 calls all of
pluto/arch/x86 unsafe (same issue as item 6 if read literally).

## 3. New findings

### G1. Generic conversion of a counted one-word value is unchecked (high)

- Kind: safety hole opened by F2.
- 18.3, line 232: "A conversion of a value of a type parameter to an interface counts as a frame copy,
  whatever the size of the type argument". Only "For a `[T owner any]` parameter" does it also count as a
  view ended by the D3 events.
- 7.18 Generics: "A type parameter without `owner` cannot be instantiated with an owner type". Maps,
  channels, `shared[T]`, `synced[T]` and `weak[P]` are counted references, not owners, so `[T any]`
  admits them.
- 7.9 Representation, lines 870 to 874: a channel or map in the data word "is lent ... the interface
  holds a copy of the variable's word and no count of its own", and "after `x := any(m)`, the assignment
  `m = nil` drops the map, and using `x` again is E3313". Section 7.9 lines 802 to 805 say the same for
  boxes and `weak`.
- Generic code is compiled per instance (abi.md 9 symbol table "generic instance"), so the instance
  uses the lent representation, while the checker, run once, treats the conversion as a frame copy only.

```go
func show[T any](x T) {
	var z T
	i := any(x)    // T = map[string]int: data word holds x's table, uncounted
	x = z          // drops the only reference: the table is freed
	fmt.Println(i) // accepted under F2; reads freed memory
}

show(make(map[string]int)) // the temporary's reference moves into x
```

- Resolution: apply the D3 view events to every conversion of a type-parameter value, not only for
  `[T owner any]`. The checker then gives the same result for every instantiation, as 18.3 promises, and
  matches what non-generic code already gets for maps and boxes. Update 18.3 line 232, appendix A row
  81 and the skill.
- Rejected: counting the copy in generic instances (adds a hidden count operation that non-generic code
  does not have, and makes the representation depend on genericity). Rejected: checking per instance
  (contradicts "checked once", D75).
- Changes a promise: yes. More generic bodies are rejected with E3313.

### G2. Drop entries break the Fixed "may run where the parent may not" promise (medium)

- Kind: F1 contradicts an unchanged rule; kernel goal conflict.
- 18.9 Fixed paragraph, lines 825 to 827: "allocating, `Reset` and `Release` never call the parent, cost
  no more than for an arena without bookkeeping, and may run where the parent may not (section 20.10,
  Interrupts)". abi.md 1.3 line 354: "allocation, `Reset` and `Release` never call the parent".
- 18.9 Drop entries, line 838: "`Reset` and `Drop` run every entry". An entry runs drop glue and user
  `Drop` methods, which may free to the parent, to the default allocator, or wait.
- 20.10 What is followed: the check follows "the drops the compiler inserts" and calls through func
  values. A drop entry is neither: it is a drop-glue pointer stored at run time and called by the
  arena. Nothing says the check follows it. Read one way, a handler that resets a per-CPU arena holding
  a vector from a waiting allocator deadlocks with no E2584. Read the other way, every `Reset` reaches
  every drop function in the program and no handler can reset an arena.
- Resolution: (1) in 20.10, state that a `Reset`, `Release` or `Drop` of an `arena.Fixed` or
  `arena.Chunked` is followed into the drop glue of every type passed to `New` or `Slice` of that arena,
  and of any arena when the compiler cannot tell which; (2) in 18.9 and abi.md 1.3, say these calls never
  call the parent for bookkeeping, and that the drops their entries run are followed like any drop.
  An arena of types without drop glue keeps the old promise in full.
- Rejected: forbidding drop-glue types in arenas used from handlers (a new rule with no gain over the
  precise check). Rejected: leaving it to the conservative func-value rule (makes ordinary handler
  resets fail for unrelated types).
- Changes a promise: yes, it narrows the Fixed promise to arenas whose entries cannot wait.

### G3. 5-level paging is a startup panic, not a compile error (medium)

- Kind: ABI contradicts a chapter; the panic is in no chapter.
- abi.md 1.3 line 423: "under 5-level paging (CR4.LA57) it panics at startup, before `main`." The next
  sentence says otherwise "the operation is a compile error".
- 21.5 lines 148 to 150: "If the target cannot provide protection and proof is incomplete, compilation
  rejects the operation". 21.2 line 77: "A feature that a target cannot provide is a compile error".
  17.1 does not list a startup panic.
- Resolution: paging mode is chosen by the kernel at run time, so a compile-time rule cannot see it.
  State in 21.5 (and list in 17.1) that a checked kernel or freestanding build requires 4-level paging and
  panics before `main` otherwise; drop the contradictory "otherwise ... compile error" clause from abi.md,
  or make the paging mode a target setting so a build for LA57 is rejected.
- Rejected: silent unchecked fallback (breaks the safety goal).
- Changes a promise: yes, it adds a stated startup failure.

### G4. `pluto_gen_metadata` and E2588 appear only in abi.md (medium)

- Kind: ABI promise to programs that no chapter states.
- abi.md 1.3 lines 419 to 422: "through a function it exports as `pluto_gen_metadata` ... A checked
  build with no such function, or with another signature, is rejected (E2588)."
- No chapter names the function or E2588. 21.1 line 61: "Each rule is stated by the chapter that
  defines it, together with its code".
- Every checked kernel program must write this function, and it must not allocate. That is a program
  obligation.
- Resolution: state it in 20.5 (Freestanding) or 21.5 with the signature, the page-aligned memory never
  returned, the no-allocation rule, and E2588.
- Changes a promise: no; it moves an existing requirement into the chapters.

### G5. `rt.Park` with preemption disabled panics, and no chapter says so (medium)

- Kind: ABI promise to programs that no chapter states.
- abi.md 12.4 line 1359: "`rt.Park` with a non-zero count panics with "blocking with preemption
  disabled"." The count is raised by `rt.DisablePreemption` and by runtime locks such as `rt.Lock`.
- 17.1 lines 82 to 83 list only parking "in interrupt context". 19.1 line 88 and 19.3 line 190 describe
  preemption-disabled regions but say nothing of a wait inside one. `rt.DisablePreemption` and
  `rt.Lock` are safe (F.2 does not mark them unsafe), so safe kernel code that receives on a channel while
  holding an `rt.Lock` hits this.
- Resolution: add "an operation that would park the task while its preemption is disabled" to 17.1 and
  19.1. A compile error in 19.3 for a wait reachable inside a non-migrating region would be better and can
  reuse the E2584 path search; I prefer it, with the panic as the backstop for calls the check cannot see.
- Changes a promise: yes if made a compile error.

### G6. Plain-counter permission omits interrupt handlers (low)

- 21.5 line 183 and abi.md 1.3 line 387: "where it proves that no other task can reach the arena or
  anything allocated from it". 19.3 line 148 makes handlers parties "like a task". A handler that drops a
  string into a per-CPU arena would race a plain counter.
- Resolution: say "no other task or interrupt handler". No promise change.

### G7. "arena: out of memory" and the New/Slice failure are not in 17.1 (low)

- 18.9 line 862: "A failed `New` or `Slice` panics (section 17.1)". abi.md 1.3: "panics with "arena: out
  of memory"". 17.1 has a generic "out of memory" (line 68) but no arena `New`/`Slice` entry.
- Resolution: add the arena failure to 17.1. No promise change.

### G8. Allocation during the entries: chapter and ABI differ (low)

- 18.9 line 857: "An allocation from the arena made while its entries run is counted as usual ... Either
  one makes the call panic". abi.md 1.3 step 2 panics only when the count "is not zero, or when an entry
  was recorded". A counted allocation made and freed within the entries leaves the count at zero, so
  under the ABI it does not panic.
- Resolution: say "An allocation still live when the entries finish, or an entry recorded while they
  run, makes the call panic". No promise change in practice.

### G9. Second waiter on one Scope (low)

- abi.md 12.10 line 1563: "A second task waiting on the same `Join` panics with "sync: two tasks wait
  on one Scope"." 19.4 and 17.1 do not state it.
- Resolution: add it to 19.4 and 17.1 Control.

### G10. E5007 not in a chapter (low)

- abi.md 10.6 line 1206: "checks that `pluto.percpu.self` is at address 0 and fails otherwise (E5007)."
  20.9 line 565 states the placement but no code.
- Resolution: add E5007 to 20.9.

### G11. abi.md version header is 0.2 (low)

- abi.md line 3: "**Version:** 0.2 (draft)". 21.6 makes abi.md part of the spec; 21.8 "There is one
  version of the language." Set it to 0.4.

### G12. `rt.DefaultStackSize` missing from F.2 (low)

- 19.1 line 65 and 21.4 line 121 name `rt.DefaultStackSize`; F.2 lines 52 to 58 list `rt.MinStackSize`
  only. Add it.

### G13. Zero-length `Slice` of a drop-glue type (low)

- 18.9 line 836: an entry is recorded when the type has drop glue, with no exception for zero elements.
  `Slice[T](0)` then takes table room and can fail on a full Fixed table. State that a zero-length
  `Slice` records no entry.

## 4. Checked and dropped

- Views between nodes in one arena: entries run before the epoch ends; a drop that reaches an earlier
  node finds valid zeroed memory (18.9 Drop entries, second bullet). Drop glue never follows views.
- Drops that free to other arenas or the parent: the free happens in the calling task (memory-model 3.8)
  and lowers that allocator's count; ordering between arenas is decided by the count rule.
- `take` and zero assignment: the place holds zero and its entry does nothing (18.9, first bullet).
- Owner moved into a pre-mark node after the mark: `Release` checks the whole count (18.9 Live
  allocations).
- Plain views reaching another task: any free racing a use is a write under "Frees are writes" (19.3 line
  218) and is rejected or caught by the epoch check; the in-place zeroing by entries is the same class as
  the old granule zeroing.
- `owned[T]` from `new(T, &a)` versus a `New` view: the box is counted and records no entry; no double or
  missed drop.
- Nested and out-of-order marks: "A foreign mark or one invalidated by reclamation ... panics";
  "Releasing a later mark does not invalidate an earlier still-live mark" (18.9 lines 947 to 949).
- Panic in an entry then a deferred `Reset`: the entry was taken off first, so it leaks rather than
  drops twice, and the deferred `Reset` resumes from the rest (18.9 third bullet; 17.2 for a panic in a
  deferred call).
- `NewUnsafe`: records no entries; its typed methods are unsafe (18.9 last bullet, F.2 line 134).
- Generic `New[T]`: an entry depends on the instance; the capacity effect is stated.
- Unions and enums: zero value is the first variant with a zero payload (7.13); `Drop` treats zero as
  empty (18.6).
- Arrays of owners: an entry holds an element count and drops last element first.
- Per-CPU arenas and a full Fixed table in the kernel: per-CPU rules (20.9) and the stated capacity rule
  decide them; only the drop reach is open (G2).
- F2 for `[T owner any]`: the stricter of both rules is stated and consistent with 7.9.
- F3 and `defer` in a loop: E2530 already bounds the saved arguments.
- abi.md panic wordings: 17.1 and 21.1 say wording is not part of the language.
- abi.md items the Task 3 pass checked and found stated in chapters: count atomicity (18.7, memory-model
  3.7), map seed (7.10), zero-size addresses (20.2), ring layout (20.2), kernel float printing (20.7),
  E2570 to E2573, E2550 to E2552, E2510 to E2513, the hosted 1 MiB guard (21.4, 20.3), exit with running
  tasks (19.1).

## 5. Sections not read closely

- abi.md was read in full by a delegated pass; I read 1.3 (lines 348 to 426) and 12.4 myself. Cross-checks
  not made: union equality against 7.13; string argument borrowing (abi 3.3) against 18.8; release of
  init-time closure regions (abi 4.1 line 615) against 15.4; the 12.7 table's "from the requested
  allocator" for stacks against 12.1 and 19.1.
- Spec chapters outside the F diff were read only where a check needed them. Skill references were
  searched, not read in full.

## 6. Judgment

The F edits are mostly sound, and F1 survives every listed arena attack except interrupt context.
F2 is not closed: by limiting the D3 events to `[T owner any]` it leaves a use after free for maps,
channels and counted boxes in ordinary generic code, which is high. With G1 open and four mediums, the
spec does not meet the bar for closed.
