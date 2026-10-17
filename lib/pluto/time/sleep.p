package time

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync"
	"unsafe"
)

// Sleeping and timers, over the scheduler's timers (rt.Timer, rt.ParkUntil;
// language spec section 19.6). They work wherever the bound scheduler
// provides a clock: hosted builds, and kernels whose scheduler implements
// Now and ArmTimer (abi.md section 12.9).

// deadline returns the reading of the scheduler's clock d from now.
func deadline(d Duration) int64 {
	now := rt.Now()
	if d <= 0 {
		return now
	}
	if w := now + int64(d); w > now {
		return w
	}
	return rt.NoDeadline - 1
}

// Sleep pauses the calling task for at least d. A negative or zero d
// returns at once. The task parks with a timer, so its processor runs
// other tasks meanwhile, and a program whose tasks all sleep
// uses no processor time. A program that starts no task, uses no
// channel, timer or wait, and has the default hosted scheduler has none to
// run: there Sleep waits in the kernel on the program's only thread, and
// the scheduler's threads are not started (rt.Sleep).
func Sleep(d Duration) {
	if d <= 0 {
		return
	}
	rt.Sleep(int64(d))
}

// drain takes a value left in c, a timer's channel, and reports whether
// there was one.
func drain(c <-chan Time) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// sendTime is the function of channel timers and tickers: arg is the
// channel, which owns the timer and gets the time unless it already holds
// a value. The channel's count is left alone: the channel stops its timer,
// waiting for this function, before it is freed.
func sendTime(arg unsafe.Pointer, now int64) {
	t := fireTime(now)
	rt.ChanTrySend(arg, unsafe.Pointer(&t))
}

// chanWord returns the address of the channel c, uncounted.
func chanWord(c <-chan Time) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&c))
}

// timerOf returns the timer that the channel c owns.
func timerOf(c <-chan Time) *rt.Timer {
	return rt.ChanTimer(chanWord(c), sendTime)
}

// newTimerChan makes a channel from a that owns a timer, which sends it the
// time after d, and every period after that if period is positive.
func newTimerChan(d, period Duration, a alloc.Allocator) <-chan Time {
	c := make(chan Time, 1, a)
	timerOf(c).Reset(deadline(d), int64(period))
	return c
}

// The Timer type represents a single event. When the Timer expires, the
// current time is sent on C, unless the Timer was made by AfterFunc.
//
// NewTimer returns an owned[Timer], allocated, with its channel, from the
// allocator it is given (the default allocator if none). Dropping it stops
// the timer and frees it (language spec section 19.6): at the end of its
// variable's scope, unless it was moved. C is a counted reference, so a
// receiver that copied C keeps the channel, not the timer. A timer that
// several tasks use is put in a synced object that holds the owned[Timer].
//
// AfterFunc returns a synced[Timer]. While its timer is armed the runtime
// holds a reference of its own, released when the function is started or
// when Stop succeeds, so dropping every handle does not stop the timer.
//
// As in Go 1.23, C has no stale values: once Stop or Reset has returned, a
// receive from C gets only a time sent by a later expiry.
type Timer owner struct {
	C  <-chan Time
	mu sync.Mutex // serializes ordinary Stop and Reset, including runner startup

	// AfterFunc: the timer, the function, the expiries whose task
	// has not started yet, and the link in the queue of the task
	// that starts them (funcs). fires, link, armed and held are guarded
	// by funcs.lock.
	r     rt.Timer
	f     func()
	fires int
	link  *Timer
	armed bool
	// held says the runtime holds a reference to the Timer (RetainSynced):
	// it is armed, or has expired and its function is not started yet.
	held bool
}

// NewTimer creates a new Timer that will send the current time on its
// channel after at least duration d. Dropping the Timer stops it.
func NewTimer(d Duration, a alloc.Allocator = alloc.Default()) owned[Timer] {
	t := alloc.Owned[Timer](a)
	t.C = newTimerChan(d, 0, a)
	return t
}

// After waits for the duration to elapse and then sends the current time on
// the returned channel. The channel owns its timer, which stops when the
// last reference to the channel goes: a select that waits on After in a
// loop frees each one when the select ends.
func After(d Duration) <-chan Time {
	return newTimerChan(d, 0, alloc.Default())
}

// timer returns the rt.Timer of t: its channel's, or its own for
// AfterFunc. Arming the timer links it into the runtime's timer heap,
// which the escape analysis cannot see the end of: Drop stops it before
// the Timer's memory is freed, so the link never outlives the Timer.
func (t *Timer) timer() *rt.Timer {
	if t.C != nil {
		return timerOf(t.C)
	}
	return rt.Pinned(&t.r)
}

