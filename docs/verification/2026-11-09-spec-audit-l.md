# Spec audit L: the rewritten race rules (v0.4, commit a75afd90)

Scope: language spec 19.1 to 19.8 in full, memory-model.md in full, 12.11, 12.12, 18.11, 18.12, the arena
parts of 18.9, 20.9, 20.10, F.1 and F.7 timer text, abi.md 12.4 and 12.9, D109 to D115, the rulings record
"Race rules rewrite", and audit K. Old text compared: `85312df7:docs/spec/language/19-concurrency.md`.

## 1. Verdicts

### Model terms

| Term | Verdict | Notes |
|---|---|---|
| Parties | open | AfterFunc functions re-run by `Reset` are not called repeated (L2). rt.Timer functions are not listed (L8, low). |
| Start of sharing | closed | "called both before and after counts as after" is clear. View-before-start wording, L12 (low). |
| Part of the enclosing function | open | Held set of a range body omits the iterator's locks at `yield` (L4). |
| Owned memory | closed with low note | Fine-grained locks inside owned children cannot guard (L9, low). |
| Static objects | closed | |
| Synced objects | closed | |
| Map contents | open | One datum per map type defeats per-object locks (L5). "memory it did not allocate" is vague (L11, low). |
| Crossings | closed | Case 5 covers atomic loads, handler loads, per-CPU loads. |
| Accesses and conflicts | open | Lock and other `pluto/sync` method calls are not said to be accesses, or of what kind (L3). Method value text conflicts with Held sets (L7). |
| Frees are writes | closed | Relies on L3 for memory that holds a lock. |
| Lock names | closed with low note | `p := &mu; p.Lock()` as a package-level guard is not stated (L13, low). |
| Held sets and summaries | open | L4, L7. |
| Non-migrating and interrupt-disabled regions | open | `rt.Yield` inside a non-migrating region switches tasks and may migrate (L1). |
| Guard set | open | Sole-writer read exemption conflicts with rule 2 case 4 (L6). |
| Valid views and ending events | closed | `Yield` is not an ending event for per-CPU views (L1). |
| Initialization | closed | |
| Lazy initialization | closed | |
| Ordering used | closed | Matches memory-model.md 4.1. |
| Precision limits | closed | Limits are exact and order-independent; "never accepted with a party left out" holds given L2. |

### Rules

| Rule | Verdict |
|---|---|
| Rule 1 | open (L3, L5) |
| Rule 2 case 1 (owned) | closed |
| Rule 2 case 2 (never changes) | closed |
| Rule 2 case 3 (atomic or synchronized) | open (L3) |
| Rule 2 case 4 (guarded) | open (L6) |
| Rule 3 (type rule) | closed |
| Releasing a lock | closed |
| Waiting in a non-migrating region | open (L1: `Yield` is neither a wait nor an end of the region) |
| Interrupt handlers | closed |
| Owner transfer | closed |
| Copying a lock | closed |
| Scoped sharing 19.4 | closed |

Counts: model terms 10 closed, 8 open (L1 to L7 account for them); rules 8 closed, 4 open.

## 2. Conformance table

All 72 rows were checked. Wrong or ambiguous rows: 2.

- Row 71 repeats row 25 (a scoped task unlocks the starter's lock, same result, same rule). Drop one or
  give 71 a different shape (for example the unlock through a helper the scoped task calls).
- Row 5 cites "rule 2 cases 2 and 3". Case 2 lists package variables and fields of static objects, not
  map contents. The map read is unchanged data under rule 1 (Initialization), and the `&echoHandler{}`
  that the loaded interface points to is a static object under case 3. Cite "Initialization; rule 1 (map
  contents); rule 2 case 3".

Row 22 was checked: the callee's summary releases `q` without holding it, so the result is an error at
every call, which matches. Rows 1, 4 and 6 depend on 18.3 frame views, consistent with 12.12.

Missing shapes that matter, with the result the text gives today:

