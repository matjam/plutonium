package main

import "pluto/sync"

// Two slices of one type may overlap. Tasks that use them only as out[i] and
// in[i] are accepted, and the program checks, before each task starts, that
// the slices start at the same element or do not overlap. Any other use of
// two such slices is an error.

func double(in, out []int) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] * 2 })
	}
}

func shifted(in, out []int) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i+1] * 2 })
	}
}

func main() {
	var buf [4]int
	double(buf[:], buf[:])
	shifted(buf[:], buf[:])
}
