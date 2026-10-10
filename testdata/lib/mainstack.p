package main

// //pu:stack runs main on a reserved stack of the given size, larger than
// the 8 MiB Linux gives a process by default, and rt.StackLeft tells deeply
// recursive code how much of it remains.

import "pluto/rt"

// depth recurses n times with about 1 KiB of frame each and returns the
// smallest StackLeft it saw.
//
//pu:noinline
func depth(n int, buf *[128]int) uintptr {
	var local [128]int
	local[n%128] = n
	if buf != nil {
		local[0] += buf[0]
	}
	if n == 0 {
		return rt.StackLeft()
	}
	return depth(n-1, &local)
}

//pu:stack 96M
func main() {
	lo, hi := rt.StackBounds()
	println("reserved", (hi-lo)>>20, "MiB")
	left := rt.StackLeft()
	println("left at main", left > 95<<20, left <= 96<<20)
	// 64 Ki frames of over 1 KiB: more than 64 MiB of stack.
	deepest := depth(64<<10, nil)
	used := left - deepest
	println("used more than 64 MiB", used > 64<<20, "and still left", deepest > 0)
}
