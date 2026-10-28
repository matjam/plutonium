package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

// A pointer method that keeps a pointer it loads from its receiver keeps
// nothing of a string the program prints: pluto/fmt's walk calls the method
// on the address of a value of its type, which the string is not, and what
// the method keeps is loaded from that value, not the value itself.

type Inner struct{ n int }

type T struct{ p *Inner }

var keep *Inner

func (t *T) String() string {
	keep = t.p
	return "t"
}

type Outer struct{ t *T }

func (o *Outer) String() string { return o.t.String() }

func show(name string, w string) {
	fmt.Println(name)
	fmt.Printf("%s %s\n", name, w)
}

func main() {
	t := alloc.New[T]()
	t.p = alloc.New[Inner]()
	o := alloc.New[Outer]()
	o.t = t
	fmt.Println(t, o)
	s := fmt.Sprint("a", 1)
	show(s, s)
}
