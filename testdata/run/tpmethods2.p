// More calls through type parameters: deferred calls, closures, method
// expressions T.m, interface type arguments, methods with their own type
// parameters, chained calls and comparable constraints with methods.
package main

import "pluto/strings"

type Stringer interface{ String() string }

type S struct{ name string }

func (s S) String() string { return s.name }

type P struct{ n int }

func (p *P) String() string {
	p.n++
	if p.n%2 == 1 {
		return "odd"
	}
	return "even"
}

func callTwice[T Stringer](x T) {
	defer println("deferred", x.String())
	defer x.String()
	f := func() string { return x.String() }
	println("closure", f())
}

func methodExpr[T Stringer](x T) string {
	f := T.String
	return f(x)
}

func viaInterface[T Stringer](x T) string {
	return x.String()
}

type Vec[E any] struct {
	items [4]E
	n     int
}

func (v *Vec[E]) Push(x E) { v.items[v.n] = x; v.n++ }

func (v *Vec[E]) Each[U Stringer](u U, f func(E, string)) {
	for i := 0; i < v.n; i++ {
		f(v.items[i], u.String())
	}
}

func (v *Vec[E]) MapS[U any](f func(E) U) Vec[U] {
	var out Vec[U]
	for i := 0; i < v.n; i++ {
		out.Push(f(v.items[i]))
	}
	return out
}

type Named[E Stringer] struct{ v Vec[E] }

func (n *Named[E]) All() {
	for i := 0; i < n.v.n; i++ {
		println("named", n.v.items[i].String())
	}
	got := n.v.MapS(func(e E) int { return len(e.String()) })
	println("lens", got.n, got.items[0])
}

type Getter[U any] interface{ Get() U }

type Box[U any] struct{ v U }

func (b Box[U]) Get() U { return b.v }

func chain[G Getter[S]](g G) string { return g.Get().String() }

type Cmp interface {
	comparable
	String() string
}

func eqStr[T Cmp](a, b T) string {
	if a == b {
		return strings.Slice(a.String(), 0, 1)
	}
	return b.String()
}

type W struct {
	S
	k int
}

func main() {
	callTwice(S{"s"})
	callTwice(&P{})
	println(methodExpr(S{"me"}), methodExpr(&P{}))
	println(viaInterface[Stringer](S{"iface"}), viaInterface[Stringer](&P{}))
	var v Vec[int]
	v.Push(1)
	v.Push(2)
	v.Each(S{"u"}, func(e int, s string) { println("each", e, s) })
	p := &P{}
	v.Each(p, func(e int, s string) { println("eachp", e, s) })
	var n Named[S]
	n.v.Push(S{"abc"})
	n.v.Push(S{"de"})
	n.All()
	println(chain(Box[S]{S{"chained"}}))
	println(eqStr(S{"x"}, S{"x"}), eqStr(S{"x"}, S{"y"}), eqStr(W{S{"w"}, 1}, W{S{"w"}, 1}))
}
