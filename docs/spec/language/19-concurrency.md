## 19. Concurrency

A **task** is an independent thread of execution with its own stack. Tasks run in parallel on the
available processors and are preempted, so a task that never blocks cannot keep the others from running.
They communicate through channels, share memory under the race rules of this chapter, and synchronize
with `pluto/sync` and `pluto/sync/atomic`.

The language provides the mechanism: the `task` statement (section 12.12, Task statements), channels
(sections 7.11, Channel types, and 19.8), `select` (section 12.13, Select statements), and the runtime
operations that start, park and wake tasks. Which task runs where and when is the policy of a
**scheduler backend**: the hosted runtime provides one, and a kernel provides its own (section 19.5). A
program and its dependencies that request no task starts, scheduler-dependent timers/callbacks,
waiting or poller operations contain no scheduler. Sequential allocation/cleanup alone requests none;
sole-thread hosted sleep has a direct alternative (section 19.6).

Safe code has no data races. Section 19.2 says what may cross from one task to another, section 19.3
how memory that several tasks reach is checked, and section 19.4 how tasks that are joined before a
function returns may use that function's variables. Section 19.7 summarizes the memory model, which
[memory-model.md](../memory-model.md) defines.

### 19.1 Tasks

**Starting a task.** The statement `task f(x, y)` starts a new task that runs the call `f(x, y)`. Section
12.12 (Task statements) defines what the statement does: how its arguments are evaluated and copied, moved
or counted into the new task, which arguments are sendable, which function values may be started (E2590,
E2594, E3003, E3004), and how a method of a box hands the box to the task. The same rules apply to the library functions
that start a task with a func argument (`rt.Go`, `(*sync.WaitGroup).Task`, `time.AfterFunc`) and to every
function of the program that passes a func parameter on to one of them; the compiler finds these functions
itself. A task that uses its starting function's variables is started by a `sync.Scope` instead (section
19.4). No task is started during package initialization (section 15.2, Panics and tasks).

**How a task starts.** A new task begins with:

- its own stack (below), on which its frames live;
- the program-wide default allocator as its current allocator, whatever override the starting task
  installed with `alloc.Use` (section 18.9, Allocators). This holds for every task, scoped or not;
- preemption enabled, and, in kernel builds, interrupts enabled whatever the state of the starting task.
  A kernel installs its interrupt table before it starts its first task.

Starting a task is a synchronizing operation: everything the starting task did before the task statement
happens before the new task's first operation (section 19.7).

**How a task ends.** A task ends when its function returns, after the function's deferred calls have run
and its variables and parameters have been dropped. `rt.ExitTask` ends a task early in the same way, from
any depth (section 17.3, Ending a task with `rt.ExitTask`). A panic in any task ends the program: in hosted
builds the panicking task first runs its pending deferred calls, while the other tasks keep running
(section 17.2, Deferred calls on a panic). Task end alone supplies no ordinary join to another running
task: a program uses a channel,
`sync.WaitGroup` or `sync.Scope` (section 19.4). When package cleanup runs, every application task has
finished before it begins (section 15.4).

**Stacks.** Each task has a stack of fixed size, which never grows or moves: nothing in the program
knows where the pointers into it are, so it cannot be moved. The size is chosen when the task starts:

1. When the call graph of the function started is bounded (no recursion, and no call through a func
   value or interface whose possible callees are not themselves bounded), the compiler computes the
   stack the task needs, locals included, and the task gets that, but not less than `rt.MinStackSize`.
2. `rt.Go(f, size)` starts `f` with a stack of `size` bytes, at least `rt.MinStackSize` (8 KiB). An explicit size takes precedence over the computed size of item 1,
   which applies to a `task` statement and to library task starts that give no size. This is
   how a program, a kernel or a scheduler binder asks for a size (appendix F).
3. Otherwise, in hosted builds, the task gets at least 64 MiB of address space where the address space
   allows, with a guard region below it, whose memory is committed only as it is touched. When it does
   not allow (the process has an address-space limit), the runtime reserves smaller stacks, never below
   64 KiB, and a program that needs a deeper stack asks for its size with `rt.Go`. In freestanding and
   kernel builds the task gets the default task stack size, `rt.DefaultStackSize` (64 KiB; section
   21.4, Implementation limits). `--stackreport` lists the tasks whose size could not be computed.

**Where a stack comes from.** A task's stack never comes from an allocator that the starting task
installed with `alloc.Use`, since that may be a local arena that is reset while the new task still runs.
In a hosted build the runtime supplies the stacks: reserved address space for stacks of unknown need, and
otherwise memory from the allocator given to `rt.Go`, or from the program-wide default allocator when none
is given, through the runtime's own cache of stacks for the default allocator. A scheduler
backend that a program binds, such as a kernel's (section 19.5), supplies stack memory itself, from pages
it owns. `rt.Go(f, size, a)` has an optional third argument, the allocator that the stack comes from
(the program-wide default when it is omitted); it must outlive the task, and a bound scheduler receives it
in the stack request and may take the memory from it or from its own pages.

A guard region is at least one page. Running past the end of a stack with a guard region is a fault, reported as a stack overflow panic with
the task's trace and the size it had, and how to ask for more (section 17.5, Faults). In freestanding and
kernel builds the scheduler backend provides the guard, below every stack whose size the compiler did not
prove (section 19.5). `rt.StackLeft()` returns the bytes left between
the caller's frame and the limit of its stack, for code that recurses deeply.

**Parallelism and preemption.** Tasks run in parallel on the processors the backend uses. A task that
runs for a full time slice (10 ms in hosted builds) without blocking is suspended wherever it is, even in
a loop that makes no calls, and another runs in its place. Every instruction is a place where a task may
be suspended, since there is no garbage collector to find pointers; the full register state is saved,
and preemption cannot be observed by the program (section 19.7). Code that must not be interrupted
(runtime and allocator locks, `rt.DisablePreemption`) disables preemption for its own duration, and a
preemption that arrives meanwhile happens when it is enabled again. Such code may not wait: a wait that a non-migrating region reaches through calls with a single known
target is a compile error (section 19.3, Waiting in a non-migrating region), and any other wait there
panics with "blocking with preemption disabled" (section 17.1) when it would park or when a function
marked `//pu:blocking` is called, in kernel mode also inside an interrupt-disabled region. Blocking on a channel, a lock or I/O,
or calling `rt.Yield()`, gives up the processor early.

**Deadlock.** In hosted builds, when every task waits for another task and none waits for I/O, a system
call or an armed timer, the program ends with a fatal error and status 2 (section 17.1) after printing the
state of every task: what it waits for (a channel operation, a `select`, a lock, a `sync.Scope`) and its
stack trace. For a task that was running deferred calls for a panic, such as a deferred `s.Wait()`, the
report also names that panic, with its message and where it happened (section 19.4, Deferred calls). The wording of the report is not part of the language (section 21.1); debugging.md section 5
describes what the hosted implementation prints.

**Program exit.** When `main` returns, the program ends; tasks still running are not waited for and their
owners are not dropped. Package cleanup runs only when no other application task is running (section
15.4, Program execution, which also defines its prohibition on new tasks and application callbacks, and
runtime retirement). The deadlock rule above still applies. `os.Exit` ends the program at once, from any
task, running no task or package cleanup. If the task that runs `main` calls `rt.ExitTask`, `main` does not return: the
other tasks go on, and once none is left, or all wait for good, the program stops with a fatal error and
status 2 (section 17.3, Ending a task with `rt.ExitTask`).

### 19.2 The sendable rule

A value **crosses** to another task when it is

- an argument or the receiver of a task statement (section 19.1), or a func value given to a function
  that starts a task with it;
- sent on a channel, or received from one;
- held inside an interface, struct or array that crosses;
- a variable that a scoped task uses, whether it takes it or captures it (section 19.4);
- loaded by a task from memory that other tasks may reach, when it is a pointer or any value that holds a
  view: a load from a package variable (a per-CPU variable included), from the object of a `synced[T]`,
  or from the contents of a map, or from memory reached through one of those, crosses to the task that
  loads it. The load counts in the task's own code and in every function it calls, an atomic load
  included, once a task other than the main task, or an interrupt handler, may run (section 19.3, The
  tasks). A value that holds a view is a pointer, a slice,
  an interface, a func value, a method value, or a struct, array or union that holds one of them. An interrupt handler that loads such a value is a party in the same way
  (section 19.3, Interrupt handlers).

