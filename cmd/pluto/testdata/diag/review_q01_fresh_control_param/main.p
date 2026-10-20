package main

// Control: a closure of a call in a loop stored through a pointer parameter (E3101).
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }

//pu:noinline
func run(keep *W) {
	for i := 0; i < 3; i++ {
		keep.f = mk(i)
	}
}

func main() {
	var w W
	run(&w)
	println(w.f())
}
