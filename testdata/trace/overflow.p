package main

func recurse(n int) int {
	var buf [64]int
	buf[n&63] = n
	return recurse(n+1) + buf[(n+1)&63]
}

// A stack overflow leaves no stack to run deferred calls on: this one does
// not run (TestTraceStackOverflow checks the output line by line).
func main() {
	defer println("deferred: not run")
	println(recurse(0))
}
