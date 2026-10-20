package main

// As q02 without p = &W{} (control).
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }

//pu:noinline
func run(keep *W) {
	p := keep
	func() {
		for i := 0; i < 3; i++ {
			p.f = mk(i)
		}
	}()
}

func main() {
	var w W
	run(&w)
	println(w.f())
}
