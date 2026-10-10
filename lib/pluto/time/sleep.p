package time

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// Sleeping and timers, over the scheduler's timers (rt.Timer, rt.ParkUntil;
// language spec section 4.7). They work wherever the bound scheduler
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
func drain(c chan Time) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// sendTime is the function of channel timers and tickers: arg points at the
// channel, which gets the time unless it already holds a value.
func sendTime(arg unsafe.Pointer, now int64) {
	c := *(*chan Time)(arg)
	select {
	case c <- fireTime(now):
	default:
	}
}

// The Timer type represents a single event. When the Timer expires, the
// current time is sent on C, unless the Timer was made by AfterFunc.
//
// NewTimer and AfterFunc allocate the Timer, and NewTimer its channel, from
// the allocator they are given (the default allocator if none). Free a
// Timer with alloc.Free, passing the same allocator, which stops it and
// frees its channel (Drop): Plutonium has no garbage collector to reclaim
// a Timer nobody refers to.
//
// As in Go 1.23, C has no stale values: once Stop or Reset has returned, a
// receive from C gets only a time sent by a later expiry.
type Timer struct {
	C <-chan Time
	c chan Time
	r rt.Timer

	// AfterFunc: the function, the expiries whose puroutine has not
	// started yet, and the link in the queue of the puroutine that starts
	// them (funcs). fires and link are guarded by funcs.lock.
	f     func()
	fires int
	link  *Timer
}

// NewTimer creates a new Timer that will send the current time on its
// channel after at least duration d.
func NewTimer(d Duration, a alloc.Allocator = alloc.Default()) *Timer {
	t := new(Timer, a)
	t.c = make(chan Time, 1, a)
	t.C = t.c
	t.r.Init(sendTime, unsafe.Pointer(&t.c))
	t.r.Reset(deadline(d), 0)
	return t
}

// After waits for the duration to elapse and then sends the current time on
// the returned channel. It is NewTimer(d).C, and the Timer it allocates is
// never freed: in a loop, or wherever the memory matters, use NewTimer and
// free the Timer when done.
func After(d Duration) <-chan Time {
	return NewTimer(d).C
}

// Stop prevents the Timer from firing. It returns true if the call stops
// the timer: the timer had not expired, or had expired and its time had
// not been received (Stop takes it from C). For a timer made by AfterFunc,
// a false result means the function has been or will be started on its own
// puroutine. Stop does not wait for that function.
func (t *Timer) Stop() bool {
	if t.c == nil && t.f == nil {
		panic("time: Stop called on uninitialized Timer")
	}
	active := t.r.Stop()
	if t.c != nil && drain(t.c) {
		active = true
	}
	return active
}

// Reset changes the timer to expire after duration d, and reports whether
// the timer had been active, as Stop does; a time not yet received from C
// is discarded. A timer made by AfterFunc runs its function again.
func (t *Timer) Reset(d Duration) bool {
	if t.c == nil && t.f == nil {
		panic("time: Reset called on uninitialized Timer")
	}
	active := t.r.Stop()
	if t.c != nil && drain(t.c) {
		active = true
	}
	t.r.Reset(deadline(d), 0)
	return active
}

// Drop stops the timer and frees its channel. alloc.Free calls it before
// freeing the Timer. For a timer made by AfterFunc, it first waits until
// the puroutines of expiries already past have started.
func (t *Timer) Drop() {
	t.r.Stop()
	if t.f != nil {
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
	if t.c != nil {
		alloc.FreeChan(t.c)
		t.c = nil
		t.C = nil
	}
}

// AfterFunc waits for the duration to elapse and then calls f in its own
// puroutine. It returns a Timer that can be used to cancel the call using
// its Stop method; the Timer's C is nil.
//
// As with a go statement, f must not capture variables or be a method
// value made in the caller's frame, since it runs after the frame may be
// gone: AfterFunc panics if f's closure record lies in the calling
// puroutine's stack (language spec section 4.7). The puroutines are started
// by one puroutine of the time package's, which AfterFunc starts the first
// time it is called and which then waits for expiries for good.
//
//pu:gofunc
func AfterFunc(d Duration, f func(), a alloc.Allocator = alloc.Default()) *Timer {
	rt.CheckGoFunc(f)
	startFuncRunner()
	t := new(Timer, a)
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
		go runFuncs()
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
			go f()
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
// NewTicker allocates it and its channel; free it with alloc.Free, which
// stops it (Drop). As with Timer, C has no stale values after Stop or
// Reset.
type Ticker struct {
	C <-chan Time
	c chan Time
	r rt.Timer
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
	t := new(Ticker, a)
	t.c = make(chan Time, 1, a)
	t.C = t.c
	t.r.Init(sendTime, unsafe.Pointer(&t.c))
	t.r.Reset(deadline(d), int64(d))
	return t
}

// Tick is NewTicker(d).C, or nil if d <= 0. The Ticker it allocates is
// never freed.
func Tick(d Duration) <-chan Time {
	if d <= 0 {
		return nil
	}
	return NewTicker(d).C
}

// Stop turns off a ticker. After Stop, no more ticks will be sent, and a
// tick not yet received is discarded.
func (t *Ticker) Stop() {
	if t.c == nil {
		return
	}
	t.r.Stop()
	drain(t.c)
}

// Reset stops a ticker and resets its period to the specified duration.
// The next tick will arrive after the new period elapses. The duration d
// must be greater than zero; if not, Reset will panic.
func (t *Ticker) Reset(d Duration) {
	if d <= 0 {
		panic("non-positive interval for Ticker.Reset")
	}
	if t.c == nil {
		panic("time: Reset called on uninitialized Ticker")
	}
	t.r.Stop()
	drain(t.c)
	t.r.Reset(deadline(d), int64(d))
}

// Drop stops the ticker and frees its channel. alloc.Free calls it before
// freeing the Ticker.
func (t *Ticker) Drop() {
	t.r.Stop()
	if t.c != nil {
		alloc.FreeChan(t.c)
		t.c = nil
		t.C = nil
	}
}
