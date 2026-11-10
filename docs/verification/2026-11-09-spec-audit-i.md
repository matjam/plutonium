# Spec audit I (version 0.4, commit 279c61ef)

Scope: the audit H edits (`git diff 4c389b94 279c61ef -- docs/spec`), and a closing read of chapter 1, 8.7, 8.8, chapters 18 and 19, 20.9, 20.10, 21.5 and memory-model.md.

Bar: the spec is closed only if no high or medium finding remains. Three medium findings remain (I1, I2, I3), so it is not closed by that bar. None is high.

## 1. Verdicts on the audit H edits

| Item | Verdict | Notes |
|---|---|---|
| H1 arena frees of every type | Holds, with I1 and I7 | The rule is stated in 19.3 (Frees are writes), 18.9 (Drop entries), 18.3 (E3313 events), 12.12, memory-model 3.8 and 4.1. 8.8 is not updated (I6, low). An arena behind a parameter or a heap pointer is covered by "for a pointer by where the pointer came from"; roadmap item 196 is a compiler gap, not a spec hole. |
| H2 exact paths only | Holds in intent; definition is ambiguous (I2) | Matches 20.10 "follows the same calls but reports only exact paths". |
| H3 park tests IF in kernel mode | Holds; the summary claim is too wide (I3) | 17.1, 19.1, 19.3, abi.md 12.4 agree. The hosted backstop is the preemption count (19.1: rt.Lock and rt.DisablePreemption disable preemption), and Cli faults there. |
| H4 runtime exemption | Holds | 18.12 bullet, 19.3 (Waiting..., What is not checked), memory-model 4.2. A user region that reaches `park` is still reported. The 1.4 glossary does not list `pluto/rt` (I6, low). |
| H5 "view" wording | Holds | 19.3 Owner transfer uses "view", E3313, E3314, E3501 consistently with 18.3. |
| H6 A row 81 | Holds | Adds `return x.Bytes()` E3001 and the fix. |
| H7 G1 bullets | Holds | "a value whose type contains a type parameter (`x` of type `T`, or `Box[T]{x}`)". |
| H8 precision of arena naming | Holds, with I7 | Cites 18.3, but does not say what happens beyond the limits. |
| H9 region rename | Holds, four leftovers (I5) | No sentence changed meaning. Bare "region" in the closure sense: debugging.md line 231, 11.6 line 534 ("caller-region storage"); in an alloc.Use sense: 10.10 line 800, debug-allocator.md line 62. |
| Fix 7.9 interface clash | Correct | Both lines marked, plus "an implementation may report it at either method". |
| Fix 7.18 `syscall.Close(int(f.fd))` | Correct | `fd` is `int32`. |
| Fix 10.10 `j int` | Correct | |
| Fix 11.6 `obj.p[i].x()` | Correct | |
| Fix 11.7 `q.M0()` E0076 | Correct | E0076 is MissingFieldOrMethod. |
| Fix 11.18 `v[i]`, `ok \|\| g()` | Correct | |
| Fix 11.24 `uint(1 << 10)` | Correct | |
| Fix 11.25 "illegal:" marks | Correct | |
| Fix 11.27 `println(cfg.name)` | Correct | |
| Fix 12.17 goto E0123 | Correct | E0123 is JumpOverDecl. |
| Fix 12.17 goto E0124 | Correct | E0124 is JumpIntoBlock. |
| Fix 15.1 `var j int = 0` | Correct | |

Counts: 9 rulings hold (H2 and H3 each with a medium finding against their wording), 12 example fixes correct, 0 wrong.

## 2. Self-claims

