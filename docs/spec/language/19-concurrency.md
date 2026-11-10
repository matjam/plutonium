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
preemption that arrives meanwhile happens when it is enabled again. Such code may not wait: a wait on an exact
path inside a non-migrating region is a compile error (section 19.3, Waiting in a non-migrating region), and any other
wait there panics with "blocking with preemption disabled" (section 17.1), in kernel mode also inside an interrupt-disabled region. Blocking on a channel, a lock or I/O,
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
- a variable that a scoped task uses, whether it takes it or captures it (section 19.4).

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
that statement on, writes are checked against the handlers like writes in any task.

**Accesses and locks.** For each task the compiler collects what it reads and writes, in its own code and
in every function it calls (a call through an interface or func value reaches every function that could
be called there), and which locks are held at each access:

- A lock is a `sync.Mutex`, a `sync.RWMutex` or an `rt.Lock` (a `sync.Mutex` cannot be taken in an interrupt
  handler, E2584, so a handler uses `rt.Lock`). It is held from `Lock` (or `RLock`, or a `TryLock` that
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

**Waiting in a non-migrating region.** An operation that may park the task is a compile error where it is reachable
inside a non-migrating region: between `rt.DisablePreemption` and `rt.EnablePreemption`, while an
`rt.Lock` is held, or while maskable interrupts are disabled. The operations are the waiting operations
of section 20.10 (Interrupts): a channel operation that may block, `Lock` of a `sync.Mutex` or a
`sync.RWMutex`, `Wait` of a `sync.WaitGroup`, a `sync.Cond` or a `sync.Scope` (and the `Drop` of a Scope), `time.Sleep`, `rt.Park`, a
function marked `//pu:blocking`, and an allocation from an allocator that may wait. A task that parks
there would hold its processor, or a lock that other processors spin on, while it waits for something
that may need that processor or that lock.

The compiler follows calls from each non-migrating region as section 20.10 (What is followed) describes, the drops it
inserts and the drop entries of arenas included, and reports the first path it finds, naming each call
on the way. It reports only exact paths: the wait is written in the region, or every call on the way
must reach a wait whichever function runs there. A direct call, a call of a method of a known type, a
deferred call, a hook and a function literal called where it is written are exact. These compile, and
the run-time check below catches them when they wait:

- a call through an interface or a func value where only some of the functions it may reach may wait;
- an allocation, or a drop that frees, where only some of the allocators the program installs may wait.

The check needs a non-migrating region on every path, as above, and it starts only from such regions in code outside
`pluto/rt`: the runtime's own locked code is unsafe code (section 18.12), and a path from a program's non-migrating region into the runtime still ends at its `park` (section 20.10). A wait that the check does not report,
such as one reached on only some paths through a non-migrating region, through one of the calls above, or from code
outside Plutonium, fails at run time. `rt.Park` panics with "blocking with preemption disabled" when the
task's preemption is disabled, and in kernel mode also when maskable interrupts are disabled, so an interrupt-disabled region opened with `x86.Cli` is covered too (section 17.1; abi.md section 12.4). In hosted builds, where
`x86.Cli` faults, and in kernel mode, every wait in a non-migrating region therefore fails to compile or
panics when it is reached; none hangs the processor in silence. Other freestanding builds do not test
the interrupt flag (abi.md section 12.4).

The check for interrupt handlers (E2584, section 20.10) stays conservative through interfaces, func
values and allocators, and this one does not, for a reason. A wait in a handler has no safe outcome at
run time: the handler runs on top of the code it interrupted, which cannot resume, so the processor
stops. A wait in a non-migrating region of a task fails loudly at the panic above. Calls through operations tables and
allocation while a lock is held are ordinary in a kernel, and a conservative rule would reject them.

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
arena it may be, within the limits of section 18.3 (How precise the frame check is). This holds for rules
1 to 3 below, for the type rule, and for scoped tasks (section 19.4). So
an object reached through an owner can change while some code may replace, drop or move that owner, and in
safe code no task frees memory that another task may still use (memory-model.md section 4.1).
Values of `pluto/sync` and `pluto/sync/atomic` types synchronize themselves through their methods, and
assigning a whole new value to one is an ordinary write.

**Rule 1: package variables.** A package variable, or a field or element of one, that two tasks may use,
or that the tasks of one task statement may use while several run, with at least one of them writing it
after tasks have started, must be atomic or guarded. Per-CPU variables follow section 20.9 instead.

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
first task, may be read by every task without a lock.

**Rule 2: views that cross.** A pointer or slice that crosses to another task (section 19.2) must point
to memory that is safe for both:

- memory of a `pluto/sync` or `pluto/sync/atomic` type, a runtime record (`rt.Task`, `rt.Timer`), a
  channel, or a struct of only those (`task worker(&wg)` with a package-level `wg`);
- a package variable, or part of one, that no task writes once tasks have started;
- the object of a `synced[T]` (rule 3);
- an object of a struct type that satisfies the **type rule**: no code overwrites a whole object of the
  type through a pointer, or frees, drops or moves one (or replaces or drops an owner that holds one) once
  tasks have started, and each field either never changes once an object is shared, or is used only
  atomically, or only under one lock of the same object or one package-level lock, inferred as above.
  Writes that initialize an object do not count: writes through a pointer to memory the function
  allocated (or that a function returned after allocating it and keeping it to itself, such as a
  constructor), made before the pointer is stored, passed to a function that keeps it, sent or returned,
  including writes by a function it is passed to for that purpose. So an object built by its constructor
  and then only read, and an object whose methods all hold its own `sync.Mutex`, may both be shared.
  An object that some code frees while tasks run is not safe for an unscoped task by this rule, since the
  task could still use it: such an object is shared as the object of a `synced[T]`, whose last release
  frees it after every use (rule 3), or handed over by moving its owner.

A pointer to, or slice of, anything other than a struct crosses only in the first three cases. Anything
else is error E3501, which names a write that could change the memory while the other task uses it. The
fixes: pass a copy of the value; move an owner (a `vec.Vec` instead of a slice) so that only the receiver
has it; or keep the shared data where rules 1 and 3 guard it.

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
neither. The guard inference recognizes an `rt.Lock` as it recognizes a `sync.Mutex`. A `volatile[T]` access, and an
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
preemption disabled. The runtime's non-migrating regions are not checked for waits (Waiting in a non-migrating region, above). A data race in unsafe code gives torn or stale values (memory-model.md section 4),
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
  happens before the `(n+1)`-th `Lock` returns, with the matching rules for `RWMutex`, `WaitGroup` and
  `Once`. A `sync.Cond`'s `Wait` unlocks its lock and locks it again before it returns, so it synchronizes
  through that lock as `Lock` and `Unlock` do, and a `Signal` or `Broadcast` that wakes a `Wait` happens
  before that `Wait` returns. The return of a scoped task happens before the return of the `Wait` or drop that joins it.
  When package cleanup runs (section 15.4), every operation of every finished application task and
  callback happens before it begins. This supplies visibility across processors, without programmer
  fences or annotations, and is not an ordinary running-program join.
  `pluto/sync/atomic` operations are sequentially consistent.
- **Safe code has no data races** (section 19.3). The race rules use locks, atomics, moves and scoped
  joins to prove it, not the order that channels and `WaitGroup` give to plain data.
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
