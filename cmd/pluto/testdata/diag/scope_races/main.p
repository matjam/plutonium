package main

// The sharing rules of sync.Scope (language spec section 4.11): tasks that
// may run at the same time as each other, or as the function that started
// them, must not use one place at once with one of them writing, unless a
// lock that both hold, an atomic type or a partition of the elements keeps
// them apart.

import "pluto/sync"

// An unpartitioned write: the tasks may write the same element.
func scatter(out []int, idx []int) {
	var s sync.Scope
	for i := range idx {
		s.Task(func() { out[idx[i]] = i })
	}
}

// A read before the join: s waits for the tasks only at the end of first.
func first(items []int) int {
	var results [4]int
	var s sync.Scope
	for i := range results {
		s.Task(func() { results[i] = items[i] * 2 })
	}
	return results[0]
}

// A counter that every task writes without a lock.
func count(counts []int) int {
	var total int
	var s sync.Scope
	for _, n := range counts {
		s.Task(func() { total += n })
	}
	s.Wait()
	return total
}

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func consume(b Buf) {}

// Moving a variable a task may still use.
func move() {
	var b Buf
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	consume(b)
}

// A task that changes a variable it takes, which the loop uses again.
func taken(items []int) {
	var s sync.Scope
	for _, it := range items {
		n := 0
		s.Task(func() { n = it })
		println(n)
	}
}

// Map calls f from several tasks at once.
func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = f(in[i]) })
	}
}

func main() {
	scatter(nil, nil)
	println(first([]int{1, 2, 3, 4}))
	println(count([]int{1, 2}))
	move()
	taken(nil)
	var lens [2]int
	calls := 0
	Map([]string{"a", "bb"}, lens[:], func(s string) int { calls++; return len(s) })
	println(calls)
}