Every value that crosses must be **sendable** (section 8.8, Sendable types), and may not hold a view that
the other task could use while it changes:

- a type that holds a `shared[T]` or a `weak[shared[T]]`, directly or through a field, element, payload,
  box or pointer, is not sendable (section 8.8): error E2010 at the task statement, at the channel type
  whose element is not sendable, or at the send. Converting a value of such a type to `error`, or to an
  interface that embeds it, is error E2020 at the conversion, since every `error` is sendable;
- a view of the starting or sending function's frame: E3004 for a task statement and E3005 for a send
  (E2011 where the type checker sees it directly);
- a view of heap or static memory that may change while both tasks can use it: E3501 (section 19.3).

Moving an owner to another task is safe because the sender loses it: only one task can use it afterwards,
and an owner that manages memory (`vec.Vec`, `hash.Map`, `strings.Builder`) takes that memory with it
(section 8.8). A `synced[T]` and a
map let several tasks reach the same memory, so what changes in it is subject to section 19.3. Strings
are sendable because their bytes never change and their counts are atomic once a program has started a task
(section 18.7, Shared ownership). A string made with `unsafe.String` is not counted (section
18.8); sending one is the responsibility of the unsafe code that made it, which must keep its bytes
alive and unchanged (section 18.12).

The type part of the rule (`shared[T]`) is checked by the type checker on the arguments and receiver of
every task statement and on the element type of every channel type, looking through struct fields, array
elements, union payloads, the referents of boxes, pointers and slices, and the transparent wrappers
`ring`, `volatile` and `aligned`. The view part is checked by the frame rules (section 18.3, Views) and the race
rules (section 19.3) where values cross, and, through interface values, for every type the program
converts to that interface, except `error` and the interfaces that embed it, which are sendable by
definition (section 8.8). A conversion whose result is a view of the frame that made it (section 18.3) is not
counted, since such a value cannot cross. In a generic function the rule is checked for each instantiation.

### 19.3 Data race freedom

A **data race** is two accesses to the same memory by different tasks or interrupt handlers, at least one a
write and at least one not atomic, that are not ordered by the memory model (memory-model.md section 2).
Safe code has none: the compiler rejects every program in which two tasks, or a task and an interrupt
handler, could make such accesses. It checks the whole program when it compiles, with no lifetime, borrow
or region annotations, by three rules, all inferred from the program. Every piece of memory that tasks
share is then owned by one task (moved there), never changed while shared, used only atomically, or
guarded by a lock that every access holds.

**The tasks.** The compiler finds every task the program may start: each task statement, each func value
passed to a function that starts a task with it (section 19.1), and each `s.Task` call (section 19.4).
It also finds every interrupt handler the program installs (section 20.10), which is a party in the
rules below like a task (see Interrupt handlers). The **main task** runs package initialization and `main`. A task statement that may run more than once
(in a loop, or in a function that may be called more than once) may start several tasks that run the
same code at once. Package initialization, which may not start a task (section 15.2, Panics and tasks), and
the part of `main` before the first statement that may start a task, run while no other task exists.
No handler is a party during that time either, until the first statement that may install an interrupt
handler (section 20.10) or enable interrupts (`x86.Sti`, `x86.RestoreFlags`), whichever comes first. From
that statement on, writes are checked against the handlers like writes in any task, and a view that a
handler loads crosses to it (section 19.2), whether or not a task has started.

**Accesses and locks.** For each task the compiler collects what it reads and writes, in its own code and
in every function it calls (a call through an interface or func value reaches every function that could
be called there), and which locks are held at each access:

- A lock is a `sync.Mutex`, a `sync.RWMutex` or an `rt.Lock` (a `sync.Mutex` cannot be taken in an interrupt
  handler, E2584, so a handler uses `rt.Lock`). It is held from `Lock` (or `RLock`, or a `TryLock` or
  `TryRLock` that succeeded, in the branch where it did) to `Unlock` (or `RUnlock`), or to the end of the
  function after `defer mu.Unlock()`.
- A lock counts as held at a statement only if it is held on every path to that statement.
- A function that returns with a lock held holds it for its caller, and a lock held around a call counts
  as held inside the callee.
- A lock reached through a pointer is named by where the pointer came from, so `s.mu` and `s.items` in
  one method are the lock and the data of the same object.
- A task, an interrupt handler, and a function that `s.Task`, `wg.Task` or a task statement starts, begin
  with no lock held, whatever the code that starts them holds.
- Every `Unlock`, `RUnlock` and `c.Wait(l)` must release a lock that the task holds (Releasing a lock,
  below). A release of a lock the compiler cannot name is a compile error.
- `c.Wait(l)` of a `sync.Cond` counts as `l.Unlock()` followed by `l.Lock()`. The argument must name a
  lock the task holds for writing: the address of a lock (`&mu`, `&s.mu`), or a pointer to one. So a lock
  held before the `Wait` is held again after it, and a view loaded before the `Wait` has crossed a release
  of that lock (rule 2). A `sync.Locker` held in a variable, the result of `rw.RLocker()`, and a `Wait`
  called through an interface or a func value do not name a lock, so such a `Wait` is a compile error.
  `Signal` and `Broadcast` need no lock, and they neither take nor release one.
- A method value may be called later, anywhere, so what its method does counts as done with no lock
  held.
- The order that channel operations, `sync.WaitGroup.Wait` and the end of a task impose is not used:
  reading shared data after `wg.Wait()` still holds the data's lock. The join of a `sync.Scope` is used,
  for the frame data it covers (section 19.4), and so is the return of `sync.Once.Do`, for the data that
  Once initializes (rule 1, Lazy initialization).

**Releasing a lock.** Every call of `Unlock` or `RUnlock` of a `sync.Mutex`, a `sync.RWMutex` or an
`rt.Lock`, and every `c.Wait(l)` of a `sync.Cond`, must release a lock that the calling task holds at that
call by the inference above: held on every path to the call, for writing (`Lock`, `TryLock`) for `Unlock`
and `Wait`, and for reading (`RLock`, `TryRLock`) for `RUnlock`. Otherwise the call is a compile error,
which names the call and a path on which the lock is not held. Interrupt handlers follow the same rule. The
rule needs no annotation and costs nothing at run time. It covers a read lock, which has no single owner:
each task releases only the read lock it took. So no task can release a lock that another task holds. A
locked lock cannot be handed to another task to unlock. To let another task end the work, send it a
message on a channel, or start it in a `sync.Scope` and wait for it. Where one task must take a permit
and another give it back, use a buffered channel as the semaphore: any task may receive from it or send
to it.

How "held at the release" is decided:

- **Names.** The compiler must name the lock the same way at the `Lock` and at the release, so that both
  reach the same lock on every path. A name is a package variable that is not a pointer, or a local
  variable or parameter (the receiver included), followed by field selections and constant indexes of
  arrays. Only the variable itself may hold a pointer on the way. So `mu`, `s.mu`, `s.inner.mu` (with
  `inner` a struct field) and `table[0]` are names, but `s.next.mu` is not: it loads a pointer from memory
  at each use, and another task may change that pointer in between. Copy the pointer into a local:
  `n := s.next; n.mu.Lock(); ...; n.mu.Unlock()`. A local that holds a pointer names nothing if its
  address is taken or a function literal assigns it. A parameter names, at each call, the lock its argument
  names there.
- **Assignments and moves.** Assigning to a variable ends every name through it. A copy carries the
  names: after `cur = next`, the lock held as `next.mu` is held as `cur.mu` too, and a release through
  either name ends both. Assigning a new value to a lock, or moving or assigning the memory that holds it,
  in the function or in a function it calls, ends its name, so a later release is an error. A copy of a
  struct that holds a lock holds a different lock, which is not held. Assigning a new value to a lock
  that another task may also use is error E3502, which names the assignment: no guard can order it with
  the other task's critical section, which lasts from its `Lock` to its release. Moving or freeing the
  memory that holds such a lock is a write of that memory, which the rules above already reject while
  another task may use it.
