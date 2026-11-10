package sync

import (
	"pluto/rt"
	"pluto/sync/atomic"
)

// A WaitGroup waits for a collection of tasks to finish: the main
// task calls Add with the number to wait for, each calls Done when it
// finishes, and Wait parks until the count is zero. A Done that brings the
// count to zero is synchronised before every Wait it unblocks
// (memory-model.md section 3.4).
type WaitGroup struct {
	mu rt.Lock
	n  int64
	q  waitQueue
}

// Add adds delta, which may be negative, to the counter. When it reaches
// zero every Wait returns; a negative counter stops the program. It never
// waits.
func (wg *WaitGroup) Add(delta int) {
	wg.mu.Lock()
	wg.n += int64(delta)
	if wg.n < 0 {
		wg.mu.Unlock()
		fail("sync: negative WaitGroup counter")
	}
	var list *waiter
	if wg.n == 0 {
		list = wg.q.head
		wg.q = waitQueue{}
	}
	wg.mu.Unlock()
	readyAll(list)
}

// Done decrements the counter by one.
func (wg *WaitGroup) Done() { wg.Add(-1) }

// Task starts f in a new task counted by wg, as Go 1.25's
// WaitGroup.Task does: Add(1), then Done when f returns. f must not capture
// variables (language spec section 12.12).
func (wg *WaitGroup) Task(f func()) {
	wg.Add(1)
	// The task uses wg until its Done, which a Wait on wg waits for:
	// a WaitGroup in a frame that waits before returning outlives it.
	// The compiler cannot check that the frame does wait, so wg is hidden
	// from the escape analysis (rt.Pinned); sync.Scope is the checked form
	// (docs/plans/2026-10-10-scoped-puroutines.md).
	task runDone(rt.Pinned(wg), f)
}

func runDone(wg *WaitGroup, f func()) {
	f()
	wg.Done()
}

// Wait parks until the counter is zero.
func (wg *WaitGroup) Wait() {
	wg.mu.Lock()
	if wg.n == 0 {
		wg.mu.Unlock()
		return
	}
	var w waiter
	w.t = self()
	wg.q.push(rt.Pinned(&w)) // popped before this task is readied
	wg.mu.Unlock()
	rt.ParkReason(rt.WaitWaitGroup)
}

// Once performs one action exactly once.
type Once struct {
	done uint32
	m    Mutex
}

// Do calls f if and only if Do is called for the first time on this Once.
// Every call returns after f has returned: the return of f is
// synchronised before the return of every Do.
func (o *Once) Do(f func()) {
	if atomic.LoadUint32(&o.done) == 0 {
		o.doSlow(f)
	}
}

func (o *Once) doSlow(f func()) {
	o.m.Lock()
	if o.done == 0 {
		f()
		atomic.StoreUint32(&o.done, 1)
	}
	o.m.Unlock()
}

// A Cond is a condition variable: a point at which tasks wait for, or
// announce, an event. Its zero value is ready to use. The lock passed to
// Wait is held while the condition is checked and changed, and Wait
// releases it while it waits. A Cond must not be copied.
type Cond struct {
	mu rt.Lock
	q  waitQueue
}

// Wait unlocks l, parks until Signal or Broadcast wakes it, and locks l
// again before it returns. As in Go, the caller must check its condition
// in a loop. The task must hold l for writing. Wait makes every check that
// may panic before it unlocks l, so it returns, or ends by a panic, only
// with l locked.
func (c *Cond) Wait(l Locker) {
	if l == nil {
		fail("sync: Cond.Wait with a nil Locker")
	}
	switch m := l.(type) {
	case *Mutex:
		if atomic.LoadUint32(&m.state)&mutexLocked == 0 {
			fail("sync: unlock of unlocked mutex")
		}
	case *RWMutex:
		if atomic.LoadInt32(&m.readerCount) >= 0 {
			fail("sync: Unlock of unlocked RWMutex")
		}
	case *rlocker:
		fail("sync: Cond.Wait needs a lock held for writing, not an RLocker")
	}
	var w waiter
	w.t = self()
	c.mu.Lock()
	c.q.push(rt.Pinned(&w)) // popped before this task is readied
	c.mu.Unlock()
	l.Unlock()
	rt.ParkReason(rt.WaitCond)
	l.Lock()
}

// Signal wakes one task waiting on c, if there is one. It never
// waits.
func (c *Cond) Signal() {
	c.mu.Lock()
	w := c.q.pop()
	c.mu.Unlock()
	if w != nil {
		rt.Ready(w.t)
	}
}

// Broadcast wakes every task waiting on c. It never waits.
func (c *Cond) Broadcast() {
	c.mu.Lock()
	list := c.q.head
	c.q = waitQueue{}
	c.mu.Unlock()
	readyAll(list)
}
