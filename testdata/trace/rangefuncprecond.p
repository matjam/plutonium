package main

// CountChecked checks its argument before returning its iterator, whose
// closure is built in a region of the caller's frame. The check panics,
// and the trace names CountChecked's own frame.
func CountChecked(n int) func(yield func(int) bool) {
	if n < 1 {
		panic("CountChecked: n must be positive")
	}
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	for x := range CountChecked(2) {
		println(x)
	}
	for x := range CountChecked(0) {
		println(x)
	}
}
