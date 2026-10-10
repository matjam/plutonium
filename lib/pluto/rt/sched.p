package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Tasks and the scheduler contract (language spec section 4.7, abi.md
// section 12).
//
// A puroutine is a task. The runtime provides the mechanism: the Task
// record every task carries, the context switch, parking with a per-task
// permit so that no wakeup is lost, preemption, and channels and pluto/sync
// built only on Park and Ready. A Scheduler, the backend, provides the
// policy and the memory: task records and stacks, which task runs on which
// processor and when, run queues, load balancing, the timer and idling.
//
// The backend is bound when the program is linked: a package-level
// variable marked //pu:scheduler, whose type implements Scheduler. The
// compiler generates the sched* hooks below to call its methods directly.
// Hosted builds bind the work-stealing backend in sched_linux.p unless the
// program binds its own; a freestanding program, such as a kernel, that
// starts puroutines or uses channels without binding one fails to build.

// DefaultStackSize is the stack size of a puroutine started by a go
// statement (language spec section 4.7).
const DefaultStackSize = 64 << 10

// MinStackSize is the smallest stack Go accepts; smaller requests are
// rounded up to it.
const MinStackSize = 8 << 10

const pageSize = 4096

// A Context is the saved state of code that is not running: a task, or a
// backend's own loop (a hosted worker's scheduling loop, a kernel's idle
// loop). Switch saves the running code's state in one Context and resumes
// another.
type Context struct {
	sp    uintptr  // stack pointer; RFLAGS and the callee-saved registers lie above it
	prev  *Context // the context that switched to this one, until it is finished
	task  *Task    // the task, or nil for a backend context
	onCPU uint32   // a processor runs it, or is still saving it
}

// Task status values. A task writes its own status before it gives up the
// processor; afterSwitch reads it once the switch has saved its state.
const (
	tRunning  = 0
	tParking  = 1 // in Park: wait for Ready
	tYielding = 2 // in Yield or preempted: runnable again at once
	tExiting  = 3 // its function returned: free it
)

// Permit states (Task.wake), changed atomically. Ready may come before the
// task it wakes has finished parking; the permit makes that safe.
const (
	wakeNone    = 0 // running, no wakeup pending
	wakePending = 1 // Ready came before Park finished: Park returns at once
	wakeParked  = 2 // switched out in Park: Ready hands it to the backend
)

// A Task is the runtime's record of a puroutine. The backend allocates it,
// embedded at offset 0 of its own task record, together with the stack
// (Scheduler.NewTask); the runtime owns every field. Backends keep track of
// tasks through their own records, or through Link and Data.
type Task struct {
	ctx      Context
	id       uint64
	status   uint32
	wake     uint32
	locks    int32  // preemption is disabled while non-zero
	pending  uint32 // a preemption was refused while locks was non-zero
	preempt  uint32 // an asynchronous preemption is in progress (hosted)
	resumePC uintptr
	stackLo  uintptr
	stackHi  uintptr
	fn       unsafe.Pointer // closure record of a func(unsafe.Pointer)
	arg      unsafe.Pointer
	alloc    Allocator // alloc.Use override, or nil for the program-wide default

	// Channel and select bookkeeping (chan.p).
	param      unsafe.Pointer // the waiting record a waker completed
	selectDone uint32
	rand       uint64

	insyscall bool    // between EnterSyscall and ExitSyscall (hosted)
	allocSite uintptr // see AllocSite

	// Link and Data are the backend's: a run-queue link and a word for its
	// own use. The runtime never touches them.
	Link *Task
	Data uintptr
}

// ID returns the task's number: 1 for the one that runs main, then
// increasing in the order tasks are started.
func (t *Task) ID() uint64 { return t.id }

// Context returns where the task's state is saved while it is not running.
func (t *Task) Context() *Context { return &t.ctx }

// Exited reports whether the task's function has returned: the backend's
// Park sees it for the last time, and FreeTask follows once the switch
// away from it is complete.
func (t *Task) Exited() bool { return t.status == tExiting }

// SetStack records the task's stack, [lo, hi), in a record that NewTask
// returns. The runtime places the arguments and the first frame at the top.
func (t *Task) SetStack(lo, hi uintptr) {
	t.stackLo = lo
	t.stackHi = hi
}

// Stack returns the task's stack as SetStack recorded it.
func (t *Task) Stack() (lo, hi uintptr) { return t.stackLo, t.stackHi }

// A StackRequest describes the stack a new task needs.
type StackRequest struct {
	// Size is how many bytes of stack the task may use. If Proven is set
	// the compiler has shown that the task's call graph never needs more,
	// so the stack needs no guard page; otherwise a guard page below it
	// (where the backend can make one) turns an overflow into a fault.
	Size   uintptr
	Proven bool
	// Alloc is the allocator the program asked for: the default
	// allocator, or the one given to Go. The backend should take the
	// stack from it.
	Alloc Allocator
}

// A Scheduler is the backend that runs tasks. The runtime calls it;
// programs do not.
//
// Every task passed to Spawn, Wake or FreeTask is switched out: its
// registers are saved and nothing runs on its stack. The runtime makes
// sure of this even when a wakeup races with a task that is still parking,
// and Switch waits for a context another processor is still saving (the
// onCPU flag), so a backend never resumes a task twice. A backend must meet
// memory-model.md section 3.6: putting a task on a run queue is a release
// and taking it off is an acquire.
type Scheduler interface {
	// NewTask allocates a task: the backend's record, with a zeroed Task
	// at offset 0, and a stack recorded with SetStack.
	NewTask(req StackRequest) *Task

	// FreeTask frees a task whose function has returned, after the switch
	// away from it.
	FreeTask(t *Task)

	// Spawn makes a new task runnable. Switching to t.Context() the first
	// time starts its function.
	Spawn(t *Task)

	// Park gives up the processor on behalf of t, the running task: it
	// picks what runs next here and switches to it with
	// Switch(t.Context(), next). The runtime calls it when t blocks (until
	// a Wake), when t yields or is preempted (the runtime calls Wake for it
	// as soon as the switch is complete), and when t has exited
	// (t.Exited()). It returns when some processor switches back to t. It
	// runs with preemption disabled.
	Park(t *Task)

	// Wake makes t runnable again. It is called on any processor and in any
	// context, including interrupt handlers and threads that run no tasks,
	// and must not block: it is under the interrupt-context rule.
	Wake(t *Task)

	// Yield is Park for a task that stays runnable: the runtime calls Wake
	// for t after the switch. A backend with nothing else to run may return
	// at once without switching.
	Yield(t *Task)
}

// Optional methods of a backend, which the hooks call if its type has
// them:
//
//	Init()             called once, before the first task operation, on the
//	                   processor that runs BootTask: set up processors,
//	                   threads, the timer (the hosted backend starts its
//	                   workers here)
//	InInterrupt() bool reports whether the processor is handling an
//	                   interrupt; there the runtime refuses operations that
//	                   block (Park, a blocking channel operation,
//	                   sync.Mutex.Lock, starting a task) with the panic
//	                   "blocking operation in interrupt context", while
//	                   non-blocking ones (select with a default case, Ready,
//	                   TryLock) are allowed

// The hooks, generated by the compiler (internal/lower/sched.go) from the
// program's //pu:scheduler variable. Each calls the method of the same name
// directly; the optional ones do nothing (or return false) when the
// backend's type lacks the method. A program that reaches one of them
// without a //pu:scheduler variable fails to build.

//pu:schedhook NewTask
func schedNewTask(req StackRequest) *Task

//pu:schedhook FreeTask
func schedFreeTask(t *Task)

//pu:schedhook Spawn
func schedSpawn(t *Task)

//pu:schedhook Park
func schedPark(t *Task)

//pu:schedhook Wake
func schedWake(t *Task)

// Yield is optional only for the build check: runtime locks, which every
// program uses, may yield when they release, but only once tasks have
// started, which needs the other hooks.
//
//pu:schedhook Yield optional
func schedYield(t *Task)

//pu:schedhook Init optional
func schedInit()

//pu:schedhook InInterrupt optional
func schedInInterrupt() bool

var (
	tasking  bool // tasks have started: the current-task slot is valid
	bootTask Task
	nextTID  uint64 = 1
)

// preemptParkFn is preemptPark, called by the asyncPreempt stub. It is a
// variable so that the stub does not pull the scheduler into programs that
// never start one; startTasks sets it.
var preemptParkFn func() uintptr

// startTasks starts the backend and adopts the running code as BootTask,
// the running task on this processor, the first time a task operation
// needs it: the first go statement, or the first operation that may park.
// A kernel points GS at this processor's per-CPU block before then.
func startTasks() {
	if tasking {
		return
	}
	schedInit()
	t := BootTask()
	t.ctx.task = t
	t.ctx.onCPU = 1
	preemptParkFn = preemptPark
	setCurrent(t)
	tasking = true
}

// BootTask returns the task the program started on, which runs package
// initialisation and main.
func BootTask() *Task {
	if bootTask.id == 0 {
		bootTask.id = 1
	}
	return &bootTask
}

// CurrentTask returns the running task, or nil in a backend's own context.
// It reads the processor's current-task slot (abi.md section 12.3).
func CurrentTask() *Task { return current() }

// checkBlocking panics if the processor is handling an interrupt.
func checkBlocking() {
	if schedInInterrupt() {
		panicMessage("blocking operation in interrupt context", "", "", "", "")
	}
}

// DisablePreemption stops the running task from being preempted, and so
// from moving to another processor, until the matching EnablePreemption.
// Calls nest. A preemption requested meanwhile happens at the last
// EnablePreemption.
func DisablePreemption() {
	if t := current(); t != nil {
		inc32(&t.locks)
	}
}

// EnablePreemption undoes one DisablePreemption.
func EnablePreemption() {
	if t := current(); t != nil {
		enablePreempt(t)
	}
}

// enablePreempt ends one disabled section of t, the running task, and
// yields if a preemption was refused during it.
func enablePreempt(t *Task) {
	dec32(&t.locks)
	if t.locks == 0 && t.pending != 0 && tasking {
		t.pending = 0
		Yield()
	}
}

// ----------------------------------------------------------------------------
// Switching

// Switch saves the running code's registers and RFLAGS in from and resumes
// the code saved in to, which continues from its own call of Switch (or
// starts, for a new task or a context made with MakeContext). It sets the
// processor's current-task slot to to's task (nil for a backend context).
// Switch returns when some processor switches back to from.
//
// After the switch, still inside Switch, the runtime finishes with the
// context that was left: it clears its onCPU flag (a release), completes a
// park, wakes a task that yielded or was preempted, or frees one that
// exited. A Switch to a context whose onCPU flag is still set, because
// another processor is still saving it, waits for the flag to clear.
func Switch(from, to *Context) {
	for load32(&to.onCPU) != 0 {
		pause()
	}
	to.onCPU = 1
	to.prev = from
	setCurrent(to.task)
	swapcontext(&from.sp, to.sp)
	finishSwitch(from)
}

// finishSwitch runs in the context self that was just switched to, and
// finishes with the context that switched to it.
func finishSwitch(self *Context) {
	p := self.prev
	self.prev = nil
	if p == nil {
		return
	}
	t := p.task
	store32(&p.onCPU, 0)
	if t != nil {
		afterSwitch(t)
	}
}

// afterSwitch finishes with t, which has just been switched out.
func afterSwitch(t *Task) {
	switch t.status {
	case tParking:
		if !cas32(&t.wake, wakeNone, wakeParked) {
			// Ready came while t was parking: it runs again at once.
			store32(&t.wake, wakeNone)
			schedWake(t)
		}
	case tYielding:
		schedWake(t)
	case tExiting:
		schedFreeTask(t)
	}
}

// MakeContext prepares c so that switching to it runs fn on the stack
// [stack, stack+size) with interrupts enabled. fn must never return. A
// backend uses it for the contexts its own loops run in.
func MakeContext(c *Context, stack unsafe.Pointer, size uintptr, fn func()) {
	f := ctxMain
	*c = Context{}
	c.sp = newContext(uintptr(stack)+size, uintptr(unsafe.Pointer(c)), *(*uintptr)(unsafe.Pointer(&f)), *(*uintptr)(unsafe.Pointer(&fn)))
}

// ctxMain starts a context made by MakeContext.
func ctxMain(c *Context, fn func()) {
	finishSwitch(c)
	fn()
	panicMessage("rt: a context function returned", "", "", "", "")
}

// newContext lays out the first frame of a new context below top: what
// swapcontext pops (RFLAGS with interrupts enabled, then the callee-saved
// registers) and goStart's address. goStart calls the closure record entry
// with a1 and a2. It returns the context's stack pointer.
func newContext(top, a1, entry, a2 uintptr) uintptr {
	top &^= 15
	x := top - 8 // returning here leaves RSP 16-byte aligned in goStart
	sp := x - 56
	frame := (*[8]uintptr)(unsafe.Pointer(sp))
	frame[0] = 0x202 // RFLAGS: IF and the reserved bit
	frame[1] = 0     // r15
	frame[2] = 0     // r14
	frame[3] = a2    // r13
	frame[4] = entry // r12
	frame[5] = a1    // rbx
	frame[6] = 0     // rbp
	frame[7] = goStartPC()
	return sp
}

// ----------------------------------------------------------------------------
// Parking

// Park suspends the running task until Ready is called for it. Each Ready
// matches one Park and grants a permit: if Ready comes first (from another
// processor, between the moment the task made itself known to its waker
// and its call of Park), Park consumes the permit and returns at once.
// Park panics if preemption is disabled.
//
//pu:blocking
func Park() {
	startTasks()
	t := current()
	if t == nil {
		panicMessage("rt.Park called outside a task", "", "", "", "")
	}
	if t.locks != 0 {
		panicMessage("blocking with preemption disabled", "", "", "", "")
	}
	checkBlocking()
	inc32(&t.locks)
	if cas32(&t.wake, wakePending, wakeNone) {
		dec32(&t.locks)
		return
	}
	t.status = tParking
	schedPark(t)
	t.status = tRunning
	enablePreempt(t)
}

// Ready makes t, which has called or is about to call Park, runnable. It
// never blocks and may be called from an interrupt handler.
func Ready(t *Task) {
	for {
		switch load32(&t.wake) {
		case wakeParked:
			if cas32(&t.wake, wakeParked, wakeNone) {
				schedWake(t)
				return
			}
		case wakeNone:
			if cas32(&t.wake, wakeNone, wakePending) {
				return
			}
		default:
			return
		}
	}
}

// Yield gives up the processor; the task stays runnable and continues
// later. It does nothing without a scheduler.
func Yield() {
	if !tasking {
		return
	}
	t := current()
	if t == nil {
		return
	}
	inc32(&t.locks)
	t.status = tYielding
	schedYield(t)
	t.status = tRunning
	dec32(&t.locks)
}

// ----------------------------------------------------------------------------
// Preemption

// Preempt preempts the running task from a timer interrupt handler that
// runs on the task's own stack, as a kernel's does: it switches to another
// task now, and the handler finishes (and returns from the interrupt) when
// the task is resumed. The handler's frame holds the interrupted state, so
// Preempt needs none. If the task has preemption disabled, it records the
// request and the task yields when it enables preemption again. Preempt
// does nothing in a backend's own context. The handler must acknowledge
// the interrupt before calling it.
func Preempt() {
	if !tasking {
		return
	}
	t := current()
	if t == nil || t.status != tRunning {
		return
	}
	if t.locks != 0 || t.preempt != 0 {
		t.pending = 1
		return
	}
	Yield()
}

// preemptAsync preempts the running task from a signal handler that runs
// on another stack (hosted builds): pc points at the interrupted
// instruction's address in the signal's saved context, which it redirects
// so that, when the handler returns, the task saves its full register
// state on its own stack, yields, and later resumes at the interrupted
// instruction (asyncPreempt in preempt_amd64.s). Every instruction is a
// safe point.
func preemptAsync(pc *uintptr) {
	if preemptParkFn == nil {
		return
	}
	t := current()
	if t == nil || t.status != tRunning || t.preempt != 0 {
		return
	}
	if t.locks != 0 {
		t.pending = 1
		return
	}
	t.preempt = 1
	t.resumePC = *pc
	*pc = asyncPreemptPC()
}

// preemptPark runs on the preempted task's stack, called by asyncPreempt
// once the registers are saved. It yields and returns the address to
// resume at.
func preemptPark() uintptr {
	t := current()
	pc := t.resumePC
	inc32(&t.locks)
	t.status = tYielding
	schedYield(t)
	t.status = tRunning
	dec32(&t.locks)
	t.preempt = 0
	return pc
}

// ----------------------------------------------------------------------------
// Starting tasks

// Go starts f as a new puroutine with a stack of size bytes (at least
// MinStackSize), allocated from a. It is the go statement with an explicit
// stack (language spec section 4.7). f must not capture variables: Go
// panics if f's closure record lies in the calling task's stack.
//
//pu:blocking
func Go(f func(), size uintptr, a Allocator = DefaultAllocator()) {
	rec := *(*unsafe.Pointer)(unsafe.Pointer(&f))
	checkGoFunc(rec)
	spawn(rec, nil, 0, 1, StackRequest{Size: size, Alloc: a})
}

// CheckGoFunc panics as a go statement does if f's closure record lies in
// the calling task's stack, for a library that starts f on a puroutine
// later from another one (time.AfterFunc): a function literal that
// captures variables, or a method value, made in the caller's frame.
func CheckGoFunc(f func()) {
	checkGoFunc(*(*unsafe.Pointer)(unsafe.Pointer(&f)))
}

// spawnGo implements the go statement: fn is the closure record of a
// compiler-generated function that takes the address of the argument
// block, args, which is copied onto the new task's stack. bound is the
// stack the compiler proved the call needs, or 0 if it could not.
//
//pu:blocking
func spawnGo(fn, args unsafe.Pointer, argSize, argAlign, bound uintptr) {
	req := StackRequest{Size: DefaultStackSize, Alloc: DefaultAllocator()}
	if bound != 0 {
		req.Size, req.Proven = bound, true
	}
	spawn(fn, args, argSize, argAlign, req)
}

// checkGoFunc panics if the closure record rec lies in the running task's
// stack: a function literal that captures variables, or a method value,
// whose record dies with the frame that made it.
func checkGoFunc(rec unsafe.Pointer) {
	t := current()
	if t == nil || t.stackHi == 0 {
		return
	}
	p := uintptr(rec)
	if p >= getsp() && p < t.stackHi {
		panicMessage("go of a function value that captures variables or binds a method value: its closure record lives in the starting frame; pass the values as arguments", "", "", "", "")
	}
}

func spawn(fn, args unsafe.Pointer, argSize, argAlign uintptr, req StackRequest) {
	startTasks()
	checkBlocking()
	if req.Alloc == nil {
		panicMessage("no allocator for the puroutine's stack: call alloc.SetDefault", "", "", "", "")
	}
	if req.Size < MinStackSize && !req.Proven {
		req.Size = MinStackSize
	}
	// The arguments sit at the top of the stack, above the first frame.
	if argAlign < 16 {
		argAlign = 16
	}
	req.Size += (argSize + argAlign + 15) &^ 15
	t := schedNewTask(req)
	if t == nil {
		panicMessage("out of memory starting a puroutine", "", "", "", "")
	}
	argp := (t.stackHi - argSize) &^ (argAlign - 1)
	if argSize > 0 {
		memmove(unsafe.Pointer(argp), args, int(argSize))
	}
	t.id = xadd64(&nextTID, 1)
	t.rand = t.id*0x9E3779B97F4A7C15 | 1
	t.fn = fn
	t.arg = unsafe.Pointer(argp)
	t.locks = 1 // until taskMain: a backend switches to it with preemption off
	t.ctx.task = t
	entry := taskMain
	t.ctx.sp = newContext(argp, uintptr(unsafe.Pointer(t)), *(*uintptr)(unsafe.Pointer(&entry)), 0)
	schedSpawn(t)
}

// taskMain is the first function of every task.
func taskMain(t *Task, _ uintptr) {
	finishSwitch(&t.ctx)
	t.status = tRunning
	dec32(&t.locks)
	var f func(unsafe.Pointer)
	*(*unsafe.Pointer)(unsafe.Pointer(&f)) = t.fn
	f(t.arg)
	inc32(&t.locks)
	t.status = tExiting
	schedPark(t)
	panicMessage("rt: an exited task was resumed", "", "", "", "")
}

// ----------------------------------------------------------------------------
// Runtime locks

// A Lock is a mutual exclusion lock for code below the scheduler: the
// runtime itself, channels and allocators. It never parks a task; a waiter
// spins and then, in hosted builds, sleeps its thread. Preemption is
// disabled while it is held, and in kernel mode interrupts are too, so an
// interrupt handler on the same processor cannot deadlock against it. The
// zero value is unlocked. pluto/sync.Mutex is the lock for programs.
type Lock struct {
	key   uint32
	flags uintptr // kernel mode: the interrupt flag before Lock
}

// Lock acquires l.
func (l *Lock) Lock() {
	if t := current(); t != nil {
		inc32(&t.locks)
	}
	lockAcquire(l)
}

// TryLock acquires l if it is free and reports whether it did.
func (l *Lock) TryLock() bool {
	t := current()
	if t != nil {
		inc32(&t.locks)
	}
	if lockTry(l) {
		return true
	}
	if t != nil {
		enablePreempt(t)
	}
	return false
}

// Unlock releases l.
func (l *Lock) Unlock() {
	lockRelease(l)
	if t := current(); t != nil {
		enablePreempt(t)
	}
}

// SpinWait executes the processor's spin-wait hint (PAUSE) n times: a
// short busy wait for a lock that another processor will release soon,
// which pluto/sync uses before it parks.
func SpinWait(n int) {
	for i := 0; i < n; i++ {
		pause()
	}
}

// ----------------------------------------------------------------------------

// fastrand returns a pseudo-random number from t's generator (xorshift64*),
// for select's order.
func fastrand(t *Task) uint64 {
	x := t.rand
	if x == 0 {
		x = 0x9E3779B97F4A7C15
	}
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	t.rand = x
	return x * 0x2545F4914F6CDD1D
}

// Atomics and the context switch, in ctx_amd64.s.
func swapcontext(save *uintptr, sp uintptr)
func goStartPC() uintptr
func asyncPreemptPC() uintptr
func getsp() uintptr
func inc32(p *int32)
func dec32(p *int32)
func load32(p *uint32) uint32
func store32(p *uint32, v uint32)
func cas32(p *uint32, old, new uint32) bool
func xadd32(p *uint32, delta int32) uint32
func xchg32(p *uint32, v uint32) uint32
func load64(p *uint64) uint64
func store64(p *uint64, v uint64)
func cas64(p *uint64, old, new uint64) bool
func xadd64(p *uint64, delta int64) uint64
func pause()