| Shape | Result now | Rule |
|---|---|---|
| `t := time.AfterFunc(d, f)`; `t.Reset(0)` while `f` runs; `f` does `n++` on a package `n` nobody else touches | accepted (race) | Parties: start runs once (L2) |
| Per-CPU `vec.Vec`: `rt.DisablePreemption(); s := runq.Slice(); rt.Yield(); use(s[0])` | accepted (race) | 20.9, regions (L1) |
| `var b owned[S]` with `S{mu sync.Mutex}`; `task user(&b.mu)`; `b = new(S{})` | depends on reading (L3) | rule 2 case 3, rule 1 |
| Locked iterator `All` that holds `c.mu` around `yield`; loop body writes through the yielded `*int` | E3502 or E3501; the explicit-closure form is accepted | Part of the enclosing function, Held sets (L4) |
| Two static `Cache{mu; m map[string]int}` objects, each method locks its own `c.mu` | E3502 | Map contents (L5) |
| Sole writer under `mu` reads `items.Slice()` without `mu` | E3501 by case 4, accepted by the guard set text | L6 |
| `wg.Task(func(){ results[i] = f(i) })` style workers writing a package slice by index, read after `wg.Wait()` | E3502 | rule 1; partitions exist only for `sync.Scope` |
| `m := s.inc; mu.Lock(); m(); mu.Unlock()` | E3502 or accepted | L7 |

## 3. Findings

### L1. `rt.Yield` inside a non-migrating region breaks per-CPU exclusion (HIGH)

Kind: safety hole in safe kernel code.

Sections: 19.3 "Non-migrating and interrupt-disabled regions"; 20.9; 19.1; abi.md 12.4.

- 19.3: a non-migrating region is code where "preemption is disabled ... The task cannot move to another
  processor inside it."
- 19.3: the interrupt-disabled region of a handler ends "to a call that may switch tasks (`rt.Preempt`,
  `rt.Yield`)". No such end is given for a non-migrating region.
- 19.1 line 94: "calling `rt.Yield()`, gives up the processor early." Calling `rt.Yield` is not unsafe
  code (18.12 lists only the code of package `pluto/rt`).
- abi.md 12.4: `rt.Park` panics with a non-zero count. `Yield` does not. The count is per task, so the
  task keeps it while switched out and may resume on another processor.
- The waiting check lists channel operations, `Lock`, `Wait`, `time.Sleep`, `rt.Park`, `//pu:blocking`,
  allocation. `Yield` is not one.

Example (kernel mode):

```go
//pu:percpu
var runq vec.Vec[int]

func steal() {
	rt.DisablePreemption()
	s := runq.Slice() // view of this processor's copy, bounded by the region
	rt.Yield()        // another task runs here and pushes to runq; this task may resume on CPU 1
	println(s[0])     // still "inside the region": accepted
	rt.EnablePreemption()
}
```

Another task on CPU 0 enters its own region and calls `runq.Push`, which may free the storage `s` views.
If this task resumes on CPU 1, the use races with the free, which no check catches. Every rule accepts it.

Resolution: a call that may switch tasks (`rt.Yield`, `rt.Preempt`, and any call whose summary may reach
them) inside a non-migrating region is a compile error, followed through calls with a single known
target as the wait check is; and `rt.Yield` with a non-zero preemption count panics at run time, as
`rt.Park` does, for calls the check does not follow. The runtime's own switch path stays exempt.
Rejected: ending the region at `Yield` (as for handlers). It keeps the code compiling but turns a region
the programmer wrote into two, so per-CPU read-modify-write split across the `Yield` silently loses its
guarantee; an error is clearer. Promise change: no; it makes the existing promise true.

### L2. An `AfterFunc` function re-armed by `Reset` is not a repeated party (HIGH)

Kind: safety hole in safe code under the literal text; at least a divergence.

Sections: 19.3 Parties; 19.6 Timers; appendix A row for `time.AfterFunc`; F.7.

