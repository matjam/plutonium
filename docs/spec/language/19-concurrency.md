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
marked `//pu:blocking` is called, in kernel mode also inside an interrupt-disabled region. Nor may it
call `rt.Yield`, which is checked the same way (section 19.3, Switching tasks in a non-migrating region).
Blocking on a channel, a lock or I/O, or calling `rt.Yield()`, gives up the processor early.

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

A value that **crosses** to another party (section 19.3, Crossings) must be **sendable** (section 8.8,
Sendable types), and it may not hold a view that the other party could use while the memory changes:

- A type that holds a `shared[T]` or a `weak[shared[T]]`, directly or through a field, element, payload,
  box or pointer, is not sendable (section 8.8). A task statement whose arguments or receiver have such a
  type, a channel type whose element is such a type, and a send of such a value are error E2010.
  Converting a value of such a type to `error`, or to an interface that embeds it, is error E2020 at the
  conversion, since every `error` is sendable.
- A view of the starting or sending function's frame is E3004 for a task statement and E3005 for a send
  (E2011 where the type checker sees it directly).
- A view of heap or static memory must meet rule 2 of section 19.3 (E3501).

Moving an owner to another party is safe because the sender loses it: only one party can use it
afterwards, and an owner that manages memory (`vec.Vec`, `hash.Map`, `strings.Builder`) takes that memory
with it (section 8.8). A `synced[T]` and a map let several parties reach the same memory, so what changes
in it is subject to section 19.3. Strings are sendable because their bytes never change and their counts
are atomic once a program has started a task (section 18.7, Shared ownership). A string made with
`unsafe.String` is not counted (section 18.8); sending one is the responsibility of the unsafe code that
made it, which must keep its bytes alive and unchanged (section 18.12).

The type part of the rule (`shared[T]`) is checked by the type checker on the arguments and receiver of
every task statement and on the element type of every channel type. It looks through struct fields, array
elements, union payloads, the referents of boxes, pointers and slices, and the transparent wrappers
`ring`, `volatile` and `aligned`. The view part is checked by the frame rules (section 18.3, Views) and by
rule 2 of section 19.3 wherever a value crosses. Through an interface value it is checked for every type
the program converts to that interface, except `error` and the interfaces that embed it, which are
sendable by definition (section 8.8). A conversion whose result is a view of the frame that made it
(section 18.3) is not counted, since such a value cannot cross. In a generic function the rule is checked
for each instantiation.

### 19.3 Data race freedom

A **data race** is two accesses to the same memory by different parties (tasks or interrupt handlers), at
least one a write and at least one not atomic, that are not ordered by the memory model (memory-model.md
section 2). Safe code has none: the compiler rejects every program in which two parties could make such
accesses. It checks the whole program when it compiles, with no lifetime, borrow or region annotations.
Everything it uses is inferred from the program. Every piece of memory that parties share is then one of
four things: owned by one party (moved there), never changed while shared, used only atomically, or
guarded by a lock that every access holds.

This section first defines the model the rules use, each term once. Then it states the rules, each with
its error. It ends with the limits of the analysis and a table of programs with their results.

**Parties.** A **party** is code that may run at the same time as other code:

- the **main task**, which runs package initialization and then `main`;
- each **task** the program may start: by a task statement (section 19.1), by a func value given to a
  function that starts a task with it, by `s.Task` of a `sync.Scope` (section 19.4) and by `wg.Task` of a
  `sync.WaitGroup`;
- each **interrupt handler** the program installs (section 20.10).

The compiler finds the parties from package initialization and `main`: a task statement in a function
that no party calls starts no party. A party is **repeated** when its start may run more than once: a
task statement in a loop, or in a function that may be called more than once, starts several tasks that
run the same code at once. Every interrupt handler is repeated, since it may run on several processors at
once. A function that `time.AfterFunc` starts is repeated wherever the call is: a `Reset` through any
handle of its timer starts it again, and a new run may overlap one still going (section 19.6). Two
instances of one repeated party count as two parties below.

The function of an `rt.Timer` runs wherever timers run (section 19.6). `Timer.Init` takes an
`unsafe.Pointer`, so the code that arms such a timer, and the timer's function, are unsafe code (section
18.12): the function is not a party of these rules, and what it shares is the responsibility of that code
(What is not checked, below).

A task, an interrupt handler, and a function that `s.Task`, `wg.Task` or a task statement starts, begin
with no lock held, whatever the code that starts them holds.

**The start of sharing.** The **start of sharing** is the first statement, in the main task's order
through package initialization and `main`, that may start a task, install an interrupt handler, or enable
interrupts (`x86.Sti`, `x86.StiHlt`, `x86.RestoreFlags`), itself or through the calls it makes. Package
initialization may not start a task (section 15.2, Panics and tasks). An access of the main task is
**before the start of sharing** when no path to it passes such a statement; an access in a deferred call
is before it only when no path from the `defer` statement passes one either (Held sets, below), so a
deferred call of `main` armed ahead of the first task start runs after the start. A function that the main task
calls both before and after the start of sharing counts as called after it. Accesses before the start of
sharing do not count in the rules below, and loads before it do not cross.

**Part of the enclosing function.** Three kinds of code are **part of the enclosing function**, at the
point where they run:

- a function literal called where it is written: `func() { ... }()`;
- a deferred function literal: `defer func() { ... }()`, which runs where the function leaves (Held
  sets, below);
- the body of a range-over-func loop (section 12.11), when the iterator calls `yield` only on the party
  that runs the loop. The iterator counts as a call around the body.

Such code runs on the party of the function that contains it. It shares that function's held set and
releases (Held sets, below) and its calls of `Once.Do` (Lazy initialization, below). Any other function
literal is a func value.

Whether an iterator calls `yield` on another party is part of its summary (Held sets, below). The summary
records a call of a func-valued parameter, or of a copy of one, **on another party** when the call is in a
function given to `s.Task`, `wg.Task`, a task statement or a function that starts a task with it, or when
the parameter is passed to a callee whose summary records such a call. A copy the compiler cannot follow
counts as called on another party. When the iterator is a func value or a method called through an
interface, every candidate counts, and a candidate the compiler cannot know counts as calling `yield` on
another party.

A loop body whose iterator may call `yield` on another party is a func value that the iterator's party
runs. It starts with no lock held. A `defer` in it is a compile error, since the deferred call would run
on the wrong party at the wrong time. If two tasks may call `yield` at once, the body follows section
19.4, Callees (E3406).

For a body that is part of the enclosing function, the locks held at its start are those held at the loop
statement, less the locks the iterator may release, intersected with those held at the end of the body on
every earlier run (as for a loop, Releasing a lock). When the body may release a lock, every access of
the iterator counts as made without the caller's hold of that lock, before its first call of `yield` as
after it; locks the iterator takes itself are counted as usual. Each call of `yield` counts as a call that
may release the lock. This needs nothing in the iterator's summary beyond its accesses and its calls of
`yield`.

To these the body adds the locks that the iterator's summary shows held at every call of `yield` (Held
sets, below), each in the same mode and under the caller's name for it, as for a lock a callee returns
holding. For an iterator that a method returns, as in `for _, p := range sc.All()`, the names map
through the capture rule of Locks and names (below): a lock `s.mu` of `All`'s closure, with `s` the
receiver, is `sc.mu` in the body. A lock that has no name in the caller is not added. The body holds these locks for its accesses
and for its views, not for releases: it may not release them, just as a func value that the iterator
calls may not (Releasing a lock). A view that the iterator passes to `yield` counts as loaded at that
call under those locks, so it is valid in the body while they stay held (Views loaded under a lock).

So a body and the same code written as a function literal passed to the iterator get the same held set
for accesses, the same valid views and the same Onces (Lazy initialization, below). They differ only
where a body can do what a func value cannot: release a lock held at the loop statement, `defer` to the
end of the enclosing function, and leave it with `break` or `return`.

**Memory and data.** The rules work on **data**. A field of a struct is one datum. All the elements of an
array or slice together are one datum, except that elements selected by different constant indexes
(`table[0]`, `table[1]`) are different data. An element selected by a variable index counts as every
element.

**The memory an object owns** is the object of each `owned[T]` it holds, and the memory managed by each
owner value it holds (the storage of a `vec.Vec`, the table of a `hash.Map`, the buffer of a
`strings.Builder`), through owners that hold one another, to any depth. It is not what a pointer, a
`shared[T]` or `synced[T]` reference, a map or a channel in it refers to. The memory a datum owns is part
of that datum: a read of it reads the datum, and a write of it writes the datum.

These are the kinds of memory that several parties can reach, each with its owner:

- A **package variable** is data, with the memory it owns.
- A **static object** is an object that a package-level initializer creates as a composite literal whose
  address it takes (`var Stdout = &File{fd: 1}`), so that it lives in static storage (section 18.2), and
  that no code moves, drops or overwrites as a whole after the start of sharing. Each of its fields is a
  datum, with the memory it owns. The compiler names the target of a `take(p)` or of an assignment
  `*p = v` by where the pointer came from (Locks and names, below); one it cannot name counts as a move of
  every static object of the type. Nothing frees static storage, and its cleanup runs only in package
  cleanup, after the tasks have ended (section 15.4).
- The object of a **`synced[T]`** is shared by every party that holds a reference to it. Each field is a
  datum, with the memory it owns. Calling a pointer method on the box (`task s.Serve()`, `s.Add(1)`) uses
  the object.
