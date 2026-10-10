// Methods with their own type parameters promoted from an embedded
// generic struct or pointer: calls with inferred and explicit type
// arguments, method values and method expressions, and a closure with a
// local type that uses both the receiver's and the method's type
// parameters.
package main

type Stack[T any] struct {
	items [4]T
	n     int
}

func (s *Stack[T]) Push(v T) { s.items[s.n] = v; s.n++ }

func (s *Stack[T]) Map[U any](f func(T) U) Stack[U] {
	var out Stack[U]
	apply := func(i int) {
		type pair struct {
			t T
			u U
		}
		p := pair{s.items[i], f(s.items[i])}
		out.Push(p.u)
	}
	for i := 0; i < s.n; i++ {
		apply(i)
	}
	return out
}

func (s Stack[T]) Count[U comparable](f func(T) U, want U) int {
	n := 0
	for i := 0; i < s.n; i++ {
		if f(s.items[i]) == want {
			n++
		}
	}
	return n
}

type Named struct {
	name string
	Stack[int]
}

type PNamed struct {
	*Stack[string]
}

func main() {
	var n Named
	n.Push(3)
	n.Push(4)
	m := n.Map(func(x int) bool { return x > 3 })
	println(m.n, m.items[0], m.items[1])
	println(n.Count(func(x int) int { return x % 2 }, 1), n.Count[bool](func(x int) bool { return x > 0 }, true))
	mv := n.Map[string]
	ms := mv(func(x int) string { return "abcdef"[:x] })
	println(ms.items[1])
	p := PNamed{&Stack[string]{}}
	p.Push("q")
	println(p.Map(func(s string) int { return len(s) }).items[0], p.Count(func(s string) string { return s }, "q"))
	ex := (*Named).Map[int]
	println(ex(&n, func(x int) int { return -x }).items[1])
}
