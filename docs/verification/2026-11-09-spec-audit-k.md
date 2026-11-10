# Spec audit K: data-race rules of chapter 19 (v0.4 at 85312df7)

Baseline c7e55d0f. Scope: J1, J2, J3, lazy initialization with `sync.Once`, `sync.Cond` as a value, and
the release rule (D108). Read-only audit. Sections cited by file and heading; "19.3" means
docs/spec/language/19-concurrency.md section 19.3.

## 1. Verdict per change

| Change | Verdict | Findings |
|---|---|---|
| J1, rule 2 case 4 | Holds for package variables and `synced[T]` objects. Three gaps around it. | K2, K3, K5, K6 (medium), K10 (low) |
| J2, static objects | Holds. Examples obey it. Shares K2 (owned memory of a static object). | K2, K11 (low) |
| J3, handler loads | Stated in 19.2, 19.3 The tasks and memory-model 4.1, but the older rules keyed to "tasks have started" were not updated. | K4 (medium) |
| Lazy initialization | Holds. One precision gap. | K9 (medium), K13 (low) |
| `sync.Cond` as a value | Holds. A helper that wraps `Wait` hits K3. | K3, K12, K14 (low) |
| D108, release rule | One hole: a range-over-func body that runs in another task. Otherwise holds. | K1 (high) |

Counts: 1 high, 7 medium, 5 low. (K8 is unused.)

## 2. Findings

### K1. A range-over-func body may run in another task and release the loop's lock (high)

Kind: soundness. Changes a promise: yes (new error or panic).

Text:
- 19.3 Releasing a lock, Function literals: "So is the body of a range-over-func loop (section 12.11),
  with the iterator as a call around it".
- 19.3 Releasing a lock, Calls: "A function reached through an interface, a func value or a method value
  counts as called with no lock held for this rule".
- 12.11: "`f` may call `yield` as often as it likes while it runs". Nothing limits which task calls it.
- 19.4 What a scoped task may use: a scoped task "may use any variable of the starting function", so an
  iterator may call its `yield` parameter from a scoped task.

Example:

```go
var (
	mu   sync.Mutex
	data int
)

func touch() { data++ }

func par(yield func() bool) {
	var s sync.Scope
	s.Task(func() { yield() }) // the loop body runs in this scoped task
	touch()                    // main task; counted with mu held, since f holds mu around par
	s.Wait()
}

func f() {
	mu.Lock()
	for range par {
		mu.Unlock() // part of f by the range-over-func bullet, so allowed; runs in the scoped task
		mu.Lock()
	}
	mu.Unlock()
}

func g() { mu.Lock(); data++; mu.Unlock() }

func main() {
	task g()
	f()
}
```

Every write of `data` holds `mu` by the analysis, so `mu` is its guard. At run time the scoped task
releases the main task's `mu`, `g` takes it, and `g` and `touch` write `data` at once. The release also
breaks the premise of memory-model 3.4 that the n-th Unlock is made by the task whose Lock began that
critical section. A reader who applies the general rule instead (a task starts with no lock held, and the
body is reached through the func value `yield`) rejects the program. The two bullets conflict for this
body, and the specific one, read as written, accepts it. A related case: two scoped tasks that call
`yield` at once run the body at once, writing the loop variables and the `defer` slots in `f`'s frame
(12.11 `defer` in the body) from two tasks.

Resolution: the summary of each function already records when it calls a func-valued parameter from
several tasks (19.4 Callees, E3406). Extend it to record a call of a func-valued parameter from any task
other than the caller's. A range-over-func loop treats its body as part of the function only when the
iterator's target is known and its summary has no such call. Otherwise the body counts as a func value:
called with no lock held, for releases and for accesses, and a `defer` in it is a compile error. Justified
by the safety goal and by "no annotations": the information comes from summaries the compiler builds
anyway, and the common loops (`v.All()`, `m.Keys()`) have known targets that call `yield` in the caller's
task, so they keep the idiom in 12.11 (`defer it.mu.Unlock()` in the body).
Rejected: a run-time check in `yield` of the calling task alone (catches the release too late, after the
analysis has trusted it); forbidding `yield` in scoped tasks (the compiler cannot tell a `yield` parameter
from any other func parameter in a generic iterator).

