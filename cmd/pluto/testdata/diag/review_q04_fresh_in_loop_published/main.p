package main

// p := &W{} inside the loop; the closure of mk(i) stored in p.f; p published to an outer slice element.
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }



func main() {
	var keep [3]*W
	for i := 0; i < 3; i++ {
		p := &W{}
		p.f = mk(i)
		keep[i] = p
	}
	println(keep[0].f())
}
