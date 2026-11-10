# Concurrency

Tasks run in parallel on all processors and are preempted. They communicate over channels and share memory only
under rules the compiler checks: **safe code has no data races**. Spec chapter 19; the memory model is
`docs/spec/memory-model.md`.

## 1. Starting tasks (sections 12.12, 19.1)

`task f(x, y)` starts a task running the call. `go` is an error that names `task`.

- Arguments (and a method receiver) are evaluated now and **copied into the task**: values are copied, owners
  **move**, a `synced[T]` or string gets a reference of its own. Every argument must be **sendable** (section 8.8):
  a type holding a `shared[T]` is not (E2010, use `synced[T]`).
- A view of the starting function's frame cannot be passed (E3004; E2011 where the type checker sees it at once):
  `task work(&wg)` with a local `wg` is the classic case. Use a package-level variable, allocate with `new`
  (the box moves in), or use `sync.Scope`.
- A function literal that captures variables cannot be started: `task func() { use(n) }()` is E2590; pass values as
  arguments: `task func(n int) { use(n) }(n)`. A method value `x.M` is E2594. A built-in call is E2591.
- `task p.Serve()` on an `owned[T]` box moves the box into the task; on a `synced[T]` it gives the task its own
  reference.
- No task may be started during package initialization (an initializer or `init`, directly or through calls): E2587.
- A task has a fixed stack: computed when the call graph is bounded, else 64 MiB of address space in hosted builds
  and `rt.DefaultStackSize` (64 KiB) in freestanding and kernel builds. Overflow is a panic with a trace in every
  build mode. `rt.Go(f, size)` starts one with an explicit size, at least `rt.MinStackSize` (8 KiB).
- A task ends when its function returns; `rt.ExitTask()` ends it early after running deferred calls and drops.
  A **panic in any task ends the program**. When `main` returns the program exits (after a grace of about a
  millisecond for tasks that are finishing), even with tasks running: join them (channel, `sync.WaitGroup` or
  `sync.Scope`) if their work matters. Only a `sync.Scope` join guarantees that package cleanup and the leak report run.
- Every task starts with the program-wide default allocator, not its starter's `alloc.Use` override.

```pluto-run
package main

import "pluto/time"

func spin() {
	time.Sleep(time.Hour)
	println("never printed")
}

func main() {
	task spin()
	println("main returns, and the program ends without waiting for spin")
}
```

## 2. Channels and select (sections 7.11, 12.5, 12.13, 19.8)

`make(chan T)` is unbuffered, `make(chan T, n)` buffered; `make(chan T, n, a)` takes an allocator. Directions
`chan<- T` and `<-chan T` convert implicitly. A channel is a counted reference: copies share it, and the last
reference frees it and drops values still buffered.

- `v := <-ch`, `v, ok := <-ch`, `ch <- v`, `close(ch)`, `for v := range ch`, `len(ch)`, `cap(ch)` as in Go.
  Sending on a closed channel and closing twice panic. A nil channel blocks forever.
- Sending an **owner moves it**; the receiver owns it. In a `select`, a send case moves only if it proceeds.
- The element type must be sendable. A `chan` of a type with `shared[T]` is E2010.
- `select` picks uniformly among ready cases; `default` makes it non-blocking. Timeouts are channel cases:
  `case <-time.After(d)`. `time.NewTimer` returns an `owned[Timer]` that stops when dropped; `time.AfterFunc`
  returns a `synced[Timer]` and keeps running when dropped.
- Deadlock (every task waiting, none on I/O or a timer) ends the program with status 2 and a report of what each
  task waits for.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/time"
)

type Job struct {
	id   int
	name string
}

func produce(out chan<- owned[Job]) {
	for i := 0; i < 3; i++ {
		out <- new(Job{id: i, name: "job"}) // the box moves to the receiver
	}
	close(out)
}

func gen(n int) <-chan int {
	ch := make(chan int)
	task func(ch chan<- int, n int) { // pass values as arguments
		for i := 0; i < n; i++ {
			ch <- i
		}
		close(ch)
	}(ch, n)
	return ch
}

func slow(out chan<- int) {
	time.Sleep(200 * time.Millisecond)
	out <- 1
}

