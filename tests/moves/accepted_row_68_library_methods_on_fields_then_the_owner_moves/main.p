package main

import (
	"pluto/hash"
	"pluto/sync"
	"pluto/sync/atomic"
	"pluto/vec"
)

// An owner whose library fields are used through their methods, then moved
// to a task: none of these calls keeps a view of what the owner holds, so
// the move is allowed (the false E3314 of spec section 19.3 table row 68).
type State owner struct {
	mu   sync.Mutex
	rw   sync.RWMutex
	wg   sync.WaitGroup
	once sync.Once
	n32  atomic.Int32
	u32  atomic.Uint32
	i64  atomic.Int64
	u64  atomic.Uint64
	v    vec.Vec[int]
	m    hash.Map[int, int]
	n    int
}

func setup() { println("once") }

func worker(s State, done chan int) {
	done <- s.n + s.v.Len() + s.m.Len()
}

func main() {
	var s State
	s.m = hash.NewMap[int, int](4)

	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	if s.mu.TryLock() {
		s.mu.Unlock()
	}

	s.rw.RLock()
	_ = s.n
	s.rw.RUnlock()
	s.rw.Lock()
	s.n++
	s.rw.Unlock()

	s.wg.Add(1)
	s.wg.Done()
	s.wg.Wait()

	s.once.Do(setup)

	s.n32.Store(1)
	s.n32.Add(2)
	s.n32.CompareAndSwap(3, 4)
	_ = s.n32.Swap(5)
	s.u32.Store(s.u32.Load() + 1)
	s.u32.CompareAndSwap(s.u32.Load(), 7)
	s.i64.Add(s.i64.Load())
	s.u64.Store(s.u64.Load() | 1)
	x := s.u64.Load()
	s.u64.CompareAndSwap(x, x+1)

	s.v.Push(1)
	s.v.Push(2)
	s.v.Set(0, s.v.At(1))
	_, _ = s.v.Pop()
	s.m.Set(1, s.v.Len())
	if k, ok := s.m.Get(1); ok {
		s.m.Set(2, k)
	}
	_ = s.m.Has(2)
	s.m.Delete(2)

	done := make(chan int)
	task worker(s, done)
	println(<-done)
}
