package main

// sync.Scope waits for its tasks on every path out of its block (language
// spec section 4.11): a return from inside a loop, a break, a function that
// leaves with rt.Goexit (as testing.FailNow does), and a task that ends
// itself with rt.Goexit. Each task counts its end in finished only after
// it has used the frame of the function that started it, so finished shows
// that the Scope waited before that frame was reclaimed. The program also
// runs under the debug allocator with one, two and many processors
// (internal/build/scope_test.go).

import (
	"pluto/rt"
	"pluto/sync"
	"pluto/sync/atomic"
)

var finished atomic.Int64

// spin makes a task run for a while, so that a Scope that did not wait
// would be seen not to.
func spin(n int) int {
	x := 0
	for i := 0; i < n*1000; i++ {
		x += i & 3
	}
	return x
}

// leave ends its task with rt.Goexit while its tasks wait to be released:
// its deferred call releases them, and s's drop then waits for them.
func leave(release chan bool) {
	defer close(release)
	var cells [4]int
	var s sync.Scope
	for i := range cells {
		s.Task(func() {
			<-release
			cells[i] = spin(i + 1)
			finished.Add(1)
		})
	}
	rt.Goexit()
	println("not reached")
}

// find returns from inside its loop, and the Scope declared in the loop
// body waits for that iteration's task.
func find(target int) int {
	for i := 0; i < 10; i++ {
		var s sync.Scope
		s.Task(func() {
			finished.Add(int64(spin(2)*0 + 1))
		})
		if i == target {
			return i
		}
	}
	return -1
}

// breaks leaves a block with break while its tasks run.
func breaks() int {
	n := 0
	for {
		var out [3]int
		var s sync.Scope
		for i := range out {
			s.Task(func() {
				out[i] = spin(3) * 0
				finished.Add(1)
			})
		}
		n++
		if n == 2 {
			break
		}
	}
	return n
}

// quitters end themselves with rt.Goexit: their deferred calls run, and
// the Scope counts them as returned.
func quitters() {
	var s sync.Scope
	for i := 0; i < 3; i++ {
		s.Task(func() {
			defer finished.Add(1)
			if i >= 0 {
				rt.Goexit()
			}
			println("not reached")
		})
	}
}

func main() {
	release := make(chan bool)
	exited := rt.CatchGoexit(func() { leave(release) })
	println("leave", exited, finished.Load())

	finished.Store(0)
	println("find", find(3), finished.Load())

	finished.Store(0)
	println("breaks", breaks(), finished.Load())

	finished.Store(0)
	quitters()
	println("quitters", finished.Load())
}
