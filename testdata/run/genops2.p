// More operations on values of type parameters: shifts with a type
// parameter count, range over a core array type (which copies) and over a
// pointer to an array, switches with constant cases, comparison with nil,
// calls through a func-typed type parameter, renamed and blank receiver
// type parameters, large values of type parameter type, promoted methods
// of an embedded pointer to a generic type, and conversions to interfaces.
package main

type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

func Shl[T, U Integer](x T, n U) T { return x << n }
func Shr[T, U Integer](x T, n U) T { return x >> n }

func RangeCopy[A ~[3]E, E Integer](a A) E {
	var sum E
	for i, v := range a {
		if i == 0 {
			a[1] = 100
			a[2] = 100
		}
		sum += v
	}
	return sum + a[2]
}

func RangePtr[P ~*[3]E, E Integer](p P) E {
	var sum E
	for i, v := range p {
		if i == 0 {
			p[2] = 50
		}
		sum += v
	}
	return sum
}

func Classify[T ~int | ~uint8](x T) string {
	switch x {
	case 0:
		return "zero"
	case 1, 2:
		return "small"
	case 255:
		return "max8"
	}
	return "other"
}

func IsNil[T ~*int | ~*string](p T) bool { return p == nil }

func IsNilSlice[S ~[]E, E any](s S) bool { return s == nil }

func Call[F ~func(int) int](f F, x int) int { return f(f(x)) }

type IntFn func(int) int

type Vec[T any] struct {
	items [4]T
	n     int
}

func (v *Vec[U]) Push(x U) { v.items[v.n] = x; v.n++ }

func (v Vec[_]) Len() int { return v.n }

func (v *Vec[V]) At(i int) V { return v.items[i] }

type Outer struct {
	name string
	*Vec[string]
}

type Big struct {
	a [8]int
	s string
}

func Swap3[T any](a, b, c T) (T, T, T) { return c, a, b }

func Make[T any](n int, v T) [4]T {
	var out [4]T
	for i := 0; i < n; i++ {
		out[i] = v
	}
	return out
}

func ToAny[T any](x T) any { return x }

func Back[T any](x any) T { return x.(T) }

func SameAny[T any](a, b T) bool { return any(a) == any(b) }

var calls int

func arr3() [3]int {
	calls++
	return [3]int{1, 2, 3}
}

// Range over arrays outside generic code: the expression is evaluated
// once (a copy) when there is a value variable, not at all when there is
// none and its length is constant, and once when it calls a function.
func rangeArrays() {
	n := 0
	a := [3]int{1, 2, 3}
	for i, v := range a {
		a[2] = 10 * (i + 1)
		n += v
	}
	println(n, a[2])
	var np *[4]int
	for i := range np {
		n += i
	}
	for i := range arr3() {
		n += i
	}
	for _, v := range arr3() {
		n += v
	}
	for range [2]int{} {
		n++
	}
	println(n, calls)
}

func main() {
	println(Shl(int8(1), uint(7)), Shl(1, int8(70)), Shr(int16(-256), uint8(4)), Shr(uint32(1<<31), 31))
	println(Shl(uint8(3), uint64(9)), Shr(int64(-1), uint16(200)), Shr(-8, 1))
	println(RangeCopy([3]int{1, 2, 3}))
	rangeArrays()
	arr := [3]uint8{1, 2, 3}
	println(RangePtr(&arr), arr[2])
	println(Classify(0), Classify(uint8(2)), Classify(uint8(255)), Classify(255), Classify(7))
	var ip *int
	s := "s"
	println(IsNil(ip), IsNil(&s), IsNilSlice([]int(nil)), IsNilSlice([]int{}))
	double := func(x int) int { return x * 2 }
	println(Call(double, 3), Call(IntFn(func(x int) int { return x + 1 }), 3))

	v := &Vec[string]{}
	v.Push("a")
	o := Outer{"o", v}
	o.Push("b")
	push := o.Push
	push("c")
	println(o.Len(), o.At(2), v.n, Vec[int]{}.Len())

	b1, b2, b3 := Swap3(Big{s: "1"}, Big{s: "2"}, Big{a: [8]int{7: 9}, s: "3"})
	println(b1.s, b2.s, b3.s, b1.a[7])
	m := Make(3, Big{s: "m"})
	println(m[2].s, m[3].s == "")
	// Values that do not fit in a word are boxed in the converting
	// function's frame, so only word-sized ones come back from ToAny.
	println(Back[Big](any(Big{s: "boxed"})).s, Back[float64](ToAny(2.5)) == 2.5, Back[*int](ToAny(ip)) == nil)
	println(ToAny(int8(-3)) == any(int8(-3)), SameAny("x", "x"), SameAny(Big{}, Big{}), SameAny(Big{s: "a"}, Big{}))
}
