// Variadic and multi-result generic functions, mutual recursion, and the
// built-ins through core types: composite literals, slicing, copy,
// clear, min and max (of strings too), len; arrays and structs of type
// parameters compared; pointer methods through a *T constraint; named
// results of type T changed by a deferred closure.
package main

type Number interface{ ~int | ~int32 | ~float64 }

func Sum[T Number](xs ...T) T {
	var t T
	for _, x := range xs {
		t += x
	}
	return t
}

func Two[T any](a, b T) (T, T) { return b, a }

func Swapped[T any](a, b T) (x, y T) {
	x, y = Two(a, b)
	return
}

func Even[T Number](n T) bool {
	if n == 0 {
		return true
	}
	return Odd(n - 1)
}

func Odd[T Number](n T) bool {
	if n == 0 {
		return false
	}
	return Even(n - 1)
}

// Composite literals, slicing, copy, clear and min/max through core
// types.
func Lit[S ~[]E, E Number]() (int, E) {
	s := S{1, 2, 3}
	return len(s), s[2]
}

type Point struct{ X, Y int }

func StructLit[P ~struct{ X, Y int }](x int) P { return P{X: x, Y: x * 2} }

func ArrLit[A ~[3]E, E any](e E) A { return A{2: e} }

func Grow[S ~[]E, E any](buf S, n int, e E) S {
	s := buf[:0]
	for i := 0; i < n; i++ {
		s = s[:i+1]
		s[i] = e
	}
	return s
}

func Copy[S ~[]E, E any](dst, src S) int { return copy(dst, src) }

func Clear[S ~[]E, E any](s S) { clear(s) }

func MinMax[T Number | ~string](a, b, c T) (T, T) { return min(a, b, c), max(a, b, c) }

func Len[S ~string | ~[]byte](s S) int { return len(s) }

// Arrays and structs of type parameters: copies and comparisons.
func ArrEq[T comparable](a [3]T, i int, v T) (bool, T) {
	b := a
	b[i] = v
	return a == b, a[i]
}

type Wrap[T comparable] struct {
	v  T
	ok bool
}

func WrapEq[T comparable](a, b T) bool { return Wrap[T]{a, true} == Wrap[T]{b, true} }

// Pointer methods through a *T constraint on elements of a slice.
type Incer[T any] interface {
	*T
	Inc()
}

type C struct{ n int }

func (c *C) Inc() { c.n++ }

func IncAll[T any, P Incer[T]](xs []T) {
	for i := range xs {
		P(&xs[i]).Inc()
	}
}

func Named[T any](v T) (out T, n int) {
	defer func() { n++ }()
	out = v
	n = 1
	return
}

func main() {
	println(Sum(1, 2, 3), Sum[int32](), int(Sum(0.5, 0.25)*100))
	xs := []int{4, 5}
	println(Sum(xs...))
	println(Swapped("a", "b"))
	println(Sum(Two(7, 8)))
	println(Even(10), Odd(int32(7)), Even(3.0))
	println(Lit[[]int]())
	p := StructLit[Point](4)
	println(p.X, p.Y)
	a := ArrLit[[3]string]("z")
	println(a[0] == "", a[2])
	var gbuf [3]string
	g := Grow[[]string](gbuf[:], 3, "g")
	println(len(g), cap(g), g[2])
	var dstArr [2]int
	dst := dstArr[:]
	println(Copy(dst, []int{9, 8, 7}), dst[1])
	Clear(dst)
	println(dst[0], dst[1])
	println(MinMax(3, 1, 2))
	println(MinMax("b", "c", "a"))
	lo, hi := MinMax(2.5, -1.0, 0.0)
	println(int(lo), int(hi))
	println(Len("four"), Len([]byte{1, 2}))
	println(ArrEq([3]string{"a", "b", "c"}, 1, "x"))
	println(ArrEq([3]int{1, 2, 3}, 0, 1))
	println(WrapEq(1, 1), WrapEq("a", "b"), WrapEq(Point{1, 2}, Point{1, 2}))
	var csArr [3]C
	cs := csArr[:]
	IncAll(cs)
	IncAll(cs[1:])
	println(cs[0].n, cs[1].n, cs[2].n)
	v, n := Named("v")
	println(v, n)
}
