# Spec audit M: data-race rules after audit L

Checkout: 21d49ec6. Scope: 19.3 after the L rulings, with 12.11, 19.4 to 19.8, 20.9, 20.10, abi.md 12.4
and 12.5. Bar: closed means no high and no medium finding.

## 1. Verdicts

| Ruling | Same everywhere | Consistent with the model | Sound | Notes |
|---|---|---|---|---|
| L1 switch in a non-migrating region | yes (19.1, 19.3, 17.1, 20.9, 20.10, abi 12.4) | yes | yes | The new sentence "A handler body is interrupt-disabled but not non-migrating" collides with 20.9 (M1). |
| L2 AfterFunc is repeated | yes (19.3 Parties, 19.6, F) | yes | yes | Self re-arming `f` is the same repeated party. |
| L3 sync calls are atomic writes; case 3 not freed | yes | yes | yes | Rows 80 and 81 depend on a box package variable being a name (low, section 2). |
| L4 body holds the iterator's locks at `yield` | yes for named iterators | partly | yes where it applies | The Go 1.23 shape, a method that returns a locking closure, has no stated name mapping (M2). |
| L5 maps kept in place | yes (19.3 Memory and data, Locks and names, rule 1) | yes | yes | The fallback error does not name the copy (M4). |
| L6 sole-writer views | yes | yes | yes | Repeated writers get no exemption, as stated. |
| L7 method values | yes | yes | yes | |
| L8 rt.Timer unsafe | yes | yes | yes, by contract | |
| L9 row 93 | yes | yes | yes | |
| L10 rows 5 and 71 | yes | yes | n/a | Row 71 is now a distinct shape. |
| L11 map trigger wording | yes | yes | yes | |
| L12 frees overstatement | yes | yes | n/a | |
| L13 address carries the name | yes | yes | yes | |

Author extensions:

- Cli-only regions follow every call target: stated in 19.3 (Switching tasks) and 17.1; consistent; sound.
  One guess: a region with the count raised on some paths only (section 4, G5).
- A returned `Do` counts inside a callee when every call on the same party follows one: stated in 19.3
  Lazy initialization and rulings; not sound as written for functions with no visible call (M3).
- A range body may release a lock held at the loop, a literal may not: stated in 19.3 Part of the
  enclosing function ("They differ only where a body can do what a func value cannot") and Held sets;
  consistent; sound. The summary list lacks the fact the rule needs (G3).
