package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Timers (language spec section 19.6, abi.md section 12.9).
//
// A Timer calls a function once its deadline on the Now clock has passed,
// and again every period if it has one. Timers live in heaps that never
// allocate: each Timer is a node of an intrusive pairing heap, so adding,
// stopping and firing one take no memory, and work from interrupt context
// in a kernel. The hosted backend keeps a heap per processor, run by its
// workers between tasks and by the poller thread while they sleep
// (sched_linux.p). Every other backend shares one heap, which the program
// runs with RunTimers when the deadline it was given through the backend's
// ArmTimer has passed.
//
// A heap is guarded by its own runtime lock. A timer's function runs with
// that lock held, so Stop never returns while the function is running: once
// Stop has returned, the function has finished or will never be called,
// and the memory it uses may be freed.

// NoDeadline is the deadline of a heap with no timers, later than every
// deadline a timer can have.
const NoDeadline int64 = 1<<63 - 1

// activeTimers counts the timers waiting in heaps. The hosted backend does
// not report a deadlock while one is pending.
var activeTimers uint32

// A Timer runs a function at a time on the Now clock. Its zero value is not
// armed; Init gives it its function, and Reset arms it. A Timer must not be
// copied or moved, nor its memory freed, while it is armed.
type Timer struct {
	f      func(arg unsafe.Pointer, now int64)
	arg    unsafe.Pointer
	when   int64
	period int64

	// heap is the heap that holds the timer, set while it waits or its
	// function runs; it changes only with that heap's lock held, and is
	// read without the lock to find it.
	heap *timerHeap

	// Links of the pairing heap: the first child, the next sibling, and
	// the previous sibling or, for a first child, the parent.
	child, next, prev *Timer

	mu Lock // orders Reset and Stop of this timer
}

// Init sets the function t calls when it fires. f runs in whatever context
// runs timers (a hosted worker between tasks, the hosted poller
// thread, or a kernel's timer interrupt): it must not block, allocate or
// use timers, and the compiler checks it like an interrupt handler when an
// interrupt handler calls RunTimers. now is the Now reading that fired it.
func (t *Timer) Init(f func(arg unsafe.Pointer, now int64), arg unsafe.Pointer) {
	t.f = f
	t.arg = arg
}

// Reset arms t to fire at when on the Now clock, and every period
// nanoseconds after that if period is positive, replacing any deadline it
// had. A deadline that has passed fires as soon as timers next run. Reset
// reports whether t was armed. It never blocks, and may be called from an
// interrupt handler.
func (t *Timer) Reset(when, period int64) bool {
	if t.f == nil {
		panicMessage("rt.Timer.Reset of a timer without a function: call Init first", "", "", "", "")
	}
	if when < 0 {
		when = 0
	}
	if when >= NoDeadline {
		when = NoDeadline - 1
	}
	if period < 0 {
		period = 0
	}
	timerStart()
	t.mu.Lock()
	active := t.stopLocked()
	h := timerHeapLocal()
	h.lock.Lock()
	t.when = when
	t.period = period
	h.insert(t)
	storep(unsafe.Pointer(&t.heap), unsafe.Pointer(h))
	xadd32(&activeTimers, 1)
	if h.root == t {
		timerEarlier(h, when)
	}
	h.lock.Unlock()
	t.mu.Unlock()
	return active
}

// Stop disarms t and reports whether it was armed. If t's function is
// running, Stop waits for it to return; once Stop returns, the function is
// not running and will not run until the next Reset. It never blocks a
// task, and may be called from an interrupt handler, but not from a
// timer's function.
func (t *Timer) Stop() bool {
	t.mu.Lock()
	active := t.stopLocked()
	t.mu.Unlock()
	return active
}

// stopLocked takes t out of its heap; t.mu is held.
func (t *Timer) stopLocked() bool {
	for {
		h := (*timerHeap)(loadp(unsafe.Pointer(&t.heap)))
		if h == nil {
			return false
		}
		h.lock.Lock()
		if t.heap == h {
			// With the heap's lock held, a timer that names the heap is in
			// it: a firing timer leaves its heap before the lock is let go.
			h.remove(t)
			storep(unsafe.Pointer(&t.heap), nil)
			xadd32(&activeTimers, -1)
			h.lock.Unlock()
			return true
		}
		h.lock.Unlock()
	}
}

