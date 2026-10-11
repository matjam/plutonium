package main

// A panic while the tasks of a sync.Scope run: the task that panics
// runs its deferred calls, but no variable is dropped, so the Scope does
// not wait, and the program stops. The tasks use main's frame, which is
// never reclaimed while they could still run.

import "pluto/sync"

func main() {
	release := make(chan bool)
	var seen [2]bool
	defer println("main deferred")
	var s sync.Scope
	for i := range seen {
		s.Task(func() {
			<-release
			seen[i] = true
		})
	}
	var xs []int
	println(xs[len(seen)])
}
