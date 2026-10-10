## 19. Concurrency

A **task** is an independent thread of execution with its own stack. Tasks run in parallel on the
available processors and are preempted, so a task that never blocks cannot keep the others from running.
They communicate through channels, share memory under the race rules of this chapter, and synchronize
with `pluto/sync` and `pluto/sync/atomic`.

The language provides the mechanism: the `task` statement (section 12.12, Task statements), channels
(sections 7.11, Channel types, and 19.8), `select` (section 12.13, Select statements), and the runtime
operations that start, park and wake tasks. Which task runs where and when is the policy of a
**scheduler backend**: the hosted runtime provides one, and a kernel provides its own (section 19.5). A
program that never starts a task, never waits on a channel or a lock, and uses no timer contains no
scheduler at all.

Safe code has no data races. Section 19.2 says what may cross from one task to another, section 19.3
how memory that several tasks reach is checked, and section 19.4 how tasks that are joined before a
function returns may use that function's variables. Section 19.7 summarizes the memory model, which
[memory-model.md](../memory-model.md) defines.

### 19.1 Tasks

**Starting a task.** The statement `task f(x, y)` (section 12.12) starts a new task that runs the call
`f(x, y)`. The function value and the arguments are evaluated in the starting task, as for an ordinary
call, and copied into the new task before the statement completes. The starting task then goes on
without waiting. When `f` returns, the new task ends; its results, if any, are discarded, which drops
those that hold owners or counted values.

- An argument of a value type is copied. An argument of a counted type gives the new task a reference of
  its own, taken before the task starts.
- An argument of an owner type is moved into the new task (section 18.5, Moves). The new task drops it
  when `f` returns, unless `f` moves it on.
- The array of a variadic call's packed arguments is copied into the new task, so each element crosses
  as a value.
- Every argument, and the receiver of a method, must be sendable (section 19.2). A view of the starting
  function's frame cannot be passed at all (E3004, or E2011 where the type checker sees it directly),
  since the task may outlive the frame.

**The function started.** A function value whose closure record lies in the starting task's frame
cannot be started, since the frame may be gone before the task runs:

- a function literal that captures variables, written in the task statement, is error E2590;
- a method value, or a local variable whose only assignment is its declaration from a capturing literal
  or method value (directly or through other such variables), started or passed as a func-typed
  argument, is error E2594;
- any other func value whose record lies in the starting task's stack (one that reached the task
  statement through a parameter, a field, or a variable assigned more than once) makes the task statement
  panic when it runs.

Pass the values the function needs as arguments instead: `task func(n int) { work(n) }(n)`. A function
literal that captures nothing is an ordinary function value and may be started. A task that uses its
starting function's variables is started by a `sync.Scope` instead (section 19.4).

Functions that start a task with one of their func arguments are checked in the same way at each call:
`rt.Go`, `(*sync.WaitGroup).Task`, `time.AfterFunc`, and any function of the program that passes a func
parameter on to a task statement or to one of these. The compiler finds these functions itself by
following each func parameter through the function's body; nothing marks them.

**A method of a box.** `task p.Serve()`, where `Serve` has a pointer receiver and `p` is an `owned[T]`
or a `synced[T]`, hands the box itself to the new task, so the object lives as long as the task needs it:

- An `owned[T]` moves into the task, as an owner argument does: `p` holds nothing afterwards, and the
  task drops the box when `Serve` returns. The receiver must be a whole variable or a temporary, since an
  owner moves only out of a whole variable (section 18.5).
- A `synced[T]` is copied: the task holds a reference of its own, taken before it starts and released
  when the method returns. A field or element may be the receiver.
- A `shared[T]` is not sendable (E2010).

The method must be declared on `T`, not promoted from a field, and the box type must not be a named
type. Any other pointer receiver is a view into an object, and crosses under the rules of section 19.3.
Starting a method value (`task f()` with `f := p.Serve`) is error E2594.

**How a task starts.** A new task begins with:

- its own stack (below), on which its frames live;
- the program-wide default allocator as its current allocator, whatever override the starting task
  installed with `alloc.Use` (section 18.9, Allocators);
- preemption enabled, and, in kernel builds, interrupts enabled whatever the state of the starting task.
  A kernel installs its interrupt table before it starts its first task.

Starting a task is a synchronizing operation: everything the starting task did before the task statement
happens before the new task's first operation (section 19.7).

**How a task ends.** A task ends when its function returns, after the function's deferred calls have run
and its variables and parameters have been dropped. `rt.Goexit` ends a task early in the same way, from
any depth (section 17.3, Ending a task with `rt.Goexit`). A panic in any task ends the program: in hosted
builds the panicking task first runs its pending deferred calls, while the other tasks keep running
(section 17.2, Deferred calls on a panic). The end of a task synchronizes with nothing, except the join of
a task started by a `sync.Scope` (section 19.4); a program that waits for a task uses a channel, a
`sync.WaitGroup` or a `sync.Scope`.