- **Variable indexes.** `locks[i]` with a variable index is not a name. Take a pointer once and use it for
  both calls: `l := &locks[i]; l.Lock(); ...; l.Unlock()`.
- **`TryLock`.** The lock is held in the branch where the call returned true: the `if` branch of
  `if mu.TryLock() {`, and the code after `if !mu.TryLock() { return }`.
- **`defer`.** `defer mu.Unlock()` releases at the end of the function, also when it ends by a panic. So
  the lock must be held at the `defer` statement and at every point after it. A `c.Wait(&mu)` there keeps
  it held, since `Wait` takes the lock again before it returns. Deferred calls run in reverse order, and
  each must find its lock held after the ones that run before it.
- **Loops.** A lock taken and released in one iteration is held between the two on every path. A lock
  released after a loop must be held on every path out of the loop, including the one that skips it.
- **Calls.** A function that returns with a lock held, such as `func (s *S) lock() { s.mu.Lock() }`, holds
  it for its caller under the caller's name for the argument: after `x.lock()`, `x.mu` is held. A release
  in a callee, such as `func (s *S) unlock() { s.mu.Unlock() }`, is allowed when every call of the callee
  holds the lock its argument names, and the caller then no longer holds it. A function reached through an
  interface, a func value or a method value counts as called with no lock held for this rule, so it may
  release only locks it takes itself. A helper that locks and unlocks its own parameter around a call of a
  func value, `withLock(&mu, f)`, compiles.
- **Recursion.** A recursive function may release a lock only if every call of it, the recursive calls
  included, holds that lock, for chains of calls of any depth.
- **Function literals.** A literal called where it is written, or deferred, such as
  `defer func() { mu.Unlock() }()`, is part of the function that contains it, at the point where it runs.
  So is the body of a range-over-func loop (section 12.11), with the iterator as a call around it: a lock
  taken in the body and released by a `defer` there is held where the function returns. A literal used in
  any other way is a func value.
- **Generics.** The rule is checked for each instantiation, and a method of a type parameter is that
  method of the type argument. So `func withLocked[L sync.Locker](l L, f func())` that locks and unlocks
  `l` compiles when `L` is `*sync.Mutex`.
- **Interfaces.** `l.Unlock()` on a `sync.Locker`, or on any interface value, does not name a lock, so it
  is an error. Pass the lock's own type (`*sync.Mutex`) or use a type parameter.

The methods of `sync.Mutex` and `sync.RWMutex`, of the `Locker` that `RWMutex.RLocker` returns, and
`sync.Cond.Wait` release locks that their callers hold, through a lock inside the type or the `Locker` they
are given. They are trusted for this (section 18.12), and the rule is checked at each call of them instead.
The runtime (`pluto/rt`), which implements `rt.Lock`, channels and the scheduler, is outside the race rules
(What is not checked, below). No other code needs an exemption.

```go
type Stats struct {
	mu    sync.Mutex
	count int
}

func (s *Stats) lock()   { s.mu.Lock() }   // returns with s.mu held
func (s *Stats) unlock() { s.mu.Unlock() } // every call holds s.mu

func (s *Stats) Add(n int) {
	s.lock()         // s.mu is held from here
	defer s.unlock() // released at the end, where it is still held
	s.count += n
}
```

```go
// finish releases a lock that another task took.
var (
	mu    sync.Mutex
	ready bool
)

func producer() {
	mu.Lock()
	ready = true
	task finish() // the new task starts with no lock held
}

func finish() {
	mu.Unlock() // error: finish does not hold mu
}
```

**Non-migrating and interrupt-disabled regions.** These two kinds of region are found the way held locks are: on every path, across calls, with a
deferred release counting as a release at the end of the function.

- A **non-migrating region** is a stretch of code where, on every path, preemption is disabled
  (`rt.DisablePreemption` to `rt.EnablePreemption`), maskable interrupts are disabled (`x86.Cli` to `x86.Sti`,
  `x86.SaveFlagsCli` to `x86.RestoreFlags`), or an `rt.Lock` is held. The task cannot move to another
  processor inside it.
- An **interrupt-disabled region** is a stretch of code where maskable interrupts are disabled by those
  calls, or an `rt.Lock` is held in kernel mode (abi.md section 12.5: runtime locks disable interrupts
  there), or the code is the body of a `//pu:interrupt` handler up to a call that may switch tasks
  (`rt.Preempt`, `rt.Yield`) or to a call of `x86.Sti`, which ends it. A handler is entered through an
  interrupt gate that disables interrupts (section 20.10), so its body is such a region. The body of a
  handler marked `//pu:interrupt trap` is not: a trap gate leaves interrupts as they were, so the handler
  can be re-entered, and it opens an interrupt-disabled region of its own with `x86.Cli` or `x86.SaveFlagsCli` where it needs one.
  Exceptions and nonmaskable interrupts are not excluded by an interrupt-disabled region; that remains a contract of the kernel
  (section 20.10).

Section 20.9 (Per-CPU variables) uses both. It also uses them for a store that depends on an earlier load
of the same per-CPU variable: that is a read-modify-write and needs a non-migrating region, as `ticks++` does. The calls named above are not unsafe code: they cannot make an
access invalid, and the analysis follows them (section 18.12). A wrong pairing can stop a processor, which
is a liveness failure, not a safety one.

**Waiting in a non-migrating region.** An operation that may park the task is a compile error where the
check below reaches it inside a non-migrating region: between `rt.DisablePreemption` and
`rt.EnablePreemption`, while an `rt.Lock` is held, or while maskable interrupts are disabled. The
operations are the waiting operations of section 20.10 (Interrupts): a channel operation that may block,
`Lock` of a `sync.Mutex` or a `sync.RWMutex`, `Wait` of a `sync.WaitGroup`, a `sync.Cond` or a
`sync.Scope` (and the `Drop` of a Scope), `time.Sleep`, `rt.Park`, a function marked `//pu:blocking`, and
an allocation from an allocator that may wait. A task that parks there would hold its processor, or a
lock that other processors spin on, while it waits for something that may need that processor or that
lock.

The check follows from each non-migrating region the calls that have a single known target, through any
number of them:

- a direct call of a named function;
- a call of a method on a concrete type, not through an interface;
- a deferred call of one of those;
- a function literal called where it is written (`func() { ... }()`, also deferred), whose one target is
  that literal;
- a drop the compiler inserts for a value of a known type, into its `Drop` method and drop glue (section
  18.6), and a `Reset`, `Release` or `Drop` of an arena the compiler can name, into the drop entries of
  that arena (section 18.9, Drop entries).

It reports a wait reached this way when every path to the wait lies inside the non-migrating region, and
names each call on the way. A wait on only some paths of a followed callee is still reported: the callee
is entered inside the non-migrating region, and the check does not ask which of the callee's paths runs.

The check does not follow a call with more than one possible target: a call through an interface, a func
value, a method value or a hook (section 20.10, Calls through interfaces and func values), or a call on
an arena the compiler cannot name. Nor does it follow an allocation, or a drop that frees, into the
allocators the program installs, which `new`, `make` and the drops reach through the `alloc.Allocator`
interface. A wait reached only through such a call compiles, and the run-time test below catches it.

Here `tick` calls `record` directly, and `record` sends on `events` on one of its paths, so the check
reports the call:

```go
var events = make(chan int, 16)

func record(n int) {
	if n > 0 {
		events <- n // blocks while the buffer is full
	}
}

func tick(n int) {
	rt.DisablePreemption()
	record(n) // error: a direct call reaches a send that may block, inside the non-migrating region
	rt.EnablePreemption()
}
```

Here the call goes through an interface, so the check does not follow it. If `s` holds a `chanSink` and
the send blocks, `rt.Park` panics:

```go
type Sink interface{ Put(n int) }

type chanSink struct{ ch chan int }

func (s chanSink) Put(n int) { s.ch <- n }

func flush(s Sink, n int) {
	rt.DisablePreemption()
	s.Put(n) // compiles: a call through an interface is not followed
	rt.EnablePreemption()
}
```

