package main

// The body of a range loop over a function is the yield function the loop
// passes to the iterator, a closure built in the frame of the function
// around the loop (spec section 12.11). What the body keeps of the values
// yield is passed is checked against what the iterator passes it, and an
// iterator may not keep yield.

var keep *int

var keepYield func(*int) bool

// seq passes yield a pointer into its own frame.
func seq(yield func(*int) bool) {
	x := 42
	yield(&x)
}

// stash keeps yield after it returns.
func stash(yield func(*int) bool) {
	keepYield = yield
}

// Kept keeps what seq yields after the loop.
func Kept() {
	var last *int
	for p := range seq {
		last = p
	}
	keep = last
}

// Stashed runs a loop whose yield stash keeps.
func Stashed() {
	n := 0
	for p := range stash {
		n += *p
	}
	println(n)
}

// Fine: the body uses the value only while yield runs.
func Fine() int {
	sum := 0
	for p := range seq {
		sum += *p
	}
	return sum
}

func main() {
	Kept()
	Stashed()
	println(Fine())
}