- The **contents of a map** are shared by every copy of the map. The contents of all maps of one type are
  one datum once a map of that type crosses (Crossings, below), or once a party reaches one through a
  package variable or through memory that neither its own code nor a function it called on its own party
  allocated. A map **kept in place** (below) is not part of that datum.
- A **per-CPU variable** has one copy per processor and follows section 20.9, with the memory it owns.
- **Frame memory** of a function that starts scoped tasks follows section 19.4.
- An **arena** (`arena.Fixed`, `arena.Chunked`) owns every object that its `New` or `Slice` allocated.
- Any other object is reached only through a view that crossed, and rule 2 decides it.

**Maps kept in place.** A **place** is a package variable of map type, or a field of map type of a struct
type (one field declaration, for each instantiation of a generic type), with every object of that type
holding one place of it. A place is **kept in place** when, in the whole program:

- every value stored in it is new: the value of a `make` call, of a map composite literal or of `nil`,
  written directly as the right side of the assignment, as the initializer, or as the field's element of
  a struct composite literal; and
- every other occurrence of the place in an expression (`c.m`, or `m` for a package variable) is one of
  these uses, which do not copy the map value: the map operand of an index expression (read, assignment,
  compound assignment, or the comma-ok form), the argument of `len`, `delete` or `clear`, the range
  expression of a `for` statement, an operand compared with `nil`, or the argument for a parameter or
  receiver that keeps it in place (below).

Every other occurrence **copies the map out**: an assignment of it to anything else, its address (`&c.m`),
a conversion to an interface, a return, a send, an argument of a task statement, an element of a
composite literal, an argument for a parameter that does not keep it in place or of a call through an
interface, a func value or a method value, and a copy of a struct value that holds the place. Moving an
owner that holds the place is not a copy: the place moves with it. A parameter or receiver of map type
**keeps it in place** when the function uses it only in the ways listed above and never assigns it; the
summary records this for each such parameter and receiver, and on a cycle of calls a parameter keeps it
in place unless some use on the cycle copies it out.

The contents of the map in a place kept in place are reached only through that place. They are part of
the datum of the place, per object, with the memory they own, and a lock that guards the place in place
guards them (Locks and names, below). A copy of a struct that holds a lock is already a compile error
(Copying a lock, below), so the common shape, a struct with a mutex and a map, loses nothing to the last
rule of the list. The contents of any other map stay in the datum of its type.

One copy anywhere in the program takes a place out of this, so an error about the contents of a map
must point at it. When the datum of an E3502 or E3503 is the contents of maps of a type, and the access
that the error names goes through a place that is not kept in place, the error names the first
occurrence of that place (by package path, then file name, then position) that copies the map out or
stores a value that is not new. It says that this occurrence takes the map out of its place, so that the
place's lock no longer guards the contents: "`return c.m` copies the map out of `Cache.m`; its contents
are now one datum for all `map[string]int` values, which `c.mu` does not guard". The occurrence may be in
another package.

**Crossings.** A value **crosses** to a party when it is

1. an argument or the receiver of a task statement (section 19.1), or a func value given to a function
   that starts a task with it;
2. sent on a channel, or received from one;
3. held inside an interface, struct, array or union that crosses;
4. a variable that a scoped task uses, whether it takes it or captures it (section 19.4);
5. loaded by a party from memory that several parties can reach, when the value holds a view: a load from
   a package variable (a per-CPU variable included), a static object, the object of a `synced[T]`, the
   contents of a map, or memory reached through one of those or through a view that crossed. The load
   counts in the party's own code and in every function it calls, an atomic load included.

A value **holds a view** when it is a pointer, a slice, an interface, a func value or a method value, or a
struct, array or union that holds one of them. Crossings count only after the start of sharing.

**Accesses.** For each party the compiler collects what it reads and writes, in its own code and in every
function it calls. A call through an interface or a func value reaches every function that could be
called there. A method value may be called later, anywhere, so its method counts as run outside every
`Do` (Lazy initialization, below). Like any call through a func value, a call of a method value runs the
method with the caller's held set (Held sets, below). An access made with `pluto/sync/atomic` operations
(the methods of `atomic.Int64` and the other atomic types, or functions such as `atomic.AddInt64(&x, 1)`)
is **atomic**. Writing a whole struct writes all its fields.

A call of a method of a `pluto/sync` value (`mu.Lock()`, `wg.Add(1)`, `o.Do(f)`, `c.Wait(&mu)`), and a
send, receive or `close` on a channel, is an access of the memory of that value, or of the channel's
object, and it counts as an atomic write: the value synchronizes itself. Such an access does not
conflict with another such call or with an atomic access. It conflicts with a plain write that changes
the value's own memory: a whole new value assigned to it, or a free, move or overwrite of an object or
owner that holds it (Frees are writes, below). A write of other memory of the same datum, such as another
field of an object that the datum owns, does not conflict with it. So when such a plain write is shared,
the datum has no guard (The guard set, below), whatever locks the write holds: no lock orders it with the
other party's call.

Two accesses **conflict** when they reach overlapping data, at least one writes, they are not both
atomic, they are made by different parties (or by two instances of one repeated party), and both come
after the start of sharing.

**Frees are writes.** Dropping, freeing or moving an object counts as a write of the whole object, and so
does assigning a new value to, or dropping, any owner that holds it (directly, or through owners that hold
one another). A `Reset`, `Release` or `Drop` of an `arena.Fixed` or `arena.Chunked`, the drop at the end
of the arena's scope included, takes memory back. It counts as freeing every object that `New` or `Slice`
allocated in that arena, of every type, whether or not the type has drop glue. The compiler cannot tell
which objects come after a mark, so a `Release` counts as freeing objects of every type allocated in the
arena too. The drops that the arena's drop entries run (section 18.9, Drop entries) count on top of that.
An allocation made through the arena's `Alloc` and still live makes the call panic and take nothing back
(section 18.9, Live allocations), so it is not freed. The compiler names an arena as it names a lock
(Locks and names, below). A call on an arena it cannot name, such as `workers[i].arena` with a variable
index, counts as that call on every arena it may be, within the limits of How precise the race check is
(below); beyond those limits it counts as that call on every arena of the program. The name follows a
move of the arena: after `b := take(a)`, a call on `b` frees the objects allocated through `a` before the
move. This holds for every rule of this section and for scoped tasks (section 19.4), so in safe code no
party frees memory that another party may use at the same time (memory-model.md section 4.1). Values of
`pluto/sync` and `pluto/sync/atomic` types synchronize themselves through their methods; assigning a
whole new value to one is an ordinary write.

**Locks and names.** A **lock** is a `sync.Mutex`, a `sync.RWMutex` or an `rt.Lock`. A `sync.Mutex`
cannot be taken in an interrupt handler (E2584), so a handler uses `rt.Lock`.

The compiler follows a lock by its **name**. A name is a package variable that is not a pointer, or a
local variable or parameter (the receiver included), followed by field selections and constant indexes of
arrays. Only the variable itself may hold a pointer on the way. An `owned[T]` box is not a pointer
(section 8.4), so for a variable `b` that holds one, `b.mu` and `b.n` reach the box's object with no
pointer load, here and for guards in place below: only a write of `b` itself changes what they reach,
and that write is a free of the object (Frees are writes, above). So `mu`, `s.mu`, `s.inner.mu` (with
`inner` a struct field) and `table[0]` are names. `s.next.mu` is not: it loads a pointer from memory at
each use, and another party may change that pointer in between. Copy the pointer into a local:
`n := s.next; n.mu.Lock(); ...; n.mu.Unlock()`. `locks[i]` with a variable index is not a name either:
take a pointer once and use it for both calls, `l := &locks[i]; l.Lock(); ...; l.Unlock()`.

- A local that holds a pointer names nothing if its address is taken, if a function literal assigns it,
  or if a scoped task captures it.
- A parameter names, at each call, the lock that its argument names there.
- A capture carries the name of a parameter or receiver into a function literal. When a literal written
  in a function captures one of its parameters or its receiver, and nothing assigns that variable or
  takes its address (not the function, not the literal, not another literal), the capture is the same
  name as the parameter inside the literal. So where the function returns the literal, and the result of
  a call `x.M(args)` is used directly as the range expression or called directly, the literal's names map
  to the caller's as the function's own do: the receiver and parameters to what `x` and `args` name at
  that call. If `M` may return several literals, each counts, as the candidates of a func value do. A
  result stored in a variable first is a func value, and its names are not mapped. Where the function
  passes the literal to a callee instead, the literal's names are the function's own parameter names,
  which its summary maps to its caller's.
- A copy carries the names: after `cur = next`, the lock named `next.mu` is named `cur.mu` too.
- An address carries the name: after `p := &mu`, `*p` is a name of `mu`. So `p.Lock()` takes the
  package-level lock `mu`, and it guards what `mu` guards; after `p := &s.mu`, `*p` is a name of `s.mu`.
- A name ends when the variable it starts from is assigned, and when the lock is given a new value or the
  memory that holds it is moved or assigned, in the function or in a function it calls.
- A copy of a struct that holds a lock holds a different lock (and is a compile error, Copying a lock,
  below).

A lock **guards in place** a datum when both are reached from the same base by field selections and
constant indexes with no pointer load between, or when the lock is **package-level**: a package variable,
or a field or constant element of one, reached with no pointer load. So `s.mu` and `s.items` in one method
are a lock and a datum of the same object, and so are `std.mu` and `std.buf` for a static object `std`.
The contents of a map kept in place are part of the datum of their place, so a lock that guards the place
guards them: `s.mu` for `s.m[k]`, for each object `s`, or a package-level lock for a package-level map.
The contents of any other map are one datum for the map type, which no lock of one object guards in
place; only a package-level lock can guard them.
Only such locks can guard a datum (The guard set, below). A lock in the frame of a function that starts
scoped tasks, used by those tasks, is one lock in all of them (section 19.4).

