package main

import (
	"pluto/fmt"
	"pluto/sync"
)

// Scoped tasks: parallel work that borrows the caller's variables.
//
// A task statement starts a task that runs in parallel with its starter. A
// sync.Scope starts tasks that may also use the local variables of the
// function that starts them, and waits for every one of them before its own
// block ends, so the variables are still there (language spec section 19.4).
// The compiler checks that the tasks cannot race (section 19.3), with no
// annotations:
//
//   - tasks that write different elements of an array or slice, each by its
//     own index, need no lock;
//   - a variable that several tasks write is guarded by a sync.Mutex, and the
//     compiler infers which lock guards it from the locks held at each
//     write. A write with no lock held is a compile error (E3502).
//
// Each iteration of a loop has its own i, so every task below sees the
// value of the iteration that started it.

// squares fills one element per task. Each task owns out[i], so the tasks do
// not touch each other's data.
func squares() [8]int {
	var out [8]int
	var s sync.Scope
	for i := 0; i < len(out); i++ {
		s.Task(func() { out[i] = i * i })
	}
	s.Wait() // after Wait every task has returned, so reading out is safe
	return out
}

func main() {
	fmt.Println("squares:", squares())

	// One variable written by every task: hold the mutex at each write.
	var mu sync.Mutex
	total := 0
	var s sync.Scope
	for i := 1; i <= 100; i++ {
		s.Task(func() {
			mu.Lock()
			total += i
			mu.Unlock()
		})
	}
	s.Wait()
	fmt.Println("sum 1..100 =", total)
}