The check needs a non-migrating region on every path, as above, and it starts only from such regions in
code outside `pluto/rt`: the runtime's own locked code is unsafe code (section 18.12), and a path from a
program's non-migrating region into the runtime still ends at its `park` (section 20.10). A wait that the
check does not report, such as one reached on only some paths through a non-migrating region or through
one of the calls above, fails at run time by one test made in two places. `rt.Park` makes it before it
parks, and the compiler inserts the same test at the entry of every function marked `//pu:blocking`; for
such a function without a body, it inserts the test before each call of it instead (section 20.3,
Directives). The test panics with "blocking with preemption disabled" when the task's preemption is
disabled, and in kernel mode also when maskable interrupts are disabled, so an interrupt-disabled region
opened with `x86.Cli` is covered too (section 17.1; abi.md section 12.4). Every waiting operation above
ends in `rt.Park` or is marked `//pu:blocking`. So in hosted builds, where `x86.Cli` faults, and in kernel
mode, a waiting operation in a non-migrating region fails to compile, or panics when it would park or
when a `//pu:blocking` function is called; none hangs the processor in silence. Other freestanding builds
do not test the interrupt flag (abi.md section 12.4).

That promise covers waiting operations, not every loop. A loop that spins until other code acts, written
without `//pu:blocking`, such as `for {}` or a poll of a flag while an `rt.Lock` is held, is not an
operation the language can see as a wait; if it never ends, the program has a bug of its own and the
processor hangs. Marking the function that spins `//pu:blocking` brings it under the check and the test.
An interrupt-safe spin lock, which waits only for another processor's progress, is not a wait (section
20.10). Code outside Plutonium that waits without being marked `//pu:blocking` breaks the contract of a
function without a body (section 18.12), and is not covered either.

The check for interrupt handlers (E2584, section 20.10) is conservative through interfaces, func values,
hooks and allocators, and this one does not follow them, for a reason. A wait in a handler has no safe
outcome at run time: the handler runs on top of the code it interrupted, which cannot resume, so the
processor stops. A wait in a non-migrating region of a task fails loudly at the panic above, and is not a
memory or data race error. Calls through operations tables and allocation while a lock is held are
ordinary in a kernel, and a conservative rule would reject them. This is the one check in the language
that accepts a program on doubt (section 1.2, Design rules).

**Guards.** For data that several tasks may use, the compiler infers its **guard**: a lock that every
write of the data holds for writing (`Lock`, not `RLock`). The data is safe when every access, read or
write, holds its guard; a read may hold it with `RLock`. Data that every access reads or changes with
`pluto/sync/atomic` operations (the methods of `atomic.Int64` and the other atomic types, or functions
such as `atomic.AddInt64(&x, 1)`) needs no lock. A field of a struct is one piece of data, and all the
elements of an array or slice together are one piece, except that elements selected by different constant
indexes (`table[0]`, `table[1]`) are different pieces; an element selected by a variable index counts as
every element. Writing a whole struct writes all its fields.

**Frees are writes.** Dropping, freeing or moving an object counts as a write of the whole object, and so
does assigning a new value to, or dropping, any owner that holds it (directly, or through owners that hold
one another). A `Reset`, `Release` or `Drop` of an `arena.Fixed` or `arena.Chunked`, the drop at the end
of the arena's scope included, takes memory back. It counts as freeing every object that `New` or `Slice`
allocated in that arena, of every type, whether or not the type has drop glue. The compiler cannot tell
which objects come after a mark, so a `Release` counts as freeing objects of every type allocated in the
arena too. The drops that the arena's drop entries run (section 18.9, Drop entries) count on top of that.
An allocation made through the arena's `Alloc` and still live makes the call panic and take nothing back
(section 18.9, Live allocations), so it is not freed. The compiler names an arena as it names a lock: by
the variable or field it is reached through, and for a pointer by where the pointer came from. A call on
an arena it cannot name, such as `workers[i].arena` with a variable index, counts as that call on every
arena it may be, within the limits of section 18.3 (How precise the frame check is); beyond those
limits, such as an arena reached through more loads than they allow, it counts as that call on every
arena of the program. The name follows a move of the arena: after `b := take(a)` or another move, the
arena is named by `b`, and a call on `b` frees the objects allocated through `a` before the move. This holds for rules
1 to 3 below, for the type rule, and for scoped tasks (section 19.4). So
an object reached through an owner can change while some code may replace, drop or move that owner, and in
safe code no task frees memory that another task may still use (memory-model.md section 4.1).
Values of `pluto/sync` and `pluto/sync/atomic` types synchronize themselves through their methods, and
assigning a whole new value to one is an ordinary write.

**Rule 1: package variables.** A package variable, or a field or element of one, that two tasks may use,
or that the tasks of one task statement may use while several run, with at least one of them writing it
after tasks have started, must be atomic or guarded. Per-CPU variables follow section 20.9 instead.
A package variable here includes the memory it owns: the object of an `owned[T]` it holds and the memory
of the owners it holds, through owners that hold one another. A pointer or other view loaded from a
package variable is a value of the variable for this rule, and a value that crosses to the task that loads
it (section 19.2), so what it points to follows rule 2. A package variable whose type is not sendable (one
that holds a `shared[T]`) may be used by one task only, since copying a reference out of it changes a
count with plain arithmetic: a use by a second task, or by the tasks of one task statement while several
run, is error E2010.

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
	println(hits) // error: E3503, hits is read without holding mu
}
```

A package variable written only during package initialization, or only by `main` before it starts its
first task, may be read by every task without a lock. That covers the variable and the memory it owns. A
pointer read from it still crosses, so memory the variable does not own, such as an object that a handler
table registered during initialization points to, is safe only by rule 2.

**Lazy initialization.** Data that a `sync.Once` initializes needs no lock either. A package variable,
with the memory it owns, is **initialized by** a package-level Once `o` when, once tasks have started,
every write of it is made during a call of a function passed to `o.Do`, and every read of it outside such
a call is made where a call of `o.Do` has returned on every path to the read. A field of an object, with
the memory it owns, is initialized in the same way by a Once that is a field of the same object. The
Once is named as a lock is named (Accesses and locks, above), so `t.once` and `t.index` reached through
one pointer are the Once and the data of one object. Data that a Once initializes may be read by every
task without a lock. The return of `f` is synchronized before the return of every `Do` (memory-model.md
section 3.4), so a read after a returned `Do` sees every write that `f` made.

- **During a call of `f`.** A write is made during the call when it is in `f`, or in a function that `f`
  calls, on the path from the call of `f` that `Do` makes; a callee counts as a lock held around a call
  counts inside the callee. The same function reached in any other way, such as a literal that is also
  called directly or stored, writes outside. A write through a pointer is a write of the data that the
  compiler names as its target, by where the pointer came from; a write whose target it cannot name is a
  write of all data of that type. A task that `f` starts is another task, so its writes are outside. A
  method value's method counts as run outside every `Do`, as it counts as run with no lock held. Writes
  made during package initialization, or by `main` before it starts its first task, do not count.
- **After a returned `Do`.** A call of `o.Do` counts for a read when it is made in the reading function,
  or in a function that it calls directly and that calls `o.Do` on every path to its return, as a lock
  held at a return counts for the caller. A `Do` made by another task, such as the task that started
  the reader, does not count, and neither does a `Do` reached through an interface, a func value or a
  method value (`rt.CatchExitTask(func() { o.Do(f) })` included). An interrupt handler cannot call `Do`,
  which may park (E2584), so it never reads data a Once initializes without the ordinary rules.
- **Otherwise, the ordinary rules.** Data written outside these calls, or read on some path with no
  returned `Do`, follows rule 1 as other data does. `Do` holds the Once's own lock while `f` runs, so the
  writes inside `f` hold that lock and it is the inferred guard. A write outside with no lock is E3502,
  which names that write. A read with no returned `Do`, when every write is inside, is E3503, which names
  the Once and the read. Data written inside the calls of two Onces has no lock that every write holds
  (E3502).
- **When `f` does not return.** `Do` returns only after some `f` has returned. A panic in `f` stops the
  program (section 17.1), and that `Do` never returns. If `f` ends its task with `rt.ExitTask` (section
  17.3), `Do` does not return to its caller either: its frame leaves with the others. The Once releases
  its lock and is not done, so the next call of `o.Do` calls its own function, which finds what the first
  one left. Either way, the writes of an `f` that did not return are followed by the writes of the next
  `f` under the same lock, and no read follows a returned `Do` until some `f` has returned.
- **Several functions.** Different calls of `o.Do` may pass different functions. Only one of them
  returns, and they all write inside. Data that the function which ran did not write keeps the value it
  had when tasks started.
- **Copying and resetting.** A Once whose whole value is assigned once tasks have started, or that is
  moved, dropped or freed with the object that holds it, initializes nothing: its data follows the
  ordinary rules. A copy of a Once is another Once, so a `Do` on the copy does not count for the data of
  the first.
- **Views.** A pointer or slice loaded from the data crosses (section 19.2), and rule 2 decides what it
  may point to. Memory that the data owns is safe by rule 2 case 2 for a package variable, and case 3 for
  a `synced[T]` object. Dropping or replacing an owner that the data holds is a write (Frees are writes,
  above), so outside `f` it sends the data back to the ordinary rules, and a view loaded from it is E3501
  as well, naming that write. A view obtained through a counted reference held in a local variable ends
  with that reference (section 18.3), so a use after the reference is dropped is E3313.

```go
type Config struct{ port int }