**Held sets.** The **held set** at a statement is the set of locks held there, each with its mode:
**write** (`Lock`, a `TryLock` that succeeded) or **read** (`RLock`, a `TryRLock` that succeeded).

- A lock is held from `Lock` (or `RLock`, or a successful `TryLock` or `TryRLock` in the branch where it
  returned true) to `Unlock` (or `RUnlock`), or to the end of the function after `defer mu.Unlock()`.
  The branch where it returned true is the `if` branch of `if mu.TryLock() {`, and the code after
  `if !mu.TryLock() { return }`. A result stored in a variable first is not followed.
- A lock counts as held at a statement only if it is held, under one of its names, on every path to that
  statement.
- A deferred call runs where its function leaves, and the function may leave at any point after the
  `defer` statement: at its end, in a call that ends the task with `rt.ExitTask` (section 17.3), or at a
  panic (section 17.2). So the deferred call, with its literal's body or its callee, counts as made at its
  `defer` statement and at every point after it. It holds a lock only if the lock is held at all of those
  points and no deferred call that runs before it releases the lock. Every other fact these rules find on
  every path to a statement counts a deferred call the same way: the regions below, the returned `Do` of
  Lazy initialization, valid views, and the start of sharing. Section 19.4 (Deferred calls) states the
  same for the join of scoped tasks.
- A lock held around a call counts as held inside the callee, for the callee's accesses. A function that
  returns with a lock held holds it for its caller, under the caller's name for the argument: after
  `x.lock()`, `x.mu` is held.
- `c.Wait(l)` of a `sync.Cond` counts as `l.Unlock()` followed by `l.Lock()`. `Signal` and `Broadcast`
  need no lock, and they neither take nor release one.
- A `Lock` of a lock that has no name holds nothing.
- A call through an interface, a func value or a method value runs its candidates with the caller's held
  set for their accesses. For releases (Releasing a lock, below), each candidate counts as called with
  nothing held, so it may release only the locks it takes itself.
- A generic function is followed for each instantiation, and a method of a type parameter is that method
  of the type argument.

Each function has a **summary**, which a call uses in place of the callee's body:

- its accesses, each with the locks held at it relative to the callee's start;
- the locks it **returns holding**;
- the locks it **releases**, which its caller then no longer holds;
- the locks it **may release**: a lock released on some path, directly or by a callee, `c.Wait(l)`
  included, to any depth, even if it is taken again before the return;
- for each func-valued parameter, the locks held at every call of it or of a copy of it, each with its
  mode, relative to the callee's start (Part of the enclosing function, above); a copy the compiler
  cannot follow counts as called with no lock held;
- the func-valued parameters it calls **on another party** (Part of the enclosing function, above), and
  those it calls from **several scoped tasks at once** (section 19.4, Callees);
- the Onces whose `Do` it calls on every path to its return, to any depth through calls with a known
  target (Lazy initialization, below);
- for each view it returns, the locks under which it was loaded and that stay held to the return with no
  ending event (Views loaded under a lock, below).

The analysis may show fewer locks held than a run holds, never more. A lock it does not show held counts
as not held.

**Non-migrating and interrupt-disabled regions.** These two kinds of region are found the way held locks
are: on every path, across calls, with a deferred release counting as a release at the end of the
function. A deferred call is inside a region only when the region holds at its `defer` statement and at
every point after it (Held sets, above).

- A **non-migrating region** is a stretch of code where, on every path, preemption is disabled
  (`rt.DisablePreemption` to `rt.EnablePreemption`), maskable interrupts are disabled (`x86.Cli` to
  `x86.Sti`, `x86.SaveFlagsCli` to `x86.RestoreFlags`), or an `rt.Lock` is held, and the body of a
  `//pu:interrupt` handler up to the end of its interrupt-disabled region (below). The task cannot move to
  another processor inside it.
- An **interrupt-disabled region** is a stretch of code where maskable interrupts are disabled by those
  calls, or an `rt.Lock` is held in kernel mode (abi.md section 12.5: runtime locks disable interrupts
  there), or the code is the body of a `//pu:interrupt` handler up to a call that may switch tasks
  (below) or to a call of `x86.Sti`, which ends it. A handler is entered through an
  interrupt gate that disables interrupts (section 20.10), so its body is such a region. The body of a
  handler marked `//pu:interrupt trap` is not: a trap gate leaves interrupts as they were, so the handler
  can be re-entered, and it opens an interrupt-disabled region of its own with `x86.Cli` or
  `x86.SaveFlagsCli` where it needs one. Exceptions and nonmaskable interrupts are not excluded by an
  interrupt-disabled region; that remains a contract of the kernel (section 20.10).

A call **may switch tasks** when it is a call of `rt.Yield` or `rt.Preempt`, or of a function whose summary
shows that it may reach one of them, at any depth. Such a call lets the task resume on another processor,
so it is a compile error inside a non-migrating region (Switching tasks in a non-migrating region, below).

A handler body's region is the one exception: a call that may switch tasks ends it, as `x86.Sti` does,
instead of being an error, so a timer handler may count ticks and then call `rt.Preempt`. Until that
call interrupts are off and nothing switches, so the handler stays on one processor. The region ends
where held locks would be released: a callee whose summary may reach a switch counts as ending it, and
inside that callee the region lasts up to its own switch. A call through an interface, a func value, a
method value or a hook ends it when any candidate may switch, and so does an allocation or drop that
reaches an installed allocator that may switch, since the handler check follows those calls (section
20.10, Calls through interfaces and func values). The code after the end is outside the region, so a
per-CPU access there needs a region of its own, and a pointer from `&v` taken before it may not be used
after it (section 20.9, E2023).

Parking also lets the task move, and the wait check covers it (Waiting in a non-migrating region,
below).

Section 20.9 (Per-CPU variables) uses both. It also uses them for a store that depends on an earlier load
of the same per-CPU variable: that is a read-modify-write and needs a non-migrating region, as `ticks++`
does. The calls named above are not unsafe code: they cannot make an access invalid, and the analysis
follows them (section 18.12). A wrong pairing can stop a processor, which is a liveness failure, not a
safety one.

**The guard set.** A write of a datum is **shared** when it conflicts with an access by another party or
another instance of its own party. The **guard set** G(D) of a datum D is the set of locks that every
shared write of D holds in write mode and that guard D in place. A write under `RLock` does not count as
holding that lock. An atomic write counts as holding a lock named "atomic", which no other access holds.

- When D has a shared write and G(D) is empty, D has no guard.
- A read of D needs a lock of G(D), held in either mode, when some shared write of D may be made by
  another party or by another instance of the reader's party. The reads of a party that is the only
  writer of D, and not repeated, need none. For "atomic", the read must be atomic.

**Views loaded under a lock.** A view loaded by a crossing load (Crossings, case 5) from a datum D, or from
memory D owns, while the party holds locks of G(D), is **valid** at a later use when one of those locks
stays held on every path from the load to the use, with no **ending event** for it on the way:

- an `Unlock`, `RUnlock` or `c.Wait` of that lock, under any of its names, even if the lock is taken
  again after it;
- a call whose summary may release that lock; a view passed to such a call counts as used after the
  release;
- the end of the lock's name.

A view loaded by the only writer of D, when that party is not repeated, from D or from memory D owns, is
valid at each use with no lock: no other party writes D, so nothing can end it but the party's own code,
which the rules of chapter 18 already check within one party. It still counts as used with no lock held
when it crosses to another party.

A copy of the view whose uses the compiler cannot follow counts as used with no lock held: one stored
anywhere but in memory that the same guard set guards, sent on a channel, passed to a task, captured by a
function literal that is not part of the enclosing function, or held in a method value. An argument of a
deferred call, or a view a deferred literal uses, is used when the call runs, which may be at any point
after the `defer` statement (Held sets, above). So it is valid only if the lock stays held, with no
ending event, from the load to the `defer` statement and to every point after it, and no deferred call
that runs before it ends the view (a `defer mu.Unlock()` written before it runs after it). A view that a call returns,
loaded in the callee, counts as loaded at the call under the locks its summary records for it that the
caller holds across the whole call.

**Initialization.** Package initialization, and the part of `main` before the start of sharing, run while
no other party exists. A datum that no access writes after the start of sharing is **unchanged**, with
the memory it owns.

**Lazy initialization.** A datum that a `sync.Once` initializes needs no lock. A package variable, with
the memory it owns, is **initialized by** a package-level Once `o` when every write of it after the start
of sharing is made during a call of a function passed to `o.Do`, and every read of it outside such a call
is made where a call of `o.Do` has returned on every path to the read. A field of an object, with the
memory it owns, is initialized in the same way by a Once that is a field of the same object. The Once is
named as a lock is named (Locks and names, above), so `t.once` and `t.index` reached through one pointer
are the Once and the data of one object. The return of `f` is synchronized before the return of every
`Do` (memory-model.md section 3.4), so a read after a returned `Do` sees every write that `f` made.

- **During a call of `f`.** A write is made during the call when it is in `f`, or in a function that `f`
  calls, on the path from the call of `f` that `Do` makes. The same function reached in any other way,
  such as a literal that is also called directly or stored, writes outside. A write through a pointer is a
  write of the data that the compiler names as its target, by where the pointer came from; a write whose
  target it cannot name is a write of all data of that type. A task that `f` starts is another party, so
  its writes are outside. A method value's method counts as run outside every `Do`.
