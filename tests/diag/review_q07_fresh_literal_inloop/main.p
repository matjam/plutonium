package main

// Literal inside the loop stores mk(i) through captured p (aliasing outer w), and also assigns p = &W{}.
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }



func main() {
	var w W
	p := &w
	for i := 0; i < 3; i++ {
		func() {
			p.f = mk(i)
			if false {
				p = &W{}
			}
		}()
	}
	println(w.f())
}
