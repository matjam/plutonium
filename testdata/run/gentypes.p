// Type switches on any(x), generic structs with methods, recursive generic
// types, generic type aliases, zero values and inference corner cases.
package main

type Kind int

func Describe[T any](x T) string {
	switch v := any(x).(type) {
	case nil:
		return "nil"
	case int:
		if v < 0 {
			return "negative int"
		}
		return "int"
	case Kind:
		return "kind"
	case string:
		return "string"
	case []T:
		return "slice of T"
	case *T:
		return "pointer to T"
	case error:
		return "error"
	case interface{ Len() int }:
		if v.Len() > 2 {
			return "long"
		}
		return "short"
	case struct{ a T }:
		return "struct of T"
	default:
		return "other"
	}
}

type Sized [3]int

func (s Sized) Len() int { return len(s) }

type errT struct{}

func (*errT) Error() string { return "e" }

// Two type parameters in one switch; a case may match both.
func Same[T, U any](x T) string {
	switch any(x).(type) {
	case U:
		return "U"
	case T:
		return "T only"
	}
	return "none"
}

// A recursive generic type.
type List[T any] struct {
	head T
	tail *List[T]
}

func (l *List[T]) Push(v T) *List[T] { return new(List[T]{v, l}) }

func (l *List[T]) Len() int {
	if l == nil {
		return 0
	}
	return 1 + l.tail.Len()
}

func (l *List[T]) Each(f func(T)) {
	for ; l != nil; l = l.tail {
		f(l.head)
	}
}

type Tree[K interface{ ~int | ~string }, V any] struct {
	left, right *Tree[K, V]
	key         K
	val         V
}

func (t *Tree[K, V]) Insert(k K, v V, store []Tree[K, V], n *int) *Tree[K, V] {
	if t == nil {
		node := &store[*n]
		*n++
		node.key, node.val = k, v
		return node
	}
	switch {
	case k < t.key:
		t.left = t.left.Insert(k, v, store, n)
	case k > t.key:
		t.right = t.right.Insert(k, v, store, n)
	default:
		t.val = v
	}
	return t
}

func (t *Tree[K, V]) Walk(f func(K, V)) {
	if t == nil {
		return
	}
	t.left.Walk(f)
	f(t.key, t.val)
	t.right.Walk(f)
}

// Mutually recursive generic types.
type Even[T any] struct {
	v    T
	next *Odd[T]
}

type Odd[T any] struct {
	v    T
	next *Even[T]
}

func countEven[T any](e *Even[T]) int {
	if e == nil {
		return 0
	}
	if e.next == nil {
		return 1
	}
	return 2 + countEven(e.next.next)
}

// Generic type aliases (Go 1.24).
type Pair[A, B any] struct {
	a A
	b B
}

type Twin[T any] = Pair[T, T]

type IntPair = Pair[int, int]

type Vec[T any] = []T

func (p Pair[A, B]) Swap() Pair[B, A] { return Pair[B, A]{p.b, p.a} }

func MakeTwin[T any](v T) Twin[T] { return Twin[T]{v, v} }

func SumVec(v Vec[int]) int {
	t := 0
	for _, x := range v {
		t += x
	}
	return t
}

// Zero values of type parameters.
func Zero[T any]() T {
	var z T
	return z
}

func IsZero[T comparable](x T) bool {
	var z T
	return x == z
}

func ZeroAfterUse[T any](x T) T {
	z := x
	var zero T
	z = zero
	return z
}

type Big struct {
	a [5]int
	s string
	p *int
}

// Inference.
func Map[T, U any](xs []T, f func(T) U, out []U) []U {
	out = out[:len(xs)]
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

func Apply[T any](x T, fs ...func(T) T) T {
	for _, f := range fs {
		x = f(x)
	}
	return x
}

func Double[T ~int | ~float64](x T) T { return x * 2 }

func First[S ~[]E, E any](s S) E { return s[0] }

func Ptr[T any](p *T) T { return *p }

func Pick[T any](a, b T, first bool) T {
	if first {
		return a
	}
	return b
}

func Keys[K comparable, V any](ps []Pair[K, V], out []K) []K {
	out = out[:len(ps)]
	for i, p := range ps {
		out[i] = p.a
	}
	return out
}

func Compose[A, B, C any](f func(A) B, g func(B) C) func(A) C {
	return nil
}

func CallWith[T any](f func(T) int, x T) int { return f(x) }

type Celsius float64

func main() {
	var ip *int
	println(Describe(3), Describe(-3), Describe(Kind(1)), Describe("s"), Describe(ip))
	println(Describe([]int{1}), Describe([]string{}), Describe[any](nil), Describe(Sized{}))
	println(Describe(struct{ a int }{1}), Describe(struct{ a string }{}), Describe(2.5))
	var e *errT
	println(Describe(e), Describe[error](e))
	println(Same[int, int](1), Same[int, string](1), Same[int, any](1), Same[Kind, int](1))

	var l *List[string]
	l = l.Push("a").Push("b").Push("c")
	println(l.Len(), l.head, l.tail.head)
	l.Each(func(s string) { print(s, " ") })
	println()

	var storeArr [8]Tree[string, int]
	store := storeArr[:]
	var root *Tree[string, int]
	n := 0
	for i, k := range []string{"m", "c", "x", "a", "c"} {
		root = root.Insert(k, i, store, &n)
	}
	root.Walk(func(k string, v int) { print(k, "=", v, " ") })
	println(n)

	o2 := &Odd[int]{v: 2}
	e1 := &Even[int]{v: 1, next: o2}
	o2.next = &Even[int]{v: 3}
	println(countEven(e1))

	t := MakeTwin(4)
	var ipair IntPair = t
	sw := Pair[string, int]{"k", 1}.Swap()
	println(t.a, t.b, ipair.a, sw.a, sw.b, SumVec(Vec[int]{1, 2, 3}))

	println(Zero[int](), Zero[string]() == "", Zero[*int]() == nil, Zero[any]() == nil, Zero[[3]int]()[2])
	b := Zero[Big]()
	println(b.a[4], b.s == "", b.p == nil, Zero[Pair[int, string]]().a)
	println(IsZero(0), IsZero(1), IsZero(""), IsZero(Pair[int, int]{}), IsZero[any](nil), IsZero(e))
	println(ZeroAfterUse(5), ZeroAfterUse("x") == "", ZeroAfterUse(Big{s: "q"}).s == "")

	var sbuf [3]string
	var bbuf [3][]byte
	strs := Map([]int{1, 2, 3}, func(i int) string { return "abc"[i-1 : i] }, sbuf[:])
	println(strs[0], strs[2], len(Map(strs, func(s string) []byte { return nil }, bbuf[:])))
	println(Apply(3, Double[int], Double[int]), int(Apply(1.5, Double)), Apply(Celsius(2), Double[Celsius]) == 4)
	println(First([]string{"x"}), First(Vec[int]{9}), Ptr(&n), Pick(1, 2.5, false) == 2.5, Pick[int8](1, 2, true))
	var kbuf [2]string
	ks := Keys([]Pair[string, bool]{{"p", true}, {"q", false}}, kbuf[:])
	println(len(ks), ks[1], Compose(Double[int], Double[int]) == nil)
	println(CallWith(func(s string) int { return len(s) }, "four"), CallWith(Double[int], 21))
}