**Stacks.** Each task has a stack of fixed size, which never grows or moves: nothing in the program
knows where the pointers into it are, so it cannot be moved. The size is chosen when the task starts:

1. When the call graph of the function started is bounded (no recursion, and no call through a func
   value or interface whose possible callees are not themselves bounded), the compiler computes the
   stack the task needs, locals included, and the task gets exactly that.
2. `rt.Go(f, size)` starts `f` with a stack of `size` bytes, at least `rt.MinStackSize` (8 KiB).
3. Otherwise, in hosted builds, the task gets at least 64 MiB of address space, with a guard region
   below it, whose memory is committed only as it is touched. In freestanding and kernel builds it gets
   the default task stack size that the implementation documents (section 21.4, Implementation limits).
   `-stackreport` lists the tasks whose size could not be computed.

Running past the end of a stack with a guard region is a fault, reported as a stack overflow panic with
the task's trace and the size it had, and how to ask for more (section 17.5, Faults). In kernel builds
the scheduler backend provides the guard (section 19.5). `rt.StackLeft()` returns the bytes left between
the caller's frame and the limit of its stack, for code that recurses deeply.

> **Open.** Which allocator provides a task's stack when the starting task has installed an override
> with `alloc.Use`. The text this chapter replaces took it from the starting task's current allocator,
> which can be a local arena that is reset or dropped while the task still runs; G35 starts every task
> with the program-wide default allocator, and the same reasoning suggests the program-wide default (or
> the allocator given to `rt.Go`) for the stack. Not decided.

**Parallelism and preemption.** Tasks run in parallel on the processors the backend uses. A task that
runs for a full time slice (10 ms in hosted builds) without blocking is suspended wherever it is, even in
a loop that makes no calls, and another runs in its place. Every instruction is a place where a task may
be suspended, since there is no garbage collector to find pointers; the full register state is saved,
and preemption cannot be observed by the program (section 19.7). Code that must not be interrupted
(runtime and allocator locks, `rt.DisablePreemption`) disables preemption for its own duration, and a
preemption that arrives meanwhile happens when it is enabled again. Blocking on a channel, a lock or I/O,
or calling `rt.Yield()`, gives up the processor early.

**Deadlock.** In hosted builds, when every task waits for another task and none waits for I/O, a system
call or an armed timer, the program stops with a fatal error that reports a deadlock, lists each task
with what it waits for (a channel operation, a `select`, a lock, a `sync.Scope`) and its stack trace,
and exits with status 2 (debugging.md section 5).

**Program exit.** When `main` returns, it does so as any function does: its deferred calls run and its
variables are dropped, and a `sync.Scope` among them waits for its tasks then (section 19.4). Then the
program exits, with the status `main` returned (section 15.4, Program execution). The return of `main`
waits for no other task: tasks still running are stopped where they are, and their deferred calls and
drops do not run. Package-level owners are never dropped (section 18.6, Drops). `os.Exit` ends the
program at once, from any task, running nothing. If the task that runs `main` calls `rt.Goexit`, `main`
does not return: the other tasks go on, and once none is left, or all wait for good, the program stops
with a fatal error and status 2.

### 19.2 The sendable rule

A value **crosses** to another task when it is

- an argument or the receiver of a task statement (section 19.1), or a func value given to a function
  that starts a task with it;
- sent on a channel, or received from one;
- held inside an interface, struct or array that crosses;
- a variable that a scoped task takes (section 19.4).

Every value that crosses must be **sendable** (section 8.8, Sendable types). A type is sendable unless it
contains, directly or in any field, element, payload or box referent:

- a `shared[T]` or a `weak[shared[T]]`, whose counts are not atomic: error E2010 at the task statement,
  at the channel type whose element is not sendable, or at the send;
- a view of the starting or sending function's frame: E3004 for a task statement and E3005 for a send
  (E2011 where the type checker sees it directly);
- a view of heap or static memory that may change while both tasks can use it: E3501 (section 19.3).

Values, strings, owners whose contents are sendable, `synced[T]` and `weak[synced[T]]` with a sendable
`T`, channels, maps, and function values that capture nothing are sendable. Moving an owner to another
task is safe because the sender loses it: only one task can use it afterwards, and a library owner that
manages memory (`vec.Vec`, `hash.Map`, `strings.Builder`) takes that memory with it. A `synced[T]` and a
map let several tasks reach the same memory, so what changes in it is subject to section 19.3. Strings
are sendable because their bytes never change and their counts are atomic once a program has several
tasks.

