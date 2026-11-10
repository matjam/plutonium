# The Plutonium Memory Model

**Version:** 0.4 (draft)
**Date:** 2026-11-09

This document defines when a read of memory in one task, or on one processor, is guaranteed to observe
a write made by another, what safe code is promised about data races, what a race in unsafe code may and
may not do, and which transformations the compiler may make. It also gives the rules for interrupt
handlers, device memory and kernels that bring their own scheduler.

The language specification ([language.md](language.md)) is the source of truth. Section 19.7 (Memory
model) states the rules of the memory model, and chapters 18 (Ownership and memory) and 19 (Concurrency)
state the rules they rest on: what may cross between tasks, which data races the compiler rejects, how
counted values are counted, and what the scheduler must do. This document refines those rules for
implementers of the compiler, the runtime, allocators and scheduler backends, with the detail the
specification leaves to it. Where this document and the specification differ, the specification holds,
and this document is to be corrected. No other memory model applies where both are silent.

Terms from the language specification are used with their meaning there: a **task** (section 19.1,
Tasks), an **owner** and a **move** (sections 18.1 and 18.5), a **counted value** (sections 8.7 and
18.1), **sendable** (section 8.8), **safe code** and **unsafe code** (section 18.12).

## Contents

1. [Advice](#1-advice)
2. [Definitions](#2-definitions)
3. [Synchronizing operations](#3-synchronizing-operations)
4. [Data races](#4-data-races)
5. [What the compiler may and may not do](#5-what-the-compiler-may-and-may-not-do)
6. [Kernels: interrupts, processors and schedulers](#6-kernels-interrupts-processors-and-schedulers)
7. [Differences from other languages](#7-differences-from-other-languages)

## 1. Advice

Memory that several tasks use must be shared in a way that orders their accesses. In safe code the
compiler enforces this (section 4.1; language spec section 19.3, Data race freedom), and the ways it
accepts are few: move an owner from one task to another (on a channel or as a task argument), share data
that never changes while shared, use `pluto/sync/atomic` operations, hold the lock the compiler infers to
guard the data, or, for the variables of one function, start the tasks with a `sync.Scope` and partition
what they write (language spec section 19.4, Scoped tasks). A program that needs
the rest of this document to explain why it works is too clever.

`volatile[T]` is not a way to synchronize processors. It orders the accesses to one device, and between
an interrupt handler and the code it interrupts on the same processor (section 6), nothing more.

## 2. Definitions

**Memory operations.** A memory operation has a kind, a location and the value read or written. The
kinds are plain read, plain write, volatile read, volatile write (accesses through a `volatile[T]`,
language spec section 7.15), atomic operations (section 3.5), and the other synchronizing operations of
section 3.

**Locations.** A variable of a type that is not a struct, an array or a packed type is one location.
Each field of a struct, and each element of an array, is a location of its own, recursively. A packed
value is one location as a whole, whatever its fields (section 4.3). A location wider than a machine word
(a string, a slice, an interface) may be read or written as several word-sized accesses (sections 4.2 and
4.3).

**Sequenced before.** Within one task, an operation is sequenced before another when it comes first in
the order of execution that the language specification defines: the order of evaluation of section 11.26
(Order of evaluation) and the execution of statements in chapter 12 (Statements).

**Synchronized before.** Some pairs of operations in different tasks, or on different processors, are
ordered by section 3: the first is synchronized before the second.

**Happens before** is the transitive closure of sequenced before and synchronized before. It is a partial
order: two operations may be ordered by neither.

**Observation.** A plain or volatile read *r* of a location *x* may observe a write *w* to *x* when both
hold:

1. *r* does not happen before *w*; and
2. there is no other write *w'* to *x* such that *w* happens before *w'* and *w'* happens before *r*.

So a read observes the latest write that happens before it, or any write that is not ordered with it. An
atomic read observes according to the total order of section 3.5.

**Data races.** Two memory operations **conflict** when they access the same location, at least one of
them writes, and at least one of them is not atomic. A **data race** is a pair of conflicting operations
in different tasks, or involving an asynchronous interrupt handler and other execution (including
code on another processor or another handler), neither of which happens before the other. Section 6.1 defines the permitted same-processor interrupt
protocols and their restricted ordering; physical interruption alone orders no unprotected plain access. An execution with no data race is
**data-race-free**.

**Sequential consistency.** An execution that is data-race-free behaves as if the operations of all its
tasks were performed one at a time, in a single order that keeps each task's sequenced-before order, with
each read observing the latest write to its location before it in that order.

## 3. Synchronizing operations

### 3.1 Initialization

Package initialization runs in one task, the main task, which then calls `main`. Initializing a package
is sequenced before initializing any package that imports it, and all initialization is sequenced before
`main` starts (language spec section 15.3, Program initialization). Initialization starts no task
(language spec section 15.2, Package initialization), so no other task exists while it runs. No
interrupt handler is a party until the first handler is installed or interrupts are enabled (language
spec section 19.3).

### 3.2 Starting a task

The statement that starts a task is synchronized before the first operation of that task. The arguments,
which are evaluated and copied when the statement runs, are therefore visible to the new task without
further synchronization, and so is every write the starting task made before the statement.

This holds for the task statement (language spec section 12.12), for `rt.Go`, for `s.Task` on a
`sync.Scope` and `wg.Task` on a `sync.WaitGroup`, and for the start of a `time.AfterFunc` function.

Task end alone creates no ordinary join for another running task. Programs use a channel, lock,
`sync.WaitGroup` or `sync.Scope`. Section 3.9 also defines the normal-termination boundary: when package cleanup
runs, the operations of every finished application task and callback happen before it.

### 3.3 Channels

For a channel of any element type:

- A send is synchronized before the completion of the receive that takes that value.
- Closing a channel is synchronized before a receive that returns because the channel is closed.
- A receive from an unbuffered channel is synchronized before the completion of the send it pairs with.
- For a channel of capacity *C* > 0, the *k*-th receive is synchronized before the completion of the
  (*k*+*C*)-th send. This is what makes a buffered channel usable as a counting semaphore.
- `len` and `cap` of a channel are not synchronizing.

A `select` statement that proceeds with a communication case synchronizes exactly as that send or receive
would on its own. A `select` that takes its `default` case performs no synchronizing operation. A
`for v := range ch` loop is a sequence of receives.

A channel is a counted reference whose plain/atomic count follows the single rule in language spec
section 18.7 (Shared ownership, When counts are atomic); section 3.7 gives its memory ordering. The release
of the last reference is synchronized before the channel's memory is freed, so freeing never races with a
task that still held a reference. Values still buffered at that point are dropped after that release and
before the memory is freed, by the task that released the last reference. The channel's timer, if it has
one (`time.After`, `time.Tick`, a `Timer` or `Ticker`), is stopped before the memory is freed; stopping
waits for the timer's function if it is running, so a timer never delivers to a freed channel.

### 3.4 `pluto/sync`

The types of `pluto/sync` are built on atomic operations, runtime locks, and the scheduler operations of
section 3.6. Their guarantees hold whichever scheduler backend is bound:

- **Mutex.** For any `sync.Mutex` `l` and *n* < *m*, the *n*-th call of `l.Unlock()` is synchronized
  before the *m*-th call of `l.Lock()` returns. A `TryLock` that succeeds is a `Lock`; one that fails is
  not synchronizing.
- **RWMutex.** For `Lock` and `Unlock`, as for `Mutex`. For a call of `l.RLock`, there is an *n* such that
  the *n*-th `l.Unlock` is synchronized before the return of that `RLock`, and the matching `l.RUnlock` is
  synchronized before the return of the (*n*+1)-th `l.Lock`.
- **WaitGroup.** A call of `wg.Done()`, or of `wg.Add` with a negative delta, that brings the counter to
  zero is synchronized before the return of every `wg.Wait()` it unblocks. The return of a function
  started with `wg.Task(f)` is synchronized before the return of every `wg.Wait()` it unblocks.
- **Once.** The return of the single call of `f()` from `once.Do(f)` is synchronized before the return of
  every call of `once.Do`.
- **Cond.** `c.Wait()` unlocks `c.L` and locks it again before it returns, so it synchronizes through
  `c.L` as `Lock` and `Unlock` do. A `c.Signal()` or `c.Broadcast()` that wakes a waiting `c.Wait()` is
  synchronized before that `Wait` returns (section 3.6, park and ready).
- **Scope.** See section 3.9.

### 3.5 `pluto/sync/atomic`

All operations of `pluto/sync/atomic` are **sequentially consistent**: there is one total order of all
atomic operations in the program, consistent with each task's sequenced-before order. An atomic read
observes the latest atomic write to its location before it in that order. If the effect of an atomic
operation *A* is observed by an atomic operation *B*, then *A* is synchronized before *B*.

A read-modify-write operation (`Add`, `Swap`, `CompareAndSwap`, `And`, `Or`) is one atomic operation; a
`CompareAndSwap` that fails is an atomic read. Atomic operations require natural alignment, which the
typed values (`atomic.Int64` and the others) always have.

`atomic.Fence()` is a sequentially consistent operation on no location. It takes a place in the total
order of atomic operations, and orders every plain, volatile and atomic access sequenced before it ahead
of every access sequenced after it, as observed by other processors and by devices. Fences are for device
drivers (publishing a descriptor in ordinary memory before writing a doorbell register in memory that is
not write-back cacheable) and for code that pairs with hardware. Tasks synchronize with each other through
atomic operations on a shared location instead.

### 3.6 Scheduler operations

The scheduler mechanism in `pluto/rt` (language spec section 19.5, The scheduler contract) gives these
guarantees, on which channels and `pluto/sync` are built:

- **Start.** Making a new task runnable is synchronized before its first operation (section 3.2).
- **Park and ready.** A call of `rt.Ready(t)` that makes a parked task `t` runnable, or that grants the
  permit a later `rt.Park` consumes, is synchronized before the return of that task's `rt.Park`.
- **Yield** is not synchronizing.
- **Preemption** cannot be observed: a task suspended by preemption and resumed later, on the same
  processor or another, observes its own earlier writes and every write that happened before its
  suspension, exactly as if it had not been suspended.

### 3.7 Owners and counted references

**Owners.** Moving an owner (language spec section 18.5), whether an `owned[T]`, a library owner such as
`vec.Vec[T]` or a program's own owner type, is an ordinary write of its bytes. Handing an owner to another
task takes one of the synchronizing operations above: the task statement or channel send that moves it is
that operation, so the receiving task observes everything the sender wrote to the owner and to the memory
it owns. Dropping an owner is ordinary code (its `Drop` method, then its fields), run by the task that
holds it. For the race rules, dropping, freeing or moving an object is a write of the whole object, and so is
assigning to or dropping an owner that holds it (language spec section 19.3, Frees are writes; section 19.4,
Sharing). So no task can still use an object while another task frees it.

**`synced[T]`** counts its references atomically:

- Every decrement of the strong count is a release, and the decrement that reaches zero is also an
  acquire. Every use of the object through any reference therefore happens before its `Drop` method runs.
  `Drop` runs on whichever task releases the last strong reference, and must not assume a particular one.
- The weak count follows the same rule. The memory is freed when both counts have reached zero, and the
  release that brings the second one to zero is synchronized before the free, so freeing never races with
  an `Upgrade` or a `Drop` still in progress.
- `Upgrade` is an atomic read-modify-write of the strong count that fails, without changing it, once the
  count has reached zero; a successful one never revives an object whose drop has begun. It synchronizes
  only through the count: the fields of `T` that change are guarded as language spec section 19.3 (Data
  race freedom) requires.
- An increment (copying a reference) is atomic and needs no ordering of its own, since the task that
  copies already holds a reference.

**`shared[T]`** and `weak[shared[T]]` counts are never atomic, which is why they cannot cross between
tasks (language spec section 19.2, The sendable rule).

**Strings and maps.** A string's buffer (language spec section 18.8) and a map's table (section 7.10)
are counted like a `synced[T]` object: every decrement is a release, and the decrement that reaches zero
is also an acquire before the memory is freed, so every use through any copy happens before the free.

**When counts are atomic.** The counts of `synced[T]` objects, string buffers, map tables, channels and
allocated errors use plain
arithmetic until the program starts its first task, and atomic operations from then on (language spec
section 18.7, Shared ownership); the switch cannot be observed by the program. A hosted program runs on
one thread until it starts its first task, and the runtime calls `rt.SetThreaded` at that point, before
the task can run, whatever the scheduler. The task start that follows is the synchronizing operation that
publishes every count written before it. A program that runs Plutonium code on another thread by other
means calls `rt.SetThreaded` first. A freestanding program counts atomically from the start, since its
kernel may run code on other processors without telling the runtime. `rt.SetThreaded` cannot be undone.
`shared[T]` counts are never atomic.

A string's bytes never change once a string uses them, so reading a shared string needs no lock. A map's
contents are ordinary locations, shared by every copy of the map.

**Allocated errors** (language spec section 16.7, Allocated errors and ownership) are `synced` objects
and follow its rules.

### 3.8 Allocators

An allocator returns zeroed memory, never takes back in `Alloc` memory it has handed out, and takes back
in `Free` only the memory it is given (language spec section 18.9, Allocators). An allocator used by
several tasks must also make each `Free(p, ...)` synchronized before any later `Alloc` that returns memory
overlapping `p`. Under these rules a task that receives freshly allocated memory never observes writes
from the memory's previous user. The hosted default allocator meets them, and an allocator given to
`alloc.SetDefault` must meet them once tasks run in parallel. An allocator that is not safe for
concurrent use (`alloc.Slab` without a lock, an `arena.Arena`) may be used by one task at a time only,
which is why every task starts with the program-wide default allocator, never with an override that the
starting task installed with `alloc.Use` (language spec section 19.1, Tasks).

An arena takes its memory back only in bulk, through `Reset`, `Release` or `Drop`. Everything allocated
through an arena's `Alloc` must be freed before the arena takes back its memory: a counted object made in
it (a string, a `shared` or `synced` box, an allocated error) is released, since releasing one writes its
count, and an owner made in it (a box, a vector's array, a map, a channel) is dropped, since it may have
moved to another task. `arena.Fixed` and `arena.Chunked` make this hold: each keeps an atomic count of its
live allocations, and `Reset`, `Release` and `Drop` panic while the count is not zero (language spec section
18.9). The plain views of `New` and `Slice` are not counted. For a type with drop glue the arena records
a drop entry instead, and `Reset`, `Release` and `Drop` run the entries, in reverse order of allocation,
in the task that calls them and before they read the count; a free that an entry's drop makes is a free
in that task (language spec section 18.9, Drop entries). An allocation may be freed in another task than
the one that uses the arena. That free changes only the arena's count, with an atomic decrement, and the
decrement that brings the count to zero is synchronized before the `Reset`, `Release` or `Drop` that reads
it. So a task that still holds a string, or an owner, made in another task's arena keeps the arena from
taking back that memory, and the owner of the arena panics instead of reusing it.

Taking memory back is a free of every object the arena reclaims, of every type, whether or not its type
has drop glue: a plain view of an integer struct from `New` is freed by the `Reset` as surely as an owner.
The race rules count it as a write of each such object (language spec section 19.3, Frees are writes), so
an object of the arena that another task may still read stops the program from compiling. The count and
the epochs do not stand in for that rule: plain views are not counted, and an epoch check is not atomic
with the read it guards (language spec section 18.11).

In a checked build, freeing heap memory gives it a new generation, and taking arena memory back
ends the incarnation of the reclaimed allocations (language spec section 18.11, Generation checks).
Fixed and Chunked ordinary views have the same guarantee, through static proof or runtime checking.
A use through a view of heap or arena memory after the free, reset or release is a
run-time panic when the free is ordered before the use: by being in the same task, or through a
synchronizing operation of section 3. A free that races with a use is a data race, which no check catches
reliably; safe code cannot make one (section 4.1).

The shared metadata directory publishes initialized bounds and tables before a tagged allocation is
handed out, and invalidates reclaimed memory before it is handed out again or returned to a parent.
Readers see consistent metadata that remains valid for their lookup, including the backing identity
of nested allocations (abi.md section 1.3). These rules protect metadata even when unrelated
allocations are reclaimed concurrently. They do not make a check and its program-memory access atomic
or permit a free to race with that access. Heap generation history persists across address and
allocator reuse; changing a mapping or layout does not restart it. Arena incarnation history likewise
survives backing reuse, and exhaustion cannot make an old view match new storage. This metadata
contract does not authorize an unsafe caller to race reclamation with an access.

### 3.9 Ending a scoped task

A task started by `s.Task(f)` on a `sync.Scope` is joined by `s.Wait()` and by the drop of `s` (language
spec section 19.4, Scoped tasks). The return of `f` is synchronized before the return of every `s.Wait()`
that waits for it, and before the end of the drop of `s`, wherever the drop runs: at the end of its block,
on a `return`, `break`, `continue` or `goto` out of it, or as `rt.ExitTask` leaves the frame. So the
starting function observes every write of its scoped tasks after the join.

A panic reclaims no frame: the panicking task's stack is not reused while the program runs, so scoped
tasks still running use valid memory until the program ends (language spec section 19.4).

**Normal termination.** After main's ordinary return cleanup, the runtime closes admission and checks
whether any application task is running or any application callback is in flight. If none is, package
cleanup runs, and all the operations of every finished task and callback, their mandatory cleanup and their
stack/task-record releases are synchronized before it begins (language spec sections 15.4 and 19.1). If
one is, the program ends at once and nothing is synchronized with it, as with `os.Exit`. Backend completion publication and observation supply the
required cross-processor visibility, through release/acquire or equivalent synchronization. No
programmer annotation or fence is required. Explicit cancellation counts as completion only after
saved arguments/captures and registrations are retired. This boundary is not an implicit join during
ordinary execution and does not replace section 19.3's race rules. Reclaimable runtime resources are
released before final allocation accounting, respecting dependencies on application allocators.

### 3.10 Timers

A `time.Timer` or `time.Ticker` delivers its time with a send on its channel, and `time.AfterFunc` starts
its function as a new task, so they synchronize as sections 3.3 and 3.2 say (language spec section 19.6,
Timers). A runtime timer (`rt.Timer`) runs its function with the lock of its timer heap held (abi.md
section 12.9): once `Stop` or `Reset` has returned, the function is not running, and a run of it that
started before the call happened before the call returned.

## 4. Data races

### 4.1 Safe code is free of data races

Every execution of a program made only of safe code is data-race-free, and so sequentially consistent
(section 2). A data race in safe code is not a run-time condition at all: the compiler rejects, over the
whole program and with no lifetime, borrow or region annotations, every program in which two tasks, or a
task and an interrupt handler, could make conflicting accesses that are not ordered (language spec sections
19.2 to 19.4). Every piece of memory that tasks and interrupt handlers share is then one of these:

- an owner moved from one task to another, which only the receiving task can then reach;
- memory that does not change while shared (freeing or moving it, replacing or dropping an owner that
  holds it, or a `Reset`, `Release` or `Drop` of an arena that takes it back, is a change), such as a package variable written only during package
  initialization or by `main` before it starts its first task, installs its first interrupt handler or
  enables interrupts;
- data that every access reads or changes with `pluto/sync/atomic` operations;
- data guarded by a lock that every access holds, the lock being inferred by the compiler: a package
  variable (rule 1 of language spec section 19.3), an object reached through a pointer that crosses to
  another task, whose changing fields are guarded by a lock of the same object or a package-level lock
  (rule 2), and the object of a `synced[T]` or the contents of a map that several tasks use (rule 3).
  For data that an interrupt handler accesses, the lock is an `rt.Lock` (language spec section 19.3,
  Interrupt handlers);
- a variable of a function whose `sync.Scope` joins the tasks that use it, synchronized by its type,
  guarded by a lock, or partitioned among the tasks (language spec section 19.4).

The proof uses locks, atomic operations, moves, the absence of writes, and scoped joins. It does not use
the order that channel operations, `sync.WaitGroup.Wait` and other ends of tasks impose on plain shared
data, although that order holds as section 3 states (it is what makes a moved owner visible to the task
that receives it). A program that relies on such an order to share a variable without a lock is rejected,
and holds the guarding lock as well.

### 4.2 Unsafe code, and word-sized locations

The race rules do not check unsafe code (language spec section 18.12, Safe and unsafe code): memory
reached through `unsafe.Pointer` or `rt.Pinned`, functions without a body, and the variables and records
of the runtime, which implements the rules above with its own locks and with preemption disabled (language
spec section 19.3). Such code can race. A data race is a bug, but it is not undefined behavior of
the program: the compiler may not assume that races are absent and use that to justify any
transformation (section 5.2). A race gives the torn or stale values of sections 4.2 to 4.4 and nothing
else directly. Those values can then break memory safety (section 4.3), so a race in unsafe code is a bug
in the code's safety contract, not a harmless one.

A plain read of a naturally aligned location of at most 8 bytes (every integer, float, boolean, pointer,
`uintptr`, `unsafe.Pointer`, func value, channel, map, `owned`, `shared`, `synced` and `weak` reference,
and packed value of 1, 2, 4 or 8 bytes) observes the value of some single write to that location that
section 2 allows it to observe. It never observes a mixture of bits from different writes, and never a
value nobody wrote.

### 4.3 Larger locations and packed values

Values larger than a word (strings, slices, interfaces, arrays, structs, packed values backed by more than
8 bytes) are read and written as several accesses. A racy read may observe some words from one write and
some from another. Such a torn value can break memory safety: a slice whose pointer came from one write
and length from another indexes outside its array, and an interface whose method table came from one
write and data word from another calls a method on the wrong kind of data. A string whose control word
came from another write than its data pointer counts the wrong buffer, and a string read while another
task overwrites the variable may lose its buffer to the writer's release before the reader takes its own
reference. Accesses that are not naturally aligned (through `unsafe`, or a field of a packed type that
does not sit on a boundary of its own size) may also tear.

A packed value is **one location**. Writing a field of a packed value reads the whole value (or the
storage unit containing the field), changes the field's bits and writes it back. Two tasks that write
different fields of the same packed value, or one that writes a field while another reads a different
field, conflict. Struct fields, by contrast, are separate locations. A packed register shared between
processors needs a lock or an atomic access to the whole value.

### 4.4 Races with volatile accesses

Volatile accesses are not atomic and do not synchronize between processors. A naturally aligned volatile
access of at most 8 bytes is one machine access of exactly its width, so section 4.2 applies to it. A
volatile access that races with a plain or volatile access from another processor is a data race.

## 5. What the compiler may and may not do

These rules bind every part of the compiler, the optimizer, the inliner and the backend included. They
are what makes section 4 hold.

### 5.1 Permitted

Between synchronizing operations, the compiler may reorder, combine and remove plain memory accesses in
any way that preserves the behavior of the task running alone:

- keep a location in a register across a loop and write it back later;
- remove a read whose value is already known (loads from the same memory state, a load of a value just
  stored);
- remove a write that is overwritten before any synchronizing operation, call or volatile access;
- move plain reads earlier and plain writes later within these bounds;
- in a checked build, let one generation or epoch check of a heap pointer cover later accesses through the
  same pointer, until a call that may free memory or an operation that synchronizes with another task
  (language spec section 18.11, Generation checks).

A consequence: a loop that spins on a plain variable written by another task may never observe the write,
because the read can be moved out of the loop. Spin on an atomic read instead. (Safe code cannot write
such a loop: the variable would need a guard or an atomic type, section 4.1.)

### 5.2 Forbidden

The compiler must not:

1. **Introduce writes.** It never writes to a location the program does not write on that path: no
   speculative stores (moving a store out of a conditional or loop that might not run it), no widening of
   a store to cover neighboring locations (writing a whole word to update one `byte` element or struct
   field), and no writing a value back unchanged. Writing a packed field as a read-modify-write of its
   storage unit is the program's own write of that location (section 4.3).
2. **Duplicate reads.** A single read in the source yields a single value. The compiler must not read a
   shared location twice and use each result in a different place, for example by reloading `*p` instead
   of keeping the first value. Spilling to stack slots, and recomputing only constants and addresses, meets
   this rule.
3. **Invent values.** A read of a word-sized location never yields a value composed from several writes
   or a value no write produced.
4. **Move accesses across synchronizing operations.** Every operation of section 3 (channel operations,
   task starts, lock operations, scheduler calls, and every `pluto/sync/atomic` operation, `Fence`
   included) is a full **compiler barrier**: no memory access sequenced before it is moved after it, and
   no access sequenced after it is moved before it. Calls of functions the compiler cannot see into
   (assembly, `//pu:extern`) are compiler barriers too. Atomic operations keep this property however they
   are compiled, as calls or as inline instructions.
5. **Touch volatile accesses.** Volatile accesses are never removed, merged, split, duplicated or widened,
   are performed as exactly one access of their own width, and are never reordered with respect to each
   other. No other memory access is moved across a volatile access in either direction (language spec
   section 7.15). In particular, a plain read that follows a volatile read must not be satisfied by a
   value read before the volatile read.
6. **Assume race freedom.** The compiler may not, for example, conclude that a pointer cannot be nil
   because another task would have to race to make it nil, or treat a racy read as undefined behavior of
   any kind.

### 5.3 Instructions

On x86-64, whose ordering is total store order, ordinary `MOV` loads and stores already have acquire and
release semantics, so sequentially consistent atomics need a fence only on the store side: atomic stores
use `XCHG`, and read-modify-write operations use `LOCK`-prefixed instructions. `Fence` is `MFENCE`. Plain
and volatile accesses compile to plain `MOV`s. On weakly ordered architectures (arm64, planned) the same
rules require `LDAR` and `STLR` for atomics and `DMB` for `Fence`; volatile accesses stay plain and do
not synchronize.

## 6. Kernels: interrupts, processors and schedulers

### 6.1 Interrupt handlers on the same processor

An interrupt handler (`//pu:interrupt`, language spec section 20.10) is asynchronous with the code it
interrupts. Same-processor instruction sequencing alone does not publish ordinary variables kept in
registers or permit unprotected plain sharing. The following protocols define visibility:

- Data that a handler shares with other code, while either side may write it, is accessed with atomic
  operations or under an `rt.Lock` (language spec section 19.3, Interrupt handlers), because a handler may run on any processor and on
  several at once. Volatile accesses and interrupt-disabled regions exclude only code on the same
  processor, so they are the checked protocol only for per-CPU data (section 6.4). Volatile operations keep their
  specified compiler ordering. For same-processor sharing, eligible accesses completed before handler
  entry are visible to the handler, and handler accesses are visible to eligible accesses after its
  return. These ordering edges apply only to the protected or volatile/atomic protocol, not every
  plain operation around an interrupt. Unprotected conflicting plain accesses race.
- A handler is entered through an interrupt gate, which disables maskable interrupts on its processor
  (language spec section 20.10), so its body is an interrupt-disabled region until `x86.Sti` or a call that
  may switch tasks. A handler marked `//pu:interrupt trap` is entered through a trap gate and its body is
  not such a region. Exceptions and nonmaskable interrupts are not excluded and stay a contract of the
  kernel.
- Disabling relevant local interrupts (`x86.Cli`, `x86.SaveFlagsCli`) and restoring the prior state
  (`x86.Sti`, `x86.RestoreFlags`) are compiler barriers and exclude those handlers on that processor
  during the interrupt-disabled region. Plain accesses there must follow the same exclusion protocol at every
  access. Restoring a previously disabled state must not enable interrupts prematurely.
- Volatile provides neither compound-operation atomicity nor mutual exclusion. An invariant requiring
  several accesses must use appropriate exclusion; a `ring[T, N]` alone is not a concurrent queue.
- An interrupt-safe spin lock disables relevant local interrupts before acquisition, excludes
  preemption that could strand the holder, releases the lock, then restores prior state. All users
  follow the protocol. Contention may rely on another processor's progress, never on interrupted
  execution resuming to release the lock. Nested exceptions/nonmaskable interrupts need a separate
  exclusion and progress protocol. Permitted spinning does not park and differs from forbidden
  interrupt-context waiting (language spec section 20.10).
- A new task starts with interrupts enabled, whatever the state of its starter (language spec section
  19.1); an interrupt-disabled region does not extend into a new task.

These same-processor rules create no synchronization between processors. Cross-processor sharing needs
atomic operations or a suitable lock. The compiler checks the forms above: atomics or an `rt.Lock` for shared
data, and volatile accesses or interrupt-disabled regions only for per-CPU data. Opaque assembly
implementations remain responsible for their explicit contracts.

### 6.2 Device memory

Accesses to memory-mapped device registers go through `volatile[T]`. Whether the device observes them in
order depends on the memory type the page tables give that memory (uncacheable, write-combining,
write-back), which the kernel decides. Ordering ordinary memory (a DMA descriptor) before a volatile write
to a device register needs nothing extra on x86-64 for write-back memory, because stores are not reordered
with other stores; it needs `atomic.Fence()` before the doorbell when the descriptor is in write-combining
memory, and on weakly ordered architectures.

### 6.3 A kernel's own scheduler

A freestanding program binds its scheduler backend at link time with `//pu:scheduler` (language spec
section 19.5; abi.md section 12). Every backend must provide the guarantees of section 3.6, because
channels, `pluto/sync` and task starts rely on them for section 3. The runtime does part of the work:
`rt.Switch` saves the context completely and is a compiler barrier, clears the old context's `onCPU` flag
with a release once it is saved, and waits for the new one's flag with an acquire, so no processor resumes
a task another is still saving; the park permit orders `rt.Ready` before the return from `rt.Park`; and
`rt.Preempt` saves the interrupted state. A kernel's backend must:

1. **Publish new tasks safely.** Putting a task on a run queue (its own processor's, or another's) is a
   release, and taking one off is an acquire: a lock or atomic operations on the queue. This makes the
   start of a task synchronized before its first operation even when another processor takes it.
2. **Order park and ready.** Making a parked task runnable is synchronized before the task resumes, again
   through the run queue's lock or atomics.
3. **Switch context completely.** A context switch saves and restores the full register state (and the FPU
   state, for code allowed to use it, language spec section 20.7) and is a compiler barrier. A task that
   moves to another processor observes its own earlier writes: the run queue's release and acquire provide
   this, and a backend that hands tasks over by other means needs a full fence on the way.
4. **Preempt invisibly.** Preemption from the timer interrupt saves the interrupted state exactly as the
   interrupt delivered it; nothing the task did may be lost or reordered by the switch.
5. **Keep its own critical sections non-preemptible.** Scheduler and allocator locks disable preemption,
   and interrupts where an interrupt handler can take the same lock, for their duration.
6. **Keep the stacks of joined frames.** A backend must not free or reuse the stack of a task that ended
   without returning from its frames until `rt.WaitScopes` has returned for it, so that the scoped tasks
   of those frames (section 3.9) never use reclaimed memory.
7. **Never wait in `Wake`.** Interrupt handlers reach the backend's `Wake` through `rt.Ready`, so `Wake`
   never blocks, allocates or switches, and the compiler checks it as it checks an interrupt handler
   (language spec sections 19.5 and 20.10).

A backend that meets these rules gives tasks the memory model of this document (language spec section
19.7), whatever its policy, as the hosted backend does. Rules 3 and 4
are met by switching only with `rt.Switch` and preempting only through `rt.Preempt`.

### 6.4 Per-CPU data

Per-CPU variables (`//pu:percpu`, language spec section 20.9) are ordinary locations, one copy per
processor, and are not subject to rule 1 of language spec section 19.3. A task that can be preempted and
moved to another processor may read one processor's copy and write another's. The compiler therefore checks
the accesses. A lone load or store of a whole per-CPU variable of at most a machine word is allowed
anywhere: it compiles to one `GS`-relative instruction and is atomic with respect to interrupts on the same
processor. Two such accesses in separate statements may reach the copies of two processors; that is defined
behavior, not a race. A store whose value or controlling condition depends, by data or control flow, on an
earlier load of the same variable (`x := ticks; ticks = x + 1`, `if current == idle { current = next }`) is
a read-modify-write: the task may move between the load and the store. It needs the same non-migrating
region as `ticks++` (E2022), and independent lone loads and stores stay allowed. Every other access (a compound assignment, `++`, a read and a write in one expression,
an access to a variable larger than a word or to part of one, a pointer-receiver method call, and `&v`)
must be inside a non-migrating region, and inside an interrupt-disabled region when an interrupt handler may
also access the variable (language spec sections 19.3 and 20.9). In that case every load or store through
a pointer from `&v` needs an interrupt-disabled region too, except a lone store of a whole variable of at
most a machine word that is the whole statement. Changing the GS base (`x86.WriteGSBase`, `x86.Swapgs`) is a compiler
barrier like every `pluto/arch/x86` intrinsic, so no per-CPU access moves across it; a pointer taken with
`&v` before the change still refers to the copy it was taken from.

## 7. Differences from other languages

This section is not normative. [Appendix A](language/a-differences-from-go.md) of the language
specification lists every rule of Plutonium, those of this document included, that differs from the
language whose syntax it follows: data races rejected at compile time in safe code, counted strings, maps
and channels instead of a collector, scoped tasks, packed and volatile types, and the kernel facilities of
section 6.

---

Portions of this document are adapted from *The Go Memory Model* (https://go.dev/ref/mem), copyright The
Go Authors, used under the Creative Commons Attribution 4.0 International License
(https://creativecommons.org/licenses/by/4.0/). The text has been modified: it is reordered, rewritten
and extended to define Plutonium's memory model, and several of its rules differ. The adapted material is
provided as is, without warranties, as that license states. The Go Authors do not endorse Plutonium.
