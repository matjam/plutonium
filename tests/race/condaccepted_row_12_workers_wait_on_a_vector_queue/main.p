package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Job struct{ id int }

var (
	mu       sync.Mutex
	nonEmpty sync.Cond
	queue    vec.Vec[Job]
)

func push(j Job) {
	mu.Lock()
	queue.Push(j)
	mu.Unlock()
	nonEmpty.Signal()
}

func worker(done chan int) {
	mu.Lock()
	for queue.Len() == 0 {
		nonEmpty.Wait(&mu)
	}
	j, _ := queue.Pop()
	mu.Unlock()
	done <- j.id
}

func main() {
	done := make(chan int)
	task worker(done)
	task worker(done)
	push(Job{id: 1})
	push(Job{id: 2})
	println(<-done + <-done)
}
