package main

// The literal reads v when it is called, after v was moved (language spec
// sections 18.3 and 18.5).

type L owner struct{ x int }

func eat(l L) {}

func main() {
	v := L{x: 7}
	f := func() int { return v.x }
	eat(v)
	println(f())
}