### K2. Case 4 does not cover memory owned by a static object or a type-rule object (medium)

Kind: goal conflict and false error. Changes a promise: yes (accepts more programs).

Text: 19.3 rule 2 case 4: "memory that a package variable owns (rule 1), or that the object of a
`synced[T]` owns through owners". Case 2 gives a static object only field conditions: "each of its fields
or elements must meet the field conditions". The type rule does the same. Rule 2 closing: "A pointer to, or
slice of, anything other than a struct crosses only in the first four cases."

Example (the Go logger shape the J2 ruling, rulings program 2, was meant to keep):

```go
type Logger struct {
	mu  sync.Mutex
	buf vec.Vec[byte]
}

var std = &Logger{}

func (l *Logger) Print(s string) {
	l.mu.Lock()
	l.buf.Append(s) // loads the backing slice of buf: a []byte reached through std
	l.mu.Unlock()
}
```

The backing slice is not a field and is not owned by a package variable or a `synced[T]` object, so it
crosses in no case: E3501. `var std Logger` compiles by case 4. The two declarations differ only in a `&`,
and the J2 ruling chose the `&Logger{}` form as the Go-familiar one.

Resolution: extend case 4 to memory owned, through owners, by a static object or by an object of a type
that satisfies the type rule, with the guard being the lock that guards the owning field ("one lock of the
same object or one package-level lock"). The free condition is unchanged: growing the vec frees its old
storage, which is a write under that lock. Rejected: telling users to write `var std Logger` (a false
friend between two spellings that Go treats alike), and `synced[Logger]` (a counted box for a singleton).

### K3. Case 4 does not say that a release inside a callee counts (medium)

Kind: precision. Changes a promise: no.

Text: case 4: "on every path from the load to the use, the guard is held and not released. A release
between them counts even when the lock is taken again". Releasing a lock, Calls, describes a callee only by
its net effect: it "holds it for its caller", or "the caller then no longer holds it".

Example (a common Cond helper):

```go
func (q *Queue) waitNonEmpty() {
	for q.items.Len() == 0 {
		q.nonEmpty.Wait(&q.mu) // every call holds q.mu, and it returns with q.mu held
	}
}

func (q *Queue) peekWait() int {
	q.mu.Lock()
	p := q.items.Ptr(0)
	q.waitNonEmpty() // releases and retakes q.mu inside
	v := *p          // by a net-effect summary: held, never released
	q.mu.Unlock()
	return v
}
```

One implementer summarizes callees by net effect and accepts; another records "may release" and rejects.
Generation checks catch the stale use at run time (18.11: a check is repeated after "an operation that
synchronizes with another task"), but an implementer who uses case 4 as the static proof that removes the
check builds a use after free.

Resolution: add to case 4 "A call whose callee may release the guard, directly or through its callees,
including by `Cond.Wait`, is a release between them." The summary bit is cheap and needs no annotation.

### K4. J3 changed when handlers become parties, but the older rules still start at "tasks have started" (medium)

Kind: inconsistency. Changes a promise: no (clarifies).

Text: 19.3 The tasks: "No handler is a party during that time either, until the first statement that may
install an interrupt handler ... or enable interrupts". memory-model 4.1 was updated: "written only during
package initialization or by `main` before it starts its first task, installs its first interrupt handler
or enables interrupts". But 19.3 rule 1 still says "A package variable written only during package
initialization, or only by `main` before it starts its first task, may be read by every task without a
lock", Lazy initialization says "Writes made during package initialization, or by `main` before it starts
its first task, do not count", and case 2 and the type rule use "once tasks have started".

Example:

```go
var table [16]Device

func main() {
	idt.Install(32, timerHandler) // the handler reads table
	x86.Sti()
	table[0] = Device{port: 0x40} // main, before its first task: exempt by the rule 1 sentence
	task worker()
}
```

Read as written, the rule 1 sentence exempts the write and the handler reads `table` while it changes. The
general sentence in The tasks rejects it. A reader who takes the specific sentence over the general one
builds the race.

Resolution: define once, in The tasks, "the start of sharing" as the first statement that may start a task,
install a handler or enable interrupts, and use that phrase in rule 1, Lazy initialization, case 2 and the
type rule.

### K5. Scoped tasks have no "not released in between" condition (medium)

Kind: inconsistency (compile error at package level, run-time panic in a Scope). Changes a promise: yes.

Text: 19.4 Sharing, form 2: "every access, in every task and in the starting function, holds one and the
same lock". J1's release condition is only in 19.3 case 4. 18.10 Containers: "A view obtained from a
container in a local variable follows the rule for a local owner's memory ... ends it at compile time
(E3313). One obtained through a pointer is a heap view, checked by generation checks".

Example:

```go
func run() {
	var mu sync.Mutex
	var items vec.Vec[int]
	items.Push(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		p := items.Ptr(0)
		mu.Unlock()
		mu.Lock()
		println(*p) // every access holds mu; the other task may have grown items
		mu.Unlock()
	})
	s.Task(func() { mu.Lock(); items.Push(1); mu.Unlock() })
}
```

The same code with `items` at package level is E3501. Here 19.4 accepts it and 18.11 makes it a panic. The
18.10 sentence suggests a view from a local container is checked only at compile time, and 18.3's E3313
events are in "the same function", so a literal reading elides the check.

Resolution: add the case 4 condition to 19.4 form 2: a view loaded under the lock is used only while it is
held with no release in between (E3402). State in 18.10 that a view of a captured container in a scoped
task is a heap view for generation checks.

### K6. "The guard" when every write holds several locks (medium)

Kind: precision. Changes a promise: no.

Text: 19.3 Guards: "the compiler infers its guard: a lock that every write of the data holds for writing".
Case 4: "the view is used only while the guard it was loaded under is still held".

Example: `stats` is written under `a` and `b` together; one reader holds `a`, another holds `b`. Both locks
qualify as "a lock that every write holds". One implementer picks one and rejects the other reader; another
accepts any qualifying lock. For case 4, a view loaded under `a` and `b`, used after `b.Unlock()` with `a`
held, is accepted or rejected by the same choice. The record covers a datum under `a` in one task and `b` in
another (rulings line 460), not this.

Resolution: define the guard set as the locks every write holds for writing. A read holds one of them; a
view is used only while one lock of the set that was held at the load stays held with no release. This is
sound, since every write holds every lock of the set.

### K7. A per-CPU variable's owned memory has no case (medium)

Kind: goal conflict (kernel). Changes a promise: yes.

Text: 20.9: "A pointer or other view stored in a per-CPU variable and loaded from it crosses ... rule 2 of
section 19.3 decides". Case 4's guard is "the one rule 1 infers", and 20.9: "Rule 1 of section 19.3 ... does
not apply to a per-CPU variable".

Example: a per-CPU run queue or free list, `//pu:percpu var runq vec.Vec[*Task]`, pushed and popped inside
an interrupt-disabled region. The owned storage belongs to one processor's copy, and the region excludes
every other party that can reach it, yet `runq.Push` loads the backing slice and no case covers it: E3501.

Resolution: in 20.9, treat memory owned by a per-CPU variable like the variable: accessed only inside the
region that a pointer from `&v` needs, and a view of it bounded by that region (the E2023 rule). Rejected:
fixed arrays only (they work, but the error cannot say so for a container, and the kernel goal names this
code).

### K9. Which function "the reading function" is for a Once read in a literal or range body (medium)

Kind: precision. Changes a promise: no.

Text: Lazy initialization: "A call of `o.Do` counts for a read when it is made in the reading function, or
in a function that it calls directly". The release rule says literals called in place, deferred literals
and range-over-func bodies "are part of the function"; Lazy initialization does not.

Example: `cfgOnce.Do(load); for _, s := range cfg.servers.All() { println(s.name, cfg.port) }`. The read of
`cfg.port` is in the body. One implementer counts the `Do` (body is part of the function, as for locks),
another reports E3503.

Resolution: say in Lazy initialization that the same literals and bodies are part of their function, as in
the release rule, with K1's exception for bodies that may run in another task.

### K10. 8.8 omits the release condition (low)

8.8: "Every change is synchronized when every access, by every task, holds one lock, and the view is used
only while that lock is held". Add "and not released and taken again in between", as case 4 says.

### K11. memory-model 4.1 lists static objects under memory that does not change (low)

4.1 puts "a static object ... whose changing fields are atomic or guarded" under "memory that does not
change while shared". Move it to its own bullet, or say "whose storage does not change".

### K12. F.5 lists Once and Cond twice (low)

F.5 has "`sync.WaitGroup`, `sync.Once` and `sync.Cond`, which synchronize as in section 19.7" and then a
bullet each for Once and Cond. Drop the two names from the first bullet.

### K13. The Once direct-callee rule goes one level, locks go any depth (low)

Lazy initialization: "a function that it calls directly and that calls `o.Do` on every path", next to "as a
lock held at a return counts for the caller", which maps through any depth. E3503 names the Once and the
read, so the programmer can add a `Do`. Either drop "directly" or say why it differs.

### K14. A copy of a `sync.Cond` is not defined (low)

F.5 makes `Cond` a value with no constructor, and Once has "A copy of a Once is another Once". Cond has no
such sentence. Go reports "sync.Cond is copied". A copy made while tasks wait is a library obligation (18,
"their obligations do not become an unstated precondition of an ordinary API"), so this is not a hole, but
say "A copy of a Cond is another Cond with no waiters" and list it in appendix A.

## 3. Checked and dropped

- Lock in a struct sent on a channel while held: the move ends the name (Releasing a lock, Assignments and
  moves), the receiver starts with none held. Deadlock only.
- Lock in memory an arena reset reclaims: Frees are writes ("counts as freeing every object").
- RWMutex readers with a writer that frees: a free is a write, which holds the lock for writing.
- View loaded under `RLock`, then `RUnlock(); Lock()`: a release between (case 4).
- Lock-name parameter given different locks for one datum: "A parameter names, at each call, the lock its
  argument names there"; no common lock, E3502 under any reading.
- Once whose function locks `mu`: the common lock decides the guard; lock-order inversion is liveness.
- Cond inside a Once-initialized object: `Wait(&s.mu)` through a local pointer names the lock; through a
  loaded pointer it is an error that says "copy the pointer into a local".
- Handler and `rt.Lock` with `x86.Sti` inside the hold: 19.3 "A wrong pairing can stop a processor, which is
  a liveness failure".
- Scoped task unlocking the starter's lock: "begin with no lock held".
- Two paths to data, one unguarded: case 4 "through every view of it".
- Datum under A in one task, A+B in another: guard A; reported only for the several-lock case (K6).
- Case 4 view used after `Unlock` through a stored copy: "A copy of the view whose uses the compiler cannot
  follow counts as used with no lock held".

## 4. Sections not read closely

19.4 after about line 1234 (deferred calls in Scopes, later examples), 19.5, 19.6, 18.12 beyond the trusted
lock bodies, docs/decisions.md D103 to D108 (relied on the rulings record), abi.md 1.3 and 12.5, chapters
15 and 17 beyond the cited lines.

## 5. Judgment

Not closed. One high and seven medium findings stand in the way.

- K1 must be fixed before the release rule can be implemented: the range-over-func bullet and the
  func-value bullet conflict, and the specific reading has a race.
- K2 and K7 reject ordinary code (a Go logger, a kernel per-CPU queue) that the goals promise.
- K3, K5 and K6 decide whether case 4 is a proof that can replace generation checks; until they are fixed
  an implementer must keep the checks.
- K4 and K9 are text alignment.

The claim "safe code has no data races" (memory-model 4.1, 19.3) rests on: 19.2 crossing; rules 1 to 3;
Guards; Frees are writes; Lazy initialization and the Once edge (memory-model 3.4); rule 2 cases 1 to 4
and the type rule; the release rule with its trusted bodies (18.12) and `pluto/rt` outside the rules; the
start of handlers as parties (J3); 19.4 sharing; owner transfer exclusivity; per-CPU rules (20.9); and
generation checks for heap views (18.11). Missing: a rule for range-over-func bodies run by another task
(K1); one definition of when sharing starts (K4); the no-release condition in 19.4 and for callees (K3,
K5); a defined guard when several locks qualify (K6).
