# Scoped puroutines: design

**Date:** 2026-10-10 (revised 2026-10-10)
**Status:** proposal, not implemented. Revised under the no-false-friends rule (language spec,
introduction; `docs/plans/2026-10-10-go-false-friends.md`, case F10): scoped puroutines are started by
a new type, `sync.Scope`, and `sync.WaitGroup` keeps Go's behavior. The earlier surface decisions (D1
and D2: a WaitGroup that joins what it is handed) are superseded; section 13 records them and why.
Section 12 lists the current decisions. The design builds on the memory safety design
(`docs/plans/2026-10-09-memory-safety-design.md`, on the escape-checks branch until it merges), whose
escape analysis (`internal/escape`) and move checker (`internal/moves`) it extends.

## 1. The problem

The most common Go concurrency idiom does not compile:

```go
func process(items []Item, results []Result) {
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		go func(i int, it Item) { defer wg.Done(); results[i] = work(it) }(i, it)
	}
	wg.Wait()
}
```

The literal captures `wg` and `results`, which live in `process`'s frame, so the `go` statement is
E2590. Passing `&wg` to a named function instead is E2011. Go 1.25's form, `wg.Go(func() {...})`,
compiles, and then panics when it runs, because `WaitGroup.Go` starts `f` with a plain `go` statement
and the runtime finds `f`'s closure record in the starting stack. Today's workarounds are to allocate
the WaitGroup and every shared variable with `new` (go2pu does this, and leaks them), or to make them
package variables.

The rejection is right for a plain `go` statement and for a WaitGroup: the puroutine may outlive the
frame, and a WaitGroup waits only where the program calls `Wait`. It is wrong for what this program
means, which is to wait for every puroutine before `process` returns. Rust accepts the same program with
`std::thread::scope`: threads started in a scope are joined before the scope ends, so they may borrow
the frame. This design gives Plutonium the same guarantee through a type whose name says it waits,
without new syntax, annotations or lifetimes:

```go
func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Go(func() { results[i] = work(it) })
	}
	s.Wait()
}
```

Two smaller defects turned up while checking the current behavior, and are fixed by this work:

- E2590's suggested fix for the first program is `go func(wg pluto/sync.WaitGroup) {...}(wg)`. That
  copies the WaitGroup, so `Done` decrements the copy and `wg.Wait()` never returns. The type is also
  printed with its full package path.
- `wg.Go(func() { n++ })` is a run-time panic where the compiler can see the literal.

## 2. Goals

1. The idiom has a Plutonium form that a Go programmer recognizes at sight: the same shape, with a type
   whose name says that it waits for its puroutines.
2. `sync.WaitGroup` behaves exactly as Go's (no false friends). It never waits on its own, and frame
   views handed to puroutines it counts stay compile errors.
3. Safe code keeps the guarantees of the memory safety design: no view outlives what it looks at, and
   no data races.
4. No new syntax, no annotations, no lifetimes. Every rejection is explained in the program's own names.
5. Whether a program compiles does not depend on optimization, as for the escape checks.
6. Plain `go` statements keep their current rules and cost.

The goal of the first version, that Go source for the idiom compile unchanged and go2pu translate it as
the identity, is withdrawn: compiling Go source unchanged is no longer a goal of the language.

## 3. Options for the surface

**(a) A new library type.** `var s sync.Scope; s.Go(func() {...}); s.Wait()`. The type waits for every
puroutine it started before its own scope ends. This is Rust's design moved into a type. The difference
from Go is in the source: the declaration names a type Go does not have, and its documentation says it
waits. Making the type an owner whose `Drop` waits reuses the drop rules that already run on every path
out of a block (spec section 6.4), so the join needs no mechanism of its own.

**(b) Make `sync.WaitGroup` scoped by inference.** Prove, by whole-program analysis, that `Wait`
dominates the return on every path after the `go` statements. As stated this is unsound: `Wait` returns
when the counter reaches zero, and the counter is whatever `Add` and `Done` make it. A `Done` called
before the puroutine's last access, an extra `Done` elsewhere, or `Add(-1)` lets `Wait` return while a
puroutine still uses the frame.