func main() {
	jobs := make(chan owned[Job])
	task produce(jobs)
	for j := range jobs {
		fmt.Println(j.id, j.name)
	}
	for v := range gen(3) {
		fmt.Print(v, " ")
	}
	fmt.Println()

	res := make(chan int)
	task slow(res)
	select {
	case v := <-res:
		fmt.Println("got", v)
	case <-time.After(20 * time.Millisecond):
		fmt.Println("timeout")
	}
	done := make(chan struct{})
	close(done)
	select {
	case <-done:
		fmt.Println("done closed")
	default:
		fmt.Println("not closed")
	}
}
```

```output
0 job
1 job
2 job
0 1 2 
timeout
done closed
```

## 3. `sync.Scope`: tasks that use the caller's variables (section 19.4)

A `sync.Scope` (zero value ready) starts tasks with `s.Task(func() {...})` that may use **any variable of the
function**, and joins them when `s.Wait()` returns or the scope ends (drop). Scopes drop before the function's
other variables, so the variables outlive the tasks. Rules the compiler enforces:

- A variable declared in a nested block (a loop body, or the loop variable itself) is **taken**: the task gets its
  own copy (owners move). Each task sees its iteration's value. A taken variable the task changes and the starter
  reads again is E3407; a view of storage that ends before the join (a slice of a loop-body array) is E3401.
- During the tasks' lifetime every place that is written must be **synchronized by its type** (`sync`, `atomic`
  values, channels), **guarded by one lock** that every access holds, or **partitioned**: each task writes its
  own elements. Anything else is E3402; writes the compiler cannot prove disjoint are E3403.
- Partitions recognised: `x[i]` where `i` is the loop variable (`for i := range ...` or `for i := a; c; i++`),
  `x[i*k : i*k+k]` windows, `x[i : i+c]` with step `c`, and `x[:m]` against `x[m:]`. `grid[y][x]` on a
  `[][]T` is not provable (E3403): use a flat slice or a `[H][W]T` array. A `vec.Vec` element is not
  partitioned; use `v.Slice()` outside the tasks, or a lock.
- Two slices of one type may overlap, so `out[i] = f(in[i])` is checked when each task starts (it panics if the
  slices overlap without starting together); `in[i+1]` stays E3402.
- While tasks run, the starter must not move, drop or reassign a variable they use (E3404) nor move the
  scope (E3405). A scope reached through a pointer or field still waits but its tasks may not use the frame.
  Dropping, moving or replacing an owner is a write of everything it holds.
- A deferred call that drops or writes a variable a scoped task uses counts as running while the task runs (E3404,
  E3402). Arm it after `s.Wait()`, or arm `defer s.Wait()` after it and before the first `s.Task`.
- One task waits on a Scope at a time: a second waiter panics with "sync: two tasks wait on one Scope".
- A `task` statement never starts a scoped task; `sync.WaitGroup` tasks are not scoped.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/sync"
)

func main() {
	// Partitioned writes: each task owns out[i]. No lock.
	in := [6]int{1, 2, 3, 4, 5, 6}
	var out [6]int
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] * in[i] })
	}
	s.Wait()
	fmt.Println(out)

	// Windows: task i owns data[2i : 2i+2].
	data := [8]int{1, 2, 3, 4, 5, 6, 7, 8}
	var parts [4]int
	var s2 sync.Scope
	for i := 0; i < 4; i++ {
		s2.Task(func() {
			w := data[i*2 : i*2+2]
			parts[i] = w[0] + w[1]
		})
	}
	s2.Wait()
	fmt.Println(parts)

	// A worker pool: a local channel, one lock for the shared total.
	jobs := make(chan int, 16)
	var mu sync.Mutex
	total := 0
	var s3 sync.Scope
	for w := 0; w < 4; w++ {
		s3.Task(func() {
			for j := range jobs {
				mu.Lock()
				total += j * j
				mu.Unlock()
			}
		})
	}
	for i := 1; i <= 10; i++ {
		jobs <- i
	}
	close(jobs)
	s3.Wait() // before reading total
	fmt.Println(total)
}
```

```output
[1 4 9 16 25 36]
[3 7 11 15]
385
```

```pluto-error E3402
package main

import "pluto/sync"

func main() {
	total := 0
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() { total += i }) // no lock: every task writes total
	}
	s.Wait()
	_ = total
}
```

```pluto-error E3404
package main

import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	v := vec.New[int]()
	var s sync.Scope
	s.Task(func() { v.Push(1) })
	w := v // moves v while the task may still use it
	_ = w
	s.Wait()
}
```

```pluto-error E3407
package main

import "pluto/sync"

func main() {
	var s sync.Scope
	for i := 0; i < 3; i++ {
		s.Task(func() { i++ }) // the task has its own copy of i
	}
	s.Wait()
}
```