- A map not kept in place is guarded only by a package-level lock: stated in Locks and names ("only a
  package-level lock can guard them") and rule 1; consistent; sound.

## 2. Conformance table

Checked rows 73 to 109 and sampled rows 1, 2, 3, 5, 9, 12, 17, 25, 29, 31, 34, 41, 46, 53, 59, 60, 61,
62, 66, 71. Problems: 4.

- Row 109 (ambiguous). "a Go 1.23 locked iterator over a guarded collection | accepted | Part of the
  enclosing function (row 82)". Row 82 uses a top-level `All` and a package `mu`. The Go 1.23 shape is
  `func (c *Cache) All() iter.Seq2[K, V]` returning a closure that locks `c.mu`. Whether that is accepted
  depends on M2, which no rule decides.
- Row 61 (conflicts with 20.9). "`runq.Push(t)` inside an interrupt-disabled region | accepted". 20.9
  says "Every other access must be inside a non-migrating region ... a call of a method with a pointer
  receiver". Inside a handler body, which 19.3 now calls "interrupt-disabled but not non-migrating", the
  two give different results (M1).
- Rows 80 and 81 (one step undecided). Both need `b.mu` to be a name for a package `var b owned[S]`, and
  row 81 needs `b.mu` to guard `b.n` in place. 19.3 says "A name is a package variable that is not a
  pointer" and guards in place need "no pointer load between". Section 8 says "A box is not a pointer",
  which settles the name, but whether the box's dereference is a "pointer load" is not said. Low: add
  "the dereference of a box held in the base variable is not a pointer load here".
- Row 25 (two rules decide). `mu.Lock(); s.Task(func() { mu.Unlock() })` cites Parties; rows 29 and 71,
  the same failure, cite Releasing a lock. Both rules reject it. Low: cite Releasing a lock, as row 71
  does.

## 3. New findings

### M1. Handler bodies are not non-migrating, so per-CPU code in a handler has no legal form (MEDIUM)

Kind: contradiction; goal conflict (kernel).

- 19.3 Non-migrating and interrupt-disabled regions (new in L): "A handler body is interrupt-disabled but
  not non-migrating, so it may call `rt.Preempt`; its region ends there."
- 19.3 lists non-migrating regions as preemption disabled, `x86.Cli` to `x86.Sti`, or an `rt.Lock` held.
  A handler body is none of them.
- 20.9: "Every other access must be inside a non-migrating region (section 19.3): a compound assignment or
  `++`, ... a call of a method with a pointer receiver, and `&v`." and "A pointer from `&v` is bounded by
  the non-migrating region it was taken in".
- 20.9, owned memory bullet: "So `runq.Push(t)` on a per-CPU `vec.Vec` inside an interrupt-disabled
  region is allowed". Row 61 agrees. The rulings say "Per-CPU views of the handler end there (20.9)",
  which assumes handlers have per-CPU views.

Example:

```go
//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++          // 20.9: E2022, no non-migrating region; 19.3 Accesses/row 61: allowed
	x86.Outb(0x20, 0x20)
	rt.Preempt()
}
```

Two careful implementers differ: one rejects every compound per-CPU access and every `&v` in a handler,
the other accepts. The first makes the most common kernel shape, a timer handler that counts ticks or
pushes onto a per-CPU run queue, impossible without `rt.DisablePreemption`, which then also turns the
handler's `rt.Preempt` into a pending flag.

Resolution: say that a `//pu:interrupt` handler body, up to the end of its interrupt-disabled region, is
also a non-migrating region for section 20.9, and that the switching check does not apply to it: its
region ends at a call that may switch, which ends any per-CPU view taken in it (E2023 after it). This is
sound: interrupts are off and nothing switches before that call, so the task stays on one processor.
State that a call through an interface, func value, method value or hook ends the region when any
candidate may switch, as the handler check follows them (20.10).

Rejected: require `rt.DisablePreemption` in handlers (unfamiliar, a cost on every interrupt, and it
silently defers the handler's own `rt.Preempt`); revert "not non-migrating" (then the switch check would
reject `rt.Preempt` in a handler, which row 76 accepts).

Promise change: no. It restores what 20.9 and row 61 already promise.

### M2. A method that returns a locking iterator closure gets no locks in the body (MEDIUM)

Kind: undecided case; Go-familiar shape.

- 19.3 Part of the enclosing function: "the body adds the locks that the iterator's summary shows held at
  every call of `yield` ... under the caller's name for it, as for a lock a callee returns holding. A
  lock that has no name in the caller is not added."
- Held sets: names map "under the caller's name for the argument". Only parameters map.
- 12.11: "The loop over `v.All()` has the meaning of the loop that `All` runs."
- Row 109 claims the Go 1.23 locked iterator is accepted, citing row 82, a top-level function.

Example:

```go
type Scores struct {
	mu sync.Mutex
	v  vec.Vec[int]
}

func (s *Scores) All() func(yield func(int, *int) bool) {
	return func(yield func(int, *int) bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.v.Len() {
			if !yield(i, s.v.Ptr(i)) {
				return
			}
		}
	}
}

func bump(sc *Scores) {
	for _, p := range sc.All() {
		*p += 1 // body holds sc.mu? the closure's lock is s.mu, s a capture, not a parameter
	}
}
```

The literal's lock is `s.mu` with `s` a captured variable of `All`. Its summary is relative to the
literal's start, where `s` is neither a parameter nor a caller name. One implementer reads 12.11 as "map
the captures through `All`'s parameters" and accepts; another applies "no name in the caller" and gives
E3501 for `*p`, and E3503 or E3502 for other guarded data. This is the shape Go 1.23 code uses for a
guarded collection, and `vec.Vec.All` in 12.11 has the same form.

Resolution: in Part of the enclosing function, add: when the iterator is the result of a call `x.M(args)`
whose result is a function literal written in `M`, a captured parameter or receiver of `M` that the
literal does not assign names, in the body, what the argument names at that call; a lock reached through
it is mapped as for a lock a callee returns holding. Have row 109 show this form. Sound because the
literal lives in the caller's closure region for the loop (12.11) and the capture cannot change while the
loop runs unless the literal assigns it.

Rejected: tell users to write a top-level iterator (not the Go shape, and methods are the common case);
treat every capture as a name (unsound when the literal assigns it, or when it is stored and called after
the variable changes).

Promise change: no. Row 109 already promises it.

### M3. The returned-`Do` callee rule is vacuous for functions with no visible call (MEDIUM)

Kind: soundness gap under a literal reading.

- 19.3 Lazy initialization: "a function counts as having a returned `o.Do` throughout its body when every
  call of it, direct or through an interface or a func value of which it is a candidate, is made on the
  same party where a call of `o.Do` has returned on every path to the call."
- Rulings, audit L attacks: "every real entry comes from an outside call first".

Functions with no call in the program satisfy "every call" vacuously: `main`, interrupt handlers (which
"cannot be called", 20.10), a function only started by `task f()` if a task statement is not read as a
call on the caller's party, an `AfterFunc` function called by `pluto/time`, a function called only from
code without a body. Example:

```go
var once sync.Once
var cfg int

func load()   { cfg = 42 }
func worker() { once.Do(load) }

func main() {
	task worker()
	println(cfg) // main has no call: vacuous credit; read races with load
}
```

A careful implementer roots `main` and the others; a literal one accepts the race. Resolution: add "Each
entry of a party (the main function, the function of a task statement, `s.Task`, `wg.Task`, `rt.Go` or
`AfterFunc`, an interrupt handler, and a function that code outside Plutonium may call) counts as a call
made with no returned `Do`." Rejected: drop the callee rule (row 85 and the closure/range agreement need
it).

Promise change: no.