var (
	cfgOnce sync.Once
	cfg     owned[Config]
)

func port() int {
	cfgOnce.Do(func() { cfg = new(Config{port: 8080}) })
	return cfg.port // allowed: every write of cfg is inside cfgOnce.Do, which has returned here
}

func serve(done chan bool) {
	println(port())
	done <- true
}

func start(done chan bool) {
	task serve(done)
	task serve(done)
}
```

A write outside the Once sends the data back to rule 1. Here `reload` replaces the owner with no lock:

```go
type Limits struct{ max int }

var (
	limOnce sync.Once
	lim     owned[Limits]
)

func maxConns() int {
	limOnce.Do(func() { lim = new(Limits{max: 64}) })
	return lim.max
}

func reload() {
	lim = new(Limits{max: 128}) // error: E3502, a write of lim outside limOnce.Do, with no lock
}

func start() {
	task reload()
	println(maxConns())
}
```

**Rule 2: views that cross.** A pointer or slice that crosses to another task (section 19.2) must point
to memory that is safe for both. That includes a pointer the task loads from a package variable, from the
object of a `synced[T]` or from a map, and a view held in an interface, func value or method value that it
loads from one, wherever the view was stored. The memory must be:

- memory of a `pluto/sync` or `pluto/sync/atomic` type, a runtime record (`rt.Task`, `rt.Timer`), a
  channel, or a struct of only those (`task worker(&wg)` with a package-level `wg`);
- a package variable, or part of one, including the memory it owns (rule 1), that no task writes once
  tasks have started, or that a package-level Once initializes (rule 1, Lazy initialization), read where
  that Once's `Do` has returned. Replacing, dropping or moving an owner the variable holds writes the memory it
  owns (Frees are writes, above). This case also covers a **static object**: an object that a
  package-level initializer creates as a composite literal whose address it takes (`var Stdout =
  &File{fd: 1}`), so that it lives in static storage (section 18.2), and that no code moves, drops or
  overwrites as a whole once tasks have started. Nothing frees static storage, and its cleanup runs only
  in package cleanup, after the tasks have ended (section 15.4), so the free condition of the type rule
  below does not apply to it: a drop, free, move or overwrite of another object of the same type does
  not count. A static object may change, but each of its fields or elements must meet the field
  conditions of the type rule: it never changes once tasks have started, or it is used only atomically,
  or only under one lock of the same object or one package-level lock. The compiler names the target of
  a `take(p)` or an assignment `*p = v` as it names a lock, by where the
  pointer came from; one it cannot name counts as a move of every static object of the type. A static
  object stays static when code assigns a new pointer to the package variable that refers to it: the
  assignment is a write of the variable under rule 1, so once tasks have started it needs the variable's
  guard or an atomic store, and a task that loaded the old pointer may go on using the old object. A
  task that loads the variable after that gets the new object, which must be safe for it by its own
  case;
- the object of a `synced[T]` (rule 3), and memory that it owns and that a Once of the same object
  initializes, read where that Once's `Do` has returned;
- memory that a package variable owns (rule 1), or that the object of a `synced[T]` owns through owners
  that hold one another, when every access to that memory, by every task and through every view of it,
  holds the guard inferred for it (Guards, above; a write holds it for writing), and the view is used
  only while the guard it was loaded under is still held: on every path from the load to the use, the
  guard is held and not released. A release between them counts even when the lock is taken again, by
  `Unlock` and `Lock` or by a `Cond.Wait` that releases it (Accesses and locks, above), since another
  task may have changed or freed the memory in between. The guard of a package variable is the one rule 1 infers,
  and the guard of a `synced[T]` object is the lock of the same object that rule 3 infers. A lock held
  around a call counts inside the callee, so `items.Push(x)` between `mu.Lock()` and `mu.Unlock()` may
  load the backing slice of a package-level `vec.Vec`; `hash.Map` and `strings.Builder` are used the
  same way. A use of the view where no guard is held, on some path, is error E3501. That includes a view
  used after `Unlock`, or returned to a caller that uses it after the callee unlocks. A copy of the view
  whose uses the compiler cannot follow counts as used with no lock held: one stored anywhere but in
  memory that the same guard guards, sent on a channel, passed to a task, captured by a function literal
  that is not called where it is written, or held in a method value. An argument of a deferred call is
  used when the call runs, at the end of the function, so it holds the guard only if the guard is still
  held there (a `defer mu.Unlock()` written before it runs after it);
- an object of a struct type that satisfies the **type rule**: no code overwrites a whole object of the
  type through a pointer, or frees, drops or moves one (or replaces or drops an owner that holds one) once
  tasks have started, and each field either never changes once an object is shared, or is used only
  atomically, or only under one lock of the same object or one package-level lock, inferred as above,
  or is initialized by a Once of the same object (rule 1, Lazy initialization).
  Writes that initialize an object do not count: writes through a pointer to memory the function
  allocated (or that a function returned after allocating it and keeping it to itself, such as a
  constructor), made before the pointer is stored, passed to a function that keeps it, sent or returned,
  including writes by a function it is passed to for that purpose. So an object built by its constructor
  and then only read, and an object whose methods all hold its own `sync.Mutex`, may both be shared.
  An object that some code frees while tasks run is not safe for an unscoped task by this rule, since the
  task could still use it: such an object is shared as the object of a `synced[T]`, whose last release
  frees it after every use (rule 3), or handed over by moving its owner.

A pointer to, or slice of, anything other than a struct crosses only in the first four cases. Anything
else is error E3501, which names a write that could change the memory while the other task uses it. The
fixes: pass a copy of the value; move an owner (a `vec.Vec` instead of a slice) so that only the receiver
has it; or keep the shared data where rules 1 and 3 guard it, and use views of it only while the guard
is held.

```go
type Config struct{ n int }
type Server owner struct{ cfg owned[Config] }

var srv Server

func watch(c *Config) { for { println(c.n) } }

func start() {
	srv.cfg = new(Config)
	task watch(&*srv.cfg) // error: E3501, the next line frees the Config that watch reads
	srv.cfg = new(Config) // assigning to the owner drops the old Config: a write of it
}
```

A pointer that reaches a task through a package variable instead of an argument crosses in the same way.
Here `reader` loads `p`, so the `Node` it points to must be safe for `reader` and `main`. The next
assignment to `b` drops that `Node`, which is a write of it once tasks have started, so the `Node` is
neither unchanged memory nor of a type that satisfies the type rule:

```go
type Node struct{ n int }

var (
	b owned[Node]
	p *Node
)

func reader() { println(p.n) } // error: E3501, p is loaded here and main frees the Node it points to

func main() {
	b = new(Node)
	p = &*b
	task reader()
	b = new(Node) // drops the Node that p points to
}
```

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

A container in a package variable under one lock is the common case of guarded owned memory. Here every
access to `items`, and to the storage it owns, holds `mu`. `Push` loads the backing slice from that
storage, which is a crossing, and uses it only inside the call, where `mu` is held:

```go
// items and the storage it owns are guarded by mu.
var (
	mu    sync.Mutex
	items vec.Vec[int]
)

func add(x int, done chan bool) {
	mu.Lock()
	items.Push(x) // allowed: Push uses the backing slice while mu is held
	mu.Unlock()
	done <- true
}

