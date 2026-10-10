package main

// A default argument that panics. The default is evaluated in the caller,
// so the trace reports the call, a line of the caller, rather than the
// default's line in the callee's declaration.

var table = []int{1, 2}

func pick(i int = table[len(table)]) int {
	return i
}

func main() {
	println(pick(1))
	println(pick())
}