The type part of the rule (`shared[T]`) is checked by the type checker on the arguments and receiver of
every task statement and on the element type of every channel type, looking through struct fields, array
elements, union payloads, the referents of `owned` and `synced`, and the transparent wrappers `ring`,
`volatile` and `aligned`. The view part is checked by the frame rules (section 18.3, Views) and the race
rules (section 19.3) where values cross, and, through interface values, for every type the program
converts to that interface. In a generic function the rule is checked for each instantiation.

### 19.3 Data race freedom

A **data race** is two accesses to the same memory by different tasks, at least one a write and at least
one not atomic, that are not ordered by the memory model (memory-model.md section 2). Safe code has none:
the compiler rejects every program in which two tasks could make such accesses. It checks the whole
program when it compiles, with no annotations, by three rules, all inferred from the program. Every
piece of memory that tasks share is then owned by one task (moved there), never changed while shared,
used only atomically, or guarded by a lock that every access holds.

**The tasks.** The compiler finds every task the program may start: each task statement, each func value
passed to a function that starts a task with it (section 19.1), and each `s.Task` call (section 19.4).
The **main task** runs package initialization and `main`. A task statement that may run more than once
(in a loop, or in a function that may be called more than once) may start several tasks that run the
same code at once. Package initialization, and the part of `main` before the first statement that may
start a task, run while no other task exists.

**Accesses and locks.** For each task the compiler collects what it reads and writes, in its own code and
in every function it calls (a call through an interface or func value reaches every function that could
be called there), and which locks are held at each access:

- A lock is a `sync.Mutex` or `sync.RWMutex`. It is held from `Lock` (or `RLock`, or a `TryLock` that
  succeeded, in the branch where it did) to `Unlock`, or to the end of the function after
  `defer mu.Unlock()`.
- A lock counts as held at a statement only if it is held on every path to that statement.
- A function that returns with a lock held holds it for its caller, and a lock held around a call counts
  as held inside the callee.
- A lock reached through a pointer is named by where the pointer came from, so `s.mu` and `s.items` in
  one method are the lock and the data of the same object.
- Unlocking a lock the compiler cannot name counts as unlocking every lock.
- A method value may be called later, anywhere, so what its method does counts as done with no lock
  held.
- The order that channel operations, `sync.WaitGroup.Wait` and the end of a task impose is not used:
  reading shared data after `wg.Wait()` still holds the data's lock. The join of a `sync.Scope` is used,
  for the frame data it covers (section 19.4).

**Guards.** For data that several tasks may use, the compiler infers its **guard**: a lock that every
write of the data holds for writing (`Lock`, not `RLock`). The data is safe when every access, read or
write, holds its guard; a read may hold it with `RLock`. Data that every access reads or changes with
`pluto/sync/atomic` operations (the methods of `atomic.Int64` and the other atomic types, or functions
such as `atomic.AddInt64(&x, 1)`) needs no lock. A field of a struct is one piece of data, and all the
elements of an array or slice together are one piece; writing a whole struct writes all its fields.
Values of `pluto/sync` and `pluto/sync/atomic` types synchronize themselves through their methods, and
assigning a whole new value to one is an ordinary write.

**Rule 1: package variables.** A package variable, or a field or element of one, that two tasks may use,
or that the tasks of one task statement may use while several run, with at least one of them writing it
after tasks have started, must be atomic or guarded:

- E3502: it is written with no lock that every write holds. The error names the write, the task that
  makes it (by the statement that starts it), and an access of another task it may meet.
- E3503: it is read without its guard. The error names the guard, the writes that hold it, and the read.

```go
var (
	mu   sync.Mutex
	hits int
)

func handle(done chan bool) {
	mu.Lock()
	hits++ // every write holds mu: mu guards hits
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		task handle(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	mu.Lock()
	println(hits) // without mu.Lock(): error E3503, hits is read without holding mu
	mu.Unlock()
}
```

A package variable written only during package initialization, or only by `main` before it starts its
first task, may be read by every task without a lock.

**Rule 2: views that cross.** A pointer or slice that crosses to another task (section 19.2) must point
to memory that is safe for both:

- memory of a `pluto/sync` or `pluto/sync/atomic` type, a runtime record (`rt.Task`, `rt.Timer`), a
  channel, or a struct of only those (`task worker(&wg)` with a package-level `wg`);
