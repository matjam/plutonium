# Lightweight tasks: PlutoniumOS's one schedulable unit

**Date:** 2026-10-09
**Status:** PlutoniumOS architecture note, proposed. Written in the Plutonium repository and to move to the
PlutoniumOS repository as `docs/architecture/lightweight-tasks.md`. Section 16 lists the decisions that
need the user's sign-off; everything else follows from the direction the user has approved.

What PlutoniumOS needs from Plutonium is in a separate note that stays in the Plutonium repository:
**Plutonium's scheduler contract**, `docs/plans/2026-10-09-scheduler-contract.md`. References below to
"the language spec", "the memory model" and "abi.md" are to Plutonium's `docs/spec`.

## Contents

1. [Summary](#1-summary)
2. [The direction: one kind of task, one scheduler](#2-the-direction-one-kind-of-task-one-scheduler)
3. [Layering](#3-layering)
4. [What PlutoniumOS needs from Plutonium](#4-what-plutoniumos-needs-from-plutonium)
5. [The kernel scheduler](#5-the-kernel-scheduler)
6. [Kernel stacks](#6-kernel-stacks)
7. [The scheduler](#7-the-scheduler)
8. [Task lifecycle and costs](#8-task-lifecycle-and-costs)
9. [The userland runtime and its system calls](#9-the-userland-runtime-and-its-system-calls)
10. [Blocking I/O and interrupts](#10-blocking-io-and-interrupts)
11. [The user-space fast path](#11-the-user-space-fast-path)
12. [IPC](#12-ipc)
13. [Interaction with the language rules](#13-interaction-with-the-language-rules)
14. [Comparison with other systems](#14-comparison-with-other-systems)
15. [Phasing](#15-phasing)
16. [Decisions for the user](#16-decisions-for-the-user)

## 1. Summary

PlutoniumOS has one kind of schedulable unit, the **task**. A task is lightweight (a few hundred bytes of
kernel state, a small stack, creation in the low hundreds of nanoseconds), is scheduled preemptively by
the kernel across all cores from per-CPU run queues with work stealing, and is used for kernel work and
for userland alike. It is neither an OS thread in the Linux sense (a heavy object with a fixed 16 KiB
kernel stack) nor a cooperative coroutine (it is preempted by the timer and runs in parallel).

A Plutonium puroutine on PlutoniumOS **is** a kernel task. The userland puroutine runtime is a thin layer
of system calls over the kernel scheduler, so there is exactly one scheduler in the system. Hosted Linux
builds keep Plutonium's own work-stealing scheduler (language spec section 4.7), because Linux threads are
too heavy to be puroutines.

The work splits in two. Plutonium provides the language and runtime mechanism (`go`, channels, the
context switch, the preemption entry, task records, park and ready) and the scheduler contract, plus its
own scheduler for hosted Linux. PlutoniumOS provides the kernel scheduler as its own code, built on that
contract, together with the hardware glue and the OS layers. There is no kernel scheduler in Plutonium's
standard library: kernel code that ordinary applications cannot use belongs in the kernel.

The recommendation on kernel stacks (section 6) is one stack per CPU for entry from user space, with the
stack attached to a task only when the task blocks or is preempted in the middle of
kernel code, and continuations for the handful of hot blocking system calls, so that a parked user task
holds no kernel stack. Kernel puroutines keep small stacks of their own, sized by the compiler.

## 2. The direction: one kind of task, one scheduler

### 2.1 What a task is

| Property | Task |
|---|---|
| Who schedules it | the kernel, preemptively, on any core |
| Kernel state | a task record of about 256 bytes, plus an FPU save area only for tasks that use the FPU |
| Stack | small: the compiler's computed bound where it has one, otherwise a default (16 KiB in the kernel, 64 KiB of reserved and demand-committed address space in user space) |
| Where it runs | in the kernel (a kernel puroutine) or in a process (a user puroutine) |
| How it blocks | channel, lock, sleep or I/O: it parks, and whoever completes the event readies it |
| Creation | target 150 ns for a kernel task, 400 ns for a user task including the system call |

A process is an address space, a handle table and a set of tasks. It is not itself scheduled; its tasks
are. A process with one task looks like a classic single-threaded process, and one with a million tasks
is a normal Plutonium server.

### 2.2 Why not two schedulers

The usual way to get cheap concurrency on a conventional kernel is M:N scheduling: a user-space scheduler
(Go's, Erlang's, Tokio's) multiplexes many lightweight units onto a few kernel threads, and the kernel
schedules those threads. That is the right design on Linux, and the hosted Plutonium runtime uses it. On
an OS we write ourselves it is the wrong one, because two schedulers that cannot see each other duplicate
work and make each other's decisions worse:

- **Blocking system calls need handoff machinery.** When a puroutine makes a blocking call, the kernel
  blocks the whole worker thread, and every puroutine queued on that worker stalls with it. Go solves this
  with a monitor thread (`sysmon`) that notices a worker stuck in a system call and hands its run queue to
  a new or idle thread; the hosted Plutonium scheduler does the same (language spec section 4.7). That is
  extra threads, extra latency before the handoff, and a poller (epoll) that exists only to turn blocking
  I/O into something the user scheduler can wait on. With one scheduler, a blocking call parks one task
  and the CPU runs the next one. There is nothing to hand off.
- **The kernel cannot see tasks waiting on locks.** A puroutine blocked on a `sync.Mutex` or a channel is
  invisible to the kernel: the kernel sees a worker thread that is running (spinning or executing other
  puroutines) or idle. It cannot boost the lock holder, cannot account the waiter's time to anyone, and
  cannot tell an idle process from a busy one. Conversely, the user scheduler cannot see that the kernel
  has preempted the worker running a lock holder, so the other workers spin on a lock whose holder is not
  running at all.
- **Placement and priority are decided twice.** The kernel picks which CPU runs each worker thread; the
  user scheduler picks which puroutine runs on each worker. Neither knows the other's choice. A
  latency-sensitive puroutine (an audio callback, an input handler) can have a high priority in the user
  scheduler and still wait behind another process's work, because the kernel only sees threads of equal
  weight. Load balancing also runs twice: the user scheduler steals between workers while the kernel
  migrates the workers between CPUs, and the two can fight.

One scheduler that sees every task removes all three. The cost is that every park and ready of a user
task involves the kernel unless the fast path of section 11 avoids it, and section 11 shows how to keep
that cost small.

## 3. Layering

```
  PlutoniumOS repository
  +--------------------------------------------------------------------------+
  | OS layers: processes, address spaces, task system calls, handles, IPC    |
  +--------------------------------------------------------------------------+
  | kernel/task: the kernel scheduler (policy)                               |
  |   per-CPU queues, work stealing, classes and groups, time slices,        |
  |   timer wheel for sleep and timeouts, idle with hlt, wake placement      |
  |   implements rt.Scheduler; bound with //pu:scheduler                     |
  +-------------------------------------+------------------------------------+
  | hardware glue: SMP bring-up, timer, IPIs, page allocator, stacks          |
  |   (task.Platform, an internal boundary inside the kernel)                |
  +--------------------------------------------------------------------------+
                  | calls rt.Switch, rt.Preempt, rt.InitStack ...
                  v                 ^ runtime calls the Scheduler hooks
  Plutonium repository
  +--------------------------------------------------------------------------+
  | compiler and pluto/rt: mechanism and the scheduler contract              |
  |   go, chan, select, pluto/sync; rt.Switch; preemption entry; task        |
  |   records and stack setup; Park and Ready; current task slot;            |
  |   interrupt-context check; //pu:panic                                    |
  |   plus the hosted scheduler for Linux (another rt.Scheduler)             |
  +--------------------------------------------------------------------------+
```

### 3.1 Plutonium: mechanism and the contract

Always present when a program uses `go` or channels, in every build mode:

- `go`, channels, `select` and `pluto/sync`, written against the scheduler contract only;
- the context switch (`rt.Switch`), which saves and restores the callee-saved registers and the stack
  pointer;
- the preemption entry: a function an interrupt handler calls, on the interrupted task's stack, to
  suspend the current task and run another;
- task records and stack setup: the runtime part of a task, and building a new task's first frame;
- `Park` and `Ready`;
- the current task, reached through a reserved per-CPU slot (`GS`) in kernel mode and through `FS` in
  user space;
- the interrupt-context check (no blocking, no allocation, no `go` on any path from a `//pu:interrupt`
  handler or the panic handler) and the compile-time `//pu:panic` handler. Both are approved and being
  implemented;
- the **scheduler contract**: the interface a backend implements and the rules it follows, complete
  enough that any kernel can implement it with nothing from the standard library beyond `pluto/rt`
  (section 4);
- the hosted scheduler for Linux, which is one implementation of the contract.

Plutonium's standard library has no kernel scheduler. Kernel code that ordinary applications cannot use
belongs in the kernel that uses it. The compiler's test kernels in Plutonium's `testdata/kernel` stay
compiler tests: they may contain a minimal backend to test the contract in kernel mode, but they are not
a library and PlutoniumOS does not build on them.

### 3.2 PlutoniumOS: the scheduler, the glue and the OS

PlutoniumOS, in its own repository, provides:

- **the kernel scheduler**, a package of the kernel (`kernel/task` below) that implements Plutonium's
  scheduler contract for kernel and user tasks alike: per-CPU queues, stealing, classes, groups, timers,
  idle (sections 5 and 7);
- **hardware glue**: SMP bring-up, the timer, IPIs, the page allocator, kernel stacks (section 3.3);
- **OS layers**: processes and address spaces, the task system calls of section 9, the handle table and
  channels of section 12.

The scheduler talks to the glue through an internal interface (`task.Platform`, section 5.1). That keeps
it testable on its own and makes it possible, later, to publish it (or other PlutoniumOS components) as a
separate Plutonium package from the PlutoniumOS repository, for other kernels to import. Such packages are
never part of Plutonium's standard library.

### 3.3 Size of the kernel side

Estimates in Plutonium lines for x86-64 under QEMU and on current AMD and Intel machines:

| Part | What it does | Estimate |
|---|---|---|
| SMP bring-up | parse the ACPI MADT, start each AP with INIT-SIPI-SIPI through a real-mode trampoline, give it a GDT, TSS and IDT, set its GS block (abi.md section 10.6), call `task.EnterCPU` | 500 to 800, plus about 100 of assembly |
| Timer | local APIC timer in TSC-deadline or one-shot mode, calibrated against the HPET or PIT; a handler that calls `task.Tick` | 200 to 300 |
| IPIs | send a fixed vector to one CPU (x2APIC ICR write); a handler that calls `task.Reschedule` | 50 to 100 |
| Page allocator | a buddy or bitmap allocator over the firmware memory map | 400 to 700 |
| Kernel stacks | map stacks with an unmapped guard page below each, from a reserved region of kernel address space; needs the page table code the kernel has anyway | 150 to 250 |
| Glue total | | about 1,300 to 2,250 |
| Scheduler (`kernel/task`) | sections 5 and 7 | 2,000 to 3,000 |

The glue for a first QEMU kernel (phase 1, section 15) is smaller: a static pool of stacks can stand in
for the page allocator, and QEMU's fixed APIC frequency for calibration, so about 700 lines.

## 4. What PlutoniumOS needs from Plutonium

Plutonium's scheduler contract (`docs/plans/2026-10-09-scheduler-contract.md` in the Plutonium
repository) states what the runtime provides and what any backend provides. Two backends run on
PlutoniumOS:

- **the kernel scheduler** (section 5), PlutoniumOS's own code, for kernel puroutines and as the policy
  behind user tasks;
- **the userland runtime** (section 9), whose backend is a set of system calls into the kernel scheduler;
  it is `pluto/rt` for a PlutoniumOS target, in Plutonium, because every application uses it (decision 9).

The design below depends on these parts of the contract, each proposed there as a change to the contract
the concurrency work is implementing:

| Contract requirement | Used by |
|---|---|
| Park consumes a per-task permit, implemented once in the runtime, so a ready that arrives first is never lost | the userland backend, where a ready can always overtake a park (section 9); SMP wakeups |
| `Ready` is interrupt-safe, and the backend's `Wake` is checked as such | drivers waking tasks from interrupt handlers (section 10) |
| The backend allocates the task record and embeds `rt.Task` at offset 0 | the scheduler's queue links, class, group and timer fields (section 5) |
| The current task is a slot (per-CPU offset 8 in kernel mode, `FS`-relative in user space) holding the runtime's record, which may be in another address space than the backend's | user tasks, whose scheduler record is in the kernel (section 9) |
| A preemption count with a pending flag; `Park` with a non-zero count panics | spin locks, per-CPU data, `task.Pin` (section 13) |
| The preemption entry runs on the interrupted task's stack, with no full register frame | preempting kernel puroutines from the timer (section 7) |
| `onCPU` in the record, cleared by `rt.Switch` with release semantics | not resuming a task on one CPU while another still saves it (section 7.2) |
| Stack requests carry the compiler's proven bound | small unguarded stacks (section 8.3) |
| Runtime spin locks disable interrupts in kernel mode | non-blocking channel operations from interrupt handlers (section 10) |
| The backend is bound at link time with `//pu:scheduler`, replacing `rt.SetScheduler` | direct calls on the channel path; a build error instead of "no scheduler" |

Preempting a user task, which runs on no stack of its own while in user mode, is the kernel's business and
outside the contract (section 6.4).

## 5. The kernel scheduler

PlutoniumOS's scheduler is a package of the kernel, `kernel/task`. It implements Plutonium's
`rt.Scheduler` contract and is bound with `//pu:scheduler`; nothing in it comes from Plutonium's standard
library beyond `pluto/rt`, `pluto/arch/x86` and `pluto/sync/atomic`.

### 5.1 What the hardware glue hands it

```go
package task

// Platform is the hardware glue the rest of the kernel provides. Every method may be
// called with interrupts disabled; none may block or allocate from a blocking allocator.
type Platform interface {
	// CPUs is the number of processors that will call EnterCPU, fixed at Init.
	CPUs() int

	// InitCPU runs on each processor inside EnterCPU, before its first task:
	// arm the timer, unmask the IPI vector, anything else per-CPU.
	InitCPU(cpu int)

	// ArmTimer programs this processor's one-shot timer to fire at deadline
	// (nanoseconds on the Now clock). A kernel with only a periodic tick
	// ignores it and calls Tick on every tick.
	ArmTimer(deadline uint64)

	// Now is a monotonic clock in nanoseconds, the same on every processor.
	Now() uint64

	// SendIPI interrupts processor cpu so that it calls Reschedule.
	SendIPI(cpu int)

	// AllocStack returns a stack of at least size bytes for a kernel task,
	// with an unmapped guard page below lo unless noGuard is set. It may fail.
	AllocStack(size uintptr, noGuard bool) (lo, hi uintptr, ok bool)
	FreeStack(lo, hi uintptr)

	// SwitchSpace is called when the next task's Space differs from the
	// previous one: load CR3 (with PCID), the FS base, and so on. Kernels
	// without address spaces return at once.
	SwitchSpace(prev, next Space)
}

// Space identifies an address space. The scheduler only compares them.
type Space uintptr
```

The rest of the kernel wires these entry points into its boot code and handlers:

```go
// The contract backend, bound at link time (scheduler contract section 5).
//pu:scheduler
var sched scheduler

// Init gives the scheduler its platform and the allocator for task records.
// Called once, on the boot processor, before any go statement or channel
// operation; one before it panics with "scheduler not initialised".
func Init(p Platform, a alloc.Allocator)

// EnterCPU turns the calling processor into a scheduler processor: it calls
// p.InitCPU(cpu), runs tasks, and idles with sti; hlt when there are none.
// The boot processor calls it last, after starting the others. Never returns.
//pu:noreturn
func EnterCPU(cpu int)

// Tick is called from the kernel's timer interrupt handler, on the
// interrupted task's stack. It expires timers, charges the running task and
// preempts it if its slice is over or a better task is waiting.
func Tick()

// Reschedule is called from the kernel's IPI handler.
func Reschedule()
```

The kernel's timer handler is then three lines:

```go
//pu:interrupt
func timerIRQ(f *x86.InterruptFrame) {
	lapicEOI()
	task.Tick()
}
```

### 5.2 What it provides

Once `Init` has run, `go f(x)` and channels work on every processor that has called `EnterCPU`. Beyond
the `go` statement the package exports, for the rest of the kernel:

```go
type Class enum { RealTime; Normal; Idle }

type Task struct { /* rt.Task at offset 0, then the scheduler's fields */ }

func Current() *Task
func Spawn(f func(arg uintptr), arg uintptr, opts Options = Options{}) *Task // explicit class, priority, CPU, stack
func Yield()
func Sleep(d time.Duration)
func SleepUntil(deadline uint64)
func ParkTimeout(deadline uint64) (readied bool) // Park with a timeout; for timed channel and lock waits

func (t *Task) SetClass(c Class, priority int)  // RealTime: 0 to 31; Normal: weight
func (t *Task) SetAffinity(cpus CPUSet)
func (t *Task) SetGroup(g *Group)                // fairness group, section 7.3

type Group struct { /* weight, per-CPU queues */ }
func NewGroup(weight int, a alloc.Allocator = alloc.Default()) owned[Group]

// Pin disables preemption until the returned owner is dropped, which happens at
// the end of its scope:  { p := task.Pin(); ...; }  or early with p.Drop().
type Pinned owner struct { /* nothing */ }
func Pin() Pinned
func (p *Pinned) Drop()
func (p *Pinned) CPU() int

// Wait queues and timers for kernel code that waits on its own events.
type WaitQueue struct { /* spin lock, list */ }
func (q *WaitQueue) Wait()                 // park the current task on q
func (q *WaitQueue) WaitUntil(deadline uint64) bool
func (q *WaitQueue) WakeOne() bool         // interrupt-safe
func (q *WaitQueue) WakeAll() int          // interrupt-safe

func Stats(cpu int) CPUStats              // run queue length, switches, steals, idle time
```

`Pinned` is an owner so that its scope is lexical, which lets a later compiler check (section 13.3) look
for blocking calls inside it. `Pin` is `rt.DisablePreempt` and its `Drop` is `rt.EnablePreempt`.

### 5.3 How it implements the contract

| Contract hook | In `kernel/task` |
|---|---|
| `NewTask(req)` | pop a record from the per-CPU slab; take a stack from the per-CPU stack cache (an unguarded slab slot if `req.Proven` and small, otherwise `Platform.AllocStack`) |
| `Start(t)` | push onto this CPU's Normal queue in the creator's group (a release under the queue lock) |
| `Block()` | under this CPU's lock with interrupts disabled: if the task is still parking, mark it parked, pick the next task, `rt.Switch` |
| `BlockUntil(d)` | insert into this CPU's timer wheel, then as `Block`; the timer and a wake race on the state word |
| `Wake(t)` | interrupt-safe: place `t` (section 7.1, runnext or inbox), IPI the target CPU only if it is idle or runs a lower class; never switches |
| `Yield()` | put the current task at the back of its group's FIFO, pick, switch |
| `Resched()` | charge the current task, put it back runnable, pick, switch |
| `Exit()` | mark dead, switch away; the next task on this CPU frees the record and stack from the CPU's dead slot |
| `Now()` | `Platform.Now` |

`Tick` and `Reschedule` call `rt.Preempt` when the running task's slice is over or a more urgent task is
waiting, which respects preemption-disabled regions and lands in `Resched`.

## 6. Kernel stacks

The question is where a task's kernel-mode state lives while it is in the kernel. It matters most for
user tasks, because a kernel puroutine is code written in ordinary straight-line Plutonium with `go`,
channels and locks: it has a call stack while it is blocked, so it needs a stack of its own whatever the
kernel does. The options below are about user tasks entering the kernel through system calls, faults and
interrupts.

### 6.1 Option A: one kernel stack per task (Linux, Zircon, Windows, most kernels)

Every user task has a kernel stack (Linux: 16 KiB on x86-64, allocated with guard pages since 4.9). Entry
switches to it; a blocking path inside the kernel simply sleeps with its call stack intact; preemption in
the kernel is a context switch like any other.

- Kernel code is ordinary sequential code. Nothing about blocking is special.
- Preemption inside the kernel is straightforward: any point outside a preempt-disabled region can switch.
- The cost is per task and paid whether the task is in the kernel or not: at 16 KiB, a million tasks need
  16 GiB of kernel stacks. Even 4 KiB stacks (which leave little room for deep paths plus interrupt frames)
  cost 4 GiB per million. This is the main reason threads on conventional kernels are not lightweight.

### 6.2 Option B: one kernel stack per CPU, with continuations (seL4, Mach 3's continuations, Fluke)

Entry from user space runs on the CPU's kernel stack. When a system call must block, it does not sleep
with its call stack; it records a **continuation** in the task record (what to do when woken: a function
and a few words of state) and returns to the scheduler, which reuses the stack for the next task. When the
task is woken, the scheduler calls the continuation on whichever CPU runs it.

- A task outside the kernel, and a task blocked through a continuation, has no kernel stack at all. Kernel
  memory per task is the task record.
- Every blocking path in the kernel must be written as a continuation, which turns those paths into event
  handlers: the state that would have lived in locals is saved by hand in the task record, and the code
  after the blocking point is a separate function. seL4 accepts this because its kernel is tiny and
  formally verified, and it goes further: the kernel is not preemptible at all, and long operations have
  explicit preemption points that abort and restart the system call. Mach 3 applied continuations only to
  the most common blocking paths and kept per-thread stacks for the rest (Draves et al., 1991).
- Preemption inside the kernel is impossible with only one stack per CPU: the preempted code's stack would
  be overwritten by the next task. So either the kernel is non-preemptible (seL4) or the stack must be
  kept by the preempted task, which is option D.

### 6.3 Option C: small per-task stacks that grow on demand

Each task starts with a small kernel stack (one page, or less), and a fault on the guard page grows it.

- Growing on a fault in kernel mode requires the page fault handler to run on another stack (an IST
  stack), to allocate a page without blocking at an arbitrary point (possibly with locks held, possibly in
  an interrupt handler that overflowed), and to have somewhere to grow to (reserved address space per
  task). An allocation failure there has no recovery but a panic. No production kernel grows kernel stacks
  on faults; Windows grows them only at explicit call sites (`KeExpandKernelStackAndCallout`).
- Plutonium cannot move stacks (no collector knows where the pointers into them are), so growth means
  reserving the maximum address space per task up front, which brings back the per-task cost in page
  tables.
- Compiler stack analysis (language spec section 4.7) bounds most kernel paths exactly, which removes the
  need for growth where it applies and does not help where it does not (recursion, indirect calls through
  interfaces, which a kernel has in its VFS and driver layers).

Rejected for kernel stacks. Demand-committed **user** stacks (address space reserved, pages mapped on
first touch) are standard and are used in section 8.3; there the fault comes from user mode, where the
kernel is in a known state.

### 6.4 Option D (recommended): per-CPU entry stacks, attached to a task on demand

A refinement of option B that keeps kernel code sequential:

1. Entry from user space (system call, fault, interrupt) runs on the CPU's current entry stack. The entry
   code saves the user registers into the task record, not onto the stack.
2. The hot blocking system calls (park, the futex-style wait, channel receive in IPC, sleep, yield; about
   six paths, all in the scheduler and the IPC layer) are written as continuations. They end by
   returning a "block, then run this continuation" result to the dispatcher, the stack is reused, and the
   task holds no kernel stack while it waits. Resuming a task blocked this way runs its continuation (or,
   for the simple ones, just returns to user mode from the registers in the record).
3. Any other kernel path that blocks (a page fault that waits for disk, a file system call deep in a
   driver) blocks in the ordinary way. The CPU's entry stack is **attached** to the task: it now belongs
   to the task until the task returns to user mode, and the CPU takes a fresh entry stack from a per-CPU
   pool. Kernel code written in the ordinary style keeps working; it just costs a stack while it waits.
4. Preemption while a user task runs kernel code is the same: the timer interrupt attaches the stack to
   the preempted task and the CPU continues on a fresh one. So the kernel is preemptible everywhere
   outside preempt-disabled regions, unlike seL4.
5. Preemption of a task running in user mode needs no stack: the timer entry saved its registers into
   the record, and the scheduler resumes another task from its record with `iretq`.
6. Kernel puroutines (drivers, kernel services) have stacks of their own, sized by the compiler's
   analysis where it can and 16 KiB by default.

While a user task runs kernel code, the current-task slot holds its kernel record, so a channel operation
or lock in that code parks through the contract's `Park` like any kernel puroutine; the attached stack is
what `rt.Switch` saves on.

The number of kernel stacks in use is the number of CPUs, plus kernel puroutines, plus user tasks that
are at that moment blocked or preempted in a non-continuation kernel path. A million user tasks parked on
channels hold none.

```
  user task T makes a system call
          |
          v
  entry stub: save T's user registers in T's record; run on this CPU's entry stack
          |
          +-- returns normally ------------------------> restore from record, sysret
          |
          +-- hot blocking call (park, wait, recv, sleep)
          |     record continuation in T; T's stack frame is discarded
          |     CPU runs the next task on the same entry stack
          |
          +-- other blocking path, or preempted in kernel code
                the entry stack is attached to T and stays with it
                CPU takes a fresh entry stack from its pool
                when T resumes, it continues on the attached stack;
                on return to user mode the stack goes back to a pool
```

**Effect on how kernel code is written.** Ordinary kernel code is ordinary Plutonium: it can block on a
channel or a lock anywhere, with the cost of attaching a stack while it waits. Only the continuation paths
are special, and they are few, in the scheduler and the IPC layer, and written once. A driver author never
writes a continuation.

**Can the language help?** Two possibilities:

- A transformation that turns a function with blocking calls into a state machine (async/await, C++20
  coroutines). Rejected: it colours functions, hides an allocation for the frame, and contradicts the
  language's goal that costs are visible (language spec section 1).
- A checked form for the few continuation paths. A continuation path must not block anywhere except at
  its final step, which hands the continuation to the scheduler. That is the interrupt-context check
  (no blocking on any path) applied to a function other than an interrupt handler. Proposal: a directive
  `//pu:nonblocking` that applies the existing whole-program check to a function and everything it calls,
  with the same explanatory errors; the continuation handlers carry it, and they end with
  `return task.BlockThen(cont, state)`, which is not itself a blocking call (it returns to the
  dispatcher). This reuses machinery being built anyway and is a small follow-up (decision 1).

**Why not pure option B.** Kernel puroutines need their own stacks regardless, so a seL4-style kernel
with every blocking path as a continuation buys nothing for them, and it would require the whole OS
service layer (file systems, network stack) to be written as event handlers or moved to user space. That
is a valid microkernel position, but it is a decision about PlutoniumOS's structure, not about tasks, and
option D does not close it off: a kernel that keeps its services in user space simply has fewer
non-continuation paths.

**Why not option A.** It works and is simpler: per-task stacks of 8 KiB with guard pages would make
PlutoniumOS's user tasks about as heavy as a Linux thread's kernel side. The design goal is tasks cheap
enough that a process uses one per connection or one per request, and the kernel stack would be most of
each task's cost.

**Phasing.** Option D can arrive in steps without changing the design: in phase 2 (section 15) every
blocking system call uses the attach path, which behaves like lazily allocated per-task stacks; the
continuations for park, wait and sleep come next and make parked tasks stackless. Measurements of how
many tasks hold stacks in real workloads decide how far to take continuations.

**Interrupt stacks.** In phase 1, interrupts that arrive in kernel mode run on the interrupted kernel
task's stack, which therefore needs room for nested interrupt frames (about 1 to 2 KiB of headroom). A
later step moves interrupt handling to per-CPU interrupt stacks, with preemption done on the way out of
the interrupt, on the task's stack, as Linux does. That needs either an assembly exit stub in the kernel
or a small compiler addition to `//pu:interrupt` (an exit hook); it is listed in section 15.

## 7. The scheduler

### 7.1 Per-CPU run queues and classes

Each CPU has its own run queues, one per class, and a scheduler lock taken with interrupts disabled. The
pick order is RealTime, then Normal, then Idle.

```
  CPU n
  +-- RealTime: 32 FIFO lists, one per priority, and a 32-bit bitmap   (pick: find-first-set)
  +-- Normal:   list of runnable groups, deficit round robin          (pick: head group's head task)
  |               group A: FIFO of tasks
  |               group B: FIFO of tasks
  +-- Idle:     FIFO
  +-- runnext:  one slot for the task just readied by the running task
  +-- inbox:    lock-free list of tasks readied by other CPUs and by interrupts
```

- **RealTime** tasks run before anything else, by fixed priority (0 to 31), FIFO within a priority, with
  an optional round-robin slice. They are for drivers' bottom halves, audio and input. A RealTime task that
  becomes runnable on a CPU running a lower class preempts it at once (an IPI if it is another CPU). A
  RealTime task that never blocks starves everything below it on that CPU; that is the contract of the
  class, and a per-CPU budget (default 95% of each second, as Linux's `sched_rt_runtime_us`) keeps a
  runaway one from hanging the machine.
- **Normal** is the default. Fairness is between **groups** first and tasks second (section 7.3).
- **Idle** runs only when nothing else is runnable.
- **runnext** holds the task the running task has just readied (the receiver of a channel send). It runs
  next on this CPU, ahead of the queue, inheriting the rest of the slice; this keeps a producer and its
  consumer on one CPU with warm caches, and is what Go's `runnext` does. A task cannot hold the slot for
  more than one slice in a row, so a ping-pong pair cannot starve the queue.

All queue operations are O(1). Blocked tasks are on no run queue; they are on the wait queue of what they
wait for (a channel, a lock, a timer), so the scheduler's cost does not depend on how many tasks exist.

### 7.2 Work stealing

A CPU whose queues are empty steals before it idles:

1. It picks a victim: CPUs in its own cache domain first (L3 share), then the rest, starting at a random
   one to avoid convoys.
2. From the victim's Normal class it takes **half** of the runnable tasks of one group (the group at the
   tail of the victim's round-robin order, which would wait longest), and enqueues them as the same group
   on its own CPU.
3. It never steals RealTime tasks; RealTime placement is by push instead: when a RealTime task becomes
   runnable and its CPU is running a higher or equal RealTime priority, the waker looks for a CPU running
   something lower and places it there.
4. It does not steal a victim's `runnext` task, or a task that last ran on the victim less than a short
   interval ago (a few microseconds) if the victim is busy and will get to it soon, to keep cache affinity.

Stealing half balances in O(log n) steps from any imbalance, and amortises the cost of the remote lock
over many tasks. Spinning: a CPU that finds nothing spins and retries for a short time (a few microseconds)
before halting, because a halt and an IPI to wake it cost more than a short spin when work arrives in
bursts. Only a bounded number of CPUs spin at once (Go limits spinning Ms to half the busy Ps), so idle
CPUs do not all hammer the same queues.

### 7.3 Fairness

When tasks are cheap, fairness between tasks is the wrong goal: a process with a million runnable tasks
would get a million times the CPU of a process with one. The Normal class therefore schedules **groups**
fairly and tasks within a group FIFO:

- A group is a weight and its runnable tasks. Each process is a group by default; a kernel can put a
  process's tasks in several groups or several processes in one (a container, a user session).
- Groups are picked by deficit round robin: each group's deficit grows by a quantum proportional to its
  weight each round, and it runs while its deficit is positive. This is O(1) per pick and gives weighted
  fairness over a few rounds; it is not as precise as CFS or EEVDF over short intervals, and it does not
  need a tree.
- Within a group, tasks run FIFO with a fixed slice (2 ms by default). A task that blocks before its
  slice ends goes to the back of the group's FIFO when woken.
- Each CPU has its own view of each group with tasks on it (as CFS group scheduling keeps a per-CPU
  scheduling entity per group); weights are divided across CPUs by each group's share of runnable tasks,
  recomputed lazily when tasks are stolen. Exact global fairness is not a goal; bounded unfairness over a
  second is.

Kernel puroutines are in the kernel's group, which has a high weight; drivers' latency-critical work is in
RealTime.

### 7.4 Timers, sleep and timeouts

Each CPU has a hierarchical timing wheel (O(1) insert and cancel, as in Linux's timer wheel) of tasks
sleeping or waiting with a timeout. `ArmTimer` is programmed for the earlier of the current task's slice
end and the wheel's next expiry. With TSC-deadline timers this makes the kernel tickless whenever a CPU
has one runnable task (no tick needed) or none (no tick until the next timer). A timeout and a ready race
on the task's state word; whichever changes it first wins, and the loser does nothing.

### 7.5 Idle

A CPU with nothing to run and nothing to steal sets its idle flag in a shared bitmap and executes
`x86.StiHlt()` (the `sti; hlt` pair that no interrupt can split). A waker that places a task on a CPU's
inbox sends an IPI only if that CPU's idle flag is set; a busy CPU sees its inbox at its next pick. Later
power management (MWAIT with C-state hints, keeping idle work on few cores so the others can sleep
deeper) fits behind the same flag.

### 7.6 Scaling to millions of tasks

- Only runnable tasks are on run queues; blocked ones cost their record and whatever wait queue holds them.
- No operation walks all tasks: picks, enqueues, steals, wakes and timer operations are O(1) or bounded by
  the number of CPUs.
- No global locks on the hot path: per-CPU queues and inboxes, per-CPU timer wheels, per-CPU slab caches
  for task records and stacks.
- Task IDs are indexes into a per-process table with a generation count, so a stale ID never names a new
  task (section 8.4).
- Statistics are per-CPU counters, summed only when read.

### 7.7 NUMA (future)

Stealing already prefers CPUs in the same cache domain; NUMA adds a node level above that: steal within the
node first, steal across nodes only when a remote node has a long queue, allocate task records and stacks
from the node the task was created on, and give groups a home node. The data structures above are per-CPU
and need no change for it.

## 8. Task lifecycle and costs

### 8.1 Creation

Kernel task (`go f(x)` in the kernel):

1. pop a task record from the per-CPU slab cache;
2. pop a stack of the requested size from the per-CPU stack cache (stacks are kept mapped when freed);
3. copy the arguments and build the first frame so that `rt.Switch` into it calls the entry trampoline,
   which calls `f` and then exits;
4. push the record onto the local run queue (the `go` statement's memory-model guarantee comes from that
   push being a release, memory model section 6.3).

Target: 150 ns with warm caches. Cold (a new stack whose pages must be mapped) is dominated by mapping and
zeroing, about 1 to 2 microseconds for 16 KiB.

User task (`go f(x)` in a process): the user runtime takes a stack from its own cache (section 8.3),
builds the first frame in user memory, and makes the spawn system call, which allocates the kernel record
and queues it. Target: 400 ns warm.

### 8.2 Size targets

| Part | Kernel task | User task |
|---|---|---|
| Kernel record (rt part, scheduler part, saved user registers) | about 192 bytes | about 384 bytes |
| FPU save area | none (kernel code is `NoSSE` outside `//pu:fpu`) | 576 bytes (FXSAVE plus XSAVE header), only for tasks that have used the FPU |
| Kernel stack | its own: compiler bound or 16 KiB | none while in user mode or parked through a continuation |
| Runtime record in user memory | n/a | about 64 bytes |
| Stack | compiler bound or 16 KiB | compiler bound, or 64 KiB reserved and one page committed |

FPU state is saved eagerly on every switch (lazy FPU switching leaked state, CVE-2018-3665), but the save
area is allocated lazily: a task starts with the FPU in its initial state, and the switch-out path checks
`XINUSE` (XGETBV with ECX=1) and allocates an area only when some component is in use. A process that
never asks for AVX runs with XCR0 limited to x87 and SSE, which keeps the area at 576 bytes.

With these numbers a million parked user tasks cost about 400 MiB of kernel memory and, with one-page
stacks, 4 GiB of user stacks. The user stacks dominate; section 8.3 is how to reduce them.

### 8.3 Stacks and guard pages

- **Kernel task stacks** come from `Platform.AllocStack`: mapped pages with an unmapped guard page below,
  from a reserved region of kernel address space. A stack whose size the compiler proved (no recursion,
  no indirect calls on the spawned function's call graph) can never overflow, so it needs no guard page
  and can be smaller than a page, carved from a slab. The contract's stack request carries the proven bound for this (section 4).
- **Kernel stack overflow** hits the guard page; the page fault cannot push its frame onto the full stack,
  so the CPU raises a double fault, whose handler runs on its own IST stack and calls the panic handler
  with the overflowing task and its trace. This is the only use of IST in the design.
- **User stacks** live in a **stack region**: a large reservation in the process's address space, created
  with one system call, in which the kernel maps pages on first touch and never maps the guard page under
  each slot. The user runtime hands out slots itself, so spawning a task needs no memory system call, and
  the kernel tracks one region rather than one mapping per stack (a million mappings would be a million
  VMAs on Linux). Freed slots are kept committed in the runtime's cache for the next spawn; the runtime
  can tell the kernel to decommit cold slots.
- **Proven stacks in user space** work as in the kernel: a function whose bound is, say, 1.5 KiB gets a
  2 KiB slot with no guard page, from the same region. This is how Plutonium can match the density of
  Go's 2 KiB growable stacks without moving stacks: Go grows stacks because it cannot bound them; the
  compiler bounds them where it can and falls back to the guarded default where it cannot.
- **User stack overflow** is a page fault in user mode on a guard page. The kernel delivers it to the
  process's fault handler (an upcall on a per-process fault stack, like a signal on an alternate stack),
  which reports it through the process's `//pu:panic` handler as a stack overflow with the faulting task's
  trace; the process then exits.

### 8.4 Exit

A puroutine exits when its function returns. The entry trampoline calls `rt.Exit`, which marks the task
dead and switches away without returning. The stack cannot be freed while the exiting task is running on
it, so the next task (or the CPU's scheduler loop) frees it from a per-CPU "dead" slot. The record is
returned to the slab at the same point. A user task's exit system call does the same for the kernel
record; the user runtime returns the stack slot to its cache before making the call (the last few frames
run on it, which is safe because the slot is not reused until the exit completes).

IDs carry a generation: a user task ID is a 32-bit index into the process's task table and a 32-bit
generation, so `ready(id)` on a task that exited (and whose slot was reused) fails with an error instead of
waking a stranger. Inside the kernel, a parked task cannot exit, and a task is removed from every wait
queue before it can run again, so pointers in wait queues never refer to dead tasks.

A process exits when its last task exits or any task calls `exit` or panics; the kernel then stops its
other tasks (an IPI to every CPU running one), and frees them.

### 8.5 Switch cost targets

| Switch | Target |
|---|---|
| kernel task to kernel task, same CPU | 50 ns |
| user task to user task, same process, through park and ready system calls | 250 ns |
| user task to user task, different process (CR3 with PCID, FS base) | 500 ns |
| `switchto` system call (section 11) | 150 ns |
| user-space switch without entering the kernel (section 11) | 30 ns |

For comparison, a Linux thread switch between processes is around 1 to 2 microseconds, and Go's goroutine
switch through its scheduler is around 100 to 200 ns. These targets are to be measured in phase 1 and 2,
not promises.

## 9. The userland runtime and its system calls

### 9.1 The thin layer

In a PlutoniumOS process, `pluto/rt`'s scheduler backend is a set of system calls. Channels, `select`
and `pluto/sync` are the same code as everywhere else; they call `Park` and `Ready` and get the kernel.
The user runtime keeps only what must be in user memory: each puroutine's runtime record (reached through
`FS`), stack slots, and the per-task state word of section 11.

### 9.2 System calls

| Call | Arguments | Result |
|---|---|---|
| `task_spawn` | entry, argument pointer, stack top, FS base, class hint | task ID |
| `task_exit` | none | does not return |
| `task_park` | address of the caller's state word, expected value, deadline (0 for none) | readied, timed out, or interrupted |
| `task_ready` | task ID | ok, or no such task |
| `task_yield` | none | none |
| `task_sleep` | deadline (monotonic ns) | none |
| `task_switchto` (phase 3) | task ID | as `task_park`, for the caller, when it is next resumed |
| `wait_addr` / `wake_addr` (phase 4) | address in shared memory, expected value, deadline / count | futex semantics |
| `stack_region` | size | base address |

The clock is read without a system call from a page the kernel maps read-only into every process (as
Linux's vDSO data page), holding the TSC scale and offset.

### 9.3 Mapping the scheduler contract

| Contract operation | In a PlutoniumOS process |
|---|---|
| start (`go`, `rt.Go`) | user runtime takes a stack slot, builds the frame, `task_spawn` |
| exit | user runtime frees the slot to its cache, `task_exit` |
| `Park` | consume the permit in the state word if set (no system call); otherwise spin briefly, then `task_park` on the state word |
| `Ready(t)` | set `t`'s permit in its state word; `task_ready` only if `t` is parked in the kernel |
| `Yield` | `task_yield` |
| preemption entry | not used in user space: the kernel preempts tasks |
| current puroutine | `FS`-relative load of the runtime record |
| `pluto/sync` locks | built on `Park` and `Ready` with waiter lists in user memory, as Go's `sync.Mutex` is built on runtime semaphores |
| `time.Sleep` | `task_sleep` |
| timed channel and lock waits | `task_park` with a deadline |

`pluto/sync` does not need futex-style waiting on arbitrary addresses within a process: its waiter lists
live in the lock, and `Park`/`Ready` wake exactly the task it chose. Address-keyed `wait_addr` and
`wake_addr` are needed only for locks in memory **shared between processes**, where the waker cannot name
the waiter's task, and arrive with shared memory in phase 4.

**Lock holders preempted in user space.** A user-space spin lock (a channel's or a mutex's internal lock)
whose holder is preempted makes other tasks spin uselessly. Two measures: those locks spin only briefly
and then park, and the per-task state word has a "short critical section" bit that the runtime sets while
holding one; the kernel defers preemption of a task with that bit set by up to one short grace period
(say 20 microseconds), once, and sets a flag asking the task to yield when it clears the bit. This is the
idea behind Linux's restartable sequences and the time-slice extension proposals, and the kernel stays in
charge: a task that abuses the bit is preempted after the grace period anyway.

## 10. Blocking I/O and interrupts

A blocking system call parks the task: a read on an empty pipe or a socket with no data puts the task on
the object's wait queue (through a continuation, section 6.4), and the CPU runs the next task. There is
no handoff of run queues, no monitor thread and no poller, because the blocked unit is the task and the
scheduler sees it.

Interrupt-driven drivers wake tasks directly:

```
  device raises IRQ
        |
        v
  //pu:interrupt handler (kernel)        checked: cannot block, cannot allocate, cannot go
    acknowledge the device and the APIC
    move completed descriptors to the driver's ring (volatile or atomics)
    q.WakeOne() / rt.Ready(driverTask)  allowed in interrupt context
        |
        v
  driver task (kernel puroutine, RealTime class) runs on the next pick
    processes completions, readies the user tasks whose I/O finished
```

The handler does the minimum and readies a task; the rest runs in task context where blocking and
allocation are allowed. This is Linux's threaded interrupts, made the default. The interrupt-context rule
lets the handler call `Ready` and `WaitQueue.WakeOne` and nothing that blocks, and the compiler checks it
on every path from the handler.

Asynchronous I/O for a process that wants many outstanding operations without a task each (a submission
and completion ring, as io_uring) is not needed for concurrency here, since a task per operation is cheap;
it may still be worth having for throughput, and fits later as a kernel object like any other.

## 11. The user-space fast path

Channels between tasks of one process are the common case of communication, and every park and ready that
reaches the kernel costs a system call each way. Four levels, cheapest to implement first:

1. **No system call unless someone is asleep.** The per-task state word (in user memory, one word per
   task, registered with the kernel at spawn) holds the permit and whether the task is running, spinning
   or parked in the kernel. `Ready` sets the permit with an atomic operation and makes a system call only
   if the target is parked in the kernel. `Park` first spins briefly on its own word. A buffered channel
   whose consumer keeps up, or a producer and consumer that are both running, never enter the kernel. This
   is the futex idea applied to each task's own word, and it is part of phase 2.
2. **`task_switchto(t)`** (Google's switchto, proposed for Linux in 2020): the caller parks and `t` runs on
   the same CPU with the rest of the caller's slice, in one system call, without a run-queue insert, a
   pick or an IPI. A synchronous request and reply between two tasks (the RPC pattern) becomes two system
   calls with no scheduler work. The kernel stays the authority trivially: it performs the switch, does
   the accounting and can refuse (if `t` is not parked or is in another process).
3. **Scheduler activations or UMCG**: the kernel upcalls a user-level scheduler when a task blocks, and
   the user scheduler decides what runs on its CPUs (Anderson et al. 1991; Barrelfish dispatchers; Linux
   UMCG). This brings back a second scheduler with its own policy, which is what this design removes.
   Rejected.
4. **A switch entirely in user space** between tasks of the same process: the runtime saves the current
   task's registers in its user-side record, marks it parked in its state word, claims the target with an
   atomic compare-and-swap on the target's state word (only a task parked in user space and not on any
   kernel queue can be claimed), loads the target's registers and `FS` base, and records in a per-CPU
   shared slot which task is now running. The kernel reconciles lazily: at its next entry on that CPU
   (timer, system call, fault) it reads the slot and updates its records. This is the fastest (tens of
   nanoseconds), and the kernel keeps authority over **preemption** (the timer still fires and preempts
   whichever task the slot names) and over **placement** (a user switch only runs a task on the CPU the
   switching task already holds; it never moves tasks between CPUs, and a task readied by the kernel goes
   through the kernel's queues). The costs are the protocol's complexity, accounting that is approximate
   until the next kernel entry, and the requirement that the kernel trust nothing in the shared slot
   beyond "this task of this process is now running here", which it must validate.

Recommendation: level 1 in phase 2, level 2 in phase 3, and level 4 only if measurements of real programs
show system call entry and exit dominating after levels 1 and 2. On current AMD processors (no KPTI
needed) a system call round trip is about 50 to 100 ns, so level 2's 150 ns target is within a few times
of level 4, with none of its protocol.

## 12. IPC

### 12.1 Channels between processes

The primary IPC is a channel between processes, typed in Plutonium and moved by the kernel:

- A **channel endpoint** is a kernel object. A process refers to it by a **handle**, an index into its
  handle table with rights (read, write, transfer, duplicate). Creating a channel returns two endpoint
  handles; one is usually sent to the other process over an existing channel or given at spawn.
- In Plutonium, a handle is an owner: `type Handle owner uint32`, whose `Drop` closes it. Sending a handle
  on a channel moves it (the sender's table loses it and the receiver's gains it), which is the move rule
  of language spec section 6.4 and the handle transfer rule of Zircon, in one. `h.Dup(rights)` makes a
  copy with equal or fewer rights, as `Clone` does for other owners.
- `ipc.Chan[T]` wraps an endpoint with Go channel syntax where the language allows (send, receive,
  `select` with local channels and timeouts). A receive with no message parks the task through the
  continuation path of section 6.4; a send to a waiting receiver copies straight into the receiver's
  buffer and readies it, one copy and no intermediate kernel buffer.

### 12.2 What can cross an address space

Language spec section 4.7 says what may cross between puroutines in one process (the sendable rule). Across
processes the rule is stricter, because nothing that points into the sender's memory means anything to the
receiver. A type is **portable** if it is made only of:

- values with no pointers (integers, floats, booleans, enums, packed types, arrays and structs of these);
- handles (owners, moved);
- **transferable owners**: owner types whose whole content is a run of pages they own exclusively, such
  as `vm.Pages` (a page-aligned buffer allocated from the page allocator, not from a slab or an arena).

Views (`string`, `[]T`, `*T`, `unsafe.Pointer`), local channels, `owned[T]` and other heap owners
(`vec.Vec`, `hash.Map`, `strings.Buf`, whose memory is spread over the sender's heap), `shared[T]` and
`synced[T]` are not portable. `ipc.Chan[T]` with a non-portable `T` is a compile error that explains which
field is the problem and how to send it (copy the bytes into a `vm.Pages`, or encode it).

Checking this needs one addition to Plutonium, since the checker is Plutonium's: a predeclared constraint,
say `portable`, satisfied by exactly the types above, so that PlutoniumOS's IPC package declares
`ipc.Chan[T portable]`. Handles and transferable owners would be marked by a declaration the constraint
recognises (for example a `//pu:portable` directive on the owner type). The constraint is general (any
OS's IPC, or a serialisation library, could use it) and does not change any existing rule; it is built like
the sendable check. It is listed in Plutonium's scheduler-contract note, section 9, as a later requirement.

### 12.3 Zero-copy by moving pages

Sending a `vm.Pages` moves it, and the move rule guarantees the sender cannot use it afterwards. So the
kernel can transfer the pages instead of copying them: unmap them from the sender, map them into the
receiver, and hand the receiver a `vm.Pages` describing the new address. The type system enforces the
zero-copy discipline that other systems leave to convention (Mach's out-of-line memory with copy-on-write,
Zircon's VMOs).

- A view obtained from the owner before the send cannot be used after it in the same function (language
  spec section 6.3, views of a local owner); a heap view that escaped the compile-time check faults on the
  unmapped page instead of reading the receiver's data. Either way no memory is shared by mistake.
- Unmapping costs a TLB shootdown on every CPU that runs one of the sender's tasks. For small messages a
  copy is cheaper; the kernel transfers by remapping above a threshold (16 KiB to start with, to be
  measured) and copies below it, invisibly to the program.
- A sender that wants to keep using the data and share it read-only sends a **memory object** handle
  instead (section 12.4).

### 12.4 Shared memory

A memory object (a set of pages, like a Zircon VMO) can be mapped into several processes. The mapping is
an owner in each process (`vm.Mapping`), and views into it are bounded by that owner like views of any
local owner. Data in shared memory must be portable values, and changes are synchronised with atomics and
the `wait_addr`/`wake_addr` calls of section 9.2, since the processes share no runtime.

### 12.5 Type agreement

The two ends of an `ipc.Chan[T]` are compiled separately. Each side computes a fingerprint of `T` (a hash
of its layout and field names, from the type descriptor) and the endpoint records the first side's
fingerprint when it is typed; the second side's connect fails with a clear error if they differ. The
kernel compares an opaque 64-bit tag and knows nothing about types.

### 12.6 Security model

Capabilities only: a process can name and use only the objects it holds handles to, with the rights those
handles carry. There is no global namespace in the kernel (paths, service names and device names are
resolved by user-space services the process has handles to). A new process receives the handles its
parent passes at spawn and nothing else. The type system's part is to make handle transfer a move and a
duplicate explicit, so that leaking authority is as visible in the source as leaking memory.

## 13. Interaction with the language rules

### 13.1 Sendable, portable, synced and shared

- Within a process the sendable rule of language spec section 4.7 is unchanged: tasks of one process share
  an address space, so heap views and `synced[T]` cross between them as between puroutines anywhere.
- `shared[T]` stays confined to one puroutine, as now. On PlutoniumOS that means one task.
- Across processes only portable types cross (section 12.2). Neither `shared` nor `synced` is portable:
  the count and the object are in the sender's memory.

### 13.2 Panic handlers

- **The kernel** has one `//pu:panic` handler, as every freestanding build must. It stops the other CPUs
  (an NMI IPI, so a CPU spinning with interrupts disabled stops too), prints the panic and the trace of
  the panicking task to the console, and halts or reboots as the kernel chooses. The runtime's rules for
  nested panics and simultaneous panics on several CPUs apply unchanged. A panicking kernel puroutine is a
  kernel panic: tasks in the kernel share the kernel's memory, so there is nothing to isolate.
- **A process** has its own `//pu:panic` handler, a default one (print the message and trace on the
  process's error stream, exit with status 2) unless the program supplies another. A panic in any task
  ends the whole process, as in Go: the program's invariants may be broken anywhere in its memory, and
  other tasks in the same address space cannot be trusted to continue. The kernel then stops and frees the
  process's other tasks and closes its handles, which wakes peers blocked on its channels with a
  "peer closed" result.
- Isolation finer than a process is had by making the unit a process: a driver that should survive its
  own bugs runs as a user process with handles to its device's registers and interrupt, which is a
  PlutoniumOS structure decision outside this note.

### 13.3 Preemption in kernel code

Kernel puroutines are preemptible at every instruction outside preempt-disabled regions (with no garbage
collector every instruction is a safe point, language spec section 4.7). Regions that must not be preempted:

- spin locks (they disable preemption, and interrupts when an interrupt handler takes the same lock);
- per-CPU read-modify-write sequences (section 13.4);
- the scheduler itself.

Blocking inside such a region is a bug that can deadlock the CPU. The runtime checks it (`Park` with a
non-zero preemption count panics). Because `task.Pin` is an owner with a lexical scope, the compiler could
also check it statically: a call that may block, reached while a `Pinned` is live in the function, is the
same whole-program blocking analysis as the interrupt-context rule. Proposed as a follow-up once that
analysis exists.

### 13.4 Per-CPU data

The rules of memory model section 6.4 and abi.md section 10.6 apply unchanged: a single word-sized access
to a `//pu:percpu` variable is one `GS`-relative instruction and needs nothing; anything that reads and
then writes, or takes an address, must not migrate in between, so it runs inside `task.Pin()` (or with
interrupts disabled, which also prevents preemption). A cheap compile-time help, proposed for later: a
warning when one function both reads and writes the same per-CPU variable with no `Pinned` in scope and
interrupts not visibly disabled. `kernel/task` keeps its own per-CPU state (run queues, current
task, inbox) in `//pu:percpu` variables and touches it only with interrupts disabled.

### 13.5 Allocators and interrupt context

`Ready`, `WaitQueue.WakeOne`, `Tick` and `Reschedule` never allocate, so they pass the interrupt-context
check. `Spawn` allocates (a record and a stack) and so cannot be called from an interrupt handler, which the
same check reports. The per-puroutine allocator override (roadmap item 27) is part of the runtime's task
record, so each task has its own.

## 14. Comparison with other systems

| System | Unit | Who schedules | Stack per unit | Blocking system call | Preemption | IPC |
|---|---|---|---|---|---|---|
| Linux threads | thread (task_struct) | kernel (EEVDF since 6.6), per-CPU queues with load balancing | 16 KiB kernel, 8 MiB user (virtual) | blocks the thread | timer, anywhere preemptible | pipes, sockets, futex, shared memory |
| Go on Linux | goroutine on M:N | Go runtime (per-P queues, steal half) over kernel threads | 2 KiB user stack, grown by copying | `sysmon` hands the P to another thread; netpoller for I/O | async (signal) since Go 1.14 | channels within one process |
| Erlang | process | BEAM scheduler per core, migration and stealing | about 2.6 KB including the initial heap | async thread pool or dirty schedulers | by reduction count | message copy between private heaps |
| seL4 | thread (TCB) | kernel, fixed priority (256 levels), MCS scheduling contexts | one kernel stack (per CPU); user stack is the user's | blocking IPC is a continuation-style kernel path | kernel not preemptible except at explicit preemption points | synchronous IPC through endpoints, capabilities |
| Fuchsia (Zircon) | thread | kernel, fair and deadline schedulers | per-thread kernel stack | blocks the thread | timer | channels with handle transfer, ports, VMOs |
| Barrelfish | dispatcher, with user-level threads | per-core CPU driver schedules dispatchers (RBED); dispatchers schedule their own threads | per-dispatcher kernel state; user-level threads in user space | upcall to the dispatcher (scheduler activations) | timer, delivered as an upcall | message passing between cores (multikernel) |
| PlutoniumOS | task (= puroutine) | kernel only: per-CPU queues, steal half, groups, RealTime class | none while parked or in user mode; kernel tasks sized by the compiler | parks the task | timer, anywhere outside preempt-disabled regions | typed channels; handles and page transfer moved by type |

PlutoniumOS takes the scheduling structure of Go (per-CPU queues, stealing half, `runnext`, cheap units)
and puts it in the kernel, where Linux, Zircon and seL4 keep their schedulers; it takes the per-CPU kernel
stack and continuations from seL4 and Mach, but only for the hot blocking paths, with Mach's fallback of
attaching a stack so that the rest of the kernel stays ordinary code and stays preemptible; it takes
handle-carrying channels and capabilities from Zircon; and it rejects Barrelfish's and scheduler
activations' split of scheduling between kernel and user space, which is the double scheduling of section
2.2 in a more cooperative form. What none of these has is a language that can bound stacks and enforce
moves: Plutonium's stack analysis is what makes small stacks safe without growth, and its move rule is what
makes page transfer in IPC safe without copy-on-write.

## 15. Phasing

### Phase 0 (Plutonium): the contract

- In the Plutonium repository: the contract with the changes in Plutonium's scheduler-contract note, the
  hosted scheduler on it, and a compiler test kernel in `testdata/kernel` with a minimal single-CPU backend
  that proves the contract works in kernel mode. That test kernel is a compiler test only.

### Phase 1 (PlutoniumOS): the kernel scheduler under QEMU

- In the PlutoniumOS repository: `kernel/task` (per-CPU queues, stealing, classes, groups with one group
  at first, timer wheel, idle, `Pin`, `WaitQueue`, `Tick`, `Reschedule`, `EnterCPU`) and the first glue of
  section 3.3 (SMP bring-up, LAPIC timer, IPIs, a static stack pool), booted under QEMU with `-smp 4`.
- Tests: `go` spreads work across all four CPUs, a spinning task is preempted, channels ping-pong between
  CPUs, stealing balances a burst of tasks, `Sleep` and timed waits fire, a RealTime task preempts Normal
  work, an interrupt handler's `Ready` wakes a task, idle CPUs halt.
- Depends on phase 0, the interrupt-context check and `//pu:panic`. Measures the kernel-side targets of
  sections 8.1 and 8.5.

### Phase 2: userland tasks

- PlutoniumOS: user entry and exit stubs that save user registers into the task record, per-CPU entry
  stacks with attach-on-block (section 6.4, steps 1, 3 to 5), `FS` base per task, address-space switches,
  the system calls of section 9.2 except `task_switchto` and the address-keyed pair, stack regions, the
  fault upcall for user stack overflows, per-process panic and exit.
- Plutonium: a userland target for PlutoniumOS (a triple such as `x86_64-plutonium`, with its own build
  tag), whose scheduler backend is the system calls and the state-word protocol of section 11, level 1.
  A userland runtime is something ordinary applications use, so it belongs in Plutonium's `pluto/rt` for
  that target, as the Linux one does; it is the first compiler and driver work this note implies, and is
  not part of it.
- Then continuations for park, sleep and the state-word wait, so parked tasks hold no kernel stack.

### Phase 3: the fast path

- `task_switchto` and its use by unbuffered channels and synchronous request/reply in the runtime.
- Measurements on real programs; the user-space switch of section 11 level 4 only if they call for it.
- Per-CPU interrupt stacks with preemption on interrupt exit (section 6.4, "Interrupt stacks").

### Phase 4: IPC

- Handles and the handle table, channel endpoints, `ipc.Chan[T]`, the portable check in the type checker,
  type fingerprints, page transfer for `vm.Pages`, memory objects and `wait_addr`/`wake_addr` for shared
  memory.

## 16. Decisions for the user

1. **Kernel stacks.** Recommendation: option D (section 6.4): per-CPU entry stacks for user tasks,
   attached to a task only when it blocks or is preempted in a non-continuation kernel path;
   continuations for the hot blocking calls; kernel puroutines keep their own compiler-sized stacks; a
   `//pu:nonblocking` directive that reuses the interrupt-context check for continuation handlers. This
   follows the lean towards one stack per CPU without making the kernel non-preemptible or forcing kernel
   code into event-driven style.
2. **Fairness unit.** Recommendation: fairness between groups (a process by default), FIFO within a group,
   deficit round robin between groups (section 7.3). Alternative: per-task fairness (CFS, EEVDF), which lets
   a process with many tasks take the machine.
3. **Classes.** Recommendation: RealTime (32 fixed priorities, with a 95% per-CPU budget), Normal (groups)
   and Idle now; a deadline class (for audio with hard periods) later if fixed priorities prove
   insufficient.
4. **Park and ready primitive.** Recommendation: a per-task permit with a user-visible state word, so that
   ready costs a system call only when the target sleeps in the kernel (section 11, level 1); `pluto/sync`
   over park and ready; address-keyed futex calls only for shared memory between processes, in phase 4.
5. **Fast path.** Recommendation: `task_switchto` in phase 3; a pure user-space switch only if measurements
   ask for it. Scheduler activations and UMCG rejected as a second scheduler.
6. **Panic scope.** Recommendation: a panic in any task ends its process (Go semantics); a kernel puroutine
   panic is a kernel panic. Finer isolation comes from running code as its own process.
7. **IPC types.** Recommendation: a "portable" rule for `ipc.Chan[T]` (values without pointers, handles,
   and page-owning owners such as `vm.Pages`), checked through a predeclared `portable` constraint added to
   Plutonium, with page transfer above a size threshold and type fingerprints checked at connect.
8. **Default stack sizes.** Recommendation: kernel mode default 16 KiB (the spec's 64 KiB is for user
   space); user space keeps 64 KiB reserved with one page committed; proven bounds below a page get
   unguarded slots in both.
9. **Where the userland runtime lives.** Recommendation: in Plutonium's `pluto/rt` for a PlutoniumOS
   target, like the Linux runtime, since every application uses it; the kernel scheduler and everything
   else in this note stays in the PlutoniumOS repository.

The decisions about the Plutonium side (binding the backend at link time, the contract changes for the
concurrency work) are in Plutonium's scheduler-contract note, section 8.
