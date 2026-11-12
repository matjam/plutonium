package main

type L owner struct{ x int }

func eat(l L) {}

func main() {
	v := L{x: 7}
	f := func() int { return v.x }
	pf := &f
	eat(v)
	println((*pf)())
}