```pluto-error E3401
package main

import "pluto/sync"

func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		var buf [4]int
		sl := buf[:]
		s.Task(func() { sl[0] = i }) // buf is reused by the next iteration
	}
	s.Wait()
}
```

## 4. Locks and atomics (sections 19.3, F.5)

- `sync.Mutex`, `sync.RWMutex` (`RLock` for readers), `sync.Once`, `sync.Cond` (a value; `c.Wait(&mu)`),
  `sync.WaitGroup` (`Add`, `Done`, `Wait`, `Task`), `pluto/sync/atomic` (`atomic.Int64`, `atomic.Bool`, and the
  `atomic.AddInt64(&x, 1)` functions). A local `sync.WaitGroup` cannot be passed to a `task` statement by address
  (E3004): make it a package variable or a field of a `synced` object, or use `sync.Scope`. A `WaitGroup` or
  `Mutex` declared in a function **may** be used by that function's `Scope` tasks, which share its frame.
- The compiler **infers which lock guards which data**: the **guard set** of a datum is the locks held at
  every write (in write mode); every read by a task other than the only writer must hold one of them, and the
  only writer (a task not repeated) may also keep a view of the data with no lock. It
  follows `Lock`/`Unlock`, `defer mu.Unlock()`, locks held around calls, and `s.mu` named through the
  pointer it came from, and through `p := &mu`. A lock counts as held only if held on every path. A call
  through an interface, a func value or a method value runs with the caller's locks. A guard must sit beside the data
  (same object, no pointer load between) or be package-level. `wg.Wait()` and channel operations do
  **not** order plain data for the checker: reading shared data after `wg.Wait()` still needs its lock.
  The join of a `sync.Scope` and the return of `once.Do` do.
- **Sharing starts** at the first statement that may start a task, install an interrupt handler, or
  enable interrupts (`x86.Sti`). Before it, nothing counts.
- **Range bodies.** A range-over-func body shares the function's locks only when the iterator calls
  `yield` on the same task (`v.All()` does). The body also holds the locks the iterator holds at every
  `yield`, so a locked iterator (`mu.Lock(); defer mu.Unlock(); for ... { if !yield(p) { return } }`)
  works, and the yielded pointer is valid in the body. An iterator that calls `yield` from `s.Task` runs
  the body on that task: it holds nothing there, and `defer` in it is a compile error.
- **Release only a lock you hold** (19.3, Releasing a lock; D108). Every `Unlock`, `RUnlock` and `c.Wait(&mu)`
  must release a lock the calling task holds on every path, in the same mode (`RUnlock` after `RLock`).
  Otherwise it is a compile error. This covers `rt.Lock` too. A task, a handler, and a function given to
  `s.Task` or `wg.Task` start with nothing held, so handing a locked mutex to another task to unlock is an
  error: send on a channel, use a buffered channel as a semaphore, or use `sync.Scope`. The lock must have a
  name: a package variable, or a local or parameter whose address is not taken, then fields and constant
  indexes. `locks[i].Unlock()` and `g.p.mu.Unlock()` (a pointer loaded from memory) are errors; copy the
  pointer into a local first (`l := &locks[i]; l.Lock(); ...; l.Unlock()`). A `sync.Locker` interface, a
  stored method value (`u := mu.Unlock`) and a func value cannot release a lock. A helper that locks or
  unlocks its parameter or receiver works when every call matches (`s.lock(); defer s.unlock()`).
  `defer mu.Unlock()` needs `mu` held at every point after the `defer`. Not checked yet: item 220.
- **Rule 1, shared data** (package variables, fields of static and `synced` objects, map contents, each with
  the memory it owns: an `owned[T]` it holds, the storage of a `vec.Vec` or `hash.Map` in it). Data that two
  tasks use, one writing, must be unchanged after sharing starts, initialized by a Once, atomic, or guarded:
  a write with no lock common to every write is E3502 (it names the write), and a read without one of those
  locks is E3503. A package variable that holds a `shared[T]` is for one task only (E2010).
