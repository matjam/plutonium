// Self-referential (F-bounded) constraints whose methods mention the type
// parameter, generic constraint interfaces with methods returning other
// instantiations, and constraints combining a type set with methods.
package main

type Lesser[T any] interface{ Less(T) bool }

func Min[T Lesser[T]](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if x.Less(m) {
			m = x
		}
	}
	return m
}

type Age int

func (a Age) Less(b Age) bool { return a < b }

type Name struct{ s string }

func (n *Name) Less(m *Name) bool { return n.s < m.s }

type Cloner[T any] interface{ Clone() T }

type Doc struct{ rev int }

func (d Doc) Clone() Doc { return Doc{d.rev + 1} }

func CloneAll[T Cloner[T]](xs []T) {
	for i := range xs {
		xs[i] = xs[i].Clone()
	}
}

type Converter[From, To any] interface{ Convert(From) To }

type Doubler struct{ k int }

func (d Doubler) Convert(x int) int64 { return int64(x * 2 * d.k) }

func ConvertAll[C Converter[F, T], F, T any](c C, xs []F, out []T) {
	for i, x := range xs {
		out[i] = c.Convert(x)
	}
}

type Mapper[T any] interface {
	Map(func(T) T) Box[T]
}

type Box[T any] struct{ v T }

func (b Box[T]) Map(f func(T) T) Box[T] { return Box[T]{f(b.v)} }

func Twice[M Mapper[T], T any](m M, f func(T) T) T { return m.Map(f).Map(f).v }

type Number interface {
	~int | ~int64
	Abs() int64
}

type Neg int64

func (n Neg) Abs() int64 {
	if n < 0 {
		return int64(-n)
	}
	return int64(n)
}

func SumAbs[T Number](xs []T) (T, int64) {
	var s T
	var a int64
	for _, x := range xs {
		s += x
		a += x.Abs()
	}
	return s, a
}

func main() {
	println(Min(Age(30), Age(7), Age(19)))
	a, b, c := &Name{"m"}, &Name{"c"}, &Name{"x"}
	println(Min(a, b, c).s)
	ds := []Doc{{1}, {5}}
	CloneAll(ds)
	println(ds[0].rev, ds[1].rev)
	out := make([]int64, 3)
	ConvertAll(Doubler{10}, []int{1, 2, 3}, out)
	println(out[0], out[2])
	println(Twice(Box[int]{3}, func(x int) int { return x * x }), Twice[Box[string]](Box[string]{"ab"}, func(s string) string { return s[1:] }) == "")
	println(SumAbs([]Neg{-3, 5, -10}))
}
