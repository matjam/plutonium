package main

// A deferred literal reached through a field, an element or a call result
// uses its owners when the function returns (language spec section 18.5).

type L owner struct{ x int }

func eat(l L) {}

type S struct{ f func() }

func main() {
	v := L{x: 1}
	s := S{f: func() { println(v.x) }}
	defer s.f()
	eat(v)
}
