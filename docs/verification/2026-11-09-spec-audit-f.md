# Spec audit F (v0.4, 2ce1fa44)

Scope: the audit E edits E1 to E9, B5, and the completions A1, B1, C1, C2 and D3
(`git diff db2a1316 2ce1fa44 -- docs/spec`). Then chapter 20 from 20.2 against abi.md, appendix A,
memory-model.md from 4.3, debugging.md, debug-allocator.md and chapter 21.

## Part 1. Verdicts per ruling

| Ruling | Verdict | Place and reason |
|---|---|---|
| E1 | Incomplete | 18.3 now says a one-word value lent to an interface is a copy whose view the D3 events end. 18.3 lines 232-233 decide generic code by "if any instantiation is larger than a word", which a checker that checks a generic body once cannot know, and the sentence names only the frame-copy half. See F2. |
| E2 | Correct | Rule 3 is now limited to the package that defines the result type. Chains through three packages, aliases, embedding, unions and pointers to pointers are all closed. |
| E3 | Incomplete | The count now covers every allocation made through `Alloc`. 18.9 lines 806-826 claim that "a program that compiles drops those owners and releases those counted values before the arena is dropped". That is false for owners and counted values stored in plain views from `New` and `Slice`. See F1. The arena example (18.9 lines 1240-1253) still says the frees "do nothing". See F6. |
| E4 | Correct | 19.4 lines 555-587 make a deferred call a statement of the interval when a path runs the `defer` and then reaches a point of the interval. Loops, branches, literals, callers, a Scope reached through a pointer and `rt.ExitTask` are covered. Two low notes: F9 and F10. |
| E5 | Correct | The D3 events are in the closure rule and in the E3314 list (18.3 line 191). |
| E6 | Correct | 18.4 line 260 now says "allowed, unless the viewed variable" has cleanup. |
| E7 | Correct | Appendix A row 80 separates large owners, viewed in place, from one-word owners, copied. |
| E8 | Correct | Row 111 cites the right section. |
| E9 | Correct | The wording near "boxes" in 18.9 is clear. |
| B5 | Correct | `%w` keeps the wrapped error, as in Go. Applied in the diff. Not attacked further. |
| A1 | Correct | Applied through E6. |
| B1 | Correct, with F1 | Follows E3. The count is consistent in 13.7, 13.9, 16.7, 18.9, 21.5, F.2, memory-model 3.8 and abi.md 1.3. The gap in F1 is in the E3 sentence. |
| C1 | Correct | Applied through E2. 8.11 and the 7.18 summary now state the limit. |
| C2 | Correct | The lent result is a view obtained from `x`, so the D3 events end it. |
| D3 | Correct | The events are listed in 18.3, the closure rule and the E3314 list. The generic gap is filed under E1 (F2). |

Counts: 13 correct, 2 incomplete, 0 wrong.

## Part 2. Findings

### F1. Owners in plain arena memory keep the count up (medium)

Kind: goal conflict and false statement. Sections: 18.9 "Live allocations" (lines 806-826), 21.5
(lines 169-174), abi.md 1.3.

18.9 says: "An owner or counted value stored in memory the arena handed out as a plain view is never
dropped by the compiler, so it keeps the count up until the program drops it or clears that place."
It then says that for a local arena "a program that compiles drops those owners and releases those
counted values before the arena is dropped, and the count stays a run-time check only for a `Reset`
or `Release` that comes earlier." The compiler never drops the fields of a node from `New`, so the
second sentence does not hold.

Example:

```go
func build() {
	a := arena.NewFixed(buf[:])
	n := a.New[Node]()            // Node has kids vec.Vec[int] and name string
	prev := alloc.Use(&a)
	n.kids.Push(1)                // Alloc on a: count 1
	n.name = fmt.Sprintf("n%d", 1) // Alloc on a: count 2
	alloc.Restore(prev)
} // a is dropped: count 2, panic
```

It compiles. The drop of `a` panics. To avoid it, code must walk every node and drop each field
before `Reset`, which defeats bulk free, the main reason to use an arena. A tree or graph of nodes
with growable fields is ordinary arena code.

Proposed resolution: for `New` and `Slice` of a type with drop glue, the arena records a drop entry
(address, element count, drop function) in its bookkeeping. `Reset`, `Release` and `Drop` run those
entries in reverse order and then check the count. Types without drop glue cost nothing. This is
the model of Rust's `typed_arena`. Fix the 18.9 sentence either way.

