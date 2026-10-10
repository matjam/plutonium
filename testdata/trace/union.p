package main

type State union {
	Idle
	Busy int
}

func busy(s State) int {
	return s.(State.Busy)
}

func main() {
	println(busy(State.Busy(3)))
	println(busy(State.Idle))
}