func start(done chan bool) {
	for i := 0; i < 4; i++ {
		task add(i, done)
	}
}
```

A view that outlives the guard is the error. Here `first` returns a pointer into the storage after its
deferred `Unlock` has run, and `Push` in another task may move the elements and free that storage:

```go
// first lets a pointer into items escape its lock.
var (
	mu    sync.Mutex
	items vec.Vec[int]
)

func first() *int {
	mu.Lock()
	defer mu.Unlock()
	return items.Ptr(0)
}

func grow() {
	mu.Lock()
	items.Push(1)
	mu.Unlock()
}

func read() {
	p := first()
	println(*p) // error: E3501, p points into items and is used after mu is released
}

func start() {
	task grow()
	task read()
}
```

A wait loop on a `sync.Cond` keeps its guard. `Wait(&mu)` releases `mu` while it waits and holds it again
when it returns, so `mu` is held on every path to `Len` and `Pop`:

```go
type Job struct{ id int }

var (
	mu       sync.Mutex
	nonEmpty sync.Cond
	jobs     vec.Vec[Job]
)

func push(j Job) {
	mu.Lock()
	jobs.Push(j)
	mu.Unlock()
	nonEmpty.Signal() // needs no lock
}

func worker() {
	mu.Lock()
	for jobs.Len() == 0 {
		nonEmpty.Wait(&mu) // allowed: counts as mu.Unlock() and then mu.Lock()
	}
	j, _ := jobs.Pop()
	mu.Unlock()
	println(j.id)
}

func start() {
	task worker()
	task worker()
	push(Job{id: 1})
	push(Job{id: 2})
}
```

A view kept across the `Wait` crosses a release of its guard. Here `add` may grow `names` while `second`
waits, which moves the elements and frees the storage that `p` points into:

```go
// second keeps a pointer into names across a Wait.
var (
	mu    sync.Mutex
	ready sync.Cond
	names vec.Vec[string]
)

func add(s string) {
	mu.Lock()
	names.Push(s)
	mu.Unlock()
	ready.Broadcast()
}

func second() {
	mu.Lock()
	for names.Len() < 1 {
		ready.Wait(&mu)
	}
	p := names.Ptr(0)
	for names.Len() < 2 {
		ready.Wait(&mu)
	}
	println(*p) // error: E3501, p was loaded under mu and is used after a Wait released mu
	mu.Unlock()
}

func start() {
	task second()
	add("a")
	add("b")
}
```

A static object needs no lock to be reached. `os.Stdout` points to a `File` that the package initializer
of `pluto/os` created, so every task may print through it, even while other tasks drop the files they
opened. The `File` fields that change are used atomically:

```go
func work(name string, done chan bool) {
	if f, err := os.Open(name); err == nil {
		f.Close() // f is dropped at the end of the if: another File, which does not count
	}
	fmt.Println("done with", name) // allowed: os.Stdout points to a static File
	done <- true
}

