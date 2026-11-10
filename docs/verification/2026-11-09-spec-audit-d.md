# Spec audit D: second pass over the unread sections

Source: /home/matjam/src/plutonium/.claude/worktrees/coord (origin/main at 3b330068), docs/spec/language/ and docs/spec/abi.md, docs/spec/memory-model.md.
Read-only. Nothing was built or changed. Each finding was checked with grep across the whole spec.
Findings already decided in completion plan 0.1 and 0.2 (A1 to A8, C1 to C14, B1 to B22) are taken as the future text and not reported again.

Counts: high 2, medium 5, low 7.

---

## D1. Freeing a shared pointee is not a "write" under the race rules

- Kind: safety hole (data race; use after free across tasks).
- Severity: high.
- Changes a promise: yes. It rejects programs the text accepts today.

Sections and quotes:

- 19.3 rule 2, the type rule: a view may cross when "no code overwrites a whole object of the type through a pointer, and each field either never changes once an object is shared" or is atomic or locked. It counts writes only.
- 12.12: "A view of other memory crosses only when what it points to cannot change while both tasks may use it."
- 19.4 "Sharing" and E3404 forbid moving or dropping the variable the scoped task uses. They say nothing about an owner reached through it.
- memory-model.md 3.7: "Dropping an owner is ordinary code."
- memory-model.md 3.8: a free that races with a use is not caught by any check, and "safe code cannot make one" (pointing at 4.1).

Example (unscoped task):

```go
type Config struct{ n int }
type Server owner struct{ cfg owned[Config] }
var srv Server

func watch(c *Config) { for { println(c.n) } }

func main() {
	srv.cfg = new(Config)
	task watch(&*srv.cfg) // Config is never written: rule 2 allows the crossing
	srv.cfg = new(Config) // drops the old Config while watch reads it
}
```

Rule 1 does not fire: only main touches `srv.cfg`. Rule 2 does not fire: no code writes a `Config` field. The drop frees memory the other task reads. Generation checks cannot help, since 3.8 says they do not order a racing free.

Scoped form: `p := &*n.child; s.Task(func() { use(p.x) }); n.child = new(Leaf)`. E3404 covers `p`, not `n.child`.

Proposed resolution: in 19.3 and 19.4, say that dropping, freeing or moving an object, or assigning to or dropping any owner that holds it, counts as a write of the whole object for rules 1 to 3 and for scoped tasks. With that, the example is a rule 2 error, because the pointee can change while shared. This serves "Rust-level race safety without annotations": Rust rejects the same program through the borrow of `srv.cfg`.

---

## D2. An owner captured by a returned closure is dropped at the function's end

- Kind: safety hole (use after free) and hole.
- Severity: high.
- Changes a promise: yes, either way it is resolved.

Sections and quotes:

- 11.4: "The enclosing function still owns the captured variable and drops it at the end of its scope."
- 11.5: a returned closure is built in a region the caller provides, and captured variables and parameters are "copied there on entry".
- abi.md 4.1: the region's drop function "releases each stored counted component". It names no owners.
- 18.8: one bullet releases a counted variable at the end of its scope, another releases what a region holds when the caller's frame returns. For a captured counted variable the two readings disagree.

Example:

```go
func counter() func() int {
	v := vec.New[int]()
	return func() int { v.Push(1); return v.Len() }
}
```

By 11.4, `counter` drops `v` when it returns. The closure in the caller's region then pushes into a freed vector. An owner parameter captured the same way has the same problem.

Proposed resolution: the region owns every captured variable that has drop glue, and drops it when the region is refilled or the caller returns. abi 4.1 then says the drop function runs drop glue for owners too. The alternative is a compile error for an owner captured by a closure that escapes into a region. The first keeps Go's counter idiom working without hidden allocation, so it fits "Go-familiar" better. Either way, add the rule to appendix A.3 next to the existing closure row.

---

## D3. Views of a local owner are not covered by E3313 when a call or field assignment frees memory

- Kind: contradiction and false friend.
- Severity: medium (high if generation checks are not meant to be the answer).
- Changes a promise: yes if E3313 is widened; no if the text is corrected to name generation checks.

Sections and quotes:

