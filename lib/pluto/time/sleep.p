package time

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// Sleeping and timers, over the scheduler's timers (rt.Timer, rt.ParkUntil;
// language spec section 12.12). They work wherever the bound scheduler
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

// Sleep pauses the calling puroutine for at least d. A negative or zero d
// returns at once. The puroutine parks with a timer, so its processor runs
// other puroutines meanwhile, and a program whose puroutines all sleep
// uses no processor time. A program that starts no puroutine, uses no
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
// NewTimer and AfterFunc allocate the Timer, and NewTimer its channel, from
// the allocator they are given (the default allocator if none). C owns the
// timer: it keeps running while anything holds a reference to C, and stops
// when the last reference goes, as a Go timer that nothing refers to any
// more does. Free the Timer with alloc.Free, passing the same allocator,
// which releases its reference to C, and stops an AfterFunc timer (Drop).
//
// As in Go 1.23, C has no stale values: once Stop or Reset has returned, a
// receive from C gets only a time sent by a later expiry.
type Timer owner struct {
	C <-chan Time

	// AfterFunc: the timer, the function, the expiries whose puroutine
	// has not started yet, and the link in the queue of the puroutine
	// that starts them (funcs). fires and link are guarded by funcs.lock.
	r     rt.Timer
	f     func()
	fires int
	link  *Timer
}

// NewTimer creates a new Timer that will send the current time on its
// channel after at least duration d.
func NewTimer(d Duration, a alloc.Allocator = alloc.Default()) *Timer {
	t := alloc.New[Timer](a)
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
// AfterFunc.
func (t *Timer) timer() *rt.Timer {
	if t.C != nil {
		return timerOf(t.C)
	}
	return &t.r
}

// Stop prevents the Timer from firing. It returns true if the call stops
// the timer: the timer had not expired, or had expired and its time had
// not been received (Stop takes it from C). For a timer made by AfterFunc,
// a false result means the function has been or will be started on its own
// puroutine. Stop does not wait for that function.
func (t *Timer) Stop() bool {
	if t.C == nil && t.f == nil {
		panic("time: Stop called on uninitialized Timer")
	}
	active := t.timer().Stop()
	if t.C != nil && drain(t.C) {
		active = true
	}
	return active
}

// Reset changes the timer to expire after duration d, and reports whether
// the timer had been active, as Stop does; a time not yet received from C
// is discarded. A timer made by AfterFunc runs its function again.
func (t *Timer) Reset(d Duration) bool {
	if t.C == nil && t.f == nil {
		panic("time: Reset called on uninitialized Timer")
	}
	tm := t.timer()
	active := tm.Stop()
	if t.C != nil && drain(t.C) {
		active = true
	}
	tm.Reset(deadline(d), 0)
	return active
}

// Drop stops a timer made by AfterFunc, first waiting until the
// puroutines of expiries already past have started. alloc.Free calls it
// before freeing the Timer; then C's reference is released, and a channel
// timer stops with the last reference to its channel.
func (t *Timer) Drop() {
	if t.f == nil {
		return
	}
	t.r.Stop()
	for {
		funcs.lock.Lock()
		n := t.fires
		funcs.lock.Unlock()
		if n == 0 {
			break
		}
		rt.Yield()
	}
}

// AfterFunc waits for the duration to elapse and then calls f in its own
// puroutine. It returns a Timer that can be used to cancel the call using
// its Stop method; the Timer's C is nil.
//
// As with a go statement, f must not capture variables or be a method
// value made in the caller's frame, since it runs after the frame may be
// gone: AfterFunc panics if f's closure record lies in the calling
// puroutine's stack (language spec section 19.1). The puroutines are started
// by one puroutine of the time package's, which AfterFunc starts the first
// time it is called and which then waits for expiries for good.
func AfterFunc(d Duration, f func(), a alloc.Allocator = alloc.Default()) *Timer {
	rt.CheckGoFunc(f)
	startFuncRunner()
	t := alloc.New[Timer](a)
	t.f = f
	t.r.Init(goFunc, unsafe.Pointer(t))
	t.r.Reset(deadline(d), 0)
	return t
}

// funcs is the queue of AfterFunc timers that have expired, and the
// puroutine that starts their functions. A timer's function runs where
// timers run (possibly an interrupt handler) and cannot start a puroutine
// itself.
var funcs struct {
	lock       rt.Lock
	head, tail *Timer
	runner     *rt.Task
	started    bool
}

func startFuncRunner() {
	funcs.lock.Lock()
	start := !funcs.started
	funcs.started = true
	funcs.lock.Unlock()
	if start {
		task runFuncs()
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
	t.fires++
	if t.fires == 1 {
		enqueueFunc(t)
	}
	r := funcs.runner
	funcs.lock.Unlock()
	if r != nil {
		rt.Ready(r)
	}
}

// runFuncs starts the functions of expired AfterFunc timers, each on a
// puroutine of its own.
func runFuncs() {
	funcs.lock.Lock()
	funcs.runner = rt.CurrentTask()
	funcs.lock.Unlock()
	for {
		funcs.lock.Lock()
		t := funcs.head
		if t == nil {
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
		for i := 0; i < n; i++ {
			task f()
		}
		funcs.lock.Lock()
		t.fires -= n
		if t.fires > 0 {
			enqueueFunc(t) // it expired again meanwhile
		}
		funcs.lock.Unlock()
	}
}

// A Ticker holds a channel that delivers "ticks" of a clock at intervals.
// NewTicker allocates it and its channel. As for Timer, C owns the ticker's
// timer, which stops when the last reference to C goes; free the Ticker
// with alloc.Free, which releases its reference. As with Timer, C has no
// stale values after Stop or Reset.
type Ticker owner struct {
	C <-chan Time
}

// NewTicker returns a new Ticker containing a channel that will send the
// current time on the channel after each tick. The period of the ticks is
// specified by the duration argument. The ticker will adjust the time
// interval or drop ticks to make up for slow receivers. The duration d must
// be greater than zero; if not, NewTicker will panic.
func NewTicker(d Duration, a alloc.Allocator = alloc.Default()) *Ticker {
	if d <= 0 {
		panic("non-positive interval for NewTicker")
	}
	t := alloc.New[Ticker](a)
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