// Active reports whether t is armed.
func (t *Timer) Active() bool { return loadp(unsafe.Pointer(&t.heap)) != nil }

// ----------------------------------------------------------------------------
// The heaps

// A timerHeap is a pairing heap of timers ordered by deadline. next, the
// root's deadline or NoDeadline, can be read without the lock.
type timerHeap struct {
	lock  Lock
	root  *Timer
	next  int64
	armed int64 // the deadline last given to the backend's ArmTimer
}

// globalTimers is the heap of backends that keep none of their own.
var globalTimers = timerHeap{next: NoDeadline, armed: NoDeadline}

// meld joins two heaps, whose roots have no siblings, and returns the root.
func meld(a, b *Timer) *Timer {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if b.when < a.when {
		a, b = b, a
	}
	b.prev = a
	b.next = a.child
	if a.child != nil {
		a.child.prev = b
	}
	a.child = b
	return a
}

// mergePairs melds the sibling list starting at first into one heap, in
// two passes: pairs from the left, then the results from the right.
func mergePairs(first *Timer) *Timer {
	var stack *Timer
	for first != nil {
		a := first
		b := a.next
		first = nil
		if b != nil {
			first = b.next
			b.next, b.prev = nil, nil
		}
		a.next, a.prev = nil, nil
		m := meld(a, b)
		m.next = stack
		stack = m
	}
	var root *Timer
	for stack != nil {
		m := stack
		stack = m.next
		m.next = nil
		root = meld(root, m)
	}
	return root
}

// insert adds t; h.lock is held.
func (h *timerHeap) insert(t *Timer) {
	t.child, t.next, t.prev = nil, nil, nil
	h.root = meld(h.root, t)
	store64((*uint64)(unsafe.Pointer(&h.next)), uint64(h.root.when))
}

// remove takes t out of h; h.lock is held.
func (h *timerHeap) remove(t *Timer) {
	if t == h.root {
		h.root = mergePairs(t.child)
	} else {
		if t.prev.child == t {
			t.prev.child = t.next
		} else {
			t.prev.next = t.next
		}
		if t.next != nil {
			t.next.prev = t.prev
		}
		h.root = meld(h.root, mergePairs(t.child))
	}
	t.child, t.next, t.prev = nil, nil, nil
	next := NoDeadline
	if h.root != nil {
		next = h.root.when
	}
	store64((*uint64)(unsafe.Pointer(&h.next)), uint64(next))
}

// nextDeadline returns the deadline of h's earliest timer, or NoDeadline,
// without the lock.
func (h *timerHeap) nextDeadline() int64 {
	return int64(load64((*uint64)(unsafe.Pointer(&h.next))))
}

// run fires h's timers whose deadline is at or before now, earliest first;
// h.lock is held. A periodic timer goes back in at its next deadline after
// now, skipping the periods it missed.
func (h *timerHeap) run(now int64) {
	for {
		t := h.root
		if t == nil || t.when > now {
			return
		}
		h.remove(t)
		if t.period > 0 {
			missed := (now - t.when) / t.period
			if when := t.when + (missed+1)*t.period; when > t.when {
				t.when = when
			} else {
				t.when = NoDeadline - 1
			}
			h.insert(t)
			t.f(t.arg, now)
			continue
		}
		t.f(t.arg, now)
		storep(unsafe.Pointer(&t.heap), nil)
		xadd32(&activeTimers, -1)
	}
}

// armGlobal gives the backend the deadline of the shared heap's earliest
// timer if it is earlier than the one it has; globalTimers.lock is held.
func armGlobal(when int64) {
	h := &globalTimers
	if when < h.armed {
		h.armed = when
		schedArmTimer(when)
	}
}

