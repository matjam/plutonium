package main

// Methods with type parameters of their own.

type Number interface {
	~int | ~int8 | ~int32 | ~int64 | ~uint8
}

// A non-generic receiver type with value and pointer receiver methods.
type Counter struct {
	n    int
	name string
}

func (c Counter) Apply[U any](f func(int) U) U { return f(c.n) }

func (c Counter) Scale[N Number](k N) N { return N(c.n) * k }

func (c *Counter) AddN[N Number](xs ...N) int {
	for _, x := range xs {
		c.n += int(x)
	}
	return c.n
}

// A generic receiver type.
type Stack[T any] struct {
	items [8]T
	n     int
}

func (s *Stack[T]) Push(v T) { s.items[s.n] = v; s.n++ }

// Map has a pointer receiver and a result that mentions both T and U.
func (s *Stack[T]) Map[U any](f func(T) U) Stack[U] {
	var out Stack[U]
	for i := 0; i < s.n; i++ {
		out.Push(f(s.items[i]))
	}
	return out
}

// Fold has a value receiver.
func (s Stack[T]) Fold[A any](init A, f func(A, T) A) A {
	acc := init
	for i := 0; i < s.n; i++ {
		acc = f(acc, s.items[i])
	}
	return acc
}

// MapTwice calls another generic method, with an instantiation that
// depends on its own type parameter.
func (s *Stack[T]) MapTwice[U, V any](f func(T) U, g func(U) V) Stack[V] {
	mid := s.Map(f)
	return mid.Map[V](g)
}

type Pair[K, V any] struct {
	Key K
	Val V
}

type Box[T any] struct{ v T }

func (b Box[T]) With[U any](u U) Pair[T, U] { return Pair[T, U]{b.v, u} }

// Nest instantiates With with a type built from its own type parameter.
func (b Box[T]) Nest[U any](u U) Pair[T, Pair[T, U]] {
	return b.With[Pair[T, U]](b.With(u))
}

// Count's type parameter bound refers to nothing of the receiver, but its
// body uses both.
func (b *Box[T]) Count[E comparable](xs []E, x E) int {
	n := 0
	for _, y := range xs {
		if y == x {
			n++
		}
	}
	return n
}

// Embedding: methods promoted through a value and a pointer field.
type Named struct {
	Counter
	label string
}

type Ref struct {
	*Counter
}

// Embedding a generic type: its generic methods are promoted.
type Tagged struct {
	Stack[int]
	tag string
}

type Wrapped[T any] struct {
	*Stack[T]
}

// A named non-struct type.
type Ints []int

func (xs Ints) Index[E comparable](ys []E, y E) int {
	for i, v := range ys {
		if v == y {
			return xs[i]
		}
	}
	return -1
}

// Each builds a closure that captures values of both kinds of type
// parameter.
func (s *Stack[T]) Each[U any](u U, f func(T, U) int) int {
	total := 0
	g := func(x T) { total += f(x, u) }
	for i := 0; i < s.n; i++ {
		g(s.items[i])
	}
	return total
}

// Kind decides a type switch on its own type parameter per instance.
func (b Box[T]) Kind[U any](u U) string {
	switch any(u).(type) {
	case int:
		return "int"
	case string:
		return "string"
	}
	return "other"
}

// Deref has a type parameter whose bound mentions the receiver's.
func (b Box[T]) Deref[P interface{ ~*T }](p P) T {
	return *p
}

// Generic functions calling generic methods.
func Lengths[T any](s *Stack[T], f func(T) int) Stack[int] {
	return s.Map(f)
}

func Convert[T, U any](s *Stack[T], f func(T) U) Stack[U] {
	return s.Map[U](f)
}

func SumWith[N Number](c Counter, ks []N) N {
	var t N
	for _, k := range ks {
		t += c.Scale(k)
	}
	return t
}

func Wrap[T any](v T) Pair[int, T] {
	return Box[int]{len("abc")}.With[T](v)
}

// ViaValue takes a method value of an instance that depends on its own
// type parameters, and defers a generic method call.
func ViaValue[T, U any](w Wrapped[T], f func(T) U) U {
	m := w.Map[U]
	defer w.Each(f(w.items[0]), func(x T, u U) int {
		println("deferred each")
		return 0
	})
	out := m(f)
	return out.items[out.n-1]
}

// Apply1 takes a generic method value whose type arguments are inferred
// from Apply1's own parameter types.
func Apply1[R any](f func(func(int) R) R, g func(int) R) R { return f(g) }

// Applier returns a method value instantiated by the result type.
func Applier(c Counter) func(func(int) bool) bool { return c.Apply }

// Ider would implement this interface but for its type parameter.
type Ider interface{ Id() int }

func (c Counter) Id[U any]() int { return c.n }

func strlen(s string) int { return len(s) }

func describe(s string) Pair[string, int] { return Pair[string, int]{s, len(s)} }

