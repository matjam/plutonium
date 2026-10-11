package main

// Only clear, close, copy, delete, panic, print and println may be deferred
// directly (spec section 12.19). A deferred value-producing builtin is a
// lowering error, even though it discards its result.

func f() {
	var s [3]int
	defer len(s)
	defer cap(s[:])
}

func main() {
	f()
}
