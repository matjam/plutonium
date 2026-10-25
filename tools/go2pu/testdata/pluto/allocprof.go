package main

// startAllocProfile starts the self-hosted compiler's allocation profile,
// which PLUTO_ALLOCPROF asks for, and returns the function that reports it.
// The translation replaces this file with the hand-written allocprof.p;
// here Go's own profiles cover it, so it does nothing.
func startAllocProfile() func() {
	return func() {}
}