Rejected: (a) arena-bound collections such as `a.Vec[T]` that never count, which do not cover
strings or maps; (b) sending such code to `NewUnsafe`, which breaks "no annotations"; (c) the status
quo with only the sentence corrected, which leaves the trap for ordinary code.

Changes a promise: yes. `Reset` would run drops, and the 18.9 guarantee changes.

### F2. E1 in generic code (medium)

Kind: hole in the checking model. Sections: 18.3 lines 232-233, 7.x `[T owner any]` (E3207).

18.3: "A generic function is checked once for all its instantiations. A conversion of a value of a
type parameter to an interface counts as a frame copy if any instantiation is larger than a word."
A body checked once does not know its instantiations, and the sentence says nothing about the D3
events that E1 now applies to one-word values.

```go
func keep[T owner any](p *T, free func(*T)) {
	i := any(*p)   // one-word T: a copy; large T: a view in place
	free(p)        // D3 event for the copy
	use(i)
}
```

Proposed resolution: for a `[T owner any]` parameter, the conversion counts as both: a frame copy
and a view of `*p` that the D3 events end. The checker applies the stricter rule of each kind, which
does not depend on the instantiation.

Rejected: checking per instantiation (breaks "checked once" and makes errors depend on callers);
forbidding the conversion for owner type parameters (too strict for printing and logging).

Changes a promise: no. It states the existing rule for generic code.

### F3. 18.4 does not list frame storage after C7 (medium)

Kind: missing rule, possible false friend. Sections: 18.4 "Loop iterations reuse their storage",
ruling C7, 11.x composite literals.

C7 moved the `&T{}` object, the slice-literal array and the variadic array out of "temporaries" and
into frame storage that is refilled "when the same expression runs again". 18.4 lists variables,
temporaries, interface copies and closure records. It does not list the new kinds.

```go
var ps [3]*T
for i := 0; i < 3; i++ {
	ps[i] = &T{n: i}
}
```

Under one reading this is E3006. Under the other it compiles and all three elements point to one
object holding `n == 2`. In Go it gives three objects, so the second reading is a false friend with
no row in appendix A.

Proposed resolution: add frame storage (the `&T{}` object, slice-literal and variadic arrays,
method-value receivers and deferred-call arguments) to the 18.4 list, so the example is E3006 with
a fix that allocates (`new` through the allocator). Add an appendix A row.

Rejected: allocating per iteration (hidden allocation); one object per static expression with no
error (false friend).

Changes a promise: no. It closes a gap.

### F4. Cost of the atomic arena counter (low)

Kind: goal conflict (performance near Rust, kernel). Sections: 21.5 lines 169-174, abi.md 1.3.

Every `Alloc` and free through an arena is a locked read-modify-write. A per-CPU kernel arena never
shared across tasks pays it on every allocation.

Proposed resolution: allow a plain counter when the arena is a local that no task can reach (the
compiler already proves this for scoped tasks), or state the cost as accepted in 21.5.

Rejected: dropping the count (reopens B1).

Changes a promise: no.

### F5. The count when an allocation fails (low)

Kind: wording. abi.md 1.3: "Alloc adds one with relaxed ordering." 18.9: the count rises "when Alloc
hands out memory". abi.md does not say what happens when `Alloc` fails. Proposed: abi.md says the
count rises only on success. Changes a promise: no.

### F6. The arena example says the frees "do nothing" (low)

Kind: wording. 18.9 lines 1240-1253: "reply, then fields, are dropped: their frees go to the arena and
do nothing". Under E3 each free lowers the count. Proposed: "their frees go to the arena, which lowers
its count and keeps the bytes". Changes a promise: no.

### F7. Deferred `Reset` of a longer-lived arena (low)

Kind: Go-style trap. Sections: 18.9, 12.19.

```go
func (s *Server) handle() {
	defer s.arena.Reset()
	prev := alloc.Use(&s.arena)
	defer alloc.Restore(prev)
	msg := fmt.Sprintf(...) // counted on s.arena
}
```

The deferred calls run before `msg` is dropped (row 111), so `Reset` always panics. It follows the
rules, but it is the natural Go shape. Proposed: add this example and its fix (a nested block,
or a helper function) to 18.9, or fold it into F1 if `Reset` comes to run drops. Rejected: a new
compile error, since a deferred `Reset` is correct when nothing was allocated in the window. Changes a promise: no.

### F8. 18.11 claims a compile-time proof that does not hold (low)

