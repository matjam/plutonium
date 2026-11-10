package main

import "pluto/sync"

// waitNonEmpty waits on q.mu, which its caller holds; the caller holds
// it again after the call.
type Queue struct {
	mu       sync.Mutex
	nonEmpty sync.Cond
	n        int
}

func (q *Queue) waitNonEmpty() {
	for q.n == 0 {
		q.nonEmpty.Wait(&q.mu)
	}
}

func (q *Queue) take(done chan int) {
	q.mu.Lock()
	q.waitNonEmpty()
	q.n--
	v := q.n
	q.mu.Unlock()
	done <- v
}

var q Queue

func main() {
	done := make(chan int)
	task q.take(done)
	q.mu.Lock()
	q.n = 2
	q.mu.Unlock()
	q.nonEmpty.Signal()
	println(<-done)
}