- 18.3, E3313: a view of a local owner may not be used after the owner is "moved, dropped or given a new value".
- 18.9 (around line 712): for arenas, `Reset` and `Release` are added to E3313 by name. No such case exists for other owners.
- 18.10: a view obtained from a container in a local "follows the rule for a local owner's memory".
- 18.11 (around line 775): the "rules of 18.3 prove at compile time that no view ... of a local owner, outlives its memory".
- 12.9 (around line 265): `c.buf = b` frees the old value.
- 7.9 (around line 798) and 18.5 (around line 251): an ordinary interface that lends an owner is "a view of the variable".
- 7.9 representation (around line 865) and abi.md 5: a value larger than one word is copied into the frame of the converting function; the data word points at the copy.

Examples:

```go
s := v.Slice()
v.Push(1)        // pointer method; may free the old array
println(s[0])    // not moved, dropped or given a new value: E3313 silent

p := &*n.child
n.child = new(Leaf) // frees the old Leaf; n is not given a new value
println(p.x)

x := any(v)      // snapshot of v's header in the frame
v.Push(1)        // may reallocate
fmt.Println(x)   // reads the old header: stale length, or a freed array
```

Only generation checks catch these, at run time, and 18.11 says checks are for heap memory reached through fields. A reader of 18.10 and 18.11 expects a compile error. A Go reader of the third example expects `x` to show either the old or the new vector, never a panic. The snapshot is also a false friend that appendix A.3 does not list: its row says the interface "is a view of the converted variable".

Proposed resolution, two options:

1. Widen E3313: a view of a local owner, or of memory it owns, is also invalid after a call that takes the owner by pointer and may free (its summary says so), and after an assignment to any owner field on the path to the view. Make ordinary-interface lending of a large owner a true view (data word points at the variable, not a copy), so it sees the owner's current value. This matches Rust and the 18.11 sentence.
2. Keep the rules and fix the text: 18.10 and 18.11 say these cases are caught by generation checks, and A.3 lists the interface snapshot.

Option 1 serves "Rust-level safety" and "performance near Rust" (no check needed). Option 2 is smaller but leaves a run-time panic where Rust rejects at compile time.

---

## D4. `defer x.Drop()` and a later move of `x` contradict each other

- Kind: contradiction.
- Severity: medium.
- Changes a promise: no, once one reading is chosen.

Sections and quotes:

- 12.19 (around line 1164) and 18.6 (around line 359): a deferred `x.Drop()` does "nothing, if x was moved meanwhile".
- 18.5 (around line 273): a deferred call with a pointer receiver counts as a use of the owner at the exit, so a move before the exit is E3301.

Example:

```go
f := os.Open(name) or return
defer f.Drop()
send(take(&f)) // 12.19: fine, the defer does nothing. 18.5: E3301.
```

Proposed resolution: keep 12.19 and 18.6. A deferred `Drop` on a whole owner variable is a drop-if-still-held, and 18.5 names it as the exception. This keeps the Go "defer close" idiom compiling.

---

## D5. Type switch and assertion on an owner that is not a whole variable

- Kind: hole.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 12.9 (around lines 474 and 491): a type switch with a binding on an owning interface moves from the "whole variable".
- 7.13 (around line 1314): an "owner union variable".
- 11.13 (around line 791): the operand of a moving assertion "must be a whole variable or a temporary". No error code is given.

Example:

```go
func (d *Dev) step() {
	switch s := d.state.(type) { // a field reached through a pointer
	case State.Idle: ...
	}
}
```

This is the usual state machine shape in kernel drivers. The text says neither that it is an error nor what happens to `d.state`.

Proposed resolution: give the 11.13 rule an error code, and apply it to the type switch with a binding. Point at `switch s := take(&d.state).(type)` as the form to write. This is explicit in the source and needs no new rule.

---

## D6. 11.5 assumes calls through func values keep their arguments; 18.3 uses summaries

- Kind: contradiction.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 11.5 (around line 285): "A call through a func value or an interface is assumed to return and keep anything passed to it." The E3101 list also counts passing to a func value or an interface as keeping.
- 18.3 (around lines 157 to 163): calls through an interface or func value use a summary over every candidate implementation.

The same program can be accepted under 18.3 and rejected under 11.5. It is unclear whether 11.5's rule is limited to the region rules.

Proposed resolution: limit the 11.5 sentence to closure regions in plain words, or replace it with a pointer to the 18.3 summaries.

---

## D7. A range over a counted variable that the body reassigns

- Kind: hole.
- Severity: medium (low if the answer is "the loop holds a count").
- Changes a promise: no.

Sections: 12.10, 18.6 (temporaries of a statement live to its end), 18.8.

Example:

```go
for k := range m { m = nil } // m's last reference released mid-loop?
for i, r := range s { s = "" }
```

