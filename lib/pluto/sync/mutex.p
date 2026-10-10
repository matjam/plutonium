package sync

import (
	"pluto/rt"
	"pluto/sync/atomic"
)

const (
	mutexLocked  = 1
	mutexWaiters = 2 // the queue may be non-empty: Unlock must look
)

// A Mutex is a mutual exclusion lock. The zero value is unlocked.
//
// An uncontended Lock and Unlock are one atomic instruction each. A
// puroutine that finds the mutex locked spins briefly, then parks; Unlock
// wakes the first waiter, which competes for the mutex again and, if it
// loses, waits at the front of the queue. The n-th Unlock is synchronised
// before the m-th Lock returns, for n < m (memory-model.md section 3.4).
type Mutex struct {
	state uint32
	mu    rt.Lock // guards q
	q     waitQueue
}

// Lock locks m, waiting until it is available.
//
//pu:blocking
func (m *Mutex) Lock() {
	if atomic.CompareAndSwapUint32(&m.state, 0, mutexLocked) {
		return
	}
	m.lockSlow()
}

//pu:blocking
func (m *Mutex) lockSlow() {
	front := false
	for {
		// Spin briefly: the holder is usually running on another
		// processor and about to unlock, which is far cheaper than
		// parking and being woken.
		for i := 0; i < 8; i++ {
			s := atomic.LoadUint32(&m.state)
			if s&mutexLocked == 0 && atomic.CompareAndSwapUint32(&m.state, s, s|mutexLocked) {
				return
			}
			rt.SpinWait(30)
		}
		m.mu.Lock()
		s := atomic.LoadUint32(&m.state)
		if s&mutexLocked == 0 {
			ok := atomic.CompareAndSwapUint32(&m.state, s, s|mutexLocked)
			m.mu.Unlock()
			if ok {
				return
			}
			continue
		}
		if !atomic.CompareAndSwapUint32(&m.state, s, s|mutexWaiters) {
			m.mu.Unlock()
			continue
		}
		var w waiter
		w.t = self()
		if front {
			m.q.pushFront(&w)
		} else {
			m.q.push(&w)
		}
		m.mu.Unlock()
		rt.ParkReason(rt.WaitMutex)
		front = true
	}
}

// TryLock locks m if it is free and reports whether it did. It never
// waits.
func (m *Mutex) TryLock() bool {
	s := atomic.LoadUint32(&m.state)
	return s&mutexLocked == 0 && atomic.CompareAndSwapUint32(&m.state, s, s|mutexLocked)
}

// Unlock unlocks m. Unlocking a mutex that is not locked stops the program.
// It never waits.
func (m *Mutex) Unlock() {
	if atomic.CompareAndSwapUint32(&m.state, mutexLocked, 0) {
		return
	}
	m.unlockSlow()
}

func (m *Mutex) unlockSlow() {
	m.mu.Lock()
	w := m.q.pop()
	for {
		s := atomic.LoadUint32(&m.state)
		if s&mutexLocked == 0 {
			m.mu.Unlock()
			fail("sync: unlock of unlocked mutex")
		}
		ns := s &^ mutexLocked
		if m.q.empty() {
			ns &^= mutexWaiters
		}
		if atomic.CompareAndSwapUint32(&m.state, s, ns) {
			break
		}
	}
	m.mu.Unlock()
	if w != nil {
		rt.Ready(w.t)
	}
}

// A sema is a counting semaphore that parks waiters: release hands its
// count straight to the first waiter.
type sema struct {
	mu rt.Lock
	n  uint32
	q  waitQueue
}

//pu:blocking
func (s *sema) acquire(r rt.WaitReason) {
	s.mu.Lock()
	if s.n > 0 {
		s.n--
		s.mu.Unlock()
		return
	}
	var w waiter
	w.t = self()
	s.q.push(&w)
	s.mu.Unlock()
	rt.ParkReason(r)
}

func (s *sema) release() {
	s.mu.Lock()
	if w := s.q.pop(); w != nil {
		s.mu.Unlock()
		rt.Ready(w.t)
		return
	}
	s.n++
	s.mu.Unlock()
}
