// union is not a keyword (spec section 4.6): a type, variable, field or
// parameter may be named union.
package main

type union struct {
	union int
}

type T struct {
	union
	n int
}

type V struct {
	union []union
}

func f(union int) int { return union * 2 }

func g() union { return union{union: 4} }

func local() int {
	var union = 3
	union++
	return union
}

func main() {
	n := local()
	u := g()
	t := T{union: u, n: 1}
	v := V{union: []union{u, {union: 5}}}
	println(n, f(n), u.union, t.union.union, t.n, len(v.union), v.union[1].union)
}
