package compat

// The compiler is single-threaded (docs/plans/2026-10-09-self-hosting-plan.md
// sections 4.1 and 6.2), so the locks and atomics of the Go sources are
// no-ops. go2pu maps sync.Mutex, sync.RWMutex and sync/atomic.Pointer to
// these types; they take no part in the scheduler.

// A Mutex is sync.Mutex without waiting: the compiler runs one task.
type Mutex struct{}

func (m *Mutex) Lock()   {}
func (m *Mutex) Unlock() {}

// An RWMutex is sync.RWMutex without waiting.
type RWMutex struct{}

func (m *RWMutex) Lock()    {}
func (m *RWMutex) Unlock()  {}
func (m *RWMutex) RLock()   {}
func (m *RWMutex) RUnlock() {}

// An AtomicPointer is atomic.Pointer[T] for one task: a plain pointer.
type AtomicPointer[T any] struct {
	p *T
}

func (a *AtomicPointer[T]) Load() *T   { return a.p }
func (a *AtomicPointer[T]) Store(p *T) { a.p = p }

func (a *AtomicPointer[T]) CompareAndSwap(old, new *T) bool {
	if a.p != old {
		return false
	}
	a.p = new
	return true
}
