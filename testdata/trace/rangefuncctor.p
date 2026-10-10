package main

// Count is a closure constructor: the call in main builds its closure in
// main's frame, but the trace names the closure after Count, as Go does.
// The closure panics reading past the end of table.
func Count(n int, table []int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(table[i]) {
				return
			}
		}
	}
}

func main() {
	table := []int{1, 2, 3}
	for x := range Count(5, table) {
		println(x)
	}
}