- 19.3: "A party is **repeated** when its start may run more than once: a task statement in a loop, or in
  a function that may be called more than once."
- Appendix A: `AfterFunc` returns `synced[Timer]`, "with `Stop` and `Reset` called the same way" as in Go.
  In Go, `Reset` on an `AfterFunc` timer runs `f` again, and may do so while an earlier run is still going.
- The start of the party is the `AfterFunc` call (a func value given to a function that starts a task
  with it). That call can run once while `f` runs any number of times.

```go
var n int // only tick touches n

func tick() { n++ }

func main() {
	t := time.AfterFunc(time.Millisecond, tick)
	for i := 0; i < 10; i++ {
		t.Reset(0) // each Reset starts tick again; runs overlap
	}
	select {}
}
```

`tick` is the only party that accesses `n`, so rule 1 asks nothing; with two runs at once, `n++` races.
The sole-writer read exemption (Guard set) also applies to a non-repeated `f`, so `f` writing under a lock
and reading without it races with its own second run.

Resolution: a function started by `time.AfterFunc` is a repeated party, whatever its call site, because
any handle's `Reset` can start it again. Say so in Parties and in 19.6. Rejected: tracking whether `Reset`
is ever called on that timer (handles are `synced` and copyable, so the analysis would follow every copy
for little gain); forbidding `Reset` on `AfterFunc` timers (a Go false friend and a visible loss).
Promise change: no.

### L3. Memory that holds a lock: crossing has no free condition, and lock calls are not defined as accesses (MEDIUM)

Kind: two careful implementers differ; under one reading a use after free in safe code.

Sections: 19.3 rule 2 case 3, Accesses, Frees are writes, rule 1 (E3502 bullet).

- Case 3: "Memory of a `pluto/sync` or `pluto/sync/atomic` type, a runtime record ... a channel, or a
  struct of only those". Unlike the struct objects of case 3, there is no condition that the memory is
  not freed or moved. The example says "with a package-level `wg`", but the rule does not.
- Accesses defines atomic accesses as `pluto/sync/atomic` operations only. Nothing says whether
  `mu.Lock()` is an access of the memory that holds `mu`, or of what kind.
- Rule 1: "Moving or freeing the memory that holds such a lock is a write of that memory, which this rule
  already covers." This holds only if the other party's `Lock` is an access that conflicts with a plain
  write.

```go
type S struct{ mu sync.Mutex }

var b owned[S]

func user(m *sync.Mutex) { for { m.Lock(); m.Unlock() } }

func main() {
	b = new(S{})
	task user(&b.mu) // case 3: memory of a sync type crosses
	b = new(S{})     // drops the old S while user locks it
}
```

Reading A: lock methods are not accesses, so nothing conflicts with the drop: accepted, and `user` locks
freed memory. Reading B: lock methods are accesses that do not conflict with each other but do conflict
with a plain write, so `b`'s write has no guard: E3502. If lock calls were plain accesses, every shared
mutex would fail, so neither reading is the text.

Resolution: in Accesses, state that a method call on a `pluto/sync` value (and a channel operation) is an
access of that value that counts as atomic: it does not conflict with another such call or an atomic
access, and it conflicts with a plain write, which includes a free or move of the memory that holds it.
This makes the E3502 bullet and row 24 follow from one rule. Also add the case 3 condition already used
for struct objects: the memory is not freed, moved or overwritten while shared. Rejected: requiring sync
values that cross to be package-level (rejects `synced[T]` objects holding a `WaitGroup`, a common
shape). Promise change: no.

### L4. A range body does not get the locks its iterator holds at `yield` (MEDIUM)

Kind: inconsistency and goal conflict that ordinary Go 1.23 code hits.

Sections: 19.3 Part of the enclosing function (lines 206 to 209), Held sets (lines 331 to 333); 12.11 "The
body's party".

- 19.3: "the locks held at its start are those held at the loop statement, less the locks the iterator
  may release, intersected with those held at the end of the body on every earlier run".
