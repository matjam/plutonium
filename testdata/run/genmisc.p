// Method values and expressions of methods with their own type
// parameters, generic function instances in interfaces and asserted back,
// interface comparison of generic values, type parameters named like
// predeclared types, and generic functions as func values.
package main

type Stack[T any] struct {
	items [4]T
	n     int
}

func (s *Stack[T]) Push(v T) { s.items[s.n] = v; s.n++ }

func (s *Stack[T]) Map[U any](f func(T) U) Stack[U] {
	var out Stack[U]
	for i := 0; i < s.n; i++ {
		out.Push(f(s.items[i]))
	}
	return out
}

func (s Stack[T]) Fold[A any](a A, f func(A, T) A) A {
	for i := 0; i < s.n; i++ {
		a = f(a, s.items[i])
	}
	return a
}

func Id[T any](x T) T { return x }

// Type parameters named like predeclared types.
func Shadow[int any, string comparable](a int, b string) (int, bool) {
	var z string
	return a, b == z
}

type Pair[K comparable, V any] struct {
	k K
	v V
}

func Eq[T comparable](a, b T) bool { return a == b }

type Num int

func (n Num) Twice() Num { return n * 2 }

func Twicer[T interface{ Twice() T }](x T) T { return x.Twice().Twice() }

func main() {
	var s Stack[int]
	s.Push(3)
	s.Push(4)
	m := s.Map[string]
	out := m(func(i int) string { return "abcdef"[:i] })
	println(out.n, out.items[1])
	mx := (*Stack[int]).Map[bool]
	bs := mx(&s, func(i int) bool { return i > 3 })
	println(bs.items[0], bs.items[1])
	fold := s.Fold[int]
	println(fold(10, func(a, x int) int { return a * x }))
	fx := Stack[int].Fold[string]
	println(fx(s, "", func(a string, x int) string { return a[:0] }) == "")

	var f any = Id[int]
	if g, ok := f.(func(int) int); ok {
		println(g(41) + 1)
	}
	_, ok := f.(func(string) string)
	println(ok)

	var a, b any = Pair[int, string]{1, "x"}, Pair[int, string]{1, "x"}
	println(a == b, a == any(Pair[int, string]{2, "x"}), a == any(Pair[int, int]{1, 0}))
	println(Eq[any](1, 1), Eq[any]("a", 1), Eq(Pair[string, int]{"a", 1}, Pair[string, int]{"a", 1}))

	println(Shadow(5, "s"))
	println(Shadow(5, ""))
	println(Twicer(Num(3)))

	var fn func(int) int
	println(fn == nil)
	fn = Id[int]
	println(fn != nil, fn(7))
}
