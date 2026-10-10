package main

// main is small enough at -O2 to be inlined into the generated pluto.main,
// where the trace must still show it as a frame of its own.

//pu:noinline
func check(n int) {
	if n > 2 {
		panic("too big")
	}
}

func main() {
	check(1)
	check(3)
}
