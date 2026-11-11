package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Queue owner struct {
	mu       sync.Mutex
	nonEmpty sync.Cond
	items    vec.Vec[int]
}

func (q *Queue) waitNonEmpty() {
	for q.items.Len() == 0 {
		q.nonEmpty.Wait(&q.mu)
	}
}

func (q *Queue) peekWait() int {
	q.mu.Lock()
	p := q.items.Ptr(0)
	q.waitNonEmpty()
	v := *p
	q.mu.Unlock()
	return v
}

var q = &Queue{}

func main() {
	task q.peekWait()
	q.mu.Lock()
	q.items.Push(1)
	q.mu.Unlock()
	q.nonEmpty.Signal()
}
