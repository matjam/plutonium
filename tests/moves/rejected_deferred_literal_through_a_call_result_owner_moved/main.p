package main

type L owner struct{ x int }

func eat(l L) {}

func id(f func()) func() { return f }

func main() {
	v := L{x: 1}
	defer id(func() { println(v.x) })()
	eat(v)
}
