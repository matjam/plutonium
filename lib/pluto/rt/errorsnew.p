package rt

// errorsNewCount adds one to the counter of an errors.New call site and
// returns the new value, which is the data word of the error that the
// evaluation makes (language spec section 16.3). The counter is atomic and
// never wraps: a counter that has reached its maximum panics at the next
// evaluation, and keeps its maximum.
func errorsNewCount(c *uint64) uint64 {
	n := xadd64(c, 1)
	if n == 0 {
		store64(c, ^uint64(0))
		panicMessage("runtime error: an errors.New call was evaluated more than 2^64-1 times", "", "", "", "")
	}
	return n
}