**(b') The WaitGroup joins the puroutines it is handed** (the previous recommendation). A second counter
that only the runtime touches makes (b) sound, and the compiler inserts a wait at the end of the
WaitGroup's block. Go source compiles unchanged. But the WaitGroup then waits where Go's never does: a
function that returns early without calling `Wait` returns at once in Go and blocks here, and a latch
(`ready.Done()` followed by more work) had to be excluded by a rule about where `Done` appears. Nothing
in the source shows either difference, so under the no-false-friends rule it is rejected.

**(c) A language construct.** A block such as `scope { go ...; go ... }` that joins at its end. It is
explicit and easy to check, but it is new syntax for what a type with a `Drop` already expresses, and a
block cannot be named, so phase 2's helpers that start puroutines for their caller's scope (section 10)
would need more syntax still.

**(d) A scope operand on `go`.** `go(s) f()` or `go s.f()`. Also new syntax, and it reads as a variant
of `go`, which hides the waiting in a statement Go programmers already know.

| | Difference visible to a Go reader | `WaitGroup` unchanged | New API or syntax | go2pu | Sound |
|---|---|---|---|---|---|
| (a) `sync.Scope` | yes: a new type | yes | a type | rewrite the declaration, drop `Add`/`Done` | yes |
| (b) inference | no | no | none | identity | no |
| (b') joining WaitGroup | no | no | none | identity | yes |
| (c) scoped block | yes | yes | syntax | restructure | yes |
| (d) `go(s)` | partly | yes | syntax | rewrite each `go` | yes |

**Recommendation: (a), named `sync.Scope`.** The working name was `sync.Group`, but Go programmers know
`errgroup.Group`, which does not wait unless `Wait` is called; a `Group` that waits on its own would be a
small false friend of its own. `Scope` says that the puroutines belong to a block, as Rust's
`thread::scope`, Swift's task groups and Kotlin's `coroutineScope` do.

## 4. The design

### 4.1 The type

```go
package sync

// A Scope starts puroutines that may use the variables of the function that
// starts them, and waits for all of them before its own scope ends: when the
// block that declares it ends, on every path out of it, it waits as Wait does.
// The zero Scope is ready to use. A Scope is an owner: it moves instead of
// being copied, and it cannot be moved while its puroutines may be running.
type Scope owner struct {
	join rt.Join
}

// Go starts f on a new puroutine that s waits for.
func (s *Scope) Go(f func())

// Wait returns when every puroutine started by s has returned.
func (s *Scope) Wait()

// Drop waits as Wait does. It runs when s goes out of scope.
func (s *Scope) Drop()
```

A Scope has no `Add` or `Done`. Its count is changed only by the runtime: a puroutine started by `s.Go`
is counted before it starts and uncounted after `f` returns. So `s.Wait()` returning proves that every
puroutine started by `s` has returned, whatever else the program does, and the compiler needs only to
know where each Scope lives (section 4.2) and where its waits are (section 4.3).

`sync.WaitGroup` is unchanged: `Add`, `Done`, `Wait` and `Go` behave as Go's, it never waits on its own,
and the puroutines it counts are unscoped (section 9).

### 4.2 Which puroutines are scoped

A Scope is **local** when it is a local variable of the function, or a field or array element of one at
a constant path (`t.s`, `scopes[2]`), so that the compiler knows where it lives and that it lives until
the end of its block. A puroutine is **scoped** when it is started by `s.Go(f)` on a local Scope `s`.

A Scope that is not local (reached through a pointer, or stored in heap memory) still waits when it is
dropped, but its puroutines are unscoped for the frame rules: `f` may not use the frame, as for a plain
`go` statement (phase 2 widens this, section 10). A `go` statement is never scoped.

What makes a Scope join is a runtime type, `rt.Join`, which `sync.Scope` embeds (section 8.6). Phase 2
lets other types that embed one, such as an error-collecting scope, work the same way, through function
summaries rather than annotations.

### 4.3 Joins

A scoped puroutine is **joined** when the function that started it is sure it has returned:

- after a call `s.Wait()` on the same Scope;
- where `s` is dropped: at the end of its block, on every path out of it (falling off the end, `return`,
  `break`, `continue`, `goto`), by the drop rules of spec section 6.4. In a function's outermost block the
  drop runs after deferred calls. Among the variables of one block, **Scopes are dropped first**, before
  the other variables in reverse order of declaration, so a puroutine has returned before anything it may
  use is dropped, whichever was declared first. A deferred `s.Wait()` joins at the same point.

The compiler follows, for each statement of the function, which `s.Go` calls may have puroutines that
are not yet joined. This is a forward data-flow problem like the move checker's "may have moved": an
`s.Go` adds itself, an `s.Wait()` removes every `s.Go` on `s`, and paths join by union. The interval from
an `s.Go` to the join is where the rules of sections 4.4 to 4.6 apply. A `Wait` inside a loop joins each
iteration's puroutines before the next iteration; a `Wait` in one branch of an `if` joins only on that
branch, and the puroutines count as running after the `if`. A Scope declared in a loop body is dropped,
and so waits, at the end of each iteration.

The drop is what makes early returns safe:

```go
for i, it := range items {
	s.Go(func() { results[i] = work(it) })
}
if err := check(items); err != nil {
	return err // s is dropped here: it waits for the puroutines before process's frame is reclaimed
}
s.Wait()
```

When an explicit `s.Wait()` has already joined everything on a path, the drop finds the count at zero
and costs one load.

### 4.4 What a scoped puroutine may use

A scoped puroutine may use any variable of the starting function, and anything a view in it points
to, as long as its storage lasts until the join. That covers views of the frame (`&x`, `buf[:n]` of a
local array, `&T{...}`), capturing function literals, method values of local variables, and views
derived from parameters, which the caller's frame keeps alive beyond the join. None of them are sinks
for the escape analysis: a view handed to a scoped puroutine is bounded by the join, which comes before
the function returns.

**Variables that end before the join are taken.** A captured variable whose block may end before the
puroutine is joined, such as a loop variable or a variable declared in the loop body when the `Wait`
is after the loop, is **taken** by the puroutine when `s.Go` runs: a value is copied, an owner is moved,
and the puroutine uses its own copy. This is Go 1.22's per-iteration semantics, which Plutonium cannot
give by reference because every iteration reuses the same frame memory (spec section 4.5). The copy
cannot be observed: the parent and the puroutine would need to share the variable after `s.Go`, which
the sharing rules of section 4.5 already reject unless neither writes it, in which case a copy and a
shared variable read the same.

```go
for i, it := range items {           // i and it end with the iteration
	s.Go(func() { results[i] = work(it) }) // the puroutine takes i and it; results is shared
}
s.Wait()
```

A taken owner is dropped by the puroutine when it returns; the starting function may not use it after
`s.Go`, as after any move. A captured owner whose block outlives the join stays where it is, and the
literal may use it, as any literal may; since a scoped literal runs exactly once, moving it inside the
literal is allowed too, and counts as a move at `s.Go`.

**Views of variables that end before the join are errors.** Taking a variable copies it; it does not
copy what a view in it points to. A slice of a loop-body array, taken into a puroutine that runs past
the iteration, still looks at that array, and is E3401 (section 8.7).

**Taken values must be sendable.** What a puroutine takes crosses to another processor, so the sendable
rule (spec section 4.7) applies to it. Variables used by reference are governed by section 4.5 instead.

### 4.5 Sharing: no data races

Scoped puroutines share the frame with each other and with the function that started them, and safe
code must still have no data races. The compiler can check this completely for the frame, because every
puroutine that can reach it during the interval is written in this function (or reached through its
callees' summaries), and nothing outside can reach it.

The rule: during the interval of section 4.3, every place that a scoped puroutine uses and that anyone
writes (the starting function, a puroutine, or several instances of one `s.Go` in a loop) must be one of:

1. **Synchronized by type.** A `pluto/sync` or `pluto/sync/atomic` value used through its methods, or a
   channel variable used for channel operations. Assigning a whole new value to such a place is a write
   like any other, so `mu = sync.Mutex{}` while puroutines run is rejected (E3402), and moving the Scope
   itself is E3405.
2. **Guarded by a local lock.** Every access to the place, in every puroutine and in the starting
   function, happens while one and the same local `sync.Mutex` (or the write lock of a local
   `sync.RWMutex`, with reads under `RLock`) is held. The compiler infers this: it follows
   `Lock`, `Unlock` and `defer mu.Unlock()` through each body with the same flow analysis, and
   intersects the sets of locks held at each access, as Clang's thread-safety analysis does with
   annotations and Eraser does at run time. A local lock is one the compiler can name, as in section
   4.2. No `//pu:guardedby` is needed.
3. **Partitioned.** Each puroutine writes elements or windows of a slice or array that no other
   puroutine or the starting function can reach during the interval, in one of the forms below.

Places that nobody writes during the interval may be read by everyone. Anything else is E3402, which
names both accesses.

**Partitions.** The compiler accepts these forms, where `i` is the puroutine's own copy (section 4.4)
of the variable of the innermost loop around the `s.Go`, the `s.Go` runs at most once per iteration,
and neither `x` nor `k` nor `m` is written during the interval:

| Accesses in the puroutine | Loop | Why the parts are disjoint |
|---|---|---|
| `x[i]` and its fields | `for i := range ...`; `for i := a; cond; i++` (or `i--`, or `i += c`) | each iteration has a different `i` |
| `x[i*k : i*k+k]` or `x[i*k : (i+1)*k]`, then anything within that window | as above | windows of `k` elements at different multiples of `k` |
| `x[i : i+c]` or `x[i : min(i+c, n)]` | `for i := a; cond; i += c` | windows start `c` apart and are at most `c` long |
| `x[:m]` in one party, `x[m:]` or `x[m+d:]` in another | any | the two parts meet at `m` |

Here `c` and `d` are positive constants, or variables not written during the interval whose value the
compiler checks is positive before the first `s.Go` (a panic otherwise). For an `s.Go` of a closure
that calls a named function with `&x[i]` or a window of `x` in one of these forms, that argument counts
as the puroutine's access, since it is the only way the puroutine reaches `x`. Bounds checks keep a
puroutine inside its window, so once it has one it may index the window freely. Index arithmetic in
these forms, and a loop step other than `++` or `--`, is checked for overflow, so the parts cannot wrap
around onto each other. Indexing a library container (`results[i]` on a `vec.Vec`, spec section 6.7)
counts as `x[i]` when the container's `Index` and `SetIndex` methods touch only that element, which their
summaries show. A partition covers the element's own storage, not memory that pointers stored in the
elements point to: two elements may point to the same object.

This is Rust's `split_at_mut` and `chunks_mut` without the API: the compiler proves disjointness from
the slice expressions Go programmers already write.

**Memory reached through views.** A captured slice or pointer is shared like the variable holding it,
and so is what it points to. Two views may point to the same memory, so a write through a view of
element type `T` conflicts with any access, by another puroutine, through a view that can reach a `T`.
In safe code memory of one type is never seen as another, so this type-based rule is sound; `results
[]Result` and `items []Item` never conflict.

**Callees.** A call counts as the accesses its callee makes. The escape analysis already summarizes, for
each function, which parameter paths it keeps and stores; the sharing check adds which paths it reads
and writes, and calls through interfaces and func values use the union of their candidates, as the
escape analysis does. A function that calls a func-valued parameter from several puroutines at once
(a parallel map, section 7.2) records that in its summary, and a caller that passes a literal which
writes its captured variables without a lock is E3406.

**Package variables and heap memory that the frame does not reach** are not covered by this section.
They follow the memory safety design's section 7, which applies to all puroutines alike. With its
decision D5(b), signed off (a heap view crosses puroutines only if it is immutable or reached through
`synced[T]`), an unscoped puroutine can never hold a mutable view of memory the frame reaches, so the
scope's check sees every puroutine that can touch it.

**Why this rule and not the others.** The alternatives considered for shared writes such as
`results[i] = ...` were: only a library API that hands out disjoint parts (safe, but Go programmers do
not write one, and the rewrite of Go code would grow); a run-time check per element (a claim table and
an atomic operation per write, and a failure at run time instead of compile time); and requiring atomics
(impossible for a struct result). The partition proof accepts the code Go programmers write, fails at
compile time with the reason, and costs nothing at run time beyond the overflow checks.

### 4.6 What the starting function may do meanwhile

During the interval the starting function is one more party to section 4.5. In addition, for each
variable that a running puroutine uses:

- moving it, dropping it (`x.Drop()`), or assigning a new value to it when it is an owner, which
  drops the old one, is E3404: the puroutine would use freed memory;
- the move checker's "used after move" and the escape analysis's "used after the owner is gone" rules
  treat the puroutine's uses as uses at every point of the interval.

Reading a shared variable that nobody writes is allowed, and so is calling `s.Go` again.

## 5. Soundness

**Panics.** A panic aborts without unwinding (spec section 8). If a scoped puroutine panics, the
program ends while the starting function is parked; nothing is freed. If the starting function panics
between `s.Go` and the join, its frame is never reclaimed: the panicking puroutine never returns, and
its stack is not reused while the process lives, so puroutines still running read valid memory until
the process exits. If the false-friends audit's F4 is adopted (a hosted panic runs the panicking
puroutine's deferred calls), those calls run while the scoped puroutines still may, which is safe for
the same reason: no frame is reclaimed and no owner is dropped. A kernel's `//pu:panic` handler never
returns, but a kernel might want to end only the faulting task. The scheduler contract gains a rule
(abi.md section 12): a backend must not free or reuse the stack of a task that has not returned normally
until `rt.WaitScopes` (below) has returned for it.

**Non-local exits.** `testing.FailNow` (and `Fatal`, `SkipNow`) leaves the test today with `longjmp`,
past frames whose blocks never end, so neither drops nor joins run, and the runner then reuses that
stack. Scoped puroutines would write into it. Two pieces close this:

- A per-task chain of active joins: when a block that holds a local Scope with scoped `s.Go` calls
  begins, the Scope's join is pushed onto the running task's chain (two stores), and it is popped when
  the block ends. `rt.WaitScopes(sp)` waits for every join on the chain whose frame is below the stack
  pointer `sp`. A kernel calls it before freeing a task's stack.
- `FailNow` either calls `rt.WaitScopes` before its `longjmp`, or, if the false-friends audit's F5 is
  adopted, becomes `rt.Goexit`, which runs the deferred calls and drops of every frame it leaves; the
  Scope's drop then waits like any other, and the chain is needed only by kernels.

A `FailNow` called from a scoped puroutine (not the test's own puroutine) panics, as calling it from
another goroutine is a bug in Go.

**Early returns, `break`, `continue`, `goto`.** Each path out of the Scope's block drops it (section
4.3), as each drops every owner. Go forbids jumping into a block, so a join cannot be skipped by entering
one.

**Wait in a loop or conditionally.** The data flow of section 4.3 is exact about which `s.Go` calls
may be running at each point. A conditional `Wait` joins only on its path; accesses after the `if` are
checked against the puroutines still possibly running:

```go
if fast {
	s.Wait()
}
results[0] = Result{} // E3402: results may still be written by the puroutines started at line 12
```

**Copies and moves of the Scope.** `sync.Scope` is an owner, so assigning it moves it and there are no
copies. Running puroutines refer to the Scope at its address, so moving it, or assigning a new Scope to
the variable (which drops the old one and would wait at an unexpected place), while puroutines it started
may run is E3405. Once everything is joined, moving it is allowed. A struct that holds a Scope must be
declared `owner`, as for any owner field.

**Nested scopes and scopes inside puroutines.** A scoped puroutine runs a function on its own stack,
and a local Scope there scopes that function's puroutines to its frame. Joins nest as the frames do: the
inner function joins its puroutines before it returns, so before its own join. Puroutines it starts may
use the outer function's frame through its captures, since the outer frame outlives the inner join. A
plain `go` statement inside a scoped puroutine is still unscoped, so it may not be handed a view of
either frame.

**Puroutines started by scoped puroutines.** A scoped puroutine that uses `s` may call `s.Go` itself.
The new puroutine is counted while its starter is still counted, so the count cannot reach zero in
between.

**Interrupt context and the panic handler.** `s.Go` starts a puroutine, which is already E2585 there.
`Scope.Drop` and `Wait` park, so the interrupt checker must follow the drops the compiler adds as it
follows the calls it adds for `new` and interface conversions (spec section 12.6); with that, a Scope
in code an interrupt handler reaches is reported as a wait.

**Kernel mode.** `rt.Join` is built on `rt.Park` and `rt.Ready`, so it works on any scheduler. The
stack rule above is the only new requirement for a backend.

**Unsafe code.** `rt.Pinned`, `unsafe.Pointer` and assembly can still hide a frame pointer from the
analysis, as they can today (memory safety design, section 11).

## 6. Closures

A capturing literal handed to a scoped puroutine refers to the frame by reference, which is now valid
because the frame outlives the puroutine.

**The closure record.** A closure record is a code pointer followed by the address of each captured
variable (`internal/lower/func.go`). A literal written as the argument of `s.Go` would, in a loop, reuse
one record slot in the frame for every iteration. So its record is not built in the frame: the spawn's
argument block carries the addresses of shared variables and the values of taken ones, and the thunk
that runs first on the new puroutine builds the record in its own frame, pointing at the shared
variables and at its own copies of the taken ones. The literal's code is unchanged and each puroutine
has its own record.

**Func values built elsewhere.** A func value that is not a literal at the `s.Go` call, such as a
variable assigned a literal, a parameter, or a method value, is passed by its record's address. Its
record must last until the join, which the escape analysis checks like any other view (a record built
in a loop body is iteration storage, spec section 4.5). A parameter's record lives in a caller's frame
and always lasts long enough. The run-time check that panics on a `go` of a func value in the stack
(spec section 4.7) is not made for scoped puroutines; the compile-time proof replaces it.

**Closure constructors.** A closure constructor (spec section 4.5) builds its variables in the caller's
frame at the call site. When the call is the argument of `s.Go`, the compiler builds them in the
argument block instead, so that each puroutine started in a loop gets its own, as with a literal.

## 7. Examples

### 7.1 The idiom

Go, then Plutonium:

```go
// Go
func process(items []Item, results []Result) {
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Go(func() { results[i] = work(it) })
	}
	wg.Wait()
}

// Plutonium
func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Go(func() { results[i] = work(it) })
	}
	s.Wait() // optional: s waits when process returns in any case
}
```

The classic Go form, `wg.Add(1); go func() { defer wg.Done(); ... }()`, becomes the same `s.Go` call;
a Scope has no `Add` or `Done` to get wrong.

A local array works the same way, and is returned by value after the join:

```go
func squares() [8]int {
	var out [8]int
	var s sync.Scope
	for i := range out {
		s.Go(func() { out[i] = i * i })
	}
	s.Wait()
	return out
}
```

What the compiler checks: `results` (the slice header) and `items` are only read; `results[i]` is a
partition by the loop variable; `i` and `it` are taken; `s` is synchronized by type; nothing the
puroutines use is dropped or moved before the join.

### 7.2 A parallel map

```go
func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Go(func() { out[i] = f(in[i]) })
	}
}
```

`Map`'s summary records that it calls `f` from several puroutines at once. A caller that passes a pure
function or a literal that only reads its captures compiles; one that counts in a captured variable is
E3406 unless it locks or uses an atomic:

```go
var calls atomic.Int64
Map(names, lens, func(s string) int { calls.Add(1); return len(s) })
```

Chunked, for many small elements:

```go
func MapChunked[T, U any](in []T, out []U, f func(T) U, k int) {
	var s sync.Scope
	for i := 0; i < len(in); i += k {
		s.Go(func() {
			dst := out[i:min(i+k, len(out))] // window: the third form of the table
			for j := range dst {
				dst[j] = f(in[i+j])
			}
		})
	}
}
```

The window is the table's third form with `c` being `k`, which nothing writes while the puroutines
run; the compiler checks that `k` is positive before the loop starts any puroutine.

### 7.3 A worker pool with a local channel and mutex

```go
func crawl(urls []string) (int, error) {
	jobs := make(chan string, 16)
	var mu sync.Mutex
	var total int
	var first error
	var s sync.Scope
	for w := 0; w < 8; w++ {
		s.Go(func() {
			for u := range jobs {
				n, err := fetch(u)
				mu.Lock()
				total += n
				if err != nil && first == nil {
					first = err
				}
				mu.Unlock()
			}
		})
	}
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)
	s.Wait()
	return total, first
}
```

`jobs` is a channel variable used for channel operations; `total` and `first` are written only while
`mu` is held, which the compiler infers; `urls` is only read. Without the `mu.Lock()` line, `total += n`
is E3402. `s.Wait()` must come before `return`: reading `total` while the workers may write it is E3402,
and the drop at the end of the function would come too late for the read in the `return` statement.

### 7.4 Recursive splitting

```go
func sortPar(v []int) {
	if len(v) < 2048 {
		slices.Sort(v)
		return
	}
	p := partition(v)
	var s sync.Scope
	s.Go(func() { sortPar(v[:p]) })
	sortPar(v[p+1:])
}
```

The puroutine reaches `v` only through `v[:p]` and the starting function only through `v[p+1:]`, with
`p` unwritten: the table's last form, with `d` being 1. `s` waits when `sortPar` returns.

### 7.5 What stays rejected

Each message follows spec section 6.9: what happened, the idea it relies on, the related lines, fixes
as code. Codes E3401 to E3406 are new (section 8.7); E3301 to E3312 are the move checker's.

**A WaitGroup or a plain `go` with a frame view** (E2590 and E3004, which keep their codes and gain a
fix):

```
main.p:14:9: cannot pass the function literal to wg.Go: it uses results, and nothing makes process wait for it
    results is a local variable of process (declared at main.p:11). wg.Go starts
    the literal on a puroutine of its own, which may still be running after
    process returns, when results' memory is reclaimed. A WaitGroup waits only
    where Wait is called, so the compiler cannot count on it.
    Use a sync.Scope instead. It waits for its puroutines before its block ends,
    so they may use process's variables:
        var s sync.Scope
        s.Go(func() { results[i] = work(it) })
    Or pass the values it needs as arguments: go func(it Item) {...}(it)
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E2590" for a longer explanation)
```

For a `go` statement the first line reads `cannot start the function literal with go: it uses results,
and nothing makes process wait for it`, and the same fix follows. E2590's argument suggestion never
passes a `sync` type by value.

**A race between puroutines (E3402):**

```
count.p:15:4: total is written by the puroutines started at count.p:13 at the same time as each other
    Each call of s.Go at count.p:13 starts a puroutine, and they run at the
    same time. Each writes total (count.p:15), a local variable of count. Two
    puroutines writing one variable at once is a data race: an update can be
    lost, and a value larger than a word can be torn.
    Lock a mutex around every use of total:
        mu.Lock()
        total += n
        mu.Unlock()
    Or use an atomic counter:
        var total atomic.Int64
        total.Add(int64(n))
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3402" for a longer explanation)
```

When the other access is the starting function's, the first line reads `results is read at main.p:20
while the puroutines started at main.p:14 may still write it`, the explanation names where the join is
("s waits at main.p:22"), and the fix is to move the access after `s.Wait()`.

**A write the compiler cannot partition (E3403):**

```
grid.p:12:5: the puroutines started at grid.p:10 may write the same element of out
    Each puroutine writes out[idx(i)] (grid.p:12). Several puroutines may write
    one slice at once only when each writes elements no other can reach, and the
    compiler can see that only for out[i], or a window such as out[i*k:(i+1)*k],
    where i is the variable of the loop that starts them.
    Index by the loop variable:
        s.Go(func() { out[i] = f(i) })
    Or give each puroutine a window and index within it:
        row := out[i*w : (i+1)*w]
    Or send the results on a channel and store them in this function.
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3403" for a longer explanation)
```

A `[][]T` written as `grid[y][x]` gets this error too, since two rows may be the same slice; the fix
suggests a flat slice with a window per row, or a local `[H][W]T` array, whose rows are elements.

**A view of iteration storage that outlives the iteration (E3401):**

```
main.p:16:12: the puroutine started here uses v, a slice of line, which is reclaimed at the end of each iteration
    v looks at line, a local array declared in the loop body (main.p:13). Every
    iteration reuses line's memory, and s does not wait for this puroutine
    until main.p:19, after the loop, so a later iteration could overwrite the
    bytes while the puroutine reads them.
    Use line itself in the function literal. The puroutine then gets its own copy:
        s.Go(func() { send(line[:n]) })
    Or wait inside the loop, before the end of its body:
        s.Wait()
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3401" for a longer explanation)
```

**Dropping what a puroutine uses (E3404):**

```
main.p:20:2: cannot drop buf here: the puroutines started at main.p:15 may still be using it
    buf.Drop() frees buf's memory now. The puroutines started at main.p:15 use
    buf, and s does not wait for them until main.p:22, so they could write to
    freed memory.
    Wait for them first:
        s.Wait()
        buf.Drop()
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3404" for a longer explanation)
```

The same message covers moving `buf` ("cannot move buf to consume here") and assigning to it ("cannot
assign to buf here: assigning drops its old value").

**Moving or replacing the Scope (E3405):** "cannot move s while puroutines it started may be running:
they find s at its address, so after the move nothing would wait for them." For an assignment to `s`:
"assigning to s drops the old Scope, which would wait here for the puroutines started at main.p:12."
The fix is `s.Wait()` first.

**A func value called concurrently (E3406):**

```
main.p:9:14: Map calls f from several puroutines at once, and the function literal passed here writes count
    Map (pmap.p:12) starts a puroutine for each element, and each calls f. The
    literal passed here adds to count (main.p:10), a local variable of main, so
    the calls would write count at the same time.
    Use an atomic counter in the literal:
        var count atomic.Int64
        ... count.Add(1) ...
    Or lock a mutex around the update.
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3406" for a longer explanation)
```

**Still rejected in phase 1, accepted in phase 2 (section 10):** a Scope reached through a pointer
(`s := new(sync.Scope)`, or a `*sync.Scope` parameter) whose `Go` is handed a frame view, a helper that
calls `s.Go` on a Scope its caller owns, an error-collecting wrapper, and sending a view of the frame on
a channel whose every user is scoped. Each is reported as the first error above, with "s is reached
through a pointer, so the compiler cannot tell where it waits" where that applies.

## 8. Implementation sketch

### 8.1 Deciding which puroutines are scoped

The rule of section 4.2 is decided from the typed AST: the receiver of a call of `sync.Scope.Go` must be
named by a local path (identifier, constant field and index steps). It depends on nothing the optimizer
does. One helper (`types2` or a small new package used by `types2`, `lower`, `escape` and `moves`)
answers "is this call scoped, and to which Scope".

The existing checks apply to `s.Go` calls that are not scoped as they apply to `WaitGroup.Go`: the
escape analysis rejects a frame view handed to them (E3004), and so do `types2`'s `checkFrameView`
(E2011) and `lower`'s E2590 and E2594 where they see it first. Scoped calls skip them, and the
whole-program checks decide.

### 8.2 The interval analysis

A new package, `internal/scope`, computes for each function and literal the scoped `s.Go` calls, their
joins (calls of `Wait`, and the drops of the Scope that lowering will insert), and at each statement the
set of `s.Go` calls that may be running (section 4.3). It uses the same control-flow walk as
`internal/moves`. `escape`, `moves` and the sharing check consume its result.

### 8.3 Escape analysis (`internal/escape`)

- A scoped `s.Go` is not a sink. Each frame token it receives is checked against the interval: the
  location's storage must last until every join of that call, else E3401 (with taken variables, section
  4.4, exempted for their own storage but not for what they point to).
- Parameter tokens handed to a scoped `s.Go` do not escape, so they add nothing to the function's
  summary.
- Loop iteration storage held by a running puroutine across a back edge is E3401 (the loop check of
  `loops.go`, with "kept by a running puroutine" as the keeper).

### 8.4 Moves (`internal/moves`)

- An owner taken by a scoped puroutine is moved at the `s.Go` call.
- A scoped literal may move a captured owner (it runs exactly once); that is a move at the `s.Go` call,
  replacing E3305 (MoveCaptured) for scoped literals.
- Moving, dropping or reassigning a variable a running puroutine uses is E3404; moving or reassigning
  the Scope while its puroutines may run is E3405.

### 8.5 The sharing check (`internal/share`, new)

- Per-function effect summaries: which parameter paths and frame locations a function reads and
  writes, with the access paths of `internal/escape` (three loads, eight steps), plus whether it calls a
  func-valued parameter from several puroutines at once. Computed to a whole-program fixpoint like the
  escape summaries; a function's effects include those of its own scoped puroutines.
- A lockset pass over each body: `Lock`, `Unlock`, `RLock`, `RUnlock` and deferred unlocks of local
  locks.
- Per interval: the accesses of every party, the three rules of section 4.5, the partition forms, and
  type-based conflict for memory reached through views. E3402, E3403, E3406.

### 8.6 Lowering and runtime

- `pluto/rt` gains `type Join struct` (the running count, a waiter, a lock word, and the chain link and
  frame address of section 5), with `enter`, `exit` and `Wait` (`Wait` parks, so the compiler infers that
  it may wait), and `rt.WaitScopes(sp uintptr)`. The task record gains one word, the head of the chain.
- `pluto/sync` gains `Scope`, an owner holding a `Join`. `Scope.Go` is `rt.GoJoined(&s.join, f)`, a
  runtime entry that the compiler recognizes at the call site and lowers as a scoped spawn when the Scope
  is local; otherwise it is a plain spawn with the run-time closure check. `Scope.Wait` and `Scope.Drop`
  wait on the join.
- A scoped spawn lowers as `go` does today (argument block, thunk, `spawnGo`), plus `join.enter()` (an
  atomic increment) before the spawn, and `join.exit()` in the thunk after the call (an atomic
  decrement, and a wake if it reached zero with a waiter, under the join's lock, touching nothing
  afterwards, as `readyAll` does today). The thunk builds the closure record (section 6). No run-time
  closure check.
- Drops: a block's Scopes are dropped first among its variables (section 4.3); the drop is an inline
  load of the count and a call only if it is not zero. Block entry pushes each local Scope with scoped
  `s.Go` calls onto the task's chain, and the drop pops it.
- `sync.WaitGroup` does not change. `WaitGroup.Go` keeps its `rt.Pinned` use and run-time check until the
  escape analysis rejects frame closures handed to it at compile time (E3004), as for a `go` statement.
- `testing.FailNow` calls `rt.WaitScopes` with the runner's stack pointer before its `longjmp`, or
  becomes `rt.Goexit` (section 5).

### 8.7 Diagnostics

New codes in `internal/diag/codes.go`, with catalog entries in `catalog_build.go` (`Text`, `Why`,
`Spec`, `Fix`), and programs under `cmd/pluto/testdata/diag/`:

| Code | Name | When |
|---|---|---|
| E3401 | ScopeOutlivesVar | a running scoped puroutine uses storage that ends before its join |
| E3402 | ScopeRace | two parties may access one place at once, one writing, and it is not synchronized or guarded |
| E3403 | ScopeNotPartitioned | indexed writes the compiler cannot prove disjoint |
| E3404 | ScopeMoveInUse | moving, dropping or reassigning a variable a running puroutine uses |
| E3405 | ScopeMoved | moving or reassigning a Scope while puroutines it started may run |
| E3406 | ScopeConcurrentCall | a literal that writes its captures, passed where it is called concurrently |

E2590, E2594, E2011 and E3004 change their fixes: when the rejected puroutine uses the frame, they
suggest a `sync.Scope` (section 7.5), and E2590's argument suggestion never passes a `sync` type by
value. Every `Why` says the same thing in plain words: Go keeps such variables alive with its garbage
collector; Plutonium has none, so a `sync.Scope` waits for the puroutines before the frame ends instead,
and a WaitGroup, which waits only when told to, cannot.

### 8.8 go2pu

go2pu rewrites a `sync.WaitGroup` whose goroutines use the frame into a `sync.Scope` when every
goroutine it counts is started in one of these forms:

1. `wg.Go(f)`;
2. a `go` statement of a function literal whose body begins with `defer wg.Done()` or ends with
   `wg.Done()`, preceded by `wg.Add(n)`;
3. a `go` statement that passes `&wg` to a function that begins with `defer p.Done()` or ends with
   `p.Done()` on that parameter.

The declaration becomes `var wg sync.Scope` (the name is kept), forms 2 and 3 become `wg.Go(...)` calls
with the `Add` and `Done` calls removed, and `wg.Wait()` stays. In these forms `Done` is the last thing
the goroutine does, so Go's `Wait` already waited until the goroutine returned, and the rewrite changes
nothing a Go program could observe. Any other use of the WaitGroup (a latch that calls `Done` and keeps
running, a WaitGroup passed to code go2pu cannot see) keeps the WaitGroup and gets a note. `heapvars.go`,
which allocates every variable that Go's escape analysis moves to the heap, leaves alone variables whose
only escape is into a goroutine rewritten this way. The Plutonium compiler then checks the result, and
anything it rejects (a race Go tolerated, an unprovable partition) is a note for hand-fixing, as today.

### 8.9 Costs

| | Plain `go` | `s.Go` (scoped) |
|---|---|---|
| start | allocate task and stack, copy the argument block, spawn | the same, plus one atomic increment |
| finish | free task and stack | the same, plus one atomic decrement (and a wake for the last) |
| closure record | not allowed | built by the thunk: one store per captured variable |
| join | none | `Wait` parks if needed; the drop is one load when already joined; two stores for the chain per block |
| compile time | escape check | plus the interval, effect summaries and lockset passes |

The cost of `s.Go` is that of Go's `WaitGroup.Go`. Stacks are the same as for any puroutine: 64 KiB by
default, or what the planned stack analysis (roadmap item 18) computes for the thunk and the literal.

### 8.10 Tests

- `internal/build/testdata/lib`: the programs of section 7. They use `sync.Scope`, which Go does not
  have, so they cannot be `testdata/run` programs; their expected output is checked by running the
  WaitGroup form of each under Go, where it means the same.
- `internal/build/testdata/run` keeps programs that use `sync.WaitGroup` as Go does (with heap or
  package variables), so the oracle checks that WaitGroup behaves exactly as Go's.
- `cmd/pluto/testdata/diag`: one program per new code and per changed message.
- Unit tests for the interval analysis (conditional waits, waits in loops, `defer s.Wait()`, `goto`,
  drop order within a block), the lockset pass, and each partition form, including the overflow checks.
- A kernel test under `testdata/kernel/sched` with a scoped puroutine on the test kernel's scheduler.
- A `pluto/testing` test that calls `t.Fatal` while scoped puroutines run.
- go2pu tests for each of the three rewritten forms and for a latch that stays a WaitGroup.

## 9. What stays as is

A `go` statement keeps every rule of spec section 4.7: arguments are copied and must be sendable, views
of the frame and capturing literals are rejected, and a func value whose record is in the stack panics
when the statement runs. Its cost does not change. `sync.WaitGroup` is Go's: `Add`, `Done`, `Wait` and
`Go` behave as in Go, it never waits on its own, and the puroutines it counts are unscoped, so a frame
view handed to them is a compile error whose fix names `sync.Scope`. Fire-and-forget puroutines, servers
that start a puroutine per connection, `time.AfterFunc`, and latches built from a WaitGroup are all
unscoped.

## 10. Phases

**Phase 1.** Everything in sections 4 to 8 for Scopes named by a local path: `sync.Scope`, its drop and
the drop order, taken variables, closures built by the thunk, the sharing rules, the chain and
`FailNow`, the diagnostics, go2pu's rewrite, and spec changes (a new section 4.11, and edits to 4.5, 4.7,
6.3, 6.9 and 13; memory-model.md 3.2 and 3.4: the return of a scoped puroutine is synchronized before
the return of the `Wait` or drop that waits for it; abi.md section 12: the task word and the stack rule).

**Phase 2.** Summaries in place of local names, still without annotations:

- "spawns `f` joined by parameter path `p`" and "waits on `p`", so that a helper can start puroutines
  for a Scope its caller owns (`func fanOut(s *sync.Scope, parts [][]byte)`), and so that a wrapper
  type can embed a Scope;
- an error-collecting scope in the library, with Go's `errgroup` shape under a name that says it waits
  (for example `sync.ErrScope`, with `Go(func() error)` and `Wait() error`);
- points-to resolution of the Scope, so that `p := &s` counts as local;
- locks taken by callees on parameter paths;
- frame views sent on a local channel whose senders and receivers are all in the scope.

The same lock inference replaces the `//pu:guardedby` annotation for `synced[T]` (memory safety
decision D7(c), signed off): one implementation serves both.

## 11. Summary

A puroutine started by `s.Go` on a local `sync.Scope` is waited for by that Scope, through a count that
only the runtime changes, before the Scope's block ends on any path. It may use the frame and capture
variables; the compiler takes the variables that would end first, and proves that the puroutines and
the starting function do not race: shared places are read-only, synchronized by type, guarded by a lock
it infers, or partitioned by the loop variable. The difference from Go is in the source, a type Go does
not have, and `sync.WaitGroup` behaves exactly as Go's.

## 12. Decisions for sign-off

| # | Decision | Options | Recommendation |
|---|---|---|---|
| D1 | Surface | (a) a new type, an owner whose drop waits; (c) a scoped block; (d) a scope operand on `go` | (a): the difference is visible in a declaration, and the join is the drop rule that already runs on every path |
| D2 | Name and API | `sync.Scope` or `sync.Group`; with or without `Add`/`Done` | `sync.Scope` with `Go`, `Wait` and `Drop` only: `Group` reads as Go's `errgroup.Group`, which does not wait on its own, and without `Add`/`Done` the count cannot be wrong |
| D3 | Join at the end of the block | (a) the Scope's drop waits, on every path, before the block's other drops; (b) a compile error unless every path calls `Wait` | (a): it is what drops already do, makes early returns safe without edits, and a hang there shows in the deadlock trace as a Scope wait |
| D4 | Shared writes | (a) partitions by loop variable and split point, inferred locks, atomics; (b) partitions only through a library API; (c) atomics and locks only; (d) run-time checks | (a), with the forms of section 4.5; extend the table only with forms whose disjointness follows from the slice expressions |
| D5 | Lock inference | (a) infer the guarding lock for scoped code; (b) require `//pu:guardedby` | (a), one implementation with memory safety D7(c) |
| D6 | Taken variables | (a) copy or move captured variables whose block ends before the join, as Go 1.22's per-iteration variables; (b) require them as arguments | (a): it is Go's meaning for the same source |
| D7 | Non-local exits | (a) a per-task chain of joins and `rt.WaitScopes`, used by kernels and by `FailNow`; (b) forbid `FailNow` while scoped puroutines may run | (a); if the false-friends audit's F5 (`rt.Goexit`) is adopted, `FailNow` drops the Scope like any owner and the chain serves kernels only. F5 is implemented (language spec section 8.3): `Goexit` leaves every frame through its ordinary return path, so the Scope's drop, emitted there like any owner's, joins; `FailNow` needs no `rt.WaitScopes` |
| D8 | Heap views across unscoped puroutines | memory safety D5 | its (b), signed off: only then is the scope's race check complete against puroutines outside it |
| D9 | `sync.WaitGroup` | (a) Go's behavior exactly, with today's escape errors for frame views and a fix that names `sync.Scope`; (b) the joining WaitGroup of the first version | (a): (b) is a false friend (section 13) |
| D10 | go2pu | (a) rewrite a WaitGroup to a Scope when every goroutine it counts calls `Done` last (section 8.8); (b) leave WaitGroups and report a note | (a): the rewrite is exact for those forms, and the rest get notes |

## 13. Superseded decisions

| Old decision | Replaced by | Date | Decided by | Why |
|---|---|---|---|---|
| D1 (b'): `sync.WaitGroup` joins the puroutines it is handed, through a second counter only the runtime changes, with a wait inserted at the end of its block | D1 (a) and D2: `sync.Scope` | 2026-10-10 | the project owner, by adopting the no-false-friends rule | A WaitGroup that waits at the end of its block changes when a Go function returns, and nothing in the source shows it. It was chosen so that Go source compiles unchanged, which is no longer a goal. |
| D2 (a): the classic form (`go` with `defer wg.Done()` first or `wg.Done()` last) is scoped as well as `wg.Go`; a latch that calls `Done` and runs on is not | D2 and D10: only `s.Go` starts scoped puroutines; go2pu rewrites the classic form | 2026-10-10 | as above | It depended on D1 (b'), and whether a puroutine was joined depended on where `Done` appeared in its body, which a reader would have to know. |
| Goal 1: the idiom, `wg.Go` and `go worker(&wg, ...)` compile unchanged, and go2pu translates them as the identity | Goals 1 and 2 (section 2) | 2026-10-10 | as above | Compiling Go source unchanged is no longer a goal of the language; Go code is kept as a test oracle instead. |
