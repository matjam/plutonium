package main

var global = make(chan int, 1)

var kept any

// An ordinary interface lends a channel, as it lends a shared[T]: it is a
// view of the variable converted and counts no reference of its own.
func temp() any { return make(chan int) }

func param(c chan int) any { return c }

func store(c chan int) { kept = c }

// A channel in a package variable may be lent anywhere.
func fromGlobal() any { return global }

// The fix: return the channel itself, which counts a reference.
func fixed() chan int { return make(chan int) }

func main() {
	_ = temp()
	_ = param(nil)
	store(global)
	_ = fromGlobal()
	_ = fixed()
}