- 19.3: "A call through an interface, a func value or a method value runs its candidates with the caller's
  held set for their accesses."

```go
var mu sync.Mutex
var scores vec.Vec[int]

func All(yield func(int, *int) bool) {
	mu.Lock()
	defer mu.Unlock()
	for i := range scores.Len() {
		if !yield(i, scores.Ptr(i)) { return }
	}
}

func bumpAll() {
	for _, p := range All { *p += 1 }                 // body holds nothing: E3502 or E3501
	All(func(_ int, p *int) bool { *p += 1; return true }) // a func value: runs with mu held, accepted
}
```

The same work is accepted as a closure and rejected as a range loop. "The iterator counts as a call
around the body" can also be read as giving the body the iterator's held set at the `yield` call, so
implementers may differ.

Resolution: for a body that is part of the enclosing function, add to its held set the locks the
iterator's summary shows held, in write or read mode, at every call of `yield` (mapped to the caller's
names, as for a returned lock). The summary gains "locks held at every call of each func parameter". A
view the iterator passes to `yield` counts as loaded at the `yield` under those locks. Rejected: leaving
it (a false rejection of the standard locked-iterator shape, and visible in source only as an error).
Promise change: no.

### L5. Map contents of one type are one datum, so per-object locks cannot guard maps (MEDIUM)

Kind: goal conflict ordinary code hits.