func start(done chan bool) {
	task work("a", done)
	task work("b", done)
}
```

**Rule 3: synced objects and maps.** Every task that holds a reference to a `synced[T]` may use its
object, so every field of `T` that some code changes must be guarded by a lock of the same object, or
used only atomically, or initialized by a Once of the same object (rule 1, Lazy initialization), with
the errors of rule 1 (E3502, E3503). Writes that initialize the object before
the reference is first copied do not count. Calling a pointer method on the box (`task s.Serve()`,
`s.Add(1)`) uses the object in the same way.

The contents of a built-in map are shared by every copy of the map, and a map may cross to another task
(section 19.2). When copies of one map may be used by several tasks, its contents follow the same rule:
the writes (assignments to elements, `delete`, `clear`) must hold one guard, and every read holds it too
(E3502, E3503). The guard is a package-level lock or a lock of the struct the map is a field of. A map used
by one task at a time needs no lock. The tasks of a `sync.Scope` that write a map during an
interval need a guard (rule 1 or 2 of Sharing, section 19.4), because a map has no partition form:
writes to different keys race on the table. Since
any copy of a map reaches its table, the compiler treats the contents of all maps of one type as one piece
of data once a map of that type crosses to another task (or a task reaches one through a package variable
or memory it did not allocate): maps of that type that tasks fill for themselves after handing them to a
function must then hold the guard too, or the shared map takes a type of its own. Maps a task fills before
it shares them, and maps of types that never cross, need no lock.

**Interrupt handlers.** Each interrupt handler the program installs (section 20.10) is a party that may run
at any point where interrupts are enabled, on any processor, and on several processors at once. Rules 1 to 3
apply with handlers as parties, and the errors name the handler as the party (E3502, E3503). So data that
a handler accesses while other code may write it, or that a handler writes while other code may access it,
must be atomic or guarded by an `rt.Lock`, unless it is a per-CPU variable (section 20.9). Data that is
not written while a handler may run, such as a table filled before the first handler is installed, needs
neither. A pointer that a handler loads from such a table crosses to the handler (section 19.2), so the
device record it points to is safe by rule 2: a package variable no party writes, a static object, or an
object whose type satisfies the type rule. The guard inference recognizes an `rt.Lock` as it recognizes a `sync.Mutex`. A `volatile[T]` access, and an
interrupt-disabled region, exclude only code on the same processor, so alone they satisfy the rules only for
per-CPU variables. An allocator has no exemption: its state is data like any other, and when a handler can
reach its `Alloc` or `Free`, the state they share with other code meets these rules (for example per-CPU
caches that are touched only in interrupt-disabled regions). The parts of an allocator written in unsafe
code are the responsibility of that code (section 18.12).

**Owner transfer requires exclusivity.** An owner moves to the receiving task, but the move is valid
only if no other view of the memory it holds is still live (section 18.3, Views): a pointer or slice
obtained from the owner and kept in a field, passed through a helper or copied to another variable
before `take` or the move, not only one its constructor kept. A view whose last use comes before the
move is allowed. A view of a local owner used after the move is E3313, and one stored where the compiler
cannot follow it is E3314, since the move ends it (section 18.3, A local owner's memory). Any other view
that may still be used while the receiving task can use the memory is E3501, which names the view, the
move and the conflicting use. Generation checks alone do not prevent races.
Thus `task c.bump()` hands over an `owned[Counter]` only under that exclusivity proof (section 8.8).

**What is not checked.** Unsafe code (section 18.12, Safe and unsafe code) is the programmer's
responsibility: memory reached through `unsafe.Pointer` or `rt.Pinned`, functions without a body, and the
variables, records and non-migrating regions of the runtime (`pluto/rt`), which implements tasks with its own locks and with
preemption disabled. The runtime's non-migrating regions are not checked for waits (Waiting in a non-migrating region, above), and
its releases of its own locks are not checked by the rule for releasing a lock (above). Nor are the
bodies of the lock methods and of `sync.Cond.Wait` that section 18.12 lists, whose callers the rule
checks instead. A data race in unsafe code gives torn or stale values (memory-model.md section 4),
and it is never undefined behavior in the compiler's sense: the compiler may not assume races are absent. A
torn pointer or length can then break memory safety, which is why unsafe code must not race.

### 19.4 Scoped tasks

A `sync.Scope` starts tasks that may use the variables of the function that starts them, and waits for
all of them before its own scope ends. It is the type for tasks that share a frame: its name says that it
waits, where a `sync.WaitGroup` waits only where `Wait` is called.

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
moves it, and a struct that holds one must be declared `owner`. One task waits on a Scope at a time: a
`Wait` or `Drop` while another task already waits on the same Scope panics with "sync: two tasks wait on
one Scope" (section 17.1).

```go
func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Task(func() { results[i] = work(it) })
	}
	s.Wait() // optional: s waits when process returns in any case
}
```

**Waiting for every task.** There is no function that waits for all the tasks of the program, and none is
planned: it would wait for tasks that the library starts and that never end, and it would be a second,
unscoped way to do what `sync.Scope` does. A program that wants to wait for its tasks uses a `sync.Scope`,
which guarantees the join and package cleanup (section 15.4), or a `sync.WaitGroup` or a channel as in Go,
which make cleanup likely but not guaranteed.

**Which tasks are scoped.** A Scope is **local** when it is a local variable or parameter of the function
of type `sync.Scope`, named directly, so that the compiler knows where it lives and that it lives until
the end of its block. A task is **scoped** when it is started by `s.Task(func() {...})`, with a function
literal written in the call, on a local Scope `s`, in the function that declares `s` or inside the literal
of another scoped task there. Every other `Task` call starts a task that is not scoped: on a Scope
reached through a pointer, stored in a field or element, or in heap memory; with a func value that is not
a literal written in the call; inside an ordinary function literal, on a Scope declared outside it; and
`task s.Task(f)` or `defer s.Task(f)`. Such a Scope still counts the task and waits for it when it is
dropped, but `f` may not use the frame, as for a task statement (section 19.1): a func value that may be a closure of
a frame is a compile error (section 12.12). A task statement never starts a scoped task. `sync.WaitGroup`
(`Add`, `Done`, `Wait` and `Task`) never waits on its own, and the tasks it counts are not scoped.

**Joins.** A scoped task is **joined** at the points where the starting function knows it has returned:

- after a call `s.Wait()` on the same Scope;
- where `s` is dropped: at the end of its block, on every path out of it, by the rules of section 18.6.
  Among the variables of one block, Scopes are dropped first, before the others are dropped in reverse
  order of declaration, so a task has returned before anything it may use is dropped. In a function's
  outermost block the drop runs after the deferred calls, so the deferred calls may run before this join
  (Deferred calls, below).
- where a deferred `s.Wait()` runs. Deferred calls run in reverse order, so it joins the tasks started on
  `s` after its `defer` statement before any call deferred earlier runs, on a return and on a panic.

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
task is bounded by the join, which comes before the function returns. A variable whose type is not sendable (a `shared[T]`, for example) may not be used by a scoped task at all, whether it is taken or captured (E2010), and the starting function may not use it while a task that uses it may run.

- **Variables that end before the join are taken.** A captured variable whose block may end before the
  task is joined is **taken** by the task when `s.Task` runs: a value is copied, an owner is moved, and
  the task uses its own copy. A variable is taken when it is declared in a block nested inside the
  Scope's block (a loop body, or the variables of a loop, such as `i` in `for i := ...`), unless an
  `s.Wait()` later in that same block joins the task before the block can end, with no `return`, `break`,
  `continue`, `goto` or label between the statement holding the call and the `Wait`. Each task therefore
  sees the value its own iteration had. A taken variable must be sendable (section 19.2). A taken owner is no
  longer the starting function's (using it afterwards is a use after move, section 18.5), and the task
  drops what is left of it when its literal returns.
- **A taken variable that the task changes** (assigns, increments, or takes the address of), and that the
  starting function uses again after the `s.Task` call (later in the source, or anywhere in the loop
  around it), is error E3407: the starting function would not see the change. Owners are exempt, since
  the starting function cannot use a moved owner.
- **Views of storage that ends before the join** are error E3401. Taking a variable copies it, not what a
  view in it points to: a slice of an array declared in the loop body, taken into a task that runs past
  the iteration, still looks at that array. Use the array itself in the literal, which the task then
  takes, or wait inside the loop.
- **Owners.** This is the one rule for owners that scoped tasks capture. An owner the task takes is the
  task's own: the starting function holds nothing in it afterward, and the literal may use it, move it
  on, or let the task drop what is left of it. A captured owner whose block outlives the join stays where
  it is, and the starting function drops it at the end of its block: the literal may use it, call its
  methods, take its address and assign it a new value, but moving it inside the literal is E3305, as in
  any literal. While a task that uses such an owner may run, the starting function may not move or drop
  it either (E3404, below). Sections 11.4 and 18.5 cite this rule.
- **Func values.** A literal written as the argument of `s.Task` gets a closure record of its own in each
  task, built on the task's stack, holding the addresses of the shared variables and its own copies of the
  taken ones.

**Sharing.** During an interval, every place that a scoped task uses and that anyone writes (the
starting function, a task, or several tasks of one `s.Task` in a loop) must be one of the forms below.
Dropping, freeing or moving an object, and replacing or dropping an owner that holds it, is a write of the
whole object here too (section 19.3, Frees are writes):

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

**Partitions.** In these forms, `i` is the task's own copy of the variable of a loop around the `s.Task`
inside the Scope's block, and every such loop's variable indexes the access (so `grid[y][x]` on a
`[H][W]T` array, with tasks started in two nested loops, is partitioned). A loop's variable tells its
iterations apart when it is the key of a `range` over an integer, an array, a pointer to an array, a slice
or a string, or the variable of `for i := a; cond; i++` (or `i--`, `i += c`, `i -= c`, the last two for
`int` and `int64` only) that the loop changes only in its post statement and whose body has no label.
Different constant indexes (`x[0]`, `x[1]`) are different elements for any two parties, and nothing
writes `x`, `k` or `m` during the interval:

| Accesses in the task | Loop | Why the parts are disjoint |
|---|---|---|
| `x[i]` and its fields | `for i := range ...`; `for i := a; cond; i++` (or `i--`, or `i += c`) | each iteration has a different `i` |
| `x[i*k : i*k+k]` or `x[i*k : (i+1)*k]`, then anything within that window | as above | windows of `k` elements at different multiples of `k` |
| `x[i : i+c]` or `x[i : min(i+c, n)]` | `for i := a; cond; i += c` | windows start `c` apart and are at most `c` long |
| `x[:m]` in one party, `x[m:]` or `x[m+d:]` in another | any | the two parts meet at `m` |

Here `c` and `d` are positive constants, or for `c` a variable the loop does not change, which is checked
to be positive before each task starts (the program panics otherwise). The steps `i += c` and `i -= c` of
these loops, and the additions, subtractions and multiplications in the index and slice expressions of a
task that use their variables, panic on overflow, so that parts cannot wrap around onto each other. A
closure that passes `&x[i]` or a window of one of these forms to a named function counts as making that
access. Bounds checks keep a task inside its window, so once it has one it may index within it freely,
also through a local variable that holds the window. A partition covers the storage of the elements, not
memory that pointers in them point to. Writes that the compiler cannot prove disjoint are error E3403; a
`[][]T` written as `grid[y][x]` is one, since two rows may be the same slice, and a flat slice with a
window per row, or a local `[H][W]T` array, is the fix. Indexing a library container (`results[i]` on a
`vec.Vec`, section 11.11, Indexing library containers) is not partitioned: tasks that write one are
E3402 or E3403.

**Memory reached through views.** A captured slice or pointer is shared like the variable holding it, and
so is what it points to. Two views of memory that the starting function's caller, a package variable or
the heap provides (through parameters, package variables, or pointers the compiler cannot follow) may
point to the same memory, so a write through one such view of type `T` conflicts with an access by
another party through another that can reach a `T`. In safe code memory of one type is never seen as
another type, so `results []Result` and `items []Item` never conflict. Two slices of one type may overlap, and then an
access through one conflicts with an access through the other (E3402), with one exception. When, in every
task of the loop, two views that may overlap are each accessed only in the same partition form, with the same
loop variable and parameters (`out[i]` and `in[i]`), the program checks before each `s.Task` call that the
two views either start at the same element or do not overlap, and panics otherwise.

```go
for i := range in {
	s.Task(func() { out[i] = f(in[i]) }) // checked when each task starts: in and out
}                                       // start at the same element or do not overlap
```

Any other access to two views that may overlap stays E3402: `out[i] = f(in[i+1])` is an error. The check is
always performed, in kernel and unchecked builds too, as the check that a variable step is positive is: it
is a race check, and the unchecked mode (section 21.5) removes only generation and epoch checks.

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
drops the old one) is error E3404, since the task could use freed memory. The same holds for an owner
that holds memory a running task reaches through a view: after `p := &*n.child` and
`s.Task(func() { use(p.x) })`, the assignment `n.child = new(Leaf)` during the interval is E3404, because it
drops the object `p` points to (section 19.3, Frees are writes). Moving a Scope on which the
function calls `Task` (assigning, passing or returning it) is error E3405: the tasks find the Scope at its
address. A Scope that moves while tasks it started may run, which the compiler cannot see when a helper
started them through a pointer, stops the program. Calling `s.Drop()` by hand is allowed, and waits.
Reading a variable nobody writes, and calling `s.Task` again, are allowed.

**Deferred calls.** A deferred call can run while a scoped task is not joined: when the function returns
before the join, since deferred calls run before the function's variables are dropped (section 18.6), and
when the starting task panics, since a panic runs the armed deferred calls while other tasks keep running
(section 17.2). So a deferred call counts as a statement of a task's interval when a path runs its `defer`
statement and then reaches a point of that interval, or a return before the task is joined. It is checked
by the Sharing rules above and by E3404, with the accesses of its callee or its literal. The rule is the
same in every build mode, so a program compiles the same way whether or not its panics run deferred calls.

```go
func fill() {
	w := vec.New[int]()
	defer w.Drop()                 // error E3404: may free w while the task pushes
	var s sync.Scope
	s.Task(func() { w.Push(1) })
	s.Wait()                       // a panic before this line runs the deferred Drop
}
```

The fix is to arm the deferred call where no task it conflicts with can run: after `s.Wait()`, or before
a deferred `s.Wait()` that comes ahead of the first `s.Task`, which then joins first. An owner may also be
left to the end of its block, since Scopes are dropped before the other variables of their block. Calling
`s.Wait()` before the return is not enough on its own: a panic between the `s.Task` and the `Wait` still
runs the deferred call while the task runs.

```go
func fill() {
	w := vec.New[int]()
	defer w.Drop()
	var s sync.Scope
	defer s.Wait()                 // runs first: joins before w is dropped
	s.Task(func() { w.Push(1) })   // allowed
}
```

A deferred `s.Wait()` that runs for a panic waits for the scoped tasks as it does on a return. If a scoped
task waits for something only the panicking task would do (a send, a `close`, an `Unlock` that no
deferred call makes), the wait never ends, and the program ends with a deadlock report instead of the
panic's report. The deadlock report then names the panic in progress, with its message and where it
happened (section 19.1, Deadlock), so the cause is not lost. A scoped task that may wait for the starting
task is given a way to stop: close its channel, or cancel it, in a deferred call armed after the deferred
`s.Wait()`, which runs before it.

**Non-local exits.** Every path out of the Scope's block drops it, so `return`, `break`, `continue` and
`goto` join. `rt.ExitTask` leaves every frame through its ordinary return path, so the Scope's drop joins
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
methods allocate and free task records and stacks, with a guard region of at least one page below each
stack whose size the compiler did not prove (`NewTask`, `FreeTask`; abi.md section 12.1), make tasks runnable (`Spawn`,
`Wake`), and switch away from the running task (`Park`, `Yield`). Optional methods give the runtime an
initialization hook (`Init`), a way to refuse waits in interrupt context (`InInterrupt`), and a clock and
a timer (`Now`, `ArmTimer`, section 19.6).

**Binding.** The backend is bound when the program is linked: a package-level variable marked
`//pu:scheduler`, whose type (not an interface) implements `rt.Scheduler`, and the runtime calls its
methods directly. Hosted builds use the runtime's own backend unless the program binds one. A freestanding
program that starts tasks, performs a channel operation or `select` that may wait (section 19.8, Waiting), or waits in `pluto/sync` without binding a backend fails to build
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
interrupt handler, the panic handler or the backend's `Wake` can reach may wait: a send, a receive, a `range` over a channel or a `select` without a `default` case, a lock that parks, `time.Sleep`, `s.Wait` or a `sync.Scope`'s
drop, or an allocator whose `Alloc` or `Free` may wait, including through a drop that the compiler inserts (E2584); nor may it start a task (E2585). Operations that do
not wait are allowed: a `select` with `default`, `close`, `len` and `cap` of a channel, `TryLock`, `rt.Ready`, atomics, and the runtime's
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