- **After a returned `Do`.** A call of `o.Do` counts for a read when it is made on the reading party, in
  the reading function or in code that is part of it (above), or in a function called from there whose
  summary shows a call of `o.Do` on every path to its return, at any depth. A `Do` made by another party,
  such as the task that started the reader, does not count, and neither does a `Do` reached through an
  interface, a func value or a method value (`rt.CatchExitTask(func() { o.Do(f) })` included). The
  other way counts, as a held lock does (Held sets, above): a function counts as having a returned
  `o.Do` throughout its body when every call of it, direct or through an interface or a func value of
  which it is a candidate, is made on the same party where a call of `o.Do` has returned on every path
  to the call. Each entry of a party counts as a call made with no returned `Do`, so no entry gets this
  credit: `main` and each `init` function, the function of a task statement, of `s.Task`, of `wg.Task`
  or of `rt.Go`, a function that `time.AfterFunc` starts, an interrupt handler, the panic handler, and
  every function that code the compiler does not analyze may call: one marked `//pu:export`, one the
  runtime calls with no call the compiler can see (the methods of the bound scheduler, section 19.5),
  and one whose value reaches a function without a body or other unsafe code. Each candidate of a func
  value whose calls the compiler cannot all find counts as called once with no returned `Do`. A function
  literal passed to an iterator, or to any function that calls it on the caller's party, so gets the
  Onces that a range body there would get. A method's run through a method value is outside every `Do`,
  as above. An interrupt handler cannot call `Do`, which may park (E2584). A read in a deferred call, in
  its literal's body or in a function it calls, counts as made at its `defer` statement and at every
  point after it, as a deferred release does (Held sets, above). So a `Do` counts for it only when the
  `Do` has returned on every path to the `defer` statement: `once.Do(load); defer show()` is covered, and
  `defer show(); once.Do(load)` is not, since a `load` that ends its task runs `show` before any `Do`
  has returned.
- **Otherwise.** A datum written outside these calls, or read on some path with no returned `Do`, follows
  rule 1 as other data does. `Do` holds the Once's own lock while `f` runs, so the writes inside `f`
  hold that lock and it is in the guard set. A write outside with no lock is E3502, which names that
  write. A read with no returned `Do`, when every write is inside, is E3503, which names the Once and the
  read. Data written inside the calls of two Onces has no lock that every write holds (E3502).
- **When `f` does not return.** `Do` returns only after some `f` has returned. A panic in `f` ends the
  program (section 17.1), and that `Do` never returns. In a hosted build the panicking task first runs
  its deferred calls while the other tasks keep running (section 17.2). The Once stays locked and not
  done meanwhile, so another task's `Do` on it waits until the program ends. A deferred call armed in
  `f`, or in a function `f` calls, runs while that lock is held and sees what `f` wrote so far, as `f`
  itself would. A deferred call armed anywhere else reads the data only where a `Do` on the Once returned
  before its `defer` statement (After a returned `Do`, above), and no `Do` on it has returned while an
  `f` runs. So no party reads the half-written data unordered. If `f` ends its task with `rt.ExitTask` (section
  17.3), `Do` does not return to its caller either: its frame leaves with the others. The Once releases
  its lock and is not done, so the next call of `o.Do` calls its own function, which finds what the first
  one left. Either way, the writes of an `f` that did not return are followed by the writes of the next
  `f` under the same lock, and no read follows a returned `Do` until some `f` has returned.
- **Several functions.** Different calls of `o.Do` may pass different functions. Only one of them
  returns, and they all write inside. Data that the function which ran did not write keeps the value it
  had at the start of sharing.
- **Resetting.** A Once whose whole value is assigned after the start of sharing, or that is moved,
  dropped or freed with the object that holds it, initializes nothing: its data follows the ordinary
  rules. A Once cannot be copied (Copying a lock, below).
- **Views.** A view loaded from the data crosses, and rule 2 decides what it may point to. Dropping or
  replacing an owner that the data holds is a write (Frees are writes, above), so outside `f` it sends
  the data back to the ordinary rules. A view obtained through a counted reference held in a local
  variable ends with that reference (section 18.3), so a use after the reference is dropped is E3313.

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