Kind: wording. 18.11 opens: "The rules of section 18.3 prove at compile time that no view ... of a
local owner outlives its memory". A same-call alias, `f(any(b), &b)` where `f` frees through the
pointer, is caught by the tag check at run time, not at compile time. Safe in checked builds.
Proposed: "prove at compile time, or the generation check catches at run time". Changes a promise: no.

### F9. Deferred `s.Wait()` during a panic can hang (low)

Kind: behavior. 19.4 fix example arms `defer s.Wait()` before `s.Task`. In hosted builds a panic runs
deferred calls. If a scoped task waits on the panicking task (the crawl example never closes `jobs`),
`Wait` blocks and the panic becomes a deadlock report instead of a panic report. Proposed: state it in
19.4, and let the deadlock report name the panic in progress. Changes a promise: no.

### F10. "when the task panics" is ambiguous (low)

Kind: wording. 19.4 line 557. It reads as the scoped task, but it means the task that runs the
deferred call. Proposed: "when the starting task panics". Changes a promise: no.

### F11. Appendix A row 164 misses two kernel differences (low)

Kind: missing rows. Row 164 lists only "kernel-mode print of a float writes its bit pattern (13.12)".
20.7 says `pluto/fmt` prints a float as its bits in a kernel build. 20.6 says type descriptors omit
field names, so `%+v` prints without them. Both differ from Go and are visible only in output.
Proposed: add both to row 164. Changes a promise: no.

### F12. `//pu:stack` out of range has no code (low)

Kind: missing code. 20.3 names E2603 (malformed) and E2602 (wrong function) but gives no code for a
size outside 64K to 64G, and 21.1 requires one. Proposed: use E2603 or a new code. Changes a
promise: no.

### F13. 21.4 says a `defer` in a loop is an error (low)

Kind: contradiction. 21.4 line 116: "A `defer` statement inside a loop is an error (section 12.19,
Defer statements)." 12.19 allows it when every path leaves the loop first (E2530). Proposed: "A
`defer` statement that a path can reach twice in one call is an error". Changes a promise: no.

### F14. Kernel default task stack has no stated bound (low)

Kind: missing limit. 19.1 item 3: "In freestanding and kernel builds the task gets the default task
stack size that the implementation documents (section 21.4)". 21.4 does not list it, and gives no
floor beyond `rt.MinStackSize`. Overflow behavior depends on the backend's guard (19.5). Proposed:
list the default in 21.4 with a floor, and state that a backend must provide the guard region of
19.1 or the build is rejected. Changes a promise: no.

### F15. Implementation-limit errors have no code (low)

Kind: missing code. 21.4: a program that exceeds a limit "is a compile error that names the limit".
21.1 requires every diagnostic to have a code, and no range covers limits. Proposed: assign one code
(in E9000 to E9999 or E0xxx) for "exceeds an implementation limit". Changes a promise: no.

## Part 3. Checked and dropped

- A view of a union payload: not addressable (11.24 note), so E1 does not apply.
- The same-call alias as a safety hole: the tag check catches it (only the wording in F8 remains).
- Nested `alloc.Use` marks: the whole-count rule is stated.
- An arena whose parent is an arena: frees go to the child, counts are per arena.
- A panic during an arena's drop: nothing is dropped after a panic.
- E2 forging through aliases, embedding, unions and pointers to pointers: closed.
- 20.10 handler passed as `any` (`x86.InterruptGate(h any)`) against E2573: an explicit exception.
- Integer overflow wraps and a store into a nil map panics: same as Go.
- Contextual keywords (`owner`, `union`, `task`, `or`, `bits`, `le`, `be`): listed in appendix B.
- memory-model 4.3 to 7: consistent with 20.9, 20.10 and 3.8.
- debug-allocator.md: no deferred calls on its panics, consistent with 17.2.
- debugging.md: fault and panic paths, deferred calls in hosted builds only, the 64-frame trace limit,
  and the deadlock report match chapters 17 and 19. The allocation profiler failing under generation
  checks is listed under "Not done yet" and is not a spec rule.
- The `//pu:inline` growth limit: exceeding it only leaves a call out of line, which is stated.
- Constant precision (5): bounds and behavior stated, as in Go.

## Part 4. Sections not read closely

- Chapters 4 to 13 were read only through the appendix A rows that cite them, not end to end.
- abi.md beyond 1.3 and section 6 was compared only where chapter 20 cites it.
- What happens when a task cannot get even 64 KiB of stack (19.1 item 3) was not checked.
- B5 was confirmed as applied but not attacked.
- debugging.md sections 6 to 9 (measurements and plans) were skimmed.
