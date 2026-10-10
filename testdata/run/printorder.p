package main

// print and println evaluate all of their arguments before printing any of
// them, so output from calls in the argument list comes first.

var counter int

//pu:noinline
func loud(n int) int {
	println("loud", n)
	return n * 10
}

//pu:noinline
func get() int { return counter }

//pu:noinline
func bump() int {
	counter += 5
	println("bump", counter)
	return counter
}

func pair() (int, bool) {
	println("pair")
	return 7, true
}

func name(s string) string {
	println("name", s)
	return s
}

func main() {
	println(loud(1), loud(2), loud(3))
	println(get(), bump(), get())
	print(loud(4), " ", bump(), "\n")
	println(pair())
	println(name("a"), loud(5) > 40, name("b"))
	x := 3
	println(x, loud(x))
}
