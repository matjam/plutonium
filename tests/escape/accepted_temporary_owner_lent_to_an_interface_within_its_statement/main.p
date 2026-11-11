package main

// A temporary owner lent to an ordinary interface may be used within the
// statement that makes it (sections 7.9 and 18.3).

import "pluto/fmt"

type Big owner struct{ a, b, c int }

func (b *Big) Drop() { println("drop", b.a) }

func makeBig(n int) Big { return Big{n, 2, 3} }

func show(i any) { fmt.Println(i) }

func main() {
	fmt.Println(makeBig(1))
	fmt.Println(any(makeBig(2)))
	show(makeBig(3))
	println("end")
}
