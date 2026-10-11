// pluto/sync: Mutex, RWMutex, WaitGroup, Once and Cond under contention.
// With the imports changed to Go's sync and sync/atomic, Go prints the
// same.

package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
)

var (
	mu      sync.Mutex
	counter int
	wg      sync.WaitGroup
)

func adder(n int) {
	for i := 0; i < n; i++ {
		mu.Lock()
		counter++
		mu.Unlock()
	}
	wg.Done()
}

var (
	rw     sync.RWMutex
	pair   [2]int // a writer keeps both halves equal
	torn   atomic.Int32
	reads  atomic.Int64
	rwDone sync.WaitGroup
)

func reader(n int) {
	for i := 0; i < n; i++ {
		rw.RLock()
		if pair[0] != pair[1] {
			torn.Add(1)
		}
		rw.RUnlock()
		reads.Add(1)
	}
	rwDone.Done()
}

func writer(n int) {
	for i := 0; i < n; i++ {
		rw.Lock()
		pair[0]++
		pair[1]++
		rw.Unlock()
	}
	rwDone.Done()
}

var (
	once     sync.Once
	initRuns atomic.Int32
	onceWG   sync.WaitGroup
)

func setup() { initRuns.Add(1) }

func useOnce() {
	once.Do(setup)
	onceWG.Done()
}

// A queue guarded by a mutex and a condition variable.
var (
	qmu    sync.Mutex
	qcond  = sync.Cond{L: &qmu}
	queue  [16]int
	qn     int
	closed bool
	qsum   atomic.Int64
	qWG    sync.WaitGroup
)

func consume() {
	for {
		qmu.Lock()
		for qn == 0 && !closed {
			qcond.Wait()
		}
		if qn == 0 && closed {
			qmu.Unlock()
			qWG.Done()
			return
		}
		qn--
		v := queue[qn]
		qcond.Broadcast() // room for the producer
		qmu.Unlock()
		qsum.Add(int64(v))
	}
}

func produce(n int) {
	for i := 1; i <= n; i++ {
		qmu.Lock()
		for qn == len(queue) {
			qcond.Wait()
		}
		queue[qn] = i
		qn++
		qcond.Signal()
		qmu.Unlock()
	}
	qmu.Lock()
	closed = true
	qcond.Broadcast()
	qmu.Unlock()
}

var goWG sync.WaitGroup
var goRuns atomic.Int32

func bump() { goRuns.Add(1) }

func main() {
	const workers = 8
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		task adder(10000)
	}
	wg.Wait()
	// mu guards counter (every write holds it), so reads hold it too.
	mu.Lock()
	println("mutex counter", counter)
	mu.Unlock()

	if !mu.TryLock() {
		println("TryLock failed on a free mutex")
	}
	if mu.TryLock() {
		println("TryLock succeeded on a held mutex")
	}
	mu.Unlock()

	rwDone.Add(6)
	for i := 0; i < 4; i++ {
		task reader(20000)
	}
	task writer(5000)
	task writer(5000)
	rwDone.Wait()
	rw.RLock()
	println("rwmutex", pair[0], pair[1], "torn reads", torn.Load(), "reads", reads.Load())
	rw.RUnlock()
	if !rw.TryRLock() {
		println("TryRLock failed")
	}
	if rw.TryLock() {
		println("TryLock succeeded with a reader")
	}
	rw.RUnlock()
	if !rw.TryLock() {
		println("TryLock failed on a free RWMutex")
	}
	rw.Unlock()

	onceWG.Add(10)
	for i := 0; i < 10; i++ {
		task useOnce()
	}
	onceWG.Wait()
	println("once ran", initRuns.Load())

	qWG.Add(3)
	for i := 0; i < 3; i++ {
		task consume()
	}
	produce(10000)
	qWG.Wait()
	println("cond sum", qsum.Load())

	for i := 0; i < 5; i++ {
		goWG.Task(bump)
	}
	goWG.Wait()
	println("WaitGroup.Task ran", goRuns.Load())
}