| Claim | Where | Backing rules |
|---|---|---|
| "Safe code has no data races" / "the compiler rejects every program in which two tasks ... could make such accesses" | 19 intro, 19.3, memory-model 4.1 | 19.2, 19.3 rules 1 to 3, Guards, Frees are writes, Interrupt handlers, 19.4. Partly unbacked by a named rule for memory reached through a package variable (I1). |
| "in safe code no task frees memory that another task may still use" | 19.3 Frees are writes | Frees are writes, rule 2 type rule, owner transfer, 18.9 live count. Same gap as I1. |
| "an object of the arena that another task may still read stops the program from compiling" | memory-model 3.8 | 19.3 Frees are writes, rule 2. Same gap as I1. |
| "every wait in a non-migrating region therefore fails to compile or panics ...; none hangs the processor in silence" | 19.3 | 19.3 check, 17.1, abi.md 12.4. Unbacked for `//pu:blocking` functions that do not park (I3). |
| "rejects what it cannot prove safe and never accepts on doubt" | 1.2 principle 1, 21.2 | Backed for safety analyses. Worded as "every static analysis", which the wait check is not (I4). |
| "Run-time checks ... never change what correct code means" | 1.2, 21.5 | 21.5 unchecked mode keeps static rules, count and drop entries. Backed. |
| "Heap generations and arena epochs never wrap" | 1.2 principle 4 | 21.5, 18.11, abi.md 1.3. Backed. |
| "The compiler never silently promotes a local to the heap" | 1.1 | 18.2, 18.3 E3001 to E3006. Backed. |
| "Unchecked behavior is reachable only across an explicit unsafe source boundary or the explicit unchecked execution choice" | 1.3 | 18.12, 21.5. Backed. |
| "preemption cannot be observed by the program" | 19.1, memory-model | memory-model 3, abi.md context switch. Backed. |
| Static memory "is never freed" | 18.3 | 18.12 (memory made from an integer is unsafe). Backed. |
| "A data race in unsafe code ... is never undefined behavior in the compiler's sense" | 19.3, 18.12, memory-model 4.2 | memory-model 5.2. Backed. |
| Strings are sendable "because their bytes never change" | 8.8, 19.2 | 18.8, unsafe.String excluded. Backed. |
| "A misspelled or misplaced directive never silently does nothing" | 20.3, 21.2 | 20.3 E codes. Backed. |

Unbacked claims: 2 (the silent-hang claim of 19.3, and the package-variable part of the race-freedom claims, counted once).

## 3. New findings

### I1. Memory reached through a pointer in a package variable is not named by rules 1 to 3 (medium)

**Kind:** reading-dependent behavior with a safety consequence.

**Sections:** 19.2, 19.3 (Rule 1, Rule 2, Guards), memory-model 4.1.

**Quotes:**
- 19.3 Rule 1: "A package variable, or a field or element of one".
- 19.3 Rule 2: "A pointer or slice that crosses to another task (section 19.2)".
- 19.2 lists what crosses: task arguments, channel values, values held inside those, scoped variables. A load from a package variable is not in the list.
- Rule 3 adds the case for maps only: "(or a task reaches one through a package variable or memory it did not allocate)".

**Example:**
```go
var b owned[Node]
var p *Node

func main() {
	b = new(Node)
	p = &*b        // written before any task: rule 1 is satisfied for p
	task reader()
	b = new(Node)  // frees the old Node: a write of it (Frees are writes)
}

func reader() { println(p.x) }
```
The same holds for a plain view of a package-level arena kept in a package variable and an `ar.Reset()` in another task. Rule 1 covers `p` and `b`, and each is fine on its own. The Node is neither a package variable nor a value that crosses. Only the general Guards paragraph ("For data that several tasks may use") and the opening claim of 19.3 reject it. An implementer who builds the three named rules accepts the program. The generation check does not save it: 18.11 says such a free "is a data race ... which no check could catch reliably".

**Resolution:** in 19.2, add to the crossing list: "a pointer, slice or other view that a task loads from a package variable, or from memory reached from one, counts as crossing to that task". Rule 2 then applies, as Rule 3 already does for maps. memory-model 4.1 rule 2 then reads "an object reached through a pointer that crosses to another task or is reached through a package variable".

**Rejected:**
- Leave it to the Guards paragraph. Two implementers then accept different programs, which is what this audit tests.
- Forbid heap views in package variables. Kernels keep `var current *Thread`-style globals; that conflicts with Go-familiar and kernel goals.

**Changes a promise:** no. It makes the stated promise hold under every reading.

### I2. "Exact path" has two definitions that disagree (medium)

**Kind:** reading-dependent acceptance.

**Sections:** 19.3 (Waiting in a non-migrating region), 20.10 (Calls through interfaces and func values).