// Stop prevents the Timer from firing. It returns true if the call stops
// the timer: the timer had not expired, or had expired and its time had
// not been received (Stop takes it from C). For a timer made by AfterFunc,
// a false result means the function has been or will be started on its own
// task. Stop does not wait for that function.
func (t *Timer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.C == nil && t.f == nil {
		panic("time: Stop called on uninitialized Timer")
	}
	active := t.timer().Stop()
	if t.C == nil && active {
		disarmFunc(t)
	}
	if t.C != nil && drain(t.C) {
		active = true
	}
	return active
}

// Reset changes the timer to expire after duration d, and reports whether
// the timer had been active, as Stop does; a time not yet received from C
// is discarded. A timer made by AfterFunc runs its function again.
func (t *Timer) Reset(d Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.C == nil && t.f == nil {
		panic("time: Reset called on uninitialized Timer")
	}
	tm := t.timer()
	active := tm.Stop()
	if t.C == nil {
		if active {
			disarmFunc(t)
		}
		startFuncRunner(t)
	}
	if t.C != nil && drain(t.C) {
		active = true
	}
	tm.Reset(deadline(d), 0)
	return active
}

// Drop stops the timer when its owner drops it; then the Timer's reference
// to C is released: a receiver that copied C keeps the channel, which gets
// no more values. A timer made by AfterFunc is dropped with its last
// reference, which the runtime holds while the timer is armed or its
// function not started, so it has nothing left to stop.
func (t *Timer) Drop() {
	if t.C != nil {
		timerOf(t.C).Stop()
	}
}

// AfterFunc waits for the duration to elapse and then calls f in its own
// task. It returns a Timer that can be used to cancel the call using its
// Stop method; the Timer's C is nil. The runtime holds a reference to the
// Timer while it is armed, so dropping every handle does not stop it: the
// statement AfterFunc(d, f) runs f, and the Timer is freed after f has
// been started and the last handle is dropped.
//
// As with a task statement, f must not capture variables or be a method
// value made in the caller's frame, since it runs after the frame may be
// gone: a func value that may be a closure of the calling frame is a
// compile error (language spec sections 12.12 and 18.3). The tasks are started
// by one task of the time package's. It waits while callbacks are armed
// or queued, then returns. A later AfterFunc or Reset starts it again.
func AfterFunc(d Duration, f func(), a alloc.Allocator = alloc.Default()) synced[Timer] {
	t := alloc.Synced[Timer](a)
	t.f = f
	// The runtime's timer heap and the queue of expired timers refer to
	// the Timer until its function has been started or Stop has disarmed
	// it: the reference the runtime holds (holdFunc) keeps it alive.
	r, arg := rt.Pinned(&t.r), unsafe.Pointer(&*t)
	r.Init(goFunc, *rt.Pinned(&arg))
	t.Reset(d)
	return t
}

// holdFunc takes the reference the runtime holds to t while t is armed or
// expired and its function not started; funcs.lock is held.
func holdFunc(t *Timer) {
	if !t.held {
		t.held = true
		rt.RetainSynced(unsafe.Pointer(t))
	}
}

// releaseFunc says whether the runtime's reference to t is no longer needed
// and takes it away (the caller then calls dropHeld once funcs.lock is
// released, as a release may free t and drops its contents).
func releaseFunc(t *Timer) bool {
	if t.held && !t.armed && t.fires == 0 {
		t.held = false
		return true
	}
	return false
}

// dropHeld releases the reference the runtime held to t. Nothing may use t
// after it unless the caller has a reference of its own.
func dropHeld(t *Timer) {
	h := synced[Timer](unsafe.Pointer(t))
	_ = h
}

// funcs is the queue of AfterFunc timers that have expired, and the
// task that starts their functions. A timer's function runs where
// timers run (possibly an interrupt handler) and cannot start a task
// itself.
var funcs struct {
	lock       rt.Lock
	head, tail *Timer
	runner     *rt.Task
	started    bool
	starting   bool
	pending    int
}

func finishFuncStart(t *Timer, done *bool) {
	if !*done {
		funcs.lock.Lock()
		t.armed = false
		funcs.pending--
		funcs.starting = false
		// Spawn may have published a runner before ending its caller.
		// It can already be parked for this now-canceled reservation.
		if r := funcs.runner; r != nil {
			rt.Ready(r)
		}
		release := releaseFunc(t)
		funcs.lock.Unlock()
		if release {
			dropHeld(t)
		}
	}
}

func startFuncRunner(t *Timer) {
	for {
		funcs.lock.Lock()
		if funcs.starting {
			funcs.lock.Unlock()
			rt.Yield()
			continue
		}
		t.armed = true
		holdFunc(t)
		funcs.pending++
		if funcs.started {
			funcs.lock.Unlock()
			return
		}
		funcs.starting = true
		funcs.lock.Unlock()
		launchFuncRunner(t)
		return
	}
}

func launchFuncRunner(t *Timer) {
	done := false
	defer finishFuncStart(t, &done)
	task runFuncs()
	funcs.lock.Lock()
	funcs.started = true
	funcs.starting = false
	done = true
	funcs.lock.Unlock()
}

