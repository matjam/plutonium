package main

// bad ignores yield's result and calls it again after the loop body asked
// it to stop.
func bad(yield func(int) bool) {
	yield(1)
	yield(2)
}

func main() {
	for x := range bad {
		println(x)
		break
	}
}