**Quotes:**
- "every call on the way must reach a wait whichever function runs there".
- "A direct call, a call of a method of a known type, a deferred call, a hook and a function literal called where it is written are exact."
- "a call through an interface or a func value where only some of the functions it may reach may wait" compiles.

**Examples:**
- A hook assigned two plain functions, one that locks a `sync.Mutex` and one that does not, called inside `rt.DisablePreemption()`. By the list it is exact and reported. By "whichever function runs there" it is not.
- `defer f()` with `f` a func value. By the list a deferred call is exact. By the bullet it is a func-value call.
- A direct call of `g`, where `g` waits only inside `if c { mu.Lock() }`. "Must reach a wait" can be read as "waits on every path of `g`" (not reported) or as "has a reachable wait" (reported, as the first paragraph's "where it is reachable" says).

**Resolution:** define exactness per call site by its target set, and drop the list as a definition: "A call is exact when every function it may reach (section 20.10) may wait; a wait reachable on any path of a function counts. A direct call, a method of a known type and a function literal called where it is written have one target. A hook's targets are the functions assigned to it. A deferred call has the targets of the call it defers." Keep the list as examples.

**Rejected:**
- Report all candidates, as E2584 does. Kernel operations tables and allocation under a lock would be rejected; 19.3 already rejects this for good reason.
- Make all hooks exact whatever their targets. A hook with one waiting target would then be an error at every call, unlike any other func value.

**Changes a promise:** no.

### I3. The "none hangs in silence" claim misses `//pu:blocking` functions that do not park (medium)

**Kind:** false promise.

**Sections:** 19.3 (Waiting in a non-migrating region), 20.10 (`//pu:blocking`, Assembly and C).

**Quotes:**
- 19.3: the waiting operations include "a function marked `//pu:blocking`".
- 19.3: "every wait in a non-migrating region therefore fails to compile or panics when it is reached; none hangs the processor in silence."
- 20.10: `//pu:blocking` "on a function with a body ... states a wait that the compiler cannot see, such as a spin until an interrupt handler or another processor sets a flag"; it "is not unsafe code".

**Example:** a method marked `//pu:blocking` spins until a flag that only a task on this processor sets. It is called through an interface inside `rt.DisablePreemption()`, and other candidates of the interface do not wait. It compiles by the H2 rule. It never calls `park`, so the run-time test never runs. The processor spins forever with no panic. This is safe code, in a hosted build.

**Resolution:** the compiler inserts, at the entry of every function marked `//pu:blocking` (with or without a body), the same test `park` makes: panic with "blocking with preemption disabled" when preemption is disabled, and in kernel mode when IF is clear. The cost is one load and one branch on an already slow path. The claim then holds as written.

**Rejected:**
- Narrow the claim to "every wait that reaches `park`". True, but it leaves a silent hang in safe code that the spec now invites with `//pu:blocking`.
- Make calls that may reach a `//pu:blocking` function conservative. It still misses a wait reached on only some paths through the region.

**Changes a promise:** no with the resolution; yes if the claim is narrowed instead.

### I4. Principle 1 says every static analysis rejects on doubt (low)

**Sections:** 1.2 principle 1 ("Every static analysis ... never accepts on doubt"), 21.2 ("never accepts a program because it could not decide"), 19.3.

The wait check accepts calls whose targets only some wait. 19.3 states this on purpose, and the specific rule wins, and the wait check is about liveness, not safety. Reword principle 1 as "Every safety analysis", and add that the wait check of 19.3 reports exact paths only.

**Changes a promise:** no.

### I5. Four bare "region" uses remain (low)

- debugging.md line 231: "provided the region it is built in" (closure region).
- 11.6 line 534: "caller-region storage" (closure region).
- 10.10 line 800: "which `alloc.Use` replaces for a region of code".
- debug-allocator.md line 62: "scope it to a region with `alloc.Use`".

Use "closure region" for the first two, and "arena window" or "a stretch of code" for the last two (18.9 calls it the arena window).

### I6. 8.8 and the glossary lag the H1 and H4 edits (low)

- 8.8 Views that cross: "freeing the memory, or replacing or dropping an owner that holds it, is a change". 12.12 and memory-model 4.1 now add "or a `Reset`, `Release` or `Drop` of an arena that takes it back". 19.3 decides it, so this is wording.
- 1.4 Safe and unsafe code lists `unsafe`, the unchecked allocator layer and foreign functions, not the code of `pluto/rt` that 18.12 now lists.

### I7. Arena naming beyond the 18.3 limits is not stated (low)

**Quote:** 19.3: "counts as that call on every arena it may be, within the limits of section 18.3". 18.3's limits say "everything reachable is assumed kept", which is about keeping, not naming. Principle 1 decides the case (doubt is an error), so it is not a hole. Add: "beyond those limits, every arena of the program". Also say that an arena's name follows a move of the arena (`b := take(a)`); the owner transfer rule already covers the cross-task case.

### I8. "The limits may be raised; they are never lowered" (low)

18.3. Read as an implementation latitude, two compilers would accept different programs. 1.3 (a program is valid only if it meets every rule; one language version) decides it as a change to the manual. Say so: "An implementation uses these limits exactly; a later edition may raise them."

## 4. Checked and dropped

- Arena behind a pointer parameter or heap pointer (roadmap 196). The spec names it "for a pointer by where the pointer came from". This is a compiler gap, recorded as item 196.
- Arena in a struct moved to another task while a third task holds a plain view. Owner transfer requires exclusivity: the view is live, so E3501. A local view after the move is E3313.
- Chunked whose parent is a Fixed, reset through the parent. The Chunked's chunks are `Alloc` allocations of the Fixed, so the Fixed's `Reset` panics with "arena has live allocations" while the Chunked holds them.
- Reset by a deferred call: a call like any other, and the implicit drop is named in 19.3. By a handler: handlers are parties to rules 1 to 3, and 20.10 follows its drop entries. By package cleanup: no other task runs (15.4, 19.1).
- Slices of arena memory sent on a channel: a slice of a non-struct type crosses only in the first three cases of rule 2, so E3501. Slices of structs meet the type rule, which the reset breaks for every type allocated in that arena.
- Strings in arena memory: counted `Alloc` allocations, so a reset panics while one is held (18.9, 21.5). `unsafe.String` is unsafe code.
- Main's local arena dropped at main's return while a task holds a view: the implicit drop is a free after tasks start, so the type rule fails (E3501).
- Hosted backstop: `rt.DisablePreemption` and `rt.Lock` disable preemption, so `park` panics in every build. Cli faults in hosted builds.
- Other freestanding builds with Cli and a wait through an interface: not tested, and 19.3 says so. A liveness failure; 1.2 does not promise freedom from deadlock.
- A runtime callback that waits under a runtime lock (rt.Timer): not compile-checked by the H4 exemption, caught by the preemption count in `park`.
- Interface candidate sets are whole-program, so a new implementation elsewhere can turn a wait compile error into a run-time panic. It is deterministic for a given program; not reading-dependent.
- 1.2 "arena epochs never wrap" and 21.5 "must not collide": consistent.

## 5. Not read closely

- Chapter 19 after 19.4's opening (19.4 body only in parts: Memory reached through views, Memory outside the frame, Callees, Nesting, Allocators; 19.5 to 19.8 not read).
- Chapter 18: 18.4 to 18.8 and 18.10 skimmed only; 18.13 not read.
- memory-model.md sections 1, 2, 3.1 to 3.6, 5, 6 read only in part; 4.1, 3.7, 3.8 read in full.
- abi.md outside the 12.4 diff; 12.9 (timer callbacks) not read.
- 20.11 onward, and chapter 21 outside 21.5.

## 6. Judgment

The spec is close enough to implement from for everything except the three medium items, and those are small text changes. H1 to H9 hold and the twelve example fixes are right. The arena rule now closes the attacks I tried, and the remaining arena work (items 196 and 212) is compiler work, not spec work.

The largest remaining risk is I1. The race rules are written as three named rules plus a general paragraph, and the named rules leave out heap and arena memory reached through a package variable. An implementer who builds the named rules ships a compiler that accepts a use-after-free race in safe code. One sentence in 19.2 closes it.