func disarmFunc(t *Timer) {
	funcs.lock.Lock()
	if t.armed {
		t.armed = false
		funcs.pending--
	}
	r := funcs.runner
	if r != nil {
		rt.Ready(r)
	}
	release := releaseFunc(t)
	funcs.lock.Unlock()
	if release {
		dropHeld(t)
	}
}

// enqueueFunc adds t to the queue; funcs.lock is held.
func enqueueFunc(t *Timer) {
	t.link = nil
	if funcs.tail != nil {
		funcs.tail.link = t
	} else {
		funcs.head = t
	}
	funcs.tail = t
}

// goFunc is the function of AfterFunc timers: it queues t for the runner.
func goFunc(arg unsafe.Pointer, now int64) {
	t := (*Timer)(arg)
	funcs.lock.Lock()
	defer funcs.lock.Unlock()
	if t.armed {
		t.armed = false
		funcs.pending--
	}
	t.fires++
	if t.fires == 1 {
		enqueueFunc(t)
	}
	r := funcs.runner
	if r != nil {
		rt.Ready(r)
	}
}

// runFuncs starts the functions of expired AfterFunc timers, each on a
// task of its own.
func runFuncs() {
	funcs.lock.Lock()
	// A backend may hand the task off and then end its caller. A retry
	// can then have started another runner; only one adopts the queue.
	if funcs.runner != nil {
		funcs.lock.Unlock()
		return
	}
	funcs.runner = rt.CurrentTask()
	funcs.lock.Unlock()
	for {
		funcs.lock.Lock()
		t := funcs.head
		if t == nil {
			if funcs.pending == 0 {
				funcs.runner = nil
				funcs.started = false
				funcs.lock.Unlock()
				return
			}
			funcs.lock.Unlock()
			rt.ParkReason(rt.WaitSystem)
			continue
		}
		funcs.head = t.link
		if funcs.head == nil {
			funcs.tail = nil
		}
		t.link = nil
		n := t.fires
		f := t.f
		funcs.lock.Unlock()
		for i := 0; i < n; {
			handedOff := false
			// Creation may end before copying the task, or Spawn may end
			// after accepting it. Catch that exit without losing this
			// queue runner, and retry only a still-promised callback.
			rt.DispatchTimerFunc(f, &handedOff)
			if handedOff {
				i++
			}
		}
		funcs.lock.Lock()
		t.fires -= n
		if t.fires > 0 {
			enqueueFunc(t) // it expired again meanwhile
		}
		release := releaseFunc(t)
		funcs.lock.Unlock()
		if release {
			dropHeld(t)
		}
	}
}

// A Ticker holds a channel that delivers "ticks" of a clock at intervals.
// NewTicker returns an owned[Ticker], allocated with its channel; dropping
// it stops the ticker and frees it. As with Timer, C has no stale values
// after Stop or Reset.
type Ticker owner struct {
	C <-chan Time
}

// Drop stops the ticker when its owner drops it; then the Ticker's
// reference to C is released.
func (t *Ticker) Drop() {
	if t.C != nil {
		timerOf(t.C).Stop()
	}
}

// NewTicker returns a new Ticker containing a channel that will send the
// current time on the channel after each tick. The period of the ticks is
// specified by the duration argument. The ticker will adjust the time
// interval or drop ticks to make up for slow receivers. The duration d must
// be greater than zero; if not, NewTicker will panic.
func NewTicker(d Duration, a alloc.Allocator = alloc.Default()) owned[Ticker] {
	if d <= 0 {
		panic("non-positive interval for NewTicker")
	}
	t := alloc.Owned[Ticker](a)
	t.C = newTimerChan(d, d, a)
	return t
}

// Tick is NewTicker(d).C, or nil if d <= 0: a channel that owns its timer,
// which stops when the last reference to the channel goes.
func Tick(d Duration) <-chan Time {
	if d <= 0 {
		return nil
	}
	return newTimerChan(d, d, alloc.Default())
}

// Stop turns off a ticker. After Stop, no more ticks will be sent, and a
// tick not yet received is discarded.
func (t *Ticker) Stop() {
	if t.C == nil {
		return
	}
	timerOf(t.C).Stop()
	drain(t.C)
}

// Reset stops a ticker and resets its period to the specified duration.
// The next tick will arrive after the new period elapses. The duration d
// must be greater than zero; if not, Reset will panic.
func (t *Ticker) Reset(d Duration) {
	if d <= 0 {
		panic("non-positive interval for Ticker.Reset")
	}
	if t.C == nil {
		panic("time: Reset called on uninitialized Ticker")
	}
	tm := timerOf(t.C)
	tm.Stop()
	drain(t.C)
	tm.Reset(deadline(d), int64(d))
}
