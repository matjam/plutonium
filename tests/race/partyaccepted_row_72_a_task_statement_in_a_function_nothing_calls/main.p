package main

var x int

func w() { x++ }

// start is never called, so its task statement starts no party.
func start() {
	task w()
	x++
}

func main() {
	x = 1
	println(x)
}