func main() {
	done := make(chan bool)
	task serve(done)
	task serve(done)
	<-done
	<-done
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

func main() {
	task reload()
	println(maxConns())
}
```

**Ordering used.** The order that channel operations, `sync.WaitGroup.Wait` and the end of a task impose
is not used: reading shared data after `wg.Wait()` still needs a lock of its guard set. Two orders are
used: the join of a `sync.Scope`, for the frame data it covers (section 19.4), and the return of
`sync.Once.Do`, for the data that Once initializes (Lazy initialization, above).

**Rule 1: shared data.** After the start of sharing, each datum that two parties may access, with at
least one access a write, must be unchanged, initialized by a Once, used only atomically, or guarded. The
data this rule covers are those that several parties can reach by name: package variables, the fields of
static objects and of `synced[T]` objects, and the contents of maps, each with the memory it owns.
Guarded means that the datum has a guard (The guard set, above) and every read that needs one holds a
lock of it.

- E3502: a datum with a shared write has no guard. The error names a write, the party that makes it (by
  the statement that starts it), and an access of another party it may meet.
- E3503: a read holds no lock of the guard set it needs. The error names the guard set, the writes that
  hold it, and the read.
- For the contents of a map, either error also names the occurrence that took the map out of its place,
  when there is one (Maps kept in place, above).
- E2010: a package variable whose type is not sendable (one that holds a `shared[T]`) is used by two
  parties, since copying a reference out of it changes a count with plain arithmetic.
- E3502: assigning a new value to a lock that another party may also use, which names the assignment. No
  guard can order it with the other party's critical section, which lasts from its `Lock` to its
  release. Moving or freeing the memory that holds such a lock is a write of that memory, which this rule
  already covers.

A field of a static object or of a `synced[T]` object can be guarded only by a lock of the same object or
a package-level lock (guards in place). Writes that initialize a `synced[T]` object before the reference
is first copied do not count. A map used by one party at a time needs no lock. Maps of one type that a
party fills before it shares them, and maps of types that never cross, need no lock either. Once a map of
a type crosses, maps of that type that parties fill for themselves after handing them to a function must
hold the guard too, or the shared map takes a type of its own. A map kept in place (Memory and data,
above) is outside all this: its contents are part of its place, and the lock that guards the place, such
as the mutex of the same object, guards them. Per-CPU variables follow section 20.9
instead of this rule.

```go
var (
	mu   sync.Mutex
	hits int
)

func handle(done chan bool) {
	mu.Lock()
	hits++ // every write holds mu: G(hits) is {mu}
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

**Rule 2: what may cross.** A view that crosses must point to memory that is safe for every party that
may use it. That includes a view the party loads from shared memory, and a view held in an interface,
func value or method value that it loads, wherever the view was stored. The memory must be one of these:

1. **Owned.** The memory of an owner that moved to the party, under the exclusivity of Owner transfer
   requires exclusivity (below).
2. **Never changes while shared.** A package variable or part of one, or a field of a static object, that
   is unchanged (Initialization, above), with the memory it owns; or such data that a Once initializes,
   read where that Once's `Do` has returned (Lazy initialization, above).
3. **Atomic or synchronized.** Memory of a `pluto/sync` or `pluto/sync/atomic` type, a runtime record
   (`rt.Task`, `rt.Timer`), a channel, or a struct of only those (`task worker(&wg)` with a package-level
   `wg`). Also a struct object of one of these kinds, each of whose fields meets the **field
   conditions**: it is unchanged once the object is shared, or it is used only atomically, or it is
   guarded by locks that guard it in place (of the same object, or package-level), or it is initialized by
   a Once of the same object:
   - a static object;
   - the object of a `synced[T]`;
   - an object whose type satisfies rule 3, the type rule.

   In both forms the memory must not be freed, moved or overwritten as a whole while another party may
   use it: no write of it, or of an object or owner that holds it, may conflict with a use by the other
   party, a call of a `pluto/sync` method included (Accesses, above).
4. **Guarded.** A datum that rule 1 guards, a field of an object of case 3 that is guarded by the field
   conditions, or memory that either owns, through a view that is valid at each use (Views loaded under a
   lock, above). Every access to that memory that the guard set requires to hold a lock (The guard set,
   above), by every party and through every view of it, holds a lock of its guard set (a write in write
   mode). A lock held around a call counts inside the callee, so
   `items.Push(x)` between `mu.Lock()` and `mu.Unlock()` may load the backing storage of a package-level
   `vec.Vec`; `hash.Map` and `strings.Builder` are used the same way.

A pointer to, or slice of, anything other than a struct crosses only in cases 1, 2 and 4 and the
`pluto/sync` part of case 3. Anything else is error E3501, at the crossing, or at the use for case 4. It
names a write that could change the memory while the other party uses it, or for case 4 the event that
ended the view. The fixes: pass a copy of the value; move an owner (a `vec.Vec` instead of a slice) so
that only the receiver has it; or keep the shared data where rule 1 guards it, and use views of it only
while a lock of its guard set is held.

A static object stays static when code assigns a new pointer to the package variable that refers to it:
the assignment is a write of the variable under rule 1, so it needs the variable's guard or an atomic
store, and a party that loaded the old pointer may go on using the old object. A party that loads the
variable after that gets the new object, which must be safe for it by its own case.

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
assignment to `b` drops that `Node`, which is a write of it after the start of sharing, so the `Node` is
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

A view that outlives its lock is the error. Here `first` returns a pointer into the storage after its
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

The same holds for memory owned by a static object. `std` is static, `mu` guards `buf` in place, and
`Push` uses the backing storage of `buf` only while `l.mu` is held:

```go
type Logger struct {
	mu  sync.Mutex
	buf vec.Vec[string]
}

var std = &Logger{}

func (l *Logger) Print(s string) {
	l.mu.Lock()
	l.buf.Push(s) // allowed: memory owned by a static object, used while l.mu is held
	l.mu.Unlock()
}

func work(done chan bool) {
	std.Print("hello\n")
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
```

A wait loop on a `sync.Cond` keeps its lock. `Wait(&mu)` releases `mu` while it waits and holds it again
when it returns, so `mu` is held on every path to `Len` and `Pop`, and `Pop` loads the storage after the
last `Wait`:

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

A view kept across the `Wait` is ended by it. Here `add` may grow `names` while `second` waits, which
moves the elements and frees the storage that `p` points into:

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

A call that may release the lock ends a view in the same way, even when the call returns with the lock
held. Here the summary of `waitNonEmpty` records that it may release `q.mu`:

```go
// peekWait keeps a view across a call that may release q.mu.
type Queue struct {
	mu       sync.Mutex
	nonEmpty sync.Cond
	items    vec.Vec[int]
}

func (q *Queue) waitNonEmpty() {
	for q.items.Len() == 0 {
		q.nonEmpty.Wait(&q.mu)
	}
}

func (q *Queue) peekWait() int {
	q.mu.Lock()
	p := q.items.Ptr(0)
	q.waitNonEmpty() // may release q.mu: ends p
	v := *p          // error: E3501, p is used after a call that may release q.mu
	q.mu.Unlock()
	return v
}

var q = &Queue{}

func main() {
	task q.peekWait()
	q.mu.Lock()
	q.items.Push(1)
	q.mu.Unlock()
	q.nonEmpty.Signal()
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

**Rule 3: the type rule.** An object of a struct type crosses by case 3 of rule 2 when the type satisfies
the **type rule**:

- **The free condition.** No code overwrites a whole object of the type through a pointer, or frees,
  drops or moves one (or replaces or drops an owner that holds one, or resets, releases or drops an arena
  that allocated one) after the start of sharing. A static object is exempt: a drop, free, move or
  overwrite of another object of the same type does not count against it.
- **The field conditions** of rule 2 case 3 hold for every field, with the guard sets inferred as for
  rule 1.

Writes that initialize an object do not count: writes through a pointer to memory the function allocated
(or that a function returned after allocating it and keeping it to itself, such as a constructor), made
before the pointer is stored, passed to a function that keeps it, sent or returned, including writes by
a function it is passed to for that purpose. So an object built by its constructor and then only read,
and an object whose methods all hold its own `sync.Mutex`, may both be shared. An object that some code
frees while tasks run fails the free condition, since a task could still use it: share it as the object
of a `synced[T]`, whose last release frees it after every use, or hand it over by moving its owner. A
type that fails the rule gives E3501 at the crossing, which names a write that breaks it.

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

**Releasing a lock.** Every call of `Unlock` or `RUnlock` of a `sync.Mutex`, a `sync.RWMutex` or an
`rt.Lock`, and every `c.Wait(l)` of a `sync.Cond`, must release a lock that the calling party holds at
that call by the held set: held on every path to the call, in write mode for `Unlock` and `Wait`, and in
read mode for `RUnlock`. Otherwise the call is a compile error, which names the call and a path on which
the lock is not held. A lock the compiler cannot name cannot be shown held, so its release is an error:
`l.Unlock()` on a `sync.Locker` or any other interface value, a `Wait` given the result of
`rw.RLocker()` or a `Locker` held in a variable, and a `Wait` called through an interface or a func value.
Pass the lock's own type (`*sync.Mutex`) or use a type parameter. Interrupt handlers follow the same
rule. The rule needs no annotation and costs nothing at run time. It covers a read lock, which has no
single owner: each party releases only the read lock it took. So no party can release a lock that
another party holds. A locked lock cannot be handed to another party to unlock. To let another task end
the work, send it a message on a channel, or start it in a `sync.Scope` and wait for it. Where one task
must take a permit and another give it back, use a buffered channel as the semaphore: any task may
receive from it or send to it.

How the held set applies at a release:

- **Names.** A release through one name of a lock ends all its names. A local or parameter that is
  assigned before the release names a different lock there.
- **`defer`.** `defer mu.Unlock()` releases at the end of the function, also when it ends by a panic. So
  the lock must be held at the `defer` statement and at every point after it. A `c.Wait(&mu)` there keeps
  it held, since `Wait` takes the lock again before it returns. Deferred calls run in reverse order, and
  each must find its lock held after the ones that run before it.
- **Loops.** A lock taken and released in one iteration is held between the two on every path. A lock
  released after a loop must be held on every path out of the loop, including the one that skips it.
- **Calls.** A release in a callee, such as `func (s *S) unlock() { s.mu.Unlock() }`, is allowed when
  every call of the callee holds the lock its argument names, and the caller then no longer holds it. A
  function reached through an interface, a func value or a method value counts as called with nothing
  held, so it may release only locks it takes itself. A helper that locks and unlocks its own parameter
  around a call of a func value, `withLock(&mu, f)`, compiles.
- **Recursion.** A recursive function may release a lock only if every call of it, the recursive calls
  included, holds that lock, for chains of calls of any depth.
- **Generics.** So `func withLocked[L sync.Locker](l L, f func())` that locks and unlocks `l` compiles
  when `L` is `*sync.Mutex`, and is an error when `L` is the interface `sync.Locker`.

The methods of `sync.Mutex` and `sync.RWMutex`, of the `Locker` that `RWMutex.RLocker` returns, and
`sync.Cond.Wait` release locks that their callers hold, through a lock inside the type or the `Locker`
they are given. They are trusted for this (section 18.12), and the rule is checked at each call of them
instead. The runtime (`pluto/rt`), which implements `rt.Lock`, channels and the scheduler, is outside the
race rules (What is not checked, below). No other code needs an exemption.

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

A range-over-func body is part of the function that holds the lock only when the iterator calls `yield`
on that function's party. Here `par` calls `yield` in a scoped task, so the body is a func value run by
that task, which holds nothing:

```go
// par runs the loop body in a scoped task.
var (
	mu   sync.Mutex
	data int
)

func par(yield func() bool) {
	var s sync.Scope
	s.Task(func() { yield() })
	s.Wait()
}

func f() {
	mu.Lock()
	for range par {
		mu.Unlock() // error: the body runs on another party, which does not hold mu
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
ends in `rt.Park` or is marked `//pu:blocking`.

The check does not start from a non-migrating region in a handler body or in a function that an
interrupt handler may reach. The handler check (E2584, section 20.10) already rejects every wait there,
through every call, and it is the stricter of the two, so such a wait is reported once, as E2584.

So in hosted builds, where `x86.Cli` faults, and in kernel
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
ordinary in a kernel, and a conservative rule would reject them. This check, and the switch check below
where the count is raised, are the only checks in the language that accept a program on doubt (section
1.2, Design rules).

**Switching tasks in a non-migrating region.** A call that may switch tasks (Non-migrating and
interrupt-disabled regions, above) is a compile error where the check reaches it inside a non-migrating
region. The task would go on after the call on whatever processor resumes it, so a per-CPU view, a
read-modify-write of a per-CPU variable, or anything else the region keeps on one processor (section
20.9) would no longer hold. The check is the wait check above, with `rt.Yield` and `rt.Preempt` as the
operations: it follows the same calls with a single known target, from regions in code outside
`pluto/rt`, and reports a switch on some path of a followed callee. It does not start from a handler
body's own region, which a switch ends instead (Non-migrating and interrupt-disabled regions, above). It
does start from every region that handler code opens itself with `x86.Cli`, `x86.SaveFlagsCli`,
`rt.DisablePreemption` or an `rt.Lock`, whether inside the handler body's own region, after `x86.Sti`,
or in a trap handler: that code asked to stay on one processor.

In a region where maskable interrupts are disabled and, on some path to the call, no preemption count is
raised (opened by `x86.Cli` or `x86.SaveFlagsCli` alone on that path), the run-time test below cannot
tell the region from a handler body, which may yield. So there, and only there, the check also follows calls with more than one target, as the handler check does
(section 20.10, Calls through interfaces and func values): a call through an interface, a func value, a
method value or a hook whose candidates may switch, and an allocation or drop that reaches an installed
allocator that may switch, is a compile error too.

For a call the check does not follow, `rt.Yield` makes the run-time test that `rt.Park` makes for the
count: it panics with "blocking with preemption disabled" when the task's preemption count is non-zero
(section 17.1; abi.md section 12.4). `rt.Preempt` with a non-zero count already switches nothing: it sets
the pending flag, and the task yields when the count returns to zero. The runtime's own switch path is
exempt: that pending yield, `preemptPark`, and the switch inside the backend. Neither call tests the
interrupt flag, so a handler can still yield.

**Interrupt handlers.** Each interrupt handler the program installs (section 20.10) is a repeated party
that may run at any point where interrupts are enabled, on any processor. Rules 1 to 3 apply with
handlers as parties, and the errors name the handler as the party (E3502, E3503, E3501). So data that a
handler accesses while other code may write it, or that a handler writes while other code may access it,
must be atomic or guarded by an `rt.Lock`, unless it is a per-CPU variable (section 20.9). Data that is
not written after the start of sharing, such as a table filled before the first handler is installed and
before interrupts are enabled, needs neither. A pointer that a handler loads from such a table crosses to
the handler, so the device record it points to must meet rule 2: a package variable no party writes, a
static object, or an object whose type satisfies the type rule. A `volatile[T]` access, and an
interrupt-disabled region, exclude only code on the same processor, so alone they satisfy the rules only
for per-CPU variables. An allocator has no exemption: its state is data like any other, and when a
handler can reach its `Alloc` or `Free`, the state they share with other code meets these rules (for
example per-CPU caches that are touched only in interrupt-disabled regions). The parts of an allocator
written in unsafe code are the responsibility of that code (section 18.12).

**Owner transfer requires exclusivity.** An owner moves to the receiving party, but the move is valid
only if no other view of the memory it holds is still live (section 18.3, Views): a pointer or slice
obtained from the owner and kept in a field, passed through a helper or copied to another variable
before `take` or the move, not only one its constructor kept. A view whose last use comes before the
move is allowed. A view of a local owner used after the move is E3313, and one stored where the compiler
cannot follow it is E3314, since the move ends it (section 18.3, A local owner's memory). Any other view
that may still be used while the receiving party can use the memory is E3501, which names the view, the
move and the conflicting use. Generation checks alone do not prevent races. Thus `task c.bump()` hands
over an `owned[Counter]` only under that exclusivity proof (section 8.8).

**Copying a lock.** A `sync.Mutex`, `sync.RWMutex`, `sync.Once`, `sync.WaitGroup` or `sync.Cond` may not
be copied after its first use, as in Go, and outside `pluto/rt` neither may an `rt.Lock`. The type system
already forbids copying a `sync.Scope`, which is an owner (section 19.4; a Scope that has started tasks
may not be moved either, E3405). For the other types the compiler cannot tell the first use, so any copy
from a place of a value whose type holds one of them, directly or through fields and array elements, is a
compile error: an assignment or initialization from a variable, field, element or dereference, an
argument, a result, a value sent on a channel, a range value, a value receiver, and a conversion,
including to an interface. A composite literal, a zero value and the result of a call are new values,
not copies. Moving an owner that holds one is a move, not a copy. Pass a pointer, or keep the value in
one place.

**What is not checked.** Unsafe code (section 18.12, Safe and unsafe code) is the programmer's
responsibility: memory reached through `unsafe.Pointer` or `rt.Pinned`, functions without a body, and the
variables, records and non-migrating regions of the runtime (`pluto/rt`), which implements tasks with its
own locks and with preemption disabled. Unsafe code includes the function of an `rt.Timer` and the code
that arms it (Parties, above). The runtime's non-migrating regions are not checked for waits or switches
(Waiting in a non-migrating region, and Switching tasks in a non-migrating region, above), and its releases of its own locks are not checked by the rule
for releasing a lock (above). Nor are the bodies of the lock methods and of `sync.Cond.Wait` that section
18.12 lists, whose callers the rule checks instead. A data race in unsafe code gives torn or stale values
(memory-model.md section 4), and it is never undefined behavior in the compiler's sense: the compiler may
not assume races are absent. A torn pointer or length can then break memory safety, which is why unsafe
code must not race.

**How precise the race check is.** So that a programmer can tell what is accepted:

- A place is a variable followed by steps: field selections, indexes and dereferences. The check follows
  a place through at most two pointer loads and eight steps. Beyond that, the place stands for everything
  reachable from the point where it was cut, of every type.
- A value passed to a function is followed from at most sixteen places it may come from. Beyond that, it
  counts as coming from anywhere under the variables those places start from.
- A func value passed as an argument is followed through at most four nested calls of functions that
  receive it.
- Recursive functions are followed until their summaries stop changing, with the limits above and no
  others: the answer does not depend on the order in which the compiler visits functions.
- An interface or func-value call reaches the candidates that section 18.3 (How precise the frame check
  is) defines.

These limits are part of the language: a program that needs more precision is rejected with the access or
crossing that could not be proved, and a program that would need a func value followed through more
calls is rejected with an implementation-limit error (section 21.4). A program is never accepted with a
party or an access left out. An implementation uses these limits exactly: it may not raise them, since a
program it then accepted would be rejected by another implementation, and acceptance may not depend on
the implementation or its optimizer (section 21.3, Acceptance does not depend on optimization). A later
edition of the language may raise them.

**Conformance.** Each row gives a program, the result that every implementation must give, and the rule
that decides it. "A compile error" is an error that has no code yet. Rows 1 to 12 are the first shape a
Go programmer would write; rows 13 to 21 are Go locking idioms; rows 52 to 66 settle audit K
(docs/verification/2026-11-09-spec-audit-k.md); rows 73 to 99 settle audit L
(docs/verification/2026-11-09-spec-audit-l.md), and rows 100 to 109 are the common Go shapes it lists;
rows 110 to 121 settle audit M (docs/verification/2026-11-09-spec-audit-m.md); rows 122 to 126 settle
audit N (docs/verification/2026-11-09-spec-audit-n.md).

| # | Program | Result | Deciding rule |
|---|---|---|---|
| 1 | `var mu sync.Mutex; var cache = map[string]*Entry{}`; `get` stores `&Entry{}` under `mu` | E3002 | 18.3: a frame view stored in package memory |
| 2 | `var cache = map[string]synced[Entry]{}`; `get` copies the `synced[Entry]` out under `mu`; the fields of `Entry` that change use its own lock | accepted | rule 1 (map contents, synced object) |
| 3 | `var std = &Logger{w: os.Stderr, prefix: "app: "}` with a `sync.Mutex` field that `Printf` takes | accepted | rule 2 case 3 (static object) |
| 4 | `func init() { Register("echo", &echoHandler{}) }` storing into a package map | E3002 | 18.3 |
| 5 | `var handlers = map[string]Handler{"echo": &echoHandler{}}`, read by every task | accepted | Initialization; rule 1 (map contents); rule 2 case 3 |
| 6 | `var cfg atomic.Pointer[Config]` with `cfg.Store(&Config{...})` in a reload task | E3003 | 18.3 |
| 7 | `var cfg owned[Config]` replaced by a reload task while readers hold `&*cfg` | E3501 | rule 2 (Frees are writes) |
| 8 | `var cfg synced[Config]` under a mutex; readers copy the reference out under it, the reload task assigns under it | accepted | rule 1 |
| 9 | `var free vec.Vec[owned[Conn]]` under `mu`; `Get` pops, `Put` pushes | accepted | rule 2 case 4 |
| 10 | `var stats Stats` or `var stats = &Stats{}` with only `atomic.Int64` fields | accepted | rule 1 (atomic); rule 2 case 3 |
| 11 | `var inst owned[DB]; var once sync.Once`; `once.Do(func() { inst = open() })`, then a read of `inst` | accepted | Lazy initialization |
| 12 | `vec.Vec[Job]` queue under `mu`; `c.Signal()` after a push; workers loop on `c.Wait(&mu)` and pop | accepted | Held sets (`Wait`); rule 2 case 4 |
| 13 | `mu.Lock(); defer mu.Unlock()` | accepted | Releasing a lock (`defer`) |
| 14 | `mu.Lock()` with early-return branches that each unlock once; a branch that unlocks twice | accepted; the second `Unlock` is a compile error | Releasing a lock |
| 15 | `rw.RLock(); ...; rw.RUnlock()`; and `rw.RLock(); rw.Unlock()` | accepted; the `Unlock` is a compile error | Releasing a lock (mode) |
| 16 | `rw.RLock(); p := items.Ptr(0); rw.RUnlock(); rw.Lock(); _ = *p` | E3501 | Views loaded under a lock |
| 17 | A struct with an embedded `sync.Mutex`: `c.Lock(); c.n++; c.Unlock()` | accepted | Locks and names |
| 18 | Hand-over-hand with locals (`cur := prev.next; cur.mu.Lock(); prev.mu.Unlock(); prev = cur`); the same with `l.head.mu.Unlock()` | accepted; the second is a compile error | Locks and names |
| 19 | `stripes[h%n].Lock(); ...; stripes[h%n].Unlock()`; and `s := &stripes[h%n]; s.Lock(); ...; s.Unlock()` | a compile error; the pointer form is accepted | Locks and names (variable index) |
| 20 | `mu.Lock(); for !ready { c.Wait(&mu) }; ...; mu.Unlock()` | accepted | Releasing a lock |
| 21 | `func (s *S) withLock(f func()) { s.mu.Lock(); defer s.mu.Unlock(); f() }` | accepted | Releasing a lock (Calls) |
| 22 | `p.Lock(); q.Unlock()` where `p` and `q` point to one mutex by different parameters | a compile error | Releasing a lock |
| 23 | `g.p.mu.Lock(); ...; g.p.mu.Unlock()` | a compile error | Locks and names (a pointer loaded from memory) |
| 24 | `mu = sync.Mutex{}` in one task while another task uses `mu` | E3502 | rule 1 (assigning a lock) |
| 25 | `mu.Lock(); s.Task(func() { mu.Unlock() })` | a compile error | Releasing a lock (the scoped task holds nothing; Parties) |
| 26 | An interrupt handler calls `l.Unlock()` on an `rt.Lock` that the interrupted code holds | a compile error | Releasing a lock |
| 27 | `u := mu.Unlock; mu.Lock(); u()` | a compile error | Releasing a lock (method value) |
| 28 | `mu.Lock(); f(1)` with `func f(n int) { if n > 0 { f(n - 1); mu.Unlock() } }` | a compile error | Releasing a lock (Recursion) |
| 29 | The `finish` example above: a task unlocks a lock its starter took | a compile error | Releasing a lock |
| 30 | A task reads `p.x` of a `*P` from `ar.New[P]()`; the starter calls `ar.Reset()` while it runs | E3501 | rule 3 (free condition; Frees are writes) |
| 31 | `b = new(Node); p = &*b; task reader(); b = new(Node)`; `reader` reads `p.n` | E3501 | Crossings case 5; rule 2 |
| 32 | `var p atomic.Pointer[Node]`; `reader` calls `p.Load()`; `main` later drops that `Node` | E3501 | Crossings (an atomic load); rule 2 |
| 33 | `var r shared[Node]`, copied by two tasks | E2010 | rule 1 |
| 34 | A handler table filled with `&devs[i]` before the first install, read by the handler | accepted | Initialization; rule 2 case 2 |
| 35 | A handler installed before any task loads `p`; `main` then replaces the owner of `*p` | E3501 | The start of sharing; rule 2 |
| 36 | `take(os.Stdout)` in one task while another prints | E3501 | Memory and data (static object); rule 3 |
| 37 | The `work` example above: a task drops its own `File` and prints to `os.Stdout` | accepted | rule 2 case 3 (static object) |
| 38 | `p := items.Ptr(0)` under `mu`, then `task use(p)` | E3501 | Views loaded under a lock (a copy that cannot be followed) |
| 39 | `defer use(items.Ptr(0))` after `defer mu.Unlock()`; and before it | accepted; E3501 | Views loaded under a lock (deferred argument) |
| 40 | `if fast { mu.Lock() }; p := items.Ptr(0)` | E3501 | Held sets (every path) |
| 41 | `items.Push` under `a` in one task and under `b` in another | E3502 | The guard set; rule 1 |
| 42 | `var m map[int]synced[Node]` under `mu`; a task copies `m[k]` out, another deletes the entry | accepted | rule 1 (map contents, synced object) |
| 43 | `var m map[int]*Node` into a `vec.Vec[owned[Node]]`, both under `mu`; `m[k]` used after `Unlock` while another task removes the node | E3501 | rule 2 case 4 |
| 44 | The `tick` example above: a direct call reaches a send inside `rt.DisablePreemption` | a compile error | Waiting in a non-migrating region |
| 45 | The `flush` example above: the send is behind an interface call | accepted (panics at run time if it parks) | Waiting in a non-migrating region |
| 46 | A second task reads data a Once initializes with no `Do` of its own | E3503 | Lazy initialization |
| 47 | The `Do` is in a helper that calls it on some paths only, then the read | E3503 | Lazy initialization (every path) |
| 48 | The `Limits` example above: `reload` writes `lim` outside the Once | E3502 | Lazy initialization; rule 1 |
| 49 | The `second` example above: a view kept across `Wait` | E3501 | Views loaded under a lock |
| 50 | `c.Wait(rw.RLocker())` | a compile error | Releasing a lock (a lock with no name) |
| 51 | A deferred `c.Wait(&mu)` after `defer mu.Unlock()` | accepted | Releasing a lock (`defer`) |
| 52 | K1: the `par` example above, a body that unlocks while `par` calls `yield` in a scoped task | a compile error | Part of the enclosing function; Releasing a lock |
| 53 | K1: `mu.Lock(); for x := range v.All() { defer mu.Unlock(); ...; break }` with `vec.Vec.All` calling `yield` in its own loop | accepted | Part of the enclosing function |
| 54 | K1: `for range par { defer cleanup() }` | a compile error | Part of the enclosing function (`defer` in a body run by another party) |
| 55 | K2: the `Logger` example above, `var std = &Logger{}` with `buf vec.Vec[string]` under `l.mu` | accepted | rule 2 case 4 (memory owned by a static object) |
| 56 | K3: the `peekWait` example above | E3501 | Views loaded under a lock (a call that may release) |
| 57 | K4: `idt.Install(32, timerHandler); x86.Sti(); table[0] = Device{port: 0x40}`, and the handler reads `table` | E3502 | The start of sharing; rule 1 |
| 58 | K5: a scoped task loads `p := items.Ptr(0)` under a frame `mu`, unlocks, locks again and uses `*p`, while another scoped task pushes under `mu` | E3402 | 19.4 Sharing (form 2) |
| 59 | K6: `stats` written only with `a` and `b` both held; one task reads under `a`, another under `b`; a view loaded under both is used after `b.Unlock()` with `a` held | accepted | The guard set; Views loaded under a lock |
| 60 | K6: task A writes `x` under `L` at one write and under `M` at another, and is its only writer; task B reads `x` under `L` | E3502 | The guard set (G(x) is empty) |
| 61 | K7: `//pu:percpu var runq vec.Vec[*Task]`, `runq.Push(t)` inside an interrupt-disabled region, opened by `x86.Cli` or the body of a `//pu:interrupt` handler | accepted | 20.9 (owned memory of a per-CPU variable); Non-migrating and interrupt-disabled regions (both kinds are non-migrating) |
| 62 | K9: `cfgOnce.Do(load); for _, s := range cfg.servers.All() { println(s.name, cfg.port) }` | accepted | Part of the enclosing function; Lazy initialization |
| 63 | K10: at package level, `mu.Lock(); p := items.Ptr(0); mu.Unlock(); mu.Lock(); _ = *p` | E3501 | Views loaded under a lock (8.8 says the same) |
| 64 | K11: a static object whose only changing field is an `atomic.Int64`, read by two tasks | accepted | rule 2 case 3 (not case 2) |
| 65 | K12: `rw.RLock(); c.Wait(&rw)` | a compile error | Releasing a lock (`Wait` needs write mode) |
| 66 | K13: `cfg()` calls `load()`, which calls `once.Do(f)` on every path; `cfg()` then reads the data | accepted | Lazy initialization (any depth) |
| 67 | K14: `c2 := c` for a `sync.Cond` `c`; and a call of a method with a value receiver on a struct that holds a `sync.Mutex` | a compile error | Copying a lock |
| 68 | An owner struct that holds a locked `sync.Mutex` moved on a channel | accepted (the receiver starts with nothing held) | Locks and names (a move ends the name) |
| 69 | `func bump(l *sync.Mutex) { l.Lock(); x++; l.Unlock() }` called with `&a` by one task and `&b` by another | E3502 | The guard set |
| 70 | Readers use an object under `rw.RLock`; a writer frees it under `rw.Lock` | accepted | Frees are writes; The guard set |
| 71 | `mu.Lock(); s.Task(func() { release() })`, where `release` calls `mu.Unlock()` | a compile error | Releasing a lock (a callee releases what its caller holds; the scoped task holds nothing) |
| 72 | A task statement in `start`, which nothing calls from `main` | accepted (no party) | Parties |
| 73 | L1: `rt.DisablePreemption(); s := runq.Slice(); rt.Yield(); use(s[0]); rt.EnablePreemption()` on a per-CPU `runq` | a compile error | Switching tasks in a non-migrating region |
| 74 | L1: inside `rt.DisablePreemption()`, a call through an interface whose candidate calls `rt.Yield` | accepted; panics "blocking with preemption disabled" if the candidate yields | Switching tasks in a non-migrating region (run-time test) |
| 75 | L1: inside `x86.Cli()` in kernel mode, a call through an interface whose candidate calls `rt.Yield` | a compile error | Switching tasks in a non-migrating region (followed through every candidate) |
| 76 | L1: a `//pu:interrupt` handler calls `rt.Preempt()` | accepted (the handler's region ends at the call) | Non-migrating and interrupt-disabled regions |
| 77 | L2: `t := time.AfterFunc(d, f)`; `t.Reset(0)` while `f` may run; `f` does `n++` on a package `n` nothing else uses | E3502 | Parties (an `AfterFunc` function is repeated); rule 1 |
| 78 | L2: as row 77, with `f` doing `n.Add(1)` on an `atomic.Int64` | accepted | Parties; rule 1 (atomic) |
| 79 | L3: `var b owned[S]` with `S{mu sync.Mutex}`; `task user(&b.mu)`; `b = new(S{})` | E3501 | rule 2 case 3 (not freed while shared); Accesses (a `pluto/sync` call) |
| 80 | L3: two tasks call `b.mu.Lock()` and `b.mu.Unlock()` on `var b owned[S]`; one also does `b = new(S{})` under `b.mu` | E3502 | Accesses (a free conflicts with a `pluto/sync` call); rule 1; Locks and names (`b.mu` is a name: a box is not a pointer) |
| 81 | L3: two tasks lock `b.mu` and write `b.n` under it, for `var b owned[S]` with `S{mu sync.Mutex; n int}` | accepted | Accesses (a write of other memory of the datum does not conflict with the call); The guard set; Locks and names (`b.mu` guards `b.n` in place: reaching the box's object is no pointer load) |
| 82 | L4: the iterator `All` holds `mu` around each `yield(i, scores.Ptr(i))`; `for _, p := range All { *p += 1 }` | accepted | Part of the enclosing function (the iterator's locks at `yield`) |
| 83 | L4: as row 82, with the body written as a literal passed to `All` | accepted | Held sets (a func value runs with the caller's held set) |
| 84 | L4: `All` takes and releases `mu` around each `yield`; the body keeps `p` from one run to the next | E3501 | Part of the enclosing function; Views loaded under a lock |
| 85 | L4: `o.Do(load)`; then `Each(func(x int) bool { use(cfg); return true })`, where `Each` calls its argument on the caller's party | accepted | Lazy initialization (a returned `Do` counts in the callee) |
| 86 | L5: `type Cache struct{ mu sync.Mutex; m map[string]int }`; `var users = &Cache{m: map[string]int{}}` and `hosts` alike; `Put` writes `c.m[k]` under `c.mu`; tasks call both | accepted | Maps kept in place; Locks and names |
| 87 | L5: as row 86, with a method `Snapshot` that returns `c.m` | E3502 | Maps kept in place (a return copies the map out); The guard set |
| 88 | L5: as row 86, with `n := len(c.m)` and `for k := range c.m` under `c.mu`, and `count(c.m)`, where `func count(m map[string]int) int { return len(m) }` | accepted | Maps kept in place (uses, and a parameter that keeps it in place) |
| 89 | L5: as row 86, with `Reset` doing `m := make(map[string]int); m["a"] = 1; c.m = m` under `c.mu` | E3502 | Maps kept in place (a value stored must be new where it is stored); The guard set |
| 90 | L6: the only writer of `items`, a task not repeated, pushes under `mu` and runs `for _, x := range items.Slice()` with no lock; other tasks read under `mu` | accepted | Views loaded under a lock (the only writer); rule 2 case 4 |
| 91 | L7: `m := s.inc; mu.Lock(); m(); mu.Unlock()`, `inc` writes a package `x` that other tasks write under `mu` | accepted | Held sets (a method value runs with the caller's held set) |
| 92 | L8: an `rt.Timer` function writes a package `n` that a task also writes | not checked (unsafe code) | Parties; What is not checked |
| 93 | L9: a list of `owned[Node]` links with a `sync.Mutex` in each node, walked hand over hand by two tasks that write node fields | E3502 | Memory and data (owned nodes are part of the root datum); The guard set |
| 94 | L11: map type `M` never crosses; two tasks each get a map of `M` from `mk()`, which makes it, and fill it | accepted | Memory and data (memory a callee on the same party allocated) |
| 95 | L12: before the start of sharing `p := items.Ptr(0)`; a task pushes under `mu`; later `mu.Lock(); _ = *p` | accepted by the race rules; the use panics if the push moved the storage (section 18.11) | Frees are writes; Views loaded under a lock |
| 96 | L13: `p := &mu; p.Lock(); x++; p.Unlock()` in one task, `mu.Lock(); x++; mu.Unlock()` in another | accepted | Locks and names (an address carries the name) |
| 97 | `wg.Task(work)` several times, `work` writes `results[next.Add(1)-1]` on a package slice; `main` reads `results` after `wg.Wait()` | E3502 | rule 1 (a variable index is every element); Ordering used |
| 98 | `results := make([]int, n)`; `for i := range n { s.Task(func() { results[i] = f(i) }) }`; read after the Scope's `Wait` | accepted | 19.4 Partitions |
| 99 | As row 98 with `wg.Task(func() { results[i] = f(i) })` | E2590 | 12.12 (a capturing literal given to a function that starts a task) |
| 100 | Go shape: `c := &Counter{}` in `main`; `task worker(c)` | E3004 | 18.3 (a frame view passed to a task) |
| 101 | Go shape: a struct with a mutex and a map, several instances, each method locking its own mutex | accepted | Maps kept in place (row 86) |
| 102 | Go shape: a worker pool with a jobs channel, a results channel, a package-level `wg`, and `wg.Wait()` then `close(results)` | accepted | Crossings (values cross by channel); Accesses (`pluto/sync` and channel calls) |
| 103 | Go shape: `task func() { defer wg.Done(); work(items[i]) }()` capturing loop data | E2590 | 12.12 |
| 104 | Go shape: workers write `results[i]`, read after waiting | E3502 with `wg.Task` (row 97); accepted with a `sync.Scope` (row 98) | rule 1; 19.4 Partitions |
| 105 | Go shape: a `done` channel closed to cancel, and `select` on it in each task | accepted | Accesses (channel operations) |
| 106 | Go shape: a package `atomic.Bool` stop flag set by one task, polled by others | accepted | rule 1 (atomic) |
| 107 | Go shape: a `sync.Once` singleton returning a pointer to its data, nothing resets it | accepted | rule 2 case 2; Lazy initialization |
| 108 | Go shape: a read-mostly package config map under a package `sync.RWMutex` | accepted | Maps kept in place; rule 1 |
| 109 | Go shape: `func (s *Scores) All() func(yield func(int, *int) bool)` returns a literal that locks `s.mu` around each `yield(i, s.v.Ptr(i))`; `for _, p := range sc.All() { *p += 1 }` in two tasks, for a static object `sc` | accepted (the body holds `sc.mu`, so every write of `sc.v` does) | Part of the enclosing function; Locks and names (a capture carries the receiver's name) |
| 110 | M1: a `//pu:interrupt` handler does `ticks++` on a per-CPU `ticks`, then `rt.Preempt()` | accepted | Non-migrating and interrupt-disabled regions (a handler body is non-migrating up to the switch); 20.9 |
| 111 | M1: a handler does `rt.Preempt()`, then `ticks++` | E2022 | Non-migrating and interrupt-disabled regions (the switch ends the handler's region); 20.9 |
| 112 | M1: a handler does `p := &ticks; rt.Preempt(); *p = 0` | E2023 | 20.9 (a pointer from `&v` is bounded by its region) |
| 113 | M1: a handler calls `h.Run()` through an interface whose candidate may call `rt.Yield`, then does `ticks++` | E2022 | Non-migrating and interrupt-disabled regions (a call that may switch through any candidate ends the region) |
| 114 | M1: a handler does `f := x86.SaveFlagsCli(); rt.Preempt(); x86.RestoreFlags(f)` | a compile error | Switching tasks in a non-migrating region (a region the handler opens itself) |
| 115 | M1: a handler does `events <- n` on a buffered channel, outside a `select` with `default` | E2584, reported once | 20.10; Waiting in a non-migrating region (not started from handler code) |
| 116 | M2: `it := sc.All(); for _, p := range it { *p += 1 }`, with `All` and `sc` as in row 109 | E3502 (`*p += 1` writes `sc.v` with no lock) | Locks and names (a result stored first is a func value; no lock is added) |
| 117 | M2: as row 109, but `All` assigns `s = s.next` after making the literal | E3502 (`*p += 1` writes with no lock) | Locks and names (an assigned capture carries no name) |
| 118 | M3: `func load() { cfg = 42 }`; `func worker() { once.Do(load) }`; `main` does `task worker(); println(cfg)` | E3503 | Lazy initialization (an entry counts as a call with no returned `Do`) |
| 119 | M3: `//pu:export func get() int { return cfg }`, every write of `cfg` is inside `once.Do`, and the only Plutonium call of `get` follows `once.Do(load)` | E3503 (C code may call `get` first) | Lazy initialization (an exported function is an entry) |
| 120 | M4: row 86, plus `func (c *Cache) Dump() map[string]int { return c.m }` in another package | E3502, naming `return c.m` as the copy that takes the map out of `Cache.m` | Maps kept in place; rule 1 |
| 121 | G3: `mu.Lock(); for x := range Each { if x < 0 { mu.Unlock(); break } }`, where `Each` reads data guarded by `mu` before its first `yield` | E3503 | Part of the enclosing function (when the body may release a lock, every access of the iterator counts without it) |
| 122 | N2: `func load() { cfg = 1; if bad() { rt.ExitTask() }; cfg = 2 }`; `func show() { println(cfg) }`; `func worker() { defer show(); once.Do(load) }`; `main` does `task worker(); task worker()` | E3503 (the deferred read runs when `load` ends its task, before any `Do` has returned, while the other task's `Do` runs `load` again) | Lazy initialization (a deferred read counts at its `defer` statement and every point after it) |
| 123 | N2: as row 122, but `worker` does `once.Do(load); defer show()` | accepted | Lazy initialization (the `Do` returned on every path to the `defer` statement) |
| 124 | N2: `n` written by other tasks under `mu`; `func f() { mu.Lock(); defer mu.Unlock(); defer func() { n++ }(); step() }` | accepted (the literal runs before the deferred `Unlock`, and `mu` is held at its `defer` statement and every point after it) | Held sets (deferred calls) |
| 125 | N2: as row 124, but `func f() { defer func() { n++ }(); mu.Lock(); defer mu.Unlock(); step() }` | E3502 (`n++` runs with no lock if `step` ends the task before `mu.Lock`, or after the deferred `Unlock`) | Held sets (deferred calls) |
| 126 | N2: `func main() { defer func() { cfg = 0 }(); task reader() }`, where `reader` reads `cfg` with no lock | E3503 (the deferred write runs after the task start, so it is a shared write) | The start of sharing (a deferred call counts after it) |

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
   reads under `RLock`). The lock is held by the rules of section 19.3, Held sets. A view loaded from
   such a place, or from memory it owns (section 19.3, Memory and data), may be used only while it is
   valid: the lock stays held from the load to the use, with no ending event between (section 19.3,
   Views loaded under a lock). A use after an `Unlock` and a new `Lock` is E3402.
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
of `pluto/time`, which starts each function. `Reset` through any handle arms the timer again, so `f` may
run more than once from one call of `AfterFunc`, and a run may overlap one still going: the race rules
count `f` as a repeated party (section 19.3, Parties).

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
