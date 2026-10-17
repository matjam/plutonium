// Closures inside generic functions, instantiated generic functions as
// values, defer in generic code, iterator functions, and generic methods.
package main

import "pluto/strings"

type Number interface{ ~int | ~int64 | ~float64 }

// Closures capturing values and variables of type parameter types.
func Accumulate[T Number](xs []T) (T, T) {
	var sum, max T
	add := func(x T) {
		sum += x
		if x > max {
			max = x
		}
	}
	for _, x := range xs {
		add(x)
	}
	return sum, max
}

func Counter[T any](xs []T, pred func(T) bool) int {
	n := 0
	visit := func(x T) {
		if pred(x) {
			n++
		}
	}
	for _, x := range xs {
		visit(x)
	}
	return n
}

func Nested[T any](x T) T {
	var out T
	outer := func() {
		inner := func(v T) { out = v }
		inner(x)
	}
	outer()
	return out
}

func Immediate[K comparable](k K, ks []K) int {
	return func() int {
		for i, v := range ks {
			if v == k {
				return i
			}
		}
		return -1
	}()
}

// A closure inside a generic method.
type Bag[T comparable] struct {
	items [8]T
	n     int
}

func (b *Bag[T]) Add(xs ...T) {
	push := func(x T) {
		b.items[b.n] = x
		b.n++
	}
	for _, x := range xs {
		push(x)
	}
}

func (b *Bag[T]) Has(x T) bool {
	found := false
	b.each(func(y T) {
		if y == x {
			found = true
		}
	})
	return found
}

func (b *Bag[T]) each(f func(T)) {
	for i := 0; i < b.n; i++ {
		f(b.items[i])
	}
}

// Instantiated generic functions as values.
func Add[T Number](a, b T) T { return a + b }
func Mul[T Number](a, b T) T { return a * b }

func Fold[T any](xs []T, z T, f func(T, T) T) T {
	for _, x := range xs {
		z = f(z, x)
	}
	return z
}

var addInt = Add[int]
var ops = [2]func(int64, int64) int64{Add[int64], Mul[int64]}

type Op[T Number] struct {
	name string
	f    func(T, T) T
}

func Table[T Number](a, b T) [2]T {
	tab := [2]Op[T]{{"add", Add[T]}, {"mul", Mul[T]}}
	var out [2]T
	for i, op := range tab {
		out[i] = op.f(a, b)
	}
	return out
}

// Defer in generic code.
func Deferred[T Number](x T) (r T) {
	defer func() { r *= 2 }()
	defer Record(x)
	r = x + 1
	return r
}

var log [8]int
var logN int

func Record[T Number](x T) {
	log[logN] = int(x)
	logN++
}

type Res[T any] struct{ v T }

func (r *Res[T]) Close() { log[logN] = -1; logN++ }

func Use[T any](v T) T {
	r := &Res[T]{v}
	defer r.Close()
	return r.v
}

// Push iterators: a generic function calls a closure for each element.
type Yield[V any] func(V) bool

func Values[S ~[]E, E any](s S, yield Yield[E]) {
	for _, v := range s {
		if !yield(v) {
			return
		}
	}
}

func Count[T Number](n T, yield func(int, T) bool) {
	var v T
	for i := 0; v < n; i++ {
		if !yield(i, v) {
			return
		}
		v++
	}
}

func Collect[V any](s []V, out []V) int {
	n := 0
	Values(s, func(v V) bool {
		out[n] = v
		n++
		return true
	})
	return n
}

// Generic methods (methods with their own type parameters).
type Stack[T any] struct {
	items [8]T
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

func (s Stack[T]) Reduce[A any](a A, f func(A, T) A) A {
	for i := 0; i < s.n; i++ {
		a = f(a, s.items[i])
	}
	return a
}

func main() {
	println(Accumulate([]int{3, 9, 2}))
	s, m := Accumulate([]float64{0.5, 1.5})
	println(int(s*10), int(m*10))
	println(Counter([]string{"a", "bb", "ccc"}, func(s string) bool { return len(s) > 1 }))
	println(Nested(7), Nested("n"), Immediate("c", []string{"a", "b", "c"}), Immediate(4, nil))
	var b Bag[string]
	b.Add("x", "y")
	println(b.n, b.Has("y"), b.Has("z"))

	println(addInt(2, 3), ops[0](4, 5), ops[1](4, 5), Fold([]int{1, 2, 3, 4}, 1, Mul[int]))
	f := Add[float64]
	println(int(f(0.25, 0.5)*100), Table(3, 4)[1], Table[int64](-2, 5)[0])
	g := Fold[string]
	println(g([]string{"a", "b"}, "", func(a, b string) string {
		if len(a) > len(b) {
			return a
		}
		return b
	}))

	println(Deferred(4), Deferred(int64(-1)), Use(9), Use("u"))
	for i := 0; i < logN; i++ {
		print(log[i], " ")
	}
	println()

	var out [4]int
	println(Collect([]int{5, 6, 7}, out[:]), out[2])
	Values([]string{"p", "q", "r"}, func(v string) bool {
		if v == "r" {
			return false
		}
		print(v)
		return true
	})
	println()
	Count(3.0, func(i int, v float64) bool {
		print(i, ":", int(v), " ")
		return true
	})
	println()

	var st Stack[int]
	st.Push(1)
	st.Push(20)
	lens := st.Map(func(x int) string { return strings.Slice("abcdefghijklmnopqrstuvwxyz", 0, x) })
	println(lens.n, lens.items[1], st.Reduce(100, func(a int, x int) int { return a - x }))
	println(lens.Reduce(0, func(a int, s string) int { return a + len(s) }), st.Map[bool](func(x int) bool { return x > 5 }).items[1])
}
