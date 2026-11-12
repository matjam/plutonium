package main

type L owner struct{ x int }

func eat(l L) {}

type S struct{ f func() int }

func main() {
	v := L{x: 7}
	s := S{f: func() int { return v.x }}
	eat(v)
	println(s.f())
}
