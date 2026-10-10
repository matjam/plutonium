# The scheduler contract: what any kernel implements

**Date:** 2026-10-09
**Status:** proposed requirements for the scheduler contract that the concurrency work (roadmap items 14
to 16) is implementing. No compiler changes in this note. Section 8 lists the decisions for the user.

Plutonium provides puroutines, channels, `select` and `pluto/sync` as language and runtime mechanism, and
a scheduler only for hosted Linux. A freestanding program that uses them, such as a kernel, supplies the
scheduling policy itself. This note states what Plutonium must give such a program, and what the program
must give back, completely enough that any kernel can implement a scheduler with nothing from the
standard library beyond `pluto/rt`. There is deliberately no kernel scheduler library in the standard
library: code that ordinary applications cannot use belongs in the kernel that uses it, not in
Plutonium.

The first such kernel is PlutoniumOS, whose task model is described in PlutoniumOS:
docs/architecture/lightweight-tasks.md (github.com/matjam/plutoniumos, private). Its requirements are the
main input to this note, but nothing here is specific to it. A second backend is the PlutoniumOS userland
runtime, whose "scheduler" is a set of system calls; the contract has to fit that case too, and the third
is the hosted M:N scheduler.

## Contents

1. [Division of labour](#1-division-of-labour)
2. [What the runtime provides](#2-what-the-runtime-provides)
3. [What a backend provides](#3-what-a-backend-provides)
4. [Rules every backend follows](#4-rules-every-backend-follows)
5. [Binding the backend](#5-binding-the-backend)
6. [A minimal backend](#6-a-minimal-backend)
7. [Changes to the contract being implemented](#7-changes-to-the-contract-being-implemented)
8. [Decisions for the user](#8-decisions-for-the-user)
9. [Later Plutonium-side requirements from PlutoniumOS](#9-later-plutonium-side-requirements-from-plutoniumos)

## 1. Division of labour

```
  program code: go, chan, select, pluto/sync, time.Sleep
        |
        v
  pluto/rt (Plutonium): mechanism
    task records (runtime part), stack setup, rt.Switch,
    permit state machine for Park/Ready, preemption count,
    current task slot, preemption entry, exit trampoline
        |  calls the backend's hooks (direct calls, section 5)
        v
  backend (the program's own code, or pluto/rt's hosted one): policy
    run queues, choosing what runs, CPUs, timers, stacks,
    record allocation, idle, priorities
```

The names below (`rt.Task`, `rt.Switch`, `rt.Scheduler` and so on) are working names; the concurrency
work's names take precedence where they differ. What matters is the set of operations and their rules.

## 2. What the runtime provides

### 2.1 The task record

```go
package rt

// Task is the runtime's part of a task. A backend embeds it as the first
// field of its own record and allocates the whole record (section 3).
type Task struct {
	sp       uintptr   // saved stack pointer while not running (rt.Switch)
	state    atomic.Uint32 // running, parking, parked, runnable; plus the permit bit
	onCPU    atomic.Uint32 // 1 until rt.Switch has finished saving this task's context
	preempt  int32     // preemption-disable count
	pending  bool      // a preemption arrived while preempt > 0
	stackLo  uintptr   // stack bounds, for overflow reports and traces
	stackHi  uintptr
	allocOverride uintptr // the puroutine's alloc.Use override (roadmap item 27)
	// reserved words for the runtime's future use
}
```

The layout is fixed and documented in abi.md, so assembly in a kernel can reach `sp` and `onCPU`. A
backend may read every field and must write none except through the functions below.

### 2.2 Starting a task

```go
// StackRequest says what stack a new task needs.
type StackRequest struct {
	Size   uintptr // bytes: the compiler's bound, or the default size
	Proven bool    // the compiler proved Size is enough: no guard page needed
}

// InitStack builds t's first frame in [lo, hi) so that the first rt.Switch into t
// calls the exit trampoline's entry: it runs the spawned function with the copied
// arguments, drops what the function leaves, and calls the backend's Exit.
func InitStack(t *Task, lo, hi uintptr, entry uintptr, args unsafe.Pointer, argSize uintptr)
```

The `go` statement and `rt.Go` compute the stack request, ask the backend for a record (`NewTask`), copy
the arguments, build the frame with `InitStack`, and hand the task to the backend (`Start`). The first
frame is 16-byte aligned at the entry call, with the direction flag clear (abi.md section 2).

### 2.3 Switching

```go
// Switch saves the callee-saved registers and the stack pointer of the running
// task in from, clears from.onCPU (a release), stores to in the current-task
// slot, and resumes to. It returns, in from, when something switches back.
// A compiler barrier. Interrupts must be disabled by the caller.
func Switch(from, to *Task)
```

`Switch` saves RBX, RBP, R12 to R15 and RSP (the callee-saved set of abi.md section 2); everything else is
saved by the call to it. It touches no vector state: kernel code is `NoSSE` outside `//pu:fpu` brackets,
and Plutonium code never changes MXCSR or the x87 control word, so those are equal in every task of a
program. A backend that runs code that changes them (C linked into user tasks) saves them itself. A
backend that never switches in-process (a system-call backend) does not use `Switch`.

### 2.4 Park and Ready

The runtime implements the permit, so every backend gets the same semantics:

- `Ready(t)` sets `t`'s permit. If `t` was parking or parked, it moves `t` to runnable and calls the
  backend's `Wake(t)`. Ready is interrupt-safe: it never blocks or allocates, and it may be called from any
  CPU and from interrupt context.
- `Park()` consumes the permit and returns at once if it was set. Otherwise it marks the current task
  parking and calls the backend's `Block()`, which switches away unless a `Ready` has meanwhile made the
  task runnable.
- `ParkUntil(deadline)` is `Park` with a timeout, through the backend's `BlockUntil`. It reports whether
  the task was readied or timed out; a ready and a timeout racing are settled on the state word, and the
  loser has no effect.

A ready that arrives before the park is therefore never lost, which is what lets channels, `select`,
`pluto/sync` and a user-space runtime whose backend is a system call (where a ready can always overtake a
park) work without an unlock-and-park primitive.

### 2.5 Preemption

```go
func DisablePreempt()      // increments the current task's count
func EnablePreempt()       // decrements it; at zero, if a preemption is pending, calls the backend's Resched
func Preempt()             // the preemption entry (below)
```

`Preempt` is called by the backend, from an interrupt handler, on the interrupted task's stack, when it
wants the current task off the CPU (its slice is over, or a more urgent task is runnable). If the task's
preemption count is zero, `Preempt` calls the backend's `Resched`, which picks a task and switches to it;
when the preempted task is later resumed, `Preempt` returns into the handler, which returns with `iretq`.
Otherwise `Preempt` sets the pending flag and returns, and the switch happens at `EnablePreempt`. No full
register frame is needed: the `//pu:interrupt` prologue saved the caller-saved registers, and `Switch`
saves the callee-saved ones.

Preempting code that runs on a stack the task does not own (a user task entering a kernel on a per-CPU
stack) is outside this contract; the kernel's entry code saves such a task's registers itself and resumes
another task however it likes.

### 2.6 The current task

- **Kernel mode:** a per-CPU slot `rt.current` at offset 8 of the per-CPU block, right after
  `pluto.percpu.self` (abi.md section 10.6), reserved by the compiler the same way. `Switch` writes it; a
  backend that switches by other means writes it too. Channel and lock code reads it with one
  `GS`-relative load.
- **User space and hosted:** a slot at a fixed offset from the `FS` base. The hosted backend writes it on
  each switch; a kernel that schedules user tasks gives each task its own `FS` base, so the slot never
  changes for a task.

In both cases the slot holds the runtime's record, which a backend for user space may keep in user memory
apart from its own record in the kernel; the runtime never follows it to the backend's part.

### 2.7 Exit, faults and traces

- When a spawned function returns, the trampoline drops whatever the function did not move on and calls
  the backend's `Exit`.
- `rt.StackOverflow(t *Task, addr uintptr)` reports a stack overflow through the `//pu:panic` handler with
  `t`'s trace, for a kernel's double-fault or guard-page handler to call.
- `rt.TraceTask(t *Task, w func(line []byte))` prints the trace of a task that is not running, from its
  saved stack pointer, for a panic handler that wants every task's trace.

### 2.8 Runtime locks and interrupts

In kernel mode, the runtime's internal spin locks (a channel's lock, a `sync` primitive's waiter list) are
taken with preemption disabled and interrupts disabled, and released in reverse. Interrupt handlers may run
non-blocking channel operations (`select` with `default`) and `Ready`, which the interrupt-context rule
allows; an interrupt that tried to take a channel lock held by the code it interrupted, on the same CPU,
would otherwise deadlock.

## 3. What a backend provides

```go
package rt

// Scheduler is the contract a backend implements. Every method is called with
// preemption disabled. Methods marked interrupt-safe may also be called with
// interrupts disabled and from interrupt context, and must not block or allocate.
type Scheduler interface {
	// NewTask allocates a task record, with rt.Task at offset 0, and a stack
	// satisfying req; it sets the stack bounds. nil when out of memory, which
	// makes the go statement panic with "cannot start puroutine".
	NewTask(req StackRequest) *Task

	// Start makes a new task runnable. A release: the task's first operation
	// must observe everything the go statement wrote (memory model 3.2).
	Start(t *Task)

	// Block switches away from the current task, whose state is parking,
	// unless a Ready has made it runnable meanwhile. Returns when the task
	// is resumed.
	Block()

	// BlockUntil is Block with a deadline on the Now clock; false on timeout.
	BlockUntil(deadline int64) bool

	// Wake makes a parked or parking task runnable. Interrupt-safe.
	// Synchronised before the task's return from Park (memory model 3.6).
	Wake(t *Task)

	// Yield puts the current task at the back of its queue and runs another.
	Yield()

	// Resched picks the next task for this CPU and switches to it; the
	// current task stays runnable. Called by rt.Preempt and rt.EnablePreempt.
	Resched()

	// Exit switches away from the current task forever and frees its record
	// and stack once nothing is running on that stack. Never returns.
	Exit()

	// Now is a monotonic clock in nanoseconds, the same on every CPU.
	Now() int64
}
```

`time.Sleep`, timers and timed channel operations are written over `Now` and `ParkUntil`. Address-keyed
waiting (futex) is not part of the contract: `pluto/sync` is built on `Park` and `Ready` with waiter lists
in the lock, and only locks in memory shared between processes need address-keyed waits, which is a
kernel's own system-call business.

## 4. Rules every backend follows

These restate memory model sections 3.6 and 6.3 as obligations, with the ones the design above adds.

1. **Publish safely.** Putting a task on a run queue is a release and taking one off is an acquire (a
   lock or atomics on the queue), on the same CPU or another. This makes `Start` and `Wake` synchronise
   as the memory model requires, including when another CPU steals the task.
2. **Do not resume a task that is still switching out.** A task readied on one CPU while another CPU is
   still saving its context in `Switch` must not be resumed until `onCPU` is clear (an acquire load that
   pairs with `Switch`'s release). Linux's `on_cpu` check is the same rule.
3. **Switch completely.** Use `rt.Switch`, or save at least what it saves and set the current-task slot.
4. **Preempt only through `rt.Preempt`,** from an interrupt handler on the interrupted task's stack, so
   that preemption-disabled regions are respected. Never switch inside `Wake`: a wake from interrupt
   context marks the CPU as needing a reschedule, and the handler calls `rt.Preempt` before it returns.
5. **Keep critical sections non-preemptible.** The backend's own locks disable preemption, and interrupts
   too where `Wake` (interrupt-safe) takes the same lock.
6. **Interrupt-safe means checked.** `Wake` and everything it calls must pass the interrupt-context check
   (no blocking, no allocation, no `go`); the compiler applies it to the backend's `Wake` as it does to
   `//pu:interrupt` handlers.
7. **Stacks.** A stack for a request with `Proven` false has an unmapped guard page below it, or the
   backend accepts that an overflow corrupts memory (a test kernel may). A proven stack needs no guard.
   Overflow faults go to `rt.StackOverflow`.
8. **Exit frees later.** `Exit` cannot free the stack it runs on; it frees the record and stack after
   switching away (from the next task, or from a per-CPU idle context).
9. **The current-task slot is always right.** From the first instruction of a task to its last, the slot
   of section 2.6 holds its record, on whichever CPU it runs.

A backend that meets these gives puroutines the same memory model as the hosted scheduler.

## 5. Binding the backend

Today the spec installs a scheduler at run time with `rt.SetScheduler`, and a `go` statement before it
panics with "no scheduler". Proposal: bind the backend at link time, as the accepted `//pu:panic` design
binds the panic handler.

- A freestanding program that uses `go`, channels or `pluto/sync` must contain exactly one package-level
  variable marked `//pu:scheduler`, of a type that implements `rt.Scheduler`. Missing or duplicated, it is
  a build error that explains what a scheduler is and points at this contract.
- Hosted builds get `pluto/rt`'s own scheduler unless the program supplies one.
- The compiler calls the backend's methods directly (the type is known at link time), so channel
  operations pay no indirect call, and `Wake` can be checked under the interrupt-context rule.
- A program that never uses `go` or channels needs none, as today.

```go
//pu:scheduler
var sched kernelScheduler
```

This needs a small compiler change (the directive, the build check, devirtualised calls) and changes
language spec section 4.7, so it should be decided before the concurrency work lands.

## 6. A minimal backend

The contract is small enough that a kernel can implement it without help. A single-CPU round-robin backend
for a test kernel is about 150 to 250 lines: a record type embedding `rt.Task`, a static stack pool,
`NewTask` and `Exit` over the pool, a FIFO run queue under a lock that disables interrupts, `Block`,
`Yield` and `Resched` as "dequeue the next, `rt.Switch` to it", `Wake` and `Start` as an enqueue, `Now`
from the TSC, `BlockUntil` as a sorted list checked on each timer tick, and a timer handler that calls
`rt.Preempt` every few ticks. The compiler's test kernels in `testdata/kernel` should include such a
backend, booted under QEMU, as the compiler's own test that the contract works in kernel mode. It is a
compiler test, not a library: a real kernel's scheduler lives in that kernel's repository.

## 7. Changes to the contract being implemented

The concurrency work's contract, as described to this note (runtime: `rt.Switch`, task records, Park and
Ready, the preemption entry, a current-puroutine hook; host or kernel: policy), needs these to fit the
backends above. Its branch had no commits when this note was written, so this list is against the
description, not code:

1. Park consumes a **permit**, implemented once in the runtime (section 2.4), so a ready that comes first
   is never lost and backends do not each reimplement the state machine.
2. **`Ready` is interrupt-safe** and the backend's `Wake` is checked as such (sections 2.4, 4.6).
3. **The backend allocates the record** and embeds `rt.Task` at offset 0 (sections 2.1, 3), instead of
   the runtime allocating a fixed record with an opaque area.
4. **The current-task hook is a slot, not a call**: per-CPU offset 8 in kernel mode, an `FS`-relative slot
   elsewhere, holding the runtime's record, which may be in a different address space from the backend's
   (section 2.6).
5. **A preemption count with a pending flag** in the record; `Park` with a non-zero count panics with
   "blocking with preemption disabled" (section 2.5).
6. **The preemption entry runs on the interrupted task's stack** and needs no full register frame
   (section 2.5).
7. **`onCPU`** in the record, cleared by `Switch` with release semantics, for rule 4.2.
8. **Stack requests carry the proven bound** (section 2.2).
9. **Runtime spin locks disable interrupts in kernel mode** (section 2.8).
10. **Link-time binding** with `//pu:scheduler` (section 5), replacing `rt.SetScheduler`.

## 8. Decisions for the user

1. **Link-time binding of the backend** (section 5). Recommendation: adopt it, replacing `rt.SetScheduler`,
   for the same reasons as `//pu:panic`: direct calls on every channel operation, an explanatory build
   error instead of a run-time panic, and an interrupt-context check on `Wake`.
2. **The contract changes of section 7.** Recommendation: adopt all ten in the first version of the
   contract; each is small now and awkward to retrofit once kernels implement the interface.
3. **Address-keyed waiting stays out of the contract** (section 3). Recommendation: keep it out;
   `pluto/sync` needs only `Park` and `Ready`.
4. **The compiler's test kernel gets a minimal backend** (section 6). Recommendation: yes, as a compiler
   test in `testdata/kernel`, with no scheduler package in the standard library.

## 9. Later Plutonium-side requirements from PlutoniumOS

Not part of the contract, and not needed before PlutoniumOS's later phases, but they are Plutonium work
and are listed here because the PlutoniumOS task model lives in the PlutoniumOS repository:

| Requirement | Why | PlutoniumOS phase |
|---|---|---|
| A userland target for PlutoniumOS (`x86_64-plutonium` or similar, with a build tag), whose `pluto/rt` backend is PlutoniumOS's task system calls and per-task state words | ordinary applications use it, as they use the Linux runtime | 2 |
| `//pu:nonblocking`: the interrupt-context check applied to any function and its callees | checking the kernel's continuation handlers | 2 |
| A per-CPU interrupt stack with an exit hook for `//pu:interrupt`, or an agreed assembly stub | moving interrupt handling off task stacks, with preemption on interrupt exit | 3 |
| A compile-time check that nothing blocks while a preemption-disabling owner (such as a kernel's `Pinned`) is live | blocking with preemption disabled can deadlock a CPU | later |
| A warning for a read and a write of one `//pu:percpu` variable in a function with preemption not visibly disabled | per-CPU read-modify-write must not migrate | later |
| A predeclared `portable` constraint (values without pointers, and types marked transferable such as handles and page-owning owners) | typed IPC channels between address spaces | 4 |
