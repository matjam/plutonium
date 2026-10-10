# The Plutonium Memory Model

**Version:** 0.2 (draft)
**Date:** 2026-10-09

This document says when a read of a variable in one puroutine, or on one processor, is guaranteed to
observe a write made by another. It is [Go's memory model](https://go.dev/ref/mem) with the changes a
language without a garbage collector or a built-in scheduler needs, plus rules for interrupt handlers and
for kernels that bring their own scheduler. Where this document is silent, Go's memory model applies.

Several of the operations below belong to features that are specified but not yet implemented
(puroutines, channels, `pluto/sync`); see [the roadmap](../roadmap.md). The rules are written now so the
implementations have a contract to meet.

## Contents

1. [Advice](#1-advice)
2. [Definitions](#2-definitions)
3. [Synchronising operations](#3-synchronising-operations)
4. [Data races](#4-data-races)
5. [What the compiler may and may not do](#5-what-the-compiler-may-and-may-not-do)
6. [Kernels: interrupts, processors and schedulers](#6-kernels-interrupts-processors-and-schedulers)
7. [Differences from Go](#7-differences-from-go)

## 1. Advice

As in Go: a program that modifies data read by other puroutines must serialise those accesses, using
channels, `pluto/sync` or `pluto/sync/atomic`. If you need the rest of this document to understand your
program, you are being too clever.

`volatile[T]` is not a synchronisation tool between processors. It orders accesses to one device or
between an interrupt handler and the code it interrupts on the same processor (section 6), nothing more.

## 2. Definitions

A **memory operation** has a kind (plain read, plain write, volatile read, volatile write, or an
atomic or synchronising operation), a location, and the value read or written. A **location** is a
variable of basic type, a struct field, an array element, or a whole packed value (section 4.3).
Locations wider than a machine word may be accessed as several word-sized accesses (section 4.2).

Within one puroutine, an operation is **sequenced before** another when it comes first in the order
given by Go's evaluation rules.

Some operations are **synchronising** (section 3). For a pair of them, the rules below say when one is
**synchronised before** the other.

**Happens before** is the transitive closure of sequenced before and synchronised before.

A plain read *r* of location *x* may observe a write *w* to *x* when:

1. *w* happens before *r*, or neither happens before the other; and
2. there is no other write *w'* to *x* with *w* happens before *w'* happens before *r*.

A program is **data-race-free** when every pair of conflicting accesses (two accesses to the same
location, at least one a write, at least one not atomic) is ordered by happens before. Data-race-free
programs behave as if all puroutines ran interleaved on one processor (sequential consistency), as in Go.

## 3. Synchronising operations

### 3.1 Initialisation

Package initialisation runs in one puroutine, the one that will call `main.main`. Initialising a package
happens before initialising any package that imports it, and all initialisation happens before
`main.main` starts. A puroutine started during initialisation runs concurrently with the rest of it.

### 3.2 Starting a puroutine

The `go` statement that starts a puroutine is synchronised before the first operation of that puroutine.
The arguments, which are evaluated and copied when the `go` statement runs, are therefore visible to the
new puroutine without further synchronisation.

The end of a puroutine is not synchronised with anything. A program that waits for a puroutine uses a
channel or a `sync.WaitGroup`.

The same applies to `rt.Go(f, size)` and `rt.Go(f, size, a)`.

### 3.3 Channels

As in Go, for channels of any element type:

- A send is synchronised before the completion of the receive that takes that value.
- Closing a channel is synchronised before a receive that returns because the channel is closed.
- A receive from an unbuffered channel is synchronised before the completion of the send it pairs with.
- For a channel of capacity *C* > 0, the *k*-th receive is synchronised before the completion of the
  (*k*+*C*)-th send. This is what makes a buffered channel usable as a counting semaphore.
- `len` and `cap` of a channel are not synchronising.

A `select` statement that proceeds with a communication case synchronises exactly as that send or
receive would on its own. A `select` that takes its `default` case performs no synchronising operation.
A `for v := range ch` loop is a sequence of receives.

A channel is reference counted with an atomic count (language spec section 4.8). The release of the last
reference is synchronised before the channel's memory is freed, so freeing never races with a puroutine
that still held a reference.

### 3.4 `pluto/sync`

`pluto/sync` provides `Mutex`, `RWMutex`, `WaitGroup` and `Once` over the scheduler (status: planned).
Their guarantees are Go's:

- **Mutex.** For any `sync.Mutex` `l` and *n* < *m*, the *n*-th call of `l.Unlock()` is synchronised
  before the *m*-th call of `l.Lock()` returns. A successful `TryLock` is a `Lock`; a failed one is not
  synchronising.
- **RWMutex.** As for `Mutex` for `Lock`/`Unlock`. For a call of `l.RLock`, there is an *n* such that the
  *n*-th `l.Unlock` is synchronised before the return of `RLock`, and the matching `l.RUnlock` is
  synchronised before the return of the (*n*+1)-th `l.Lock`.
- **WaitGroup.** A call of `wg.Done()` (or `wg.Add` with a negative delta that reaches zero) is
  synchronised before the return of every `wg.Wait()` it unblocks.
- **Once.** The return of the single call of `f()` from `once.Do(f)` is synchronised before the return
  of every call of `once.Do`.

These hold whichever scheduler is installed, including a kernel's own (section 6.3).

### 3.5 `pluto/sync/atomic`

All operations in `pluto/sync/atomic` are **sequentially consistent**, as in Go: there is one total order
of all atomic operations in the program, consistent with each puroutine's sequenced-before order, and if
the effect of an atomic operation *A* is observed by an atomic operation *B*, then *A* is synchronised
before *B*.

A read-modify-write operation (`Add`, `Swap`, `CompareAndSwap`, `And`, `Or`) is one atomic operation; a
failed `CompareAndSwap` is an atomic load. Atomic operations require natural alignment; the typed values
(`atomic.Int64` and the rest) are aligned for you.

`atomic.Fence()` is a sequentially consistent operation on no location. It takes a place in the total
order of atomic operations and orders every plain, volatile and atomic access sequenced before it ahead of
every access sequenced after it, as observed by other processors and by devices. On x86-64 it is
`MFENCE`. Fences are for device drivers (publishing a descriptor in ordinary memory before writing a
device's doorbell register through memory that is not write-back cacheable) and for code that pairs with
hardware; puroutines synchronise through atomic operations on a shared location instead.

### 3.6 Scheduler operations

The scheduler interface in `pluto/rt` (language spec section 4.7) has four operations. Their guarantees,
which channel and lock implementations build on:

- **Start.** Making a new puroutine runnable is synchronised before its first operation (section 3.2).
- **Park and ready.** A call that makes a parked puroutine runnable is synchronised before that
  puroutine's return from its park call.
- **Yield** is not synchronising.
- **Preemption** is invisible: a puroutine suspended by preemption and resumed later, on the same
  processor or another, observes its own earlier writes and every write that happened before its
  suspension, exactly as if it had not been suspended.

### 3.7 Owned and counted pointers

`owned[T]` moves are ordinary writes; passing one to another puroutine needs one of the synchronising
operations above (a `go` statement or a channel send usually is that operation).

`synced[T]` counts references atomically. Every decrement of the count is a release, and the decrement
that reaches zero is an acquire, as in Rust's `Arc`: every use of the object through any reference
happens before its `Drop` method runs and before its memory is freed. `shared[T]`'s count is not atomic,
which is why it cannot cross puroutines (language spec section 4.7).

### 3.8 Allocators

An allocator used from several puroutines must make each `Free(p, ...)` synchronised before any later
`Alloc` that returns memory overlapping `p`, and must zero memory before returning it. Under that rule a
puroutine that receives freshly allocated memory never observes writes from its previous owner. The
hosted default allocator and `alloc.Slab` behind a lock meet it; `arena.Arena` is not safe for
concurrent use at all.

## 4. Data races

### 4.1 Go's position

Plutonium takes Go's position on races, not C's. A racy program is wrong, but it is not undefined: the
compiler may not assume races are absent and use that to justify arbitrary transformations. A race has the
limited consequences below and no others.

### 4.2 Word-sized and smaller locations

A plain read of a naturally aligned location of at most 8 bytes (every integer, float, boolean, pointer,
`uintptr`, `unsafe.Pointer`, func value, `owned`/`shared`/`synced` pointer, and packed value of 1, 2, 4
or 8 bytes) observes the value of some single write to that location that it may observe under section 2
rules. It never observes a mixture of bits from different writes, and never a value nobody wrote.

### 4.3 Larger locations and packed values

Values larger than a word (strings, slices, interfaces, arrays, structs, packed values backed by more than
8 bytes) are read and written as several accesses. A racy read may observe some words from one write and
some from another. Such a torn value can break memory safety: a slice whose pointer came from one write
and length from another indexes outside its array, and an interface whose method table came from one
write and data word from another calls a method on the wrong kind of data. As in Go, this is the reason
races on multi-word values are dangerous.

Accesses that are not naturally aligned (through `unsafe`, or a field of a packed type that does not sit
on a boundary of its own size) may also tear.

A packed value is **one location**. Writing a field of a packed value reads the whole value (or the
storage unit containing the field), changes the field's bits and writes it back. Two puroutines that write
different fields of the same packed value, or one that writes a field while another reads a different
field, race. This differs from struct fields, which are separate locations as in Go. A packed register
shared between processors needs a lock or atomic access to the whole value.

### 4.4 Races with volatile accesses

Volatile accesses are not atomic and do not synchronise between processors. A naturally aligned volatile
access of at most 8 bytes is one machine access of exactly its width, so section 4.2 applies to it. A
volatile access racing with a plain or volatile access from another processor is a data race.

## 5. What the compiler may and may not do

These rules bind every pass of the compiler, including the backend and the inliner. They are what makes
section 4 hold.

### 5.1 Permitted

Between synchronising operations, the compiler may reorder, combine and remove plain memory accesses in
any way that preserves the behaviour of the puroutine running alone:

- keep a location in a register across a loop and write it back later;
- remove a read whose value is already known (CSE of loads from the same memory state, store-to-load
  forwarding);
- remove a write that is overwritten before any synchronising operation, call or volatile access;
- move plain reads earlier and plain writes later within these bounds.

A consequence, as in Go: a loop that spins on a plain variable written by another puroutine may never
observe the write, because the read can be hoisted out of the loop. Spin on an atomic load instead.

### 5.2 Forbidden

The compiler must not:

1. **Introduce writes.** It never writes to a location the program does not write on that path: no
   speculative stores (moving a store out of a conditional or loop that might not execute it), no
   widening of a store to cover neighbouring locations (writing a whole word to update one `byte`
   element or struct field), and no writing a value back unchanged. Writing a packed field as a
   read-modify-write of its storage unit is the program's own write of that location (section 4.3).
2. **Duplicate reads.** A single read in the source yields a single value. The compiler must not read a
   shared location twice and use each result in a different place (for example, re-loading `*p` instead
   of spilling the first value to the stack). Plutonium's register allocator spills to stack slots and
   rematerialises only constants and addresses, never loads, so it meets this rule.
3. **Invent values.** A read of a word-sized location never yields a value composed from several writes
   or a value no write produced.
4. **Move accesses across synchronising operations.** Every operation in section 3 (channel operations,
   `go` statements, lock operations, scheduler calls, and every `pluto/sync/atomic` operation including
   `Fence`) is a full **compiler barrier**: no memory access sequenced before it is moved after it, and
   no access sequenced after it is moved before it. Calls to functions the compiler cannot see into
   (assembly, `//pu:extern`) are compiler barriers too. Today `pluto/sync/atomic` is implemented in
   assembly, so every atomic operation is such a call; if atomics become inline intrinsics, they must
   keep this property.
5. **Touch volatile accesses.** Volatile accesses are never removed, merged, split, duplicated or
   widened, are performed with exactly one access of their own width, and are never reordered with
   respect to each other. No other memory access is moved across a volatile access in either direction
   (language spec section 5.2). In particular, a plain read that follows a volatile read in program order
   must not be satisfied by a value read before the volatile read.

   **Known gap:** the compiler does not yet meet the last sentence. A volatile load does not start a new
   memory state, so CSE merges two plain loads of the same location on either side of a volatile
   polling loop (verified with `pluto build -S` on a loop that spins on a `volatile[uint32]` status
   register between two reads of a buffer). The fix is to make volatile loads produce a memory state, as
   stores do; it is listed in [the roadmap](../roadmap.md).
6. **Assume race freedom.** The compiler may not, for example, conclude that a pointer cannot be nil
   because another puroutine would have to race to make it nil, or turn a racy read into undefined
   behaviour of any kind.

### 5.3 Instructions

On x86-64 (TSO), ordinary `MOV` loads and stores already have acquire and release semantics at the
hardware level, so the only fence sequentially consistent atomics need is on the store side: atomic
stores use `XCHG`, and read-modify-write operations use `LOCK`-prefixed instructions. `Fence` is `MFENCE`.
Plain and volatile accesses compile to plain `MOV`s. On weaker architectures (arm64, planned) the same
rules require `LDAR`/`STLR` for atomics and `DMB` for `Fence`; volatile accesses stay plain and remain
non-synchronising.

## 6. Kernels: interrupts, processors and schedulers

### 6.1 Interrupt handlers on the same processor

An interrupt handler (`//pu:interrupt`, planned) runs on the processor it interrupts, between two
instructions of the interrupted code. A processor observes its own memory accesses in program order, so
only the compiler can make the handler see the interrupted code's accesses out of order, or not at all
(a value still in a register). The rules:

- Data shared between a handler and the code it interrupts on the same processor must be accessed through
  `volatile[T]` or `pluto/sync/atomic` on both sides. Volatile is enough on one processor: the compiler
  keeps volatile accesses in program order and does not move other accesses across them (section 5.2),
  and the processor does the rest.
- Plain accesses to such data race with the handler, with the consequences of section 4.
- Disabling interrupts (`x86.Cli`, `x86.SaveFlagsCli`) acts like acquiring a lock that every handler on
  that processor holds while it runs; re-enabling them (`x86.Sti`, `x86.RestoreFlags`) acts like
  releasing it. Both are compiler barriers (language spec section 11.4). Plain accesses between them are
  not observed by handlers on that processor until interrupts are enabled again.
- An interrupt handler's entry is synchronised after every operation of the interrupted code that came
  before the interrupted instruction, and its `iretq` is synchronised before every operation that comes
  after it. This is exact on one processor because the hardware preserves program order there; it is
  what lets a handler and the interrupted code use volatile accesses as a single-producer,
  single-consumer queue (language spec section 5.4).

Nothing about interrupts orders memory between processors. A handler that shares data with code running
on other processors needs atomics or a spin lock taken with interrupts disabled, as in any SMP kernel.

### 6.2 Device memory

Accesses to memory-mapped device registers go through `volatile[T]`. Whether the device observes them in
order depends on the memory type the page tables give that region (uncacheable, write-combining,
write-back), which is the kernel's business. Ordering ordinary memory (a DMA descriptor) before a
volatile write to a device register needs nothing extra on x86-64 for write-back memory, because stores
are not reordered with other stores; it needs `atomic.Fence()` before the doorbell when the descriptor is
in write-combining memory, and on weakly ordered architectures.

### 6.3 A kernel's own scheduler

A freestanding program installs its scheduler with `rt.SetScheduler` (language spec section 4.7). Every
scheduler must provide the guarantees of section 3.6, because channels, `pluto/sync` and `go` statements
rely on them for section 3. Concretely, a kernel scheduler must:

1. **Publish new puroutines safely.** Pushing a puroutine onto a run queue (its own processor's, or
   another's for work stealing) must be a release, and taking one off must be an acquire: use a lock or
   atomic operations on the queue. This makes the `go` statement synchronised before the puroutine's
   first operation even when another processor steals it.
2. **Order park and ready.** The ready operation must be synchronised before the parked puroutine
   resumes, again through the run queue's lock or atomics.
3. **Switch context completely.** A context switch saves and restores the full register state (and FPU
   state, for code allowed to use it, section 11.3 of the language spec) and is a compiler barrier. A
   puroutine that migrates to another processor must observe its own earlier writes: the run-queue
   release/acquire pair provides this, and a scheduler that hands puroutines over by other means needs a
   full fence on the way.
4. **Preempt invisibly.** Preemption from the timer interrupt saves the interrupted state exactly as the
   interrupt delivered it; nothing the puroutine did may be lost or reordered by the switch.
5. **Keep its own critical sections non-preemptible.** Scheduler and allocator locks disable preemption
   (and, where an interrupt handler can take the same lock, interrupts) for their duration.

A scheduler that meets these rules gives puroutines the same memory model as the hosted scheduler.

### 6.4 Per-CPU data

Per-CPU variables (proposed, `//pu:percpu`, language spec section 11.5) are ordinary locations, one copy
per processor. A puroutine that can be preempted and migrated may read one processor's copy and write
another's; such sequences must run with preemption (or interrupts) disabled. A single word-sized per-CPU
access compiles to one `GS`-relative instruction and is therefore atomic with respect to interrupts on
the same processor.

## 7. Differences from Go

| Topic | Go | Plutonium |
|---|---|---|
| Atomics | sequentially consistent | the same |
| Races on word-sized values | observe some written value | the same |
| Races on larger values | may tear | the same |
| Struct fields | separate locations | the same |
| Packed fields | n/a | the whole packed value is one location (section 4.3) |
| Volatile | n/a | ordered by the compiler; not synchronising between processors |
| Finalizers, GC | `runtime.SetFinalizer` synchronises | none; `synced[T]` drop ordering instead (section 3.7) |
| Interrupt handlers | n/a | section 6.1 |
| Scheduler | built in | any scheduler must meet section 3.6 |
| `Fence` | not provided | `atomic.Fence`, sequentially consistent, `MFENCE` |
