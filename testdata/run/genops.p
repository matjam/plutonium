// Type sets, ~T constraints and comparable; operations allowed on values
// of type parameters through their core types: arithmetic, comparison,
// indexing, len, cap, slicing, range, conversions between type parameters
// and to and from interfaces.
package main

type Signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type Unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type Integer interface{ Signed | Unsigned }

type Float interface{ ~float32 | ~float64 }

type Ordered interface {
	Integer | Float | ~string
}

type Celsius int16
type Name string
type Bits uint8
type IntID int

func Max[T Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Clamp[T Ordered](x, lo, hi T) T { return Max(lo, min(x, hi)) }

func Abs[T Signed | Float](x T) T {
	if x < 0 {
		return -x
	}
	return x
}

func Ops[T Integer](a, b T) (T, T, T, T, T, T, T, T) {
	return a + b, a - b, a * b, a / b, a % b, a & b, a | b, a ^ b
}

func Shifts[T Integer](x T, n uint) (T, T, T) {
	return x << n, x >> n, ^x
}

func Wrap[T Integer](x T) T {
	x++
	x += 100
	x *= 3
	return x
}

func Avg[T Float](xs []T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s / T(len(xs))
}

func Count[T comparable](xs []T, v T) int {
	n := 0
	for i := range xs {
		if xs[i] == v {
			n++
		}
	}
	return n
}

type Key struct {
	a int
	b string
}

// Conversions between type parameters with numeric core types.
func Conv[T, U Integer | Float](x T) U { return U(x) }

func Trunc[F Float, I Integer](f F) I { return I(f) }

// String-like type sets: indexing, len, slicing and range over bytes or runes.
func FirstByte[S ~string | ~[]byte](s S) byte {
	if len(s) == 0 {
		return 0
	}
	return s[0]
}

func Runes[S ~string](s S) int {
	n := 0
	for _, r := range s {
		if r > 127 {
			n += 10
		} else {
			n++
		}
	}
	return n
}

func Tail[S ~string](s S, i int) S { return s[i:] }

// Slices and arrays through core types.
func SumSlice[S ~[]E, E Integer](s S) E {
	var t E
	for _, v := range s {
		t += v
	}
	return t
}

func Reverse[S ~[]E, E any](s S) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func Window[S ~[]E, E any](s S, lo, hi int) S { return s[lo:hi:hi] }

func ArrLen[A ~[4]E, E any](a A) (int, E) { return len(a), a[3] }

func PtrArr[P ~*[3]int](p P) int { return p[0] + len(p) }

func CapOf[S ~[]E, E any](s S) int { return cap(s) }

type IntList []int

func Last[E any](s []E) E { return s[len(s)-1] }

// Range over an integer of type parameter type.
func Triangle[T ~int](n T) T {
	var t T
	for i := range n {
		t += i
	}
	return t
}

// Interfaces: a type parameter value converts to an interface, and back
// by assertion.
type Shower interface{ Show() int }

type Point struct{ x, y int }

func (p Point) Show() int { return p.x*10 + p.y }

func Box[T any](x T) any { return x }

func Unbox[T any](v any) (T, bool) {
	t, ok := v.(T)
	return t, ok
}

func ToShower[T Shower](x T) Shower { return x }

func IsShower[T any](x T) bool {
	_, ok := any(x).(Shower)
	return ok
}

func Equal[T comparable](a, b T) bool {
	var ia, ib any = a, b
	return ia == ib && a == b
}

func main() {
	println(Max(3, 7), Max(int8(-3), -7), Max(Celsius(10), 5), Max(Name("b"), "a"), Max("abc", "abd"))
	println(Clamp(15, 0, 10), Clamp(-5, 0, 10), Clamp(Bits(200), 10, 100))
	println(Abs(-4), Abs(int8(-128)), int(Abs(-2.5)*10), Abs(Celsius(-3)))
	println(Ops(17, 5))
	println(Ops(uint8(250), 7))
	println(Ops(int8(-17), 5))
	println(Shifts(int8(-64), 2))
	println(Shifts(uint16(0x8001), 3))
	println(Wrap(uint8(100)), Wrap(int8(100)), Wrap(Bits(1)))
	println(int(Avg([]float64{1, 2, 4}) * 100), int(Avg([]float32{1, 2}) * 100))
	println(Count([]int{1, 2, 1, 3, 1}, 1), Count([]string{"a", "b"}, "c"), Count([]Key{{1, "x"}, {1, "y"}, {1, "x"}}, Key{1, "x"}))
	println(Count([]*int{nil, nil}, nil), Count([]any{1, "x", 1, 2.5}, any(1)), Count([][2]int{{1, 2}, {2, 1}}, [2]int{2, 1}))
	println(Conv[int, uint8](300), Conv[int8, int](-1), int(Conv[int, float64](7)*2), Conv[float64, int](-3.9), Conv[uint8, int8](200))
	println(Trunc[float64, int16](1234.9), Trunc[float32, uint8](255.5), Conv[Celsius, Bits](Celsius(-1)))
	println(FirstByte("hello"), FirstByte([]byte{9, 8}), FirstByte(Name("")), Runes("aé€b"), Tail(Name("prefix"), 3))
	println(SumSlice([]int{1, 2, 3}), SumSlice(IntList{4, 5}), SumSlice([]uint8{200, 100}))
	xs := IntList{1, 2, 3, 4, 5}
	Reverse(xs)
	println(xs[0], xs[4], len(Window(xs, 1, 3)), cap(Window(xs, 1, 3)), Last(xs))
	println(ArrLen([4]string{"a", "b", "c", "d"}))
	arr := [3]int{5, 6, 7}
	println(PtrArr(&arr), CapOf(make([]int, 2, 9)), CapOf(xs[1:]))
	println(Triangle(5), Triangle(IntID(30)))
	b := Box(Point{1, 2})
	p, ok := Unbox[Point](b)
	println(p.x, p.y, ok)
	_, ok = Unbox[int](b)
	println(ok)
	n, ok := Unbox[int](Box(42))
	println(n, ok)
	println(ToShower(Point{3, 4}).Show(), IsShower(Point{}), IsShower(3))
	println(Equal(1, 1), Equal("a", "b"), Equal(Point{1, 2}, Point{1, 2}), Equal[any](1, "1"))
}