- **Rule 2, what may cross** (a `*T` or `[]T` passed to a task or sent, or **loaded by a task** from a package
  variable, a per-CPU variable, a `synced` object or a map, also inside an interface or func value, an atomic
  load included). So `p = &*b; task reader(); b = new(Node)` is E3501 when `reader` reads `p`: the drop frees
  what `p` points to. It must point to: (1) an owner moved to the task; (2) a package variable or static-object
  field nobody writes after sharing starts; (3) synchronized memory (`sync`/`atomic` values, channels, a struct
  of only those), or a **static object** (`var Stdout = &File{...}` in a package initializer, never moved or
  overwritten; dropping some other `File` does not count, so `fmt.Println` from any task is fine), a `synced`
  object, or an object whose type passes rule 3, each of whose fields is unchanged, atomic, guarded by a lock of
  the same object or a package-level one, or set by a Once of the same object; or (4) **guarded memory, used
  through a view that is still valid**: loaded while a guard was held, and that guard held to the use with no
  `Unlock`, `Cond.Wait` or call that may release it between (a package-level `vec.Vec` under one mutex is fine;
  a pointer from `items.Ptr(i)` kept past `Unlock`, across a `Cond.Wait`, an `Unlock`/`Lock` pair or a helper
  that waits, returned from a function that unlocks, stored, sent or captured is E3501). Otherwise E3501 names
  a write that could race.
- **Rule 3, the type rule.** An object of a struct type may be shared when no code frees, drops, moves or
  overwrites one after sharing starts, and each field is unchanged, atomic, guarded by a lock of **the same
  object**, or set by a Once of the same object. A lock in the container that holds the `synced` references,
  such as the `Bank` that owns a map of accounts, does not guard the accounts (E3502). Give the record its own
  mutex, take the container's lock only to find the record, and lock the record to change it. A map **kept in
  place** in a field or package variable (only ever set to `make`, a map literal or `nil` there, and only
  indexed, ranged over, passed to `len`, `delete`, `clear`, compared with `nil`, or passed to a parameter that
  does only those) belongs to that field: `c.mu` guards `c.m[k]` for each object, so a struct with a mutex and
  a map works with many instances. A map copied out anywhere else (returned, assigned, sent, put in an
  interface) joins the one datum of its map type, which only a package-level lock can guard. A map used by one
  task at a time needs none.
- A function started by `time.AfterFunc` is a repeated party: `Reset` can run it again while a run is going,
  so what it writes needs a lock or an atomic even if nothing else touches it.
- `rt.Yield` inside a non-migrating region (`rt.DisablePreemption`, a held `rt.Lock`, `x86.Cli`) is a compile
  error, or panics where the call is not followed; the task could resume on another processor.
- Moving, freeing or overwriting memory that holds a `sync` value or channel another task may use is an error,
  whatever lock the write holds (E3502 by name, E3501 through a pointer).
- **Do not copy locks.** Copying a `sync.Mutex`, `RWMutex`, `Once`, `WaitGroup` or `Cond`, or a struct that
  holds one, is a compile error (Go only has `go vet` warn); that includes a value receiver. Use pointers.
- **Lazy initialization with `sync.Once`.** Data written only inside functions passed to `once.Do`, and read only
  where `once.Do` has returned on every path in the reading function (directly, or in a helper that calls it on
  every path, at any depth), needs no lock: `once.Do(func() { cfg =
  new(Config{...}) }); return cfg.port` compiles. A package-level Once covers package variables; a Once field
  covers the other fields of the same object (`t.once.Do(...)` then `t.index`). A write anywhere else, even in
  a reset function reached through a func value, is E3502; a read with no `Do` before it, such as in a task
  that `main` started after its own `Do`, is E3503. Do not reassign the Once. A function passed to
  `Do` that ends its task with `rt.ExitTask` leaves the Once not done; the next `Do` runs its function.
- **`sync.Cond`.** `var c sync.Cond` (zero value ready, no `NewCond`, nothing to free). Wait with the lock:
  `mu.Lock(); for q.Len() == 0 { c.Wait(&mu) }; j, _ := q.Pop(); mu.Unlock()`. The compiler counts
  `c.Wait(&mu)` as `mu.Unlock()` then `mu.Lock()`, so the loop keeps its guard. A pointer loaded before the
  `Wait` and used after it is E3501. `mu` must be held for writing at the `Wait`. Pass the lock as `&mu` or
  `&s.mu`: a `Locker` in a variable, `rw.RLocker()`, or a `Wait` through an interface is a compile error,
  since the compiler cannot name the lock (D108). `Signal` and `Broadcast` need no lock. (Today's library
  still has `NewCond` and `c.Wait()`; see known-gaps, items 197, 219 and 220.)
- `shared[T]` is for one task and never crosses; `synced[T]` counts atomically. A `string`, map or channel count
  becomes atomic as soon as the first task starts.

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/sync"
	"pluto/sync/atomic"
)

type Counter struct {
	mu sync.Mutex
	n  int
	wg sync.WaitGroup
}

func (c *Counter) Add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	c.wg.Done()
}

func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