- a package variable, or part of one, that no task writes once tasks have started;
- the object of a `synced[T]` (rule 3);
- an object of a struct type that satisfies the **type rule**: no code overwrites a whole object of the
  type through a pointer, and each field either never changes once an object is shared, or is used only
  atomically, or only under one lock of the same object or one package-level lock, inferred as above.
  Writes that initialize an object do not count: writes through a pointer to memory the function
  allocated (or that a function returned after allocating it and keeping it to itself, such as a
  constructor), made before the pointer is stored, passed to a function that keeps it, sent or returned,
  including writes by a function it is passed to for that purpose. So an object built by its constructor
  and then only read, and an object whose methods all hold its own `sync.Mutex`, may both be shared.

A pointer to, or slice of, anything other than a struct crosses only in the first three cases. Anything
else is error E3501, which names a write that could change the memory while the other task uses it. The
fixes: pass a copy of the value; move an owner (a `vec.Vec` instead of a slice) so that only the receiver
has it; or keep the shared data where rules 1 and 3 guard it.

```go
type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add()      { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *Counter) Peek() int { return c.n } // makes the task statement below error E3501:
                                            // n is changed under c.mu in Add and read here without it

var counter Counter

func run(done chan bool) {
	task work(&counter, done) // work calls Add, and run calls Peek
	println(counter.Peek())
}
```

**Rule 3: synced objects and maps.** Every task that holds a reference to a `synced[T]` may use its
object, so every field of `T` that some code changes must be guarded by a lock of the same object, or
used only atomically, with the errors of rule 1 (E3502, E3503). Writes that initialize the object before
the reference is first copied do not count. Calling a pointer method on the box (`task s.Serve()`,
`s.Add(1)`) uses the object in the same way.

The contents of a built-in map are shared by every copy of the map, and a map may cross to another task
(section 19.2). When copies of one map may be used by several tasks, its contents follow the same rule:
the writes (assignments to elements, `delete`, `clear`) must hold one guard, and every read holds it too
(E3502, E3503). A map used by one task at a time, or by the tasks of a `sync.Scope` under its partition
rules, needs no lock.

**An owner needs none of this.** An owner moves to the task that receives it, so only that task has it.
`task c.bump()` on an `owned[Counter]` hands over the box, and no other task can reach the counter.

**What is not checked.** Unsafe code (section 18.12, Safe and unsafe code) is the programmer's
responsibility: memory reached through `unsafe.Pointer` or `rt.Pinned`, functions without a body, and the
variables and records of the runtime (`pluto/rt`), which implements tasks with its own locks and with
preemption disabled. A data race in unsafe code has the limited consequences of memory-model.md section
4, never undefined behavior.

### 19.4 Scoped tasks

A `sync.Scope` starts tasks that may use the variables of the function that starts them, and waits for
all of them before its own scope ends. It is how Plutonium writes Go's `sync.WaitGroup` idiom for tasks
that share a frame: the type's name says that it waits, where a `WaitGroup` waits only where `Wait` is
called.

```go
package sync

type Scope owner struct{ /* unexported fields */ }

func (s *Scope) Task(f func()) // starts f on a new task that s waits for
func (s *Scope) Wait()         // returns when every task started by s has returned
func (s *Scope) Drop()         // waits as Wait does; runs when s is dropped
```

The zero `Scope` is ready to use. A Scope has no `Add` or `Done`: its count is changed only by the
runtime, which counts a task before it starts and uncounts it after `f` returns. `s.Wait()` returning
therefore proves that every task started by `s` has returned. Since a Scope is an owner, assigning it
moves it, and a struct that holds one must be declared `owner`.

```go
func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Task(func() { results[i] = work(it) })
	}
	s.Wait() // optional: s waits when process returns in any case
}
```

