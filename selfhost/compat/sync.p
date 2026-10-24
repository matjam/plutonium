package compat

// The compiler is single-threaded (docs/plans/2026-10-09-self-hosting-plan.md
// sections 4.1 and 6.2), so the locks and atomics of the Go sources are
// no-ops. go2pu maps sync.Mutex, sync.RWMutex, sync.Once and
// sync/atomic.Pointer to these types; they take no part in the scheduler.

// NumCPU is runtime.NumCPU for a compiler that runs one task: 1, so that
// what the Go sources size by the CPUs (the workers of the new escape
// analysis's parallel driver, internal/escape2/sched) runs in this task.
func NumCPU() int { return 1 }

// A Mutex is sync.Mutex without waiting: the compiler runs one task.
type Mutex struct{}

func (m *Mutex) Lock()   {}
func (m *Mutex) Unlock() {}

// A Once is sync.Once for one task.
type Once struct {
	done bool
}

func (o *Once) Do(f func()) {
	if !o.done {
		o.done = true
		f()
	}
}

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