var (
	hits atomic.Int64
	wg   sync.WaitGroup
)

func hit() {
	hits.Add(1)
	wg.Done()
}

var once sync.Once

func main() {
	c := alloc.Synced[Counter]() // a counted object tasks may share
	for i := 0; i < 5; i++ {
		c.wg.Add(1)
		task c.Add() // the task gets its own reference
	}
	c.wg.Wait()
	fmt.Println(c.Get()) // reads under the lock, as the rules require

	for i := 0; i < 10; i++ {
		wg.Add(1)
		task hit()
	}
	wg.Wait()
	fmt.Println(hits.Load())

	once.Do(func() { fmt.Println("setup once") })
	once.Do(func() { fmt.Println("setup once") })
}
```

```output
5
10
setup once
```

```pluto-error E3502
package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Account struct{ balance int }

type Bank struct {
	mu   sync.Mutex
	accs map[string]synced[Account]
}

func (b *Bank) Deposit(id string, n int) {
	b.mu.Lock()
	a := b.accs[id]
	a.balance += n // the bank's lock is not a lock of the account: give Account its own mutex
	b.mu.Unlock()
}

func main() {
	b := Bank{accs: map[string]synced[Account]{}}
	b.accs["x"] = alloc.Synced[Account]()
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() { b.Deposit("x", 1) })
	}
	s.Wait()
}
```

```pluto-error E2011
package main

import "pluto/sync"

func work(wg *sync.WaitGroup) { wg.Done() }

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	task work(&wg) // a view of main's frame given to a task
	wg.Wait()
}
```

```pluto-error E2590
package main

func main() {
	n := 1
	task func() { println(n) }() // pass n as an argument
}
```

```pluto-error E2010
package main

import "pluto/alloc"

type Item struct{ n int }

func use(s shared[Item]) {}

func main() {
	s := alloc.Shared[Item]()
	task use(s) // shared counts without atomics; use alloc.Synced
}
```

```pluto-error E3502
package main

var hits int

func bump(done chan bool) { hits++; done <- true } // no lock at the write

func main() {
	done := make(chan bool)
	task bump(done)
	task bump(done)
	<-done
	<-done
}
```

```pluto-error E3503
package main

import "pluto/sync"

var (
	mu    sync.Mutex
	count int
)

func add(done chan bool) {
	mu.Lock()
	count++
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task add(done)
	<-done
	println(count) // read without mu
}
```

```pluto-error E3501
package main

type Counter struct{ n int }

var counter Counter

func bump(c *Counter, done chan bool) {
	c.n++ // changed with no lock
	done <- true
}

func main() {
	done := make(chan bool)
	task bump(&counter, done)
	<-done
}
```

```pluto-error E2587
package main

func work() {}

var started = start()

func start() int {
	task work() // package initialization starts no task
	return 1
}

func main() {}
```

## 5. Known limits (today's compiler)

- `sync.Scope` through a pointer, helpers that start scoped tasks, `vec.Vec` element partitions and same-type slice
  partitions are not provable yet; use a lock or restructure.
- A `vec.Vec`, `hash.Map` or `strings.Builder` crosses with the memory it owns, also in a program that imports
  `pluto/fmt`. If it allocates from an allocator that has no lock of its own (an arena), the allocator is shared
  with the sender and E3501 names it.
- The race check groups built-in maps by type: if one map of a type crosses tasks, every map of that type needs
  the guard. Maps kept in place are not grouped in the spec; the compiler does not do that yet (item 192).

## 6. Interrupt context (kernel code)

Nothing an interrupt handler or the panic handler can reach may wait: no channel send or receive except in a
`select` with `default`, no `Lock` of a `sync.Mutex` (use `rt.Lock`), no `time.Sleep`, `s.Wait`, task start or
allocator that waits (E2584, E2585). Data a handler touches while other code may write it, or writes while other
code may touch it, must be atomic or guarded by an `rt.Lock`, unless it is a per-CPU variable; data nobody writes
while a handler may run needs neither. The same waits are a compile error inside a non-migrating region (`rt.DisablePreemption`, a held `rt.Lock`, interrupts off) when reached through calls with one known target (direct calls, methods of a concrete type, deferred calls, a literal called where written), even on only some paths of the callee. A wait behind an interface, a func value, a hook or an allocator compiles and panics with "blocking with preemption disabled" when it would park; so does a call of a `//pu:blocking` function there, which tests at entry. In kernel mode the test also panics with interrupts disabled (section 19.3). A plain spin loop is not a wait and is not caught. See `kernel-mode.md`.