// RunTimers runs the timers whose deadline has passed and gives the
// backend the next deadline through ArmTimer. A backend whose program uses
// timers calls it once a deadline it was given has passed (from a timer
// interrupt handler, in a kernel), or on every tick. It never blocks, and
// may be called from an interrupt handler; it must not be called from
// ArmTimer. The hosted backend runs its own timers and needs no call.
func RunTimers() {
	h := &globalTimers
	h.lock.Lock()
	h.run(Now())
	h.armed = NoDeadline
	if h.root != nil {
		armGlobal(h.root.when)
	}
	h.lock.Unlock()
}

// ----------------------------------------------------------------------------
// The clock and timed parking

// The timer hooks (internal/lower/sched.go): a program that reaches one
// with a scheduler that lacks the method fails to build.

func schedNow() int64

func schedArmTimer(deadline int64)

// Now returns the reading of the scheduler's monotonic clock in
// nanoseconds: the same on every processor, never decreasing, and counted
// from an unspecified start. It is the clock of timer deadlines and
// ParkUntil. Hosted builds read CLOCK_MONOTONIC (through the vDSO when the
// kernel provides one); a kernel's scheduler provides it as its Now method.
func Now() int64 { return schedNow() }

// Sleep pauses the running code for at least ns nanoseconds; ns must be
// positive. The compiler gives it one of two bodies, once it knows what the
// whole program does (internal/lower/sched.go). A program that never
// starts the scheduler (no task statement, no blocking channel operation or
// pluto/sync wait, no timer, no ParkUntil) and has the default hosted
// backend sleeps in sleepDirect: its only thread waits in the kernel, and
// no monitor or poller thread exists. Any other program sleeps in
// sleepParked, so that the processor runs other tasks meanwhile.
func Sleep(ns int64)

// sleepParked parks the running task until ns nanoseconds have passed on
// the Now clock.
func sleepParked(ns int64) {
	now := Now()
	end := now + ns
	if end <= now {
		end = NoDeadline - 1
	}
	for Now() < end {
		ParkUntil(end)
	}
}

// A parkTimer is the timer of one ParkUntil, in the parking task's frame.
type parkTimer struct {
	tm    Timer
	t     *Task
	state uint32 // 0 waiting, 1 timed out, 2 readied first
}

// parkTimeout fires the timer of a ParkUntil: it readies the task unless
// a Ready has already settled the park.
func parkTimeout(arg unsafe.Pointer, now int64) {
	pt := (*parkTimer)(arg)
	if cas32(&pt.state, 0, 1) {
		Ready(pt.t)
	}
}

// ParkUntil is Park with a deadline on the Now clock: it suspends the
// running task until Ready is called for it, and reports true, or until
// the deadline has passed, and reports false. A permit that Ready granted
// before the call is consumed at once (true). A Ready that loses the race
// to the deadline still grants its permit: a caller that registered the
// task with a waker removes it under the waker's lock, and if the waker had
// already taken it, consumes that wakeup with Park. A deadline that has
// passed returns false at once.
func ParkUntil(deadline int64) bool {
	startTasks()
	t := current()
	if t == nil {
		panicMessage("rt.ParkUntil called outside a task", "", "", "", "")
	}
	if t.locks != 0 {
		panicMessage("blocking with preemption disabled", "", "", "", "")
	}
	checkBlocking()
	if cas32(&t.wake, wakePending, wakeNone) {
		return true
	}
	if deadline <= Now() {
		return false
	}
	var pt parkTimer
	pt.t = t
	// The timer heap holds pt.tm only until Stop below (Pinned).
	pp := Pinned(&pt)
	pp.tm.Init(parkTimeout, unsafe.Pointer(pp))
	pp.tm.Reset(deadline, 0)
	ParkReason(WaitSleep)
	if cas32(&pt.state, 0, 2) {
		// Readied before the deadline: the timer's function, if it is
		// running, finds the park settled. Stop waits for it, since pt
		// dies with this frame.
		pt.tm.Stop()
		return true
	}
	// The timer won, and its Ready woke this park (or will be the permit
	// for a later one, if another Ready woke it first). Stop waits until
	// the timer's function has returned.
	pt.tm.Stop()
	return false
}

func loadp(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(load64((*uint64)(p))))
}

func storep(p, v unsafe.Pointer) {
	store64((*uint64)(p), uint64(uintptr(v)))
}