**Which tasks are scoped.** A Scope is **local** when it is a local variable of the function, or a field
or element of one at a constant path (`t.s`, `scopes[2]`), so that the compiler knows where it lives and
that it lives until the end of its block. A task is **scoped** when it is started by `s.Task(f)` on a
local Scope `s`. A Scope reached through a pointer, or stored in heap memory, still waits when it is
dropped, but the tasks it starts are not scoped: `f` may not use the frame, as for a task statement
(section 19.1). A task statement never starts a scoped task. `sync.WaitGroup` behaves exactly as Go's
(`Add`, `Done`, `Wait`, and `Task` for Go's `Go`): it never waits on its own, and the tasks it counts are
not scoped.

**Joins.** A scoped task is **joined** at the points where the starting function knows it has returned:

- after a call `s.Wait()` on the same Scope;
- where `s` is dropped: at the end of its block, on every path out of it, by the rules of section 18.6.
  Among the variables of one block, Scopes are dropped first, before the others are dropped in reverse
  order of declaration, so a task has returned before anything it may use is dropped. In a function's
  outermost block the drop runs after the deferred calls, and a deferred `s.Wait()` joins at the same
  point.

The compiler follows, at each statement, which `s.Task` calls may have tasks that are not yet joined:
an `s.Task` adds itself, an `s.Wait()` removes every `s.Task` on `s`, and where paths meet, the sets are
united. A `Wait` inside a loop joins each iteration's tasks before the next iteration; a `Wait` in one
branch of an `if` joins only on that branch, so the tasks count as running after the `if`. A Scope
declared in a loop body is dropped, and so waits, at the end of each iteration. The stretch of the
function from an `s.Task` to its joins is that call's **interval**, where the rules below apply. The join
is a synchronizing operation: the return of a scoped task happens before the return of the `Wait` or
drop that joins it (section 19.7).

**What a scoped task may use.** A scoped task may use any variable of the starting function, and
anything a view in it points to, as long as the storage lasts until the join: views of the frame (`&x`,
`buf[:n]` of a local array, `&T{...}`), capturing function literals, method values of local variables,
and views derived from parameters. None of these is an escape (section 18.3): a view given to a scoped
task is bounded by the join, which comes before the function returns.

- **Variables that end before the join are taken.** A captured variable whose block may end before the
  task is joined, such as a loop variable, or a variable declared in the loop body when the `Wait` is
  after the loop, is **taken** by the task when `s.Task` runs: a value is copied, an owner is moved, and
  the task uses its own copy. This gives Go 1.22's per-iteration meaning to the source as written. The
  copy cannot be observed, since the sharing rules below reject a variable that the starting function and
  the task both use after `s.Task` unless neither writes it. A taken variable must be sendable (section
  19.2), and a taken owner is dropped by the task when it returns.
- **Views of storage that ends before the join** are error E3401. Taking a variable copies it, not what a
  view in it points to: a slice of an array declared in the loop body, taken into a task that runs past
  the iteration, still looks at that array. Use the array itself in the literal, which the task then
  takes, or wait inside the loop.
- **Captured owners.** A captured owner whose block outlives the join stays where it is, and the literal
  may use it. Since a scoped literal runs exactly once, it may also move the owner, which counts as a move
  at the `s.Task` call (section 18.5).
- **Func values.** A literal written as the argument of `s.Task` gets a closure record of its own in each
  task, holding the addresses of the shared variables and its own copies of the taken ones. A func value
  made elsewhere (a variable, a parameter, a method value) is used through its record, which must last
  until the join, by the rules of section 18.3.

**Sharing.** During an interval, every place that a scoped task uses and that anyone writes (the
starting function, a task, or several tasks of one `s.Task` in a loop) must be one of:

1. **Synchronized by its type:** a `pluto/sync` or `pluto/sync/atomic` value used through its methods,
   or a channel variable used for channel operations. Assigning a whole new value to such a place is a
   write like any other.
2. **Guarded by a lock:** every access, in every task and in the starting function, holds one and the
   same lock, inferred as in section 19.3 (a `sync.Mutex`, or the write lock of a `sync.RWMutex` with
   reads under `RLock`).
3. **Partitioned:** each task writes elements or windows of a slice or array that no other task, and not
   the starting function, can reach during the interval, in one of the forms below.

A place that nobody writes during the interval may be read by everyone. Anything else is error E3402,
which names both accesses.

**Partitions.** In these forms, `i` is the task's own copy of the variable of the innermost loop around
the `s.Task`, the `s.Task` runs at most once per iteration, and nothing writes `x`, `k` or `m` during the
interval:

| Accesses in the task | Loop | Why the parts are disjoint |
|---|---|---|
| `x[i]` and its fields | `for i := range ...`; `for i := a; cond; i++` (or `i--`, or `i += c`) | each iteration has a different `i` |
| `x[i*k : i*k+k]` or `x[i*k : (i+1)*k]`, then anything within that window | as above | windows of `k` elements at different multiples of `k` |
| `x[i : i+c]` or `x[i : min(i+c, n)]` | `for i := a; cond; i += c` | windows start `c` apart and are at most `c` long |
| `x[:m]` in one party, `x[m:]` or `x[m+d:]` in another | any | the two parts meet at `m` |

Here `c` and `d` are positive constants, or variables nothing writes during the interval, which the
compiler checks are positive before the first `s.Task` (the program panics otherwise). The index
arithmetic of these forms is checked for overflow, and a loop step other than `++` or `--` too, so that
parts cannot wrap around onto each other. A closure that passes `&x[i]` or a window of one of these forms
to a named function counts as making that access. Bounds checks keep a task inside its window, so once it
has one it may index within it freely. Indexing a library container (`results[i]` on a `vec.Vec`, section
11.11, Indexing library containers) counts as `x[i]` when the container's indexing methods touch only that
element. A partition covers the storage of the elements, not memory that pointers in them point to. Writes
that the compiler cannot prove disjoint are error E3403; a `[][]T` written as `grid[y][x]` is one, since
two rows may be the same slice, and a flat slice with a window per row, or a local `[H][W]T` array, is the
fix.

**Memory reached through views.** A captured slice or pointer is shared like the variable holding it, and
so is what it points to. Two views may point to the same memory, so a write through a view of element type
`T` conflicts with an access by another party through any view that can reach a `T`. In safe code memory
of one type is never seen as another type, so `results []Result` and `items []Item` never conflict.

**Memory outside the frame.** Package variables, and heap memory that the frame does not reach, follow
section 19.3, which applies to scoped and unscoped tasks alike. Since rule 2 of that section never lets a
view of changing memory cross to an unscoped task, no task outside the Scope can reach the frame's
variables, and the check of this section sees every task that can.

**Callees.** A call counts as the accesses its callee makes, through the callee's summary of which
parameter paths it reads and writes. A function that calls a func-valued parameter from several tasks at
once (a parallel map) records that in its summary, and passing it a literal that writes its captured
variables without a lock or an atomic is error E3406.

**What the starting function may not do meanwhile.** During an interval, for each variable a running
task uses, moving it, dropping it (`x.Drop()`), or assigning a new value to it when it is an owner (which
drops the old one) is error E3404, since the task could use freed memory. Moving the Scope, or assigning a
new Scope to its variable, while tasks it started may run is error E3405: the tasks find the Scope at its
address. Once everything is joined, both are allowed. Reading a variable nobody writes, and calling
`s.Task` again, are allowed.

**Non-local exits.** Every path out of the Scope's block drops it, so `return`, `break`, `continue` and
`goto` join. `rt.Goexit` leaves every frame through its ordinary return path, so the Scope's drop joins
there too (section 17.3). A panic reclaims no frame: the panicking task never returns, its stack is not
reused while the program runs, and tasks still running read valid memory until the program ends. A
kernel that ends a task without returning from its frames must not free or reuse that task's stack until
`rt.WaitScopes` has returned for it, which waits for every scoped task joined by a frame on that stack
(abi.md section 12).

**Nesting.** A scoped task may declare a Scope of its own: joins nest as the frames do. Tasks it starts
may use the outer function's frame through their captures, since the outer frame outlives the inner
join. A scoped task that uses `s` may call `s.Task` itself; the new task is counted while its starter
still is, so the count cannot reach zero in between. A task statement inside a scoped task is not
scoped.

**Allocators.** A scoped task starts with the program-wide default allocator, like every task (section
19.1). A task that should allocate from an arena installs one of its own.

**Examples.** A worker pool with a local channel and lock:

```go
func crawl(urls []string) (int, error) {
	jobs := make(chan string, 16)
	var mu sync.Mutex
	var total int
	var first error
	var s sync.Scope
	for w := 0; w < 8; w++ {
		s.Task(func() {
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

`jobs` is a channel variable used for channel operations, `total` and `first` are written only while `mu`
is held, and `urls` is only read. Without the `mu.Lock()` line, `total += n` is error E3402. `s.Wait()`
must come before `return`: reading `total` while the workers may write it is E3402, and the drop at the end
of the function would come too late for the read in the `return` statement.

Recursive splitting, where the task and the starting function use the two sides of a split point:

```go
func sortPar(v []int) {
	if len(v) < 2048 {
		slices.Sort(v)
		return
	}
	p := partition(v)
	var s sync.Scope
	s.Task(func() { sortPar(v[:p]) })
	sortPar(v[p+1:])
} // s waits here
```

### 19.5 The scheduler contract

The scheduler is split in two. abi.md section 12 is normative; this section summarizes it.

**The runtime** (`pluto/rt`) provides the mechanism: the record of each task (`rt.Task`), the context
switch (`rt.Switch`), parking with a permit, so that a wakeup that arrives before the park is never lost
(`rt.Park`, `rt.Ready`), the preemption entry (`rt.Preempt`), the preemption-disable count, timers on the
backend's clock, and channels, `select` and `pluto/sync` built only on those.

**A backend** provides the policy and the memory. It is a type that implements `rt.Scheduler`, whose
methods allocate and free task records and stacks (`NewTask`, `FreeTask`), make tasks runnable (`Spawn`,
`Wake`), and switch away from the running task (`Park`, `Yield`). Optional methods give the runtime an
initialization hook (`Init`), a way to refuse waits in interrupt context (`InInterrupt`), and a clock and
a timer (`Now`, `ArmTimer`, section 19.6).

**Binding.** The backend is bound when the program is linked: a package-level variable marked
`//pu:scheduler`, whose type (not an interface) implements `rt.Scheduler`, and the runtime calls its
methods directly. Hosted builds use the runtime's own backend unless the program binds one. A freestanding
program that starts tasks, uses channels or waits in `pluto/sync` without binding a backend fails to build
(E2593), as does one that binds two or binds one of the wrong type (E2592), and one that uses timers with a
backend that lacks `Now` or `ArmTimer` (E2595).

```go
//pu:scheduler
var scheduler kernelScheduler // implements rt.Scheduler
```

**What every backend must do.** Putting a task on a run queue is a release and taking it off an acquire,
on the same processor or another; a task is resumed only once the processor that switched it out has
finished saving it; switching is done only with `rt.Switch`; preemption goes only through `rt.Preempt`;
the backend's own locks disable preemption, and interrupts where `Wake` takes them; and `Wake` never
blocks, allocates or switches, since interrupt handlers reach it through `rt.Ready`. The compiler checks
`Wake` as it checks an interrupt handler. A backend that meets these rules gives tasks the memory model
of section 19.7, whatever its policy.

**The hosted backend** runs tasks on one worker thread per processor (or as many as `$PLUTO_MAXPROCS`
says), each with its own run queue, and an idle worker steals half the queue of a busy one. A monitor
thread preempts a task that has run for 10 ms and hands the processor of a worker blocked in a system
call to another worker. Waits for I/O on non-blocking descriptors go through one poller, so a task that
reads an empty pipe parks and its processor runs other tasks.

**Interrupt context.** In kernel builds, an interrupt handler runs on top of whatever code its processor
was running, so if it waited, the code it interrupted could never resume to end the wait. Nothing an
interrupt handler, the panic handler or the backend's `Wake` can reach may wait: a channel operation
other than a `select` with a `default` case, a lock that parks, `time.Sleep`, `s.Wait` or a `sync.Scope`'s
drop, or an allocator whose `Alloc` may wait (E2584); nor may it start a task (E2585). Operations that do
not wait are allowed: a `select` with `default`, `TryLock`, `rt.Ready`, atomics, and the runtime's
timers. The compiler checks this over the whole program; section 20.10 (Interrupts) defines it.

### 19.6 Timers

`time.Sleep(d)` parks the calling task until the deadline passes, so its processor runs other tasks, and a
program whose tasks all sleep uses no processor time. A hosted program that never starts the scheduler
sleeps in the operating system on its only thread instead, with the same effect.

Timers deliver the time on a channel, so a `select` waits for a timer as for any other case:
`time.After(d)` and `time.Tick(d)` return such a channel, and a `time.Timer` or `time.Ticker` holds one
in its field `C`.

```go
t := time.NewTimer(100 * time.Millisecond)
select {
case v := <-results:
	use(v)
case <-t.C:
	println("timed out")
}
```

**The channel owns its timer.** A timer belongs to the channel it delivers on, which is a counted
reference (section 19.8). When the last reference to the channel goes, the timer is stopped and freed with
it. `time.After` in a `select` therefore frees its timer when the `select` drops the temporary channel,
and a `Timer` or `Ticker` holds a reference to its channel like any other value that contains one. The
runtime's own link from its timer heap to the channel is not counted. Once `Stop` or `Reset` has returned,
a timer's channel holds no value from before the call.

`time.AfterFunc(d, f)` starts `f` on a task of its own once the timer expires. Like a function given to a
task statement, `f` must not capture variables (section 19.1). The function does not run where the timer
fires, which may be an interrupt handler in a kernel: an expired timer is queued for a task of
`pluto/time`, which starts each function.

**The mechanism.** The runtime's clock is `rt.Now()`, monotonic nanoseconds, the same on every processor.
An `rt.Timer` calls a function once its deadline has passed, and again every period if it has one;
arming, stopping and firing one never allocate or block, and its function must not block or allocate
either. `rt.ParkUntil(deadline)` is `rt.Park` with a deadline. A kernel's backend provides the clock and
the hardware through two optional methods: `Now`, and `ArmTimer`, which arranges for `rt.RunTimers()` to
be called (typically from the timer interrupt) once a deadline has passed. `rt.RunTimers`, and arming and
stopping a timer, may be used in interrupt handlers; `time.Sleep` and `rt.ParkUntil` park, so interrupt
handlers must not reach them (section 19.5).

### 19.7 Memory model

[memory-model.md](../memory-model.md) defines when a read in one task is guaranteed to observe a write
in another. In brief:

- **Happens before.** Within one task, operations happen in the order of evaluation (section 11.26, Order
  of evaluation). Between tasks, the synchronizing operations below order them, and happens before is
  the transitive closure of the two. A program with no data races behaves as if its tasks' operations
  were interleaved in some order on one processor.
- **Synchronizing operations.** A task statement happens before the first operation of the task it
  starts. A send on a channel happens before the receive that takes the value completes; closing a
  channel happens before a receive that returns because it is closed; for a channel of capacity `C`, the
  `k`-th receive happens before the `(k+C)`-th send completes. The `n`-th `Unlock` of a `sync.Mutex`
  happens before the `(n+1)`-th `Lock` returns, with the matching rules for `RWMutex`, `WaitGroup` and
  `Once`. The return of a scoped task happens before the return of the `Wait` or drop that joins it.
  `pluto/sync/atomic` operations are sequentially consistent.
- **Safe code has no data races** (section 19.3). The race rules use locks, atomics, moves and scoped
  joins to prove it, not the order that channels and `WaitGroup` give to plain data.
- **Unsafe code** can race. A race is a bug with limited consequences, never undefined behavior: a racy
  read of a naturally aligned value of at most a word observes some value written to it, and larger values
  (strings, slices, interfaces, structs) may tear. The compiler may not assume that a program has no races.
- **Moves and counts.** Moving an owner is an ordinary write; the task statement or channel send that
  hands it to another task is the synchronizing operation. Every use of a `synced[T]` object, a string
  buffer, a map table or a channel through any reference happens before it is dropped and freed.
- **Packed and volatile.** A packed value is one memory location, so two tasks writing different fields
  of it race. `volatile[T]` accesses are never removed, merged or reordered with one another and do not
  synchronize between processors; they order the accesses of an interrupt handler and the code it
  interrupts on the same processor.
- **Schedulers.** Every backend makes starting a task and readying a parked one synchronizing operations,
  so these rules hold however tasks are scheduled.

### 19.8 Channels

A channel (section 7.11, Channel types) passes values between tasks. `make(chan T)` makes an unbuffered
channel and `make(chan T, n)` one with a buffer of `n` elements, from the current allocator or from an
allocator given as the final argument, `make(chan Msg, 16, a)` (section 13.7, Making channels and maps).

**Operations.**

- A send `ch <- v` (section 12.5) on an unbuffered channel waits until a receiver takes the value. On a
  buffered channel it waits only while the buffer is full. Values are received in the order they were
  sent.
- A receive `<-ch` (section 11.23, Receive operator) waits until a value is available. `v, ok := <-ch`
  reports `false` when the value is the zero value because the channel is closed and empty.
- `close(ch)` (section 13.4, Close) records that no more values will be sent. Receives then return the
  values still buffered, and after them the zero value with `ok` false, without waiting. Sending on a
  closed channel panics, and so does closing a closed or nil channel.
- `for v := range ch` (section 12.10, For statements) receives until the channel is closed and empty.
- Every send and receive on a nil channel waits forever.
- A `select` statement (section 12.13) waits until one of its cases can proceed, and when several can,
  chooses one uniformly at random. With a `default` case it never waits. `select {}` waits forever.
- `len(ch)` and `cap(ch)` report the number of buffered values and the buffer's size. They never wait,
  and they do not synchronize.

**Owners and views.** The element type must be sendable (section 19.2). Sending an owner moves it: after
`ch <- b`, `b` holds nothing, and the task that receives it owns it. In a `select`, an owner in a send case
is moved only if that case proceeds (section 18.5). A view sent on a channel follows the rules of section
19.3.

```go
func produce(out chan<- vec.Vec[int]) {
	for i := 0; i < 3; i++ {
		v := vec.New[int]()
		v.Push(i)
		out <- v // moves v to whoever receives it
	}
	close(out)
}
```

**Counting.** A channel value is one word, a counted reference to the channel. Copying it adds a
reference; dropping a copy releases one, atomically. When the last reference goes, the values still in
the buffer are dropped (owners are dropped, counted values released) by the task that released it, and
the channel and its buffer are freed to the allocator they came from. A task waiting on a channel holds a
reference to it while it waits.

**Waiting.** The operations that may wait (a send or receive, a `range` over a channel, a `select` without
a `default` case) park the calling task, so its processor runs other tasks. A `select` with a `default`
case, `close`, `len` and `cap` never wait, which is what lets interrupt handlers use them (section 19.5).
A task parked on a channel is listed in a deadlock report with what it waits for: a receive, a send, a
`select`, or one of these on a nil channel.

> Adapted in part from the Go specification, copyright The Go Authors, CC BY 4.0; see
> [Attribution](../language.md#attribution).
