package main

// An owner larger than one word lent to an ordinary interface is not
// copied: the data word points at the variable (language spec section 7.9,
// abi.md section 5, D61). Each interface below sees the change made after
// the conversion. A temporary owner lent for one statement is dropped when the
// statement ends.

import "pluto/fmt"

type Big owner struct{ a, b, c int }

func (b *Big) Drop() { fmt.Println("drop", b.a) }

type Outer owner struct {
	x Big
	n int
}

var pkg Big

func viaParam(i any) { fmt.Println(i) }

func main() {
	o := Big{1, 2, 3}
	var i any = o
	o.a = 7
	fmt.Println(i)
	viaParam(o)
	viaParam(Big{8, 8, 8})
	out := Outer{x: Big{4, 5, 6}}
	j := any(out.x)
	out.x.c = 60
	fmt.Println(j)
	p := &o
	k := any(*p)
	p.c = 30
	fmt.Println(k)
	q := any(pkg)
	pkg.a = 9
	fmt.Println(q)
	fmt.Println("end")
}
