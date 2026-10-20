package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

// A String method that keeps its receiver must not make every program that
// formats a value fail the frame check: the compiler cannot tell which type
// fmt calls String on, and must not take a pointer into fmt's own frame for
// the receiver of a pointer method. (The type checker of the self-hosted
// compiler has such methods.)

type Named struct{ name string }

var last *Named

func (n *Named) String() string {
	last = n
	return "named " + n.name
}

type Wide struct{ a, b, c int }

func (w Wide) String() string { return fmt.Sprint("wide ", w.a+w.b+w.c) }

type Choice union {
	N int
	W Wide
}

func main() {
	n := alloc.New[Named]()
	*n = Named{"a"}
	fmt.Println(n)
	fmt.Println(1, "x", 2.5)
	fmt.Printf("%v %s %d\n", n, n, 7)
	fmt.Println(last.name)
	fmt.Println(Wide{1, 2, 3})
	c := Choice.W(Wide{4, 5, 6})
	fmt.Println(c)
}