Sections: 19.3 Memory and data ("The contents of all maps of one type are one datum once a map of that
type crosses ..."), Locks and names ("A lock guards the contents of a map in place when it guards the
variable or field that holds the map in place: `s.mu` for `s.m[k]`"), The guard set.

```go
type Cache struct {
	mu sync.Mutex
	m  map[string]int
}

func (c *Cache) Put(k string, v int) { c.mu.Lock(); c.m[k] = v; c.mu.Unlock() }

var users = &Cache{m: map[string]int{}}
var hosts = &Cache{m: map[string]int{}}
```

With tasks calling `users.Put` and `hosts.Put`, the one datum "contents of `map[string]int`" is written
under `users.mu` and under `hosts.mu`. G is empty: E3502. The "guards in place" sentence for maps works
only while one object of the type exists. A struct with a mutex and a map is the most common concurrent
Go type. The listed workaround ("the shared map takes a type of its own") cannot work for objects made at
run time (`synced[Cache]` values).

Resolution: make the contents of a map held in a field or variable a datum of that place when every copy
of the map value stays in places of that field (the map value is never copied out of `c.m` except to
index it, range over it, or call `len`, `delete` or `clear` on it). Then `c.mu` guards `c.m`'s contents in
place, per object. A map value copied anywhere else falls back to the type-wide datum. Rejected: alias
analysis of map values (fragile, order-dependent, against 21.3); keeping the rule (rejects the commonest
shape). Promise change: no. Add a row.

### L6. Sole-writer reads conflict with rule 2 case 4 (MEDIUM)

Kind: two careful implementers differ.

- Guard set: "The reads of a party that is the only writer of D, and not repeated, need none."
- Rule 2 case 4: "Every access to that memory, by every party and through every view of it, holds a lock
  of its guard set."
- Views loaded under a lock: a view is valid only if loaded "while the party holds locks of G(D)".

So the sole writer may read `items.Len()` without `mu` but not `for _, x := range items.Slice()`, unless
case 4 wins, in which case it may not read `items.Len()` either. Both directions are safe for the sole
writer; acceptance differs.

Resolution: case 4 says "every access that the guard set requires to hold a lock"; a view loaded by the
sole non-repeated writer of D, from D or memory D owns, is valid at each use with no lock, since no other
party writes D. Rejected: dropping the exemption (rulings attack 2 adopted it on purpose). Promise
change: no.

### L7. Method values: "no lock held" versus "the caller's held set" (MEDIUM)

Sections: 19.3 Accesses, line 260: "A method value may be called later, anywhere, so what its method does
counts as done with no lock held". Held sets, line 331: "A call through an interface, a func value or a
method value runs its candidates with the caller's held set for their accesses."

`m := s.inc; mu.Lock(); m(); mu.Unlock()`, with `inc` writing a package `x` that other tasks write under
`mu`: line 260 gives E3502, line 331 accepts. Acceptance must not differ (21.3).

Resolution: keep line 331 (every call site is seen, so it is sound) and delete the held-set part of line
260, keeping only "outside every `Do`" for Lazy initialization, where the reason (no known call site for
the Once) still applies. Rejected: keeping 260 and carving method values out of 331 (needlessly rejects
the closure-equivalent shape). Promise change: no.

### L8. rt.Timer functions are not parties (LOW)

19.6 and abi.md 12.9: "A timer's function runs wherever timers run (a hosted worker between tasks, the
hosted poller thread, or a kernel's timer interrupt)". The party list does not name them. The function's
type is `func(unsafe.Pointer, int64)`, so its code is unsafe (18.12) and the race promise does not cover
it. Say so in Parties or What is not checked, so an implementer does not treat it as main-task code.

### L9. Locks inside owned children cannot guard (LOW)

Memory and data: "The memory a datum owns is part of that datum". A list of `owned[Node]` with a mutex per
node makes every node write a write of the root datum, which only locks in place for the root can
guard; hand-over-hand over owned links is E3502. Decided by the text, uncommon (Go code uses pointers).
Add a row so it reads as intended.

### L10. Row 71 duplicates row 25; row 5 cites case 2 (LOW)

See section 2.

### L11. "memory it did not allocate" (LOW)

The map trigger "once a party reaches one through a package variable or through memory it did not
allocate" does not say whether memory a callee allocated and returned counts. Say "memory that its own
code, or a function it called on its own party, allocated".

### L12. "no party frees memory that another party may still use" overstates (LOW)

A view loaded before the start of sharing is not a crossing. Main may keep `p := items.Ptr(0)`, start a
task that pushes under `mu`, and read `*p` under `mu` later. The free and the use are ordered by `mu`, so
this is the run-time "use of freed memory" panic of 18.11, not a race. The rules are sound; the sentence
in Frees are writes promises more. Reword to "no party frees memory that another party may use at the
same time".

### L13. Address of a package lock in a local (LOW)

`p := &mu; p.Lock(); x++; p.Unlock()`: "A copy carries the names" covers `cur = next`, not `&mu`. Say
that `p := &mu` makes `*p` a name of `mu`, so it guards package data as `mu` does.

## 4. Checked and dropped

- Owner holding a held mutex moved on a channel, `rt.ExitTask` with a lock held, panics with locks held:
  liveness only.
- `rt.CatchExitTask` returning-holding through a func value: summaries show fewer locks, safe.
- Channels and `select` of views: a sent view is "used with no lock held", E3501.
- Interfaces holding guarded data: 19.2 checks every converted type.
- Maps of locks: by value is a copy error; by pointer `l := m[k]` names the lock.
- Locks inside `synced[T]`: `s.mu` names them; field conditions apply.
- Generics: per instantiation, for accesses and releases.
- Arenas: `Reset`, `Release`, `Drop` free every New/Slice object (Frees are writes; 18.9 Drop entries);
  row 30 matches.
- Row 22 aliasing parameters: error either way.
- Package cleanup (15.4): runs after tasks end; no ordering used, so at most harmless rejections.
- Handlers installed by a raw IDT write: unsafe code (20.10).
- Interrupt handler starting `AfterFunc`: starting a task is forbidden in interrupt context (20.10).
- Scoped tasks mixed with `wg.Task`: `wg.Task` func values cannot be frame closures (12.12, E3003).
- Per-CPU pointer stored and loaded: crossing, rule 2 (20.9).
- View loaded before the start of sharing: ordered, run-time panic (L12, wording only).

Old-text sample (Task 4): 31 sentences of the old 19.3 checked against the new text: tasks found from
main; handlers as parties from the first install or `Sti`; held on every path; callee and caller
mapping; method value no lock (now conflicting, L7); `Wait` = Unlock + Lock; `Wait` write mode; unnamed
`Locker` release error; `RLocker` `Wait` error; ordering not used; release rule with modes; no handing a
locked lock; semaphore advice; names and pointer locals; copies carry names; release through one name
ends all; assigning a lock E3502; variable index; TryLock branches; result in a variable not followed
(new, consistent); defer order; loops; callee release; func-value callee releases only its own;
`withLock`; recursion; generics `withLocked`; trusted lock bodies; waiting-check targets, run-time test,
freestanding flag, "not every loop", "accepts on doubt"; handler rules incl. allocator no exemption;
owner transfer incl. "Generation checks alone do not prevent races"; what is not checked. All are kept
with the same meaning, except: "every access holds its guard" (now the sole-writer exemption, a
recorded ruling, attack 2); the map "no partition form" sentence of old rule 3 (dropped; partitions in
19.4 cover only slices and arrays, so the meaning stands). No rule vanished without a decision.

## 5. Sections not read closely

11.5 (closure regions), 15.4 (read only the timer and cleanup lines), 18.3 (frame views; relied on for rows
1, 4, 6), 18.9 outside Drop entries and plain views, 20.10 after line 749 (what the handler check follows),
8.7, appendix A beyond the timer row, 21.3 to 21.5.

## 6. Judgment

Not closed: two high findings (L1, L2) and five medium (L3 to L7).

The rewrite is better than the old text. It has one vocabulary (parties, data, guard set, valid views,
start of sharing) used the same way across 19, 20.9, 20.10 and memory-model.md; it closes audit K's holes;
the precision limits are exact; and the 72-row table pins most shapes. Old sentences survive with their
meaning.

What stands in the way: both highs are narrow and fixable in a sentence each (switching calls in
non-migrating regions; `AfterFunc` functions are repeated). L3 needs one definition in Accesses. L4 and
L5 are the ones ordinary Go code will hit first: a locked range iterator and a struct with a mutex and a
map. Without them, the rules are sound for those shapes but reject code a Go programmer writes on day
one, against the "ordinary code needs no implementation knowledge" goal.

Ten common Go shapes not in the table (Task 5):

1. `c := &Counter{}` in `main`, passed to tasks: E3004 (frame view to a task). Use `synced[Counter]` or a
   package variable. 12.12.
2. Struct with mutex and map, several instances: E3502 (L5).
3. Worker pool: jobs channel, results channel, `wg.Wait()` then `close(results)`: accepted (values cross
   by channel; `wg` package-level or in a Scope).
4. `go func() { defer wg.Done(); ... }()` capturing loop data: E2590; use `s.Task` or pass arguments.
5. Workers writing `results[i]` then reading after `wg.Wait()`: E3502 with `wg.Task`; accepted with
   `sync.Scope` (partition form).
6. Done channel closed to cancel, `select` on it: accepted.
7. `atomic.Bool` stop flag: accepted (rule 1 atomic).
8. `sync.Once` singleton returning a pointer to its data: accepted (rule 2 case 2 with Lazy
   initialization) while nothing resets it.
9. Read-mostly config map under `sync.RWMutex` at package level: accepted (map contents, package-level
   guard).
10. Go 1.23 locked iterator over a guarded collection: rejected (L4).

What "safe code has no data races" rests on: complete parties (L2, L8 gaps); the start of sharing; access
collection through calls, interfaces and func values with conservative fallbacks; the guard set and
held-set inference; the release rule; valid views and their ending events; frees are writes; owner
transfer exclusivity; the Once edge and the Scope join; per-CPU and handler rules over regions that are
truly non-migrating (L1 gap); the trusted bodies of lock methods, `pluto/rt`, and the unsafe wrappers in
`vec` and `hash`; and a definition of which sync-method calls are accesses (L3 gap). Generation checks
cover ordered stale uses only, as 18.11 says.
