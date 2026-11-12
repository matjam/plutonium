package main

type L owner struct{ x int }

func eat(l L) {}

func main() {
	v := L{x: 1}
	fs := [1]func(){func() { println(v.x) }}
	defer fs[0]()
	eat(v)
}
