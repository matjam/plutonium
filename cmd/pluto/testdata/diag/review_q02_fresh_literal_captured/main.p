package main

// Inside a function literal, captured p aliases the caller's *W; the literal also assigns p = &W{}, so its own analysis takes p for a fresh allocation.
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }

//pu:noinline
func run(keep *W) {
	p := keep
	func() {
		for i := 0; i < 3; i++ {
			p.f = mk(i)
		}
		if false {
			p = &W{}
		}
	}()
}

func main() {
	var w W
	run(&w)
	println(w.f())
}