**Who owns a timer.** `time.NewTimer` and `time.NewTicker` return an `owned[Timer]` or `owned[Ticker]`, an
owner like any other: when it is dropped, at the end of its variable's scope unless it was moved, the timer
is stopped and freed. `time.AfterFunc` returns a `synced[Timer]` handle instead: a counted reference whose drop releases a count and does not stop the timer, in contrast with the `owned[Timer]` of `NewTimer`, which is stopped on drop. While the timer is armed
the runtime holds a reference of its own, released when `f` is started or when `Stop` succeeds, so dropping
handles does not stop the timer, and the timer is freed after the last reference is released. The channel in its field `C` is a counted reference, so a
receiver that copied it keeps the channel, not the timer. `time.After` and `time.Tick` return only a
channel, and that channel owns its timer: when the last reference to the channel goes, the timer is
stopped and freed with it, so `time.After` in a `select` frees its timer when the `select` drops the
temporary channel. The runtime's own link from its timer heap to the channel is not counted. Once `Stop`
or `Reset` has returned, a timer's channel holds no value from before the call. No timer is freed by hand:
nothing in the library has a `Free` that a program must call.

`time.AfterFunc(d, f)` starts `f` on a task of its own once the timer expires. Like a function given to a
task statement, `f` must not be a closure of a frame (section 12.12, Task statements). Discarding the result, as in the
statement `time.AfterFunc(d, f)`, leaves the timer armed; `Stop` through any handle cancels it. The function does not run
where the timer fires, which may be an interrupt handler in a kernel: an expired timer is queued for a task
of `pluto/time`, which starts each function.

**The mechanism.** The runtime's clock is `rt.Now()`, monotonic nanoseconds, the same on every processor.
An `rt.Timer` calls a function once its deadline has passed, and again every period if it has one;
arming, stopping and firing one never allocate or block, and its function must not block, allocate, or arm, stop or reset a timer (the lock of the timer heap
is held while it runs, and `Stop` waits for a running function). `rt.ParkUntil(deadline)` is `rt.Park` with a deadline. A kernel's backend provides the clock and
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
  happens before the `(n+1)`-th `Lock` returns, with the matching rules for `RWMutex` and `WaitGroup`.
  The return of the function that `once.Do(f)` runs to its end happens before the return of every call
  of `once.Do`; `Do` returns only after such a return, and a function that ends its task with
  `rt.ExitTask` leaves the Once not done, so the next `Do` runs its own function. A `sync.Cond`'s
  `c.Wait(l)` unlocks `l` and locks it again before it returns, so it synchronizes through `l` as `Lock`
  and `Unlock` do, and a `Signal` or `Broadcast` that wakes a `Wait` happens before that `Wait` returns. The return of a scoped task happens before the return of the `Wait` or drop that joins it.
  When package cleanup runs (section 15.4), every operation of every finished application task and
  callback happens before it begins. This supplies visibility across processors, without programmer
  fences or annotations, and is not an ordinary running-program join.
  `pluto/sync/atomic` operations are sequentially consistent.
- **Safe code has no data races** (section 19.3). The race rules use locks, atomics, moves, scoped
  joins and the return of `Once.Do` to prove it, not the order that channels and `WaitGroup` give to
  plain data. A lock orders the critical sections of the tasks that take it only because each `Unlock`,
  `RUnlock` and `Cond.Wait` releases a lock its own task holds, which the compiler checks (section 19.3,
  Releasing a lock). So the `n`-th `Unlock` above ends the critical section of the task whose `Lock` began
  it.
- **Unsafe code** can race. A race is a bug that gives torn or stale values, never undefined behavior in
  the compiler's sense: a racy read of a naturally aligned value of at most a word observes some value
  written to it, and larger values (strings, slices, interfaces, structs) may tear. The compiler may not
  assume that a program has no races. A torn pointer or length can then break memory safety
  (memory-model.md section 4.3).
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

**Counting.** A channel is a counted reference whose last release drops the values still buffered and frees
the channel (section 7.11, Channel types). Its count is plain or atomic by the rule of section 18.7 (Shared
ownership, When counts are atomic), and a task waiting on a channel holds a reference to it while it waits.

**Waiting.** The operations that may wait (a send or receive, a `range` over a channel, a `select` without
a `default` case) park the calling task, so its processor runs other tasks. A `select` with a `default`
case, `close`, `len` and `cap` never wait, which is what lets interrupt handlers use them (section 19.5).
A task parked on a channel is listed in a deadlock report with what it waits for: a receive, a send, a
`select`, or one of these on a nil channel.

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
