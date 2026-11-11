package main

import (
	"pluto/arena"
	"pluto/sync"
)

// The twin of scoperejected_the_starter_resets_an_arena_while_a_task_reads_an_object_from_it:
// the Reset comes after the join.

func main() {
	a := arena.NewFixed(1024)
	p := a.New[int]()
	*p = 1
	var s sync.Scope
	s.Task(func() { println(*p) })
	s.Wait()
	a.Reset()
}
