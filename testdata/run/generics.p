package main

type Number interface {
	~int | ~int32 | ~int64 | ~float64
}

func Max[T Number](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Sum[T Number](xs []T) T {
	var t T
	for _, x := range xs {
		t += x
	}
	return t
}

func Map[T, U any](xs []T, out []U, f func(T) U) {
	for i, x := range xs {
		out[i] = f(x)
	}
}

type Pair[K, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) Swap() Pair[V, K] { return Pair[V, K]{p.Val, p.Key} }

type Stack[T any] struct {
	items [16]T
	n     int
}

func (s *Stack[T]) Push(v T) { s.items[s.n] = v; s.n++ }
func (s *Stack[T]) Pop() T {
	s.n--
	return s.items[s.n]
}
func (s *Stack[T]) Len() int { return s.n }

type MyInt int

func Index[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

func main() {
	println("max", Max(3, 7), Max(int64(-2), -9), int(Max(1.5, 2.5)*10), Max(MyInt(4), 2))
	println("sum", Sum([]int{1, 2, 3}), int(Sum([]float64{0.5, 0.25})*100))
	in := []int{1, 2, 3}
	var outArr [3]string
	out := outArr[:]
	Map(in, out, func(x int) string {
		if x%2 == 0 {
			return "even"
		}
		return "odd"
	})
	println("map", out[0], out[1], out[2])
	var s Stack[string]
	s.Push("a")
	s.Push("b")
	println("stack", s.Len(), s.Pop(), s.Pop(), s.Len())
	var si Stack[int]
	for i := 0; i < 10; i++ {
		si.Push(i * i)
	}
	println("stack int", si.Pop(), si.Len())
	println("index", Index([]string{"x", "y", "z"}, "z"), Index([]int{5, 6}, 7))
	p := Pair[string, int]{"k", 9}.Swap()
	println("pair", p.Key, p.Val)
}
