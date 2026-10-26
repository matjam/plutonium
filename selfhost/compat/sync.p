package compat

import "pluto/rt"

// The locks of the Go sources. go2pu maps sync.Mutex, sync.RWMutex,
// sync.Once and sync/atomic.Pointer to these types. They are built on
// rt.Lock rather than pluto/sync: sync.Mutex's waiter queue holds
// pointers, which makes the escape analysis lose precision on every
// struct that embeds a lock. An rt.Lock never parks a task, so these
// guard short critical sections only.

// NumCPU is runtime.NumCPU: the processors that run the program's tasks.
func NumCPU() int { return rt.NumCPU() }

// A Mutex is sync.Mutex.
type Mutex struct {
	l rt.Lock
}

func (m *Mutex) Lock()   { m.l.Lock() }
func (m *Mutex) Unlock() { m.l.Unlock() }

// A Once is sync.Once. f runs with the lock held, so it must not call Do
// on the same Once.
type Once struct {
	l    rt.Lock
	done bool
}

func (o *Once) Do(f func()) {
	o.l.Lock()
	if !o.done {
		o.done = true
		f()
	}
	o.l.Unlock()
}

// An RWMutex is sync.RWMutex, with readers excluding each other too.
type RWMutex struct {
	l rt.Lock
}

func (m *RWMutex) Lock()    { m.l.Lock() }
func (m *RWMutex) Unlock()  { m.l.Unlock() }
func (m *RWMutex) RLock()   { m.l.Lock() }
func (m *RWMutex) RUnlock() { m.l.Unlock() }

// An AtomicPointer is atomic.Pointer[T]: a pointer behind a lock.
// atomic.Pointer goes through unsafe.Pointer, which defeats the escape
// analysis.
type AtomicPointer[T any] struct {
	l rt.Lock
	p *T
}

func (a *AtomicPointer[T]) Load() *T {
	a.l.Lock()
	p := a.p
	a.l.Unlock()
	return p
}

func (a *AtomicPointer[T]) Store(p *T) {
	a.l.Lock()
	a.p = p
	a.l.Unlock()
}

func (a *AtomicPointer[T]) CompareAndSwap(old, new *T) bool {
	a.l.Lock()
	ok := a.p == old
	if ok {
		a.p = new
	}
	a.l.Unlock()
	return ok
}