func main() {
	c := Counter{n: 3, name: "c"}

	// Explicit instantiation and inference, value receiver.
	println("apply", c.Apply[int](func(i int) int { return i * 10 }), c.Apply(func(i int) bool { return i > 2 }))
	println("apply string", c.Apply(func(i int) string {
		if i == 3 {
			return "three"
		}
		return "other"
	}))

	// The same method instantiated with several type arguments.
	println("scale", c.Scale(4), c.Scale(int8(5)), c.Scale[int64](-7), int(c.Scale(uint8(100))))

	// Pointer receiver through an addressable value, and variadic.
	println("addn", c.AddN(1, 2), c.AddN[int32](10), c.AddN[int8](), c.n)
	pc := &c
	println("addn ptr", pc.AddN(int64(4)), pc.Apply(func(i int) int { return -i }))

	// A generic receiver type.
	var names Stack[string]
	names.Push("alpha")
	names.Push("be")
	names.Push("gamma")
	lens := names.Map(strlen)
	println("map", lens.n, lens.items[0], lens.items[1], lens.items[2])
	evens := names.Map[bool](func(s string) bool { return len(s)%2 == 0 })
	println("map bool", evens.items[0], evens.items[1], evens.items[2])
	pairs := names.Map(describe)
	println("map pair", pairs.items[2].Key, pairs.items[2].Val)
	total := lens.Fold(0, func(a int, x int) int { return a + x })
	joined := names.Fold[int64](1, func(a int64, s string) int64 { return a*10 + int64(len(s)) })
	println("fold", total, joined)
	twice := names.MapTwice(strlen, func(n int) bool { return n > 2 })
	println("maptwice", twice.n, twice.items[0], twice.items[1], twice.items[2])
	sp := &names
	println("fold ptr", sp.Fold("", func(a string, s string) string {
		if len(s) > len(a) {
			return s
		}
		return a
	}))

	// Results that nest the type parameters.
	b := Box[string]{"x"}
	p := b.With(42)
	println("with", p.Key, p.Val)
	q := b.Nest(true)
	println("nest", q.Key, q.Val.Key, q.Val.Val)
	println("count", b.Count([]int{1, 2, 1, 1}, 1), b.Count([]string{"a", "b"}, "c"))

	// Promoted methods.
	nm := Named{Counter{n: 7, name: "inner"}, "outer"}
	println("promoted", nm.Apply(func(i int) int { return i + 1 }), nm.Scale(int32(3)), nm.AddN(5), nm.n)
	r := Ref{&Counter{n: 2}}
	println("promoted ptr", r.AddN(8), r.Scale(int64(2)), r.Counter.n)

	// Inside generic functions.
	ls := Lengths(&names, strlen)
	println("generic fn", ls.items[0], ls.items[2])
	cs := Convert(&ls, func(n int) Pair[int, int] { return Pair[int, int]{n, n * n} })
	println("convert", cs.items[1].Key, cs.items[1].Val)
	println("sumwith", SumWith(Counter{n: 2}, []int8{1, 2, 3}), SumWith(Counter{n: 5}, []int{10}))
	w := Wrap("wrapped")
	println("wrap", w.Key, w.Val)

	// Method values after instantiation.
	f := c.Scale[int]
	g := b.With[int]
	println("method value", f(2), g(5).Key, g(5).Val)
	var h func(func(int) string) string = c.Apply
	println("assigned", h(func(i int) string {
		if i > 10 {
			return "big"
		}
		return "small"
	}))
	add := pc.AddN[int]
	add(1)
	println("ptr method value", c.n)
	println("inferred value", Apply1(Counter{n: 4}.Apply, func(i int) int { return i + 1 }))
	println("returned value", Applier(Counter{n: 9})(func(i int) bool { return i == 9 }))
	println("id", c.Id[string](), c.Id[bool]())

	// A method with type parameters does not implement an interface.
	var x any = c
	_, ok := x.(Ider)
	switch x.(type) {
	case Ider:
		println("type switch Ider")
	case Counter:
		println("type switch Counter", ok)
	}

	sc := nm.Scale[int64]
	mm := names.Map[int]
	println("promoted value", sc(3), mm(strlen).items[0])

	// Embedded generic types.
	var tg Tagged
	tg.Push(4)
	tg.Push(9)
	sq := tg.Map(func(n int) int { return n * n })
	println("tagged", sq.items[0], sq.items[1], tg.Fold[string]("", func(a string, n int) string {
		if n > 5 {
			return "big"
		}
		return a
	}))
	wr := Wrapped[string]{&names}
	v1 := ViaValue(wr, strlen)
	v2 := ViaValue(Wrapped[int]{&ls}, func(n int) bool { return n == 5 })
	println("wrapped", v1, v2)

	// Other receivers and type parameter shapes.
	println("ints", Ints{10, 20, 30}.Index([]string{"a", "b", "c"}, "b"), Ints{7}.Index([]bool{true}, false))
	println("each", names.Each(100, func(s string, k int) int { return len(s) * k }))
	println("kind", b.Kind(1), b.Kind("s"), b.Kind(true), b.Kind[Pair[int, int]](Pair[int, int]{}))
	str := "pointed"
	println("deref", b.Deref(&str))

	// Method expressions.
	me := Counter.Scale[int]
	println("method expr", me(Counter{n: 6}, 7))
	var mp func(*Stack[string], func(string) int) Stack[int] = (*Stack[string]).Map
	println("method expr ptr", mp(&names, strlen).items[1])

	// Deferred call of a generic method.
	defer func() { println("deferred", c.n) }()
	defer c.AddN(100)
	defer pc.AddN[int8](1, 1)
}
