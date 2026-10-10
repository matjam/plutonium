# Scoped puroutines: design

**Date:** 2026-10-10
**Status:** proposal, not implemented. Section 12 lists the decisions that need sign-off. It builds on
the memory safety design (`docs/plans/2026-10-09-memory-safety-design.md`, on the escape-checks branch
until it merges), whose escape analysis (`internal/escape`) and move checker (`internal/moves`) it
extends.

## 1. The problem

The most common Go concurrency idiom does not compile:

```go
func process(items []Item) []Result {
	results := make([]Result, len(items))
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		go func(i int, it Item) { defer wg.Done(); results[i] = work(it) }(i, it)
	}
	wg.Wait()
	return results
}
```

The literal captures `wg` and `results`, which live in `process`'s frame, so the `go` statement is
E2590. Passing `&wg` to a named function instead is E2011. Go 1.25's form, `wg.Go(func() {...})`,
compiles, and then panics when it runs, because `WaitGroup.Go` starts `f` with a plain `go` statement
and the runtime finds `f`'s closure record in the starting stack. Today's workarounds are to allocate
the WaitGroup and every shared variable with `new` (go2pu does this, and leaks them), or to make them
package variables.

The rejection is right for a plain `go` statement: the puroutine may outlive the frame. It is wrong for
this program, which waits for every puroutine before `process` returns. Rust accepts the same program
with `std::thread::scope`: threads started in a scope are joined before the scope ends, so they may
borrow the frame. This design gives Plutonium the same guarantee without new syntax, without
annotations and without lifetimes.

Two smaller defects turned up while checking the current behavior:

- E2590's suggested fix for the program above is `go func(wg pluto/sync.WaitGroup) {...}(wg)`. That
  copies the WaitGroup, so `Done` decrements the copy and `wg.Wait()` never returns. The type is also
  printed with its full package path.
- `wg.Go(func() { n++ })` is a run-time panic where the compiler can see the literal.

## 2. Goals

1. The idiom above, Go 1.25's `wg.Go`, and `go worker(&wg, ...)` compile unchanged, so go2pu's
   translation of them is the identity.
2. Safe code keeps the guarantees of the memory safety design: no view outlives what it looks at, and
   no data races.
3. No new syntax, no annotations, no lifetimes. Every rejection is explained in the program's own names.
4. Whether a program compiles does not depend on optimization, as for the escape checks.
5. Plain `go` statements keep their current rules and cost.

## 3. Options for the surface

**(a) A new library type.** `var g sync.Group; g.Go(func() {...}); g.Wait()`, with a compiler rule that
a Group is waited on before its frame ends. This is Rust's design moved into a type. It adds an API Go
does not have, so go2pu must rewrite every WaitGroup that guards captured variables, and Go readers meet
an unfamiliar type. It also needs rules for Groups that are copied or moved while puroutines run, which
a WaitGroup needs anyway.

**(b) Make `sync.WaitGroup` scoped by inference.** Prove, by whole-program analysis, that `Wait`
dominates the return on every path after the `go` statements. As stated this is unsound: `Wait` returns
when the counter reaches zero, and the counter is whatever `Add` and `Done` make it. A `Done` called
before the puroutine's last access, an extra `Done` elsewhere, or `Add(-1)` lets `Wait` return while a
puroutine still uses the frame. No analysis of `Wait`'s position can fix that, because the counter does
not know which puroutines exist.

**(c) A language construct.** A block such as `scope { go ...; go ... }` that joins at its end. It is
explicit and easy to check, but it is new syntax, Go code has no such block, and go2pu would have to
restructure every function that uses the idiom.

