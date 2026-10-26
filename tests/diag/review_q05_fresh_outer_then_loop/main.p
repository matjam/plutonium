package main

// p := new(W) before the loop; p.f = mk(i) inside the loop; kept past the iteration in p's memory.
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }



func main() {
	p := new(W)
	for i := 0; i < 3; i++ {
		p.f = mk(i)
	}
	println(p.f())
}
