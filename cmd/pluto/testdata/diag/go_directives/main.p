//go:build linux

// A //go: line comment is an error that names the //pu: directive doing the
// same job, or says there is none (spec sections 4.1 and 20.3).

package main

//go:generate stringer -type=Color

//go:noinline
func slow(n int) int { return n * 2 }

//go:nosplit
func fast(n int) int { return n + 1 }

func main() {
	println(slow(2), fast(3)) // go:noinline after a space is a note
}