**(b') Recommended: the WaitGroup joins the puroutines it is handed.** Keep (b)'s surface, and fix its
flaw in the runtime instead of in an analysis. A WaitGroup gets a second counter that only the runtime
touches: a puroutine started for it is counted before it starts and uncounted after its function
returns. `Wait` waits for both counters. Since `Add` and `Done` cannot change the second counter,
`wg.Wait()` returning proves that every such puroutine has returned, whatever the program does with
`Add` and `Done`. The compiler then needs only to know which puroutines are counted (section 4.1) and
where the waits are (section 4.2), both of which it can see in one function.

| | Go source compiles unchanged | New API or syntax | go2pu | Sound |
|---|---|---|---|---|
| (a) `sync.Group` | no | a type | rewrite each use | yes |
| (b) inference | yes | none | identity | no |
| (c) scoped block | no | syntax | restructure | yes |
| (b') joining WaitGroup | yes | none | identity | yes |

## 4. The design

### 4.1 Which puroutines are scoped

A WaitGroup is **local** when it is a local variable of the function, or a field or array element of
one at a constant path (`s.wg`, `wgs[2]`), so that the compiler knows where it lives and that it lives
until the end of its block. A puroutine is **scoped** to a local WaitGroup `wg` when it is started in
one of these ways:

1. `wg.Go(f)`, Go 1.25's form.
2. A `go` statement of a function literal that uses `wg` and whose body begins with `defer wg.Done()` or
   ends with `wg.Done()`.
3. A `go` statement that passes `&wg` to a function that begins with `defer p.Done()` or ends with
   `p.Done()` on that parameter.

The condition on `Done` in forms 2 and 3 is not needed for safety, which the second counter provides. It
makes sure the program behaves exactly as it does in Go: when `Done` is the last thing the puroutine
does, Go's `Wait` already waits until the puroutine has returned, so counting it changes nothing. A
puroutine that calls `Done` and keeps running is a latch, not a worker:

```go
var ready sync.WaitGroup
ready.Add(1)
go func() { setup(); ready.Done(); serve() }() // runs on after ready.Wait() returns
ready.Wait()
```

Joining it would make `ready.Wait()` wait for `serve` to return, which changes what the program does,
so it is not scoped, and it stays an error because it uses the frame (section 9).

A puroutine started in any other way is **unscoped**, and the rules of spec section 4.7 apply to it
unchanged.

What makes a WaitGroup join is a runtime type, `rt.Join`, which `sync.WaitGroup` embeds (section 8.5).
Phase 2 (section 10) lets other types that embed one, such as an errgroup, work the same way, through
function summaries rather than annotations.

### 4.2 Joins

A scoped puroutine is **joined** when the function that started it is sure it has returned:

- after a call `wg.Wait()` on the same WaitGroup, which waits for both counters;
- at the end of `wg`'s block, on every path out of it (falling off the end, `return`, `break`,
  `continue`, `goto`), where the compiler inserts a wait for the second counter. In a function's
  outermost block the wait runs after deferred calls and before variables are dropped. A deferred
  `wg.Wait()` joins at the same point.

The compiler follows, for each statement of the function, which `go` statements may have puroutines
that are not yet joined. This is a forward data-flow problem like the move checker's "may have moved":
a scoped `go` statement adds itself, a `wg.Wait()` removes every `go` statement scoped to `wg`, and
paths join by union. The interval from a `go` statement to the join is where the rules of sections 4.3
to 4.5 apply. A `Wait` inside a loop joins each iteration's puroutines before the next iteration; a
`Wait` in one branch of an `if` joins only on that branch, and the puroutines count as running after the
`if`.

The inserted wait is what makes early returns safe:

```go
for i, it := range items {
	wg.Go(func() { results[i] = work(it) })
}
if err := check(items); err != nil {
	return err // waits for the puroutines here, before process's frame is reclaimed
}
wg.Wait()
```

When an explicit `wg.Wait()` has already joined everything on a path, the inserted wait finds the
counter at zero and costs one load.

### 4.3 What a scoped puroutine may use

A scoped puroutine may use any variable of the starting function, and anything a view in it points
to, as long as its storage lasts until the join. That covers views of the frame (`&x`, `buf[:n]` of a
local array, `&T{...}`), capturing function literals, method values of local variables, and views
derived from parameters, which the caller's frame keeps alive beyond the join. None of them are sinks
for the escape analysis: a view handed to a scoped puroutine is bounded by the join, which comes before
the function returns.

**Variables that end before the join are taken.** A captured variable whose block may end before the
puroutine is joined, such as a loop variable or a variable declared in the loop body when the `Wait`
is after the loop, is **taken** by the puroutine when the `go` statement runs: a value is copied, an
owner is moved, and the puroutine uses its own copy. This is Go 1.22's per-iteration semantics, which
Plutonium cannot give by reference because every iteration reuses the same frame memory (spec section
4.5). The copy cannot be observed: the parent and the puroutine would need to share the variable after
the `go` statement, which the sharing rules of section 4.4 already reject unless neither writes it, in
which case a copy and a shared variable read the same.

```go
for i, it := range items {           // i and it end with the iteration
	wg.Go(func() { results[i] = work(it) }) // the puroutine takes i and it; results is shared
}
wg.Wait()
```

A taken owner is dropped by the puroutine when it returns; the starting function may not use it after
the `go` statement, as after any move. A captured owner whose block outlives the join stays where it is,
and the literal may use it, as any literal may; since a scoped literal runs exactly once, moving it
inside the literal is allowed too, and counts as a move at the `go` statement.

**Views of variables that end before the join are errors.** Taking a variable copies it; it does not
copy what a view in it points to. A slice of a loop-body array, taken into a puroutine that runs past
the iteration, still looks at that array, and is E3301 (section 8.7).

**Taken values must be sendable.** What a puroutine takes crosses to another processor, so the sendable
rule (spec section 4.7) applies to it. Variables used by reference are governed by section 4.4 instead.

### 4.4 Sharing: no data races

Scoped puroutines share the frame with each other and with the function that started them, and safe
code must still have no data races. The compiler can check this completely for the frame, because every
puroutine that can reach it during the interval is written in this function (or reached through its
callees' summaries), and nothing outside can reach it.

The rule: during the interval of section 4.2, every place that a scoped puroutine uses and that anyone
writes (the starting function, a puroutine, or several instances of one `go` statement in a loop) must
be one of:

1. **Synchronized by type.** A `pluto/sync` or `pluto/sync/atomic` value used through its methods, or a
   channel variable used for channel operations. Assigning a whole new value to such a place is a write
   like any other, so `wg = sync.WaitGroup{}` while puroutines run is rejected (E3305).
2. **Guarded by a local lock.** Every access to the place, in every puroutine and in the starting
   function, happens while one and the same local `sync.Mutex` (or the write lock of a local
   `sync.RWMutex`, with reads under `RLock`) is held. The compiler infers this: it follows
   `Lock`, `Unlock` and `defer mu.Unlock()` through each body with the same flow analysis, and
   intersects the sets of locks held at each access, as Clang's thread-safety analysis does with
   annotations and Eraser does at run time. A local lock is one the compiler can name, as in section
   4.1. No `//pu:guardedby` is needed.
3. **Partitioned.** Each puroutine writes elements or windows of a slice or array that no other
   puroutine or the starting function can reach during the interval, in one of the forms below.

Places that nobody writes during the interval may be read by everyone. Anything else is E3302, which
names both accesses.

**Partitions.** The compiler accepts these forms, where `i` is the puroutine's own copy (section 4.3)
of the variable of the innermost loop around the `go` statement, the `go` statement runs at most once
per iteration, and neither `x` nor `k` nor `m` is written during the interval:

| Accesses in the puroutine | Loop | Why the parts are disjoint |
|---|---|---|
| `x[i]` and its fields | `for i := range ...`; `for i := a; cond; i++` (or `i--`, or `i += c`) | each iteration has a different `i` |
| `x[i*k : i*k+k]` or `x[i*k : (i+1)*k]`, then anything within that window | as above | windows of `k` elements at different multiples of `k` |
| `x[i : i+c]` or `x[i : min(i+c, n)]` | `for i := a; cond; i += c` | windows start `c` apart and are at most `c` long |
| `x[:m]` in one party, `x[m:]` or `x[m+d:]` in another | any | the two parts meet at `m` |

Here `c` and `d` are positive constants, or variables not written during the interval whose value the
compiler checks is positive before the first `go` statement (a panic otherwise). For a `go`
statement of a named function, an argument `&x[i]` or a window of `x` in one of these forms counts as
the puroutine's access, since it is the only way the puroutine reaches `x`. Bounds checks keep a
puroutine inside its window, so once it has one it may index the window freely.
Index arithmetic in these forms, and a loop step other than `++` or `--`, is checked for overflow, so
the parts cannot wrap around onto each other. Indexing a library container (`results[i]` on a
`vec.Vec`, spec section 6.7) counts as `x[i]` when the container's `Index` and `SetIndex` methods touch
only that element, which their summaries show. A partition covers the element's own storage, not
memory that pointers stored in the elements point to: two elements may point to the same object.

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
writes its captured variables without a lock is E3306.

**Package variables and heap memory that the frame does not reach** are not covered by this section.
They follow the memory safety design's section 7, which applies to all puroutines alike. With its
decision D5(b) (a heap view crosses puroutines only if it is immutable or reached through `synced[T]`),
an unscoped puroutine can never hold a mutable view of memory the frame reaches, so the scope's check
sees every puroutine that can touch it. With D5(a), scoped puroutines are race-free among themselves
but not against unscoped puroutines that were given the same heap memory, which is today's state for
all puroutines.

**Why this rule and not the others.** The alternatives considered for shared writes such as
`results[i] = ...` were: only a library API that hands out disjoint parts (safe, but Go's code does not
use one, so go2pu could not translate the idiom); a run-time check per element (a claim table and an
atomic operation per write, and a failure at run time instead of compile time); and requiring atomics
(impossible for a struct result). The partition proof accepts the code Go programmers write, fails at
compile time with the reason, and costs nothing at run time beyond the overflow checks.

### 4.5 What the starting function may do meanwhile

During the interval the starting function is one more party to section 4.4. In addition, for each
variable that a running puroutine uses:

- moving it, dropping it (`x.Drop()`), or assigning a new value to it when it is an owner, which
  drops the old one, is E3304: the puroutine would use freed memory;
- the move checker's "used after move" and the escape analysis's "used after the owner is gone" rules
  treat the puroutine's uses as uses at every point of the interval.

Reading a shared variable that nobody writes is allowed, and so is calling `wg.Go` again.

## 5. Soundness

**Panics.** A panic aborts without unwinding (spec section 8). If a scoped puroutine panics, the
program ends while the starting function is parked; nothing is freed. If the starting function panics
between the `go` statement and the join, its frame is never reclaimed: the panicking puroutine never
returns, and its stack is not reused while the process lives, so puroutines still running read valid
memory until the process exits. This is enough in hosted builds. A kernel's `//pu:panic` handler
never returns, but a kernel might want to end only the faulting task. The scheduler contract gains a
rule (abi.md section 12): a backend must not free or reuse the stack of a task that has not returned
normally until `rt.WaitScopes` (below) has returned for it.

**Non-local exits.** `testing.FailNow` (and `Fatal`, `SkipNow`) leaves the test with `longjmp`, past
frames whose blocks never end, so neither inserted waits nor drops run, and the runner then reuses
that stack. Scoped puroutines would write into it. The fix is a per-task chain of active joins: when a
block that holds a local WaitGroup with scoped `go` statements begins, the WaitGroup's join is pushed
onto the running task's chain (two stores), and it is popped when the block ends. `rt.WaitScopes(sp)`
waits for every join on the chain whose frame is below the stack pointer `sp`. `FailNow` calls it
before jumping, and a kernel calls it before freeing a task's stack. A `FailNow` called from a scoped
puroutine (not the test's own puroutine) panics, as calling it from another goroutine is a bug in Go.

**Early returns, `break`, `continue`, `goto`.** Each path out of `wg`'s block runs the inserted wait
(section 4.2), as each runs drops. Go forbids jumping into a block, so a join cannot be skipped by
entering one.

**Wait in a loop or conditionally.** The data flow of section 4.2 is exact about which `go` statements
may be running at each point. A conditional `Wait` joins only on its path; accesses after the `if`
are checked against the puroutines still possibly running:

```go
if fast {
	wg.Wait()
}
results[0] = Result{} // E3302: results may still be written by the puroutines started at line 12
```

**Other `Done` and `Add` calls.** They change only the first counter, which the safety argument does
not use. An extra `Done` can make `Wait` return no earlier than the last scoped puroutine.

**Copies and moves of the WaitGroup.** `sync.WaitGroup` stays a value type, not an owner, so Go
structs that embed one need no `owner` mark. Running puroutines refer to the WaitGroup at its address;
a copy made while they run counts none of them, and overwriting the original would reset its counter
and let the join pass early. Both are writes of a synchronized place (section 4.4, rule 1) and are
E3305. Once everything is joined, copying is allowed, as in Go (where `go vet` reports it).

**Nested scopes and scopes inside puroutines.** A scoped puroutine runs a function on its own stack,
and a local WaitGroup there scopes that function's puroutines to its frame. Joins nest as the frames
do: the inner function joins its puroutines before it returns, so before its own join. Puroutines it
starts may use the outer function's frame through its captures, since the outer frame outlives the
inner join. A plain `go` statement inside a scoped puroutine is still unscoped, so it may not be handed
a view of either frame.

**Puroutines started by scoped puroutines.** A scoped puroutine that uses `wg` may call `wg.Go` itself.
The new puroutine is counted while its starter is still counted, so the counter cannot reach zero in
between.

**Interrupt context and the panic handler.** Starting a puroutine is already E2585 there, and the
inserted wait calls a `//pu:blocking` function, which the interrupt checker follows like any call the
compiler adds. Nothing changes.

**Kernel mode.** `rt.Join` is built on `rt.Park` and `rt.Ready`, so it works on any scheduler. The
stack rule above is the only new requirement for a backend.

**Unsafe code.** `rt.Pinned`, `unsafe.Pointer` and assembly can still hide a frame pointer from the
analysis, as they can today (memory safety design, section 11).

## 6. Closures

A capturing literal handed to a scoped puroutine refers to the frame by reference, which is now valid
because the frame outlives the puroutine.

**The closure record.** A closure record is a code pointer followed by the address of each captured
variable (`internal/lower/func.go`). A literal written at the `go` statement or as the argument of
`wg.Go` would, in a loop, reuse one record slot in the frame for every iteration. So its record is not
built in the frame: the `go` statement's argument block carries the addresses of shared variables and
the values of taken ones, and the thunk that runs first on the new puroutine builds the record in its
own frame, pointing at the shared variables and at its own copies of the taken ones. The literal's
code is unchanged and each puroutine has its own record.

**Func values built elsewhere.** A func value that is not a literal at the `go` statement, such as a
variable assigned a literal, a parameter, or a method value, is passed by its record's address. Its
record must last until the join, which the escape analysis checks like any other view (a record built
in a loop body is iteration storage, spec section 4.5). A parameter's record lives in a caller's frame
and always lasts long enough. The run-time check that panics on a `go` of a func value in the stack
(spec section 4.7) is not made for scoped puroutines; the compile-time proof replaces it.

**Closure constructors.** A closure constructor (spec section 4.5) builds its variables in the caller's
frame at the call site. When the call is the argument of `wg.Go`, the compiler builds them in the
argument block instead, so that each puroutine started in a loop gets its own, as with a literal.

## 7. Examples

### 7.1 The idiom

All three Go forms compile as written:

```go
func process(items []Item) []Result {
	results := make([]Result, len(items))
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Go(func() { results[i] = work(it) }) // Go 1.25
	}
	wg.Wait()
	return results
}
```

```go
	for i, it := range items {
		wg.Add(1)
		go func(i int, it Item) { defer wg.Done(); results[i] = work(it) }(i, it) // classic
	}
```

```go
func fill(wg *sync.WaitGroup, out *Result, it Item) {
	defer wg.Done()
	*out = work(it)
}

	for i, it := range items {
		wg.Add(1)
		go fill(&wg, &results[i], it) // named function: &results[i] is the partition x[i]
	}
```

A local array works the same way, and is returned by value after the join:

```go
func squares() [8]int {
	var out [8]int
	var wg sync.WaitGroup
	for i := range out {
		wg.Go(func() { out[i] = i * i })
	}
	wg.Wait()
	return out
}
```

What the compiler checks: `results` (the slice header) and `items` are only read; `results[i]` is a
partition by the loop variable; `i` and `it` are taken; `wg` is synchronized by type; `return results`
comes after the join.

### 7.2 A parallel map

```go
func Map[T, U any](in []T, out []U, f func(T) U) {
	var wg sync.WaitGroup
	for i := range in {
		wg.Go(func() { out[i] = f(in[i]) })
	}
	wg.Wait()
}
```

`Map`'s summary records that it calls `f` from several puroutines at once. A caller that passes a pure
function or a literal that only reads its captures compiles; one that counts in a captured variable is
E3306 unless it locks or uses an atomic:

```go
var calls atomic.Int64
Map(names, lens, func(s string) int { calls.Add(1); return len(s) })
```

Chunked, for many small elements:

```go
func MapChunked[T, U any](in []T, out []U, f func(T) U, k int) {
	var wg sync.WaitGroup
	for i := 0; i < len(in); i += k {
		wg.Go(func() {
			dst := out[i:min(i+k, len(out))] // window: the third form of the table
			for j := range dst {
				dst[j] = f(in[i+j])
			}
		})
	}
	wg.Wait()
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
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Go(func() {
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
	wg.Wait()
	alloc.FreeChan(jobs) // until channels are counted (spec section 4.8)
	return total, first
}
```

`jobs` is a channel variable used for channel operations; `total` and `first` are written only while
`mu` is held, which the compiler infers; `urls` is only read. Without the `mu.Lock()` line, `total += n`
is E3302.

### 7.4 Recursive splitting

```go
func sortPar(s []int) {
	if len(s) < 2048 {
		slices.Sort(s)
		return
	}
	p := partition(s)
	var wg sync.WaitGroup
	wg.Go(func() { sortPar(s[:p]) })
	sortPar(s[p+1:])
	wg.Wait()
}
```

The puroutine reaches `s` only through `s[:p]` and the starting function only through `s[p+1:]`, with
`p` unwritten: the table's last form, with `d` being 1.

### 7.5 What stays rejected

Each message follows spec section 6.9: what happened, the idea it relies on, the related lines, fixes
as code. Codes E3301 to E3306 are new (section 8.7).

**No WaitGroup waits for the puroutine** (replaces E2590 and E3004 when the puroutine uses the frame):

```
main.p:14:6: cannot start the function literal with go: it uses results, and nothing waits for it before process returns
    results is a local variable of process (declared at main.p:11). A puroutine
    started with go runs on its own and may still be running after process
    returns, when results' memory is reclaimed.
    Start it with a local sync.WaitGroup. Its Wait, and the end of its block,
    wait until the puroutine has returned:
        var wg sync.WaitGroup
        wg.Go(func() { results[i] = work(it) })
        wg.Wait()
    Or pass the values it needs as arguments: go func(it Item) {...}(it)
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E2590" for a longer explanation)
```

The latch of section 4.1 adds: "The function literal calls ready.Done() at main.p:9 and goes on
running, so ready.Wait() does not wait for it to return. Signal readiness with a channel passed as an
argument instead."

**A race between puroutines (E3302):**

```
count.p:15:4: total is written by the puroutines started at count.p:13 at the same time as each other
    Each call of wg.Go at count.p:13 starts a puroutine, and they run at the
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
    (run "pluto explain E3302" for a longer explanation)
```

When the other access is the starting function's, the first line reads `results is read at main.p:20
while the puroutines started at main.p:14 may still write it`, the explanation names where the join is
("wg is waited for at main.p:22"), and the fix is to move the access after `wg.Wait()`.

**A write the compiler cannot partition (E3303):**

```
grid.p:12:5: the puroutines started at grid.p:10 may write the same element of out
    Each puroutine writes out[idx(i)] (grid.p:12). Several puroutines may write
    one slice at once only when each writes elements no other can reach, and the
    compiler can see that only for out[i], or a window such as out[i*k:(i+1)*k],
    where i is the variable of the loop that starts them.
    Index by the loop variable:
        wg.Go(func() { out[i] = f(i) })
    Or give each puroutine a window and index within it:
        row := out[i*w : (i+1)*w]
    Or send the results on a channel and store them in this function.
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3303" for a longer explanation)
```

A `[][]T` written as `grid[y][x]` gets this error too, since two rows may be the same slice; the fix
suggests a flat slice with a window per row, or a local `[H][W]T` array, whose rows are elements.

**A view of iteration storage that outlives the iteration (E3301):**

```
main.p:16:12: the puroutine started here uses s, a slice of line, which is reclaimed at the end of each iteration
    s looks at line, a local array declared in the loop body (main.p:13). Every
    iteration reuses line's memory, and wg does not wait for this puroutine
    until main.p:19, after the loop, so a later iteration could overwrite the
    bytes while the puroutine reads them.
    Use line itself in the function literal. The puroutine then gets its own copy:
        wg.Go(func() { send(line[:n]) })
    Or wait inside the loop, before the end of its body:
        wg.Wait()
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3301" for a longer explanation)
```

**Dropping what a puroutine uses (E3304):**

```
main.p:20:2: cannot drop buf here: the puroutines started at main.p:15 may still be using it
    buf.Drop() frees buf's memory now. The puroutines started at main.p:15 use
    buf, and wg does not wait for them until main.p:22, so they could write to
    freed memory.
    Wait for them first:
        wg.Wait()
        buf.Drop()
    (see "Scoped puroutines" in the Plutonium spec, section 4.11)
    (run "pluto explain E3304" for a longer explanation)
```

The same message covers moving `buf` ("cannot move buf to consume here") and assigning to it ("cannot
assign to buf here: assigning drops its old value").

**Copying or resetting the WaitGroup (E3305):** "cannot copy wg while puroutines it waits for may be
running: they are counted in wg at its address, so a copy does not wait for them." For an assignment
to `wg`: "assigning to wg resets its counters, so its Wait would not wait for them."

**A func value called concurrently (E3306):**

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
    (run "pluto explain E3306" for a longer explanation)
```

**Still rejected in phase 1, accepted in phase 2 (section 10):** a WaitGroup reached through a pointer
(`wg := new(sync.WaitGroup)`, or a `*sync.WaitGroup` parameter), a helper that calls `wg.Go` on a
WaitGroup its caller owns, an errgroup-style wrapper, and sending a view of the frame on a channel
whose every user is scoped. Each is reported as the unscoped error above, with "wg is reached through a
pointer, so the compiler cannot tell where it is waited for" where that applies.

## 8. Implementation sketch

### 8.1 Deciding which `go` statements are scoped

The forms of section 4.1 are decided from the typed AST: the WaitGroup must be named by a local path
(identifier, constant field and index steps), the function literal or the statically known callee is
inspected for its `Done` placement, and `wg.Go` is recognized as a method of `sync.WaitGroup`. It
depends on nothing the optimizer does. One helper (`types2` or a small new package used by `types2`,
`lower`, `escape` and `moves`) answers "is this `go` statement scoped, and to which WaitGroups".

The existing syntactic checks skip scoped statements: `types2`'s `checkFrameView` (E2011) in
`plutonium_conc.go`, and `lower`'s E2590 and E2594 in `conc.go`. The whole-program checks then decide.

### 8.2 The interval analysis

A new package, `internal/scope`, computes for each function and literal the scoped `go` statements,
their joins, and at each statement the set of `go` statements that may be running (section 4.2). It
uses the same control-flow walk as `internal/moves`. `escape`, `moves` and the sharing check consume
its result.

### 8.3 Escape analysis (`internal/escape`)

- A scoped `go` statement is not a sink. Each frame token it receives is checked against the interval:
  the location's storage must last until every join of that statement, else E3301 (with taken
  variables, section 4.3, exempted for their own storage but not for what they point to).
- Parameter tokens handed to a scoped `go` statement do not escape, so they add nothing to the
  function's summary.
- Loop iteration storage held by a running puroutine across a back edge is E3301 (the loop check of
  `loops.go`, with "kept by a running puroutine" as the keeper).

### 8.4 Moves (`internal/moves`)

- An owner taken by a scoped puroutine is moved at the `go` statement.
- A scoped literal may move a captured owner (it runs exactly once); that is a move at the `go`
  statement, replacing E3305 for scoped literals.
- Moving, dropping or reassigning a variable a running puroutine uses is E3304.

### 8.5 The sharing check (`internal/share`, new)

- Per-function effect summaries: which parameter paths and frame locations a function reads and
  writes, with the access paths of `internal/escape` (three loads, eight steps), plus whether it calls a
  func-valued parameter from several puroutines at once. Computed to a whole-program fixpoint like the
  escape summaries; a function's effects include those of its own scoped puroutines.
- A lockset pass over each body: `Lock`, `Unlock`, `RLock`, `RUnlock` and deferred unlocks of local
  locks.
- Per interval: the accesses of every party, the four rules of section 4.4, the partition forms, and
  type-based conflict for memory reached through views. E3302, E3303, E3305, E3306.

### 8.6 Lowering and runtime

- `pluto/rt` gains `type Join struct` (the running counter, a waiter, a lock word, and the chain link
  and frame address of section 5), with `enter`, `exit` and `Wait` (marked `//pu:blocking`), and
  `rt.WaitScopes(sp uintptr)`. The task record gains one word, the head of the chain.
- `sync.WaitGroup` embeds a `Join`; `Wait` waits on the join, then on its counter as now. `WaitGroup.Go`
  becomes `rt.GoJoined(&wg.join, f)`, a runtime entry that the compiler recognizes at the call site of
  `wg.Go` and lowers as a scoped `go` statement. Called on a WaitGroup that is not local, it behaves as
  a plain `go` statement, with the run-time closure check.
- A scoped `go` statement lowers as `go` does today (argument block, thunk, `spawnGo`), plus
  `join.enter()` (an atomic increment) before the spawn, and `join.exit()` in the thunk after the call
  (an atomic decrement, and a wake if it reached zero with a waiter, under the join's lock, touching
  nothing afterwards, as `readyAll` does today). The thunk builds the closure record (section 6). No
  run-time closure check.
- Block entry pushes each local join that has scoped `go` statements onto the task's chain; every exit
  path calls the inserted wait (an inline load of the counter, and a call only if it is not zero) and
  pops the chain. This sits with the drop insertion in `lower`, ordered after deferred calls and before
  drops.
- `testing.FailNow` calls `rt.WaitScopes` with the runner's stack pointer before its `longjmp`.

### 8.7 Diagnostics

New codes in `internal/diag/codes.go`, with catalog entries in `catalog_build.go` (`Text`, `Why`,
`Spec`, `Fix`), and programs under `cmd/pluto/testdata/diag/`:

| Code | Name | When |
|---|---|---|
| E3301 | ScopeOutlivesVar | a running scoped puroutine uses storage that ends before its join |
| E3302 | ScopeRace | two parties may access one place at once, one writing, and it is not synchronized or guarded |
| E3303 | ScopeNotPartitioned | indexed writes the compiler cannot prove disjoint |
| E3304 | ScopeMoveInUse | moving, dropping or reassigning a variable a running puroutine uses |
| E3305 | ScopeJoinCopied | copying or assigning a WaitGroup while puroutines it joins may run |
| E3306 | ScopeConcurrentCall | a literal that writes its captures, passed where it is called concurrently |

E2590, E2594, E2011 and E3004 change their fixes: when the function has (or could have) a local
WaitGroup, they suggest `wg.Go`, and E2590's argument suggestion never passes a `sync` type by value.
Every `Why` says the same thing in plain words: Go keeps such variables alive with its garbage collector;
Plutonium has none, so it waits for the puroutines before the frame ends instead.

### 8.8 go2pu

The three forms of section 4.1 translate as the identity. `heapvars.go` currently allocates every
variable that Go's escape analysis moves to the heap; it should leave alone variables whose only escape
is into a `go` statement that is scoped by section 4.1. The Plutonium compiler then checks the result,
and anything it rejects (a race Go tolerated, an unprovable partition) is a note for hand-fixing, as
today.

### 8.9 Costs

| | Plain `go` | Scoped `go` |
|---|---|---|
| start | allocate task and stack, copy the argument block, spawn | the same, plus one atomic increment |
| finish | free task and stack | the same, plus one atomic decrement (and a wake for the last) |
| closure record | not allowed | built by the thunk: one store per captured variable |
| join | none | `Wait` as today; the inserted wait is one load when already joined; two stores for the chain per block |
| compile time | escape check | plus the interval, effect summaries and lockset passes |

The cost of `wg.Go` is that of Go's `WaitGroup.Go`. Stacks are the same as for any puroutine: 64 KiB by
default, or what the planned stack analysis (roadmap item 18) computes for the thunk and the literal.

### 8.10 Tests

- `internal/build/testdata/run`: the forms of section 7, which are in the common subset of Go and
  Plutonium, so their expected output comes from Go (`-update`). Each runs at -O0 and -O2.
- `cmd/pluto/testdata/diag`: one program per new code and per changed message.
- Unit tests for the interval analysis (conditional waits, waits in loops, `defer wg.Wait()`, `goto`),
  the lockset pass, and each partition form, including the overflow checks.
- A kernel test under `testdata/kernel/sched` with a scoped puroutine on the test kernel's scheduler.
- A `pluto/testing` test that calls `t.Fatal` while scoped puroutines run.

## 9. What stays as is

A `go` statement that is not scoped keeps every rule of spec section 4.7: arguments are copied and must
be sendable, views of the frame and capturing literals are rejected, and a func value whose record is
in the stack panics when the statement runs. Its cost does not change. Fire-and-forget puroutines,
servers that start a puroutine per connection, `time.AfterFunc`, and latches built from a WaitGroup are
all unscoped.

## 10. Phases

**Phase 1.** Everything in sections 4 to 8 for WaitGroups named by a local path: the three forms,
inserted waits, taken variables, closures built by the thunk, the sharing rules, the chain and
`FailNow`, the diagnostics, and spec changes (a new section 4.11, and edits to 4.5, 4.7, 6.3, 6.9 and
13; memory-model.md 3.2 and 3.4: the return of a scoped puroutine is synchronized before the return of
the join that waits for it; abi.md section 12: the task word and the stack rule).

**Phase 2.** Summaries in place of local names, still without annotations:

- "spawns `f` joined by parameter path `p`" and "waits on `p`", so that a helper or a wrapper
  (`errgroup.Group.Go`, which embeds a WaitGroup) can start or join puroutines for a WaitGroup its
  caller owns;
- points-to resolution of the WaitGroup, so that `p := &wg` and `wg := &sync.WaitGroup{}` (a frame
  literal) count as local;
- locks taken by callees on parameter paths;
- frame views sent on a local channel whose senders and receivers are all in the scope.

The same lock inference could replace the `//pu:guardedby` annotation the memory safety design proposes
for `synced[T]` (its decision D7): each field's accesses across the program are known, so the lock
that guards them can be inferred the same way. That is a separate decision.

## 11. Summary

A puroutine started by `wg.Go`, or by the classic `go` with `defer wg.Done()`, on a WaitGroup that is a
local variable, is joined by that WaitGroup, through a counter that only the runtime changes. The
compiler waits for it at the end of the WaitGroup's block on every path, lets it use the frame and
capture variables, takes the variables that would end first, and proves that the puroutines and the
starting function do not race: shared places are read-only, synchronized by type, guarded by a lock it
infers, or partitioned by the loop variable. Go source for the idiom compiles unchanged.

## 12. Decisions for sign-off

| # | Decision | Options | Recommendation |
|---|---|---|---|
| D1 | Surface | (a) a new `sync.Group`; (b') `sync.WaitGroup` joins what it is handed; (c) a scoped block | (b'): Go source compiles unchanged and go2pu stays the identity |
| D2 | Classic form | (a) scoped when `Done` is at the puroutine's return (first-statement `defer` or last statement), as well as `wg.Go`; (b) only `wg.Go`, go2pu rewrites the rest | (a): the most common Go code compiles as written, and Go's behavior is unchanged |
| D3 | Join at the end of the block | (a) the compiler inserts a wait on every path; (b) a compile error unless every path waits | (a): it is what drops already do, makes early returns safe without edits, and a hang there shows in the deadlock trace as a WaitGroup wait |
| D4 | Shared writes | (a) partitions by loop variable and split point, inferred locks, atomics; (b) partitions only through a library API; (c) atomics and locks only; (d) run-time checks | (a), with the forms of section 4.4; extend the table only with forms whose disjointness follows from the slice expressions |
| D5 | Lock inference | (a) infer the guarding lock for scoped code; (b) require `//pu:guardedby` | (a); whether to extend it program-wide in place of the memory safety design's D7 is a separate decision |
| D6 | Taken variables | (a) copy or move captured variables whose block ends before the join, as Go 1.22's per-iteration variables; (b) require them as arguments | (a): it is Go's meaning for the same source |
| D7 | Non-local exits | (a) a per-task chain of joins and `rt.WaitScopes`, used by `FailNow` and kernels; (b) forbid `FailNow` while scoped puroutines may run | (a): two stores per block that needs it, and it gives kernels the rule they need too |
| D8 | Heap views across unscoped puroutines | memory safety design D5 | adopt its (b): only then is the scope's race check complete against puroutines outside it |