### M4. The map fallback error does not name the copy that caused it (MEDIUM)

Kind: diagnostic gap; goal "ordinary code needs no implementation knowledge".

- Maps kept in place: one copy-out anywhere in the whole program sends every object's map of that place to
  the type datum ("A place is kept in place when, in the whole program").
- Rule 1: "E3502: a datum with a shared write has no guard. The error names a write, the party that makes
  it (by the statement that starts it), and an access of another party it may meet."

Example: `Put` writes `c.m[k]` under `c.mu` (row 86). Someone in another package adds
`func (c *Cache) All() map[string]int { return c.m }` for a debug page. Every `Put` now fails with E3502
at `c.m[k] = v`, a write that visibly holds `c.mu`, naming another `Put` on another task. Nothing points
at `All`. The user has to know the kept-in-place rule to find the cause.

Resolution: when the datum of an E3502 or E3503 is the contents of a map whose place would be kept in place
but for some occurrences, the error names the first such occurrence and says it copies the map out
("`return c.m` copies the map out of `Cache.m`; its contents are then one datum for
`map[string]int`"). Rejected: make the copy itself the error (breaks maps that are only read after
start of sharing, and programs that guard such maps with a package lock, which the rule allows).

Promise change: no.

## 4. Guesses (19.3 read as a compiler writer)

- G1. Returned-closure iterators: how the literal's captures map to caller names (M2).
- G2. Whether a box dereference in a package variable counts as a pointer load for names and guards in
  place (rows 80, 81).
- G3. "A lock that the body may release counts as not held in the iterator after its call of `yield`":
  the summary list (Held sets) has no item saying which accesses may follow a call of a func parameter.
  For an iterator in another package the caller cannot apply the rule. Add that item, or say every access
  of the iterator counts without the lock when the body may release it.
- G4. Whether a handler body's region ends at a call through an interface whose candidate may switch
  (M1 resolution covers it).
- G5. "In a region where only maskable interrupts are disabled ... no count raised": on every path, or on
  some? A region with the count raised on some paths only is covered by the run-time test only there.
  Say "where the count may be zero on some path".
- G6. Entry points for the Once callee rule (M3).
- G7. Summary item "the locks held at every call of it" for func-valued parameters says nothing of copies
  of the parameter; the on-another-party item does ("or of a copy of one"). Say the same for held locks.

## 5. Checked and dropped

- Non-blocking channel ops and `select` with `default` in a non-migrating region: they ready, never
  switch (19.5, 19.8).
- Scheduler-aware allocator that yields: count regions panic in `rt.Yield`; Cli-only regions follow
  allocators (19.3).
- `rt.ExitTask` and panics in a region: deferred releases run on the return path; the count is per task
  (abi 12.4, 19.4 Non-local exits).
- `rt.Lock` regions: raise the count, so the run-time test applies (abi 12.4, 12.5 rule 6).
- `s.Wait` in a region: a park, covered by the wait check.
- Per-CPU views by return, field, closure, task, channel: 20.9 forbids each.
- C and asm calls: unsafe code by contract (What is not checked).
- Trap handlers: not interrupt-disabled; they open their own region (20.10).
- AfterFunc that re-arms itself: still one repeated party; Stop gives no order that the rules use.
- Mutex in arena memory: Reset frees every object, a write that conflicts with lock calls (Accesses).
- WaitGroup or channel in frame memory used by scoped tasks: 19.4 Sharing form 1.
- Body that takes a read lock at yield and writes: the lock is added "in the same mode"; a write under
  `RLock` does not count for the guard set.
- Nested locked iterators, break, return, goto with the iterator's lock held: the body may not release
  added locks; leaving runs the iterator's deferred unlock.
- Func-value iterator with several candidates: "may show fewer locks held than a run holds, never more"
  forces the intersection.
- View passed to `yield` kept after the loop: invalid once the lock ends (Views loaded under a lock).
- Kept in place under whole-struct assignment from a composite literal, embed, `clear`, `delete`, nil
  compare, `maps.Clone`-like helpers that only range: decided by the list and summaries.
- Struct value copies of a struct holding a lock and a map: already a lock-copy error.
- Method value receiver mapping, repeated sole writer, `owned` hand-over-hand, whole-program map
  fallback: recorded in the audit L attacks.
- Row 91 and the method value bound to a receiver whose lock is the caller's: recorded.

## 6. Sections not read closely

8.8, 17.1 beyond the new bullet, 18.12, appendix F.5 and F.7, memory-model.md sections 3 and 4, abi.md
12.6 to 12.10, 20.10 after the E2584 rules, rows 4, 6 to 8, 10, 11, 13 to 16, 18 to 24, 26 to 28, 30, 32
to 33, 35 to 40, 42 to 45, 47 to 52, 54 to 58, 63 to 65, 67 to 70, 72.

## 7. Judgment

Not closed. Four mediums stand in the way: M1 (handler bodies and 20.9), M2 (method-returned locking
iterators, which row 109 promises), M3 (vacuous Once credit for entry points) and M4 (the map fallback
error). Each has a local fix with no promise change. No high finding. The L rulings themselves hold.