18.6 keeps temporaries alive, but `m` is a variable, not a temporary. Go keeps iterating the original map or string.

Proposed resolution: say the range expression is evaluated once into a hidden temporary that holds its own count until the loop ends. This matches Go and costs one count operation.

---

## D8. Per-iteration copies of loop header variables do not fit owners

- Kind: hole.
- Severity: low.

12.10 (around line 533): each iteration's variable is "initialized to the value of the previous iteration's variable". For an owner header variable that is a duplicate. 18.6 drops body variables per iteration but says nothing of header owners.

Proposed resolution: say header variables of owner kind move from one iteration to the next and are dropped once, when the loop ends.

## D9. Every error "may be sent" is overstated

- Kind: contradiction (wording).
- Severity: low.

16.7 point 7: "every error value may be sent between tasks". An error that lends an owner, holds `&p.err` of a local, or holds a frame copy is a frame view, which E3005 and E2011 reject. 19.2 already exempts conversions whose result is a frame view.

Proposed resolution: "every error value that is not a view of a frame may be sent".

## D10. Unsafe races: "never undefined behavior" against "can break memory safety"

- Kind: contradiction.
- Severity: low.

19.7, memory-model.md 4, and appendix A.3 ("a bug with limited consequences") against memory-model.md 4.3, which says torn values "can break memory safety".

Proposed resolution: say a race in unsafe code gives torn or stale values with no undefined behavior in the compiler's sense, and that a torn pointer or length can then break memory safety. Make A.3 match.

## D11. 18.13 Conn example comment is wrong

- Kind: mistake.
- Severity: low.

18.13: `name string // a counted field: copied with the struct`. `Conn` is an owner, so the struct moves and the string moves with it.

Proposed resolution: "a counted field: moves with the struct".

## D12. Chapter 9 names only owners as dropped at block end

- Kind: wording.
- Severity: low.

Chapter 9 says owners are dropped at the end of a block. Chapter 12's introduction and 18.8 also release counted values there. After A1 the gap matters more.

Proposed resolution: "variables with cleanup (owners and counted values)".

## D13. 7.14 calls a register value a "typed view"

- Kind: wording.
- Severity: low.

7.14: "a raw register value becomes a typed view". It is a value of a packed type, not a view in the 8.7 sense.

Proposed resolution: "a typed value".

## D14. 11.5's list of allowed uses is incomplete

- Kind: wording.
- Severity: low.

11.5 lists what may be done with a capturing closure but omits storing it in memory of the same frame and passing it to a scoped task, which 18.3 and 19.4 allow.

Proposed resolution: add both, with references.

---

## Checked and dropped

- Owning interface holding a view, returned: E3001 "inside an interface" covers it.
- Loop refilling an owning interface's frame slot: E3006 covers it.
- Sending an owning interface with a frame payload: E3005 covers it.
- Promoted consuming method: 8.6 and E3303 are explicit.
- A struct with view and counted fields counts on copy: 8.7 is explicit.
- Maps and channels of views: E3002 and E3005; 7.10 bars owners as keys and elements.
- A generic view argument, `new(&x)`, captured counted locals: decided by A1 and C items.
- Assignment drop order; 10.6 to 10.15; chapter 14: consistent.
- Range over a vec whose body calls Pop: covered by 12.10 "Owners in iterators".
- `errors.As` with a borrowed `*T`: covered by summaries and the 18.3 rule on views through a counted reference.
- A callee storing an arena allocation in a global: B1 plus 18.11.
- 15.4 cleanup against 18.6: consistent.
- memory-model.md 3.x against 19.7 (Mutex, Cond, WaitGroup, channels, Scope): consistent.
- abi.md 5 interface layout against 7.9: consistent (and the basis of D3).
- abi.md 2 and 6: no language rule depends on them beyond what the chapters say.
- The 19.4 examples `crawl` and `sortPar`: valid.
- Appendix A: rows checked for D2, D3 and D10; other rows match their chapters as far as read. Wrong reference 12.11 is A4.

## Sections not read closely

- abi.md 7, 8 and 11: skimmed; no language rule was traced to them.
- memory-model.md 4.2: the middle part only skimmed.
- Appendix A: read for false friends only, not row by row against every chapter.
- Three candidates left unverified: 7.17 ring `All` or `Peek` with `Pop` (probably fine, inline storage); 7.18 conversion of `type H owner *T` to `*T` and whether the result is bounded by the owner; 7.19 aliases of `shared[T]` that `Push` (same root as D3).
